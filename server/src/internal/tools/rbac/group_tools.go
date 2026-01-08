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
    "context"
    "fmt"
    "strings"

    "github.com/pgedge/ai-workbench/server/internal/auth"
    "github.com/pgedge/ai-workbench/server/internal/mcp"
    "github.com/pgedge/ai-workbench/server/internal/tools"
)

// CreateGroupTool creates a tool for creating RBAC groups
func CreateGroupTool(authStore *auth.AuthStore) tools.Tool {
    return tools.Tool{
        Definition: mcp.Tool{
            Name:        "create_group",
            Description: "Create a new RBAC group for organizing users and managing privileges. Groups can contain users and other groups for hierarchical access control. Requires superuser privileges.",
            InputSchema: mcp.InputSchema{
                Type: "object",
                Properties: map[string]interface{}{
                    "name": map[string]interface{}{
                        "type":        "string",
                        "description": "Unique name for the group",
                    },
                    "description": map[string]interface{}{
                        "type":        "string",
                        "description": "Optional description of the group's purpose",
                    },
                },
                Required: []string{"name"},
            },
        },
        Handler: func(args map[string]interface{}) (mcp.ToolResponse, error) {
            ctx := getContextFromArgs(args)
            if !auth.IsSuperuserFromContext(ctx) {
                return mcp.NewToolError("Access denied: superuser privileges required")
            }

            name, ok := args["name"].(string)
            if !ok || strings.TrimSpace(name) == "" {
                return mcp.NewToolError("Missing or empty 'name' parameter")
            }

            description := ""
            if d, ok := args["description"].(string); ok {
                description = d
            }

            groupID, err := authStore.CreateGroup(name, description)
            if err != nil {
                return mcp.NewToolError(fmt.Sprintf("Failed to create group: %v", err))
            }

            return mcp.NewToolSuccess(fmt.Sprintf("Group '%s' created successfully (ID: %d)", name, groupID))
        },
    }
}

// UpdateGroupTool creates a tool for updating RBAC groups
func UpdateGroupTool(authStore *auth.AuthStore) tools.Tool {
    return tools.Tool{
        Definition: mcp.Tool{
            Name:        "update_group",
            Description: "Update an existing RBAC group's name or description. Requires superuser privileges.",
            InputSchema: mcp.InputSchema{
                Type: "object",
                Properties: map[string]interface{}{
                    "group_id": map[string]interface{}{
                        "type":        "integer",
                        "description": "ID of the group to update",
                    },
                    "name": map[string]interface{}{
                        "type":        "string",
                        "description": "New name for the group",
                    },
                    "description": map[string]interface{}{
                        "type":        "string",
                        "description": "New description for the group",
                    },
                },
                Required: []string{"group_id"},
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

            name := ""
            if n, ok := args["name"].(string); ok {
                name = n
            }
            description := ""
            if d, ok := args["description"].(string); ok {
                description = d
            }

            if name == "" && description == "" {
                return mcp.NewToolError("At least one of 'name' or 'description' must be provided")
            }

            if err := authStore.UpdateGroup(int64(groupID), name, description); err != nil {
                return mcp.NewToolError(fmt.Sprintf("Failed to update group: %v", err))
            }

            return mcp.NewToolSuccess(fmt.Sprintf("Group %d updated successfully", groupID))
        },
    }
}

// DeleteGroupTool creates a tool for deleting RBAC groups
func DeleteGroupTool(authStore *auth.AuthStore) tools.Tool {
    return tools.Tool{
        Definition: mcp.Tool{
            Name:        "delete_group",
            Description: "Delete an RBAC group. This will also remove all group memberships and privilege grants. Requires superuser privileges.",
            InputSchema: mcp.InputSchema{
                Type: "object",
                Properties: map[string]interface{}{
                    "group_id": map[string]interface{}{
                        "type":        "integer",
                        "description": "ID of the group to delete",
                    },
                },
                Required: []string{"group_id"},
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

            if err := authStore.DeleteGroup(int64(groupID)); err != nil {
                return mcp.NewToolError(fmt.Sprintf("Failed to delete group: %v", err))
            }

            return mcp.NewToolSuccess(fmt.Sprintf("Group %d deleted successfully", groupID))
        },
    }
}

// ListGroupsTool creates a tool for listing RBAC groups
func ListGroupsTool(authStore *auth.AuthStore) tools.Tool {
    return tools.Tool{
        Definition: mcp.Tool{
            Name:        "list_groups",
            Description: "List all RBAC groups with their members and assigned privileges. Requires superuser privileges.",
            InputSchema: mcp.InputSchema{
                Type:       "object",
                Properties: map[string]interface{}{},
            },
        },
        Handler: func(args map[string]interface{}) (mcp.ToolResponse, error) {
            ctx := getContextFromArgs(args)
            if !auth.IsSuperuserFromContext(ctx) {
                return mcp.NewToolError("Access denied: superuser privileges required")
            }

            groups, err := authStore.ListGroups()
            if err != nil {
                return mcp.NewToolError(fmt.Sprintf("Failed to list groups: %v", err))
            }

            if len(groups) == 0 {
                return mcp.NewToolSuccess("No groups found.")
            }

            var sb strings.Builder
            sb.WriteString("RBAC Groups:\n")
            sb.WriteString(strings.Repeat("=", 60) + "\n\n")

            for _, group := range groups {
                sb.WriteString(fmt.Sprintf("ID: %d\n", group.ID))
                sb.WriteString(fmt.Sprintf("Name: %s\n", group.Name))
                if group.Description != "" {
                    sb.WriteString(fmt.Sprintf("Description: %s\n", group.Description))
                }

                // Get members
                members, err := authStore.GetGroupMembers(int64(group.ID))
                if err == nil && members != nil {
                    if len(members.UserMembers) > 0 {
                        sb.WriteString(fmt.Sprintf("User Members: %s\n", strings.Join(members.UserMembers, ", ")))
                    }
                    if len(members.GroupMembers) > 0 {
                        sb.WriteString(fmt.Sprintf("Group Members: %s\n", strings.Join(members.GroupMembers, ", ")))
                    }
                }

                sb.WriteString("\n")
            }

            return mcp.NewToolSuccess(sb.String())
        },
    }
}

// AddGroupMemberTool creates a tool for adding members to groups
func AddGroupMemberTool(authStore *auth.AuthStore) tools.Tool {
    return tools.Tool{
        Definition: mcp.Tool{
            Name:        "add_group_member",
            Description: "Add a user or group to an RBAC group. Use either user_id or member_group_id, not both. Requires superuser privileges.",
            InputSchema: mcp.InputSchema{
                Type: "object",
                Properties: map[string]interface{}{
                    "group_id": map[string]interface{}{
                        "type":        "integer",
                        "description": "ID of the group to add the member to",
                    },
                    "user_id": map[string]interface{}{
                        "type":        "integer",
                        "description": "ID of the user to add (use this OR member_group_id)",
                    },
                    "member_group_id": map[string]interface{}{
                        "type":        "integer",
                        "description": "ID of the group to add as a nested member (use this OR user_id)",
                    },
                },
                Required: []string{"group_id"},
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

            userID, userErr := parseIntArg(args, "user_id")
            memberGroupID, groupErr := parseIntArg(args, "member_group_id")

            if userErr != nil && groupErr != nil {
                return mcp.NewToolError("Must specify either 'user_id' or 'member_group_id'")
            }

            if userErr == nil && groupErr == nil {
                return mcp.NewToolError("Cannot specify both 'user_id' and 'member_group_id'")
            }

            if userErr == nil {
                // Add user to group
                if err := authStore.AddUserToGroup(int64(groupID), int64(userID)); err != nil {
                    return mcp.NewToolError(fmt.Sprintf("Failed to add user to group: %v", err))
                }
                return mcp.NewToolSuccess(fmt.Sprintf("User %d added to group %d", userID, groupID))
            }

            // Add group to group
            if err := authStore.AddGroupToGroup(int64(groupID), int64(memberGroupID)); err != nil {
                return mcp.NewToolError(fmt.Sprintf("Failed to add group to group: %v", err))
            }
            return mcp.NewToolSuccess(fmt.Sprintf("Group %d added to group %d", memberGroupID, groupID))
        },
    }
}

// RemoveGroupMemberTool creates a tool for removing members from groups
func RemoveGroupMemberTool(authStore *auth.AuthStore) tools.Tool {
    return tools.Tool{
        Definition: mcp.Tool{
            Name:        "remove_group_member",
            Description: "Remove a user or group from an RBAC group. Use either user_id or member_group_id, not both. Requires superuser privileges.",
            InputSchema: mcp.InputSchema{
                Type: "object",
                Properties: map[string]interface{}{
                    "group_id": map[string]interface{}{
                        "type":        "integer",
                        "description": "ID of the group to remove the member from",
                    },
                    "user_id": map[string]interface{}{
                        "type":        "integer",
                        "description": "ID of the user to remove (use this OR member_group_id)",
                    },
                    "member_group_id": map[string]interface{}{
                        "type":        "integer",
                        "description": "ID of the group to remove (use this OR user_id)",
                    },
                },
                Required: []string{"group_id"},
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

            userID, userErr := parseIntArg(args, "user_id")
            memberGroupID, groupErr := parseIntArg(args, "member_group_id")

            if userErr != nil && groupErr != nil {
                return mcp.NewToolError("Must specify either 'user_id' or 'member_group_id'")
            }

            if userErr == nil && groupErr == nil {
                return mcp.NewToolError("Cannot specify both 'user_id' and 'member_group_id'")
            }

            if userErr == nil {
                // Remove user from group
                if err := authStore.RemoveUserFromGroup(int64(groupID), int64(userID)); err != nil {
                    return mcp.NewToolError(fmt.Sprintf("Failed to remove user from group: %v", err))
                }
                return mcp.NewToolSuccess(fmt.Sprintf("User %d removed from group %d", userID, groupID))
            }

            // Remove group from group
            if err := authStore.RemoveGroupFromGroup(int64(groupID), int64(memberGroupID)); err != nil {
                return mcp.NewToolError(fmt.Sprintf("Failed to remove group from group: %v", err))
            }
            return mcp.NewToolSuccess(fmt.Sprintf("Group %d removed from group %d", memberGroupID, groupID))
        },
    }
}

// Helper function to get context from args
func getContextFromArgs(args map[string]interface{}) context.Context {
    if ctx, ok := args["__context"].(context.Context); ok {
        return ctx
    }
    return context.Background()
}

// parseIntArg parses an integer argument from the args map
func parseIntArg(args map[string]interface{}, name string) (int, error) {
    val, ok := args[name]
    if !ok {
        return 0, fmt.Errorf("parameter not found")
    }

    switch v := val.(type) {
    case float64:
        return int(v), nil
    case int:
        return v, nil
    case int64:
        return int(v), nil
    default:
        return 0, fmt.Errorf("invalid type: expected number")
    }
}
