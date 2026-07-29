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
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The queryid filter scopes a time series to a single pg_stat_statements
// statement. Before it existed, the Query detail dashboard's execution time
// and call count charts aggregated every statement on the connection, so the
// tests below cover both the generated SQL and the guard that keeps the
// filter away from probes with no queryid column.

const queryIDTestProbe = "pg_stat_statements_qid_test"

// setupQueryIDFixture creates a trimmed pg_stat_statements-shaped probe table
// holding two statements sampled a minute apart, and returns a cleanup that
// drops it.
func setupQueryIDFixture(t *testing.T, pool *pgxpool.Pool) func() {
	t.Helper()
	ctx := context.Background()

	if _, err := pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS metrics"); err != nil {
		t.Fatalf("failed to create metrics schema: %v", err)
	}

	dropTable(ctx, pool, queryIDTestProbe)

	ddl := `CREATE TABLE metrics."` + queryIDTestProbe + `" (
        connection_id   integer NOT NULL,
        collected_at    timestamp with time zone NOT NULL,
        inserted_at     timestamp without time zone NOT NULL DEFAULT now(),
        database_name   text,
        queryid         bigint,
        calls           bigint,
        total_exec_time double precision
    )`
	if _, err := pool.Exec(ctx, ddl); err != nil {
		t.Fatalf("failed to create fixture table: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Minute)
	insert := `INSERT INTO metrics."` + queryIDTestProbe + `"
        (connection_id, collected_at, database_name, queryid, calls,
         total_exec_time)
        VALUES ($1, $2, $3, $4, $5, $6)`
	rows := []struct {
		offset  time.Duration
		queryID int64
		calls   int64
		total   float64
	}{
		{-3 * time.Minute, 1001, 10, 100},
		{-3 * time.Minute, 2002, 500, 5000},
		{-2 * time.Minute, 1001, 20, 300},
		{-2 * time.Minute, 2002, 900, 9000},
	}
	for i, r := range rows {
		if _, err := pool.Exec(ctx, insert, 1, now.Add(r.offset), "northwind",
			r.queryID, r.calls, r.total); err != nil {
			dropTable(ctx, pool, queryIDTestProbe)
			t.Fatalf("failed to insert fixture row %d: %v", i, err)
		}
	}

	return func() { dropTable(context.Background(), pool, queryIDTestProbe) }
}

// TestMetricQueryBase_QueryIDFilter asserts the placeholder numbering of the
// queryid clause on its own and behind every other filter, because it is the
// last filter appended and so is the one a numbering mistake would break.
func TestMetricQueryBase_QueryIDFilter(t *testing.T) {
	start := time.Now().UTC().Add(-time.Hour)
	end := time.Now().UTC()

	t.Run("queryid alone binds the first filter placeholder", func(t *testing.T) {
		where, args := metricQueryBase(1, start, end, time.Minute,
			MetricFilters{QueryID: "1001"})

		if !strings.Contains(where, `"queryid"::text = $5`) {
			t.Errorf("where clause missing queryid filter, got: %s", where)
		}
		if len(args) != 5 {
			t.Fatalf("expected 5 args, got %d: %v", len(args), args)
		}
		if args[4] != "1001" {
			t.Errorf("expected queryid arg \"1001\", got %v", args[4])
		}
	})

	t.Run("queryid binds after every other filter", func(t *testing.T) {
		where, args := metricQueryBase(1, start, end, time.Minute,
			MetricFilters{
				DatabaseName:   "northwind",
				DatabaseColumn: "database_name",
				SchemaName:     "public",
				TableName:      "orders",
				IndexName:      "pk_orders",
				QueryID:        "1001",
			})

		for _, want := range []string{
			`"database_name" = $5`,
			"schemaname = $6",
			"relname = $7",
			"indexrelname = $8",
			`"queryid"::text = $9`,
		} {
			if !strings.Contains(where, want) {
				t.Errorf("where clause missing %q, got: %s", want, where)
			}
		}
		if len(args) != 9 {
			t.Fatalf("expected 9 args, got %d: %v", len(args), args)
		}
		if args[8] != "1001" {
			t.Errorf("expected queryid arg \"1001\", got %v", args[8])
		}
	})

	t.Run("empty queryid adds no clause", func(t *testing.T) {
		where, args := metricQueryBase(1, start, end, time.Minute,
			MetricFilters{TableName: "orders"})
		if strings.Contains(where, "queryid") {
			t.Errorf("empty QueryID must not add a clause, got: %s", where)
		}
		if len(args) != 5 {
			t.Fatalf("expected 5 args, got %d", len(args))
		}
	})

	t.Run("the value never reaches the SQL text", func(t *testing.T) {
		const evil = "1 OR 1=1; DROP TABLE metrics.pg_stat_statements"
		where, args := metricQueryBase(1, start, end, time.Minute,
			MetricFilters{QueryID: evil})
		if strings.Contains(where, evil) {
			t.Errorf("queryid value was interpolated into SQL: %s", where)
		}
		if args[len(args)-1] != evil {
			t.Errorf("queryid value missing from args: %v", args)
		}
	})
}

// TestBuildQueriesWithQueryIDFilter confirms both query builders that share
// metricQueryBase carry the filter through to the generated statement.
func TestBuildQueriesWithQueryIDFilter(t *testing.T) {
	start := time.Now().UTC().Add(-time.Hour)
	end := time.Now().UTC()

	t.Run("raw column query", func(t *testing.T) {
		query, args, err := BuildMetricsQuery(
			queryIDTestProbe,
			[]string{"calls"},
			map[string]string{"calls": "bigint"},
			1, start, end, 60, "avg",
			MetricFilters{QueryID: "1001"},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(query, `"queryid"::text = $5`) {
			t.Errorf("query missing queryid filter:\n%s", query)
		}
		if len(args) != 5 || args[4] != "1001" {
			t.Errorf("unexpected args: %v", args)
		}
	})

	t.Run("derived metric query", func(t *testing.T) {
		query, args, err := BuildDerivedMetricsQuery(
			queryIDTestProbe,
			[]DerivedMetric{{
				OutputName: "calls_per_sec",
				BaseColumn: "calls",
				Kind:       DerivedPerSec,
			}},
			1, start, end, 60, "avg",
			MetricFilters{QueryID: "1001"},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(query, `"queryid"::text = $5`) {
			t.Errorf("query missing queryid filter:\n%s", query)
		}
		if len(args) != 5 || args[4] != "1001" {
			t.Errorf("unexpected args: %v", args)
		}
	})

	t.Run("latest rows query", func(t *testing.T) {
		query, args := buildLatestRowsQuery(
			queryIDTestProbe,
			[]string{"database_name", "queryid", "calls"},
			map[string]string{
				"database_name": "text",
				"queryid":       "bigint",
				"calls":         "bigint",
			},
			[]int{1},
			MetricFilters{
				DatabaseName:   "northwind",
				DatabaseColumn: "database_name",
				QueryID:        "1001",
			},
			"calls", "desc", 5,
		)

		if !strings.Contains(query, `"queryid"::text = $3`) {
			t.Errorf("query missing queryid filter:\n%s", query)
		}
		if !strings.Contains(query, "LIMIT $4") {
			t.Errorf("limit placeholder should follow the filters:\n%s", query)
		}
		if len(args) != 4 || args[2] != "1001" || args[3] != 5 {
			t.Errorf("unexpected args: %v", args)
		}
	})
}

// TestProbeHasColumn_Integration covers the catalog lookup behind the
// queryid support check.
func TestProbeHasColumn_Integration(t *testing.T) {
	pool, closePool := newLatestRowsTestPool(t)
	defer closePool()
	cleanup := setupQueryIDFixture(t, pool)
	defer cleanup()

	ctx := context.Background()

	tests := []struct {
		name   string
		probe  string
		column string
		want   bool
	}{
		{"column present", queryIDTestProbe, "queryid", true},
		{"column absent", queryIDTestProbe, "indexrelname", false},
		{"table absent", "no_such_probe_table", "queryid", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ProbeHasColumn(ctx, pool, tc.probe, tc.column)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("ProbeHasColumn = %v, want %v", got, tc.want)
			}
		})
	}

	t.Run("query failure surfaces", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := ProbeHasColumn(cctx, pool, queryIDTestProbe,
			"queryid"); err == nil {
			t.Fatal("expected an error from a canceled context")
		}
	})
}

// TestCheckQueryIDFilter_Integration covers the guard directly, including the
// error paths that the handler surfaces to callers as a 400.
func TestCheckQueryIDFilter_Integration(t *testing.T) {
	pool, closePool := newLatestRowsTestPool(t)
	defer closePool()
	cleanup := setupQueryIDFixture(t, pool)
	defer cleanup()

	ctx := context.Background()

	t.Run("no filter is always allowed", func(t *testing.T) {
		if err := checkQueryIDFilter(ctx, pool, "anything",
			MetricFilters{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("supported probe passes", func(t *testing.T) {
		if err := checkQueryIDFilter(ctx, pool, queryIDTestProbe,
			MetricFilters{QueryID: "1001"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("unsupported probe is rejected", func(t *testing.T) {
		err := checkQueryIDFilter(ctx, pool, "no_such_probe_table",
			MetricFilters{QueryID: "1001"})
		if err == nil {
			t.Fatal("expected an error for a probe without a queryid column")
		}
		if !strings.Contains(err.Error(), "does not support the queryid filter") {
			t.Errorf("unexpected error text: %v", err)
		}
	})

	t.Run("lookup failure surfaces", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		err := checkQueryIDFilter(cctx, pool, queryIDTestProbe,
			MetricFilters{QueryID: "1001"})
		if err == nil {
			t.Fatal("expected an error from a canceled context")
		}
		if !strings.Contains(err.Error(), "failed to resolve queryid column") {
			t.Errorf("unexpected error text: %v", err)
		}
	})
}

// TestQueryTimeSeries_QueryIDFilter_Integration proves the filter really does
// scope the series to one statement, which is the bug it was added to fix,
// and that an unsupported probe fails cleanly rather than reaching PostgreSQL
// as an undefined-column error.
func TestQueryTimeSeries_QueryIDFilter_Integration(t *testing.T) {
	pool, closePool := newLatestRowsTestPool(t)
	defer closePool()
	cleanup := setupQueryIDFixture(t, pool)
	defer cleanup()

	tsCleanup := setupTimeSeriesFixture(t, pool)
	defer tsCleanup()

	ctx := context.Background()

	// lastValue returns the final non-zero data point of the named series.
	lastValue := func(t *testing.T, series []MetricSeries, name string) float64 {
		t.Helper()
		for _, s := range series {
			if s.Metric != name {
				continue
			}
			if len(s.Data) == 0 {
				t.Fatalf("series %q has no data points", name)
			}
			return s.Data[len(s.Data)-1].Value
		}
		t.Fatalf("series %q not found", name)
		return 0
	}

	t.Run("scopes the series to one statement", func(t *testing.T) {
		filtered, err := QueryTimeSeries(ctx, pool, queryIDTestProbe,
			[]int{1}, "1h", MetricFilters{QueryID: "1001"}, 60, "max",
			[]string{"calls"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := lastValue(t, filtered, "calls"); got != 20 {
			t.Errorf("filtered calls = %v, want 20", got)
		}

		// Without the filter the two statements are summed into one series,
		// which is exactly the aggregation bug the parameter fixes.
		unfiltered, err := QueryTimeSeries(ctx, pool, queryIDTestProbe,
			[]int{1}, "1h", MetricFilters{}, 60, "max", []string{"calls"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := lastValue(t, unfiltered, "calls"); got != 900 {
			t.Errorf("unfiltered calls = %v, want 900", got)
		}
	})

	t.Run("composes with the database filter", func(t *testing.T) {
		series, err := QueryTimeSeries(ctx, pool, queryIDTestProbe,
			[]int{1}, "1h",
			MetricFilters{DatabaseName: "northwind", QueryID: "2002"},
			60, "max", []string{"calls"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := lastValue(t, series, "calls"); got != 900 {
			t.Errorf("calls = %v, want 900", got)
		}
	})

	t.Run("unknown queryid yields no data", func(t *testing.T) {
		series, err := QueryTimeSeries(ctx, pool, queryIDTestProbe,
			[]int{1}, "1h", MetricFilters{QueryID: "404404"}, 60, "max",
			[]string{"calls"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Every bucket is empty, so the series comes back present but with
		// no data points at all rather than with a run of zeroes.
		found := false
		for _, s := range series {
			if s.Metric != "calls" {
				continue
			}
			found = true
			if len(s.Data) != 0 {
				t.Errorf("expected no data points, got %d: %v", len(s.Data),
					s.Data)
			}
		}
		if !found {
			t.Fatal("series \"calls\" not found")
		}
	})

	t.Run("probe without a queryid column is rejected", func(t *testing.T) {
		_, err := QueryTimeSeries(ctx, pool, timeSeriesTestProbe,
			[]int{1}, "1h", MetricFilters{QueryID: "1001"}, 60, "avg",
			[]string{"seq_scan"})
		if err == nil {
			t.Fatal("expected an error for a probe without a queryid column")
		}
		if !strings.Contains(err.Error(), "does not support the queryid filter") {
			t.Errorf("unexpected error text: %v", err)
		}
	})
}

// TestQueryLatestRows_QueryIDFilter_Integration covers the same filter on the
// latest-row path, where the support check runs against the columns already
// discovered for the probe.
func TestQueryLatestRows_QueryIDFilter_Integration(t *testing.T) {
	pool, closePool := newLatestRowsTestPool(t)
	defer closePool()
	cleanup := setupQueryIDFixture(t, pool)
	defer cleanup()

	tsCleanup := setupTimeSeriesFixture(t, pool)
	defer tsCleanup()

	ctx := context.Background()

	t.Run("returns only the requested statement", func(t *testing.T) {
		rows, err := QueryLatestRows(ctx, pool, queryIDTestProbe, []int{1},
			MetricFilters{QueryID: "1001"}, "calls", "desc", 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("expected 1 row, got %d: %v", len(rows), rows)
		}
		if got := rows[0]["calls"]; got != int64(20) {
			t.Errorf("calls = %v, want 20", got)
		}
	})

	t.Run("probe without a queryid column is rejected", func(t *testing.T) {
		_, err := QueryLatestRows(ctx, pool, timeSeriesTestProbe, []int{1},
			MetricFilters{QueryID: "1001"}, "seq_scan", "desc", 10)
		if err == nil {
			t.Fatal("expected an error for a probe without a queryid column")
		}
		if !strings.Contains(err.Error(), "does not support the queryid filter") {
			t.Errorf("unexpected error text: %v", err)
		}
	})
}
