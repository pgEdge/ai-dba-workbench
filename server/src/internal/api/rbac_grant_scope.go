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
)

// grantOutOfTokenScope is the refusal given when an API token would
// hand out access to a connection its own connection scope does not
// cover.
const grantOutOfTokenScope = "Permission denied: this token's connection scope does not cover the access being granted"

// requireGrantInTokenScope answers 403, recording the denial like the
// other RBAC refusals, and returns false unless inScope holds. The RBAC
// write handlers pass it the result of one of the RBACChecker grant
// checks, so that a token bounded to some connections can never grant
// a user, a group or another token access beyond them (issue #471).
// Session callers always pass those checks.
func (h *RBACHandler) requireGrantInTokenScope(w http.ResponseWriter,
	r *http.Request, inScope bool) bool {

	if inScope {
		return true
	}
	h.recordDenial(r, grantOutOfTokenScope)
	RespondError(w, http.StatusForbidden, grantOutOfTokenScope)
	return false
}

// ownerWithinTokenScope reports whether the named user's access falls
// inside the acting token's connection scope. The user is looked up
// only when the acting token is bounded, and an unknown user is out of
// scope, so that the check cannot pass on a name that resolves to
// nothing.
func (h *RBACHandler) ownerWithinTokenScope(ctx context.Context,
	username string) bool {

	if h.rbacChecker.AllConnectionsInTokenScope(ctx) {
		return true
	}
	user, err := h.authStore.GetUser(username)
	if err != nil || user == nil {
		return false
	}
	return h.rbacChecker.UserWithinTokenScope(ctx, user.ID)
}

// tokenWithinActorScope reports whether the token tokenID, once its
// connection scope is the given one, reaches no further than the acting
// token. A token's reach is its owner's access narrowed by its
// connection scope, so a non-empty scope must itself be grantable by
// the acting token, and an empty one, which leaves the token
// unrestricted, needs the owner's whole access to be.
func (h *RBACHandler) tokenWithinActorScope(ctx context.Context,
	tokenID int64, connections []auth.ScopedConnection) bool {

	if h.rbacChecker.AllConnectionsInTokenScope(ctx) {
		return true
	}
	if len(connections) > 0 {
		return h.rbacChecker.ScopedConnectionsInTokenScope(ctx, connections)
	}
	token, err := h.authStore.GetTokenByID(tokenID)
	if err != nil || token == nil {
		return false
	}
	return h.rbacChecker.UserWithinTokenScope(ctx, token.OwnerID)
}

// storedTokenConnections returns the connection scope already stored
// for a token, so that a scope change that leaves the connections alone
// is judged on the scope the token will keep. The second result is
// false when the scope cannot be read.
func (h *RBACHandler) storedTokenConnections(tokenID int64) ([]auth.ScopedConnection, bool) {
	scope, err := h.authStore.GetTokenScope(tokenID)
	if err != nil {
		return nil, false
	}
	if scope == nil {
		return nil, true
	}
	return scope.Connections, true
}

// groupPrivilegesInTokenScope reports whether every connection the
// group itself holds a grant on is in the acting token's connection
// scope at read_write. Deleting the group drops those grants, and
// dropping a connection's last group grant lifts its restriction, which
// opens a shared connection to every user.
func (h *RBACHandler) groupPrivilegesInTokenScope(ctx context.Context,
	groupID int64) bool {

	if h.rbacChecker.AllConnectionsInTokenScope(ctx) {
		return true
	}
	privs, err := h.authStore.ListGroupConnectionPrivileges(groupID)
	if err != nil {
		return false
	}
	for _, cp := range privs {
		if !h.rbacChecker.ConnectionInTokenScope(ctx, cp.ConnectionID) {
			return false
		}
	}
	return true
}
