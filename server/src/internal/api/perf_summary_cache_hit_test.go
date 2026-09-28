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
	if err := h.queryDatabaseCacheHitTimeSeries(ctx, tx, connID, cacheHitBase,
		cacheHitBase.Add(time.Hour), "60 seconds", dbMap); err != nil {
		t.Fatalf("queryDatabaseCacheHitTimeSeries failed: %v", err)
	}
	return dbMap
}

// runQueryDatabaseCacheHitErr is runQueryDatabaseCacheHit for the failure
// cases: it returns the helper's error alongside the summaries instead of
// failing the test on it.
func runQueryDatabaseCacheHitErr(
	t *testing.T,
	h *PerfSummaryHandler,
	pool *pgxpool.Pool,
	connID int,
	datname string,
) (*DatabaseSummary, error) {
	t.Helper()
	ctx := context.Background()
	tx := mustTx(t, pool)
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after read is a no-op

	db := &DatabaseSummary{
		DatabaseName:  datname,
		CacheHitRatio: CacheHitRatioData{TimeSeries: []CacheHitRatioPoint{}},
	}
	err := h.queryDatabaseCacheHitTimeSeries(ctx, tx, connID, cacheHitBase,
		cacheHitBase.Add(time.Hour), "60 seconds",
		map[string]*DatabaseSummary{datname: db})
	return db, err
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
	// Two databases per sample so the summing of per-database deltas
	// into a bucket is exercised; both follow the same healthy-then-poor
	// shape.
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

// TestQueryCacheHit_NewDatabaseExcludedFromInterval seeds a database
// that first appears in the latest sample with large lifetime counters.
// Deltas are taken per database, so it has no predecessor and must not
// be counted as block activity in that interval; the ratio comes from
// the existing database alone.
func TestQueryCacheHit_NewDatabaseExcludedFromInterval(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	const connID = 509
	seedCacheHitSamples(t, pool, connID, []cacheHitSample{
		{0, "old", 1000, 1000},
		{1, "old", 1900, 1100},       // +900/+100 -> 90%
		{1, "new", 1000000, 1000000}, // created since minute 0
	})

	hit, read, current, points := runQueryCacheHit(t, h, pool, connID)

	if len(points) != 1 {
		t.Fatalf("len(points) = %d, want 1: %#v", len(points), points)
	}
	assertRatio(t, "points[0]", points[0].Value, 90.0)
	assertRatio(t, "current", current, 90.0)
	if hit != 900 || read != 100 {
		t.Errorf("latest deltas = (%v, %v), want (900, 100)", hit, read)
	}
}

// TestQueryCacheHit_DroppedDatabaseKeepsInterval seeds a database that
// disappears between two samples. Summing the counters across databases
// first would make the interval's total fall and discard the sample;
// per-database deltas keep the interval for the surviving database.
func TestQueryCacheHit_DroppedDatabaseKeepsInterval(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	const connID = 510
	seedCacheHitSamples(t, pool, connID, []cacheHitSample{
		{0, "keep", 1000, 1000},
		{0, "gone", 1000000, 1000000}, // dropped before minute 1
		{1, "keep", 1900, 1100},       // +900/+100 -> 90%
	})

	hit, read, current, points := runQueryCacheHit(t, h, pool, connID)

	if len(points) != 1 {
		t.Fatalf("len(points) = %d, want 1 (interval kept): %#v",
			len(points), points)
	}
	assertRatio(t, "points[0]", points[0].Value, 90.0)
	assertRatio(t, "current", current, 90.0)
	if hit != 900 || read != 100 {
		t.Errorf("latest deltas = (%v, %v), want (900, 100)", hit, read)
	}
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

// resetWhileOtherActiveSamples seeds two databases sampled at the same
// minutes. "steady" adds 900 hits and 100 reads per interval throughout.
// "reset" has its counters fall between minute 1 and minute 2 (a stats
// reset), then adds 50 hits and 50 reads in the final interval. This is
// the one case in which discarding the reset per database row differs
// from discarding it per bucket: the minute 2 bucket must keep steady's
// 90% delta whilst dropping reset's negative one, and the minute 3
// bucket sums both databases again.
func resetWhileOtherActiveSamples() []cacheHitSample {
	return []cacheHitSample{
		{0, "steady", 1000, 1000},
		{1, "steady", 1900, 1100},
		{2, "steady", 2800, 1200},
		{3, "steady", 3700, 1300},
		{0, "reset", 5000, 500},
		{1, "reset", 5900, 600},
		{2, "reset", 10, 1},  // stats reset: negative delta, discarded
		{3, "reset", 60, 51}, // +50/+50 -> 50%
	}
}

// TestQueryCacheHit_ResetInOneDatabaseKeepsOther checks that a stats
// reset in one database does not discard the bucket for a database that
// stayed active in the same interval, and that the reset database
// rejoins the sum once it has a valid delta again.
func TestQueryCacheHit_ResetInOneDatabaseKeepsOther(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	const connID = 516
	seedCacheHitSamples(t, pool, connID, resetWhileOtherActiveSamples())

	hit, read, current, points := runQueryCacheHit(t, h, pool, connID)

	if len(points) != 3 {
		t.Fatalf("len(points) = %d, want 3: %#v", len(points), points)
	}
	// Minute 1: both databases at 90%.
	assertRatio(t, "points[0]", points[0].Value, 90.0)
	// Minute 2: steady alone (900/100), reset's row discarded.
	assertRatio(t, "points[1]", points[1].Value, 90.0)
	// Minute 3: steady 900/100 plus reset 50/50 -> 950/1100 = 86.36%.
	assertRatio(t, "points[2]", points[2].Value, 86.36)
	assertRatio(t, "current", current, 86.36)
	if hit != 950 || read != 150 {
		t.Errorf("latest deltas = (%v, %v), want (950, 150)", hit, read)
	}
	assertSaneRatios(t, points)
}

// TestDatabaseCacheHit_ResetInOneDatabaseKeepsOther is the per-database
// counterpart: the steady database keeps every bucket, the reset
// database loses only the bucket in which its counters fell.
func TestDatabaseCacheHit_ResetInOneDatabaseKeepsOther(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	const connID = 517
	seedCacheHitSamples(t, pool, connID, resetWhileOtherActiveSamples())

	dbMap := runQueryDatabaseCacheHit(t, h, pool, connID, "steady", "reset")

	steady := dbMap["steady"].CacheHitRatio
	if len(steady.TimeSeries) != 3 {
		t.Fatalf("steady: len(TimeSeries) = %d, want 3: %#v",
			len(steady.TimeSeries), steady.TimeSeries)
	}
	for i, pt := range steady.TimeSeries {
		assertRatio(t, fmt.Sprintf("steady points[%d]", i), pt.Value, 90.0)
	}
	assertRatio(t, "steady current", steady.Current, 90.0)

	reset := dbMap["reset"].CacheHitRatio
	if len(reset.TimeSeries) != 2 {
		t.Fatalf("reset: len(TimeSeries) = %d, want 2 (reset bucket "+
			"discarded): %#v", len(reset.TimeSeries), reset.TimeSeries)
	}
	assertRatio(t, "reset points[0]", reset.TimeSeries[0].Value, 90.0)
	assertRatio(t, "reset points[1]", reset.TimeSeries[1].Value, 50.0)
	assertRatio(t, "reset current", reset.Current, 50.0)
	// The reset bucket is minute 2, so the surviving points are minutes
	// 1 and 3.
	if got := reset.TimeSeries[1].Time.Sub(reset.TimeSeries[0].Time); got != 2*time.Minute {
		t.Errorf("reset bucket spacing = %v, want 2m", got)
	}
}

// installFailingCacheHitView replaces metrics.pg_stat_database with a
// view over a renamed copy of the table whose blks_read column divides
// by (blks_read - blks_read). The planner cannot fold that to a
// constant, so the query prepares cleanly and only fails at execution,
// which is the path that reaches rows.Err rather than the tx.Query
// error return. Samples are seeded so that the failing expression is
// actually evaluated. The returned function restores the schema.
func installFailingCacheHitView(t *testing.T, pool *pgxpool.Pool, connID int) func() {
	t.Helper()
	ctx := context.Background()
	stmts := []string{
		`ALTER TABLE metrics.pg_stat_database RENAME TO pg_stat_database_src`,
		`CREATE VIEW metrics.pg_stat_database AS
            SELECT connection_id, collected_at, datname, blks_hit,
                   blks_read / (blks_read - blks_read) AS blks_read
            FROM metrics.pg_stat_database_src`,
		`INSERT INTO metrics.pg_stat_database_src
            (connection_id, collected_at, datname, blks_hit, blks_read)
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

// TestQueryCacheHit_ExecutionErrorReturnsNoData drives the rows.Err
// branch of queryCacheHit. A scan failure is not reachable with real
// rows, because every output column is an explicit float cast or a
// date_bin over a bounded timestamptz, so the execution-time failure is
// the path that exercises the post-loop error handling; the result must
// be the same empty shape as a query that failed to prepare.
func TestQueryCacheHit_ExecutionErrorReturnsNoData(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	const connID = 518
	restore := installFailingCacheHitView(t, pool, connID)
	defer restore()

	hit, read, current, points := runQueryCacheHit(t, h, pool, connID)

	if points == nil || len(points) != 0 {
		t.Errorf("points = %#v, want empty non-nil slice", points)
	}
	assertNilRatio(t, "current", current)
	if hit != 0 || read != 0 {
		t.Errorf("latest deltas = (%v, %v), want (0, 0)", hit, read)
	}
}

// TestDatabaseCacheHit_ExecutionErrorLeavesEntriesEmpty is the
// per-database counterpart of the execution-error test. Unlike
// queryCacheHit, this helper returns the failure, so that the
// database-summaries endpoint can report it (issue #519).
func TestDatabaseCacheHit_ExecutionErrorLeavesEntriesEmpty(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	const connID = 519
	restore := installFailingCacheHitView(t, pool, connID)
	defer restore()

	db, err := runQueryDatabaseCacheHitErr(t, h, pool, connID, "db")

	if err == nil || isUndefinedTableError(err) {
		t.Errorf("err = %v, want a non-undefined_table error", err)
	}
	if len(db.CacheHitRatio.TimeSeries) != 0 {
		t.Errorf("TimeSeries = %#v, want empty", db.CacheHitRatio.TimeSeries)
	}
	assertNilRatio(t, "current", db.CacheHitRatio.Current)
}

// TestDatabaseCacheHit_QueryError drops the metrics tables so that the
// query fails to prepare, mirroring TestQueryCacheHit_QueryError.
func TestDatabaseCacheHit_QueryError(t *testing.T) {
	h, pool, cleanup := newDatabaseSummariesTestHandler(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		databaseSummariesTestSchemaTeardown); err != nil {
		t.Fatalf("teardown for query-error test failed: %v", err)
	}

	db, err := runQueryDatabaseCacheHitErr(t, h, pool, 520, "db")

	if !isUndefinedTableError(err) {
		t.Errorf("err = %v, want an undefined_table error", err)
	}
	if len(db.CacheHitRatio.TimeSeries) != 0 {
		t.Errorf("TimeSeries = %#v, want empty", db.CacheHitRatio.TimeSeries)
	}
	assertNilRatio(t, "current", db.CacheHitRatio.Current)
}
