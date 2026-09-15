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

// This file carries the *ActorStore views of the privilege and admin
// permission mutations in privileges.go. Each method has the same
// signature and semantics as its *AuthStore namesake, and differs only
// in attributing the audit event to the store's actor rather than to
// the system actor.

// GrantMCPPrivilege grants an MCP privilege to a group.
func (a *ActorStore) GrantMCPPrivilege(groupID, privilegeID int64) error {
	return a.s.grantMCPPrivilege(a.actor, groupID, privilegeID)
}

// GrantMCPPrivilegeByName grants an MCP privilege to a group by
// identifier name.
func (a *ActorStore) GrantMCPPrivilegeByName(groupID int64, identifier string) error {
	return a.s.grantMCPPrivilegeByName(a.actor, groupID, identifier)
}

// RevokeMCPPrivilege revokes an MCP privilege from a group.
func (a *ActorStore) RevokeMCPPrivilege(groupID, privilegeID int64) error {
	return a.s.revokeMCPPrivilege(a.actor, groupID, privilegeID)
}

// RevokeMCPPrivilegeByName revokes an MCP privilege from a group by
// identifier name.
func (a *ActorStore) RevokeMCPPrivilegeByName(groupID int64, identifier string) error {
	return a.s.revokeMCPPrivilegeByName(a.actor, groupID, identifier)
}

// GrantConnectionPrivilege grants access to a database connection for a
// group.
func (a *ActorStore) GrantConnectionPrivilege(groupID int64, connectionID int, accessLevel string) error {
	return a.s.grantConnectionPrivilege(a.actor, groupID, connectionID, accessLevel)
}

// RevokeConnectionPrivilege revokes access to a database connection
// from a group.
func (a *ActorStore) RevokeConnectionPrivilege(groupID int64, connectionID int) error {
	return a.s.revokeConnectionPrivilege(a.actor, groupID, connectionID)
}

// GrantAdminPermission grants an admin permission to a group.
func (a *ActorStore) GrantAdminPermission(groupID int64, permission string) error {
	return a.s.grantAdminPermission(a.actor, groupID, permission)
}

// RevokeAdminPermission revokes an admin permission from a group.
func (a *ActorStore) RevokeAdminPermission(groupID int64, permission string) error {
	return a.s.revokeAdminPermission(a.actor, groupID, permission)
}
