/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Tests covering how the scheduler tells an absent extension apart from
// one that is installed but has nothing to report (#438), and the
// CONNECT-privilege filter on the database enumeration (#440).
package scheduler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/collector/src/probes"
)

// extensionProbeStub is an ExtensionProbe whose Execute returns a
// canned result, so both the "extension absent" and "extension present
// but empty" outcomes can be driven deterministically.
type extensionProbeStub struct {
	probes.BaseMetricsProbe
	probeName string
	extension string
	dbScoped  bool
	metrics   []map[string]any
	err       error
}

func (p *extensionProbeStub) GetName() string          { return p.probeName }
func (p *extensionProbeStub) GetTableName() string     { return p.probeName }
func (p *extensionProbeStub) GetQuery() string         { return "" }
func (p *extensionProbeStub) IsDatabaseScoped() bool   { return p.dbScoped }
func (p *extensionProbeStub) GetExtensionName() string { return p.extension }
func (p *extensionProbeStub) GetConfig() *probes.ProbeConfig {
	return &probes.ProbeConfig{Name: p.probeName}
}

func (p *extensionProbeStub) Execute(_ context.Context, _ string, _ *pgxpool.Conn, _ int) ([]map[string]any, error) {
	return p.metrics, p.err
}

func (p *extensionProbeStub) Store(_ context.Context, _ *pgxpool.Conn, _ int, _ time.Time, _ []map[string]any) error {
	return nil
}

func (p *extensionProbeStub) EnsurePartition(_ context.Context, _ *pgxpool.Conn, _ time.Time) error {
	return nil
}

// readAvailability returns the availability row recorded for a probe.
func readAvailability(t *testing.T, f *integrationFixture, probeName string) (bool, *string) {
	t.Helper()

	dsConn, err := f.ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	defer f.ds.ReturnConnection(dsConn)

	var available bool
	var reason *string
	err = dsConn.QueryRow(context.Background(), `
		SELECT is_available, unavailable_reason
		FROM probe_availability
		WHERE connection_id = $1 AND probe_name = $2`,
		f.connID, probeName).Scan(&available, &reason)
	if err != nil {
		t.Fatalf("read probe_availability for %s: %v", probeName, err)
	}
	return available, reason
}

// TestExtensionStatusMerge checks that presence outranks absence and
// absence outranks no observation at all, whichever order the databases
// are visited in.
func TestExtensionStatusMerge(t *testing.T) {
	cases := []struct {
		name  string
		left  extensionStatus
		right extensionStatus
		want  extensionStatus
	}{
		{"unknown and unknown", extensionUnknown, extensionUnknown, extensionUnknown},
		{"unknown then absent", extensionUnknown, extensionAbsent, extensionAbsent},
		{"absent then unknown", extensionAbsent, extensionUnknown, extensionAbsent},
		{"absent then present", extensionAbsent, extensionPresent, extensionPresent},
		{"present then absent", extensionPresent, extensionAbsent, extensionPresent},
		{"present then present", extensionPresent, extensionPresent, extensionPresent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.left.merge(tc.right); got != tc.want {
				t.Errorf("merge = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestClassifyProbeResult covers the mapping from one probe.Execute
// outcome to an extension observation. An empty but non-nil slice means
// the extension is installed with nothing to report, which is the
// distinction #438 is about.
func TestClassifyProbeResult(t *testing.T) {
	cases := []struct {
		name    string
		metrics []map[string]any
		err     error
		want    extensionStatus
	}{
		{"absent", nil, probes.ErrExtensionNotInstalled, extensionAbsent},
		{"absent wrapped", nil,
			fmt.Errorf("probe failed: %w", probes.ErrExtensionNotInstalled),
			extensionAbsent},
		{"present with rows", []map[string]any{{"a": 1}}, nil, extensionPresent},
		{"present but empty", []map[string]any{}, nil, extensionPresent},
		{"nil result without error", nil, nil, extensionUnknown},
		{"other failure", nil, fmt.Errorf("boom"), extensionUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyProbeResult(tc.metrics, tc.err); got != tc.want {
				t.Errorf("classifyProbeResult = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestExecuteProbeForConnection_ExtensionPresentButEmpty is the
// availability half of #438: an extension that is installed but has no
// rows this cycle must be recorded as available, not as missing.
func TestExecuteProbeForConnection_ExtensionPresentButEmpty(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), testServerSecret)
	defer ps.Stop()

	for _, dbScoped := range []bool{true, false} {
		name := "server-wide"
		if dbScoped {
			name = "database-scoped"
		}
		t.Run(name, func(t *testing.T) {
			probeName := fmt.Sprintf("ext-present-empty-%v", dbScoped)
			probe := &extensionProbeStub{
				probeName: probeName,
				extension: "pg_stat_statements",
				dbScoped:  dbScoped,
				metrics:   []map[string]any{},
			}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			ps.executeProbeForConnection(ctx, probe, makeMonitoredConn(f))

			available, reason := readAvailability(t, f, probeName)
			if !available {
				t.Errorf("is_available = false, want true for an installed extension with no rows")
			}
			if reason != nil {
				t.Errorf("unavailable_reason = %q, want NULL", *reason)
			}
		})
	}
}

// TestExecuteProbeForConnection_ExtensionAbsent confirms the other half:
// a probe that reports the extension missing is still recorded as
// unavailable, with the reason naming the extension.
func TestExecuteProbeForConnection_ExtensionAbsent(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), testServerSecret)
	defer ps.Stop()

	for _, dbScoped := range []bool{true, false} {
		name := "server-wide"
		if dbScoped {
			name = "database-scoped"
		}
		t.Run(name, func(t *testing.T) {
			probeName := fmt.Sprintf("ext-absent-%v", dbScoped)
			probe := &extensionProbeStub{
				probeName: probeName,
				extension: "pg_stat_statements",
				dbScoped:  dbScoped,
				err:       probes.ErrExtensionNotInstalled,
			}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			ps.executeProbeForConnection(ctx, probe, makeMonitoredConn(f))

			available, reason := readAvailability(t, f, probeName)
			if available {
				t.Error("is_available = true, want false for a missing extension")
			}
			if reason == nil {
				t.Fatal("unavailable_reason = NULL, want the missing-extension reason")
			}
			want := "extension 'pg_stat_statements' not installed"
			if *reason != want {
				t.Errorf("unavailable_reason = %q, want %q", *reason, want)
			}
		})
	}
}

// TestGetDatabaseList_SkipsDatabasesWithoutConnectPrivilege covers #440:
// a database the monitoring user holds no CONNECT privilege on, such as
// rdsadmin on RDS, must not be enumerated, so no probe ever tries it.
func TestGetDatabaseList_SkipsDatabasesWithoutConnectPrivilege(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(f.ds, f.pool, integrationTestConfig(), testServerSecret)
	defer ps.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	suffix := time.Now().UnixNano()
	roleName := fmt.Sprintf("t440_role_%d", suffix)
	forbiddenDB := fmt.Sprintf("t440_noconnect_%d", suffix)
	allowedDB := fmt.Sprintf("t440_connect_%d", suffix)

	adminPool, err := pgxpool.New(ctx, fmt.Sprintf(
		"host=%s port=%d user=%s password=%s sslmode=disable dbname=postgres",
		f.host, f.port, f.username, f.rawPassword))
	if err != nil {
		t.Skipf("connect to admin database: %v", err)
	}
	// Closing the admin pool is registered first so it runs last: test
	// cleanups run in reverse order of registration, and the drops below
	// still need the pool.
	t.Cleanup(adminPool.Close)

	exec := func(sql string) error {
		_, execErr := adminPool.Exec(ctx, sql)
		return execErr
	}

	// Cleanup runs after the test's context is done, so tearing down
	// the role and databases needs a context of its own.
	cleanupExec := func(sql string) error {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(), 30*time.Second)
		defer cleanupCancel()
		_, execErr := adminPool.Exec(cleanupCtx, sql)
		return execErr
	}

	if err := exec(fmt.Sprintf("CREATE ROLE %s LOGIN", roleName)); err != nil {
		t.Skipf("create test role: %v", err)
	}
	t.Cleanup(func() {
		if err := cleanupExec(fmt.Sprintf("DROP ROLE IF EXISTS %s", roleName)); err != nil {
			t.Logf("drop role %s: %v", roleName, err)
		}
	})

	for _, db := range []string{forbiddenDB, allowedDB} {
		if err := exec(fmt.Sprintf("CREATE DATABASE %s", db)); err != nil {
			t.Skipf("create database %s: %v", db, err)
		}
		name := db
		t.Cleanup(func() {
			if err := cleanupExec(fmt.Sprintf(
				"DROP DATABASE IF EXISTS %s WITH (FORCE)", name)); err != nil {
				t.Logf("drop database %s: %v", name, err)
			}
		})
	}

	// Mirror the RDS shape: the database allows connections, but the
	// monitoring role is not permitted to make one.
	if err := exec(fmt.Sprintf(
		"REVOKE CONNECT ON DATABASE %s FROM PUBLIC", forbiddenDB)); err != nil {
		t.Fatalf("revoke connect: %v", err)
	}

	// Enumerate as the unprivileged role, which is what a monitoring
	// user is in practice.
	rolePool, err := pgxpool.New(ctx, fmt.Sprintf(
		"host=%s port=%d user=%s sslmode=disable dbname=postgres",
		f.host, f.port, roleName))
	if err != nil {
		t.Skipf("connect as the test role: %v", err)
	}
	defer rolePool.Close()

	roleConn, err := rolePool.Acquire(ctx)
	if err != nil {
		t.Skipf("acquire a connection as the test role: %v", err)
	}
	defer roleConn.Release()

	databases, err := ps.getDatabaseList(ctx, roleConn)
	if err != nil {
		t.Fatalf("getDatabaseList: %v", err)
	}

	var sawForbidden, sawAllowed bool
	for _, db := range databases {
		switch db {
		case forbiddenDB:
			sawForbidden = true
		case allowedDB:
			sawAllowed = true
		}
	}
	if sawForbidden {
		t.Errorf("database list included %s, which the role cannot connect to",
			forbiddenDB)
	}
	if !sawAllowed {
		t.Errorf("database list omitted %s, which the role can connect to",
			allowedDB)
	}
}
