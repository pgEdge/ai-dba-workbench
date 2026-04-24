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
	"github.com/pgedge/ai-workbench/server/internal/logging"
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
	GroupID         *int    `json:"group_id,omitempty"`
	Name            string  `json:"name,omitempty"`
	Description     *string `json:"description,omitempty"`
	ReplicationType *string `json:"replication_type,omitempty"`
}

// AssignServerRequest is the request body for assigning a server to a cluster
type AssignServerRequest struct {
	ClusterID *int    `json:"cluster_id"`
	Role      *string `json:"role,omitempty"`
}

// AddServerToClusterRequest is the request body for adding a server to a cluster
type AddServerToClusterRequest struct {
	ConnectionID int     `json:"connection_id"`
	Role         *string `json:"role,omitempty"`
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

	// RBAC first: resolve the caller's visible connection set before any
	// datastore work so a zero-grant caller never triggers a refresh or
	// a topology build. VisibleConnectionIDs loads sharing metadata once
	// so this check does not issue per-server lookups.
	lister := database.NewVisibilityLister(h.datastore)
	visibleIDs, allConnections, err := h.rbacChecker.VisibleConnectionIDs(ctx, lister)
	if err != nil {
		log.Printf("[ERROR] Failed to resolve visible connections for topology: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to filter cluster topology")
		return
	}

	// Zero-grant caller: return an empty topology without refreshing
	// cluster assignments or reading the topology table. This is the
	// defense-in-depth gate that makes issue #67 regressions catchable
	// at the HTTP boundary without a datastore mock.
	if !allConnections && len(visibleIDs) == 0 {
		RespondJSON(w, http.StatusOK, []database.TopologyGroup{})
		return
	}

	// Sync cluster_id assignments before reading topology
	if err := h.datastore.RefreshClusterAssignments(ctx); err != nil {
		log.Printf("[WARN] Failed to refresh cluster assignments: %v", err)
	}

	// Push the caller's allow-list into the topology query. A nil slice
	// (superuser or wildcard scope) means "no filter"; a non-nil slice
	// prunes servers, empty clusters, and empty groups inside the
	// datastore before the result crosses the boundary.
	var filterIDs []int
	if !allConnections {
		filterIDs = visibleIDs
	}
	topology, err := h.datastore.GetClusterTopology(ctx, filterIDs)
	if err != nil {
		log.Printf("[ERROR] Failed to get cluster topology: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to get cluster topology")
		return
	}

	// Belt-and-suspenders: re-apply the handler-level pruning in case
	// the datastore query missed a join. This keeps filterTopologyByVisibility
	// as a defensive net that runs on every response.
	if !allConnections {
		topology = filterTopologyByVisibility(topology, visibleIDs)
	}

	RespondJSON(w, http.StatusOK, topology)
}

// filterTopologyByVisibility removes servers, clusters, and groups that
// the caller is not allowed to see. A cluster with no visible servers
// is dropped; a group with no visible clusters is dropped. The
// visibleIDs slice is converted to a set once so the filter walks the
// topology in linear time.
func filterTopologyByVisibility(groups []database.TopologyGroup, visibleIDs []int) []database.TopologyGroup {
	visible := make(map[int]bool, len(visibleIDs))
	for _, id := range visibleIDs {
		visible[id] = true
	}

	filtered := make([]database.TopologyGroup, 0, len(groups))
	for i := range groups {
		group := groups[i]
		clusters := make([]database.TopologyCluster, 0, len(group.Clusters))
		for j := range group.Clusters {
			cluster := group.Clusters[j]
			servers := filterTopologyServers(cluster.Servers, visible)
			if len(servers) == 0 {
				continue
			}
			cluster.Servers = servers
			clusters = append(clusters, cluster)
		}
		if len(clusters) == 0 {
			continue
		}
		group.Clusters = clusters
		filtered = append(filtered, group)
	}
	return filtered
}

// filterTopologyServers walks the server tree, retaining only servers
// whose connection ID is in the visible set. Child servers are filtered
// recursively so a hidden parent with visible children is dropped along
// with those children; this matches the existing "cluster visibility"
// contract where children are only meaningful under an accessible
// parent. Relationships pointing at hidden peers are also dropped so
// TargetServerID and TargetServerName never leak across the visibility
// boundary at the handler layer.
func filterTopologyServers(servers []database.TopologyServerInfo, visible map[int]bool) []database.TopologyServerInfo {
	out := make([]database.TopologyServerInfo, 0, len(servers))
	for i := range servers {
		s := servers[i]
		if !visible[s.ID] {
			continue
		}
		if len(s.Children) > 0 {
			s.Children = filterTopologyServers(s.Children, visible)
		}
		if len(s.Relationships) > 0 {
			rels := make([]database.TopologyRelationship, 0, len(s.Relationships))
			for _, rel := range s.Relationships {
				if visible[rel.TargetServerID] {
					rels = append(rels, rel)
				}
			}
			s.Relationships = rels
		}
		s.IsExpandable = len(s.Children) > 0
		out = append(out, s)
	}
	return out
}

// resolveVisibleConnections delegates to the package-level helper.
func (h *ClusterHandler) resolveVisibleConnections(ctx context.Context) (map[int]bool, bool, error) {
	return resolveVisibleConnectionSet(ctx, h.rbacChecker, h.datastore)
}

// clusterHasVisibleConnection delegates to the package-level helper.
func (h *ClusterHandler) clusterHasVisibleConnection(ctx context.Context, clusterID int, visible map[int]bool) (bool, error) {
	return clusterHasVisibleConnectionFn(ctx, h.datastore, clusterID, visible)
}

// groupHasVisibleConnection delegates to the package-level helper.
func (h *ClusterHandler) groupHasVisibleConnection(ctx context.Context, groupID int, visible map[int]bool) (bool, error) {
	return groupHasVisibleConnectionFn(ctx, h.datastore, groupID, visible)
}

// clusterConnectionMembership delegates to the package-level helper.
func (h *ClusterHandler) clusterConnectionMembership(ctx context.Context, clusterIDs []int) (map[int][]int, error) {
	return clusterConnectionMembershipFn(ctx, h.datastore, clusterIDs)
}

// clusterMembersVisible returns true when at least one connection ID in
// members appears in the visible set. A cluster with no member
// connections is treated as not visible to any non-privileged caller.
func clusterMembersVisible(members []int, visible map[int]bool) bool {
	for _, id := range members {
		if visible[id] {
			return true
		}
	}
	return false
}

// filterGroupsByVisibility delegates to the package-level helper.
func (h *ClusterHandler) filterGroupsByVisibility(ctx context.Context, groups []database.ClusterGroup, visible map[int]bool) ([]database.ClusterGroup, error) {
	return filterGroupsByVisibilityFn(ctx, h.datastore, groups, visible)
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

	// DELETE /api/v1/clusters/{id}/servers/{connectionId}
	if len(parts) == 3 && parts[1] == "servers" {
		clusterID, err := strconv.Atoi(parts[0])
		if err != nil {
			RespondError(w, http.StatusBadRequest, "Invalid cluster ID")
			return
		}
		connectionID, err := strconv.Atoi(parts[2])
		if err != nil {
			RespondError(w, http.StatusBadRequest, "Invalid connection ID")
			return
		}
		h.handleRemoveServerFromCluster(w, r, clusterID, connectionID)
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
		case http.MethodDelete:
			h.deleteAutoDetectedCluster(w, r, parts[0])
		default:
			w.Header().Set("Allow", "PUT, DELETE")
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

	visible, allConnections, err := h.resolveVisibleConnections(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to resolve visible connections for cluster groups: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to filter cluster groups")
		return
	}
	if !allConnections {
		filtered, err := h.filterGroupsByVisibility(ctx, groups, visible)
		if err != nil {
			log.Printf("[ERROR] Failed to filter cluster groups by visibility: %v", err)
			RespondError(w, http.StatusInternalServerError, "Failed to filter cluster groups")
			return
		}
		groups = filtered
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

	visible, allConnections, err := h.resolveVisibleConnections(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to resolve visible connections for cluster group %d: %v", id, err)
		RespondError(w, http.StatusInternalServerError, "Failed to load cluster group")
		return
	}
	if !allConnections {
		ok, err := h.groupHasVisibleConnection(ctx, id, visible)
		if err != nil {
			log.Printf("[ERROR] Failed to check cluster group visibility (id=%d): %v", id, err)
			RespondError(w, http.StatusInternalServerError, "Failed to load cluster group")
			return
		}
		if !ok {
			RespondError(w, http.StatusNotFound, "Cluster group not found")
			return
		}
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

	visible, allConnections, err := h.resolveVisibleConnections(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to resolve visible connections for cluster group %d: %v", id, err)
		RespondError(w, http.StatusInternalServerError, "Failed to delete cluster group")
		return
	}
	if !allConnections {
		ok, err := h.groupHasVisibleConnection(ctx, id, visible)
		if err != nil {
			log.Printf("[ERROR] Failed to check cluster group visibility (id=%d): %v", id, err)
			RespondError(w, http.StatusInternalServerError, "Failed to delete cluster group")
			return
		}
		if !ok {
			RespondError(w, http.StatusNotFound, "Cluster group not found")
			return
		}
	}

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

	visible, allConnections, err := h.resolveVisibleConnections(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to resolve visible connections for group %d: %v", groupID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to filter clusters")
		return
	}
	if !allConnections {
		// Gate on parent-group visibility before per-cluster filtering so
		// the response mirrors getClusterGroup: callers who cannot see
		// the group at all must receive 404 rather than an empty list
		// that leaks the group's existence.
		groupVisible, err := h.groupHasVisibleConnection(ctx, groupID, visible)
		if err != nil {
			log.Printf("[ERROR] Failed to check cluster group visibility (id=%d): %v", groupID, err)
			RespondError(w, http.StatusInternalServerError, "Failed to filter clusters")
			return
		}
		if !groupVisible {
			RespondError(w, http.StatusNotFound, "Cluster group not found")
			return
		}
		ids := make([]int, 0, len(clusters))
		for i := range clusters {
			ids = append(ids, clusters[i].ID)
		}
		membership, err := h.clusterConnectionMembership(ctx, ids)
		if err != nil {
			log.Printf("[ERROR] Failed to resolve cluster membership for group %d: %v", groupID, err)
			RespondError(w, http.StatusInternalServerError, "Failed to filter clusters")
			return
		}
		filtered := make([]database.Cluster, 0, len(clusters))
		for i := range clusters {
			if clusterMembersVisible(membership[clusters[i].ID], visible) {
				filtered = append(filtered, clusters[i])
			}
		}
		clusters = filtered
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

	visible, allConnections, err := h.resolveVisibleConnections(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to resolve visible connections for cluster %d: %v", id, err)
		RespondError(w, http.StatusInternalServerError, "Failed to load cluster")
		return
	}
	if !allConnections {
		ok, err := h.clusterHasVisibleConnection(ctx, id, visible)
		if err != nil {
			log.Printf("[ERROR] Failed to check cluster visibility (id=%d): %v", id, err)
			RespondError(w, http.StatusInternalServerError, "Failed to load cluster")
			return
		}
		if !ok {
			RespondError(w, http.StatusNotFound, "Cluster not found")
			return
		}
	}

	RespondJSON(w, http.StatusOK, cluster)
}

func (h *ClusterHandler) updateCluster(w http.ResponseWriter, r *http.Request, id int) {
	var req ClusterRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	// At least one field must be provided for update
	if req.Name == "" && req.GroupID == nil && req.Description == nil && req.ReplicationType == nil {
		RespondError(w, http.StatusBadRequest, "At least name, group_id, description, or replication_type is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	visible, allConnections, err := h.resolveVisibleConnections(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to resolve visible connections for cluster %d: %v", id, err)
		RespondError(w, http.StatusInternalServerError, "Failed to update cluster")
		return
	}
	if !allConnections {
		ok, err := h.clusterHasVisibleConnection(ctx, id, visible)
		if err != nil {
			log.Printf("[ERROR] Failed to check cluster visibility (id=%d): %v", id, err)
			RespondError(w, http.StatusInternalServerError, "Failed to update cluster")
			return
		}
		if !ok {
			RespondError(w, http.StatusNotFound, "Cluster not found")
			return
		}
	}

	cluster, err := h.datastore.UpdateClusterPartial(ctx, id, req.GroupID, req.Name, req.Description, req.ReplicationType)
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

	visible, allConnections, err := h.resolveVisibleConnections(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to resolve visible connections for cluster %d: %v", id, err)
		RespondError(w, http.StatusInternalServerError, "Failed to delete cluster")
		return
	}
	if !allConnections {
		ok, err := h.clusterHasVisibleConnection(ctx, id, visible)
		if err != nil {
			log.Printf("[ERROR] Failed to check cluster visibility (id=%d): %v", id, err)
			RespondError(w, http.StatusInternalServerError, "Failed to delete cluster")
			return
		}
		if !ok {
			RespondError(w, http.StatusNotFound, "Cluster not found")
			return
		}
	}

	err = h.datastore.DeleteCluster(ctx, id)
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
		log.Printf("[ERROR] Failed to update auto-detected cluster %s: %v", logging.SanitizeForLog(clusterID), err) //nolint:gosec // G706: clusterID passed through logging.SanitizeForLog
		RespondError(w, http.StatusInternalServerError, "Failed to update auto-detected cluster")
		return
	}

	RespondJSON(w, http.StatusOK, cluster)
}

// deleteAutoDetectedCluster handles DELETE requests for auto-detected
// clusters. It resolves the auto_cluster_key from the topology ID and
// soft-deletes the cluster.
func (h *ClusterHandler) deleteAutoDetectedCluster(w http.ResponseWriter, r *http.Request, clusterID string) {
	if !h.rbacChecker.HasAdminPermission(r.Context(), auth.PermManageConnections) {
		RespondError(w, http.StatusForbidden,
			"Permission denied: you do not have permission to delete auto-detected clusters")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// For Spock clusters, the key is unambiguous
	if strings.HasPrefix(clusterID, "cluster-spock-") {
		prefix := strings.TrimPrefix(clusterID, "cluster-spock-")
		autoKey := "spock:" + prefix
		if err := h.datastore.DeleteAutoDetectedCluster(ctx, autoKey); err != nil {
			log.Printf("[ERROR] Failed to delete auto-detected cluster %s: %v", logging.SanitizeForLog(clusterID), err) //nolint:gosec // G706: clusterID passed through logging.SanitizeForLog
			RespondError(w, http.StatusInternalServerError, "Failed to delete auto-detected cluster")
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// For server-{id}, multiple auto_cluster_key prefixes share the same ID.
	// The topology builder may have created a binary, standalone, or logical
	// cluster depending on the connection's replication role. Since we cannot
	// know which type it was from the ID alone, we dismiss all candidate keys.
	if strings.HasPrefix(clusterID, "server-") {
		idStr := strings.TrimPrefix(clusterID, "server-")
		candidates := []string{
			"binary:" + idStr,
			"standalone:" + idStr,
			"logical:" + idStr,
		}
		if err := h.datastore.DismissAutoDetectedClusterKeys(ctx, candidates); err != nil {
			log.Printf("[ERROR] Failed to delete auto-detected cluster %s: %v", logging.SanitizeForLog(clusterID), err) //nolint:gosec // G706: clusterID passed through logging.SanitizeForLog
			RespondError(w, http.StatusInternalServerError, "Failed to delete auto-detected cluster")
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	RespondError(w, http.StatusBadRequest, "Invalid auto-detected cluster ID")
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
		log.Printf("[ERROR] Failed to update auto-detected group %s: %v", logging.SanitizeForLog(groupID), err) //nolint:gosec // G706: groupID passed through logging.SanitizeForLog
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
	case http.MethodPost:
		h.addServerToCluster(w, r, clusterID)
	default:
		w.Header().Set("Allow", "GET, POST")
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

	visible, allConnections, err := h.resolveVisibleConnections(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to resolve visible connections for cluster %d servers: %v", clusterID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to list servers")
		return
	}
	if !allConnections {
		ok, err := h.clusterHasVisibleConnection(ctx, clusterID, visible)
		if err != nil {
			log.Printf("[ERROR] Failed to check cluster visibility (id=%d): %v", clusterID, err)
			RespondError(w, http.StatusInternalServerError, "Failed to list servers")
			return
		}
		if !ok {
			RespondError(w, http.StatusNotFound, "Cluster not found")
			return
		}
		filtered := make([]database.ServerInfo, 0, len(servers))
		for i := range servers {
			if visible[servers[i].ID] {
				filtered = append(filtered, servers[i])
			}
		}
		servers = filtered
	}

	RespondJSON(w, http.StatusOK, servers)
}

// addServerToCluster handles POST /api/v1/clusters/{id}/servers
func (h *ClusterHandler) addServerToCluster(w http.ResponseWriter, r *http.Request, clusterID int) {
	var req AddServerToClusterRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	if req.ConnectionID <= 0 {
		RespondError(w, http.StatusBadRequest, "A valid connection_id is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	visible, allConnections, err := h.resolveVisibleConnections(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to resolve visible connections for cluster %d: %v", clusterID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to add server to cluster")
		return
	}
	if !allConnections {
		ok, err := h.clusterHasVisibleConnection(ctx, clusterID, visible)
		if err != nil {
			log.Printf("[ERROR] Failed to check cluster visibility (id=%d): %v", clusterID, err)
			RespondError(w, http.StatusInternalServerError, "Failed to add server to cluster")
			return
		}
		if !ok {
			RespondError(w, http.StatusNotFound, "Cluster not found")
			return
		}
		if !visible[req.ConnectionID] {
			RespondError(w, http.StatusNotFound, "Connection not found")
			return
		}
	}

	err = h.datastore.AddServerToCluster(ctx, clusterID, req.ConnectionID, req.Role)
	if err != nil {
		if errors.Is(err, database.ErrClusterNotFound) {
			RespondError(w, http.StatusNotFound, "Cluster not found")
			return
		}
		if errors.Is(err, database.ErrConnectionNotFound) {
			RespondError(w, http.StatusNotFound, "Connection not found")
			return
		}
		log.Printf("[ERROR] Failed to add server %d to cluster %d: %v", req.ConnectionID, clusterID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to add server to cluster")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]any{
		"cluster_id":    clusterID,
		"connection_id": req.ConnectionID,
		"role":          req.Role,
	})
}

// handleRemoveServerFromCluster handles DELETE /api/v1/clusters/{id}/servers/{connectionId}
func (h *ClusterHandler) handleRemoveServerFromCluster(w http.ResponseWriter, r *http.Request, clusterID int, connectionID int) {
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", "DELETE")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	visible, allConnections, err := h.resolveVisibleConnections(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to resolve visible connections for cluster %d: %v", clusterID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to remove server from cluster")
		return
	}
	if !allConnections {
		ok, err := h.clusterHasVisibleConnection(ctx, clusterID, visible)
		if err != nil {
			log.Printf("[ERROR] Failed to check cluster visibility (id=%d): %v", clusterID, err)
			RespondError(w, http.StatusInternalServerError, "Failed to remove server from cluster")
			return
		}
		if !ok {
			RespondError(w, http.StatusNotFound, "Cluster not found")
			return
		}
	}

	err = h.datastore.RemoveServerFromCluster(ctx, clusterID, connectionID)
	if err != nil {
		if errors.Is(err, database.ErrConnectionNotFound) {
			RespondError(w, http.StatusNotFound, "Connection not found in this cluster")
			return
		}
		log.Printf("[ERROR] Failed to remove server %d from cluster %d: %v", connectionID, clusterID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to remove server from cluster")
		return
	}

	w.WriteHeader(http.StatusNoContent)
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

	visible, allConnections, err := h.resolveVisibleConnections(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to resolve visible connections for cluster autocomplete: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to list clusters")
		return
	}
	if !allConnections {
		ids := make([]int, 0, len(clusters))
		for i := range clusters {
			ids = append(ids, clusters[i].ID)
		}
		membership, err := h.clusterConnectionMembership(ctx, ids)
		if err != nil {
			log.Printf("[ERROR] Failed to resolve cluster membership for autocomplete: %v", err)
			RespondError(w, http.StatusInternalServerError, "Failed to list clusters")
			return
		}
		filtered := make([]database.ClusterSummary, 0, len(clusters))
		for i := range clusters {
			if clusterMembersVisible(membership[clusters[i].ID], visible) {
				filtered = append(filtered, clusters[i])
			}
		}
		clusters = filtered
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

	RespondJSON(w, http.StatusCreated, map[string]any{
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
