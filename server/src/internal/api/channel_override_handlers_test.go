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
	"os"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

func TestNewChannelOverrideHandler(t *testing.T) {
	handler := NewChannelOverrideHandler(nil, nil, nil)
	if handler == nil {
		t.Fatal("NewChannelOverrideHandler returned nil")
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

func TestChannelOverrideHandler_HandleNotConfigured(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/channel-overrides/server/1", nil)
	rec := httptest.NewRecorder()

	HandleNotConfigured("Channel override management")(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	expectedError := "Channel override management is not available. The datastore is not configured."
	if response.Error != expectedError {
		t.Errorf("Expected error %q, got %q", expectedError, response.Error)
	}
}

func TestChannelOverrideHandler_InvalidScope(t *testing.T) {
	handler := NewChannelOverrideHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/channel-overrides/invalid/1", nil)
	rec := httptest.NewRecorder()

	handler.handleChannelOverrides(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	expectedError := "Invalid scope: must be server, cluster, or group"
	if response.Error != expectedError {
		t.Errorf("Expected error %q, got %q", expectedError, response.Error)
	}
}

func TestChannelOverrideHandler_InvalidScopeID(t *testing.T) {
	handler := NewChannelOverrideHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/channel-overrides/server/abc", nil)
	rec := httptest.NewRecorder()

	handler.handleChannelOverrides(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "Invalid scope ID" {
		t.Errorf("Expected error 'Invalid scope ID', got %q", response.Error)
	}
}

func TestChannelOverrideHandler_InvalidChannelID(t *testing.T) {
	handler := NewChannelOverrideHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/channel-overrides/server/1/abc", nil)
	rec := httptest.NewRecorder()

	handler.handleChannelOverrides(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "Invalid channel ID" {
		t.Errorf("Expected error 'Invalid channel ID', got %q", response.Error)
	}
}

func TestChannelOverrideHandler_ListMethodNotAllowed(t *testing.T) {
	handler := NewChannelOverrideHandler(nil, nil, nil)

	methods := []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}
	for _, method := range methods {
		req := httptest.NewRequest(method, "/api/v1/channel-overrides/server/1", nil)
		rec := httptest.NewRecorder()

		handler.handleChannelOverrides(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("Method %s: expected status %d, got %d", method, http.StatusMethodNotAllowed, rec.Code)
		}

		allowed := rec.Header().Get("Allow")
		if allowed != "GET" {
			t.Errorf("Method %s: expected Allow header 'GET', got %q", method, allowed)
		}
	}
}

func TestChannelOverrideHandler_SubpathMethodNotAllowed(t *testing.T) {
	handler := NewChannelOverrideHandler(nil, nil, nil)

	methods := []string{http.MethodGet, http.MethodPost, http.MethodPatch}
	for _, method := range methods {
		req := httptest.NewRequest(method, "/api/v1/channel-overrides/server/1/2", nil)
		rec := httptest.NewRecorder()

		handler.handleChannelOverrides(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("Method %s: expected status %d, got %d", method, http.StatusMethodNotAllowed, rec.Code)
		}

		allowed := rec.Header().Get("Allow")
		if allowed != "PUT, DELETE" {
			t.Errorf("Method %s: expected Allow header 'PUT, DELETE', got %q", method, allowed)
		}
	}
}

func TestChannelOverrideHandler_MissingPathSegments(t *testing.T) {
	handler := NewChannelOverrideHandler(nil, nil, nil)

	paths := []string{
		"/api/v1/channel-overrides/",
		"/api/v1/channel-overrides/server/",
	}

	for _, path := range paths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()

		handler.handleChannelOverrides(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("Path %s: expected status %d, got %d", path, http.StatusNotFound, rec.Code)
		}
	}
}

func TestChannelOverrideHandler_ExtraPathSegments(t *testing.T) {
	handler := NewChannelOverrideHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/channel-overrides/server/1/2/extra", nil)
	rec := httptest.NewRecorder()

	handler.handleChannelOverrides(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
}

func TestChannelOverrideHandler_ValidScopes(t *testing.T) {
	authStore, cleanup := createTestAuthStoreForChannelOverrides(t)
	defer cleanup()

	rbac := auth.NewRBACChecker(authStore, true)
	handler := NewChannelOverrideHandler(nil, nil, rbac)

	scopes := []string{"server", "cluster", "group"}
	for _, scope := range scopes {
		// Use DELETE to exercise scope validation and permission check.
		// With auth enabled and no user context, the permission check
		// returns 403, which confirms the scope was accepted.
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/channel-overrides/"+scope+"/1/2", nil)
		rec := httptest.NewRecorder()

		handler.handleChannelOverrides(rec, req)

		if rec.Code == http.StatusBadRequest {
			var response ErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&response); err == nil {
				if response.Error == "Invalid scope: must be server, cluster, or group" {
					t.Errorf("Scope %q was incorrectly rejected as invalid", scope)
				}
			}
		}
	}
}

func createTestAuthStoreForChannelOverrides(t *testing.T) (*auth.AuthStore, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "channel-override-test-*")
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

func TestChannelOverrideHandler_UpsertRequiresPermission(t *testing.T) {
	authStore, cleanup := createTestAuthStoreForChannelOverrides(t)
	defer cleanup()

	rbac := auth.NewRBACChecker(authStore, true)
	handler := NewChannelOverrideHandler(nil, nil, rbac)

	body, _ := json.Marshal(map[string]interface{}{"enabled": true})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/channel-overrides/server/1/2", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.handleChannelOverrides(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d", http.StatusForbidden, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	expectedError := "Permission denied: you do not have permission to manage notification channels"
	if response.Error != expectedError {
		t.Errorf("Expected error %q, got %q", expectedError, response.Error)
	}
}

func TestChannelOverrideHandler_DeleteRequiresPermission(t *testing.T) {
	authStore, cleanup := createTestAuthStoreForChannelOverrides(t)
	defer cleanup()

	rbac := auth.NewRBACChecker(authStore, true)
	handler := NewChannelOverrideHandler(nil, nil, rbac)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/channel-overrides/cluster/5/10", nil)
	rec := httptest.NewRecorder()

	handler.handleChannelOverrides(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d", http.StatusForbidden, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	expectedError := "Permission denied: you do not have permission to manage notification channels"
	if response.Error != expectedError {
		t.Errorf("Expected error %q, got %q", expectedError, response.Error)
	}
}

func TestChannelOverrideHandler_RegisterRoutes_NotConfigured(t *testing.T) {
	handler := NewChannelOverrideHandler(nil, nil, nil)
	mux := http.NewServeMux()
	noopWrapper := func(h http.HandlerFunc) http.HandlerFunc { return h }

	handler.RegisterRoutes(mux, noopWrapper)

	paths := []string{
		"/api/v1/channel-overrides/server/1",
		"/api/v1/channel-overrides/cluster/2",
		"/api/v1/channel-overrides/group/3",
		"/api/v1/channel-overrides/server/1/5",
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

func TestChannelOverrideHandler_PermissionCheckAllScopes(t *testing.T) {
	authStore, cleanup := createTestAuthStoreForChannelOverrides(t)
	defer cleanup()

	rbac := auth.NewRBACChecker(authStore, true)
	handler := NewChannelOverrideHandler(nil, nil, rbac)

	scopes := []string{"server", "cluster", "group"}
	for _, scope := range scopes {
		body, _ := json.Marshal(map[string]interface{}{"enabled": true})
		req := httptest.NewRequest(http.MethodPut, "/api/v1/channel-overrides/"+scope+"/1/2", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.handleChannelOverrides(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Scope %s PUT: expected status %d, got %d", scope, http.StatusForbidden, rec.Code)
		}

		req = httptest.NewRequest(http.MethodDelete, "/api/v1/channel-overrides/"+scope+"/1/2", nil)
		rec = httptest.NewRecorder()

		handler.handleChannelOverrides(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Scope %s DELETE: expected status %d, got %d", scope, http.StatusForbidden, rec.Code)
		}
	}
}
