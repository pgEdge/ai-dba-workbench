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
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// ClusterHandler handles REST API requests for cluster hierarchy management
type ClusterHandler struct {
	datastore *database.Datastore
	authStore *auth.AuthStore
}

// NewClusterHandler creates a new cluster handler
func NewClusterHandler(datastore *database.Datastore, authStore *auth.AuthStore) *ClusterHandler {
	return &ClusterHandler{
		datastore: datastore,
		authStore: authStore,
	}
}

// ClusterGroupRequest is the request body for creating/updating cluster groups
type ClusterGroupRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

// ClusterRequest is the request body for creating/updating clusters
type ClusterRequest struct {
	GroupID     int     `json:"group_id"`
	Name        string  `json:"name"`
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
		mux.HandleFunc("/api/clusters", authWrapper(h.handleNotConfigured))
		mux.HandleFunc("/api/clusters/", authWrapper(h.handleNotConfigured))
		mux.HandleFunc("/api/cluster-groups", authWrapper(h.handleNotConfigured))
		mux.HandleFunc("/api/cluster-groups/", authWrapper(h.handleNotConfigured))
		return
	}

	// Cluster hierarchy endpoint (returns full hierarchy for ClusterNavigator)
	mux.HandleFunc("/api/clusters", authWrapper(h.handleClusters))

	// Cluster CRUD endpoints
	mux.HandleFunc("/api/clusters/", authWrapper(h.handleClusterSubpath))

	// Cluster group endpoints
	mux.HandleFunc("/api/cluster-groups", authWrapper(h.handleClusterGroups))
	mux.HandleFunc("/api/cluster-groups/", authWrapper(h.handleClusterGroupSubpath))
}

// handleNotConfigured returns an error when datastore is not configured
func (h *ClusterHandler) handleNotConfigured(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	//nolint:errcheck // Encoding simple error response
	json.NewEncoder(w).Encode(ErrorResponse{
		Error: "Cluster management is not available. The datastore is not configured.",
	})
}

// handleClusters handles GET /api/clusters (returns auto-detected topology)
func (h *ClusterHandler) handleClusters(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	topology, err := h.datastore.GetClusterTopology(ctx)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: fmt.Sprintf("Failed to get cluster topology: %v", err),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	//nolint:errcheck // Encoding topology
	json.NewEncoder(w).Encode(topology)
}

// handleClusterSubpath handles /api/clusters/{id}
func (h *ClusterHandler) handleClusterSubpath(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/clusters/")
	if path == "" {
		http.NotFound(w, r)
		return
	}

	// Check for servers sub-path: /api/clusters/{id}/servers
	parts := strings.Split(path, "/")
	if len(parts) == 2 && parts[1] == "servers" {
		clusterID, err := strconv.Atoi(parts[0])
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			//nolint:errcheck // Encoding simple error response
			json.NewEncoder(w).Encode(ErrorResponse{
				Error: "Invalid cluster ID",
			})
			return
		}
		h.handleClusterServers(w, r, clusterID)
		return
	}

	// Parse cluster ID
	clusterID, err := strconv.Atoi(parts[0])
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: "Invalid cluster ID",
		})
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

// handleClusterGroups handles GET/POST /api/cluster-groups
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

// handleClusterGroupSubpath handles /api/cluster-groups/{id} and sub-paths
func (h *ClusterHandler) handleClusterGroupSubpath(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/cluster-groups/")
	if path == "" {
		http.NotFound(w, r)
		return
	}

	// Check for clusters sub-path: /api/cluster-groups/{id}/clusters
	parts := strings.Split(path, "/")
	if len(parts) == 2 && parts[1] == "clusters" {
		groupID, err := strconv.Atoi(parts[0])
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			//nolint:errcheck // Encoding simple error response
			json.NewEncoder(w).Encode(ErrorResponse{
				Error: "Invalid group ID",
			})
			return
		}
		h.handleGroupClusters(w, r, groupID)
		return
	}

	// Parse group ID
	groupID, err := strconv.Atoi(parts[0])
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: "Invalid group ID",
		})
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: fmt.Sprintf("Failed to list cluster groups: %v", err),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	//nolint:errcheck // Encoding groups list
	json.NewEncoder(w).Encode(groups)
}

func (h *ClusterHandler) getClusterGroup(w http.ResponseWriter, r *http.Request, id int) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	group, err := h.datastore.GetClusterGroup(ctx, id)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: fmt.Sprintf("Cluster group not found: %v", err),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	//nolint:errcheck // Encoding group
	json.NewEncoder(w).Encode(group)
}

func (h *ClusterHandler) createClusterGroup(w http.ResponseWriter, r *http.Request) {
	var req ClusterGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: "Invalid request body",
		})
		return
	}

	if req.Name == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: "Name is required",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	group, err := h.datastore.CreateClusterGroup(ctx, req.Name, req.Description)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: fmt.Sprintf("Failed to create cluster group: %v", err),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	//nolint:errcheck // Encoding group
	json.NewEncoder(w).Encode(group)
}

func (h *ClusterHandler) updateClusterGroup(w http.ResponseWriter, r *http.Request, id int) {
	var req ClusterGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: "Invalid request body",
		})
		return
	}

	if req.Name == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: "Name is required",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	group, err := h.datastore.UpdateClusterGroup(ctx, id, req.Name, req.Description)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: fmt.Sprintf("Failed to update cluster group: %v", err),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	//nolint:errcheck // Encoding group
	json.NewEncoder(w).Encode(group)
}

func (h *ClusterHandler) deleteClusterGroup(w http.ResponseWriter, r *http.Request, id int) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	err := h.datastore.DeleteClusterGroup(ctx, id)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(err.Error(), "not found") {
			w.WriteHeader(http.StatusNotFound)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: fmt.Sprintf("Failed to delete cluster group: %v", err),
		})
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: fmt.Sprintf("Failed to list clusters: %v", err),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	//nolint:errcheck // Encoding clusters list
	json.NewEncoder(w).Encode(clusters)
}

func (h *ClusterHandler) createClusterInGroup(w http.ResponseWriter, r *http.Request, groupID int) {
	var req ClusterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: "Invalid request body",
		})
		return
	}

	if req.Name == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: "Name is required",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	cluster, err := h.datastore.CreateCluster(ctx, groupID, req.Name, req.Description)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: fmt.Sprintf("Failed to create cluster: %v", err),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	//nolint:errcheck // Encoding cluster
	json.NewEncoder(w).Encode(cluster)
}

func (h *ClusterHandler) getCluster(w http.ResponseWriter, r *http.Request, id int) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	cluster, err := h.datastore.GetCluster(ctx, id)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: fmt.Sprintf("Cluster not found: %v", err),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	//nolint:errcheck // Encoding cluster
	json.NewEncoder(w).Encode(cluster)
}

func (h *ClusterHandler) updateCluster(w http.ResponseWriter, r *http.Request, id int) {
	var req ClusterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: "Invalid request body",
		})
		return
	}

	if req.Name == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: "Name is required",
		})
		return
	}

	if req.GroupID == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: "group_id is required",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	cluster, err := h.datastore.UpdateCluster(ctx, id, req.GroupID, req.Name, req.Description)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: fmt.Sprintf("Failed to update cluster: %v", err),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	//nolint:errcheck // Encoding cluster
	json.NewEncoder(w).Encode(cluster)
}

func (h *ClusterHandler) deleteCluster(w http.ResponseWriter, r *http.Request, id int) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	err := h.datastore.DeleteCluster(ctx, id)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(err.Error(), "not found") {
			w.WriteHeader(http.StatusNotFound)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: fmt.Sprintf("Failed to delete cluster: %v", err),
		})
		return
	}

	w.WriteHeader(http.StatusNoContent)
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		//nolint:errcheck // Encoding simple error response
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: fmt.Sprintf("Failed to list servers: %v", err),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	//nolint:errcheck // Encoding servers list
	json.NewEncoder(w).Encode(servers)
}
