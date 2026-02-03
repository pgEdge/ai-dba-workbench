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
	"github.com/pgedge/ai-workbench/server/internal/database"
)

func TestNewBlackoutHandler(t *testing.T) {
	handler := NewBlackoutHandler(nil, nil, nil)
	if handler == nil {
		t.Fatal("NewBlackoutHandler returned nil")
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

func TestBlackoutHandler_HandleNotConfigured(t *testing.T) {
	handler := NewBlackoutHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/blackouts", nil)
	rec := httptest.NewRecorder()

	handler.handleNotConfigured(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", contentType)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	expectedError := "Blackout management is not available. The datastore is not configured."
	if response.Error != expectedError {
		t.Errorf("Expected error %q, got %q", expectedError, response.Error)
	}
}

func TestBlackoutHandler_HandleBlackouts_MethodNotAllowed(t *testing.T) {
	handler := NewBlackoutHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/blackouts", nil)
	rec := httptest.NewRecorder()

	handler.handleBlackouts(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	allowed := rec.Header().Get("Allow")
	if allowed != "GET, POST" {
		t.Errorf("Expected Allow header 'GET, POST', got %q", allowed)
	}
}

func TestBlackoutHandler_HandleBlackoutSchedules_MethodNotAllowed(t *testing.T) {
	handler := NewBlackoutHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/blackout-schedules", nil)
	rec := httptest.NewRecorder()

	handler.handleBlackoutSchedules(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	allowed := rec.Header().Get("Allow")
	if allowed != "GET, POST" {
		t.Errorf("Expected Allow header 'GET, POST', got %q", allowed)
	}
}

func TestBlackoutHandler_HandleBlackoutSubpath_InvalidID(t *testing.T) {
	handler := NewBlackoutHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/blackouts/abc", nil)
	rec := httptest.NewRecorder()

	handler.handleBlackoutSubpath(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "Invalid blackout ID" {
		t.Errorf("Expected error 'Invalid blackout ID', got %q", response.Error)
	}
}

func TestBlackoutHandler_HandleBlackoutSubpath_MethodNotAllowed(t *testing.T) {
	handler := NewBlackoutHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/blackouts/1", nil)
	rec := httptest.NewRecorder()

	handler.handleBlackoutSubpath(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	allowed := rec.Header().Get("Allow")
	if allowed != "GET, PUT, DELETE" {
		t.Errorf("Expected Allow header 'GET, PUT, DELETE', got %q", allowed)
	}
}

func TestBlackoutHandler_HandleBlackoutSubpath_StopRequiresPost(t *testing.T) {
	handler := NewBlackoutHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/blackouts/1/stop", nil)
	rec := httptest.NewRecorder()

	handler.handleBlackoutSubpath(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestBlackoutHandler_HandleBlackoutScheduleSubpath_InvalidID(t *testing.T) {
	handler := NewBlackoutHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/blackout-schedules/xyz", nil)
	rec := httptest.NewRecorder()

	handler.handleBlackoutScheduleSubpath(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestBlackoutHandler_HandleBlackoutScheduleSubpath_MethodNotAllowed(t *testing.T) {
	handler := NewBlackoutHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/blackout-schedules/1", nil)
	rec := httptest.NewRecorder()

	handler.handleBlackoutScheduleSubpath(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	allowed := rec.Header().Get("Allow")
	if allowed != "GET, PUT, DELETE" {
		t.Errorf("Expected Allow header 'GET, PUT, DELETE', got %q", allowed)
	}
}

func TestValidateBlackoutScope(t *testing.T) {
	tests := []struct {
		name         string
		scope        string
		groupID      *int
		clusterID    *int
		connectionID *int
		wantErr      bool
		errContains  string
	}{
		{
			name:  "valid estate scope",
			scope: "estate",
		},
		{
			name:         "estate scope with connection_id",
			scope:        "estate",
			connectionID: intPtr(1),
			wantErr:      true,
			errContains:  "must not specify",
		},
		{
			name:    "valid group scope",
			scope:   "group",
			groupID: intPtr(1),
		},
		{
			name:        "group scope without group_id",
			scope:       "group",
			wantErr:     true,
			errContains: "requires group_id",
		},
		{
			name:      "valid cluster scope",
			scope:     "cluster",
			clusterID: intPtr(1),
		},
		{
			name:        "cluster scope without cluster_id",
			scope:       "cluster",
			wantErr:     true,
			errContains: "requires cluster_id",
		},
		{
			name:         "valid server scope",
			scope:        "server",
			connectionID: intPtr(1),
		},
		{
			name:        "server scope without connection_id",
			scope:       "server",
			wantErr:     true,
			errContains: "requires connection_id",
		},
		{
			name:        "invalid scope",
			scope:       "invalid",
			wantErr:     true,
			errContains: "invalid scope",
		},
		{
			name:        "group scope with cluster_id",
			scope:       "group",
			groupID:     intPtr(1),
			clusterID:   intPtr(2),
			wantErr:     true,
			errContains: "must not specify cluster_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBlackoutScope(tt.scope, tt.groupID, tt.clusterID, tt.connectionID)
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				} else if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("Expected error containing %q, got %q", tt.errContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestBlackoutCreateRequest_JSON(t *testing.T) {
	connID := 1
	req := BlackoutCreateRequest{
		Scope:        "server",
		ConnectionID: &connID,
		Reason:       "Maintenance window",
		StartTime:    "2026-01-31T00:00:00Z",
		EndTime:      "2026-01-31T02:00:00Z",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var decoded BlackoutCreateRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if decoded.Scope != "server" {
		t.Errorf("Expected scope 'server', got %q", decoded.Scope)
	}
	if decoded.ConnectionID == nil || *decoded.ConnectionID != 1 {
		t.Error("Expected connection_id 1")
	}
	if decoded.Reason != "Maintenance window" {
		t.Errorf("Expected reason 'Maintenance window', got %q", decoded.Reason)
	}
}

func TestBlackoutScheduleRequest_JSON(t *testing.T) {
	enabled := true
	req := BlackoutScheduleRequest{
		Scope:           "estate",
		Name:            "Weekly maintenance",
		CronExpression:  "0 2 * * 0",
		DurationMinutes: 120,
		Timezone:        "America/New_York",
		Reason:          "Weekly maintenance window",
		Enabled:         &enabled,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var decoded BlackoutScheduleRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if decoded.Name != "Weekly maintenance" {
		t.Errorf("Expected name 'Weekly maintenance', got %q", decoded.Name)
	}
	if decoded.DurationMinutes != 120 {
		t.Errorf("Expected duration_minutes 120, got %d", decoded.DurationMinutes)
	}
}

func TestBlackoutHandler_RegisterRoutes_NotConfigured(t *testing.T) {
	handler := NewBlackoutHandler(nil, nil, nil)
	mux := http.NewServeMux()
	noopWrapper := func(h http.HandlerFunc) http.HandlerFunc { return h }

	handler.RegisterRoutes(mux, noopWrapper)

	// Test that all routes return 503
	paths := []string{
		"/api/v1/blackouts",
		"/api/v1/blackouts/1",
		"/api/v1/blackout-schedules",
		"/api/v1/blackout-schedules/1",
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

func TestBlackoutHandler_CreateBlackout_ValidationErrors(t *testing.T) {
	// Create handler with auth-disabled RBAC checker so permission checks pass
	rbac := auth.NewRBACChecker(nil, false)
	handler := NewBlackoutHandler(nil, nil, rbac)

	tests := []struct {
		name        string
		body        interface{}
		wantStatus  int
		errContains string
	}{
		{
			name: "invalid start_time",
			body: BlackoutCreateRequest{
				Scope:     "estate",
				Reason:    "test",
				StartTime: "not-a-time",
				EndTime:   "2026-01-31T02:00:00Z",
			},
			wantStatus:  http.StatusBadRequest,
			errContains: "start_time",
		},
		{
			name: "end_time before start_time",
			body: BlackoutCreateRequest{
				Scope:     "estate",
				Reason:    "test",
				StartTime: "2026-01-31T02:00:00Z",
				EndTime:   "2026-01-31T00:00:00Z",
			},
			wantStatus:  http.StatusBadRequest,
			errContains: "end_time must be after start_time",
		},
		{
			name: "invalid scope",
			body: BlackoutCreateRequest{
				Scope:     "invalid",
				Reason:    "test",
				StartTime: "2026-01-31T00:00:00Z",
				EndTime:   "2026-01-31T02:00:00Z",
			},
			wantStatus:  http.StatusBadRequest,
			errContains: "invalid scope",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/blackouts", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			handler.createBlackout(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("Expected status %d, got %d", tt.wantStatus, rec.Code)
			}

			var response ErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&response); err == nil {
				if tt.errContains != "" && !contains(response.Error, tt.errContains) {
					t.Errorf("Expected error containing %q, got %q", tt.errContains, response.Error)
				}
			}
		})
	}
}

func TestBlackoutHandler_CreateSchedule_ValidationErrors(t *testing.T) {
	rbac := auth.NewRBACChecker(nil, false)
	handler := NewBlackoutHandler(nil, nil, rbac)

	tests := []struct {
		name        string
		body        interface{}
		wantStatus  int
		errContains string
	}{
		{
			name: "missing name",
			body: BlackoutScheduleRequest{
				Scope:           "estate",
				CronExpression:  "0 2 * * 0",
				DurationMinutes: 120,
				Reason:          "test",
			},
			wantStatus:  http.StatusBadRequest,
			errContains: "Name is required",
		},
		{
			name: "missing cron expression",
			body: BlackoutScheduleRequest{
				Scope:           "estate",
				Name:            "test",
				DurationMinutes: 120,
				Reason:          "test",
			},
			wantStatus:  http.StatusBadRequest,
			errContains: "Cron expression is required",
		},
		{
			name: "zero duration",
			body: BlackoutScheduleRequest{
				Scope:          "estate",
				Name:           "test",
				CronExpression: "0 2 * * 0",
				Reason:         "test",
			},
			wantStatus:  http.StatusBadRequest,
			errContains: "Duration minutes must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/blackout-schedules", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			handler.createBlackoutSchedule(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("Expected status %d, got %d", tt.wantStatus, rec.Code)
			}

			var response ErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&response); err == nil {
				if tt.errContains != "" && !contains(response.Error, tt.errContains) {
					t.Errorf("Expected error containing %q, got %q", tt.errContains, response.Error)
				}
			}
		})
	}
}

func TestBlackoutTypes(t *testing.T) {
	// Verify scope constants
	if string(database.BlackoutScopeEstate) != "estate" {
		t.Errorf("Expected BlackoutScopeEstate to be 'estate', got %q", database.BlackoutScopeEstate)
	}
	if string(database.BlackoutScopeGroup) != "group" {
		t.Errorf("Expected BlackoutScopeGroup to be 'group', got %q", database.BlackoutScopeGroup)
	}
	if string(database.BlackoutScopeCluster) != "cluster" {
		t.Errorf("Expected BlackoutScopeCluster to be 'cluster', got %q", database.BlackoutScopeCluster)
	}
	if string(database.BlackoutScopeServer) != "server" {
		t.Errorf("Expected BlackoutScopeServer to be 'server', got %q", database.BlackoutScopeServer)
	}

	// Verify valid scopes map
	for _, scope := range []string{"estate", "group", "cluster", "server"} {
		if !database.ValidBlackoutScopes[scope] {
			t.Errorf("Expected %q to be in ValidBlackoutScopes", scope)
		}
	}
	if database.ValidBlackoutScopes["invalid"] {
		t.Error("Expected 'invalid' to not be in ValidBlackoutScopes")
	}
}

func TestBlackoutHandler_HandleBlackoutSubpath_EmptyPath(t *testing.T) {
	handler := NewBlackoutHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/blackouts/", nil)
	rec := httptest.NewRecorder()

	handler.handleBlackoutSubpath(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
}

func TestBlackoutHandler_HandleBlackoutSubpath_UnknownSubpath(t *testing.T) {
	handler := NewBlackoutHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/blackouts/1/unknown", nil)
	rec := httptest.NewRecorder()

	handler.handleBlackoutSubpath(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
}

func TestBlackoutHandler_HandleBlackoutScheduleSubpath_EmptyPath(t *testing.T) {
	handler := NewBlackoutHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/blackout-schedules/", nil)
	rec := httptest.NewRecorder()

	handler.handleBlackoutScheduleSubpath(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
}

func TestBlackoutUpdateRequest_JSON(t *testing.T) {
	reason := "Extended maintenance"
	endTime := "2026-01-31T04:00:00Z"
	req := BlackoutUpdateRequest{
		Reason:  &reason,
		EndTime: &endTime,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var decoded BlackoutUpdateRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if decoded.Reason == nil || *decoded.Reason != reason {
		t.Error("Reason mismatch")
	}
	if decoded.EndTime == nil || *decoded.EndTime != endTime {
		t.Error("EndTime mismatch")
	}

	// Test JSON keys
	var rawJSON map[string]interface{}
	if err := json.Unmarshal(data, &rawJSON); err != nil {
		t.Fatalf("Failed to unmarshal to map: %v", err)
	}

	if _, ok := rawJSON["reason"]; !ok {
		t.Error("Expected 'reason' key in JSON")
	}
	if _, ok := rawJSON["end_time"]; !ok {
		t.Error("Expected 'end_time' key in JSON")
	}
}

func TestBlackoutHandler_CreateBlackout_InvalidEndTime(t *testing.T) {
	rbac := auth.NewRBACChecker(nil, false)
	handler := NewBlackoutHandler(nil, nil, rbac)

	body := BlackoutCreateRequest{
		Scope:     "estate",
		Reason:    "test",
		StartTime: "2026-01-31T00:00:00Z",
		EndTime:   "not-a-time",
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/blackouts",
		bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.createBlackout(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !contains(response.Error, "end_time") {
		t.Errorf("Expected error about end_time, got %q", response.Error)
	}
}

func TestBlackoutHandler_UpdateSchedule_ValidationErrors(t *testing.T) {
	rbac := auth.NewRBACChecker(nil, false)
	handler := NewBlackoutHandler(nil, nil, rbac)

	tests := []struct {
		name        string
		body        interface{}
		wantStatus  int
		errContains string
	}{
		{
			name: "missing name",
			body: BlackoutScheduleRequest{
				Scope:           "estate",
				CronExpression:  "0 2 * * 0",
				DurationMinutes: 120,
				Reason:          "test",
			},
			wantStatus:  http.StatusBadRequest,
			errContains: "Name is required",
		},
		{
			name: "missing cron expression",
			body: BlackoutScheduleRequest{
				Scope:           "estate",
				Name:            "test",
				DurationMinutes: 120,
				Reason:          "test",
			},
			wantStatus:  http.StatusBadRequest,
			errContains: "Cron expression is required",
		},
		{
			name: "negative duration",
			body: BlackoutScheduleRequest{
				Scope:           "estate",
				Name:            "test",
				CronExpression:  "0 2 * * 0",
				DurationMinutes: -1,
				Reason:          "test",
			},
			wantStatus:  http.StatusBadRequest,
			errContains: "Duration minutes must be positive",
		},
		{
			name: "invalid scope",
			body: BlackoutScheduleRequest{
				Scope:           "invalid",
				Name:            "test",
				CronExpression:  "0 2 * * 0",
				DurationMinutes: 120,
				Reason:          "test",
			},
			wantStatus:  http.StatusBadRequest,
			errContains: "invalid scope",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPut, "/api/v1/blackout-schedules/1",
				bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			handler.updateBlackoutSchedule(rec, req, 1)

			if rec.Code != tt.wantStatus {
				t.Errorf("Expected status %d, got %d", tt.wantStatus, rec.Code)
			}

			var response ErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&response); err == nil {
				if tt.errContains != "" && !contains(response.Error, tt.errContains) {
					t.Errorf("Expected error containing %q, got %q",
						tt.errContains, response.Error)
				}
			}
		})
	}
}

// Helper functions

func intPtr(v int) *int {
	return &v
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
