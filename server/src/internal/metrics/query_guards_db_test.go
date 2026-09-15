/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package metrics

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// guardTestProbe is shaped like pg_stat_database with a stats_reset marker,
// keyed per datname, for the reset guard, gap rejection, fill policy and
// bucket clamp introduced by issue #402.
const guardTestProbe = "pg_stat_database_guard_test"

// setupGuardFixture creates the probe table and registers xact_commit as a
// counter guarded by stats_reset, with a one-minute global interval and a
// five-minute server-scope interval for connection 4. One scenario per
// connection, offsets in minutes from the returned base:
//
//	conn 1  -4  1000 (reset A)   -3  1060 (A)   -2  1120 (reset B)   -1  1180 (B)
//	        the marker changes at -2 min whilst the counter keeps rising
//	conn 2  -10  0   -6  240   -4  360   -3  420
//	        a four-minute gap (rejected), then a two-minute gap (accepted)
//	conn 3  -10  numbackends 5, nothing afterwards
//	        a gauge that stops reporting
//	conn 4  no rows; its server-scope interval is 300s for the bucket clamp
func setupGuardFixture(t *testing.T, pool *pgxpool.Pool) (time.Time, func()) {
	t.Helper()
	ctx := context.Background()

	if _, err := pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS metrics"); err != nil {
		t.Fatalf("failed to create metrics schema: %v", err)
	}

	dropTable(ctx, pool, guardTestProbe)
	registerProbeKindsForTest(t, guardTestProbe, probeRegistryEntry{
		kinds:       columnKinds(counters("xact_commit")),
		resetColumn: fixedReset("stats_reset"),
	})
	setProbeIntervalForTest(t, pool, guardTestProbe, 0, 60)
	setProbeIntervalForTest(t, pool, guardTestProbe, 4, 300)

	ddl := `CREATE TABLE metrics."` + guardTestProbe + `" (
        connection_id integer NOT NULL,
        collected_at  timestamp with time zone NOT NULL,
        inserted_at   timestamp without time zone NOT NULL DEFAULT now(),
        datname       text NOT NULL,
        numbackends   integer,
        xact_commit   bigint,
        stats_reset   timestamp with time zone,
        PRIMARY KEY (connection_id, collected_at, datname)
    )`
	if _, err := pool.Exec(ctx, ddl); err != nil {
		t.Fatalf("failed to create guard fixture table: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Minute)
	resetA := now.Add(-time.Hour)
	resetB := now.Add(-150 * time.Second)
	samples := []struct {
		conn        int
		offset      time.Duration
		numbackends int
		xactCommit  int64
		reset       *time.Time
	}{
		{1, -4 * time.Minute, 3, 1000, &resetA},
		{1, -3 * time.Minute, 3, 1060, &resetA},
		{1, -2 * time.Minute, 3, 1120, &resetB},
		{1, -1 * time.Minute, 3, 1180, &resetB},

		{2, -10 * time.Minute, 2, 0, nil},
		{2, -6 * time.Minute, 2, 240, nil},
		{2, -4 * time.Minute, 2, 360, nil},
		{2, -3 * time.Minute, 2, 420, nil},

		{3, -10 * time.Minute, 5, 0, nil},
	}

	insert := `INSERT INTO metrics."` + guardTestProbe + `"
        (connection_id, collected_at, datname, numbackends, xact_commit, stats_reset)
        VALUES ($1, $2, 'northwind', $3, $4, $5)`
	for i, s := range samples {
		_, err := pool.Exec(ctx, insert,
			s.conn, now.Add(s.offset), s.numbackends, s.xactCommit, s.reset)
		if err != nil {
			dropTable(ctx, pool, guardTestProbe)
			t.Fatalf("failed to insert guard fixture sample %d: %v", i, err)
		}
	}

	return now, func() { dropTable(context.Background(), pool, guardTestProbe) }
}

func TestQueryTimeSeriesGuards_Integration(t *testing.T) {
	pool, closePool := newLatestRowsTestPool(t)
	defer closePool()
	base, cleanup := setupGuardFixture(t, pool)
	defer cleanup()

	ctx := context.Background()
	minutes := func(n int) time.Time { return base.Add(-time.Duration(n) * time.Minute) }

	t.Run("reset marker change nulls the rate and zeroes the delta", func(t *testing.T) {
		series, err := QueryTimeSeries(ctx, pool, guardTestProbe,
			[]int{1}, windowSince(base, 4), MetricFilters{}, 4, "avg",
			[]string{"xact_commit_per_sec", "xact_commit_delta"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		rate := seriesByMetric(t, series, "xact_commit_per_sec")
		delta := seriesByMetric(t, series, "xact_commit_delta")

		// The counter rose 60 between -3 and -2 min, but the marker moved
		// from A to B, so that interval is a reset: no rate, and a delta
		// of 0 rather than 60. The intervals either side are clean.
		assertNullAt(t, rate, minutes(4))
		if got := pointAt(t, rate, minutes(3)); got != 1 {
			t.Errorf("rate at -3 min = %v, want 1", got)
		}
		assertNullAt(t, rate, minutes(2))
		if got := pointAt(t, rate, minutes(1)); got != 1 {
			t.Errorf("rate at -1 min = %v, want 1", got)
		}

		// The -4 min sample has no predecessor inside the window, so its
		// delta is 0; the reset interval contributes 0 rather than 60.
		want := map[int]float64{4: 0, 3: 60, 2: 0, 1: 60}
		for m, v := range want {
			if got := pointAt(t, delta, minutes(m)); got != v {
				t.Errorf("delta at -%d min = %v, want %v", m, got, v)
			}
		}
		// Nothing has been collected since -1 min, so the bucket at the
		// window's end is not spanned by any accepted interval and its
		// events are unknown rather than zero.
		assertNullAt(t, delta, minutes(0))
	})

	t.Run("gap wider than three intervals is rejected, two intervals accepted", func(t *testing.T) {
		series, err := QueryTimeSeries(ctx, pool, guardTestProbe,
			[]int{2}, windowSince(base, 10), MetricFilters{}, 10, "avg",
			[]string{"xact_commit_per_sec", "xact_commit_delta"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		rate := seriesByMetric(t, series, "xact_commit_per_sec")
		delta := seriesByMetric(t, series, "xact_commit_delta")

		// -6 min: four minutes since the previous sample, beyond three
		// intervals, so the rate is null and the delta bucket is a gap
		// (null), not 0. -4 min: two minutes since -6, accepted, so the
		// 120 rise and its 1/s rate both land.
		assertNullAt(t, rate, minutes(6))
		assertNullAt(t, delta, minutes(6))
		if got := pointAt(t, rate, minutes(4)); got != 1 {
			t.Errorf("rate at -4 min = %v, want 1", got)
		}
		if got := pointAt(t, delta, minutes(4)); got != 120 {
			t.Errorf("delta at -4 min = %v, want 120", got)
		}
		if got := pointAt(t, delta, minutes(3)); got != 60 {
			t.Errorf("delta at -3 min = %v, want 60", got)
		}

		// The first sample lands at -10 min with no predecessor, so its
		// own bucket reads 0; -5 min holds no sample but lies inside the
		// accepted interval from -6 to -4 min, whose events the -4 min
		// delta already reports, so it reads 0 too.
		for _, m := range []int{10, 5} {
			if got := pointAt(t, delta, minutes(m)); got != 0 {
				t.Errorf("delta at -%d min = %v, want 0", m, got)
			}
		}
		// The sample-less buckets inside the four-minute outage, and those
		// after the last sample, are spanned by no accepted interval:
		// nothing counts their events anywhere, so they are null exactly
		// as the rate is rather than three confident zero bars.
		for _, m := range []int{9, 8, 7, 2, 1, 0} {
			assertNullAt(t, delta, minutes(m))
		}
		// The rate never carries: every bucket without an accepted
		// interval is null for it.
		for _, m := range []int{10, 9, 8, 7, 5, 2, 1, 0} {
			assertNullAt(t, rate, minutes(m))
		}
		if got := len(nonNull(rate)); got != 2 {
			t.Errorf("rate has %d values, want 2 (never carried forward)", got)
		}
	})

	t.Run("gauge carries for three intervals then goes null", func(t *testing.T) {
		series, err := QueryTimeSeries(ctx, pool, guardTestProbe,
			[]int{3}, windowSince(base, 10), MetricFilters{}, 10, "avg",
			[]string{"numbackends"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		s := seriesByMetric(t, series, "numbackends")
		for _, m := range []int{10, 9, 8, 7} {
			if got := pointAt(t, s, minutes(m)); got != 5 {
				t.Errorf("numbackends at -%d min = %v, want 5 (carried)", m, got)
			}
		}
		for _, m := range []int{6, 5, 4, 3, 2, 1, 0} {
			assertNullAt(t, s, minutes(m))
		}
	})

	t.Run("every series has one point per bucket", func(t *testing.T) {
		series, err := QueryTimeSeries(ctx, pool, guardTestProbe,
			[]int{2, 3}, windowSince(base, 10), MetricFilters{}, 10, "avg",
			[]string{"numbackends", "xact_commit_per_sec", "xact_commit_delta"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(series) != 6 {
			t.Fatalf("got %d series, want 6 (three metrics for two connections)", len(series))
		}
		// A 10-minute window in 60-second buckets: generate_series is
		// inclusive of the end, so 11 points.
		for _, s := range series {
			if len(s.Data) != 11 {
				t.Errorf("%s has %d points, want 11", s.Name, len(s.Data))
				continue
			}
			for i, p := range s.Data {
				if want := series[0].Data[i].Time; !p.Time.Equal(want) {
					t.Errorf("%s point %d at %s, want %s", s.Name, i, p.Time, want)
				}
			}
		}
	})

	t.Run("buckets are clamped to the probe interval", func(t *testing.T) {
		// Connection 4 runs the probe every 300 s, so a one-hour window
		// holds at most 12 buckets however many are requested: 13 points
		// (the inclusive end) spaced 300 s apart.
		window := windowSince(base, 60)
		series, err := QueryTimeSeries(ctx, pool, guardTestProbe,
			[]int{4}, window, MetricFilters{}, 150, "avg",
			[]string{"numbackends", "xact_commit_per_sec"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, s := range series {
			if len(s.Data) != 13 {
				t.Errorf("%s has %d points, want 13 (12 buckets plus the end)",
					s.Metric, len(s.Data))
				continue
			}
			for i := 1; i < len(s.Data); i++ {
				if got := s.Data[i].Time.Sub(s.Data[i-1].Time); got != 300*time.Second {
					t.Errorf("%s bucket %d is %v wide, want 5m0s", s.Metric, i, got)
				}
			}
			if !s.Data[0].Time.Equal(window.Start) {
				t.Errorf("%s starts at %s, want %s", s.Metric, s.Data[0].Time, window.Start)
			}
		}
	})

	t.Run("tightening the interval does not blank older history", func(t *testing.T) {
		// Issue #402 review: the gap bound came from the probe_configs
		// row as it reads now, but the stored samples were taken at
		// whatever interval was configured when they were collected. With
		// the minute-spaced samples of connection 1 judged against a
		// ten-second interval, every one of them looked like a four-fold
		// collection gap and the whole series went blank.
		window := windowSince(base, 4)
		query := func(t *testing.T, metrics ...string) []MetricSeries {
			t.Helper()
			series, err := QueryTimeSeries(ctx, pool, guardTestProbe,
				[]int{1}, window, MetricFilters{}, 4, "avg", metrics)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			return series
		}

		before := query(t, "xact_commit_per_sec", "xact_commit_delta")
		rateBefore := nonNull(seriesByMetric(t, before, "xact_commit_per_sec"))
		if len(rateBefore) != 2 {
			t.Fatalf("rate has %d values at the configured minute, want 2",
				len(rateBefore))
		}

		// Tighten the probe to ten seconds, leaving the samples untouched.
		if _, err := pool.Exec(ctx,
			`UPDATE probe_configs SET collection_interval_seconds = 10
             WHERE name = $1 AND scope = 'global'`, guardTestProbe); err != nil {
			t.Fatalf("failed to tighten the probe interval: %v", err)
		}
		defer func() {
			if _, err := pool.Exec(context.Background(),
				`UPDATE probe_configs SET collection_interval_seconds = 60
                 WHERE name = $1 AND scope = 'global'`, guardTestProbe); err != nil {
				t.Fatalf("failed to restore the probe interval: %v", err)
			}
		}()

		after := query(t, "xact_commit_per_sec", "xact_commit_delta")
		rateAfter := nonNull(seriesByMetric(t, after, "xact_commit_per_sec"))
		if len(rateAfter) != len(rateBefore) {
			t.Errorf("rate has %d values after tightening the interval, want %d",
				len(rateAfter), len(rateBefore))
		}
		for i := range rateAfter {
			if i >= len(rateBefore) {
				break
			}
			if !rateAfter[i].Time.Equal(rateBefore[i].Time) ||
				*rateAfter[i].Value != *rateBefore[i].Value {
				t.Errorf("rate point %d = %v at %s, want %v at %s", i,
					*rateAfter[i].Value, rateAfter[i].Time,
					*rateBefore[i].Value, rateBefore[i].Time)
			}
		}

		// The delta keeps its real increments rather than collapsing to
		// the structural zeros a rejected interval leaves behind.
		delta := seriesByMetric(t, after, "xact_commit_delta")
		for _, m := range []int{3, 1} {
			if got := pointAt(t, delta, minutes(m)); got != 60 {
				t.Errorf("delta at -%d min = %v, want 60", m, got)
			}
		}

		// The gauge carry has the same dependency: three ten-second
		// intervals would drop numbackends one bucket after the last
		// sample, whilst the samples themselves are a minute apart.
		gauge := seriesByMetric(t, query(t, "numbackends"), "numbackends")
		if got := pointAt(t, gauge, minutes(0)); got != 3 {
			t.Errorf("numbackends at the window end = %v, want 3 (carried)", got)
		}
	})

	t.Run("a window shorter than the interval keeps one bucket", func(t *testing.T) {
		window := TimeWindow{Start: base.Add(-2 * time.Minute), End: base}
		series, err := QueryTimeSeries(ctx, pool, guardTestProbe,
			[]int{4}, window, MetricFilters{}, 150, "avg",
			[]string{"numbackends"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		s := seriesByMetric(t, series, "numbackends")
		if len(s.Data) != 2 {
			t.Errorf("got %d points, want 2 (one bucket plus the end)", len(s.Data))
		}
	})
}
