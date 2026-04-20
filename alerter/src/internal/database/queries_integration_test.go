/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package database

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// historicalMetricsTestSchema creates the minimal tables required by
// GetHistoricalMetricValues. Only the columns actually referenced by the
// queries are included; this keeps the test decoupled from the full
// collector schema.
//
// The metrics.* tables intentionally do not carry a foreign key to
// connections. That is production behavior - orphaned rows can exist
// briefly after a connection is deleted, and GetHistoricalMetricValues
// must filter them out. The test relies on that to exercise issue #56.
const historicalMetricsTestSchema = `
DROP SCHEMA IF EXISTS metrics CASCADE;
DROP TABLE IF EXISTS connections CASCADE;

CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE SCHEMA metrics;

CREATE TABLE metrics.pg_settings (
    connection_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    setting TEXT NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE metrics.pg_stat_activity (
    connection_id INTEGER NOT NULL,
    backend_type TEXT,
    state TEXT,
    wait_event_type TEXT,
    xact_start TIMESTAMPTZ,
    query_start TIMESTAMPTZ,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE metrics.pg_stat_database (
    connection_id INTEGER NOT NULL,
    database_name TEXT,
    datname TEXT,
    blks_hit BIGINT,
    blks_read BIGINT,
    deadlocks BIGINT,
    temp_files BIGINT,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE metrics.pg_sys_cpu_usage_info (
    connection_id INTEGER NOT NULL,
    processor_time_percent DOUBLE PRECISION,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE metrics.pg_sys_memory_info (
    connection_id INTEGER NOT NULL,
    used_memory BIGINT,
    total_memory BIGINT,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE metrics.pg_sys_load_avg_info (
    connection_id INTEGER NOT NULL,
    load_avg_fifteen_minutes DOUBLE PRECISION,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE metrics.pg_sys_disk_info (
    connection_id INTEGER NOT NULL,
    used_space BIGINT,
    total_space BIGINT,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

// historicalMetricsTestTeardown drops every object created by the schema
// above, in dependency order.
const historicalMetricsTestTeardown = `
DROP SCHEMA IF EXISTS metrics CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
`

// newHistoricalMetricsTestDatastore returns a Datastore backed by the
// integration test database. The test is skipped if no test database is
// available. The returned cleanup function drops the schema and closes
// the pool.
func newHistoricalMetricsTestDatastore(t *testing.T) (*Datastore, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping historical metrics integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("Test database ping failed: %v", err)
	}

	if _, err := pool.Exec(ctx, historicalMetricsTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create historical metrics test schema: %v", err)
	}

	ds := &Datastore{pool: pool, config: nil}

	cleanup := func() {
		if _, err := pool.Exec(context.Background(), historicalMetricsTestTeardown); err != nil {
			t.Logf("historical metrics teardown failed: %v", err)
		}
		pool.Close()
	}

	return ds, pool, cleanup
}

// insertConnection inserts a row into the connections table and returns
// the generated id.
func insertConnection(t *testing.T, pool *pgxpool.Pool, name string) int {
	t.Helper()

	var id int
	err := pool.QueryRow(context.Background(),
		`INSERT INTO connections (name, enabled) VALUES ($1, TRUE) RETURNING id`,
		name).Scan(&id)
	if err != nil {
		t.Fatalf("Failed to insert connection %q: %v", name, err)
	}
	return id
}

// deleteConnection removes a connection by id, leaving any existing
// metrics.* rows referencing it intact (simulating the orphan scenario
// that issue #56 describes).
func deleteConnection(t *testing.T, pool *pgxpool.Pool, id int) {
	t.Helper()

	if _, err := pool.Exec(context.Background(),
		`DELETE FROM connections WHERE id = $1`, id); err != nil {
		t.Fatalf("Failed to delete connection %d: %v", id, err)
	}
}

// metricCase describes one GetHistoricalMetricValues branch and the
// per-branch INSERT statements required to populate both live and
// orphaned rows so the filter behavior can be verified.
type metricCase struct {
	metricName string
	// insert takes a connection id and a base collected_at. It writes
	// enough rows to produce at least one result per connection id.
	insert func(ctx context.Context, pool *pgxpool.Pool, connID int, base time.Time) error
}

// allMetricCases enumerates every branch of GetHistoricalMetricValues
// that returns a connection id. Each case inserts rows for two
// connections; the caller then deletes one and asserts that the query
// no longer returns data for the deleted connection.
func allMetricCases() []metricCase {
	return []metricCase{
		{
			metricName: "pg_settings.max_connections",
			insert: func(ctx context.Context, pool *pgxpool.Pool, connID int, base time.Time) error {
				_, err := pool.Exec(ctx, `
					INSERT INTO metrics.pg_settings
					    (connection_id, name, setting, collected_at)
					VALUES ($1, 'max_connections', '200', $2)
				`, connID, base)
				return err
			},
		},
		{
			metricName: "connection_utilization_percent",
			insert: func(ctx context.Context, pool *pgxpool.Pool, connID int, base time.Time) error {
				if _, err := pool.Exec(ctx, `
					INSERT INTO metrics.pg_settings
					    (connection_id, name, setting, collected_at)
					VALUES ($1, 'max_connections', '200', $2)
				`, connID, base); err != nil {
					return err
				}
				// Three rows at the same collected_at => COUNT(*) = 3.
				_, err := pool.Exec(ctx, `
					INSERT INTO metrics.pg_stat_activity
					    (connection_id, backend_type, collected_at)
					VALUES
					    ($1, 'client backend', $2),
					    ($1, 'client backend', $2),
					    ($1, 'client backend', $2)
				`, connID, base)
				return err
			},
		},
		{
			metricName: "pg_stat_activity.count",
			insert: func(ctx context.Context, pool *pgxpool.Pool, connID int, base time.Time) error {
				_, err := pool.Exec(ctx, `
					INSERT INTO metrics.pg_stat_activity
					    (connection_id, backend_type, collected_at)
					VALUES ($1, 'client backend', $2)
				`, connID, base)
				return err
			},
		},
		{
			metricName: "pg_stat_activity.blocked_count",
			insert: func(ctx context.Context, pool *pgxpool.Pool, connID int, base time.Time) error {
				_, err := pool.Exec(ctx, `
					INSERT INTO metrics.pg_stat_activity
					    (connection_id, backend_type, wait_event_type, collected_at)
					VALUES ($1, 'client backend', 'Lock', $2)
				`, connID, base)
				return err
			},
		},
		{
			metricName: "pg_stat_activity.idle_in_transaction_seconds",
			insert: func(ctx context.Context, pool *pgxpool.Pool, connID int, base time.Time) error {
				_, err := pool.Exec(ctx, `
					INSERT INTO metrics.pg_stat_activity
					    (connection_id, state, xact_start, collected_at)
					VALUES ($1, 'idle in transaction', $2::timestamptz - INTERVAL '30 seconds', $2)
				`, connID, base)
				return err
			},
		},
		{
			metricName: "pg_stat_activity.max_query_duration_seconds",
			insert: func(ctx context.Context, pool *pgxpool.Pool, connID int, base time.Time) error {
				_, err := pool.Exec(ctx, `
					INSERT INTO metrics.pg_stat_activity
					    (connection_id, state, query_start, collected_at)
					VALUES ($1, 'active', $2::timestamptz - INTERVAL '10 seconds', $2)
				`, connID, base)
				return err
			},
		},
		{
			metricName: "pg_stat_activity.max_xact_duration_seconds",
			insert: func(ctx context.Context, pool *pgxpool.Pool, connID int, base time.Time) error {
				_, err := pool.Exec(ctx, `
					INSERT INTO metrics.pg_stat_activity
					    (connection_id, xact_start, collected_at)
					VALUES ($1, $2::timestamptz - INTERVAL '45 seconds', $2)
				`, connID, base)
				return err
			},
		},
		{
			metricName: "pg_sys_cpu_usage_info.processor_time_percent",
			insert: func(ctx context.Context, pool *pgxpool.Pool, connID int, base time.Time) error {
				_, err := pool.Exec(ctx, `
					INSERT INTO metrics.pg_sys_cpu_usage_info
					    (connection_id, processor_time_percent, collected_at)
					VALUES ($1, 42.5, $2)
				`, connID, base)
				return err
			},
		},
		{
			metricName: "pg_sys_memory_info.used_percent",
			insert: func(ctx context.Context, pool *pgxpool.Pool, connID int, base time.Time) error {
				_, err := pool.Exec(ctx, `
					INSERT INTO metrics.pg_sys_memory_info
					    (connection_id, used_memory, total_memory, collected_at)
					VALUES ($1, 4096, 16384, $2)
				`, connID, base)
				return err
			},
		},
		{
			metricName: "pg_sys_load_avg_info.load_avg_fifteen_minutes",
			insert: func(ctx context.Context, pool *pgxpool.Pool, connID int, base time.Time) error {
				_, err := pool.Exec(ctx, `
					INSERT INTO metrics.pg_sys_load_avg_info
					    (connection_id, load_avg_fifteen_minutes, collected_at)
					VALUES ($1, 1.75, $2)
				`, connID, base)
				return err
			},
		},
		{
			metricName: "pg_sys_disk_info.used_percent",
			insert: func(ctx context.Context, pool *pgxpool.Pool, connID int, base time.Time) error {
				_, err := pool.Exec(ctx, `
					INSERT INTO metrics.pg_sys_disk_info
					    (connection_id, used_space, total_space, collected_at)
					VALUES ($1, 50, 100, $2)
				`, connID, base)
				return err
			},
		},
		{
			// cache_hit_ratio requires two rows per connection for LAG()
			// plus a delta of at least 10000 blocks to clear the filter.
			metricName: "pg_stat_database.cache_hit_ratio",
			insert: func(ctx context.Context, pool *pgxpool.Pool, connID int, base time.Time) error {
				_, err := pool.Exec(ctx, `
					INSERT INTO metrics.pg_stat_database
					    (connection_id, database_name, datname, blks_hit, blks_read, collected_at)
					VALUES
					    ($1, 'appdb', 'appdb', 1000,  100,  $2::timestamptz - INTERVAL '1 minute'),
					    ($1, 'appdb', 'appdb', 20000, 200,  $2)
				`, connID, base)
				return err
			},
		},
		{
			// deadlocks_delta requires two rows per connection for LAG()
			// to emit a non-null previous value.
			metricName: "pg_stat_database.deadlocks_delta",
			insert: func(ctx context.Context, pool *pgxpool.Pool, connID int, base time.Time) error {
				_, err := pool.Exec(ctx, `
					INSERT INTO metrics.pg_stat_database
					    (connection_id, database_name, datname, deadlocks, collected_at)
					VALUES
					    ($1, 'appdb', 'appdb', 0, $2::timestamptz - INTERVAL '1 minute'),
					    ($1, 'appdb', 'appdb', 5, $2)
				`, connID, base)
				return err
			},
		},
		{
			// temp_files_delta: same two-row pattern.
			metricName: "pg_stat_database.temp_files_delta",
			insert: func(ctx context.Context, pool *pgxpool.Pool, connID int, base time.Time) error {
				_, err := pool.Exec(ctx, `
					INSERT INTO metrics.pg_stat_database
					    (connection_id, database_name, datname, temp_files, collected_at)
					VALUES
					    ($1, 'appdb', 'appdb', 0, $2::timestamptz - INTERVAL '1 minute'),
					    ($1, 'appdb', 'appdb', 3, $2)
				`, connID, base)
				return err
			},
		},
	}
}

// resetMetricsTables clears every metrics.* table so each subtest starts
// from a clean slate without rebuilding the schema.
func resetMetricsTables(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	tables := []string{
		"metrics.pg_settings",
		"metrics.pg_stat_activity",
		"metrics.pg_stat_database",
		"metrics.pg_sys_cpu_usage_info",
		"metrics.pg_sys_memory_info",
		"metrics.pg_sys_load_avg_info",
		"metrics.pg_sys_disk_info",
	}
	for _, tbl := range tables {
		if _, err := pool.Exec(context.Background(),
			fmt.Sprintf("TRUNCATE %s", tbl)); err != nil {
			t.Fatalf("Failed to truncate %s: %v", tbl, err)
		}
	}
	if _, err := pool.Exec(context.Background(),
		`DELETE FROM connections`); err != nil {
		t.Fatalf("Failed to clear connections: %v", err)
	}
}

// TestGetHistoricalMetricValues_FiltersOrphanedConnections is the
// regression test for GitHub issue #56. For every metric branch inside
// GetHistoricalMetricValues, it populates rows for both a live
// connection and a deleted connection, then verifies that the results
// contain only the live connection's id. This proves the INNER JOIN
// against the connections table is actually wired up on every branch.
func TestGetHistoricalMetricValues_FiltersOrphanedConnections(t *testing.T) {
	ds, pool, cleanup := newHistoricalMetricsTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	base := time.Now().UTC().Add(-1 * time.Hour)

	for _, mc := range allMetricCases() {
		mc := mc // capture range var
		t.Run(mc.metricName, func(t *testing.T) {
			resetMetricsTables(t, pool)

			liveID := insertConnection(t, pool, "live-"+mc.metricName)
			orphanID := insertConnection(t, pool, "orphan-"+mc.metricName)

			if err := mc.insert(ctx, pool, liveID, base); err != nil {
				t.Fatalf("insert for live connection %d failed: %v", liveID, err)
			}
			if err := mc.insert(ctx, pool, orphanID, base); err != nil {
				t.Fatalf("insert for orphan connection %d failed: %v", orphanID, err)
			}

			// Simulate the race / legacy state: connection is deleted
			// while metrics rows still reference its id.
			deleteConnection(t, pool, orphanID)

			results, err := ds.GetHistoricalMetricValues(ctx, mc.metricName, 1)
			if err != nil {
				t.Fatalf("GetHistoricalMetricValues(%s) failed: %v",
					mc.metricName, err)
			}

			if len(results) == 0 {
				t.Fatalf("expected at least one row for live connection %d, got none",
					liveID)
			}

			sawLive := false
			for _, r := range results {
				if r.ConnectionID == orphanID {
					t.Errorf("result contains orphan connection id %d; "+
						"INNER JOIN against connections is missing or wrong",
						orphanID)
				}
				if r.ConnectionID == liveID {
					sawLive = true
				}
			}
			if !sawLive {
				t.Errorf("expected results to contain live connection id %d, got %+v",
					liveID, results)
			}
		})
	}
}
