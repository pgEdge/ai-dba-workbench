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
	"fmt"
	"net/http"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// maxFieldLength is the maximum byte length permitted for short string
// fields such as Name, Host, Maintenance Database, and Username. It
// matches the VARCHAR(255) constraint on the corresponding database
// columns, so callers can reject over-length input with a clear 400
// before the datastore call rather than surfacing a generic 500 when
// the database rejects the row.
const maxFieldLength = 255

// validateMaxLen enforces the maxFieldLength byte-length ceiling on a
// single request field. When value exceeds max it sends a 400 response
// with a field-specific message and returns false; otherwise it returns
// true. Byte length (len) is used deliberately to mirror the backing
// VARCHAR(max) column behaviour.
func validateMaxLen(w http.ResponseWriter, fieldLabel, value string, max int) bool {
	if len(value) > max {
		RespondError(w, http.StatusBadRequest,
			fmt.Sprintf("%s must be %d characters or less", fieldLabel, max))
		return false
	}
	return true
}

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
