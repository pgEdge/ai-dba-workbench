/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package rbac

import (
    "fmt"
    "strings"

    "github.com/pgedge/ai-workbench/server/internal/auth"
    "github.com/pgedge/ai-workbench/server/internal/mcp"
    "github.com/pgedge/ai-workbench/server/internal/tools"
)

// GrantMCPPrivilegeTool creates a tool for granting MCP privileges to groups
func GrantMCPPrivilegeTool(authStore *auth.AuthStore) tools.Tool {
    return tools.Tool{
        Definition: mcp.Tool{
            Name:        "grant_mcp_privilege",
            Description: "Grant an MCP privilege (tool, resource, or prompt) to a group. Requires superuser privileges.",
            InputSchema: mcp.InputSchema{
                Type: "object",
                Properties: map[string]interface{}{
                    "group_id": map[string]interface{}{
                        "type":        "integer",
                        "description": "ID of the group to grant the privilege to",
                    },
                    "privilege": map[string]interface{}{
                        "type":        "string",
                        "description": "Privilege identifier (e.g., 'query_database', 'pg://system_info')",
                    },
                },
                Required: []string{"group_id", "privilege"},
            },
        },
        Handler: func(args map[string]interface{}) (mcp.ToolResponse, error) {
            ctx := getContextFromArgs(args)
            if !auth.IsSuperuserFromContext(ctx) {
                return mcp.NewToolError("Access denied: superuser privileges required")
            }

            groupID, err := parseIntArg(args, "group_id")
            if err != nil {
                return mcp.NewToolError("Missing or invalid 'group_id' parameter")
            }

            privilege, ok := args["privilege"].(string)
            if !ok || strings.TrimSpace(privilege) == "" {
                return mcp.NewToolError("Missing or empty 'privilege' parameter")
            }

            if err := authStore.GrantMCPPrivilegeByName(int64(groupID), privilege); err != nil {
                return mcp.NewToolError(fmt.Sprintf("Failed to grant privilege: %v", err))
            }

            return mcp.NewToolSuccess(fmt.Sprintf("Privilege '%s' granted to group %d", privilege, groupID))
        },
    }
}

// RevokeMCPPrivilegeTool creates a tool for revoking MCP privileges from groups
func RevokeMCPPrivilegeTool(authStore *auth.AuthStore) tools.Tool {
    return tools.Tool{
        Definition: mcp.Tool{
            Name:        "revoke_mcp_privilege",
            Description: "Revoke an MCP privilege (tool, resource, or prompt) from a group. Requires superuser privileges.",
            InputSchema: mcp.InputSchema{
                Type: "object",
                Properties: map[string]interface{}{
                    "group_id": map[string]interface{}{
                        "type":        "integer",
                        "description": "ID of the group to revoke the privilege from",
                    },
                    "privilege": map[string]interface{}{
                        "type":        "string",
                        "description": "Privilege identifier to revoke",
                    },
                },
                Required: []string{"group_id", "privilege"},
            },
        },
        Handler: func(args map[string]interface{}) (mcp.ToolResponse, error) {
            ctx := getContextFromArgs(args)
            if !auth.IsSuperuserFromContext(ctx) {
                return mcp.NewToolError("Access denied: superuser privileges required")
            }

            groupID, err := parseIntArg(args, "group_id")
            if err != nil {
                return mcp.NewToolError("Missing or invalid 'group_id' parameter")
            }

            privilege, ok := args["privilege"].(string)
            if !ok || strings.TrimSpace(privilege) == "" {
                return mcp.NewToolError("Missing or empty 'privilege' parameter")
            }

            if err := authStore.RevokeMCPPrivilegeByName(int64(groupID), privilege); err != nil {
                return mcp.NewToolError(fmt.Sprintf("Failed to revoke privilege: %v", err))
            }

            return mcp.NewToolSuccess(fmt.Sprintf("Privilege '%s' revoked from group %d", privilege, groupID))
        },
    }
}

// GrantConnectionPrivilegeTool creates a tool for granting connection privileges to groups
func GrantConnectionPrivilegeTool(authStore *auth.AuthStore) tools.Tool {
    return tools.Tool{
        Definition: mcp.Tool{
            Name:        "grant_connection_privilege",
            Description: "Grant access to a database connection for a group. Access levels are 'read' (SELECT only) or 'read_write' (full access). Requires superuser privileges.",
            InputSchema: mcp.InputSchema{
                Type: "object",
                Properties: map[string]interface{}{
                    "group_id": map[string]interface{}{
                        "type":        "integer",
                        "description": "ID of the group to grant the connection privilege to",
                    },
                    "connection_id": map[string]interface{}{
                        "type":        "integer",
                        "description": "ID of the database connection",
                    },
                    "access_level": map[string]interface{}{
                        "type":        "string",
                        "description": "Access level: 'read' or 'read_write'",
                        "enum":        []string{"read", "read_write"},
                    },
                },
                Required: []string{"group_id", "connection_id", "access_level"},
            },
        },
        Handler: func(args map[string]interface{}) (mcp.ToolResponse, error) {
            ctx := getContextFromArgs(args)
            if !auth.IsSuperuserFromContext(ctx) {
                return mcp.NewToolError("Access denied: superuser privileges required")
            }

            groupID, err := parseIntArg(args, "group_id")
            if err != nil {
                return mcp.NewToolError("Missing or invalid 'group_id' parameter")
            }

            connectionID, err := parseIntArg(args, "connection_id")
            if err != nil {
                return mcp.NewToolError("Missing or invalid 'connection_id' parameter")
            }

            accessLevel, ok := args["access_level"].(string)
            if !ok || (accessLevel != "read" && accessLevel != "read_write") {
                return mcp.NewToolError("Invalid 'access_level': must be 'read' or 'read_write'")
            }

            if err := authStore.GrantConnectionPrivilege(int64(groupID), connectionID, accessLevel); err != nil {
                return mcp.NewToolError(fmt.Sprintf("Failed to grant connection privilege: %v", err))
            }

            return mcp.NewToolSuccess(fmt.Sprintf("Connection %d granted to group %d with '%s' access", connectionID, groupID, accessLevel))
        },
    }
}

// RevokeConnectionPrivilegeTool creates a tool for revoking connection privileges from groups
func RevokeConnectionPrivilegeTool(authStore *auth.AuthStore) tools.Tool {
    return tools.Tool{
        Definition: mcp.Tool{
            Name:        "revoke_connection_privilege",
            Description: "Revoke access to a database connection from a group. Requires superuser privileges.",
            InputSchema: mcp.InputSchema{
                Type: "object",
                Properties: map[string]interface{}{
                    "group_id": map[string]interface{}{
                        "type":        "integer",
                        "description": "ID of the group to revoke the connection privilege from",
                    },
                    "connection_id": map[string]interface{}{
                        "type":        "integer",
                        "description": "ID of the database connection to revoke",
                    },
                },
                Required: []string{"group_id", "connection_id"},
            },
        },
        Handler: func(args map[string]interface{}) (mcp.ToolResponse, error) {
            ctx := getContextFromArgs(args)
            if !auth.IsSuperuserFromContext(ctx) {
                return mcp.NewToolError("Access denied: superuser privileges required")
            }

            groupID, err := parseIntArg(args, "group_id")
            if err != nil {
                return mcp.NewToolError("Missing or invalid 'group_id' parameter")
            }

            connectionID, err := parseIntArg(args, "connection_id")
            if err != nil {
                return mcp.NewToolError("Missing or invalid 'connection_id' parameter")
            }

            if err := authStore.RevokeConnectionPrivilege(int64(groupID), connectionID); err != nil {
                return mcp.NewToolError(fmt.Sprintf("Failed to revoke connection privilege: %v", err))
            }

            return mcp.NewToolSuccess(fmt.Sprintf("Connection %d revoked from group %d", connectionID, groupID))
        },
    }
}

// ListPrivilegesTool creates a tool for listing available and assigned privileges
func ListPrivilegesTool(authStore *auth.AuthStore) tools.Tool {
    return tools.Tool{
        Definition: mcp.Tool{
            Name:        "list_privileges",
            Description: "List all registered MCP privileges and optionally show assignments for a specific group. Requires superuser privileges.",
            InputSchema: mcp.InputSchema{
                Type: "object",
                Properties: map[string]interface{}{
                    "group_id": map[string]interface{}{
                        "type":        "integer",
                        "description": "Optional: ID of a group to show its specific privileges",
                    },
                },
            },
        },
        Handler: func(args map[string]interface{}) (mcp.ToolResponse, error) {
            ctx := getContextFromArgs(args)
            if !auth.IsSuperuserFromContext(ctx) {
                return mcp.NewToolError("Access denied: superuser privileges required")
            }

            var sb strings.Builder

            // If group_id is provided, show that group's privileges
            if groupID, err := parseIntArg(args, "group_id"); err == nil {
                groupPrivs, err := authStore.GetGroupWithPrivileges(int64(groupID))
                if err != nil {
                    return mcp.NewToolError(fmt.Sprintf("Failed to get group privileges: %v", err))
                }

                sb.WriteString(fmt.Sprintf("Privileges for group '%s' (ID: %d):\n", groupPrivs.Group.Name, groupPrivs.Group.ID))
                sb.WriteString(strings.Repeat("=", 60) + "\n\n")

                if len(groupPrivs.MCPPrivileges) == 0 && len(groupPrivs.ConnectionPrivileges) == 0 {
                    sb.WriteString("No privileges assigned to this group.\n")
                } else {
                    if len(groupPrivs.MCPPrivileges) > 0 {
                        sb.WriteString("MCP Privileges:\n")
                        for _, priv := range groupPrivs.MCPPrivileges {
                            sb.WriteString(fmt.Sprintf("  - %s (%s)\n", priv.Identifier, priv.ItemType))
                        }
                        sb.WriteString("\n")
                    }

                    if len(groupPrivs.ConnectionPrivileges) > 0 {
                        sb.WriteString("Connection Privileges:\n")
                        for _, priv := range groupPrivs.ConnectionPrivileges {
                            sb.WriteString(fmt.Sprintf("  - Connection %d: %s\n", priv.ConnectionID, priv.AccessLevel))
                        }
                    }
                }

                return mcp.NewToolSuccess(sb.String())
            }

            // List all registered privileges
            privileges, err := authStore.ListMCPPrivileges()
            if err != nil {
                return mcp.NewToolError(fmt.Sprintf("Failed to list privileges: %v", err))
            }

            if len(privileges) == 0 {
                return mcp.NewToolSuccess("No MCP privileges registered.")
            }

            sb.WriteString("Registered MCP Privileges:\n")
            sb.WriteString(strings.Repeat("=", 60) + "\n\n")

            // Group by type
            toolPrivs := []*auth.MCPPrivilege{}
            resourcePrivs := []*auth.MCPPrivilege{}
            promptPrivs := []*auth.MCPPrivilege{}

            for _, priv := range privileges {
                switch priv.ItemType {
                case auth.MCPPrivilegeTypeTool:
                    toolPrivs = append(toolPrivs, priv)
                case auth.MCPPrivilegeTypeResource:
                    resourcePrivs = append(resourcePrivs, priv)
                case auth.MCPPrivilegeTypePrompt:
                    promptPrivs = append(promptPrivs, priv)
                }
            }

            if len(toolPrivs) > 0 {
                sb.WriteString("Tools:\n")
                for _, priv := range toolPrivs {
                    sb.WriteString(fmt.Sprintf("  %s", priv.Identifier))
                    if priv.Description != "" {
                        sb.WriteString(fmt.Sprintf(" - %s", priv.Description))
                    }
                    sb.WriteString("\n")
                }
                sb.WriteString("\n")
            }

            if len(resourcePrivs) > 0 {
                sb.WriteString("Resources:\n")
                for _, priv := range resourcePrivs {
                    sb.WriteString(fmt.Sprintf("  %s", priv.Identifier))
                    if priv.Description != "" {
                        sb.WriteString(fmt.Sprintf(" - %s", priv.Description))
                    }
                    sb.WriteString("\n")
                }
                sb.WriteString("\n")
            }

            if len(promptPrivs) > 0 {
                sb.WriteString("Prompts:\n")
                for _, priv := range promptPrivs {
                    sb.WriteString(fmt.Sprintf("  %s", priv.Identifier))
                    if priv.Description != "" {
                        sb.WriteString(fmt.Sprintf(" - %s", priv.Description))
                    }
                    sb.WriteString("\n")
                }
            }

            return mcp.NewToolSuccess(sb.String())
        },
    }
}
