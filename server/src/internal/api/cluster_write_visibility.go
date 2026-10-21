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

	"github.com/jackc/pgx/v5"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// requireVisibleTargetGroup answers and returns false unless the
// cluster group exists and the caller can see it, so that a cluster is
// never created in, or moved into, a group the caller cannot see (issue
// #673). A hidden group gets the same 404 as a missing one, and both
// run the same queries.
//
// A group whose clusters have no member connections is visible only to
// its owner and to callers who see every connection, as getClusterGroup
// hides it from everyone else; the owner exception lets a caller fill a
// group they have just created. The owner is matched on the context
// username, which an API token carries as its owner's, so a token is
// treated as its owner. That is safe because every caller of this
// helper first runs requireAllConnectionsInTokenScope, so no narrowed
// token reaches it.
//
// The default group is always writable, as a cluster created without a
// group lands there (see handleCreateCluster).
func (h *ClusterHandler) requireVisibleTargetGroup(ctx context.Context,
	w http.ResponseWriter, r *http.Request, groupID int, failMsg string) bool {

	visible, allConnections, err := h.resolveVisibleConnections(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to resolve visible connections for cluster group %d: %v", groupID, err)
		RespondError(w, http.StatusInternalServerError, failMsg)
		return false
	}
	var members []int
	defaultID := 0
	if !allConnections {
		members, err = h.datastore.GetConnectionIDsForGroup(ctx, groupID)
		if err == nil {
			defaultID, err = h.datastore.GetDefaultGroupID(ctx)
		}
		if err != nil {
			log.Printf("[ERROR] Failed to check cluster group visibility (id=%d): %v", groupID, err)
			RespondError(w, http.StatusInternalServerError, failMsg)
			return false
		}
	}
	group, err := h.datastore.GetClusterGroup(ctx, groupID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			RespondError(w, http.StatusNotFound, "Cluster group not found")
			return false
		}
		log.Printf("[ERROR] Failed to load cluster group (id=%d): %v", groupID, err)
		RespondError(w, http.StatusInternalServerError, failMsg)
		return false
	}
	if allConnections || group.ID == defaultID {
		return true
	}

	v := clusterGroupVisibility{visible: visible, members: members}
	if v.hidden(ownsClusterGroup(auth.GetUsernameFromContext(r.Context()), group)) {
		RespondError(w, http.StatusNotFound, "Cluster group not found")
		return false
	}
	return true
}

// requireVisibleAutoDetectedCluster answers 404 and returns false when a
// caller who does not see every connection cannot see the auto-detected
// cluster with the given topology ID (issue #673). A server-{id} cluster
// is visible when its root connection is, and a cluster-spock-{prefix}
// cluster when at least one connection assigned to it is. A missing
// cluster gets the same answer as a hidden one. When autoKey is set it
// must name the same cluster, so that a visible topology ID cannot carry
// the key of a hidden cluster.
func (h *ClusterHandler) requireVisibleAutoDetectedCluster(ctx context.Context,
	w http.ResponseWriter, clusterID, autoKey, failMsg string) bool {

	visible, allConnections, err := h.resolveVisibleConnections(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to resolve visible connections for an auto-detected cluster: %v", err)
		RespondError(w, http.StatusInternalServerError, failMsg)
		return false
	}
	if allConnections {
		return true
	}

	seen := false
	if idStr, ok := strings.CutPrefix(clusterID, "server-"); ok {
		id, convErr := strconv.Atoi(idStr)
		keyMatches := autoKey == "" || autoKey == "binary:"+idStr ||
			autoKey == "standalone:"+idStr || autoKey == "logical:"+idStr
		seen = convErr == nil && keyMatches && visible[id]
	} else if prefix, ok := strings.CutPrefix(clusterID, "cluster-spock-"); ok {
		key := "spock:" + prefix
		members, err := h.datastore.GetConnectionIDsForAutoClusterKey(ctx, key)
		if err != nil {
			log.Printf("[ERROR] Failed to check auto-detected cluster visibility: %v", err)
			RespondError(w, http.StatusInternalServerError, failMsg)
			return false
		}
		seen = (autoKey == "" || autoKey == key) &&
			clusterMembersVisible(members, visible)
	}
	if !seen {
		RespondError(w, http.StatusNotFound, "Cluster not found")
		return false
	}
	return true
}

// requireClusterMoveMembersVisible refuses, for a restricted caller, a
// change of the cluster's group unless the caller can see every member
// of the cluster, as moving it changes which group overrides reach each
// member (issue #716). The cluster's old and new groups need no such
// check: only the moved cluster's members are affected. A request that
// names the group the cluster is already in changes nothing, which is
// what the client sends when it renames a cluster, so it passes.
func (h *ClusterHandler) requireClusterMoveMembersVisible(ctx context.Context,
	w http.ResponseWriter, r *http.Request, clusterID, groupID int,
	visible map[int]bool) bool {

	cluster, err := h.datastore.GetCluster(ctx, clusterID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			RespondError(w, http.StatusNotFound, "Cluster not found")
			return false
		}
		log.Printf("[ERROR] Failed to load cluster (id=%d): %v", clusterID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to update cluster")
		return false
	}
	if cluster.GroupID.Valid && int(cluster.GroupID.Int32) == groupID {
		return true
	}
	return checkScopeMembersVisible(w, r, h.datastore, h.denialRecorder,
		auth.PermManageConnections, "cluster", clusterID, visible)
}

// requireAutoClusterMembersVisible refuses a write to the auto-detected
// clusters with the given keys unless the caller can see every
// connection assigned to them (issue #716). It runs after
// requireVisibleAutoDetectedCluster, so the caller already sees the
// cluster; the refusal is the manage_connections refusal, audited, and
// says nothing about which members are hidden. A caller with
// unrestricted visibility always passes.
func (h *ClusterHandler) requireAutoClusterMembersVisible(ctx context.Context,
	w http.ResponseWriter, r *http.Request, keys []string, failMsg string) bool {

	visible, allConnections, err := h.resolveVisibleConnections(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to resolve visible connections for an auto-detected cluster: %v", err)
		RespondError(w, http.StatusInternalServerError, failMsg)
		return false
	}
	if allConnections {
		return true
	}
	for _, key := range keys {
		members, err := h.datastore.GetConnectionIDsForAutoClusterKey(ctx, key)
		if err != nil {
			log.Printf("[ERROR] Failed to list auto-detected cluster members: %v", err)
			RespondError(w, http.StatusInternalServerError, failMsg)
			return false
		}
		if !allMembersVisible(members, visible) {
			h.denialRecorder.refuse(w, r,
				adminPermissionRefusal(auth.PermManageConnections))
			return false
		}
	}
	return true
}

// autoDetectedClusterKeys returns the auto_cluster_key values a topology
// ID may name: the one Spock key for cluster-spock-{prefix}, and for
// server-{id} the binary, standalone and logical keys, since the ID alone
// does not say which kind the topology builder created. Any other ID
// names no key.
func autoDetectedClusterKeys(clusterID string) []string {
	if prefix, ok := strings.CutPrefix(clusterID, "cluster-spock-"); ok {
		return []string{"spock:" + prefix}
	}
	if idStr, ok := strings.CutPrefix(clusterID, "server-"); ok {
		return []string{
			"binary:" + idStr,
			"standalone:" + idStr,
			"logical:" + idStr,
		}
	}
	return nil
}
