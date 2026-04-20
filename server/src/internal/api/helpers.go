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

// NewRBACCheckerWithSharing creates a new RBACChecker and configures the
// connection sharing lookup using the provided datastore. This ensures
// that CanAccessConnection respects the is_shared flag on connections.
func NewRBACCheckerWithSharing(authStore *auth.AuthStore, datastore *database.Datastore) *auth.RBACChecker {
	checker := auth.NewRBACChecker(authStore)
	if datastore != nil {
		ds := datastore
		checker.SetConnectionSharingLookup(func(ctx context.Context, connectionID int) (bool, string, error) {
			return ds.GetConnectionSharingInfo(ctx, connectionID)
		})
	}
	return checker
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

// connectionVisibilityLister adapts *database.Datastore.GetAllConnections
// to the auth.ConnectionVisibilityLister interface. A single call loads
// sharing metadata for every connection; this lets VisibleConnectionIDs
// compute the visible set without issuing one query per connection.
type connectionVisibilityLister struct {
	ds *database.Datastore
}

// newConnectionVisibilityLister returns a lister that wraps the given
// datastore. A nil datastore yields a nil lister; callers must handle
// that case.
func newConnectionVisibilityLister(ds *database.Datastore) auth.ConnectionVisibilityLister {
	if ds == nil {
		return nil
	}
	return &connectionVisibilityLister{ds: ds}
}

// GetAllConnections implements auth.ConnectionVisibilityLister by
// projecting database.ConnectionListItem into the minimal struct the
// auth package needs.
func (l *connectionVisibilityLister) GetAllConnections(ctx context.Context) ([]auth.ConnectionVisibilityInfo, error) {
	conns, err := l.ds.GetAllConnections(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]auth.ConnectionVisibilityInfo, 0, len(conns))
	for i := range conns {
		result = append(result, auth.ConnectionVisibilityInfo{
			ID:            conns[i].ID,
			IsShared:      conns[i].IsShared,
			OwnerUsername: conns[i].OwnerUsername,
		})
	}
	return result, nil
}
