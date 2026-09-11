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
	"time"

	"github.com/pgedge/ai-workbench/server/internal/metrics"
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

// TestTimelineHandler_HandleTimelineEvents_InvalidTimeRange covers the
// ordering rule, which is shared with /metrics/query through
// metrics.ResolveCustomWindow: the end must fall strictly after the
// start, so an equal pair is rejected as well as a reversed one.
func TestTimelineHandler_HandleTimelineEvents_InvalidTimeRange(t *testing.T) {
	handler := NewTimelineHandler(nil, nil, nil)

	tests := []struct {
		name  string
		query string
	}{
		{
			name:  "end_time before start_time",
			query: "start_time=2026-01-31T00:00:00Z&end_time=2026-01-30T00:00:00Z",
		},
		{
			name:  "end_time equal to start_time",
			query: "start_time=2026-01-30T00:00:00Z&end_time=2026-01-30T00:00:00Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet,
				"/api/v1/timeline/events?"+tt.query, nil)
			rec := httptest.NewRecorder()

			handler.handleTimelineEvents(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
			}

			var response ErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			const want = "invalid time range: end_time must be after start_time"
			if response.Error != want {
				t.Errorf("Expected error %q, got %q", want, response.Error)
			}
		})
	}
}

// TestTimelineHandler_HandleTimelineEvents_FutureStart confirms that a
// window starting in the future is rejected with the same 400 that
// /metrics/query returns, rather than passing through to an empty
// result. The nil datastore would panic if the request were accepted.
func TestTimelineHandler_HandleTimelineEvents_FutureStart(t *testing.T) {
	handler := NewTimelineHandler(nil, nil, nil)

	now := time.Now().UTC()
	url := "/api/v1/timeline/events?start_time=" +
		now.Add(time.Hour).Format(time.RFC3339) + "&end_time=" +
		now.Add(2*time.Hour).Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()

	handler.handleTimelineEvents(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	const want = "invalid start_time: must not be in the future"
	if response.Error != want {
		t.Errorf("Expected error %q, got %q", want, response.Error)
	}
}

// TestResolveTimelineWindow_FutureEndClamped drives the window resolver
// directly, because the clamped end is only observable in the filter
// handed to the datastore. An end in the future must be pulled back to
// now whilst the start is left untouched.
func TestResolveTimelineWindow_FutureEndClamped(t *testing.T) {
	now := time.Now().UTC()
	start := now.Add(-2 * time.Hour).Truncate(time.Second)
	url := "/api/v1/timeline/events?start_time=" +
		start.Format(time.RFC3339) + "&end_time=" +
		now.Add(time.Hour).Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()

	window, ok := resolveTimelineWindow(rec, req)
	if !ok {
		t.Fatalf("Expected the window to resolve, got %d: %s",
			rec.Code, rec.Body.String())
	}
	if !window.Start.Equal(start) {
		t.Errorf("start = %v, want %v", window.Start, start)
	}
	if window.End.After(time.Now().UTC()) {
		t.Errorf("clamped end %v is still in the future", window.End)
	}
	if window.End.Before(now) {
		t.Errorf("clamped end %v is before now %v", window.End, now)
	}
}

// TestResolveTimelineWindow_Errors covers each 400 the resolver can
// write, confirming that the parameter names in the messages match the
// timeline endpoint's own query parameters rather than the metrics
// endpoint's time_start and time_end.
func TestResolveTimelineWindow_Errors(t *testing.T) {
	now := time.Now().UTC()
	iso := func(t time.Time) string { return t.Format(time.RFC3339) }

	tests := []struct {
		name    string
		query   string
		wantErr string
	}{
		{
			name:    "missing start_time",
			query:   "end_time=" + iso(now),
			wantErr: "start_time is required",
		},
		{
			name:    "missing end_time",
			query:   "start_time=" + iso(now.Add(-time.Hour)),
			wantErr: "end_time is required",
		},
		{
			name:    "unparsable start_time",
			query:   "start_time=yesterday&end_time=" + iso(now),
			wantErr: "Invalid start_time format, expected RFC3339",
		},
		{
			name:    "unparsable end_time",
			query:   "start_time=" + iso(now.Add(-time.Hour)) + "&end_time=tomorrow",
			wantErr: "Invalid end_time format, expected RFC3339",
		},
		{
			name:    "reversed window",
			query:   "start_time=" + iso(now) + "&end_time=" + iso(now.Add(-time.Hour)),
			wantErr: "invalid time range: end_time must be after start_time",
		},
		{
			name:    "future start",
			query:   "start_time=" + iso(now.Add(time.Hour)) + "&end_time=" + iso(now.Add(2*time.Hour)),
			wantErr: "invalid start_time: must not be in the future",
		},
		{
			name:    "span over the cap",
			query:   "start_time=" + iso(now.Add(-367*24*time.Hour)) + "&end_time=" + iso(now),
			wantErr: "invalid time range: span must not exceed 366 days",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet,
				"/api/v1/timeline/events?"+tt.query, nil)
			rec := httptest.NewRecorder()

			window, ok := resolveTimelineWindow(rec, req)
			if ok {
				t.Fatalf("Expected the window to be rejected, got %+v", window)
			}
			if rec.Code != http.StatusBadRequest {
				t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
			}

			var response ErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}
			if response.Error != tt.wantErr {
				t.Errorf("Expected error %q, got %q", tt.wantErr, response.Error)
			}
		})
	}
}

// TestTimelineHandler_HandleTimelineEvents_SpanCap covers the maximum
// span check. Every accepted case carries a deliberately invalid
// event_types value, because a request that clears validation would go
// on to dereference the nil datastore; an event_type error therefore
// proves the span itself was accepted.
func TestTimelineHandler_HandleTimelineEvents_SpanCap(t *testing.T) {
	handler := NewTimelineHandler(nil, nil, nil)

	const spanError = "invalid time range: span must not exceed 366 days"
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		span       time.Duration
		expectSpan bool
	}{
		{
			name:       "well within the cap",
			span:       24 * time.Hour,
			expectSpan: false,
		},
		{
			name:       "one second under the cap",
			span:       metrics.MaxCustomTimeSpan - time.Second,
			expectSpan: false,
		},
		{
			name:       "exactly at the cap",
			span:       metrics.MaxCustomTimeSpan,
			expectSpan: false,
		},
		{
			name:       "one second over the cap",
			span:       metrics.MaxCustomTimeSpan + time.Second,
			expectSpan: true,
		},
		{
			name:       "a decade",
			span:       3650 * 24 * time.Hour,
			expectSpan: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/api/v1/timeline/events?start_time=" +
				start.Format(time.RFC3339) + "&end_time=" +
				start.Add(tt.span).Format(time.RFC3339) +
				"&event_types=invalid_type"
			req := httptest.NewRequest(http.MethodGet, url, nil)
			rec := httptest.NewRecorder()

			handler.handleTimelineEvents(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("Expected status %d, got %d",
					http.StatusBadRequest, rec.Code)
			}

			var response ErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			if tt.expectSpan && response.Error != spanError {
				t.Errorf("Expected error %q, got %q", spanError, response.Error)
			}
			if !tt.expectSpan && response.Error == spanError {
				t.Errorf("Span of %v was rejected by the cap unexpectedly", tt.span)
			}
		})
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
