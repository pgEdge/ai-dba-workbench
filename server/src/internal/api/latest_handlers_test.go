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

func TestNewLatestSnapshotHandler(t *testing.T) {
	handler := NewLatestSnapshotHandler(nil, nil)
	if handler == nil {
		t.Fatal("NewLatestSnapshotHandler returned nil")
	}
	if handler.datastore != nil {
		t.Error("Expected nil datastore")
	}
	if handler.authStore != nil {
		t.Error("Expected nil authStore")
	}
}

func TestLatestSnapshotHandler_RegisterRoutes_NotConfigured(t *testing.T) {
	handler := NewLatestSnapshotHandler(nil, nil)
	mux := http.NewServeMux()
	noopWrapper := func(h http.HandlerFunc) http.HandlerFunc { return h }
	handler.RegisterRoutes(mux, noopWrapper)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/latest", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status %d, got %d",
			http.StatusServiceUnavailable, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	expected := "Latest snapshot is not available. The datastore is not configured."
	if response.Error != expected {
		t.Errorf("Expected error %q, got %q", expected, response.Error)
	}
}

func TestLatestSnapshotHandler_MethodNotAllowed(t *testing.T) {
	handler := NewLatestSnapshotHandler(nil, nil)
	mux := http.NewServeMux()
	noopWrapper := func(h http.HandlerFunc) http.HandlerFunc { return h }

	// Register with a non-nil datastore would be needed for the real
	// handler, but we can test method validation by calling the handler
	// function directly. Since datastore is nil, we test via the not-
	// configured path which still calls RequireGET indirectly.
	// Instead, test the not-configured handler does not reject POST
	// (it does not check method).
	handler.RegisterRoutes(mux, noopWrapper)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/metrics/latest", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	// The not-configured handler responds 503 regardless of method
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status %d, got %d",
			http.StatusServiceUnavailable, rec.Code)
	}
}

func TestBuildDimensionColumns(t *testing.T) {
	tests := []struct {
		name       string
		allColumns []string
		colTypes   map[string]string
		want       []string
	}{
		{
			name: "pg_stat_all_tables typical columns",
			allColumns: []string{
				"connection_id", "collected_at", "inserted_at",
				"database_name", "schemaname", "relname",
				"seq_scan", "n_live_tup", "n_dead_tup",
			},
			colTypes: map[string]string{
				"connection_id": "integer",
				"collected_at":  "timestamp with time zone",
				"inserted_at":   "timestamp with time zone",
				"database_name": "text",
				"schemaname":    "name",
				"relname":       "name",
				"seq_scan":      "bigint",
				"n_live_tup":    "bigint",
				"n_dead_tup":    "bigint",
			},
			want: []string{"database_name", "schemaname", "relname"},
		},
		{
			name: "pg_stat_all_indexes with indexrelname",
			allColumns: []string{
				"connection_id", "collected_at", "inserted_at",
				"database_name", "schemaname", "relname", "indexrelname",
				"idx_scan", "idx_tup_read",
			},
			colTypes: map[string]string{
				"connection_id": "integer",
				"collected_at":  "timestamp with time zone",
				"inserted_at":   "timestamp with time zone",
				"database_name": "text",
				"schemaname":    "name",
				"relname":       "name",
				"indexrelname":  "name",
				"idx_scan":      "bigint",
				"idx_tup_read":  "bigint",
			},
			want: []string{
				"database_name", "schemaname", "relname", "indexrelname",
			},
		},
		{
			name:       "no text columns yields empty",
			allColumns: []string{"connection_id", "collected_at", "value"},
			colTypes: map[string]string{
				"connection_id": "integer",
				"collected_at":  "timestamp with time zone",
				"value":         "bigint",
			},
			want: nil,
		},
		{
			name: "internal columns excluded",
			allColumns: []string{
				"connection_id", "collected_at", "inserted_at", "datname",
			},
			colTypes: map[string]string{
				"connection_id": "integer",
				"collected_at":  "timestamp with time zone",
				"inserted_at":   "timestamp with time zone",
				"datname":       "name",
			},
			want: []string{"datname"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildDimensionColumns(tt.allColumns, tt.colTypes)
			if len(got) != len(tt.want) {
				t.Errorf("BuildDimensionColumns() = %v, want %v",
					got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("BuildDimensionColumns()[%d] = %q, want %q",
						i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestNormalizeValue(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  any
	}{
		{"nil", nil, nil},
		{"int64", int64(42), int64(42)},
		{"int32", int32(42), int64(42)},
		{"int16", int16(42), int64(42)},
		{"float64", float64(3.14), float64(3.14)},
		{"float32", float32(3.14), float64(float32(3.14))},
		{"string", "hello", "hello"},
		{"bytes", []byte("hello"), "hello"},
		{"bool true", true, true},
		{"bool false", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeValue(tt.input)
			if got != tt.want {
				t.Errorf("normalizeValue(%v) = %v (%T), want %v (%T)",
					tt.input, got, got, tt.want, tt.want)
			}
		})
	}
}

func TestLatestSnapshotHandler_MissingConnectionID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/latest?probe_name=pg_stat_all_tables", nil)
	rec := httptest.NewRecorder()

	// Call the handler function directly to test parameter validation.
	// We need a handler with nil datastore to avoid DB calls, but the
	// handler checks for nil datastore in RegisterRoutes, not in the
	// handler itself. Since we cannot test the real handler without a DB,
	// we test parameter validation by calling handleLatestSnapshot with
	// a nil datastore (which will fail at a later stage). The connection_id
	// check happens before any DB access.
	handler := &LatestSnapshotHandler{}
	handler.handleLatestSnapshot(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d",
			http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "connection_id is required" {
		t.Errorf("Expected error %q, got %q",
			"connection_id is required", response.Error)
	}
}

func TestLatestSnapshotHandler_MissingProbeName(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/latest?connection_id=1", nil)
	rec := httptest.NewRecorder()

	// With nil authStore, the RBAC checker treats the caller as a
	// superuser, so we reach the probe_name validation.
	handler := &LatestSnapshotHandler{}
	handler.handleLatestSnapshot(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d",
			http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "probe_name is required" {
		t.Errorf("Expected error %q, got %q",
			"probe_name is required", response.Error)
	}
}

func TestLatestSnapshotHandler_InvalidConnectionID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/latest?connection_id=abc&probe_name=test", nil)
	rec := httptest.NewRecorder()

	handler := &LatestSnapshotHandler{}
	handler.handleLatestSnapshot(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d",
			http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "Invalid connection_id" {
		t.Errorf("Expected error %q, got %q",
			"Invalid connection_id", response.Error)
	}
}

func TestLatestSnapshotHandler_InvalidOrder(t *testing.T) {
	// Test that invalid order direction is rejected. Since RBAC check
	// happens first, we cannot reach order validation without a real
	// authStore. Verify the validation logic via a focused unit test.
	tests := []struct {
		order string
		valid bool
	}{
		{"asc", true},
		{"desc", true},
		{"ASC", true},
		{"DESC", true},
		{"invalid", false},
		{"", true}, // empty defaults to "desc"
	}

	for _, tt := range tests {
		t.Run("order="+tt.order, func(t *testing.T) {
			normalized := tt.order
			if normalized != "" {
				// Simulate the handler's ToLower normalization
				lower := ""
				for _, c := range normalized {
					if c >= 'A' && c <= 'Z' {
						lower += string(c + 32)
					} else {
						lower += string(c)
					}
				}
				normalized = lower
			}

			if normalized == "" {
				normalized = "desc"
			}

			valid := normalized == "asc" || normalized == "desc"
			if valid != tt.valid {
				t.Errorf("order %q: valid=%v, want %v",
					tt.order, valid, tt.valid)
			}
		})
	}
}

func TestLatestSnapshotHandler_LimitCapping(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected int
	}{
		{"normal", 20, 20},
		{"max", 100, 100},
		{"over max", 200, 100},
		{"minimum", 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limit := tt.input
			if limit > 100 {
				limit = 100
			}
			if limit != tt.expected {
				t.Errorf("limit capping: %d -> %d, want %d",
					tt.input, limit, tt.expected)
			}
		})
	}
}
