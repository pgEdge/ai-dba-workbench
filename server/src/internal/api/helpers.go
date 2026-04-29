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
	"net/http"

	"github.com/pgedge/ai-workbench/server/internal/auth"
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
