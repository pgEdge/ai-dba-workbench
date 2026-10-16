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

// requireVisibleTargetGroup writes a response and returns false unless
// the cluster group exists and the caller can see it, so that a cluster
// is never created in, or moved into, a group the caller cannot see
// (issue #673).
//
// A hidden group gets the same 404 as a missing one, and the caller's
// visibility and the group's members are loaded before the group itself,
// so both cases run the same queries. A missing group is a 404 for every
// caller, not the 500 the foreign key violation used to produce.
//
// A group with members is visible when at least one member connection is
// visible, as getClusterGroup decides. A group with no members is hidden
// from restricted callers by getClusterGroup; here it stays visible to
// its owner as well, so a caller can fill a group they have just
// created.
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
