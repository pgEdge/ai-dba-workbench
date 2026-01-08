/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package auth

import (
	"database/sql"
	"fmt"
)

// =============================================================================
// MCP Privilege Registration
// =============================================================================

// RegisterMCPPrivilege registers a new MCP privilege identifier (tool, resource, or prompt)
func (s *AuthStore) RegisterMCPPrivilege(identifier, itemType, description string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate item type
	if itemType != MCPPrivilegeTypeTool && itemType != MCPPrivilegeTypeResource && itemType != MCPPrivilegeTypePrompt {
		return 0, fmt.Errorf("invalid item type: %s (must be 'tool', 'resource', or 'prompt')", itemType)
	}

	// Use INSERT OR IGNORE to handle duplicates gracefully
	result, err := s.db.Exec(
		`INSERT OR IGNORE INTO mcp_privilege_identifiers (identifier, item_type, description)
         VALUES (?, ?, ?)`,
		identifier, itemType, description,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to register MCP privilege: %w", err)
	}

	// If no rows were inserted (due to INSERT OR IGNORE), get the existing ID
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
		`SELECT id, identifier, item_type, description, created_at
         FROM mcp_privilege_identifiers WHERE identifier = ?`,
		identifier,
	).Scan(&priv.ID, &priv.Identifier, &priv.ItemType, &priv.Description, &priv.CreatedAt)

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
		`SELECT id, identifier, item_type, description, created_at
         FROM mcp_privilege_identifiers WHERE id = ?`,
		id,
	).Scan(&priv.ID, &priv.Identifier, &priv.ItemType, &priv.Description, &priv.CreatedAt)

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
		`SELECT id, identifier, item_type, description, created_at
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
		if err := rows.Scan(&priv.ID, &priv.Identifier, &priv.ItemType, &priv.Description, &priv.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan privilege: %w", err)
		}
		privileges = append(privileges, &priv)
	}

	return privileges, nil
}

// ListMCPPrivilegesByType returns all privileges of a specific type
func (s *AuthStore) ListMCPPrivilegesByType(itemType string) ([]*MCPPrivilege, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT id, identifier, item_type, description, created_at
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
		if err := rows.Scan(&priv.ID, &priv.Identifier, &priv.ItemType, &priv.Description, &priv.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan privilege: %w", err)
		}
		privileges = append(privileges, &priv)
	}

	return privileges, nil
}

// =============================================================================
// MCP Privilege Grants
// =============================================================================

// GrantMCPPrivilege grants an MCP privilege to a group
func (s *AuthStore) GrantMCPPrivilege(groupID, privilegeID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO group_mcp_privileges (group_id, privilege_identifier_id)
         VALUES (?, ?)`,
		groupID, privilegeID,
	)
	if err != nil {
		return fmt.Errorf("failed to grant MCP privilege: %w", err)
	}

	return nil
}

// GrantMCPPrivilegeByName grants an MCP privilege to a group by identifier name
func (s *AuthStore) GrantMCPPrivilegeByName(groupID int64, identifier string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Get privilege ID
	var privilegeID int64
	err := s.db.QueryRow(
		"SELECT id FROM mcp_privilege_identifiers WHERE identifier = ?",
		identifier,
	).Scan(&privilegeID)
	if err == sql.ErrNoRows {
		return fmt.Errorf("privilege not found: %s", identifier)
	}
	if err != nil {
		return fmt.Errorf("failed to get privilege: %w", err)
	}

	_, err = s.db.Exec(
		`INSERT OR IGNORE INTO group_mcp_privileges (group_id, privilege_identifier_id)
         VALUES (?, ?)`,
		groupID, privilegeID,
	)
	if err != nil {
		return fmt.Errorf("failed to grant MCP privilege: %w", err)
	}

	return nil
}

// RevokeMCPPrivilege revokes an MCP privilege from a group
func (s *AuthStore) RevokeMCPPrivilege(groupID, privilegeID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(
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
		return fmt.Errorf("privilege grant not found")
	}

	return nil
}

// RevokeMCPPrivilegeByName revokes an MCP privilege from a group by identifier name
func (s *AuthStore) RevokeMCPPrivilegeByName(groupID int64, identifier string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(
		`DELETE FROM group_mcp_privileges
         WHERE group_id = ?
         AND privilege_identifier_id = (SELECT id FROM mcp_privilege_identifiers WHERE identifier = ?)`,
		groupID, identifier,
	)
	if err != nil {
		return fmt.Errorf("failed to revoke MCP privilege: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("privilege grant not found")
	}

	return nil
}

// =============================================================================
// Connection Privilege Grants
// =============================================================================

// GrantConnectionPrivilege grants access to a database connection for a group
func (s *AuthStore) GrantConnectionPrivilege(groupID int64, connectionID int, accessLevel string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate access level
	if accessLevel != AccessLevelRead && accessLevel != AccessLevelReadWrite {
		return fmt.Errorf("invalid access level: %s (must be 'read' or 'read_write')", accessLevel)
	}

	// Use INSERT OR REPLACE to update existing grants
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO connection_privileges (group_id, connection_id, access_level)
         VALUES (?, ?, ?)`,
		groupID, connectionID, accessLevel,
	)
	if err != nil {
		return fmt.Errorf("failed to grant connection privilege: %w", err)
	}

	return nil
}

// RevokeConnectionPrivilege revokes access to a database connection from a group
func (s *AuthStore) RevokeConnectionPrivilege(groupID int64, connectionID int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(
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
		return fmt.Errorf("connection privilege not found")
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

// ListGroupMCPPrivileges returns all MCP privileges granted to a group
func (s *AuthStore) ListGroupMCPPrivileges(groupID int64) ([]*MCPPrivilege, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT mpi.id, mpi.identifier, mpi.item_type, mpi.description, mpi.created_at
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
		if err := rows.Scan(&priv.ID, &priv.Identifier, &priv.ItemType, &priv.Description, &priv.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan privilege: %w", err)
		}
		privileges = append(privileges, &priv)
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
// This determines if the privilege is "restricted" (requires authorization) or "public"
func (s *AuthStore) IsPrivilegeAssignedToAnyGroup(identifier string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	err := s.db.QueryRow(`
        SELECT COUNT(*)
        FROM group_mcp_privileges gmp
        JOIN mcp_privilege_identifiers mpi ON gmp.privilege_identifier_id = mpi.id
        WHERE mpi.identifier = ?
    `, identifier).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check privilege assignment: %w", err)
	}

	return count > 0, nil
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
		rows.Close()
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
		"SELECT COUNT(*) FROM connection_privileges WHERE connection_id = ?",
		connectionID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check connection assignment: %w", err)
	}

	return count > 0, nil
}

// MCPPrivilegeCount returns the number of registered MCP privileges
func (s *AuthStore) MCPPrivilegeCount() int {
	var count int
	//nolint:errcheck // Returns 0 on error, which is acceptable
	s.db.QueryRow("SELECT COUNT(*) FROM mcp_privilege_identifiers").Scan(&count)
	return count
}
