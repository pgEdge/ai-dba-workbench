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
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This file holds verification tests for an audit of the alerter
// subsystem. Each test documents an audit claim and pins the behavior
// that the code actually exhibits today. Where the audit claim
// describes a defect, the test asserts the CURRENT (defective)
// behavior and the comment states what the behavior SHOULD be, so the
// suite stays green until the defect is fixed; a fix will make the
// pinned assertion fail, which is the intended signal to update the
// test alongside the fix.
//
// Tests whose name ends in "Demo" assert the CORRECT behavior instead
// and therefore fail against the current code. They are skipped unless
// ALERTER_DEFECT_DEMO=1 is set, so CI stays green while the defect can
// still be demonstrated on demand.

// defectDemoEnabled skips the calling test unless ALERTER_DEFECT_DEMO
// is set. Demo tests assert the behavior the audit says the code
// should have, so they fail while the defect is present.
func defectDemoEnabled(t *testing.T) {
	t.Helper()
	if os.Getenv("ALERTER_DEFECT_DEMO") == "" {
		t.Skip("set ALERTER_DEFECT_DEMO=1 to run the defect demonstration")
	}
}

// auditDefectsSchema mirrors the production collector schema for the
// columns the metric registry queries actually read. The table set is
// deliberately faithful on one point: metrics.pg_stat_wal exists (it
// carries the archiver columns in production) and
// metrics.pg_stat_archiver does NOT, because the collector never
// creates such a table.
const auditDefectsSchema = `
DROP SCHEMA IF EXISTS metrics CASCADE;
DROP TABLE IF EXISTS metric_baselines CASCADE;
DROP TABLE IF EXISTS connections CASCADE;

CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    is_monitored BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE metric_baselines (
    id BIGSERIAL PRIMARY KEY,
    connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    database_name TEXT,
    metric_name TEXT NOT NULL,
    period_type TEXT NOT NULL,
    day_of_week INTEGER,
    hour_of_day INTEGER,
    mean REAL NOT NULL,
    stddev REAL NOT NULL,
    min REAL NOT NULL,
    max REAL NOT NULL,
    sample_count BIGINT NOT NULL DEFAULT 0,
    last_calculated TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    earliest_sample_at TIMESTAMPTZ
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

CREATE TABLE metrics.pg_stat_all_tables (
    connection_id INTEGER NOT NULL,
    database_name TEXT NOT NULL,
    schemaname TEXT NOT NULL,
    relname TEXT NOT NULL,
    n_live_tup BIGINT,
    n_dead_tup BIGINT,
    collected_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE metrics.pg_stat_database (
    connection_id INTEGER NOT NULL,
    database_name VARCHAR(255) NOT NULL,
    datname TEXT,
    blks_hit BIGINT,
    blks_read BIGINT,
    collected_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE metrics.pg_stat_statements (
    connection_id INTEGER NOT NULL,
    database_name TEXT NOT NULL,
    queryid BIGINT NOT NULL,
    calls BIGINT,
    mean_exec_time DOUBLE PRECISION,
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

// auditDefectsTeardown drops everything auditDefectsSchema creates.
const auditDefectsTeardown = `
DROP SCHEMA IF EXISTS metrics CASCADE;
DROP TABLE IF EXISTS metric_baselines CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
`

// Seed statements used by the audit tests. They are named constants so
// the Codacy/Semgrep go_sql_rule-concat-sqli rule does not flag inline
// multi-line SQL passed to Exec; every value is still bound via $N.
const (
	insertAuditConnectionSQL = `
        INSERT INTO connections (name, enabled, is_monitored)
        VALUES ($1, TRUE, TRUE)
        RETURNING id
    `

	insertAuditPgSettingSQL = `
        INSERT INTO metrics.pg_settings
            (connection_id, name, setting, collected_at)
        VALUES ($1, $2, $3, $4)
    `

	insertAuditAllTablesSQL = `
        INSERT INTO metrics.pg_stat_all_tables
            (connection_id, database_name, schemaname, relname,
             n_live_tup, n_dead_tup, collected_at)
        VALUES ($1, $2, 'public', $3, $4, 0, $5)
    `

	insertAuditStatDatabaseSQL = `
        INSERT INTO metrics.pg_stat_database
            (connection_id, database_name, datname, blks_hit, blks_read,
             collected_at)
        VALUES ($1, $2, $3, $4, $5, $6)
    `

	insertAuditStatStatementsSQL = `
        INSERT INTO metrics.pg_stat_statements
            (connection_id, database_name, queryid, calls, mean_exec_time,
             collected_at)
        VALUES ($1, $2, $3, $4, $5, $6)
    `

	insertAuditCPUUsageSQL = `
        INSERT INTO metrics.pg_sys_cpu_usage_info
            (connection_id, usermode_normal_process_percent,
             kernelmode_process_percent, idle_mode_percent,
             processor_time_percent, collected_at)
        VALUES ($1, $2, $3, $4, $5, $6)
    `

	insertAuditBaselineSQL = `
        INSERT INTO metric_baselines
            (connection_id, database_name, metric_name, period_type,
             day_of_week, hour_of_day, mean, stddev, min, max,
             sample_count, earliest_sample_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
    `
)

// newAuditDefectsDatastore returns a Datastore backed by the
// integration test database with auditDefectsSchema installed. The
// test is skipped when no test database is configured, matching the
// convention used by the other integration tests in this package.
func newAuditDefectsDatastore(t *testing.T) (*Datastore, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping audit defect test")
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

	if _, err := pool.Exec(ctx, auditDefectsSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create audit defect schema: %v", err)
	}

	ds := &Datastore{pool: pool, config: nil}

	cleanup := func() {
		if _, err := pool.Exec(context.Background(),
			auditDefectsTeardown); err != nil {
			t.Logf("audit defect teardown failed: %v", err)
		}
		pool.Close()
	}

	return ds, pool, cleanup
}

// insertAuditConnection inserts a connection and returns its id.
func insertAuditConnection(t *testing.T, pool *pgxpool.Pool, name string) int {
	t.Helper()
	var id int
	if err := pool.QueryRow(context.Background(),
		insertAuditConnectionSQL, name).Scan(&id); err != nil {
		t.Fatalf("failed to insert connection %q: %v", name, err)
	}
	return id
}

// collectorSchemaMetricsTables scans the collector schema source for
// every "CREATE TABLE ... metrics.<name>" statement and returns the
// sorted, de-duplicated table names. The test is skipped when the
// collector source is not present (for example in a packaged build),
// because this is a cross-module source inspection rather than a
// runtime assertion.
func collectorSchemaMetricsTables(t *testing.T) []string {
	t.Helper()

	path, err := filepath.Abs(filepath.Join(
		"..", "..", "..", "..", "collector", "src", "database", "schema.go"))
	if err != nil {
		t.Skipf("cannot resolve collector schema path: %v", err)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- fixed repo-relative path
	if err != nil {
		t.Skipf("collector schema source unavailable at %s: %v", path, err)
	}

	re := regexp.MustCompile(`CREATE TABLE (?:IF NOT EXISTS )?metrics\.([a-z0-9_]+)`)
	seen := make(map[string]struct{})
	for _, m := range re.FindAllStringSubmatch(string(data), -1) {
		seen[m[1]] = struct{}{}
	}

	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// TestAuditC2ArchiverTableDoesNotExist verifies audit claim C2: the
// "wal_archive_failed" rule reads its metric from
// metrics.pg_stat_archiver, but the collector never creates that
// table. The archiver columns live on metrics.pg_stat_wal instead, so
// every evaluation of the rule fails with an undefined-table error.
//
// The registry entry SHOULD read failed_count from
// metrics.pg_stat_wal.
func TestAuditC2ArchiverTableDoesNotExist(t *testing.T) {
	t.Run("collector schema never creates metrics.pg_stat_archiver", func(t *testing.T) {
		tables := collectorSchemaMetricsTables(t)

		index := make(map[string]struct{}, len(tables))
		for _, name := range tables {
			index[name] = struct{}{}
		}

		if _, ok := index["pg_stat_archiver"]; ok {
			t.Errorf("collector schema unexpectedly creates "+
				"metrics.pg_stat_archiver; tables=%v", tables)
		}
		if _, ok := index["pg_stat_wal"]; !ok {
			t.Errorf("expected metrics.pg_stat_wal in collector schema; "+
				"tables=%v", tables)
		}
		t.Logf("collector metrics tables (%d): %v", len(tables), tables)
	})

	t.Run("registry entry targets the missing table", func(t *testing.T) {
		cfg, ok := metricRegistry["pg_stat_archiver.failed_count_delta"]
		if !ok {
			t.Fatal("pg_stat_archiver.failed_count_delta missing from registry")
		}
		// Current (defective) behavior. It SHOULD be
		// "metrics.pg_stat_wal".
		if !strings.Contains(cfg.latestSQL, "FROM metrics.pg_stat_archiver") {
			t.Error("expected latestSQL to select FROM metrics.pg_stat_archiver")
		}
	})

	t.Run("query errors at runtime", func(t *testing.T) {
		ds, pool, cleanup := newAuditDefectsDatastore(t)
		defer cleanup()

		ctx := context.Background()
		connID := insertAuditConnection(t, pool, "audit-c2")

		// Seed the archiver data where it really lives so the failure
		// cannot be blamed on missing data.
		if _, err := pool.Exec(ctx, `
            INSERT INTO metrics.pg_stat_wal
                (connection_id, archived_count, failed_count, collected_at)
            VALUES ($1, 10, 1, NOW() - INTERVAL '10 minutes'),
                   ($1, 10, 7, NOW() - INTERVAL '1 minute')
        `, connID); err != nil {
			t.Fatalf("failed to seed pg_stat_wal: %v", err)
		}

		_, err := ds.GetLatestMetricValues(ctx,
			"pg_stat_archiver.failed_count_delta")
		if err == nil {
			t.Fatal("expected an error from GetLatestMetricValues, got nil")
		}
		if !strings.Contains(err.Error(), "metrics.pg_stat_archiver") {
			t.Errorf("expected undefined-table error naming "+
				"metrics.pg_stat_archiver, got: %v", err)
		}
		t.Logf("GetLatestMetricValues error: %v", err)
	})
}

// TestAuditC3TransactionWraparoundReturnsConstant verifies audit claim
// C3: the "age_percent" metric backing the transaction_wraparound rule
// selects the literal 50.0 rather than a computed transaction age. The
// seeded rule compares with "> 75", so the rule can never fire.
//
// The metric SHOULD compute age(relfrozenxid) / autovacuum_freeze_max_age.
func TestAuditC3TransactionWraparoundReturnsConstant(t *testing.T) {
	cfg, ok := metricRegistry["age_percent"]
	if !ok {
		t.Fatal("age_percent missing from registry")
	}
	// Current (defective) behavior: a hardcoded literal.
	if !strings.Contains(cfg.latestSQL, "50.0::float as value") {
		t.Error("expected age_percent latestSQL to select the literal 50.0")
	}

	ds, pool, cleanup := newAuditDefectsDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertAuditConnection(t, pool, "audit-c3")

	if _, err := pool.Exec(ctx, insertAuditPgSettingSQL,
		connID, "autovacuum_freeze_max_age", "200000000",
		nowMinus(t, pool, "1 minute")); err != nil {
		t.Fatalf("failed to seed pg_settings: %v", err)
	}

	// Two tables with wildly different live-tuple counts; whatever the
	// metric claims to measure, the result must not depend on them for
	// the constant to be provable.
	for i, relname := range []string{"tiny", "huge"} {
		liveTup := int64(1)
		if i == 1 {
			liveTup = 5_000_000_000
		}
		if _, err := pool.Exec(ctx, insertAuditAllTablesSQL,
			connID, "appdb", relname, liveTup,
			nowMinus(t, pool, "1 minute")); err != nil {
			t.Fatalf("failed to seed pg_stat_all_tables: %v", err)
		}
	}

	values, err := ds.GetLatestMetricValues(ctx, "age_percent")
	if err != nil {
		t.Fatalf("GetLatestMetricValues(age_percent) failed: %v", err)
	}
	if len(values) != 1 {
		t.Fatalf("expected one row, got %d", len(values))
	}
	if values[0].Value != 50.0 {
		t.Errorf("expected the hardcoded 50.0, got %v", values[0].Value)
	}

	// The seeded rule is "age_percent > 75" (critical). 50 > 75 is
	// false for every possible input, so the rule is inert.
	const seededThreshold = 75.0
	if values[0].Value > seededThreshold {
		t.Errorf("value %v unexpectedly exceeds the seeded threshold %v",
			values[0].Value, seededThreshold)
	}
}

// TestAuditC4PgSettingsMetricsExpireAfterOneHour verifies the alerter
// half of audit claim C4: both pg_settings.max_connections and
// connection_utilization_percent require a metrics.pg_settings row
// collected within the last hour. When the newest row is older, the
// metric reports no data and the rule silently stops evaluating.
//
// The collector half of the claim (the pg_settings probe skips writes
// when the settings have not changed) is covered by the collector's
// own TestPgSettingsProbe_StoreUnchanged.
//
// The registry entries SHOULD use DISTINCT ON without a recency
// window, or the probe SHOULD refresh on a heartbeat.
func TestAuditC4PgSettingsMetricsExpireAfterOneHour(t *testing.T) {
	ds, pool, cleanup := newAuditDefectsDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertAuditConnection(t, pool, "audit-c4")

	// A fresh pg_settings snapshot plus current activity: both metrics
	// resolve.
	if _, err := pool.Exec(ctx, insertAuditPgSettingSQL,
		connID, "max_connections", "600",
		nowMinus(t, pool, "5 minutes")); err != nil {
		t.Fatalf("failed to seed fresh pg_settings: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := pool.Exec(ctx, `
            INSERT INTO metrics.pg_stat_activity
                (connection_id, backend_type, collected_at)
            VALUES ($1, 'client backend', NOW() - INTERVAL '1 minute')
        `, connID); err != nil {
			t.Fatalf("failed to seed pg_stat_activity: %v", err)
		}
	}

	if _, err := ds.GetLatestMetricValues(ctx,
		"pg_settings.max_connections"); err != nil {
		t.Fatalf("expected max_connections to resolve with a fresh row: %v", err)
	}
	if _, err := ds.GetLatestMetricValues(ctx,
		"connection_utilization_percent"); err != nil {
		t.Fatalf("expected connection_utilization to resolve with a fresh row: %v",
			err)
	}

	// Age the only pg_settings snapshot past the one-hour window, as
	// happens whenever the configuration has not changed for an hour.
	if _, err := pool.Exec(ctx, `
        UPDATE metrics.pg_settings
        SET collected_at = NOW() - INTERVAL '61 minutes'
        WHERE connection_id = $1
    `, connID); err != nil {
		t.Fatalf("failed to age pg_settings: %v", err)
	}

	// Current (defective) behavior: both metrics go dark. They SHOULD
	// continue to report the most recent known configuration.
	if _, err := ds.GetLatestMetricValues(ctx,
		"pg_settings.max_connections"); err == nil {
		t.Error("expected max_connections to report no data after one hour")
	} else {
		t.Logf("max_connections after 61 minutes: %v", err)
	}
	if _, err := ds.GetLatestMetricValues(ctx,
		"connection_utilization_percent"); err == nil {
		t.Error("expected connection_utilization to report no data after one hour")
	} else {
		t.Logf("connection_utilization after 61 minutes: %v", err)
	}
}

// TestAuditC5CPUUsageNullProcessorTimeReadsZero verifies the alerter
// half of audit claim C5: the cpu_usage_high rule keys on
// processor_time_percent, and the registry wraps it in COALESCE(..., 0).
// When the column is NULL, the metric reports 0, which can never
// exceed the seeded threshold of 80.
//
// Whether system_stats actually leaves processor_time_percent NULL on
// Linux cannot be verified here; the extension is not installed in the
// test environment. This test verifies only the consequence.
//
// The metric SHOULD derive CPU usage from the Linux-populated columns
// (for example 100 - idle_mode_percent) or from a COALESCE chain.
func TestAuditC5CPUUsageNullProcessorTimeReadsZero(t *testing.T) {
	ds, pool, cleanup := newAuditDefectsDatastore(t)
	defer cleanup()

	ctx := context.Background()

	cases := []struct {
		name               string
		processorTime      any
		usermodeNormal     float64
		kernelmode         float64
		idle               float64
		wantValue          float64
		wantExceedsDefault bool
	}{
		{
			name:           "linux shape leaves processor_time NULL",
			processorTime:  nil,
			usermodeNormal: 71.5,
			kernelmode:     23.5,
			idle:           5.0,
			// COALESCE(NULL, 0) yields 0 even though the machine is
			// 95% busy. It SHOULD report roughly 95.
			wantValue:          0,
			wantExceedsDefault: false,
		},
		{
			name:               "windows shape populates processor_time",
			processorTime:      92.5,
			usermodeNormal:     0,
			kernelmode:         0,
			idle:               7.5,
			wantValue:          92.5,
			wantExceedsDefault: true,
		},
	}

	const seededThreshold = 80.0

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx,
				`DELETE FROM metrics.pg_sys_cpu_usage_info`); err != nil {
				t.Fatalf("failed to reset cpu rows: %v", err)
			}
			if _, err := pool.Exec(ctx, `DELETE FROM connections`); err != nil {
				t.Fatalf("failed to reset connections: %v", err)
			}
			connID := insertAuditConnection(t, pool, "audit-c5-"+tc.name)

			if _, err := pool.Exec(ctx, insertAuditCPUUsageSQL,
				connID, tc.usermodeNormal, tc.kernelmode, tc.idle,
				tc.processorTime, nowMinus(t, pool, "1 minute")); err != nil {
				t.Fatalf("failed to seed cpu usage: %v", err)
			}

			values, err := ds.GetLatestMetricValues(ctx,
				"pg_sys_cpu_usage_info.processor_time_percent")
			if err != nil {
				t.Fatalf("GetLatestMetricValues failed: %v", err)
			}
			if len(values) != 1 {
				t.Fatalf("expected one row, got %d", len(values))
			}
			if values[0].Value != tc.wantValue {
				t.Errorf("value = %v, want %v", values[0].Value, tc.wantValue)
			}
			if got := values[0].Value > seededThreshold; got != tc.wantExceedsDefault {
				t.Errorf("value > %v = %v, want %v",
					seededThreshold, got, tc.wantExceedsDefault)
			}
		})
	}
}

// TestAuditC6BaselineOrderingPrefersAll verifies audit claim C6:
// GetMetricBaselines orders by period_type, which is TEXT. The three
// period types sort alphabetically as 'all' < 'daily' < 'hourly', so
// the first row is always the 'all' baseline whenever one exists.
// detectAnomalies reads baselines[0] and therefore never uses the
// time-aware baselines.
//
// The query SHOULD select the baseline matching the current hour or
// weekday, falling back to 'all'.
func TestAuditC6BaselineOrderingPrefersAll(t *testing.T) {
	ds, pool, cleanup := newAuditDefectsDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertAuditConnection(t, pool, "audit-c6")

	// Insert in an order that would surface a different first row if
	// the query respected insertion order or specificity.
	rows := []struct {
		periodType string
		dayOfWeek  any
		hourOfDay  any
		mean       float64
	}{
		{"hourly", nil, 3, 30},
		{"daily", 2, nil, 20},
		{"all", nil, nil, 10},
	}
	for _, r := range rows {
		if _, err := pool.Exec(ctx, insertAuditBaselineSQL,
			connID, nil, "pg_stat_activity.count", r.periodType,
			r.dayOfWeek, r.hourOfDay, r.mean, 1.0, r.mean-1, r.mean+1,
			int64(500), nil); err != nil {
			t.Fatalf("failed to insert %s baseline: %v", r.periodType, err)
		}
	}

	baselines, err := ds.GetMetricBaselines(ctx, connID,
		"pg_stat_activity.count")
	if err != nil {
		t.Fatalf("GetMetricBaselines failed: %v", err)
	}
	if len(baselines) != 3 {
		t.Fatalf("expected 3 baselines, got %d", len(baselines))
	}

	got := make([]string, len(baselines))
	for i, b := range baselines {
		got[i] = b.PeriodType
	}
	// Current (defective) ordering: strictly alphabetical on the TEXT
	// column, so 'all' always wins the baselines[0] selection made by
	// detectAnomalies.
	want := []string{"all", "daily", "hourly"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("period_type order = %v, want %v", got, want)
		}
	}
	if baselines[0].PeriodType != "all" {
		t.Errorf("baselines[0].PeriodType = %q, want \"all\"",
			baselines[0].PeriodType)
	}
}

// TestAuditC10BaselineLookupIgnoresDatabase verifies the query half of
// audit claim C10: GetMetricBaselines filters on connection_id and
// metric_name only. For a per-database metric it therefore returns one
// row per database with no way for the caller to pick the right one,
// and the ORDER BY cannot break the tie because every returned row
// shares the same period_type, day_of_week, and hour_of_day.
//
// The lookup SHOULD accept a database name and filter on it.
func TestAuditC10BaselineLookupIgnoresDatabase(t *testing.T) {
	ds, pool, cleanup := newAuditDefectsDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertAuditConnection(t, pool, "audit-c10-query")

	for _, dbName := range []string{"alpha", "beta"} {
		if _, err := pool.Exec(ctx, insertAuditBaselineSQL,
			connID, dbName, "pg_stat_database.deadlocks_delta", "all",
			nil, nil, 1.0, 0.5, 0.0, 2.0, int64(500), nil); err != nil {
			t.Fatalf("failed to insert %s baseline: %v", dbName, err)
		}
	}

	baselines, err := ds.GetMetricBaselines(ctx, connID,
		"pg_stat_database.deadlocks_delta")
	if err != nil {
		t.Fatalf("GetMetricBaselines failed: %v", err)
	}

	// Current (defective) behavior: both per-database baselines come
	// back from a call that had no way to name a database.
	if len(baselines) != 2 {
		t.Fatalf("expected both per-database baselines, got %d", len(baselines))
	}
	for _, b := range baselines {
		if b.DatabaseName == nil {
			t.Fatal("expected per-database baselines to carry a database name")
		}
		if b.PeriodType != "all" {
			t.Fatalf("unexpected period_type %q", b.PeriodType)
		}
	}
	// Every ordering key is identical across the two rows, so
	// baselines[0] is whichever row the planner happened to emit first.
	t.Logf("baselines[0] database = %q (arbitrary among %d equal-ranked rows)",
		*baselines[0].DatabaseName, len(baselines))
}

// TestAuditC7HistoricalSQLCoverage verifies the counting half of audit
// claim C7. The audit says "12 of 34 metrics" have an empty
// historicalSQL; the registry actually holds a different number of
// entries and a different number of empty ones. The test pins both
// figures so the discrepancy is explicit.
//
// Metrics with an empty historicalSQL fall back to
// calculateGlobalBaselinesFallback, which cannot produce a warm
// baseline; see the engine-side test for that half of the claim.
func TestAuditC7HistoricalSQLCoverage(t *testing.T) {
	var empty []string
	for name, cfg := range metricRegistry {
		if strings.TrimSpace(cfg.historicalSQL) == "" {
			empty = append(empty, name)
		}
	}
	sort.Strings(empty)

	t.Logf("registry entries: %d; empty historicalSQL: %d",
		len(metricRegistry), len(empty))
	for _, name := range empty {
		t.Logf("  no historical SQL: %s", name)
	}

	// Pinned figures for the current code. The audit's "12 of 34" is
	// wrong on both numbers; 34 is the count of seeded alert_rules
	// rows, not of registry metrics.
	const (
		wantRegistryEntries = 32
		wantEmptyHistorical = 14
	)
	if len(metricRegistry) != wantRegistryEntries {
		t.Errorf("registry entries = %d, want %d",
			len(metricRegistry), wantRegistryEntries)
	}
	if len(empty) != wantEmptyHistorical {
		t.Errorf("entries with empty historicalSQL = %d, want %d",
			len(empty), wantEmptyHistorical)
	}

	// Every empty-historicalSQL metric must in fact fail
	// GetHistoricalMetricValues, because that is what pushes baseline
	// calculation onto the fallback path.
	ds, _, cleanup := newAuditDefectsDatastore(t)
	defer cleanup()

	ctx := context.Background()
	for _, name := range empty {
		if _, err := ds.GetHistoricalMetricValues(ctx, name, 7); err == nil {
			t.Errorf("GetHistoricalMetricValues(%s) unexpectedly succeeded", name)
		}
	}
}

// TestAuditC8CacheHitRatioReturnsEveryDeltaRow verifies audit claim
// C8: unlike its sibling delta metrics, pg_stat_database.cache_hit_ratio
// performs no latest-sample reduction, so it returns one row per
// sample interval in the 15-minute window.
//
// The consequence depends on where the violating interval sits in the
// window, because the query carries no ORDER BY and PostgreSQL emits
// the rows in the window-function ordering (collected_at ascending):
//
//   - Violation in the oldest interval: the evaluator fires (it scans
//     every row) and the cleaner never clears (it stops at the first
//     matching row, which still violates). The alert latches.
//   - Violation in the newest interval: the evaluator fires and the
//     cleaner clears on the same data, which flaps.
//
// The query SHOULD reduce to the most recent interval, as
// deadlocks_delta and temp_files_delta do with MAX()/GROUP BY.
func TestAuditC8CacheHitRatioReturnsEveryDeltaRow(t *testing.T) {
	ds, pool, cleanup := newAuditDefectsDatastore(t)
	defer cleanup()

	ctx := context.Background()
	const seededThreshold = 80.0

	// Each case seeds four samples, producing three delta intervals.
	// Every interval moves at least 10000 blocks so it clears the
	// query's minimum-activity filter.
	cases := []struct {
		name string
		// hits and reads are cumulative counters per sample.
		hits  []int64
		reads []int64
		// wantFirstRowViolates records whether the row the cleaner
		// would inspect breaches the threshold.
		wantFirstRowViolates bool
	}{
		{
			name:                 "violation in oldest interval",
			hits:                 []int64{0, 10_000, 40_000, 70_000},
			reads:                []int64{0, 10_000, 10_000, 10_000},
			wantFirstRowViolates: true,
		},
		{
			name:                 "violation in newest interval",
			hits:                 []int64{0, 30_000, 60_000, 60_000},
			reads:                []int64{0, 0, 0, 30_000},
			wantFirstRowViolates: false,
		},
	}

	offsets := []string{"12 minutes", "8 minutes", "4 minutes", "1 minute"}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx,
				`DELETE FROM metrics.pg_stat_database`); err != nil {
				t.Fatalf("failed to reset pg_stat_database: %v", err)
			}
			if _, err := pool.Exec(ctx, `DELETE FROM connections`); err != nil {
				t.Fatalf("failed to reset connections: %v", err)
			}
			connID := insertAuditConnection(t, pool, "audit-c8-"+tc.name)

			for i, offset := range offsets {
				if _, err := pool.Exec(ctx, insertAuditStatDatabaseSQL,
					connID, "appdb", "appdb", tc.hits[i], tc.reads[i],
					nowMinus(t, pool, offset)); err != nil {
					t.Fatalf("failed to seed pg_stat_database: %v", err)
				}
			}

			values, err := ds.GetLatestMetricValues(ctx,
				"pg_stat_database.cache_hit_ratio")
			if err != nil {
				t.Fatalf("GetLatestMetricValues failed: %v", err)
			}

			// Current (defective) behavior: three rows for a single
			// connection/database pair. There SHOULD be exactly one.
			if len(values) != 3 {
				t.Fatalf("expected 3 unreduced delta rows, got %d", len(values))
			}

			var violating int
			for _, v := range values {
				if v.Value < seededThreshold {
					violating++
				}
				t.Logf("row: value=%.2f collected_at=%s", v.Value, v.CollectedAt)
			}
			if violating != 1 {
				t.Fatalf("expected exactly one violating row, got %d", violating)
			}

			// The evaluator fires because at least one row violates.
			// The cleaner inspects values[0] only.
			gotFirstViolates := values[0].Value < seededThreshold
			if gotFirstViolates != tc.wantFirstRowViolates {
				t.Errorf("first row violates = %v, want %v (values=%v)",
					gotFirstViolates, tc.wantFirstRowViolates, values)
			}
		})
	}

	// Contrast with the sibling delta metrics, which do reduce.
	for _, name := range []string{
		"pg_stat_database.deadlocks_delta",
		"pg_stat_database.temp_files_delta",
	} {
		cfg := metricRegistry[name]
		if !strings.Contains(cfg.latestSQL, "GROUP BY") {
			t.Errorf("%s unexpectedly lacks a GROUP BY reduction", name)
		}
	}
	if strings.Contains(metricRegistry["pg_stat_database.cache_hit_ratio"].latestSQL,
		"GROUP BY") {
		t.Error("cache_hit_ratio unexpectedly contains a GROUP BY reduction")
	}
}

// TestAuditC9SlowQueryCountUsesLifetimeMean verifies audit claim C9:
// slow_query_count counts distinct queryids whose mean_exec_time
// exceeds 1000 ms. pg_stat_statements.mean_exec_time is a lifetime
// average since the last stats reset, so a query that ran slowly once
// keeps the count elevated forever, even when it never executes again.
//
// The metric SHOULD derive a windowed mean from the deltas of
// total_exec_time and calls.
func TestAuditC9SlowQueryCountUsesLifetimeMean(t *testing.T) {
	cfg := metricRegistry["pg_stat_statements.slow_query_count"]
	if !strings.Contains(cfg.latestSQL, "mean_exec_time > 1000") {
		t.Error("expected slow_query_count to filter on mean_exec_time > 1000")
	}
	if strings.Contains(cfg.latestSQL, "total_exec_time") {
		t.Error("slow_query_count unexpectedly references total_exec_time")
	}

	ds, pool, cleanup := newAuditDefectsDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertAuditConnection(t, pool, "audit-c9")

	// Twelve queryids whose lifetime mean is above 1000 ms and whose
	// call counts never change across the window: they have not
	// executed at all during the last 15 minutes.
	for queryID := int64(1); queryID <= 12; queryID++ {
		for _, offset := range []string{"12 minutes", "6 minutes", "1 minute"} {
			if _, err := pool.Exec(ctx, insertAuditStatStatementsSQL,
				connID, "appdb", queryID, int64(5), 4200.0,
				nowMinus(t, pool, offset)); err != nil {
				t.Fatalf("failed to seed pg_stat_statements: %v", err)
			}
		}
	}

	values, err := ds.GetLatestMetricValues(ctx,
		"pg_stat_statements.slow_query_count")
	if err != nil {
		t.Fatalf("GetLatestMetricValues failed: %v", err)
	}
	if len(values) != 1 {
		t.Fatalf("expected one row, got %d", len(values))
	}

	// Current (defective) behavior: all twelve idle queries are
	// counted, so the seeded "> 10" rule fires. The count SHOULD be 0
	// because nothing ran slowly in the window.
	const seededThreshold = 10.0
	if values[0].Value != 12 {
		t.Errorf("slow_query_count = %v, want 12 (every idle queryid counted)",
			values[0].Value)
	}
	if !(values[0].Value > seededThreshold) {
		t.Errorf("expected the seeded rule to fire on idle queries; value=%v",
			values[0].Value)
	}
}

// TestAuditC9SlowQueryCountDemo asserts the behavior the metric
// SHOULD have: a query that has not executed during the evaluation
// window must not be counted as slow. It fails against the current
// implementation, so it is skipped unless ALERTER_DEFECT_DEMO=1.
func TestAuditC9SlowQueryCountDemo(t *testing.T) {
	defectDemoEnabled(t)

	ds, pool, cleanup := newAuditDefectsDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertAuditConnection(t, pool, "audit-c9-demo")

	for _, offset := range []string{"12 minutes", "6 minutes", "1 minute"} {
		if _, err := pool.Exec(ctx, insertAuditStatStatementsSQL,
			connID, "appdb", int64(1), int64(5), 4200.0,
			nowMinus(t, pool, offset)); err != nil {
			t.Fatalf("failed to seed pg_stat_statements: %v", err)
		}
	}

	values, err := ds.GetLatestMetricValues(ctx,
		"pg_stat_statements.slow_query_count")
	if err != nil {
		// A correct implementation reports no slow queries, which the
		// registry surfaces as a "no data" error; that is acceptable.
		return
	}
	for _, v := range values {
		if v.Value != 0 {
			t.Fatalf("slow_query_count = %v for a query that did not run "+
				"during the window, want 0", v.Value)
		}
	}
}

// nowMinus returns the database's NOW() minus the given interval. The
// value is computed server-side so the seeded timestamps line up with
// the NOW() used inside the registry queries regardless of client
// clock skew or session time zone.
func nowMinus(t *testing.T, pool *pgxpool.Pool, interval string) (ts any) {
	t.Helper()
	if err := pool.QueryRow(context.Background(),
		`SELECT NOW() - $1::interval`, interval).Scan(&ts); err != nil {
		t.Fatalf("failed to compute NOW() - %s: %v", interval, err)
	}
	return ts
}
