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
	"net/http"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// HandleNotConfigured returns an http.HandlerFunc that responds with a 503
// status indicating that the given service is unavailable because the
// datastore is not configured.
func HandleNotConfigured(service string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		RespondError(w, http.StatusServiceUnavailable,
			service+" is not available. The datastore is not configured.")
	}
}

// RequireAdminPermission returns a function that checks whether the caller
// has the specified admin permission. If the check fails it sends a 403
// response and returns false; otherwise it returns true.
func RequireAdminPermission(rbac *auth.RBACChecker, permission string, description string) func(http.ResponseWriter, *http.Request) bool {
	return func(w http.ResponseWriter, r *http.Request) bool {
		if !rbac.HasAdminPermission(r.Context(), permission) {
			RespondError(w, http.StatusForbidden,
				"Permission denied: you do not have permission to "+description)
			return false
		}
		return true
	}
}

// connectionSliceVisibilityLister adapts an already-loaded slice of
// database.ConnectionListItem to auth.ConnectionVisibilityLister. It
// lets callers that have already fetched the full connection list
// reuse that snapshot for visibility resolution, avoiding a second
// datastore read and eliminating the race where the filter set and
// the served slice come from different snapshots.
type connectionSliceVisibilityLister struct {
	connections []database.ConnectionListItem
}

// newConnectionSliceVisibilityLister returns a lister that projects
// the provided slice into auth.ConnectionVisibilityInfo values. The
// caller retains ownership of the slice; the lister does not modify
// it.
func newConnectionSliceVisibilityLister(connections []database.ConnectionListItem) auth.ConnectionVisibilityLister {
	return connectionSliceVisibilityLister{connections: connections}
}

// GetAllConnections implements auth.ConnectionVisibilityLister over
// the pre-loaded slice. It never returns an error because no I/O is
// performed; the context is accepted only to satisfy the interface.
func (l connectionSliceVisibilityLister) GetAllConnections(_ context.Context) ([]auth.ConnectionVisibilityInfo, error) {
	result := make([]auth.ConnectionVisibilityInfo, 0, len(l.connections))
	for i := range l.connections {
		result = append(result, auth.ConnectionVisibilityInfo{
			ID:            l.connections[i].ID,
			IsShared:      l.connections[i].IsShared,
			OwnerUsername: l.connections[i].OwnerUsername,
		})
	}
	return result, nil
}
