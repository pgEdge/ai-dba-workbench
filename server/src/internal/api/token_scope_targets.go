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

// targetOutOfTokenScope is the refusal given when an API token's
// connection scope does not cover what a request would change.
const targetOutOfTokenScope = "Permission denied: this token's connection scope does not cover the target"

// targetInTokenScope reports whether the acting API token's connection
// scope admits a change to something attached at the given scope, as
// blackouts, blackout schedules and alert, probe and channel overrides
// are ("server", "cluster", "group" or "estate").
//
// Those handlers are gated on an admin permission, which says nothing
// about which connections a token was issued for (see issue #471). A
// target on one server needs that connection in scope at read_write; a
// cluster, group or estate-wide target reaches connections the token
// may not name, including ones added later, so it needs a scope that
// covers every connection. A server target with no connection id falls
// into the second case, so an unexpected shape cannot widen access.
// Sessions, and tokens with no connection scope, always pass.
func targetInTokenScope(ctx context.Context, rc *auth.RBACChecker,
	scope string, connectionID *int) bool {

	if scope == string(database.BlackoutScopeServer) && connectionID != nil {
		return rc.ConnectionInTokenScope(ctx, *connectionID)
	}
	return rc.AllConnectionsInTokenScope(ctx)
}

// requireTargetInTokenScope applies targetInTokenScope, answering 403
// and returning false when the target is out of scope.
func requireTargetInTokenScope(w http.ResponseWriter, r *http.Request,
	rc *auth.RBACChecker, scope string, connectionID *int) bool {

	if targetInTokenScope(r.Context(), rc, scope, connectionID) {
		return true
	}
	RespondError(w, http.StatusForbidden, targetOutOfTokenScope)
	return false
}

// blackoutTargetInTokenScope is targetInTokenScope for blackouts and
// blackout schedules. A blackout silences alerting, which is Workbench
// metadata rather than a change to the monitored server, so a read
// entry in the connection scope admits one on that server, and a
// cluster, group or estate-wide one needs every connection in scope at
// either level.
func blackoutTargetInTokenScope(ctx context.Context, rc *auth.RBACChecker,
	scope string, connectionID *int) bool {

	if scope == string(database.BlackoutScopeServer) && connectionID != nil {
		return rc.ConnectionReadableInTokenScope(ctx, *connectionID)
	}
	return rc.ConnectionReadableInTokenScope(ctx, auth.ConnectionIDAll)
}

// requireBlackoutTargetInTokenScope applies blackoutTargetInTokenScope,
// answering 403 and returning false when the target is out of scope.
func requireBlackoutTargetInTokenScope(w http.ResponseWriter, r *http.Request,
	rc *auth.RBACChecker, scope string, connectionID *int) bool {

	if blackoutTargetInTokenScope(r.Context(), rc, scope, connectionID) {
		return true
	}
	RespondError(w, http.StatusForbidden, targetOutOfTokenScope)
	return false
}

// denialRecorder writes a refused request to the audit log through the
// RBAC handler's coalescing denial recorder; the server wires it to
// RBACHandler.RecordDenial. A nil recorder audits nothing, which is the
// case when the server runs without an auth store.
type denialRecorder func(*http.Request, string)

// denialAuditor is embedded by the handlers whose token-scope refusals
// are audited, and holds the recorder they pass to the scope checks.
type denialAuditor struct {
	denialRecorder denialRecorder
}

// SetDenialRecorder installs the function used to audit refused
// requests; the server wires it to RBACHandler.RecordDenial.
func (a *denialAuditor) SetDenialRecorder(record func(*http.Request, string)) {
	a.denialRecorder = record
}

// refuse audits the refusal, when a recorder is set, and answers 403
// with reason.
func (record denialRecorder) refuse(w http.ResponseWriter, r *http.Request,
	reason string) {

	if record != nil {
		record(r, reason)
	}
	RespondError(w, http.StatusForbidden, reason)
}

// requireConnectionsInTokenScope answers 403, auditing the refusal
// through record, and returns false unless every listed connection is
// in the acting token's connection scope at read_write.
func requireConnectionsInTokenScope(w http.ResponseWriter, r *http.Request,
	rc *auth.RBACChecker, record denialRecorder, connectionIDs ...int) bool {

	for _, id := range connectionIDs {
		if !rc.ConnectionInTokenScope(r.Context(), id) {
			record.refuse(w, r, targetOutOfTokenScope)
			return false
		}
	}
	return true
}

// requireAllConnectionsInTokenScope answers 403, auditing the refusal
// through record, and returns false unless the acting token's
// connection scope covers every connection, which a change to a
// cluster's definition or topology needs.
func requireAllConnectionsInTokenScope(w http.ResponseWriter, r *http.Request,
	rc *auth.RBACChecker, record denialRecorder) bool {

	if rc.AllConnectionsInTokenScope(r.Context()) {
		return true
	}
	record.refuse(w, r, targetOutOfTokenScope)
	return false
}
