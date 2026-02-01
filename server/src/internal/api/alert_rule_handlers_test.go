/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

func TestNewAlertRuleHandler(t *testing.T) {
	handler := NewAlertRuleHandler(nil, nil, nil)
	if handler == nil {
		t.Fatal("NewAlertRuleHandler returned nil")
	}
	if handler.datastore != nil {
		t.Error("Expected nil datastore")
	}
	if handler.authStore != nil {
		t.Error("Expected nil authStore")
	}
	if handler.rbacChecker != nil {
		t.Error("Expected nil rbacChecker")
	}
}

func TestAlertRuleHandler_HandleNotConfigured(t *testing.T) {
	handler := NewAlertRuleHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alert-rules", nil)
	rec := httptest.NewRecorder()

	handler.handleNotConfigured(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	expectedError := "Alert rule management is not available. The datastore is not configured."
	if response.Error != expectedError {
		t.Errorf("Expected error %q, got %q", expectedError, response.Error)
	}
}

func TestAlertRuleHandler_HandleAlertRules_MethodNotAllowed(t *testing.T) {
	handler := NewAlertRuleHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/alert-rules", nil)
	rec := httptest.NewRecorder()

	handler.handleAlertRules(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	allowed := rec.Header().Get("Allow")
	if allowed != "GET" {
		t.Errorf("Expected Allow header 'GET', got %q", allowed)
	}
}

func TestAlertRuleHandler_HandleSubpath_InvalidID(t *testing.T) {
	handler := NewAlertRuleHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alert-rules/abc", nil)
	rec := httptest.NewRecorder()

	handler.handleAlertRuleSubpath(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "Invalid alert rule ID" {
		t.Errorf("Expected error 'Invalid alert rule ID', got %q", response.Error)
	}
}

func TestAlertRuleHandler_HandleSubpath_MethodNotAllowed(t *testing.T) {
	handler := NewAlertRuleHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/alert-rules/1", nil)
	rec := httptest.NewRecorder()

	handler.handleAlertRuleSubpath(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	allowed := rec.Header().Get("Allow")
	if allowed != "GET, PUT" {
		t.Errorf("Expected Allow header 'GET, PUT', got %q", allowed)
	}
}

func createTestAuthStoreForAlertRules(t *testing.T) (*auth.AuthStore, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "alert-rule-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	store, err := auth.NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create auth store: %v", err)
	}
	return store, func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}
}

func TestAlertRuleHandler_UpdateRequiresPermission(t *testing.T) {
	authStore, cleanup := createTestAuthStoreForAlertRules(t)
	defer cleanup()

	rbac := auth.NewRBACChecker(authStore, true)
	handler := NewAlertRuleHandler(nil, nil, rbac)

	body, _ := json.Marshal(map[string]bool{"default_enabled": false})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/alert-rules/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.handleAlertRuleSubpath(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d", http.StatusForbidden, rec.Code)
	}
}

func TestAlertRuleHandler_ThresholdCreateRequiresPermission(t *testing.T) {
	authStore, cleanup := createTestAuthStoreForAlertRules(t)
	defer cleanup()

	rbac := auth.NewRBACChecker(authStore, true)
	handler := NewAlertRuleHandler(nil, nil, rbac)

	body, _ := json.Marshal(map[string]interface{}{
		"operator":  ">",
		"threshold": 90.0,
		"severity":  "critical",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alert-rules/1/thresholds", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.handleAlertRuleSubpath(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d", http.StatusForbidden, rec.Code)
	}
}

func TestAlertRuleHandler_ThresholdSubpath_InvalidThresholdID(t *testing.T) {
	handler := NewAlertRuleHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/alert-rules/1/thresholds/abc", nil)
	rec := httptest.NewRecorder()

	handler.handleAlertRuleSubpath(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "Invalid threshold ID" {
		t.Errorf("Expected error 'Invalid threshold ID', got %q", response.Error)
	}
}

func TestAlertRuleHandler_ThresholdSubpath_MethodNotAllowed(t *testing.T) {
	handler := NewAlertRuleHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/alert-rules/1/thresholds/1", nil)
	rec := httptest.NewRecorder()

	handler.handleAlertRuleSubpath(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	allowed := rec.Header().Get("Allow")
	if allowed != "PUT, DELETE" {
		t.Errorf("Expected Allow header 'PUT, DELETE', got %q", allowed)
	}
}

func TestAlertRuleHandler_ThresholdsCollection_MethodNotAllowed(t *testing.T) {
	handler := NewAlertRuleHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/alert-rules/1/thresholds", nil)
	rec := httptest.NewRecorder()

	handler.handleAlertRuleSubpath(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	allowed := rec.Header().Get("Allow")
	if allowed != "GET, POST" {
		t.Errorf("Expected Allow header 'GET, POST', got %q", allowed)
	}
}

func TestAlertRuleHandler_RegisterRoutes_NotConfigured(t *testing.T) {
	handler := NewAlertRuleHandler(nil, nil, nil)
	mux := http.NewServeMux()
	noopWrapper := func(h http.HandlerFunc) http.HandlerFunc { return h }

	handler.RegisterRoutes(mux, noopWrapper)

	paths := []string{
		"/api/v1/alert-rules",
		"/api/v1/alert-rules/1",
		"/api/v1/alert-rules/1/thresholds",
	}

	for _, path := range paths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("Path %s: expected status %d, got %d", path, http.StatusServiceUnavailable, rec.Code)
		}
	}
}
