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

func TestNewProbeConfigHandler(t *testing.T) {
	handler := NewProbeConfigHandler(nil, nil, nil)
	if handler == nil {
		t.Fatal("NewProbeConfigHandler returned nil")
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

func TestProbeConfigHandler_HandleNotConfigured(t *testing.T) {
	handler := NewProbeConfigHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/probe-configs", nil)
	rec := httptest.NewRecorder()

	handler.handleNotConfigured(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	expectedError := "Probe configuration is not available. The datastore is not configured."
	if response.Error != expectedError {
		t.Errorf("Expected error %q, got %q", expectedError, response.Error)
	}
}

func TestProbeConfigHandler_HandleProbeConfigs_MethodNotAllowed(t *testing.T) {
	handler := NewProbeConfigHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/probe-configs", nil)
	rec := httptest.NewRecorder()

	handler.handleProbeConfigs(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	allowed := rec.Header().Get("Allow")
	if allowed != "GET" {
		t.Errorf("Expected Allow header 'GET', got %q", allowed)
	}
}

func TestProbeConfigHandler_HandleSubpath_InvalidID(t *testing.T) {
	handler := NewProbeConfigHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/probe-configs/abc", nil)
	rec := httptest.NewRecorder()

	handler.handleProbeConfigSubpath(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "Invalid probe config ID" {
		t.Errorf("Expected error 'Invalid probe config ID', got %q", response.Error)
	}
}

func TestProbeConfigHandler_HandleSubpath_MethodNotAllowed(t *testing.T) {
	handler := NewProbeConfigHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/probe-configs/1", nil)
	rec := httptest.NewRecorder()

	handler.handleProbeConfigSubpath(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	allowed := rec.Header().Get("Allow")
	if allowed != "GET, PUT" {
		t.Errorf("Expected Allow header 'GET, PUT', got %q", allowed)
	}
}

func TestProbeConfigHandler_UpdateRequiresPermission(t *testing.T) {
	// Create a real auth store for permission checking
	tmpDir, err := os.MkdirTemp("", "probe-config-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	authStore, err := auth.NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		t.Fatalf("Failed to create auth store: %v", err)
	}
	defer authStore.Close()

	rbac := auth.NewRBACChecker(authStore, true)
	handler := NewProbeConfigHandler(nil, nil, rbac)

	body, _ := json.Marshal(map[string]bool{"is_enabled": false})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/probe-configs/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.handleProbeConfigSubpath(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d", http.StatusForbidden, rec.Code)
	}
}

func TestProbeConfigHandler_RegisterRoutes_NotConfigured(t *testing.T) {
	handler := NewProbeConfigHandler(nil, nil, nil)
	mux := http.NewServeMux()
	noopWrapper := func(h http.HandlerFunc) http.HandlerFunc { return h }

	handler.RegisterRoutes(mux, noopWrapper)

	paths := []string{
		"/api/v1/probe-configs",
		"/api/v1/probe-configs/1",
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
