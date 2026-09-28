/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package auth

import "context"

// The checks in this file bound what an API token may hand out. An
// admin permission such as manage_permissions or manage_users says
// nothing about which connections a token was issued for, so without
// them a token scoped to one connection could grant a group access to
// every connection, create a superuser, or take over an account that
// reaches further than the token does (issue #471). Each check passes
// for a session caller, which has no token, and for a token whose
// connection scope covers every connection at read_write; otherwise the
// access being granted must fall inside the acting token's own scope.
// Like the other scope checks they fail closed: a token context missing
// its id, or a lookup that fails, is out of scope.

// CanGrantConnectionInTokenScope reports whether the acting API token's
// connection scope admits connectionID at the given access level, so
// that the token may grant that level on that connection to someone
// else. ConnectionIDAll asks about every connection, which only a token
// with no connection scope, or the wildcard at that level, covers.
func (rc *RBACChecker) CanGrantConnectionInTokenScope(ctx context.Context,
	connectionID int, level string) bool {

	if rc.authStore == nil {
		return true
	}
	if tokenContextIncomplete(ctx) {
		return false
	}
	if !knownAccessLevel(level) || level == AccessLevelNone {
		return false
	}
	inScope, granted := rc.applyConnectionTokenScope(ctx, connectionID, level)
	return inScope && granted == level
}

// ConnectionReadableInTokenScope reports whether the acting API token's
// connection scope names connectionID at any access level, read
// included. Writes that change only Workbench metadata about a
// connection, such as its blackouts, need no more than read access to
// the monitored server, so they use this in place of
// ConnectionInTokenScope. ConnectionIDAll asks about every connection,
// which only a token with no connection scope, or the wildcard at
// either level, covers.
func (rc *RBACChecker) ConnectionReadableInTokenScope(ctx context.Context,
	connectionID int) bool {

	return rc.CanGrantConnectionInTokenScope(ctx, connectionID, AccessLevelRead)
}

// connectionGrantsInTokenScope reports whether every connection
// privilege in privs could be granted by the acting token.
func (rc *RBACChecker) connectionGrantsInTokenScope(ctx context.Context,
	privs map[int]string) bool {

	for connectionID, level := range privs {
		if !rc.CanGrantConnectionInTokenScope(ctx, connectionID, level) {
			return false
		}
	}
	return true
}

// UserWithinTokenScope reports whether everything the given user can
// reach falls inside the acting API token's connection scope. It
// decides whether the token may change what is effectively the user's
// access, such as setting the user's password, re-enabling the account
// or minting a token that acts as the user.
//
// A superuser reaches every connection, and an admin permission acts
// across the whole estate, so a user holding either is only within a
// token that covers every connection. Otherwise each of the user's
// group connection privileges must be grantable by the token.
func (rc *RBACChecker) UserWithinTokenScope(ctx context.Context,
	userID int64) bool {

	// This also admits a session caller, and a checker with no store.
	if rc.AllConnectionsInTokenScope(ctx) {
		return true
	}

	user, err := rc.authStore.GetUserByID(userID)
	if err != nil || user == nil || user.IsSuperuser {
		return false
	}

	adminPerms, err := rc.authStore.GetUserAdminPermissions(userID)
	if err != nil || len(adminPerms) > 0 {
		return false
	}

	privs, err := rc.authStore.GetUserConnectionPrivileges(userID)
	if err != nil {
		return false
	}
	return rc.connectionGrantsInTokenScope(ctx, privs)
}

// GroupWithinTokenScope reports whether everything membership of the
// given group confers, through its own grants and those of every group
// it belongs to, falls inside the acting API token's connection scope.
// It decides whether the token may add a user or a group to the group.
// As for UserWithinTokenScope, an admin permission reaches the whole
// estate and so needs a token that covers every connection.
func (rc *RBACChecker) GroupWithinTokenScope(ctx context.Context,
	groupID int64) bool {

	// This also admits a session caller, and a checker with no store.
	if rc.AllConnectionsInTokenScope(ctx) {
		return true
	}

	adminPerms, err := rc.authStore.GetGroupEffectiveAdminPermissions(groupID)
	if err != nil || len(adminPerms) > 0 {
		return false
	}

	connPrivs, err := rc.authStore.GetGroupEffectiveConnectionPrivileges(groupID)
	if err != nil {
		return false
	}
	privs := make(map[int]string, len(connPrivs))
	for _, cp := range connPrivs {
		privs[cp.ConnectionID] = cp.AccessLevel
	}
	return rc.connectionGrantsInTokenScope(ctx, privs)
}

// ScopedConnectionsInTokenScope reports whether every entry of a
// connection scope being written to another token could be granted by
// the acting token, so that the token being changed ends up no wider
// than the one changing it.
func (rc *RBACChecker) ScopedConnectionsInTokenScope(ctx context.Context,
	connections []ScopedConnection) bool {

	for _, sc := range connections {
		if !rc.CanGrantConnectionInTokenScope(ctx, sc.ConnectionID, sc.AccessLevel) {
			return false
		}
	}
	return true
}
