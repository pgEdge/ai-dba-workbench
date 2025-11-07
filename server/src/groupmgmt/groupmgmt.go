/*-----------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package groupmgmt

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgEdge/ai-workbench/server/src/privileges"
)

// CreateUserGroup creates a new user group
func CreateUserGroup(ctx context.Context, pool *pgxpool.Pool, name, description string) (int, error) {
	var groupID int
	err := pool.QueryRow(ctx, `
		INSERT INTO user_groups (name, description)
		VALUES ($1, $2)
		RETURNING id
	`, name, description).Scan(&groupID)

	if err != nil {
		return 0, fmt.Errorf("failed to create user group: %w", err)
	}

	return groupID, nil
}

// UpdateUserGroup updates an existing user group
func UpdateUserGroup(ctx context.Context, pool *pgxpool.Pool, groupID int, name, description string) error {
	result, err := pool.Exec(ctx, `
		UPDATE user_groups
		SET name = $1, description = $2, updated_at = CURRENT_TIMESTAMP
		WHERE id = $3
	`, name, description, groupID)

	if err != nil {
		return fmt.Errorf("failed to update user group: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("user group not found")
	}

	return nil
}

// DeleteUserGroup deletes a user group
func DeleteUserGroup(ctx context.Context, pool *pgxpool.Pool, groupID int) error {
	result, err := pool.Exec(ctx, `
		DELETE FROM user_groups
		WHERE id = $1
	`, groupID)

	if err != nil {
		return fmt.Errorf("failed to delete user group: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("user group not found")
	}

	return nil
}

// ListUserGroups returns all user groups
func ListUserGroups(ctx context.Context, pool *pgxpool.Pool) ([]map[string]interface{}, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, name, description, created_at, updated_at
		FROM user_groups
		ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query user groups: %w", err)
	}
	defer rows.Close()

	groups := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id int
		var name, description string
		var createdAt, updatedAt string

		err := rows.Scan(&id, &name, &description, &createdAt, &updatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user group: %w", err)
		}

		groups = append(groups, map[string]interface{}{
			"id":          id,
			"name":        name,
			"description": description,
			"created_at":  createdAt,
			"updated_at":  updatedAt,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating user groups: %w", err)
	}

	return groups, nil
}

// AddGroupMember adds a user or group as a member of a parent group
func AddGroupMember(ctx context.Context, pool *pgxpool.Pool, parentGroupID int, memberUserID *int, memberGroupID *int) error {
	// Validate that exactly one of memberUserID or memberGroupID is provided
	if (memberUserID == nil && memberGroupID == nil) || (memberUserID != nil && memberGroupID != nil) {
		return fmt.Errorf("must specify either memberUserID or memberGroupID, not both")
	}

	// If adding a group as a member, validate hierarchy
	if memberGroupID != nil {
		err := privileges.ValidateGroupHierarchy(ctx, pool, parentGroupID, *memberGroupID)
		if err != nil {
			return fmt.Errorf("invalid group hierarchy: %w", err)
		}
	}

	// Insert membership
	_, err := pool.Exec(ctx, `
		INSERT INTO group_memberships (parent_group_id, member_user_id, member_group_id)
		VALUES ($1, $2, $3)
	`, parentGroupID, memberUserID, memberGroupID)

	if err != nil {
		return fmt.Errorf("failed to add group member: %w", err)
	}

	return nil
}

// RemoveGroupMember removes a user or group from a parent group
func RemoveGroupMember(ctx context.Context, pool *pgxpool.Pool, parentGroupID int, memberUserID *int, memberGroupID *int) error {
	// Validate that exactly one of memberUserID or memberGroupID is provided
	if (memberUserID == nil && memberGroupID == nil) || (memberUserID != nil && memberGroupID != nil) {
		return fmt.Errorf("must specify either memberUserID or memberGroupID, not both")
	}

	// Remove membership
	var result interface{ RowsAffected() int64 }
	var err error

	if memberUserID != nil {
		result, err = pool.Exec(ctx, `
			DELETE FROM group_memberships
			WHERE parent_group_id = $1 AND member_user_id = $2
		`, parentGroupID, *memberUserID)
	} else {
		result, err = pool.Exec(ctx, `
			DELETE FROM group_memberships
			WHERE parent_group_id = $1 AND member_group_id = $2
		`, parentGroupID, *memberGroupID)
	}

	if err != nil {
		return fmt.Errorf("failed to remove group member: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("membership not found")
	}

	return nil
}

// ListGroupMembers returns all members (users and groups) of a group
func ListGroupMembers(ctx context.Context, pool *pgxpool.Pool, groupID int) ([]map[string]interface{}, error) {
	rows, err := pool.Query(ctx, `
		SELECT
			gm.id,
			gm.member_user_id,
			gm.member_group_id,
			gm.created_at,
			COALESCE(u.username, '') as username,
			COALESCE(g.name, '') as group_name
		FROM group_memberships gm
		LEFT JOIN user_accounts u ON gm.member_user_id = u.id
		LEFT JOIN user_groups g ON gm.member_group_id = g.id
		WHERE gm.parent_group_id = $1
		ORDER BY COALESCE(u.username, g.name)
	`, groupID)
	if err != nil {
		return nil, fmt.Errorf("failed to query group members: %w", err)
	}
	defer rows.Close()

	members := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id int
		var memberUserID, memberGroupID *int
		var createdAt, username, groupName string

		err := rows.Scan(&id, &memberUserID, &memberGroupID, &createdAt, &username, &groupName)
		if err != nil {
			return nil, fmt.Errorf("failed to scan group member: %w", err)
		}

		member := map[string]interface{}{
			"id":         id,
			"created_at": createdAt,
		}

		if memberUserID != nil {
			member["type"] = "user"
			member["user_id"] = *memberUserID
			member["username"] = username
		} else if memberGroupID != nil {
			member["type"] = "group"
			member["group_id"] = *memberGroupID
			member["group_name"] = groupName
		}

		members = append(members, member)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating group members: %w", err)
	}

	return members, nil
}

// ListUserGroupMemberships returns all groups that a user belongs to (direct and indirect)
func ListUserGroupMemberships(ctx context.Context, pool *pgxpool.Pool, userID int) ([]map[string]interface{}, error) {
	// Get all groups (direct and indirect)
	groupIDs, err := privileges.GetUserGroups(ctx, pool, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user groups: %w", err)
	}

	if len(groupIDs) == 0 {
		return []map[string]interface{}{}, nil
	}

	// Get group details
	rows, err := pool.Query(ctx, `
		SELECT id, name, description
		FROM user_groups
		WHERE id = ANY($1)
		ORDER BY name
	`, groupIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to query group details: %w", err)
	}
	defer rows.Close()

	groups := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id int
		var name, description string

		err := rows.Scan(&id, &name, &description)
		if err != nil {
			return nil, fmt.Errorf("failed to scan group: %w", err)
		}

		groups = append(groups, map[string]interface{}{
			"id":          id,
			"name":        name,
			"description": description,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating groups: %w", err)
	}

	return groups, nil
}

// GrantConnectionPrivilege grants a group access to a connection at the specified level
func GrantConnectionPrivilege(ctx context.Context, pool *pgxpool.Pool, groupID int, connectionID int, accessLevel string) error {
	// Validate access level
	if accessLevel != "read" && accessLevel != "read_write" {
		return fmt.Errorf("invalid access level: must be 'read' or 'read_write'")
	}

	// Insert or update privilege
	_, err := pool.Exec(ctx, `
		INSERT INTO connection_privileges (group_id, connection_id, access_level)
		VALUES ($1, $2, $3)
		ON CONFLICT (group_id, connection_id)
		DO UPDATE SET access_level = EXCLUDED.access_level
	`, groupID, connectionID, accessLevel)

	if err != nil {
		return fmt.Errorf("failed to grant connection privilege: %w", err)
	}

	return nil
}

// RevokeConnectionPrivilege revokes a group's access to a connection
func RevokeConnectionPrivilege(ctx context.Context, pool *pgxpool.Pool, groupID int, connectionID int) error {
	result, err := pool.Exec(ctx, `
		DELETE FROM connection_privileges
		WHERE group_id = $1 AND connection_id = $2
	`, groupID, connectionID)

	if err != nil {
		return fmt.Errorf("failed to revoke connection privilege: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("privilege not found")
	}

	return nil
}

// ListConnectionPrivileges returns all group privileges for a connection
func ListConnectionPrivileges(ctx context.Context, pool *pgxpool.Pool, connectionID int) ([]map[string]interface{}, error) {
	rows, err := pool.Query(ctx, `
		SELECT
			cp.id,
			cp.group_id,
			cp.access_level,
			cp.created_at,
			ug.name as group_name,
			ug.description as group_description
		FROM connection_privileges cp
		INNER JOIN user_groups ug ON cp.group_id = ug.id
		WHERE cp.connection_id = $1
		ORDER BY ug.name
	`, connectionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query connection privileges: %w", err)
	}
	defer rows.Close()

	privileges := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, groupID int
		var accessLevel, createdAt, groupName, groupDescription string

		err := rows.Scan(&id, &groupID, &accessLevel, &createdAt, &groupName, &groupDescription)
		if err != nil {
			return nil, fmt.Errorf("failed to scan connection privilege: %w", err)
		}

		privileges = append(privileges, map[string]interface{}{
			"id":                  id,
			"group_id":            groupID,
			"group_name":          groupName,
			"group_description":   groupDescription,
			"access_level":        accessLevel,
			"created_at":          createdAt,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating connection privileges: %w", err)
	}

	return privileges, nil
}

// ListMCPPrivilegeIdentifiers returns all registered MCP privilege identifiers
func ListMCPPrivilegeIdentifiers(ctx context.Context, pool *pgxpool.Pool) ([]map[string]interface{}, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, identifier, item_type, description, created_at
		FROM mcp_privilege_identifiers
		ORDER BY item_type, identifier
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query privilege identifiers: %w", err)
	}
	defer rows.Close()

	identifiers := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id int
		var identifier, itemType, description, createdAt string

		err := rows.Scan(&id, &identifier, &itemType, &description, &createdAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan privilege identifier: %w", err)
		}

		identifiers = append(identifiers, map[string]interface{}{
			"id":          id,
			"identifier":  identifier,
			"item_type":   itemType,
			"description": description,
			"created_at":  createdAt,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating privilege identifiers: %w", err)
	}

	return identifiers, nil
}

// GrantMCPPrivilege grants a group access to an MCP item
func GrantMCPPrivilege(ctx context.Context, pool *pgxpool.Pool, groupID int, privilegeIdentifier string) error {
	// First, ensure the privilege identifier exists
	var privilegeID int
	err := pool.QueryRow(ctx, `
		SELECT id FROM mcp_privilege_identifiers WHERE identifier = $1
	`, privilegeIdentifier).Scan(&privilegeID)

	if err != nil {
		return fmt.Errorf("privilege identifier not found: %w", err)
	}

	// Grant the privilege
	_, err = pool.Exec(ctx, `
		INSERT INTO group_mcp_privileges (group_id, privilege_identifier_id)
		VALUES ($1, $2)
		ON CONFLICT (group_id, privilege_identifier_id) DO NOTHING
	`, groupID, privilegeID)

	if err != nil {
		return fmt.Errorf("failed to grant MCP privilege: %w", err)
	}

	return nil
}

// RevokeMCPPrivilege revokes a group's access to an MCP item
func RevokeMCPPrivilege(ctx context.Context, pool *pgxpool.Pool, groupID int, privilegeIdentifier string) error {
	// First, get the privilege identifier ID
	var privilegeID int
	err := pool.QueryRow(ctx, `
		SELECT id FROM mcp_privilege_identifiers WHERE identifier = $1
	`, privilegeIdentifier).Scan(&privilegeID)

	if err != nil {
		return fmt.Errorf("privilege identifier not found: %w", err)
	}

	// Revoke the privilege
	result, err := pool.Exec(ctx, `
		DELETE FROM group_mcp_privileges
		WHERE group_id = $1 AND privilege_identifier_id = $2
	`, groupID, privilegeID)

	if err != nil {
		return fmt.Errorf("failed to revoke MCP privilege: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("privilege not found")
	}

	return nil
}

// ListGroupMCPPrivileges returns all MCP privileges for a group
func ListGroupMCPPrivileges(ctx context.Context, pool *pgxpool.Pool, groupID int) ([]map[string]interface{}, error) {
	rows, err := pool.Query(ctx, `
		SELECT
			gmp.id,
			gmp.created_at,
			mpi.id as privilege_id,
			mpi.identifier,
			mpi.item_type,
			mpi.description
		FROM group_mcp_privileges gmp
		INNER JOIN mcp_privilege_identifiers mpi ON gmp.privilege_identifier_id = mpi.id
		WHERE gmp.group_id = $1
		ORDER BY mpi.item_type, mpi.identifier
	`, groupID)
	if err != nil {
		return nil, fmt.Errorf("failed to query group MCP privileges: %w", err)
	}
	defer rows.Close()

	privileges := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, privilegeID int
		var createdAt, identifier, itemType, description string

		err := rows.Scan(&id, &createdAt, &privilegeID, &identifier, &itemType, &description)
		if err != nil {
			return nil, fmt.Errorf("failed to scan MCP privilege: %w", err)
		}

		privileges = append(privileges, map[string]interface{}{
			"id":          id,
			"privilege_id": privilegeID,
			"identifier":  identifier,
			"item_type":   itemType,
			"description": description,
			"created_at":  createdAt,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating MCP privileges: %w", err)
	}

	return privileges, nil
}

// SetTokenConnectionScope limits a token to specific connections
func SetTokenConnectionScope(ctx context.Context, pool *pgxpool.Pool, tokenID int, tokenType string, connectionIDs []int) error {
	// Validate token type
	if tokenType != "user" && tokenType != "service" {
		return fmt.Errorf("invalid token type: must be 'user' or 'service'")
	}

	// Start a transaction
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Clear existing scope
	_, err = tx.Exec(ctx, `
		DELETE FROM token_connection_scope
		WHERE token_id = $1 AND token_type = $2
	`, tokenID, tokenType)
	if err != nil {
		return fmt.Errorf("failed to clear existing connection scope: %w", err)
	}

	// Insert new scope entries
	for _, connectionID := range connectionIDs {
		_, err = tx.Exec(ctx, `
			INSERT INTO token_connection_scope (token_id, token_type, connection_id)
			VALUES ($1, $2, $3)
		`, tokenID, tokenType, connectionID)
		if err != nil {
			return fmt.Errorf("failed to add connection to scope: %w", err)
		}
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// SetTokenMCPScope limits a token to specific MCP items
func SetTokenMCPScope(ctx context.Context, pool *pgxpool.Pool, tokenID int, tokenType string, privilegeIdentifiers []string) error {
	// Validate token type
	if tokenType != "user" && tokenType != "service" {
		return fmt.Errorf("invalid token type: must be 'user' or 'service'")
	}

	// Start a transaction
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Clear existing scope
	_, err = tx.Exec(ctx, `
		DELETE FROM token_mcp_scope
		WHERE token_id = $1 AND token_type = $2
	`, tokenID, tokenType)
	if err != nil {
		return fmt.Errorf("failed to clear existing MCP scope: %w", err)
	}

	// Insert new scope entries
	for _, identifier := range privilegeIdentifiers {
		// Get the privilege identifier ID
		var privilegeID int
		err = tx.QueryRow(ctx, `
			SELECT id FROM mcp_privilege_identifiers WHERE identifier = $1
		`, identifier).Scan(&privilegeID)
		if err != nil {
			return fmt.Errorf("privilege identifier '%s' not found: %w", identifier, err)
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO token_mcp_scope (token_id, token_type, privilege_identifier_id)
			VALUES ($1, $2, $3)
		`, tokenID, tokenType, privilegeID)
		if err != nil {
			return fmt.Errorf("failed to add privilege to scope: %w", err)
		}
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetTokenScope returns the scope restrictions for a token
func GetTokenScope(ctx context.Context, pool *pgxpool.Pool, tokenID int, tokenType string) (map[string]interface{}, error) {
	// Validate token type
	if tokenType != "user" && tokenType != "service" {
		return nil, fmt.Errorf("invalid token type: must be 'user' or 'service'")
	}

	result := map[string]interface{}{
		"token_id":   tokenID,
		"token_type": tokenType,
		"connections": []map[string]interface{}{},
		"mcp_items":   []map[string]interface{}{},
	}

	// Get connection scope
	connRows, err := pool.Query(ctx, `
		SELECT
			tcs.id,
			tcs.connection_id,
			c.connection_name,
			tcs.created_at
		FROM token_connection_scope tcs
		INNER JOIN connections c ON tcs.connection_id = c.id
		WHERE tcs.token_id = $1 AND tcs.token_type = $2
		ORDER BY c.connection_name
	`, tokenID, tokenType)
	if err != nil {
		return nil, fmt.Errorf("failed to query connection scope: %w", err)
	}
	defer connRows.Close()

	connections := make([]map[string]interface{}, 0)
	for connRows.Next() {
		var id, connectionID int
		var connectionName, createdAt string

		err := connRows.Scan(&id, &connectionID, &connectionName, &createdAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan connection scope: %w", err)
		}

		connections = append(connections, map[string]interface{}{
			"id":              id,
			"connection_id":   connectionID,
			"connection_name": connectionName,
			"created_at":      createdAt,
		})
	}
	result["connections"] = connections

	// Get MCP scope
	mcpRows, err := pool.Query(ctx, `
		SELECT
			tms.id,
			mpi.id as privilege_id,
			mpi.identifier,
			mpi.item_type,
			mpi.description,
			tms.created_at
		FROM token_mcp_scope tms
		INNER JOIN mcp_privilege_identifiers mpi ON tms.privilege_identifier_id = mpi.id
		WHERE tms.token_id = $1 AND tms.token_type = $2
		ORDER BY mpi.item_type, mpi.identifier
	`, tokenID, tokenType)
	if err != nil {
		return nil, fmt.Errorf("failed to query MCP scope: %w", err)
	}
	defer mcpRows.Close()

	mcpItems := make([]map[string]interface{}, 0)
	for mcpRows.Next() {
		var id, privilegeID int
		var identifier, itemType, description, createdAt string

		err := mcpRows.Scan(&id, &privilegeID, &identifier, &itemType, &description, &createdAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan MCP scope: %w", err)
		}

		mcpItems = append(mcpItems, map[string]interface{}{
			"id":           id,
			"privilege_id": privilegeID,
			"identifier":   identifier,
			"item_type":    itemType,
			"description":  description,
			"created_at":   createdAt,
		})
	}
	result["mcp_items"] = mcpItems

	return result, nil
}

// ClearTokenScope removes all scope restrictions for a token
func ClearTokenScope(ctx context.Context, pool *pgxpool.Pool, tokenID int, tokenType string) error {
	// Validate token type
	if tokenType != "user" && tokenType != "service" {
		return fmt.Errorf("invalid token type: must be 'user' or 'service'")
	}

	// Start a transaction
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Clear connection scope
	_, err = tx.Exec(ctx, `
		DELETE FROM token_connection_scope
		WHERE token_id = $1 AND token_type = $2
	`, tokenID, tokenType)
	if err != nil {
		return fmt.Errorf("failed to clear connection scope: %w", err)
	}

	// Clear MCP scope
	_, err = tx.Exec(ctx, `
		DELETE FROM token_mcp_scope
		WHERE token_id = $1 AND token_type = $2
	`, tokenID, tokenType)
	if err != nil {
		return fmt.Errorf("failed to clear MCP scope: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
