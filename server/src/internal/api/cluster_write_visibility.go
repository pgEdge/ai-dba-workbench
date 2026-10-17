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
