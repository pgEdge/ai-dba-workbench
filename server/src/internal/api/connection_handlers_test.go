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

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

func TestNewConnectionHandler(t *testing.T) {
	handler := NewConnectionHandler(nil, nil, nil)
	if handler == nil {
		t.Fatal("NewConnectionHandler returned nil")
	}
	if handler.datastore != nil {
		t.Error("Expected nil datastore")
	}
	if handler.authStore != nil {
		t.Error("Expected nil authStore")
	}
	if handler.hostValidator == nil {
		t.Error("Expected non-nil hostValidator (should use default)")
	}
}

func TestNewConnectionHandlerWithSecurity(t *testing.T) {
	handler := NewConnectionHandlerWithSecurity(nil, nil, nil, true,
		[]string{"allowed.example.com"},
		[]string{"blocked.example.com"})

	if handler == nil {
		t.Fatal("NewConnectionHandlerWithSecurity returned nil")
	}
	if handler.hostValidator == nil {
		t.Error("Expected non-nil hostValidator")
	}
	if !handler.hostValidator.AllowInternalNetworks {
		t.Error("Expected AllowInternalNetworks to be true")
	}
}

func TestConnectionHandler_HandleNotConfigured(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections", nil)
	rec := httptest.NewRecorder()

	HandleNotConfigured("Database connection management")(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	expected := "Database connection management is not available. The datastore is not configured."
	if response.Error != expected {
		t.Errorf("Expected error %q, got %q", expected, response.Error)
	}
}

func TestConnectionHandler_HandleConnections_MethodNotAllowed(t *testing.T) {
	handler := NewConnectionHandler(nil, nil, nil)

	tests := []struct {
		name   string
		method string
	}{
		{"DELETE not allowed", http.MethodDelete},
		{"PUT not allowed", http.MethodPut},
		{"PATCH not allowed", http.MethodPatch},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/v1/connections", nil)
			rec := httptest.NewRecorder()

			handler.handleConnections(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("Expected status %d, got %d",
					http.StatusMethodNotAllowed, rec.Code)
			}

			allowed := rec.Header().Get("Allow")
			if allowed != "GET, POST" {
				t.Errorf("Expected Allow header 'GET, POST', got %q", allowed)
			}
		})
	}
}

func TestConnectionHandler_HandleConnectionSubpath_InvalidID(t *testing.T) {
	handler := NewConnectionHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections/abc", nil)
	rec := httptest.NewRecorder()

	handler.handleConnectionSubpath(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "Invalid connection ID" {
		t.Errorf("Expected error 'Invalid connection ID', got %q", response.Error)
	}
}

func TestConnectionHandler_HandleConnectionSubpath_MethodNotAllowed(t *testing.T) {
	handler := NewConnectionHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/connections/1", nil)
	rec := httptest.NewRecorder()

	handler.handleConnectionSubpath(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	allowed := rec.Header().Get("Allow")
	if allowed != "GET, PUT, DELETE" {
		t.Errorf("Expected Allow header 'GET, PUT, DELETE', got %q", allowed)
	}
}

func TestConnectionHandler_HandleConnectionSubpath_DatabasesMethodNotAllowed(t *testing.T) {
	handler := NewConnectionHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/connections/1/databases", nil)
	rec := httptest.NewRecorder()

	handler.handleConnectionSubpath(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	allowed := rec.Header().Get("Allow")
	if allowed != "GET" {
		t.Errorf("Expected Allow header 'GET', got %q", allowed)
	}
}

func TestConnectionHandler_HandleCurrentConnection_MethodNotAllowed(t *testing.T) {
	handler := NewConnectionHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/connections/current", nil)
	req.Header.Set("Authorization", "Bearer testtoken")
	rec := httptest.NewRecorder()

	handler.handleCurrentConnection(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	allowed := rec.Header().Get("Allow")
	if allowed != "GET, POST, DELETE" {
		t.Errorf("Expected Allow header 'GET, POST, DELETE', got %q", allowed)
	}
}

func TestConnectionHandler_HandleCurrentConnection_MissingAuth(t *testing.T) {
	handler := NewConnectionHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections/current", nil)
	rec := httptest.NewRecorder()

	handler.handleCurrentConnection(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "Invalid or missing authentication token" {
		t.Errorf("Expected auth error, got %q", response.Error)
	}
}

func TestConnectionHandler_RegisterRoutes_NotConfigured(t *testing.T) {
	handler := NewConnectionHandler(nil, nil, nil)
	mux := http.NewServeMux()
	noopWrapper := func(h http.HandlerFunc) http.HandlerFunc { return h }

	handler.RegisterRoutes(mux, noopWrapper)

	paths := []string{
		"/api/v1/connections",
		"/api/v1/connections/1",
		"/api/v1/connections/current",
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

func TestConnectionHandler_CreateConnection_NoAuth(t *testing.T) {
	// Test that createConnection requires authentication
	rbac := auth.NewRBACChecker(nil)
	handler := NewConnectionHandler(nil, nil, rbac)

	body, _ := json.Marshal(ConnectionCreateRequest{
		Name:         "test",
		Host:         "example.com",
		Port:         5432,
		DatabaseName: "testdb",
		Username:     "user",
		Password:     "pass",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connections",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// No Authorization header
	rec := httptest.NewRecorder()

	handler.createConnection(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusUnauthorized, rec.Code, rec.Body.String())
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "Invalid or missing authentication token" {
		t.Errorf("Expected auth error, got %q", response.Error)
	}
}

func TestConnectionHandler_UpdateConnection_NoAuth(t *testing.T) {
	// Test that updateConnection requires authentication
	rbac := auth.NewRBACChecker(nil)
	handler := NewConnectionHandler(nil, nil, rbac)

	body, _ := json.Marshal(ConnectionFullUpdateRequest{})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/connections/1",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// No Authorization header
	rec := httptest.NewRecorder()

	handler.updateConnection(rec, req, 1)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "Invalid or missing authentication token" {
		t.Errorf("Expected auth error, got %q", response.Error)
	}
}

func TestConnectionHandler_SetCurrentConnection_InvalidConnectionID(t *testing.T) {
	handler := NewConnectionHandler(nil, nil, nil)

	body, _ := json.Marshal(CurrentConnectionRequest{ConnectionID: 0})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connections/current",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.setCurrentConnection(rec, req, "test-token-hash")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "connection_id is required" {
		t.Errorf("Expected error 'connection_id is required', got %q", response.Error)
	}
}

func TestConnectionCreateRequest_JSON(t *testing.T) {
	sslMode := "require"
	req := ConnectionCreateRequest{
		Name:         "Production DB",
		Host:         "db.example.com",
		Port:         5432,
		DatabaseName: "mydb",
		Username:     "admin",
		Password:     "secret",
		SSLMode:      &sslMode,
		IsShared:     true,
		IsMonitored:  true,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var decoded ConnectionCreateRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if decoded.Name != req.Name {
		t.Errorf("Name = %q, want %q", decoded.Name, req.Name)
	}
	if decoded.Host != req.Host {
		t.Errorf("Host = %q, want %q", decoded.Host, req.Host)
	}
	if decoded.Port != req.Port {
		t.Errorf("Port = %d, want %d", decoded.Port, req.Port)
	}
	if decoded.SSLMode == nil || *decoded.SSLMode != sslMode {
		t.Error("SSLMode mismatch")
	}
	if !decoded.IsShared {
		t.Error("Expected IsShared to be true")
	}
	if !decoded.IsMonitored {
		t.Error("Expected IsMonitored to be true")
	}
}

func TestConnectionFullUpdateRequest_JSON(t *testing.T) {
	name := "Updated Name"
	port := 5433
	req := ConnectionFullUpdateRequest{
		Name: &name,
		Port: &port,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var decoded ConnectionFullUpdateRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if decoded.Name == nil || *decoded.Name != name {
		t.Error("Name mismatch")
	}
	if decoded.Port == nil || *decoded.Port != port {
		t.Error("Port mismatch")
	}
}

func TestCurrentConnectionRequest_JSON(t *testing.T) {
	dbName := "testdb"
	req := CurrentConnectionRequest{
		ConnectionID: 42,
		DatabaseName: &dbName,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var decoded CurrentConnectionRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if decoded.ConnectionID != 42 {
		t.Errorf("ConnectionID = %d, want 42", decoded.ConnectionID)
	}
	if decoded.DatabaseName == nil || *decoded.DatabaseName != dbName {
		t.Error("DatabaseName mismatch")
	}
}

func TestCurrentConnectionResponse_JSON(t *testing.T) {
	dbName := "testdb"
	resp := CurrentConnectionResponse{
		ConnectionID: 42,
		DatabaseName: &dbName,
		Host:         "db.example.com",
		Port:         5432,
		Name:         "Production",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var decoded CurrentConnectionResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if decoded.ConnectionID != resp.ConnectionID {
		t.Errorf("ConnectionID = %d, want %d", decoded.ConnectionID, resp.ConnectionID)
	}
	if decoded.Host != resp.Host {
		t.Errorf("Host = %q, want %q", decoded.Host, resp.Host)
	}
	if decoded.Port != resp.Port {
		t.Errorf("Port = %d, want %d", decoded.Port, resp.Port)
	}
	if decoded.Name != resp.Name {
		t.Errorf("Name = %q, want %q", decoded.Name, resp.Name)
	}
}

func TestConnectionHandler_HandleSubpath_NotFound(t *testing.T) {
	handler := NewConnectionHandler(nil, nil, nil)

	// Test unknown subpath
	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections/1/unknown", nil)
	rec := httptest.NewRecorder()

	handler.handleConnectionSubpath(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
}

func TestConnectionHandler_HandleSubpath_EmptyPath(t *testing.T) {
	handler := NewConnectionHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections/", nil)
	rec := httptest.NewRecorder()

	handler.handleConnectionSubpath(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
}
