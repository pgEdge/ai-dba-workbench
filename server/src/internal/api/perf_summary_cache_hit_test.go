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
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// These tests cover the issue #401 fix: cache hit ratios are computed from
// per-interval deltas of the pg_stat_database block counters rather than
// from the lifetime counters, so a recent problem is visible even after a
// long history of healthy samples. They reuse the trimmed metrics schema
// and connection helper from perf_summary_database_summaries_test.go and
// must not run in parallel because the metrics tables are shared.

// cacheHitBase is a fixed, bucket-aligned origin so that a "60 seconds"
// bucket interval starting at cacheHitBase places the sample taken at
// cacheHitBase + i minutes in bucket i exactly.
var cacheHitBase = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// cacheHitSample is one pg_stat_database row to seed.
type cacheHitSample struct {
	minute   int
	datname  string
	blksHit  int64
	blksRead int64
}

// seedCacheHitSamples inserts the given samples for a connection at
// cacheHitBase + minute.
func seedCacheHitSamples(
	t *testing.T,
	pool *pgxpool.Pool,
	connID int,
	samples []cacheHitSample,
) {
	t.Helper()
	ctx := context.Background()
	for _, smp := range samples {
		if _, err := pool.Exec(ctx, `INSERT INTO metrics.pg_stat_database
            (connection_id, collected_at, datname, blks_hit, blks_read)
            VALUES ($1, $2, $3, $4, $5)`,
			connID, cacheHitBase.Add(time.Duration(smp.minute)*time.Minute),
			smp.datname, smp.blksHit, smp.blksRead); err != nil {
			t.Fatalf("seed pg_stat_database: %v", err)
		}
	}
}

// healthyThenPoorSamples builds a long history in which every interval
// adds 990 hits and 10 reads (99%), followed by one interval that adds
// 50 hits and 50 reads (50%). The lifetime ratio after the final sample
// is still well above 97%, so any assertion that the latest bucket is 50%
// proves the ratio is not a since-reset average.
func healthyThenPoorSamples(datname string, offsetHit, offsetRead int64) []cacheHitSample {
	const history = 30
	samples := make([]cacheHitSample, 0, history+1)
	hit, read := offsetHit, offsetRead
	for i := 0; i <= history; i++ {
		samples = append(samples, cacheHitSample{i, datname, hit, read})
		hit += 990
		read += 10
	}
	// Final sample: 50% interval.
	last := samples[len(samples)-1]
	samples = append(samples, cacheHitSample{
		history + 1, datname, last.blksHit + 50, last.blksRead + 50})
	return samples
}

// runQueryCacheHit drives queryCacheHit in a read-only transaction over
// the fixed window [cacheHitBase, cacheHitBase + 1h] with 60s buckets.
func runQueryCacheHit(
	t *testing.T,
	h *PerfSummaryHandler,
	pool *pgxpool.Pool,
	connID int,
) (float64, float64, *float64, []CacheHitRatioPoint) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after read is a no-op
	return h.queryCacheHit(ctx, tx, connID, cacheHitBase,
		cacheHitBase.Add(time.Hour), "60 seconds")
}

// runQueryDatabaseCacheHit drives queryDatabaseCacheHitTimeSeries over
// the same fixed window with a dbMap pre-seeded for the named databases,
// mirroring the base set queryDatabaseSizes would have produced.
func runQueryDatabaseCacheHit(
	t *testing.T,
	h *PerfSummaryHandler,
	pool *pgxpool.Pool,
	connID int,
	datnames ...string,
) map[string]*DatabaseSummary {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after read is a no-op

	dbMap := make(map[string]*DatabaseSummary, len(datnames))
	for _, name := range datnames {
		dbMap[name] = &DatabaseSummary{
			DatabaseName:  name,
			CacheHitRatio: CacheHitRatioData{TimeSeries: []CacheHitRatioPoint{}},
		}
	}
	h.queryDatabaseCacheHitTimeSeries(ctx, tx, connID, cacheHitBase,
		cacheHitBase.Add(time.Hour), "60 seconds", dbMap)
	return dbMap
}

// fmtFloatPtr renders a *float64 for test failure messages.
func fmtFloatPtr(v *float64) string {
	if v == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%v", *v)
}

// assertRatio fails unless got is non-nil and equal to want.
func assertRatio(t *testing.T, what string, got *float64, want float64) {
	t.Helper()
	if got == nil || *got != want {
		t.Errorf("%s = %s, want %v", what, fmtFloatPtr(got), want)
	}
}

// assertNilRatio fails unless got is nil.
func assertNilRatio(t *testing.T, what string, got *float64) {
	t.Helper()
	if got != nil {
		t.Errorf("%s = %v, want nil", what, *got)
	}
}

// assertSaneRatios fails if any point carries a negative or out-of-range
// ratio, which is what a counter reset would produce if it were not
// discarded.
func assertSaneRatios(t *testing.T, points []CacheHitRatioPoint) {
	t.Helper()
	for _, pt := range points {
		if pt.Value != nil && (*pt.Value < 0 || *pt.Value > 100) {
			t.Errorf("point at %v has out-of-range ratio %v", pt.Time, *pt.Value)
		}
	}
}

func TestQueryCacheHit_LatestIntervalNotLifetimeAverage(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	const connID = 501
	// Two databases per sample so the query's SUM across databases is
	// exercised; both follow the same healthy-then-poor shape.
	seedCacheHitSamples(t, pool, connID, healthyThenPoorSamples("a", 0, 0))
	seedCacheHitSamples(t, pool, connID, healthyThenPoorSamples("b", 5000, 500))

	hit, read, current, points := runQueryCacheHit(t, h, pool, connID)

	// 32 samples -> 31 deltas, one per bucket.
	if len(points) != 31 {
		t.Fatalf("len(points) = %d, want 31", len(points))
	}
	for _, pt := range points[:len(points)-1] {
		assertRatio(t, "historical point", pt.Value, 99.0)
	}
	assertRatio(t, "latest point", points[len(points)-1].Value, 50.0)
	assertRatio(t, "current", current, 50.0)
	// Latest-bucket deltas summed over both databases: 100 hits, 100 reads.
	if hit != 100 || read != 100 {
		t.Errorf("latest deltas = (%v, %v), want (100, 100)", hit, read)
	}
	assertSaneRatios(t, points)
}

func TestQueryCacheHit_CounterResetDiscarded(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	const connID = 502
	seedCacheHitSamples(t, pool, connID, []cacheHitSample{
		{0, "db", 1000, 100},
		{1, "db", 2000, 200}, // +1000/+100 -> 90.91%
		{2, "db", 100, 10},   // counters dropped: stats reset, discarded
		{3, "db", 600, 60},   // +500/+50 -> 90.91%
	})

	_, _, current, points := runQueryCacheHit(t, h, pool, connID)

	if len(points) != 2 {
		t.Fatalf("len(points) = %d, want 2 (reset sample discarded): %#v",
			len(points), points)
	}
	assertRatio(t, "points[0]", points[0].Value, 90.91)
	assertRatio(t, "points[1]", points[1].Value, 90.91)
	assertRatio(t, "current", current, 90.91)
	assertSaneRatios(t, points)
}

func TestQueryCacheHit_ZeroActivityIsNull(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	const connID = 503
	seedCacheHitSamples(t, pool, connID, []cacheHitSample{
		{0, "db", 100, 10},
		{1, "db", 100, 10}, // idle interval
	})

	hit, read, current, points := runQueryCacheHit(t, h, pool, connID)

	if len(points) != 1 {
		t.Fatalf("len(points) = %d, want 1 (idle bucket still emitted)",
			len(points))
	}
	assertNilRatio(t, "points[0]", points[0].Value)
	assertNilRatio(t, "current", current)
	if hit != 0 || read != 0 {
		t.Errorf("latest deltas = (%v, %v), want (0, 0)", hit, read)
	}
}

func TestQueryCacheHit_NoRows(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	hit, read, current, points := runQueryCacheHit(t, h, pool, 504)

	if points == nil || len(points) != 0 {
		t.Errorf("points = %#v, want empty non-nil slice", points)
	}
	assertNilRatio(t, "current", current)
	if hit != 0 || read != 0 {
		t.Errorf("latest deltas = (%v, %v), want (0, 0)", hit, read)
	}
}

func TestQueryCacheHit_QueryError(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		databaseSummariesTestSchemaTeardown); err != nil {
		t.Fatalf("teardown for query-error test failed: %v", err)
	}

	hit, read, current, points := runQueryCacheHit(t, h, pool, 505)

	if points == nil || len(points) != 0 {
		t.Errorf("points = %#v, want empty non-nil slice", points)
	}
	assertNilRatio(t, "current", current)
	if hit != 0 || read != 0 {
		t.Errorf("latest deltas = (%v, %v), want (0, 0)", hit, read)
	}
}

// TestQueryCacheHit_MultiConnectionAggregate seeds two connections whose
// lifetime ratios are identical but whose latest-bucket deltas differ,
// and checks that the aggregate handlePerfSummary computes is weighted
// by the latest-bucket deltas (and is nil when every connection is
// idle).
func TestQueryCacheHit_MultiConnectionAggregate(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	const connA, connB, connIdle = 506, 507, 508
	// Both connections start from a 50% lifetime position. A's latest
	// interval is 900 hits / 100 reads (90%), B's is 100 / 900 (10%).
	seedCacheHitSamples(t, pool, connA, []cacheHitSample{
		{0, "db", 10000, 10000},
		{1, "db", 10900, 10100},
	})
	seedCacheHitSamples(t, pool, connB, []cacheHitSample{
		{0, "db", 10000, 10000},
		{1, "db", 10100, 10900},
	})
	seedCacheHitSamples(t, pool, connIdle, []cacheHitSample{
		{0, "db", 10000, 10000},
		{1, "db", 10000, 10000},
	})

	hitA, readA, curA, _ := runQueryCacheHit(t, h, pool, connA)
	hitB, readB, curB, _ := runQueryCacheHit(t, h, pool, connB)
	hitI, readI, curI, _ := runQueryCacheHit(t, h, pool, connIdle)

	assertRatio(t, "connA current", curA, 90.0)
	assertRatio(t, "connB current", curB, 10.0)
	assertNilRatio(t, "idle current", curI)

	// Equal weights: (900 + 100) / (1000 + 1000) = 50%.
	assertRatio(t, "aggregate A+B",
		aggregateCacheHitRatio(hitA+hitB, readA+readB), 50.0)
	// The idle connection contributes nothing to the weights.
	assertRatio(t, "aggregate A+B+idle",
		aggregateCacheHitRatio(hitA+hitB+hitI, readA+readB+readI), 50.0)
	// A alone: 900 / 1000 = 90%, rounded to one decimal place.
	assertRatio(t, "aggregate A", aggregateCacheHitRatio(hitA, readA), 90.0)
	// Nothing but idle connections: nil rather than 0%.
	assertNilRatio(t, "aggregate idle", aggregateCacheHitRatio(hitI, readI))
}

func TestDatabaseCacheHit_LatestIntervalNotLifetimeAverage(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	const connID = 511
	// "keep" degrades to 50% in its final interval, "other" stays at 99%
	// throughout; the PARTITION BY datname must keep them apart.
	seedCacheHitSamples(t, pool, connID, healthyThenPoorSamples("keep", 0, 0))
	other := healthyThenPoorSamples("other", 0, 0)
	other = other[:len(other)-1]
	seedCacheHitSamples(t, pool, connID, other)

	dbMap := runQueryDatabaseCacheHit(t, h, pool, connID, "keep", "other")

	keep := dbMap["keep"]
	if len(keep.CacheHitRatio.TimeSeries) != 31 {
		t.Fatalf("keep: len(TimeSeries) = %d, want 31",
			len(keep.CacheHitRatio.TimeSeries))
	}
	series := keep.CacheHitRatio.TimeSeries
	for _, pt := range series[:len(series)-1] {
		assertRatio(t, "keep historical point", pt.Value, 99.0)
	}
	assertRatio(t, "keep latest point", series[len(series)-1].Value, 50.0)
	assertRatio(t, "keep current", keep.CacheHitRatio.Current, 50.0)
	assertSaneRatios(t, series)

	oth := dbMap["other"]
	if len(oth.CacheHitRatio.TimeSeries) != 30 {
		t.Fatalf("other: len(TimeSeries) = %d, want 30",
			len(oth.CacheHitRatio.TimeSeries))
	}
	for _, pt := range oth.CacheHitRatio.TimeSeries {
		assertRatio(t, "other point", pt.Value, 99.0)
	}
	assertRatio(t, "other current", oth.CacheHitRatio.Current, 99.0)
}

func TestDatabaseCacheHit_CounterResetDiscarded(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	const connID = 512
	seedCacheHitSamples(t, pool, connID, []cacheHitSample{
		{0, "db", 1000, 100},
		{1, "db", 2000, 200}, // +1000/+100 -> 90.91%
		{2, "db", 100, 10},   // reset, discarded
		{3, "db", 600, 60},   // +500/+50 -> 90.91%
	})

	db := runQueryDatabaseCacheHit(t, h, pool, connID, "db")["db"]

	series := db.CacheHitRatio.TimeSeries
	if len(series) != 2 {
		t.Fatalf("len(TimeSeries) = %d, want 2: %#v", len(series), series)
	}
	assertRatio(t, "points[0]", series[0].Value, 90.91)
	assertRatio(t, "points[1]", series[1].Value, 90.91)
	assertRatio(t, "current", db.CacheHitRatio.Current, 90.91)
	assertSaneRatios(t, series)
}

func TestDatabaseCacheHit_ZeroActivityIsNull(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	const connID = 513
	seedCacheHitSamples(t, pool, connID, []cacheHitSample{
		{0, "db", 100, 10},
		{1, "db", 100, 10},
	})

	db := runQueryDatabaseCacheHit(t, h, pool, connID, "db")["db"]

	if len(db.CacheHitRatio.TimeSeries) != 1 {
		t.Fatalf("len(TimeSeries) = %d, want 1 (idle bucket still emitted)",
			len(db.CacheHitRatio.TimeSeries))
	}
	assertNilRatio(t, "points[0]", db.CacheHitRatio.TimeSeries[0].Value)
	assertNilRatio(t, "current", db.CacheHitRatio.Current)
}

func TestDatabaseCacheHit_NoRows(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	db := runQueryDatabaseCacheHit(t, h, pool, 514, "db")["db"]

	if len(db.CacheHitRatio.TimeSeries) != 0 {
		t.Errorf("TimeSeries = %#v, want empty", db.CacheHitRatio.TimeSeries)
	}
	assertNilRatio(t, "current", db.CacheHitRatio.Current)
}

// TestDatabaseCacheHit_ScanErrorSkipsRow seeds two samples with a NULL
// datname so that a valid delta row is emitted whose datname cannot be
// scanned into a string; the row must be skipped without creating an
// entry or disturbing the other database.
func TestDatabaseCacheHit_ScanErrorSkipsRow(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	const connID = 515
	ctx := context.Background()
	for i, hit := range []int64{100, 200} {
		if _, err := pool.Exec(ctx, `INSERT INTO metrics.pg_stat_database
            (connection_id, collected_at, datname, blks_hit, blks_read)
            VALUES ($1, $2, NULL, $3, 10)`, connID,
			cacheHitBase.Add(time.Duration(i)*time.Minute), hit); err != nil {
			t.Fatalf("insert NULL datname row: %v", err)
		}
	}
	seedCacheHitSamples(t, pool, connID, []cacheHitSample{
		{0, "db", 100, 0},
		{1, "db", 200, 0},
	})

	dbMap := runQueryDatabaseCacheHit(t, h, pool, connID, "db")

	if len(dbMap) != 1 {
		t.Fatalf("scan failures must not create entries; got %#v", dbMap)
	}
	db := dbMap["db"]
	if len(db.CacheHitRatio.TimeSeries) != 1 {
		t.Fatalf("len(TimeSeries) = %d, want 1", len(db.CacheHitRatio.TimeSeries))
	}
	assertRatio(t, "current", db.CacheHitRatio.Current, 100.0)
}

func TestAggregateCacheHitRatio(t *testing.T) {
	tests := []struct {
		name    string
		hit     float64
		read    float64
		wantNil bool
		wantVal float64
	}{
		{name: "idle", hit: 0, read: 0, wantNil: true},
		{name: "all hits", hit: 10, read: 0, wantVal: 100.0},
		{name: "all reads", hit: 0, read: 10, wantVal: 0.0},
		{name: "rounded to one place", hit: 2, read: 1, wantVal: 66.7},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := aggregateCacheHitRatio(tc.hit, tc.read)
			if tc.wantNil {
				assertNilRatio(t, "ratio", got)
				return
			}
			assertRatio(t, "ratio", got, tc.wantVal)
		})
	}
}
