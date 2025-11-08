/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgEdge/ai-workbench/server/src/config"
	"github.com/pgEdge/ai-workbench/server/src/groupmgmt"
	"github.com/pgedge/ai-workbench/pkg/logger"
	"github.com/pgEdge/ai-workbench/server/src/privileges"
	"github.com/pgEdge/ai-workbench/server/src/usermgmt"
)

// Handler processes MCP requests
type Handler struct {
	serverName    string
	serverVersion string
	initialized   bool
	dbPool        *pgxpool.Pool
	config        *config.Config
	userInfo      *UserInfo
}

// NewHandler creates a new MCP handler
func NewHandler(serverName, serverVersion string, dbPool *pgxpool.Pool, cfg *config.Config) *Handler {
	return &Handler{
		serverName:    serverName,
		serverVersion: serverVersion,
		initialized:   false,
		dbPool:        dbPool,
		config:        cfg,
	}
}

// HandleRequest processes an MCP request and returns a response
func (h *Handler) HandleRequest(data []byte, bearerToken string) (*Response,
	error) {
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		logger.Errorf("Failed to unmarshal request: %v", err)
		return NewErrorResponse(nil, ParseError, "Parse error", err.Error()),
			nil
	}

	logger.Infof("Handling MCP request: method=%s, id=%v", req.Method,
		req.ID)

	// Validate JSON-RPC version
	if req.JSONRPC != JSONRPCVersion {
		return NewErrorResponse(req.ID, InvalidRequest,
			"Invalid JSON-RPC version", nil), nil
	}

	// Check authentication for protected methods
	// Methods that don't require authentication:
	// - initialize, ping (protocol/health check methods)
	// For tools/call, we'll check if it's authenticate_user specifically
	// Skip authentication if dbPool is nil (for unit tests)
	requiresAuth := true
	if h.dbPool == nil {
		// No database pool, skip authentication (unit test mode)
		requiresAuth = false
	} else {
		switch req.Method {
		case "initialize", "ping":
			requiresAuth = false
		case "tools/call":
			// Need to check if it's the authenticate_user tool
			var params struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(req.Params, &params); err == nil {
				if params.Name == "authenticate_user" {
					requiresAuth = false
				}
			}
		}
	}

	var userInfo *UserInfo
	if requiresAuth {
		// Validate bearer token
		var err error
		userInfo, err = h.validateToken(bearerToken)
		if err != nil {
			logger.Errorf("Token validation failed: %v", err)
			return NewErrorResponse(req.ID, InternalError,
				"Authentication failed", nil), nil
		}
		if userInfo == nil || !userInfo.IsAuthenticated {
			logger.Errorf("Unauthenticated request for method: %s", req.Method)
			return NewErrorResponse(req.ID, InvalidRequest,
				"Authentication required", nil), nil
		}
	}

	// Store userInfo in handler for access by tool handlers
	h.userInfo = userInfo

	// Route to appropriate handler based on method
	switch req.Method {
	case "initialize":
		return h.handleInitialize(req)
	case "ping":
		return h.handlePing(req)
	case "resources/list":
		return h.handleListResources(req)
	case "resources/read":
		return h.handleReadResource(req)
	case "tools/list":
		return h.handleListTools(req)
	case "tools/call":
		return h.handleCallTool(req)
	case "prompts/list":
		return h.handleListPrompts(req)
	default:
		logger.Errorf("Method not found: %s", req.Method)
		return NewErrorResponse(req.ID, MethodNotFound, "Method not found",
			nil), nil
	}
}

// handleInitialize processes the initialize request
func (h *Handler) handleInitialize(req Request) (*Response, error) {
	var params InitializeParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		logger.Errorf("Failed to unmarshal initialize params: %v", err)
		return NewErrorResponse(req.ID, InvalidParams, "Invalid parameters",
			err.Error()), nil
	}

	logger.Infof("Initialize request from client: %s v%s",
		params.ClientInfo.Name, params.ClientInfo.Version)

	// Mark as initialized
	h.initialized = true

	// Build response with server capabilities (empty for now, no tools/resources)
	result := InitializeResult{
		ProtocolVersion: "2024-11-05", // MCP protocol version
		Capabilities:    make(map[string]interface{}),
		ServerInfo: ServerInfo{
			Name:    h.serverName,
			Version: h.serverVersion,
		},
	}

	logger.Info("Server initialized successfully")
	return NewResponse(req.ID, result), nil
}

// handlePing processes the ping request
func (h *Handler) handlePing(req Request) (*Response, error) {
	result := map[string]interface{}{
		"status": "ok",
	}
	return NewResponse(req.ID, result), nil
}

// handleListResources processes the resources/list request
func (h *Handler) handleListResources(req Request) (*Response, error) {
	resources := []map[string]interface{}{
		{
			"uri":         "ai-workbench://users",
			"name":        "User Accounts",
			"description": "List of all user accounts in the system",
			"mimeType":    "application/json",
		},
		{
			"uri":         "ai-workbench://service-tokens",
			"name":        "Service Tokens",
			"description": "List of all service tokens in the system",
			"mimeType":    "application/json",
		},
	}

	result := map[string]interface{}{
		"resources": resources,
	}
	logger.Info("Listed resources")
	return NewResponse(req.ID, result), nil
}

// handleReadResource processes the resources/read request
func (h *Handler) handleReadResource(req Request) (*Response, error) {
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		logger.Errorf("Failed to unmarshal resource read params: %v", err)
		return NewErrorResponse(req.ID, InvalidParams, "Invalid parameters",
			err.Error()), nil
	}

	ctx := context.Background()
	var contents []map[string]interface{}

	switch params.URI {
	case "ai-workbench://users":
		// Fetch user accounts from database
		rows, err := h.dbPool.Query(ctx, `
			SELECT username, email, full_name, password_expiry, is_superuser,
			       created_at, updated_at
			FROM user_accounts
			ORDER BY username
		`)
		if err != nil {
			logger.Errorf("Failed to query user accounts: %v", err)
			return NewErrorResponse(req.ID, InternalError,
				"Failed to query user accounts", err.Error()), nil
		}
		defer rows.Close()

		for rows.Next() {
			var username, email, fullName string
			var passwordExpiry, createdAt, updatedAt interface{}
			var isSuperuser bool

			if err := rows.Scan(&username, &email, &fullName, &passwordExpiry,
				&isSuperuser, &createdAt, &updatedAt); err != nil {
				logger.Errorf("Failed to scan user account: %v", err)
				continue
			}

			contents = append(contents, map[string]interface{}{
				"uri":      fmt.Sprintf("ai-workbench://users/%s", username),
				"mimeType": "application/json",
				"text": fmt.Sprintf(`{"username": %q, "email": %q, "fullName": %q, "isSuperuser": %t, "passwordExpiry": %v, "createdAt": %v, "updatedAt": %v}`,
					username, email, fullName, isSuperuser, passwordExpiry, createdAt, updatedAt),
			})
		}

	case "ai-workbench://service-tokens":
		// Fetch service tokens from database
		rows, err := h.dbPool.Query(ctx, `
			SELECT name, is_superuser, note, expires_at, created_at, updated_at
			FROM service_tokens
			ORDER BY name
		`)
		if err != nil {
			logger.Errorf("Failed to query service tokens: %v", err)
			return NewErrorResponse(req.ID, InternalError,
				"Failed to query service tokens", err.Error()), nil
		}
		defer rows.Close()

		for rows.Next() {
			var name string
			var isSuperuser bool
			var note, expiresAt, createdAt, updatedAt interface{}

			if err := rows.Scan(&name, &isSuperuser, &note, &expiresAt,
				&createdAt, &updatedAt); err != nil {
				logger.Errorf("Failed to scan service token: %v", err)
				continue
			}

			contents = append(contents, map[string]interface{}{
				"uri":      fmt.Sprintf("ai-workbench://service-tokens/%s", name),
				"mimeType": "application/json",
				"text": fmt.Sprintf(`{"name": %q, "isSuperuser": %t, "note": %v, "expiresAt": %v, "createdAt": %v, "updatedAt": %v}`,
					name, isSuperuser, note, expiresAt, createdAt, updatedAt),
			})
		}

	default:
		return NewErrorResponse(req.ID, InvalidParams, "Unknown resource URI",
			nil), nil
	}

	result := map[string]interface{}{
		"contents": contents,
	}
	logger.Infof("Read resource: %s (%d items)", params.URI, len(contents))
	return NewResponse(req.ID, result), nil
}

// handleListTools processes the tools/list request
func (h *Handler) handleListTools(req Request) (*Response, error) {
	tools := []map[string]interface{}{
		{
			"name":        "authenticate_user",
			"description": "Authenticate a user and obtain a session token",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"username": map[string]interface{}{
						"type":        "string",
						"description": "Username to authenticate",
					},
					"password": map[string]interface{}{
						"type":        "string",
						"description": "Password for authentication",
					},
				},
				"required": []string{"username", "password"},
			},
		},
		{
			"name":        "create_user",
			"description": "Create a new user account",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"username": map[string]interface{}{
						"type":        "string",
						"description": "Username for the new user",
					},
					"email": map[string]interface{}{
						"type":        "string",
						"description": "Email address for the new user",
					},
					"fullName": map[string]interface{}{
						"type":        "string",
						"description": "Full name of the user",
					},
					"password": map[string]interface{}{
						"type":        "string",
						"description": "Password for the new user",
					},
					"isSuperuser": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether the user should have superuser privileges",
						"default":     false,
					},
					"passwordExpiry": map[string]interface{}{
						"type":        "string",
						"description": "Password expiry date (YYYY-MM-DD format, optional)",
					},
				},
				"required": []string{"username", "email", "fullName", "password"},
			},
		},
		{
			"name":        "update_user",
			"description": "Update an existing user account",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"username": map[string]interface{}{
						"type":        "string",
						"description": "Username of the user to update",
					},
					"email": map[string]interface{}{
						"type":        "string",
						"description": "New email address (optional)",
					},
					"fullName": map[string]interface{}{
						"type":        "string",
						"description": "New full name (optional)",
					},
					"password": map[string]interface{}{
						"type":        "string",
						"description": "New password (optional)",
					},
					"isSuperuser": map[string]interface{}{
						"type":        "boolean",
						"description": "Update superuser status (optional)",
					},
					"passwordExpiry": map[string]interface{}{
						"type":        "string",
						"description": "New password expiry date (YYYY-MM-DD format, optional)",
					},
					"clearPasswordExpiry": map[string]interface{}{
						"type":        "boolean",
						"description": "Clear password expiry (optional)",
						"default":     false,
					},
				},
				"required": []string{"username"},
			},
		},
		{
			"name":        "delete_user",
			"description": "Delete a user account",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"username": map[string]interface{}{
						"type":        "string",
						"description": "Username of the user to delete",
					},
				},
				"required": []string{"username"},
			},
		},
		{
			"name":        "create_service_token",
			"description": "Create a new service token",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Name for the service token",
					},
					"isSuperuser": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether the token should have superuser privileges",
						"default":     false,
					},
					"note": map[string]interface{}{
						"type":        "string",
						"description": "Optional note about the token",
					},
					"expiresAt": map[string]interface{}{
						"type":        "string",
						"description": "Expiry date (YYYY-MM-DD format, optional)",
					},
				},
				"required": []string{"name"},
			},
		},
		{
			"name":        "update_service_token",
			"description": "Update an existing service token",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Name of the service token to update",
					},
					"isSuperuser": map[string]interface{}{
						"type":        "boolean",
						"description": "Update superuser status (optional)",
					},
					"note": map[string]interface{}{
						"type":        "string",
						"description": "Update note (optional)",
					},
					"expiresAt": map[string]interface{}{
						"type":        "string",
						"description": "New expiry date (YYYY-MM-DD format, optional)",
					},
					"clearNote": map[string]interface{}{
						"type":        "boolean",
						"description": "Clear the note (optional)",
						"default":     false,
					},
					"clearExpiresAt": map[string]interface{}{
						"type":        "boolean",
						"description": "Clear expiry date (optional)",
						"default":     false,
					},
				},
				"required": []string{"name"},
			},
		},
		{
			"name":        "delete_service_token",
			"description": "Delete a service token",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Name of the service token to delete",
					},
				},
				"required": []string{"name"},
			},
		},
		{
			"name":        "create_user_token",
			"description": "Create a new user token for command-line API access",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"username": map[string]interface{}{
						"type":        "string",
						"description": "Username who will own this token",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Optional name for the token",
					},
					"lifetimeDays": map[string]interface{}{
						"type":        "integer",
						"description": "Token lifetime in days (0 = indefinite, subject to server maximum)",
					},
					"note": map[string]interface{}{
						"type":        "string",
						"description": "Optional note about the token",
					},
				},
				"required": []string{"username", "lifetimeDays"},
			},
		},
		{
			"name":        "list_user_tokens",
			"description": "List all tokens for a specific user",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"username": map[string]interface{}{
						"type":        "string",
						"description": "Username whose tokens to list",
					},
				},
				"required": []string{"username"},
			},
		},
		{
			"name":        "delete_user_token",
			"description": "Delete a user token by ID",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"username": map[string]interface{}{
						"type":        "string",
						"description": "Username who owns the token",
					},
					"tokenId": map[string]interface{}{
						"type":        "integer",
						"description": "ID of the token to delete",
					},
				},
				"required": []string{"username", "tokenId"},
			},
		},
		{
			"name":        "create_user_group",
			"description": "Create a new user group for organizing users and permissions",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Unique name for the group",
					},
					"description": map[string]interface{}{
						"type":        "string",
						"description": "Optional description of the group's purpose",
					},
				},
				"required": []string{"name"},
			},
		},
		{
			"name":        "update_user_group",
			"description": "Update an existing user group",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"groupId": map[string]interface{}{
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
				"required": []string{"groupId", "name"},
			},
		},
		{
			"name":        "delete_user_group",
			"description": "Delete a user group (also removes all memberships and privileges)",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"groupId": map[string]interface{}{
						"type":        "integer",
						"description": "ID of the group to delete",
					},
				},
				"required": []string{"groupId"},
			},
		},
		{
			"name":        "list_user_groups",
			"description": "List all user groups in the system",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
				"required":   []string{},
			},
		},
		{
			"name":        "add_group_member",
			"description": "Add a user or nested group as a member of a parent group",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"parentGroupId": map[string]interface{}{
						"type":        "integer",
						"description": "ID of the parent group",
					},
					"memberUserId": map[string]interface{}{
						"type":        "integer",
						"description": "ID of the user to add (specify either memberUserId or memberGroupId)",
					},
					"memberGroupId": map[string]interface{}{
						"type":        "integer",
						"description": "ID of the group to add (specify either memberUserId or memberGroupId)",
					},
				},
				"required": []string{"parentGroupId"},
			},
		},
		{
			"name":        "remove_group_member",
			"description": "Remove a user or nested group from a parent group",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"parentGroupId": map[string]interface{}{
						"type":        "integer",
						"description": "ID of the parent group",
					},
					"memberUserId": map[string]interface{}{
						"type":        "integer",
						"description": "ID of the user to remove (specify either memberUserId or memberGroupId)",
					},
					"memberGroupId": map[string]interface{}{
						"type":        "integer",
						"description": "ID of the group to remove (specify either memberUserId or memberGroupId)",
					},
				},
				"required": []string{"parentGroupId"},
			},
		},
		{
			"name":        "list_group_members",
			"description": "List all members (users and nested groups) of a group",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"groupId": map[string]interface{}{
						"type":        "integer",
						"description": "ID of the group",
					},
				},
				"required": []string{"groupId"},
			},
		},
		{
			"name":        "list_user_group_memberships",
			"description": "List all groups a user belongs to (direct and indirect membership)",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"username": map[string]interface{}{
						"type":        "string",
						"description": "Username to look up",
					},
				},
				"required": []string{"username"},
			},
		},
		{
			"name":        "grant_connection_privilege",
			"description": "Grant a group access to a connection at a specified level (read or read_write)",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"groupId": map[string]interface{}{
						"type":        "number",
						"description": "ID of the group to grant access to",
					},
					"connectionId": map[string]interface{}{
						"type":        "number",
						"description": "ID of the connection",
					},
					"accessLevel": map[string]interface{}{
						"type":        "string",
						"description": "Access level: 'read' or 'read_write'",
						"enum":        []string{"read", "read_write"},
					},
				},
				"required": []string{"groupId", "connectionId", "accessLevel"},
			},
		},
		{
			"name":        "revoke_connection_privilege",
			"description": "Revoke a group's access to a connection",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"groupId": map[string]interface{}{
						"type":        "number",
						"description": "ID of the group",
					},
					"connectionId": map[string]interface{}{
						"type":        "number",
						"description": "ID of the connection",
					},
				},
				"required": []string{"groupId", "connectionId"},
			},
		},
		{
			"name":        "list_connection_privileges",
			"description": "List all group privileges for a connection",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"connectionId": map[string]interface{}{
						"type":        "number",
						"description": "ID of the connection",
					},
				},
				"required": []string{"connectionId"},
			},
		},
		{
			"name":        "list_mcp_privilege_identifiers",
			"description": "List all registered MCP privilege identifiers (tools, resources, prompts)",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
				"required":   []string{},
			},
		},
		{
			"name":        "grant_mcp_privilege",
			"description": "Grant a group access to an MCP tool, resource, or prompt",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"groupId": map[string]interface{}{
						"type":        "number",
						"description": "ID of the group",
					},
					"privilegeIdentifier": map[string]interface{}{
						"type":        "string",
						"description": "Identifier of the MCP item (e.g., 'create_user', 'list_connections')",
					},
				},
				"required": []string{"groupId", "privilegeIdentifier"},
			},
		},
		{
			"name":        "revoke_mcp_privilege",
			"description": "Revoke a group's access to an MCP tool, resource, or prompt",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"groupId": map[string]interface{}{
						"type":        "number",
						"description": "ID of the group",
					},
					"privilegeIdentifier": map[string]interface{}{
						"type":        "string",
						"description": "Identifier of the MCP item",
					},
				},
				"required": []string{"groupId", "privilegeIdentifier"},
			},
		},
		{
			"name":        "list_group_mcp_privileges",
			"description": "List all MCP privileges granted to a group",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"groupId": map[string]interface{}{
						"type":        "number",
						"description": "ID of the group",
					},
				},
				"required": []string{"groupId"},
			},
		},
		{
			"name":        "set_token_connection_scope",
			"description": "Limit a token's access to specific connections",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"tokenId": map[string]interface{}{
						"type":        "number",
						"description": "ID of the token",
					},
					"tokenType": map[string]interface{}{
						"type":        "string",
						"description": "Type of token: 'user' or 'service'",
						"enum":        []string{"user", "service"},
					},
					"connectionIds": map[string]interface{}{
						"type":        "array",
						"description": "Array of connection IDs to limit access to",
						"items": map[string]interface{}{
							"type": "number",
						},
					},
				},
				"required": []string{"tokenId", "tokenType", "connectionIds"},
			},
		},
		{
			"name":        "set_token_mcp_scope",
			"description": "Limit a token's access to specific MCP tools, resources, or prompts",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"tokenId": map[string]interface{}{
						"type":        "number",
						"description": "ID of the token",
					},
					"tokenType": map[string]interface{}{
						"type":        "string",
						"description": "Type of token: 'user' or 'service'",
						"enum":        []string{"user", "service"},
					},
					"privilegeIdentifiers": map[string]interface{}{
						"type":        "array",
						"description": "Array of MCP privilege identifiers",
						"items": map[string]interface{}{
							"type": "string",
						},
					},
				},
				"required": []string{"tokenId", "tokenType", "privilegeIdentifiers"},
			},
		},
		{
			"name":        "get_token_scope",
			"description": "Get the scope restrictions for a token",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"tokenId": map[string]interface{}{
						"type":        "number",
						"description": "ID of the token",
					},
					"tokenType": map[string]interface{}{
						"type":        "string",
						"description": "Type of token: 'user' or 'service'",
						"enum":        []string{"user", "service"},
					},
				},
				"required": []string{"tokenId", "tokenType"},
			},
		},
		{
			"name":        "clear_token_scope",
			"description": "Remove all scope restrictions for a token",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"tokenId": map[string]interface{}{
						"type":        "number",
						"description": "ID of the token",
					},
					"tokenType": map[string]interface{}{
						"type":        "string",
						"description": "Type of token: 'user' or 'service'",
						"enum":        []string{"user", "service"},
					},
				},
				"required": []string{"tokenId", "tokenType"},
			},
		},
	}

	result := map[string]interface{}{
		"tools": tools,
	}
	logger.Info("Listed tools")
	return NewResponse(req.ID, result), nil
}

// handleListPrompts processes the prompts/list request
func (h *Handler) handleListPrompts(req Request) (*Response, error) {
	// Return empty list for now - prompts will be added later
	result := map[string]interface{}{
		"prompts": []interface{}{},
	}
	logger.Info("Listed prompts")
	return NewResponse(req.ID, result), nil
}

// IsInitialized returns whether the handler has been initialized
func (h *Handler) IsInitialized() bool {
	return h.initialized
}

// handleCallTool processes the tools/call request
func (h *Handler) handleCallTool(req Request) (*Response, error) {
	var params struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		logger.Errorf("Failed to unmarshal tool call params: %v", err)
		return NewErrorResponse(req.ID, InvalidParams, "Invalid parameters",
			err.Error()), nil
	}

	logger.Infof("Calling tool: %s", params.Name)

	// Route to appropriate tool handler
	var result interface{}
	var err error

	switch params.Name {
	case "authenticate_user":
		result, err = h.handleAuthenticateUser(params.Arguments)
	case "create_user":
		result, err = h.handleCreateUser(params.Arguments)
	case "update_user":
		result, err = h.handleUpdateUser(params.Arguments)
	case "delete_user":
		result, err = h.handleDeleteUser(params.Arguments)
	case "create_service_token":
		result, err = h.handleCreateServiceToken(params.Arguments)
	case "update_service_token":
		result, err = h.handleUpdateServiceToken(params.Arguments)
	case "delete_service_token":
		result, err = h.handleDeleteServiceToken(params.Arguments)
	case "create_user_token":
		result, err = h.handleCreateUserToken(params.Arguments)
	case "list_user_tokens":
		result, err = h.handleListUserTokens(params.Arguments)
	case "delete_user_token":
		result, err = h.handleDeleteUserToken(params.Arguments)
	case "create_user_group":
		result, err = h.handleCreateUserGroup(params.Arguments)
	case "update_user_group":
		result, err = h.handleUpdateUserGroup(params.Arguments)
	case "delete_user_group":
		result, err = h.handleDeleteUserGroup(params.Arguments)
	case "list_user_groups":
		result, err = h.handleListUserGroups(params.Arguments)
	case "add_group_member":
		result, err = h.handleAddGroupMember(params.Arguments)
	case "remove_group_member":
		result, err = h.handleRemoveGroupMember(params.Arguments)
	case "list_group_members":
		result, err = h.handleListGroupMembers(params.Arguments)
	case "list_user_group_memberships":
		result, err = h.handleListUserGroupMemberships(params.Arguments)
	case "grant_connection_privilege":
		result, err = h.handleGrantConnectionPrivilege(params.Arguments)
	case "revoke_connection_privilege":
		result, err = h.handleRevokeConnectionPrivilege(params.Arguments)
	case "list_connection_privileges":
		result, err = h.handleListConnectionPrivileges(params.Arguments)
	case "list_mcp_privilege_identifiers":
		result, err = h.handleListMCPPrivilegeIdentifiers(params.Arguments)
	case "grant_mcp_privilege":
		result, err = h.handleGrantMCPPrivilege(params.Arguments)
	case "revoke_mcp_privilege":
		result, err = h.handleRevokeMCPPrivilege(params.Arguments)
	case "list_group_mcp_privileges":
		result, err = h.handleListGroupMCPPrivileges(params.Arguments)
	case "set_token_connection_scope":
		result, err = h.handleSetTokenConnectionScope(params.Arguments)
	case "set_token_mcp_scope":
		result, err = h.handleSetTokenMCPScope(params.Arguments)
	case "get_token_scope":
		result, err = h.handleGetTokenScope(params.Arguments)
	case "clear_token_scope":
		result, err = h.handleClearTokenScope(params.Arguments)
	default:
		logger.Errorf("Unknown tool: %s", params.Name)
		return NewErrorResponse(req.ID, MethodNotFound, "Tool not found",
			nil), nil
	}

	if err != nil {
		logger.Errorf("Tool execution failed: %v", err)
		return NewErrorResponse(req.ID, InternalError, "Tool execution failed",
			err.Error()), nil
	}

	logger.Infof("Tool %s executed successfully", params.Name)
	return NewResponse(req.ID, result), nil
}

// handleCreateUser executes the create_user tool
func (h *Handler) handleCreateUser(args map[string]interface{}) (interface{},
	error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("permission denied: superuser privileges required")
	}

	// Superusers bypass all privilege checks
	if !h.userInfo.IsSuperuser {
		// If no database pool (test mode), require superuser
		if h.dbPool == nil {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// Service tokens must be superusers for this operation
		if h.userInfo.IsServiceToken {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// For non-superuser users, check privileges via group membership
		ctx := context.Background()

		// Get user ID from username
		var userID int
		err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1",
			h.userInfo.Username).Scan(&userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}

		// Check if user has privilege to create users
		canAccess, err := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "create_user")
		if err != nil {
			return nil, fmt.Errorf("failed to check privileges: %w", err)
		}
		if !canAccess {
			return nil, fmt.Errorf("permission denied: insufficient privileges")
		}
	}

	username, _ := args["username"].(string)   //nolint:errcheck // Optional argument, empty string is acceptable default
	email, _ := args["email"].(string)         //nolint:errcheck // Optional argument, empty string is acceptable default
	fullName, _ := args["fullName"].(string)   //nolint:errcheck // Optional argument, empty string is acceptable default
	password, _ := args["password"].(string)   //nolint:errcheck // Optional argument, empty string is acceptable default
	isSuperuser, _ := args["isSuperuser"].(bool) //nolint:errcheck // Optional argument, false is acceptable default

	var passwordExpiry *time.Time
	if expiryStr, ok := args["passwordExpiry"].(string); ok && expiryStr != "" {
		expiry, err := time.Parse("2006-01-02", expiryStr)
		if err != nil {
			return nil, fmt.Errorf("invalid password expiry date format: %w",
				err)
		}
		passwordExpiry = &expiry
	}

	message, err := usermgmt.CreateUserNonInteractive(h.dbPool, username, email,
		fullName, password, isSuperuser, passwordExpiry)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": message,
			},
		},
	}, nil
}

// handleUpdateUser executes the update_user tool
func (h *Handler) handleUpdateUser(args map[string]interface{}) (interface{},
	error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("permission denied: superuser privileges required")
	}

	// Superusers bypass all privilege checks
	if !h.userInfo.IsSuperuser {
		// If no database pool (test mode), require superuser
		if h.dbPool == nil {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// Service tokens must be superusers for this operation
		if h.userInfo.IsServiceToken {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// For non-superuser users, check privileges via group membership
		ctx := context.Background()

		// Get user ID from username
		var userID int
		err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1",
			h.userInfo.Username).Scan(&userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}

		// Check if user has privilege to update users
		canAccess, err := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "update_user")
		if err != nil {
			return nil, fmt.Errorf("failed to check privileges: %w", err)
		}
		if !canAccess {
			return nil, fmt.Errorf("permission denied: insufficient privileges")
		}
	}

	username, _ := args["username"].(string) //nolint:errcheck // Required argument, empty string handled by validation

	var email, fullName, password *string
	var isSuperuser *bool
	var passwordExpiry *time.Time
	clearPasswordExpiry := false

	if val, ok := args["email"].(string); ok {
		email = &val
	}
	if val, ok := args["fullName"].(string); ok {
		fullName = &val
	}
	if val, ok := args["password"].(string); ok {
		password = &val
	}
	if val, ok := args["isSuperuser"].(bool); ok {
		isSuperuser = &val
	}
	if val, ok := args["clearPasswordExpiry"].(bool); ok {
		clearPasswordExpiry = val
	}
	if expiryStr, ok := args["passwordExpiry"].(string); ok && expiryStr != "" {
		expiry, err := time.Parse("2006-01-02", expiryStr)
		if err != nil {
			return nil, fmt.Errorf("invalid password expiry date format: %w",
				err)
		}
		passwordExpiry = &expiry
	}

	message, err := usermgmt.UpdateUserNonInteractive(h.dbPool, username, email,
		fullName, password, isSuperuser, passwordExpiry, clearPasswordExpiry)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": message,
			},
		},
	}, nil
}

// handleDeleteUser executes the delete_user tool
func (h *Handler) handleDeleteUser(args map[string]interface{}) (interface{},
	error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("permission denied: superuser privileges required")
	}

	// Superusers bypass all privilege checks
	if !h.userInfo.IsSuperuser {
		// If no database pool (test mode), require superuser
		if h.dbPool == nil {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// Service tokens must be superusers for this operation
		if h.userInfo.IsServiceToken {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// For non-superuser users, check privileges via group membership
		ctx := context.Background()

		// Get user ID from username
		var userID int
		err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1",
			h.userInfo.Username).Scan(&userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}

		// Check if user has privilege to delete users
		canAccess, err := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "delete_user")
		if err != nil {
			return nil, fmt.Errorf("failed to check privileges: %w", err)
		}
		if !canAccess {
			return nil, fmt.Errorf("permission denied: insufficient privileges")
		}
	}

	username, _ := args["username"].(string) //nolint:errcheck // Required argument, empty string handled by validation

	message, err := usermgmt.DeleteUserNonInteractive(h.dbPool, username)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": message,
			},
		},
	}, nil
}

// handleCreateServiceToken executes the create_service_token tool
func (h *Handler) handleCreateServiceToken(args map[string]interface{}) (interface{},
	error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("permission denied: superuser privileges required")
	}

	// Superusers bypass all privilege checks
	if !h.userInfo.IsSuperuser {
		// If no database pool (test mode), require superuser
		if h.dbPool == nil {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// Service tokens must be superusers for this operation
		if h.userInfo.IsServiceToken {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// For non-superuser users, check privileges via group membership
		ctx := context.Background()

		// Get user ID from username
		var userID int
		err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1",
			h.userInfo.Username).Scan(&userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}

		// Check if user has privilege to create service tokens
		canAccess, err := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "create_service_token")
		if err != nil {
			return nil, fmt.Errorf("failed to check privileges: %w", err)
		}
		if !canAccess {
			return nil, fmt.Errorf("permission denied: insufficient privileges")
		}
	}

	name, _ := args["name"].(string)           //nolint:errcheck // Required argument, empty string handled by validation
	isSuperuser, _ := args["isSuperuser"].(bool) //nolint:errcheck // Optional argument, false is acceptable default

	var note *string
	var expiresAt *time.Time

	if val, ok := args["note"].(string); ok && val != "" {
		note = &val
	}
	if expiryStr, ok := args["expiresAt"].(string); ok && expiryStr != "" {
		expiry, err := time.Parse("2006-01-02", expiryStr)
		if err != nil {
			return nil, fmt.Errorf("invalid expiry date format: %w", err)
		}
		expiresAt = &expiry
	}

	message, token, err := usermgmt.CreateServiceTokenNonInteractive(h.dbPool,
		name, isSuperuser, note, expiresAt)
	if err != nil {
		return nil, err
	}

	fullMessage := fmt.Sprintf("%s\nToken: %s\nIMPORTANT: Save this token "+
		"now. You won't be able to see it again.", message, token)

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fullMessage,
			},
		},
	}, nil
}

// handleUpdateServiceToken executes the update_service_token tool
func (h *Handler) handleUpdateServiceToken(args map[string]interface{}) (interface{},
	error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("permission denied: superuser privileges required")
	}

	// Superusers bypass all privilege checks
	if !h.userInfo.IsSuperuser {
		// If no database pool (test mode), require superuser
		if h.dbPool == nil {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// Service tokens must be superusers for this operation
		if h.userInfo.IsServiceToken {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// For non-superuser users, check privileges via group membership
		ctx := context.Background()

		// Get user ID from username
		var userID int
		err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1",
			h.userInfo.Username).Scan(&userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}

		// Check if user has privilege to update service tokens
		canAccess, err := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "update_service_token")
		if err != nil {
			return nil, fmt.Errorf("failed to check privileges: %w", err)
		}
		if !canAccess {
			return nil, fmt.Errorf("permission denied: insufficient privileges")
		}
	}

	name, _ := args["name"].(string) //nolint:errcheck // Required argument, empty string handled by validation

	var isSuperuser *bool
	var note *string
	var expiresAt *time.Time
	clearNote := false
	clearExpiresAt := false

	if val, ok := args["isSuperuser"].(bool); ok {
		isSuperuser = &val
	}
	if val, ok := args["note"].(string); ok {
		note = &val
	}
	if val, ok := args["clearNote"].(bool); ok {
		clearNote = val
	}
	if val, ok := args["clearExpiresAt"].(bool); ok {
		clearExpiresAt = val
	}
	if expiryStr, ok := args["expiresAt"].(string); ok && expiryStr != "" {
		expiry, err := time.Parse("2006-01-02", expiryStr)
		if err != nil {
			return nil, fmt.Errorf("invalid expiry date format: %w", err)
		}
		expiresAt = &expiry
	}

	message, err := usermgmt.UpdateServiceTokenNonInteractive(h.dbPool, name,
		isSuperuser, note, expiresAt, clearNote, clearExpiresAt)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": message,
			},
		},
	}, nil
}

// handleDeleteServiceToken executes the delete_service_token tool
func (h *Handler) handleDeleteServiceToken(args map[string]interface{}) (interface{},
	error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("permission denied: superuser privileges required")
	}

	// Superusers bypass all privilege checks
	if !h.userInfo.IsSuperuser {
		// If no database pool (test mode), require superuser
		if h.dbPool == nil {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// Service tokens must be superusers for this operation
		if h.userInfo.IsServiceToken {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// For non-superuser users, check privileges via group membership
		ctx := context.Background()

		// Get user ID from username
		var userID int
		err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1",
			h.userInfo.Username).Scan(&userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}

		// Check if user has privilege to delete service tokens
		canAccess, err := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "delete_service_token")
		if err != nil {
			return nil, fmt.Errorf("failed to check privileges: %w", err)
		}
		if !canAccess {
			return nil, fmt.Errorf("permission denied: insufficient privileges")
		}
	}

	name, _ := args["name"].(string) //nolint:errcheck // Required argument, empty string handled by validation

	message, err := usermgmt.DeleteServiceTokenNonInteractive(h.dbPool, name)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": message,
			},
		},
	}, nil
}

// handleCreateUserToken executes the create_user_token tool
func (h *Handler) handleCreateUserToken(args map[string]interface{}) (interface{},
	error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("authentication required")
	}

	username, _ := args["username"].(string) //nolint:errcheck // Required argument, empty string handled by validation
	lifetimeDays := 0
	if val, ok := args["lifetimeDays"].(float64); ok {
		lifetimeDays = int(val)
	}

	// Check authorization: users can only create tokens for themselves unless they're superuser
	if h.userInfo.Username != username && !h.userInfo.IsSuperuser {
		return nil, fmt.Errorf("permission denied: can only create tokens for your own account")
	}

	// If no database pool (test mode), cannot proceed
	if h.dbPool == nil {
		return nil, fmt.Errorf("database connection required")
	}

	var name *string
	var note *string

	if val, ok := args["name"].(string); ok && val != "" {
		name = &val
	}
	if val, ok := args["note"].(string); ok && val != "" {
		note = &val
	}

	// Get max lifetime from config
	maxLifetimeDays := 0
	if h.config != nil {
		maxLifetimeDays = h.config.GetMaxUserTokenLifetimeDays()
	}

	message, token, err := usermgmt.CreateUserTokenNonInteractive(h.dbPool,
		username, name, lifetimeDays, maxLifetimeDays, note)
	if err != nil {
		return nil, err
	}

	fullMessage := fmt.Sprintf("%s\nToken: %s\nIMPORTANT: Save this token "+
		"now. You won't be able to see it again.", message, token)

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fullMessage,
			},
		},
	}, nil
}

// handleListUserTokens executes the list_user_tokens tool
func (h *Handler) handleListUserTokens(args map[string]interface{}) (interface{},
	error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("authentication required")
	}

	username, _ := args["username"].(string) //nolint:errcheck // Required argument, empty string handled by validation

	// Check authorization: users can only list their own tokens unless they're superuser
	if h.userInfo.Username != username && !h.userInfo.IsSuperuser {
		return nil, fmt.Errorf("permission denied: can only list your own tokens")
	}

	// If no database pool (test mode), cannot proceed
	if h.dbPool == nil {
		return nil, fmt.Errorf("database connection required")
	}

	tokens, err := usermgmt.ListUserTokens(h.dbPool, username)
	if err != nil {
		return nil, err
	}

	// Format tokens as JSON for display
	tokensJSON, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to format tokens: %w", err)
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("User tokens for '%s':\n%s", username,
					string(tokensJSON)),
			},
		},
	}, nil
}

// handleDeleteUserToken executes the delete_user_token tool
func (h *Handler) handleDeleteUserToken(args map[string]interface{}) (interface{},
	error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("authentication required")
	}

	username, _ := args["username"].(string) //nolint:errcheck // Required argument, empty string handled by validation
	tokenID := 0
	if val, ok := args["tokenId"].(float64); ok {
		tokenID = int(val)
	}

	// Check authorization: users can only delete their own tokens unless they're superuser
	if h.userInfo.Username != username && !h.userInfo.IsSuperuser {
		return nil, fmt.Errorf("permission denied: can only delete your own tokens")
	}

	// If no database pool (test mode), cannot proceed
	if h.dbPool == nil {
		return nil, fmt.Errorf("database connection required")
	}

	message, err := usermgmt.DeleteUserTokenNonInteractive(h.dbPool, username,
		tokenID)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": message,
			},
		},
	}, nil
}

// UserInfo contains information about an authenticated user or service token
type UserInfo struct {
	IsAuthenticated bool
	IsSuperuser     bool
	Username        string // Empty for service tokens
	IsServiceToken  bool
}

// validateToken validates a bearer token against service_tokens, user_tokens, and user_sessions
func (h *Handler) validateToken(token string) (*UserInfo, error) {
	if token == "" {
		return nil, nil
	}

	ctx := context.Background()

	// First, check service_tokens table
	tokenHash := usermgmt.HashPassword(token)
	var expiresAt interface{}
	var isSuperuser bool
	err := h.dbPool.QueryRow(ctx, `
		SELECT expires_at, is_superuser
		FROM service_tokens
		WHERE token_hash = $1
	`, tokenHash).Scan(&expiresAt, &isSuperuser)

	if err == nil {
		// Service token found, check if expired
		if expiresAt != nil {
			if expiry, ok := expiresAt.(time.Time); ok {
				if time.Now().After(expiry) {
					logger.Infof("Service token expired")
					return nil, nil
				}
			}
		}
		logger.Infof("Valid service token (superuser: %t)", isSuperuser)
		return &UserInfo{
			IsAuthenticated: true,
			IsSuperuser:     isSuperuser,
			Username:        "",
			IsServiceToken:  true,
		}, nil
	}

	// Service token not found, check user_tokens table
	var username string
	err = h.dbPool.QueryRow(ctx, `
		SELECT ua.username, ut.expires_at, ua.is_superuser
		FROM user_tokens ut
		JOIN user_accounts ua ON ut.user_id = ua.id
		WHERE ut.token_hash = $1
	`, tokenHash).Scan(&username, &expiresAt, &isSuperuser)

	if err == nil {
		// User token found, check if expired
		if expiresAt != nil {
			if expiry, ok := expiresAt.(time.Time); ok {
				if time.Now().After(expiry) {
					logger.Infof("User token expired for user '%s'", username)
					return nil, nil
				}
			}
		}
		logger.Infof("Valid user token for user '%s' (superuser: %t)", username,
			isSuperuser)
		return &UserInfo{
			IsAuthenticated: true,
			IsSuperuser:     isSuperuser,
			Username:        username,
			IsServiceToken:  false,
		}, nil
	}

	// User token not found, check user_sessions table
	err = h.dbPool.QueryRow(ctx, `
		SELECT us.username, us.expires_at, ua.is_superuser
		FROM user_sessions us
		JOIN user_accounts ua ON us.username = ua.username
		WHERE us.session_token = $1
	`, token).Scan(&username, &expiresAt, &isSuperuser)

	if err != nil {
		// Token not found in any table
		return nil, nil
	}

	// Session found, check if expired
	if expiry, ok := expiresAt.(time.Time); ok {
		if time.Now().After(expiry) {
			logger.Infof("Session token expired for user '%s'", username)
			return nil, nil
		}
	}

	// Update last_used_at timestamp
	_, err = h.dbPool.Exec(ctx, `
		UPDATE user_sessions
		SET last_used_at = $1
		WHERE session_token = $2
	`, time.Now(), token)
	if err != nil {
		logger.Errorf("Failed to update session last_used_at: %v", err)
		// Don't fail authentication just because we couldn't update timestamp
	}

	logger.Infof("Valid session token for user '%s' (superuser: %t)", username,
		isSuperuser)
	return &UserInfo{
		IsAuthenticated: true,
		IsSuperuser:     isSuperuser,
		Username:        username,
		IsServiceToken:  false,
	}, nil
}

// handleAuthenticateUser executes the authenticate_user tool
func (h *Handler) handleAuthenticateUser(args map[string]interface{}) (interface{},
	error) {
	username, _ := args["username"].(string) //nolint:errcheck // Required argument, empty string handled by validation
	password, _ := args["password"].(string) //nolint:errcheck // Required argument, empty string handled by validation

	ctx := context.Background()

	// Query user account
	var passwordHash string
	var passwordExpiry interface{}
	err := h.dbPool.QueryRow(ctx, `
		SELECT password_hash, password_expiry
		FROM user_accounts
		WHERE username = $1
	`, username).Scan(&passwordHash, &passwordExpiry)

	if err != nil {
		// User not found or other error
		logger.Errorf("Authentication failed for user '%s': %v", username, err)
		return nil, fmt.Errorf("authentication failed: invalid username or password")
	}

	// Verify password
	if usermgmt.HashPassword(password) != passwordHash {
		logger.Errorf("Authentication failed for user '%s': invalid password",
			username)
		return nil, fmt.Errorf("authentication failed: invalid username or password")
	}

	// Check if password has expired
	if passwordExpiry != nil {
		if expiry, ok := passwordExpiry.(time.Time); ok {
			if time.Now().After(expiry) {
				logger.Errorf("Authentication failed for user '%s': password expired",
					username)
				return nil, fmt.Errorf("authentication failed: password has expired")
			}
		}
	}

	// Generate session token
	sessionToken, err := usermgmt.GenerateToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate session token: %w", err)
	}

	// Calculate expiration time (24 hours from now)
	expiresAt := time.Now().Add(24 * time.Hour)

	// Insert into user_sessions table
	_, err = h.dbPool.Exec(ctx, `
		INSERT INTO user_sessions (session_token, username, expires_at)
		VALUES ($1, $2, $3)
	`, sessionToken, username, expiresAt)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	logger.Infof("User '%s' authenticated successfully", username)

	// Return session token
	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("Authentication successful. Session token: %s\nExpires at: %s",
					sessionToken, expiresAt.Format(time.RFC3339)),
			},
		},
	}, nil
}

// handleCreateUserGroup executes the create_user_group tool
func (h *Handler) handleCreateUserGroup(args map[string]interface{}) (interface{}, error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("permission denied: superuser privileges required")
	}

	// Superusers bypass all privilege checks
	if !h.userInfo.IsSuperuser {
		// If no database pool (test mode), require superuser
		if h.dbPool == nil {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// Service tokens must be superusers for this operation
		if h.userInfo.IsServiceToken {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// For non-superuser users, check privileges via group membership
		ctx := context.Background()

		// Get user ID from username
		var userID int
		err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1",
			h.userInfo.Username).Scan(&userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}

		// Check if user has privilege to create user groups
		canAccess, err := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "create_user_group")
		if err != nil {
			return nil, fmt.Errorf("failed to check privileges: %w", err)
		}
		if !canAccess {
			return nil, fmt.Errorf("permission denied: insufficient privileges")
		}
	}

	name, _ := args["name"].(string)               //nolint:errcheck // Optional argument, empty string is acceptable default
	description, _ := args["description"].(string) //nolint:errcheck // Optional argument, empty string is acceptable default

	ctx := context.Background()
	groupID, err := groupmgmt.CreateUserGroup(ctx, h.dbPool, name, description)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("User group '%s' created successfully with ID: %d", name, groupID),
			},
		},
	}, nil
}

// handleUpdateUserGroup executes the update_user_group tool
func (h *Handler) handleUpdateUserGroup(args map[string]interface{}) (interface{}, error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("permission denied: superuser privileges required")
	}

	// Superusers bypass all privilege checks
	if !h.userInfo.IsSuperuser {
		// If no database pool (test mode), require superuser
		if h.dbPool == nil {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// Service tokens must be superusers for this operation
		if h.userInfo.IsServiceToken {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// For non-superuser users, check privileges via group membership
		ctx := context.Background()

		// Get user ID from username
		var userID int
		err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1",
			h.userInfo.Username).Scan(&userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}

		// Check if user has privilege to update user groups
		canAccess, err := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "update_user_group")
		if err != nil {
			return nil, fmt.Errorf("failed to check privileges: %w", err)
		}
		if !canAccess {
			return nil, fmt.Errorf("permission denied: insufficient privileges")
		}
	}

	groupIDFloat, ok := args["groupId"].(float64)
	if !ok {
		return nil, fmt.Errorf("invalid groupId parameter")
	}
	groupID := int(groupIDFloat)

	name, _ := args["name"].(string)               //nolint:errcheck // Optional argument, empty string is acceptable default
	description, _ := args["description"].(string) //nolint:errcheck // Optional argument, empty string is acceptable default

	ctx := context.Background()
	err := groupmgmt.UpdateUserGroup(ctx, h.dbPool, groupID, name, description)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("User group %d updated successfully", groupID),
			},
		},
	}, nil
}

// handleDeleteUserGroup executes the delete_user_group tool
func (h *Handler) handleDeleteUserGroup(args map[string]interface{}) (interface{}, error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("permission denied: superuser privileges required")
	}

	// Superusers bypass all privilege checks
	if !h.userInfo.IsSuperuser {
		// If no database pool (test mode), require superuser
		if h.dbPool == nil {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// Service tokens must be superusers for this operation
		if h.userInfo.IsServiceToken {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// For non-superuser users, check privileges via group membership
		ctx := context.Background()

		// Get user ID from username
		var userID int
		err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1",
			h.userInfo.Username).Scan(&userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}

		// Check if user has privilege to delete user groups
		canAccess, err := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "delete_user_group")
		if err != nil {
			return nil, fmt.Errorf("failed to check privileges: %w", err)
		}
		if !canAccess {
			return nil, fmt.Errorf("permission denied: insufficient privileges")
		}
	}

	groupIDFloat, ok := args["groupId"].(float64)
	if !ok {
		return nil, fmt.Errorf("invalid groupId parameter")
	}
	groupID := int(groupIDFloat)

	ctx := context.Background()
	err := groupmgmt.DeleteUserGroup(ctx, h.dbPool, groupID)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("User group %d deleted successfully", groupID),
			},
		},
	}, nil
}

// handleListUserGroups executes the list_user_groups tool
func (h *Handler) handleListUserGroups(args map[string]interface{}) (interface{}, error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("permission denied: superuser privileges required")
	}

	// Superusers bypass all privilege checks
	if !h.userInfo.IsSuperuser {
		// If no database pool (test mode), require superuser
		if h.dbPool == nil {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// Service tokens must be superusers for this operation
		if h.userInfo.IsServiceToken {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// For non-superuser users, check privileges via group membership
		ctx := context.Background()

		// Get user ID from username
		var userID int
		err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1",
			h.userInfo.Username).Scan(&userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}

		// Check if user has privilege to list user groups
		canAccess, err := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "list_user_groups")
		if err != nil {
			return nil, fmt.Errorf("failed to check privileges: %w", err)
		}
		if !canAccess {
			return nil, fmt.Errorf("permission denied: insufficient privileges")
		}
	}

	ctx := context.Background()
	groups, err := groupmgmt.ListUserGroups(ctx, h.dbPool)
	if err != nil {
		return nil, err
	}

	// Format groups as JSON string
	groupsJSON, err := json.Marshal(groups)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal groups: %w", err)
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("User groups:\n%s", string(groupsJSON)),
			},
		},
	}, nil
}

// handleAddGroupMember executes the add_group_member tool
func (h *Handler) handleAddGroupMember(args map[string]interface{}) (interface{}, error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("permission denied: superuser privileges required")
	}

	// Superusers bypass all privilege checks
	if !h.userInfo.IsSuperuser {
		// If no database pool (test mode), require superuser
		if h.dbPool == nil {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// Service tokens must be superusers for this operation
		if h.userInfo.IsServiceToken {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// For non-superuser users, check privileges via group membership
		ctx := context.Background()

		// Get user ID from username
		var userID int
		err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1",
			h.userInfo.Username).Scan(&userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}

		// Check if user has privilege to add group members
		canAccess, err := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "add_group_member")
		if err != nil {
			return nil, fmt.Errorf("failed to check privileges: %w", err)
		}
		if !canAccess {
			return nil, fmt.Errorf("permission denied: insufficient privileges")
		}
	}

	parentGroupIDFloat, ok := args["parentGroupId"].(float64)
	if !ok {
		return nil, fmt.Errorf("invalid parentGroupId parameter")
	}
	parentGroupID := int(parentGroupIDFloat)

	var memberUserID *int
	var memberGroupID *int

	if userIDFloat, ok := args["memberUserId"].(float64); ok {
		uid := int(userIDFloat)
		memberUserID = &uid
	}

	if groupIDFloat, ok := args["memberGroupId"].(float64); ok {
		gid := int(groupIDFloat)
		memberGroupID = &gid
	}

	ctx := context.Background()
	err := groupmgmt.AddGroupMember(ctx, h.dbPool, parentGroupID, memberUserID, memberGroupID)
	if err != nil {
		return nil, err
	}

	memberType := "user"
	memberID := 0
	if memberUserID != nil {
		memberID = *memberUserID
	} else {
		memberType = "group"
		memberID = *memberGroupID
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("Added %s %d as member of group %d", memberType, memberID, parentGroupID),
			},
		},
	}, nil
}

// handleRemoveGroupMember executes the remove_group_member tool
func (h *Handler) handleRemoveGroupMember(args map[string]interface{}) (interface{}, error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("permission denied: superuser privileges required")
	}

	// Superusers bypass all privilege checks
	if !h.userInfo.IsSuperuser {
		// If no database pool (test mode), require superuser
		if h.dbPool == nil {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// Service tokens must be superusers for this operation
		if h.userInfo.IsServiceToken {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// For non-superuser users, check privileges via group membership
		ctx := context.Background()

		// Get user ID from username
		var userID int
		err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1",
			h.userInfo.Username).Scan(&userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}

		// Check if user has privilege to remove group members
		canAccess, err := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "remove_group_member")
		if err != nil {
			return nil, fmt.Errorf("failed to check privileges: %w", err)
		}
		if !canAccess {
			return nil, fmt.Errorf("permission denied: insufficient privileges")
		}
	}

	parentGroupIDFloat, ok := args["parentGroupId"].(float64)
	if !ok {
		return nil, fmt.Errorf("invalid parentGroupId parameter")
	}
	parentGroupID := int(parentGroupIDFloat)

	var memberUserID *int
	var memberGroupID *int

	if userIDFloat, ok := args["memberUserId"].(float64); ok {
		uid := int(userIDFloat)
		memberUserID = &uid
	}

	if groupIDFloat, ok := args["memberGroupId"].(float64); ok {
		gid := int(groupIDFloat)
		memberGroupID = &gid
	}

	ctx := context.Background()
	err := groupmgmt.RemoveGroupMember(ctx, h.dbPool, parentGroupID, memberUserID, memberGroupID)
	if err != nil {
		return nil, err
	}

	memberType := "user"
	memberID := 0
	if memberUserID != nil {
		memberID = *memberUserID
	} else {
		memberType = "group"
		memberID = *memberGroupID
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("Removed %s %d from group %d", memberType, memberID, parentGroupID),
			},
		},
	}, nil
}

// handleListGroupMembers executes the list_group_members tool
func (h *Handler) handleListGroupMembers(args map[string]interface{}) (interface{}, error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("permission denied: superuser privileges required")
	}

	// Superusers bypass all privilege checks
	if !h.userInfo.IsSuperuser {
		// If no database pool (test mode), require superuser
		if h.dbPool == nil {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// Service tokens must be superusers for this operation
		if h.userInfo.IsServiceToken {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// For non-superuser users, check privileges via group membership
		ctx := context.Background()

		// Get user ID from username
		var userID int
		err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1",
			h.userInfo.Username).Scan(&userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}

		// Check if user has privilege to list group members
		canAccess, err := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "list_group_members")
		if err != nil {
			return nil, fmt.Errorf("failed to check privileges: %w", err)
		}
		if !canAccess {
			return nil, fmt.Errorf("permission denied: insufficient privileges")
		}
	}

	groupIDFloat, ok := args["groupId"].(float64)
	if !ok {
		return nil, fmt.Errorf("invalid groupId parameter")
	}
	groupID := int(groupIDFloat)

	ctx := context.Background()
	members, err := groupmgmt.ListGroupMembers(ctx, h.dbPool, groupID)
	if err != nil {
		return nil, err
	}

	// Format members as JSON string
	membersJSON, err := json.Marshal(members)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal members: %w", err)
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("Members of group %d:\n%s", groupID, string(membersJSON)),
			},
		},
	}, nil
}

// handleListUserGroupMemberships executes the list_user_group_memberships tool
func (h *Handler) handleListUserGroupMemberships(args map[string]interface{}) (interface{}, error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("permission denied: superuser privileges required")
	}

	// Superusers bypass all privilege checks
	if !h.userInfo.IsSuperuser {
		// If no database pool (test mode), require superuser
		if h.dbPool == nil {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// Service tokens must be superusers for this operation
		if h.userInfo.IsServiceToken {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// For non-superuser users, check privileges via group membership
		ctx := context.Background()

		// Get user ID from username
		var userID int
		err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1",
			h.userInfo.Username).Scan(&userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}

		// Check if user has privilege to list user group memberships
		canAccess, err := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "list_user_group_memberships")
		if err != nil {
			return nil, fmt.Errorf("failed to check privileges: %w", err)
		}
		if !canAccess {
			return nil, fmt.Errorf("permission denied: insufficient privileges")
		}
	}

	username, ok := args["username"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid username parameter")
	}

	// Get user ID from username
	ctx := context.Background()
	var userID int
	err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1", username).Scan(&userID)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	groups, err := groupmgmt.ListUserGroupMemberships(ctx, h.dbPool, userID)
	if err != nil {
		return nil, err
	}

	// Format groups as JSON string
	groupsJSON, err := json.Marshal(groups)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal groups: %w", err)
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("Groups for user '%s' (direct and indirect):\n%s", username, string(groupsJSON)),
			},
		},
	}, nil
}

// handleGrantConnectionPrivilege executes the grant_connection_privilege tool
func (h *Handler) handleGrantConnectionPrivilege(args map[string]interface{}) (interface{}, error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("permission denied: superuser privileges required")
	}

	// Superusers bypass all privilege checks
	if !h.userInfo.IsSuperuser {
		// If no database pool (test mode), require superuser
		if h.dbPool == nil {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// Service tokens must be superusers for this operation
		if h.userInfo.IsServiceToken {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// For non-superuser users, check privileges via group membership
		ctx := context.Background()

		// Get user ID from username
		var userID int
		err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1",
			h.userInfo.Username).Scan(&userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}

		// Check if user has privilege to grant connection privileges
		canAccess, err := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "grant_connection_privilege")
		if err != nil {
			return nil, fmt.Errorf("failed to check privileges: %w", err)
		}
		if !canAccess {
			return nil, fmt.Errorf("permission denied: insufficient privileges")
		}
	}

	groupID := int(args["groupId"].(float64))       //nolint:errcheck // Type assertion, JSON numbers are float64
	connectionID := int(args["connectionId"].(float64)) //nolint:errcheck // Type assertion
	accessLevel, _ := args["accessLevel"].(string)  //nolint:errcheck // Optional argument

	ctx := context.Background()
	err := groupmgmt.GrantConnectionPrivilege(ctx, h.dbPool, groupID, connectionID, accessLevel)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("Connection privilege granted: group %d can access connection %d with level '%s'", groupID, connectionID, accessLevel),
			},
		},
	}, nil
}

// handleRevokeConnectionPrivilege executes the revoke_connection_privilege tool
func (h *Handler) handleRevokeConnectionPrivilege(args map[string]interface{}) (interface{}, error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("permission denied: superuser privileges required")
	}

	// Superusers bypass all privilege checks
	if !h.userInfo.IsSuperuser {
		// If no database pool (test mode), require superuser
		if h.dbPool == nil {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// Service tokens must be superusers for this operation
		if h.userInfo.IsServiceToken {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// For non-superuser users, check privileges via group membership
		ctx := context.Background()

		// Get user ID from username
		var userID int
		err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1",
			h.userInfo.Username).Scan(&userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}

		// Check if user has privilege to revoke connection privileges
		canAccess, err := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "revoke_connection_privilege")
		if err != nil {
			return nil, fmt.Errorf("failed to check privileges: %w", err)
		}
		if !canAccess {
			return nil, fmt.Errorf("permission denied: insufficient privileges")
		}
	}

	groupID := int(args["groupId"].(float64))       //nolint:errcheck // Type assertion
	connectionID := int(args["connectionId"].(float64)) //nolint:errcheck // Type assertion

	ctx := context.Background()
	err := groupmgmt.RevokeConnectionPrivilege(ctx, h.dbPool, groupID, connectionID)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("Connection privilege revoked: group %d no longer has access to connection %d", groupID, connectionID),
			},
		},
	}, nil
}

// handleListConnectionPrivileges executes the list_connection_privileges tool
func (h *Handler) handleListConnectionPrivileges(args map[string]interface{}) (interface{}, error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("permission denied: superuser privileges required")
	}

	// Superusers bypass all privilege checks
	if !h.userInfo.IsSuperuser {
		// If no database pool (test mode), require superuser
		if h.dbPool == nil {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// Service tokens must be superusers for this operation
		if h.userInfo.IsServiceToken {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// For non-superuser users, check privileges via group membership
		ctx := context.Background()

		// Get user ID from username
		var userID int
		err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1",
			h.userInfo.Username).Scan(&userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}

		// Check if user has privilege to list connection privileges
		canAccess, err := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "list_connection_privileges")
		if err != nil {
			return nil, fmt.Errorf("failed to check privileges: %w", err)
		}
		if !canAccess {
			return nil, fmt.Errorf("permission denied: insufficient privileges")
		}
	}

	connectionID := int(args["connectionId"].(float64)) //nolint:errcheck // Type assertion

	ctx := context.Background()
	privileges, err := groupmgmt.ListConnectionPrivileges(ctx, h.dbPool, connectionID)
	if err != nil {
		return nil, err
	}

	// Format privileges as JSON string
	privilegesJSON, err := json.Marshal(privileges)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal privileges: %w", err)
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("Connection privileges for connection %d:\n%s", connectionID, string(privilegesJSON)),
			},
		},
	}, nil
}

// handleListMCPPrivilegeIdentifiers executes the list_mcp_privilege_identifiers tool
func (h *Handler) handleListMCPPrivilegeIdentifiers(args map[string]interface{}) (interface{}, error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("permission denied: superuser privileges required")
	}

	// Superusers bypass all privilege checks
	if !h.userInfo.IsSuperuser {
		// If no database pool (test mode), require superuser
		if h.dbPool == nil {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// Service tokens must be superusers for this operation
		if h.userInfo.IsServiceToken {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// For non-superuser users, check privileges via group membership
		ctx := context.Background()

		// Get user ID from username
		var userID int
		err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1",
			h.userInfo.Username).Scan(&userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}

		// Check if user has privilege to list MCP privilege identifiers
		canAccess, err := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "list_mcp_privilege_identifiers")
		if err != nil {
			return nil, fmt.Errorf("failed to check privileges: %w", err)
		}
		if !canAccess {
			return nil, fmt.Errorf("permission denied: insufficient privileges")
		}
	}

	ctx := context.Background()
	identifiers, err := groupmgmt.ListMCPPrivilegeIdentifiers(ctx, h.dbPool)
	if err != nil {
		return nil, err
	}

	// Format identifiers as JSON string
	identifiersJSON, err := json.Marshal(identifiers)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal identifiers: %w", err)
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("MCP privilege identifiers:\n%s", string(identifiersJSON)),
			},
		},
	}, nil
}

// handleGrantMCPPrivilege executes the grant_mcp_privilege tool
func (h *Handler) handleGrantMCPPrivilege(args map[string]interface{}) (interface{}, error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("permission denied: superuser privileges required")
	}

	// Superusers bypass all privilege checks
	if !h.userInfo.IsSuperuser {
		// If no database pool (test mode), require superuser
		if h.dbPool == nil {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// Service tokens must be superusers for this operation
		if h.userInfo.IsServiceToken {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// For non-superuser users, check privileges via group membership
		ctx := context.Background()

		// Get user ID from username
		var userID int
		err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1",
			h.userInfo.Username).Scan(&userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}

		// Check if user has privilege to grant MCP privileges
		canAccess, err := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "grant_mcp_privilege")
		if err != nil {
			return nil, fmt.Errorf("failed to check privileges: %w", err)
		}
		if !canAccess {
			return nil, fmt.Errorf("permission denied: insufficient privileges")
		}
	}

	groupID := int(args["groupId"].(float64))            //nolint:errcheck // Type assertion
	privilegeIdentifier, _ := args["privilegeIdentifier"].(string) //nolint:errcheck // Type assertion

	ctx := context.Background()
	err := groupmgmt.GrantMCPPrivilege(ctx, h.dbPool, groupID, privilegeIdentifier)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("MCP privilege granted: group %d can now access '%s'", groupID, privilegeIdentifier),
			},
		},
	}, nil
}

// handleRevokeMCPPrivilege executes the revoke_mcp_privilege tool
func (h *Handler) handleRevokeMCPPrivilege(args map[string]interface{}) (interface{}, error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("permission denied: superuser privileges required")
	}

	// Superusers bypass all privilege checks
	if !h.userInfo.IsSuperuser {
		// If no database pool (test mode), require superuser
		if h.dbPool == nil {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// Service tokens must be superusers for this operation
		if h.userInfo.IsServiceToken {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// For non-superuser users, check privileges via group membership
		ctx := context.Background()

		// Get user ID from username
		var userID int
		err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1",
			h.userInfo.Username).Scan(&userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}

		// Check if user has privilege to revoke MCP privileges
		canAccess, err := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "revoke_mcp_privilege")
		if err != nil {
			return nil, fmt.Errorf("failed to check privileges: %w", err)
		}
		if !canAccess {
			return nil, fmt.Errorf("permission denied: insufficient privileges")
		}
	}

	groupID := int(args["groupId"].(float64))            //nolint:errcheck // Type assertion
	privilegeIdentifier, _ := args["privilegeIdentifier"].(string) //nolint:errcheck // Type assertion

	ctx := context.Background()
	err := groupmgmt.RevokeMCPPrivilege(ctx, h.dbPool, groupID, privilegeIdentifier)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("MCP privilege revoked: group %d no longer has access to '%s'", groupID, privilegeIdentifier),
			},
		},
	}, nil
}

// handleListGroupMCPPrivileges executes the list_group_mcp_privileges tool
func (h *Handler) handleListGroupMCPPrivileges(args map[string]interface{}) (interface{}, error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("permission denied: superuser privileges required")
	}

	// Superusers bypass all privilege checks
	if !h.userInfo.IsSuperuser {
		// If no database pool (test mode), require superuser
		if h.dbPool == nil {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// Service tokens must be superusers for this operation
		if h.userInfo.IsServiceToken {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// For non-superuser users, check privileges via group membership
		ctx := context.Background()

		// Get user ID from username
		var userID int
		err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1",
			h.userInfo.Username).Scan(&userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}

		// Check if user has privilege to list group MCP privileges
		canAccess, err := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "list_group_mcp_privileges")
		if err != nil {
			return nil, fmt.Errorf("failed to check privileges: %w", err)
		}
		if !canAccess {
			return nil, fmt.Errorf("permission denied: insufficient privileges")
		}
	}

	groupID := int(args["groupId"].(float64)) //nolint:errcheck // Type assertion

	ctx := context.Background()
	privileges, err := groupmgmt.ListGroupMCPPrivileges(ctx, h.dbPool, groupID)
	if err != nil {
		return nil, err
	}

	// Format privileges as JSON string
	privilegesJSON, err := json.Marshal(privileges)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal privileges: %w", err)
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("MCP privileges for group %d:\n%s", groupID, string(privilegesJSON)),
			},
		},
	}, nil
}

// handleSetTokenConnectionScope executes the set_token_connection_scope tool
func (h *Handler) handleSetTokenConnectionScope(args map[string]interface{}) (interface{}, error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("permission denied: superuser privileges required")
	}

	// Superusers bypass all privilege checks
	if !h.userInfo.IsSuperuser {
		// If no database pool (test mode), require superuser
		if h.dbPool == nil {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// Service tokens must be superusers for this operation
		if h.userInfo.IsServiceToken {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// For non-superuser users, check privileges via group membership
		ctx := context.Background()

		// Get user ID from username
		var userID int
		err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1",
			h.userInfo.Username).Scan(&userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}

		// Check if user has privilege to set token connection scope
		canAccess, err := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "set_token_connection_scope")
		if err != nil {
			return nil, fmt.Errorf("failed to check privileges: %w", err)
		}
		if !canAccess {
			return nil, fmt.Errorf("permission denied: insufficient privileges")
		}
	}

	tokenID := int(args["tokenId"].(float64))      //nolint:errcheck // Type assertion
	tokenType, _ := args["tokenType"].(string)     //nolint:errcheck // Type assertion
	connectionIDsRaw, _ := args["connectionIds"].([]interface{}) //nolint:errcheck // Type assertion

	// Convert []interface{} to []int
	connectionIDs := make([]int, len(connectionIDsRaw))
	for i, v := range connectionIDsRaw {
		connectionIDs[i] = int(v.(float64)) //nolint:errcheck // Type assertion
	}

	ctx := context.Background()
	err := groupmgmt.SetTokenConnectionScope(ctx, h.dbPool, tokenID, tokenType, connectionIDs)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("Token connection scope set: %s token %d is now limited to %d connection(s)", tokenType, tokenID, len(connectionIDs)),
			},
		},
	}, nil
}

// handleSetTokenMCPScope executes the set_token_mcp_scope tool
func (h *Handler) handleSetTokenMCPScope(args map[string]interface{}) (interface{}, error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("permission denied: superuser privileges required")
	}

	// Superusers bypass all privilege checks
	if !h.userInfo.IsSuperuser {
		// If no database pool (test mode), require superuser
		if h.dbPool == nil {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// Service tokens must be superusers for this operation
		if h.userInfo.IsServiceToken {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// For non-superuser users, check privileges via group membership
		ctx := context.Background()

		// Get user ID from username
		var userID int
		err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1",
			h.userInfo.Username).Scan(&userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}

		// Check if user has privilege to set token MCP scope
		canAccess, err := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "set_token_mcp_scope")
		if err != nil {
			return nil, fmt.Errorf("failed to check privileges: %w", err)
		}
		if !canAccess {
			return nil, fmt.Errorf("permission denied: insufficient privileges")
		}
	}

	tokenID := int(args["tokenId"].(float64))      //nolint:errcheck // Type assertion
	tokenType, _ := args["tokenType"].(string)     //nolint:errcheck // Type assertion
	privilegeIdentifiersRaw, _ := args["privilegeIdentifiers"].([]interface{}) //nolint:errcheck // Type assertion

	// Convert []interface{} to []string
	privilegeIdentifiers := make([]string, len(privilegeIdentifiersRaw))
	for i, v := range privilegeIdentifiersRaw {
		privilegeIdentifiers[i], _ = v.(string) //nolint:errcheck // Type assertion
	}

	ctx := context.Background()
	err := groupmgmt.SetTokenMCPScope(ctx, h.dbPool, tokenID, tokenType, privilegeIdentifiers)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("Token MCP scope set: %s token %d is now limited to %d MCP item(s)", tokenType, tokenID, len(privilegeIdentifiers)),
			},
		},
	}, nil
}

// handleGetTokenScope executes the get_token_scope tool
func (h *Handler) handleGetTokenScope(args map[string]interface{}) (interface{}, error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("permission denied: superuser privileges required")
	}

	// Superusers bypass all privilege checks
	if !h.userInfo.IsSuperuser {
		// If no database pool (test mode), require superuser
		if h.dbPool == nil {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// Service tokens must be superusers for this operation
		if h.userInfo.IsServiceToken {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// For non-superuser users, check privileges via group membership
		ctx := context.Background()

		// Get user ID from username
		var userID int
		err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1",
			h.userInfo.Username).Scan(&userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}

		// Check if user has privilege to get token scope
		canAccess, err := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "get_token_scope")
		if err != nil {
			return nil, fmt.Errorf("failed to check privileges: %w", err)
		}
		if !canAccess {
			return nil, fmt.Errorf("permission denied: insufficient privileges")
		}
	}

	tokenID := int(args["tokenId"].(float64))  //nolint:errcheck // Type assertion
	tokenType, _ := args["tokenType"].(string) //nolint:errcheck // Type assertion

	ctx := context.Background()
	scope, err := groupmgmt.GetTokenScope(ctx, h.dbPool, tokenID, tokenType)
	if err != nil {
		return nil, err
	}

	// Format scope as JSON string
	scopeJSON, err := json.Marshal(scope)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal scope: %w", err)
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("Scope for %s token %d:\n%s", tokenType, tokenID, string(scopeJSON)),
			},
		},
	}, nil
}

// handleClearTokenScope executes the clear_token_scope tool
func (h *Handler) handleClearTokenScope(args map[string]interface{}) (interface{}, error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("permission denied: superuser privileges required")
	}

	// Superusers bypass all privilege checks
	if !h.userInfo.IsSuperuser {
		// If no database pool (test mode), require superuser
		if h.dbPool == nil {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// Service tokens must be superusers for this operation
		if h.userInfo.IsServiceToken {
			return nil, fmt.Errorf("permission denied: superuser privileges required")
		}

		// For non-superuser users, check privileges via group membership
		ctx := context.Background()

		// Get user ID from username
		var userID int
		err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1",
			h.userInfo.Username).Scan(&userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}

		// Check if user has privilege to clear token scope
		canAccess, err := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "clear_token_scope")
		if err != nil {
			return nil, fmt.Errorf("failed to check privileges: %w", err)
		}
		if !canAccess {
			return nil, fmt.Errorf("permission denied: insufficient privileges")
		}
	}

	tokenID := int(args["tokenId"].(float64))  //nolint:errcheck // Type assertion
	tokenType, _ := args["tokenType"].(string) //nolint:errcheck // Type assertion

	ctx := context.Background()
	err := groupmgmt.ClearTokenScope(ctx, h.dbPool, tokenID, tokenType)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("All scope restrictions cleared for %s token %d", tokenType, tokenID),
			},
		},
	}, nil
}

// FormatResponse formats a response as JSON
func FormatResponse(resp *Response) ([]byte, error) {
	data, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}
	return data, nil
}
