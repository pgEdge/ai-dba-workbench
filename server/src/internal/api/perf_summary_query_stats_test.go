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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// callQueryStats invokes the query-stats handler with the supplied raw query
// string and returns the recorder for inspection.
func callQueryStats(
	t *testing.T,
	h *PerfSummaryHandler,
	rawQuery string,
) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/query-stats?"+rawQuery, nil)
	rec := httptest.NewRecorder()
	h.handleQueryStats(rec, req)
	return rec
}

// decodeQueryStats asserts a 200 response and decodes the JSON object body.
func decodeQueryStats(
	t *testing.T,
	rec *httptest.ResponseRecorder,
) QueryStatsResponse {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code,
			rec.Body.String())
	}
	var resp QueryStatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response body is not a QueryStatsResponse: %v; body: %s",
			err, rec.Body.String())
	}
	return resp
}

// minutesAgo returns a timestamp the given number of minutes before now,
// so that a test can place several rows in one collection by reusing the
// same value.
func minutesAgo(minutes int) time.Time {
	return time.Now().UTC().Add(-time.Duration(minutes) * time.Minute)
}

// insertQueryStatsSample writes one pg_stat_statements sample carrying the
// cumulative counters observed at the given offset before now.
func insertQueryStatsSample(
	t *testing.T,
	pool *pgxpool.Pool,
	connID int,
	queryID int64,
	minutes int,
	calls int64,
	totalExecTime float64,
) {
	t.Helper()
	insertQueryStatsSampleAt(t, pool, connID, queryID, minutesAgo(minutes),
		calls, totalExecTime)
}

// insertQueryStatsSampleAt writes one pg_stat_statements row at an explicit
// collection timestamp.
func insertQueryStatsSampleAt(
	t *testing.T,
	pool *pgxpool.Pool,
	connID int,
	queryID int64,
	collectedAt time.Time,
	calls int64,
	totalExecTime float64,
) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO metrics.pg_stat_statements
        (connection_id, collected_at, queryid, userid, dbid, database_name,
         query, calls, total_exec_time, mean_exec_time,
         min_exec_time, max_exec_time, rows,
         shared_blks_hit, shared_blks_read)
        VALUES ($1, $2, $3, 10, 100, 'alpha', 'SELECT 1', $4, $5, 0,
                0, 0, 0, 0, 0)`,
		connID, collectedAt, queryID, calls, totalExecTime); err != nil {
		t.Fatalf("query stats seed failed: %v", err)
	}
}

// TestBuildQueryStats covers the pure translation from summed deltas to the
// response body, including every case in which the average must be reported
// as null rather than as a number.
func TestBuildQueryStats(t *testing.T) {
	tests := []struct {
		name      string
		pairs     int64
		calls     int64
		totalTime float64
		wantAvg   *float64
		wantTotal float64
	}{
		{
			name:      "average over usable pairs",
			pairs:     3,
			calls:     30,
			totalTime: 600,
			wantAvg:   floatPtr(20),
			wantTotal: 600,
		},
		{
			name:      "average is rounded to three places",
			pairs:     1,
			calls:     3,
			totalTime: 1,
			wantAvg:   floatPtr(0.333),
			wantTotal: 1,
		},
		{
			name:      "no usable sample pairs",
			pairs:     0,
			calls:     0,
			totalTime: 0,
			wantAvg:   nil,
			wantTotal: 0,
		},
		{
			name:      "pairs but no calls",
			pairs:     4,
			calls:     0,
			totalTime: 0,
			wantAvg:   nil,
			wantTotal: 0,
		},
		{
			name:      "calls recorded without a usable pair",
			pairs:     0,
			calls:     7,
			totalTime: 5,
			wantAvg:   nil,
			wantTotal: 5,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildQueryStats("1234", tc.pairs, tc.calls, tc.totalTime)
			if got.QueryID != "1234" {
				t.Errorf("queryid = %q, want \"1234\"", got.QueryID)
			}
			if got.Calls != tc.calls {
				t.Errorf("calls = %d, want %d", got.Calls, tc.calls)
			}
			if got.TotalExecTime != tc.wantTotal {
				t.Errorf("total_exec_time = %v, want %v", got.TotalExecTime,
					tc.wantTotal)
			}
			switch {
			case tc.wantAvg == nil && got.AvgExecTime != nil:
				t.Errorf("avg_exec_time = %v, want null", *got.AvgExecTime)
			case tc.wantAvg != nil && got.AvgExecTime == nil:
				t.Errorf("avg_exec_time = null, want %v", *tc.wantAvg)
			case tc.wantAvg != nil && *got.AvgExecTime != *tc.wantAvg:
				t.Errorf("avg_exec_time = %v, want %v", *got.AvgExecTime,
					*tc.wantAvg)
			}
		})
	}
}

// floatPtr returns a pointer to the supplied value, for building expected
// nullable averages in table tests.
func floatPtr(v float64) *float64 {
	return &v
}

// TestBuildQueryStats_NullSerialisesAsJSONNull confirms the absent average
// reaches the client as JSON null rather than as zero, which is the
// distinction the Query detail dashboard relies on.
func TestBuildQueryStats_NullSerialisesAsJSONNull(t *testing.T) {
	body, err := json.Marshal(buildQueryStats("42", 0, 0, 0))
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	avg, present := decoded["avg_exec_time"]
	if !present {
		t.Fatal("avg_exec_time is absent from the response body")
	}
	if avg != nil {
		t.Errorf("avg_exec_time = %#v, want null", avg)
	}
}

// TestQueryStats_AveragesDeltasOverThePeriod checks the headline behavior:
// the average covers only the requested range and is computed from the
// deltas between consecutive samples, not from the cumulative counters.
func TestQueryStats_AveragesDeltasOverThePeriod(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()

	// Cumulative counters: 10 calls / 100 ms at the first sample, then two
	// intervals adding 10 calls / 200 ms and 20 calls / 400 ms. The period
	// average is therefore 600 / 30 = 20 ms, well away from the lifetime
	// mean of 700 / 40 = 17.5 ms.
	insertQueryStatsSample(t, pool, topQueriesConnID, 1001, 30, 10, 100)
	insertQueryStatsSample(t, pool, topQueriesConnID, 1001, 20, 20, 300)
	insertQueryStatsSample(t, pool, topQueriesConnID, 1001, 10, 40, 700)

	resp := decodeQueryStats(t, callQueryStats(t, h,
		"connection_id=4242&queryid=1001&time_range=1h"))

	if resp.QueryID != "1001" {
		t.Errorf("queryid = %q, want \"1001\"", resp.QueryID)
	}
	if resp.Calls != 30 {
		t.Errorf("calls = %d, want 30", resp.Calls)
	}
	if resp.TotalExecTime != 600 {
		t.Errorf("total_exec_time = %v, want 600", resp.TotalExecTime)
	}
	if resp.AvgExecTime == nil || *resp.AvgExecTime != 20 {
		t.Fatalf("avg_exec_time = %v, want 20", resp.AvgExecTime)
	}
}

// TestQueryStats_SumsRowsWithinOneSample confirms that several rows sharing a
// queryid in a single collection (pg_stat_statements records one row per
// database, role, and toplevel flag) are collapsed before the delta is taken.
func TestQueryStats_SumsRowsWithinOneSample(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()

	first := minutesAgo(30)
	second := minutesAgo(10)
	collected := func(at time.Time, calls int64, execTime float64) {
		insertQueryStatsSampleAt(t, pool, topQueriesConnID, 2002, at, calls,
			execTime)
	}
	// Two rows per collection, each carrying half of the counters.
	collected(first, 5, 50)
	collected(first, 5, 50)
	collected(second, 15, 250)
	collected(second, 15, 250)

	resp := decodeQueryStats(t, callQueryStats(t, h,
		"connection_id=4242&queryid=2002"))

	if resp.Calls != 20 {
		t.Errorf("calls = %d, want 20", resp.Calls)
	}
	if resp.TotalExecTime != 400 {
		t.Errorf("total_exec_time = %v, want 400", resp.TotalExecTime)
	}
	if resp.AvgExecTime == nil || *resp.AvgExecTime != 20 {
		t.Fatalf("avg_exec_time = %v, want 20", resp.AvgExecTime)
	}
}

// TestQueryStats_CounterResetCostsOneInterval verifies that a pg_stat_reset()
// inside the period discards only the sample pair that straddles the reset,
// leaving the surrounding intervals intact.
func TestQueryStats_CounterResetCostsOneInterval(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()

	// Pre-reset: 10 calls / 200 ms accumulate between the first two
	// samples. The third sample drops back to a freshly reset counter, so
	// that pair is discarded. The fourth sample then adds 20 calls /
	// 100 ms on top of the reset counters.
	insertQueryStatsSample(t, pool, topQueriesConnID, 3003, 40, 100, 1000)
	insertQueryStatsSample(t, pool, topQueriesConnID, 3003, 30, 110, 1200)
	insertQueryStatsSample(t, pool, topQueriesConnID, 3003, 20, 5, 25)
	insertQueryStatsSample(t, pool, topQueriesConnID, 3003, 10, 25, 125)

	resp := decodeQueryStats(t, callQueryStats(t, h,
		"connection_id=4242&queryid=3003"))

	if resp.Calls != 30 {
		t.Errorf("calls = %d, want 30 (10 before the reset, 20 after)",
			resp.Calls)
	}
	if resp.TotalExecTime != 300 {
		t.Errorf("total_exec_time = %v, want 300", resp.TotalExecTime)
	}
	// Without the reset guard the negative deltas would drag the average
	// below zero; with it the answer is the honest 300 / 30.
	if resp.AvgExecTime == nil || *resp.AvgExecTime != 10 {
		t.Fatalf("avg_exec_time = %v, want 10", resp.AvgExecTime)
	}
}

// TestQueryStats_NoDataCases covers every route to a null average: an unknown
// query, a single sample with no predecessor, samples that fall outside the
// requested range, and a period in which the query was never executed.
func TestQueryStats_NoDataCases(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()

	// A lone sample: no predecessor, so no usable pair.
	insertQueryStatsSample(t, pool, topQueriesConnID, 4004, 10, 10, 100)

	// Two samples well outside a 1h window.
	insertQueryStatsSample(t, pool, topQueriesConnID, 5005, 300, 10, 100)
	insertQueryStatsSample(t, pool, topQueriesConnID, 5005, 290, 20, 300)

	// Two identical samples: the query was not executed between them.
	insertQueryStatsSample(t, pool, topQueriesConnID, 6006, 30, 10, 100)
	insertQueryStatsSample(t, pool, topQueriesConnID, 6006, 10, 10, 100)

	// A sample on a different connection that must not leak in.
	insertQueryStatsSample(t, pool, topQueriesConnID+1, 7007, 30, 10, 100)
	insertQueryStatsSample(t, pool, topQueriesConnID+1, 7007, 10, 20, 300)

	tests := []struct {
		name      string
		query     string
		wantCalls int64
		wantTotal float64
	}{
		{"unknown queryid",
			"connection_id=4242&queryid=9999", 0, 0},
		{"single sample",
			"connection_id=4242&queryid=4004", 0, 0},
		{"samples outside the range",
			"connection_id=4242&queryid=5005&time_range=1h", 0, 0},
		{"no calls in the period",
			"connection_id=4242&queryid=6006", 0, 0},
		{"another connection's samples",
			"connection_id=4242&queryid=7007", 0, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := decodeQueryStats(t, callQueryStats(t, h, tc.query))
			if resp.AvgExecTime != nil {
				t.Errorf("avg_exec_time = %v, want null", *resp.AvgExecTime)
			}
			if resp.Calls != tc.wantCalls {
				t.Errorf("calls = %d, want %d", resp.Calls, tc.wantCalls)
			}
			if resp.TotalExecTime != tc.wantTotal {
				t.Errorf("total_exec_time = %v, want %v", resp.TotalExecTime,
					tc.wantTotal)
			}
		})
	}
}

// TestQueryStats_WiderRangeIncludesOlderSamples confirms the time_range
// parameter really does widen the window rather than being ignored.
func TestQueryStats_WiderRangeIncludesOlderSamples(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()

	// Five hours ago: outside 1h, inside 6h.
	insertQueryStatsSample(t, pool, topQueriesConnID, 8008, 300, 10, 100)
	insertQueryStatsSample(t, pool, topQueriesConnID, 8008, 290, 20, 300)

	narrow := decodeQueryStats(t, callQueryStats(t, h,
		"connection_id=4242&queryid=8008&time_range=1h"))
	if narrow.AvgExecTime != nil {
		t.Errorf("1h avg_exec_time = %v, want null", *narrow.AvgExecTime)
	}

	wide := decodeQueryStats(t, callQueryStats(t, h,
		"connection_id=4242&queryid=8008&time_range=6h"))
	if wide.AvgExecTime == nil || *wide.AvgExecTime != 20 {
		t.Fatalf("6h avg_exec_time = %v, want 20", wide.AvgExecTime)
	}
	if wide.Calls != 10 || wide.TotalExecTime != 200 {
		t.Errorf("6h calls = %d and total = %v, want 10 and 200", wide.Calls,
			wide.TotalExecTime)
	}
}

// TestQueryStats_InvalidParameters checks that bad input is rejected with a
// 400 rather than reaching the database.
func TestQueryStats_InvalidParameters(t *testing.T) {
	h, _, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()

	tests := []struct {
		name  string
		query string
	}{
		{"missing connection", "queryid=1001"},
		{"multiple connections", "connection_ids=1,2&queryid=1001"},
		{"non-numeric connection", "connection_id=abc&queryid=1001"},
		{"missing queryid", "connection_id=4242"},
		{"empty queryid", "connection_id=4242&queryid="},
		{"invalid time range",
			"connection_id=4242&queryid=1001&time_range=99y"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := callQueryStats(t, h, tc.query)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body: %s", rec.Code,
					rec.Body.String())
			}
		})
	}
}

// TestQueryStats_SQLMetacharactersStayBound confirms the queryid reaches the
// statement as a bound parameter: an injection attempt simply matches no rows.
func TestQueryStats_SQLMetacharactersStayBound(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()

	insertQueryStatsSample(t, pool, topQueriesConnID, 1001, 30, 10, 100)
	insertQueryStatsSample(t, pool, topQueriesConnID, 1001, 10, 20, 300)

	resp := decodeQueryStats(t, callQueryStats(t, h,
		"connection_id=4242&queryid=1001%27%20OR%20%271%27%3D%271"))
	if resp.AvgExecTime != nil || resp.Calls != 0 {
		t.Fatalf("resp = %#v, want an empty result", resp)
	}

	// The table is still there, which it would not be after a successful
	// injection.
	var count int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM metrics.pg_stat_statements`).Scan(
		&count); err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if count != 2 {
		t.Errorf("row count = %d, want 2", count)
	}
}

// TestQueryStats_MissingMetricsTables confirms that an uncollected workbench
// yields an empty result rather than an error, matching the top-queries
// endpoint's treatment of missing metrics tables.
func TestQueryStats_MissingMetricsTables(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()

	if _, err := pool.Exec(context.Background(),
		topQueriesTestSchemaTeardown); err != nil {
		t.Fatalf("teardown failed: %v", err)
	}

	resp := decodeQueryStats(t, callQueryStats(t, h,
		"connection_id=4242&queryid=1001"))
	if resp.QueryID != "1001" {
		t.Errorf("queryid = %q, want \"1001\"", resp.QueryID)
	}
	if resp.AvgExecTime != nil {
		t.Errorf("avg_exec_time = %v, want null", *resp.AvgExecTime)
	}
	if resp.Calls != 0 || resp.TotalExecTime != 0 {
		t.Errorf("calls = %d and total = %v, want zeroes", resp.Calls,
			resp.TotalExecTime)
	}
}

// TestQueryStats_PermissionDenied confirms the connection-level RBAC check
// guards the endpoint once a real auth store is wired in.
func TestQueryStats_PermissionDenied(t *testing.T) {
	_, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()

	authStore, err := auth.NewAuthStore(t.TempDir(), 0, 0)
	if err != nil {
		t.Fatalf("NewAuthStore failed: %v", err)
	}
	h := NewPerfSummaryHandler(database.NewTestDatastore(pool), authStore)

	rec := callQueryStats(t, h, "connection_id=4242&queryid=1001")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body: %s", rec.Code,
			rec.Body.String())
	}
}

// TestQueryStats_MethodNotAllowed confirms non-GET requests are rejected.
func TestQueryStats_MethodNotAllowed(t *testing.T) {
	h, _, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/metrics/query-stats?connection_id=4242&queryid=1001", nil)
	rec := httptest.NewRecorder()
	h.handleQueryStats(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
