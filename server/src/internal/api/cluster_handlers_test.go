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
