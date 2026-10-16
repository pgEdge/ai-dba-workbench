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

	"github.com/jackc/pgx/v5"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// requireVisibleTargetGroup answers and returns false unless the
// cluster group exists and the caller can see it, so that a cluster is
// never created in, or moved into, a group the caller cannot see (issue
// #673). A hidden group gets the same 404 as a missing one, and both
// run the same queries. A group whose clusters have no member
// connections is visible only to its owner and to callers who see every
// connection, as getClusterGroup hides it from everyone else; the owner
// exception lets a caller fill a group they have just created.
func (h *ClusterHandler) requireVisibleTargetGroup(ctx context.Context,
	w http.ResponseWriter, r *http.Request, groupID int, failMsg string) bool {

	visible, allConnections, err := h.resolveVisibleConnections(ctx)
	if err != nil {
		log.Printf("[ERROR] Failed to resolve visible connections for cluster group %d: %v", groupID, err)
		RespondError(w, http.StatusInternalServerError, failMsg)
		return false
	}
	var members []int
	if !allConnections {
		members, err = h.datastore.GetConnectionIDsForGroup(ctx, groupID)
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
	if allConnections {
		return true
	}

	username := auth.GetUsernameFromContext(r.Context())
	isOwner := username != "" && group.OwnerUsername.Valid &&
		group.OwnerUsername.String == username
	if membersHidden(members, visible) || (len(members) == 0 && !isOwner) {
		RespondError(w, http.StatusNotFound, "Cluster group not found")
		return false
	}
	return true
}
