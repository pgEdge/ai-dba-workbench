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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
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
// non-nil the constructor installs it as the alertResolver and the
// alertUnacknowledger. Wiring both fields up front unblocks the
// RBAC-first mutation flows without needing tests to call the setter
// helpers explicitly.
func TestNewAlertHandler_WiresResolver(t *testing.T) {
	ds := &database.Datastore{}
	handler := NewAlertHandler(ds, nil, nil)
	if handler == nil {
		t.Fatal("NewAlertHandler returned nil")
	}
	if handler.alertResolver == nil {
		t.Error("Expected alertResolver to be wired to the datastore")
	}
	if handler.unacknowledgeFn == nil {
		t.Error("Expected unacknowledgeFn to be wired to the datastore")
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

// fakeUnacknowledger lets unacknowledgeAlert handler tests inject an
// arbitrary error from the datastore boundary. The handler maps three
// outcomes to three HTTP codes (200, 404 for ErrAlertNotFound, 409 for
// ErrAlertNotAcknowledged, 500 for anything else) and these tests cover
// every branch without touching a real database.
type fakeUnacknowledger struct {
	err   error
	calls int
}

func (f *fakeUnacknowledger) UnacknowledgeAlert(_ context.Context, _ int64) error {
	f.calls++
	return f.err
}

// TestAlertHandler_UnacknowledgeAlert_Happy verifies the success path
// where the datastore returns nil. The handler must respond 200 with
// {"status":"active"}.
func TestAlertHandler_UnacknowledgeAlert_Happy(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	aliceID := newTestUser(t, store, "alice")
	checker := mockSharingChecker(t, store, rbacUnsharedConnID, "alice", false)
	handler := NewAlertHandler(nil, store, checker)
	handler.setAlertResolver(&fakeAlertResolver{connID: rbacUnsharedConnID})
	fake := &fakeUnacknowledger{}
	handler.setUnacknowledgeFn(fake)

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/alerts/acknowledge?alert_id=7", nil)
	req = withUser(req, aliceID)
	req = withUsername(req, "alice")
	rec := httptest.NewRecorder()
	handler.unacknowledgeAlert(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if fake.calls != 1 {
		t.Errorf("expected fake.calls=1, got %d", fake.calls)
	}
	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp["status"] != "active" {
		t.Errorf("status field = %q, want active", resp["status"])
	}
}

// TestAlertHandler_UnacknowledgeAlert_NotFound_Maps404 verifies that
// ErrAlertNotFound from the datastore produces HTTP 404, not the 500
// the pre-fix code returned for every datastore error.
func TestAlertHandler_UnacknowledgeAlert_NotFound_Maps404(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	aliceID := newTestUser(t, store, "alice")
	checker := mockSharingChecker(t, store, rbacUnsharedConnID, "alice", false)
	handler := NewAlertHandler(nil, store, checker)
	handler.setAlertResolver(&fakeAlertResolver{connID: rbacUnsharedConnID})
	fake := &fakeUnacknowledger{err: fmt.Errorf("wrapped: %w", database.ErrAlertNotFound)}
	handler.setUnacknowledgeFn(fake)

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/alerts/acknowledge?alert_id=42", nil)
	req = withUser(req, aliceID)
	req = withUsername(req, "alice")
	rec := httptest.NewRecorder()
	handler.unacknowledgeAlert(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body=%s",
			rec.Code, http.StatusNotFound, rec.Body.String())
	}
	var resp ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.Error != "Alert not found" {
		t.Errorf("error = %q, want %q", resp.Error, "Alert not found")
	}
}

// TestAlertHandler_UnacknowledgeAlert_NotAcknowledged_Maps409 verifies
// that the new sentinel ErrAlertNotAcknowledged produces HTTP 409, which
// is the precise status code for "the resource is in a state that
// prevents the requested transition." A 500 here (the pre-fix behavior)
// would suggest a server problem when in reality the request is racing
// the alerter or arriving from a stale UI.
func TestAlertHandler_UnacknowledgeAlert_NotAcknowledged_Maps409(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	aliceID := newTestUser(t, store, "alice")
	checker := mockSharingChecker(t, store, rbacUnsharedConnID, "alice", false)
	handler := NewAlertHandler(nil, store, checker)
	handler.setAlertResolver(&fakeAlertResolver{connID: rbacUnsharedConnID})
	fake := &fakeUnacknowledger{
		err: fmt.Errorf("unacknowledge alert 42 (status=%q): %w",
			"active", database.ErrAlertNotAcknowledged),
	}
	handler.setUnacknowledgeFn(fake)

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/alerts/acknowledge?alert_id=42", nil)
	req = withUser(req, aliceID)
	req = withUsername(req, "alice")
	rec := httptest.NewRecorder()
	handler.unacknowledgeAlert(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body=%s",
			rec.Code, http.StatusConflict, rec.Body.String())
	}
	var resp ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.Error != "Alert is not currently acknowledged" {
		t.Errorf("error = %q, want %q",
			resp.Error, "Alert is not currently acknowledged")
	}
}

// TestAlertHandler_UnacknowledgeAlert_OtherError_Maps500 verifies that
// arbitrary datastore errors (transaction begin failure, commit failure,
// etc.) still produce HTTP 500. The handler must NOT misclassify
// internal failures as 404 or 409.
func TestAlertHandler_UnacknowledgeAlert_OtherError_Maps500(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	aliceID := newTestUser(t, store, "alice")
	checker := mockSharingChecker(t, store, rbacUnsharedConnID, "alice", false)
	handler := NewAlertHandler(nil, store, checker)
	handler.setAlertResolver(&fakeAlertResolver{connID: rbacUnsharedConnID})
	fake := &fakeUnacknowledger{err: errors.New("commit failed: connection reset")}
	handler.setUnacknowledgeFn(fake)

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/alerts/acknowledge?alert_id=99", nil)
	req = withUser(req, aliceID)
	req = withUsername(req, "alice")
	rec := httptest.NewRecorder()
	handler.unacknowledgeAlert(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d, body=%s",
			rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	var resp ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.Error != "Failed to unacknowledge alert" {
		t.Errorf("error = %q, want %q",
			resp.Error, "Failed to unacknowledge alert")
	}
}

// TestAlertHandler_UnacknowledgeAlert_MissingUnackFn_Maps500 makes sure
// the explicit nil-check on the unacknowledge function returns 500
// rather than panicking. The nil-resolver case has the same shape and is
// already covered in rbac_issue35_test.go; this test mirrors it for the
// new injected dependency.
func TestAlertHandler_UnacknowledgeAlert_MissingUnackFn_Maps500(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	aliceID := newTestUser(t, store, "alice")
	checker := mockSharingChecker(t, store, rbacUnsharedConnID, "alice", false)
	handler := NewAlertHandler(nil, store, checker)
	handler.setAlertResolver(&fakeAlertResolver{connID: rbacUnsharedConnID})
	// Intentionally do NOT call setUnacknowledgeFn.

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/alerts/acknowledge?alert_id=99", nil)
	req = withUser(req, aliceID)
	req = withUsername(req, "alice")
	rec := httptest.NewRecorder()
	handler.unacknowledgeAlert(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d, body=%s",
			rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
}

// TestAlertHandler_SetUnacknowledgeFn_OverridesDefault verifies the
// test-only injection point installs a custom unacknowledger. The
// default constructor wires the datastore as the unacknowledger; tests
// override it to drive the sentinel-error paths.
func TestAlertHandler_SetUnacknowledgeFn_OverridesDefault(t *testing.T) {
	handler := NewAlertHandler(nil, nil, nil)
	if handler.unacknowledgeFn != nil {
		t.Fatalf("expected nil unacknowledgeFn for nil-datastore constructor")
	}
	fake := &fakeUnacknowledger{}
	handler.setUnacknowledgeFn(fake)
	if handler.unacknowledgeFn == nil {
		t.Errorf("setUnacknowledgeFn did not install the fake")
	}
	// The wired-up datastore case is covered by
	// TestNewAlertHandler_WiresResolver; this test focuses on the
	// override entry point.
	_ = auth.UsernameContextKey
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
