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
	// The Query detail dashboard's charts rely on the time-series path
	// forwarding queryid into MetricFilters.QueryID; without it both
	// charts aggregate across every statement in the database.
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
			"&queryid=-1234567890123456789", nil)
	rec := httptest.NewRecorder()

	handler.handleMetricsQuery(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body %q)",
			http.StatusOK, rec.Code, rec.Body.String())
	}
	if !called {
		t.Fatal("expected time-series query function to be called")
	}
	if gotFilters.QueryID != "-1234567890123456789" {
		t.Errorf("expected QueryID %q reaching the query layer, got %q",
			"-1234567890123456789", gotFilters.QueryID)
	}
}

func TestHandleMetricsQuery_TimeSeriesMode_InvalidQueryID(t *testing.T) {
	// A queryid that is not a 64-bit integer is rejected before any
	// query runs, so a malformed value can never reach the database.
	handler := &MetricsHandler{
		datastore: &database.Datastore{},
		queryTimeSeriesFn: func(
			_ context.Context,
			_ *pgxpool.Pool,
			_ string,
			_ []int,
			_ string,
			_ metrics.MetricFilters,
			_ int,
			_ string,
			_ []string,
		) ([]metrics.MetricSeries, error) {
			t.Fatal("query function must not be called for a bad queryid")
			return nil, nil
		},
	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/query?connection_id=1"+
			"&probe_name=pg_stat_statements&time_range=1h"+
			"&queryid=not-a-number", nil)
	rec := httptest.NewRecorder()

	handler.handleMetricsQuery(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d",
			http.StatusBadRequest, rec.Code)
	}
	if resp := decodeError(t, rec); resp.Error !=
		"Invalid queryid: must be a 64-bit integer" {
		t.Errorf("unexpected error: %q", resp.Error)
	}
}

func TestHandleMetricsQuery_LatestRowsMode_InvalidQueryID(t *testing.T) {
	// The latest-row path shares the same queryid validation, and must
	// reject a malformed value before it touches the pool.
	handler := &MetricsHandler{datastore: &database.Datastore{}}

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/query?connection_id=1"+
			"&probe_name=pg_stat_statements&limit=5"+
			"&queryid=99999999999999999999", nil)
	rec := httptest.NewRecorder()

	handler.handleMetricsQuery(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d",
			http.StatusBadRequest, rec.Code)
	}
	if resp := decodeError(t, rec); resp.Error !=
		"Invalid queryid: must be a 64-bit integer" {
		t.Errorf("unexpected error: %q", resp.Error)
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
