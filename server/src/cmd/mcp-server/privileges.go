/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// grantMCPPrivilegeCommand grants an MCP privilege to a group
func grantMCPPrivilegeCommand(dataDir, groupName, identifier string) error {
	if groupName == "" {
		return fmt.Errorf("group name is required")
	}
	if identifier == "" {
		return fmt.Errorf("privilege identifier is required")
	}

	// Open auth store
	store, err := auth.NewAuthStore(dataDir, 0, 0)
	if err != nil {
		return fmt.Errorf("failed to open auth store: %w", err)
	}
	defer store.Close()

	// Get group
	group, err := store.GetGroupByName(groupName)
	if err != nil {
		return fmt.Errorf("failed to find group: %w", err)
	}
	if group == nil {
		return fmt.Errorf("group '%s' not found", groupName)
	}

	// Get privilege
	priv, err := store.GetMCPPrivilege(identifier)
	if err != nil {
		return fmt.Errorf("failed to find privilege: %w", err)
	}
	if priv == nil {
		return fmt.Errorf("privilege '%s' not found (register it first)", identifier)
	}

	// Grant privilege
	if err := store.GrantMCPPrivilege(group.ID, priv.ID); err != nil {
		return fmt.Errorf("failed to grant privilege: %w", err)
	}

	fmt.Printf("Granted privilege '%s' to group '%s'\n", identifier, groupName)
	return nil
}

// revokeMCPPrivilegeCommand revokes an MCP privilege from a group
func revokeMCPPrivilegeCommand(dataDir, groupName, identifier string) error {
	if groupName == "" {
		return fmt.Errorf("group name is required")
	}
	if identifier == "" {
		return fmt.Errorf("privilege identifier is required")
	}

	// Open auth store
	store, err := auth.NewAuthStore(dataDir, 0, 0)
	if err != nil {
		return fmt.Errorf("failed to open auth store: %w", err)
	}
	defer store.Close()

	// Get group
	group, err := store.GetGroupByName(groupName)
	if err != nil {
		return fmt.Errorf("failed to find group: %w", err)
	}
	if group == nil {
		return fmt.Errorf("group '%s' not found", groupName)
	}

	// Get privilege
	priv, err := store.GetMCPPrivilege(identifier)
	if err != nil {
		return fmt.Errorf("failed to find privilege: %w", err)
	}
	if priv == nil {
		return fmt.Errorf("privilege '%s' not found", identifier)
	}

	// Revoke privilege
	if err := store.RevokeMCPPrivilege(group.ID, priv.ID); err != nil {
		return fmt.Errorf("failed to revoke privilege: %w", err)
	}

	fmt.Printf("Revoked privilege '%s' from group '%s'\n", identifier, groupName)
	return nil
}

// grantConnectionPrivilegeCommand grants a connection privilege to a group
func grantConnectionPrivilegeCommand(dataDir, groupName string, connectionID int, accessLevel string) error {
	if groupName == "" {
		return fmt.Errorf("group name is required")
	}
	if connectionID <= 0 {
		return fmt.Errorf("valid connection ID is required")
	}
	if accessLevel == "" {
		accessLevel = "read"
	}
	if accessLevel != "read" && accessLevel != "read_write" {
		return fmt.Errorf("access level must be 'read' or 'read_write'")
	}

	// Open auth store
	store, err := auth.NewAuthStore(dataDir, 0, 0)
	if err != nil {
		return fmt.Errorf("failed to open auth store: %w", err)
	}
	defer store.Close()

	// Get group
	group, err := store.GetGroupByName(groupName)
	if err != nil {
		return fmt.Errorf("failed to find group: %w", err)
	}
	if group == nil {
		return fmt.Errorf("group '%s' not found", groupName)
	}

	// Grant connection privilege
	if err := store.GrantConnectionPrivilege(group.ID, connectionID, accessLevel); err != nil {
		return fmt.Errorf("failed to grant connection privilege: %w", err)
	}

	fmt.Printf("Granted %s access to connection %d for group '%s'\n", accessLevel, connectionID, groupName)
	return nil
}

// revokeConnectionPrivilegeCommand revokes a connection privilege from a group
func revokeConnectionPrivilegeCommand(dataDir, groupName string, connectionID int) error {
	if groupName == "" {
		return fmt.Errorf("group name is required")
	}
	if connectionID <= 0 {
		return fmt.Errorf("valid connection ID is required")
	}

	// Open auth store
	store, err := auth.NewAuthStore(dataDir, 0, 0)
	if err != nil {
		return fmt.Errorf("failed to open auth store: %w", err)
	}
	defer store.Close()

	// Get group
	group, err := store.GetGroupByName(groupName)
	if err != nil {
		return fmt.Errorf("failed to find group: %w", err)
	}
	if group == nil {
		return fmt.Errorf("group '%s' not found", groupName)
	}

	// Revoke connection privilege
	if err := store.RevokeConnectionPrivilege(group.ID, connectionID); err != nil {
		return fmt.Errorf("failed to revoke connection privilege: %w", err)
	}

	fmt.Printf("Revoked access to connection %d from group '%s'\n", connectionID, groupName)
	return nil
}

// listPrivilegesCommand lists all registered MCP privileges
func listPrivilegesCommand(dataDir string) error {
	// Open auth store
	store, err := auth.NewAuthStore(dataDir, 0, 0)
	if err != nil {
		return fmt.Errorf("failed to open auth store: %w", err)
	}
	defer store.Close()

	privs, err := store.ListMCPPrivileges()
	if err != nil {
		return fmt.Errorf("failed to list privileges: %w", err)
	}

	if len(privs) == 0 {
		fmt.Println("No MCP privileges registered.")
		return nil
	}

	fmt.Println("\nMCP Privileges:")
	fmt.Println(strings.Repeat("=", 90))
	fmt.Printf("%-6s %-10s %-30s %s\n", "ID", "Type", "Identifier", "Description")
	fmt.Println(strings.Repeat("-", 90))

	for _, priv := range privs {
		description := priv.Description
		if len(description) > 35 {
			description = description[:32] + "..."
		}

		fmt.Printf("%-6d %-10s %-30s %s\n",
			priv.ID,
			priv.ItemType,
			priv.Identifier,
			description)
	}
	fmt.Println(strings.Repeat("=", 90) + "\n")

	return nil
}

// showGroupPrivilegesCommand shows all privileges for a group
func showGroupPrivilegesCommand(dataDir, groupName string) error {
	if groupName == "" {
		return fmt.Errorf("group name is required")
	}

	// Open auth store
	store, err := auth.NewAuthStore(dataDir, 0, 0)
	if err != nil {
		return fmt.Errorf("failed to open auth store: %w", err)
	}
	defer store.Close()

	// Get group
	group, err := store.GetGroupByName(groupName)
	if err != nil {
		return fmt.Errorf("failed to find group: %w", err)
	}
	if group == nil {
		return fmt.Errorf("group '%s' not found", groupName)
	}

	// Get privileges
	result, err := store.GetGroupWithPrivileges(group.ID)
	if err != nil {
		return fmt.Errorf("failed to get group privileges: %w", err)
	}

	fmt.Printf("\nPrivileges for group '%s':\n", groupName)
	fmt.Println(strings.Repeat("=", 70))

	if len(result.MCPPrivileges) == 0 {
		fmt.Println("MCP Privileges: None")
	} else {
		fmt.Println("\nMCP Privileges:")
		for _, priv := range result.MCPPrivileges {
			fmt.Printf("  - [%s] %s\n", priv.ItemType, priv.Identifier)
		}
	}

	if len(result.ConnectionPrivileges) == 0 {
		fmt.Println("\nConnection Privileges: None")
	} else {
		fmt.Println("\nConnection Privileges:")
		for _, conn := range result.ConnectionPrivileges {
			fmt.Printf("  - Connection %d: %s\n", conn.ConnectionID, conn.AccessLevel)
		}
	}

	fmt.Println(strings.Repeat("=", 70) + "\n")

	return nil
}

// registerPrivilegeCommand registers a new MCP privilege identifier
func registerPrivilegeCommand(dataDir, identifier, itemType, description string) error {
	if identifier == "" {
		return fmt.Errorf("privilege identifier is required")
	}
	if itemType == "" {
		return fmt.Errorf("item type is required (tool, resource, or prompt)")
	}

	// Open auth store
	store, err := auth.NewAuthStore(dataDir, 0, 0)
	if err != nil {
		return fmt.Errorf("failed to open auth store: %w", err)
	}
	defer store.Close()

	// Register privilege
	id, err := store.RegisterMCPPrivilege(identifier, itemType, description)
	if err != nil {
		return fmt.Errorf("failed to register privilege: %w", err)
	}

	fmt.Printf("Privilege '%s' registered (ID: %d)\n", identifier, id)
	return nil
}

// parseConnectionIDs parses a comma-separated list of connection IDs
func parseConnectionIDs(s string) ([]int, error) {
	if s == "" {
		return nil, nil
	}

	parts := strings.Split(s, ",")
	ids := make([]int, 0, len(parts))

	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("invalid connection ID '%s': %w", p, err)
		}
		ids = append(ids, id)
	}

	return ids, nil
}

// registerMCPPrivileges registers all MCP privilege identifiers for RBAC
// This is called on server startup to ensure all tools and resources are registered
func registerMCPPrivileges(store *auth.AuthStore) {
	// Define all MCP privileges to register
	privileges := []struct {
		identifier  string
		itemType    string
		description string
	}{
		// Database Query Tools
		{"query_database", "tool", "Execute read-only SQL queries against the database"},
		{"get_schema_info", "tool", "Retrieve database schema information (tables, columns, indexes)"},
		{"execute_explain", "tool", "Execute EXPLAIN on queries to analyze execution plans"},
		{"count_rows", "tool", "Count rows in database tables"},

		// Metrics and Monitoring Tools
		{"list_probes", "tool", "List available metric collection probes"},
		{"describe_probe", "tool", "Get detailed information about a specific probe"},
		{"query_metrics", "tool", "Query historical metrics data from the datastore"},

		// Connection Management Tools
		{"list_connections", "tool", "List available database connections"},

		// Knowledge Base and Search Tools
		{"generate_embedding", "tool", "Generate vector embeddings for text"},
		{"search_knowledgebase", "tool", "Search the knowledge base for relevant information"},
		{"similarity_search", "tool", "Perform vector similarity search on database tables"},
		{"read_resource", "tool", "Read MCP resource content"},

		// Authentication Tool (typically unrestricted but registered for completeness)
		{"authenticate_user", "tool", "Authenticate a user with username and password"},

		// RBAC Administration Tools (require superuser)
		{"create_group", "tool", "Create a new RBAC group"},
		{"update_group", "tool", "Update an existing RBAC group"},
		{"delete_group", "tool", "Delete an RBAC group"},
		{"list_groups", "tool", "List all RBAC groups"},
		{"add_group_member", "tool", "Add a user or group to a group"},
		{"remove_group_member", "tool", "Remove a user or group from a group"},
		{"grant_mcp_privilege", "tool", "Grant an MCP privilege to a group"},
		{"revoke_mcp_privilege", "tool", "Revoke an MCP privilege from a group"},
		{"grant_connection_privilege", "tool", "Grant connection access to a group"},
		{"revoke_connection_privilege", "tool", "Revoke connection access from a group"},
		{"list_privileges", "tool", "List all registered MCP privileges"},
		{"set_token_scope", "tool", "Set scope restrictions for a token"},
		{"get_token_scope", "tool", "Get current scope restrictions for a token"},
		{"clear_token_scope", "tool", "Clear all scope restrictions from a token"},
		{"set_superuser", "tool", "Set or remove superuser status for a user"},
		{"list_users", "tool", "List all users with their group memberships"},
		{"get_user_privileges", "tool", "View a user's effective privileges"},

		// Resources
		{"pg://system_info", "resource", "PostgreSQL system information resource"},
		{"pg://connection_info", "resource", "Current database connection information resource"},
	}

	// Register each privilege (RegisterMCPPrivilege handles duplicates gracefully)
	registered := 0
	for _, p := range privileges {
		_, err := store.RegisterMCPPrivilege(p.identifier, p.itemType, p.description)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to register privilege '%s': %v\n", p.identifier, err)
		} else {
			registered++
		}
	}

	fmt.Fprintf(os.Stderr, "RBAC: %d MCP privileges registered\n", store.MCPPrivilegeCount())
}
