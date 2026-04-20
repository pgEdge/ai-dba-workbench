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

func TestNewTimelineHandler(t *testing.T) {
	handler := NewTimelineHandler(nil, nil, nil)
	if handler == nil {
		t.Fatal("NewTimelineHandler returned nil")
	}
	if handler.datastore != nil {
		t.Error("Expected nil datastore")
	}
	if handler.authStore != nil {
		t.Error("Expected nil authStore")
	}
}

func TestTimelineHandler_HandleNotConfigured(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/timeline/events", nil)
	rec := httptest.NewRecorder()

	HandleNotConfigured("Timeline")(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got %s", contentType)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	expectedError := "Timeline is not available. The datastore is not configured."
	if response.Error != expectedError {
		t.Errorf("Expected error %q, got %q", expectedError, response.Error)
	}
}

func TestTimelineHandler_HandleTimelineEvents_MethodNotAllowed(t *testing.T) {
	handler := NewTimelineHandler(nil, nil, nil)

	methods := []string{
		http.MethodPost,
		http.MethodPut,
		http.MethodDelete,
		http.MethodPatch,
	}

	for _, method := range methods {
		t.Run(method+" not allowed", func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/v1/timeline/events", nil)
			rec := httptest.NewRecorder()

			handler.handleTimelineEvents(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("Expected status %d, got %d",
					http.StatusMethodNotAllowed, rec.Code)
			}

			allowed := rec.Header().Get("Allow")
			if allowed != "GET" {
				t.Errorf("Expected Allow header 'GET', got %q", allowed)
			}
		})
	}
}

func TestTimelineHandler_HandleTimelineEvents_MissingStartTime(t *testing.T) {
	handler := NewTimelineHandler(nil, nil, nil)

	// Missing start_time
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/timeline/events?end_time=2026-01-31T00:00:00Z", nil)
	rec := httptest.NewRecorder()

	handler.handleTimelineEvents(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "start_time is required" {
		t.Errorf("Expected error 'start_time is required', got %q", response.Error)
	}
}

func TestTimelineHandler_HandleTimelineEvents_MissingEndTime(t *testing.T) {
	handler := NewTimelineHandler(nil, nil, nil)

	// Missing end_time
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/timeline/events?start_time=2026-01-30T00:00:00Z", nil)
	rec := httptest.NewRecorder()

	handler.handleTimelineEvents(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "end_time is required" {
		t.Errorf("Expected error 'end_time is required', got %q", response.Error)
	}
}

func TestTimelineHandler_HandleTimelineEvents_InvalidStartTime(t *testing.T) {
	handler := NewTimelineHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/timeline/events?start_time=invalid&end_time=2026-01-31T00:00:00Z", nil)
	rec := httptest.NewRecorder()

	handler.handleTimelineEvents(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestTimelineHandler_HandleTimelineEvents_InvalidEndTime(t *testing.T) {
	handler := NewTimelineHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/timeline/events?start_time=2026-01-30T00:00:00Z&end_time=invalid", nil)
	rec := httptest.NewRecorder()

	handler.handleTimelineEvents(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestTimelineHandler_HandleTimelineEvents_InvalidTimeRange(t *testing.T) {
	handler := NewTimelineHandler(nil, nil, nil)

	// end_time before start_time
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/timeline/events?start_time=2026-01-31T00:00:00Z&end_time=2026-01-30T00:00:00Z", nil)
	rec := httptest.NewRecorder()

	handler.handleTimelineEvents(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "end_time must be after start_time" {
		t.Errorf("Expected error about time range, got %q", response.Error)
	}
}

func TestTimelineHandler_HandleTimelineEvents_InvalidEventType(t *testing.T) {
	handler := NewTimelineHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/timeline/events?start_time=2026-01-30T00:00:00Z&end_time=2026-01-31T00:00:00Z&event_types=invalid_type", nil)
	rec := httptest.NewRecorder()

	handler.handleTimelineEvents(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Should mention invalid event_type
	if response.Error == "" {
		t.Error("Expected non-empty error message")
	}
}

func TestTimelineHandler_HandleTimelineEvents_InvalidConnectionID(t *testing.T) {
	handler := NewTimelineHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/timeline/events?start_time=2026-01-30T00:00:00Z&end_time=2026-01-31T00:00:00Z&connection_id=abc", nil)
	rec := httptest.NewRecorder()

	handler.handleTimelineEvents(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestTimelineHandler_HandleTimelineEvents_InvalidConnectionIDs(t *testing.T) {
	handler := NewTimelineHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/timeline/events?start_time=2026-01-30T00:00:00Z&end_time=2026-01-31T00:00:00Z&connection_ids=1,abc,3", nil)
	rec := httptest.NewRecorder()

	handler.handleTimelineEvents(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestTimelineHandler_RegisterRoutes_NotConfigured(t *testing.T) {
	handler := NewTimelineHandler(nil, nil, nil)
	mux := http.NewServeMux()
	noopWrapper := func(h http.HandlerFunc) http.HandlerFunc { return h }

	handler.RegisterRoutes(mux, noopWrapper)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/timeline/events", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status %d, got %d",
			http.StatusServiceUnavailable, rec.Code)
	}
}
