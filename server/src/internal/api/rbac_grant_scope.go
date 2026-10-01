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
	"log"
	"net/http"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// grantOutOfTokenScope is the refusal given when an API token would
// hand out access its own scope does not cover, in connections, MCP
// items or admin permissions.
const grantOutOfTokenScope = "Permission denied: this token's scope does not cover the access being granted"

// requireGrantInTokenScope answers 403, recording the denial like the
// other RBAC refusals, and returns false unless inScope holds. The RBAC
// write handlers pass it the result of one of the RBACChecker grant
// checks, so that a token bounded in any scope kind can never grant a
// user, a group or another token access beyond it (issue #471).
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

// userWithinTokenScope reports whether everything the user reaches
// falls inside the acting token's scope, enumerating the unrestricted
// connections through the handler's connection lister.
func (h *RBACHandler) userWithinTokenScope(ctx context.Context,
	userID int64) bool {

	return h.rbacChecker.UserWithinTokenScope(ctx, userID, h.connLister)
}

// ownerWithinTokenScope reports whether a new token owned by the named
// user, which starts with no scope and so reaches all the user does,
// falls inside the acting token's scope. The user is looked up only
// when the acting token is bounded, and an unknown user is out of
// scope, so that the check cannot pass on a name that resolves to
// nothing.
func (h *RBACHandler) ownerWithinTokenScope(ctx context.Context,
	username string) bool {

	if h.rbacChecker.TokenScopeUnrestricted(ctx) {
		return true
	}
	user, err := h.authStore.GetUser(username)
	if err != nil || user == nil {
		return false
	}
	return h.userWithinTokenScope(ctx, user.ID)
}

// tokenWithinActorScope reports whether the token tokenID, once its
// scope is the given one, reaches no further than the acting token, in
// each of the three scope kinds.
func (h *RBACHandler) tokenWithinActorScope(ctx context.Context,
	tokenID int64, scope auth.GrantedTokenScope) bool {

	if h.rbacChecker.TokenScopeUnrestricted(ctx) {
		return true
	}
	token, err := h.authStore.GetTokenByID(tokenID)
	if err != nil || token == nil {
		return false
	}
	return h.rbacChecker.TokenScopeWithinTokenScope(ctx, token.OwnerID,
		scope, h.connLister)
}

// storedTokenScope returns the scope already stored for a token, in all
// three kinds, so that a scope change that leaves a kind alone is
// judged on what the token will keep in it. The second result is false
// when the scope cannot be read.
func (h *RBACHandler) storedTokenScope(tokenID int64) (auth.GrantedTokenScope, bool) {
	var stored auth.GrantedTokenScope
	scope, err := h.authStore.GetTokenScope(tokenID)
	if err != nil {
		return stored, false
	}
	if scope != nil {
		stored.Connections = scope.Connections
	}
	if stored.MCPPrivileges, err = h.authStore.GetTokenMCPScope(tokenID); err != nil {
		return stored, false
	}
	if stored.AdminPermissions, err = h.authStore.GetTokenAdminScope(tokenID); err != nil {
		return stored, false
	}
	return stored, true
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

// requireRenameInTokenScope refuses a group rename by a bounded token
// when the group's access reaches beyond the token's scope, or when the
// old or the new name is one the OIDC group map assigns federated users
// to. Federation finds a group by name, so either rename would steer
// federated users into access the token does not hold. A session, and a
// token unrestricted in every kind, may rename freely; a rename that
// keeps the current name is not a rename. A group that does not exist
// is left to the update to report.
func (h *RBACHandler) requireRenameInTokenScope(w http.ResponseWriter,
	r *http.Request, groupID int64, name string) bool {

	ctx := r.Context()
	if h.rbacChecker.TokenScopeUnrestricted(ctx) {
		return true
	}

	group, err := h.authStore.GetGroup(groupID)
	if err != nil {
		log.Printf("[ERROR] Failed to get group %d for rename check: %v", groupID, err)
		const reason = "Failed to get group"
		h.recordDenial(r, reason)
		RespondError(w, http.StatusInternalServerError, reason)
		return false
	}
	if group == nil || group.Name == name {
		return true
	}

	inScope := !h.federatedGroups[group.Name] && !h.federatedGroups[name] &&
		h.rbacChecker.GroupWithinTokenScope(ctx, groupID)
	return h.requireGrantInTokenScope(w, r, inScope)
}
