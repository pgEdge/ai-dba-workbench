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
	"unicode/utf8"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// maxFieldLength is the maximum character length permitted for short
// string fields such as Name, Host, Maintenance Database, and Username.
// It matches the VARCHAR(255) constraint on the corresponding database
// columns, so callers can reject over-length input with a clear 400
// before the datastore call rather than surfacing a generic 500 when
// the database rejects the row.
const maxFieldLength = 255

// validateMaxLen enforces the maxFieldLength character-length ceiling on
// a single request field. When value exceeds limit it sends a 400
// response with a field-specific message and returns false; otherwise it
// returns true. Rune count (utf8.RuneCountInString) is used deliberately
// to mirror the backing VARCHAR(limit) column, which limits by characters
// rather than bytes.
func validateMaxLen(w http.ResponseWriter, fieldLabel, value string, limit int) bool {
	if utf8.RuneCountInString(value) > limit {
		RespondError(w, http.StatusBadRequest,
			fmt.Sprintf("%s must be %d characters or less", fieldLabel, limit))
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

// requireAdminPermission returns a function that checks whether the
// caller has the specified admin permission. If the check fails it
// audits the refusal through auditor's denial recorder, sends a 403
// response naming the permission and returns false; otherwise it
// returns true. The recorder is read on each refusal rather than when
// the gate is built, because the server installs it with
// SetDenialRecorder after the handler is constructed (issue #648). A
// nil auditor, or one with no recorder, still refuses but audits
// nothing, and a nil rbac denies every caller (issue #561).
func requireAdminPermission(rbac *auth.RBACChecker, auditor *denialAuditor,
	permission string) func(http.ResponseWriter, *http.Request) bool {

	reason := "Permission denied: requires " + permission + " permission"
	return func(w http.ResponseWriter, r *http.Request) bool {
		if rbac.HasAdminPermission(r.Context(), permission) {
			return true
		}
		var record denialRecorder
		if auditor != nil {
			record = auditor.denialRecorder
		}
		record.refuse(w, r, reason)
		return false
	}
}
