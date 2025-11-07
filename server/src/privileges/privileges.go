/*-----------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package privileges

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AccessLevel represents the level of access requested for a connection
type AccessLevel string

const (
	AccessLevelRead      AccessLevel = "read"
	AccessLevelReadWrite AccessLevel = "read_write"
)

// GetUserGroups recursively resolves all groups that a user belongs to
// (both directly and through nested group membership).
// Returns a slice of group IDs.
func GetUserGroups(ctx context.Context, pool *pgxpool.Pool, userID int) ([]int, error) {
	groupIDs := make([]int, 0)
	visited := make(map[int]bool)

	// Use recursive CTE to find all groups the user belongs to
	query := `
		WITH RECURSIVE user_groups_recursive AS (
			-- Base case: direct group memberships for this user
			SELECT parent_group_id as group_id
			FROM group_memberships
			WHERE member_user_id = $1

			UNION

			-- Recursive case: groups that contain groups we're already in
			SELECT gm.parent_group_id
			FROM group_memberships gm
			INNER JOIN user_groups_recursive ugr ON gm.member_group_id = ugr.group_id
		)
		SELECT DISTINCT group_id
		FROM user_groups_recursive
		ORDER BY group_id;
	`

	rows, err := pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query user groups: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var groupID int
		if err := rows.Scan(&groupID); err != nil {
			return nil, fmt.Errorf("failed to scan group ID: %w", err)
		}
		if !visited[groupID] {
			groupIDs = append(groupIDs, groupID)
			visited[groupID] = true
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating user groups: %w", err)
	}

	return groupIDs, nil
}

// CanAccessConnection checks if a user can access a connection at the requested level.
// Returns true if:
// - User is a superuser, OR
// - Connection is shared with no groups assigned (accessible to all), OR
// - Connection has groups assigned AND user is in one of those groups with sufficient access level
func CanAccessConnection(ctx context.Context, pool *pgxpool.Pool, userID int, connectionID int, requestedLevel AccessLevel) (bool, error) {
	// Check if user is superuser
	var isSuperuser bool
	err := pool.QueryRow(ctx, "SELECT is_superuser FROM user_accounts WHERE id = $1", userID).Scan(&isSuperuser)
	if err != nil {
		return false, fmt.Errorf("failed to check superuser status: %w", err)
	}
	if isSuperuser {
		return true, nil
	}

	// Check if connection is shared
	var isShared bool
	err = pool.QueryRow(ctx, "SELECT is_shared FROM connections WHERE id = $1", connectionID).Scan(&isShared)
	if err != nil {
		return false, fmt.Errorf("failed to check connection shared status: %w", err)
	}

	// If not shared, check if it's owned by the user
	if !isShared {
		var ownerUsername *string
		err = pool.QueryRow(ctx, "SELECT owner_username FROM connections WHERE id = $1", connectionID).Scan(&ownerUsername)
		if err != nil {
			return false, fmt.Errorf("failed to check connection owner: %w", err)
		}

		// Get user's username
		var username string
		err = pool.QueryRow(ctx, "SELECT username FROM user_accounts WHERE id = $1", userID).Scan(&username)
		if err != nil {
			return false, fmt.Errorf("failed to get username: %w", err)
		}

		if ownerUsername != nil && *ownerUsername == username {
			return true, nil
		}

		// Not shared and not owned by user
		return false, nil
	}

	// For shared connections, check if any groups are assigned to it
	var groupCount int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM connection_privileges WHERE connection_id = $1", connectionID).Scan(&groupCount)
	if err != nil {
		return false, fmt.Errorf("failed to count connection privileges: %w", err)
	}

	// If no groups assigned to this shared connection, default to deny for security
	if groupCount == 0 {
		return false, nil
	}

	// Get all groups the user belongs to
	userGroups, err := GetUserGroups(ctx, pool, userID)
	if err != nil {
		return false, fmt.Errorf("failed to get user groups: %w", err)
	}

	if len(userGroups) == 0 {
		// User is not in any groups and connection has restricted access
		return false, nil
	}

	// Check if any of the user's groups have access to this connection
	// Build a query to check if any of the user's groups have the required access level
	query := `
		SELECT COUNT(*)
		FROM connection_privileges
		WHERE connection_id = $1
		  AND group_id = ANY($2)
		  AND (
			  access_level = 'read_write'
			  OR ($3 = 'read' AND access_level = 'read')
		  )
	`

	var matchCount int
	err = pool.QueryRow(ctx, query, connectionID, userGroups, requestedLevel).Scan(&matchCount)
	if err != nil {
		return false, fmt.Errorf("failed to check connection privileges: %w", err)
	}

	return matchCount > 0, nil
}

// CanAccessMCPItem checks if a user can access an MCP tool, resource, or prompt.
// Returns true if:
// - User is a superuser, OR
// - No groups have been assigned this privilege (accessible to all), OR
// - User is in a group that has been granted this privilege
func CanAccessMCPItem(ctx context.Context, pool *pgxpool.Pool, userID int, itemIdentifier string) (bool, error) {
	// Check if user is superuser
	var isSuperuser bool
	err := pool.QueryRow(ctx, "SELECT is_superuser FROM user_accounts WHERE id = $1", userID).Scan(&isSuperuser)
	if err != nil {
		return false, fmt.Errorf("failed to check superuser status: %w", err)
	}
	if isSuperuser {
		return true, nil
	}

	// Check if the privilege identifier exists
	var privilegeID int
	err = pool.QueryRow(ctx, "SELECT id FROM mcp_privilege_identifiers WHERE identifier = $1", itemIdentifier).Scan(&privilegeID)
	if err != nil {
		// If privilege identifier doesn't exist, deny access by default
		return false, fmt.Errorf("privilege identifier not found: %s", itemIdentifier)
	}

	// Check if any groups have been assigned this privilege
	var groupCount int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM group_mcp_privileges WHERE privilege_identifier_id = $1", privilegeID).Scan(&groupCount)
	if err != nil {
		return false, fmt.Errorf("failed to count MCP privileges: %w", err)
	}

	// If no groups assigned to this privilege, default to deny for security
	// This ensures that administrative operations require explicit privilege grants
	if groupCount == 0 {
		return false, nil
	}

	// Get all groups the user belongs to
	userGroups, err := GetUserGroups(ctx, pool, userID)
	if err != nil {
		return false, fmt.Errorf("failed to get user groups: %w", err)
	}

	if len(userGroups) == 0 {
		// User is not in any groups and privilege has restricted access
		return false, nil
	}

	// Check if any of the user's groups have this privilege
	query := `
		SELECT COUNT(*)
		FROM group_mcp_privileges
		WHERE privilege_identifier_id = $1
		  AND group_id = ANY($2)
	`

	var matchCount int
	err = pool.QueryRow(ctx, query, privilegeID, userGroups).Scan(&matchCount)
	if err != nil {
		return false, fmt.Errorf("failed to check MCP privileges: %w", err)
	}

	return matchCount > 0, nil
}

// ValidateGroupHierarchy checks if adding a group as a member would create a circular reference.
// Returns nil if valid, error if circular reference detected.
func ValidateGroupHierarchy(ctx context.Context, pool *pgxpool.Pool, parentGroupID int, memberGroupID int) error {
	// A group cannot be a member of itself
	if parentGroupID == memberGroupID {
		return fmt.Errorf("a group cannot be a member of itself")
	}

	// Check if adding this membership would create a cycle
	// Use recursive CTE to find all ancestor groups of the parent
	query := `
		WITH RECURSIVE ancestor_groups AS (
			-- Base case: the group we want to add as a parent
			SELECT $1::INTEGER as group_id

			UNION

			-- Recursive case: find groups that contain this group
			SELECT gm.parent_group_id
			FROM group_memberships gm
			INNER JOIN ancestor_groups ag ON gm.member_group_id = ag.group_id
		)
		SELECT COUNT(*)
		FROM ancestor_groups
		WHERE group_id = $2;
	`

	var matchCount int
	err := pool.QueryRow(ctx, query, parentGroupID, memberGroupID).Scan(&matchCount)
	if err != nil {
		return fmt.Errorf("failed to validate group hierarchy: %w", err)
	}

	if matchCount > 0 {
		return fmt.Errorf("adding this membership would create a circular reference")
	}

	return nil
}
