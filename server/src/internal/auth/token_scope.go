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

// SetTokenConnectionScope sets the connection scope for a token
// If connectionIDs is empty, clears all connection scoping (token has no connection restrictions)
func (s *AuthStore) SetTokenConnectionScope(tokenID int64, connectionIDs []int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Clear existing scope
	_, err := s.db.Exec("DELETE FROM token_connection_scope WHERE token_id = ?", tokenID)
	if err != nil {
		return fmt.Errorf("failed to clear token connection scope: %w", err)
	}

	// Add new scope entries
	for _, connID := range connectionIDs {
		_, err := s.db.Exec(
			"INSERT INTO token_connection_scope (token_id, connection_id) VALUES (?, ?)",
			tokenID, connID,
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

// SetTokenMCPScopeByNames sets the MCP privilege scope for a token using privilege identifiers
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
		"SELECT connection_id FROM token_connection_scope WHERE token_id = ?",
		tokenID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get token connection scope: %w", err)
	}

	for rows.Next() {
		var connID int
		if err := rows.Scan(&connID); err != nil {
			rows.Close()
			return nil, fmt.Errorf("failed to scan connection ID: %w", err)
		}
		scope.ConnectionIDs = append(scope.ConnectionIDs, connID)
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

	// Return nil if no scope is defined
	if len(scope.ConnectionIDs) == 0 && len(scope.MCPPrivileges) == 0 {
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

	return nil
}

// IsConnectionInTokenScope checks if a connection is within a token's scope
// Returns true if:
// - The token has no connection scope defined (no restrictions)
// - The connection is explicitly in the token's scope
func (s *AuthStore) IsConnectionInTokenScope(tokenID int64, connectionID int) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Check if token has any connection scope
	var scopeCount int
	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM token_connection_scope WHERE token_id = ?",
		tokenID,
	).Scan(&scopeCount)
	if err != nil {
		return false, fmt.Errorf("failed to check token scope: %w", err)
	}

	// No scope defined - no restrictions
	if scopeCount == 0 {
		return true, nil
	}

	// Check if this connection is in scope
	var inScope int
	err = s.db.QueryRow(
		"SELECT COUNT(*) FROM token_connection_scope WHERE token_id = ? AND connection_id = ?",
		tokenID, connectionID,
	).Scan(&inScope)
	if err != nil {
		return false, fmt.Errorf("failed to check connection in scope: %w", err)
	}

	return inScope > 0, nil
}

// IsMCPItemInTokenScope checks if an MCP item is within a token's scope
// Returns true if:
// - The token has no MCP scope defined (no restrictions)
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

// GetTokenMCPScope returns the MCP privilege identifiers in scope for a token
func (s *AuthStore) GetTokenMCPScope(tokenID int64) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

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

	var connCount, mcpCount int

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

	return connCount > 0 || mcpCount > 0, nil
}
