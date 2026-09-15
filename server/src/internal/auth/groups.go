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
	"strings"
)

// ErrGroupNameExists is returned by CreateGroup and UpdateGroup when the
// requested group name collides with an existing group. The schema marks
// user_groups.name UNIQUE, so the underlying driver reports a UNIQUE
// constraint violation; the store maps that violation to this sentinel so
// the API layer can return a 409 Conflict rather than a generic 500.
var ErrGroupNameExists = errors.New("a group with this name already exists")

// ErrGroupNotFound is returned (wrapped) by UpdateGroup when the requested
// group id does not match any existing group. The store reports this as a
// sentinel so the API layer can distinguish a missing-group update from
// other failures and return a 404 Not Found rather than a generic 500.
var ErrGroupNotFound = errors.New("group not found")

// isUniqueViolation reports whether err represents a SQLite UNIQUE
// constraint failure on the named column. The modernc.org/sqlite driver
// surfaces these as an error whose message has the stable form
// "UNIQUE constraint failed: <table>.<column>", so a substring match on
// that text is the portable way to detect the collision without importing
// the driver's concrete error type.
func isUniqueViolation(err error, qualifiedColumn string) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") &&
		strings.Contains(msg, qualifiedColumn)
}

// =============================================================================
// Group Management
// =============================================================================

// CreateGroup creates a new user group, attributing the change to the
// system actor. When the name collides with an existing group it
// returns ErrGroupNameExists (wrapped) so the caller can distinguish a
// duplicate-name conflict from other failures.
func (s *AuthStore) CreateGroup(name, description string) (int64, error) {
	return s.createGroup(systemActor, name, description)
}

func (s *AuthStore) createGroup(actor Actor, name,
	description string) (id int64, err error) {

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	target := &auditTarget{
		action:     "group.create",
		targetType: "group",
		targetName: name,
	}
	defer func() {
		if err != nil {
			s.failAudit(tx, actor, target, err)
		}
	}()

	result, execErr := tx.Exec(
		"INSERT INTO user_groups (name, description) VALUES (?, ?)",
		name, description,
	)
	if execErr != nil {
		if isUniqueViolation(execErr, "user_groups.name") {
			err = fmt.Errorf("failed to create group: %w", ErrGroupNameExists)
		} else {
			err = fmt.Errorf("failed to create group: %w", execErr)
		}
		return 0, err
	}

	newID, idErr := result.LastInsertId()
	if idErr != nil {
		err = fmt.Errorf("failed to get group ID: %w", idErr)
		return 0, err
	}
	target.targetID = &newID

	after, snapErr := groupSnapshotTx(tx, newID)
	if snapErr != nil {
		err = fmt.Errorf("failed to read created group: %w", snapErr)
		return 0, err
	}

	if auditErr := s.recordAudit(tx, newEvent(actor, "group.create", "group",
		&newID, after.Name, map[string]any{"after": after})); auditErr != nil {
		err = auditErr
		return 0, err
	}

	if err = tx.Commit(); err != nil {
		err = fmt.Errorf("failed to commit transaction: %w", err)
		return 0, err
	}

	return newID, nil
}

// UpdateGroup updates an existing group's name and/or description,
// attributing the change to the system actor. An empty name leaves the
// existing name untouched, so a caller can update only the description
// without clobbering the name; the name column is otherwise UNIQUE, so
// a colliding name returns ErrGroupNameExists (wrapped).
func (s *AuthStore) UpdateGroup(id int64, name, description string) error {
	return s.updateGroup(systemActor, id, name, description)
}

func (s *AuthStore) updateGroup(actor Actor, id int64, name,
	description string) (err error) {

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	target := &auditTarget{
		action:     "group.update",
		targetType: "group",
		targetID:   &id,
	}
	defer func() {
		if err != nil {
			s.failAudit(tx, actor, target, err)
		}
	}()

	before, snapErr := groupSnapshotTx(tx, id)
	if snapErr != nil {
		if errors.Is(snapErr, sql.ErrNoRows) {
			err = fmt.Errorf("group not found: %d: %w", id, ErrGroupNotFound)
		} else {
			err = groupLookupFailed(snapErr)
		}
		return err
	}
	target.targetName = before.Name

	var query string
	var args []any
	if name == "" {
		query = "UPDATE user_groups SET description = ? WHERE id = ?"
		args = []any{description, id}
	} else {
		query = "UPDATE user_groups SET name = ?, description = ? WHERE id = ?"
		args = []any{name, description, id}
	}

	result, execErr := tx.Exec(query, args...)
	if execErr != nil {
		if isUniqueViolation(execErr, "user_groups.name") {
			err = fmt.Errorf("failed to update group: %w", ErrGroupNameExists)
		} else {
			err = fmt.Errorf("failed to update group: %w", execErr)
		}
		return err
	}

	rows, rowsErr := result.RowsAffected()
	if rowsErr != nil {
		err = fmt.Errorf("failed to get rows affected: %w", rowsErr)
		return err
	}
	if rows == 0 {
		// Unreachable in practice, because the snapshot above proves
		// the row exists and s.mu admits one writer at a time; kept as
		// a guard against a concurrent deleter outside this store.
		err = fmt.Errorf("group not found: %d: %w", id, ErrGroupNotFound)
		return err
	}

	after := before
	after.Description = description
	if name != "" {
		after.Name = name
	}

	if auditErr := s.recordAudit(tx, newEvent(actor, "group.update", "group",
		&id, before.Name, map[string]any{
			"before": before,
			"after":  after,
		})); auditErr != nil {
		err = auditErr
		return err
	}

	if err = tx.Commit(); err != nil {
		err = fmt.Errorf("failed to commit transaction: %w", err)
		return err
	}

	return nil
}

// DeleteGroup deletes a group and all of its dependent rows in a single
// atomic transaction, attributing the change to the system actor. This
// removes the group's memberships (both as a parent and as a nested
// child), its MCP and connection privilege grants, and its admin
// permissions. The schema declares ON DELETE CASCADE on these foreign
// keys and NewAuthStore enables the foreign_keys pragma in the DSN, so
// the cascades do fire; the explicit deletes are kept as defense in
// depth, so that a database opened without the pragma cannot leave
// orphaned privilege rows behind to be attached to a future group
// reusing the same id.
func (s *AuthStore) DeleteGroup(id int64) error {
	return s.deleteGroup(systemActor, id)
}

func (s *AuthStore) deleteGroup(actor Actor, id int64) (err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	target := &auditTarget{
		action:     "group.delete",
		targetType: "group",
		targetID:   &id,
	}
	defer func() {
		if err != nil {
			s.failAudit(tx, actor, target, err)
		}
	}()

	// Look up the group first so we can fail fast on "not found" and
	// capture both the before snapshot and the audit details describing
	// everything the delete is about to remove.
	before, details, beforeErr := groupDeleteBeforeTx(tx, id)
	if beforeErr != nil {
		err = beforeErr
		return err
	}
	target.targetName = before.Name

	if depErr := deleteGroupDependentsTx(tx, id); depErr != nil {
		err = depErr
		return err
	}

	if rowErr := deleteGroupRowTx(tx, id); rowErr != nil {
		err = rowErr
		return err
	}

	if auditErr := s.recordAudit(tx, newEvent(actor, "group.delete", "group",
		&id, before.Name, details)); auditErr != nil {
		err = auditErr
		return err
	}

	if commitErr := tx.Commit(); commitErr != nil {
		err = fmt.Errorf("failed to commit group deletion: %w", commitErr)
		return err
	}

	return nil
}

// groupDeleteBeforeTx captures the state a group delete is about to
// destroy: the group's own snapshot, and the audit details listing the
// memberships, privileges and permissions that go with it. It is read
// before any dependent row is removed, so the event says what access
// was lost.
func groupDeleteBeforeTx(tx *sql.Tx, id int64) (groupSnapshot, map[string]any,
	error) {

	before, snapErr := groupSnapshotTx(tx, id)
	if snapErr != nil {
		if errors.Is(snapErr, sql.ErrNoRows) {
			return before, nil, fmt.Errorf("group not found: %d", id)
		}
		return before, nil, groupLookupFailed(snapErr)
	}

	details, detailsErr := groupDeleteDetailsTx(tx, before)
	if detailsErr != nil {
		return before, nil, detailsErr
	}

	return before, details, nil
}

// deleteGroupDependentsTx removes every row that references the group:
// its membership rows on both sides, its MCP and connection privilege
// grants, and its admin permission grants.
func deleteGroupDependentsTx(tx *sql.Tx, id int64) error {
	// Remove the group's membership rows. A group can appear either as the
	// parent_group_id (its own direct members) or as a nested
	// member_group_id inside another group; both sides must be cleared.
	if _, execErr := tx.Exec(
		"DELETE FROM group_memberships WHERE parent_group_id = ? OR member_group_id = ?",
		id, id,
	); execErr != nil {
		return fmt.Errorf("failed to delete group memberships: %w", execErr)
	}

	// Remove MCP privilege grants held by this group.
	if _, execErr := tx.Exec(
		"DELETE FROM group_mcp_privileges WHERE group_id = ?", id,
	); execErr != nil {
		return fmt.Errorf("failed to delete group MCP privileges: %w", execErr)
	}

	// Remove connection privilege grants held by this group.
	if _, execErr := tx.Exec(
		"DELETE FROM connection_privileges WHERE group_id = ?", id,
	); execErr != nil {
		return fmt.Errorf("failed to delete group connection privileges: %w", execErr)
	}

	// Remove admin permission grants held by this group.
	if _, execErr := tx.Exec(
		"DELETE FROM group_admin_permissions WHERE group_id = ?", id,
	); execErr != nil {
		return fmt.Errorf("failed to delete group admin permissions: %w", execErr)
	}

	return nil
}

// deleteGroupRowTx deletes the group row itself. We use RowsAffected on
// this statement to report the "not found" case; the dependent deletes
// are no-ops when the group never existed, so they cannot mask a
// missing-group error here.
func deleteGroupRowTx(tx *sql.Tx, id int64) error {
	result, execErr := tx.Exec("DELETE FROM user_groups WHERE id = ?", id)
	if execErr != nil {
		return fmt.Errorf("failed to delete group: %w", execErr)
	}

	rows, rowsErr := result.RowsAffected()
	if rowsErr != nil {
		return fmt.Errorf("failed to get rows affected: %w", rowsErr)
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

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating groups: %w", err)
	}

	return groups, nil
}

// GroupWithMemberCount pairs a group with its direct member count.
type GroupWithMemberCount struct {
	UserGroup
	MemberCount int `json:"member_count"`
}

// ListGroupsWithMemberCount returns all groups with their direct member
// counts in a single query, avoiding N+1 lookups.
func (s *AuthStore) ListGroupsWithMemberCount() ([]*GroupWithMemberCount, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
        SELECT g.id, g.name, g.description, g.created_at,
               COUNT(gm.id) AS member_count
        FROM user_groups g
        LEFT JOIN group_memberships gm ON g.id = gm.parent_group_id
        GROUP BY g.id, g.name, g.description, g.created_at
        ORDER BY g.name
    `)
	if err != nil {
		return nil, fmt.Errorf("failed to list groups with member counts: %w", err)
	}
	defer rows.Close()

	var groups []*GroupWithMemberCount
	for rows.Next() {
		var g GroupWithMemberCount
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.CreatedAt,
			&g.MemberCount); err != nil {
			return nil, fmt.Errorf("failed to scan group: %w", err)
		}
		groups = append(groups, &g)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating groups: %w", err)
	}

	return groups, nil
}

// =============================================================================
// Group Membership Management
// =============================================================================

// membershipChange applies one membership mutation and records the
// resulting group.member event, both inside a single transaction so
// that the change and the event are committed together. The group's
// name is read in the transaction; a group id that matches no row
// yields an empty target name and leaves apply to produce the error,
// which for a real mutation is the foreign key violation on the
// membership insert.
func (s *AuthStore) membershipChange(actor Actor, action, memberType string,
	groupID, memberID int64, apply func(tx *sql.Tx) error) (err error) {

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	target := &auditTarget{
		action:     action,
		targetType: "group",
		targetID:   &groupID,
	}
	defer func() {
		if err != nil {
			s.failAudit(tx, actor, target, err)
		}
	}()

	group, snapErr := groupSnapshotTx(tx, groupID)
	if snapErr != nil && !errors.Is(snapErr, sql.ErrNoRows) {
		err = groupLookupFailed(snapErr)
		return err
	}
	target.targetName = group.Name

	if applyErr := apply(tx); applyErr != nil {
		err = applyErr
		return err
	}

	memberName, nameErr := groupMemberNameTx(tx, memberType, memberID)
	if nameErr != nil {
		err = nameErr
		return err
	}

	if auditErr := s.recordAudit(tx, newEvent(actor, action, "group", &groupID,
		group.Name, membershipDetails(memberType, memberID,
			memberName))); auditErr != nil {
		err = auditErr
		return err
	}

	if err = tx.Commit(); err != nil {
		err = fmt.Errorf("failed to commit transaction: %w", err)
		return err
	}

	return nil
}

// AddUserToGroup adds a user as a member of a group, attributing the
// change to the system actor.
func (s *AuthStore) AddUserToGroup(groupID, userID int64) error {
	return s.addUserToGroup(systemActor, groupID, userID)
}

func (s *AuthStore) addUserToGroup(actor Actor, groupID, userID int64) error {
	return s.membershipChange(actor, "group.member.add", memberTypeUser,
		groupID, userID, func(tx *sql.Tx) error {
			if _, err := tx.Exec(
				"INSERT INTO group_memberships (parent_group_id, member_user_id) VALUES (?, ?)",
				groupID, userID,
			); err != nil {
				return fmt.Errorf("failed to add user to group: %w", err)
			}
			return nil
		})
}

// AddGroupToGroup adds a child group as a member of a parent group
// (nested groups), attributing the change to the system actor.
func (s *AuthStore) AddGroupToGroup(parentGroupID, childGroupID int64) error {
	return s.addGroupToGroup(systemActor, parentGroupID, childGroupID)
}

func (s *AuthStore) addGroupToGroup(actor Actor, parentGroupID,
	childGroupID int64) error {

	return s.membershipChange(actor, "group.member.add", memberTypeGroup,
		parentGroupID, childGroupID, func(tx *sql.Tx) error {
			// Check for circular reference before adding.
			if err := validateNoCircularReference(tx, parentGroupID,
				childGroupID); err != nil {
				return err
			}

			if _, err := tx.Exec(
				"INSERT INTO group_memberships (parent_group_id, member_group_id) VALUES (?, ?)",
				parentGroupID, childGroupID,
			); err != nil {
				return fmt.Errorf("failed to add group to group: %w", err)
			}
			return nil
		})
}

// RemoveUserFromGroup removes a user from a group, attributing the
// change to the system actor.
func (s *AuthStore) RemoveUserFromGroup(groupID, userID int64) error {
	return s.removeUserFromGroup(systemActor, groupID, userID)
}

func (s *AuthStore) removeUserFromGroup(actor Actor, groupID, userID int64) error {
	return s.membershipChange(actor, "group.member.remove", memberTypeUser,
		groupID, userID, func(tx *sql.Tx) error {
			result, err := tx.Exec(
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
				return errors.New("user is not a member of this group")
			}
			return nil
		})
}

// RemoveGroupFromGroup removes a child group from a parent group,
// attributing the change to the system actor.
func (s *AuthStore) RemoveGroupFromGroup(parentGroupID, childGroupID int64) error {
	return s.removeGroupFromGroup(systemActor, parentGroupID, childGroupID)
}

func (s *AuthStore) removeGroupFromGroup(actor Actor, parentGroupID,
	childGroupID int64) error {

	return s.membershipChange(actor, "group.member.remove", memberTypeGroup,
		parentGroupID, childGroupID, func(tx *sql.Tx) error {
			result, err := tx.Exec(
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
				return errors.New("group is not a member of this group")
			}
			return nil
		})
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

	if err := userRows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating user members: %w", err)
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

	if err := groupRows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating group members: %w", err)
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

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating user groups: %w", err)
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

	return validateNoCircularReference(s.db, parentGroupID, childGroupID)
}

// rowQuerier is the subset of *sql.DB and *sql.Tx that the
// circular-reference check needs, so the same check can run either
// standalone against the store's database or inside a mutation's
// transaction, where it must see that transaction's uncommitted rows.
type rowQuerier interface {
	QueryRow(query string, args ...any) *sql.Row
}

// validateNoCircularReference is the internal implementation (assumes
// the store's lock is held by the caller).
func validateNoCircularReference(q rowQuerier, parentGroupID, childGroupID int64) error {
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
	err := q.QueryRow(query, childGroupID, parentGroupID).Scan(&count)
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

// GetTokenByID retrieves a token by ID
func (s *AuthStore) GetTokenByID(tokenID int64) (*StoredToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var token StoredToken
	var annotation sql.NullString
	var database sql.NullString
	err := s.db.QueryRow(
		`SELECT id, token_hash, owner_id, expires_at, annotation, created_at, database
         FROM tokens WHERE id = ?`,
		tokenID,
	).Scan(&token.ID, &token.TokenHash, &token.OwnerID,
		&token.ExpiresAt, &annotation, &token.CreatedAt, &database)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	if annotation.Valid {
		token.Annotation = annotation.String
	}
	if database.Valid {
		token.Database = database.String
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
