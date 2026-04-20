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
	"os"
	"testing"
)

// createTestAuthStoreForGroups creates a temporary auth store for testing
func createTestAuthStoreForGroups(t *testing.T) (*AuthStore, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "auth-groups-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	store, err := NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create auth store: %v", err)
	}

	cleanup := func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}

	return store, cleanup
}

func TestCreateGroup(t *testing.T) {
	store, cleanup := createTestAuthStoreForGroups(t)
	defer cleanup()

	// Create a group
	groupID, err := store.CreateGroup("test-group", "A test group")
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	if groupID <= 0 {
		t.Errorf("Expected positive group ID, got %d", groupID)
	}

	// Verify the group exists
	group, err := store.GetGroup(groupID)
	if err != nil {
		t.Fatalf("Failed to get group: %v", err)
	}

	if group == nil {
		t.Fatal("Expected group to exist")
	}

	if group.Name != "test-group" {
		t.Errorf("Expected name 'test-group', got %q", group.Name)
	}

	if group.Description != "A test group" {
		t.Errorf("Expected description 'A test group', got %q", group.Description)
	}
}

func TestCreateGroupDuplicateName(t *testing.T) {
	store, cleanup := createTestAuthStoreForGroups(t)
	defer cleanup()

	// Create a group
	_, err := store.CreateGroup("test-group", "First group")
	if err != nil {
		t.Fatalf("Failed to create first group: %v", err)
	}

	// Try to create another group with the same name
	_, err = store.CreateGroup("test-group", "Second group")
	if err == nil {
		t.Error("Expected error when creating duplicate group name")
	}
}

func TestUpdateGroup(t *testing.T) {
	store, cleanup := createTestAuthStoreForGroups(t)
	defer cleanup()

	// Create a group
	groupID, err := store.CreateGroup("test-group", "Original description")
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	// Update the group
	err = store.UpdateGroup(groupID, "updated-group", "Updated description")
	if err != nil {
		t.Fatalf("Failed to update group: %v", err)
	}

	// Verify the update
	group, err := store.GetGroup(groupID)
	if err != nil {
		t.Fatalf("Failed to get group: %v", err)
	}

	if group.Name != "updated-group" {
		t.Errorf("Expected name 'updated-group', got %q", group.Name)
	}

	if group.Description != "Updated description" {
		t.Errorf("Expected description 'Updated description', got %q", group.Description)
	}
}

func TestDeleteGroup(t *testing.T) {
	store, cleanup := createTestAuthStoreForGroups(t)
	defer cleanup()

	// Create a group
	groupID, err := store.CreateGroup("test-group", "A test group")
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	// Delete the group
	err = store.DeleteGroup(groupID)
	if err != nil {
		t.Fatalf("Failed to delete group: %v", err)
	}

	// Verify the group no longer exists
	group, err := store.GetGroup(groupID)
	if err != nil {
		t.Fatalf("Failed to get group: %v", err)
	}

	if group != nil {
		t.Error("Expected group to be deleted")
	}
}

func TestDeleteNonExistentGroup(t *testing.T) {
	store, cleanup := createTestAuthStoreForGroups(t)
	defer cleanup()

	err := store.DeleteGroup(99999)
	if err == nil {
		t.Error("Expected error when deleting non-existent group")
	}
}

// TestDeleteGroupCleansUpDependentRows verifies that deleting a group
// removes every row that references the group, including memberships,
// MCP privilege grants, connection privilege grants, and admin
// permission grants. Regression test for the orphan-rows bug tracked
// in GitHub issue #51: previously the handler relied on ON DELETE
// CASCADE foreign keys that SQLite does not enforce, so users who
// belonged to a deleted group retained all privileges the group
// granted indefinitely.
func TestDeleteGroupCleansUpDependentRows(t *testing.T) {
	store, cleanup := createTestAuthStoreForGroups(t)
	defer cleanup()

	// Create a user that will be a member of the group under test.
	if err := store.CreateUser("alice", "Password1", "", "", ""); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	userID, err := store.GetUserID("alice")
	if err != nil {
		t.Fatalf("Failed to get user ID: %v", err)
	}

	// Create the group under test and a nested child group so we exercise
	// both sides of the group_memberships self-reference.
	groupID, err := store.CreateGroup("target-group", "Group to delete")
	if err != nil {
		t.Fatalf("Failed to create target group: %v", err)
	}
	childID, err := store.CreateGroup("child-group", "Nested child")
	if err != nil {
		t.Fatalf("Failed to create child group: %v", err)
	}

	// Create a second, unrelated group whose rows must NOT be touched by
	// the deletion. This guards against the DELETE statements missing a
	// WHERE clause.
	otherID, err := store.CreateGroup("other-group", "Unrelated group")
	if err != nil {
		t.Fatalf("Failed to create other group: %v", err)
	}

	// Memberships: add the user to the target, nest the child inside the
	// target, and add an unrelated membership on the other group.
	if err := store.AddUserToGroup(groupID, userID); err != nil {
		t.Fatalf("Failed to add user to target group: %v", err)
	}
	if err := store.AddGroupToGroup(groupID, childID); err != nil {
		t.Fatalf("Failed to add child group to target group: %v", err)
	}
	if err := store.AddUserToGroup(otherID, userID); err != nil {
		t.Fatalf("Failed to add user to other group: %v", err)
	}

	// Register an MCP privilege and grant it to both groups.
	privID, err := store.RegisterMCPPrivilege("tool.test", MCPPrivilegeTypeTool, "Test tool", false)
	if err != nil {
		t.Fatalf("Failed to register MCP privilege: %v", err)
	}
	if err := store.GrantMCPPrivilege(groupID, privID); err != nil {
		t.Fatalf("Failed to grant MCP privilege to target group: %v", err)
	}
	if err := store.GrantMCPPrivilege(otherID, privID); err != nil {
		t.Fatalf("Failed to grant MCP privilege to other group: %v", err)
	}

	// Grant a connection privilege and an admin permission to both groups.
	if err := store.GrantConnectionPrivilege(groupID, 42, "read_write"); err != nil {
		t.Fatalf("Failed to grant connection privilege to target group: %v", err)
	}
	if err := store.GrantConnectionPrivilege(otherID, 42, "read"); err != nil {
		t.Fatalf("Failed to grant connection privilege to other group: %v", err)
	}
	if err := store.GrantAdminPermission(groupID, "manage_users"); err != nil {
		t.Fatalf("Failed to grant admin permission to target group: %v", err)
	}
	if err := store.GrantAdminPermission(otherID, "manage_groups"); err != nil {
		t.Fatalf("Failed to grant admin permission to other group: %v", err)
	}

	// Delete the target group. All of its dependent rows must disappear.
	if err := store.DeleteGroup(groupID); err != nil {
		t.Fatalf("Failed to delete target group: %v", err)
	}

	// Helper that counts rows matching a predicate on a given table and
	// fails the test with a descriptive message if the count is non-zero.
	assertNoRows := func(label, query string, args ...any) {
		t.Helper()
		var count int
		if err := store.db.QueryRow(query, args...).Scan(&count); err != nil {
			t.Fatalf("Failed to query %s: %v", label, err)
		}
		if count != 0 {
			t.Errorf("Expected 0 orphan rows in %s for deleted group, got %d", label, count)
		}
	}

	// No membership rows may reference the deleted group in either slot.
	assertNoRows("group_memberships (parent)",
		"SELECT COUNT(*) FROM group_memberships WHERE parent_group_id = ?", groupID)
	assertNoRows("group_memberships (member)",
		"SELECT COUNT(*) FROM group_memberships WHERE member_group_id = ?", groupID)
	assertNoRows("group_mcp_privileges",
		"SELECT COUNT(*) FROM group_mcp_privileges WHERE group_id = ?", groupID)
	assertNoRows("connection_privileges",
		"SELECT COUNT(*) FROM connection_privileges WHERE group_id = ?", groupID)
	assertNoRows("group_admin_permissions",
		"SELECT COUNT(*) FROM group_admin_permissions WHERE group_id = ?", groupID)

	// Sanity check: the unrelated group's rows must survive unchanged.
	assertRowCount := func(label, query string, want int, args ...any) {
		t.Helper()
		var count int
		if err := store.db.QueryRow(query, args...).Scan(&count); err != nil {
			t.Fatalf("Failed to query %s: %v", label, err)
		}
		if count != want {
			t.Errorf("Expected %d rows in %s for unrelated group, got %d", want, label, count)
		}
	}
	assertRowCount("group_memberships (other)",
		"SELECT COUNT(*) FROM group_memberships WHERE parent_group_id = ?", 1, otherID)
	assertRowCount("group_mcp_privileges (other)",
		"SELECT COUNT(*) FROM group_mcp_privileges WHERE group_id = ?", 1, otherID)
	assertRowCount("connection_privileges (other)",
		"SELECT COUNT(*) FROM connection_privileges WHERE group_id = ?", 1, otherID)
	assertRowCount("group_admin_permissions (other)",
		"SELECT COUNT(*) FROM group_admin_permissions WHERE group_id = ?", 1, otherID)

	// End-to-end check: the user must no longer report membership of the
	// deleted group. Before the fix, the membership row survived and
	// GetUserGroups would still list the deleted group id.
	groupIDs, err := store.GetUserGroups(userID)
	if err != nil {
		t.Fatalf("Failed to get user groups: %v", err)
	}
	for _, gid := range groupIDs {
		if gid == groupID {
			t.Errorf("User still reports membership of deleted group %d", groupID)
		}
	}

	// The child group itself is NOT deleted (we only deleted the parent),
	// so it must still exist as a standalone group. This guards against
	// an overzealous fix that accidentally deletes nested groups.
	child, err := store.GetGroup(childID)
	if err != nil {
		t.Fatalf("Failed to get child group: %v", err)
	}
	if child == nil {
		t.Error("Nested child group was unexpectedly deleted along with its parent")
	}
}

func TestListGroups(t *testing.T) {
	store, cleanup := createTestAuthStoreForGroups(t)
	defer cleanup()

	// Create multiple groups
	_, err := store.CreateGroup("group-a", "Group A")
	if err != nil {
		t.Fatalf("Failed to create group A: %v", err)
	}

	_, err = store.CreateGroup("group-b", "Group B")
	if err != nil {
		t.Fatalf("Failed to create group B: %v", err)
	}

	_, err = store.CreateGroup("group-c", "Group C")
	if err != nil {
		t.Fatalf("Failed to create group C: %v", err)
	}

	// List groups
	groups, err := store.ListGroups()
	if err != nil {
		t.Fatalf("Failed to list groups: %v", err)
	}

	if len(groups) != 3 {
		t.Errorf("Expected 3 groups, got %d", len(groups))
	}

	// Verify alphabetical order
	if groups[0].Name != "group-a" {
		t.Errorf("Expected first group 'group-a', got %q", groups[0].Name)
	}
}

func TestAddUserToGroup(t *testing.T) {
	store, cleanup := createTestAuthStoreForGroups(t)
	defer cleanup()

	// Create a user
	err := store.CreateUser("testuser", "Password1", "Test user", "", "")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	userID, err := store.GetUserID("testuser")
	if err != nil {
		t.Fatalf("Failed to get user ID: %v", err)
	}

	// Create a group
	groupID, err := store.CreateGroup("test-group", "A test group")
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}

	// Add user to group
	err = store.AddUserToGroup(groupID, userID)
	if err != nil {
		t.Fatalf("Failed to add user to group: %v", err)
	}

	// Verify membership
	isMember, err := store.IsUserInGroup(userID, groupID)
	if err != nil {
		t.Fatalf("Failed to check membership: %v", err)
	}

	if !isMember {
		t.Error("Expected user to be member of group")
	}
}

func TestRemoveUserFromGroup(t *testing.T) {
	store, cleanup := createTestAuthStoreForGroups(t)
	defer cleanup()

	// Create user and group
	err := store.CreateUser("testuser", "Password1", "Test user", "", "")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	userID, _ := store.GetUserID("testuser")
	groupID, _ := store.CreateGroup("test-group", "A test group")

	// Add and then remove user
	err = store.AddUserToGroup(groupID, userID)
	if err != nil {
		t.Fatalf("Failed to add user: %v", err)
	}

	err = store.RemoveUserFromGroup(groupID, userID)
	if err != nil {
		t.Fatalf("Failed to remove user: %v", err)
	}

	// Verify removal
	isMember, _ := store.IsUserInGroup(userID, groupID)
	if isMember {
		t.Error("Expected user to not be member of group after removal")
	}
}

func TestGroupHierarchy(t *testing.T) {
	store, cleanup := createTestAuthStoreForGroups(t)
	defer cleanup()

	// Create hierarchy: parent-group -> child-group -> user
	parentID, _ := store.CreateGroup("parent-group", "Parent")
	childID, _ := store.CreateGroup("child-group", "Child")

	err := store.CreateUser("testuser", "Password1", "Test user", "", "")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	userID, _ := store.GetUserID("testuser")

	// Add child to parent
	err = store.AddGroupToGroup(parentID, childID)
	if err != nil {
		t.Fatalf("Failed to add child group to parent: %v", err)
	}

	// Add user to child
	err = store.AddUserToGroup(childID, userID)
	if err != nil {
		t.Fatalf("Failed to add user to child group: %v", err)
	}

	// Get user's groups (should include both child and parent)
	groups, err := store.GetUserGroups(userID)
	if err != nil {
		t.Fatalf("Failed to get user groups: %v", err)
	}

	if len(groups) != 2 {
		t.Errorf("Expected user to be in 2 groups, got %d", len(groups))
	}

	// Verify user is member of both
	inParent, _ := store.IsUserInGroup(userID, parentID)
	inChild, _ := store.IsUserInGroup(userID, childID)

	if !inParent {
		t.Error("Expected user to be member of parent group through inheritance")
	}

	if !inChild {
		t.Error("Expected user to be member of child group")
	}
}

func TestCircularReferenceDetection(t *testing.T) {
	store, cleanup := createTestAuthStoreForGroups(t)
	defer cleanup()

	// Create groups
	groupA, _ := store.CreateGroup("group-a", "Group A")
	groupB, _ := store.CreateGroup("group-b", "Group B")
	groupC, _ := store.CreateGroup("group-c", "Group C")

	// Create chain: A -> B -> C
	err := store.AddGroupToGroup(groupA, groupB)
	if err != nil {
		t.Fatalf("Failed to add B to A: %v", err)
	}

	err = store.AddGroupToGroup(groupB, groupC)
	if err != nil {
		t.Fatalf("Failed to add C to B: %v", err)
	}

	// Try to create cycle: C -> A (should fail)
	err = store.AddGroupToGroup(groupC, groupA)
	if err == nil {
		t.Error("Expected error when creating circular reference")
	}
}

func TestSelfReferenceDetection(t *testing.T) {
	store, cleanup := createTestAuthStoreForGroups(t)
	defer cleanup()

	groupID, _ := store.CreateGroup("test-group", "Test")

	// Try to add group to itself
	err := store.AddGroupToGroup(groupID, groupID)
	if err == nil {
		t.Error("Expected error when adding group to itself")
	}
}

func TestGetGroupMembers(t *testing.T) {
	store, cleanup := createTestAuthStoreForGroups(t)
	defer cleanup()

	// Create a group with user and nested group members
	parentID, _ := store.CreateGroup("parent", "Parent group")
	childID, _ := store.CreateGroup("child", "Child group")

	err := store.CreateUser("user1", "Password1", "User 1", "", "")
	if err != nil {
		t.Fatalf("Failed to create user1: %v", err)
	}
	err = store.CreateUser("user2", "Password1", "User 2", "", "")
	if err != nil {
		t.Fatalf("Failed to create user2: %v", err)
	}

	user1ID, _ := store.GetUserID("user1")
	user2ID, _ := store.GetUserID("user2")

	// Add members
	store.AddUserToGroup(parentID, user1ID)
	store.AddUserToGroup(parentID, user2ID)
	store.AddGroupToGroup(parentID, childID)

	// Get members
	result, err := store.GetGroupMembers(parentID)
	if err != nil {
		t.Fatalf("Failed to get group members: %v", err)
	}

	if len(result.UserMembers) != 2 {
		t.Errorf("Expected 2 user members, got %d", len(result.UserMembers))
	}

	if len(result.GroupMembers) != 1 {
		t.Errorf("Expected 1 group member, got %d", len(result.GroupMembers))
	}
}

func TestSetUserSuperuser(t *testing.T) {
	store, cleanup := createTestAuthStoreForGroups(t)
	defer cleanup()

	// Create a user
	err := store.CreateUser("testuser", "Password1", "Test user", "", "")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Initially not superuser
	user, _ := store.GetUser("testuser")
	if user.IsSuperuser {
		t.Error("Expected user to not be superuser initially")
	}

	// Set as superuser
	err = store.SetUserSuperuser("testuser", true)
	if err != nil {
		t.Fatalf("Failed to set superuser: %v", err)
	}

	// Verify
	user, _ = store.GetUser("testuser")
	if !user.IsSuperuser {
		t.Error("Expected user to be superuser after update")
	}

	// Remove superuser
	err = store.SetUserSuperuser("testuser", false)
	if err != nil {
		t.Fatalf("Failed to remove superuser: %v", err)
	}

	// Verify
	user, _ = store.GetUser("testuser")
	if user.IsSuperuser {
		t.Error("Expected user to not be superuser after removal")
	}
}

func TestGetGroupByName(t *testing.T) {
	store, cleanup := createTestAuthStoreForGroups(t)
	defer cleanup()

	// Create a group
	groupID, _ := store.CreateGroup("test-group", "A test group")

	// Get by name
	group, err := store.GetGroupByName("test-group")
	if err != nil {
		t.Fatalf("Failed to get group by name: %v", err)
	}

	if group == nil {
		t.Fatal("Expected group to exist")
	}

	if group.ID != groupID {
		t.Errorf("Expected ID %d, got %d", groupID, group.ID)
	}

	// Get non-existent group
	group, err = store.GetGroupByName("nonexistent")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if group != nil {
		t.Error("Expected nil for non-existent group")
	}
}

func TestGroupCount(t *testing.T) {
	store, cleanup := createTestAuthStoreForGroups(t)
	defer cleanup()

	if count := store.GroupCount(); count != 0 {
		t.Errorf("Expected 0 groups initially, got %d", count)
	}

	store.CreateGroup("group1", "")
	store.CreateGroup("group2", "")

	if count := store.GroupCount(); count != 2 {
		t.Errorf("Expected 2 groups, got %d", count)
	}
}
