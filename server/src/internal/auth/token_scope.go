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

// ErrUnknownMCPPrivilege is returned when an MCP scope names a privilege
// identifier that is not registered. Writing the rest of the list would
// silently drop the unknown name, and a list made up only of unknown
// names would then store no scope at all, which reads as unrestricted,
// so the whole change is refused instead.
var ErrUnknownMCPPrivilege = errors.New("unknown MCP privilege identifier")

// =============================================================================
// Token Scope Management
// =============================================================================

// SetTokenConnectionScope sets the connection scope for a token.
// If connections is empty, clears all connection scoping (token has no connection restrictions).
// The change is attributed to the system actor.
func (s *AuthStore) SetTokenConnectionScope(tokenID int64, connections []ScopedConnection) error {
	return s.setTokenConnectionScope(systemActor, tokenID, connections)
}

func (s *AuthStore) setTokenConnectionScope(actor Actor, tokenID int64,
	connections []ScopedConnection) (err error) {

	// The stored access level is checked here rather than left to the
	// SQLite CHECK constraint, so that a bad level is reported as what
	// it is instead of an opaque insert failure, and so that the rule
	// is visible to a reader of this code.
	for _, conn := range connections {
		if conn.AccessLevel != AccessLevelRead &&
			conn.AccessLevel != AccessLevelReadWrite {
			return fmt.Errorf(
				"invalid access level %q for connection %d: must be %q or %q",
				conn.AccessLevel, conn.ConnectionID, AccessLevelRead,
				AccessLevelReadWrite)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, target, err := s.beginTokenScopeChange(actor, tokenID,
		"token.scope.set_connections")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			s.failAudit(tx, actor, target, err)
		}
	}()

	before, err := tokenConnectionScopeTx(tx, tokenID)
	if err != nil {
		return err
	}

	// Clear existing scope
	if _, execErr := tx.Exec(
		"DELETE FROM token_connection_scope WHERE token_id = ?", tokenID,
	); execErr != nil {
		err = fmt.Errorf("failed to clear token connection scope: %w", execErr)
		return err
	}

	// Add new scope entries
	for _, conn := range connections {
		if _, execErr := tx.Exec(
			"INSERT INTO token_connection_scope (token_id, connection_id, access_level) VALUES (?, ?, ?)",
			tokenID, conn.ConnectionID, conn.AccessLevel,
		); execErr != nil {
			err = fmt.Errorf("failed to add connection to token scope: %w", execErr)
			return err
		}
	}

	after, err := tokenConnectionScopeTx(tx, tokenID)
	if err != nil {
		return err
	}

	return s.commitTokenScopeChange(tx, actor, target,
		map[string]any{"before": before, "after": after})
}

// beginTokenScopeChange opens the transaction shared by every token
// scope mutation and builds its audit target. The token's annotation
// labels the event when the row can be read; a token that does not
// exist is not an error, because these methods have never required one.
// The caller must hold s.mu.
func (s *AuthStore) beginTokenScopeChange(actor Actor, tokenID int64,
	action string) (*sql.Tx, *auditTarget, error) {

	tx, err := s.db.Begin()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	id := tokenID
	target := &auditTarget{
		action:     action,
		targetType: "token",
		targetID:   &id,
	}
	if annotation, ok := tokenAnnotationTx(tx, tokenID); ok {
		target.targetName = annotation
	}

	return tx, target, nil
}

// commitTokenScopeChange records the scope event and commits, so that
// the change and the event that describes it land together. A commit
// error is returned as the driver reports it, which is what
// SetTokenAdminScope has always done.
func (s *AuthStore) commitTokenScopeChange(tx *sql.Tx, actor Actor,
	target *auditTarget, details map[string]any) error {

	if err := s.recordAudit(tx, newEvent(actor, target.action, target.targetType,
		target.targetID, target.targetName, details)); err != nil {
		return err
	}

	return tx.Commit()
}

// SetTokenMCPScope sets the MCP privilege scope for a token
// If privilegeIDs is empty, clears all MCP scoping (token has no MCP restrictions)
// The change is attributed to the system actor.
func (s *AuthStore) SetTokenMCPScope(tokenID int64, privilegeIDs []int64) error {
	return s.setTokenMCPScope(systemActor, tokenID, privilegeIDs)
}

func (s *AuthStore) setTokenMCPScope(actor Actor, tokenID int64,
	privilegeIDs []int64) (err error) {

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, target, err := s.beginTokenScopeChange(actor, tokenID,
		"token.scope.set_tools")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			s.failAudit(tx, actor, target, err)
		}
	}()

	before, err := tokenMCPScopeIDsTx(tx, tokenID)
	if err != nil {
		return err
	}

	// Clear existing scope
	if _, execErr := tx.Exec(
		"DELETE FROM token_mcp_scope WHERE token_id = ?", tokenID,
	); execErr != nil {
		err = fmt.Errorf("failed to clear token MCP scope: %w", execErr)
		return err
	}

	// Add new scope entries
	for _, privID := range privilegeIDs {
		if _, execErr := tx.Exec(
			"INSERT INTO token_mcp_scope (token_id, privilege_identifier_id) VALUES (?, ?)",
			tokenID, privID,
		); execErr != nil {
			err = fmt.Errorf("failed to add privilege to token scope: %w", execErr)
			return err
		}
	}

	after, err := tokenMCPScopeIDsTx(tx, tokenID)
	if err != nil {
		return err
	}

	return s.commitTokenScopeChange(tx, actor, target,
		map[string]any{"before": before, "after": after})
}

// MCPPrivilegeIDWildcard is the sentinel privilege_identifier_id stored in
// token_mcp_scope to represent a wildcard ("all MCP privileges") grant.
const MCPPrivilegeIDWildcard int64 = 0

// SetTokenMCPScopeByNames sets the MCP privilege scope for a token using privilege identifiers.
// If identifiers contains "*", a single wildcard entry is stored instead of
// looking up individual privilege IDs. The change is attributed to the
// system actor.
func (s *AuthStore) SetTokenMCPScopeByNames(tokenID int64, identifiers []string) error {
	return s.setTokenMCPScopeByNames(systemActor, tokenID, identifiers)
}

func (s *AuthStore) setTokenMCPScopeByNames(actor Actor, tokenID int64,
	identifiers []string) (err error) {

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, target, err := s.beginTokenScopeChange(actor, tokenID,
		"token.scope.set_tools")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			s.failAudit(tx, actor, target, err)
		}
	}()

	before, err := tokenMCPScopeNamesTx(tx, tokenID)
	if err != nil {
		return err
	}

	// Clear existing scope
	if _, execErr := tx.Exec(
		"DELETE FROM token_mcp_scope WHERE token_id = ?", tokenID,
	); execErr != nil {
		err = fmt.Errorf("failed to clear token MCP scope: %w", execErr)
		return err
	}

	// Add new scope entries
	for _, identifier := range identifiers {
		if identifier == mcpWildcardIdentifier {
			// Store wildcard sentinel (privilege_identifier_id = 0)
			if _, execErr := tx.Exec(
				"INSERT INTO token_mcp_scope (token_id, privilege_identifier_id) VALUES (?, ?)",
				tokenID, MCPPrivilegeIDWildcard,
			); execErr != nil {
				err = fmt.Errorf("failed to add wildcard privilege to token scope: %w", execErr)
				return err
			}
			// Wildcard covers everything; skip remaining identifiers
			break
		}

		res, execErr := tx.Exec(
			`INSERT INTO token_mcp_scope (token_id, privilege_identifier_id)
             SELECT ?, id FROM mcp_privilege_identifiers WHERE identifier = ?`,
			tokenID, identifier,
		)
		if execErr != nil {
			err = fmt.Errorf("failed to add privilege to token scope: %w", execErr)
			return err
		}
		inserted, rowsErr := res.RowsAffected()
		if rowsErr != nil {
			err = fmt.Errorf("failed to add privilege to token scope: %w", rowsErr)
			return err
		}
		if inserted == 0 {
			err = fmt.Errorf("%w: %q", ErrUnknownMCPPrivilege, identifier)
			return err
		}
	}

	after, err := tokenMCPScopeNamesTx(tx, tokenID)
	if err != nil {
		return err
	}

	return s.commitTokenScopeChange(tx, actor, target,
		map[string]any{"before": before, "after": after})
}

// GetTokenScope retrieves the complete scope configuration for a token
// Returns nil if the token has no scope restrictions
func (s *AuthStore) GetTokenScope(tokenID int64) (*TokenScope, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	scope := &TokenScope{TokenID: tokenID}

	// Get connection scope
	rows, err := s.db.Query(
		"SELECT connection_id, access_level FROM token_connection_scope WHERE token_id = ?",
		tokenID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get token connection scope: %w", err)
	}

	for rows.Next() {
		var conn ScopedConnection
		if err := rows.Scan(&conn.ConnectionID, &conn.AccessLevel); err != nil {
			rows.Close()
			return nil, fmt.Errorf("failed to scan connection scope: %w", err)
		}
		scope.Connections = append(scope.Connections, conn)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("error iterating connection scope: %w", err)
	}
	rows.Close()

	// Get MCP scope
	rows, err = s.db.Query(
		"SELECT privilege_identifier_id FROM token_mcp_scope WHERE token_id = ?",
		tokenID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get token MCP scope: %w", err)
	}

	for rows.Next() {
		var privID int64
		if err := rows.Scan(&privID); err != nil {
			rows.Close()
			return nil, fmt.Errorf("failed to scan privilege ID: %w", err)
		}
		scope.MCPPrivileges = append(scope.MCPPrivileges, privID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("error iterating MCP scope: %w", err)
	}
	rows.Close()

	// Get admin scope
	rows, err = s.db.Query(
		"SELECT permission FROM token_admin_scope WHERE token_id = ?",
		tokenID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get token admin scope: %w", err)
	}

	for rows.Next() {
		var perm string
		if err := rows.Scan(&perm); err != nil {
			rows.Close()
			return nil, fmt.Errorf("failed to scan admin permission: %w", err)
		}
		scope.AdminPermissions = append(scope.AdminPermissions, perm)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("error iterating admin scope: %w", err)
	}
	rows.Close()

	// Return nil if no scope is defined
	if len(scope.Connections) == 0 && len(scope.MCPPrivileges) == 0 && len(scope.AdminPermissions) == 0 {
		return nil, nil
	}

	return scope, nil
}

// ClearTokenScope removes all scope restrictions from a token,
// attributing the change to the system actor.
func (s *AuthStore) ClearTokenScope(tokenID int64) error {
	return s.clearTokenScope(systemActor, tokenID)
}

func (s *AuthStore) clearTokenScope(actor Actor, tokenID int64) (err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, target, err := s.beginTokenScopeChange(actor, tokenID,
		"token.scope.clear")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			s.failAudit(tx, actor, target, err)
		}
	}()

	before, err := tokenScopesTx(tx, tokenID)
	if err != nil {
		return err
	}

	if _, execErr := tx.Exec(
		"DELETE FROM token_connection_scope WHERE token_id = ?", tokenID,
	); execErr != nil {
		err = fmt.Errorf("failed to clear token connection scope: %w", execErr)
		return err
	}

	if _, execErr := tx.Exec(
		"DELETE FROM token_mcp_scope WHERE token_id = ?", tokenID,
	); execErr != nil {
		err = fmt.Errorf("failed to clear token MCP scope: %w", execErr)
		return err
	}

	if _, execErr := tx.Exec(
		"DELETE FROM token_admin_scope WHERE token_id = ?", tokenID,
	); execErr != nil {
		err = fmt.Errorf("failed to clear token admin scope: %w", execErr)
		return err
	}

	return s.commitTokenScopeChange(tx, actor, target,
		map[string]any{"before": before})
}

// IsConnectionInTokenScope checks if a connection is within a token's scope.
// Returns (inScope, accessLevel, error) where:
// - inScope is true if no connection scope is defined (unrestricted) or the connection is in scope.
// - accessLevel is the scoped access level ("read" or "read_write"), or empty if unrestricted.
func (s *AuthStore) IsConnectionInTokenScope(tokenID int64, connectionID int) (bool, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Check if token has any connection scope
	var count int
	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM token_connection_scope WHERE token_id = ?",
		tokenID,
	).Scan(&count)
	if err != nil {
		return false, "", fmt.Errorf("failed to check connection scope: %w", err)
	}

	// No scope means unrestricted
	if count == 0 {
		return true, AccessLevelNone, nil
	}

	// Check for specific connection
	var accessLevel string
	err = s.db.QueryRow(
		"SELECT access_level FROM token_connection_scope WHERE token_id = ? AND connection_id = ?",
		tokenID, connectionID,
	).Scan(&accessLevel)
	if err == nil {
		return true, accessLevel, nil
	}
	if err != sql.ErrNoRows {
		return false, "", fmt.Errorf("failed to check connection in scope: %w", err)
	}

	// Check for wildcard connection (connection_id = 0 means all connections)
	err = s.db.QueryRow(
		"SELECT access_level FROM token_connection_scope WHERE token_id = ? AND connection_id = ?",
		tokenID, ConnectionIDAll,
	).Scan(&accessLevel)
	if err == sql.ErrNoRows {
		return false, AccessLevelNone, nil
	}
	if err != nil {
		return false, "", fmt.Errorf("failed to check wildcard connection in scope: %w", err)
	}

	return true, accessLevel, nil
}

// IsMCPItemInTokenScope checks if an MCP item is within a token's scope
// Returns true if:
// - The token has no MCP scope defined (no restrictions)
// - The scope contains the wildcard sentinel (privilege_identifier_id = 0)
// - The item's privilege is explicitly in the token's scope
func (s *AuthStore) IsMCPItemInTokenScope(tokenID int64, identifier string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Check if token has any MCP scope
	var scopeCount int
	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM token_mcp_scope WHERE token_id = ?",
		tokenID,
	).Scan(&scopeCount)
	if err != nil {
		return false, fmt.Errorf("failed to check token scope: %w", err)
	}

	// No scope defined - no restrictions
	if scopeCount == 0 {
		return true, nil
	}

	// Check for wildcard sentinel (privilege_identifier_id = 0 means all privileges)
	var wildcardCount int
	err = s.db.QueryRow(
		"SELECT COUNT(*) FROM token_mcp_scope WHERE token_id = ? AND privilege_identifier_id = ?",
		tokenID, MCPPrivilegeIDWildcard,
	).Scan(&wildcardCount)
	if err != nil {
		return false, fmt.Errorf("failed to check wildcard scope: %w", err)
	}
	if wildcardCount > 0 {
		return true, nil
	}

	// Check if this item is in scope
	var inScope int
	err = s.db.QueryRow(
		`SELECT COUNT(*) FROM token_mcp_scope tms
         JOIN mcp_privilege_identifiers mpi ON tms.privilege_identifier_id = mpi.id
         WHERE tms.token_id = ? AND mpi.identifier = ?`,
		tokenID, identifier,
	).Scan(&inScope)
	if err != nil {
		return false, fmt.Errorf("failed to check item in scope: %w", err)
	}

	return inScope > 0, nil
}

// GetTokenConnectionScope returns just the connection IDs in scope for a token
func (s *AuthStore) GetTokenConnectionScope(tokenID int64) ([]int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		"SELECT connection_id FROM token_connection_scope WHERE token_id = ? ORDER BY connection_id",
		tokenID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get token connection scope: %w", err)
	}
	defer rows.Close()

	var connections []int
	for rows.Next() {
		var connID int
		if err := rows.Scan(&connID); err != nil {
			return nil, fmt.Errorf("failed to scan connection ID: %w", err)
		}
		connections = append(connections, connID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating token connection scope: %w", err)
	}

	return connections, nil
}

// GetTokenMCPScope returns the MCP privilege identifiers in scope for a token.
// If the scope contains the wildcard sentinel (privilege_identifier_id = 0),
// this returns ["*"].
func (s *AuthStore) GetTokenMCPScope(tokenID int64) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Check for wildcard sentinel first
	var wildcardCount int
	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM token_mcp_scope WHERE token_id = ? AND privilege_identifier_id = ?",
		tokenID, MCPPrivilegeIDWildcard,
	).Scan(&wildcardCount)
	if err != nil {
		return nil, fmt.Errorf("failed to check wildcard MCP scope: %w", err)
	}
	if wildcardCount > 0 {
		return []string{"*"}, nil
	}

	rows, err := s.db.Query(
		`SELECT mpi.identifier FROM token_mcp_scope tms
         JOIN mcp_privilege_identifiers mpi ON tms.privilege_identifier_id = mpi.id
         WHERE tms.token_id = ?
         ORDER BY mpi.identifier`,
		tokenID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get token MCP scope: %w", err)
	}
	defer rows.Close()

	var identifiers []string
	for rows.Next() {
		var identifier string
		if err := rows.Scan(&identifier); err != nil {
			return nil, fmt.Errorf("failed to scan identifier: %w", err)
		}
		identifiers = append(identifiers, identifier)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating token MCP scope: %w", err)
	}

	return identifiers, nil
}

// HasTokenScope checks if a token has any scope restrictions
func (s *AuthStore) HasTokenScope(tokenID int64) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var connCount, mcpCount, adminCount int

	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM token_connection_scope WHERE token_id = ?",
		tokenID,
	).Scan(&connCount)
	if err != nil && err != sql.ErrNoRows {
		return false, fmt.Errorf("failed to check connection scope: %w", err)
	}

	err = s.db.QueryRow(
		"SELECT COUNT(*) FROM token_mcp_scope WHERE token_id = ?",
		tokenID,
	).Scan(&mcpCount)
	if err != nil && err != sql.ErrNoRows {
		return false, fmt.Errorf("failed to check MCP scope: %w", err)
	}

	err = s.db.QueryRow(
		"SELECT COUNT(*) FROM token_admin_scope WHERE token_id = ?",
		tokenID,
	).Scan(&adminCount)
	if err != nil && err != sql.ErrNoRows {
		return false, fmt.Errorf("failed to check admin scope: %w", err)
	}

	return connCount > 0 || mcpCount > 0 || adminCount > 0, nil
}

// AdminPermissionWildcard is the sentinel permission string stored in
// token_admin_scope to represent a wildcard ("all admin permissions") grant.
const AdminPermissionWildcard = "*"

// SetTokenAdminScope sets the admin permission scope for a token.
// This restricts which admin permissions the token can use.
// If permissions contains "*", a single wildcard entry is stored instead of
// individual permission strings. The change is attributed to the system
// actor.
func (s *AuthStore) SetTokenAdminScope(tokenID int64, permissions []string) error {
	return s.setTokenAdminScope(systemActor, tokenID, permissions)
}

func (s *AuthStore) setTokenAdminScope(actor Actor, tokenID int64,
	permissions []string) (err error) {

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, target, err := s.beginTokenScopeChange(actor, tokenID,
		"token.scope.set_admin")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			s.failAudit(tx, actor, target, err)
		}
	}()

	before, err := tokenAdminScopeTx(tx, tokenID)
	if err != nil {
		return err
	}

	// Clear existing admin scope
	if _, execErr := tx.Exec(
		"DELETE FROM token_admin_scope WHERE token_id = ?", tokenID,
	); execErr != nil {
		err = fmt.Errorf("failed to clear admin scope: %w", execErr)
		return err
	}

	// Insert new admin permissions
	for _, perm := range permissions {
		if perm == AdminPermissionWildcard {
			// Store only the wildcard; skip remaining permissions
			if _, execErr := tx.Exec(
				"INSERT INTO token_admin_scope (token_id, permission) VALUES (?, ?)",
				tokenID, AdminPermissionWildcard,
			); execErr != nil {
				err = fmt.Errorf("failed to add wildcard admin permission to token scope: %w", execErr)
				return err
			}
			break
		}

		if _, execErr := tx.Exec(
			"INSERT INTO token_admin_scope (token_id, permission) VALUES (?, ?)",
			tokenID, perm,
		); execErr != nil {
			err = fmt.Errorf("failed to add admin permission %s to token scope: %w", perm, execErr)
			return err
		}
	}

	after, err := tokenAdminScopeTx(tx, tokenID)
	if err != nil {
		return err
	}

	return s.commitTokenScopeChange(tx, actor, target,
		map[string]any{"before": before, "after": after})
}

// GetTokenAdminScope returns the admin permissions in a token's scope.
func (s *AuthStore) GetTokenAdminScope(tokenID int64) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		"SELECT permission FROM token_admin_scope WHERE token_id = ?",
		tokenID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get admin scope: %w", err)
	}
	defer rows.Close()

	var permissions []string
	for rows.Next() {
		var perm string
		if err := rows.Scan(&perm); err != nil {
			return nil, fmt.Errorf("failed to scan admin permission: %w", err)
		}
		permissions = append(permissions, perm)
	}
	return permissions, rows.Err()
}

// IsAdminPermissionInTokenScope checks if a given admin permission is in the token's scope.
// Returns true if the token has no admin scope (unrestricted), if the scope
// contains the wildcard "*", or if the permission is explicitly in scope.
func (s *AuthStore) IsAdminPermissionInTokenScope(tokenID int64, permission string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Check if token has any admin scope at all
	var count int
	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM token_admin_scope WHERE token_id = ?",
		tokenID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check admin scope: %w", err)
	}

	// No admin scope means unrestricted
	if count == 0 {
		return true, nil
	}

	// Check for wildcard ("*" means all admin permissions)
	var wildcardCount int
	err = s.db.QueryRow(
		"SELECT COUNT(*) FROM token_admin_scope WHERE token_id = ? AND permission = ?",
		tokenID, AdminPermissionWildcard,
	).Scan(&wildcardCount)
	if err != nil {
		return false, fmt.Errorf("failed to check wildcard admin scope: %w", err)
	}
	if wildcardCount > 0 {
		return true, nil
	}

	// Check if the specific permission is in scope
	err = s.db.QueryRow(
		"SELECT COUNT(*) FROM token_admin_scope WHERE token_id = ? AND permission = ?",
		tokenID, permission,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check admin permission in scope: %w", err)
	}

	return count > 0, nil
}
