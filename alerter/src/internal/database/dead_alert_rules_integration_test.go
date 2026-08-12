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
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This file covers GitHub issue #406: five built-in alert rules that
// could not fire under any conditions. Each test seeds the metrics
// tables with data that reproduces the scenario the rule claims to
// detect, then asserts that the metric now returns a value which
// crosses the rule's shipped default threshold.
//
// The thresholds below mirror the seed data in the collector's
// schema.go so a change to either side shows up here.
const (
	// wal_archive_failed fires above zero failures in the window.
	deadRuleArchiveFailedThreshold = 0
	// transaction_wraparound fires above 75 percent of the limit.
	deadRuleWraparoundThreshold = 75
	// high_max_connections fires above 500 configured connections.
	deadRuleMaxConnectionsThreshold = 500
	// connection_utilization fires above 80 percent utilization.
	deadRuleUtilizationThreshold = 80
	// cpu_usage_high fires above 80 percent busy.
	deadRuleCPUThreshold = 80
	// checkpoint_warning fires above 12 requested checkpoints per hour.
	deadRuleCheckpointThreshold = 12
)

// deadRuleSchema mirrors the production collector schema for every
// column the metric registry reads on behalf of the five rules under
// test. It is deliberately faithful about which table carries the
// archiver counters: they live on metrics.pg_stat_wal, and the
// collector never creates a metrics.pg_stat_archiver table.
const deadRuleSchema = `
DROP SCHEMA IF EXISTS metrics CASCADE;
DROP TABLE IF EXISTS connections CASCADE;

CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    is_monitored BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE SCHEMA metrics;

CREATE TABLE metrics.pg_settings (
    connection_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    setting TEXT,
    collected_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE metrics.pg_stat_activity (
    connection_id INTEGER NOT NULL,
    backend_type TEXT,
    collected_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE metrics.pg_stat_wal (
    connection_id INTEGER NOT NULL,
    archived_count BIGINT,
    failed_count BIGINT,
    collected_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE metrics.pg_stat_checkpointer (
    connection_id INTEGER NOT NULL,
    num_requested BIGINT,
    num_timed BIGINT,
    collected_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE metrics.pg_database (
    connection_id INTEGER NOT NULL,
    datname TEXT NOT NULL,
    datistemplate BOOLEAN,
    age_datfrozenxid BIGINT,
    collected_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE metrics.pg_stat_all_tables (
    connection_id INTEGER NOT NULL,
    database_name TEXT NOT NULL,
    schemaname TEXT NOT NULL,
    relname TEXT NOT NULL,
    n_live_tup BIGINT,
    n_dead_tup BIGINT,
    last_autovacuum TIMESTAMPTZ,
    collected_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE metrics.pg_sys_cpu_usage_info (
    connection_id INTEGER NOT NULL,
    usermode_normal_process_percent REAL,
    usermode_niced_process_percent REAL,
    kernelmode_process_percent REAL,
    io_completion_percent REAL,
    servicing_irq_percent REAL,
    servicing_softirq_percent REAL,
    idle_mode_percent REAL,
    user_time_percent REAL,
    processor_time_percent REAL,
    privileged_time_percent REAL,
    interrupt_time_percent REAL,
    collected_at TIMESTAMPTZ NOT NULL
);
`

// deadRuleTeardown drops everything deadRuleSchema creates.
const deadRuleTeardown = `
DROP SCHEMA IF EXISTS metrics CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
`

// Seed statements are named constants so no test builds SQL by
// concatenation; every value is bound as a parameter.
const (
	insertDeadRuleConnectionSQL = `
        INSERT INTO connections (name, enabled, is_monitored)
        VALUES ($1, TRUE, TRUE)
        RETURNING id
    `

	insertDeadRuleSettingSQL = `
        INSERT INTO metrics.pg_settings
            (connection_id, name, setting, collected_at)
        VALUES ($1, $2, $3, $4)
    `

	insertDeadRuleActivitySQL = `
        INSERT INTO metrics.pg_stat_activity
            (connection_id, backend_type, collected_at)
        SELECT $1, 'client backend', $2
        FROM generate_series(1, $3::int)
    `

	insertDeadRuleWALSQL = `
        INSERT INTO metrics.pg_stat_wal
            (connection_id, archived_count, failed_count, collected_at)
        VALUES ($1, $2, $3, $4)
    `

	insertDeadRuleCheckpointerSQL = `
        INSERT INTO metrics.pg_stat_checkpointer
            (connection_id, num_requested, num_timed, collected_at)
        VALUES ($1, $2, $3, $4)
    `

	insertDeadRuleDatabaseSQL = `
        INSERT INTO metrics.pg_database
            (connection_id, datname, datistemplate, age_datfrozenxid,
             collected_at)
        VALUES ($1, $2, $3, $4, $5)
    `

	insertDeadRuleTableSQL = `
        INSERT INTO metrics.pg_stat_all_tables
            (connection_id, database_name, schemaname, relname, n_live_tup,
             n_dead_tup, last_autovacuum, collected_at)
        VALUES ($1, $2, 'public', $3, $4, $5, $6, $7)
    `

	insertDeadRuleCPUSQL = `
        INSERT INTO metrics.pg_sys_cpu_usage_info
            (connection_id, usermode_normal_process_percent,
             kernelmode_process_percent, io_completion_percent,
             servicing_irq_percent, servicing_softirq_percent,
             idle_mode_percent, processor_time_percent, collected_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
    `
)

// newDeadRuleDatastore returns a Datastore backed by the integration
// test database with deadRuleSchema installed. The test is skipped when
// no test database is configured, matching the other integration tests
// in this package.
func newDeadRuleDatastore(t *testing.T) (*Datastore, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping dead rule test")
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

	if _, err := pool.Exec(ctx, deadRuleSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create dead rule schema: %v", err)
	}

	ds := &Datastore{pool: pool, config: nil}

	cleanup := func() {
		if _, err := pool.Exec(context.Background(),
			deadRuleTeardown); err != nil {
			t.Logf("dead rule teardown failed: %v", err)
		}
		pool.Close()
	}

	return ds, pool, cleanup
}

// insertDeadRuleConnection inserts a connection and returns its id.
func insertDeadRuleConnection(t *testing.T, pool *pgxpool.Pool,
	name string) int {
	t.Helper()
	var id int
	if err := pool.QueryRow(context.Background(),
		insertDeadRuleConnectionSQL, name).Scan(&id); err != nil {
		t.Fatalf("failed to insert connection %q: %v", name, err)
	}
	return id
}

// execDeadRuleSeed runs one of the named seed statements above.
func execDeadRuleSeed(t *testing.T, pool *pgxpool.Pool, sql string,
	args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("failed to seed metrics row: %v", err)
	}
}

// deadRuleValueFor returns the metric value the registry reports for
// the given connection, failing the test when the metric produces no
// row for it.
func deadRuleValueFor(t *testing.T, ds *Datastore, metric string,
	connID int) float64 {
	t.Helper()
	values, err := ds.GetLatestMetricValues(context.Background(), metric)
	if err != nil {
		t.Fatalf("GetLatestMetricValues(%s) failed: %v", metric, err)
	}
	for _, v := range values {
		if v.ConnectionID == connID {
			return v.Value
		}
	}
	t.Fatalf("metric %s returned no row for connection %d (rows: %d)",
		metric, connID, len(values))
	return 0
}

// TestDeadRuleWALArchiveFailedFires covers root cause 1. The archiver
// counters live on metrics.pg_stat_wal, so the metric used to fail with
// SQLSTATE 42P01 on every evaluation and wal_archive_failed could never
// fire. Two archive failures per sample over three samples must now be
// reported as four failures, which crosses the rule's zero threshold.
func TestDeadRuleWALArchiveFailedFires(t *testing.T) {
	ds, pool, cleanup := newDeadRuleDatastore(t)
	defer cleanup()

	connID := insertDeadRuleConnection(t, pool, "wal-archive-failures")
	now := time.Now()
	for i, failed := range []int64{3, 5, 7} {
		execDeadRuleSeed(t, pool, insertDeadRuleWALSQL, connID, int64(100),
			failed, now.Add(-time.Duration(20-i*10)*time.Minute))
	}

	value := deadRuleValueFor(t, ds,
		"pg_stat_archiver.failed_count_delta", connID)
	if value != 4 {
		t.Errorf("archive failures = %v, want 4", value)
	}
	if !(value > deadRuleArchiveFailedThreshold) {
		t.Errorf("archive failures %v does not cross threshold %v",
			value, deadRuleArchiveFailedThreshold)
	}
}

// TestDeadRuleWALArchiveFailedIgnoresStatsReset checks that a reset of
// the archiver statistics, which drops failed_count back towards zero,
// contributes no failures rather than a negative or inflated delta.
func TestDeadRuleWALArchiveFailedIgnoresStatsReset(t *testing.T) {
	ds, pool, cleanup := newDeadRuleDatastore(t)
	defer cleanup()

	connID := insertDeadRuleConnection(t, pool, "wal-archive-reset")
	now := time.Now()
	// 40 failures, then a reset to 0, then one further failure.
	for i, failed := range []int64{40, 0, 1} {
		execDeadRuleSeed(t, pool, insertDeadRuleWALSQL, connID, int64(100),
			failed, now.Add(-time.Duration(20-i*10)*time.Minute))
	}

	value := deadRuleValueFor(t, ds,
		"pg_stat_archiver.failed_count_delta", connID)
	if value != 1 {
		t.Errorf("archive failures across a stats reset = %v, want 1", value)
	}
}

// TestDeadRuleWALArchiveFailedSingleSample checks that a connection with
// only one sample in the window reports zero rather than dropping out of
// the result set, which is what makes the rule's evaluation stable
// across probe intervals.
func TestDeadRuleWALArchiveFailedSingleSample(t *testing.T) {
	ds, pool, cleanup := newDeadRuleDatastore(t)
	defer cleanup()

	connID := insertDeadRuleConnection(t, pool, "wal-archive-single")
	execDeadRuleSeed(t, pool, insertDeadRuleWALSQL, connID, int64(100),
		int64(9), time.Now().Add(-time.Minute))

	value := deadRuleValueFor(t, ds,
		"pg_stat_archiver.failed_count_delta", connID)
	if value != 0 {
		t.Errorf("archive failures from a single sample = %v, want 0", value)
	}
}

// TestDeadRuleTransactionWraparoundFires covers root cause 2. The metric
// used to return the literal 50.0 regardless of the data, so
// transaction_wraparound could not fire at its 75 percent threshold. The
// value is now the age of the oldest non-template database as a
// percentage of the 2^31-1 wraparound limit.
func TestDeadRuleTransactionWraparoundFires(t *testing.T) {
	ds, pool, cleanup := newDeadRuleDatastore(t)
	defer cleanup()

	connID := insertDeadRuleConnection(t, pool, "wraparound-critical")
	now := time.Now().Add(-time.Minute)
	execDeadRuleSeed(t, pool, insertDeadRuleDatabaseSQL, connID, "appdb",
		false, int64(1_800_000_000), now)
	execDeadRuleSeed(t, pool, insertDeadRuleDatabaseSQL, connID, "quietdb",
		false, int64(1_000_000), now)
	// A template database with a huge age must not drive the metric.
	execDeadRuleSeed(t, pool, insertDeadRuleDatabaseSQL, connID, "template0",
		true, int64(2_100_000_000), now)

	value := deadRuleValueFor(t, ds, "age_percent", connID)
	want := 1_800_000_000.0 / 2147483647.0 * 100.0
	if diff := value - want; diff > 0.01 || diff < -0.01 {
		t.Errorf("age_percent = %v, want %v", value, want)
	}
	if !(value > deadRuleWraparoundThreshold) {
		t.Errorf("age_percent %v does not cross threshold %v",
			value, deadRuleWraparoundThreshold)
	}
}

// TestDeadRuleTransactionWraparoundTracksData checks the other half of
// the constant-value defect: a healthy server must report a low value
// rather than the old hardcoded 50.0.
func TestDeadRuleTransactionWraparoundTracksData(t *testing.T) {
	ds, pool, cleanup := newDeadRuleDatastore(t)
	defer cleanup()

	connID := insertDeadRuleConnection(t, pool, "wraparound-healthy")
	execDeadRuleSeed(t, pool, insertDeadRuleDatabaseSQL, connID, "appdb",
		false, int64(50_000_000), time.Now().Add(-time.Minute))

	value := deadRuleValueFor(t, ds, "age_percent", connID)
	if value > deadRuleWraparoundThreshold {
		t.Errorf("age_percent = %v, want a value below %v", value,
			deadRuleWraparoundThreshold)
	}
	if value < 2 || value > 3 {
		t.Errorf("age_percent = %v, want roughly 2.3", value)
	}
}

// TestDeadRulePgSettingsMetricsSurviveStaleSnapshot covers root cause 3.
// The pg_settings probe is change-tracked, so a stable server stores one
// snapshot at onboarding and nothing afterwards. Both settings-derived
// metrics used to require a row written in the last hour, which killed
// them within the hour. Both must now resolve from a day-old snapshot.
func TestDeadRulePgSettingsMetricsSurviveStaleSnapshot(t *testing.T) {
	ds, pool, cleanup := newDeadRuleDatastore(t)
	defer cleanup()

	connID := insertDeadRuleConnection(t, pool, "stable-settings")
	dayOld := time.Now().Add(-24 * time.Hour)
	execDeadRuleSeed(t, pool, insertDeadRuleSettingSQL, connID,
		"max_connections", "1000", dayOld)
	execDeadRuleSeed(t, pool, insertDeadRuleActivitySQL, connID,
		time.Now().Add(-time.Minute), 900)

	maxConns := deadRuleValueFor(t, ds, "pg_settings.max_connections", connID)
	if maxConns != 1000 {
		t.Errorf("max_connections = %v, want 1000", maxConns)
	}
	if !(maxConns > deadRuleMaxConnectionsThreshold) {
		t.Errorf("max_connections %v does not cross threshold %v",
			maxConns, deadRuleMaxConnectionsThreshold)
	}

	utilization := deadRuleValueFor(t, ds, "connection_utilization_percent",
		connID)
	if utilization != 90 {
		t.Errorf("connection utilization = %v, want 90", utilization)
	}
	if !(utilization > deadRuleUtilizationThreshold) {
		t.Errorf("connection utilization %v does not cross threshold %v",
			utilization, deadRuleUtilizationThreshold)
	}
}

// TestDeadRulePgSettingsMetricsUseNewestSnapshot checks that removing
// the one-hour window did not make the metrics read an outdated value:
// where several snapshots exist, the newest one wins, and only one row
// per connection is returned.
func TestDeadRulePgSettingsMetricsUseNewestSnapshot(t *testing.T) {
	ds, pool, cleanup := newDeadRuleDatastore(t)
	defer cleanup()

	connID := insertDeadRuleConnection(t, pool, "changed-settings")
	execDeadRuleSeed(t, pool, insertDeadRuleSettingSQL, connID,
		"max_connections", "100", time.Now().Add(-72*time.Hour))
	execDeadRuleSeed(t, pool, insertDeadRuleSettingSQL, connID,
		"max_connections", "600", time.Now().Add(-24*time.Hour))
	execDeadRuleSeed(t, pool, insertDeadRuleActivitySQL, connID,
		time.Now().Add(-time.Minute), 300)

	values, err := ds.GetLatestMetricValues(context.Background(),
		"connection_utilization_percent")
	if err != nil {
		t.Fatalf("connection_utilization_percent failed: %v", err)
	}
	if len(values) != 1 {
		t.Fatalf("connection_utilization_percent returned %d rows, want 1",
			len(values))
	}
	if values[0].Value != 50 {
		t.Errorf("connection utilization = %v, want 50", values[0].Value)
	}

	maxConns := deadRuleValueFor(t, ds, "pg_settings.max_connections", connID)
	if maxConns != 600 {
		t.Errorf("max_connections = %v, want 600", maxConns)
	}
}

// TestDeadRuleAutovacuumSettingsHonoured covers the collateral damage
// described in root cause 3: table_last_autovacuum_hours read the
// autovacuum settings through the same one-hour window, so a stable
// server always fell back to the shipped defaults of 50 and 0.2. With a
// tuned threshold of 100000 the seeded table is below the calculated
// limit and must not be reported.
func TestDeadRuleAutovacuumSettingsHonoured(t *testing.T) {
	ds, pool, cleanup := newDeadRuleDatastore(t)
	defer cleanup()

	dayOld := time.Now().Add(-24 * time.Hour)
	recent := time.Now().Add(-time.Minute)

	tunedID := insertDeadRuleConnection(t, pool, "tuned-autovacuum")
	execDeadRuleSeed(t, pool, insertDeadRuleSettingSQL, tunedID,
		"autovacuum_vacuum_threshold", "100000", dayOld)
	execDeadRuleSeed(t, pool, insertDeadRuleSettingSQL, tunedID,
		"autovacuum_vacuum_scale_factor", "0.5", dayOld)
	// 10000 dead tuples clears the default limit of 50 + 0.2 * 1000 but
	// stays well under the tuned limit of 100000 + 0.5 * 1000.
	execDeadRuleSeed(t, pool, insertDeadRuleTableSQL, tunedID, "appdb",
		"orders", int64(1000), int64(10000), dayOld, recent)

	// An untuned connection with identical table data still reports,
	// which proves the tuned connection was excluded by its settings
	// rather than by the seed data or the window.
	defaultID := insertDeadRuleConnection(t, pool, "default-autovacuum")
	execDeadRuleSeed(t, pool, insertDeadRuleTableSQL, defaultID, "appdb",
		"orders", int64(1000), int64(10000), dayOld, recent)

	values, err := ds.GetLatestMetricValues(context.Background(),
		"table_last_autovacuum_hours")
	if err != nil {
		t.Fatalf("table_last_autovacuum_hours failed: %v", err)
	}
	seenDefault := false
	for _, v := range values {
		switch v.ConnectionID {
		case tunedID:
			t.Errorf("table_last_autovacuum_hours reported %v for a "+
				"table below the tuned autovacuum limit", v.Value)
		case defaultID:
			seenDefault = true
		}
	}
	if !seenDefault {
		t.Error("table_last_autovacuum_hours reported nothing for the " +
			"connection running the default autovacuum settings")
	}
}

// TestDeadRuleCPUUsageFiresOnLinux covers root cause 4. The system_stats
// extension only fills processor_time_percent on Windows, so coalescing
// it to zero made cpu_usage_high read 0 percent on a saturated Linux
// host. A 95 percent busy Linux row must now report 95.
func TestDeadRuleCPUUsageFiresOnLinux(t *testing.T) {
	ds, pool, cleanup := newDeadRuleDatastore(t)
	defer cleanup()

	connID := insertDeadRuleConnection(t, pool, "busy-linux-host")
	execDeadRuleSeed(t, pool, insertDeadRuleCPUSQL, connID,
		70.0, 20.0, 3.0, 1.0, 1.0, 5.0, nil, time.Now().Add(-time.Minute))

	value := deadRuleValueFor(t, ds,
		"pg_sys_cpu_usage_info.processor_time_percent", connID)
	if value != 95 {
		t.Errorf("cpu busy percent = %v, want 95", value)
	}
	if !(value > deadRuleCPUThreshold) {
		t.Errorf("cpu busy percent %v does not cross threshold %v",
			value, deadRuleCPUThreshold)
	}
}

// TestDeadRuleCPUUsageWindowsAndIdleFallbacks checks the other two
// branches of the portable CPU expression: a Windows row still reports
// processor_time_percent, and a Linux row with no idle percentage falls
// back to the sum of the busy buckets.
func TestDeadRuleCPUUsageWindowsAndIdleFallbacks(t *testing.T) {
	ds, pool, cleanup := newDeadRuleDatastore(t)
	defer cleanup()

	windowsID := insertDeadRuleConnection(t, pool, "windows-host")
	execDeadRuleSeed(t, pool, insertDeadRuleCPUSQL, windowsID,
		nil, nil, nil, nil, nil, nil, 92.5, time.Now().Add(-time.Minute))

	bucketsID := insertDeadRuleConnection(t, pool, "linux-no-idle")
	execDeadRuleSeed(t, pool, insertDeadRuleCPUSQL, bucketsID,
		60.0, 25.0, 2.0, 0.5, 0.5, nil, nil, time.Now().Add(-time.Minute))

	idleID := insertDeadRuleConnection(t, pool, "linux-idle-host")
	execDeadRuleSeed(t, pool, insertDeadRuleCPUSQL, idleID,
		2.0, 1.0, 0.0, 0.0, 0.0, 97.0, nil, time.Now().Add(-time.Minute))

	metric := "pg_sys_cpu_usage_info.processor_time_percent"
	if got := deadRuleValueFor(t, ds, metric, windowsID); got != 92.5 {
		t.Errorf("windows cpu busy percent = %v, want 92.5", got)
	}
	if got := deadRuleValueFor(t, ds, metric, bucketsID); got != 88 {
		t.Errorf("bucket-sum cpu busy percent = %v, want 88", got)
	}
	if got := deadRuleValueFor(t, ds, metric, idleID); got != 3 {
		t.Errorf("idle host cpu busy percent = %v, want 3", got)
	}
}

// TestDeadRuleCheckpointWarningFires covers root cause 5. The metric
// used to report the largest increase between two consecutive samples of
// a 600 second probe, so "more than 50 requested checkpoints" was a
// per-ten-minute figure that the seeded rule could not reach. The value
// is now the total requested over the last hour.
func TestDeadRuleCheckpointWarningFires(t *testing.T) {
	ds, pool, cleanup := newDeadRuleDatastore(t)
	defer cleanup()

	connID := insertDeadRuleConnection(t, pool, "checkpoint-storm")
	now := time.Now()
	for i, requested := range []int64{1000, 1006, 1012, 1018, 1024, 1030} {
		execDeadRuleSeed(t, pool, insertDeadRuleCheckpointerSQL, connID,
			requested, int64(10),
			now.Add(-time.Duration(50-i*10)*time.Minute))
	}

	value := deadRuleValueFor(t, ds,
		"pg_stat_checkpointer.checkpoints_req_delta", connID)
	if value != 30 {
		t.Errorf("requested checkpoints per hour = %v, want 30", value)
	}
	if !(value > deadRuleCheckpointThreshold) {
		t.Errorf("requested checkpoints %v does not cross threshold %v",
			value, deadRuleCheckpointThreshold)
	}
}

// TestDeadRuleCheckpointWarningStableAcrossIntervals checks the
// intermittency half of root cause 5: a connection with a single sample
// in the window used to disappear from the result set entirely, which
// made the rule evaluate only about half the time. It must now report
// zero, and a stats reset must not inflate the count.
func TestDeadRuleCheckpointWarningStableAcrossIntervals(t *testing.T) {
	ds, pool, cleanup := newDeadRuleDatastore(t)
	defer cleanup()

	singleID := insertDeadRuleConnection(t, pool, "checkpoint-single")
	execDeadRuleSeed(t, pool, insertDeadRuleCheckpointerSQL, singleID,
		int64(500), int64(10), time.Now().Add(-time.Minute))

	resetID := insertDeadRuleConnection(t, pool, "checkpoint-reset")
	now := time.Now()
	for i, requested := range []int64{900, 0, 4} {
		execDeadRuleSeed(t, pool, insertDeadRuleCheckpointerSQL, resetID,
			requested, int64(10),
			now.Add(-time.Duration(30-i*10)*time.Minute))
	}

	metric := "pg_stat_checkpointer.checkpoints_req_delta"
	if got := deadRuleValueFor(t, ds, metric, singleID); got != 0 {
		t.Errorf("single sample requested checkpoints = %v, want 0", got)
	}
	if got := deadRuleValueFor(t, ds, metric, resetID); got != 4 {
		t.Errorf("requested checkpoints across a reset = %v, want 4", got)
	}
}

// TestDeadRuleArchiverMetricReadsPgStatWal pins the table the archiver
// metric reads, because the whole defect was a query against a relation
// the collector never creates. Dropping metrics.pg_stat_wal must break
// the metric; there must be no metrics.pg_stat_archiver to fall back on.
func TestDeadRuleArchiverMetricReadsPgStatWal(t *testing.T) {
	ds, pool, cleanup := newDeadRuleDatastore(t)
	defer cleanup()

	if _, err := pool.Exec(context.Background(),
		"DROP TABLE metrics.pg_stat_wal"); err != nil {
		t.Fatalf("failed to drop metrics.pg_stat_wal: %v", err)
	}

	_, err := ds.GetLatestMetricValues(context.Background(),
		"pg_stat_archiver.failed_count_delta")
	if err == nil {
		t.Fatal("archiver metric succeeded without metrics.pg_stat_wal")
	}
}
