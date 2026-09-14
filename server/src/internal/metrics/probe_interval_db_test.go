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
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// setProbeIntervalForTest records a collection interval for probe in
// probe_configs: a server-scope row for connID, or the global row when
// connID is 0. The test database may not carry the collector schema, so
// the table is created if absent with the columns the resolver reads (the
// real DDL is a superset, and CREATE TABLE IF NOT EXISTS leaves it alone).
// Every row for the probe is deleted in t.Cleanup.
func setProbeIntervalForTest(
	t testing.TB, pool *pgxpool.Pool, probe string, connID int, seconds int,
) {
	t.Helper()
	ctx := context.Background()

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
