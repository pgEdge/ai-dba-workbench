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

// CanSeeSystemAlerts reports whether the caller may see system alerts:
// alerts the alerter raises about itself, such as anomaly detection
// degrading because an LLM provider keeps failing (GitHub issue #582). A system alert has no connection, so connection
// grants and a token's connection scope do not apply to it; anyone who
// can reach the alert endpoints or the alert MCP tools may see one.
//
// The check fails closed wherever the caller's identity is in doubt:
//
//   - a nil checker denies;
//   - an API-token context that lost its token ID denies, because
//     nothing about the token can be evaluated;
//   - a context with no user ID denies, as HasAdminPermission does;
//   - a token whose scope cannot be read denies, as VisibleConnectionIDs
//     does for connections.
//
// A checker with no auth store is the server's no-authentication mode,
// in which every other RBACChecker method grants full access, and this
// one follows suit.
func (rc *RBACChecker) CanSeeSystemAlerts(ctx context.Context) bool {
	if rc == nil {
		return false
	}
	if rc.authStore == nil {
		return true
	}
	if tokenContextIncomplete(ctx) {
		return false
	}
	if IsSuperuserFromContext(ctx) {
		return true
	}
	if GetUserIDFromContext(ctx) == 0 {
		return false
	}
	if tokenID := GetTokenIDFromContext(ctx); tokenID > 0 {
		if _, err := rc.authStore.GetTokenScope(tokenID); err != nil {
			return false
		}
	}
	return true
}

// CanManageSystemAlerts reports whether the caller may acknowledge or
// unacknowledge a system alert. Seeing one is open to anyone with alert
// access (CanSeeSystemAlerts), but a system alert describes the whole
// estate rather than one connection, so silencing it is reserved for a
// superuser or a holder of the manage_alert_rules admin permission.
//
// The decision is HasAdminPermission's, so it fails closed in the same
// places: a nil checker, an API-token context that lost its token ID, a
// context with no user ID, a permission lookup that errors and a token
// whose admin scope cannot be read all deny, and a token whose admin
// scope omits manage_alert_rules denies even when its owner holds the
// permission. A checker with no auth store is the no-authentication
// mode and allows.
func (rc *RBACChecker) CanManageSystemAlerts(ctx context.Context) bool {
	if rc == nil {
		return false
	}
	return rc.HasAdminPermission(ctx, PermManageAlertRules)
}
