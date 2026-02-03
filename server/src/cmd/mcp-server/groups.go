/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package main

import (
	"fmt"
	"strings"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// addGroupCommand handles the add-group command
func addGroupCommand(dataDir, name, description string) error {
	if name == "" {
		return fmt.Errorf("group name is required")
	}

	// Open auth store
	store, err := auth.NewAuthStore(dataDir, 0, 0)
	if err != nil {
		return fmt.Errorf("failed to open auth store: %w", err)
	}
	defer store.Close()

	// Create group
	groupID, err := store.CreateGroup(name, description)
	if err != nil {
		return fmt.Errorf("failed to create group: %w", err)
	}

	fmt.Printf("Group '%s' created successfully (ID: %d)\n", name, groupID)
	return nil
}

// deleteGroupCommand handles the delete-group command
func deleteGroupCommand(dataDir, name string) error {
	if name == "" {
		return fmt.Errorf("group name is required")
	}

	// Open auth store
	store, err := auth.NewAuthStore(dataDir, 0, 0)
	if err != nil {
		return fmt.Errorf("failed to open auth store: %w", err)
	}
	defer store.Close()

	// Get group by name
	group, err := store.GetGroupByName(name)
	if err != nil {
		return fmt.Errorf("failed to find group: %w", err)
	}
	if group == nil {
		return fmt.Errorf("group '%s' not found", name)
	}

	// Delete group
	if err := store.DeleteGroup(group.ID); err != nil {
		return fmt.Errorf("failed to delete group: %w", err)
	}

	fmt.Printf("Group '%s' deleted successfully\n", name)
	return nil
}

// listGroupsCommand handles the list-groups command
func listGroupsCommand(dataDir string) error {
	// Open auth store
	store, err := auth.NewAuthStore(dataDir, 0, 0)
	if err != nil {
		return fmt.Errorf("failed to open auth store: %w", err)
	}
	defer store.Close()

	groups, err := store.ListGroups()
	if err != nil {
		return fmt.Errorf("failed to list groups: %w", err)
	}

	if len(groups) == 0 {
		fmt.Println("No groups found.")
		return nil
	}

	fmt.Println("\nGroups:")
	fmt.Println(strings.Repeat("=", 80))
	fmt.Printf("%-6s %-20s %-20s %s\n", "ID", "Name", "Created", "Description")
	fmt.Println(strings.Repeat("-", 80))

	for _, group := range groups {
		description := group.Description
		if len(description) > 30 {
			description = description[:27] + "..."
		}

		fmt.Printf("%-6d %-20s %-20s %s\n",
			group.ID,
			group.Name,
			group.CreatedAt.Format("2006-01-02 15:04"),
			description)
	}
	fmt.Println(strings.Repeat("=", 80) + "\n")

	return nil
}

// addMemberCommand handles the add-member command
func addMemberCommand(dataDir, groupName, memberUsername, memberGroupName string) error {
	if groupName == "" {
		return fmt.Errorf("group name is required")
	}
	if memberUsername == "" && memberGroupName == "" {
		return fmt.Errorf("either member username or member group name is required")
	}
	if memberUsername != "" && memberGroupName != "" {
		return fmt.Errorf("specify either member username or member group name, not both")
	}

	// Open auth store
	store, err := auth.NewAuthStore(dataDir, 0, 0)
	if err != nil {
		return fmt.Errorf("failed to open auth store: %w", err)
	}
	defer store.Close()

	// Get parent group
	parentGroup, err := store.GetGroupByName(groupName)
	if err != nil {
		return fmt.Errorf("failed to find parent group: %w", err)
	}
	if parentGroup == nil {
		return fmt.Errorf("group '%s' not found", groupName)
	}

	if memberUsername != "" {
		// Add user to group
		user, err := store.GetUser(memberUsername)
		if err != nil {
			return fmt.Errorf("failed to find user: %w", err)
		}
		if user == nil {
			return fmt.Errorf("user '%s' not found", memberUsername)
		}

		if err := store.AddUserToGroup(parentGroup.ID, user.ID); err != nil {
			return fmt.Errorf("failed to add user to group: %w", err)
		}

		fmt.Printf("User '%s' added to group '%s'\n", memberUsername, groupName)
	} else {
		// Add group to group (nested)
		childGroup, err := store.GetGroupByName(memberGroupName)
		if err != nil {
			return fmt.Errorf("failed to find member group: %w", err)
		}
		if childGroup == nil {
			return fmt.Errorf("member group '%s' not found", memberGroupName)
		}

		if err := store.AddGroupToGroup(parentGroup.ID, childGroup.ID); err != nil {
			return fmt.Errorf("failed to add group to group: %w", err)
		}

		fmt.Printf("Group '%s' added to group '%s'\n", memberGroupName, groupName)
	}

	return nil
}

// removeMemberCommand handles the remove-member command
func removeMemberCommand(dataDir, groupName, memberUsername, memberGroupName string) error {
	if groupName == "" {
		return fmt.Errorf("group name is required")
	}
	if memberUsername == "" && memberGroupName == "" {
		return fmt.Errorf("either member username or member group name is required")
	}
	if memberUsername != "" && memberGroupName != "" {
		return fmt.Errorf("specify either member username or member group name, not both")
	}

	// Open auth store
	store, err := auth.NewAuthStore(dataDir, 0, 0)
	if err != nil {
		return fmt.Errorf("failed to open auth store: %w", err)
	}
	defer store.Close()

	// Get parent group
	parentGroup, err := store.GetGroupByName(groupName)
	if err != nil {
		return fmt.Errorf("failed to find parent group: %w", err)
	}
	if parentGroup == nil {
		return fmt.Errorf("group '%s' not found", groupName)
	}

	if memberUsername != "" {
		// Remove user from group
		user, err := store.GetUser(memberUsername)
		if err != nil {
			return fmt.Errorf("failed to find user: %w", err)
		}
		if user == nil {
			return fmt.Errorf("user '%s' not found", memberUsername)
		}

		if err := store.RemoveUserFromGroup(parentGroup.ID, user.ID); err != nil {
			return fmt.Errorf("failed to remove user from group: %w", err)
		}

		fmt.Printf("User '%s' removed from group '%s'\n", memberUsername, groupName)
	} else {
		// Remove group from group
		childGroup, err := store.GetGroupByName(memberGroupName)
		if err != nil {
			return fmt.Errorf("failed to find member group: %w", err)
		}
		if childGroup == nil {
			return fmt.Errorf("member group '%s' not found", memberGroupName)
		}

		if err := store.RemoveGroupFromGroup(parentGroup.ID, childGroup.ID); err != nil {
			return fmt.Errorf("failed to remove group from group: %w", err)
		}

		fmt.Printf("Group '%s' removed from group '%s'\n", memberGroupName, groupName)
	}

	return nil
}

// setSuperuserCommand handles the set-superuser command for users
func setSuperuserCommand(dataDir, username string, isSuperuser bool) error {
	if username == "" {
		return fmt.Errorf("username is required")
	}

	// Open auth store
	store, err := auth.NewAuthStore(dataDir, 0, 0)
	if err != nil {
		return fmt.Errorf("failed to open auth store: %w", err)
	}
	defer store.Close()

	// Check if user exists
	user, err := store.GetUser(username)
	if err != nil {
		return fmt.Errorf("failed to find user: %w", err)
	}
	if user == nil {
		return fmt.Errorf("user '%s' not found", username)
	}

	// Set superuser status
	if err := store.SetUserSuperuser(username, isSuperuser); err != nil {
		return fmt.Errorf("failed to set superuser status: %w", err)
	}

	if isSuperuser {
		fmt.Printf("User '%s' is now a superuser\n", username)
	} else {
		fmt.Printf("Superuser status removed from user '%s'\n", username)
	}

	return nil
}
