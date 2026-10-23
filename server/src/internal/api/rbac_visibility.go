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
	"log"
	"net/http"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// rbacVisibilityDatastore is the narrow datastore contract required by
// the shared visibility helpers. The full *database.Datastore satisfies
// this interface; tests may substitute a lightweight fake.
type rbacVisibilityDatastore interface {
	GetConnectionIDsForCluster(ctx context.Context, clusterID int) ([]int, error)
	GetConnectionIDsForGroup(ctx context.Context, groupID int) ([]int, error)
}

// resolveVisibleConnectionSet returns the caller's visible connection-ID
// set. The second return value is true when the caller has unrestricted
// visibility (superuser or wildcard grant); callers should then skip
// filtering entirely. The visible map is nil when allConnections is
// true.
//
// A nil checker yields an empty visible set, never unrestricted
// visibility: RBACChecker's methods deny on a nil receiver (issue #561).
func resolveVisibleConnectionSet(ctx context.Context, rbac *auth.RBACChecker, ds *database.Datastore) (map[int]bool, bool, error) {
	lister := database.NewVisibilityLister(ds)
	ids, all, err := rbac.VisibleConnectionIDs(ctx, lister)
	if err != nil {
		return nil, false, err
	}
	if all {
		return nil, true, nil
	}
	visible := make(map[int]bool, len(ids))
	for _, id := range ids {
		visible[id] = true
	}
	return visible, false, nil
}

// clusterHasVisibleConnectionFn reports whether the given cluster has any
// member connection in the visible set. Empty clusters are treated as
// invisible: without a member connection there is no ownership or
// sharing claim the caller could satisfy.
func clusterHasVisibleConnectionFn(ctx context.Context, ds rbacVisibilityDatastore, clusterID int, visible map[int]bool) (bool, error) {
	ids, err := ds.GetConnectionIDsForCluster(ctx, clusterID)
	if err != nil {
		return false, err
	}
	return clusterMembersVisible(ids, visible), nil
}

// groupHasVisibleConnectionFn reports whether the given group contains
// at least one cluster whose member connections include a visible ID.
// See clusterHasVisibleConnectionFn for the empty-group contract.
func groupHasVisibleConnectionFn(ctx context.Context, ds rbacVisibilityDatastore, groupID int, visible map[int]bool) (bool, error) {
	ids, err := ds.GetConnectionIDsForGroup(ctx, groupID)
	if err != nil {
		return false, err
	}
	return clusterMembersVisible(ids, visible), nil
}

// clusterConnectionMembershipFn returns a map keyed by cluster ID whose
// values are the connection IDs assigned to each cluster. Clusters
// absent from the map have no connections. The implementation issues
// one datastore call per cluster.
//
// TODO(#35 follow-up): the per-cluster lookup is intentional at
// autocomplete scale (tens of clusters per caller); batch via the
// datastore if cluster count grows large enough to become a hot path.
func clusterConnectionMembershipFn(ctx context.Context, ds rbacVisibilityDatastore, clusterIDs []int) (map[int][]int, error) {
	out := make(map[int][]int, len(clusterIDs))
	for _, id := range clusterIDs {
		ids, err := ds.GetConnectionIDsForCluster(ctx, id)
		if err != nil {
			return nil, err
		}
		out[id] = ids
	}
	return out, nil
}

// filterClustersByVisibility returns the clusters that have at least one
// member connection in the visible set. It is used only for restricted
// callers; unrestricted callers skip filtering entirely.
func filterClustersByVisibility(ctx context.Context, ds rbacVisibilityDatastore, clusters []database.ClusterSummary, visible map[int]bool) ([]database.ClusterSummary, error) {
	ids := make([]int, 0, len(clusters))
	for i := range clusters {
		ids = append(ids, clusters[i].ID)
	}
	membership, err := clusterConnectionMembershipFn(ctx, ds, ids)
	if err != nil {
		return nil, err
	}
	filtered := make([]database.ClusterSummary, 0, len(clusters))
	for i := range clusters {
		if clusterMembersVisible(membership[clusters[i].ID], visible) {
			filtered = append(filtered, clusters[i])
		}
	}
	return filtered, nil
}

// scopeVisibleToCaller reports whether the caller may see the given
// scope and writes a 404 response when they cannot. The caller should
// return immediately when this function returns false. The scope
// argument must be one of "server", "cluster", or "group".
func scopeVisibleToCaller(ctx context.Context, w http.ResponseWriter, rbac *auth.RBACChecker, ds *database.Datastore, scope string, scopeID int, notFoundMsg string) bool {
	// Validate the scope string before any visibility shortcut so that
	// unrestricted callers (superuser or wildcard grant) cannot bypass
	// the documented "server" | "cluster" | "group" contract by passing
	// garbage. Restricted callers hit the same check, keeping the 400
	// response consistent across caller types.
	switch scope {
	case "server", "cluster", "group":
	default:
		RespondError(w, http.StatusBadRequest, "Invalid scope")
		return false
	}

	visible, allConnections, err := resolveVisibleConnectionSet(ctx, rbac, ds)
	if err != nil {
		log.Printf("[ERROR] Failed to resolve visible connections for %s scope %d: %v", scope, scopeID, err) //nolint:gosec // G706: scope validated against allowlist; scopeID is an integer
		RespondError(w, http.StatusInternalServerError, "Failed to check scope visibility")
		return false
	}
	if allConnections {
		return true
	}

	var ok bool
	switch scope {
	case "server":
		ok = visible[scopeID]
	case "cluster":
		ok, err = clusterHasVisibleConnectionFn(ctx, ds, scopeID, visible)
	case "group":
		ok, err = groupHasVisibleConnectionFn(ctx, ds, scopeID, visible)
	}
	if err != nil {
		log.Printf("[ERROR] Failed to check %s scope %d visibility: %v", scope, scopeID, err) //nolint:gosec // G706: scope validated against allowlist; scopeID is an integer
		RespondError(w, http.StatusInternalServerError, "Failed to check scope visibility")
		return false
	}
	if !ok {
		RespondError(w, http.StatusNotFound, notFoundMsg)
		return false
	}
	return true
}

// requireScopeMembersVisible refuses a write at cluster or group scope
// unless the caller can see every member connection of that cluster or
// group (issue #716). An override written there applies to every member,
// so seeing one member is not enough to change what the others do. It
// writes the response and returns false when the caller may not
// proceed. A server scope, and a caller with unrestricted visibility
// (superuser or wildcard grant), always pass.
//
// It runs after scopeVisibleToCaller, so a cluster or group with no
// visible member, or none at all, has already answered 404. The refusal
// is the admin-permission refusal for permission, audited through
// record, so that it says nothing about how many members are hidden or
// which; a caller learns no more than that the write is not theirs to
// make.
func requireScopeMembersVisible(w http.ResponseWriter, r *http.Request,
	rbac *auth.RBACChecker, ds *database.Datastore, record denialRecorder,
	permission, scope string, scopeID int) bool {

	if scope != "cluster" && scope != "group" {
		return true
	}
	visible, all, err := resolveVisibleConnectionSet(r.Context(), rbac, ds)
	if err != nil {
		log.Printf("[ERROR] Failed to resolve visible connections for %s scope %d: %v", scope, scopeID, err) //nolint:gosec // G706: scope validated against allowlist; scopeID is an integer
		RespondError(w, http.StatusInternalServerError, "Failed to check scope visibility")
		return false
	}
	if all {
		return true
	}
	return checkScopeMembersVisible(w, r, ds, record, permission, scope, scopeID, visible)
}

// checkScopeMembersVisible is requireScopeMembersVisible for a
// restricted caller whose visible set is already resolved. scope must be
// "cluster" or "group".
func checkScopeMembersVisible(w http.ResponseWriter, r *http.Request,
	ds rbacVisibilityDatastore, record denialRecorder, permission,
	scope string, scopeID int, visible map[int]bool) bool {

	var members []int
	var err error
	if scope == "cluster" {
		members, err = ds.GetConnectionIDsForCluster(r.Context(), scopeID)
	} else {
		members, err = ds.GetConnectionIDsForGroup(r.Context(), scopeID)
	}
	if err != nil {
		log.Printf("[ERROR] Failed to list members of %s scope %d: %v", scope, scopeID, err) //nolint:gosec // G706: scope validated against allowlist; scopeID is an integer
		RespondError(w, http.StatusInternalServerError, "Failed to check scope visibility")
		return false
	}
	if !allMembersVisible(members, visible) {
		record.refuse(w, r, adminPermissionRefusal(permission))
		return false
	}
	return true
}

// allMembersVisible reports whether every listed connection is in the
// visible set. An empty list is vacuously visible; the callers' earlier
// visibility check is what refuses an empty cluster or group.
func allMembersVisible(members []int, visible map[int]bool) bool {
	for _, id := range members {
		if !visible[id] {
			return false
		}
	}
	return true
}

// filterGroupsByVisibilityFn drops cluster groups whose clusters contain
// no visible member connections, keeping a group with no member
// connections only when owner names its owner (issue #705), which is the
// rule getClusterGroup applies to one group. An empty owner keeps no
// empty group. It issues one datastore lookup per group via
// GetConnectionIDsForGroup.
//
// TODO(#35 follow-up): the per-group lookup is intentional at
// autocomplete scale (tens of groups per caller); batch via the
// datastore if group count grows large enough to become a hot path.
func filterGroupsByVisibilityFn(ctx context.Context, ds rbacVisibilityDatastore, groups []database.ClusterGroup, visible map[int]bool, owner string) ([]database.ClusterGroup, error) {
	out := make([]database.ClusterGroup, 0, len(groups))
	for i := range groups {
		members, err := ds.GetConnectionIDsForGroup(ctx, groups[i].ID)
		if err != nil {
			return nil, err
		}
		v := clusterGroupVisibility{visible: visible, members: members}
		if !v.hidden(ownsClusterGroup(owner, &groups[i])) {
			out = append(out, groups[i])
		}
	}
	return out, nil
}
