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

// The refusals given when an API token would hand out, or take away,
// access beyond its own (issue #471). Each is a fixed string, so that
// the denial audit coalesces repeats and no refusal names a connection
// or other object the token may not see. Every one says what exceeded
// "this token's access".
const (
	refuseConnectionGrant  = "Permission denied: the connection access being granted exceeds this token's access"
	refuseConnectionRevoke = "Permission denied: removing access to a connection needs read access to it, " +
		"and removing its last grant needs read_write, within this token's access"
	refuseMCPGrant   = "Permission denied: the MCP privilege being granted is outside this token's access"
	refuseAdminGrant = "Permission denied: the admin permission being granted exceeds this token's access; " +
		"granting one also needs every connection at read_write, and granting one that manages users, " +
		"groups, permissions or token scopes needs every MCP privilege and admin permission too"
	refuseGroupAccess  = "Permission denied: the group's access exceeds this token's access"
	refuseGroupRemoval = "Permission denied: removing the group's access needs read access to each of its " +
		"connections, and read_write on any whose last grant it holds, within this token's access"
	refuseUserAccess    = "Permission denied: the user's access exceeds this token's access"
	refuseNewUserAccess = "Permission denied: the new user's access would exceed this token's access"
	refuseSuperuser     = "Permission denied: making a superuser needs a superuser's token with unrestricted " +
		"scope, which exceeds this token's access"
	refuseTokenOwner = "Permission denied: the token owner's access exceeds this token's access"
	refuseTokenScope = "Permission denied: the token scope change grants access that exceeds this token's access"
	refuseFederated  = "Permission denied: changing a group the OIDC group map names needs a superuser's " +
		"token with unrestricted scope, which exceeds this token's access"
)

// requireGrantInTokenScope answers 403 with reason, recording the
// denial like the other RBAC refusals, and returns false unless inScope
// holds. The RBAC write handlers pass it the result of one of the
// RBACChecker grant checks, so that a token can never grant a user, a
// group or another token access beyond its own (issue #471). Session
// callers always pass those checks.
func (h *RBACHandler) requireGrantInTokenScope(w http.ResponseWriter,
	r *http.Request, inScope bool, reason string) bool {

	if inScope {
		return true
	}
	h.recordDenial(r, reason)
	RespondError(w, http.StatusForbidden, reason)
	return false
}

// userWithinTokenScope reports whether everything the user reaches
// lies within the acting token's access, enumerating the unrestricted
// connections through the handler's connection lister.
func (h *RBACHandler) userWithinTokenScope(ctx context.Context,
	userID int64) bool {

	return h.rbacChecker.UserWithinTokenScope(ctx, userID, h.connLister)
}

// ownerWithinTokenScope reports whether a new token owned by the named
// user, which starts with no scope and so reaches all the user does,
// lies within the acting token's access. The user is looked up only
// when the acting token does not already hold everything, and an
// unknown user is refused, so that the check cannot pass on a name that
// resolves to nothing.
func (h *RBACHandler) ownerWithinTokenScope(ctx context.Context,
	username string) bool {

	if h.rbacChecker.TokenHoldsEverything(ctx) {
		return true
	}
	user, err := h.authStore.GetUser(username)
	if err != nil || user == nil {
		return false
	}
	return h.userWithinTokenScope(ctx, user.ID)
}

// requireTokenScopeChange refuses a change to token tokenID's scope that
// would grant beyond the acting token's access, answering 500, and
// recording the denial, when the stored scope cannot be read to judge
// it. With clearAll set the change lifts every restriction, as DELETE on
// the scope does.
func (h *RBACHandler) requireTokenScopeChange(w http.ResponseWriter,
	r *http.Request, tokenID int64, change auth.TokenScopeChange,
	clearAll bool) bool {

	ctx := r.Context()
	if h.rbacChecker.TokenHoldsEverything(ctx) {
		return true
	}
	var allowed bool
	var err error
	if clearAll {
		allowed, err = h.rbacChecker.TokenScopeClearWithinCeiling(ctx, tokenID,
			h.connLister)
	} else {
		allowed, err = h.rbacChecker.TokenScopeChangeWithinCeiling(ctx, tokenID,
			change)
	}
	if err != nil {
		log.Printf("[ERROR] Failed to read scope for token %d: %v", tokenID, err)
		// The change is refused, so it is recorded as a denial like the
		// 403s around it.
		const reason = "Failed to get token scope"
		h.recordDenial(r, reason)
		RespondError(w, http.StatusInternalServerError, reason)
		return false
	}
	return h.requireGrantInTokenScope(w, r, allowed, refuseTokenScope)
}

// requireRenameInTokenScope refuses a group rename by a token when the
// group's access reaches beyond the token's own, or when the old or the
// new name is one the OIDC group map assigns federated users to.
// Federation finds a group by name, so either rename would steer
// federated users into access the token does not hold. A session, and a
// token that holds everything a superuser does (see
// TokenHoldsEverything), may rename freely; a rename that
// keeps the current name is not a rename. A group that does not exist
// is left to the update to report.
func (h *RBACHandler) requireRenameInTokenScope(w http.ResponseWriter,
	r *http.Request, groupID int64, name string) bool {

	ctx := r.Context()
	if h.rbacChecker.TokenHoldsEverything(ctx) {
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

	if h.federatedGroups[group.Name] || h.federatedGroups[name] {
		return h.requireGrantInTokenScope(w, r, false, refuseFederated)
	}
	return h.requireGrantInTokenScope(w, r,
		h.rbacChecker.GroupWithinTokenScope(ctx, groupID), refuseGroupAccess)
}

// federatedNameInTokenScope reports whether the acting caller may create
// a group called name. Federation assigns federated users to a group by
// name, so creating a group the OIDC group map names would put those
// users into a group the token then shapes, so only a token that holds
// everything a superuser does may do it. Any other name, and a session,
// pass.
func (h *RBACHandler) federatedNameInTokenScope(ctx context.Context,
	name string) bool {

	return !h.federatedGroups[name] || h.rbacChecker.TokenHoldsEverything(ctx)
}

// requireGroupDeleteInTokenScope refuses a token that does not hold
// everything a superuser does deleting a group the OIDC group map names, for the same reason as a rename of
// one: federated users are matched to it by name, so deleting it, and
// recreating it later, moves them between groups. The group is looked
// up only when a federated name could be at stake, and a group that
// does not exist is left to the delete to report.
func (h *RBACHandler) requireGroupDeleteInTokenScope(w http.ResponseWriter,
	r *http.Request, groupID int64) bool {

	if len(h.federatedGroups) == 0 ||
		h.rbacChecker.TokenHoldsEverything(r.Context()) {
		return true
	}

	group, err := h.authStore.GetGroup(groupID)
	if err != nil {
		log.Printf("[ERROR] Failed to get group %d for delete check: %v", groupID, err)
		const reason = "Failed to get group"
		h.recordDenial(r, reason)
		RespondError(w, http.StatusInternalServerError, reason)
		return false
	}
	if group == nil {
		return true
	}
	return h.requireGrantInTokenScope(w, r, !h.federatedGroups[group.Name],
		refuseFederated)
}
