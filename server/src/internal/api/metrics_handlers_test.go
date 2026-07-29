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
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/metrics"
)

func TestNewMetricsHandler(t *testing.T) {
	handler := NewMetricsHandler(nil, nil)
	if handler == nil {
		t.Fatal("NewMetricsHandler returned nil")
	}
	if handler.datastore != nil {
		t.Error("expected nil datastore")
	}
	if handler.authStore != nil {
		t.Error("expected nil authStore")
	}
}

func TestMetricsHandler_RegisterRoutes_NotConfigured(t *testing.T) {
	handler := NewMetricsHandler(nil, nil)
	mux := http.NewServeMux()
	noopWrapper := func(h http.HandlerFunc) http.HandlerFunc { return h }
	handler.RegisterRoutes(mux, noopWrapper)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/query", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d",
			http.StatusServiceUnavailable, rec.Code)
	}
}

// decodeError reads an ErrorResponse from the recorder.
func decodeError(t *testing.T, rec *httptest.ResponseRecorder) ErrorResponse {
	t.Helper()
	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return response
}

func TestHandleMetricsQuery_MissingConnectionID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/query?probe_name=pg_stat_all_tables", nil)
	rec := httptest.NewRecorder()

	handler := &MetricsHandler{}
	handler.handleMetricsQuery(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d",
			http.StatusBadRequest, rec.Code)
	}
	if resp := decodeError(t, rec); resp.Error !=
		"Either connection_id or connection_ids is required" {
		t.Errorf("unexpected error: %q", resp.Error)
	}
}

func TestHandleMetricsQuery_MissingProbeName(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/query?connection_id=1", nil)
	rec := httptest.NewRecorder()

	// With nil authStore the RBAC checker treats the caller as a
	// superuser, so we reach probe_name validation.
	handler := &MetricsHandler{}
	handler.handleMetricsQuery(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d",
			http.StatusBadRequest, rec.Code)
	}
	if resp := decodeError(t, rec); resp.Error != "probe_name is required" {
		t.Errorf("unexpected error: %q", resp.Error)
	}
}

func TestHandleMetricsQuery_InvalidProbeName(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/query?connection_id=1&probe_name=bad;name", nil)
	rec := httptest.NewRecorder()

	handler := &MetricsHandler{}
	handler.handleMetricsQuery(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d",
			http.StatusBadRequest, rec.Code)
	}
}

func TestHandleMetricsQuery_LatestMode_InvalidLimit(t *testing.T) {
	cases := []string{"0", "-1", "101", "abc"}
	for _, limit := range cases {
		t.Run("limit="+limit, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet,
				"/api/v1/metrics/query?connection_id=1"+
					"&probe_name=pg_stat_all_tables&limit="+limit, nil)
			rec := httptest.NewRecorder()

			handler := &MetricsHandler{}
			handler.handleMetricsQuery(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected status %d, got %d",
					http.StatusBadRequest, rec.Code)
			}
			if resp := decodeError(t, rec); resp.Error !=
				"Invalid limit: must be between 1 and 100" {
				t.Errorf("unexpected error: %q", resp.Error)
			}
		})
	}
}

func TestHandleMetricsQuery_LatestMode_InvalidOrderBy(t *testing.T) {
	// A syntactically invalid order_by is rejected at the handler before
	// any column discovery or SQL execution occurs.
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/query?connection_id=1"+
			"&probe_name=pg_stat_all_tables&order_by=bad-column", nil)
	rec := httptest.NewRecorder()

	handler := &MetricsHandler{}
	handler.handleMetricsQuery(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d",
			http.StatusBadRequest, rec.Code)
	}
	if resp := decodeError(t, rec); resp.Error !=
		"Invalid order_by: must contain only letters, numbers, and underscores" {
		t.Errorf("unexpected error: %q", resp.Error)
	}
}

func TestHandleMetricsQuery_LatestMode_InvalidOrder(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/query?connection_id=1"+
			"&probe_name=pg_stat_all_tables&limit=1"+
			"&order_by=n_live_tup&order=sideways", nil)
	rec := httptest.NewRecorder()

	handler := &MetricsHandler{}
	handler.handleMetricsQuery(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d",
			http.StatusBadRequest, rec.Code)
	}
	if resp := decodeError(t, rec); resp.Error !=
		"Invalid order: must be asc or desc" {
		t.Errorf("unexpected error: %q", resp.Error)
	}
}

func TestHandleMetricsQuery_TimeSeriesMode_InvalidTimeRange(t *testing.T) {
	// Without limit/order_by the handler stays on the time-series path;
	// an invalid time_range is rejected there.
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/query?connection_id=1"+
			"&probe_name=pg_stat_all_tables&time_range=99z", nil)
	rec := httptest.NewRecorder()

	handler := &MetricsHandler{}
	handler.handleMetricsQuery(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d",
			http.StatusBadRequest, rec.Code)
	}
	if resp := decodeError(t, rec); resp.Error !=
		"Invalid time_range: must be one of 1h, 6h, 24h, 7d, 30d" {
		t.Errorf("unexpected error: %q", resp.Error)
	}
}

func TestHandleMetricsQuery_TimeSeriesMode_ParsesIndexNameFilter(t *testing.T) {
	// The Index detail dashboard's Scan Activity chart relies on the
	// time-series path forwarding index_name into MetricFilters.IndexName.
	// Inject a fake query function to capture the filters the handler
	// actually passes downstream; the request uses only valid parameters
	// so the handler reaches the query layer instead of short-circuiting
	// on a validation error. This test fails if index_name parsing on the
	// time-series path is removed or broken.
	var gotFilters metrics.MetricFilters
	called := false
	handler := &MetricsHandler{
		datastore: &database.Datastore{},
		queryTimeSeriesFn: func(
			_ context.Context,
			_ *pgxpool.Pool,
			_ string,
			_ []int,
			_ string,
			filters metrics.MetricFilters,
			_ int,
			_ string,
			_ []string,
		) ([]metrics.MetricSeries, error) {
			called = true
			gotFilters = filters
			return []metrics.MetricSeries{}, nil
		},
	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/query?connection_id=1"+
			"&probe_name=pg_stat_all_indexes&time_range=1h"+
			"&index_name=pk_orders", nil)
	rec := httptest.NewRecorder()

	handler.handleMetricsQuery(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body %q)",
			http.StatusOK, rec.Code, rec.Body.String())
	}
	if !called {
		t.Fatal("expected time-series query function to be called")
	}
	if gotFilters.IndexName != "pk_orders" {
		t.Errorf("expected IndexName %q reaching the query layer, got %q",
			"pk_orders", gotFilters.IndexName)
	}
}

func TestHandleMetricsQuery_TimeSeriesMode_ParsesQueryIDFilter(t *testing.T) {
	// The Query detail dashboard charts one statement at a time, so the
	// queryid parameter must reach MetricFilters.QueryID; without it the
	// charts aggregate every statement on the connection (issue #350). The
	// same fake-injection seam as the index_name test above captures the
	// filters passed downstream.
	var gotFilters metrics.MetricFilters
	called := false
	handler := &MetricsHandler{
		datastore: &database.Datastore{},
		queryTimeSeriesFn: func(
			_ context.Context,
			_ *pgxpool.Pool,
			_ string,
			_ []int,
			_ string,
			filters metrics.MetricFilters,
			_ int,
			_ string,
			_ []string,
		) ([]metrics.MetricSeries, error) {
			called = true
			gotFilters = filters
			return []metrics.MetricSeries{}, nil
		},
	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/query?connection_id=1"+
			"&probe_name=pg_stat_statements&time_range=1h"+
			"&queryid=-1234567890123&database_name=northwind", nil)
	rec := httptest.NewRecorder()

	handler.handleMetricsQuery(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body %q)",
			http.StatusOK, rec.Code, rec.Body.String())
	}
	if !called {
		t.Fatal("expected time-series query function to be called")
	}
	if gotFilters.QueryID != "-1234567890123" {
		t.Errorf("expected QueryID %q reaching the query layer, got %q",
			"-1234567890123", gotFilters.QueryID)
	}
	if gotFilters.DatabaseName != "northwind" {
		t.Errorf("expected DatabaseName %q reaching the query layer, got %q",
			"northwind", gotFilters.DatabaseName)
	}
}

func TestHandleMetricsQuery_TimeSeriesMode_DefaultQueryFn(t *testing.T) {
	// Exercises the nil-queryTimeSeriesFn fallback: a handler built without
	// an injected query function must resolve to metrics.QueryTimeSeries. A
	// real pool lets that default run; an unknown probe makes the query
	// return an error the handler maps to 400, proving the default wiring
	// reaches the query layer instead of relying on the injected seam.
	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping datastore integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("Test database ping failed: %v", err)
	}

	handler := &MetricsHandler{datastore: database.NewTestDatastore(pool)}

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/query?connection_id=1"+
			"&probe_name=zzz_no_such_probe&time_range=1h", nil)
	rec := httptest.NewRecorder()

	handler.handleMetricsQuery(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body %q)",
			http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

// TestHandleMetricsQuery_QueryIDOnUnsupportedProbe confirms the guard in the
// metrics layer surfaces as a clean 400 on both request shapes, rather than
// reaching PostgreSQL as an undefined-column error.
func TestHandleMetricsQuery_QueryIDOnUnsupportedProbe(t *testing.T) {
	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping datastore integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("Test database ping failed: %v", err)
	}

	const probe = "queryid_unsupported_probe_test"
	if _, err := pool.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS metrics;
        DROP TABLE IF EXISTS metrics.queryid_unsupported_probe_test CASCADE;
        CREATE TABLE metrics.queryid_unsupported_probe_test (
            connection_id integer NOT NULL,
            collected_at  timestamptz NOT NULL,
            seq_scan      bigint
        );`); err != nil {
		t.Fatalf("failed to create fixture table: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(),
			`DROP TABLE IF EXISTS metrics.queryid_unsupported_probe_test CASCADE`)
	}()

	handler := &MetricsHandler{datastore: database.NewTestDatastore(pool)}

	tests := []struct {
		name  string
		query string
	}{
		{"time series path",
			"connection_id=1&probe_name=" + probe +
				"&time_range=1h&queryid=1001"},
		{"latest rows path",
			"connection_id=1&probe_name=" + probe +
				"&limit=1&queryid=1001"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet,
				"/api/v1/metrics/query?"+tc.query, nil)
			rec := httptest.NewRecorder()

			handler.handleMetricsQuery(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected status %d, got %d (body %q)",
					http.StatusBadRequest, rec.Code, rec.Body.String())
			}
			if resp := decodeError(t, rec); resp.Error !=
				`probe "`+probe+`" does not support the queryid filter` {
				t.Errorf("unexpected error: %q", resp.Error)
			}
		})
	}
}

// newFakeMetricsHandler returns a handler whose time-series query function
// records the arguments it receives, so tests can assert on parsing without
// a database.
func newFakeMetricsHandler(
	captured *fakeTimeSeriesCall,
) *MetricsHandler {
	return &MetricsHandler{
		datastore: &database.Datastore{},
		queryTimeSeriesFn: func(
			_ context.Context,
			_ *pgxpool.Pool,
			_ string,
			_ []int,
			timeRange string,
			filters metrics.MetricFilters,
			buckets int,
			aggregation string,
			requestedMetrics []string,
		) ([]metrics.MetricSeries, error) {
			captured.called = true
			captured.timeRange = timeRange
			captured.filters = filters
			captured.buckets = buckets
			captured.aggregation = aggregation
			captured.metrics = requestedMetrics
			return []metrics.MetricSeries{}, nil
		},
	}
}

// fakeTimeSeriesCall records one call into the injected query function.
type fakeTimeSeriesCall struct {
	called      bool
	timeRange   string
	filters     metrics.MetricFilters
	buckets     int
	aggregation string
	metrics     []string
}

// TestHandleMetricsQuery_TimeSeriesMode_ParameterDefaults covers the
// remaining parsing branches of the time-series path: the defaults applied
// when a parameter is omitted, and the values forwarded when it is supplied.
func TestHandleMetricsQuery_TimeSeriesMode_ParameterDefaults(t *testing.T) {
	tests := []struct {
		name            string
		query           string
		wantTimeRange   string
		wantBuckets     int
		wantAggregation string
		wantMetrics     []string
	}{
		{
			name:            "defaults applied",
			query:           "connection_id=1&probe_name=pg_stat_all_tables",
			wantTimeRange:   "1h",
			wantBuckets:     150,
			wantAggregation: "avg",
		},
		{
			name: "explicit values forwarded",
			query: "connection_id=1&probe_name=pg_stat_all_tables" +
				"&time_range=24h&buckets=30&aggregation=LAST" +
				"&metrics=seq_scan,+idx_scan+,,",
			wantTimeRange:   "24h",
			wantBuckets:     30,
			wantAggregation: "last",
			wantMetrics:     []string{"seq_scan", "idx_scan"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got fakeTimeSeriesCall
			handler := newFakeMetricsHandler(&got)

			req := httptest.NewRequest(http.MethodGet,
				"/api/v1/metrics/query?"+tc.query, nil)
			rec := httptest.NewRecorder()
			handler.handleMetricsQuery(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected status %d, got %d (body %q)",
					http.StatusOK, rec.Code, rec.Body.String())
			}
			if !got.called {
				t.Fatal("expected the query function to be called")
			}
			if got.timeRange != tc.wantTimeRange {
				t.Errorf("time_range = %q, want %q", got.timeRange,
					tc.wantTimeRange)
			}
			if got.buckets != tc.wantBuckets {
				t.Errorf("buckets = %d, want %d", got.buckets, tc.wantBuckets)
			}
			if got.aggregation != tc.wantAggregation {
				t.Errorf("aggregation = %q, want %q", got.aggregation,
					tc.wantAggregation)
			}
			if len(got.metrics) != len(tc.wantMetrics) {
				t.Fatalf("metrics = %v, want %v", got.metrics, tc.wantMetrics)
			}
			for i := range tc.wantMetrics {
				if got.metrics[i] != tc.wantMetrics[i] {
					t.Errorf("metrics = %v, want %v", got.metrics,
						tc.wantMetrics)
				}
			}
		})
	}
}

// TestHandleMetricsQuery_TimeSeriesMode_RejectsBadParameters covers the
// validation branches that short-circuit before the query layer.
func TestHandleMetricsQuery_TimeSeriesMode_RejectsBadParameters(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		wantError string
	}{
		{
			name: "invalid probe name",
			// The semicolon is percent-encoded so it survives query
			// parsing and reaches the identifier check.
			query: "connection_id=1&probe_name=bad%3Bname",
			wantError: "Invalid probe_name: must contain only letters, " +
				"numbers, and underscores",
		},
		{
			name:      "buckets below the range",
			query:     "connection_id=1&probe_name=pg_stat_all_tables&buckets=0",
			wantError: "Invalid buckets: must be between 1 and 500",
		},
		{
			name:      "buckets above the range",
			query:     "connection_id=1&probe_name=pg_stat_all_tables&buckets=501",
			wantError: "Invalid buckets: must be between 1 and 500",
		},
		{
			name:      "non-numeric buckets",
			query:     "connection_id=1&probe_name=pg_stat_all_tables&buckets=lots",
			wantError: "Invalid buckets: must be between 1 and 500",
		},
		{
			name: "unknown aggregation",
			query: "connection_id=1&probe_name=pg_stat_all_tables" +
				"&aggregation=median",
			wantError: "Invalid aggregation: must be one of avg, sum, min, max, last",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got fakeTimeSeriesCall
			handler := newFakeMetricsHandler(&got)

			req := httptest.NewRequest(http.MethodGet,
				"/api/v1/metrics/query?"+tc.query, nil)
			rec := httptest.NewRecorder()
			handler.handleMetricsQuery(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected status %d, got %d (body %q)",
					http.StatusBadRequest, rec.Code, rec.Body.String())
			}
			if resp := decodeError(t, rec); resp.Error != tc.wantError {
				t.Errorf("error = %q, want %q", resp.Error, tc.wantError)
			}
			if got.called {
				t.Error("the query function must not run for invalid input")
			}
		})
	}
}

// TestHandleMetricsQuery_MethodNotAllowed confirms non-GET requests are
// rejected before any parsing.
func TestHandleMetricsQuery_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/metrics/query?connection_id=1&probe_name=pg_stat_all_tables",
		nil)
	rec := httptest.NewRecorder()

	handler := &MetricsHandler{}
	handler.handleMetricsQuery(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status %d, got %d", http.StatusMethodNotAllowed,
			rec.Code)
	}
}

// TestHandleMetricsQuery_PermissionDenied confirms the connection-level RBAC
// check guards the endpoint once a real auth store is wired in and the caller
// is neither a superuser nor the owner of the connection.
func TestHandleMetricsQuery_PermissionDenied(t *testing.T) {
	authStore, err := auth.NewAuthStore(t.TempDir(), 0, 0)
	if err != nil {
		t.Fatalf("NewAuthStore failed: %v", err)
	}

	// The RBAC checker consults the datastore for connection sharing, so
	// this path needs a live pool rather than the zero-value datastore the
	// other fake-backed tests use.
	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping datastore integration test")
	}
	pool, err := pgxpool.New(context.Background(), connStr)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	defer pool.Close()

	var got fakeTimeSeriesCall
	handler := newFakeMetricsHandler(&got)
	handler.datastore = database.NewTestDatastore(pool)
	handler.authStore = authStore

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/query?connection_id=1&probe_name=pg_stat_all_tables",
		nil)
	rec := httptest.NewRecorder()
	handler.handleMetricsQuery(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d (body %q)", http.StatusForbidden,
			rec.Code, rec.Body.String())
	}
	if got.called {
		t.Error("the query function must not run for a denied request")
	}
}
