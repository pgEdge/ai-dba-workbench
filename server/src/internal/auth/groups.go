/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package auth

import (
	"database/sql"
	"fmt"
	"time"
)

// =============================================================================
// Group Management
// =============================================================================

// CreateGroup creates a new user group
func (s *AuthStore) CreateGroup(name, description string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(
		"INSERT INTO user_groups (name, description) VALUES (?, ?)",
		name, description,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to create group: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get group ID: %w", err)
	}

	return id, nil
}

// UpdateGroup updates an existing group's name and/or description
func (s *AuthStore) UpdateGroup(id int64, name, description string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(
		"UPDATE user_groups SET name = ?, description = ? WHERE id = ?",
		name, description, id,
	)
	if err != nil {
		return fmt.Errorf("failed to update group: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("group not found: %d", id)
	}

	return nil
}

// DeleteGroup deletes a group (cascade deletes memberships via foreign key)
func (s *AuthStore) DeleteGroup(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec("DELETE FROM user_groups WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete group: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("group not found: %d", id)
	}

	return nil
}

// GetGroup retrieves a group by ID
func (s *AuthStore) GetGroup(id int64) (*UserGroup, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var group UserGroup
	err := s.db.QueryRow(
		"SELECT id, name, description, created_at FROM user_groups WHERE id = ?",
		id,
	).Scan(&group.ID, &group.Name, &group.Description, &group.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get group: %w", err)
	}

	return &group, nil
}

// GetGroupByName retrieves a group by name
func (s *AuthStore) GetGroupByName(name string) (*UserGroup, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var group UserGroup
	err := s.db.QueryRow(
		"SELECT id, name, description, created_at FROM user_groups WHERE name = ?",
		name,
	).Scan(&group.ID, &group.Name, &group.Description, &group.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get group: %w", err)
	}

	return &group, nil
}

// ListGroups returns all groups
func (s *AuthStore) ListGroups() ([]*UserGroup, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		"SELECT id, name, description, created_at FROM user_groups ORDER BY name",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list groups: %w", err)
	}
	defer rows.Close()

	var groups []*UserGroup
	for rows.Next() {
		var group UserGroup
		if err := rows.Scan(&group.ID, &group.Name, &group.Description, &group.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan group: %w", err)
		}
		groups = append(groups, &group)
	}

	return groups, nil
}

// =============================================================================
// Group Membership Management
// =============================================================================

// AddUserToGroup adds a user as a member of a group
func (s *AuthStore) AddUserToGroup(groupID, userID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(
		"INSERT INTO group_memberships (parent_group_id, member_user_id) VALUES (?, ?)",
		groupID, userID,
	)
	if err != nil {
		return fmt.Errorf("failed to add user to group: %w", err)
	}

	return nil
}

// AddGroupToGroup adds a child group as a member of a parent group (nested groups)
func (s *AuthStore) AddGroupToGroup(parentGroupID, childGroupID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for circular reference before adding
	if err := s.validateNoCircularReferenceInternal(parentGroupID, childGroupID); err != nil {
		return err
	}

	_, err := s.db.Exec(
		"INSERT INTO group_memberships (parent_group_id, member_group_id) VALUES (?, ?)",
		parentGroupID, childGroupID,
	)
	if err != nil {
		return fmt.Errorf("failed to add group to group: %w", err)
	}

	return nil
}

// RemoveUserFromGroup removes a user from a group
func (s *AuthStore) RemoveUserFromGroup(groupID, userID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(
		"DELETE FROM group_memberships WHERE parent_group_id = ? AND member_user_id = ?",
		groupID, userID,
	)
	if err != nil {
		return fmt.Errorf("failed to remove user from group: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("user is not a member of this group")
	}

	return nil
}

// RemoveGroupFromGroup removes a child group from a parent group
func (s *AuthStore) RemoveGroupFromGroup(parentGroupID, childGroupID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(
		"DELETE FROM group_memberships WHERE parent_group_id = ? AND member_group_id = ?",
		parentGroupID, childGroupID,
	)
	if err != nil {
		return fmt.Errorf("failed to remove group from group: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("group is not a member of this group")
	}

	return nil
}

// GetGroupMembers returns all direct members of a group (users and child groups)
func (s *AuthStore) GetGroupMembers(groupID int64) (*GroupWithMembers, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Get the group
	var group UserGroup
	err := s.db.QueryRow(
		"SELECT id, name, description, created_at FROM user_groups WHERE id = ?",
		groupID,
	).Scan(&group.ID, &group.Name, &group.Description, &group.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("group not found: %d", groupID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get group: %w", err)
	}

	result := &GroupWithMembers{Group: group}

	// Get user members
	userRows, err := s.db.Query(`
        SELECT u.username
        FROM group_memberships gm
        JOIN users u ON gm.member_user_id = u.id
        WHERE gm.parent_group_id = ?
        ORDER BY u.username
    `, groupID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user members: %w", err)
	}
	defer userRows.Close()

	for userRows.Next() {
		var username string
		if err := userRows.Scan(&username); err != nil {
			return nil, fmt.Errorf("failed to scan user member: %w", err)
		}
		result.UserMembers = append(result.UserMembers, username)
	}

	// Get group members
	groupRows, err := s.db.Query(`
        SELECT g.name
        FROM group_memberships gm
        JOIN user_groups g ON gm.member_group_id = g.id
        WHERE gm.parent_group_id = ?
        ORDER BY g.name
    `, groupID)
	if err != nil {
		return nil, fmt.Errorf("failed to get group members: %w", err)
	}
	defer groupRows.Close()

	for groupRows.Next() {
		var groupName string
		if err := groupRows.Scan(&groupName); err != nil {
			return nil, fmt.Errorf("failed to scan group member: %w", err)
		}
		result.GroupMembers = append(result.GroupMembers, groupName)
	}

	return result, nil
}

// =============================================================================
// Group Hierarchy Traversal
// =============================================================================

// GetUserGroups returns all group IDs that a user belongs to (including through inheritance)
// Uses recursive CTE to traverse the group hierarchy
func (s *AuthStore) GetUserGroups(userID int64) ([]int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.getUserGroupsInternal(userID)
}

// getUserGroupsInternal is the internal implementation (assumes lock is held)
func (s *AuthStore) getUserGroupsInternal(userID int64) ([]int64, error) {
	// Recursive CTE to find all groups the user belongs to through the hierarchy
	query := `
        WITH RECURSIVE user_groups_cte AS (
            -- Base case: direct group memberships
            SELECT parent_group_id as group_id
            FROM group_memberships
            WHERE member_user_id = ?

            UNION

            -- Recursive case: parent groups of groups we're already members of
            SELECT gm.parent_group_id
            FROM group_memberships gm
            INNER JOIN user_groups_cte ugc ON gm.member_group_id = ugc.group_id
        )
        SELECT DISTINCT group_id FROM user_groups_cte
    `

	rows, err := s.db.Query(query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user groups: %w", err)
	}
	defer rows.Close()

	var groupIDs []int64
	for rows.Next() {
		var groupID int64
		if err := rows.Scan(&groupID); err != nil {
			return nil, fmt.Errorf("failed to scan group ID: %w", err)
		}
		groupIDs = append(groupIDs, groupID)
	}

	return groupIDs, nil
}

// GetUserGroupsByUsername returns all group IDs that a user belongs to by username
func (s *AuthStore) GetUserGroupsByUsername(username string) ([]int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Get user ID first
	var userID int64
	err := s.db.QueryRow("SELECT id FROM users WHERE username = ?", username).Scan(&userID)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user not found: %s", username)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	return s.getUserGroupsInternal(userID)
}

// =============================================================================
// Circular Reference Prevention
// =============================================================================

// ValidateNoCircularReference checks if adding childGroupID to parentGroupID would create a cycle
func (s *AuthStore) ValidateNoCircularReference(parentGroupID, childGroupID int64) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.validateNoCircularReferenceInternal(parentGroupID, childGroupID)
}

// validateNoCircularReferenceInternal is the internal implementation (assumes lock is held)
func (s *AuthStore) validateNoCircularReferenceInternal(parentGroupID, childGroupID int64) error {
	// Check if parentGroupID is the same as childGroupID
	if parentGroupID == childGroupID {
		return fmt.Errorf("cannot add group to itself")
	}

	// Use recursive CTE to check if parentGroupID is reachable as a descendant of childGroupID
	// If so, adding childGroupID to parentGroupID would create a cycle
	// We traverse DOWN the hierarchy from childGroupID to see if we can reach parentGroupID
	query := `
        WITH RECURSIVE descendants AS (
            -- Start from the child group's children
            SELECT member_group_id as group_id
            FROM group_memberships
            WHERE parent_group_id = ?
            AND member_group_id IS NOT NULL

            UNION

            -- Recursively find all descendants
            SELECT gm.member_group_id
            FROM group_memberships gm
            INNER JOIN descendants d ON gm.parent_group_id = d.group_id
            WHERE gm.member_group_id IS NOT NULL
        )
        SELECT COUNT(*) FROM descendants WHERE group_id = ?
    `

	var count int
	err := s.db.QueryRow(query, childGroupID, parentGroupID).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check circular reference: %w", err)
	}

	if count > 0 {
		return fmt.Errorf("adding this group would create a circular reference")
	}

	return nil
}

// =============================================================================
// Helper Methods
// =============================================================================

// GetUserID returns the ID for a username
func (s *AuthStore) GetUserID(username string) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var userID int64
	err := s.db.QueryRow("SELECT id FROM users WHERE username = ?", username).Scan(&userID)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("user not found: %s", username)
	}
	if err != nil {
		return 0, fmt.Errorf("failed to get user ID: %w", err)
	}

	return userID, nil
}

// IsUserInGroup checks if a user is a member of a group (directly or through inheritance)
func (s *AuthStore) IsUserInGroup(userID, groupID int64) (bool, error) {
	groups, err := s.GetUserGroups(userID)
	if err != nil {
		return false, err
	}

	for _, gid := range groups {
		if gid == groupID {
			return true, nil
		}
	}

	return false, nil
}

// GetGroupsForUser returns detailed group information for a user
func (s *AuthStore) GetGroupsForUser(userID int64) ([]*UserGroup, error) {
	groupIDs, err := s.GetUserGroups(userID)
	if err != nil {
		return nil, err
	}

	if len(groupIDs) == 0 {
		return nil, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var groups []*UserGroup
	for _, gid := range groupIDs {
		var group UserGroup
		err := s.db.QueryRow(
			"SELECT id, name, description, created_at FROM user_groups WHERE id = ?",
			gid,
		).Scan(&group.ID, &group.Name, &group.Description, &group.CreatedAt)
		if err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("failed to get group %d: %w", gid, err)
		}
		if err == nil {
			groups = append(groups, &group)
		}
	}

	return groups, nil
}

// SetUserSuperuser sets or clears the superuser flag for a user
func (s *AuthStore) SetUserSuperuser(username string, isSuperuser bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(
		"UPDATE users SET is_superuser = ? WHERE username = ?",
		isSuperuser, username,
	)
	if err != nil {
		return fmt.Errorf("failed to update superuser status: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("user not found: %s", username)
	}

	return nil
}

// GetTokenByID retrieves a token by ID
func (s *AuthStore) GetTokenByID(tokenID int64) (*StoredToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var token StoredToken
	err := s.db.QueryRow(
		`SELECT id, token_hash, token_type, owner_id, expires_at, annotation, created_at, database, is_superuser
         FROM tokens WHERE id = ?`,
		tokenID,
	).Scan(&token.ID, &token.TokenHash, &token.TokenType, &token.OwnerID,
		&token.ExpiresAt, &token.Annotation, &token.CreatedAt, &token.Database, &token.IsSuperuser)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	return &token, nil
}

// GroupCount returns the number of groups in the store
func (s *AuthStore) GroupCount() int {
	var count int
	//nolint:errcheck // Returns 0 on error, which is acceptable
	s.db.QueryRow("SELECT COUNT(*) FROM user_groups").Scan(&count)
	return count
}

// Unused time import workaround
var _ = time.Now
