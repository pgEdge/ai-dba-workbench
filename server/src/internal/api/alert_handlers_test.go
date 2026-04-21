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
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/database"
)

func TestNewAlertHandler(t *testing.T) {
	handler := NewAlertHandler(nil, nil, nil)
	if handler == nil {
		t.Fatal("NewAlertHandler returned nil")
	}
	if handler.datastore != nil {
		t.Error("Expected nil datastore")
	}
	if handler.authStore != nil {
		t.Error("Expected nil authStore")
	}
	if handler.alertResolver != nil {
		t.Error("Expected nil alertResolver for nil-datastore constructor")
	}
}

// TestNewAlertHandler_WiresResolver verifies that when the datastore is
// non-nil the constructor installs it as the alertResolver. The
// resolver unblocks RBAC-first mutation flows without needing tests to
// call setAlertResolver explicitly.
func TestNewAlertHandler_WiresResolver(t *testing.T) {
	ds := &database.Datastore{}
	handler := NewAlertHandler(ds, nil, nil)
	if handler == nil {
		t.Fatal("NewAlertHandler returned nil")
	}
	if handler.alertResolver == nil {
		t.Error("Expected alertResolver to be wired to the datastore")
	}
}

func TestAlertHandler_HandleNotConfigured(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
	rec := httptest.NewRecorder()

	HandleNotConfigured("Alert management")(rec, req)

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

	expectedError := "Alert management is not available. The datastore is not configured."
	if response.Error != expectedError {
		t.Errorf("Expected error %q, got %q", expectedError, response.Error)
	}
}

func TestAlertHandler_HandleAlerts_MethodNotAllowed(t *testing.T) {
	handler := NewAlertHandler(nil, nil, nil)

	tests := []struct {
		name   string
		method string
	}{
		{"POST not allowed", http.MethodPost},
		{"PUT not allowed", http.MethodPut},
		{"DELETE not allowed", http.MethodDelete},
		{"PATCH not allowed", http.MethodPatch},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/v1/alerts", nil)
			rec := httptest.NewRecorder()

			handler.handleAlerts(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
			}
		})
	}
}

func TestAlertHandler_HandleAlertCounts_MethodNotAllowed(t *testing.T) {
	handler := NewAlertHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/counts", nil)
	rec := httptest.NewRecorder()

	handler.handleAlertCounts(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestAlertHandler_HandleAcknowledge_MethodNotAllowed(t *testing.T) {
	handler := NewAlertHandler(nil, nil, nil)

	// PATCH is not allowed
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/alerts/acknowledge", nil)
	rec := httptest.NewRecorder()

	handler.handleAcknowledge(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "Method not allowed" {
		t.Errorf("Expected error 'Method not allowed', got %q", response.Error)
	}
}

func TestAlertHandler_AcknowledgeAlert_MissingAlertID(t *testing.T) {
	handler := NewAlertHandler(nil, nil, nil)

	body := `{"message": "test acknowledgment"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/acknowledge",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.acknowledgeAlert(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "alert_id is required" {
		t.Errorf("Expected error 'alert_id is required', got %q", response.Error)
	}
}

func TestAlertHandler_AcknowledgeAlert_InvalidJSON(t *testing.T) {
	handler := NewAlertHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/acknowledge",
		bytes.NewBufferString("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.acknowledgeAlert(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "Invalid request body" {
		t.Errorf("Expected error 'Invalid request body', got %q", response.Error)
	}
}

func TestAlertHandler_UnacknowledgeAlert_MissingAlertID(t *testing.T) {
	handler := NewAlertHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/alerts/acknowledge", nil)
	rec := httptest.NewRecorder()

	handler.unacknowledgeAlert(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "alert_id query parameter is required" {
		t.Errorf("Expected error 'alert_id query parameter is required', got %q", response.Error)
	}
}

func TestAlertHandler_UnacknowledgeAlert_InvalidAlertID(t *testing.T) {
	handler := NewAlertHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/alerts/acknowledge?alert_id=invalid", nil)
	rec := httptest.NewRecorder()

	handler.unacknowledgeAlert(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestAlertHandler_RegisterRoutes_NotConfigured(t *testing.T) {
	handler := NewAlertHandler(nil, nil, nil)
	mux := http.NewServeMux()
	noopWrapper := func(h http.HandlerFunc) http.HandlerFunc { return h }

	handler.RegisterRoutes(mux, noopWrapper)

	paths := []string{
		"/api/v1/alerts",
		"/api/v1/alerts/counts",
		"/api/v1/alerts/acknowledge",
	}

	for _, path := range paths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("Path %s: expected status %d, got %d",
				path, http.StatusServiceUnavailable, rec.Code)
		}
	}
}

func TestAcknowledgeRequest_JSON(t *testing.T) {
	req := AcknowledgeRequest{
		AlertID:       123,
		Message:       "Acknowledged during maintenance",
		FalsePositive: true,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal AcknowledgeRequest: %v", err)
	}

	var decoded AcknowledgeRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal AcknowledgeRequest: %v", err)
	}

	if decoded.AlertID != req.AlertID {
		t.Errorf("AlertID = %d, want %d", decoded.AlertID, req.AlertID)
	}
	if decoded.Message != req.Message {
		t.Errorf("Message = %q, want %q", decoded.Message, req.Message)
	}
	if decoded.FalsePositive != req.FalsePositive {
		t.Errorf("FalsePositive = %v, want %v", decoded.FalsePositive, req.FalsePositive)
	}

	// Verify JSON field names
	var rawJSON map[string]any
	if err := json.Unmarshal(data, &rawJSON); err != nil {
		t.Fatalf("Failed to unmarshal to map: %v", err)
	}

	if _, ok := rawJSON["alert_id"]; !ok {
		t.Error("Expected 'alert_id' key in JSON")
	}
	if _, ok := rawJSON["message"]; !ok {
		t.Error("Expected 'message' key in JSON")
	}
	if _, ok := rawJSON["false_positive"]; !ok {
		t.Error("Expected 'false_positive' key in JSON")
	}
}

// TestAlertHandler_SaveAnalysis_Validation covers the input-validation
// branches of handleSaveAnalysis: method-not-allowed, invalid JSON,
// missing alert_id, missing analysis. These run BEFORE the resolver so
// no datastore is required.
func TestAlertHandler_SaveAnalysis_Validation(t *testing.T) {
	cases := []struct {
		name     string
		method   string
		body     string
		wantCode int
	}{
		{
			name:     "method-not-allowed",
			method:   http.MethodPost,
			wantCode: http.StatusMethodNotAllowed,
		},
		{
			name:     "invalid-json",
			method:   http.MethodPut,
			body:     "not json",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "missing-alert-id",
			method:   http.MethodPut,
			body:     `{"analysis":"x"}`,
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "missing-analysis",
			method:   http.MethodPut,
			body:     `{"alert_id":42}`,
			wantCode: http.StatusBadRequest,
		},
	}
	handler := NewAlertHandler(nil, nil, nil)
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/api/v1/alerts/analysis",
				bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			handler.handleSaveAnalysis(rec, req)

			if rec.Code != tc.wantCode {
				t.Errorf("case %q: expected %d, got %d (body=%q)",
					tc.name, tc.wantCode, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAlertHandler_HandleAcknowledge_Methods(t *testing.T) {
	handler := NewAlertHandler(nil, nil, nil)

	tests := []struct {
		name     string
		method   string
		wantCode int
	}{
		{"POST dispatches to acknowledge", http.MethodPost, http.StatusBadRequest},
		{"DELETE dispatches to unacknowledge", http.MethodDelete, http.StatusBadRequest},
		{"GET not allowed", http.MethodGet, http.StatusMethodNotAllowed},
		{"PUT not allowed", http.MethodPut, http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body string
			if tt.method == http.MethodPost {
				body = `{}`
			}
			req := httptest.NewRequest(tt.method, "/api/v1/alerts/acknowledge",
				bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			handler.handleAcknowledge(rec, req)

			if rec.Code != tt.wantCode {
				t.Errorf("Expected status %d, got %d", tt.wantCode, rec.Code)
			}
		})
	}
}
