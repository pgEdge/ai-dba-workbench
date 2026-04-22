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
	"fmt"
)

// =============================================================================
// Token Scope Management
// =============================================================================

// SetTokenConnectionScope sets the connection scope for a token.
// If connections is empty, clears all connection scoping (token has no connection restrictions).
func (s *AuthStore) SetTokenConnectionScope(tokenID int64, connections []ScopedConnection) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Clear existing scope
	_, err := s.db.Exec("DELETE FROM token_connection_scope WHERE token_id = ?", tokenID)
	if err != nil {
		return fmt.Errorf("failed to clear token connection scope: %w", err)
	}

	// Add new scope entries
	for _, conn := range connections {
		_, err := s.db.Exec(
			"INSERT INTO token_connection_scope (token_id, connection_id, access_level) VALUES (?, ?, ?)",
			tokenID, conn.ConnectionID, conn.AccessLevel,
		)
		if err != nil {
			return fmt.Errorf("failed to add connection to token scope: %w", err)
		}
	}

	return nil
}

// SetTokenMCPScope sets the MCP privilege scope for a token
// If privilegeIDs is empty, clears all MCP scoping (token has no MCP restrictions)
func (s *AuthStore) SetTokenMCPScope(tokenID int64, privilegeIDs []int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Clear existing scope
	_, err := s.db.Exec("DELETE FROM token_mcp_scope WHERE token_id = ?", tokenID)
	if err != nil {
		return fmt.Errorf("failed to clear token MCP scope: %w", err)
	}

	// Add new scope entries
	for _, privID := range privilegeIDs {
		_, err := s.db.Exec(
			"INSERT INTO token_mcp_scope (token_id, privilege_identifier_id) VALUES (?, ?)",
			tokenID, privID,
		)
		if err != nil {
			return fmt.Errorf("failed to add privilege to token scope: %w", err)
		}
	}

	return nil
}

// MCPPrivilegeIDWildcard is the sentinel privilege_identifier_id stored in
// token_mcp_scope to represent a wildcard ("all MCP privileges") grant.
const MCPPrivilegeIDWildcard int64 = 0

// SetTokenMCPScopeByNames sets the MCP privilege scope for a token using privilege identifiers.
// If identifiers contains "*", a single wildcard entry is stored instead of
// looking up individual privilege IDs.
func (s *AuthStore) SetTokenMCPScopeByNames(tokenID int64, identifiers []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Clear existing scope
	_, err := s.db.Exec("DELETE FROM token_mcp_scope WHERE token_id = ?", tokenID)
	if err != nil {
		return fmt.Errorf("failed to clear token MCP scope: %w", err)
	}

	// Add new scope entries
	for _, identifier := range identifiers {
		if identifier == "*" {
			// Store wildcard sentinel (privilege_identifier_id = 0)
			_, err := s.db.Exec(
				"INSERT INTO token_mcp_scope (token_id, privilege_identifier_id) VALUES (?, ?)",
				tokenID, MCPPrivilegeIDWildcard,
			)
			if err != nil {
				return fmt.Errorf("failed to add wildcard privilege to token scope: %w", err)
			}
			// Wildcard covers everything; skip remaining identifiers
			return nil
		}

		_, err := s.db.Exec(
			`INSERT INTO token_mcp_scope (token_id, privilege_identifier_id)
             SELECT ?, id FROM mcp_privilege_identifiers WHERE identifier = ?`,
			tokenID, identifier,
		)
		if err != nil {
			return fmt.Errorf("failed to add privilege to token scope: %w", err)
		}
	}

	return nil
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

// ClearTokenScope removes all scope restrictions from a token
func (s *AuthStore) ClearTokenScope(tokenID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec("DELETE FROM token_connection_scope WHERE token_id = ?", tokenID)
	if err != nil {
		return fmt.Errorf("failed to clear token connection scope: %w", err)
	}

	_, err = s.db.Exec("DELETE FROM token_mcp_scope WHERE token_id = ?", tokenID)
	if err != nil {
		return fmt.Errorf("failed to clear token MCP scope: %w", err)
	}

	_, err = s.db.Exec("DELETE FROM token_admin_scope WHERE token_id = ?", tokenID)
	if err != nil {
		return fmt.Errorf("failed to clear token admin scope: %w", err)
	}

	return nil
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
// individual permission strings.
func (s *AuthStore) SetTokenAdminScope(tokenID int64, permissions []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // Rollback after commit is a no-op

	// Clear existing admin scope
	_, err = tx.Exec("DELETE FROM token_admin_scope WHERE token_id = ?", tokenID)
	if err != nil {
		return fmt.Errorf("failed to clear admin scope: %w", err)
	}

	// Insert new admin permissions
	for _, perm := range permissions {
		if perm == AdminPermissionWildcard {
			// Store only the wildcard; skip remaining permissions
			_, err = tx.Exec(
				"INSERT INTO token_admin_scope (token_id, permission) VALUES (?, ?)",
				tokenID, AdminPermissionWildcard,
			)
			if err != nil {
				return fmt.Errorf("failed to add wildcard admin permission to token scope: %w", err)
			}
			return tx.Commit()
		}

		_, err = tx.Exec(
			"INSERT INTO token_admin_scope (token_id, permission) VALUES (?, ?)",
			tokenID, perm,
		)
		if err != nil {
			return fmt.Errorf("failed to add admin permission %s to token scope: %w", perm, err)
		}
	}

	return tx.Commit()
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
