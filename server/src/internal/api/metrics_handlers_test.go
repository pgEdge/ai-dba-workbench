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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
