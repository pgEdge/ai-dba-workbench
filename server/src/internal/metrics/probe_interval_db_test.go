/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package metrics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ensureConnectionForTest makes sure connections holds a row with id
// connID, so a server-scope probe_configs row may reference it. Another
// package's fixture creates probe_configs with a foreign key on
// connection_id (server/src/internal/database/config_queries_integration_test.go
// does exactly that) and the table outlives that package's run, so without
// the referenced row the insert below fails with SQLSTATE 23503 and this
// package cannot be tested on its own.
//
// It never creates the connections table: other packages create their own
// with a plain CREATE TABLE and would fail against one left behind here.
// With no such table there can be no foreign key either, so there is
// nothing to do. The insert names only the columns that are NOT NULL
// without a default in either the collector's DDL or that fixture's, and
// sets owner_username so the collector's chk_owner constraint holds; a
// table of some other shape simply refuses it, and the probe_configs
// insert then reports the foreign key itself. A row this call did not
// create is left alone, and one it did create is deleted in t.Cleanup.
func ensureConnectionForTest(t testing.TB, pool *pgxpool.Pool, connID int) {
	t.Helper()
	ctx := context.Background()

	var exists *string
	if err := pool.QueryRow(ctx,
		"SELECT to_regclass('public.connections')::text").Scan(&exists); err != nil {
		t.Fatalf("failed to look for the connections table: %v", err)
	}
	if exists == nil {
		return
	}

	const insert = `INSERT INTO connections
        (id, name, host, database_name, username, owner_username)
        VALUES ($1, 'metrics test fixture', 'localhost', 'postgres',
                'postgres', 'metrics_test')
        ON CONFLICT (id) DO NOTHING
        RETURNING id`
	var inserted int
	err := pool.QueryRow(ctx, insert, connID).Scan(&inserted)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// The row was already there; it is not ours to remove.
		return
	case err != nil:
		t.Logf("could not add connections row %d (%v);"+
			" probe_configs may refuse the fixture row", connID, err)
		return
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			"DELETE FROM connections WHERE id = $1", connID)
	})
}

// setProbeIntervalForTest records a collection interval for probe in
// probe_configs: a server-scope row for connID, or the global row when
// connID is 0. The test database may not carry the collector schema, so
// the table is created if absent with the columns the resolver reads (the
// real DDL is a superset, and CREATE TABLE IF NOT EXISTS leaves it alone).
// A server-scope row needs its connection to exist, so connID is created
// first. Every row for the probe is deleted in t.Cleanup.
func setProbeIntervalForTest(
	t testing.TB, pool *pgxpool.Pool, probe string, connID int, seconds int,
) {
	t.Helper()
	ctx := context.Background()

	if connID != 0 {
		ensureConnectionForTest(t, pool, connID)
	}

	const ddl = `CREATE TABLE IF NOT EXISTS probe_configs (
        id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
        connection_id INTEGER,
        is_enabled BOOLEAN NOT NULL DEFAULT TRUE,
        name TEXT NOT NULL,
        description TEXT NOT NULL DEFAULT '',
        collection_interval_seconds INTEGER NOT NULL DEFAULT 60,
        retention_days INTEGER NOT NULL DEFAULT 28,
        scope TEXT NOT NULL DEFAULT 'global'
    )`
	if _, err := pool.Exec(ctx, ddl); err != nil {
		t.Fatalf("failed to create probe_configs fixture table: %v", err)
	}

	scope := "global"
	var conn *int
	if connID != 0 {
		scope = "server"
		conn = &connID
	}
	const insert = `INSERT INTO probe_configs
        (connection_id, name, description, collection_interval_seconds, scope)
        VALUES ($1, $2, 'test fixture', $3, $4)`
	if _, err := pool.Exec(ctx, insert, conn, probe, seconds, scope); err != nil {
		t.Fatalf("failed to insert probe_configs fixture row: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			"DELETE FROM probe_configs WHERE name = $1", probe)
	})
}

func TestResolveProbeInterval_Integration(t *testing.T) {
	pool, closePool := newLatestRowsTestPool(t)
	defer closePool()
	ctx := context.Background()

	// Each case uses its own probe name so the rows cannot interfere.
	resolve := func(t *testing.T, probe string, conns ...int) time.Duration {
		t.Helper()
		got, err := ResolveProbeInterval(ctx, pool, probe, conns)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return got
	}

	t.Run("no rows falls back to the default", func(t *testing.T) {
		// Make sure the table exists even when this is the first test to
		// touch it, then ask about a probe with no rows at all.
		setProbeIntervalForTest(t, pool, "pi_other_probe", 0, 60)
		if got := resolve(t, "pi_unconfigured_probe", 1); got != DefaultProbeInterval {
			t.Errorf("interval = %v, want the %v default", got, DefaultProbeInterval)
		}
	})

	t.Run("global row applies when no server row exists", func(t *testing.T) {
		setProbeIntervalForTest(t, pool, "pi_global_probe", 0, 600)
		if got := resolve(t, "pi_global_probe", 1); got != 600*time.Second {
			t.Errorf("interval = %v, want 600s", got)
		}
	})

	t.Run("server row wins over the global row", func(t *testing.T) {
		setProbeIntervalForTest(t, pool, "pi_server_probe", 0, 600)
		setProbeIntervalForTest(t, pool, "pi_server_probe", 1, 60)
		if got := resolve(t, "pi_server_probe", 1); got != 60*time.Second {
			t.Errorf("interval = %v, want the 60s server row", got)
		}
	})

	t.Run("largest server row wins across connections", func(t *testing.T) {
		setProbeIntervalForTest(t, pool, "pi_multi_probe", 1, 60)
		setProbeIntervalForTest(t, pool, "pi_multi_probe", 2, 120)
		setProbeIntervalForTest(t, pool, "pi_multi_probe", 3, 30)
		if got := resolve(t, "pi_multi_probe", 1, 2, 3); got != 120*time.Second {
			t.Errorf("interval = %v, want 120s", got)
		}
		// A connection outside the request does not take part.
		if got := resolve(t, "pi_multi_probe", 1, 3); got != 60*time.Second {
			t.Errorf("interval = %v, want 60s", got)
		}
	})

	t.Run("global row competes for a connection without a server row", func(t *testing.T) {
		setProbeIntervalForTest(t, pool, "pi_mixed_probe", 0, 600)
		setProbeIntervalForTest(t, pool, "pi_mixed_probe", 1, 60)
		if got := resolve(t, "pi_mixed_probe", 1, 2); got != 600*time.Second {
			t.Errorf("interval = %v, want the 600s global row for connection 2", got)
		}
	})

	t.Run("server rows decide when a connection has none and there is no global row", func(t *testing.T) {
		setProbeIntervalForTest(t, pool, "pi_partial_probe", 1, 90)
		if got := resolve(t, "pi_partial_probe", 1, 2); got != 90*time.Second {
			t.Errorf("interval = %v, want 90s", got)
		}
	})

	t.Run("query error surfaces", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := ResolveProbeInterval(cctx, pool, "pi_global_probe", []int{1}); err == nil {
			t.Error("expected an error from a canceled context")
		}
	})
}

// spacingTestProbe is the fixture table for the observed-spacing tests.
const spacingTestProbe = "pg_stat_spacing_test"

// setupSpacingFixture creates a probe table holding, per connection:
//
//	conn 1  samples a minute apart, with one four-minute outage in the middle
//	conn 2  samples ten minutes apart
//	conn 3  a single sample, so it shows no spacing at all
//
// The returned base is the newest sample time.
func setupSpacingFixture(t *testing.T, pool *pgxpool.Pool) (time.Time, func()) {
	t.Helper()
	ctx := context.Background()

	if _, err := pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS metrics"); err != nil {
		t.Fatalf("failed to create metrics schema: %v", err)
	}
	dropTable(ctx, pool, spacingTestProbe)

	ddl := `CREATE TABLE metrics."` + spacingTestProbe + `" (
        connection_id integer NOT NULL,
        collected_at  timestamp with time zone NOT NULL,
        datname       text NOT NULL,
        xact_commit   bigint,
        PRIMARY KEY (connection_id, collected_at, datname)
    )`
	if _, err := pool.Exec(ctx, ddl); err != nil {
		t.Fatalf("failed to create spacing fixture table: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Minute)
	samples := []struct {
		conn   int
		offset time.Duration
	}{
		{1, -20 * time.Minute}, {1, -19 * time.Minute}, {1, -18 * time.Minute},
		{1, -14 * time.Minute}, {1, -13 * time.Minute},

		{2, -30 * time.Minute}, {2, -20 * time.Minute}, {2, -10 * time.Minute},

		{3, -5 * time.Minute},
	}
	// Two entity rows per sample, so the query must not mistake two rows
	// sharing a collection time for two samples.
	insert := `INSERT INTO metrics."` + spacingTestProbe + `"
        (connection_id, collected_at, datname, xact_commit)
        VALUES ($1, $2, $3, 1)`
	for i, s := range samples {
		for _, db := range []string{"northwind", "pagila"} {
			if _, err := pool.Exec(ctx, insert, s.conn, now.Add(s.offset), db); err != nil {
				dropTable(ctx, pool, spacingTestProbe)
				t.Fatalf("failed to insert spacing fixture sample %d: %v", i, err)
			}
		}
	}

	return now, func() { dropTable(context.Background(), pool, spacingTestProbe) }
}

func TestObserveSampleSpacing_Integration(t *testing.T) {
	pool, closePool := newLatestRowsTestPool(t)
	defer closePool()
	base, cleanup := setupSpacingFixture(t, pool)
	defer cleanup()

	ctx := context.Background()
	hour := TimeWindow{Start: base.Add(-time.Hour), End: base}

	observe := func(t *testing.T, window TimeWindow, conns ...int) time.Duration {
		t.Helper()
		got, err := ObserveSampleSpacing(ctx, pool, spacingTestProbe, conns, window)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return got
	}

	t.Run("the tightest spacing wins, so an outage is not the cadence", func(t *testing.T) {
		if got := observe(t, hour, 1); got != time.Minute {
			t.Errorf("spacing = %v, want 1m0s", got)
		}
	})

	t.Run("a wider cadence is reported in full", func(t *testing.T) {
		if got := observe(t, hour, 2); got != 10*time.Minute {
			t.Errorf("spacing = %v, want 10m0s", got)
		}
	})

	t.Run("the coarsest connection decides", func(t *testing.T) {
		if got := observe(t, hour, 1, 2); got != 10*time.Minute {
			t.Errorf("spacing = %v, want 10m0s", got)
		}
	})

	t.Run("one sample shows no spacing", func(t *testing.T) {
		if got := observe(t, hour, 3); got != 0 {
			t.Errorf("spacing = %v, want 0", got)
		}
	})

	t.Run("an empty window shows no spacing", func(t *testing.T) {
		empty := TimeWindow{Start: base.Add(-3 * time.Hour), End: base.Add(-2 * time.Hour)}
		if got := observe(t, empty, 1, 2, 3); got != 0 {
			t.Errorf("spacing = %v, want 0", got)
		}
	})

	t.Run("only the window is measured", func(t *testing.T) {
		// The four-minute outage of connection 1 is the only spacing
		// inside this window, so it is the cadence as far as it knows.
		window := TimeWindow{
			Start: base.Add(-18 * time.Minute),
			End:   base.Add(-14 * time.Minute),
		}
		if got := observe(t, window, 1); got != 4*time.Minute {
			t.Errorf("spacing = %v, want 4m0s", got)
		}
	})

	t.Run("an invalid probe name is refused", func(t *testing.T) {
		_, err := ObserveSampleSpacing(ctx, pool, "bad name; DROP", []int{1}, hour)
		if err == nil {
			t.Fatal("expected an error for an invalid probe name")
		}
	})

	t.Run("a missing probe table is an error", func(t *testing.T) {
		_, err := ObserveSampleSpacing(ctx, pool, "no_such_probe_table", []int{1}, hour)
		if err == nil {
			t.Fatal("expected an error for a missing probe table")
		}
	})
}

func TestResolveEffectiveInterval_Integration(t *testing.T) {
	pool, closePool := newLatestRowsTestPool(t)
	defer closePool()
	base, cleanup := setupSpacingFixture(t, pool)
	defer cleanup()

	ctx := context.Background()
	hour := TimeWindow{Start: base.Add(-time.Hour), End: base}

	t.Run("the configured interval wins when the samples agree", func(t *testing.T) {
		setProbeIntervalForTest(t, pool, spacingTestProbe, 0, 300)
		configured, effective, err := ResolveEffectiveInterval(
			ctx, pool, spacingTestProbe, []int{1}, hour)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if configured != 300*time.Second || effective != 300*time.Second {
			t.Errorf("got configured %v, effective %v, want 5m0s and 5m0s",
				configured, effective)
		}
	})

	t.Run("wider samples widen the effective interval", func(t *testing.T) {
		// Ten-second configuration against minute-spaced samples: the
		// samples decide, so bounds derived from it cannot reject them.
		setProbeIntervalForTest(t, pool, spacingTestProbe, 0, 10)
		configured, effective, err := ResolveEffectiveInterval(
			ctx, pool, spacingTestProbe, []int{1}, hour)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if configured != 10*time.Second {
			t.Errorf("configured = %v, want 10s", configured)
		}
		if effective != time.Minute {
			t.Errorf("effective = %v, want 1m0s", effective)
		}
	})

	t.Run("no samples leaves the configured interval alone", func(t *testing.T) {
		setProbeIntervalForTest(t, pool, spacingTestProbe, 0, 10)
		empty := TimeWindow{Start: base.Add(-3 * time.Hour), End: base.Add(-2 * time.Hour)}
		_, effective, err := ResolveEffectiveInterval(
			ctx, pool, spacingTestProbe, []int{1}, empty)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if effective != 10*time.Second {
			t.Errorf("effective = %v, want 10s", effective)
		}
	})

	t.Run("an invalid probe name is refused", func(t *testing.T) {
		_, _, err := ResolveEffectiveInterval(
			ctx, pool, "bad name; DROP", []int{1}, hour)
		if err == nil {
			t.Fatal("expected an error for an invalid probe name")
		}
	})

	t.Run("a missing probe table is reported", func(t *testing.T) {
		// The name resolves an interval (probe_configs is keyed by name,
		// not by table) but has no table to measure spacing in.
		_, _, err := ResolveEffectiveInterval(
			ctx, pool, "no_such_probe_table", []int{1}, hour)
		if err == nil {
			t.Fatal("expected an error for a missing probe table")
		}
	})
}
