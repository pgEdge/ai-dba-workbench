/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/metrics"
)

// databaseSummariesTestSchema mirrors the minimum columns the five
// database-summaries query helpers read. It lives in the metrics schema
// because the queries fully qualify their table names with metrics.*.
const databaseSummariesTestSchema = `
CREATE SCHEMA IF NOT EXISTS metrics;
DROP TABLE IF EXISTS metrics.pg_database CASCADE;
DROP TABLE IF EXISTS metrics.pg_stat_database CASCADE;
DROP TABLE IF EXISTS metrics.pg_stat_all_tables CASCADE;

CREATE TABLE metrics.pg_database (
    connection_id        integer     NOT NULL,
    collected_at         timestamptz NOT NULL,
    datname              text        NOT NULL,
    datistemplate        boolean     NOT NULL DEFAULT false,
    database_size_bytes  bigint
);

CREATE TABLE metrics.pg_stat_database (
    connection_id  integer     NOT NULL,
    collected_at   timestamptz NOT NULL,
    datname        text,
    numbackends    integer     NOT NULL DEFAULT 0,
    blks_hit       bigint      NOT NULL DEFAULT 0,
    blks_read      bigint      NOT NULL DEFAULT 0,
    xact_commit    bigint      NOT NULL DEFAULT 0,
    xact_rollback  bigint      NOT NULL DEFAULT 0,
    stats_reset    timestamptz
);

CREATE TABLE metrics.pg_stat_all_tables (
    connection_id  integer     NOT NULL,
    collected_at   timestamptz NOT NULL,
    database_name  text,
    n_live_tup     bigint      NOT NULL DEFAULT 0,
    n_dead_tup     bigint      NOT NULL DEFAULT 0
);
`

const databaseSummariesTestSchemaTeardown = `
DROP TABLE IF EXISTS metrics.pg_database CASCADE;
DROP TABLE IF EXISTS metrics.pg_stat_database CASCADE;
DROP TABLE IF EXISTS metrics.pg_stat_all_tables CASCADE;
`

// newDatabaseSummariesTestHandler wires a PerfSummaryHandler to the
// TEST_AI_WORKBENCH_SERVER Postgres instance and installs the trimmed
// metrics schema above. The test is skipped when the environment is not
// configured to run database-backed tests.
func newDatabaseSummariesTestHandler(
	t *testing.T,
) (*PerfSummaryHandler, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping issue #362 test")
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

	if _, err := pool.Exec(ctx, databaseSummariesTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create database summaries test schema: %v", err)
	}

	ds := database.NewTestDatastore(pool)
	handler := NewPerfSummaryHandler(ds, nil)
	cleanup := func() {
		_, _ = pool.Exec(context.Background(),
			databaseSummariesTestSchemaTeardown)
		pool.Close()
	}
	return handler, pool, cleanup
}

// buildDatabaseSummaries runs the five query helpers through
// collectDatabaseSummaries, exactly as handleDatabaseSummaries does, and
// returns the resulting summaries keyed by database name. It isolates the
// aggregation logic under test from the HTTP/RBAC plumbing in the handler,
// and fails the test if any sub-query reports an error.
func buildDatabaseSummaries(
	t *testing.T,
	h *PerfSummaryHandler,
	pool *pgxpool.Pool,
	connID int,
	startTime, endTime time.Time,
	bucketInterval string,
) map[string]DatabaseSummary {
	t.Helper()

	ctx := context.Background()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after read is a no-op

	dbMap := make(map[string]*DatabaseSummary)
	window := metrics.TimeWindow{Start: startTime, End: endTime}
	if err := h.collectDatabaseSummaries(ctx, tx, connID, window,
		bucketInterval, dbMap); err != nil {
		t.Fatalf("collectDatabaseSummaries failed: %v", err)
	}

	out := make(map[string]DatabaseSummary, len(dbMap))
	for name, db := range dbMap {
		out[name] = *db
	}
	return out
}

// seedDatabaseSummariesFixture inserts a "keep" database that is present
// in every metrics table's latest snapshot, plus a "ghost" database that
// only has historical pg_stat_database rows inside the query window but is
// absent from the latest pg_database / pg_stat_database snapshot. This is
// the exact shape of issue #362: a recently dropped database whose
// historical cache-hit samples would otherwise resurrect a ghost card.
func seedDatabaseSummariesFixture(
	t *testing.T,
	pool *pgxpool.Pool,
	connID int,
	latest, prev, historical time.Time,
) {
	t.Helper()
	ctx := context.Background()

	exec := func(sql string, args ...any) {
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed exec failed: %v\nSQL: %s", err, sql)
		}
	}

	// pg_database: only "keep" exists in the latest snapshot. A template
	// database is present too and must be filtered out by the WHERE
	// datistemplate = false clause. "ghost" is deliberately absent.
	exec(`INSERT INTO metrics.pg_database
        (connection_id, collected_at, datname, datistemplate, database_size_bytes)
        VALUES
        ($1, $2, 'keep', false, 1048576),
        ($1, $2, 'template0', true, 8388608)`, connID, latest)

	// pg_stat_database latest snapshot: "keep" only.
	exec(`INSERT INTO metrics.pg_stat_database
        (connection_id, collected_at, datname, numbackends, blks_hit,
         blks_read, xact_commit, xact_rollback)
        VALUES ($1, $2, 'keep', 7, 900, 100, 5000, 50)`, connID, latest)

	// pg_stat_database previous snapshot: needed for the transaction-rate
	// delta of "keep" and to give "ghost" a second historical sample.
	exec(`INSERT INTO metrics.pg_stat_database
        (connection_id, collected_at, datname, numbackends, blks_hit,
         blks_read, xact_commit, xact_rollback)
        VALUES
        ($1, $2, 'keep', 6, 400, 60, 4000, 40),
        ($1, $2, 'ghost', 3, 300, 30, 2000, 20)`, connID, prev)

	// pg_stat_database older historical rows for "ghost" only. These are
	// inside the query window, so query 5 (cache-hit time series) would
	// find them and, before the fix, resurrect a ghost card.
	exec(`INSERT INTO metrics.pg_stat_database
        (connection_id, collected_at, datname, numbackends, blks_hit,
         blks_read, xact_commit, xact_rollback)
        VALUES ($1, $2, 'ghost', 2, 200, 20, 1000, 10)`, connID, historical)

	// pg_stat_all_tables latest snapshot: "keep" only, plus a stale
	// "ghost" row at an older collected_at that the latest-snapshot filter
	// must exclude.
	exec(`INSERT INTO metrics.pg_stat_all_tables
        (connection_id, collected_at, database_name, n_live_tup, n_dead_tup)
        VALUES ($1, $2, 'keep', 800, 200)`, connID, latest)
	exec(`INSERT INTO metrics.pg_stat_all_tables
        (connection_id, collected_at, database_name, n_live_tup, n_dead_tup)
        VALUES ($1, $2, 'ghost', 500, 500)`, connID, historical)
}

// TestDatabaseSummaries_Issue362_DropsGhostDatabases verifies that a
// database missing from the latest pg_database snapshot is excluded from
// the result even though it still has historical pg_stat_database samples
// inside the requested time range, while a currently-existing database is
// returned with a fully populated summary.
func TestDatabaseSummaries_Issue362_DropsGhostDatabases(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	const connID = 42
	now := time.Now().UTC()
	latest := now.Add(-1 * time.Minute)
	prev := now.Add(-2 * time.Minute)
	historical := now.Add(-90 * time.Minute)
	startTime := now.Add(-24 * time.Hour)

	seedDatabaseSummariesFixture(t, pool, connID, latest, prev, historical)

	summaries := buildDatabaseSummaries(t, h, pool, connID, startTime, now,
		"60 seconds")

	if _, ok := summaries["ghost"]; ok {
		t.Fatalf("dropped database 'ghost' must not appear in summaries; "+
			"got: %#v", summaries)
	}
	if _, ok := summaries["template0"]; ok {
		t.Fatalf("template database 'template0' must be excluded")
	}

	keep, ok := summaries["keep"]
	if !ok {
		t.Fatalf("existing database 'keep' missing from summaries: %#v",
			summaries)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected exactly 1 summary, got %d: %#v",
			len(summaries), summaries)
	}

	// Full, non-regressed summary for the surviving database: every query
	// helper must have enriched it.
	if keep.SizeBytes != 1048576 {
		t.Errorf("SizeBytes = %d, want 1048576", keep.SizeBytes)
	}
	if keep.SizePretty == "" {
		t.Errorf("SizePretty should be populated")
	}
	if keep.ActiveConnections != 7 {
		t.Errorf("ActiveConnections = %d, want 7", keep.ActiveConnections)
	}
	// The latest interval moved blks_hit 400 -> 900 and blks_read
	// 60 -> 100, so the current (per-interval) ratio is 500/540 = 92.59%.
	// The lifetime ratio of the latest snapshot (900/1000 = 90%) must
	// not be reported (issue #401).
	if keep.CacheHitRatio.Current == nil ||
		*keep.CacheHitRatio.Current != 92.59 {
		t.Errorf("CacheHitRatio.Current = %v, want 92.59",
			fmtFloatPtr(keep.CacheHitRatio.Current))
	}
	if len(keep.CacheHitRatio.TimeSeries) == 0 {
		t.Errorf("CacheHitRatio.TimeSeries should be populated")
	}
	// n_live_tup=800, n_dead_tup=200 -> 20% dead tuple ratio.
	if keep.DeadTupleRatio != 20.0 {
		t.Errorf("DeadTupleRatio = %v, want 20.0", keep.DeadTupleRatio)
	}
	if keep.TransactionRate <= 0 {
		t.Errorf("TransactionRate = %v, want > 0", keep.TransactionRate)
	}
}

// TestDatabaseSummaries_Issue362_EnrichmentSkipsUnknownDatabase drives each
// enrichment helper directly to prove that none of queries 2-5 create a new
// dbMap entry for a database absent from the size-derived base set.
func TestDatabaseSummaries_Issue362_EnrichmentSkipsUnknownDatabase(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	const connID = 43
	now := time.Now().UTC()
	latest := now.Add(-1 * time.Minute)
	prev := now.Add(-2 * time.Minute)
	historical := now.Add(-90 * time.Minute)
	startTime := now.Add(-24 * time.Hour)

	seedDatabaseSummariesFixture(t, pool, connID, latest, prev, historical)

	ctx := context.Background()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after read is a no-op

	dbMap := make(map[string]*DatabaseSummary)
	enrich := func() {
		t.Helper()
		for name, err := range map[string]error{
			"stats":      h.queryDatabaseStats(ctx, tx, connID, startTime, now, dbMap),
			"dead tuple": h.queryDeadTupleRatios(ctx, tx, connID, startTime, now, dbMap),
			"tx rate":    h.queryTransactionRates(ctx, tx, connID, startTime, now, dbMap),
			"cache hit": h.queryDatabaseCacheHitTimeSeries(ctx, tx, connID,
				startTime, now, "60 seconds", dbMap),
		} {
			if err != nil {
				t.Fatalf("%s helper failed: %v", name, err)
			}
		}
	}

	// Start with an EMPTY base set (skip queryDatabaseSizes). Every
	// enrichment helper must leave the map empty because none may create
	// entries.
	enrich()

	if len(dbMap) != 0 {
		t.Fatalf("enrichment helpers must not create entries; got: %#v",
			dbMap)
	}

	// Now seed only "keep" as the base set and confirm the enrichment
	// helpers populate it while still ignoring "ghost".
	dbMap["keep"] = &DatabaseSummary{
		DatabaseName:  "keep",
		CacheHitRatio: CacheHitRatioData{TimeSeries: []CacheHitRatioPoint{}},
	}
	enrich()

	if _, ok := dbMap["ghost"]; ok {
		t.Fatalf("'ghost' must not be created by enrichment helpers")
	}
	keep, ok := dbMap["keep"]
	if !ok {
		t.Fatalf("'keep' entry disappeared")
	}
	if keep.ActiveConnections != 7 {
		t.Errorf("ActiveConnections = %d, want 7", keep.ActiveConnections)
	}
	if keep.DeadTupleRatio != 20.0 {
		t.Errorf("DeadTupleRatio = %v, want 20.0", keep.DeadTupleRatio)
	}
	if len(keep.CacheHitRatio.TimeSeries) == 0 {
		t.Errorf("CacheHitRatio.TimeSeries should be populated for 'keep'")
	}
}

// TestDatabaseSummaries_QueryError verifies that every query helper
// reports a failing query (here, missing metrics tables) as an error that
// isUndefinedTableError detects, rather than logging it and returning
// as though the query had found nothing, and that none of them creates a
// spurious entry on the way (issue #519).
func TestDatabaseSummaries_QueryError(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	ctx := context.Background()
	// Drop the metrics tables so every helper's query fails.
	if _, err := pool.Exec(ctx,
		databaseSummariesTestSchemaTeardown); err != nil {
		t.Fatalf("teardown for query-error test failed: %v", err)
	}

	dbMap := make(map[string]*DatabaseSummary)
	errStart := time.Now().UTC().Add(-time.Hour)
	errEnd := time.Now().UTC()
	helpers := []struct {
		name string
		fn   func(pgx.Tx) error
	}{
		{"sizes", func(tx pgx.Tx) error {
			return h.queryDatabaseSizes(ctx, tx, 1, errStart, errEnd, dbMap)
		}},
		{"stats", func(tx pgx.Tx) error {
			return h.queryDatabaseStats(ctx, tx, 1, errStart, errEnd, dbMap)
		}},
		{"dead tuples", func(tx pgx.Tx) error {
			return h.queryDeadTupleRatios(ctx, tx, 1, errStart, errEnd, dbMap)
		}},
		{"transaction rates", func(tx pgx.Tx) error {
			return h.queryTransactionRates(ctx, tx, 1, errStart, errEnd, dbMap)
		}},
		{"cache hit", func(tx pgx.Tx) error {
			return h.queryDatabaseCacheHitTimeSeries(ctx, tx, 1,
				errStart, errEnd, "60 seconds", dbMap)
		}},
	}
	for _, helper := range helpers {
		// Each query gets its own transaction, rolled back immediately
		// after use, since a failed query aborts the transaction and
		// would otherwise leak a checked-out pool connection that
		// pool.Close() waits on forever in cleanup().
		itemTx := mustTx(t, pool)
		err := helper.fn(itemTx)
		_ = itemTx.Rollback(ctx)
		if !isUndefinedTableError(err) {
			t.Errorf("%s: err = %v, want an undefined_table error",
				helper.name, err)
		}
	}

	if len(dbMap) != 0 {
		t.Fatalf("query errors must not create entries; got: %#v", dbMap)
	}
}

// mustTx opens a fresh read-only transaction, failing the test on error. A
// fresh transaction is needed per query because a failed query aborts the
// current transaction.
func mustTx(t *testing.T, pool *pgxpool.Pool) pgx.Tx {
	t.Helper()
	tx, err := pool.BeginTx(context.Background(),
		pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}
	return tx
}

// TestDatabaseSummaries_ScanError verifies that a row carrying a NULL in a
// column the collector may leave NULL is skipped, without failing the
// sub-query and without creating a dbMap entry.
func TestDatabaseSummaries_ScanError(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	ctx := context.Background()
	const connID = 44
	latest := time.Now().UTC().Add(-time.Minute)

	// NULL size -> queryDatabaseSizes scan into int64 fails.
	if _, err := pool.Exec(ctx, `INSERT INTO metrics.pg_database
        (connection_id, collected_at, datname, datistemplate, database_size_bytes)
        VALUES ($1, $2, 'bad', false, NULL)`, connID, latest); err != nil {
		t.Fatalf("insert pg_database: %v", err)
	}
	// NULL datname -> queryDatabaseStats and queryDatabaseCacheHitTimeSeries
	// scan into string fails.
	if _, err := pool.Exec(ctx, `INSERT INTO metrics.pg_stat_database
        (connection_id, collected_at, datname, numbackends, blks_hit,
         blks_read, xact_commit, xact_rollback)
        VALUES ($1, $2, NULL, 1, 10, 1, 1, 0)`, connID, latest); err != nil {
		t.Fatalf("insert pg_stat_database: %v", err)
	}
	// NULL database_name -> queryDeadTupleRatios scan into string fails.
	if _, err := pool.Exec(ctx, `INSERT INTO metrics.pg_stat_all_tables
        (connection_id, collected_at, database_name, n_live_tup, n_dead_tup)
        VALUES ($1, $2, NULL, 5, 5)`, connID, latest); err != nil {
		t.Fatalf("insert pg_stat_all_tables: %v", err)
	}

	startTime := time.Now().UTC().Add(-time.Hour)
	summaries := buildDatabaseSummaries(t, h, pool, connID, startTime,
		time.Now().UTC(), "60 seconds")

	if len(summaries) != 0 {
		t.Fatalf("rows that fail to scan must not create entries; got: %#v",
			summaries)
	}
}

// doDatabaseSummariesRequest drives handleDatabaseSummaries over the full
// HTTP path so that parameter parsing, the time-window resolver and the
// response encoding are all exercised.
func doDatabaseSummariesRequest(
	h *PerfSummaryHandler,
	query string,
) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/database-summaries?"+query, nil)
	rec := httptest.NewRecorder()
	h.handleDatabaseSummaries(rec, req)
	return rec
}

// decodeDatabaseSummaries asserts a 200 response and returns the summaries
// keyed by database name.
func decodeDatabaseSummaries(
	t *testing.T,
	rec *httptest.ResponseRecorder,
) map[string]DatabaseSummary {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code,
			rec.Body.String())
	}
	var resp DatabaseSummaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v; body: %s", err,
			rec.Body.String())
	}
	out := make(map[string]DatabaseSummary, len(resp.Databases))
	for _, db := range resp.Databases {
		out[db.DatabaseName] = db
	}
	return out
}

// seedDatabaseSummariesWindowFixture inserts two eras of samples for a
// single database: a historical pair around ten hours ago and a recent pair
// within the last few minutes. Every metric differs between the two eras, so
// a window that selects one era cannot accidentally report the other's
// figures.
func seedDatabaseSummariesWindowFixture(
	t *testing.T,
	pool *pgxpool.Pool,
	connID int,
	histPrev, histLatest, recentPrev, recentLatest time.Time,
) {
	t.Helper()
	ctx := context.Background()

	exec := func(sql string, args ...any) {
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed exec failed: %v\nSQL: %s", err, sql)
		}
	}

	exec(`INSERT INTO metrics.pg_database
        (connection_id, collected_at, datname, datistemplate,
         database_size_bytes)
        VALUES ($1, $2, 'app', false, 1000000),
               ($1, $3, 'app', false, 2000000)`,
		connID, histLatest, recentLatest)

	exec(`INSERT INTO metrics.pg_stat_database
        (connection_id, collected_at, datname, numbackends, blks_hit,
         blks_read, xact_commit, xact_rollback)
        VALUES
        ($1, $2, 'app', 3, 100, 20, 1000, 10),
        ($1, $3, 'app', 4, 400, 50, 1300, 13),
        ($1, $4, 'app', 9, 5000, 500, 5000, 50),
        ($1, $5, 'app', 11, 5600, 560, 5600, 56)`,
		connID, histPrev, histLatest, recentPrev, recentLatest)

	exec(`INSERT INTO metrics.pg_stat_all_tables
        (connection_id, collected_at, database_name, n_live_tup, n_dead_tup)
        VALUES ($1, $2, 'app', 900, 100),
               ($1, $3, 'app', 500, 500)`,
		connID, histLatest, recentLatest)
}

// TestDatabaseSummaries_CustomWindowSelectsHistoricalSamples verifies that a
// custom window bounds every sub-query, not only the cache-hit time series:
// a window over the historical era must report that era's size, connection
// count, dead tuple ratio and cache hit ratio, and the default preset must
// report the recent era's instead. Without the window bound on the
// MAX(collected_at) sub-queries, a historical window would return today's
// sizes alongside a historical chart.
func TestDatabaseSummaries_CustomWindowSelectsHistoricalSamples(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	const connID = 601
	now := time.Now().UTC()
	histPrev := now.Add(-10*time.Hour - 5*time.Minute)
	histLatest := now.Add(-10 * time.Hour)
	recentPrev := now.Add(-6 * time.Minute)
	recentLatest := now.Add(-1 * time.Minute)
	seedDatabaseSummariesWindowFixture(t, pool, connID, histPrev, histLatest,
		recentPrev, recentLatest)

	iso := func(ts time.Time) string { return ts.Format(time.RFC3339) }
	historical := decodeDatabaseSummaries(t, doDatabaseSummariesRequest(h,
		fmt.Sprintf(
			"connection_id=%d&time_range=custom&time_start=%s&time_end=%s",
			connID, iso(now.Add(-11*time.Hour)), iso(now.Add(-9*time.Hour)))))

	app, ok := historical["app"]
	if !ok {
		t.Fatalf("expected 'app' in the historical window; got %#v",
			historical)
	}
	if app.SizeBytes != 1000000 {
		t.Errorf("SizeBytes = %d, want 1000000 (the historical snapshot)",
			app.SizeBytes)
	}
	if app.ActiveConnections != 4 {
		t.Errorf("ActiveConnections = %d, want 4", app.ActiveConnections)
	}
	if app.DeadTupleRatio != 10.0 {
		t.Errorf("DeadTupleRatio = %v, want 10.0", app.DeadTupleRatio)
	}
	// blks_hit moved 100 -> 400 and blks_read 20 -> 50 inside the window,
	// so the ratio is 300/330 = 90.91%.
	if app.CacheHitRatio.Current == nil ||
		*app.CacheHitRatio.Current != 90.91 {
		t.Errorf("CacheHitRatio.Current = %v, want 90.91",
			fmtFloatPtr(app.CacheHitRatio.Current))
	}
	// xact_commit moved 1000 -> 1300 over 300 seconds, so 1 txn/sec.
	if app.TransactionRate != 1.0 {
		t.Errorf("TransactionRate = %v, want 1.0", app.TransactionRate)
	}

	// The default preset covers the recent era and must report it.
	recent := decodeDatabaseSummaries(t, doDatabaseSummariesRequest(h,
		fmt.Sprintf("connection_id=%d", connID)))
	app, ok = recent["app"]
	if !ok {
		t.Fatalf("expected 'app' in the default window; got %#v", recent)
	}
	if app.SizeBytes != 2000000 {
		t.Errorf("SizeBytes = %d, want 2000000 (the recent snapshot)",
			app.SizeBytes)
	}
	if app.ActiveConnections != 11 {
		t.Errorf("ActiveConnections = %d, want 11", app.ActiveConnections)
	}
	if app.DeadTupleRatio != 50.0 {
		t.Errorf("DeadTupleRatio = %v, want 50.0", app.DeadTupleRatio)
	}
}

// TestDatabaseSummaries_PresetWindowSelectsRecentSamples verifies that an
// explicit preset resolves through the same resolver and reports the recent
// era, and that a preset too short to reach the historical era excludes it.
func TestDatabaseSummaries_PresetWindowSelectsRecentSamples(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	const connID = 602
	now := time.Now().UTC()
	seedDatabaseSummariesWindowFixture(t, pool, connID,
		now.Add(-10*time.Hour-5*time.Minute), now.Add(-10*time.Hour),
		now.Add(-6*time.Minute), now.Add(-1*time.Minute))

	for _, preset := range []string{"1h", "6h", "24h", "7d", "30d"} {
		t.Run(preset, func(t *testing.T) {
			summaries := decodeDatabaseSummaries(t,
				doDatabaseSummariesRequest(h, fmt.Sprintf(
					"connection_id=%d&time_range=%s", connID, preset)))
			app, ok := summaries["app"]
			if !ok {
				t.Fatalf("expected 'app' for time_range=%s; got %#v",
					preset, summaries)
			}
			if app.SizeBytes != 2000000 {
				t.Errorf("SizeBytes = %d, want 2000000", app.SizeBytes)
			}
			if app.ActiveConnections != 11 {
				t.Errorf("ActiveConnections = %d, want 11",
					app.ActiveConnections)
			}
		})
	}
}

// TestDatabaseSummaries_CustomWindowFutureEndClamped verifies that a custom
// window whose end lies in the future is clamped to now rather than
// rejected, so the newest snapshot is still reported.
func TestDatabaseSummaries_CustomWindowFutureEndClamped(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	const connID = 603
	now := time.Now().UTC()
	seedDatabaseSummariesWindowFixture(t, pool, connID,
		now.Add(-10*time.Hour-5*time.Minute), now.Add(-10*time.Hour),
		now.Add(-6*time.Minute), now.Add(-1*time.Minute))

	summaries := decodeDatabaseSummaries(t, doDatabaseSummariesRequest(h,
		fmt.Sprintf(
			"connection_id=%d&time_range=custom&time_start=%s&time_end=%s",
			connID, now.Add(-2*time.Hour).Format(time.RFC3339),
			now.Add(2*time.Hour).Format(time.RFC3339))))

	app, ok := summaries["app"]
	if !ok {
		t.Fatalf("a clamped window must still find the snapshot; got %#v",
			summaries)
	}
	if app.SizeBytes != 2000000 {
		t.Errorf("SizeBytes = %d, want 2000000", app.SizeBytes)
	}
}

// TestDatabaseSummaries_InvalidTimeRange verifies the 400 response for an
// unsupported preset, which carries ResolveTimeWindow's own wording so that
// it matches /metrics/query.
func TestDatabaseSummaries_InvalidTimeRange(t *testing.T) {
	h, _, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	rec := doDatabaseSummariesRequest(h, "connection_id=604&time_range=90m")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code,
			rec.Body.String())
	}
	want := `invalid time range "90m": must be one of 1h, 6h, 24h, 7d, 30d, custom`
	if got := errorMessageFromBody(t, rec); got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
}

// TestDatabaseSummaries_CustomWindowRejections verifies that every rejection
// ResolveTimeWindow can raise surfaces as a 400 carrying the resolver's own
// message.
func TestDatabaseSummaries_CustomWindowRejections(t *testing.T) {
	h, _, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	now := time.Now().UTC()
	iso := func(ts time.Time) string { return ts.Format(time.RFC3339) }

	tests := []struct {
		name    string
		query   string
		wantErr string
	}{
		{
			name:    "missing both timestamps",
			query:   "&time_range=custom",
			wantErr: "time_start and time_end are both required",
		},
		{
			name:    "missing end",
			query:   "&time_range=custom&time_start=" + iso(now.Add(-time.Hour)),
			wantErr: "time_start and time_end are both required",
		},
		{
			name: "unparsable start",
			query: "&time_range=custom&time_start=yesterday&time_end=" +
				iso(now),
			wantErr: `invalid time_start "yesterday": must be an RFC 3339 timestamp`,
		},
		{
			name: "unparsable end",
			query: "&time_range=custom&time_start=" + iso(now.Add(-time.Hour)) +
				"&time_end=tomorrow",
			wantErr: `invalid time_end "tomorrow": must be an RFC 3339 timestamp`,
		},
		{
			name: "end before start",
			query: "&time_range=custom&time_start=" + iso(now.Add(-time.Hour)) +
				"&time_end=" + iso(now.Add(-2*time.Hour)),
			wantErr: "time_end must be after time_start",
		},
		{
			name: "start in the future",
			query: "&time_range=custom&time_start=" + iso(now.Add(time.Hour)) +
				"&time_end=" + iso(now.Add(2*time.Hour)),
			wantErr: "invalid time_start: must not be in the future",
		},
		{
			// Well past both the shared 366-day resolver cap and this
			// endpoint's own 30-day cap; the tighter one wins, because
			// ResolveTimeWindow's own rejection fires first only when the
			// span also exceeds 366 days, and the messages differ.
			name: "span beyond the cap",
			query: "&time_range=custom&time_start=" +
				iso(now.Add(-400*24*time.Hour)) + "&time_end=" + iso(now),
			wantErr: "span must not exceed 366 days",
		},
		{
			name: "span just beyond the endpoint cap",
			query: "&time_range=custom&time_start=" +
				iso(now.Add(-maxAggregationTimeSpan-time.Minute)) +
				"&time_end=" + iso(now),
			wantErr: "invalid time range: span must not exceed 30 days",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doDatabaseSummariesRequest(h, "connection_id=605"+tt.query)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body: %s", rec.Code,
					rec.Body.String())
			}
			if got := errorMessageFromBody(t, rec); !strings.Contains(
				got, tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", got, tt.wantErr)
			}
		})
	}
}

// TestDatabaseSummaries_AcceptsWindowJustInsideCap pins the accepted side
// of the per-endpoint span cap. Before issue #387 this handler validated
// time_range against the preset map, which capped the window at 30 days by
// construction; the explicit check restores that bound now that custom
// windows resolve through metrics.ResolveTimeWindow.
func TestDatabaseSummaries_AcceptsWindowJustInsideCap(t *testing.T) {
	h, _, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	now := time.Now().UTC()
	iso := func(ts time.Time) string { return ts.Format(time.RFC3339) }

	rec := doDatabaseSummariesRequest(h, "connection_id=607"+
		"&time_range=custom&time_start="+
		iso(now.Add(-maxAggregationTimeSpan+time.Minute))+
		"&time_end="+iso(now))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code,
			rec.Body.String())
	}
}

// TestDatabaseSummaries_ShortCustomWindowFloorsBucketWidth verifies the ten
// second bucket floor. The bucket width is the window span divided by sixty,
// which a five-minute custom window would drive down to five seconds, below
// any realistic probe interval; the floor keeps it at ten.
func TestDatabaseSummaries_ShortCustomWindowFloorsBucketWidth(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	const connID = 606
	now := time.Now().UTC()
	first := now.Add(-4 * time.Minute)
	second := now.Add(-3 * time.Minute)

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO metrics.pg_database
        (connection_id, collected_at, datname, datistemplate,
         database_size_bytes)
        VALUES ($1, $2, 'app', false, 4096)`, connID, second); err != nil {
		t.Fatalf("insert pg_database: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO metrics.pg_stat_database
        (connection_id, collected_at, datname, numbackends, blks_hit,
         blks_read, xact_commit, xact_rollback)
        VALUES ($1, $2, 'app', 2, 100, 20, 100, 1),
               ($1, $3, 'app', 5, 400, 50, 400, 4)`,
		connID, first, second); err != nil {
		t.Fatalf("insert pg_stat_database: %v", err)
	}

	summaries := decodeDatabaseSummaries(t, doDatabaseSummariesRequest(h,
		fmt.Sprintf(
			"connection_id=%d&time_range=custom&time_start=%s&time_end=%s",
			connID, now.Add(-5*time.Minute).Format(time.RFC3339),
			now.Format(time.RFC3339))))

	app, ok := summaries["app"]
	if !ok {
		t.Fatalf("expected 'app' in a five-minute window; got %#v", summaries)
	}
	// The two samples are a minute apart, so a ten second bucket puts each
	// delta in a bucket of its own; only the second sample has a delta.
	if len(app.CacheHitRatio.TimeSeries) != 1 {
		t.Errorf("TimeSeries length = %d, want 1; got %#v",
			len(app.CacheHitRatio.TimeSeries), app.CacheHitRatio.TimeSeries)
	}
	if app.CacheHitRatio.Current == nil ||
		*app.CacheHitRatio.Current != 90.91 {
		t.Errorf("CacheHitRatio.Current = %v, want 90.91",
			fmtFloatPtr(app.CacheHitRatio.Current))
	}
}

// TestDatabaseSummaries_TransactionBeginFailure verifies the 500 path when a
// read-only transaction cannot be started, here by closing the pool first.
func TestDatabaseSummaries_TransactionBeginFailure(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	_, _ = pool.Exec(context.Background(),
		databaseSummariesTestSchemaTeardown)
	pool.Close()

	rec := doDatabaseSummariesRequest(h, "connection_id=607")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body: %s", rec.Code,
			rec.Body.String())
	}
}

// transactionRatesProjection selects which deliberately broken projection
// of datname installTransactionRatesView installs over the base table.
type transactionRatesProjection int

const (
	// datnameAsTextArray projects datname as a text array, so that scanning
	// the column into a string fails.
	datnameAsTextArray transactionRatesProjection = iota
	// datnameAsDivisionByZero projects datname as a division by zero that
	// cannot be folded to a constant, so the statement prepares cleanly and
	// fails only when the rows are produced.
	datnameAsDivisionByZero
)

// transactionRatesBaseTable replaces metrics.pg_stat_database with a base
// table of the same shape, ready for one of the views below to project.
const transactionRatesBaseTable = `
        DROP TABLE IF EXISTS metrics.pg_stat_database CASCADE;
        CREATE TABLE metrics.pg_stat_database_base (
            connection_id  integer     NOT NULL,
            collected_at   timestamptz NOT NULL,
            datname        text,
            numbackends    integer     NOT NULL DEFAULT 0,
            blks_hit       bigint      NOT NULL DEFAULT 0,
            blks_read      bigint      NOT NULL DEFAULT 0,
            xact_commit    bigint      NOT NULL DEFAULT 0,
            xact_rollback  bigint      NOT NULL DEFAULT 0
        );`

// transactionRatesTextArrayView projects datname as a one-element text
// array.
const transactionRatesTextArrayView = `
        CREATE VIEW metrics.pg_stat_database AS
            SELECT connection_id, collected_at, ARRAY[datname] AS datname,
                   numbackends, blks_hit, blks_read, xact_commit,
                   xact_rollback
            FROM metrics.pg_stat_database_base;`

// transactionRatesDivisionView projects datname as a division by zero that
// the planner cannot fold away.
const transactionRatesDivisionView = `
        CREATE VIEW metrics.pg_stat_database AS
            SELECT connection_id, collected_at,
                   (1 / (numbackends - numbackends))::text AS datname,
                   numbackends, blks_hit, blks_read, xact_commit,
                   xact_rollback
            FROM metrics.pg_stat_database_base;`

// installTransactionRatesView replaces metrics.pg_stat_database with a base
// table of the same shape plus a view that projects datname through the
// chosen broken expression, so that a query over the view can be made to
// fail in a chosen way. The returned function restores the plain table.
func installTransactionRatesView(
	t *testing.T,
	pool *pgxpool.Pool,
	projection transactionRatesProjection,
) func() {
	t.Helper()
	ctx := context.Background()

	var viewDDL string
	switch projection {
	case datnameAsTextArray:
		viewDDL = transactionRatesTextArrayView
	case datnameAsDivisionByZero:
		viewDDL = transactionRatesDivisionView
	default:
		t.Fatalf("unknown transaction rates projection: %d", projection)
	}

	if _, err := pool.Exec(ctx, transactionRatesBaseTable); err != nil {
		t.Fatalf("failed to install pg_stat_database base table: %v", err)
	}
	if _, err := pool.Exec(ctx, viewDDL); err != nil {
		t.Fatalf("failed to install pg_stat_database view: %v", err)
	}

	return func() {
		_, _ = pool.Exec(context.Background(), `
            DROP VIEW IF EXISTS metrics.pg_stat_database;
            DROP TABLE IF EXISTS metrics.pg_stat_database_base;`)
	}
}

// seedTransactionRatesBase inserts the two samples queryTransactionRates
// needs to compute a delta.
func seedTransactionRatesBase(
	t *testing.T,
	pool *pgxpool.Pool,
	connID int,
	prev, latest time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO metrics.pg_stat_database_base
        (connection_id, collected_at, datname, numbackends, blks_hit,
         blks_read, xact_commit, xact_rollback)
        VALUES ($1, $2, 'app', 3, 100, 20, 1000, 10),
               ($1, $3, 'app', 4, 400, 50, 1300, 13)`,
		connID, prev, latest); err != nil {
		t.Fatalf("seed pg_stat_database_base failed: %v", err)
	}
}

// runTransactionRates drives queryTransactionRates alone against a base set
// holding only "app", in its own read-only transaction, and returns the
// summaries together with the helper's error.
func runTransactionRates(
	t *testing.T,
	h *PerfSummaryHandler,
	pool *pgxpool.Pool,
	connID int,
	startTime, endTime time.Time,
) (map[string]*DatabaseSummary, error) {
	t.Helper()
	ctx := context.Background()
	tx := mustTx(t, pool)
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after read is a no-op

	dbMap := map[string]*DatabaseSummary{
		"app": {
			DatabaseName:  "app",
			CacheHitRatio: CacheHitRatioData{TimeSeries: []CacheHitRatioPoint{}},
		},
	}
	err := h.queryTransactionRates(ctx, tx, connID, startTime, endTime, dbMap)
	return dbMap, err
}

// TestDatabaseSummaries_TransactionRatesScanError verifies that a row whose
// datname cannot be scanned into a string is returned as an error. pgx
// closes the result set on a scan failure, so skipping the row would
// silently drop every row after it too (issue #519). A plain NULL datname
// cannot reach the scan, because the self-join on datname discards NULLs,
// so the column is projected as a text array instead.
func TestDatabaseSummaries_TransactionRatesScanError(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	restore := installTransactionRatesView(t, pool, datnameAsTextArray)
	defer restore()

	const connID = 608
	now := time.Now().UTC()
	seedTransactionRatesBase(t, pool, connID, now.Add(-6*time.Minute),
		now.Add(-time.Minute))

	dbMap, err := runTransactionRates(t, h, pool, connID,
		now.Add(-time.Hour), now)
	if err == nil {
		t.Fatal("a scan failure must be returned as an error")
	}
	if dbMap["app"].TransactionRate != 0 {
		t.Errorf("TransactionRate = %v, want 0",
			dbMap["app"].TransactionRate)
	}
}

// TestDatabaseSummaries_TransactionRatesRowsError verifies that an error
// raised at execution time, which surfaces only through rows.Err() after
// iteration, is returned to the caller and leaves the summaries untouched.
// This is the path a statement timeout takes (issue #519). The division by
// (numbackends - numbackends) cannot be folded to a constant, so the
// statement prepares cleanly and fails only when the rows are produced.
func TestDatabaseSummaries_TransactionRatesRowsError(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	restore := installTransactionRatesView(t, pool, datnameAsDivisionByZero)
	defer restore()

	const connID = 609
	now := time.Now().UTC()
	seedTransactionRatesBase(t, pool, connID, now.Add(-6*time.Minute),
		now.Add(-time.Minute))

	dbMap, err := runTransactionRates(t, h, pool, connID,
		now.Add(-time.Hour), now)
	if err == nil {
		t.Fatal("an execution-time failure must be returned as an error")
	}
	if isUndefinedTableError(err) {
		t.Errorf("err = %v, must not be classed as undefined_table", err)
	}
	if dbMap["app"].TransactionRate != 0 {
		t.Errorf("TransactionRate = %v, want 0", dbMap["app"].TransactionRate)
	}
}

// TestDatabaseSummaries_MissingTablesReturnEmptySuccess verifies that the
// endpoint keeps reporting a missing metrics schema, the state of a
// workbench whose collector has never run, as a 200 with an empty
// databases array rather than as a failure (issue #519).
func TestDatabaseSummaries_MissingTablesReturnEmptySuccess(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	if _, err := pool.Exec(context.Background(),
		databaseSummariesTestSchemaTeardown); err != nil {
		t.Fatalf("teardown for missing-tables test failed: %v", err)
	}

	rec := doDatabaseSummariesRequest(h, "connection_id=610")
	summaries := decodeDatabaseSummaries(t, rec)
	if len(summaries) != 0 {
		t.Errorf("databases = %#v, want empty", summaries)
	}
	if !strings.Contains(rec.Body.String(), `"databases":[]`) {
		t.Errorf("body = %s, want an empty databases array, not null",
			rec.Body.String())
	}
}

// TestDatabaseSummaries_MissingLaterTableReturnsEmptySuccess drops only a
// table read by a later sub-query, so the size query succeeds and creates
// an entry before the undefined_table error arrives. The response must
// still be the empty success rather than a partial set of cards or a
// failure caused by the aborted transaction.
func TestDatabaseSummaries_MissingLaterTableReturnsEmptySuccess(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	const connID = 611
	now := time.Now().UTC()
	seedDatabaseSummariesFixture(t, pool, connID, now.Add(-time.Minute),
		now.Add(-2*time.Minute), now.Add(-90*time.Minute))
	if _, err := pool.Exec(context.Background(),
		`DROP TABLE metrics.pg_stat_all_tables`); err != nil {
		t.Fatalf("drop pg_stat_all_tables failed: %v", err)
	}

	rec := doDatabaseSummariesRequest(h, fmt.Sprintf("connection_id=%d",
		connID))
	if summaries := decodeDatabaseSummaries(t, rec); len(summaries) != 0 {
		t.Errorf("databases = %#v, want empty", summaries)
	}
}

// TestDatabaseSummaries_QueryFailureReportsError verifies that a failure
// other than a missing table, here an execution-time division by zero that
// stands in for a statement timeout, is reported as a 500 rather than as a
// successful response with the affected figures silently absent (issue
// #519).
func TestDatabaseSummaries_QueryFailureReportsError(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	restore := installTransactionRatesView(t, pool, datnameAsDivisionByZero)
	defer restore()

	const connID = 612
	now := time.Now().UTC()
	latest := now.Add(-time.Minute)
	seedTransactionRatesBase(t, pool, connID, now.Add(-6*time.Minute), latest)
	if _, err := pool.Exec(context.Background(), `INSERT INTO metrics.pg_database
        (connection_id, collected_at, datname, datistemplate,
         database_size_bytes)
        VALUES ($1, $2, 'app', false, 1048576)`, connID, latest); err != nil {
		t.Fatalf("seed pg_database failed: %v", err)
	}

	rec := doDatabaseSummariesRequest(h, fmt.Sprintf("connection_id=%d",
		connID))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body: %s", rec.Code,
			rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Failed to query database summaries") {
		t.Errorf("body = %s, want the generic failure message", body)
	}
	if strings.Contains(body, "division") {
		t.Errorf("body = %s, must not leak the database error", body)
	}
}

// TestDatabaseSummaries_NullRowsDoNotHideOthers seeds a row carrying NULLs
// in every table alongside a complete one, and checks that the complete
// database is still reported in full. Before the NULL-tolerant scans, a
// NULL ended the iteration of whichever helper met it, so every row after
// it was lost as well.
func TestDatabaseSummaries_NullRowsDoNotHideOthers(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	ctx := context.Background()
	const connID = 613
	now := time.Now().UTC()
	latest := now.Add(-time.Minute)
	prev := now.Add(-6 * time.Minute)

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed exec failed: %v\nSQL: %s", err, sql)
		}
	}
	exec(`INSERT INTO metrics.pg_database
        (connection_id, collected_at, datname, datistemplate,
         database_size_bytes)
        VALUES ($1, $2, 'gone', false, NULL),
               ($1, $2, 'good', false, 2048)`, connID, latest)
	exec(`ALTER TABLE metrics.pg_stat_database
        ALTER COLUMN numbackends DROP NOT NULL`)
	exec(`INSERT INTO metrics.pg_stat_database
        (connection_id, collected_at, datname, numbackends, blks_hit,
         blks_read, xact_commit, xact_rollback)
        VALUES ($1, $2, NULL, 1, 10, 1, 1, 0),
               ($1, $3, NULL, 1, 20, 2, 2, 0),
               ($1, $2, 'good', NULL, 100, 0, 100, 0),
               ($1, $3, 'good', 5, 190, 10, 400, 0)`, connID, prev, latest)
	exec(`INSERT INTO metrics.pg_stat_all_tables
        (connection_id, collected_at, database_name, n_live_tup, n_dead_tup)
        VALUES ($1, $2, NULL, 5, 5),
               ($1, $2, 'good', 90, 10)`, connID, latest)

	rec := doDatabaseSummariesRequest(h, fmt.Sprintf("connection_id=%d",
		connID))
	summaries := decodeDatabaseSummaries(t, rec)
	if _, ok := summaries["gone"]; ok {
		t.Errorf("a database with a NULL size must be skipped")
	}
	good, ok := summaries["good"]
	if !ok {
		t.Fatalf("'good' missing from %#v", summaries)
	}
	if good.SizeBytes != 2048 {
		t.Errorf("SizeBytes = %d, want 2048", good.SizeBytes)
	}
	if good.ActiveConnections != 5 {
		t.Errorf("ActiveConnections = %d, want 5", good.ActiveConnections)
	}
	if good.DeadTupleRatio != 10.0 {
		t.Errorf("DeadTupleRatio = %v, want 10", good.DeadTupleRatio)
	}
	// 300 commits over the five minutes between the two samples.
	if good.TransactionRate != 1.0 {
		t.Errorf("TransactionRate = %v, want 1.0", good.TransactionRate)
	}
	if len(good.CacheHitRatio.TimeSeries) != 1 {
		t.Fatalf("len(TimeSeries) = %d, want 1",
			len(good.CacheHitRatio.TimeSeries))
	}
	// 90 hits and 10 reads between the two samples.
	assertRatio(t, "good current", good.CacheHitRatio.Current, 90.0)
}

// TestDatabaseSummaries_ScanTypeMismatchIsAnError replaces a metrics table
// with a view that projects the database name as a text array, so that
// the helper's scan into a string fails. pgx closes the result set on a
// scan failure, so the helper must return the error rather than report
// the rows it read before it as the whole result.
func TestDatabaseSummaries_ScanTypeMismatchIsAnError(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	ctx := context.Background()
	start := time.Now().UTC().Add(-time.Hour)
	end := time.Now().UTC()

	cases := []struct {
		name  string
		table string
		view  string
		run   func(pgx.Tx, map[string]*DatabaseSummary) error
	}{
		{
			name:  "sizes",
			table: "metrics.pg_database",
			view: `SELECT 1 AS connection_id,
                       now() - INTERVAL '1 minute' AS collected_at,
                       ARRAY['app'] AS datname, false AS datistemplate,
                       1::bigint AS database_size_bytes`,
			run: func(tx pgx.Tx, m map[string]*DatabaseSummary) error {
				return h.queryDatabaseSizes(ctx, tx, 1, start, end, m)
			},
		},
		{
			name:  "dead tuples",
			table: "metrics.pg_stat_all_tables",
			view: `SELECT 1 AS connection_id,
                       now() - INTERVAL '1 minute' AS collected_at,
                       ARRAY['app'] AS database_name,
                       1::bigint AS n_live_tup, 1::bigint AS n_dead_tup`,
			run: func(tx pgx.Tx, m map[string]*DatabaseSummary) error {
				return h.queryDeadTupleRatios(ctx, tx, 1, start, end, m)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, fmt.Sprintf(
				"DROP TABLE %s; CREATE VIEW %s AS %s",
				tc.table, tc.table, tc.view)); err != nil {
				t.Fatalf("install view: %v", err)
			}
			// The shared teardown drops tables, not views, so the view
			// must go before the next test rebuilds the schema.
			defer func() {
				if _, err := pool.Exec(ctx,
					"DROP VIEW IF EXISTS "+tc.table); err != nil {
					t.Errorf("drop view: %v", err)
				}
			}()

			tx := mustTx(t, pool)
			defer tx.Rollback(ctx) //nolint:errcheck // rollback after read is a no-op
			dbMap := map[string]*DatabaseSummary{
				"app": {DatabaseName: "app"},
			}
			err := tc.run(tx, dbMap)
			if err == nil || isUndefinedTableError(err) {
				t.Errorf("err = %v, want a scan error", err)
			}
		})
	}
}
