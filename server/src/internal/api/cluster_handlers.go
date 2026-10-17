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

	"github.com/jackc/pgx/v5"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/logging"
)

// ClusterHandler handles REST API requests for cluster hierarchy management
type ClusterHandler struct {
	datastore   *database.Datastore
	authStore   *auth.AuthStore
	rbacChecker *auth.RBACChecker

	// denialAuditor audits this handler's token-scope refusals, and
	// the ownership and permission refusals on the cluster-group routes.
	denialAuditor
}

// NewClusterHandler creates a new cluster handler
func NewClusterHandler(datastore *database.Datastore, authStore *auth.AuthStore, rbacChecker *auth.RBACChecker) *ClusterHandler {
	return &ClusterHandler{
		datastore:   datastore,
		authStore:   authStore,
		rbacChecker: rbacChecker,
	}
}

// ClusterGroupRequest is the request body for creating/updating cluster
// groups. IsShared is a pointer so an omitted field (nil) can be
// distinguished from an explicit false, which matters for partial updates:
// on update, a nil IsShared preserves the group's current visibility rather
// than forcing it private (issue #304).
type ClusterGroupRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	IsShared    *bool   `json:"is_shared,omitempty"`
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
	if respondDBError(w, err, "get cluster topology") {
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

// membersHidden reports whether a cluster or group has members but none
// of them is in the visible set. A container with no members is not
// hidden by this test, so a write to it falls to the caller's other
// checks.
func membersHidden(members []int, visible map[int]bool) bool {
	return len(members) > 0 && !clusterMembersVisible(members, visible)
}

// clusterGroupVisibility is what decides whether a caller may see one
// cluster group: whether they see every connection and, when they do
// not, the connections visible to them and the group's members.
type clusterGroupVisibility struct {
	all     bool
	visible map[int]bool
	members []int
}

// hidden reports whether the group must be answered as missing: it has
// members and none is visible to the caller, or it has none and the
// caller does not own it. A caller who sees every connection sees every
// group. Every cluster-group route applies this one rule, so an empty
// group is visible to its owner and to callers who see every
// connection, and to nobody else whatever permissions they hold (issue
// #705).
func (v clusterGroupVisibility) hidden(isOwner bool) bool {
	if v.all {
		return false
	}
	return membersHidden(v.members, v.visible) ||
		(len(v.members) == 0 && !isOwner)
}

// ownsClusterGroup reports whether username, when set, is the group's
// recorded owner.
func ownsClusterGroup(username string, group *database.ClusterGroup) bool {
	return username != "" && group.OwnerUsername.Valid &&
		group.OwnerUsername.String == username
}

// emptyGroupOwnerName returns the name the cluster-group reads match a
// group's owner against: the context username, which an API token
// carries as its owner's. A token narrowed to some connections gets "",
// so it is never treated as an owner and an empty group stays hidden
// from it; the write routes refuse such a token before their lookup
// instead.
func (h *ClusterHandler) emptyGroupOwnerName(r *http.Request) string {
	if !h.rbacChecker.AllConnectionsInTokenScope(r.Context()) {
		return ""
	}
	return auth.GetUsernameFromContext(r.Context())
}

// loadClusterGroupVisibility resolves the caller's visible connections
// and, when those are restricted, the group's member connections,
// writing 500 with failMsg and returning false when either fails. The
// cluster-group handlers call it before they look the group up, so a
// missing id, which has no members, runs the same queries as a group
// the caller cannot see, which narrows the timing difference between
// the two as well as giving them the same 404 (issue #649).
func (h *ClusterHandler) loadClusterGroupVisibility(ctx context.Context,
	w http.ResponseWriter, id int, failMsg string) (clusterGroupVisibility, bool) {

	visible, allConnections, err := h.resolveVisibleConnections(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to resolve visible connections for cluster group %d: %v", id, err)
		RespondError(w, http.StatusInternalServerError, failMsg)
		return clusterGroupVisibility{}, false
	}
	v := clusterGroupVisibility{all: allConnections, visible: visible}
	if allConnections {
		return v, true
	}
	v.members, err = h.datastore.GetConnectionIDsForGroup(ctx, id)
	if err != nil {
		log.Printf("[ERROR] Failed to check cluster group visibility (id=%d): %v", id, err)
		RespondError(w, http.StatusInternalServerError, failMsg)
		return clusterGroupVisibility{}, false
	}
	return v, true
}

// filterGroupsByVisibility delegates to the package-level helper.
func (h *ClusterHandler) filterGroupsByVisibility(ctx context.Context, groups []database.ClusterGroup, visible map[int]bool, owner string) ([]database.ClusterGroup, error) {
	return filterGroupsByVisibilityFn(ctx, h.datastore, groups, visible, owner)
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
	if respondDBError(w, err, "list cluster groups") {
		return
	}

	visible, allConnections, err := h.resolveVisibleConnections(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to resolve visible connections for cluster groups: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to filter cluster groups")
		return
	}
	if !allConnections {
		filtered, err := h.filterGroupsByVisibility(ctx, groups, visible,
			h.emptyGroupOwnerName(r))
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

	// Visibility first, so a missing id and a hidden group cost the
	// same queries (issue #649); a group with no members is visible only
	// to its owner among restricted callers (issue #705).
	v, ok := h.loadClusterGroupVisibility(ctx, w, id, "Failed to load cluster group")
	if !ok {
		return
	}

	group, err := h.datastore.GetClusterGroup(ctx, id)
	if err != nil {
		log.Printf("[ERROR] Cluster group not found (id=%d): %v", id, err)
		RespondError(w, http.StatusNotFound, "Cluster group not found")
		return
	}
	if v.hidden(ownsClusterGroup(h.emptyGroupOwnerName(r), group)) {
		RespondError(w, http.StatusNotFound, "Cluster group not found")
		return
	}

	RespondJSON(w, http.StatusOK, group)
}

func (h *ClusterHandler) createClusterGroup(w http.ResponseWriter, r *http.Request) {
	// Issue #207: gate cluster-group creation on manage_connections.
	// Check before decoding the body so denied callers cannot probe payload
	// shape via validation error messages. The refusal is audited, as
	// are the other ownership and permission refusals on the
	// cluster-group routes (issue #589).
	if !h.rbacChecker.HasAdminPermission(r.Context(), auth.PermManageConnections) {
		h.denialRecorder.refuse(w, r,
			"Permission denied: requires manage_connections permission")
		return
	}
	// A new group holds clusters and group-wide settings that reach
	// every connection later placed in it, so, as for the other
	// container writes, a token must cover every connection (issue
	// #471). The session lookup below refuses an API token today; this
	// keeps the rule should that change.
	if !requireAllConnectionsInTokenScope(w, r, h.rbacChecker, h.denialRecorder) {
		return
	}

	var req ClusterGroupRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	// Issue #269: validate the name's characters, not just non-emptiness.
	// ValidateDisplayName also enforces the VARCHAR(255) limit on the raw
	// name, so no separate validateMaxLen("Name", ...) call is needed here
	// (issue #270).
	if err := ValidateDisplayName(req.Name); err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Resolve the creating user so the new group records its owner. This
	// mirrors updateClusterGroup's use of getUserInfoCompat and lets the
	// owner-fallback authorization on update/delete apply to groups the
	// user creates (issue #304).
	username, _, err := getUserInfoCompat(r)
	if err != nil {
		RespondError(w, http.StatusUnauthorized, "Invalid or missing authentication token")
		return
	}

	// is_shared defaults to false when the field is omitted; only an
	// explicit true opts the group into visibility for all users.
	isShared := req.IsShared != nil && *req.IsShared

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	group, err := h.datastore.CreateClusterGroupWithOwner(ctx, req.Name, req.Description, &username, isShared)
	if err != nil {
		log.Printf("[ERROR] Failed to create cluster group %s: %v", req.Name, err)
		RespondError(w, http.StatusInternalServerError, "Failed to create cluster group")
		return
	}

	RespondJSON(w, http.StatusCreated, group)
}

func (h *ClusterHandler) updateClusterGroup(w http.ResponseWriter, r *http.Request, id int) {
	// Check user permissions
	username, _, err := getUserInfoCompat(r)
	if err != nil {
		RespondError(w, http.StatusUnauthorized, "Invalid or missing authentication token")
		return
	}

	hasManageConns := h.rbacChecker.HasAdminPermission(r.Context(), auth.PermManageConnections)

	// A group's definition decides which group-wide blackouts,
	// overrides and grants reach its members, so a token must cover
	// every connection to change it (issue #471). The check depends
	// only on the caller, so it runs before the lookup and its refusal
	// says nothing about whether the group exists.
	if !requireAllConnectionsInTokenScope(w, r, h.rbacChecker, h.denialRecorder) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// The visibility queries run before the lookup, so a missing id
	// costs the same queries as a hidden group (issue #649).
	v, ok := h.loadClusterGroupVisibility(ctx, w, id, "Failed to update cluster group")
	if !ok {
		return
	}

	existingGroup, err := h.datastore.GetClusterGroup(ctx, id)
	if err != nil {
		log.Printf("[ERROR] Cluster group not found for update (id=%d): %v", id, err)
		RespondError(w, http.StatusNotFound, "Cluster group not found")
		return
	}

	// Visibility comes before ownership, so that a group the caller
	// cannot see gets the same 404 as a missing one rather than a 403
	// that confirms it exists (issue #649). A group with no members is
	// visible to its owner, who can still rename a group they have just
	// created (issue #58), and to no other restricted caller, as for the
	// GET: manage_connections does not make it visible (issue #705).
	isOwner := ownsClusterGroup(username, existingGroup)
	if v.hidden(isOwner) {
		RespondError(w, http.StatusNotFound, "Cluster group not found")
		return
	}

	// Permission check: manage_connections permission, or owner
	if !hasManageConns && !isOwner {
		h.denialRecorder.refuse(w, r,
			"You do not have permission to update this cluster group")
		return
	}

	var req ClusterGroupRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	// Issue #269: validate the name's characters, not just non-emptiness.
	if err := ValidateDisplayName(req.Name); err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Partial-update semantics: a nil Description or IsShared preserves the
	// existing column value rather than clearing it, so a client that sends
	// only some fields does not accidentally null the others (issue #304).
	// The owner/manage_connections permission check above has already run,
	// so no is_shared visibility change reaches the datastore for a caller
	// who is neither the owner nor an admin.
	group, err := h.datastore.UpdateClusterGroup(ctx, id, req.Name, req.Description, req.IsShared)
	if err != nil {
		log.Printf("[ERROR] Failed to update cluster group (id=%d): %v", id, err)
		RespondError(w, http.StatusInternalServerError, "Failed to update cluster group")
		return
	}

	RespondJSON(w, http.StatusOK, group)
}

func (h *ClusterHandler) deleteClusterGroup(w http.ResponseWriter, r *http.Request, id int) {
	// Issue #207: owner-fallback authorization. Admins with
	// manage_connections can delete any group they can see; the group's
	// owner can delete their own group. Authorization is enforced early
	// (before any response body write). The ownership check needs the
	// group row, so it follows the lookup and the visibility check: a
	// group the caller cannot see answers 404 exactly as a missing one
	// does, and only a visible group can draw the ownership 403 (issue
	// #649).
	username, _, err := getUserInfoCompat(r)
	if err != nil {
		RespondError(w, http.StatusUnauthorized,
			"Invalid or missing authentication token")
		return
	}

	hasManageConns := h.rbacChecker.HasAdminPermission(r.Context(),
		auth.PermManageConnections)

	// Deleting a group cascades to its clusters and to the group-scope
	// blackouts, schedules, probe configs, alert thresholds and channel
	// overrides that reach its members, so a token must cover every
	// connection to do it (issue #471). The check depends only on the
	// caller, so it runs before the lookup.
	if !requireAllConnectionsInTokenScope(w, r, h.rbacChecker, h.denialRecorder) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// The visibility queries run before the lookup, so a missing id
	// costs the same queries as a hidden group (issue #649).
	v, ok := h.loadClusterGroupVisibility(ctx, w, id, "Failed to delete cluster group")
	if !ok {
		return
	}

	existingGroup, err := h.datastore.GetClusterGroup(ctx, id)
	if err != nil {
		log.Printf("[ERROR] Cluster group not found for delete (id=%d): %v", id, err)
		RespondError(w, http.StatusNotFound, "Cluster group not found")
		return
	}

	// An empty group is visible to its owner, who can therefore delete
	// it, and hidden from every other restricted caller, as for the GET
	// (issue #705).
	isOwner := ownsClusterGroup(username, existingGroup)
	if v.hidden(isOwner) {
		RespondError(w, http.StatusNotFound, "Cluster group not found")
		return
	}

	if !hasManageConns && !isOwner {
		h.denialRecorder.refuse(w, r,
			"You do not have permission to delete this cluster group")
		return
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

	// Gate on parent-group visibility before reading the clusters, so
	// the response mirrors getClusterGroup: callers who cannot see the
	// group at all, or name one that does not exist, receive 404 rather
	// than an empty list that leaks the group's existence, and a missing
	// id costs the same queries as a hidden group (issues #649 and
	// #705).
	v, ok := h.loadClusterGroupVisibility(ctx, w, groupID, "Failed to filter clusters")
	if !ok {
		return
	}
	group, err := h.datastore.GetClusterGroup(ctx, groupID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			RespondError(w, http.StatusNotFound, "Cluster group not found")
			return
		}
		log.Printf("[ERROR] Failed to load cluster group (id=%d): %v", groupID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to list clusters")
		return
	}
	if v.hidden(ownsClusterGroup(h.emptyGroupOwnerName(r), group)) {
		RespondError(w, http.StatusNotFound, "Cluster group not found")
		return
	}

	clusters, err := h.datastore.GetClustersInGroup(ctx, groupID)
	if err != nil {
		log.Printf("[ERROR] Failed to list clusters in group %d: %v", groupID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to list clusters")
		return
	}

	if !v.all {
		visible := v.visible
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
	// Issue #207: gate cluster creation under a group on manage_connections.
	if !h.rbacChecker.HasAdminPermission(r.Context(), auth.PermManageConnections) {
		h.denialRecorder.refuse(w, r,
			"Permission denied: requires manage_connections permission")
		return
	}
	// A new cluster in a group inherits that group's blackouts, probe
	// configurations and overrides, and so does any connection later
	// moved into it, so a token must cover every connection (issue
	// #471).
	if !requireAllConnectionsInTokenScope(w, r, h.rbacChecker, h.denialRecorder) {
		return
	}

	var req ClusterRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	// Issue #269: validate the name's characters, not just non-emptiness.
	// ValidateDisplayName also enforces the VARCHAR(255) limit on the raw
	// name, so no separate validateMaxLen("Name", ...) call is needed here
	// (issue #270).
	if err := ValidateDisplayName(req.Name); err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if !h.requireVisibleTargetGroup(ctx, w, r, groupID, "Failed to create cluster") {
		return
	}

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
	// Issue #207: gate cluster mutations on manage_connections. The
	// spec asked for an owner-fallback variant, but the clusters table
	// (see collector/src/database/schema.go) has no owner_username
	// column, so no literal owner check is possible without a schema
	// change. Apply the plain admin gate; the visibility check farther
	// down still returns 404 for callers who cannot see the cluster, so
	// the gate is layered on top of existing visibility filtering.
	if !h.rbacChecker.HasAdminPermission(r.Context(), auth.PermManageConnections) {
		h.denialRecorder.refuse(w, r,
			"Permission denied: requires manage_connections permission")
		return
	}

	// A cluster's definition decides which cluster-wide blackouts,
	// overrides and grants reach its members, so a token must cover
	// every connection to change it (issue #471).
	if !requireAllConnectionsInTokenScope(w, r, h.rbacChecker, h.denialRecorder) {
		return
	}

	var req ClusterRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	// At least one field must be provided for update
	if req.Name == "" && req.GroupID == nil && req.Description == nil && req.ReplicationType == nil {
		RespondError(w, http.StatusBadRequest, "At least name, group_id, description, or replication_type is required")
		return
	}

	// Issue #269: name is optional on update (an empty string means "leave
	// unchanged"). Validate its characters only when a name is supplied.
	if req.Name != "" {
		if err := ValidateDisplayName(req.Name); err != nil {
			RespondError(w, http.StatusBadRequest, err.Error())
			return
		}
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

	if req.GroupID != nil && !h.requireVisibleTargetGroup(ctx, w, r, *req.GroupID, "Failed to update cluster") {
		return
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
	// Issue #207: plain admin gate. Same rationale as updateCluster: the
	// clusters table has no owner_username column, so the spec's
	// "owner-fallback" cannot be applied literally. Admins with
	// manage_connections can delete; everyone else is rejected before
	// any datastore read.
	if !h.rbacChecker.HasAdminPermission(r.Context(), auth.PermManageConnections) {
		h.denialRecorder.refuse(w, r,
			"Permission denied: requires manage_connections permission")
		return
	}

	// A cluster's definition decides which cluster-wide blackouts,
	// overrides and grants reach its members, so a token must cover
	// every connection to change it (issue #471).
	if !requireAllConnectionsInTokenScope(w, r, h.rbacChecker, h.denialRecorder) {
		return
	}

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
		h.denialRecorder.refuse(w, r,
			"Permission denied: you do not have permission to modify auto-detected clusters")
		return
	}

	// A cluster's definition decides which cluster-wide blackouts,
	// overrides and grants reach its members, so a token must cover
	// every connection to change it (issue #471).
	if !requireAllConnectionsInTokenScope(w, r, h.rbacChecker, h.denialRecorder) {
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

	// Issue #269: name is optional here (empty means "leave unchanged").
	// Validate its characters only when a name is supplied.
	if req.Name != "" {
		if err := ValidateDisplayName(req.Name); err != nil {
			RespondError(w, http.StatusBadRequest, err.Error())
			return
		}
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

	// A caller who cannot see the cluster, or whose auto_cluster_key
	// names a different cluster, gets the answer for a missing one; the
	// target group must be visible too (issue #673).
	if !h.requireVisibleAutoDetectedCluster(ctx, w, clusterID, autoKey, "Failed to update auto-detected cluster") {
		return
	}
	if req.GroupID != nil && !h.requireVisibleTargetGroup(ctx, w, r, *req.GroupID, "Failed to update auto-detected cluster") {
		return
	}

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
		h.denialRecorder.refuse(w, r,
			"Permission denied: you do not have permission to delete auto-detected clusters")
		return
	}

	// A cluster's definition decides which cluster-wide blackouts,
	// overrides and grants reach its members, so a token must cover
	// every connection to change it (issue #471).
	if !requireAllConnectionsInTokenScope(w, r, h.rbacChecker, h.denialRecorder) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// A caller who cannot see the cluster gets the answer for a missing
	// one (issue #673).
	if !h.requireVisibleAutoDetectedCluster(ctx, w, clusterID, "", "Failed to delete auto-detected cluster") {
		return
	}

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
		h.denialRecorder.refuse(w, r,
			"Permission denied: you do not have permission to rename auto-detected groups")
		return
	}

	// A group's definition decides which group-wide blackouts,
	// overrides and grants reach its members, so a token must cover
	// every connection to change it (issue #471).
	if !requireAllConnectionsInTokenScope(w, r, h.rbacChecker, h.denialRecorder) {
		return
	}

	// Parse request body
	var req ClusterGroupRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	// Issue #269: validate the name's characters, not just non-emptiness.
	if err := ValidateDisplayName(req.Name); err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
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
	// Issue #207: server-attach is a system-topology mutation with no
	// clear owner concept; apply the plain admin gate.
	if !h.rbacChecker.HasAdminPermission(r.Context(), auth.PermManageConnections) {
		h.denialRecorder.refuse(w, r,
			"Permission denied: requires manage_connections permission")
		return
	}

	var req AddServerToClusterRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	if req.ConnectionID <= 0 {
		RespondError(w, http.StatusBadRequest, "A valid connection_id is required")
		return
	}
	// The role is written to connections.role, so it gets the same
	// check as PUT /connections/{id}/cluster.
	if req.Role != nil && !knownConnectionRoles[*req.Role] {
		RespondError(w, http.StatusBadRequest, "Invalid role")
		return
	}
	if !requireConnectionsInTokenScope(w, r, h.rbacChecker, h.denialRecorder, req.ConnectionID) {
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

	// Issue #207: server-detach is a system-topology mutation with no
	// clear owner concept; apply the plain admin gate.
	if !h.rbacChecker.HasAdminPermission(r.Context(), auth.PermManageConnections) {
		h.denialRecorder.refuse(w, r,
			"Permission denied: requires manage_connections permission")
		return
	}

	if !requireConnectionsInTokenScope(w, r, h.rbacChecker, h.denialRecorder, connectionID) {
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
	if respondDBError(w, err, "list clusters") {
		return
	}

	visible, allConnections, err := h.resolveVisibleConnections(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to resolve visible connections for cluster autocomplete: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to list clusters")
		return
	}
	if !allConnections {
		clusters, err = filterClustersByVisibility(ctx, h.datastore, clusters, visible)
		if err != nil {
			log.Printf("[ERROR] Failed to resolve cluster membership for autocomplete: %v", err)
			RespondError(w, http.StatusInternalServerError, "Failed to list clusters")
			return
		}
	}

	RespondJSON(w, http.StatusOK, clusters)
}

// handleCreateCluster handles POST /api/v1/clusters
func (h *ClusterHandler) handleCreateCluster(w http.ResponseWriter, r *http.Request) {
	// Issue #207: cluster creation has no notion of owner before
	// creation, so apply the plain admin gate.
	if !h.rbacChecker.HasAdminPermission(r.Context(), auth.PermManageConnections) {
		h.denialRecorder.refuse(w, r,
			"Permission denied: requires manage_connections permission")
		return
	}
	// As on POST /cluster-groups/{id}/clusters: the new cluster, and
	// any connection later moved into it, inherits its group's
	// settings, so a token must cover every connection (issue #471).
	if !requireAllConnectionsInTokenScope(w, r, h.rbacChecker, h.denialRecorder) {
		return
	}

	var req ManualClusterRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	// Issue #269: validate the name's characters, not just non-emptiness.
	// ValidateDisplayName also enforces the VARCHAR(255) limit on the raw
	// name, so no separate validateMaxLen("Name", ...) call is needed here
	// (issue #270).
	if err := ValidateDisplayName(req.Name); err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// With no group_id the cluster goes into the default group, which
	// every caller holding manage_connections may write to on purpose:
	// requiring visibility of it would hide it from a restricted caller
	// whose connections all sit in other groups, and they could then
	// create no clusters at all. requireVisibleTargetGroup admits an
	// explicit default group id for the same reason.
	if req.GroupID != nil && !h.requireVisibleTargetGroup(ctx, w, r, *req.GroupID, "Failed to create cluster") {
		return
	}

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

	// Issue #678: a restricted caller sees only clusters with a visible
	// member, and only relationships whose both ends are visible.
	visible, allConnections, err := h.resolveVisibleConnections(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to resolve visible connections for cluster %d relationships: %v", clusterID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to get cluster relationships")
		return
	}
	if !allConnections {
		ok, err := h.clusterHasVisibleConnection(ctx, clusterID, visible)
		if err != nil {
			log.Printf("[ERROR] Failed to check cluster visibility (id=%d): %v", clusterID, err)
			RespondError(w, http.StatusInternalServerError, "Failed to get cluster relationships")
			return
		}
		if !ok {
			RespondError(w, http.StatusNotFound, "Cluster not found")
			return
		}
	}

	relationships, err := h.datastore.GetClusterRelationships(ctx, clusterID)
	if err != nil {
		log.Printf("[ERROR] Failed to get cluster relationships (cluster=%d): %v", clusterID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to get cluster relationships")
		return
	}

	if !allConnections {
		relationships = filterRelationshipsByVisibility(relationships, visible)
	}

	if relationships == nil {
		relationships = []database.NodeRelationship{}
	}

	RespondJSON(w, http.StatusOK, relationships)
}

// filterRelationshipsByVisibility keeps only the relationships whose
// source and target connections are both in the visible set, so that a
// hidden connection's id and name never cross the visibility boundary.
func filterRelationshipsByVisibility(rels []database.NodeRelationship, visible map[int]bool) []database.NodeRelationship {
	out := make([]database.NodeRelationship, 0, len(rels))
	for _, rel := range rels {
		if visible[rel.SourceConnectionID] && visible[rel.TargetConnectionID] {
			out = append(out, rel)
		}
	}
	return out
}

// hiddenRelationshipRefusal answers a rewrite or clear that would delete
// a manual relationship to a connection the caller cannot see. It is a
// fixed string, so the audit coalescer's key stays bounded.
const hiddenRelationshipRefusal = "Permission denied: the connection has relationships to connections you cannot access"

// visibleSourceInCluster reports whether a restricted caller may rewrite
// or clear the manual relationships of a source connection: the source
// must be visible and a member of the addressed cluster. A nil visible
// map means unrestricted visibility. The targets of the rows a rewrite
// or clear deletes are checked by the delete itself (see
// refuseHiddenRelationship), so a row written concurrently is covered.
//
// It writes the response and returns false when the caller may not
// proceed: 404 for a hidden source, so that the source's existence is
// not disclosed, and 400 for a source outside the cluster. Membership
// is checked first because a connection that moved clusters keeps its
// rows in the old cluster, which may have no visible member, and a 403
// there would confirm a hidden relationship in a cluster the caller
// cannot see.
func (h *ClusterHandler) visibleSourceInCluster(ctx context.Context, w http.ResponseWriter, clusterID, sourceID int, visible map[int]bool) bool {
	if visible == nil {
		return true
	}
	if !visible[sourceID] {
		RespondError(w, http.StatusNotFound, "Connection not found")
		return false
	}
	inCluster, err := h.datastore.IsConnectionInCluster(ctx, clusterID, sourceID)
	if err != nil {
		log.Printf("[ERROR] Failed to check source cluster membership (cluster=%d, conn=%d): %v", clusterID, sourceID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to validate relationships")
		return false
	}
	if !inCluster {
		RespondError(w, http.StatusBadRequest, "Source connection does not belong to this cluster")
		return false
	}
	return true
}

// refuseHiddenRelationship answers 403, auditing the refusal, and
// returns true when err reports that a rewrite or clear was rolled back
// because it would have deleted a manual relationship to a connection
// outside the caller's visible set.
func (h *ClusterHandler) refuseHiddenRelationship(w http.ResponseWriter, r *http.Request, err error) bool {
	if !errors.Is(err, database.ErrHiddenRelationship) {
		return false
	}
	h.denialRecorder.refuse(w, r, hiddenRelationshipRefusal)
	return true
}

// restrictedVisibleSet resolves the caller's visible connection set for
// the relationship write routes. It returns a nil map for a caller with
// unrestricted visibility and a non-nil map (possibly empty) otherwise,
// as resolveVisibleConnectionSet does. On failure it writes a 500 and
// returns false.
func (h *ClusterHandler) restrictedVisibleSet(ctx context.Context, w http.ResponseWriter, clusterID int, failMsg string) (map[int]bool, bool) {
	visible, allConnections, err := h.resolveVisibleConnections(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to resolve visible connections for cluster %d relationships: %v", clusterID, err)
		RespondError(w, http.StatusInternalServerError, failMsg)
		return nil, false
	}
	if allConnections {
		return nil, true
	}
	return visible, true
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
	// Issue #207: topology relationships are a system-wide concern with
	// no per-object owner; apply the plain admin gate.
	if !h.rbacChecker.HasAdminPermission(r.Context(), auth.PermManageConnections) {
		h.denialRecorder.refuse(w, r,
			"Permission denied: requires manage_connections permission")
		return
	}

	// Setting a source's relationships first deletes every manual
	// relationship from it, whatever the target, so the token must
	// cover every connection rather than only those the request names.
	if !requireAllConnectionsInTokenScope(w, r, h.rbacChecker, h.denialRecorder) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Issue #678: the admin permission is not enough on its own; a
	// restricted caller must also see the source, and, checked by
	// SetNodeRelationships below, every connection whose relationship
	// the rewrite would delete. This runs before the body is decoded so
	// a hidden source discloses nothing.
	visible, ok := h.restrictedVisibleSet(ctx, w, clusterID, "Failed to validate relationships")
	if !ok || !h.visibleSourceInCluster(ctx, w, clusterID, connID, visible) {
		return
	}

	var req SetRelationshipsRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

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

		// A target the caller cannot see is refused exactly as a target
		// outside the cluster is, so the response does not disclose
		// whether the hidden connection is a member.
		if visible != nil && !visible[rel.TargetConnectionID] {
			RespondError(w, http.StatusBadRequest,
				"Target connection does not belong to this cluster")
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
	if err := h.datastore.SetNodeRelationships(ctx, clusterID, connID, req.Relationships, visible); err != nil {
		if h.refuseHiddenRelationship(w, r, err) {
			return
		}
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
			// Insert only: replacing the target's manual relationships
			// would delete its other rows, including any to connections
			// the caller cannot see (issue #678).
			reverseRel := database.RelationshipInput{
				TargetConnectionID: connID, RelationshipType: "replicates_with",
			}
			if err := h.datastore.AddNodeRelationship(ctx, clusterID, rel.TargetConnectionID, reverseRel); err != nil {
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

	if visible != nil {
		relationships = filterRelationshipsByVisibility(relationships, visible)
	}

	if relationships == nil {
		relationships = []database.NodeRelationship{}
	}

	RespondJSON(w, http.StatusOK, relationships)
}

// handleDeleteRelationship handles DELETE
// /api/v1/clusters/{id}/relationships/{relationshipId}
func (h *ClusterHandler) handleDeleteRelationship(w http.ResponseWriter, r *http.Request, clusterID int, relationshipID int) {
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", "DELETE")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Issue #207: topology relationships are a system-wide concern with
	// no per-object owner; apply the plain admin gate.
	if !h.rbacChecker.HasAdminPermission(r.Context(), auth.PermManageConnections) {
		h.denialRecorder.refuse(w, r,
			"Permission denied: requires manage_connections permission")
		return
	}

	// A relationship addressed by id could join any two connections, so
	// deleting one needs a token that covers every connection.
	if !requireAllConnectionsInTokenScope(w, r, h.rbacChecker, h.denialRecorder) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Issue #678: a restricted caller may delete only a relationship in
	// the addressed cluster whose both ends they can see. Anything else
	// is answered as a missing relationship, so hidden rows stay hidden.
	visible, ok := h.restrictedVisibleSet(ctx, w, clusterID, "Failed to delete relationship")
	if !ok {
		return
	}
	if visible != nil {
		rels, err := h.datastore.GetClusterRelationships(ctx, clusterID)
		if err != nil {
			log.Printf("[ERROR] Failed to read relationships (cluster=%d): %v", clusterID, err)
			RespondError(w, http.StatusInternalServerError, "Failed to delete relationship")
			return
		}
		found := false
		for _, rel := range filterRelationshipsByVisibility(rels, visible) {
			if rel.ID == relationshipID {
				found = true
				break
			}
		}
		if !found {
			RespondError(w, http.StatusNotFound, "Relationship not found")
			return
		}
	}

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
	// Issue #207: topology relationships are a system-wide concern with
	// no per-object owner; apply the plain admin gate.
	if !h.rbacChecker.HasAdminPermission(r.Context(), auth.PermManageConnections) {
		h.denialRecorder.refuse(w, r,
			"Permission denied: requires manage_connections permission")
		return
	}

	// Clearing deletes every manual relationship from the source,
	// whatever the target, so the token must cover every connection.
	if !requireAllConnectionsInTokenScope(w, r, h.rbacChecker, h.denialRecorder) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Issue #678: a restricted caller must see the source and, checked
	// by ClearNodeRelationships, every target whose manual relationship
	// the clear would delete.
	visible, ok := h.restrictedVisibleSet(ctx, w, clusterID, "Failed to clear relationships")
	if !ok || !h.visibleSourceInCluster(ctx, w, clusterID, connID, visible) {
		return
	}

	if err := h.datastore.ClearNodeRelationships(ctx, clusterID, connID, visible); err != nil {
		if h.refuseHiddenRelationship(w, r, err) {
			return
		}
		log.Printf("[ERROR] Failed to clear relationships (cluster=%d, conn=%d): %v", clusterID, connID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to clear relationships")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
