/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
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

// MCPPrivilege represents an MCP privilege identifier
type MCPPrivilege struct {
    Identifier  string
    ItemType    string
    Description string
}

// GetDefaultMCPPrivileges returns the list of all MCP tools that should be
// registered in the privilege system
func GetDefaultMCPPrivileges() []MCPPrivilege {
    return []MCPPrivilege{
        // User Management Tools
        {
            Identifier:  "authenticate_user",
            ItemType:    "tool",
            Description: "Authenticate a user and obtain a session token",
        },
        {
            Identifier:  "create_user",
            ItemType:    "tool",
            Description: "Create a new user account",
        },
        {
            Identifier:  "update_user",
            ItemType:    "tool",
            Description: "Update an existing user account",
        },
        {
            Identifier:  "delete_user",
            ItemType:    "tool",
            Description: "Delete a user account",
        },

        // Service Token Management Tools
        {
            Identifier:  "create_service_token",
            ItemType:    "tool",
            Description: "Create a service token for programmatic access",
        },
        {
            Identifier:  "update_service_token",
            ItemType:    "tool",
            Description: "Update a service token",
        },
        {
            Identifier:  "delete_service_token",
            ItemType:    "tool",
            Description: "Delete a service token",
        },

        // User Token Management Tools
        {
            Identifier:  "create_user_token",
            ItemType:    "tool",
            Description: "Create a user token for personal access",
        },
        {
            Identifier:  "list_user_tokens",
            ItemType:    "tool",
            Description: "List all tokens for a user",
        },
        {
            Identifier:  "delete_user_token",
            ItemType:    "tool",
            Description: "Delete a user token",
        },

        // Group Management Tools
        {
            Identifier:  "create_user_group",
            ItemType:    "tool",
            Description: "Create a new user group",
        },
        {
            Identifier:  "update_user_group",
            ItemType:    "tool",
            Description: "Update an existing user group",
        },
        {
            Identifier:  "delete_user_group",
            ItemType:    "tool",
            Description: "Delete a user group",
        },
        {
            Identifier:  "list_user_groups",
            ItemType:    "tool",
            Description: "List all user groups",
        },
        {
            Identifier:  "add_group_member",
            ItemType:    "tool",
            Description: "Add a user or group as a member of a group",
        },
        {
            Identifier:  "remove_group_member",
            ItemType:    "tool",
            Description: "Remove a member from a group",
        },
        {
            Identifier:  "list_group_members",
            ItemType:    "tool",
            Description: "List all members of a group",
        },
        {
            Identifier:  "list_user_group_memberships",
            ItemType:    "tool",
            Description: "List all groups a user belongs to",
        },

        // Connection Privilege Management Tools
        {
            Identifier:  "grant_connection_privilege",
            ItemType:    "tool",
            Description: "Grant a group access to a connection",
        },
        {
            Identifier:  "revoke_connection_privilege",
            ItemType:    "tool",
            Description: "Revoke a group's access to a connection",
        },
        {
            Identifier:  "list_connection_privileges",
            ItemType:    "tool",
            Description: "List all group privileges for a connection",
        },

        // MCP Privilege Management Tools
        {
            Identifier:  "list_mcp_privilege_identifiers",
            ItemType:    "tool",
            Description: "List all registered MCP privilege identifiers",
        },
        {
            Identifier:  "grant_mcp_privilege",
            ItemType:    "tool",
            Description: "Grant a group access to an MCP tool, resource, or prompt",
        },
        {
            Identifier:  "revoke_mcp_privilege",
            ItemType:    "tool",
            Description: "Revoke a group's access to an MCP item",
        },
        {
            Identifier:  "list_group_mcp_privileges",
            ItemType:    "tool",
            Description: "List all MCP privileges granted to a group",
        },

        // Token Scope Management Tools
        {
            Identifier:  "set_token_connection_scope",
            ItemType:    "tool",
            Description: "Limit a token's access to specific connections",
        },
        {
            Identifier:  "set_token_mcp_scope",
            ItemType:    "tool",
            Description: "Limit a token's access to specific MCP items",
        },
        {
            Identifier:  "get_token_scope",
            ItemType:    "tool",
            Description: "Get the scope restrictions for a token",
        },
        {
            Identifier:  "clear_token_scope",
            ItemType:    "tool",
            Description: "Remove all scope restrictions for a token",
        },

        // Connection Management Tools
        {
            Identifier:  "create_connection",
            ItemType:    "tool",
            Description: "Create a new database connection",
        },
        {
            Identifier:  "update_connection",
            ItemType:    "tool",
            Description: "Update an existing database connection",
        },
        {
            Identifier:  "delete_connection",
            ItemType:    "tool",
            Description: "Delete a database connection",
        },
        {
            Identifier:  "execute_query",
            ItemType:    "tool",
            Description: "Execute SQL queries on database connections in read-only mode",
        },

        // Session Context Management Tools
        {
            Identifier:  "set_database_context",
            ItemType:    "tool",
            Description: "Set the current database context for the session",
        },
        {
            Identifier:  "get_database_context",
            ItemType:    "tool",
            Description: "Get the current database context for the session",
        },
        {
            Identifier:  "clear_database_context",
            ItemType:    "tool",
            Description: "Clear the current database context for the session",
        },

        // Datastore Query Tool
        {
            Identifier:  "query_datastore",
            ItemType:    "tool",
            Description: "Query the datastore containing historical metrics and performance data",
        },
    }
}

// SeedMCPPrivileges seeds the mcp_privilege_identifiers table with all
// registered MCP tools. This is idempotent and can be called multiple times.
func SeedMCPPrivileges(ctx context.Context, pool *pgxpool.Pool) error {
    privileges := GetDefaultMCPPrivileges()

    for _, priv := range privileges {
        // Use ON CONFLICT to make this idempotent
        _, err := pool.Exec(ctx, `
            INSERT INTO mcp_privilege_identifiers (identifier, item_type, description)
            VALUES ($1, $2, $3)
            ON CONFLICT (identifier) DO UPDATE
            SET item_type = EXCLUDED.item_type,
                description = EXCLUDED.description
        `, priv.Identifier, priv.ItemType, priv.Description)

        if err != nil {
            return fmt.Errorf("failed to seed privilege identifier '%s': %w", priv.Identifier, err)
        }
    }

    return nil
}
