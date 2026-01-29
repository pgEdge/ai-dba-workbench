/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
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

// SetSuperuserTool creates a tool for promoting/demoting superuser status
func SetSuperuserTool(authStore *auth.AuthStore) tools.Tool {
	return tools.Tool{
		Definition: mcp.Tool{
			Name:        "set_superuser",
			Description: "Promote or demote a user's superuser status. Superusers bypass all RBAC privilege checks. Requires superuser privileges.",
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"username": map[string]interface{}{
						"type":        "string",
						"description": "Username of the user to modify",
					},
					"is_superuser": map[string]interface{}{
						"type":        "boolean",
						"description": "True to grant superuser, false to revoke",
					},
				},
				Required: []string{"username", "is_superuser"},
			},
		},
		Handler: func(args map[string]interface{}) (mcp.ToolResponse, error) {
			if err := RequireSuperuser(args); err != nil {
				return mcp.NewToolError(err.Error())
			}

			username, ok := args["username"].(string)
			if !ok || strings.TrimSpace(username) == "" {
				return mcp.NewToolError("Missing or empty 'username' parameter")
			}

			isSuperuser, ok := args["is_superuser"].(bool)
			if !ok {
				return mcp.NewToolError("Missing or invalid 'is_superuser' parameter")
			}

			if err := authStore.SetUserSuperuser(username, isSuperuser); err != nil {
				return mcp.NewToolError(fmt.Sprintf("Failed to update superuser status: %v", err))
			}

			if isSuperuser {
				return mcp.NewToolSuccess(fmt.Sprintf("User '%s' is now a superuser", username))
			}
			return mcp.NewToolSuccess(fmt.Sprintf("Superuser status removed from user '%s'", username))
		},
	}
}

// ListUsersTool creates a tool for listing all users with their group memberships
func ListUsersTool(authStore *auth.AuthStore, checker *auth.RBACChecker) tools.Tool {
	return tools.Tool{
		Definition: mcp.Tool{
			Name:        "list_users",
			Description: "List all users with their group memberships and superuser status. Requires manage_users permission.",
			InputSchema: mcp.InputSchema{
				Type:       "object",
				Properties: map[string]interface{}{},
			},
		},
		Handler: func(args map[string]interface{}) (mcp.ToolResponse, error) {
			if err := RequirePermission(args, checker, auth.PermManageUsers); err != nil {
				return mcp.NewToolError(err.Error())
			}

			users, err := authStore.ListUsers()
			if err != nil {
				return mcp.NewToolError(fmt.Sprintf("Failed to list users: %v", err))
			}

			if len(users) == 0 {
				return mcp.NewToolSuccess("No users found.")
			}

			var sb strings.Builder
			sb.WriteString("Users:\n")
			sb.WriteString(strings.Repeat("=", 60) + "\n\n")

			for _, user := range users {
				sb.WriteString(fmt.Sprintf("ID: %d\n", user.ID))
				sb.WriteString(fmt.Sprintf("Username: %s\n", user.Username))
				if user.IsSuperuser {
					sb.WriteString("Superuser: Yes\n")
				}
				sb.WriteString(fmt.Sprintf("Enabled: %v\n", user.Enabled))
				if user.Annotation != "" {
					sb.WriteString(fmt.Sprintf("Annotation: %s\n", user.Annotation))
				}

				// Get user's groups
				groups, err := authStore.GetGroupsForUser(user.ID)
				if err == nil && len(groups) > 0 {
					groupNames := make([]string, len(groups))
					for i, g := range groups {
						groupNames[i] = g.Name
					}
					sb.WriteString(fmt.Sprintf("Groups: %s\n", strings.Join(groupNames, ", ")))
				}

				sb.WriteString("\n")
			}

			return mcp.NewToolSuccess(sb.String())
		},
	}
}

// GetUserPrivilegesTool creates a tool for viewing a user's effective privileges
func GetUserPrivilegesTool(authStore *auth.AuthStore, checker *auth.RBACChecker) tools.Tool {
	return tools.Tool{
		Definition: mcp.Tool{
			Name:        "get_user_privileges",
			Description: "View a user's effective privileges (from all group memberships). Requires manage_users permission.",
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"username": map[string]interface{}{
						"type":        "string",
						"description": "Username to check privileges for",
					},
				},
				Required: []string{"username"},
			},
		},
		Handler: func(args map[string]interface{}) (mcp.ToolResponse, error) {
			if err := RequirePermission(args, checker, auth.PermManageUsers); err != nil {
				return mcp.NewToolError(err.Error())
			}

			username, ok := args["username"].(string)
			if !ok || strings.TrimSpace(username) == "" {
				return mcp.NewToolError("Missing or empty 'username' parameter")
			}

			// Get user
			user, err := authStore.GetUser(username)
			if err != nil {
				return mcp.NewToolError(fmt.Sprintf("Failed to get user: %v", err))
			}
			if user == nil {
				return mcp.NewToolError(fmt.Sprintf("User '%s' not found", username))
			}

			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("Effective privileges for user '%s':\n", username))
			sb.WriteString(strings.Repeat("=", 60) + "\n\n")

			if user.IsSuperuser {
				sb.WriteString("SUPERUSER: Yes (bypasses all privilege checks)\n")
				return mcp.NewToolSuccess(sb.String())
			}

			// Get user's groups
			groups, err := authStore.GetGroupsForUser(user.ID)
			if err == nil && len(groups) > 0 {
				groupNames := make([]string, len(groups))
				for i, g := range groups {
					groupNames[i] = g.Name
				}
				sb.WriteString(fmt.Sprintf("Member of groups: %s\n\n", strings.Join(groupNames, ", ")))
			} else {
				sb.WriteString("Not a member of any groups.\n\n")
			}

			// Get MCP privileges
			mcpPrivs, err := authStore.GetUserMCPPrivileges(user.ID)
			if err != nil {
				sb.WriteString(fmt.Sprintf("Error getting MCP privileges: %v\n", err))
			} else if len(mcpPrivs) > 0 {
				sb.WriteString("MCP Privileges:\n")
				for identifier := range mcpPrivs {
					sb.WriteString(fmt.Sprintf("  - %s\n", identifier))
				}
				sb.WriteString("\n")
			} else {
				sb.WriteString("MCP Privileges: None\n\n")
			}

			// Get connection privileges
			connPrivs, err := authStore.GetUserConnectionPrivileges(user.ID)
			if err != nil {
				sb.WriteString(fmt.Sprintf("Error getting connection privileges: %v\n", err))
			} else if len(connPrivs) > 0 {
				sb.WriteString("Connection Privileges:\n")
				for connID, accessLevel := range connPrivs {
					sb.WriteString(fmt.Sprintf("  - Connection %d: %s\n", connID, accessLevel))
				}
			} else {
				sb.WriteString("Connection Privileges: None\n")
			}

			return mcp.NewToolSuccess(sb.String())
		},
	}
}
