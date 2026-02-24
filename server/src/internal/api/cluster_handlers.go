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
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// ClusterHandler handles REST API requests for cluster hierarchy management
type ClusterHandler struct {
	datastore   *database.Datastore
	authStore   *auth.AuthStore
	rbacChecker *auth.RBACChecker
}

// NewClusterHandler creates a new cluster handler
func NewClusterHandler(datastore *database.Datastore, authStore *auth.AuthStore, rbacChecker *auth.RBACChecker) *ClusterHandler {
	return &ClusterHandler{
		datastore:   datastore,
		authStore:   authStore,
		rbacChecker: rbacChecker,
	}
}

// ClusterGroupRequest is the request body for creating/updating cluster groups
type ClusterGroupRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

// ClusterRequest is the request body for creating/updating clusters
type ClusterRequest struct {
	GroupID     *int    `json:"group_id,omitempty"`
	Name        string  `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

// AssignServerRequest is the request body for assigning a server to a cluster
type AssignServerRequest struct {
	ClusterID *int    `json:"cluster_id"`
	Role      *string `json:"role,omitempty"`
}

// RegisterRoutes registers cluster management routes on the mux
func (h *ClusterHandler) RegisterRoutes(mux *http.ServeMux, authWrapper func(http.HandlerFunc) http.HandlerFunc) {
	if h.datastore == nil {
		// Datastore not configured, register handlers that return appropriate errors
		notConfigured := HandleNotConfigured("Cluster management")
		mux.HandleFunc("/api/v1/clusters", authWrapper(notConfigured))
		mux.HandleFunc("/api/v1/clusters/list", authWrapper(notConfigured))
		mux.HandleFunc("/api/v1/clusters/", authWrapper(notConfigured))
		mux.HandleFunc("/api/v1/cluster-groups", authWrapper(notConfigured))
		mux.HandleFunc("/api/v1/cluster-groups/", authWrapper(notConfigured))
		return
	}

	// Cluster hierarchy endpoint (returns full hierarchy for ClusterNavigator)
	mux.HandleFunc("/api/v1/clusters", authWrapper(h.handleClusters))

	// Cluster list endpoint (flat list for autocomplete/selection UIs)
	mux.HandleFunc("/api/v1/clusters/list", authWrapper(h.handleListClusters))

	// Cluster CRUD endpoints
	mux.HandleFunc("/api/v1/clusters/", authWrapper(h.handleClusterSubpath))

	// Cluster group endpoints
	mux.HandleFunc("/api/v1/cluster-groups", authWrapper(h.handleClusterGroups))
	mux.HandleFunc("/api/v1/cluster-groups/", authWrapper(h.handleClusterGroupSubpath))
}

// handleClusters handles GET/POST /api/v1/clusters
func (h *ClusterHandler) handleClusters(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.getClusterTopology(w, r)
	case http.MethodPost:
		h.handleCreateCluster(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// getClusterTopology returns the full cluster hierarchy for the ClusterNavigator
func (h *ClusterHandler) getClusterTopology(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Sync cluster_id assignments before reading topology
	if err := h.datastore.RefreshClusterAssignments(ctx); err != nil {
		log.Printf("[WARN] Failed to refresh cluster assignments: %v", err)
	}

	topology, err := h.datastore.GetClusterTopology(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to get cluster topology: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to get cluster topology")
		return
	}

	RespondJSON(w, http.StatusOK, topology)
}

// handleClusterSubpath handles /api/v1/clusters/{id}
func (h *ClusterHandler) handleClusterSubpath(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/clusters/")
	if path == "" {
		http.NotFound(w, r)
		return
	}

	// Check for sub-paths: /api/v1/clusters/{id}/servers, /api/v1/clusters/{id}/relationships, etc.
	parts := strings.Split(path, "/")
	if len(parts) == 2 && parts[1] == "servers" {
		clusterID, err := strconv.Atoi(parts[0])
		if err != nil {
			RespondError(w, http.StatusBadRequest, "Invalid cluster ID")
			return
		}
		h.handleClusterServers(w, r, clusterID)
		return
	}

	// GET /api/v1/clusters/{id}/relationships
	if len(parts) == 2 && parts[1] == "relationships" {
		clusterID, err := strconv.Atoi(parts[0])
		if err != nil {
			RespondError(w, http.StatusBadRequest, "Invalid cluster ID")
			return
		}
		h.handleClusterRelationships(w, r, clusterID)
		return
	}

	// DELETE /api/v1/clusters/{id}/relationships/{relationshipId}
	if len(parts) == 3 && parts[1] == "relationships" {
		clusterID, err := strconv.Atoi(parts[0])
		if err != nil {
			RespondError(w, http.StatusBadRequest, "Invalid cluster ID")
			return
		}
		relationshipID, err := strconv.Atoi(parts[2])
		if err != nil {
			RespondError(w, http.StatusBadRequest, "Invalid relationship ID")
			return
		}
		h.handleDeleteRelationship(w, r, clusterID, relationshipID)
		return
	}

	// PUT/DELETE /api/v1/clusters/{id}/connections/{connId}/relationships
	if len(parts) == 4 && parts[1] == "connections" && parts[3] == "relationships" {
		clusterID, err := strconv.Atoi(parts[0])
		if err != nil {
			RespondError(w, http.StatusBadRequest, "Invalid cluster ID")
			return
		}
		connID, err := strconv.Atoi(parts[2])
		if err != nil {
			RespondError(w, http.StatusBadRequest, "Invalid connection ID")
			return
		}
		h.handleConnectionRelationships(w, r, clusterID, connID)
		return
	}

	// Check if it's an auto-detected cluster ID (server-{id} or cluster-spock-{prefix})
	if strings.HasPrefix(parts[0], "server-") || strings.HasPrefix(parts[0], "cluster-spock-") {
		switch r.Method {
		case http.MethodPut:
			h.updateAutoDetectedCluster(w, r, parts[0])
		default:
			w.Header().Set("Allow", "PUT")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// Parse cluster ID - handle both numeric (123) and prefixed (cluster-123) formats
	var clusterID int
	var err error
	if strings.HasPrefix(parts[0], "cluster-") {
		// Database-backed cluster with cluster-{id} format
		idStr := strings.TrimPrefix(parts[0], "cluster-")
		clusterID, err = strconv.Atoi(idStr)
	} else {
		// Plain numeric ID
		clusterID, err = strconv.Atoi(parts[0])
	}
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid cluster ID")
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getCluster(w, r, clusterID)
	case http.MethodPut:
		h.updateCluster(w, r, clusterID)
	case http.MethodDelete:
		h.deleteCluster(w, r, clusterID)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleClusterGroups handles GET/POST /api/v1/cluster-groups
func (h *ClusterHandler) handleClusterGroups(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listClusterGroups(w, r)
	case http.MethodPost:
		h.createClusterGroup(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleClusterGroupSubpath handles /api/v1/cluster-groups/{id} and sub-paths
func (h *ClusterHandler) handleClusterGroupSubpath(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/cluster-groups/")
	if path == "" {
		http.NotFound(w, r)
		return
	}

	// Check for clusters sub-path: /api/v1/cluster-groups/{id}/clusters
	parts := strings.Split(path, "/")
	if len(parts) == 2 && parts[1] == "clusters" {
		groupID, err := strconv.Atoi(parts[0])
		if err != nil {
			RespondError(w, http.StatusBadRequest, "Invalid group ID")
			return
		}
		h.handleGroupClusters(w, r, groupID)
		return
	}

	// Check if it's an auto-detected group ID (group-auto)
	if strings.HasPrefix(parts[0], "group-auto") {
		switch r.Method {
		case http.MethodPut:
			h.updateAutoDetectedGroup(w, r, parts[0])
		default:
			w.Header().Set("Allow", "PUT")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// Parse group ID (numeric for database-backed groups)
	groupID, err := strconv.Atoi(parts[0])
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid group ID")
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getClusterGroup(w, r, groupID)
	case http.MethodPut:
		h.updateClusterGroup(w, r, groupID)
	case http.MethodDelete:
		h.deleteClusterGroup(w, r, groupID)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// Cluster Group CRUD operations

func (h *ClusterHandler) listClusterGroups(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	groups, err := h.datastore.GetClusterGroups(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to list cluster groups: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to list cluster groups")
		return
	}

	RespondJSON(w, http.StatusOK, groups)
}

func (h *ClusterHandler) getClusterGroup(w http.ResponseWriter, r *http.Request, id int) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	group, err := h.datastore.GetClusterGroup(ctx, id)
	if err != nil {
		log.Printf("[ERROR] Cluster group not found (id=%d): %v", id, err)
		RespondError(w, http.StatusNotFound, "Cluster group not found")
		return
	}

	RespondJSON(w, http.StatusOK, group)
}

func (h *ClusterHandler) createClusterGroup(w http.ResponseWriter, r *http.Request) {
	var req ClusterGroupRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	if req.Name == "" {
		RespondError(w, http.StatusBadRequest, "Name is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	group, err := h.datastore.CreateClusterGroup(ctx, req.Name, req.Description)
	if err != nil {
		log.Printf("[ERROR] Failed to create cluster group %s: %v", req.Name, err)
		RespondError(w, http.StatusInternalServerError, "Failed to create cluster group")
		return
	}

	RespondJSON(w, http.StatusCreated, group)
}

func (h *ClusterHandler) updateClusterGroup(w http.ResponseWriter, r *http.Request, id int) {
	// Check user permissions
	username, _, err := getUserInfoCompat(r, h.authStore)
	if err != nil {
		RespondError(w, http.StatusUnauthorized, "Invalid or missing authentication token")
		return
	}

	hasManageConns := h.rbacChecker.HasAdminPermission(r.Context(), auth.PermManageConnections)

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Get group to check ownership
	existingGroup, err := h.datastore.GetClusterGroup(ctx, id)
	if err != nil {
		log.Printf("[ERROR] Cluster group not found for update (id=%d): %v", id, err)
		RespondError(w, http.StatusNotFound, "Cluster group not found")
		return
	}

	// Permission check: manage_connections permission, or owner
	isOwner := existingGroup.OwnerUsername.Valid && existingGroup.OwnerUsername.String == username
	if !hasManageConns && !isOwner {
		RespondError(w, http.StatusForbidden,
			"You do not have permission to update this cluster group")
		return
	}

	var req ClusterGroupRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	if req.Name == "" {
		RespondError(w, http.StatusBadRequest, "Name is required")
		return
	}

	group, err := h.datastore.UpdateClusterGroup(ctx, id, req.Name, req.Description)
	if err != nil {
		log.Printf("[ERROR] Failed to update cluster group (id=%d): %v", id, err)
		RespondError(w, http.StatusInternalServerError, "Failed to update cluster group")
		return
	}

	RespondJSON(w, http.StatusOK, group)
}

func (h *ClusterHandler) deleteClusterGroup(w http.ResponseWriter, r *http.Request, id int) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Protect the default group from deletion
	defaultGroupID, err := h.datastore.GetDefaultGroupID(ctx)
	if err == nil && defaultGroupID == id {
		RespondError(w, http.StatusForbidden, "The default group cannot be deleted")
		return
	}

	err = h.datastore.DeleteClusterGroup(ctx, id)
	if err != nil {
		log.Printf("[ERROR] Failed to delete cluster group (id=%d): %v", id, err)
		if errors.Is(err, database.ErrClusterGroupNotFound) {
			RespondError(w, http.StatusNotFound, "Cluster group not found")
		} else {
			RespondError(w, http.StatusInternalServerError, "Failed to delete cluster group")
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Cluster CRUD operations

func (h *ClusterHandler) handleGroupClusters(w http.ResponseWriter, r *http.Request, groupID int) {
	switch r.Method {
	case http.MethodGet:
		h.listClustersInGroup(w, r, groupID)
	case http.MethodPost:
		h.createClusterInGroup(w, r, groupID)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *ClusterHandler) listClustersInGroup(w http.ResponseWriter, r *http.Request, groupID int) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	clusters, err := h.datastore.GetClustersInGroup(ctx, groupID)
	if err != nil {
		log.Printf("[ERROR] Failed to list clusters in group %d: %v", groupID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to list clusters")
		return
	}

	RespondJSON(w, http.StatusOK, clusters)
}

func (h *ClusterHandler) createClusterInGroup(w http.ResponseWriter, r *http.Request, groupID int) {
	var req ClusterRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	if req.Name == "" {
		RespondError(w, http.StatusBadRequest, "Name is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	cluster, err := h.datastore.CreateCluster(ctx, groupID, req.Name, req.Description)
	if err != nil {
		log.Printf("[ERROR] Failed to create cluster %s in group %d: %v", req.Name, groupID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to create cluster")
		return
	}

	RespondJSON(w, http.StatusCreated, cluster)
}

func (h *ClusterHandler) getCluster(w http.ResponseWriter, r *http.Request, id int) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	cluster, err := h.datastore.GetCluster(ctx, id)
	if err != nil {
		log.Printf("[ERROR] Cluster not found (id=%d): %v", id, err)
		RespondError(w, http.StatusNotFound, "Cluster not found")
		return
	}

	RespondJSON(w, http.StatusOK, cluster)
}

func (h *ClusterHandler) updateCluster(w http.ResponseWriter, r *http.Request, id int) {
	var req ClusterRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	// At least name or group_id must be provided for update
	if req.Name == "" && req.GroupID == nil {
		RespondError(w, http.StatusBadRequest, "At least name or group_id is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	cluster, err := h.datastore.UpdateClusterPartial(ctx, id, req.GroupID, req.Name, req.Description)
	if err != nil {
		log.Printf("[ERROR] Failed to update cluster (id=%d): %v", id, err)
		RespondError(w, http.StatusInternalServerError, "Failed to update cluster")
		return
	}

	RespondJSON(w, http.StatusOK, cluster)
}

func (h *ClusterHandler) deleteCluster(w http.ResponseWriter, r *http.Request, id int) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	err := h.datastore.DeleteCluster(ctx, id)
	if err != nil {
		log.Printf("[ERROR] Failed to delete cluster (id=%d): %v", id, err)
		if errors.Is(err, database.ErrClusterNotFound) {
			RespondError(w, http.StatusNotFound, "Cluster not found")
		} else {
			RespondError(w, http.StatusInternalServerError, "Failed to delete cluster")
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// AutoDetectedClusterRequest is the request body for updating auto-detected clusters
type AutoDetectedClusterRequest struct {
	Name           string  `json:"name,omitempty"`
	Description    *string `json:"description,omitempty"`      // Optional: update cluster description
	AutoClusterKey string  `json:"auto_cluster_key,omitempty"` // Optional: use if provided, else compute from ID
	GroupID        *int    `json:"group_id,omitempty"`         // Optional: move cluster to different group
}

// updateAutoDetectedCluster handles PUT requests for auto-detected clusters
// (binary replication, logical replication, or Spock clusters)
// Supports both renaming and moving clusters to different groups
func (h *ClusterHandler) updateAutoDetectedCluster(w http.ResponseWriter, r *http.Request, clusterID string) {
	// Check user permissions - requires manage_connections permission
	if !h.rbacChecker.HasAdminPermission(r.Context(), auth.PermManageConnections) {
		RespondError(w, http.StatusForbidden,
			"Permission denied: you do not have permission to modify auto-detected clusters")
		return
	}

	// Parse request body
	var req AutoDetectedClusterRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	// At least name, description, or group_id must be provided
	if req.Name == "" && req.Description == nil && req.GroupID == nil {
		RespondError(w, http.StatusBadRequest, "At least name, description, or group_id is required")
		return
	}

	// Use auto_cluster_key from request if provided, else compute from cluster ID
	autoKey := req.AutoClusterKey
	if autoKey == "" {
		autoKey = computeAutoClusterKey(clusterID)
	}
	if autoKey == "" {
		RespondError(w, http.StatusBadRequest, "auto_cluster_key is required for this cluster type")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Update cluster record (name and/or group_id)
	cluster, err := h.datastore.UpsertAutoDetectedCluster(ctx, autoKey, req.Name, req.Description, req.GroupID)
	if err != nil {
		log.Printf("[ERROR] Failed to update auto-detected cluster %s: %v", clusterID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to update auto-detected cluster")
		return
	}

	RespondJSON(w, http.StatusOK, cluster)
}

// computeAutoClusterKey computes the auto_cluster_key from a cluster ID
// For Spock clusters (cluster-spock-{prefix}), computes spock:{prefix}
// For standalone servers (server-{id}), computes standalone:{id}
// For binary/logical clusters, the frontend should provide the auto_cluster_key
func computeAutoClusterKey(clusterID string) string {
	if strings.HasPrefix(clusterID, "cluster-spock-") {
		prefix := strings.TrimPrefix(clusterID, "cluster-spock-")
		return "spock:" + prefix
	}
	// For server-{id} format without auto_cluster_key from frontend,
	// assume it's a standalone server (binary clusters will provide the key)
	if strings.HasPrefix(clusterID, "server-") {
		idStr := strings.TrimPrefix(clusterID, "server-")
		return "standalone:" + idStr
	}
	return ""
}

// updateAutoDetectedGroup handles PUT requests for auto-detected groups (e.g., group-auto)
func (h *ClusterHandler) updateAutoDetectedGroup(w http.ResponseWriter, r *http.Request, groupID string) {
	// Check user permissions - requires manage_connections permission
	if !h.rbacChecker.HasAdminPermission(r.Context(), auth.PermManageConnections) {
		RespondError(w, http.StatusForbidden,
			"Permission denied: you do not have permission to rename auto-detected groups")
		return
	}

	// Parse request body
	var req ClusterGroupRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	if req.Name == "" {
		RespondError(w, http.StatusBadRequest, "Name is required")
		return
	}

	// Compute auto_group_key from group ID
	// group-auto -> auto
	autoKey := strings.TrimPrefix(groupID, "group-")
	if autoKey == "" {
		RespondError(w, http.StatusBadRequest, "Invalid auto-detected group ID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Upsert group record with custom name
	group, err := h.datastore.UpsertGroupByAutoKey(ctx, autoKey, req.Name)
	if err != nil {
		log.Printf("[ERROR] Failed to update auto-detected group %s: %v", groupID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to update auto-detected group")
		return
	}

	RespondJSON(w, http.StatusOK, group)
}

// Server operations

func (h *ClusterHandler) handleClusterServers(w http.ResponseWriter, r *http.Request, clusterID int) {
	switch r.Method {
	case http.MethodGet:
		h.listServersInCluster(w, r, clusterID)
	default:
		w.Header().Set("Allow", "GET")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *ClusterHandler) listServersInCluster(w http.ResponseWriter, r *http.Request, clusterID int) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	servers, err := h.datastore.GetServersInCluster(ctx, clusterID)
	if err != nil {
		log.Printf("[ERROR] Failed to list servers in cluster %d: %v", clusterID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to list servers")
		return
	}

	RespondJSON(w, http.StatusOK, servers)
}

// ManualClusterRequest is the request body for creating a cluster directly
// (not through a cluster group sub-resource).
type ManualClusterRequest struct {
	Name            string `json:"name"`
	Description     string `json:"description"`
	ReplicationType string `json:"replication_type"`
	GroupID         *int   `json:"group_id,omitempty"`
}

// handleListClusters handles GET /api/v1/clusters/list
func (h *ClusterHandler) handleListClusters(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	clusters, err := h.datastore.ListClustersForAutocomplete(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to list clusters: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to list clusters")
		return
	}

	RespondJSON(w, http.StatusOK, clusters)
}

// handleCreateCluster handles POST /api/v1/clusters
func (h *ClusterHandler) handleCreateCluster(w http.ResponseWriter, r *http.Request) {
	var req ManualClusterRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	if req.Name == "" {
		RespondError(w, http.StatusBadRequest, "Name is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	clusterID, err := h.datastore.CreateManualCluster(ctx, req.Name, req.Description, req.ReplicationType, req.GroupID)
	if err != nil {
		log.Printf("[ERROR] Failed to create cluster %s: %v", req.Name, err)
		RespondError(w, http.StatusInternalServerError, "Failed to create cluster")
		return
	}

	RespondJSON(w, http.StatusCreated, map[string]interface{}{
		"id":   clusterID,
		"name": req.Name,
	})
}

// validRelationshipTypes defines the allowed relationship type values
var validRelationshipTypes = map[string]bool{
	"streams_from":    true,
	"subscribes_to":   true,
	"replicates_with": true,
}

// SetRelationshipsRequest is the request body for PUT
// /api/v1/clusters/{id}/connections/{connId}/relationships
type SetRelationshipsRequest struct {
	Relationships []database.RelationshipInput `json:"relationships"`
}

// handleClusterRelationships handles GET /api/v1/clusters/{id}/relationships
func (h *ClusterHandler) handleClusterRelationships(w http.ResponseWriter, r *http.Request, clusterID int) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	relationships, err := h.datastore.GetClusterRelationships(ctx, clusterID)
	if err != nil {
		log.Printf("[ERROR] Failed to get cluster relationships (cluster=%d): %v", clusterID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to get cluster relationships")
		return
	}

	if relationships == nil {
		relationships = []database.NodeRelationship{}
	}

	RespondJSON(w, http.StatusOK, relationships)
}

// handleConnectionRelationships handles PUT and DELETE for
// /api/v1/clusters/{id}/connections/{connId}/relationships
func (h *ClusterHandler) handleConnectionRelationships(w http.ResponseWriter, r *http.Request, clusterID int, connID int) {
	switch r.Method {
	case http.MethodPut:
		h.setConnectionRelationships(w, r, clusterID, connID)
	case http.MethodDelete:
		h.clearConnectionRelationships(w, r, clusterID, connID)
	default:
		w.Header().Set("Allow", "PUT, DELETE")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// setConnectionRelationships handles PUT
// /api/v1/clusters/{id}/connections/{connId}/relationships
func (h *ClusterHandler) setConnectionRelationships(w http.ResponseWriter, r *http.Request, clusterID int, connID int) {
	var req SetRelationshipsRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Validate that the source connection belongs to this cluster
	sourceInCluster, err := h.datastore.IsConnectionInCluster(ctx, clusterID, connID)
	if err != nil {
		log.Printf("[ERROR] Failed to check source cluster membership: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to validate cluster membership")
		return
	}
	if !sourceInCluster {
		RespondError(w, http.StatusBadRequest, "Source connection does not belong to this cluster")
		return
	}

	// Validate each relationship entry
	for _, rel := range req.Relationships {
		// Validate relationship type
		if !validRelationshipTypes[rel.RelationshipType] {
			RespondError(w, http.StatusBadRequest,
				"Invalid relationship type: "+rel.RelationshipType)
			return
		}

		// Reject self-relationships
		if rel.TargetConnectionID == connID {
			RespondError(w, http.StatusBadRequest, "Self-relationships are not allowed")
			return
		}

		// Validate that the target connection belongs to this cluster
		targetInCluster, err := h.datastore.IsConnectionInCluster(ctx, clusterID, rel.TargetConnectionID)
		if err != nil {
			log.Printf("[ERROR] Failed to check target cluster membership: %v", err)
			RespondError(w, http.StatusInternalServerError, "Failed to validate cluster membership")
			return
		}
		if !targetInCluster {
			RespondError(w, http.StatusBadRequest,
				"Target connection does not belong to this cluster")
			return
		}
	}

	// Set the relationships for the source connection
	if err := h.datastore.SetNodeRelationships(ctx, clusterID, connID, req.Relationships); err != nil {
		log.Printf("[ERROR] Failed to set relationships (cluster=%d, conn=%d): %v", clusterID, connID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to set relationships")
		return
	}

	// For replicates_with entries, auto-create reverse rows
	for _, rel := range req.Relationships {
		if rel.RelationshipType != "replicates_with" {
			continue
		}

		// Check if a reverse relationship already exists
		existing, err := h.datastore.GetClusterRelationships(ctx, clusterID)
		if err != nil {
			log.Printf("[WARN] Failed to check existing reverse relationships: %v", err)
			continue
		}

		reverseExists := false
		for _, ex := range existing {
			if ex.SourceConnectionID == rel.TargetConnectionID &&
				ex.TargetConnectionID == connID &&
				ex.RelationshipType == "replicates_with" {
				reverseExists = true
				break
			}
		}

		if !reverseExists {
			reverseRel := []database.RelationshipInput{
				{TargetConnectionID: connID, RelationshipType: "replicates_with"},
			}
			if err := h.datastore.SetNodeRelationships(ctx, clusterID, rel.TargetConnectionID, reverseRel); err != nil {
				log.Printf("[WARN] Failed to create reverse replicates_with relationship: %v", err)
			}
		}
	}

	// Return the updated relationships for this cluster
	relationships, err := h.datastore.GetClusterRelationships(ctx, clusterID)
	if err != nil {
		log.Printf("[ERROR] Failed to get updated relationships: %v", err)
		RespondError(w, http.StatusInternalServerError, "Relationships saved but failed to retrieve updated list")
		return
	}

	if relationships == nil {
		relationships = []database.NodeRelationship{}
	}

	RespondJSON(w, http.StatusOK, relationships)
}

// handleDeleteRelationship handles DELETE
// /api/v1/clusters/{id}/relationships/{relationshipId}
func (h *ClusterHandler) handleDeleteRelationship(w http.ResponseWriter, r *http.Request, clusterID int, relationshipID int) {
	_ = clusterID // clusterID is part of the URL for REST consistency

	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", "DELETE")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if err := h.datastore.RemoveNodeRelationship(ctx, relationshipID); err != nil {
		log.Printf("[ERROR] Failed to delete relationship (id=%d): %v", relationshipID, err)
		RespondError(w, http.StatusNotFound, "Relationship not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// clearConnectionRelationships handles DELETE
// /api/v1/clusters/{id}/connections/{connId}/relationships
func (h *ClusterHandler) clearConnectionRelationships(w http.ResponseWriter, r *http.Request, clusterID int, connID int) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if err := h.datastore.ClearNodeRelationships(ctx, clusterID, connID); err != nil {
		log.Printf("[ERROR] Failed to clear relationships (cluster=%d, conn=%d): %v", clusterID, connID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to clear relationships")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
