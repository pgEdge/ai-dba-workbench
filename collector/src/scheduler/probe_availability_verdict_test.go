/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Tests covering the probe availability verdict for extension probes
// whose extension is installed in some databases and not others, or is
// installed but has nothing to report, as Spock does on a quiet pgEdge
// cluster (#612).
package scheduler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/collector/src/probes"
)

// stubResult is one canned probe.Execute outcome.
type stubResult struct {
	metrics []map[string]any
	err     error
}

// perDatabaseExtensionProbe is an ExtensionProbe whose Execute result
// depends on the database it is run against, so a connection where the
// extension is installed in one database and absent from the rest can be
// driven deterministically. Databases missing from results get fallback.
type perDatabaseExtensionProbe struct {
	extensionProbeStub
	results  map[string]stubResult
	fallback stubResult
}

func (p *perDatabaseExtensionProbe) Execute(_ context.Context, _ string, conn *pgxpool.Conn, _ int) ([]map[string]any, error) {
	r, ok := p.results[conn.Conn().Config().Database]
	if !ok {
		r = p.fallback
	}
	if r.metrics == nil {
		return nil, r.err
	}
	// Hand back fresh maps so the scheduler's _database_name tagging of
	// one database's rows cannot leak into another's.
	out := make([]map[string]any, 0, len(r.metrics))
	for _, m := range r.metrics {
		row := make(map[string]any, len(m))
		for k, v := range m {
			row[k] = v
		}
		out = append(out, row)
	}
	return out, r.err
}

// TestAvailabilityVerdict covers the decision executeProbeForConnection
// records, including statuses merged across several databases. An
// unknown status no longer reads as "not installed": every
// ExtensionProbe reports absence with ErrExtensionNotInstalled, so
// extensionUnknown only arises when every execution failed.
func TestAvailabilityVerdict(t *testing.T) {
	spock := "spock"
	cases := []struct {
		name       string
		stored     int
		extName    *string
		status     extensionStatus
		wantAvail  bool
		wantReason string
	}{
		{"present with rows", 3, &spock, extensionPresent, true, ""},
		{"present but empty", 0, &spock, extensionPresent, true, ""},
		{"absent", 0, &spock, extensionAbsent, false,
			"extension 'spock' not installed"},
		{"absent in postgres, present in pgedge", 0, &spock,
			extensionAbsent.merge(extensionPresent), true, ""},
		{"present in pgedge, absent in postgres", 0, &spock,
			extensionPresent.merge(extensionAbsent), true, ""},
		{"absent in one, failed in another", 0, &spock,
			extensionAbsent.merge(extensionUnknown), false,
			"extension 'spock' not installed"},
		{"every execution failed", 0, &spock, extensionUnknown, false,
			"probe execution failed for extension 'spock'"},
		{"rows stored despite unknown status", 1, &spock, extensionUnknown, true, ""},
		{"non-extension probe without rows", 0, nil, extensionUnknown, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			avail, reason := availabilityVerdict(tc.stored, tc.extName, tc.status)
			if avail != tc.wantAvail {
				t.Errorf("available = %v, want %v", avail, tc.wantAvail)
			}
			switch {
			case tc.wantReason == "" && reason != nil:
				t.Errorf("reason = %q, want nil", *reason)
			case tc.wantReason != "" && reason == nil:
				t.Errorf("reason = nil, want %q", tc.wantReason)
			case tc.wantReason != "" && *reason != tc.wantReason:
				t.Errorf("reason = %q, want %q", *reason, tc.wantReason)
			}
		})
	}
}

// TestExecuteProbeForConnection_ExtensionInOneDatabase reproduces the
// pgEdge layout behind #612: Spock installed in an application database
// and absent from the rest, with nothing in its 15-minute window. The
// merged outcome must be "available" whichever database is visited
// first, so the case runs with Spock in the connection's default
// database and with Spock only in a database visited later.
func TestExecuteProbeForConnection_ExtensionInOneDatabase(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), testServerSecret)
	defer ps.Stop()

	absent := stubResult{err: probes.ErrExtensionNotInstalled}
	presentEmpty := stubResult{metrics: []map[string]any{}}

	cases := []struct {
		name    string
		spockIn string
	}{
		{"present in default database only", f.dbName},
		{"present in a later database only", "postgres"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			probeName := fmt.Sprintf("ext-one-db-%d", i)
			probe := &perDatabaseExtensionProbe{
				extensionProbeStub: extensionProbeStub{
					probeName: probeName,
					extension: "spock",
					dbScoped:  true,
				},
				results:  map[string]stubResult{tc.spockIn: presentEmpty},
				fallback: absent,
			}

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			ps.executeProbeForConnection(ctx, probe, makeMonitoredConn(f))

			available, reason := readAvailability(t, f, probeName)
			if !available {
				t.Errorf("is_available = false, want true when spock is installed in %s",
					tc.spockIn)
			}
			if reason != nil {
				t.Errorf("unavailable_reason = %q, want NULL", *reason)
			}
		})
	}
}

// TestExecuteProbeForConnection_SpockQuietCluster is the end-to-end
// regression test for #612, driving the real Spock probes rather than a
// stub. A throwaway database stands in for pgEdge's application
// database, with a stub Spock registered in it and nothing in the
// 15-minute window, whilst every other database on the server has no
// Spock. Both probes must be recorded as available.
func TestExecuteProbeForConnection_SpockQuietCluster(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), testServerSecret)
	defer ps.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	spockDB := fmt.Sprintf("%s_spock", f.dbName)
	adminPool, err := pgxpool.New(ctx, fmt.Sprintf(
		"host=%s port=%d user=%s password=%s sslmode=disable dbname=postgres pool_max_conns=1",
		f.host, f.port, f.username, f.rawPassword))
	if err != nil {
		t.Fatalf("connect to admin database: %v", err)
	}
	// Registered first so it runs last, after the drop below.
	t.Cleanup(adminPool.Close)

	if _, err := adminPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", spockDB)); err != nil {
		t.Fatalf("create database %s: %v", spockDB, err)
	}
	t.Cleanup(func() {
		// The scheduler's pool manager still holds connections to the
		// database, so the drop has to be forced; once it is gone the
		// database is no longer enumerated and that pool is never used.
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, err := adminPool.Exec(cleanupCtx, fmt.Sprintf(
			"DROP DATABASE IF EXISTS %s WITH (FORCE)", spockDB)); err != nil {
			t.Logf("drop database %s: %v", spockDB, err)
		}
	})

	spockPool, err := pgxpool.New(ctx, fmt.Sprintf(
		"host=%s port=%d user=%s password=%s sslmode=disable dbname=%s pool_max_conns=1",
		f.host, f.port, f.username, f.rawPassword, spockDB))
	if err != nil {
		t.Fatalf("connect to %s: %v", spockDB, err)
	}
	defer spockPool.Close()

	// Only the columns the probes select are needed; registering the
	// extension in pg_extension is what CheckExtensionExists looks for.
	// A single literal script, run over the simple protocol since it
	// takes no arguments.
	if _, err := spockPool.Exec(ctx, `
		CREATE SCHEMA spock;
		CREATE TABLE spock.exception_log (
			remote_origin OID, remote_commit_ts TIMESTAMPTZ,
			command_counter INTEGER, retry_errored_at TIMESTAMPTZ,
			remote_xid BIGINT, local_origin OID,
			local_commit_ts TIMESTAMPTZ, table_schema TEXT,
			table_name TEXT, operation TEXT, local_tup JSONB,
			remote_old_tup JSONB, remote_new_tup JSONB,
			ddl_statement TEXT, ddl_user TEXT, error_message TEXT);
		CREATE TABLE spock.resolutions (
			id BIGINT, node_name TEXT, log_time TIMESTAMPTZ,
			relname TEXT, idxname TEXT, conflict_type TEXT,
			conflict_resolution TEXT, local_origin INTEGER,
			local_tuple TEXT, local_xid XID,
			local_timestamp TIMESTAMPTZ, remote_origin INTEGER,
			remote_tuple TEXT, remote_xid XID,
			remote_timestamp TIMESTAMPTZ, remote_lsn PG_LSN);
		INSERT INTO pg_extension (oid, extname, extowner,
			extnamespace, extrelocatable, extversion,
			extconfig, extcondition)
			SELECT (SELECT MAX(oid::oid::int) FROM pg_extension) + 1,
				'spock', 10,
				(SELECT oid FROM pg_namespace WHERE nspname = 'spock'),
				TRUE, '5.0', NULL, NULL`); err != nil {
		t.Fatalf("set up stub spock: %v", err)
	}

	for _, probe := range []probes.MetricsProbe{
		probes.NewSpockExceptionLogProbe(&probes.ProbeConfig{Name: probes.ProbeNameSpockExceptionLog}),
		probes.NewSpockResolutionsProbe(&probes.ProbeConfig{Name: probes.ProbeNameSpockResolutions}),
	} {
		name := probe.GetConfig().Name
		t.Run(name, func(t *testing.T) {
			ps.executeProbeForConnection(ctx, probe, makeMonitoredConn(f))

			available, reason := readAvailability(t, f, name)
			if !available {
				t.Errorf("is_available = false, want true with spock installed in %s", spockDB)
			}
			if reason != nil {
				t.Errorf("unavailable_reason = %q, want NULL", *reason)
			}
		})
	}
}

// TestExecuteProbeForConnection_ExtensionPresentWithRows covers the
// collecting case: rows are stored and the probe is recorded available.
func TestExecuteProbeForConnection_ExtensionPresentWithRows(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), testServerSecret)
	defer ps.Stop()

	for _, dbScoped := range []bool{true, false} {
		t.Run(fmt.Sprintf("dbScoped=%v", dbScoped), func(t *testing.T) {
			probeName := fmt.Sprintf("ext-present-rows-%v", dbScoped)
			probe := &perDatabaseExtensionProbe{
				extensionProbeStub: extensionProbeStub{
					probeName: probeName,
					extension: "spock",
					dbScoped:  dbScoped,
				},
				fallback: stubResult{metrics: []map[string]any{{"n": 1}}},
			}

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			ps.executeProbeForConnection(ctx, probe, makeMonitoredConn(f))

			available, reason := readAvailability(t, f, probeName)
			if !available {
				t.Error("is_available = false, want true for a probe that stored rows")
			}
			if reason != nil {
				t.Errorf("unavailable_reason = %q, want NULL", *reason)
			}
		})
	}
}

// TestExecuteProbeForConnection_ExtensionProbeFails checks that an
// extension probe whose every execution failed is no longer recorded as
// a missing extension, which sent operators looking for a problem that
// was not there.
func TestExecuteProbeForConnection_ExtensionProbeFails(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), testServerSecret)
	defer ps.Stop()

	for _, dbScoped := range []bool{true, false} {
		t.Run(fmt.Sprintf("dbScoped=%v", dbScoped), func(t *testing.T) {
			probeName := fmt.Sprintf("ext-fails-%v", dbScoped)
			probe := &extensionProbeStub{
				probeName: probeName,
				extension: "spock",
				dbScoped:  dbScoped,
				err:       fmt.Errorf("relation does not exist"),
			}

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			ps.executeProbeForConnection(ctx, probe, makeMonitoredConn(f))

			available, reason := readAvailability(t, f, probeName)
			if available {
				t.Error("is_available = true, want false when every execution failed")
			}
			want := "probe execution failed for extension 'spock'"
			if reason == nil || *reason != want {
				t.Errorf("unavailable_reason = %v, want %q", reason, want)
			}
		})
	}
}
