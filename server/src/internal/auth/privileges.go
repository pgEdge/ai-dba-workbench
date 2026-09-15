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

import (
	"database/sql"
	"errors"
	"fmt"
)

// =============================================================================
// MCP Privilege Registration
// =============================================================================

// RegisterMCPPrivilege registers a new MCP privilege identifier (tool, resource, or prompt)
// The isPublic parameter indicates whether the privilege should be accessible without
// group membership. Public tools are accessible to any authenticated user.
func (s *AuthStore) RegisterMCPPrivilege(identifier, itemType, description string, isPublic bool) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate item type
	if itemType != MCPPrivilegeTypeTool && itemType != MCPPrivilegeTypeResource && itemType != MCPPrivilegeTypePrompt {
		return 0, fmt.Errorf("invalid item type: %s (must be 'tool', 'resource', or 'prompt')", itemType)
	}

	// Use INSERT OR IGNORE to handle duplicates gracefully
	result, err := s.db.Exec(
		`INSERT OR IGNORE INTO mcp_privilege_identifiers (identifier, item_type, description, is_public)
         VALUES (?, ?, ?, ?)`,
		identifier, itemType, description, isPublic,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to register MCP privilege: %w", err)
	}

	// If no rows were inserted (due to INSERT OR IGNORE), get the existing ID
	// and update the is_public flag (in case it changed)
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to check insert result: %w", err)
	}
	if rowsAffected == 0 {
		var existingID int64
		err := s.db.QueryRow(
			"SELECT id FROM mcp_privilege_identifiers WHERE identifier = ?",
			identifier,
		).Scan(&existingID)
		if err != nil {
			return 0, fmt.Errorf("failed to get existing privilege ID: %w", err)
		}
		// Update is_public flag in case it changed
		_, err = s.db.Exec(
			"UPDATE mcp_privilege_identifiers SET is_public = ? WHERE id = ?",
			isPublic, existingID,
		)
		if err != nil {
			return 0, fmt.Errorf("failed to update privilege is_public flag: %w", err)
		}
		return existingID, nil
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get privilege ID: %w", err)
	}

	return id, nil
}

// GetMCPPrivilege retrieves a privilege by identifier
func (s *AuthStore) GetMCPPrivilege(identifier string) (*MCPPrivilege, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var priv MCPPrivilege
	err := s.db.QueryRow(
		`SELECT id, identifier, item_type, description, is_public, created_at
         FROM mcp_privilege_identifiers WHERE identifier = ?`,
		identifier,
	).Scan(&priv.ID, &priv.Identifier, &priv.ItemType, &priv.Description, &priv.IsPublic, &priv.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get privilege: %w", err)
	}

	return &priv, nil
}

// GetMCPPrivilegeByID retrieves a privilege by ID
func (s *AuthStore) GetMCPPrivilegeByID(id int64) (*MCPPrivilege, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var priv MCPPrivilege
	err := s.db.QueryRow(
		`SELECT id, identifier, item_type, description, is_public, created_at
         FROM mcp_privilege_identifiers WHERE id = ?`,
		id,
	).Scan(&priv.ID, &priv.Identifier, &priv.ItemType, &priv.Description, &priv.IsPublic, &priv.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get privilege: %w", err)
	}

	return &priv, nil
}

// ListMCPPrivileges returns all registered MCP privileges
func (s *AuthStore) ListMCPPrivileges() ([]*MCPPrivilege, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT id, identifier, item_type, description, is_public, created_at
         FROM mcp_privilege_identifiers
         ORDER BY item_type, identifier`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list MCP privileges: %w", err)
	}
	defer rows.Close()

	var privileges []*MCPPrivilege
	for rows.Next() {
		var priv MCPPrivilege
		if err := rows.Scan(&priv.ID, &priv.Identifier, &priv.ItemType, &priv.Description, &priv.IsPublic, &priv.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan privilege: %w", err)
		}
		privileges = append(privileges, &priv)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating privileges: %w", err)
	}

	return privileges, nil
}

// ListMCPPrivilegesByType returns all privileges of a specific type
func (s *AuthStore) ListMCPPrivilegesByType(itemType string) ([]*MCPPrivilege, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT id, identifier, item_type, description, is_public, created_at
         FROM mcp_privilege_identifiers
         WHERE item_type = ?
         ORDER BY identifier`,
		itemType,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list MCP privileges by type: %w", err)
	}
	defer rows.Close()

	var privileges []*MCPPrivilege
	for rows.Next() {
		var priv MCPPrivilege
		if err := rows.Scan(&priv.ID, &priv.Identifier, &priv.ItemType, &priv.Description, &priv.IsPublic, &priv.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan privilege: %w", err)
		}
		privileges = append(privileges, &priv)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating privileges by type: %w", err)
	}

	return privileges, nil
}

// =============================================================================
// Privilege Audit Helpers
// =============================================================================

// Audit action names for the privilege and admin permission mutations
// recorded in this file.
const (
	auditActionPrivilegeMCPGrant         = "privilege.mcp.grant"
	auditActionPrivilegeMCPRevoke        = "privilege.mcp.revoke"
	auditActionPrivilegeConnectionGrant  = "privilege.connection.grant"
	auditActionPrivilegeConnectionRevoke = "privilege.connection.revoke"
	auditActionPermissionAdminGrant      = "permission.admin.grant"
	auditActionPermissionAdminRevoke     = "permission.admin.revoke"
)

// auditTargetGroup is the audit target type used throughout this file:
// a privilege or permission is always granted to, or revoked from, a
// group.
const auditTargetGroup = "group"

// groupAuditName reads a group's name inside the caller's transaction,
// for use as the audit target name. A missing group, or a failed read,
// yields the empty string so that the event still carries the group id.
func groupAuditName(tx *sql.Tx, groupID int64) string {
	var name string
	err := tx.QueryRow("SELECT name FROM user_groups WHERE id = ?",
		groupID).Scan(&name)
	if err != nil {
		return ""
	}

	return name
}

// mcpPrivilegeAuditName renders a privilege identifier for the audit
// details: "*" for the wildcard sentinel, the registered identifier
// otherwise, and the empty string when the id is not registered.
func mcpPrivilegeAuditName(tx *sql.Tx, privilegeID int64) string {
	if privilegeID == MCPPrivilegeIDWildcard {
		return "*"
	}

	var identifier string
	err := tx.QueryRow(
		"SELECT identifier FROM mcp_privilege_identifiers WHERE id = ?",
		privilegeID).Scan(&identifier)
	if err != nil {
		return ""
	}

	return identifier
}

// =============================================================================
// MCP Privilege Grants
// =============================================================================

// GrantMCPPrivilege grants an MCP privilege to a group, recording the
// change as the system actor.
func (s *AuthStore) GrantMCPPrivilege(groupID, privilegeID int64) error {
	return s.grantMCPPrivilege(systemActor, groupID, privilegeID)
}

// grantMCPPrivilege grants an MCP privilege to a group and records the
// audit event in the same transaction, so that the grant and its event
// commit together.
func (s *AuthStore) grantMCPPrivilege(actor Actor, groupID,
	privilegeID int64) (err error) {

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to grant MCP privilege: %w", err)
	}

	target := &auditTarget{
		action:     auditActionPrivilegeMCPGrant,
		targetType: auditTargetGroup,
		targetID:   &groupID,
	}
	defer func() {
		if err != nil {
			s.failAudit(tx, actor, target, err)
		}
	}()

	target.targetName = groupAuditName(tx, groupID)

	if _, err = tx.Exec(
		`INSERT OR IGNORE INTO group_mcp_privileges (group_id, privilege_identifier_id)
         VALUES (?, ?)`,
		groupID, privilegeID,
	); err != nil {
		return fmt.Errorf("failed to grant MCP privilege: %w", err)
	}

	if err = s.recordAudit(tx, newEvent(actor, auditActionPrivilegeMCPGrant,
		auditTargetGroup, &groupID, target.targetName, map[string]any{
			"privilege_id":   privilegeID,
			"privilege_name": mcpPrivilegeAuditName(tx, privilegeID),
		})); err != nil {
		return fmt.Errorf("failed to grant MCP privilege: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to grant MCP privilege: %w", err)
	}

	return nil
}

// GrantMCPPrivilegeByName grants an MCP privilege to a group by
// identifier name, recording the change as the system actor. When
// identifier is "*", it stores MCPPrivilegeIDWildcard (0) directly
// instead of looking up a privilege identifier row.
func (s *AuthStore) GrantMCPPrivilegeByName(groupID int64, identifier string) error {
	return s.grantMCPPrivilegeByName(systemActor, groupID, identifier)
}

// grantMCPPrivilegeByName resolves the identifier, grants the privilege
// and records the audit event in the same transaction.
func (s *AuthStore) grantMCPPrivilegeByName(actor Actor, groupID int64,
	identifier string) (err error) {

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to grant MCP privilege: %w", err)
	}

	target := &auditTarget{
		action:     auditActionPrivilegeMCPGrant,
		targetType: auditTargetGroup,
		targetID:   &groupID,
	}
	defer func() {
		if err != nil {
			s.failAudit(tx, actor, target, err)
		}
	}()

	target.targetName = groupAuditName(tx, groupID)

	var privilegeID int64
	if identifier == "*" {
		privilegeID = MCPPrivilegeIDWildcard
	} else {
		err = tx.QueryRow(
			"SELECT id FROM mcp_privilege_identifiers WHERE identifier = ?",
			identifier,
		).Scan(&privilegeID)
		if errors.Is(err, sql.ErrNoRows) {
			err = fmt.Errorf("privilege not found: %s", identifier)
			return err
		}
		if err != nil {
			return fmt.Errorf("failed to get privilege: %w", err)
		}
	}

	if _, err = tx.Exec(
		`INSERT OR IGNORE INTO group_mcp_privileges (group_id, privilege_identifier_id)
         VALUES (?, ?)`,
		groupID, privilegeID,
	); err != nil {
		return fmt.Errorf("failed to grant MCP privilege: %w", err)
	}

	// If granting "All MCP Privileges", remove individual privilege grants
	if identifier == "*" {
		if _, err = tx.Exec(
			"DELETE FROM group_mcp_privileges WHERE group_id = ? AND privilege_identifier_id != ?",
			groupID, MCPPrivilegeIDWildcard,
		); err != nil {
			return fmt.Errorf("failed to clean up individual MCP privileges: %w", err)
		}
	}

	if err = s.recordAudit(tx, newEvent(actor, auditActionPrivilegeMCPGrant,
		auditTargetGroup, &groupID, target.targetName, map[string]any{
			"privilege_id":   privilegeID,
			"privilege_name": identifier,
		})); err != nil {
		return fmt.Errorf("failed to grant MCP privilege: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to grant MCP privilege: %w", err)
	}

	return nil
}

// RevokeMCPPrivilege revokes an MCP privilege from a group, recording
// the change as the system actor.
func (s *AuthStore) RevokeMCPPrivilege(groupID, privilegeID int64) error {
	return s.revokeMCPPrivilege(systemActor, groupID, privilegeID)
}

// revokeMCPPrivilege revokes an MCP privilege and records the audit
// event in the same transaction.
func (s *AuthStore) revokeMCPPrivilege(actor Actor, groupID,
	privilegeID int64) (err error) {

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to revoke MCP privilege: %w", err)
	}

	target := &auditTarget{
		action:     auditActionPrivilegeMCPRevoke,
		targetType: auditTargetGroup,
		targetID:   &groupID,
	}
	defer func() {
		if err != nil {
			s.failAudit(tx, actor, target, err)
		}
	}()

	target.targetName = groupAuditName(tx, groupID)
	privilegeName := mcpPrivilegeAuditName(tx, privilegeID)

	result, err := tx.Exec(
		"DELETE FROM group_mcp_privileges WHERE group_id = ? AND privilege_identifier_id = ?",
		groupID, privilegeID,
	)
	if err != nil {
		return fmt.Errorf("failed to revoke MCP privilege: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		err = fmt.Errorf("privilege grant not found")
		return err
	}

	if err = s.recordAudit(tx, newEvent(actor, auditActionPrivilegeMCPRevoke,
		auditTargetGroup, &groupID, target.targetName, map[string]any{
			"privilege_id":   privilegeID,
			"privilege_name": privilegeName,
		})); err != nil {
		return fmt.Errorf("failed to revoke MCP privilege: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to revoke MCP privilege: %w", err)
	}

	return nil
}

// RevokeMCPPrivilegeByName revokes an MCP privilege from a group by
// identifier name, recording the change as the system actor. When
// identifier is "*", it deletes the wildcard row
// (privilege_identifier_id = 0) directly instead of looking up a
// privilege identifier row.
func (s *AuthStore) RevokeMCPPrivilegeByName(groupID int64, identifier string) error {
	return s.revokeMCPPrivilegeByName(systemActor, groupID, identifier)
}

// revokeMCPPrivilegeByName revokes an MCP privilege by identifier name
// and records the audit event in the same transaction.
func (s *AuthStore) revokeMCPPrivilegeByName(actor Actor, groupID int64,
	identifier string) (err error) {

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to revoke MCP privilege: %w", err)
	}

	target := &auditTarget{
		action:     auditActionPrivilegeMCPRevoke,
		targetType: auditTargetGroup,
		targetID:   &groupID,
	}
	defer func() {
		if err != nil {
			s.failAudit(tx, actor, target, err)
		}
	}()

	target.targetName = groupAuditName(tx, groupID)

	var result sql.Result
	privilegeID := MCPPrivilegeIDWildcard
	if identifier == "*" {
		result, err = tx.Exec(
			"DELETE FROM group_mcp_privileges WHERE group_id = ? AND privilege_identifier_id = ?",
			groupID, MCPPrivilegeIDWildcard,
		)
	} else {
		// The identifier may no longer be registered, in which case the
		// delete below matches nothing and the caller sees the usual
		// not-found error.
		if scanErr := tx.QueryRow(
			"SELECT id FROM mcp_privilege_identifiers WHERE identifier = ?",
			identifier).Scan(&privilegeID); scanErr != nil {
			privilegeID = 0
		}
		result, err = tx.Exec(
			`DELETE FROM group_mcp_privileges
             WHERE group_id = ?
             AND privilege_identifier_id = (SELECT id FROM mcp_privilege_identifiers WHERE identifier = ?)`,
			groupID, identifier,
		)
	}
	if err != nil {
		return fmt.Errorf("failed to revoke MCP privilege: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		err = fmt.Errorf("privilege grant not found")
		return err
	}

	if err = s.recordAudit(tx, newEvent(actor, auditActionPrivilegeMCPRevoke,
		auditTargetGroup, &groupID, target.targetName, map[string]any{
			"privilege_id":   privilegeID,
			"privilege_name": identifier,
		})); err != nil {
		return fmt.Errorf("failed to revoke MCP privilege: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to revoke MCP privilege: %w", err)
	}

	return nil
}

// =============================================================================
// Connection Privilege Grants
// =============================================================================

// GrantConnectionPrivilege grants access to a database connection for a
// group, recording the change as the system actor.
func (s *AuthStore) GrantConnectionPrivilege(groupID int64, connectionID int, accessLevel string) error {
	return s.grantConnectionPrivilege(systemActor, groupID, connectionID,
		accessLevel)
}

// grantConnectionPrivilege grants connection access to a group and
// records the audit event in the same transaction.
func (s *AuthStore) grantConnectionPrivilege(actor Actor, groupID int64,
	connectionID int, accessLevel string) (err error) {

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to grant connection privilege: %w", err)
	}

	target := &auditTarget{
		action:     auditActionPrivilegeConnectionGrant,
		targetType: auditTargetGroup,
		targetID:   &groupID,
	}
	defer func() {
		if err != nil {
			s.failAudit(tx, actor, target, err)
		}
	}()

	target.targetName = groupAuditName(tx, groupID)

	// Validate access level
	if accessLevel != AccessLevelRead && accessLevel != AccessLevelReadWrite {
		err = fmt.Errorf("invalid access level: %s (must be 'read' or 'read_write')", accessLevel)
		return err
	}

	// Use INSERT OR REPLACE to update existing grants
	if _, err = tx.Exec(
		`INSERT OR REPLACE INTO connection_privileges (group_id, connection_id, access_level)
         VALUES (?, ?, ?)`,
		groupID, connectionID, accessLevel,
	); err != nil {
		return fmt.Errorf("failed to grant connection privilege: %w", err)
	}

	if err = s.recordAudit(tx, newEvent(actor,
		auditActionPrivilegeConnectionGrant, auditTargetGroup, &groupID,
		target.targetName, map[string]any{
			"connection_id": connectionID,
		})); err != nil {
		return fmt.Errorf("failed to grant connection privilege: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to grant connection privilege: %w", err)
	}

	return nil
}

// RevokeConnectionPrivilege revokes access to a database connection
// from a group, recording the change as the system actor.
func (s *AuthStore) RevokeConnectionPrivilege(groupID int64, connectionID int) error {
	return s.revokeConnectionPrivilege(systemActor, groupID, connectionID)
}

// revokeConnectionPrivilege revokes connection access from a group and
// records the audit event in the same transaction.
func (s *AuthStore) revokeConnectionPrivilege(actor Actor, groupID int64,
	connectionID int) (err error) {

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to revoke connection privilege: %w", err)
	}

	target := &auditTarget{
		action:     auditActionPrivilegeConnectionRevoke,
		targetType: auditTargetGroup,
		targetID:   &groupID,
	}
	defer func() {
		if err != nil {
			s.failAudit(tx, actor, target, err)
		}
	}()

	target.targetName = groupAuditName(tx, groupID)

	result, err := tx.Exec(
		"DELETE FROM connection_privileges WHERE group_id = ? AND connection_id = ?",
		groupID, connectionID,
	)
	if err != nil {
		return fmt.Errorf("failed to revoke connection privilege: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		err = fmt.Errorf("connection privilege not found")
		return err
	}

	if err = s.recordAudit(tx, newEvent(actor,
		auditActionPrivilegeConnectionRevoke, auditTargetGroup, &groupID,
		target.targetName, map[string]any{
			"connection_id": connectionID,
		})); err != nil {
		return fmt.Errorf("failed to revoke connection privilege: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to revoke connection privilege: %w", err)
	}

	return nil
}

// GetConnectionPrivilege gets the access level for a specific connection grant
func (s *AuthStore) GetConnectionPrivilege(groupID int64, connectionID int) (*ConnectionPrivilege, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var priv ConnectionPrivilege
	err := s.db.QueryRow(
		`SELECT id, group_id, connection_id, access_level, created_at
         FROM connection_privileges
         WHERE group_id = ? AND connection_id = ?`,
		groupID, connectionID,
	).Scan(&priv.ID, &priv.GroupID, &priv.ConnectionID, &priv.AccessLevel, &priv.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get connection privilege: %w", err)
	}

	return &priv, nil
}

// =============================================================================
// Group Privilege Queries
// =============================================================================

// ListGroupMCPPrivileges returns all MCP privileges granted to a group.
// If the group has the wildcard grant (privilege_identifier_id = 0), a
// synthetic MCPPrivilege with Identifier "*" is prepended to the result.
func (s *AuthStore) ListGroupMCPPrivileges(groupID int64) ([]*MCPPrivilege, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT mpi.id, mpi.identifier, mpi.item_type, mpi.description, mpi.is_public, mpi.created_at
         FROM mcp_privilege_identifiers mpi
         JOIN group_mcp_privileges gmp ON mpi.id = gmp.privilege_identifier_id
         WHERE gmp.group_id = ?
         ORDER BY mpi.item_type, mpi.identifier`,
		groupID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list group MCP privileges: %w", err)
	}
	defer rows.Close()

	var privileges []*MCPPrivilege
	for rows.Next() {
		var priv MCPPrivilege
		if err := rows.Scan(&priv.ID, &priv.Identifier, &priv.ItemType, &priv.Description, &priv.IsPublic, &priv.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan privilege: %w", err)
		}
		privileges = append(privileges, &priv)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating group MCP privileges: %w", err)
	}

	// Check for wildcard (All MCP Privileges) grant
	var wildcardCount int
	//nolint:errcheck // Best effort; zero on error is acceptable
	s.db.QueryRow(
		"SELECT COUNT(*) FROM group_mcp_privileges WHERE group_id = ? AND privilege_identifier_id = ?",
		groupID, MCPPrivilegeIDWildcard,
	).Scan(&wildcardCount)
	if wildcardCount > 0 {
		privileges = append([]*MCPPrivilege{{Identifier: "*"}}, privileges...)
	}

	return privileges, nil
}

// ListGroupConnectionPrivileges returns all connection privileges granted to a group
func (s *AuthStore) ListGroupConnectionPrivileges(groupID int64) ([]*ConnectionPrivilege, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT id, group_id, connection_id, access_level, created_at
         FROM connection_privileges
         WHERE group_id = ?
         ORDER BY connection_id`,
		groupID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list group connection privileges: %w", err)
	}
	defer rows.Close()

	var privileges []*ConnectionPrivilege
	for rows.Next() {
		var priv ConnectionPrivilege
		if err := rows.Scan(&priv.ID, &priv.GroupID, &priv.ConnectionID, &priv.AccessLevel, &priv.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan privilege: %w", err)
		}
		privileges = append(privileges, &priv)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating group connection privileges: %w", err)
	}

	return privileges, nil
}

// GetGroupWithPrivileges returns a group with all its assigned privileges
func (s *AuthStore) GetGroupWithPrivileges(groupID int64) (*GroupWithPrivileges, error) {
	// Get the group
	group, err := s.GetGroup(groupID)
	if err != nil {
		return nil, err
	}
	if group == nil {
		return nil, fmt.Errorf("group not found: %d", groupID)
	}

	result := &GroupWithPrivileges{Group: *group}

	// Get MCP privileges
	mcpPrivs, err := s.ListGroupMCPPrivileges(groupID)
	if err != nil {
		return nil, err
	}
	for _, p := range mcpPrivs {
		result.MCPPrivileges = append(result.MCPPrivileges, *p)
	}

	// Get connection privileges
	connPrivs, err := s.ListGroupConnectionPrivileges(groupID)
	if err != nil {
		return nil, err
	}
	for _, p := range connPrivs {
		result.ConnectionPrivileges = append(result.ConnectionPrivileges, *p)
	}

	return result, nil
}

// =============================================================================
// Privilege Lookup for Authorization
// =============================================================================

// IsPrivilegeAssignedToAnyGroup checks if a privilege has been assigned to any group
// This determines if the privilege is "restricted" (requires authorization) or "public".
// A privilege is considered restricted when it is explicitly granted to a group OR
// when any group has the wildcard MCP privilege (privilege_identifier_id = 0),
// which means all MCP items are restricted.
func (s *AuthStore) IsPrivilegeAssignedToAnyGroup(identifier string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	err := s.db.QueryRow(`
        SELECT COUNT(*)
        FROM group_mcp_privileges gmp
        LEFT JOIN mcp_privilege_identifiers mpi ON gmp.privilege_identifier_id = mpi.id
        WHERE mpi.identifier = ? OR gmp.privilege_identifier_id = 0
    `, identifier).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check privilege assignment: %w", err)
	}

	return count > 0, nil
}

// IsPrivilegePublic checks if a privilege is marked as public (accessible without group membership).
// Returns (isPublic, isRegistered, error).
// - If the privilege is not registered, returns (false, false, nil).
// - If the privilege is registered and public, returns (true, true, nil).
// - If the privilege is registered and not public, returns (false, true, nil).
func (s *AuthStore) IsPrivilegePublic(identifier string) (isPublic bool, isRegistered bool, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var publicFlag bool
	err = s.db.QueryRow(
		"SELECT is_public FROM mcp_privilege_identifiers WHERE identifier = ?",
		identifier,
	).Scan(&publicFlag)

	if err == sql.ErrNoRows {
		return false, false, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("failed to check privilege public status: %w", err)
	}

	return publicFlag, true, nil
}

// GetUserMCPPrivileges returns all MCP privilege identifiers accessible to a user (through all groups)
func (s *AuthStore) GetUserMCPPrivileges(userID int64) (map[string]bool, error) {
	// Get all groups the user belongs to
	groupIDs, err := s.GetUserGroups(userID)
	if err != nil {
		return nil, err
	}

	if len(groupIDs) == 0 {
		return map[string]bool{}, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Build query with group IDs
	privileges := make(map[string]bool)

	for _, groupID := range groupIDs {
		rows, err := s.db.Query(`
            SELECT mpi.identifier
            FROM mcp_privilege_identifiers mpi
            JOIN group_mcp_privileges gmp ON mpi.id = gmp.privilege_identifier_id
            WHERE gmp.group_id = ?
        `, groupID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user privileges: %w", err)
		}

		for rows.Next() {
			var identifier string
			if err := rows.Scan(&identifier); err != nil {
				rows.Close()
				return nil, fmt.Errorf("failed to scan privilege: %w", err)
			}
			privileges[identifier] = true
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("error iterating user privileges: %w", err)
		}
		rows.Close()

		// Check for wildcard MCP privilege (privilege_identifier_id = 0)
		var wildcardCount int
		err = s.db.QueryRow(
			"SELECT COUNT(*) FROM group_mcp_privileges WHERE group_id = ? AND privilege_identifier_id = ?",
			groupID, MCPPrivilegeIDWildcard,
		).Scan(&wildcardCount)
		if err == nil && wildcardCount > 0 {
			privileges["*"] = true
		}
	}

	return privileges, nil
}

// GetUserConnectionPrivileges returns all connection privileges accessible to a user (through all groups)
// Returns a map of connection_id -> access_level (highest level wins: read_write > read)
func (s *AuthStore) GetUserConnectionPrivileges(userID int64) (map[int]string, error) {
	// Get all groups the user belongs to
	groupIDs, err := s.GetUserGroups(userID)
	if err != nil {
		return nil, err
	}

	if len(groupIDs) == 0 {
		return map[int]string{}, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Build query with group IDs
	privileges := make(map[int]string)

	for _, groupID := range groupIDs {
		rows, err := s.db.Query(`
            SELECT connection_id, access_level
            FROM connection_privileges
            WHERE group_id = ?
        `, groupID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user connection privileges: %w", err)
		}

		for rows.Next() {
			var connID int
			var accessLevel string
			if err := rows.Scan(&connID, &accessLevel); err != nil {
				rows.Close()
				return nil, fmt.Errorf("failed to scan privilege: %w", err)
			}

			// Take the highest access level (read_write > read)
			existing, exists := privileges[connID]
			if !exists || accessLevel == AccessLevelReadWrite {
				privileges[connID] = accessLevel
			} else if existing == AccessLevelRead && accessLevel == AccessLevelReadWrite {
				privileges[connID] = accessLevel
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("error iterating connection privileges: %w", err)
		}
		rows.Close()
	}

	return privileges, nil
}

// IsConnectionAssignedToAnyGroup checks if a connection has been assigned to any group
// This determines if the connection is "restricted" or "public"
func (s *AuthStore) IsConnectionAssignedToAnyGroup(connectionID int) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM connection_privileges WHERE connection_id = ? OR connection_id = 0",
		connectionID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check connection assignment: %w", err)
	}

	return count > 0, nil
}

// =============================================================================
// Admin Permission Grants
// =============================================================================

// GrantAdminPermission grants an admin permission to a group, recording
// the change as the system actor.
func (s *AuthStore) GrantAdminPermission(groupID int64, permission string) error {
	return s.grantAdminPermission(systemActor, groupID, permission)
}

// grantAdminPermission grants an admin permission to a group and records
// the audit event in the same transaction. Granting the wildcard removes
// the group's individual permissions, and the removed names are listed
// in the event details.
func (s *AuthStore) grantAdminPermission(actor Actor, groupID int64,
	permission string) (err error) {

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to grant admin permission: %w", err)
	}

	target := &auditTarget{
		action:     auditActionPermissionAdminGrant,
		targetType: auditTargetGroup,
		targetID:   &groupID,
	}
	defer func() {
		if err != nil {
			s.failAudit(tx, actor, target, err)
		}
	}()

	target.targetName = groupAuditName(tx, groupID)

	// Granting "All Admin Permissions" replaces the group's individual
	// grants, so the names about to be removed are read before anything
	// changes.
	var removed []string
	if permission == AdminPermissionWildcard {
		if removed, err = individualAdminPermissions(tx, groupID); err != nil {
			return fmt.Errorf("failed to list individual admin permissions: %w", err)
		}
	}

	if _, err = tx.Exec(
		`INSERT OR IGNORE INTO group_admin_permissions (group_id, permission)
         VALUES (?, ?)`,
		groupID, permission,
	); err != nil {
		return fmt.Errorf("failed to grant admin permission: %w", err)
	}

	details := map[string]any{"permission": permission}

	if permission == AdminPermissionWildcard {
		if _, err = tx.Exec(
			"DELETE FROM group_admin_permissions WHERE group_id = ? AND permission != ?",
			groupID, AdminPermissionWildcard,
		); err != nil {
			return fmt.Errorf("failed to clean up individual admin permissions: %w", err)
		}

		details["removed_individual"] = removed
	}

	if err = s.recordAudit(tx, newEvent(actor, auditActionPermissionAdminGrant,
		auditTargetGroup, &groupID, target.targetName, details)); err != nil {
		return fmt.Errorf("failed to grant admin permission: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to grant admin permission: %w", err)
	}

	return nil
}

// individualAdminPermissions lists a group's non-wildcard admin
// permissions inside the caller's transaction, so that the wildcard
// grant can record what it removed.
func individualAdminPermissions(tx *sql.Tx, groupID int64) ([]string, error) {
	rows, err := tx.Query(
		`SELECT permission FROM group_admin_permissions
         WHERE group_id = ? AND permission != ?
         ORDER BY permission`,
		groupID, AdminPermissionWildcard,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	permissions := []string{}
	for rows.Next() {
		var permission string
		if err := rows.Scan(&permission); err != nil {
			return nil, err
		}
		permissions = append(permissions, permission)
	}

	return permissions, rows.Err()
}

// RevokeAdminPermission revokes an admin permission from a group,
// recording the change as the system actor.
func (s *AuthStore) RevokeAdminPermission(groupID int64, permission string) error {
	return s.revokeAdminPermission(systemActor, groupID, permission)
}

// revokeAdminPermission revokes an admin permission from a group and
// records the audit event in the same transaction.
func (s *AuthStore) revokeAdminPermission(actor Actor, groupID int64,
	permission string) (err error) {

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to revoke admin permission: %w", err)
	}

	target := &auditTarget{
		action:     auditActionPermissionAdminRevoke,
		targetType: auditTargetGroup,
		targetID:   &groupID,
	}
	defer func() {
		if err != nil {
			s.failAudit(tx, actor, target, err)
		}
	}()

	target.targetName = groupAuditName(tx, groupID)

	result, err := tx.Exec(
		"DELETE FROM group_admin_permissions WHERE group_id = ? AND permission = ?",
		groupID, permission,
	)
	if err != nil {
		return fmt.Errorf("failed to revoke admin permission: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		err = fmt.Errorf("admin permission grant not found")
		return err
	}

	if err = s.recordAudit(tx, newEvent(actor, auditActionPermissionAdminRevoke,
		auditTargetGroup, &groupID, target.targetName, map[string]any{
			"permission": permission,
		})); err != nil {
		return fmt.Errorf("failed to revoke admin permission: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to revoke admin permission: %w", err)
	}

	return nil
}

// ListGroupAdminPermissions returns all admin permissions granted to a group
func (s *AuthStore) ListGroupAdminPermissions(groupID int64) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT permission FROM group_admin_permissions
         WHERE group_id = ?
         ORDER BY permission`,
		groupID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list group admin permissions: %w", err)
	}
	defer rows.Close()

	var permissions []string
	for rows.Next() {
		var perm string
		if err := rows.Scan(&perm); err != nil {
			return nil, fmt.Errorf("failed to scan permission: %w", err)
		}
		permissions = append(permissions, perm)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating admin permissions: %w", err)
	}

	return permissions, nil
}

// GetUserAdminPermissions returns all admin permissions for a user through all group memberships
func (s *AuthStore) GetUserAdminPermissions(userID int64) (map[string]bool, error) {
	// Get all groups the user belongs to (including nested groups)
	groupIDs, err := s.GetUserGroups(userID)
	if err != nil {
		return nil, err
	}

	if len(groupIDs) == 0 {
		return map[string]bool{}, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	permissions := make(map[string]bool)

	for _, groupID := range groupIDs {
		rows, err := s.db.Query(
			"SELECT permission FROM group_admin_permissions WHERE group_id = ?",
			groupID,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to get user admin permissions: %w", err)
		}

		for rows.Next() {
			var perm string
			if err := rows.Scan(&perm); err != nil {
				rows.Close()
				return nil, fmt.Errorf("failed to scan permission: %w", err)
			}
			permissions[perm] = true
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("error iterating admin permissions: %w", err)
		}
		rows.Close()
	}

	return permissions, nil
}

// =============================================================================
// Group Effective (Inherited) Privileges
// =============================================================================

// GetGroupEffectiveMCPPrivileges returns all MCP privilege identifiers accessible
// to a group through its own grants and all ancestor groups in the hierarchy.
// If any group in the hierarchy has the wildcard grant (privilege_identifier_id = 0),
// "*" is prepended to the result.
func (s *AuthStore) GetGroupEffectiveMCPPrivileges(groupID int64) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
        WITH RECURSIVE ancestor_groups AS (
            SELECT ? as group_id
            UNION
            SELECT gm.parent_group_id
            FROM group_memberships gm
            INNER JOIN ancestor_groups ag ON gm.member_group_id = ag.group_id
            WHERE gm.member_group_id IS NOT NULL
        )
        SELECT DISTINCT mpi.identifier
        FROM mcp_privilege_identifiers mpi
        JOIN group_mcp_privileges gmp ON mpi.id = gmp.privilege_identifier_id
        JOIN ancestor_groups ag ON gmp.group_id = ag.group_id
        ORDER BY mpi.identifier
    `

	rows, err := s.db.Query(query, groupID)
	if err != nil {
		return nil, fmt.Errorf("failed to get group effective MCP privileges: %w", err)
	}
	defer rows.Close()

	var privileges []string
	for rows.Next() {
		var identifier string
		if err := rows.Scan(&identifier); err != nil {
			return nil, fmt.Errorf("failed to scan privilege: %w", err)
		}
		privileges = append(privileges, identifier)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating group effective MCP privileges: %w", err)
	}

	// Check for wildcard (All MCP Privileges) in any ancestor group
	var wildcardCount int
	wildcardQuery := `
        WITH RECURSIVE ancestor_groups AS (
            SELECT ? as group_id
            UNION
            SELECT gm.parent_group_id
            FROM group_memberships gm
            INNER JOIN ancestor_groups ag ON gm.member_group_id = ag.group_id
            WHERE gm.member_group_id IS NOT NULL
        )
        SELECT COUNT(*)
        FROM group_mcp_privileges gmp
        JOIN ancestor_groups ag ON gmp.group_id = ag.group_id
        WHERE gmp.privilege_identifier_id = ?
    `
	//nolint:errcheck // Best effort; zero on error is acceptable
	s.db.QueryRow(wildcardQuery, groupID, MCPPrivilegeIDWildcard).Scan(&wildcardCount)
	if wildcardCount > 0 {
		privileges = append([]string{"*"}, privileges...)
	}

	return privileges, nil
}

// GetGroupEffectiveConnectionPrivileges returns all connection privileges accessible
// to a group through its own grants and all ancestor groups. For duplicate
// connection IDs, the highest access level wins (read_write > read).
func (s *AuthStore) GetGroupEffectiveConnectionPrivileges(groupID int64) ([]ConnectionPrivilege, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
        WITH RECURSIVE ancestor_groups AS (
            SELECT ? as group_id
            UNION
            SELECT gm.parent_group_id
            FROM group_memberships gm
            INNER JOIN ancestor_groups ag ON gm.member_group_id = ag.group_id
            WHERE gm.member_group_id IS NOT NULL
        )
        SELECT cp.connection_id, cp.access_level
        FROM connection_privileges cp
        JOIN ancestor_groups ag ON cp.group_id = ag.group_id
    `

	rows, err := s.db.Query(query, groupID)
	if err != nil {
		return nil, fmt.Errorf("failed to get group effective connection privileges: %w", err)
	}
	defer rows.Close()

	// Collect highest access level per connection
	connMap := make(map[int]string)
	for rows.Next() {
		var connID int
		var accessLevel string
		if err := rows.Scan(&connID, &accessLevel); err != nil {
			return nil, fmt.Errorf("failed to scan privilege: %w", err)
		}

		existing, exists := connMap[connID]
		if !exists || (existing == AccessLevelRead && accessLevel == AccessLevelReadWrite) {
			connMap[connID] = accessLevel
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating group effective connection privileges: %w", err)
	}

	var privileges []ConnectionPrivilege
	for connID, accessLevel := range connMap {
		privileges = append(privileges, ConnectionPrivilege{
			ConnectionID: connID,
			AccessLevel:  accessLevel,
		})
	}

	return privileges, nil
}

// GetGroupEffectiveAdminPermissions returns all admin permissions accessible
// to a group through its own grants and all ancestor groups in the hierarchy.
func (s *AuthStore) GetGroupEffectiveAdminPermissions(groupID int64) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
        WITH RECURSIVE ancestor_groups AS (
            SELECT ? as group_id
            UNION
            SELECT gm.parent_group_id
            FROM group_memberships gm
            INNER JOIN ancestor_groups ag ON gm.member_group_id = ag.group_id
            WHERE gm.member_group_id IS NOT NULL
        )
        SELECT DISTINCT gap.permission
        FROM group_admin_permissions gap
        JOIN ancestor_groups ag ON gap.group_id = ag.group_id
        ORDER BY gap.permission
    `

	rows, err := s.db.Query(query, groupID)
	if err != nil {
		return nil, fmt.Errorf("failed to get group effective admin permissions: %w", err)
	}
	defer rows.Close()

	var permissions []string
	for rows.Next() {
		var perm string
		if err := rows.Scan(&perm); err != nil {
			return nil, fmt.Errorf("failed to scan permission: %w", err)
		}
		permissions = append(permissions, perm)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating group effective admin permissions: %w", err)
	}

	return permissions, nil
}

// MCPPrivilegeCount returns the number of registered MCP privileges
func (s *AuthStore) MCPPrivilegeCount() int {
	var count int
	//nolint:errcheck // Returns 0 on error, which is acceptable
	s.db.QueryRow("SELECT COUNT(*) FROM mcp_privilege_identifiers").Scan(&count)
	return count
}
