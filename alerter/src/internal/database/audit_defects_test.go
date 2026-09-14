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
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

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
// Claims C2 to C5 were fixed in #406 (the archiver table, the constant
// wraparound value, the one-hour pg_settings expiry and the Windows-only
// CPU column). Their pinned tests were removed with that fix, as the
// header above prescribes, and the behavior they described is now
// covered against the corrected code in
// dead_alert_rules_integration_test.go, which tests each rule directly
// rather than pinning a defect.
//
// Claims C8 (cache_hit_ratio returned every delta row) and C9
// (slow_query_count used the lifetime mean) were fixed in #407. Their
// tests now assert the corrected behavior directly and keep their
// original names so the audit trail stays searchable.

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
    userid OID NOT NULL DEFAULT 10,
    dbid OID NOT NULL DEFAULT 16384,
    queryid BIGINT NOT NULL,
    toplevel BOOLEAN NOT NULL DEFAULT TRUE,
    calls BIGINT,
    total_exec_time DOUBLE PRECISION,
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

	insertAuditStatDatabaseSQL = `
        INSERT INTO metrics.pg_stat_database
            (connection_id, database_name, datname, blks_hit, blks_read,
             collected_at)
        VALUES ($1, $2, $3, $4, $5, $6)
    `

	insertAuditStatStatementsSQL = `
        INSERT INTO metrics.pg_stat_statements
            (connection_id, database_name, queryid, calls, mean_exec_time,
             total_exec_time, collected_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7)
    `

	insertAuditStatStatementsIdentitySQL = `
        INSERT INTO metrics.pg_stat_statements
            (connection_id, database_name, queryid, userid, calls,
             total_exec_time, collected_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7)
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
// entries and a different number of empty ones. The test pins the
// affected metrics by name so the discrepancy is explicit and any
// future drift identifies the metric that moved.
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

	// Pinned by name rather than by count. The audit's "12 of 34" is
	// wrong on both numbers; 34 is the count of seeded alert_rules
	// rows, not of registry metrics. Listing every affected metric
	// means that adding, removing, or backfilling one produces a
	// failure naming the metric that changed, rather than an opaque
	// count mismatch that says nothing about which entry moved.
	wantEmpty := []string{
		"age_percent",
		"pg_node_role.subscription_worker_down",
		"pg_replication_slots.inactive",
		"pg_replication_slots.retained_bytes",
		"pg_stat_activity.max_lock_wait_seconds",
		"pg_stat_all_tables.dead_tuple_percent",
		"pg_stat_archiver.failed_count_delta",
		"pg_stat_checkpointer.checkpoints_req_delta",
		"pg_stat_replication.lag_bytes",
		"pg_stat_replication.replay_lag_seconds",
		"pg_stat_replication.standby_disconnected",
		"pg_stat_statements.slow_query_count",
		"table_last_autovacuum_hours",
	}
	// Both slices are sorted, so a positional diff names the first
	// metric that differs.
	if len(empty) != len(wantEmpty) {
		t.Errorf("metrics with empty historicalSQL = %d, want %d\n"+
			" got: %v\nwant: %v", len(empty), len(wantEmpty), empty, wantEmpty)
	}
	for i := 0; i < len(empty) && i < len(wantEmpty); i++ {
		if empty[i] != wantEmpty[i] {
			t.Errorf("metric with empty historicalSQL [%d] = %q, want %q",
				i, empty[i], wantEmpty[i])
		}
	}

	// Every empty-historicalSQL metric must in fact fail
	// GetHistoricalMetricValues, because that is what pushes baseline
	// calculation onto the fallback path. The exact message matters:
	// it proves the registry guard rejected the call before any SQL
	// ran, so removing the guard or hitting an unrelated query error
	// both surface as failures here.
	ds, _, cleanup := newAuditDefectsDatastore(t)
	defer cleanup()

	// A named const rather than an inline literal so that building the
	// expected message below is a const-plus-variable expression, which
	// the Opengrep rule go_sql_rule-concat-sqli does not treat as a
	// taint source.
	const notImplementedPrefix = "historical data not implemented for metric "

	ctx := context.Background()
	for _, name := range empty {
		_, err := ds.GetHistoricalMetricValues(ctx, name, 7)
		if err == nil {
			t.Errorf("GetHistoricalMetricValues(%s) unexpectedly succeeded", name)
			continue
		}
		want := notImplementedPrefix + name
		if err.Error() != want {
			t.Errorf("GetHistoricalMetricValues(%s) error = %q, want %q",
				name, err.Error(), want)
		}
	}
}

// TestAuditC8CacheHitRatioReducesToLatestDelta covers audit claim C8,
// fixed in #407. pg_stat_database.cache_hit_ratio used to return every
// delta row in the 15-minute window with no ORDER BY, so the evaluator
// (which fires on any violating row) and the cleaner (which stops at the
// first row) could disagree on identical data, flapping the alert when
// the newest interval violated and latching it when the oldest did.
//
// The query now reduces to the newest qualifying delta per connection
// and database with DISTINCT ON ... ORDER BY collected_at DESC, so both
// sides read the same, most recent value. Each case seeds four samples
// (three delta intervals) with exactly one violating interval, and
// asserts that a single row comes back carrying the newest interval's
// value: healthy when the violation is in the oldest interval,
// violating when it is in the newest.
func TestAuditC8CacheHitRatioReducesToLatestDelta(t *testing.T) {
	ds, pool, cleanup := newAuditDefectsDatastore(t)
	defer cleanup()

	ctx := context.Background()
	const seededThreshold = 80.0

	// Every interval moves at least 10000 blocks so it clears the
	// query's minimum-activity filter.
	//
	// connName is a literal rather than a value derived from name for
	// the reason given on the C5 table above: concatenating it would
	// make it a taint source for the Opengrep rule
	// go_sql_rule-concat-sqli.
	cases := []struct {
		name     string
		connName string
		// hits and reads are cumulative counters per sample.
		hits  []int64
		reads []int64
		// wantViolates says whether the newest interval breaches the
		// threshold, and wantValue is its exact ratio.
		wantViolates bool
		wantValue    float64
	}{
		{
			name:         "violation in oldest interval reads healthy",
			connName:     "audit-c8-oldest",
			hits:         []int64{0, 10_000, 40_000, 70_000},
			reads:        []int64{0, 10_000, 10_000, 10_000},
			wantViolates: false,
			wantValue:    100,
		},
		{
			name:         "violation in newest interval reads violating",
			connName:     "audit-c8-newest",
			hits:         []int64{0, 30_000, 60_000, 60_000},
			reads:        []int64{0, 0, 0, 30_000},
			wantViolates: true,
			wantValue:    0,
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
			connID := insertAuditConnection(t, pool, tc.connName)

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
			if len(values) != 1 {
				t.Fatalf("expected one reduced row for a single "+
					"connection/database, got %d: %+v", len(values), values)
			}

			got := values[0]
			if got.ConnectionID != connID {
				t.Errorf("row connection_id = %d, want %d", got.ConnectionID, connID)
			}
			if got.DatabaseName == nil || *got.DatabaseName != "appdb" {
				t.Errorf("row database_name = %v, want \"appdb\"", got.DatabaseName)
			}
			if got.Value != tc.wantValue {
				t.Errorf("value = %.2f, want %.2f (the newest interval)",
					got.Value, tc.wantValue)
			}
			if violates := got.Value < seededThreshold; violates != tc.wantViolates {
				t.Errorf("violates = %v, want %v", violates, tc.wantViolates)
			}

			// The row must carry the newest interval's timestamp, which
			// is what makes the evaluator and the cleaner agree.
			newest, ok := nowMinus(t, pool, "1 minute").(time.Time)
			if !ok {
				t.Fatalf("nowMinus returned %T, want time.Time", nowMinus(t, pool, "1 minute"))
			}
			if got.CollectedAt.Sub(newest).Abs() > 5*time.Second {
				t.Errorf("collected_at = %s, want about %s",
					got.CollectedAt, newest)
			}
		})
	}

	// The reduction is DISTINCT ON ordered newest first; the sibling
	// delta metrics reduce with GROUP BY. Either shape is fine, what
	// matters is that none of the three returns unreduced rows.
	for _, name := range []string{
		"pg_stat_database.deadlocks_delta",
		"pg_stat_database.temp_files_delta",
	} {
		cfg := metricRegistry[name]
		if !strings.Contains(cfg.latestSQL, "GROUP BY") {
			t.Errorf("%s unexpectedly lacks a GROUP BY reduction", name)
		}
	}
	latest := metricRegistry["pg_stat_database.cache_hit_ratio"].latestSQL
	if !strings.Contains(latest, "DISTINCT ON (connection_id, database_name)") ||
		!strings.Contains(latest, "collected_at DESC") {
		t.Error("cache_hit_ratio latestSQL lacks the DISTINCT ON newest-first reduction")
	}
}

// insertAuditStatement seeds one metrics.pg_stat_statements row for the
// default statement identity (userid, dbid and toplevel take the
// fixture's defaults).
func insertAuditStatement(t *testing.T, pool *pgxpool.Pool, connID int,
	queryID, calls int64, meanMs, totalMs float64, offset string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), insertAuditStatStatementsSQL,
		connID, "appdb", queryID, calls, meanMs, totalMs,
		nowMinus(t, pool, offset)); err != nil {
		t.Fatalf("failed to seed pg_stat_statements: %v", err)
	}
}

// slowQueryCount runs the slow_query_count metric and returns the value
// for connID, failing if the connection is missing from the result.
func slowQueryCount(t *testing.T, ds *Datastore, connID int) float64 {
	t.Helper()
	values, err := ds.GetLatestMetricValues(context.Background(),
		"pg_stat_statements.slow_query_count")
	if err != nil {
		t.Fatalf("GetLatestMetricValues failed: %v", err)
	}
	for _, v := range values {
		if v.ConnectionID == connID {
			if v.DatabaseName == nil || *v.DatabaseName != "appdb" {
				t.Fatalf("row database_name = %v, want \"appdb\"", v.DatabaseName)
			}
			return v.Value
		}
	}
	t.Fatalf("no slow_query_count row for connection %d: %+v", connID, values)
	return 0
}

// TestAuditC9SlowQueryCountUsesIntervalMean covers audit claim C9, fixed
// in #407. slow_query_count used to count queryids whose lifetime
// mean_exec_time exceeded 1000 ms, so a query that ran slowly once kept
// the count elevated for ever. It now derives the mean over the most
// recent probe interval from delta(total_exec_time) / delta(calls), and
// reports 0 (rather than no row) for a database whose statements are all
// idle or fast.
func TestAuditC9SlowQueryCountUsesIntervalMean(t *testing.T) {
	cfg := metricRegistry["pg_stat_statements.slow_query_count"]
	if strings.Contains(cfg.latestSQL, "mean_exec_time") {
		t.Error("slow_query_count still reads the lifetime mean_exec_time column")
	}
	for _, want := range []string{"total_exec_time", "LAG(calls)", "COUNT(*) FILTER"} {
		if !strings.Contains(cfg.latestSQL, want) {
			t.Errorf("slow_query_count latestSQL lacks %q", want)
		}
	}

	ds, pool, cleanup := newAuditDefectsDatastore(t)
	defer cleanup()

	offsets := []string{"12 minutes", "6 minutes", "1 minute"}

	t.Run("idle queries with a slow lifetime mean report 0", func(t *testing.T) {
		connID := insertAuditConnection(t, pool, "audit-c9-idle")
		// Twelve queryids whose lifetime mean is above 1000 ms and
		// whose call counts never change: none ran in the window. The
		// seeded "> 10" rule must not fire, and the database must
		// still be present with a value of 0.
		for queryID := int64(1); queryID <= 12; queryID++ {
			for _, offset := range offsets {
				insertAuditStatement(t, pool, connID, queryID, 5, 4200, 21000, offset)
			}
		}
		if got := slowQueryCount(t, ds, connID); got != 0 {
			t.Errorf("slow_query_count = %v, want 0 for idle queries", got)
		}
	})

	t.Run("interval mean decides, not lifetime mean", func(t *testing.T) {
		connID := insertAuditConnection(t, pool, "audit-c9-interval")
		// queryid 1: lifetime mean 4200 ms, but the newest interval
		// added 10 calls in 1000 ms (100 ms each): fast, not counted.
		insertAuditStatement(t, pool, connID, 1, 5, 4200, 21000, "12 minutes")
		insertAuditStatement(t, pool, connID, 1, 5, 4200, 21000, "6 minutes")
		insertAuditStatement(t, pool, connID, 1, 15, 1466.7, 22000, "1 minute")
		// queryid 2: lifetime mean 50 ms, but the newest interval added
		// 2 calls in 5000 ms (2500 ms each): slow, counted.
		insertAuditStatement(t, pool, connID, 2, 1000, 50, 50000, "12 minutes")
		insertAuditStatement(t, pool, connID, 2, 1000, 50, 50000, "6 minutes")
		insertAuditStatement(t, pool, connID, 2, 1002, 54.9, 55000, "1 minute")
		// queryid 3: slow in the older interval (2 calls, 6000 ms) but
		// idle in the newest one; only the newest interval counts.
		insertAuditStatement(t, pool, connID, 3, 10, 100, 1000, "12 minutes")
		insertAuditStatement(t, pool, connID, 3, 12, 583.3, 7000, "6 minutes")
		insertAuditStatement(t, pool, connID, 3, 12, 583.3, 7000, "1 minute")
		if got := slowQueryCount(t, ds, connID); got != 1 {
			t.Errorf("slow_query_count = %v, want 1 (queryid 2 only)", got)
		}
	})

	t.Run("a stats reset is not a slow query", func(t *testing.T) {
		connID := insertAuditConnection(t, pool, "audit-c9-reset")
		// calls fell from 1000 to 3 between the two newest samples, so
		// the counters were reset; the 3 post-reset calls took 9000 ms
		// but the delta is meaningless and must not be counted.
		insertAuditStatement(t, pool, connID, 1, 1000, 50, 50000, "6 minutes")
		insertAuditStatement(t, pool, connID, 1, 3, 3000, 9000, "1 minute")
		if got := slowQueryCount(t, ds, connID); got != 0 {
			t.Errorf("slow_query_count = %v, want 0 after a stats reset", got)
		}
	})

	t.Run("a single sample in the window reports 0", func(t *testing.T) {
		connID := insertAuditConnection(t, pool, "audit-c9-single")
		insertAuditStatement(t, pool, connID, 1, 5, 4200, 21000, "1 minute")
		if got := slowQueryCount(t, ds, connID); got != 0 {
			t.Errorf("slow_query_count = %v, want 0 with no predecessor", got)
		}
	})

	t.Run("deltas are taken within each statement identity", func(t *testing.T) {
		connID := insertAuditConnection(t, pool, "audit-c9-identity")
		// The same queryid under two userids: identity A is reset
		// between samples (calls 1000 -> 2), identity B runs 4 slow
		// calls in 20000 ms. Differenced per identity, only B counts
		// and it counts once. Differenced across identities, the
		// interleaved rows would produce nonsense.
		for _, row := range []struct {
			userid int64
			calls  int64
			total  float64
			offset string
		}{
			{10, 1000, 50000, "6 minutes"},
			{10, 2, 6000, "1 minute"},
			{20, 100, 10000, "6 minutes"},
			{20, 104, 30000, "1 minute"},
		} {
			if _, err := pool.Exec(context.Background(),
				insertAuditStatStatementsIdentitySQL,
				connID, "appdb", int64(1), row.userid, row.calls, row.total,
				nowMinus(t, pool, row.offset)); err != nil {
				t.Fatalf("failed to seed pg_stat_statements: %v", err)
			}
		}
		if got := slowQueryCount(t, ds, connID); got != 1 {
			t.Errorf("slow_query_count = %v, want 1 (one queryid, one slow identity)", got)
		}
	})
}

// TestMetricRegistryLatestSQLFreshnessCutoff walks the registry and
// asserts that every latest query bounds collected_at with a NOW() -
// INTERVAL cutoff, or is on the allowlist below with a reason. Without a
// cutoff a metric keeps reporting the newest row the table still holds,
// so an alert raised before the collector stopped, or before the
// connection stopped being monitored, fires on days-old data until
// retention purges the partition; #407 found two slot metrics doing
// exactly that.
func TestMetricRegistryLatestSQLFreshnessCutoff(t *testing.T) {
	// Metrics whose latest query deliberately carries no cutoff.
	allowlist := map[string]string{
		// The pg_settings probe is change-tracked and writes nothing
		// while the settings hash is unchanged, so any max-age
		// predicate kills the metric within the hour (#406). Freshness
		// is not meaningful for a value that only changes on a
		// configuration reload.
		"pg_settings.max_connections": "change-tracked probe with no heartbeat",
	}
	cutoff := regexp.MustCompile(
		`collected_at > NOW\(\) - INTERVAL '\d+ (minute|minutes|hour|hours)'`)

	names := make([]string, 0, len(metricRegistry))
	for name := range metricRegistry {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		cfg := metricRegistry[name]
		if reason, ok := allowlist[name]; ok {
			if cutoff.MatchString(cfg.latestSQL) {
				t.Errorf("%s is allowlisted (%s) but its latestSQL now has a cutoff; remove it from the allowlist",
					name, reason)
			}
			continue
		}
		if !cutoff.MatchString(cfg.latestSQL) {
			t.Errorf("%s latestSQL has no collected_at freshness cutoff; add one or allowlist it with a reason",
				name)
		}
	}
}

// TestMetricClearsWhenAbsent pins the clear-when-absent classification
// for representative registry entries and for an unknown metric.
func TestMetricClearsWhenAbsent(t *testing.T) {
	ds := &Datastore{}
	cases := map[string]bool{
		// Emit a row only while the condition holds, or describe an
		// object that can be dropped.
		"pg_replication_slots.inactive":            true,
		"pg_replication_slots.max_retained_bytes":  true,
		"pg_stat_activity.blocked_count":           true,
		"pg_stat_replication.standby_disconnected": true,
		"spock_exception_log.recent_count":         true,
		// Emit a row for every healthy connection.
		"pg_sys_cpu_usage_info.processor_time_percent": false,
		"pg_stat_checkpointer.checkpoints_req_delta":   false,
		"pg_stat_database.cache_hit_ratio":             false,
		"pg_stat_statements.slow_query_count":          false,
		// Not in the registry at all.
		"probe_staleness_ratio": false,
	}
	for name, want := range cases {
		if got := ds.MetricClearsWhenAbsent(name); got != want {
			t.Errorf("MetricClearsWhenAbsent(%q) = %v, want %v", name, got, want)
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

// TestMetricRegistryProbeName walks the registry and asserts that every
// entry names a collector probe, and that the probe named is a metrics
// table the latest query actually reads. The alert cleaner refuses to
// treat an absent row as a recovery unless that probe is currently
// reporting, so an entry with a missing or wrong probe name either never
// clears or clears on the freshness of some other probe. See GitHub issue
// #407.
func TestMetricRegistryProbeName(t *testing.T) {
	names := make([]string, 0, len(metricRegistry))
	for name := range metricRegistry {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		cfg := metricRegistry[name]
		if cfg.probeName == "" {
			t.Errorf("%s names no collector probe; set probeName to the probe "+
				"that fills the table its latest query reads", name)
			continue
		}
		if !strings.Contains(cfg.latestSQL, "metrics."+cfg.probeName) {
			t.Errorf("%s names probe %q but its latest query does not read "+
				"metrics.%s", name, cfg.probeName, cfg.probeName)
		}
	}
}

// TestMetricProbeName pins the accessor the cleaner reads, including the
// zero value for a metric the registry does not know.
func TestMetricProbeName(t *testing.T) {
	ds := &Datastore{}
	cases := map[string]string{
		"pg_replication_slots.inactive_count": "pg_replication_slots",
		"pg_stat_activity.blocked_count":      "pg_stat_activity",
		"spock_resolutions.recent_count":      "spock_resolutions",
		"table_last_autovacuum_hours":         "pg_stat_all_tables",
		"pg_stat_archiver.failed_count_delta": "pg_stat_wal",
		"age_percent":                         "pg_database",
		"probe_staleness_ratio":               "",
	}
	for name, want := range cases {
		if got := ds.MetricProbeName(name); got != want {
			t.Errorf("MetricProbeName(%q) = %q, want %q", name, got, want)
		}
	}
}
