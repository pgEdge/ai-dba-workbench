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

	t.Run("query error surfaces", func(t *testing.T) {
		// A canceled context fails when the pool tries to acquire a
		// connection, so pool.Query returns an error before any rows scan.
		cctx, cancel := context.WithCancel(context.Background())
		cancel()
		dataMap := map[seriesKey][]MetricDataPoint{}
		lastKnown := map[string]float64{}
		err := scanSeriesRows(cctx, pool, "SELECT now(), 1", nil,
			[]string{"m"}, 1, dataMap, lastKnown)
		if err == nil {
			t.Fatal("expected error from canceled context")
		}
	})

	t.Run("scan error on destination mismatch", func(t *testing.T) {
		// Three result columns but only two scan destinations (bucket time
		// plus one metric) forces a scan destination mismatch.
		dataMap := map[seriesKey][]MetricDataPoint{}
		lastKnown := map[string]float64{}
		err := scanSeriesRows(context.Background(), pool, "SELECT now(), 1, 2",
			nil, []string{"only_one"}, 1, dataMap, lastKnown)
		if err == nil {
			t.Fatal("expected scan error for mismatched destination count")
		}
	})

	t.Run("LOCF carries prior value across a NULL gap", func(t *testing.T) {
		// The query yields a real value, then a NULL, then a real value;
		// scanSeriesRows must carry the prior value forward across the gap
		// rather than dropping the bucket.
		dataMap := map[seriesKey][]MetricDataPoint{}
		lastKnown := map[string]float64{}
		query := `SELECT b, v FROM (VALUES
            (now() - interval '2 min', 5::float8),
            (now() - interval '1 min', NULL::float8),
            (now(),                    9::float8)
        ) AS t(b, v) ORDER BY b`
		err := scanSeriesRows(context.Background(), pool, query, nil,
			[]string{"m"}, 1, dataMap, lastKnown)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		data := dataMap[seriesKey{metric: "m", connectionID: 1}]
		if len(data) != 3 {
			t.Fatalf("expected 3 points (with LOCF fill), got %d", len(data))
		}
		if data[0].Value != 5 || data[1].Value != 5 || data[2].Value != 9 {
			t.Errorf("unexpected LOCF sequence: %v %v %v",
				data[0].Value, data[1].Value, data[2].Value)
		}
	})

	t.Run("leading NULL with no prior value is skipped", func(t *testing.T) {
		// A NULL in the very first bucket has no prior value to carry, so it
		// is dropped; the following real value is retained.
		dataMap := map[seriesKey][]MetricDataPoint{}
		lastKnown := map[string]float64{}
		query := `SELECT b, v FROM (VALUES
            (now() - interval '1 min', NULL::float8),
            (now(),                    7::float8)
        ) AS t(b, v) ORDER BY b`
		err := scanSeriesRows(context.Background(), pool, query, nil,
			[]string{"m"}, 1, dataMap, lastKnown)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		data := dataMap[seriesKey{metric: "m", connectionID: 1}]
		if len(data) != 1 {
			t.Fatalf("expected 1 point (leading NULL skipped), got %d", len(data))
		}
		if data[0].Value != 7 {
			t.Errorf("retained point = %v, want 7", data[0].Value)
		}
	})
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
		// carrying forward must preserve that value.
		for _, p := range s.Data {
			if p.Value != 90 {
				t.Errorf("raw n_live_tup point = %v, want 90", p.Value)
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
		for _, p := range s.Data {
			if p.Value < 0 {
				t.Errorf("rate point = %v, want non-negative", p.Value)
			}
			if p.Value >= 0.9 && p.Value <= 1.1 {
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
		for _, p := range s.Data {
			if p.Value < 0 {
				t.Errorf("rate point = %v, want non-negative", p.Value)
			}
			if p.Value >= 0.9 && p.Value <= 1.1 {
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
		for _, p := range s.Data {
			if p.Value < 0 || p.Value > 100 {
				t.Errorf("ratio point = %v, want within [0,100]", p.Value)
			}
			if p.Value >= 9.0 && p.Value <= 11.0 {
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

	t.Run("raw query execution error propagates", func(t *testing.T) {
		// An aggregation that names no real SQL function makes the built raw
		// query fail at execution, exercising the raw-path error return.
		_, err := QueryTimeSeries(ctx, pool, timeSeriesTestProbe,
			[]int{1}, lastHourWindow(), MetricFilters{}, 60, "no_such_agg",
			[]string{"seq_scan"})
		if err == nil {
			t.Fatal("expected error from failing raw query")
		}
	})

	t.Run("derived query execution error propagates", func(t *testing.T) {
		// The same invalid aggregation makes the derived rate query fail at
		// execution, exercising the derived-path error return.
		_, err := QueryTimeSeries(ctx, pool, timeSeriesTestProbe,
			[]int{1}, lastHourWindow(), MetricFilters{}, 60, "no_such_agg",
			[]string{"seq_scan_per_sec"})
		if err == nil {
			t.Fatal("expected error from failing derived query")
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
		for _, p := range s.Data {
			if p.Value < 0 {
				t.Errorf("rate point = %v, want non-negative", p.Value)
			}
			if p.Value >= 0.9 && p.Value <= 1.1 {
				sawExpected = true
			}
		}
		if !sawExpected {
			t.Errorf("expected an idx_scan rate near 1.0/sec, got %v", s.Data)
		}
	})

	t.Run("index_name filter narrows out non-matching indexes", func(t *testing.T) {
		// A different index name matches no rows, so the series is present but
		// empty; this is what previously happened for every index because the
		// filter was silently dropped and the wrong dimension was queried.
		series, err := QueryTimeSeries(ctx, pool, timeSeriesTestProbe,
			[]int{1}, lastHourWindow(),
			MetricFilters{IndexName: "some_other_index"},
			60, "avg", []string{"idx_scan_per_sec"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		s := seriesByMetric(t, series, "idx_scan_per_sec")
		if len(s.Data) != 0 {
			t.Errorf("expected no data for a non-matching index, got %d points",
				len(s.Data))
		}
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
		if first.Value < 0.9 || first.Value > 1.1 {
			t.Errorf("first rate = %v, want near 1.0/sec", first.Value)
		}
	})

	t.Run("no sample before the window leaves the first bucket empty", func(t *testing.T) {
		// With the window opening on the earliest sample there is nothing
		// earlier to borrow, so that sample still has no LAG and its bucket
		// is dropped by LOCF: the behavior is unchanged from before the fix.
		window := windowSince(base, 5)
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
		wantFirst := window.Start.Add(time.Minute)
		if !first.Time.Equal(wantFirst) {
			t.Errorf("first point at %s, want %s (the second sample)",
				first.Time, wantFirst)
		}
		if first.Value < 0.9 || first.Value > 1.1 {
			t.Errorf("first rate = %v, want near 1.0/sec", first.Value)
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
		s := seriesByMetric(t, series, "idx_scan_per_sec")
		if len(s.Data) != 0 {
			t.Errorf("expected no data for a non-matching index, got %d points",
				len(s.Data))
		}
	})

	t.Run("raw column request honors index_name filter", func(t *testing.T) {
		// The raw-column path shares metricQueryBase, so IndexName must scope
		// it too; a non-matching index yields an empty raw series.
		series, err := QueryTimeSeries(ctx, pool, timeSeriesTestProbe,
			[]int{1}, lastHourWindow(),
			MetricFilters{IndexName: "no_such_index"},
			60, "avg", []string{"idx_scan"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		s := seriesByMetric(t, series, "idx_scan")
		if len(s.Data) != 0 {
			t.Errorf("expected no raw data for a non-matching index, got %d points",
				len(s.Data))
		}
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
		// sample: sample-less buckets are COALESCEd to 0 rather than left
		// NULL for the caller's LOCF fill to duplicate.
		if len(s.Data) < 50 {
			t.Fatalf("expected the whole window to be filled, got %d points",
				len(s.Data))
		}

		var total float64
		var nonZero []float64
		for _, p := range s.Data {
			if p.Value < 0 {
				t.Errorf("delta point = %v, want non-negative", p.Value)
			}
			total += p.Value
			if p.Value != 0 {
				nonZero = append(nonZero, p.Value)
			}
		}

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

		// The sample-less minute sits between the 20 and the 70, so at least
		// one zero-valued bucket must separate them.
		zeros := len(s.Data) - len(nonZero)
		if zeros == 0 {
			t.Error("expected zero-filled buckets between the samples")
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
		var total float64
		for _, p := range d.Data {
			total += p.Value
		}
		if total != 120 {
			t.Errorf("summed deltas alongside per_sec = %v, want 120", total)
		}
		r := seriesByMetric(t, series, "seq_scan_per_sec")
		if len(r.Data) == 0 {
			t.Error("expected per-second rate data alongside the delta")
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
		if s.Data[0].Value != 20 {
			t.Errorf("first bucket delta = %v, want 20 (the rise since the "+
				"sample before the window)", s.Data[0].Value)
		}

		var total float64
		var nonZero []float64
		for _, p := range s.Data {
			if p.Value < 0 {
				t.Errorf("delta point = %v, want non-negative", p.Value)
			}
			total += p.Value
			if p.Value != 0 {
				nonZero = append(nonZero, p.Value)
			}
		}
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
		if s.Data[0].Value != 0 {
			t.Errorf("first bucket delta = %v, want 0 (no earlier sample)",
				s.Data[0].Value)
		}
		var total float64
		var nonZero []float64
		for _, p := range s.Data {
			total += p.Value
			if p.Value != 0 {
				nonZero = append(nonZero, p.Value)
			}
		}
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
