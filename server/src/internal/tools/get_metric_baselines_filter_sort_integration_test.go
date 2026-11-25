/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Integration tests for the get_metric_baselines period_type filter, the
// order_by sort orders and the row limit maximum.

package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// baselineModeArgs returns args with connection_id set to connID in
// single-connection mode and left out in multi-connection mode.
func baselineModeArgs(mode string, connID int, args map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range args {
		out[k] = v
	}
	if mode == "single" {
		out["connection_id"] = connID
	}
	return out
}

// baselineColumn returns column name of a data row, given the mode the
// row came from: multi-connection rows carry two leading columns.
func baselineColumn(t *testing.T, row, mode, name string) string {
	t.Helper()
	cols := []string{"metric_name", "period_type", "day_of_week", "hour_of_day",
		"mean", "stddev", "min", "max", "sample_count"}
	if mode == "multi" {
		cols = append([]string{"connection_id", "connection_name"}, cols...)
	}
	fields := strings.Split(row, "\t")
	for i, c := range cols {
		if c == name && i < len(fields) {
			return fields[i]
		}
	}
	t.Fatalf("no column %q in row %q", name, row)
	return ""
}

// baselineFloat parses a numeric column of a data row.
func baselineFloat(t *testing.T, row, mode, name string) float64 {
	t.Helper()
	v, err := strconv.ParseFloat(baselineColumn(t, row, mode, name), 64)
	if err != nil {
		t.Fatalf("column %q of %q is not a number: %v", name, row, err)
	}
	return v
}

// TestGetMetricBaselinesPeriodTypeFilterIntegration verifies that
// period_type narrows both the returned rows and the total in the count
// line, in both modes.
func TestGetMetricBaselinesPeriodTypeFilterIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	connA := seedBaselineConnection(t, pool, "period-a", true, "")
	connB := seedBaselineConnection(t, pool, "period-b", true, "")
	seedAlerterShapedBaselines(t, pool, connA, 20, 1)
	seedAlerterShapedBaselines(t, pool, connB, 20, 1)

	tool := asSuperuser(GetMetricBaselinesTool(pool, testRBACChecker(t), nil, nil))

	cases := []struct {
		periodType string
		perConn    int
	}{
		{"all", 20},
		{"hourly", 20 * 24},
		{"daily", 20 * 7},
	}
	for _, tc := range cases {
		for _, mode := range []string{"single", "multi"} {
			t.Run(tc.periodType+"/"+mode, func(t *testing.T) {
				total := tc.perConn
				if mode == "multi" {
					total *= 2
				}
				body := mustSuccess(t, tool, baselineModeArgs(mode, connA,
					map[string]any{"period_type": tc.periodType}))
				countLine, rows := baselineOutput(t, body)
				want := fmt.Sprintf("Showing all %d baselines", total)
				if total > baselinesDefaultLimit {
					want = fmt.Sprintf("Showing rows 1-%d of %d baselines (more available - use offset=%d for next page)",
						baselinesDefaultLimit, total, baselinesDefaultLimit)
				}
				if countLine != want {
					t.Errorf("count line = %q; want %q", countLine, want)
				}
				for _, r := range rows {
					if got := baselineColumn(t, r, mode, "period_type"); got != tc.periodType {
						t.Fatalf("row has period_type %q; want only %q: %s", got, tc.periodType, r)
					}
				}
			})
		}
	}

	t.Run("filter past the end", func(t *testing.T) {
		body := mustSuccess(t, tool, map[string]any{
			"connection_id": connA, "period_type": "all", "offset": float64(50),
		})
		want := "No rows at offset 50: 20 baselines match, so use an offset below 20."
		if body != want {
			t.Errorf("got %q; want %q", body, want)
		}
	})

	t.Run("filter with no matching metric", func(t *testing.T) {
		body := mustSuccess(t, tool, map[string]any{
			"connection_id": connA, "period_type": "daily", "metric_name": "no_such_metric",
		})
		if !strings.Contains(body, `No metric baselines matched "no_such_metric" with period_type daily`) {
			t.Errorf("expected the no-match helper naming the filter, got %q", body)
		}
	})
}

// TestGetMetricBaselinesOrderByIntegration walks every page of every
// sort order, following the offset each count line names, and checks
// that the rows come back in the requested order and that the pages
// together return each row exactly once. Means and standard deviations
// are seeded with many ties, so this also proves each order is total.
func TestGetMetricBaselinesOrderByIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	connA := seedBaselineConnection(t, pool, "order-a", true, "")
	connB := seedBaselineConnection(t, pool, "order-b", true, "")
	seedAlerterShapedBaselines(t, pool, connA, 3, 2)
	seedAlerterShapedBaselines(t, pool, connB, 2, 2)
	// sample_count keeps every row's text distinct once means tie.
	if _, err := pool.Exec(context.Background(),
		`UPDATE metric_baselines SET mean = id % 7, stddev = id % 5, sample_count = id`); err != nil {
		t.Fatalf("failed to tie baselines: %v", err)
	}
	totalA := 3 * baselineShapesPerMetric * 2
	totalB := 2 * baselineShapesPerMetric * 2

	tool := asSuperuser(GetMetricBaselinesTool(pool, testRBACChecker(t), nil, nil))

	// inOrder reports whether row b may follow row a under an order.
	inOrder := map[string]func(t *testing.T, a, b, mode string) bool{
		"metric": func(t *testing.T, a, b, mode string) bool {
			ka := baselineColumn(t, a, mode, "metric_name")
			kb := baselineColumn(t, b, mode, "metric_name")
			if mode == "multi" {
				ka = baselineColumn(t, a, mode, "connection_name") + "\x00" + ka
				kb = baselineColumn(t, b, mode, "connection_name") + "\x00" + kb
			}
			return ka <= kb
		},
		"mean_desc": func(t *testing.T, a, b, mode string) bool {
			return baselineFloat(t, a, mode, "mean") >= baselineFloat(t, b, mode, "mean")
		},
		"mean_asc": func(t *testing.T, a, b, mode string) bool {
			return baselineFloat(t, a, mode, "mean") <= baselineFloat(t, b, mode, "mean")
		},
		"stddev_desc": func(t *testing.T, a, b, mode string) bool {
			return baselineFloat(t, a, mode, "stddev") >= baselineFloat(t, b, mode, "stddev")
		},
	}

	for _, order := range []string{"metric", "mean_desc", "mean_asc", "stddev_desc"} {
		for _, mode := range []string{"single", "multi"} {
			t.Run(order+"/"+mode, func(t *testing.T) {
				total := totalA
				if mode == "multi" {
					total += totalB
				}
				const limit = 23
				seen := make(map[string]int)
				var all []string
				offset := 0
				for pages := 0; ; pages++ {
					if pages > total {
						t.Fatal("paging did not terminate")
					}
					countLine, rows := baselineOutput(t, mustSuccess(t, tool, baselineModeArgs(mode, connA,
						map[string]any{"order_by": order, "limit": float64(limit), "offset": float64(offset)})))
					for _, r := range rows {
						seen[r]++
					}
					all = append(all, rows...)
					m := nextOffsetPattern.FindStringSubmatch(countLine)
					if m == nil {
						break
					}
					next, err := strconv.Atoi(m[1])
					if err != nil || next != offset+limit || len(rows) != limit {
						t.Fatalf("page at offset %d: %d rows, count line %q", offset, len(rows), countLine)
					}
					offset = next
				}
				if len(seen) != total || len(all) != total {
					t.Errorf("saw %d rows, %d distinct; want %d", len(all), len(seen), total)
				}
				for r, n := range seen {
					if n != 1 {
						t.Errorf("row returned %d times: %s", n, r)
					}
				}
				for i := 1; i < len(all); i++ {
					if !inOrder[order](t, all[i-1], all[i], mode) {
						t.Fatalf("rows %d and %d out of %s order:\n%s\n%s", i-1, i, order, all[i-1], all[i])
					}
				}
			})
		}
	}
}

// TestGetMetricBaselinesInvalidFilterSortIntegration verifies that an
// unknown period_type or order_by is rejected with an error stating the
// valid values, and that the limit maximum is enforced.
func TestGetMetricBaselinesInvalidFilterSortIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	connID := seedBaselineConnection(t, pool, "invalid-sort", true, "")
	seedAlerterShapedBaselines(t, pool, connID, 20, 1)

	tool := asSuperuser(GetMetricBaselinesTool(pool, testRBACChecker(t), nil, nil))

	periodErr := "Invalid 'period_type' parameter: must be one of all, hourly, daily"
	orderErr := "Invalid 'order_by' parameter: must be one of metric, mean_desc, mean_asc, stddev_desc"
	limitErr := "Invalid 'limit' parameter: must be an integer between 1 and 100"

	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"weekly period", map[string]any{"period_type": "weekly"}, periodErr},
		{"unknown period", map[string]any{"period_type": "monthly"}, periodErr},
		{"numeric period", map[string]any{"period_type": float64(1)}, periodErr},
		{"unknown order", map[string]any{"order_by": "mean"}, orderErr},
		{"sql in order", map[string]any{"order_by": "mean DESC; DROP TABLE metric_baselines"}, orderErr},
		{"numeric order", map[string]any{"order_by": float64(1)}, orderErr},
		{"limit above maximum", map[string]any{"limit": float64(101)}, limitErr},
		{"former maximum", map[string]any{"limit": float64(300)}, limitErr},
	}
	for _, tc := range cases {
		for _, mode := range []string{"single", "multi"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				resp, err := tool.Handler(baselineModeArgs(mode, connID, tc.args))
				if err != nil {
					t.Fatalf("handler returned unexpected error: %v", err)
				}
				if !resp.IsError || len(resp.Content) == 0 {
					t.Fatalf("expected an error response, got: %+v", resp)
				}
				if !strings.Contains(resp.Content[0].Text, tc.want) {
					t.Errorf("got %q; want it to contain %q", resp.Content[0].Text, tc.want)
				}
			})
		}
	}

	t.Run("maximum limit accepted", func(t *testing.T) {
		_, rows := baselineOutput(t, mustSuccess(t, tool, map[string]any{
			"connection_id": connID, "limit": float64(100),
		}))
		if len(rows) != 100 {
			t.Errorf("got %d rows; want 100", len(rows))
		}
	})
}

// TestGetMetricBaselinesHighestHourlyIntegration runs the call the tool
// description recommends for a "highest hourly baseline" question over
// a connection carrying the alerter's 20 metrics, with larger 'all' and
// 'daily' means planted as decoys, and checks that the planted highest
// hourly row comes back first in a small result.
func TestGetMetricBaselinesHighestHourlyIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	connID := seedBaselineConnection(t, pool, "highest-hourly", true, "")
	seedAlerterShapedBaselines(t, pool, connID, 20, 1)
	for _, plant := range []string{
		`UPDATE metric_baselines SET mean = 5000000 WHERE metric_name = 'pg_stat_database.metric_13'
             AND period_type = 'hourly' AND hour_of_day = 14`,
		`UPDATE metric_baselines SET mean = 6000000 WHERE metric_name = 'pg_stat_database.metric_05'
             AND period_type = 'daily' AND day_of_week = 3`,
		`UPDATE metric_baselines SET mean = 7000000 WHERE metric_name = 'pg_stat_database.metric_09'
             AND period_type = 'all'`,
	} {
		if _, err := pool.Exec(context.Background(), plant); err != nil {
			t.Fatalf("failed to plant baseline: %v", err)
		}
	}

	tool := asSuperuser(GetMetricBaselinesTool(pool, testRBACChecker(t), nil, nil))

	body := mustSuccess(t, tool, map[string]any{
		"connection_id": connID,
		"period_type":   "hourly",
		"order_by":      "mean_desc",
		"limit":         float64(5),
	})
	t.Logf("example call output: %d characters\n%s", len(body), body)

	countLine, rows := baselineOutput(t, body)
	want := "Showing rows 1-5 of 480 baselines (more available - use offset=5 for next page)"
	if countLine != want {
		t.Errorf("count line = %q; want %q", countLine, want)
	}
	if len(rows) != 5 {
		t.Fatalf("got %d rows; want 5", len(rows))
	}
	first := rows[0]
	if baselineColumn(t, first, "single", "metric_name") != "pg_stat_database.metric_13" ||
		baselineColumn(t, first, "single", "period_type") != "hourly" ||
		baselineColumn(t, first, "single", "hour_of_day") != "14" ||
		baselineFloat(t, first, "single", "mean") != 5000000 {
		t.Errorf("first row = %q; want the planted metric_13 hour 14 row", first)
	}
	if len(body) > 1000 {
		t.Errorf("example call output is %d characters; want at most 1000", len(body))
	}
}
