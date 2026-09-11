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
// on. It inserts four connection-1 samples spaced one minute apart across the
// last few minutes so they fall inside a "1h" query window. The cumulative
// counters advance by a fixed amount per minute, giving a clean 1.0-per-second
// rate, whilst n_live_tup and n_dead_tup stay constant at 90 and 10 so the
// dead-tuple ratio is a steady 10 percent. It returns a cleanup that drops the
// table.
func setupTimeSeriesFixture(t *testing.T, pool *pgxpool.Pool) func() {
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
		{-4 * time.Minute, 100, 1000, 50},
		{-3 * time.Minute, 160, 1060, 110},
		{-2 * time.Minute, 220, 1120, 170},
		{-1 * time.Minute, 280, 1180, 230},
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

	return func() { dropTable(context.Background(), pool, timeSeriesTestProbe) }
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
	cleanup := setupTimeSeriesFixture(t, pool)
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
