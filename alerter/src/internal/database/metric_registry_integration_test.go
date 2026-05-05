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
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// metricRegistryTestSchema creates the minimal subset of tables required to
// exercise the four new metric_registry entries added for the Spock exception,
// Spock resolution, and replication slot retention/inactivity alerts.
//
// The schema mirrors the production v3 collector schema for the columns the
// metric registry queries actually read; columns that are never referenced
// by these queries (for example the JSONB tuple snapshots in
// spock_exception_log) are omitted to keep the test focused.
//
// The metrics.* tables intentionally do not declare a foreign key to the
// connections table here because GetLatestMetricValues does not join through
// the connections table; the tests instead seed connections directly so the
// realistic primary key still applies.
const metricRegistryTestSchema = `
DROP SCHEMA IF EXISTS metrics CASCADE;
DROP TABLE IF EXISTS connections CASCADE;

CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE SCHEMA metrics;

CREATE TABLE metrics.spock_exception_log (
    connection_id INTEGER NOT NULL,
    database_name TEXT NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    remote_origin OID NOT NULL,
    remote_commit_ts TIMESTAMPTZ NOT NULL,
    command_counter INTEGER NOT NULL,
    retry_errored_at TIMESTAMPTZ NOT NULL,
    remote_xid BIGINT NOT NULL,
    PRIMARY KEY (connection_id, database_name, collected_at, remote_origin,
                 remote_commit_ts, command_counter, retry_errored_at)
);

CREATE TABLE metrics.spock_resolutions (
    connection_id INTEGER NOT NULL,
    database_name TEXT NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    id INTEGER NOT NULL,
    node_name NAME NOT NULL,
    log_time TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (connection_id, database_name, collected_at, id, node_name)
);

CREATE TABLE metrics.pg_replication_slots (
    connection_id INTEGER NOT NULL,
    slot_name TEXT NOT NULL,
    active BOOLEAN,
    retained_bytes NUMERIC,
    collected_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (connection_id, collected_at, slot_name)
);
`

// metricRegistryTestTeardown drops every object the schema creates so a
// subsequent test run starts from a clean slate.
const metricRegistryTestTeardown = `
DROP SCHEMA IF EXISTS metrics CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
`

// newMetricRegistryTestDatastore returns a Datastore backed by the
// integration-test database referenced by TEST_AI_WORKBENCH_SERVER. The test
// is skipped when the variable is unset (or SKIP_DB_TESTS is set), matching
// the behavior of the other integration tests in this package. The returned
// cleanup function tears down the schema and closes the pool.
func newMetricRegistryTestDatastore(t *testing.T) (*Datastore, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping metric registry integration test")
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

	if _, err := pool.Exec(ctx, metricRegistryTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create metric registry test schema: %v", err)
	}

	ds := &Datastore{pool: pool, config: nil}

	cleanup := func() {
		if _, err := pool.Exec(context.Background(), metricRegistryTestTeardown); err != nil {
			t.Logf("metric registry teardown failed: %v", err)
		}
		pool.Close()
	}

	return ds, pool, cleanup
}

// insertSpockExceptionRow writes a single metrics.spock_exception_log row.
// command_counter is used as the unique discriminator inside a sample so a
// caller can stack several rows with the same collected_at without violating
// the composite primary key.
func insertSpockExceptionRow(t *testing.T, pool *pgxpool.Pool, connID int,
	collectedAt time.Time, commandCounter int) {
	t.Helper()

	if _, err := pool.Exec(context.Background(), `
		INSERT INTO metrics.spock_exception_log (
		    connection_id, database_name, collected_at, remote_origin,
		    remote_commit_ts, command_counter, retry_errored_at, remote_xid
		) VALUES (
		    $1, 'app', $2, 12345, $2, $3, $2, 999
		)
	`, connID, collectedAt, commandCounter); err != nil {
		t.Fatalf("Failed to insert spock_exception_log row: %v", err)
	}
}

// insertSpockResolutionRow writes a single metrics.spock_resolutions row.
// id is used as the unique discriminator inside a sample.
func insertSpockResolutionRow(t *testing.T, pool *pgxpool.Pool, connID int,
	collectedAt time.Time, id int) {
	t.Helper()

	if _, err := pool.Exec(context.Background(), `
		INSERT INTO metrics.spock_resolutions (
		    connection_id, database_name, collected_at, id, node_name, log_time
		) VALUES (
		    $1, 'app', $2, $3, 'node-a', $2
		)
	`, connID, collectedAt, id); err != nil {
		t.Fatalf("Failed to insert spock_resolutions row: %v", err)
	}
}

// insertReplicationSlotRow writes a single metrics.pg_replication_slots row.
func insertReplicationSlotRow(t *testing.T, pool *pgxpool.Pool, connID int,
	collectedAt time.Time, slotName string, active bool, retainedBytes int64) {
	t.Helper()

	if _, err := pool.Exec(context.Background(), `
		INSERT INTO metrics.pg_replication_slots (
		    connection_id, slot_name, active, retained_bytes, collected_at
		) VALUES (
		    $1, $2, $3, $4, $5
		)
	`, connID, slotName, active, retainedBytes, collectedAt); err != nil {
		t.Fatalf("Failed to insert pg_replication_slots row: %v", err)
	}
}

// findValueForConnection returns the scanned value for the given connection
// id, failing the test if the result set does not contain that connection.
func findValueForConnection(t *testing.T, results []MetricValue, connID int) float64 {
	t.Helper()

	for _, mv := range results {
		if mv.ConnectionID == connID {
			return mv.Value
		}
	}
	t.Fatalf("no metric value returned for connection_id=%d (got %+v)",
		connID, results)
	return 0
}

// TestMetricRegistry_SpockExceptionLogRecentCount verifies that the
// spock_exception_log.recent_count metric returns COUNT(*) of the latest
// sample only — older samples must not contribute to the count.
//
// Three rows are seeded across two distinct collected_at samples (one row in
// the older sample, two rows in the newer sample). The latest-sample count
// must therefore be 2.
func TestMetricRegistry_SpockExceptionLogRecentCount(t *testing.T) {
	ds, pool, cleanup := newMetricRegistryTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertConnection(t, pool, "spock-exception-test")
	older := time.Now().UTC().Add(-10 * time.Minute)
	newer := time.Now().UTC().Add(-1 * time.Minute)

	// Older sample: one row.
	insertSpockExceptionRow(t, pool, connID, older, 1)
	// Newer sample: two rows. Distinct command_counter values keep them
	// from colliding on the composite primary key.
	insertSpockExceptionRow(t, pool, connID, newer, 1)
	insertSpockExceptionRow(t, pool, connID, newer, 2)

	results, err := ds.GetLatestMetricValues(ctx, "spock_exception_log.recent_count")
	if err != nil {
		t.Fatalf("GetLatestMetricValues(spock_exception_log.recent_count) failed: %v", err)
	}

	got := findValueForConnection(t, results, connID)
	if got != 2 {
		t.Errorf("spock_exception_log.recent_count = %v; want 2 (latest sample only)", got)
	}
}

// TestMetricRegistry_SpockResolutionsRecentCount verifies that the
// spock_resolutions.recent_count metric returns COUNT(*) of the latest
// sample only.
func TestMetricRegistry_SpockResolutionsRecentCount(t *testing.T) {
	ds, pool, cleanup := newMetricRegistryTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertConnection(t, pool, "spock-resolutions-test")
	older := time.Now().UTC().Add(-10 * time.Minute)
	newer := time.Now().UTC().Add(-1 * time.Minute)

	// Older sample: one row.
	insertSpockResolutionRow(t, pool, connID, older, 1)
	// Newer sample: two rows.
	insertSpockResolutionRow(t, pool, connID, newer, 1)
	insertSpockResolutionRow(t, pool, connID, newer, 2)

	results, err := ds.GetLatestMetricValues(ctx, "spock_resolutions.recent_count")
	if err != nil {
		t.Fatalf("GetLatestMetricValues(spock_resolutions.recent_count) failed: %v", err)
	}

	got := findValueForConnection(t, results, connID)
	if got != 2 {
		t.Errorf("spock_resolutions.recent_count = %v; want 2 (latest sample only)", got)
	}
}

// TestMetricRegistry_SpockExceptionLogRecentCount_StaleSampleExcluded
// proves the freshness cutoff on the latest CTE: a sample older than
// the 5-minute cutoff must not keep the metric reporting a non-zero
// value after the rolling source-side window has drained.
//
// The probe short-circuits Store when Execute returns no rows, so a
// stale non-empty sample can otherwise persist as the latest-recorded
// state long after the underlying condition has resolved. The
// freshness cutoff in the latest CTE is what causes the alert to
// auto-clear in that situation.
//
// Seed a single row at now() - 6 minutes (outside the 5-minute cutoff)
// and assert the metric returns no row for that connection.
// GetLatestMetricValues returns "no data found for metric" when the
// underlying query yields zero rows; that is the expected outcome
// when every sample lies outside the cutoff.
func TestMetricRegistry_SpockExceptionLogRecentCount_StaleSampleExcluded(t *testing.T) {
	ds, pool, cleanup := newMetricRegistryTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertConnection(t, pool, "spock-exception-stale")
	stale := time.Now().UTC().Add(-6 * time.Minute)
	insertSpockExceptionRow(t, pool, connID, stale, 1)

	results, err := ds.GetLatestMetricValues(ctx, "spock_exception_log.recent_count")
	if err == nil {
		// If GetLatestMetricValues returns rows the cutoff did not
		// apply; surface a precise diagnostic before failing.
		for _, mv := range results {
			if mv.ConnectionID == connID {
				t.Errorf("spock_exception_log.recent_count returned a "+
					"value for connection %d after the only sample "+
					"aged out of the 5-minute cutoff: got %v",
					connID, mv.Value)
			}
		}
		return
	}

	// "no data found" is the expected error path when every sample
	// for every connection is outside the cutoff. Any other error
	// indicates a real failure in the registry SQL.
	if !strings.Contains(err.Error(), "no data found") {
		t.Fatalf("unexpected error from GetLatestMetricValues: %v", err)
	}
}

// TestMetricRegistry_SpockResolutionsRecentCount_StaleSampleExcluded
// mirrors the exception_log freshness assertion for the resolutions
// table. The cutoff is what allows the high/critical resolutions
// alerts to auto-clear once Spock stops auto-resolving conflicts.
func TestMetricRegistry_SpockResolutionsRecentCount_StaleSampleExcluded(t *testing.T) {
	ds, pool, cleanup := newMetricRegistryTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertConnection(t, pool, "spock-resolutions-stale")
	stale := time.Now().UTC().Add(-6 * time.Minute)
	insertSpockResolutionRow(t, pool, connID, stale, 1)

	results, err := ds.GetLatestMetricValues(ctx, "spock_resolutions.recent_count")
	if err == nil {
		for _, mv := range results {
			if mv.ConnectionID == connID {
				t.Errorf("spock_resolutions.recent_count returned a "+
					"value for connection %d after the only sample "+
					"aged out of the 5-minute cutoff: got %v",
					connID, mv.Value)
			}
		}
		return
	}

	if !strings.Contains(err.Error(), "no data found") {
		t.Fatalf("unexpected error from GetLatestMetricValues: %v", err)
	}
}

// TestMetricRegistry_PgReplicationSlotsInactiveCount verifies that the
// pg_replication_slots.inactive_count metric returns the number of slots in
// the latest sample whose active column is FALSE.
//
// Three slots are seeded in a single sample: two inactive, one active. The
// expected value is 2.
func TestMetricRegistry_PgReplicationSlotsInactiveCount(t *testing.T) {
	ds, pool, cleanup := newMetricRegistryTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertConnection(t, pool, "slot-inactive-test")
	sample := time.Now().UTC().Add(-1 * time.Minute)

	insertReplicationSlotRow(t, pool, connID, sample, "slot_a", false, 100)
	insertReplicationSlotRow(t, pool, connID, sample, "slot_b", false, 200)
	insertReplicationSlotRow(t, pool, connID, sample, "slot_c", true, 300)

	results, err := ds.GetLatestMetricValues(ctx, "pg_replication_slots.inactive_count")
	if err != nil {
		t.Fatalf("GetLatestMetricValues(pg_replication_slots.inactive_count) failed: %v", err)
	}

	got := findValueForConnection(t, results, connID)
	if got != 2 {
		t.Errorf("pg_replication_slots.inactive_count = %v; want 2", got)
	}
}

// TestMetricRegistry_PgReplicationSlotsMaxRetainedBytes verifies that the
// pg_replication_slots.max_retained_bytes metric returns the maximum
// retained_bytes value across slots in the latest sample.
//
// Three slots are seeded with retained_bytes 100, 5_000_000_000, and 200.
// The expected maximum is 5_000_000_000 (a value larger than int32 to
// confirm the NUMERIC/float round-trip preserves it).
func TestMetricRegistry_PgReplicationSlotsMaxRetainedBytes(t *testing.T) {
	ds, pool, cleanup := newMetricRegistryTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertConnection(t, pool, "slot-retained-test")
	sample := time.Now().UTC().Add(-1 * time.Minute)

	insertReplicationSlotRow(t, pool, connID, sample, "slot_a", true, 100)
	insertReplicationSlotRow(t, pool, connID, sample, "slot_b", true, 5_000_000_000)
	insertReplicationSlotRow(t, pool, connID, sample, "slot_c", true, 200)

	results, err := ds.GetLatestMetricValues(ctx, "pg_replication_slots.max_retained_bytes")
	if err != nil {
		t.Fatalf("GetLatestMetricValues(pg_replication_slots.max_retained_bytes) failed: %v", err)
	}

	got := findValueForConnection(t, results, connID)
	if got != 5_000_000_000 {
		t.Errorf("pg_replication_slots.max_retained_bytes = %v; want 5000000000", got)
	}
}
