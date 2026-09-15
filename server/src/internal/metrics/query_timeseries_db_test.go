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
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// These integration tests exercise the DB-executing time-series query path,
// covering both the raw-column branch and the derived-metric branch
// (per-second rates and the dead-tuple ratio) of QueryTimeSeries, together
// with the shared scanSeriesRows helper. They follow the same gating
// convention as query_db_test.go: they connect to TEST_AI_WORKBENCH_SERVER,
// skip cleanly when it is unset or SKIP_DB_TESTS is set, and skip on any
// connection or ping failure.

const timeSeriesTestProbe = "pg_stat_all_tables_ts_test"

// lastHourWindow returns the resolved window these tests query over. It
// matches what the HTTP boundary produces for time_range=1h, which is the
// window the fixture samples are placed inside.
func lastHourWindow() TimeWindow {
	now := time.Now().UTC()
	return TimeWindow{Start: now.Add(-time.Hour), End: now}
}

// setupTimeSeriesFixture creates the metrics schema (if absent) and a probe
// table carrying the counter and tuple columns the table dashboards depend
// on. It inserts five connection-1 samples spaced one minute apart across the
// last few minutes so they fall inside a "1h" query window. The cumulative
// counters advance by a fixed amount per minute, giving a clean 1.0-per-second
// rate, whilst n_live_tup and n_dead_tup stay constant at 90 and 10 so the
// dead-tuple ratio is a steady 10 percent. The earliest sample, five minutes
// back, sits outside the narrower windows the lookback tests use and so
// serves as the pre-window sample that feeds the LAG.
//
// It returns the minute-truncated base time the sample offsets are relative
// to, so tests can build windows that line up with the samples exactly
// rather than re-reading the clock and risking a minute rollover, together
// with a cleanup that drops the table.
func setupTimeSeriesFixture(t *testing.T, pool *pgxpool.Pool) (time.Time, func()) {
	t.Helper()
	ctx := context.Background()

	if _, err := pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS metrics"); err != nil {
		t.Fatalf("failed to create metrics schema: %v", err)
	}

	dropTable(ctx, pool, timeSeriesTestProbe)
	// The fixture table is not a real probe, so it has to register its
	// counters itself or every _per_sec request is refused as a gauge.
	registerProbeKindsForTest(t, timeSeriesTestProbe,
		countersForTest("seq_scan", "idx_scan", "n_tup_ins"))
	setProbeIntervalForTest(t, pool, timeSeriesTestProbe, 0, 60)

	// indexrelname carries the index dimension so the IndexName filter can be
	// exercised end-to-end, mirroring the pg_stat_all_indexes probe shape.
	ddl := `CREATE TABLE metrics."` + timeSeriesTestProbe + `" (
        connection_id integer NOT NULL,
        collected_at  timestamp with time zone NOT NULL,
        inserted_at   timestamp without time zone NOT NULL DEFAULT now(),
        database_name text,
        schemaname    name,
        relname       name,
        indexrelname  name,
        seq_scan      bigint,
        idx_scan      bigint,
        n_tup_ins     bigint,
        n_live_tup    bigint,
        n_dead_tup    bigint
    )`
	if _, err := pool.Exec(ctx, ddl); err != nil {
		t.Fatalf("failed to create fixture table: %v", err)
	}

	// Bucket boundaries in QueryTimeSeries are anchored at the window start
	// (now-1h) with a 60-second width, so minute-aligned samples fall cleanly
	// into consecutive buckets. Each counter rises by 60 per minute, i.e. a
	// rate of exactly 1.0 per second between adjacent samples.
	now := time.Now().UTC().Truncate(time.Minute)
	type sample struct {
		offset  time.Duration
		seqScan int64
		idxScan int64
		nTupIns int64
	}
	samples := []sample{
		{-5 * time.Minute, 40, 940, 60},
		{-4 * time.Minute, 100, 1000, 120},
		{-3 * time.Minute, 160, 1060, 180},
		{-2 * time.Minute, 220, 1120, 240},
		{-1 * time.Minute, 280, 1180, 300},
	}

	insert := `INSERT INTO metrics."` + timeSeriesTestProbe + `"
        (connection_id, collected_at, database_name, schemaname, relname,
         indexrelname, seq_scan, idx_scan, n_tup_ins, n_live_tup, n_dead_tup)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
	for i, s := range samples {
		_, err := pool.Exec(ctx, insert,
			1, now.Add(s.offset), "northwind", "public", "orders",
			"pk_orders", s.seqScan, s.idxScan, s.nTupIns, 90, 10)
		if err != nil {
			dropTable(ctx, pool, timeSeriesTestProbe)
			t.Fatalf("failed to insert fixture sample %d: %v", i, err)
		}
	}

	return now, func() { dropTable(context.Background(), pool, timeSeriesTestProbe) }
}

// windowSince returns the window running from minutes before base up to
// base. Fixture samples are placed at whole-minute offsets from the same
// base, so the window start lands exactly on a sample and the lookback
// tests can say precisely which samples are inside the window and which one
// sits just before it.
func windowSince(base time.Time, minutes int) TimeWindow {
	return TimeWindow{
		Start: base.Add(-time.Duration(minutes) * time.Minute),
		End:   base,
	}
}

func TestScanSeriesRows_Integration(t *testing.T) {
	pool, closePool := newLatestRowsTestPool(t)
	defer closePool()

	gauge := []fillPolicy{fillGauge}
	none := []fillPolicy{fillNone}

	t.Run("query error surfaces", func(t *testing.T) {
		// A canceled context fails when the pool tries to acquire a
		// connection, so pool.Query returns an error before any rows scan.
		cctx, cancel := context.WithCancel(context.Background())
		cancel()
		dataMap := map[seriesKey][]MetricDataPoint{}
		err := scanSeriesRows(cctx, pool, "SELECT now(), 1", nil,
			[]string{"m"}, gauge, time.Minute, 1, dataMap)
		if err == nil {
			t.Fatal("expected error from canceled context")
		}
	})

	t.Run("scan error on destination mismatch", func(t *testing.T) {
		// Three result columns but only two scan destinations (bucket time
		// plus one metric) forces a scan destination mismatch.
		dataMap := map[seriesKey][]MetricDataPoint{}
		err := scanSeriesRows(context.Background(), pool, "SELECT now(), 1, 2",
			nil, []string{"only_one"}, gauge, time.Minute, 1, dataMap)
		if err == nil {
			t.Fatal("expected scan error for mismatched destination count")
		}
	})

	// Seven minute-spaced buckets: a value, then five NULLs, then a value.
	// With a one-minute interval a gauge may be carried for three buckets.
	const sevenBuckets = `SELECT b, v FROM (VALUES
            (now() - interval '6 min', 5::float8),
            (now() - interval '5 min', NULL::float8),
            (now() - interval '4 min', NULL::float8),
            (now() - interval '3 min', NULL::float8),
            (now() - interval '2 min', NULL::float8),
            (now() - interval '1 min', NULL::float8),
            (now(),                    9::float8)
        ) AS t(b, v) ORDER BY b`

	t.Run("gauge carries for three intervals then goes null", func(t *testing.T) {
		dataMap := map[seriesKey][]MetricDataPoint{}
		err := scanSeriesRows(context.Background(), pool, sevenBuckets, nil,
			[]string{"m"}, gauge, time.Minute, 1, dataMap)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := valuesOf(dataMap[seriesKey{metric: "m", connectionID: 1}])
		want := []*float64{f(5), f(5), f(5), f(5), nil, nil, f(9)}
		assertValues(t, got, want)
	})

	t.Run("rate never carries", func(t *testing.T) {
		dataMap := map[seriesKey][]MetricDataPoint{}
		err := scanSeriesRows(context.Background(), pool, sevenBuckets, nil,
			[]string{"m"}, none, time.Minute, 1, dataMap)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := valuesOf(dataMap[seriesKey{metric: "m", connectionID: 1}])
		want := []*float64{f(5), nil, nil, nil, nil, nil, f(9)}
		assertValues(t, got, want)
	})

	t.Run("leading NULL with no prior value is emitted as null", func(t *testing.T) {
		// A NULL in the very first bucket has no prior value to carry, so it
		// is emitted as a null point rather than dropped: every series in a
		// response holds one point per bucket.
		dataMap := map[seriesKey][]MetricDataPoint{}
		query := `SELECT b, v FROM (VALUES
            (now() - interval '1 min', NULL::float8),
            (now(),                    7::float8)
        ) AS t(b, v) ORDER BY b`
		err := scanSeriesRows(context.Background(), pool, query, nil,
			[]string{"m"}, gauge, time.Minute, 1, dataMap)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := valuesOf(dataMap[seriesKey{metric: "m", connectionID: 1}])
		assertValues(t, got, []*float64{nil, f(7)})
	})

	t.Run("non-finite sample is a gap", func(t *testing.T) {
		// NaN cannot be marshaled to JSON; it is treated exactly like a
		// NULL bucket, so a gauge carries across it and a rate shows a gap.
		dataMap := map[seriesKey][]MetricDataPoint{}
		query := `SELECT b, v FROM (VALUES
            (now() - interval '1 min', 4::float8),
            (now(),                    'NaN'::float8)
        ) AS t(b, v) ORDER BY b`
		err := scanSeriesRows(context.Background(), pool, query, nil,
			[]string{"g", "r"}, []fillPolicy{fillGauge, fillNone},
			time.Minute, 1, dataMap)
		if err == nil {
			// Two names but one value column: the scan must fail. Guard
			// against a silent pass below by asserting the error here.
			t.Fatal("expected a scan error for the mismatched column count")
		}
		dataMap = map[seriesKey][]MetricDataPoint{}
		query = `SELECT b, v, v FROM (VALUES
            (now() - interval '1 min', 4::float8),
            (now(),                    'NaN'::float8)
        ) AS t(b, v) ORDER BY b`
		err = scanSeriesRows(context.Background(), pool, query, nil,
			[]string{"g", "r"}, []fillPolicy{fillGauge, fillNone},
			time.Minute, 1, dataMap)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertValues(t, valuesOf(dataMap[seriesKey{metric: "g", connectionID: 1}]),
			[]*float64{f(4), f(4)})
		assertValues(t, valuesOf(dataMap[seriesKey{metric: "r", connectionID: 1}]),
			[]*float64{f(4), nil})
	})
}

// f returns a pointer to v, for building expected point values.
func f(v float64) *float64 { return &v }

// valuesOf returns the point values of data in order.
func valuesOf(data []MetricDataPoint) []*float64 {
	out := make([]*float64, len(data))
	for i, p := range data {
		out[i] = p.Value
	}
	return out
}

// assertValues fails the test unless got and want hold the same values,
// nil for nil, at every position.
func assertValues(t *testing.T, got, want []*float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d points, want %d: %s", len(got), len(want), fmtValues(got))
	}
	for i := range want {
		switch {
		case got[i] == nil && want[i] == nil:
		case got[i] == nil || want[i] == nil || *got[i] != *want[i]:
			t.Errorf("point[%d] = %s, want %s (all: %s)",
				i, fmtValue(got[i]), fmtValue(want[i]), fmtValues(got))
		}
	}
}

func fmtValue(v *float64) string {
	if v == nil {
		return "null"
	}
	return fmt.Sprintf("%v", *v)
}

func fmtValues(vs []*float64) string {
	parts := make([]string, len(vs))
	for i, v := range vs {
		parts[i] = fmtValue(v)
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// val returns the value of p, failing the test when the point is null.
func val(t *testing.T, s MetricSeries, p MetricDataPoint) float64 {
	t.Helper()
	if p.Value == nil {
		t.Fatalf("series %q point at %s is null, want a value", s.Metric, p.Time)
	}
	return *p.Value
}

// nonNull returns the non-null points of s in order.
func nonNull(s MetricSeries) []MetricDataPoint {
	var out []MetricDataPoint
	for _, p := range s.Data {
		if p.Value != nil {
			out = append(out, p)
		}
	}
	return out
}

// assertAllNull fails the test unless s holds at least one point and every
// point is null: the shape of a series whose query matched no rows.
func assertAllNull(t *testing.T, s MetricSeries) {
	t.Helper()
	if len(s.Data) == 0 {
		t.Fatalf("series %q has no points; every bucket should be emitted", s.Metric)
	}
	if got := nonNull(s); len(got) != 0 {
		t.Errorf("series %q has %d non-null points, want none: %s",
			s.Metric, len(got), fmtValues(valuesOf(got)))
	}
}

// sumValues returns the sum of the non-null point values of s, failing the
// test on a negative value, along with the non-zero values in order.
func sumValues(t *testing.T, s MetricSeries) (float64, []float64) {
	t.Helper()
	var total float64
	var nonZero []float64
	for _, p := range nonNull(s) {
		if *p.Value < 0 {
			t.Errorf("%s point at %s = %v, want non-negative", s.Metric, p.Time, *p.Value)
		}
		total += *p.Value
		if *p.Value != 0 {
			nonZero = append(nonZero, *p.Value)
		}
	}
	return total, nonZero
}

// seriesByMetric returns the first series whose Metric matches name, failing
// the test when no such series is present.
func seriesByMetric(t *testing.T, series []MetricSeries, name string) MetricSeries {
	t.Helper()
	for _, s := range series {
		if s.Metric == name {
			return s
		}
	}
	t.Fatalf("no series found for metric %q in %d series", name, len(series))
	return MetricSeries{}
}

func TestQueryTimeSeries_Integration(t *testing.T) {
	pool, closePool := newLatestRowsTestPool(t)
	defer closePool()
	base, cleanup := setupTimeSeriesFixture(t, pool)
	defer cleanup()

	ctx := context.Background()

	t.Run("raw column request", func(t *testing.T) {
		series, err := QueryTimeSeries(ctx, pool, timeSeriesTestProbe,
			[]int{1}, lastHourWindow(), MetricFilters{}, 60, "avg", []string{"n_live_tup"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(series) != 1 {
			t.Fatalf("expected 1 series, got %d", len(series))
		}
		s := series[0]
		if s.Metric != "n_live_tup" || s.Name != "n_live_tup" {
			t.Errorf("unexpected series identity: metric=%q name=%q",
				s.Metric, s.Name)
		}
		if len(s.Data) == 0 {
			t.Fatal("expected at least one data point for the raw column")
		}
		// n_live_tup is a constant 90 across all samples; averaging or
		// carrying forward must preserve that value. The buckets before
		// the first sample and beyond the carry bound are null.
		points := nonNull(s)
		if len(points) == 0 {
			t.Fatal("expected at least one non-null raw point")
		}
		for _, p := range points {
			if *p.Value != 90 {
				t.Errorf("raw n_live_tup point = %v, want 90", *p.Value)
			}
		}
	})

	t.Run("per_sec rate request", func(t *testing.T) {
		series, err := QueryTimeSeries(ctx, pool, timeSeriesTestProbe,
			[]int{1}, lastHourWindow(), MetricFilters{}, 60, "avg",
			[]string{"seq_scan_per_sec"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(series) != 1 {
			t.Fatalf("expected 1 series, got %d", len(series))
		}
		s := series[0]
		if s.Metric != "seq_scan_per_sec" {
			t.Errorf("unexpected metric %q", s.Metric)
		}
		if len(s.Data) == 0 {
			t.Fatal("expected at least one rate data point")
		}
		// The counter rises 60 per 60 seconds, so every rate sample is 1.0.
		// Rates must never be negative and at least one must reflect the
		// observed 1.0-per-second delta.
		sawExpected := false
		for _, p := range nonNull(s) {
			if *p.Value < 0 {
				t.Errorf("rate point = %v, want non-negative", *p.Value)
			}
			if *p.Value >= 0.9 && *p.Value <= 1.1 {
				sawExpected = true
			}
		}
		if !sawExpected {
			t.Errorf("expected a rate near 1.0/sec, got %v", s.Data)
		}
	})

	t.Run("per_sec rate with last aggregation", func(t *testing.T) {
		series, err := QueryTimeSeries(ctx, pool, timeSeriesTestProbe,
			[]int{1}, lastHourWindow(), MetricFilters{}, 60, "last",
			[]string{"idx_scan_per_sec"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		s := seriesByMetric(t, series, "idx_scan_per_sec")
		if len(s.Data) == 0 {
			t.Fatal("expected at least one rate data point")
		}
		sawExpected := false
		for _, p := range nonNull(s) {
			if *p.Value < 0 {
				t.Errorf("rate point = %v, want non-negative", *p.Value)
			}
			if *p.Value >= 0.9 && *p.Value <= 1.1 {
				sawExpected = true
			}
		}
		if !sawExpected {
			t.Errorf("expected an idx_scan rate near 1.0/sec, got %v", s.Data)
		}
	})

	t.Run("dead_tuple_ratio request", func(t *testing.T) {
		series, err := QueryTimeSeries(ctx, pool, timeSeriesTestProbe,
			[]int{1}, lastHourWindow(), MetricFilters{}, 60, "avg",
			[]string{"dead_tuple_ratio"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(series) != 1 {
			t.Fatalf("expected 1 series, got %d", len(series))
		}
		s := series[0]
		if s.Metric != "dead_tuple_ratio" {
			t.Errorf("unexpected metric %q", s.Metric)
		}
		if len(s.Data) == 0 {
			t.Fatal("expected at least one ratio data point")
		}
		// 10 dead / (90 live + 10 dead) * 100 = 10 percent, on a 0-100 scale.
		sawExpected := false
		for _, p := range nonNull(s) {
			if *p.Value < 0 || *p.Value > 100 {
				t.Errorf("ratio point = %v, want within [0,100]", *p.Value)
			}
			if *p.Value >= 9.0 && *p.Value <= 11.0 {
				sawExpected = true
			}
		}
		if !sawExpected {
			t.Errorf("expected a dead-tuple ratio near 10, got %v", s.Data)
		}
	})

	t.Run("mixed raw and derived preserves request order", func(t *testing.T) {
		requested := []string{"n_live_tup", "seq_scan_per_sec", "dead_tuple_ratio"}
		series, err := QueryTimeSeries(ctx, pool, timeSeriesTestProbe,
			[]int{1}, lastHourWindow(), MetricFilters{}, 60, "avg", requested)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(series) != len(requested) {
			t.Fatalf("expected %d series, got %d", len(requested), len(series))
		}
		for i, want := range requested {
			if series[i].Metric != want {
				t.Errorf("series[%d].Metric = %q, want %q",
					i, series[i].Metric, want)
			}
		}
	})

	t.Run("empty request returns all numeric columns", func(t *testing.T) {
		series, err := QueryTimeSeries(ctx, pool, timeSeriesTestProbe,
			[]int{1}, lastHourWindow(), MetricFilters{}, 60, "avg", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// All five numeric counter columns are discovered as raw metrics.
		for _, want := range []string{
			"seq_scan", "idx_scan", "n_tup_ins", "n_live_tup", "n_dead_tup",
		} {
			seriesByMetric(t, series, want)
		}
	})

	t.Run("unknown metric rejected", func(t *testing.T) {
		_, err := QueryTimeSeries(ctx, pool, timeSeriesTestProbe,
			[]int{1}, lastHourWindow(), MetricFilters{}, 60, "avg",
			[]string{"not_a_real_metric"})
		if err == nil {
			t.Fatal("expected error for unknown metric")
		}
	})

	t.Run("missing probe rejected", func(t *testing.T) {
		_, err := QueryTimeSeries(ctx, pool, "does_not_exist_probe",
			[]int{1}, lastHourWindow(), MetricFilters{}, 60, "avg", []string{"seq_scan"})
		if err == nil {
			t.Fatal("expected error for missing probe")
		}
	})

	t.Run("invalid probe name rejected", func(t *testing.T) {
		_, err := QueryTimeSeries(ctx, pool, "bad-name",
			[]int{1}, lastHourWindow(), MetricFilters{}, 60, "avg", []string{"seq_scan"})
		if err == nil {
			t.Fatal("expected error for invalid probe name")
		}
	})

	t.Run("canceled context surfaces probe-verify error", func(t *testing.T) {
		cctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := QueryTimeSeries(cctx, pool, timeSeriesTestProbe,
			[]int{1}, lastHourWindow(), MetricFilters{}, 60, "avg", []string{"seq_scan"})
		if err == nil {
			t.Fatal("expected error from canceled context")
		}
	})

	t.Run("multiple connections tag series with connection id", func(t *testing.T) {
		// Connection 2 has no fixture rows, but requesting more than one
		// connection must still tag every series name with its connection
		// and emit an empty series for the connection without data.
		series, err := QueryTimeSeries(ctx, pool, timeSeriesTestProbe,
			[]int{1, 2}, lastHourWindow(), MetricFilters{}, 60, "avg", []string{"n_live_tup"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(series) != 2 {
			t.Fatalf("expected 2 series (one per connection), got %d",
				len(series))
		}
		names := map[string]bool{}
		for _, s := range series {
			names[s.Name] = true
			if s.Metric != "n_live_tup" {
				t.Errorf("unexpected metric %q", s.Metric)
			}
		}
		for _, want := range []string{"n_live_tup (conn 1)", "n_live_tup (conn 2)"} {
			if !names[want] {
				t.Errorf("expected series named %q, got %v", want, names)
			}
		}
	})

	t.Run("probe with no numeric columns rejected", func(t *testing.T) {
		const internalOnly = "ts_internal_only_test"
		dropTable(ctx, pool, internalOnly)
		ddl := `CREATE TABLE metrics."` + internalOnly + `" (
            connection_id integer NOT NULL,
            collected_at  timestamp with time zone NOT NULL,
            inserted_at   timestamp without time zone NOT NULL DEFAULT now(),
            relname       name
        )`
		if _, err := pool.Exec(ctx, ddl); err != nil {
			t.Fatalf("failed to create internal-only table: %v", err)
		}
		defer dropTable(ctx, pool, internalOnly)

		_, err := QueryTimeSeries(ctx, pool, internalOnly,
			[]int{1}, lastHourWindow(), MetricFilters{}, 60, "avg", nil)
		if err == nil {
			t.Fatal("expected error for probe with no numeric metrics")
		}
	})

	// A pre-resolved database column the table does not have builds valid
	// SQL that fails at execution, which is how both execution-error
	// returns are exercised. An invalid aggregation no longer serves:
	// the builders refuse one before any SQL is produced.
	missingColumn := MetricFilters{
		DatabaseName:   "northwind",
		DatabaseColumn: "no_such_database_column",
	}

	t.Run("raw query execution error propagates", func(t *testing.T) {
		_, err := QueryTimeSeries(ctx, pool, timeSeriesTestProbe,
			[]int{1}, lastHourWindow(), missingColumn, 60, "avg",
			[]string{"seq_scan"})
		if err == nil {
			t.Fatal("expected error from failing raw query")
		}
	})

	t.Run("derived query execution error propagates", func(t *testing.T) {
		_, err := QueryTimeSeries(ctx, pool, timeSeriesTestProbe,
			[]int{1}, lastHourWindow(), missingColumn, 60, "avg",
			[]string{"seq_scan_per_sec"})
		if err == nil {
			t.Fatal("expected error from failing derived query")
		}
	})

	t.Run("an invalid aggregation is refused before any query runs", func(t *testing.T) {
		_, err := QueryTimeSeries(ctx, pool, timeSeriesTestProbe,
			[]int{1}, lastHourWindow(), MetricFilters{}, 60, "no_such_agg",
			[]string{"seq_scan"})
		if err == nil {
			t.Fatal("expected error for an invalid aggregation")
		}
		if !strings.Contains(err.Error(), "invalid aggregation") {
			t.Errorf("error = %v, want it to mention an invalid aggregation", err)
		}
	})

	t.Run("database filter resolves and narrows", func(t *testing.T) {
		series, err := QueryTimeSeries(ctx, pool, timeSeriesTestProbe,
			[]int{1}, lastHourWindow(), MetricFilters{DatabaseName: "northwind"}, 60,
			"avg", []string{"n_live_tup"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		s := seriesByMetric(t, series, "n_live_tup")
		if len(s.Data) == 0 {
			t.Fatal("expected data for matching database filter")
		}
	})

	t.Run("index_name filter yields per-sec data for the Scan Activity chart", func(t *testing.T) {
		// This reproduces the fixed bug: the Index detail dashboard requests
		// idx_scan_per_sec scoped to a single index. With IndexName plumbed
		// through metricQueryBase, the matching index returns real rate data.
		series, err := QueryTimeSeries(ctx, pool, timeSeriesTestProbe,
			[]int{1}, lastHourWindow(),
			MetricFilters{SchemaName: "public", TableName: "orders", IndexName: "pk_orders"},
			60, "avg", []string{"idx_scan_per_sec"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		s := seriesByMetric(t, series, "idx_scan_per_sec")
		if len(s.Data) == 0 {
			t.Fatal("expected rate data for the matching index name")
		}
		sawExpected := false
		for _, p := range nonNull(s) {
			if *p.Value < 0 {
				t.Errorf("rate point = %v, want non-negative", *p.Value)
			}
			if *p.Value >= 0.9 && *p.Value <= 1.1 {
				sawExpected = true
			}
		}
		if !sawExpected {
			t.Errorf("expected an idx_scan rate near 1.0/sec, got %v", s.Data)
		}
	})

	t.Run("index_name filter narrows out non-matching indexes", func(t *testing.T) {
		// A different index name matches no rows, so the series is present
		// with every bucket null; this is what previously happened for
		// every index because the filter was silently dropped and the
		// wrong dimension was queried.
		series, err := QueryTimeSeries(ctx, pool, timeSeriesTestProbe,
			[]int{1}, lastHourWindow(),
			MetricFilters{IndexName: "some_other_index"},
			60, "avg", []string{"idx_scan_per_sec"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertAllNull(t, seriesByMetric(t, series, "idx_scan_per_sec"))
	})

	t.Run("first rate bucket includes the rise from before the window", func(t *testing.T) {
		// The window opens on the -4 min sample, leaving the -5 min sample
		// just outside it. The counter rose 60 between the two, so the very
		// first bucket must report 1.0/sec: before the sample query reached
		// back past the window start, that first sample had no LAG, its rate
		// was NULL, and the increase was simply lost.
		window := windowSince(base, 4)
		series, err := QueryTimeSeries(ctx, pool, timeSeriesTestProbe,
			[]int{1}, window, MetricFilters{}, 60, "avg",
			[]string{"seq_scan_per_sec"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		s := seriesByMetric(t, series, "seq_scan_per_sec")
		if len(s.Data) == 0 {
			t.Fatal("expected rate data points")
		}
		first := s.Data[0]
		if !first.Time.Equal(window.Start) {
			t.Errorf("first point at %s, want the window start %s",
				first.Time, window.Start)
		}
		if v := val(t, s, first); v < 0.9 || v > 1.1 {
			t.Errorf("first rate = %v, want near 1.0/sec", v)
		}
	})

	t.Run("no sample before the window leaves the first bucket null", func(t *testing.T) {
		// With the window opening on the earliest sample there is nothing
		// earlier to borrow, so that sample still has no LAG and its bucket
		// is a null point; the second bucket carries the first real rate.
		window := windowSince(base, 5)
		series, err := QueryTimeSeries(ctx, pool, timeSeriesTestProbe,
			[]int{1}, window, MetricFilters{}, 60, "avg",
			[]string{"seq_scan_per_sec"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		s := seriesByMetric(t, series, "seq_scan_per_sec")
		if len(s.Data) < 2 {
			t.Fatal("expected rate data points")
		}
		if !s.Data[0].Time.Equal(window.Start) || s.Data[0].Value != nil {
			t.Errorf("first point = %s %s, want a null at the window start",
				s.Data[0].Time, fmtValue(s.Data[0].Value))
		}
		second := s.Data[1]
		if !second.Time.Equal(window.Start.Add(time.Minute)) {
			t.Errorf("second point at %s, want the second sample's minute",
				second.Time)
		}
		if v := val(t, s, second); v < 0.9 || v > 1.1 {
			t.Errorf("second rate = %v, want near 1.0/sec", v)
		}
	})

	t.Run("lookback respects the dimension filters", func(t *testing.T) {
		// A filter that matches no row must not let the lookback pull in
		// another entity's sample; the series stays empty.
		series, err := QueryTimeSeries(ctx, pool, timeSeriesTestProbe,
			[]int{1}, windowSince(base, 4),
			MetricFilters{IndexName: "some_other_index"},
			60, "avg", []string{"idx_scan_per_sec"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertAllNull(t, seriesByMetric(t, series, "idx_scan_per_sec"))
	})

	t.Run("raw column request honors index_name filter", func(t *testing.T) {
		// The raw-column path shares metricQueryBase, so IndexName must scope
		// it too; a non-matching index yields an all-null raw series.
		series, err := QueryTimeSeries(ctx, pool, timeSeriesTestProbe,
			[]int{1}, lastHourWindow(),
			MetricFilters{IndexName: "no_such_index"},
			60, "avg", []string{"idx_scan"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertAllNull(t, seriesByMetric(t, series, "idx_scan"))
	})
}

const deltaTestProbe = "pg_stat_all_tables_delta_test"

// setupDeltaFixture creates a probe table carrying a single cumulative
// counter and inserts a known progression of connection-1 samples, one per
// minute, deliberately including a minute with no sample at all and a
// counter reset. Because the query bucket width is 60 seconds for a "1h"
// window, samples a minute apart always land in distinct buckets whatever
// the window origin happens to be, so the per-bucket deltas are exact:
//
//	-6 min  1000   first sample, no LAG, contributes nothing
//	-5 min  1010   delta 10
//	-4 min  1030   delta 20
//	-3 min  (none) bucket with no sample, must read 0
//	-2 min  1100   delta 70, covering the sample-less minute
//	-1 min     5   counter reset, contributes nothing
//	-0 min    25   delta 20
//
// It returns the minute-truncated base time the offsets are relative to, so
// tests can build windows lining up with the samples exactly, together with
// a cleanup that drops the table.
func setupDeltaFixture(t *testing.T, pool *pgxpool.Pool) (time.Time, func()) {
	t.Helper()
	ctx := context.Background()

	if _, err := pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS metrics"); err != nil {
		t.Fatalf("failed to create metrics schema: %v", err)
	}

	dropTable(ctx, pool, deltaTestProbe)
	// The registry names a reset marker the fixture table does not carry,
	// as a server sees against a collector schema predating the column:
	// the builder must drop it silently and fall back to the negative
	// delta guard, which the reset below then exercises.
	registerProbeKindsForTest(t, deltaTestProbe, probeRegistryEntry{
		kinds:       columnKinds(counters("seq_scan")),
		resetColumn: fixedReset("stats_reset"),
	})
	setProbeIntervalForTest(t, pool, deltaTestProbe, 0, 60)

	ddl := `CREATE TABLE metrics."` + deltaTestProbe + `" (
        connection_id integer NOT NULL,
        collected_at  timestamp with time zone NOT NULL,
        inserted_at   timestamp without time zone NOT NULL DEFAULT now(),
        relname       name,
        seq_scan      bigint
    )`
	if _, err := pool.Exec(ctx, ddl); err != nil {
		t.Fatalf("failed to create delta fixture table: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Minute)
	samples := []struct {
		offset  time.Duration
		seqScan int64
	}{
		{-6 * time.Minute, 1000},
		{-5 * time.Minute, 1010},
		{-4 * time.Minute, 1030},
		{-2 * time.Minute, 1100},
		{-1 * time.Minute, 5},
		{0, 25},
	}

	insert := `INSERT INTO metrics."` + deltaTestProbe + `"
        (connection_id, collected_at, relname, seq_scan)
        VALUES ($1, $2, $3, $4)`
	for i, s := range samples {
		_, err := pool.Exec(ctx, insert, 1, now.Add(s.offset), "orders", s.seqScan)
		if err != nil {
			dropTable(ctx, pool, deltaTestProbe)
			t.Fatalf("failed to insert delta fixture sample %d: %v", i, err)
		}
	}

	return now, func() { dropTable(context.Background(), pool, deltaTestProbe) }
}

func TestQueryTimeSeriesDelta_Integration(t *testing.T) {
	pool, closePool := newLatestRowsTestPool(t)
	defer closePool()
	base, cleanup := setupDeltaFixture(t, pool)
	defer cleanup()

	ctx := context.Background()

	t.Run("per-bucket deltas with zero fill and reset guard", func(t *testing.T) {
		series, err := QueryTimeSeries(ctx, pool, deltaTestProbe,
			[]int{1}, lastHourWindow(), MetricFilters{}, 60, "avg",
			[]string{"seq_scan_delta"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		s := seriesByMetric(t, series, "seq_scan_delta")

		// Every bucket of the hour is emitted, not just the six carrying a
		// sample: a sample-less bucket an accepted interval spans reads 0
		// rather than being left NULL for the caller's LOCF fill to
		// duplicate.
		if len(s.Data) < 50 {
			t.Fatalf("expected the whole window to be filled, got %d points",
				len(s.Data))
		}

		total, nonZero := sumValues(t, s)

		// 10 + 20 + 70 + 20; the first sample and the reset add nothing.
		if total != 120 {
			t.Errorf("summed deltas = %v, want 120 (%v)", total, nonZero)
		}
		want := []float64{10, 20, 70, 20}
		if len(nonZero) != len(want) {
			t.Fatalf("expected %d non-zero buckets, got %d: %v",
				len(want), len(nonZero), nonZero)
		}
		for i := range want {
			if nonZero[i] != want[i] {
				t.Errorf("non-zero delta[%d] = %v, want %v",
					i, nonZero[i], want[i])
			}
		}

		// The sample-less minute sits between the 20 and the 70, and the
		// two-minute spacing across it is within the gap bound, so the
		// interval spanning that bucket is accepted and it reads 0 rather
		// than breaking the series.
		zeros := len(nonNull(s)) - len(nonZero)
		if zeros == 0 {
			t.Error("expected zero-filled buckets between the samples")
		}

		// The samples only start six minutes before the window's end, so
		// every bucket before the one holding the first sample is spanned
		// by no interval at all: those events are unknown, not zero, and
		// the buckets are null. From the first sample on, every bucket has
		// a value.
		first := -1
		for i, p := range s.Data {
			if p.Value != nil {
				first = i
				break
			}
		}
		if first < 0 {
			t.Fatal("every delta bucket is null")
		}
		if got := s.Data[first].Time; got.After(base.Add(-6 * time.Minute)) {
			t.Errorf("first non-null bucket at %s, want one holding the sample at %s",
				got, base.Add(-6*time.Minute))
		}
		if first < 50 {
			t.Errorf("only %d leading buckets are null, want the hour before the"+
				" first sample to be null", first)
		}
		last := len(s.Data) - 1
		for _, p := range s.Data[first:last] {
			if p.Value == nil {
				t.Errorf("bucket at %s is null, want a value", p.Time)
			}
		}
		// generate_series is inclusive of the window end, so the final
		// point opens a bucket that starts after the last sample: nothing
		// spans it, and it is null rather than a confident zero.
		if v := s.Data[last].Value; v != nil {
			t.Errorf("trailing bucket at %s = %v, want null", s.Data[last].Time, *v)
		}
	})

	t.Run("delta and per_sec requested together", func(t *testing.T) {
		requested := []string{"seq_scan_per_sec", "seq_scan_delta"}
		series, err := QueryTimeSeries(ctx, pool, deltaTestProbe,
			[]int{1}, lastHourWindow(), MetricFilters{}, 60, "avg", requested)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(series) != len(requested) {
			t.Fatalf("expected %d series, got %d", len(requested), len(series))
		}
		for i, want := range requested {
			if series[i].Metric != want {
				t.Errorf("series[%d].Metric = %q, want %q",
					i, series[i].Metric, want)
			}
		}
		d := seriesByMetric(t, series, "seq_scan_delta")
		if total, _ := sumValues(t, d); total != 120 {
			t.Errorf("summed deltas alongside per_sec = %v, want 120", total)
		}
		r := seriesByMetric(t, series, "seq_scan_per_sec")
		if len(nonNull(r)) == 0 {
			t.Error("expected per-second rate data alongside the delta")
		}
		if len(r.Data) != len(d.Data) {
			t.Errorf("rate has %d points, delta %d; want one point per bucket each",
				len(r.Data), len(d.Data))
		}
	})

	t.Run("first bucket includes the increase from before the window", func(t *testing.T) {
		// The window opens on the -4 min sample (1030), leaving the -5 min
		// sample (1010) just outside it, so the first bucket must report the
		// increase of 20 that happened between them. The remaining buckets
		// are unchanged: 70 for the sample-less minute, nothing for the
		// reset, then 20.
		window := windowSince(base, 4)
		series, err := QueryTimeSeries(ctx, pool, deltaTestProbe,
			[]int{1}, window, MetricFilters{}, 60, "avg",
			[]string{"seq_scan_delta"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		s := seriesByMetric(t, series, "seq_scan_delta")
		if len(s.Data) == 0 {
			t.Fatal("expected delta data points")
		}
		if !s.Data[0].Time.Equal(window.Start) {
			t.Errorf("first point at %s, want the window start %s",
				s.Data[0].Time, window.Start)
		}
		if v := val(t, s, s.Data[0]); v != 20 {
			t.Errorf("first bucket delta = %v, want 20 (the rise since the "+
				"sample before the window)", v)
		}

		total, nonZero := sumValues(t, s)
		if total != 110 {
			t.Errorf("summed deltas = %v, want 110 (%v)", total, nonZero)
		}
		want := []float64{20, 70, 20}
		if len(nonZero) != len(want) {
			t.Fatalf("expected %d non-zero buckets, got %d: %v",
				len(want), len(nonZero), nonZero)
		}
		for i := range want {
			if nonZero[i] != want[i] {
				t.Errorf("non-zero delta[%d] = %v, want %v",
					i, nonZero[i], want[i])
			}
		}
	})

	t.Run("no sample before the window contributes nothing extra", func(t *testing.T) {
		// The window opens on the earliest sample of all, so there is no
		// earlier sample to borrow and that first sample still contributes
		// nothing: the totals are exactly what they were before the fix.
		window := windowSince(base, 6)
		series, err := QueryTimeSeries(ctx, pool, deltaTestProbe,
			[]int{1}, window, MetricFilters{}, 60, "avg",
			[]string{"seq_scan_delta"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		s := seriesByMetric(t, series, "seq_scan_delta")
		if len(s.Data) == 0 {
			t.Fatal("expected delta data points")
		}
		if v := val(t, s, s.Data[0]); v != 0 {
			t.Errorf("first bucket delta = %v, want 0 (no earlier sample)", v)
		}
		total, nonZero := sumValues(t, s)
		if total != 120 {
			t.Errorf("summed deltas = %v, want 120 (%v)", total, nonZero)
		}
		want := []float64{10, 20, 70, 20}
		if len(nonZero) != len(want) {
			t.Fatalf("expected %d non-zero buckets, got %d: %v",
				len(want), len(nonZero), nonZero)
		}
		for i := range want {
			if nonZero[i] != want[i] {
				t.Errorf("non-zero delta[%d] = %v, want %v",
					i, nonZero[i], want[i])
			}
		}
	})

	t.Run("unknown delta base rejected", func(t *testing.T) {
		_, err := QueryTimeSeries(ctx, pool, deltaTestProbe,
			[]int{1}, lastHourWindow(), MetricFilters{}, 60, "avg",
			[]string{"no_such_column_delta"})
		if err == nil {
			t.Fatal("expected error for unknown delta base column")
		}
	})
}

const networkTestProbe = "pg_sys_network_info_ts_test"

// setupNetworkFixture creates an entity-keyed probe table shaped like
// pg_sys_network_info, with the real table's primary key so
// GetProbeEntityKeyColumns discovers interface_name, and inserts
// minute-spaced samples for connection 1 in which the interface set changes
// under the window:
//
//	          eth0    eth1                  eth2
//	-6 min    6000    3000
//	-5 min    6600    3300
//	-4 min    7200     100  counter reset
//	-3 min    7800     400
//	-2 min    8400          eth1 torn down
//	-1 min    9000                        500000  new, lifetime counter
//	 0 min    9600                        500060
//
// eth0 rises 600 a minute (10/s) throughout, eth1 300 a minute (5/s) until
// it resets and then disappears, and eth2 arrives carrying a large lifetime
// total and then rises 60 a minute (1/s). Summing the interfaces before
// differencing would report a negative change at -4 and -2 min and a
// 500000 spike at -1 min; differencing per interface reports none of
// those.
func setupNetworkFixture(t *testing.T, pool *pgxpool.Pool) (time.Time, func()) {
	t.Helper()
	ctx := context.Background()

	if _, err := pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS metrics"); err != nil {
		t.Fatalf("failed to create metrics schema: %v", err)
	}

	dropTable(ctx, pool, networkTestProbe)
	// The real probe excludes the loopback interface; the fixture carries
	// the same exclusion and a lo row on every sample so both the raw and
	// the derived path can be shown to ignore it.
	registerProbeKindsForTest(t, networkTestProbe, probeRegistryEntry{
		kinds:           columnKinds(counters("tx_bytes")),
		excludeEntities: ProbeEntityExclusion("pg_sys_network_info"),
	})
	setProbeIntervalForTest(t, pool, networkTestProbe, 0, 60)

	ddl := `CREATE TABLE metrics."` + networkTestProbe + `" (
        connection_id  integer NOT NULL,
        collected_at   timestamp with time zone NOT NULL,
        inserted_at    timestamp without time zone NOT NULL DEFAULT now(),
        interface_name text NOT NULL,
        ip_address     text,
        tx_bytes       bigint,
        PRIMARY KEY (connection_id, collected_at, interface_name)
    )`
	if _, err := pool.Exec(ctx, ddl); err != nil {
		t.Fatalf("failed to create network fixture table: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Minute)
	samples := []struct {
		offset  time.Duration
		iface   string
		txBytes int64
	}{
		{-6 * time.Minute, "eth0", 6000},
		{-6 * time.Minute, "eth1", 3000},
		{-5 * time.Minute, "eth0", 6600},
		{-5 * time.Minute, "eth1", 3300},
		{-4 * time.Minute, "eth0", 7200},
		{-4 * time.Minute, "eth1", 100},
		{-3 * time.Minute, "eth0", 7800},
		{-3 * time.Minute, "eth1", 400},
		{-2 * time.Minute, "eth0", 8400},
		{-1 * time.Minute, "eth0", 9000},
		{-1 * time.Minute, "eth2", 500000},
		{0, "eth0", 9600},
		{0, "eth2", 500060},
	}
	// Loopback traffic on every sample, far larger than the real
	// interfaces so that any leak into a total is unmistakable.
	for i := 0; i <= 6; i++ {
		samples = append(samples, struct {
			offset  time.Duration
			iface   string
			txBytes int64
		}{-time.Duration(6-i) * time.Minute, "lo", int64(1000000 * (i + 1))})
	}

	insert := `INSERT INTO metrics."` + networkTestProbe + `"
        (connection_id, collected_at, interface_name, ip_address, tx_bytes)
        VALUES ($1, $2, $3, $4, $5)`
	for i, s := range samples {
		_, err := pool.Exec(ctx, insert,
			1, now.Add(s.offset), s.iface, "192.0.2.1", s.txBytes)
		if err != nil {
			dropTable(ctx, pool, networkTestProbe)
			t.Fatalf("failed to insert network fixture sample %d: %v", i, err)
		}
	}

	return now, func() { dropTable(context.Background(), pool, networkTestProbe) }
}

// pointAt returns the value of the point at exactly ts, failing the test
// when the series has no such point or the point is null.
func pointAt(t *testing.T, s MetricSeries, ts time.Time) float64 {
	t.Helper()
	p := rawPointAt(t, s, ts)
	return val(t, s, p)
}

// rawPointAt returns the point at exactly ts, null or not, failing the test
// when the series has no such point.
func rawPointAt(t *testing.T, s MetricSeries, ts time.Time) MetricDataPoint {
	t.Helper()
	for _, p := range s.Data {
		if p.Time.Equal(ts) {
			return p
		}
	}
	t.Fatalf("series %q has no point at %s (%d points)", s.Metric, ts, len(s.Data))
	return MetricDataPoint{}
}

// assertNullAt fails the test unless the point of s at ts exists and is
// null.
func assertNullAt(t *testing.T, s MetricSeries, ts time.Time) {
	t.Helper()
	if p := rawPointAt(t, s, ts); p.Value != nil {
		t.Errorf("%s at %s = %v, want null", s.Metric, ts, *p.Value)
	}
}

func TestQueryTimeSeriesEntityKeyed_Integration(t *testing.T) {
	pool, closePool := newLatestRowsTestPool(t)
	defer closePool()
	base, cleanup := setupNetworkFixture(t, pool)
	defer cleanup()

	ctx := context.Background()

	t.Run("entity keys are discovered from the primary key", func(t *testing.T) {
		keys, err := GetProbeEntityKeyColumns(ctx, pool, networkTestProbe)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(keys) != 1 || keys[0] != "interface_name" {
			t.Errorf("entity keys = %v, want [interface_name]", keys)
		}
	})

	t.Run("delta is differenced per interface before summing", func(t *testing.T) {
		series, err := QueryTimeSeries(ctx, pool, networkTestProbe,
			[]int{1}, windowSince(base, 60), MetricFilters{}, 60, "avg",
			[]string{"tx_bytes_delta"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		s := seriesByMetric(t, series, "tx_bytes_delta")

		// Per minute: eth0's 600 throughout; eth1 adds 300 at -5 and -3
		// min, nothing at its reset (-4) and nothing once gone; eth2 adds
		// nothing on arrival (-1) and 60 at 0 min.
		want := map[time.Duration]float64{
			-6 * time.Minute: 0,
			-5 * time.Minute: 900,
			-4 * time.Minute: 600,
			-3 * time.Minute: 900,
			-2 * time.Minute: 600,
			-1 * time.Minute: 600,
			0:                660,
		}
		for offset, v := range want {
			if got := pointAt(t, s, base.Add(offset)); got != v {
				t.Errorf("delta at %v = %v, want %v", offset, got, v)
			}
		}
		total, nonZero := sumValues(t, s)
		for _, v := range nonZero {
			if v > 1000 {
				t.Errorf("delta point = %v: a new interface's lifetime "+
					"counter folded into one interval, or loopback leaked", v)
			}
		}
		if total != 4260 {
			t.Errorf("summed deltas = %v, want 4260", total)
		}
	})

	t.Run("rate is differenced per interface before summing", func(t *testing.T) {
		series, err := QueryTimeSeries(ctx, pool, networkTestProbe,
			[]int{1}, windowSince(base, 60), MetricFilters{}, 60, "avg",
			[]string{"tx_bytes_per_sec"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		s := seriesByMetric(t, series, "tx_bytes_per_sec")

		// eth0 is a steady 10/s; eth1 adds 5/s where it has a valid
		// predecessor and drops out of the sum at its reset and after its
		// teardown rather than pulling the total negative; eth2 adds 1/s
		// only once it has a second sample.
		want := map[time.Duration]float64{
			-5 * time.Minute: 15,
			-4 * time.Minute: 10,
			-3 * time.Minute: 15,
			-2 * time.Minute: 10,
			-1 * time.Minute: 10,
			0:                11,
		}
		for offset, v := range want {
			if got := pointAt(t, s, base.Add(offset)); got != v {
				t.Errorf("rate at %v = %v, want %v", offset, got, v)
			}
		}
		for _, p := range nonNull(s) {
			if *p.Value < 0 || *p.Value > 20 {
				t.Errorf("rate point = %v, want within [0, 20]", *p.Value)
			}
		}
	})

	t.Run("loopback rows are ignored by the raw path", func(t *testing.T) {
		// Summing tx_bytes across interfaces at -2 min, when only eth0 and
		// lo have rows, must give eth0's 8400 alone.
		series, err := QueryTimeSeries(ctx, pool, networkTestProbe,
			[]int{1}, windowSince(base, 60), MetricFilters{}, 60, "sum",
			[]string{"tx_bytes"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		s := seriesByMetric(t, series, "tx_bytes")
		if got := pointAt(t, s, base.Add(-2*time.Minute)); got != 8400 {
			t.Errorf("raw tx_bytes sum at -2 min = %v, want 8400 (eth0 only)", got)
		}
		for _, p := range nonNull(s) {
			if *p.Value >= 1000000 {
				t.Errorf("raw point at %s = %v: loopback leaked in", p.Time, *p.Value)
			}
		}
	})

	t.Run("connection with no samples yields only null points", func(t *testing.T) {
		// A disabled, never-run or failing probe must render as no data,
		// not as a confident flat zero: the delta's zero fill is gated on
		// the window holding at least one sample.
		for _, metric := range []string{"tx_bytes_delta", "tx_bytes_per_sec"} {
			series, err := QueryTimeSeries(ctx, pool, networkTestProbe,
				[]int{99}, lastHourWindow(), MetricFilters{}, 150, "avg",
				[]string{metric})
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", metric, err)
			}
			assertAllNull(t, seriesByMetric(t, series, metric))
		}
	})

	t.Run("window with no samples yields only null points", func(t *testing.T) {
		// The same connection, but a window that closes before its first
		// sample: no in-window sample, so no zero fill either.
		window := TimeWindow{
			Start: base.Add(-3 * time.Hour),
			End:   base.Add(-2 * time.Hour),
		}
		series, err := QueryTimeSeries(ctx, pool, networkTestProbe,
			[]int{1}, window, MetricFilters{}, 60, "avg",
			[]string{"tx_bytes_delta"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertAllNull(t, seriesByMetric(t, series, "tx_bytes_delta"))
	})

	t.Run("a single in-window sample fills only its own bucket", func(t *testing.T) {
		// The window opens on the probe's very first sample, so that
		// sample has no predecessor to difference against: its bucket
		// reads 0, and the bucket after it is spanned by no interval at
		// all and stays null rather than asserting that nothing happened.
		window := TimeWindow{
			Start: base.Add(-6*time.Minute - 30*time.Second),
			End:   base.Add(-5*time.Minute - 30*time.Second),
		}
		series, err := QueryTimeSeries(ctx, pool, networkTestProbe,
			[]int{1}, window, MetricFilters{}, 60, "avg",
			[]string{"tx_bytes_delta"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// The one-minute window clamps to a single 60-second bucket, so
		// generate_series yields the bucket at the start and one at the end.
		s := seriesByMetric(t, series, "tx_bytes_delta")
		if len(s.Data) != 2 {
			t.Fatalf("got %d points, want 2 (one bucket plus the end)", len(s.Data))
		}
		if got := val(t, s, s.Data[0]); got != 0 {
			t.Errorf("point at %s = %v, want 0", s.Data[0].Time, got)
		}
		if s.Data[1].Value != nil {
			t.Errorf("point at %s = %v, want null", s.Data[1].Time, *s.Data[1].Value)
		}
	})
}

const lookbackTestProbe = "pg_stat_wal_lookback_test"

// setupLookbackFixture creates a single-row probe table (its primary key is
// just connection_id and collected_at, so GetProbeEntityKeyColumns yields
// nothing) holding one scenario per connection for the lookback bound:
//
//	conn 1  -5 min 1000, -4 min 5, -3 min 25, -2 min 45
//	        a reset straddling a window that opens at -4 min
//	conn 2  -50 min 100, then -3 min 200, -2 min 260, -1 min 320, 0 min 380
//	        a predecessor far older than the probe interval, beyond the
//	        30-minute floor of a short window
//	conn 3  -20 min 100, then the same -3..0 min samples as conn 2
//	        a predecessor inside the floor, so it is borrowed
//	conn 4  -26 h 0, then -2 h 1000, -1 h 1060, 0 h 1120
//	        a predecessor beyond three 1-hour buckets of a 2 h window
//	conn 5  -4.5 h 400, then the same -2..0 h samples as conn 4
//	        a predecessor inside three 1-hour buckets, so it is borrowed
func setupLookbackFixture(t *testing.T, pool *pgxpool.Pool) (time.Time, func()) {
	t.Helper()
	ctx := context.Background()

	if _, err := pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS metrics"); err != nil {
		t.Fatalf("failed to create metrics schema: %v", err)
	}

	dropTable(ctx, pool, lookbackTestProbe)
	registerProbeKindsForTest(t, lookbackTestProbe, countersForTest("wal_records"))
	// Connections 1 to 3 run the probe every minute; 4 and 5 are hourly,
	// so their 2-hour windows keep two buckets and a predecessor up to
	// three hours back is within the gap bound.
	setProbeIntervalForTest(t, pool, lookbackTestProbe, 0, 60)
	setProbeIntervalForTest(t, pool, lookbackTestProbe, 4, 3600)
	setProbeIntervalForTest(t, pool, lookbackTestProbe, 5, 3600)

	ddl := `CREATE TABLE metrics."` + lookbackTestProbe + `" (
        connection_id integer NOT NULL,
        collected_at  timestamp with time zone NOT NULL,
        inserted_at   timestamp without time zone NOT NULL DEFAULT now(),
        wal_records   bigint,
        PRIMARY KEY (connection_id, collected_at)
    )`
	if _, err := pool.Exec(ctx, ddl); err != nil {
		t.Fatalf("failed to create lookback fixture table: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Minute)
	samples := []struct {
		conn   int
		offset time.Duration
		value  int64
	}{
		{1, -5 * time.Minute, 1000},
		{1, -4 * time.Minute, 5},
		{1, -3 * time.Minute, 25},
		{1, -2 * time.Minute, 45},

		{2, -50 * time.Minute, 100},
		{2, -3 * time.Minute, 200},
		{2, -2 * time.Minute, 260},
		{2, -1 * time.Minute, 320},
		{2, 0, 380},

		{3, -20 * time.Minute, 100},
		{3, -3 * time.Minute, 200},
		{3, -2 * time.Minute, 260},
		{3, -1 * time.Minute, 320},
		{3, 0, 380},

		{4, -26 * time.Hour, 0},
		{4, -2 * time.Hour, 1000},
		{4, -1 * time.Hour, 1060},
		{4, 0, 1120},

		{5, -270 * time.Minute, 400},
		{5, -2 * time.Hour, 1000},
		{5, -1 * time.Hour, 1060},
		{5, 0, 1120},
	}

	insert := `INSERT INTO metrics."` + lookbackTestProbe + `"
        (connection_id, collected_at, wal_records) VALUES ($1, $2, $3)`
	for i, s := range samples {
		if _, err := pool.Exec(ctx, insert, s.conn, now.Add(s.offset), s.value); err != nil {
			dropTable(ctx, pool, lookbackTestProbe)
			t.Fatalf("failed to insert lookback fixture sample %d: %v", i, err)
		}
	}

	return now, func() { dropTable(context.Background(), pool, lookbackTestProbe) }
}

// nonZeroValues returns the non-zero point values of s in order, failing
// the test on any negative value.
func nonZeroValues(t *testing.T, s MetricSeries) []float64 {
	t.Helper()
	_, nonZero := sumValues(t, s)
	return nonZero
}

func TestQueryTimeSeriesLookbackBound_Integration(t *testing.T) {
	pool, closePool := newLatestRowsTestPool(t)
	defer closePool()
	base, cleanup := setupLookbackFixture(t, pool)
	defer cleanup()

	ctx := context.Background()

	t.Run("bookkeeping-only primary key yields no entity keys", func(t *testing.T) {
		keys, err := GetProbeEntityKeyColumns(ctx, pool, lookbackTestProbe)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(keys) != 0 {
			t.Errorf("entity keys = %v, want none", keys)
		}
	})

	t.Run("reset straddling the window boundary contributes nothing", func(t *testing.T) {
		// The borrowed -5 min sample (1000) is followed by the first
		// in-window sample of 5: a reset, not a fall of 995 and not a rise
		// of 5. The first bucket must read 0 and the rest 20 each.
		series, err := QueryTimeSeries(ctx, pool, lookbackTestProbe,
			[]int{1}, windowSince(base, 4), MetricFilters{}, 4, "avg",
			[]string{"wal_records_delta", "wal_records_per_sec"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		d := seriesByMetric(t, series, "wal_records_delta")
		if got := pointAt(t, d, base.Add(-4*time.Minute)); got != 0 {
			t.Errorf("first bucket delta = %v, want 0 across a reset", got)
		}
		if got := nonZeroValues(t, d); len(got) != 2 || got[0] != 20 || got[1] != 20 {
			t.Errorf("non-zero deltas = %v, want [20 20]", got)
		}
		// The rate has no valid value in the first bucket, so that point is
		// null rather than a carried-forward reset; the -3 min bucket
		// carries the first real rate.
		r := seriesByMetric(t, series, "wal_records_per_sec")
		assertNullAt(t, r, base.Add(-4*time.Minute))
		if got := pointAt(t, r, base.Add(-3*time.Minute)); got != 20.0/60.0 {
			t.Errorf("first rate = %v, want %v", got, 20.0/60.0)
		}
	})

	t.Run("predecessor beyond the floor is not borrowed", func(t *testing.T) {
		// A 3-minute window has 60s buckets, so the bound is the 30-minute
		// floor; the -50 min sample is 47 minutes before the window start
		// and must not inflate the first bucket with the 100 the counter
		// rose across the whole gap.
		series, err := QueryTimeSeries(ctx, pool, lookbackTestProbe,
			[]int{2}, windowSince(base, 3), MetricFilters{}, 3, "avg",
			[]string{"wal_records_delta", "wal_records_per_sec"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		d := seriesByMetric(t, series, "wal_records_delta")
		if got := pointAt(t, d, base.Add(-3*time.Minute)); got != 0 {
			t.Errorf("first bucket delta = %v, want 0 (predecessor out of reach)", got)
		}
		if got := nonZeroValues(t, d); len(got) != 3 || got[0] != 60 || got[1] != 60 || got[2] != 60 {
			t.Errorf("non-zero deltas = %v, want [60 60 60]", got)
		}
		r := seriesByMetric(t, series, "wal_records_per_sec")
		assertNullAt(t, r, base.Add(-3*time.Minute))
		for _, p := range nonNull(r) {
			if *p.Value != 1 {
				t.Errorf("rate at %s = %v, want 1", p.Time, *p.Value)
			}
		}
		if len(nonNull(r)) != 3 {
			t.Errorf("got %d rate values, want 3", len(nonNull(r)))
		}
	})

	t.Run("predecessor inside the floor but beyond the gap bound is a gap", func(t *testing.T) {
		// Same window, but the predecessor is 17 minutes before the start:
		// inside the 30-minute floor, so it is borrowed, yet 17 minutes is
		// more than three one-minute probe intervals, so the interval is a
		// collection gap. The first bucket is null for both the delta
		// (a gap, not a zero) and the rate, rather than 100 spread over the
		// outage; the "not borrowed" case above reads 0 instead.
		series, err := QueryTimeSeries(ctx, pool, lookbackTestProbe,
			[]int{3}, windowSince(base, 3), MetricFilters{}, 3, "avg",
			[]string{"wal_records_delta", "wal_records_per_sec"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		d := seriesByMetric(t, series, "wal_records_delta")
		assertNullAt(t, d, base.Add(-3*time.Minute))
		if got := nonZeroValues(t, d); len(got) != 3 || got[0] != 60 || got[1] != 60 || got[2] != 60 {
			t.Errorf("non-zero deltas = %v, want [60 60 60]", got)
		}
		r := seriesByMetric(t, series, "wal_records_per_sec")
		assertNullAt(t, r, base.Add(-3*time.Minute))
		if got := pointAt(t, r, base.Add(-2*time.Minute)); got != 1 {
			t.Errorf("rate at -2 min = %v, want 1", got)
		}
	})

	t.Run("predecessor beyond three wide buckets is not borrowed", func(t *testing.T) {
		// A 2-hour window in two buckets has 1-hour buckets, so the bound
		// is three hours and the bucket term, not the floor, decides. The
		// -26 h sample is a day before the window start and would report
		// 1000 records against 60 in every real bucket.
		window := TimeWindow{Start: base.Add(-2 * time.Hour), End: base}
		series, err := QueryTimeSeries(ctx, pool, lookbackTestProbe,
			[]int{4}, window, MetricFilters{}, 2, "avg",
			[]string{"wal_records_delta"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		d := seriesByMetric(t, series, "wal_records_delta")
		if got := pointAt(t, d, window.Start); got != 0 {
			t.Errorf("first bucket delta = %v, want 0 (predecessor out of reach)", got)
		}
		if got := nonZeroValues(t, d); len(got) != 2 || got[0] != 60 || got[1] != 60 {
			t.Errorf("non-zero deltas = %v, want [60 60]", got)
		}
	})

	t.Run("predecessor inside three wide buckets is borrowed", func(t *testing.T) {
		// Connection 5 runs the probe hourly, so a predecessor two and a
		// half hours back is within the gap bound as well as the lookback
		// bound, and the first bucket carries its 600 rise.
		window := TimeWindow{Start: base.Add(-2 * time.Hour), End: base}
		series, err := QueryTimeSeries(ctx, pool, lookbackTestProbe,
			[]int{5}, window, MetricFilters{}, 2, "avg",
			[]string{"wal_records_delta"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		d := seriesByMetric(t, series, "wal_records_delta")
		if got := pointAt(t, d, window.Start); got != 600 {
			t.Errorf("first bucket delta = %v, want 600 (borrowed predecessor)", got)
		}
	})

	t.Run("entity key that is not a plain identifier is rejected", func(t *testing.T) {
		// Every real metrics table uses plain identifiers, so this only
		// guards the interpolation path against a table someone adds later.
		const odd = "pg_odd_key_test"
		dropTable(ctx, pool, odd)
		defer dropTable(ctx, pool, odd)
		ddl := `CREATE TABLE metrics."` + odd + `" (
            connection_id integer NOT NULL,
            collected_at  timestamp with time zone NOT NULL,
            "if-name"     text NOT NULL,
            PRIMARY KEY (connection_id, collected_at, "if-name")
        )`
		if _, err := pool.Exec(ctx, ddl); err != nil {
			t.Fatalf("failed to create odd-key table: %v", err)
		}
		if _, err := GetProbeEntityKeyColumns(ctx, pool, odd); err == nil {
			t.Error("expected an error for a hyphenated entity key column")
		}
	})

	t.Run("entity key discovery error surfaces", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := GetProbeEntityKeyColumns(cctx, pool, lookbackTestProbe); err == nil {
			t.Error("expected an error from a canceled context")
		}
	})
}

// timeShareTestProbe is shaped like pg_stat_database's time columns, keyed
// per datname, for the _pct and _sessions derived kinds (issue #402).
const timeShareTestProbe = "pg_stat_database_ts_test"

// setupTimeShareFixture creates a probe table carrying a cumulative time
// counter (blk_read_time), a session time counter (active_time), a gauge
// (numbackends) and a plain counter (xact_commit), registered with those
// kinds via the test hook. Each time column rises 30000 ms per minute: half
// of the 60000 ms of wall-clock time, so blk_read_time_pct is exactly 50
// and active_time_sessions exactly 0.5; xact_commit rises 60 per minute.
// It returns the minute-truncated base time the offsets hang off.
func setupTimeShareFixture(t *testing.T, pool *pgxpool.Pool) (time.Time, func()) {
	t.Helper()
	ctx := context.Background()

	if _, err := pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS metrics"); err != nil {
		t.Fatalf("failed to create metrics schema: %v", err)
	}

	dropTable(ctx, pool, timeShareTestProbe)
	setProbeIntervalForTest(t, pool, timeShareTestProbe, 0, 60)
	registerProbeKindsForTest(t, timeShareTestProbe, probeRegistryEntry{
		kinds: columnKinds(
			counters("xact_commit"),
			timeCounters("blk_read_time"),
			sessionTimeCounters("active_time"),
		),
	})

	ddl := `CREATE TABLE metrics."` + timeShareTestProbe + `" (
        connection_id integer NOT NULL,
        collected_at  timestamp with time zone NOT NULL,
        inserted_at   timestamp without time zone NOT NULL DEFAULT now(),
        datname       text NOT NULL,
        numbackends   integer,
        xact_commit   bigint,
        blk_read_time double precision,
        active_time   double precision,
        PRIMARY KEY (connection_id, collected_at, datname)
    )`
	if _, err := pool.Exec(ctx, ddl); err != nil {
		t.Fatalf("failed to create time share fixture table: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Minute)
	insert := `INSERT INTO metrics."` + timeShareTestProbe + `"
        (connection_id, collected_at, datname, numbackends, xact_commit,
         blk_read_time, active_time)
        VALUES ($1, $2, $3, $4, $5, $6, $7)`
	for i := 0; i <= 4; i++ {
		offset := -time.Duration(4-i) * time.Minute
		_, err := pool.Exec(ctx, insert,
			1, now.Add(offset), "northwind", 3, 1000+60*i,
			float64(100000+30000*i), float64(500000+30000*i))
		if err != nil {
			dropTable(ctx, pool, timeShareTestProbe)
			t.Fatalf("failed to insert time share fixture sample %d: %v", i, err)
		}
	}

	return now, func() { dropTable(context.Background(), pool, timeShareTestProbe) }
}

func TestQueryTimeSeriesTimeShare_Integration(t *testing.T) {
	pool, closePool := newLatestRowsTestPool(t)
	defer closePool()
	base, cleanup := setupTimeShareFixture(t, pool)
	defer cleanup()

	ctx := context.Background()
	window := windowSince(base, 4)

	t.Run("pct and sessions values and units", func(t *testing.T) {
		series, err := QueryTimeSeries(ctx, pool, timeShareTestProbe,
			[]int{1}, window, MetricFilters{}, 4, "avg",
			[]string{
				"blk_read_time_pct", "active_time_sessions",
				"blk_read_time_delta", "xact_commit_per_sec", "numbackends",
			})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		pct := seriesByMetric(t, series, "blk_read_time_pct")
		if pct.Unit != "%" {
			t.Errorf("blk_read_time_pct unit = %q, want %%", pct.Unit)
		}
		sess := seriesByMetric(t, series, "active_time_sessions")
		if sess.Unit != "sessions" {
			t.Errorf("active_time_sessions unit = %q, want sessions", sess.Unit)
		}
		delta := seriesByMetric(t, series, "blk_read_time_delta")
		if delta.Unit != "ms" {
			t.Errorf("blk_read_time_delta unit = %q, want ms", delta.Unit)
		}
		rate := seriesByMetric(t, series, "xact_commit_per_sec")
		if rate.Unit != "/s" {
			t.Errorf("xact_commit_per_sec unit = %q, want /s", rate.Unit)
		}
		raw := seriesByMetric(t, series, "numbackends")
		if raw.Unit != "" {
			t.Errorf("raw column unit = %q, want empty", raw.Unit)
		}

		// The window opens on the earliest sample, which then has no LAG
		// and no derived value (a null rate, a zero delta); the remaining
		// four minutes each carry one, and every series has one point per
		// bucket.
		assertNullAt(t, pct, base.Add(-4*time.Minute))
		assertNullAt(t, sess, base.Add(-4*time.Minute))
		assertNullAt(t, rate, base.Add(-4*time.Minute))
		if got := pointAt(t, delta, base.Add(-4*time.Minute)); got != 0 {
			t.Errorf("blk_read_time_delta at the first sample = %v, want 0", got)
		}
		for _, s := range series {
			if len(s.Data) != len(pct.Data) {
				t.Errorf("%s has %d points, %s has %d; want identical lengths",
					s.Metric, len(s.Data), pct.Metric, len(pct.Data))
			}
		}
		for i := 1; i <= 4; i++ {
			ts := base.Add(-time.Duration(4-i) * time.Minute)
			if got := pointAt(t, pct, ts); math.Abs(got-50) > 1e-9 {
				t.Errorf("blk_read_time_pct at %s = %v, want 50", ts, got)
			}
			if got := pointAt(t, sess, ts); math.Abs(got-0.5) > 1e-9 {
				t.Errorf("active_time_sessions at %s = %v, want 0.5", ts, got)
			}
			if got := pointAt(t, delta, ts); math.Abs(got-30000) > 1e-9 {
				t.Errorf("blk_read_time_delta at %s = %v, want 30000", ts, got)
			}
			if got := pointAt(t, rate, ts); math.Abs(got-1) > 1e-9 {
				t.Errorf("xact_commit_per_sec at %s = %v, want 1", ts, got)
			}
		}
		if got := pointAt(t, raw, base); got != 3 {
			t.Errorf("numbackends at %s = %v, want 3", base, got)
		}
	})

	t.Run("kind mismatches are client errors", func(t *testing.T) {
		for _, tc := range []struct{ metric, want string }{
			{"numbackends_per_sec", `"numbackends" is a gauge`},
			{"blk_read_time_per_sec", `request "blk_read_time_pct"`},
			{"active_time_per_sec", `request "active_time_sessions"`},
			{"xact_commit_pct", `not a cumulative time counter`},
			{"blk_read_time_sessions", `not a session time counter`},
		} {
			_, err := QueryTimeSeries(ctx, pool, timeShareTestProbe,
				[]int{1}, window, MetricFilters{}, 4, "avg", []string{tc.metric})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%s: got %v, want an error containing %q", tc.metric, err, tc.want)
			}
		}
	})
}
