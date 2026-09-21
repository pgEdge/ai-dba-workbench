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
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// These tests cover the issue #469 fix: transaction throughput is computed
// from per-database deltas of the pg_stat_database transaction counters
// rather than from a cross-database SUM differenced after the fact, and an
// interval in which a database's stats_reset changed is discarded. They
// reuse the trimmed metrics schema and connection helper from
// perf_summary_database_summaries_test.go and must not run in parallel
// because the metrics tables are shared.

// txnSample is one pg_stat_database row to seed for the transaction
// queries. The sample is taken at cacheHitBase + minute, so a "60 seconds"
// bucket interval anchored at cacheHitBase places it in bucket minute.
type txnSample struct {
	minute     int
	datname    string
	commit     int64
	rollback   int64
	statsReset *time.Time
}

// seedTxnSamples inserts the given samples for a connection.
func seedTxnSamples(
	t *testing.T,
	pool *pgxpool.Pool,
	connID int,
	samples []txnSample,
) {
	t.Helper()
	ctx := context.Background()
	for _, smp := range samples {
		if _, err := pool.Exec(ctx, `INSERT INTO metrics.pg_stat_database
            (connection_id, collected_at, datname, xact_commit,
             xact_rollback, stats_reset)
            VALUES ($1, $2, $3, $4, $5, $6)`,
			connID, cacheHitBase.Add(time.Duration(smp.minute)*time.Minute),
			smp.datname, smp.commit, smp.rollback,
			smp.statsReset); err != nil {
			t.Fatalf("seed pg_stat_database: %v", err)
		}
	}
}

// runQueryTransactions drives queryTransactions in a read-only
// transaction over the fixed window [cacheHitBase, cacheHitBase + 1h]
// with 60 second buckets.
func runQueryTransactions(
	t *testing.T,
	h *PerfSummaryHandler,
	pool *pgxpool.Pool,
	connID int,
) (float64, float64, []TransactionPoint) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after read is a no-op
	return h.queryTransactions(ctx, tx, connID, cacheHitBase,
		cacheHitBase.Add(time.Hour), "60 seconds")
}

// assertSinglePoint fails unless exactly one point came back, and returns
// it for further assertions.
func assertSinglePoint(
	t *testing.T,
	points []TransactionPoint,
) TransactionPoint {
	t.Helper()
	if len(points) != 1 {
		t.Fatalf("len(points) = %d, want 1: %#v", len(points), points)
	}
	return points[0]
}

// TestQueryTransactions_NewDatabaseExcludedFromInterval seeds a database
// that first appears in the second sample carrying a large lifetime
// commit count. Differencing a cross-database SUM would charge the whole
// of that lifetime to the interval; per-database deltas must leave it out
// because the new database has no predecessor sample.
func TestQueryTransactions_NewDatabaseExcludedFromInterval(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	const connID = 520
	seedTxnSamples(t, pool, connID, []txnSample{
		{minute: 0, datname: "old", commit: 1000},
		{minute: 1, datname: "old", commit: 1060},
		{minute: 1, datname: "new", commit: 1000000},
	})

	cps, rbPct, points := runQueryTransactions(t, h, pool, connID)

	pt := assertSinglePoint(t, points)
	// 60 commits over 60 seconds, with the new database contributing
	// nothing at all.
	if pt.CommitsPerSec != 1.0 {
		t.Errorf("commits_per_sec = %v, want 1", pt.CommitsPerSec)
	}
	if cps != 1.0 || rbPct != 0 {
		t.Errorf("current = (%v, %v), want (1, 0)", cps, rbPct)
	}
}

// TestQueryTransactions_DroppedDatabaseKeepsInterval seeds a busy
// database that disappears between two samples. Summing across databases
// first makes the interval's total fall, which the negative-delta guard
// turns into a missing or flat-zero bucket; per-database deltas keep the
// surviving database's throughput.
func TestQueryTransactions_DroppedDatabaseKeepsInterval(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	const connID = 521
	seedTxnSamples(t, pool, connID, []txnSample{
		{minute: 0, datname: "keep", commit: 1000},
		{minute: 0, datname: "gone", commit: 1000000},
		{minute: 1, datname: "keep", commit: 1060},
	})

	cps, _, points := runQueryTransactions(t, h, pool, connID)

	pt := assertSinglePoint(t, points)
	if pt.CommitsPerSec != 1.0 {
		t.Errorf("commits_per_sec = %v, want 1 (not flattened to zero)",
			pt.CommitsPerSec)
	}
	if cps != 1.0 {
		t.Errorf("current commits_per_sec = %v, want 1", cps)
	}
}

// TestQueryTransactions_StatsResetDiscardsOnlyThatDatabase seeds two
// databases over one interval, one of which reset its statistics and
// climbed past its previous value in the same interval. The delta is
// positive, so only the stats_reset guard can catch it, and it must
// invalidate that database's interval alone.
func TestQueryTransactions_StatsResetDiscardsOnlyThatDatabase(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	const connID = 522
	first := cacheHitBase.Add(-time.Hour)
	second := cacheHitBase.Add(30 * time.Second)
	seedTxnSamples(t, pool, connID, []txnSample{
		{minute: 0, datname: "a", commit: 1000, statsReset: &first},
		{minute: 0, datname: "b", commit: 2000, statsReset: &first},
		{minute: 1, datname: "a", commit: 1060, statsReset: &first},
		// b reset and then overshot its previous value: +3000 looks
		// plausible but is meaningless.
		{minute: 1, datname: "b", commit: 5000, statsReset: &second},
	})

	cps, _, points := runQueryTransactions(t, h, pool, connID)

	pt := assertSinglePoint(t, points)
	if pt.CommitsPerSec != 1.0 {
		t.Errorf("commits_per_sec = %v, want 1 (only a's delta counted)",
			pt.CommitsPerSec)
	}
	if cps != 1.0 {
		t.Errorf("current commits_per_sec = %v, want 1", cps)
	}
}

// TestQueryTransactions_NegativeDeltaDiscarded keeps the pre-existing
// guard honest: a database whose counters fall without any change of
// stats_reset still has its interval discarded.
func TestQueryTransactions_NegativeDeltaDiscarded(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	const connID = 523
	seedTxnSamples(t, pool, connID, []txnSample{
		{minute: 0, datname: "a", commit: 1000},
		{minute: 0, datname: "b", commit: 9000},
		{minute: 1, datname: "a", commit: 1060},
		{minute: 1, datname: "b", commit: 10}, // counters went backwards
	})

	cps, _, points := runQueryTransactions(t, h, pool, connID)

	pt := assertSinglePoint(t, points)
	if pt.CommitsPerSec != 1.0 {
		t.Errorf("commits_per_sec = %v, want 1 (b's interval dropped)",
			pt.CommitsPerSec)
	}
	if cps != 1.0 {
		t.Errorf("current commits_per_sec = %v, want 1", cps)
	}
}

// TestQueryTransactions_ElapsedIsPerSampleNotPerDatabase seeds four
// databases across one 60 second interval. Elapsed time must come from
// the distinct sample timestamps, so the rate is the summed delta over 60
// seconds; carrying elapsed on every per-database row and summing it
// would divide by 240 seconds instead and quarter the answer.
func TestQueryTransactions_ElapsedIsPerSampleNotPerDatabase(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	const connID = 524
	names := []string{"a", "b", "c", "d"}
	var samples []txnSample
	for _, name := range names {
		samples = append(samples,
			txnSample{minute: 0, datname: name, commit: 1000, rollback: 100},
			txnSample{minute: 1, datname: name, commit: 1030, rollback: 110},
		)
	}
	seedTxnSamples(t, pool, connID, samples)

	cps, rbPct, points := runQueryTransactions(t, h, pool, connID)

	pt := assertSinglePoint(t, points)
	// 4 databases * 30 commits = 120 commits over 60 seconds.
	if pt.CommitsPerSec != 2.0 {
		t.Errorf("commits_per_sec = %v, want 2", pt.CommitsPerSec)
	}
	// 40 rollbacks against 160 transactions.
	if pt.RollbackPercent != 25.0 {
		t.Errorf("rollback_percent = %v, want 25", pt.RollbackPercent)
	}
	if cps != 2.0 || rbPct != 25.0 {
		t.Errorf("current = (%v, %v), want (2, 25)", cps, rbPct)
	}
}

// TestQueryTransactions_MultipleBuckets checks that several consecutive
// intervals each land in their own bucket with their own rate, and that
// the returned "current" values come from the latest bucket.
func TestQueryTransactions_MultipleBuckets(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	const connID = 525
	seedTxnSamples(t, pool, connID, []txnSample{
		{minute: 0, datname: "a", commit: 0},
		{minute: 1, datname: "a", commit: 60},  // 1/s
		{minute: 2, datname: "a", commit: 180}, // 2/s
		{minute: 3, datname: "a", commit: 360}, // 3/s
	})

	cps, _, points := runQueryTransactions(t, h, pool, connID)

	if len(points) != 3 {
		t.Fatalf("len(points) = %d, want 3: %#v", len(points), points)
	}
	for i, want := range []float64{1, 2, 3} {
		if points[i].CommitsPerSec != want {
			t.Errorf("points[%d].CommitsPerSec = %v, want %v",
				i, points[i].CommitsPerSec, want)
		}
	}
	if cps != 3.0 {
		t.Errorf("current commits_per_sec = %v, want 3", cps)
	}
}

// TestQueryTransactions_IdleInterval checks that a bucket with valid but
// zero deltas is still emitted, with a zero rate and a zero rollback
// percentage rather than a division error.
func TestQueryTransactions_IdleInterval(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	const connID = 526
	seedTxnSamples(t, pool, connID, []txnSample{
		{minute: 0, datname: "a", commit: 500, rollback: 5},
		{minute: 1, datname: "a", commit: 500, rollback: 5},
	})

	cps, rbPct, points := runQueryTransactions(t, h, pool, connID)

	pt := assertSinglePoint(t, points)
	if pt.CommitsPerSec != 0 || pt.RollbackPercent != 0 {
		t.Errorf("idle point = (%v, %v), want (0, 0)",
			pt.CommitsPerSec, pt.RollbackPercent)
	}
	if cps != 0 || rbPct != 0 {
		t.Errorf("current = (%v, %v), want (0, 0)", cps, rbPct)
	}
}

// TestQueryTransactions_NoRows checks the empty-result contract: an empty
// non-nil slice and zeroed current values.
func TestQueryTransactions_NoRows(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	cps, rbPct, points := runQueryTransactions(t, h, pool, 527)

	if points == nil || len(points) != 0 {
		t.Errorf("points = %#v, want empty non-nil slice", points)
	}
	if cps != 0 || rbPct != 0 {
		t.Errorf("current = (%v, %v), want (0, 0)", cps, rbPct)
	}
}

// TestQueryTransactions_QueryError drops the metrics tables so the query
// fails, and checks that the failure is reported as an empty result
// rather than a panic or a nil slice.
func TestQueryTransactions_QueryError(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		databaseSummariesTestSchemaTeardown); err != nil {
		t.Fatalf("teardown for query-error test failed: %v", err)
	}

	cps, rbPct, points := runQueryTransactions(t, h, pool, 528)

	if points == nil || len(points) != 0 {
		t.Errorf("points = %#v, want empty non-nil slice", points)
	}
	if cps != 0 || rbPct != 0 {
		t.Errorf("current = (%v, %v), want (0, 0)", cps, rbPct)
	}
}

// installFailingTransactionView replaces metrics.pg_stat_database with a
// view over a renamed copy of the table whose xact_commit column divides
// by (xact_commit - xact_commit). The planner cannot fold that to a
// constant, so the query prepares cleanly and only fails at execution,
// which is the path that reaches rows.Err rather than the tx.Query error
// return. Samples are seeded so that the failing expression is actually
// evaluated. The returned function restores the schema.
func installFailingTransactionView(
	t *testing.T,
	pool *pgxpool.Pool,
	connID int,
) func() {
	t.Helper()
	ctx := context.Background()
	stmts := []string{
		`ALTER TABLE metrics.pg_stat_database RENAME TO pg_stat_database_src`,
		`CREATE VIEW metrics.pg_stat_database AS
            SELECT connection_id, collected_at, datname,
                   xact_commit / (xact_commit - xact_commit) AS xact_commit,
                   xact_rollback, stats_reset
            FROM metrics.pg_stat_database_src`,
		`INSERT INTO metrics.pg_stat_database_src
            (connection_id, collected_at, datname, xact_commit, xact_rollback)
            VALUES ($1, $2, 'db', 100, 10), ($1, $3, 'db', 200, 20)`,
	}
	for i, stmt := range stmts {
		var err error
		if i == len(stmts)-1 {
			_, err = pool.Exec(ctx, stmt, connID, cacheHitBase,
				cacheHitBase.Add(time.Minute))
		} else {
			_, err = pool.Exec(ctx, stmt)
		}
		if err != nil {
			t.Fatalf("install failing view: %v", err)
		}
	}
	return func() {
		for _, stmt := range []string{
			`DROP VIEW IF EXISTS metrics.pg_stat_database`,
			`ALTER TABLE metrics.pg_stat_database_src RENAME TO pg_stat_database`,
		} {
			if _, err := pool.Exec(ctx, stmt); err != nil {
				t.Errorf("restore schema: %v", err)
			}
		}
	}
}

// TestQueryTransactions_ExecutionErrorReturnsNoData drives the rows.Err
// branch. A scan failure is not reachable with real rows, because every
// output column is a numeric division by a positive elapsed time, an
// explicit float cast or a date_bin over a bounded timestamptz, so an
// execution-time failure is the only way into the loop's error handling.
func TestQueryTransactions_ExecutionErrorReturnsNoData(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	const connID = 529
	restore := installFailingTransactionView(t, pool, connID)
	defer restore()

	cps, rbPct, points := runQueryTransactions(t, h, pool, connID)

	if points == nil || len(points) != 0 {
		t.Errorf("points = %#v, want empty non-nil slice", points)
	}
	if cps != 0 || rbPct != 0 {
		t.Errorf("current = (%v, %v), want (0, 0)", cps, rbPct)
	}
}
