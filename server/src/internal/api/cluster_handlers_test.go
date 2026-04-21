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
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

func TestNewClusterHandler(t *testing.T) {
	// Test creation without datastore
	handler := NewClusterHandler(nil, nil, nil)
	if handler == nil {
		t.Fatal("NewClusterHandler returned nil")
	}
	if handler.datastore != nil {
		t.Error("Expected nil datastore")
	}
	if handler.authStore != nil {
		t.Error("Expected nil authStore")
	}
}

func TestClusterHandler_HandleNotConfigured(t *testing.T) {
	// Create a test request
	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters", nil)
	rec := httptest.NewRecorder()

	// Call the handler
	HandleNotConfigured("Cluster management")(rec, req)

	// Check status code
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}

	// Check content type
	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", contentType)
	}

	// Check response body
	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	expectedError := "Cluster management is not available. The datastore is not configured."
	if response.Error != expectedError {
		t.Errorf("Expected error %q, got %q", expectedError, response.Error)
	}
}

func TestClusterHandler_HandleClusters_MethodNotAllowed(t *testing.T) {
	handler := NewClusterHandler(nil, nil, nil)

	// Test DELETE method (should be rejected; only GET and POST are allowed)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/clusters", nil)
	rec := httptest.NewRecorder()

	// Note: We can only test the method check part here since datastore is nil
	// In production, routes are registered differently when datastore is nil
	handler.handleClusters(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	allowed := rec.Header().Get("Allow")
	if allowed != "GET, POST" {
		t.Errorf("Expected Allow header 'GET, POST', got %q", allowed)
	}
}

func TestClusterHandler_HandleClusterGroups_MethodNotAllowed(t *testing.T) {
	handler := NewClusterHandler(nil, nil, nil)

	// Test DELETE method (should be rejected)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/cluster-groups", nil)
	rec := httptest.NewRecorder()

	handler.handleClusterGroups(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	allowed := rec.Header().Get("Allow")
	if allowed != "GET, POST" {
		t.Errorf("Expected Allow header 'GET, POST', got %q", allowed)
	}
}

func TestClusterGroupRequest_JSON(t *testing.T) {
	// Test JSON serialization/deserialization
	description := "Test description"
	req := ClusterGroupRequest{
		Name:        "Production",
		Description: &description,
	}

	// Marshal to JSON
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Unmarshal back
	var decoded ClusterGroupRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if decoded.Name != req.Name {
		t.Errorf("Name mismatch: expected %q, got %q", req.Name, decoded.Name)
	}

	if decoded.Description == nil || *decoded.Description != *req.Description {
		t.Error("Description mismatch")
	}
}

func TestClusterRequest_JSON(t *testing.T) {
	// Test JSON serialization/deserialization
	description := "Test cluster"
	groupID := 1
	req := ClusterRequest{
		GroupID:     &groupID,
		Name:        "US East Cluster",
		Description: &description,
	}

	// Marshal to JSON
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Unmarshal back
	var decoded ClusterRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if decoded.GroupID == nil || *decoded.GroupID != *req.GroupID {
		t.Errorf("GroupID mismatch")
	}

	if decoded.Name != req.Name {
		t.Errorf("Name mismatch: expected %q, got %q", req.Name, decoded.Name)
	}
}

func TestAssignServerRequest_JSON(t *testing.T) {
	// Test JSON serialization/deserialization
	clusterID := 5
	role := "replica"
	req := AssignServerRequest{
		ClusterID: &clusterID,
		Role:      &role,
	}

	// Marshal to JSON
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Unmarshal back
	var decoded AssignServerRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if decoded.ClusterID == nil || *decoded.ClusterID != *req.ClusterID {
		t.Error("ClusterID mismatch")
	}

	if decoded.Role == nil || *decoded.Role != *req.Role {
		t.Error("Role mismatch")
	}
}

func TestClusterHandler_CreateClusterGroup_InvalidRequest(t *testing.T) {
	handler := NewClusterHandler(nil, nil, nil)

	// Test with invalid JSON
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster-groups", bytes.NewBufferString("invalid json"))
	rec := httptest.NewRecorder()

	handler.createClusterGroup(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "Invalid request body" {
		t.Errorf("Expected 'Invalid request body', got %q", response.Error)
	}
}

func TestClusterHandler_CreateClusterGroup_MissingName(t *testing.T) {
	handler := NewClusterHandler(nil, nil, nil)

	// Test with missing name
	body := `{"description": "Test group"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster-groups", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.createClusterGroup(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "Name is required" {
		t.Errorf("Expected 'Name is required', got %q", response.Error)
	}
}

func TestClusterHandler_UpdateCluster_MissingBothNameAndGroupID(t *testing.T) {
	handler := NewClusterHandler(nil, nil, nil)

	// Test with missing both name and group_id
	body := `{}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/clusters/1", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.updateCluster(rec, req, 1)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "At least name, group_id, description, or replication_type is required" {
		t.Errorf("Expected 'At least name, group_id, description, or replication_type is required', got %q", response.Error)
	}
}

func TestClusterHandler_HandleClusterSubpath_InvalidID(t *testing.T) {
	handler := NewClusterHandler(nil, nil, nil)

	// Test with invalid cluster ID
	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/invalid", nil)
	rec := httptest.NewRecorder()

	handler.handleClusterSubpath(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "Invalid cluster ID" {
		t.Errorf("Expected 'Invalid cluster ID', got %q", response.Error)
	}
}

func TestClusterHandler_HandleClusterGroupSubpath_InvalidID(t *testing.T) {
	handler := NewClusterHandler(nil, nil, nil)

	// Test with invalid group ID
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster-groups/invalid", nil)
	rec := httptest.NewRecorder()

	handler.handleClusterGroupSubpath(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "Invalid group ID" {
		t.Errorf("Expected 'Invalid group ID', got %q", response.Error)
	}
}

func TestClusterHandler_HandleClusterServers_MethodNotAllowed(t *testing.T) {
	handler := NewClusterHandler(nil, nil, nil)

	// Test DELETE method (should be rejected; only GET and POST are allowed)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/clusters/1/servers", nil)
	rec := httptest.NewRecorder()

	handler.handleClusterServers(rec, req, 1)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	allowed := rec.Header().Get("Allow")
	if allowed != "GET, POST" {
		t.Errorf("Expected Allow header 'GET, POST', got %q", allowed)
	}
}

func TestClusterHandler_HandleClusterSubpath_EmptyPath(t *testing.T) {
	handler := NewClusterHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/", nil)
	rec := httptest.NewRecorder()

	handler.handleClusterSubpath(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
}

func TestClusterHandler_HandleClusterGroupSubpath_EmptyPath(t *testing.T) {
	handler := NewClusterHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster-groups/", nil)
	rec := httptest.NewRecorder()

	handler.handleClusterGroupSubpath(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
}

func TestClusterHandler_HandleClusterSubpath_Servers_InvalidID(t *testing.T) {
	handler := NewClusterHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/abc/servers", nil)
	rec := httptest.NewRecorder()

	handler.handleClusterSubpath(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "Invalid cluster ID" {
		t.Errorf("Expected 'Invalid cluster ID', got %q", response.Error)
	}
}

func TestClusterHandler_HandleClusterSubpath_MethodNotAllowed(t *testing.T) {
	handler := NewClusterHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/clusters/1", nil)
	rec := httptest.NewRecorder()

	handler.handleClusterSubpath(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	allowed := rec.Header().Get("Allow")
	if allowed != "GET, PUT, DELETE" {
		t.Errorf("Expected Allow header 'GET, PUT, DELETE', got %q", allowed)
	}
}

func TestClusterHandler_HandleClusterGroupSubpath_MethodNotAllowed(t *testing.T) {
	handler := NewClusterHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/cluster-groups/1", nil)
	rec := httptest.NewRecorder()

	handler.handleClusterGroupSubpath(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	allowed := rec.Header().Get("Allow")
	if allowed != "GET, PUT, DELETE" {
		t.Errorf("Expected Allow header 'GET, PUT, DELETE', got %q", allowed)
	}
}

func TestClusterHandler_HandleClusterGroupSubpath_Clusters_InvalidID(t *testing.T) {
	handler := NewClusterHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster-groups/abc/clusters", nil)
	rec := httptest.NewRecorder()

	handler.handleClusterGroupSubpath(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "Invalid group ID" {
		t.Errorf("Expected 'Invalid group ID', got %q", response.Error)
	}
}

func TestClusterHandler_HandleGroupClusters_MethodNotAllowed(t *testing.T) {
	handler := NewClusterHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/cluster-groups/1/clusters", nil)
	rec := httptest.NewRecorder()

	handler.handleGroupClusters(rec, req, 1)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	allowed := rec.Header().Get("Allow")
	if allowed != "GET, POST" {
		t.Errorf("Expected Allow header 'GET, POST', got %q", allowed)
	}
}

func TestClusterHandler_CreateClusterInGroup_MissingName(t *testing.T) {
	handler := NewClusterHandler(nil, nil, nil)

	body := `{"description": "Test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster-groups/1/clusters",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.createClusterInGroup(rec, req, 1)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "Name is required" {
		t.Errorf("Expected 'Name is required', got %q", response.Error)
	}
}

func TestComputeAutoClusterKey(t *testing.T) {
	tests := []struct {
		name      string
		clusterID string
		expected  string
	}{
		{
			name:      "spock cluster",
			clusterID: "cluster-spock-abc123",
			expected:  "spock:abc123",
		},
		{
			name:      "server standalone",
			clusterID: "server-42",
			expected:  "standalone:42",
		},
		{
			name:      "unknown format",
			clusterID: "unknown-format",
			expected:  "",
		},
		{
			name:      "numeric ID",
			clusterID: "123",
			expected:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := computeAutoClusterKey(tt.clusterID)
			if result != tt.expected {
				t.Errorf("computeAutoClusterKey(%q) = %q, want %q",
					tt.clusterID, result, tt.expected)
			}
		})
	}
}

func TestAutoDetectedClusterRequest_JSON(t *testing.T) {
	groupID := 1
	req := AutoDetectedClusterRequest{
		Name:           "My Cluster",
		AutoClusterKey: "spock:abc",
		GroupID:        &groupID,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var decoded AutoDetectedClusterRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if decoded.Name != req.Name {
		t.Errorf("Name = %q, want %q", decoded.Name, req.Name)
	}
	if decoded.AutoClusterKey != req.AutoClusterKey {
		t.Errorf("AutoClusterKey = %q, want %q", decoded.AutoClusterKey, req.AutoClusterKey)
	}
	if decoded.GroupID == nil || *decoded.GroupID != *req.GroupID {
		t.Error("GroupID mismatch")
	}
}

func TestClusterHandler_HandleClusterRelationships_MethodNotAllowed(t *testing.T) {
	handler := NewClusterHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/clusters/1/relationships", nil)
	rec := httptest.NewRecorder()

	handler.handleClusterRelationships(rec, req, 1)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	allowed := rec.Header().Get("Allow")
	if allowed != "GET" {
		t.Errorf("Expected Allow header 'GET', got %q", allowed)
	}
}

func TestClusterHandler_HandleConnectionRelationships_MethodNotAllowed(t *testing.T) {
	handler := NewClusterHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/1/connections/2/relationships", nil)
	rec := httptest.NewRecorder()

	handler.handleConnectionRelationships(rec, req, 1, 2)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	allowed := rec.Header().Get("Allow")
	if allowed != "PUT, DELETE" {
		t.Errorf("Expected Allow header 'PUT, DELETE', got %q", allowed)
	}
}

func TestClusterHandler_HandleDeleteRelationship_MethodNotAllowed(t *testing.T) {
	handler := NewClusterHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/1/relationships/5", nil)
	rec := httptest.NewRecorder()

	handler.handleDeleteRelationship(rec, req, 1, 5)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	allowed := rec.Header().Get("Allow")
	if allowed != "DELETE" {
		t.Errorf("Expected Allow header 'DELETE', got %q", allowed)
	}
}

func TestClusterHandler_SetRelationships_InvalidJSON(t *testing.T) {
	handler := NewClusterHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/clusters/1/connections/2/relationships",
		bytes.NewBufferString("invalid json"))
	rec := httptest.NewRecorder()

	handler.setConnectionRelationships(rec, req, 1, 2)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestClusterHandler_HandleClusterSubpath_Relationships_InvalidClusterID(t *testing.T) {
	handler := NewClusterHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/abc/relationships", nil)
	rec := httptest.NewRecorder()

	handler.handleClusterSubpath(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "Invalid cluster ID" {
		t.Errorf("Expected 'Invalid cluster ID', got %q", response.Error)
	}
}

func TestClusterHandler_HandleClusterSubpath_DeleteRelationship_InvalidRelID(t *testing.T) {
	handler := NewClusterHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/clusters/1/relationships/abc", nil)
	rec := httptest.NewRecorder()

	handler.handleClusterSubpath(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "Invalid relationship ID" {
		t.Errorf("Expected 'Invalid relationship ID', got %q", response.Error)
	}
}

func TestClusterHandler_HandleClusterSubpath_ConnectionRelationships_InvalidConnID(t *testing.T) {
	handler := NewClusterHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/clusters/1/connections/abc/relationships", nil)
	rec := httptest.NewRecorder()

	handler.handleClusterSubpath(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "Invalid connection ID" {
		t.Errorf("Expected 'Invalid connection ID', got %q", response.Error)
	}
}

func TestSetRelationshipsRequest_JSON(t *testing.T) {
	body := `{"relationships":[{"target_connection_id":3,"relationship_type":"streams_from"}]}`

	var req SetRelationshipsRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if len(req.Relationships) != 1 {
		t.Fatalf("Expected 1 relationship, got %d", len(req.Relationships))
	}

	if req.Relationships[0].TargetConnectionID != 3 {
		t.Errorf("Expected target_connection_id=3, got %d", req.Relationships[0].TargetConnectionID)
	}

	if req.Relationships[0].RelationshipType != "streams_from" {
		t.Errorf("Expected relationship_type='streams_from', got %q", req.Relationships[0].RelationshipType)
	}
}

func TestValidRelationshipTypes(t *testing.T) {
	validTypes := []string{"streams_from", "subscribes_to", "replicates_with"}
	for _, rt := range validTypes {
		if !validRelationshipTypes[rt] {
			t.Errorf("Expected %q to be a valid relationship type", rt)
		}
	}

	invalidTypes := []string{"invalid", "primary", "replica", ""}
	for _, rt := range invalidTypes {
		if validRelationshipTypes[rt] {
			t.Errorf("Expected %q to be an invalid relationship type", rt)
		}
	}
}

// =============================================================================
// PUT /api/v1/cluster-groups/{id} tests
//
// Regression coverage for GitHub issue #58. Users saw "Invalid group ID
// format" errors when saving changes to an existing cluster group. The bug
// itself was frontend-only (PR #59) but these tests exercise the backend
// update endpoint to ensure it stays correct: path parsing, auth gating,
// body validation, method dispatch, and the auto-detected-group branch.
// =============================================================================

// TestClusterHandler_UpdateClusterGroup_InvalidIDFormat confirms a non-numeric,
// non-auto group id in the path returns 400 with the "Invalid group ID"
// message — matching the user-visible error that prompted issue #58.
func TestClusterHandler_UpdateClusterGroup_InvalidIDFormat(t *testing.T) {
	handler := NewClusterHandler(nil, nil, nil)

	body := `{"name": "Renamed"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/cluster-groups/not-a-number",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.handleClusterGroupSubpath(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusBadRequest, rec.Code, rec.Body.String())
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "Invalid group ID" {
		t.Errorf("Expected 'Invalid group ID', got %q", response.Error)
	}
}

// TestClusterHandler_UpdateClusterGroup_NoAuth confirms an unauthenticated
// PUT returns 401, not 400 or 500, and does not reach the datastore.
func TestClusterHandler_UpdateClusterGroup_NoAuth(t *testing.T) {
	// A real auth store is required so ValidateSessionToken runs; we pass
	// nil datastore because the handler short-circuits before using it
	// when authentication fails.
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	checker := auth.NewRBACChecker(store)
	handler := NewClusterHandler(nil, store, checker)

	body, _ := json.Marshal(ClusterGroupRequest{Name: "Renamed"})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/cluster-groups/42",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// No Authorization header
	rec := httptest.NewRecorder()

	handler.handleClusterGroupSubpath(rec, req)

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

// TestClusterHandler_UpdateClusterGroup_BadToken confirms an unknown bearer
// token is rejected with 401.
func TestClusterHandler_UpdateClusterGroup_BadToken(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	checker := auth.NewRBACChecker(store)
	handler := NewClusterHandler(nil, store, checker)

	body, _ := json.Marshal(ClusterGroupRequest{Name: "Renamed"})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/cluster-groups/42",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer not-a-real-session-token")
	rec := httptest.NewRecorder()

	handler.handleClusterGroupSubpath(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusUnauthorized, rec.Code, rec.Body.String())
	}
}

// TestClusterHandler_UpdateClusterGroup_MethodNotAllowed confirms methods
// other than GET/PUT/DELETE return 405 with the correct Allow header.
func TestClusterHandler_UpdateClusterGroup_MethodNotAllowed(t *testing.T) {
	handler := NewClusterHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/cluster-groups/1",
		bytes.NewBufferString(`{"name":"x"}`))
	rec := httptest.NewRecorder()

	handler.handleClusterGroupSubpath(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	allowed := rec.Header().Get("Allow")
	if allowed != "GET, PUT, DELETE" {
		t.Errorf("Expected Allow header 'GET, PUT, DELETE', got %q", allowed)
	}
}

// TestClusterHandler_UpdateAutoDetectedGroup_MethodNotAllowed confirms that
// the auto-detected branch only accepts PUT (GET/DELETE not supported).
func TestClusterHandler_UpdateAutoDetectedGroup_MethodNotAllowed(t *testing.T) {
	handler := NewClusterHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster-groups/group-auto", nil)
	rec := httptest.NewRecorder()

	handler.handleClusterGroupSubpath(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	allowed := rec.Header().Get("Allow")
	if allowed != "PUT" {
		t.Errorf("Expected Allow header 'PUT', got %q", allowed)
	}
}

// TestClusterHandler_UpdateAutoDetectedGroup_PermissionDenied confirms the
// auto-detected branch requires manage_connections permission and returns
// 403 for users without it.
func TestClusterHandler_UpdateAutoDetectedGroup_PermissionDenied(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	checker := auth.NewRBACChecker(store)
	handler := NewClusterHandler(nil, store, checker)

	// Create an unprivileged user.
	if err := store.CreateUser("noperm", "Password1", "", "", ""); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	userID, err := store.GetUserID("noperm")
	if err != nil {
		t.Fatalf("Failed to get user id: %v", err)
	}

	body, _ := json.Marshal(ClusterGroupRequest{Name: "My Renamed Auto Group"})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/cluster-groups/group-auto",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.handleClusterGroupSubpath(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusForbidden, rec.Code, rec.Body.String())
	}
}

// TestClusterHandler_UpdateAutoDetectedGroup_MissingName confirms the
// auto-detected branch validates that Name is non-empty, even when the
// caller has the required permission.
func TestClusterHandler_UpdateAutoDetectedGroup_MissingName(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	checker := auth.NewRBACChecker(store)
	handler := NewClusterHandler(nil, store, checker)

	// Superuser bypass grants the permission check; we still expect 400
	// because the request body is missing Name.
	body := `{}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/cluster-groups/group-auto",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.handleClusterGroupSubpath(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusBadRequest, rec.Code, rec.Body.String())
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if response.Error != "Name is required" {
		t.Errorf("Expected 'Name is required', got %q", response.Error)
	}
}

// TestClusterHandler_UpdateAutoDetectedGroup_InvalidJSON confirms malformed
// bodies return 400 from the auto-detected branch.
func TestClusterHandler_UpdateAutoDetectedGroup_InvalidJSON(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	checker := auth.NewRBACChecker(store)
	handler := NewClusterHandler(nil, store, checker)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/cluster-groups/group-auto",
		bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	req = withSuperuser(req)
	rec := httptest.NewRecorder()

	handler.handleClusterGroupSubpath(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

// =============================================================================
// PUT /api/v1/cluster-groups/{id} — end-to-end datastore integration
//
// These tests exercise the full create → update → verify flow against a
// real PostgreSQL instance. They are skipped when TEST_AI_WORKBENCH_SERVER
// is not set, matching the convention documented in
// .claude/golang-expert/testing-strategy.md and used by
// server/src/internal/resources/integration_test.go.
// =============================================================================

// clusterGroupsTestSchema creates the minimum set of tables the datastore's
// cluster group operations touch. It is a trimmed copy of the collector
// migration and is intentionally limited to the columns and constraints
// referenced by GetClusterGroup, CreateClusterGroup, UpdateClusterGroup,
// DeleteClusterGroup, GetDefaultGroupID, and UpsertGroupByAutoKey.
const clusterGroupsTestSchema = `
DROP TABLE IF EXISTS cluster_groups CASCADE;
CREATE TABLE cluster_groups (
    id SERIAL PRIMARY KEY,
    owner_username VARCHAR(255),
    owner_token VARCHAR(255),
    is_shared BOOLEAN NOT NULL DEFAULT TRUE,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    auto_group_key VARCHAR(255) UNIQUE,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT cluster_groups_name_unique UNIQUE (name)
);
CREATE UNIQUE INDEX idx_cluster_groups_is_default
    ON cluster_groups (is_default) WHERE is_default = TRUE;
`

// newTestDatastore returns a *database.Datastore wired to the Postgres
// instance named by TEST_AI_WORKBENCH_SERVER. The test is skipped if the
// env var is missing or the connection cannot be established. The caller
// receives a cleanup function that drops the test schema and closes the
// pool.
func newTestDatastore(t *testing.T) (*database.Datastore, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping datastore integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("Test database ping failed: %v", err)
	}

	if _, err := pool.Exec(ctx, clusterGroupsTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create test schema: %v", err)
	}

	ds := database.NewTestDatastore(pool)

	cleanup := func() {
		// Best-effort teardown of the test schema.
		_, _ = pool.Exec(context.Background(), "DROP TABLE IF EXISTS cluster_groups CASCADE")
		pool.Close()
	}

	return ds, pool, cleanup
}

// setupGroupUpdateHandler builds a ClusterHandler backed by the given
// datastore, creates a user with manage_connections permission, and
// authenticates that user so the caller has a real session token to put
// in the Authorization header.
//
// updateClusterGroup uses two different auth surfaces: getUserInfoCompat
// validates a bearer token via the auth store, while rbacChecker.
// HasAdminPermission reads the user ID from the request context (the
// production middleware populates that context from the same token).
// Handler-level tests bypass the middleware, so callers must set BOTH
// the Authorization header (via withBearer) AND the user context (via
// withUser). The returned userID lets callers do the latter.
//
// Returns the handler, the auth store, the user's ID, the user's session
// token, and a cleanup function for the auth store.
func setupGroupUpdateHandler(t *testing.T, ds *database.Datastore) (*ClusterHandler, *auth.AuthStore, int64, string, func()) {
	t.Helper()

	_, store, storeCleanup := createTestRBACHandler(t)
	userID := setupUserWithPermission(t, store, "group_updater",
		auth.PermManageConnections)
	token, _, err := store.AuthenticateUser("group_updater", "Password1")
	if err != nil {
		storeCleanup()
		t.Fatalf("Failed to authenticate test user: %v", err)
	}
	checker := auth.NewRBACChecker(store)
	handler := NewClusterHandler(ds, store, checker)

	return handler, store, userID, token, storeCleanup
}

// withBearer sets an Authorization: Bearer header on the request.
func withBearer(req *http.Request, token string) *http.Request {
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

// TestClusterHandler_UpdateClusterGroup_Integration_HappyPath exercises the
// full create → PUT → verify flow for a numeric-ID cluster group. This is
// the primary regression test for GitHub issue #58.
func TestClusterHandler_UpdateClusterGroup_Integration_HappyPath(t *testing.T) {
	ds, pool, cleanupDS := newTestDatastore(t)
	defer cleanupDS()

	handler, _, userID, token, cleanupStore := setupGroupUpdateHandler(t, ds)
	defer cleanupStore()

	ctx := context.Background()
	originalDesc := "original description"
	created, err := ds.CreateClusterGroup(ctx, "Integration Original", &originalDesc)
	if err != nil {
		t.Fatalf("Failed to create cluster group: %v", err)
	}

	newDesc := "updated description"
	body, _ := json.Marshal(ClusterGroupRequest{
		Name:        "Integration Renamed",
		Description: &newDesc,
	})
	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/cluster-groups/"+strconv.Itoa(created.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withBearer(req, token)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.handleClusterGroupSubpath(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp database.ClusterGroup
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if resp.ID != created.ID {
		t.Errorf("Response ID = %d, want %d", resp.ID, created.ID)
	}
	if resp.Name != "Integration Renamed" {
		t.Errorf("Response Name = %q, want %q", resp.Name, "Integration Renamed")
	}
	if resp.Description == nil || *resp.Description != newDesc {
		t.Errorf("Response Description = %v, want %q", resp.Description, newDesc)
	}

	// Verify the change was actually persisted.
	var dbName string
	var dbDesc *string
	err = pool.QueryRow(ctx,
		"SELECT name, description FROM cluster_groups WHERE id = $1",
		created.ID).Scan(&dbName, &dbDesc)
	if err != nil {
		t.Fatalf("Failed to read updated row: %v", err)
	}
	if dbName != "Integration Renamed" {
		t.Errorf("Persisted name = %q, want %q", dbName, "Integration Renamed")
	}
	if dbDesc == nil || *dbDesc != newDesc {
		t.Errorf("Persisted description = %v, want %q", dbDesc, newDesc)
	}
}

// TestClusterHandler_UpdateClusterGroup_Integration_NameOnly confirms a PUT
// that only updates the name (no description) still succeeds and leaves
// the description column untouched.
func TestClusterHandler_UpdateClusterGroup_Integration_NameOnly(t *testing.T) {
	ds, pool, cleanupDS := newTestDatastore(t)
	defer cleanupDS()

	handler, _, userID, token, cleanupStore := setupGroupUpdateHandler(t, ds)
	defer cleanupStore()

	ctx := context.Background()
	origDesc := "keep me"
	created, err := ds.CreateClusterGroup(ctx, "Name Only Original", &origDesc)
	if err != nil {
		t.Fatalf("Failed to create cluster group: %v", err)
	}

	body := `{"name": "Name Only Renamed"}`
	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/cluster-groups/"+strconv.Itoa(created.ID), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withBearer(req, token)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.handleClusterGroupSubpath(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rec.Code, rec.Body.String())
	}

	// The UpdateClusterGroup SQL sets description to the value in the
	// request. With description omitted from the JSON body, the request
	// struct's Description pointer is nil, so the row's description
	// becomes NULL. Capture the current contract so any future change
	// is deliberate.
	var dbName string
	var dbDesc *string
	err = pool.QueryRow(ctx,
		"SELECT name, description FROM cluster_groups WHERE id = $1",
		created.ID).Scan(&dbName, &dbDesc)
	if err != nil {
		t.Fatalf("Failed to read updated row: %v", err)
	}
	if dbName != "Name Only Renamed" {
		t.Errorf("Persisted name = %q, want %q", dbName, "Name Only Renamed")
	}
	if dbDesc != nil {
		t.Errorf("Persisted description = %v, want nil (omitted fields "+
			"are cleared by current handler contract)", *dbDesc)
	}
}

// TestClusterHandler_UpdateClusterGroup_Integration_MissingName confirms
// the endpoint rejects requests with an empty Name even after the
// ownership/permission check passes.
func TestClusterHandler_UpdateClusterGroup_Integration_MissingName(t *testing.T) {
	ds, _, cleanupDS := newTestDatastore(t)
	defer cleanupDS()

	handler, _, userID, token, cleanupStore := setupGroupUpdateHandler(t, ds)
	defer cleanupStore()

	ctx := context.Background()
	created, err := ds.CreateClusterGroup(ctx, "Needs Name", nil)
	if err != nil {
		t.Fatalf("Failed to create cluster group: %v", err)
	}

	body := `{"description": "only a description"}`
	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/cluster-groups/"+strconv.Itoa(created.ID), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withBearer(req, token)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.handleClusterGroupSubpath(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusBadRequest, rec.Code, rec.Body.String())
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if response.Error != "Name is required" {
		t.Errorf("Expected 'Name is required', got %q", response.Error)
	}
}

// TestClusterHandler_UpdateClusterGroup_Integration_NotFound confirms that
// PUT against a non-existent numeric group ID returns 404. This is the
// legitimate "group does not exist" path, distinct from the "invalid id
// format" 400 path.
func TestClusterHandler_UpdateClusterGroup_Integration_NotFound(t *testing.T) {
	ds, _, cleanupDS := newTestDatastore(t)
	defer cleanupDS()

	handler, _, userID, token, cleanupStore := setupGroupUpdateHandler(t, ds)
	defer cleanupStore()

	body, _ := json.Marshal(ClusterGroupRequest{Name: "Ghost"})
	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/cluster-groups/999999", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withBearer(req, token)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.handleClusterGroupSubpath(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusNotFound, rec.Code, rec.Body.String())
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if response.Error != "Cluster group not found" {
		t.Errorf("Expected 'Cluster group not found', got %q", response.Error)
	}
}

// TestClusterHandler_UpdateClusterGroup_Integration_Forbidden confirms that
// an authenticated user with no manage_connections permission who is not
// the owner of the target group is rejected with 403. The group in this
// test is created with no owner, so the ownership branch also returns
// false, meaning the combined "not owner and not privileged" rule fires.
func TestClusterHandler_UpdateClusterGroup_Integration_Forbidden(t *testing.T) {
	ds, _, cleanupDS := newTestDatastore(t)
	defer cleanupDS()

	_, store, cleanupStore := createTestRBACHandler(t)
	defer cleanupStore()

	// A user with no admin permissions at all.
	if err := store.CreateUser("outsider", "Password1", "", "", ""); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	userID, err := store.GetUserID("outsider")
	if err != nil {
		t.Fatalf("Failed to get outsider user id: %v", err)
	}
	token, _, err := store.AuthenticateUser("outsider", "Password1")
	if err != nil {
		t.Fatalf("Failed to authenticate outsider: %v", err)
	}

	checker := auth.NewRBACChecker(store)
	handler := NewClusterHandler(ds, store, checker)

	ctx := context.Background()
	created, err := ds.CreateClusterGroup(ctx, "Forbidden Target", nil)
	if err != nil {
		t.Fatalf("Failed to create cluster group: %v", err)
	}

	body, _ := json.Marshal(ClusterGroupRequest{Name: "Attempt"})
	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/cluster-groups/"+strconv.Itoa(created.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withBearer(req, token)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.handleClusterGroupSubpath(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusForbidden, rec.Code, rec.Body.String())
	}
}

// TestClusterHandler_UpdateAutoDetectedGroup_Integration_HappyPath exercises
// the auto-detected branch (PUT /api/v1/cluster-groups/group-auto) end to
// end: upsert the row, rename it via the API, and verify the persisted
// name.
func TestClusterHandler_UpdateAutoDetectedGroup_Integration_HappyPath(t *testing.T) {
	ds, pool, cleanupDS := newTestDatastore(t)
	defer cleanupDS()

	_, store, cleanupStore := createTestRBACHandler(t)
	defer cleanupStore()
	checker := auth.NewRBACChecker(store)
	handler := NewClusterHandler(ds, store, checker)

	body, _ := json.Marshal(ClusterGroupRequest{Name: "Custom Auto Name"})
	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/cluster-groups/group-auto", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withSuperuser(req) // manage_connections is satisfied by superuser
	rec := httptest.NewRecorder()

	handler.handleClusterGroupSubpath(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp database.ClusterGroup
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if resp.Name != "Custom Auto Name" {
		t.Errorf("Response Name = %q, want %q", resp.Name, "Custom Auto Name")
	}

	// Verify the upsert landed with auto_group_key = "auto".
	var dbName, dbAutoKey string
	err := pool.QueryRow(context.Background(),
		"SELECT name, auto_group_key FROM cluster_groups WHERE auto_group_key = $1",
		"auto").Scan(&dbName, &dbAutoKey)
	if err != nil {
		t.Fatalf("Failed to read upserted row: %v", err)
	}
	if dbName != "Custom Auto Name" {
		t.Errorf("Persisted name = %q, want %q", dbName, "Custom Auto Name")
	}
	if dbAutoKey != "auto" {
		t.Errorf("Persisted auto_group_key = %q, want %q", dbAutoKey, "auto")
	}

	// A second PUT with a different name should update the existing row
	// (upsert), not create a second row.
	body2, _ := json.Marshal(ClusterGroupRequest{Name: "Custom Auto Name 2"})
	req2 := httptest.NewRequest(http.MethodPut,
		"/api/v1/cluster-groups/group-auto", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	req2 = withSuperuser(req2)
	rec2 := httptest.NewRecorder()

	handler.handleClusterGroupSubpath(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("Second PUT: expected status %d, got %d. Body: %s",
			http.StatusOK, rec2.Code, rec2.Body.String())
	}

	var count int
	err = pool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM cluster_groups WHERE auto_group_key = $1",
		"auto").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count auto rows: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected exactly 1 auto-keyed row after second PUT, got %d", count)
	}
}

// =============================================================================
// Topology visibility filter (regression coverage for issue #35)
// =============================================================================

// buildTestTopology constructs a two-group topology in which each server
// has a distinct connection ID. This gives the filter tests a realistic
// tree with nested children.
func buildTestTopology() []database.TopologyGroup {
	return []database.TopologyGroup{
		{
			ID:   "g1",
			Name: "Group A",
			Clusters: []database.TopologyCluster{
				{
					ID:   "c1",
					Name: "Cluster 1",
					Servers: []database.TopologyServerInfo{
						{ID: 1, Name: "a-primary", Children: []database.TopologyServerInfo{
							{ID: 2, Name: "a-replica"},
						}},
						{ID: 3, Name: "a-standalone"},
					},
				},
				{
					ID:   "c2",
					Name: "Cluster 2",
					Servers: []database.TopologyServerInfo{
						{ID: 4, Name: "b-primary"},
					},
				},
			},
		},
		{
			ID:   "g2",
			Name: "Group B",
			Clusters: []database.TopologyCluster{
				{
					ID:   "c3",
					Name: "Cluster 3",
					Servers: []database.TopologyServerInfo{
						{ID: 5, Name: "c-primary"},
					},
				},
			},
		},
	}
}

// topologyServerIDs returns all server IDs in a topology, including
// nested children, to make assertions concise.
func topologyServerIDs(groups []database.TopologyGroup) map[int]bool {
	ids := make(map[int]bool)
	var walk func([]database.TopologyServerInfo)
	walk = func(servers []database.TopologyServerInfo) {
		for _, s := range servers {
			ids[s.ID] = true
			if len(s.Children) > 0 {
				walk(s.Children)
			}
		}
	}
	for _, g := range groups {
		for _, c := range g.Clusters {
			walk(c.Servers)
		}
	}
	return ids
}

func TestFilterTopologyByVisibility_DropsHiddenServersClustersAndGroups(t *testing.T) {
	// Visible: servers 1, 2 only. This keeps cluster 1 (with server 1
	// and its child 2, dropping server 3), drops cluster 2 entirely
	// (server 4 hidden), and drops group B entirely (server 5 hidden).
	visible := []int{1, 2}

	filtered := filterTopologyByVisibility(buildTestTopology(), visible)

	if len(filtered) != 1 {
		t.Fatalf("Expected 1 group after filter, got %d", len(filtered))
	}
	if filtered[0].ID != "g1" {
		t.Errorf("Expected group g1, got %q", filtered[0].ID)
	}
	if len(filtered[0].Clusters) != 1 {
		t.Fatalf("Expected 1 cluster in g1, got %d", len(filtered[0].Clusters))
	}
	if filtered[0].Clusters[0].ID != "c1" {
		t.Errorf("Expected cluster c1, got %q", filtered[0].Clusters[0].ID)
	}

	got := topologyServerIDs(filtered)
	want := map[int]bool{1: true, 2: true}
	if len(got) != len(want) {
		t.Errorf("Expected server set %v, got %v", want, got)
	}
	for id := range want {
		if !got[id] {
			t.Errorf("Expected server %d to be visible", id)
		}
	}
	if got[3] || got[4] || got[5] {
		t.Errorf("Unexpected hidden servers visible: %v", got)
	}
}

func TestFilterTopologyByVisibility_EmptyVisibleSet_ReturnsEmpty(t *testing.T) {
	filtered := filterTopologyByVisibility(buildTestTopology(), []int{})
	if len(filtered) != 0 {
		t.Errorf("Expected no groups when nothing is visible, got %d", len(filtered))
	}
}

func TestFilterTopologyByVisibility_DropsHiddenParent_WithVisibleChild(t *testing.T) {
	// Server 1 (hidden) has child 2 (visible). Dropping the parent must
	// also drop the child because the child is only meaningful under an
	// accessible parent.
	visible := []int{2, 3}

	filtered := filterTopologyByVisibility(buildTestTopology(), visible)
	got := topologyServerIDs(filtered)

	if got[1] || got[2] {
		t.Errorf("Child of hidden parent must be dropped; got %v", got)
	}
	if !got[3] {
		t.Error("Expected server 3 to remain visible")
	}
}

func TestFilterTopologyServers_PreservesNestedChildren(t *testing.T) {
	servers := []database.TopologyServerInfo{
		{ID: 10, Name: "root", Children: []database.TopologyServerInfo{
			{ID: 11, Name: "mid", Children: []database.TopologyServerInfo{
				{ID: 12, Name: "leaf-visible"},
				{ID: 13, Name: "leaf-hidden"},
			}},
		}},
	}
	visible := map[int]bool{10: true, 11: true, 12: true}

	got := filterTopologyServers(servers, visible)
	if len(got) != 1 || got[0].ID != 10 {
		t.Fatalf("Expected single root server, got %v", got)
	}
	if len(got[0].Children) != 1 || got[0].Children[0].ID != 11 {
		t.Fatalf("Expected single mid child, got %v", got[0].Children)
	}
	if len(got[0].Children[0].Children) != 1 || got[0].Children[0].Children[0].ID != 12 {
		t.Errorf("Expected only visible leaf 12 retained, got %v",
			got[0].Children[0].Children)
	}
}

// TestFilterTopologyServers_FiltersRelationshipsToHiddenPeers verifies
// that the handler-layer filter strips TopologyRelationship entries
// pointing at hidden peers so TargetServerID and TargetServerName never
// leak across the visibility boundary. Relationships targeting visible
// peers must survive.
func TestFilterTopologyServers_FiltersRelationshipsToHiddenPeers(t *testing.T) {
	servers := []database.TopologyServerInfo{
		{
			ID:           1,
			Name:         "visible-1",
			IsExpandable: true,
			Relationships: []database.TopologyRelationship{
				{
					TargetServerID:   2,
					TargetServerName: "visible-peer",
					RelationshipType: "spock_subscriber",
				},
				{
					TargetServerID:   99,
					TargetServerName: "hidden-peer",
					RelationshipType: "spock_subscriber",
				},
			},
		},
		{
			ID:   2,
			Name: "visible-2",
			Relationships: []database.TopologyRelationship{
				{
					TargetServerID:   77,
					TargetServerName: "another-hidden",
					RelationshipType: "spock_provider",
				},
			},
		},
	}
	visible := map[int]bool{1: true, 2: true}

	got := filterTopologyServers(servers, visible)

	if len(got) != 2 {
		t.Fatalf("Expected 2 servers, got %d", len(got))
	}
	if len(got[0].Relationships) != 1 {
		t.Fatalf("Expected 1 relationship on server 1, got %d",
			len(got[0].Relationships))
	}
	if got[0].Relationships[0].TargetServerID != 2 {
		t.Errorf("Expected relationship target 2, got %d",
			got[0].Relationships[0].TargetServerID)
	}
	if got[0].Relationships[0].TargetServerName != "visible-peer" {
		t.Errorf("Expected visible-peer target name, got %q",
			got[0].Relationships[0].TargetServerName)
	}
	if len(got[1].Relationships) != 0 {
		t.Errorf("Expected relationships to hidden peers to be dropped, got %d",
			len(got[1].Relationships))
	}
	// Server 1 has no visible children, so IsExpandable must reflect the
	// pruned state rather than the pre-populated flag.
	if got[0].IsExpandable {
		t.Errorf("Expected IsExpandable=false when no children remain")
	}
}

// TestFilterTopologyServers_IsExpandableReflectsVisibleChildren verifies
// that IsExpandable is recomputed from the pruned Children slice at the
// handler layer. A server whose only children are hidden must become
// non-expandable, and a server with a remaining visible child stays
// expandable.
func TestFilterTopologyServers_IsExpandableReflectsVisibleChildren(t *testing.T) {
	servers := []database.TopologyServerInfo{
		{
			ID:           1,
			Name:         "parent-all-hidden-children",
			IsExpandable: true,
			Children: []database.TopologyServerInfo{
				{ID: 98, Name: "hidden-child-a"},
				{ID: 99, Name: "hidden-child-b"},
			},
		},
		{
			ID:           2,
			Name:         "parent-mixed",
			IsExpandable: true,
			Children: []database.TopologyServerInfo{
				{ID: 3, Name: "visible-child"},
				{ID: 97, Name: "hidden-child"},
			},
		},
	}
	visible := map[int]bool{1: true, 2: true, 3: true}

	got := filterTopologyServers(servers, visible)

	if len(got) != 2 {
		t.Fatalf("Expected 2 top-level servers, got %d", len(got))
	}
	if got[0].IsExpandable {
		t.Errorf("Expected IsExpandable=false when all children hidden")
	}
	if !got[1].IsExpandable {
		t.Errorf("Expected IsExpandable=true when one child is visible")
	}
	if len(got[1].Children) != 1 || got[1].Children[0].ID != 3 {
		t.Errorf("Expected single visible child ID=3, got %+v", got[1].Children)
	}
}
