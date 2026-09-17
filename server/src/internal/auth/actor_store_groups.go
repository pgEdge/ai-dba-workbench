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

// memberTypeUser and memberTypeGroup are the member kinds recorded in a
// group.member.add or group.member.remove event's details.
const (
	memberTypeUser  = "user"
	memberTypeGroup = "group"
)

// groupSnapshot is the audited view of a user_groups row. It carries
// only the fields an administrator changes, so a before or after
// snapshot stays readable in the audit log.
type groupSnapshot struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// groupSnapshotColumns lists the snapshot columns in the order
// groupSnapshotTx scans them.
const groupSnapshotColumns = `id, name, description`

// groupSnapshotTx reads a group's snapshot inside the caller's
// transaction, returning sql.ErrNoRows unwrapped when the group does
// not exist so that callers can render their own not-found message.
func groupSnapshotTx(tx *sql.Tx, id int64) (groupSnapshot, error) {
	var (
		snap        groupSnapshot
		description sql.NullString
	)

	err := tx.QueryRow(
		"SELECT "+groupSnapshotColumns+" FROM user_groups WHERE id = ?", id,
	).Scan(&snap.ID, &snap.Name, &description)
	if err != nil {
		return snap, err
	}
	snap.Description = description.String

	return snap, nil
}

// groupLookupFailed wraps a group lookup error that is not a plain
// missing row, so that a broken schema reads differently from a group
// that simply does not exist.
func groupLookupFailed(err error) error {
	return fmt.Errorf("failed to look up group: %w", err)
}

// groupMembers lists a group's direct members by name, split by kind.
// Both fields are always non-nil so that the audit details carry []
// rather than null.
type groupMembers struct {
	Users  []string `json:"users"`
	Groups []string `json:"groups"`
}

// connectionPrivilegeSnapshot is the audited view of one connection
// privilege grant held by a group.
type connectionPrivilegeSnapshot struct {
	ConnectionID int    `json:"connection_id"`
	AccessLevel  string `json:"access_level"`
}

// scanStringsTx collects a single-column string result set, returning
// an empty slice rather than nil when there are no rows.
func scanStringsTx(tx *sql.Tx, query, failure string, args ...any) ([]string, error) {
	rows, err := tx.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", failure, err)
	}
	defer rows.Close()

	values := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("%s: %w", failure, err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", failure, err)
	}

	return values, nil
}

// groupMembersTx lists the group's direct user and nested group members
// by name, in name order.
func groupMembersTx(tx *sql.Tx, groupID int64) (groupMembers, error) {
	members := groupMembers{Users: []string{}, Groups: []string{}}

	users, err := scanStringsTx(tx, `
        SELECT u.username
        FROM group_memberships m
        JOIN users u ON u.id = m.member_user_id
        WHERE m.parent_group_id = ?
        ORDER BY u.username`, "failed to list group members", groupID)
	if err != nil {
		return members, err
	}
	members.Users = users

	groups, err := scanStringsTx(tx, `
        SELECT g.name
        FROM group_memberships m
        JOIN user_groups g ON g.id = m.member_group_id
        WHERE m.parent_group_id = ?
        ORDER BY g.name`, "failed to list group members", groupID)
	if err != nil {
		return members, err
	}
	members.Groups = groups

	return members, nil
}

// groupMCPPrivilegesTx lists the MCP privilege identifiers granted to
// the group. The wildcard grant is stored as the sentinel identifier id
// MCPPrivilegeIDWildcard, which matches no row in
// mcp_privilege_identifiers, so it is rendered as "*".
func groupMCPPrivilegesTx(tx *sql.Tx, groupID int64) ([]string, error) {
	return scanStringsTx(tx, `
        SELECT CASE WHEN gmp.privilege_identifier_id = ? THEN '*'
                    ELSE COALESCE(mpi.identifier, '') END AS identifier
        FROM group_mcp_privileges gmp
        LEFT JOIN mcp_privilege_identifiers mpi
            ON mpi.id = gmp.privilege_identifier_id
        WHERE gmp.group_id = ?
        ORDER BY identifier`, "failed to list group MCP privileges",
		MCPPrivilegeIDWildcard, groupID)
}

// groupAdminPermissionsTx lists the admin permissions granted to the
// group, in permission order.
func groupAdminPermissionsTx(tx *sql.Tx, groupID int64) ([]string, error) {
	return scanStringsTx(tx, `
        SELECT permission FROM group_admin_permissions
        WHERE group_id = ?
        ORDER BY permission`, "failed to list group admin permissions", groupID)
}

// groupConnectionPrivilegesTx lists the connection privilege grants
// held by the group, in connection order.
func groupConnectionPrivilegesTx(tx *sql.Tx,
	groupID int64) ([]connectionPrivilegeSnapshot, error) {

	rows, err := tx.Query(`
        SELECT connection_id, access_level FROM connection_privileges
        WHERE group_id = ?
        ORDER BY connection_id`, groupID)
	if err != nil {
		return nil, fmt.Errorf("failed to list group connection privileges: %w", err)
	}
	defer rows.Close()

	privileges := []connectionPrivilegeSnapshot{}
	for rows.Next() {
		var priv connectionPrivilegeSnapshot
		if err := rows.Scan(&priv.ConnectionID, &priv.AccessLevel); err != nil {
			return nil, fmt.Errorf("failed to list group connection privileges: %w", err)
		}
		privileges = append(privileges, priv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to list group connection privileges: %w", err)
	}

	return privileges, nil
}

// groupDeleteDetailsTx assembles the details of a group.delete event:
// the group's own before snapshot alongside everything the delete is
// about to remove, so that the event alone says what access was lost.
// It must be called before the dependent rows are deleted.
func groupDeleteDetailsTx(tx *sql.Tx, before groupSnapshot) (map[string]any, error) {
	members, err := groupMembersTx(tx, before.ID)
	if err != nil {
		return nil, err
	}

	mcpPrivileges, err := groupMCPPrivilegesTx(tx, before.ID)
	if err != nil {
		return nil, err
	}

	connectionPrivileges, err := groupConnectionPrivilegesTx(tx, before.ID)
	if err != nil {
		return nil, err
	}

	adminPermissions, err := groupAdminPermissionsTx(tx, before.ID)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"before":                before,
		"members":               members,
		"mcp_privileges":        mcpPrivileges,
		"connection_privileges": connectionPrivileges,
		"admin_permissions":     adminPermissions,
	}, nil
}

// groupMemberNameTx resolves a membership target's display name inside
// the caller's transaction. A member row that no longer resolves yields
// an empty name rather than an error, because the audit event must
// still be recorded for the change that is being made.
func groupMemberNameTx(tx *sql.Tx, memberType string, memberID int64) (string, error) {
	var query string
	switch memberType {
	case memberTypeUser:
		query = "SELECT username FROM users WHERE id = ?"
	case memberTypeGroup:
		query = "SELECT name FROM user_groups WHERE id = ?"
	default:
		return "", fmt.Errorf("unsupported member type %q", memberType)
	}

	var name string
	err := tx.QueryRow(query, memberID).Scan(&name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("failed to look up group member: %w", err)
	}

	return name, nil
}

// membershipDetails renders the details shared by the group.member.add
// and group.member.remove events.
func membershipDetails(memberType string, memberID int64,
	memberName string) map[string]any {

	return map[string]any{
		"member_type": memberType,
		"member_id":   memberID,
		"member_name": memberName,
	}
}

// =============================================================================
// Actor-attributed group mutations
// =============================================================================

// CreateGroup creates a new user group, attributing the change to this
// store's actor.
func (a *ActorStore) CreateGroup(name, description string) (int64, error) {
	return a.s.createGroup(a.actor, name, description)
}

// UpdateGroup updates an existing group's name and/or description,
// attributing the change to this store's actor.
func (a *ActorStore) UpdateGroup(id int64, name, description string) error {
	return a.s.updateGroup(a.actor, id, name, description)
}

// DeleteGroup deletes a group and all of its dependent rows,
// attributing the change to this store's actor.
func (a *ActorStore) DeleteGroup(id int64) error {
	return a.s.deleteGroup(a.actor, id)
}

// AddUserToGroup adds a user as a member of a group, attributing the
// change to this store's actor.
func (a *ActorStore) AddUserToGroup(groupID, userID int64) error {
	return a.s.addUserToGroup(a.actor, groupID, userID)
}

// AddGroupToGroup adds a child group as a member of a parent group,
// attributing the change to this store's actor.
func (a *ActorStore) AddGroupToGroup(parentGroupID, childGroupID int64) error {
	return a.s.addGroupToGroup(a.actor, parentGroupID, childGroupID)
}

// RemoveUserFromGroup removes a user from a group, attributing the
// change to this store's actor.
func (a *ActorStore) RemoveUserFromGroup(groupID, userID int64) error {
	return a.s.removeUserFromGroup(a.actor, groupID, userID)
}

// RemoveGroupFromGroup removes a child group from a parent group,
// attributing the change to this store's actor.
func (a *ActorStore) RemoveGroupFromGroup(parentGroupID, childGroupID int64) error {
	return a.s.removeGroupFromGroup(a.actor, parentGroupID, childGroupID)
}
