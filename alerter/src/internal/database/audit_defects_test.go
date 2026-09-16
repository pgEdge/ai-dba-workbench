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
	"strconv"
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

	connStr := requireLocalTestDSN(t, "the audit defect test")

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

// TestAuditC6BaselineOrderingCarriesNoPreference covers the query half
// of audit claim C6, fixed in #408. GetMetricBaselines used to be read
// through baselines[0], so its alphabetical ORDER BY on the TEXT column
// period_type ('all' < 'daily' < 'hourly') silently chose the global
// baseline every time. Selection now happens in the engine
// (selectBaseline), and the query's only obligation is to return every
// row for the (connection, metric, database) in a deterministic order.
func TestAuditC6BaselineOrderingCarriesNoPreference(t *testing.T) {
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
		"pg_stat_activity.count", nil)
	if err != nil {
		t.Fatalf("GetMetricBaselines failed: %v", err)
	}
	if len(baselines) != 3 {
		t.Fatalf("expected 3 baselines, got %d", len(baselines))
	}

	// Every period type comes back, so the caller can choose; the
	// order is deterministic (period_type, day_of_week, hour_of_day)
	// but is not a preference and nothing may treat it as one.
	got := make([]string, len(baselines))
	for i, b := range baselines {
		got[i] = b.PeriodType
	}
	want := []string{"all", "daily", "hourly"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("period_type order = %v, want %v", got, want)
		}
	}

	again, err := ds.GetMetricBaselines(ctx, connID,
		"pg_stat_activity.count", nil)
	if err != nil {
		t.Fatalf("GetMetricBaselines (second call) failed: %v", err)
	}
	for i := range again {
		if again[i].ID != baselines[i].ID {
			t.Fatalf("row order changed between calls: %d vs %d",
				again[i].ID, baselines[i].ID)
		}
	}
}

// TestAuditC10BaselineLookupScopedByDatabase covers the query half of
// audit claim C10, fixed in #408: GetMetricBaselines takes a database
// name and matches it NULL-aware, exactly as GetActiveAnomalyAlert
// does. A per-database metric's rows are therefore returned one
// database at a time, and connection-wide rows (database_name IS NULL)
// are only returned for a nil lookup.
func TestAuditC10BaselineLookupScopedByDatabase(t *testing.T) {
	ds, pool, cleanup := newAuditDefectsDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertAuditConnection(t, pool, "audit-c10-query")

	for i, dbName := range []string{"alpha", "beta"} {
		if _, err := pool.Exec(ctx, insertAuditBaselineSQL,
			connID, dbName, "pg_stat_database.deadlocks_delta", "all",
			nil, nil, float64(i+1), 0.5, 0.0, 2.0, int64(500), nil); err != nil {
			t.Fatalf("failed to insert %s baseline: %v", dbName, err)
		}
	}
	// A connection-wide row for the same metric, which a real run
	// never writes but which must not leak into a per-database lookup.
	if _, err := pool.Exec(ctx, insertAuditBaselineSQL,
		connID, nil, "pg_stat_database.deadlocks_delta", "all",
		nil, nil, 99.0, 0.5, 0.0, 2.0, int64(500), nil); err != nil {
		t.Fatalf("failed to insert NULL-database baseline: %v", err)
	}

	cases := []struct {
		name     string
		dbName   *string
		wantMean float64
		wantDB   *string
	}{
		{"alpha", ptr("alpha"), 1, ptr("alpha")},
		{"beta", ptr("beta"), 2, ptr("beta")},
		{"nil matches only NULL rows", nil, 99, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			baselines, err := ds.GetMetricBaselines(ctx, connID,
				"pg_stat_database.deadlocks_delta", tc.dbName)
			if err != nil {
				t.Fatalf("GetMetricBaselines failed: %v", err)
			}
			if len(baselines) != 1 {
				t.Fatalf("expected exactly one baseline, got %d", len(baselines))
			}
			b := baselines[0]
			if b.Mean != tc.wantMean {
				t.Errorf("mean = %v, want %v", b.Mean, tc.wantMean)
			}
			switch {
			case tc.wantDB == nil && b.DatabaseName != nil:
				t.Errorf("database_name = %q, want NULL", *b.DatabaseName)
			case tc.wantDB != nil && (b.DatabaseName == nil || *b.DatabaseName != *tc.wantDB):
				t.Errorf("database_name = %v, want %q", b.DatabaseName, *tc.wantDB)
			}
		})
	}

	// An unknown database returns nothing rather than another
	// database's rows.
	baselines, err := ds.GetMetricBaselines(ctx, connID,
		"pg_stat_database.deadlocks_delta", ptr("gamma"))
	if err != nil {
		t.Fatalf("GetMetricBaselines(gamma) failed: %v", err)
	}
	if len(baselines) != 0 {
		t.Errorf("expected no baselines for an unknown database, got %d", len(baselines))
	}
}

// ptr returns a pointer to its argument, for optional query parameters.
func ptr[T any](v T) *T {
	return &v
}

// TestAuditC7HistoricalSQLCoverage pins, by name, the registry metrics
// that have no historicalSQL and therefore cannot be baselined. Since
// #408 those metrics are excluded from baseline calculation and
// detection through SupportsBaselines rather than routed through a
// fallback that wrote permanently cold rows; giving them a historical
// query is tracked separately. Listing every affected metric means
// that adding, removing, or backfilling one produces a failure naming
// the metric that changed, rather than an opaque count mismatch.
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

	// SupportsBaselines must agree with the registry exactly: false
	// for every empty entry, true for every other entry, and false
	// for names that are not in the registry at all (metric_staleness
	// is evaluated by its own code path and must not be baselined).
	for _, name := range empty {
		if SupportsBaselines(name) {
			t.Errorf("SupportsBaselines(%s) = true, want false", name)
		}
	}
	supported := BaselineSupportedMetrics()
	if len(supported)+len(empty) != len(metricRegistry) {
		t.Errorf("supported (%d) + empty (%d) != registry (%d)",
			len(supported), len(empty), len(metricRegistry))
	}
	if !sort.StringsAreSorted(supported) {
		t.Errorf("BaselineSupportedMetrics is not sorted: %v", supported)
	}
	for _, name := range supported {
		if !SupportsBaselines(name) {
			t.Errorf("SupportsBaselines(%s) = false for a listed metric", name)
		}
		if strings.TrimSpace(metricRegistry[name].historicalSQL) == "" {
			t.Errorf("BaselineSupportedMetrics lists %s, which has no historicalSQL", name)
		}
	}
	for _, name := range []string{"metric_staleness", "probe_staleness_ratio", ""} {
		if SupportsBaselines(name) {
			t.Errorf("SupportsBaselines(%q) = true for a name outside the registry", name)
		}
	}

	// Every empty-historicalSQL metric must still fail
	// GetHistoricalMetricValues with the registry guard's message, so
	// a caller that skips the SupportsBaselines check gets an error
	// before any SQL runs.
	ds, _, cleanup := newAuditDefectsDatastore(t)
	defer cleanup()

	// A named const rather than an inline literal so that building the
	// expected message below is a const-plus-variable expression, which
	// the Opengrep rule go_sql_rule-concat-sqli does not treat as a
	// taint source.
	const notImplementedPrefix = "historical data not implemented for metric "

	ctx := context.Background()
	for _, name := range append(empty, "metric_staleness") {
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

// TestAuditC7DeleteBaselinesForUnsupportedMetrics verifies the sweep
// that removes the permanently cold rows the pre-#408 fallback wrote:
// rows for metrics outside BaselineSupportedMetrics go, rows for
// supported metrics stay, and the count of deleted rows is reported.
func TestAuditC7DeleteBaselinesForUnsupportedMetrics(t *testing.T) {
	ds, pool, cleanup := newAuditDefectsDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertAuditConnection(t, pool, "audit-c7-sweep")

	seed := []struct {
		metric string
		dbName any
	}{
		{"pg_replication_slots.inactive", nil},         // no historicalSQL
		{"pg_stat_statements.slow_query_count", "app"}, // no historicalSQL
		{"metric_staleness", nil},                      // not in the registry
		{"pg_stat_activity.count", nil},                // supported
		{"pg_stat_database.deadlocks_delta", "app"},    // supported
	}
	for _, r := range seed {
		if _, err := pool.Exec(ctx, insertAuditBaselineSQL,
			connID, r.dbName, r.metric, "all", nil, nil,
			1.0, 0.0, 1.0, 1.0, int64(1), nil); err != nil {
			t.Fatalf("failed to insert %s baseline: %v", r.metric, err)
		}
	}

	deleted, err := ds.DeleteBaselinesForUnsupportedMetrics(ctx)
	if err != nil {
		t.Fatalf("DeleteBaselinesForUnsupportedMetrics failed: %v", err)
	}
	if deleted != 3 {
		t.Errorf("deleted = %d, want 3", deleted)
	}

	rows, err := pool.Query(ctx,
		`SELECT metric_name FROM metric_baselines WHERE connection_id = $1 ORDER BY metric_name`,
		connID)
	if err != nil {
		t.Fatalf("failed to read remaining baselines: %v", err)
	}
	defer rows.Close()
	var remaining []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan failed: %v", err)
		}
		remaining = append(remaining, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("row iteration error: %v", err)
	}
	want := []string{"pg_stat_activity.count", "pg_stat_database.deadlocks_delta"}
	if len(remaining) != len(want) {
		t.Fatalf("remaining = %v, want %v", remaining, want)
	}
	for i := range want {
		if remaining[i] != want[i] {
			t.Errorf("remaining[%d] = %q, want %q", i, remaining[i], want[i])
		}
	}

	// A second sweep finds nothing to do.
	deleted, err = ds.DeleteBaselinesForUnsupportedMetrics(ctx)
	if err != nil {
		t.Fatalf("second DeleteBaselinesForUnsupportedMetrics failed: %v", err)
	}
	if deleted != 0 {
		t.Errorf("second sweep deleted = %d, want 0", deleted)
	}

	// A canceled context is reported rather than swallowed.
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ds.DeleteBaselinesForUnsupportedMetrics(canceled); err == nil {
		t.Error("expected an error from a canceled context")
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
	// LEAD, not LAG: the identity window is ordered descending so the
	// ROW_NUMBER that picks the newest sample can share it, which sorts
	// the window once rather than twice.
	for _, want := range []string{"total_exec_time", "LEAD(calls)", "COUNT(*) FILTER"} {
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

	for _, name := range registryNames() {
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

// TestMetricAbsenceWindow pins the accessor the cleaner gates on,
// including the zero value for a metric the registry does not know and
// for one that does not clear when absent.
func TestMetricAbsenceWindow(t *testing.T) {
	ds := &Datastore{}
	cases := map[string]time.Duration{
		"pg_replication_slots.inactive":         15 * time.Minute,
		"pg_node_role.subscription_worker_down": 15 * time.Minute,
		"table_last_autovacuum_hours":           15 * time.Minute,
		"pg_stat_activity.blocked_count":        5 * time.Minute,
		"spock_exception_log.recent_count":      5 * time.Minute,
		// Not clearWhenAbsent, so it declares no window.
		"pg_stat_database.cache_hit_ratio": 0,
		// Not in the registry at all.
		"probe_staleness_ratio": 0,
	}
	for name, want := range cases {
		if got := ds.MetricAbsenceWindow(name); got != want {
			t.Errorf("MetricAbsenceWindow(%q) = %s, want %s", name, got, want)
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
	for _, name := range registryNames() {
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

// seededProbeIntervals is the collection interval, in seconds, that the
// collector's schema seeds for each probe the registry names. It is the
// yardstick for the window audit below, and
// TestSeededProbeIntervalsMatchCollector checks it against the
// collector's own seed so the two cannot drift apart.
var seededProbeIntervals = map[string]int{
	"pg_node_role":         300,
	"pg_replication_slots": 300,
	"pg_stat_activity":     60,
	"pg_stat_all_tables":   300,
	"pg_stat_replication":  30,
	"spock_exception_log":  60,
	"spock_resolutions":    60,
}

// collectorProbeSeedPath is the collector source file that seeds
// probe_configs, read relative to this package's directory.
const collectorProbeSeedPath = "../../../../collector/src/database/schema.go"

// probeSeedRow matches one seeded probe_configs row in the collector's
// schema: (NULL, TRUE, 'probe_name', 'description', interval, retention).
var probeSeedRow = regexp.MustCompile(
	`\(NULL, TRUE, '([a-z0-9_]+)', '[^']*', (\d+), \d+\)`)

// cutoffLiteral matches a collected_at freshness cutoff in a registry
// query, capturing the interval's magnitude and unit.
var cutoffLiteral = regexp.MustCompile(
	`collected_at > NOW\(\) - INTERVAL '(\d+) (minute|minutes|hour|hours)'`)

// shortestCutoff returns the shortest collected_at cutoff in a query,
// which is the bound that decides whether its result is empty, and
// reports whether the query carries one at all.
func shortestCutoff(sql string) (time.Duration, bool) {
	var shortest time.Duration
	for _, m := range cutoffLiteral.FindAllStringSubmatch(sql, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		d := time.Duration(n) * time.Minute
		if strings.HasPrefix(m[2], "hour") {
			d = time.Duration(n) * time.Hour
		}
		if shortest == 0 || d < shortest {
			shortest = d
		}
	}
	return shortest, shortest > 0
}

// TestMetricRegistryAbsenceWindowMatchesSQL asserts that every
// clearWhenAbsent entry declares an absenceWindow, that the declared
// value equals the shortest collected_at cutoff its latest query
// actually uses, and that entries which do not clear on absence declare
// no window. The cleaner reads the typed field rather than the SQL, so a
// value that drifts from the literal would gate the clear on a window
// the query does not use. See GitHub issue #407.
func TestMetricRegistryAbsenceWindowMatchesSQL(t *testing.T) {
	for _, name := range registryNames() {
		cfg := metricRegistry[name]
		if !cfg.clearWhenAbsent {
			if cfg.absenceWindow != 0 {
				t.Errorf("%s does not clear when absent but declares absenceWindow %s; "+
					"the field is only meaningful for clearWhenAbsent entries",
					name, cfg.absenceWindow)
			}
			continue
		}
		want, ok := shortestCutoff(cfg.latestSQL)
		if !ok {
			t.Errorf("%s clears when absent but its latest query has no collected_at "+
				"cutoff, so the cleaner has no window to gate the clear on", name)
			continue
		}
		if cfg.absenceWindow != want {
			t.Errorf("%s declares absenceWindow %s but its latest query reads back %s; "+
				"the two must agree", name, cfg.absenceWindow, want)
		}
	}
}

// TestMetricRegistryAbsenceWindowCoversProbeInterval asserts that every
// clearWhenAbsent window spans at least three of its probe's seeded
// collection intervals. A window of one interval holds a single sample,
// so one collection landing later than the probe's own runtime empties
// the query, the cleaner reads that as a recovery, and a critical alert
// clears and re-fires on the next sample. pg_node_role's five minute
// window over a 300 second probe was exactly that defect. See GitHub
// issue #407.
func TestMetricRegistryAbsenceWindowCoversProbeInterval(t *testing.T) {
	const minIntervals = 3

	for _, name := range registryNames() {
		cfg := metricRegistry[name]
		if !cfg.clearWhenAbsent {
			continue
		}
		seconds, ok := seededProbeIntervals[cfg.probeName]
		if !ok {
			t.Errorf("%s names probe %q, whose seeded collection interval is not in "+
				"seededProbeIntervals; add it so the window can be audited",
				name, cfg.probeName)
			continue
		}
		floor := minIntervals * time.Duration(seconds) * time.Second
		if cfg.absenceWindow < floor {
			t.Errorf("%s has a %s window over a %ds %s probe (%.1f intervals); "+
				"widen it to at least %s, or %d intervals",
				name, cfg.absenceWindow, seconds, cfg.probeName,
				cfg.absenceWindow.Seconds()/float64(seconds), floor, minIntervals)
		}
	}
}

// TestSeededProbeIntervalsMatchCollector reads the collector's
// probe_configs seed and checks every interval this package audits
// against it, so a change to a probe's interval in the collector shows
// up here rather than silently invalidating the window audit. The
// collector source is always checked out beside the alerter, so failing
// to read it is a failure rather than a skip: a skip would turn green
// for ever the moment the file moved and leave seededProbeIntervals
// checked against nothing.
func TestSeededProbeIntervalsMatchCollector(t *testing.T) {
	source, err := os.ReadFile(collectorProbeSeedPath)
	if err != nil {
		t.Fatalf("collector schema not readable beside the alerter: %v", err)
	}

	seeded := make(map[string]int)
	for _, m := range probeSeedRow.FindAllStringSubmatch(string(source), -1) {
		seconds, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		seeded[m[1]] = seconds
	}
	if len(seeded) == 0 {
		t.Fatalf("no probe_configs seed rows found in %s; the seed's shape has "+
			"changed and probeSeedRow needs updating", collectorProbeSeedPath)
	}

	for probe, want := range seededProbeIntervals {
		got, ok := seeded[probe]
		if !ok {
			t.Errorf("probe %q is not seeded by the collector any more; update "+
				"seededProbeIntervals and the registry entries naming it", probe)
			continue
		}
		if got != want {
			t.Errorf("probe %q is seeded at %ds, but seededProbeIntervals says %ds; "+
				"update the map and re-check every window that depends on it",
				probe, got, want)
		}
	}
}

// registryNames returns the registry's metric names in sorted order, so
// the audits walk it deterministically.
func registryNames() []string {
	names := make([]string, 0, len(metricRegistry))
	for name := range metricRegistry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
