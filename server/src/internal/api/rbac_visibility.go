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
func resolveVisibleConnectionSet(ctx context.Context, rbac *auth.RBACChecker, ds *database.Datastore) (map[int]bool, bool, error) {
	lister := newConnectionVisibilityLister(ds)
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

// scopeVisibleToCaller reports whether the caller may see the given
// scope and writes a 404 response when they cannot. The caller should
// return immediately when this function returns false. The scope
// argument must be one of "server", "cluster", or "group".
func scopeVisibleToCaller(ctx context.Context, w http.ResponseWriter, rbac *auth.RBACChecker, ds *database.Datastore, scope string, scopeID int, notFoundMsg string) bool {
	visible, allConnections, err := resolveVisibleConnectionSet(ctx, rbac, ds)
	if err != nil {
		log.Printf("[ERROR] Failed to resolve visible connections for %s scope %d: %v", scope, scopeID, err)
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
	default:
		RespondError(w, http.StatusBadRequest, "Invalid scope")
		return false
	}
	if err != nil {
		log.Printf("[ERROR] Failed to check %s scope %d visibility: %v", scope, scopeID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to check scope visibility")
		return false
	}
	if !ok {
		RespondError(w, http.StatusNotFound, notFoundMsg)
		return false
	}
	return true
}

// filterGroupsByVisibilityFn drops cluster groups whose clusters contain
// no visible member connections. It issues one datastore lookup per
// group via GetConnectionIDsForGroup.
func filterGroupsByVisibilityFn(ctx context.Context, ds rbacVisibilityDatastore, groups []database.ClusterGroup, visible map[int]bool) ([]database.ClusterGroup, error) {
	out := make([]database.ClusterGroup, 0, len(groups))
	for i := range groups {
		ok, err := groupHasVisibleConnectionFn(ctx, ds, groups[i].ID, visible)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, groups[i])
		}
	}
	return out, nil
}
