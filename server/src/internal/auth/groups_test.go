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
	err := store.CreateUser("testuser", "password", "Test user", "", "")
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
	err := store.CreateUser("testuser", "password", "Test user", "", "")
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

	err := store.CreateUser("testuser", "password", "Test user", "", "")
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

	err := store.CreateUser("user1", "password", "User 1", "", "")
	if err != nil {
		t.Fatalf("Failed to create user1: %v", err)
	}
	err = store.CreateUser("user2", "password", "User 2", "", "")
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
	err := store.CreateUser("testuser", "password", "Test user", "", "")
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
