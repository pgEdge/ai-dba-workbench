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
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/database"
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
    xact_rollback  bigint      NOT NULL DEFAULT 0
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
	handler := NewPerfSummaryHandler(ds, nil, "")
	cleanup := func() {
		_, _ = pool.Exec(context.Background(),
			databaseSummariesTestSchemaTeardown)
		pool.Close()
	}
	return handler, pool, cleanup
}

// buildDatabaseSummaries runs the five query helpers in the same sequence
// as handleDatabaseSummaries and returns the resulting summaries keyed by
// database name. It isolates the aggregation logic under test from the
// HTTP/RBAC plumbing in the handler.
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
	h.queryDatabaseSizes(ctx, tx, connID, dbMap)
	h.queryDatabaseStats(ctx, tx, connID, dbMap)
	h.queryDeadTupleRatios(ctx, tx, connID, dbMap)
	h.queryTransactionRates(ctx, tx, connID, dbMap)
	h.queryDatabaseCacheHitTimeSeries(ctx, tx, connID, startTime, endTime,
		bucketInterval, dbMap)

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
	// blks_hit=900, blks_read=100 -> 90% current cache hit ratio.
	if keep.CacheHitRatio.Current != 90.0 {
		t.Errorf("CacheHitRatio.Current = %v, want 90.0",
			keep.CacheHitRatio.Current)
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

	// Start with an EMPTY base set (skip queryDatabaseSizes). Every
	// enrichment helper must leave the map empty because none may create
	// entries.
	dbMap := make(map[string]*DatabaseSummary)
	h.queryDatabaseStats(ctx, tx, connID, dbMap)
	h.queryDeadTupleRatios(ctx, tx, connID, dbMap)
	h.queryTransactionRates(ctx, tx, connID, dbMap)
	h.queryDatabaseCacheHitTimeSeries(ctx, tx, connID, startTime, now,
		"60 seconds", dbMap)

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
	h.queryDatabaseStats(ctx, tx, connID, dbMap)
	h.queryDeadTupleRatios(ctx, tx, connID, dbMap)
	h.queryTransactionRates(ctx, tx, connID, dbMap)
	h.queryDatabaseCacheHitTimeSeries(ctx, tx, connID, startTime, now,
		"60 seconds", dbMap)

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

// TestDatabaseSummaries_QueryError verifies every query helper handles a
// failing query (here, missing metrics tables) gracefully by logging and
// returning without panicking or creating spurious entries.
func TestDatabaseSummaries_QueryError(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	ctx := context.Background()
	// Drop the metrics tables so every helper's query fails.
	if _, err := pool.Exec(ctx,
		databaseSummariesTestSchemaTeardown); err != nil {
		t.Fatalf("teardown for query-error test failed: %v", err)
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after read is a no-op

	dbMap := make(map[string]*DatabaseSummary)
	h.queryDatabaseSizes(ctx, tx, 1, dbMap)
	// A failed query aborts the transaction, so restart it for the rest.
	_ = tx.Rollback(ctx)
	for _, fn := range []func(pgx.Tx){
		func(tx pgx.Tx) { h.queryDatabaseStats(ctx, tx, 1, dbMap) },
		func(tx pgx.Tx) { h.queryDeadTupleRatios(ctx, tx, 1, dbMap) },
		func(tx pgx.Tx) { h.queryTransactionRates(ctx, tx, 1, dbMap) },
		func(tx pgx.Tx) {
			h.queryDatabaseCacheHitTimeSeries(ctx, tx, 1,
				time.Now().Add(-time.Hour), time.Now(), "60 seconds", dbMap)
		},
	} {
		// Each query gets its own transaction, rolled back immediately
		// after use, since a failed query aborts the transaction and
		// would otherwise leak a checked-out pool connection that
		// pool.Close() waits on forever in cleanup().
		itemTx := mustTx(t, pool)
		fn(itemTx)
		_ = itemTx.Rollback(ctx)
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

// TestDatabaseSummaries_ScanError verifies that a row which fails to scan
// (NULL values in NOT NULL destination columns) is skipped rather than
// crashing the helper, and does not create a dbMap entry.
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
