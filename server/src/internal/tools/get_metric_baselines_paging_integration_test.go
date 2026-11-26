/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Integration tests for get_metric_baselines paging: the default row
// limit, the count line at the head of the output, offset paging and the
// rejection of invalid limit and offset values.

package tools

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// baselineShapesPerMetric is the number of rows the alerter writes for one
// metric: one 'all' row, 24 'hourly' rows and 7 'daily' rows.
const baselineShapesPerMetric = 1 + 24 + 7

// seedAlerterShapedBaselines writes, for each of metrics metric names,
// the rows the alerter writes for one metric, copies times over. Copies
// stand in for the per-database rows the real table holds, which share a
// sort key; every row gets a distinct mean so tests can tell rows apart.
func seedAlerterShapedBaselines(t *testing.T, pool *pgxpool.Pool, connID, metrics, copies int) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
        INSERT INTO metric_baselines
            (connection_id, metric_name, period_type, day_of_week,
             hour_of_day, mean, stddev, min, max, sample_count)
        SELECT $1,
               'pg_stat_database.metric_' || lpad(m::text, 2, '0'),
               s.period_type, s.day_of_week, s.hour_of_day,
               row_number() OVER () * 1000.123456, 3456.7891,
               12.3456, 98765.4321, 123456
        FROM generate_series(1, $2) AS m
        CROSS JOIN (
            SELECT 'all' AS period_type, NULL::int AS day_of_week,
                   NULL::int AS hour_of_day
            UNION ALL
            SELECT 'hourly', NULL, h FROM generate_series(0, 23) AS h
            UNION ALL
            SELECT 'daily', d, NULL FROM generate_series(0, 6) AS d
        ) AS s
        CROSS JOIN generate_series(1, $3) AS c`,
		connID, metrics, copies)
	if err != nil {
		t.Fatalf("failed to seed baselines: %v", err)
	}
}

// baselineOutput splits a get_metric_baselines result into its count
// line (the second line) and its data rows (the lines after the TSV
// header up to the first blank line).
func baselineOutput(t *testing.T, body string) (countLine string, rows []string) {
	t.Helper()
	lines := strings.Split(body, "\n")
	if len(lines) > 1 {
		countLine = lines[1]
	}
	header := -1
	for i, line := range lines {
		if strings.Contains(line, "metric_name\tperiod_type") {
			header = i
			break
		}
	}
	if header < 0 {
		t.Fatalf("no TSV header in output: %s", body)
	}
	for _, line := range lines[header+1:] {
		if line == "" {
			break
		}
		rows = append(rows, line)
	}
	return countLine, rows
}

// nextOffsetPattern extracts the offset the count line tells the caller
// to use for the next page.
var nextOffsetPattern = regexp.MustCompile(`use offset=(\d+) for next page`)

// TestGetMetricBaselinesDefaultBoundedIntegration verifies that a
// connection carrying the alerter's 20 metrics returns a bounded default
// page, in both single- and multi-connection mode, with the count line
// at the head of the output and not at its tail.
func TestGetMetricBaselinesDefaultBoundedIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	connA := seedBaselineConnection(t, pool, "paging-a", true, "")
	connB := seedBaselineConnection(t, pool, "paging-b", true, "")
	seedAlerterShapedBaselines(t, pool, connA, 20, 1)
	seedAlerterShapedBaselines(t, pool, connB, 20, 1)
	perConn := 20 * baselineShapesPerMetric

	tool := asSuperuser(GetMetricBaselinesTool(pool, testRBACChecker(t), nil, nil))

	cases := []struct {
		name  string
		args  map[string]any
		total int
	}{
		{"single connection", map[string]any{"connection_id": connA}, perConn},
		{"all connections", map[string]any{}, 2 * perConn},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := mustSuccess(t, tool, tc.args)
			t.Logf("default output: %d characters", len(body))
			if len(body) >= 20000 {
				t.Errorf("default output is %d characters; want under 20000", len(body))
			}
			countLine, rows := baselineOutput(t, body)
			want := fmt.Sprintf("Showing rows 1-%d of %d baselines (more available - use offset=%d for next page)",
				baselinesDefaultLimit, tc.total, baselinesDefaultLimit)
			if countLine != want {
				t.Errorf("count line = %q; want %q", countLine, want)
			}
			if len(rows) != baselinesDefaultLimit {
				t.Errorf("got %d rows; want %d", len(rows), baselinesDefaultLimit)
			}
			for i, line := range strings.Split(body, "\n") {
				if i != 1 && strings.HasPrefix(line, "Showing rows ") {
					t.Errorf("count repeated at line %d: %q", i+1, line)
				}
			}
		})
	}
}

// TestGetMetricBaselinesOffsetPagingIntegration walks every page by
// following the offset each count line names, and checks that the pages
// together return each row exactly once. The seeded rows share sort keys
// (as per-database rows do in the real table), so this also proves the
// ordering is total.
func TestGetMetricBaselinesOffsetPagingIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	connA := seedBaselineConnection(t, pool, "walk-a", true, "")
	connB := seedBaselineConnection(t, pool, "walk-b", true, "")
	seedAlerterShapedBaselines(t, pool, connA, 3, 5)
	seedAlerterShapedBaselines(t, pool, connB, 2, 5)
	totalA := 3 * baselineShapesPerMetric * 5
	totalB := 2 * baselineShapesPerMetric * 5

	tool := asSuperuser(GetMetricBaselinesTool(pool, testRBACChecker(t), nil, nil))

	cases := []struct {
		name  string
		args  map[string]any
		total int
	}{
		{"single connection", map[string]any{"connection_id": connA}, totalA},
		{"all connections", map[string]any{}, totalA + totalB},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const limit = 37
			seen := make(map[string]int)
			offset := 0
			pages := 0
			for {
				args := map[string]any{"limit": float64(limit), "offset": float64(offset)}
				for k, v := range tc.args {
					args[k] = v
				}
				countLine, rows := baselineOutput(t, mustSuccess(t, tool, args))
				pages++
				for _, r := range rows {
					seen[r]++
				}
				m := nextOffsetPattern.FindStringSubmatch(countLine)
				if m == nil {
					want := fmt.Sprintf("Showing rows %d-%d of %d baselines (last page)",
						offset+1, tc.total, tc.total)
					if countLine != want {
						t.Errorf("last count line = %q; want %q", countLine, want)
					}
					break
				}
				next, err := strconv.Atoi(m[1])
				if err != nil {
					t.Fatalf("bad next offset in %q: %v", countLine, err)
				}
				if next != offset+limit || len(rows) != limit {
					t.Fatalf("page at offset %d: %d rows, next offset %d; want %d rows, next %d",
						offset, len(rows), next, limit, offset+limit)
				}
				offset = next
				if pages > tc.total {
					t.Fatal("paging did not terminate")
				}
			}
			if len(seen) != tc.total {
				t.Errorf("saw %d distinct rows; want %d", len(seen), tc.total)
			}
			for r, n := range seen {
				if n != 1 {
					t.Errorf("row returned %d times: %s", n, r)
				}
			}
		})
	}
}

// TestGetMetricBaselinesCountLineIntegration checks the count line for a
// result that fits on one page and for an offset beyond the last row.
func TestGetMetricBaselinesCountLineIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	connID := seedBaselineConnection(t, pool, "count-conn", true, "")
	seedAlerterShapedBaselines(t, pool, connID, 20, 1)

	tool := asSuperuser(GetMetricBaselinesTool(pool, testRBACChecker(t), nil, nil))

	t.Run("all shown", func(t *testing.T) {
		body := mustSuccess(t, tool, map[string]any{
			"connection_id": connID,
			"metric_name":   "metric_07",
		})
		countLine, rows := baselineOutput(t, body)
		want := fmt.Sprintf("Showing all %d baselines", baselineShapesPerMetric)
		if countLine != want {
			t.Errorf("count line = %q; want %q", countLine, want)
		}
		if len(rows) != baselineShapesPerMetric {
			t.Errorf("got %d rows; want %d", len(rows), baselineShapesPerMetric)
		}
	})

	t.Run("offset past the end", func(t *testing.T) {
		body := mustSuccess(t, tool, map[string]any{
			"connection_id": connID,
			"metric_name":   "metric_07",
			"offset":        float64(500),
		})
		want := fmt.Sprintf("No rows at offset 500: %d baselines match, so use an offset below %d.",
			baselineShapesPerMetric, baselineShapesPerMetric)
		if !strings.Contains(body, want) {
			t.Errorf("got %q; want it to contain %q", body, want)
		}
	})

	t.Run("offset past the end across connections", func(t *testing.T) {
		body := mustSuccess(t, tool, map[string]any{"offset": float64(10000)})
		want := fmt.Sprintf("No rows at offset 10000: %d baselines match", 20*baselineShapesPerMetric)
		if !strings.Contains(body, want) {
			t.Errorf("got %q; want it to contain %q", body, want)
		}
	})

	t.Run("offset with no matching rows", func(t *testing.T) {
		body := mustSuccess(t, tool, map[string]any{
			"connection_id": connID,
			"metric_name":   "no_such_metric",
			"offset":        float64(100),
		})
		if !strings.Contains(body, "No metric baselines matched") {
			t.Errorf("expected the no-match helper, got %q", body)
		}
	})
}

// TestBaselinesPastEndQueryErrorIntegration verifies that a failing count
// query surfaces as an error rather than a misleading message.
func TestBaselinesPastEndQueryErrorIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	if _, err := baselinesPastEnd(context.Background(), pool, "FROM no_such_table", nil, 5); err == nil {
		t.Error("expected an error from a failing count query")
	}
}

// TestGetMetricBaselinesInvalidPagingIntegration verifies that invalid
// limit and offset values are rejected with an error naming the valid
// form, before any rows are returned.
func TestGetMetricBaselinesInvalidPagingIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	connID := seedBaselineConnection(t, pool, "invalid-conn", true, "")
	seedAlerterShapedBaselines(t, pool, connID, 1, 1)

	tool := asSuperuser(GetMetricBaselinesTool(pool, testRBACChecker(t), nil, nil))

	limitErr := fmt.Sprintf("Invalid 'limit' parameter: must be an integer between 1 and %d", baselinesMaxLimit)
	offsetErr := "Invalid 'offset' parameter: must be an integer of 0 or more"

	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"zero limit", map[string]any{"limit": float64(0)}, limitErr},
		{"negative limit", map[string]any{"limit": float64(-5)}, limitErr},
		{"limit above maximum", map[string]any{"limit": float64(baselinesMaxLimit + 1)}, limitErr},
		{"fractional limit", map[string]any{"limit": 10.5}, limitErr},
		{"string limit", map[string]any{"limit": "10"}, limitErr},
		{"negative offset", map[string]any{"offset": float64(-1)}, offsetErr},
		{"fractional offset", map[string]any{"offset": 1.5}, offsetErr},
		{"string offset", map[string]any{"offset": "10"}, offsetErr},
	}
	for _, tc := range cases {
		for _, mode := range []string{"single", "multi"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				args := map[string]any{}
				for k, v := range tc.args {
					args[k] = v
				}
				if mode == "single" {
					args["connection_id"] = connID
				}
				resp, err := tool.Handler(args)
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
}

// TestGetMetricBaselinesQueryErrorsIntegration verifies that a failing
// page query and a row that cannot be scanned each come back as a tool
// error in both modes.
func TestGetMetricBaselinesQueryErrorsIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	connID := seedBaselineConnection(t, pool, "errors-conn", true, "")
	seedAlerterShapedBaselines(t, pool, connID, 1, 1)

	tool := asSuperuser(GetMetricBaselinesTool(pool, testRBACChecker(t), nil, nil))

	cases := []struct {
		name  string
		setup string
		want  string
	}{
		{"unscannable row", "ALTER TABLE metric_baselines ALTER COLUMN mean TYPE text", "Failed to scan row"},
		{"failing query", "DROP TABLE metric_baselines", "Failed to query metric baselines"},
	}
	for _, tc := range cases {
		if _, err := pool.Exec(context.Background(), tc.setup); err != nil {
			t.Fatalf("setup %q: %v", tc.setup, err)
		}
		for _, mode := range []string{"single", "multi"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				args := map[string]any{}
				if mode == "single" {
					args["connection_id"] = connID
				}
				resp, err := tool.Handler(args)
				if err != nil {
					t.Fatalf("handler returned unexpected error: %v", err)
				}
				if !resp.IsError || len(resp.Content) == 0 ||
					!strings.Contains(resp.Content[0].Text, tc.want) {
					t.Errorf("got %+v; want an error containing %q", resp, tc.want)
				}
			})
		}
	}
}
