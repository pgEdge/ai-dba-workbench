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
	"strconv"
	"strings"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
	"github.com/pgedge/ai-workbench/server/internal/tools"
)

// SetTokenScopeTool creates a tool for setting token scope restrictions
func SetTokenScopeTool(authStore *auth.AuthStore) tools.Tool {
	return tools.Tool{
		Definition: mcp.Tool{
			Name:        "set_token_scope",
			Description: "Restrict a token's access to specific connections or MCP privileges. This can only restrict access, never expand beyond the token owner's privileges. Requires superuser privileges.",
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"token_id": map[string]interface{}{
						"type":        "integer",
						"description": "ID of the token to scope",
					},
					"connection_ids": map[string]interface{}{
						"type":        "string",
						"description": "Comma-separated list of connection IDs to restrict to (e.g., '1,2,3')",
					},
					"privileges": map[string]interface{}{
						"type":        "string",
						"description": "Comma-separated list of MCP privilege identifiers (e.g., 'query_database,get_schema_info')",
					},
				},
				Required: []string{"token_id"},
			},
		},
		Handler: func(args map[string]interface{}) (mcp.ToolResponse, error) {
			ctx := getContextFromArgs(args)
			if !auth.IsSuperuserFromContext(ctx) {
				return mcp.NewToolError("Access denied: superuser privileges required")
			}

			tokenID, err := parseIntArg(args, "token_id")
			if err != nil {
				return mcp.NewToolError("Missing or invalid 'token_id' parameter")
			}

			var results []string

			// Set connection scope if provided
			if connStr, ok := args["connection_ids"].(string); ok && connStr != "" {
				connIDs, err := parseIntList(connStr)
				if err != nil {
					return mcp.NewToolError(fmt.Sprintf("Invalid 'connection_ids': %v", err))
				}

				if err := authStore.SetTokenConnectionScope(int64(tokenID), connIDs); err != nil {
					return mcp.NewToolError(fmt.Sprintf("Failed to set connection scope: %v", err))
				}
				results = append(results, fmt.Sprintf("Connection scope set to: %v", connIDs))
			}

			// Set MCP privilege scope if provided
			if privStr, ok := args["privileges"].(string); ok && privStr != "" {
				privNames := strings.Split(privStr, ",")
				for i, name := range privNames {
					privNames[i] = strings.TrimSpace(name)
				}

				if err := authStore.SetTokenMCPScopeByNames(int64(tokenID), privNames); err != nil {
					return mcp.NewToolError(fmt.Sprintf("Failed to set MCP scope: %v", err))
				}
				results = append(results, fmt.Sprintf("MCP privilege scope set to: %v", privNames))
			}

			if len(results) == 0 {
				return mcp.NewToolError("Must specify at least 'connection_ids' or 'privileges'")
			}

			return mcp.NewToolSuccess(fmt.Sprintf("Token %d scope updated:\n%s", tokenID, strings.Join(results, "\n")))
		},
	}
}

// GetTokenScopeTool creates a tool for viewing token scope restrictions
func GetTokenScopeTool(authStore *auth.AuthStore) tools.Tool {
	return tools.Tool{
		Definition: mcp.Tool{
			Name:        "get_token_scope",
			Description: "View the scope restrictions for a token. Requires superuser privileges.",
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"token_id": map[string]interface{}{
						"type":        "integer",
						"description": "ID of the token to view scope for",
					},
				},
				Required: []string{"token_id"},
			},
		},
		Handler: func(args map[string]interface{}) (mcp.ToolResponse, error) {
			ctx := getContextFromArgs(args)
			if !auth.IsSuperuserFromContext(ctx) {
				return mcp.NewToolError("Access denied: superuser privileges required")
			}

			tokenID, err := parseIntArg(args, "token_id")
			if err != nil {
				return mcp.NewToolError("Missing or invalid 'token_id' parameter")
			}

			scope, err := authStore.GetTokenScope(int64(tokenID))
			if err != nil {
				return mcp.NewToolError(fmt.Sprintf("Failed to get token scope: %v", err))
			}

			if scope == nil {
				return mcp.NewToolSuccess(fmt.Sprintf("Token %d has no scope restrictions (full access to assigned privileges)", tokenID))
			}

			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("Token %d Scope:\n", tokenID))
			sb.WriteString(strings.Repeat("=", 40) + "\n\n")

			if len(scope.ConnectionIDs) > 0 {
				sb.WriteString("Connection restrictions:\n")
				for _, connID := range scope.ConnectionIDs {
					sb.WriteString(fmt.Sprintf("  - Connection %d\n", connID))
				}
				sb.WriteString("\n")
			}

			if len(scope.MCPPrivileges) > 0 {
				sb.WriteString("MCP privilege restrictions:\n")
				for _, privID := range scope.MCPPrivileges {
					priv, err := authStore.GetMCPPrivilegeByID(privID)
					if err == nil && priv != nil {
						sb.WriteString(fmt.Sprintf("  - %s (%s)\n", priv.Identifier, priv.ItemType))
					} else {
						sb.WriteString(fmt.Sprintf("  - Privilege ID %d\n", privID))
					}
				}
			}

			return mcp.NewToolSuccess(sb.String())
		},
	}
}

// ClearTokenScopeTool creates a tool for clearing token scope restrictions
func ClearTokenScopeTool(authStore *auth.AuthStore) tools.Tool {
	return tools.Tool{
		Definition: mcp.Tool{
			Name:        "clear_token_scope",
			Description: "Remove all scope restrictions from a token, restoring full access to assigned privileges. Requires superuser privileges.",
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"token_id": map[string]interface{}{
						"type":        "integer",
						"description": "ID of the token to clear scope for",
					},
				},
				Required: []string{"token_id"},
			},
		},
		Handler: func(args map[string]interface{}) (mcp.ToolResponse, error) {
			ctx := getContextFromArgs(args)
			if !auth.IsSuperuserFromContext(ctx) {
				return mcp.NewToolError("Access denied: superuser privileges required")
			}

			tokenID, err := parseIntArg(args, "token_id")
			if err != nil {
				return mcp.NewToolError("Missing or invalid 'token_id' parameter")
			}

			if err := authStore.ClearTokenScope(int64(tokenID)); err != nil {
				return mcp.NewToolError(fmt.Sprintf("Failed to clear token scope: %v", err))
			}

			return mcp.NewToolSuccess(fmt.Sprintf("All scope restrictions cleared from token %d", tokenID))
		},
	}
}

// parseIntList parses a comma-separated list of integers
func parseIntList(s string) ([]int, error) {
	var result []int
	parts := strings.Split(s, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		val, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("invalid integer: %s", part)
		}
		result = append(result, val)
	}
	return result, nil
}
