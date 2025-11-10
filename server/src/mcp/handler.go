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
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgEdge/ai-workbench/server/src/config"
	"github.com/pgEdge/ai-workbench/server/src/crypto"
	"github.com/pgEdge/ai-workbench/server/src/groupmgmt"
	"github.com/pgedge/ai-workbench/pkg/logger"
	"github.com/pgEdge/ai-workbench/server/src/privileges"
	"github.com/pgEdge/ai-workbench/server/src/session"
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
		{
			"uri":         "ai-workbench://groups",
			"name":        "User Groups",
			"description": "List of all user groups in the system",
			"mimeType":    "application/json",
		},
		{
			"uri":         "ai-workbench://mcp-privileges",
			"name":        "MCP Privilege Identifiers",
			"description": "List of all MCP privilege identifiers",
			"mimeType":    "application/json",
		},
		{
			"uri":         "ai-workbench://users/{username}/tokens",
			"name":        "User Tokens",
			"description": "List of tokens for a specific user (replace {username} with actual username)",
			"mimeType":    "application/json",
		},
		{
			"uri":         "ai-workbench://users/{username}/groups",
			"name":        "User Group Memberships",
			"description": "List of groups a user belongs to (replace {username} with actual username)",
			"mimeType":    "application/json",
		},
		{
			"uri":         "ai-workbench://groups/{groupId}/members",
			"name":        "Group Members",
			"description": "List of members in a group (replace {groupId} with actual group ID)",
			"mimeType":    "application/json",
		},
		{
			"uri":         "ai-workbench://groups/{groupId}/mcp-privileges",
			"name":        "Group MCP Privileges",
			"description": "List of MCP privileges for a group (replace {groupId} with actual group ID)",
			"mimeType":    "application/json",
		},
		{
			"uri":         "ai-workbench://connections/{connectionId}/privileges",
			"name":        "Connection Privileges",
			"description": "List of privileges for a database connection (replace {connectionId} with actual connection ID)",
			"mimeType":    "application/json",
		},
		{
			"uri":         "ai-workbench://connections",
			"name":        "Database Connections",
			"description": "List of all database connections in the system",
			"mimeType":    "application/json",
		},
		{
			"uri":         "ai-workbench://session/context",
			"name":        "Session Database Context",
			"description": "Current database context for your session (if set)",
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

	case "ai-workbench://groups":
		// List all user groups
		groups, err := groupmgmt.ListUserGroups(ctx, h.dbPool)
		if err != nil {
			logger.Errorf("Failed to list user groups: %v", err)
			return NewErrorResponse(req.ID, InternalError,
				"Failed to list user groups", err.Error()), nil
		}

		for _, group := range groups {
			groupJSON, err := json.Marshal(group)
			if err != nil {
				logger.Errorf("Failed to marshal group: %v", err)
				continue
			}
			groupID := group["id"]
			contents = append(contents, map[string]interface{}{
				"uri":      fmt.Sprintf("ai-workbench://groups/%v", groupID),
				"mimeType": "application/json",
				"text":     string(groupJSON),
			})
		}

	case "ai-workbench://mcp-privileges":
		// List MCP privilege identifiers
		identifiers, err := groupmgmt.ListMCPPrivilegeIdentifiers(ctx, h.dbPool)
		if err != nil {
			logger.Errorf("Failed to list MCP privilege identifiers: %v", err)
			return NewErrorResponse(req.ID, InternalError,
				"Failed to list MCP privilege identifiers", err.Error()), nil
		}

		for _, identifier := range identifiers {
			identifierJSON, err := json.Marshal(identifier)
			if err != nil {
				logger.Errorf("Failed to marshal identifier: %v", err)
				continue
			}
			privID := identifier["identifier"]
			contents = append(contents, map[string]interface{}{
				"uri":      fmt.Sprintf("ai-workbench://mcp-privileges/%v", privID),
				"mimeType": "application/json",
				"text":     string(identifierJSON),
			})
		}

	case "ai-workbench://connections":
		// List all database connections
		// Only show connections owned by the current user/token, unless they're a superuser
		var rows pgx.Rows
		var err error
		if h.userInfo == nil || !h.userInfo.IsAuthenticated {
			return NewErrorResponse(req.ID, InternalError,
				"Authentication required", nil), nil
		}

		if h.userInfo.IsSuperuser {
			// Superusers can see all connections
			rows, err = h.dbPool.Query(ctx, `
				SELECT id, owner_username, owner_token, is_shared, is_monitored,
				       name, host, hostaddr, port, database_name, username,
				       sslmode, created_at, updated_at
				FROM connections
				ORDER BY name
			`)
		} else if h.userInfo.IsServiceToken {
			// Service tokens see their own connections
			rows, err = h.dbPool.Query(ctx, `
				SELECT id, owner_username, owner_token, is_shared, is_monitored,
				       name, host, hostaddr, port, database_name, username,
				       sslmode, created_at, updated_at
				FROM connections
				WHERE owner_token = $1
				ORDER BY name
			`, h.userInfo.Username)
		} else {
			// Regular users see their own connections
			rows, err = h.dbPool.Query(ctx, `
				SELECT id, owner_username, owner_token, is_shared, is_monitored,
				       name, host, hostaddr, port, database_name, username,
				       sslmode, created_at, updated_at
				FROM connections
				WHERE owner_username = $1
				ORDER BY name
			`, h.userInfo.Username)
		}

		if err != nil {
			logger.Errorf("Failed to query connections: %v", err)
			return NewErrorResponse(req.ID, InternalError,
				"Failed to query connections", err.Error()), nil
		}
		defer rows.Close()

		for rows.Next() {
			var id int
			var ownerUsername, ownerToken, hostaddr, sslmode interface{}
			var isShared, isMonitored bool
			var name, host string
			var port int
			var databaseName, username string
			var createdAt, updatedAt time.Time

			if err := rows.Scan(&id, &ownerUsername, &ownerToken, &isShared,
				&isMonitored, &name, &host, &hostaddr, &port, &databaseName,
				&username, &sslmode, &createdAt, &updatedAt); err != nil {
				logger.Errorf("Failed to scan connection: %v", err)
				continue
			}

			connData := map[string]interface{}{
				"id":           id,
				"isShared":     isShared,
				"isMonitored":  isMonitored,
				"name":         name,
				"host":         host,
				"hostaddr":     hostaddr,
				"port":         port,
				"databaseName": databaseName,
				"username":     username,
				"sslmode":      sslmode,
				"createdAt":    createdAt,
				"updatedAt":    updatedAt,
			}

			// Only include ownership info for superusers
			if h.userInfo.IsSuperuser {
				connData["ownerUsername"] = ownerUsername
				connData["ownerToken"] = ownerToken
			}

			connJSON, err := json.Marshal(connData)
			if err != nil {
				logger.Errorf("Failed to marshal connection: %v", err)
				continue
			}

			contents = append(contents, map[string]interface{}{
				"uri":      fmt.Sprintf("ai-workbench://connections/%d", id),
				"mimeType": "application/json",
				"text":     string(connJSON),
			})
		}

	case "ai-workbench://session/context":
		// Return the current session database context
		if h.userInfo == nil || !h.userInfo.IsAuthenticated {
			return NewErrorResponse(req.ID, InternalError,
				"Authentication required", nil), nil
		}

		// Import the session package
		ctx := session.GetContext(h.userInfo.Username)
		if ctx == nil {
			contents = append(contents, map[string]interface{}{
				"uri":      "ai-workbench://session/context",
				"mimeType": "application/json",
				"text":     `{"hasContext": false, "message": "No database context set. Use set_database_context to establish a working database."}`,
			})
		} else {
			contextJSON := fmt.Sprintf(`{"hasContext": true, "connectionId": %d, "databaseName": %q}`,
				ctx.ConnectionID, ctx.DatabaseName)
			contents = append(contents, map[string]interface{}{
				"uri":      "ai-workbench://session/context",
				"mimeType": "application/json",
				"text":     contextJSON,
			})
		}

	default:
		// Handle parameterized URIs
		if strings.HasPrefix(params.URI, "ai-workbench://users/") && strings.HasSuffix(params.URI, "/tokens") {
			// Extract username from URI: ai-workbench://users/{username}/tokens
			username := strings.TrimSuffix(strings.TrimPrefix(params.URI, "ai-workbench://users/"), "/tokens")

			// Check authorization: users can only list their own tokens unless they're superuser
			if h.userInfo == nil || !h.userInfo.IsAuthenticated {
				return NewErrorResponse(req.ID, InternalError,
					"Authentication required", nil), nil
			}
			if h.userInfo.Username != username && !h.userInfo.IsSuperuser {
				return NewErrorResponse(req.ID, InternalError,
					"Permission denied: can only list your own tokens", nil), nil
			}

			tokens, err := usermgmt.ListUserTokens(h.dbPool, username)
			if err != nil {
				logger.Errorf("Failed to list user tokens: %v", err)
				return NewErrorResponse(req.ID, InternalError,
					"Failed to list user tokens", err.Error()), nil
			}

			for _, token := range tokens {
				tokenJSON, err := json.Marshal(token)
				if err != nil {
					logger.Errorf("Failed to marshal token: %v", err)
					continue
				}
				tokenID := token["id"]
				contents = append(contents, map[string]interface{}{
					"uri":      fmt.Sprintf("ai-workbench://users/%s/tokens/%v", username, tokenID),
					"mimeType": "application/json",
					"text":     string(tokenJSON),
				})
			}

		} else if strings.HasPrefix(params.URI, "ai-workbench://users/") && strings.HasSuffix(params.URI, "/groups") {
			// Extract username from URI: ai-workbench://users/{username}/groups
			username := strings.TrimSuffix(strings.TrimPrefix(params.URI, "ai-workbench://users/"), "/groups")

			// Get user ID from username
			var userID int
			err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1", username).Scan(&userID)
			if err != nil {
				logger.Errorf("User not found: %v", err)
				return NewErrorResponse(req.ID, InternalError,
					"User not found", err.Error()), nil
			}

			groups, err := groupmgmt.ListUserGroupMemberships(ctx, h.dbPool, userID)
			if err != nil {
				logger.Errorf("Failed to list user group memberships: %v", err)
				return NewErrorResponse(req.ID, InternalError,
					"Failed to list user group memberships", err.Error()), nil
			}

			for _, group := range groups {
				groupJSON, err := json.Marshal(group)
				if err != nil {
					logger.Errorf("Failed to marshal group: %v", err)
					continue
				}
				groupID := group["group_id"]
				contents = append(contents, map[string]interface{}{
					"uri":      fmt.Sprintf("ai-workbench://groups/%v", groupID),
					"mimeType": "application/json",
					"text":     string(groupJSON),
				})
			}

		} else if strings.HasPrefix(params.URI, "ai-workbench://groups/") && strings.HasSuffix(params.URI, "/members") {
			// Extract groupId from URI: ai-workbench://groups/{groupId}/members
			groupIDStr := strings.TrimSuffix(strings.TrimPrefix(params.URI, "ai-workbench://groups/"), "/members")
			groupID, err := strconv.Atoi(groupIDStr)
			if err != nil {
				return NewErrorResponse(req.ID, InvalidParams,
					"Invalid group ID in URI", err.Error()), nil
			}

			members, err := groupmgmt.ListGroupMembers(ctx, h.dbPool, groupID)
			if err != nil {
				logger.Errorf("Failed to list group members: %v", err)
				return NewErrorResponse(req.ID, InternalError,
					"Failed to list group members", err.Error()), nil
			}

			for _, member := range members {
				memberJSON, err := json.Marshal(member)
				if err != nil {
					logger.Errorf("Failed to marshal member: %v", err)
					continue
				}
				username := member["username"]
				contents = append(contents, map[string]interface{}{
					"uri":      fmt.Sprintf("ai-workbench://users/%v", username),
					"mimeType": "application/json",
					"text":     string(memberJSON),
				})
			}

		} else if strings.HasPrefix(params.URI, "ai-workbench://groups/") && strings.HasSuffix(params.URI, "/mcp-privileges") {
			// Extract groupId from URI: ai-workbench://groups/{groupId}/mcp-privileges
			groupIDStr := strings.TrimSuffix(strings.TrimPrefix(params.URI, "ai-workbench://groups/"), "/mcp-privileges")
			groupID, err := strconv.Atoi(groupIDStr)
			if err != nil {
				return NewErrorResponse(req.ID, InvalidParams,
					"Invalid group ID in URI", err.Error()), nil
			}

			privileges, err := groupmgmt.ListGroupMCPPrivileges(ctx, h.dbPool, groupID)
			if err != nil {
				logger.Errorf("Failed to list group MCP privileges: %v", err)
				return NewErrorResponse(req.ID, InternalError,
					"Failed to list group MCP privileges", err.Error()), nil
			}

			for _, priv := range privileges {
				privJSON, err := json.Marshal(priv)
				if err != nil {
					logger.Errorf("Failed to marshal privilege: %v", err)
					continue
				}
				identifier := priv["identifier"]
				contents = append(contents, map[string]interface{}{
					"uri":      fmt.Sprintf("ai-workbench://mcp-privileges/%v", identifier),
					"mimeType": "application/json",
					"text":     string(privJSON),
				})
			}

		} else if strings.HasPrefix(params.URI, "ai-workbench://connections/") && strings.HasSuffix(params.URI, "/privileges") {
			// Extract connectionId from URI: ai-workbench://connections/{connectionId}/privileges
			connectionIDStr := strings.TrimSuffix(strings.TrimPrefix(params.URI, "ai-workbench://connections/"), "/privileges")
			connectionID, err := strconv.Atoi(connectionIDStr)
			if err != nil {
				return NewErrorResponse(req.ID, InvalidParams,
					"Invalid connection ID in URI", err.Error()), nil
			}

			privileges, err := groupmgmt.ListConnectionPrivileges(ctx, h.dbPool, connectionID)
			if err != nil {
				logger.Errorf("Failed to list connection privileges: %v", err)
				return NewErrorResponse(req.ID, InternalError,
					"Failed to list connection privileges", err.Error()), nil
			}

			for _, priv := range privileges {
				privJSON, err := json.Marshal(priv)
				if err != nil {
					logger.Errorf("Failed to marshal connection privilege: %v", err)
					continue
				}
				privGroupID := priv["group_id"]
				contents = append(contents, map[string]interface{}{
					"uri":      fmt.Sprintf("ai-workbench://connections/%d/privileges/%v", connectionID, privGroupID),
					"mimeType": "application/json",
					"text":     string(privJSON),
				})
			}

		} else {
			return NewErrorResponse(req.ID, InvalidParams, "Unknown resource URI",
				nil), nil
		}
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
		{
			"name":        "create_connection",
			"description": "Create a new database connection",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "User-friendly name for the connection",
					},
					"host": map[string]interface{}{
						"type":        "string",
						"description": "Hostname or IP address of the PostgreSQL server",
					},
					"port": map[string]interface{}{
						"type":        "number",
						"description": "Port number for PostgreSQL connection (default 5432)",
						"default":     5432,
					},
					"databaseName": map[string]interface{}{
						"type":        "string",
						"description": "Database to connect to",
					},
					"username": map[string]interface{}{
						"type":        "string",
						"description": "Username for connection",
					},
					"password": map[string]interface{}{
						"type":        "string",
						"description": "Password for connection",
					},
					"isShared": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether the connection is shared among users or private",
						"default":     false,
					},
					"isMonitored": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether this connection should be actively monitored",
						"default":     false,
					},
					"hostaddr": map[string]interface{}{
						"type":        "string",
						"description": "IP address to bypass DNS lookup (optional)",
					},
					"sslmode": map[string]interface{}{
						"type":        "string",
						"description": "SSL mode for connection (optional)",
						"enum":        []string{"disable", "allow", "prefer", "require", "verify-ca", "verify-full"},
					},
					"sslcert": map[string]interface{}{
						"type":        "string",
						"description": "SSL certificate (optional)",
					},
					"sslkey": map[string]interface{}{
						"type":        "string",
						"description": "SSL key (optional)",
					},
					"sslrootcert": map[string]interface{}{
						"type":        "string",
						"description": "SSL root certificate (optional)",
					},
				},
				"required": []string{"name", "host", "databaseName", "username", "password"},
			},
		},
		{
			"name":        "update_connection",
			"description": "Update an existing database connection",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id": map[string]interface{}{
						"type":        "number",
						"description": "ID of the connection to update",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "New name for the connection (optional)",
					},
					"host": map[string]interface{}{
						"type":        "string",
						"description": "New hostname or IP address (optional)",
					},
					"port": map[string]interface{}{
						"type":        "number",
						"description": "New port number (optional)",
					},
					"databaseName": map[string]interface{}{
						"type":        "string",
						"description": "New database name (optional)",
					},
					"username": map[string]interface{}{
						"type":        "string",
						"description": "New username (optional)",
					},
					"password": map[string]interface{}{
						"type":        "string",
						"description": "New password (optional)",
					},
					"isShared": map[string]interface{}{
						"type":        "boolean",
						"description": "Update shared status (optional)",
					},
					"isMonitored": map[string]interface{}{
						"type":        "boolean",
						"description": "Update monitoring status (optional)",
					},
					"hostaddr": map[string]interface{}{
						"type":        "string",
						"description": "New IP address (optional)",
					},
					"sslmode": map[string]interface{}{
						"type":        "string",
						"description": "New SSL mode (optional)",
						"enum":        []string{"disable", "allow", "prefer", "require", "verify-ca", "verify-full"},
					},
					"sslcert": map[string]interface{}{
						"type":        "string",
						"description": "New SSL certificate (optional)",
					},
					"sslkey": map[string]interface{}{
						"type":        "string",
						"description": "New SSL key (optional)",
					},
					"sslrootcert": map[string]interface{}{
						"type":        "string",
						"description": "New SSL root certificate (optional)",
					},
				},
				"required": []string{"id"},
			},
		},
		{
			"name":        "delete_connection",
			"description": "Delete a database connection",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id": map[string]interface{}{
						"type":        "number",
						"description": "ID of the connection to delete",
					},
				},
				"required": []string{"id"},
			},
		},
		{
			"name": "execute_query",
			"description": `Execute SQL queries on database connections in read-only mode. If no connectionId is provided, uses the session context (set via set_database_context). Cannot access datastore (connection ID 0) - use query_datastore for historical metrics.`,
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"connectionId": map[string]interface{}{
						"type":        "number",
						"description": "Optional: Connection ID to execute the query on. If not provided, uses the session context set by set_database_context. Cannot be 0 (use query_datastore for historical metrics).",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "SQL query to execute (will run in read-only transaction mode)",
					},
					"databaseName": map[string]interface{}{
						"type":        "string",
						"description": "Optional: specific database name to connect to on the server (overrides connection's default database or session context database).",
					},
					"maxRows": map[string]interface{}{
						"type":        "number",
						"description": "Maximum number of rows to return (default: 1000, max: 10000)",
						"default":     1000,
					},
				},
				"required": []string{"query"},
			},
		},
		{
			"name": "set_database_context",
			"description": `Set the working database for this session. Subsequent execute_query calls will use this connection and database by default.`,
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"connectionId": map[string]interface{}{
						"type":        "number",
						"description": "Connection ID to use as the current database context (must be a connection you have access to, cannot be 0 for datastore)",
					},
					"databaseName": map[string]interface{}{
						"type":        "string",
						"description": "Optional: specific database name to use on this server (overrides connection's default database)",
					},
				},
				"required": []string{"connectionId"},
			},
		},
		{
			"name": "get_database_context",
			"description": `Get the current working database context for this session.`,
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
				"required":   []string{},
			},
		},
		{
			"name": "clear_database_context",
			"description": `Clear the working database context for this session.`,
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
				"required":   []string{},
			},
		},
		{
			"name": "query_datastore",
			"description": `Query historical performance metrics collected from monitored PostgreSQL servers. The datastore contains time-series data in the 'metrics' schema for trend analysis. Use execute_query for live database queries.`,
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "SQL query to execute on the datastore (will run in read-only transaction mode)",
					},
					"maxRows": map[string]interface{}{
						"type":        "number",
						"description": "Maximum number of rows to return (default: 1000, max: 10000)",
						"default":     1000,
					},
				},
				"required": []string{"query"},
			},
		},
		{
			"name": "read_resource",
			"description": `Read data from an MCP resource. Use this to fetch actual data from resources like ai-workbench://connections (list of database connections), ai-workbench://users (user accounts), etc. Call this tool to discover what connections are available before trying to connect to them.`,
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"uri": map[string]interface{}{
						"type":        "string",
						"description": "The resource URI to read (e.g., 'ai-workbench://connections' to list all available database connections)",
					},
				},
				"required": []string{"uri"},
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
	case "delete_user_token":
		result, err = h.handleDeleteUserToken(params.Arguments)
	case "create_user_group":
		result, err = h.handleCreateUserGroup(params.Arguments)
	case "update_user_group":
		result, err = h.handleUpdateUserGroup(params.Arguments)
	case "delete_user_group":
		result, err = h.handleDeleteUserGroup(params.Arguments)
	case "add_group_member":
		result, err = h.handleAddGroupMember(params.Arguments)
	case "remove_group_member":
		result, err = h.handleRemoveGroupMember(params.Arguments)
	case "grant_connection_privilege":
		result, err = h.handleGrantConnectionPrivilege(params.Arguments)
	case "revoke_connection_privilege":
		result, err = h.handleRevokeConnectionPrivilege(params.Arguments)
	case "grant_mcp_privilege":
		result, err = h.handleGrantMCPPrivilege(params.Arguments)
	case "revoke_mcp_privilege":
		result, err = h.handleRevokeMCPPrivilege(params.Arguments)
	case "set_token_connection_scope":
		result, err = h.handleSetTokenConnectionScope(params.Arguments)
	case "set_token_mcp_scope":
		result, err = h.handleSetTokenMCPScope(params.Arguments)
	case "get_token_scope":
		result, err = h.handleGetTokenScope(params.Arguments)
	case "clear_token_scope":
		result, err = h.handleClearTokenScope(params.Arguments)
	case "create_connection":
		result, err = h.handleCreateConnection(params.Arguments)
	case "update_connection":
		result, err = h.handleUpdateConnection(params.Arguments)
	case "delete_connection":
		result, err = h.handleDeleteConnection(params.Arguments)
	case "execute_query":
		result, err = h.handleExecuteQuery(params.Arguments)
	case "set_database_context":
		result, err = h.handleSetDatabaseContext(params.Arguments)
	case "get_database_context":
		result, err = h.handleGetDatabaseContext(params.Arguments)
	case "clear_database_context":
		result, err = h.handleClearDatabaseContext(params.Arguments)
	case "query_datastore":
		result, err = h.handleQueryDatastore(params.Arguments)
	case "read_resource":
		result, err = h.handleReadResourceTool(params.Arguments)
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
	Username        string // User username or service token name
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
	var tokenName string
	err := h.dbPool.QueryRow(ctx, `
		SELECT name, expires_at, is_superuser
		FROM service_tokens
		WHERE token_hash = $1
	`, tokenHash).Scan(&tokenName, &expiresAt, &isSuperuser)

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
		logger.Infof("Valid service token '%s' (superuser: %t)", tokenName, isSuperuser)
		return &UserInfo{
			IsAuthenticated: true,
			IsSuperuser:     isSuperuser,
			Username:        tokenName,
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

// handleCreateConnection executes the create_connection tool
func (h *Handler) handleCreateConnection(args map[string]interface{}) (interface{}, error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("authentication required")
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

		// Check if user has privilege to create connections
		canAccess, err := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "create_connection")
		if err != nil {
			return nil, fmt.Errorf("failed to check privileges: %w", err)
		}
		if !canAccess {
			return nil, fmt.Errorf("permission denied: insufficient privileges")
		}
	}

	// Extract parameters
	name, _ := args["name"].(string)                     //nolint:errcheck
	host, _ := args["host"].(string)                     //nolint:errcheck
	databaseName, _ := args["databaseName"].(string)     //nolint:errcheck
	username, _ := args["username"].(string)             //nolint:errcheck
	password, _ := args["password"].(string)             //nolint:errcheck
	port := 5432                                          // default
	if portFloat, ok := args["port"].(float64); ok {
		port = int(portFloat)
	}
	isShared, _ := args["isShared"].(bool)               //nolint:errcheck
	isMonitored, _ := args["isMonitored"].(bool)         //nolint:errcheck
	hostaddr, _ := args["hostaddr"].(string)             //nolint:errcheck
	sslmode, _ := args["sslmode"].(string)               //nolint:errcheck
	sslcert, _ := args["sslcert"].(string)               //nolint:errcheck
	sslkey, _ := args["sslkey"].(string)                 //nolint:errcheck
	sslrootcert, _ := args["sslrootcert"].(string)       //nolint:errcheck

	// Encrypt the password
	encryptedPassword, err := crypto.EncryptPassword(password, h.config.GetServerSecret(), username)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt password: %w", err)
	}

	// Determine ownership
	var ownerUsername, ownerToken interface{}
	if h.userInfo.IsServiceToken {
		ownerToken = h.userInfo.Username
		ownerUsername = nil
	} else {
		ownerUsername = h.userInfo.Username
		ownerToken = nil
	}

	// Insert into database
	ctx := context.Background()
	var connectionID int
	err = h.dbPool.QueryRow(ctx, `
		INSERT INTO connections (
			owner_username, owner_token, is_shared, is_monitored,
			name, host, hostaddr, port, database_name, username,
			password_encrypted, sslmode, sslcert, sslkey, sslrootcert
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING id
	`, ownerUsername, ownerToken, isShared, isMonitored, name, host, hostaddr,
		port, databaseName, username, encryptedPassword, sslmode, sslcert, sslkey, sslrootcert).Scan(&connectionID)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection: %w", err)
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("Connection '%s' created successfully with ID %d", name, connectionID),
			},
		},
	}, nil
}

// handleUpdateConnection executes the update_connection tool
func (h *Handler) handleUpdateConnection(args map[string]interface{}) (interface{}, error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("authentication required")
	}

	// Get connection ID
	connectionID := int(args["id"].(float64)) //nolint:errcheck

	// Check ownership and permissions
	ctx := context.Background()
	var ownerUsername, ownerToken interface{}
	err := h.dbPool.QueryRow(ctx, `
		SELECT owner_username, owner_token
		FROM connections
		WHERE id = $1
	`, connectionID).Scan(&ownerUsername, &ownerToken)
	if err != nil {
		return nil, fmt.Errorf("connection not found: %w", err)
	}

	// Check authorization
	if !h.userInfo.IsSuperuser {
		if h.userInfo.IsServiceToken {
			tokenStr, ok := ownerToken.(string)
			if ownerToken == nil || !ok || tokenStr != h.userInfo.Username {
				return nil, fmt.Errorf("permission denied: can only update own connections")
			}
		} else {
			usernameStr, ok := ownerUsername.(string)
			if ownerUsername == nil || !ok || usernameStr != h.userInfo.Username {
				return nil, fmt.Errorf("permission denied: can only update own connections")
			}

			// Check privilege for non-superuser users
			var userID int
			err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1",
				h.userInfo.Username).Scan(&userID)
			if err != nil {
				return nil, fmt.Errorf("failed to get user ID: %w", err)
			}

			canAccess, err := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "update_connection")
			if err != nil {
				return nil, fmt.Errorf("failed to check privileges: %w", err)
			}
			if !canAccess {
				return nil, fmt.Errorf("permission denied: insufficient privileges")
			}
		}
	}

	// Build update query dynamically based on provided fields
	updates := []string{}
	values := []interface{}{}
	paramIdx := 1

	if name, ok := args["name"].(string); ok {
		updates = append(updates, fmt.Sprintf("name = $%d", paramIdx))
		values = append(values, name)
		paramIdx++
	}

	if host, ok := args["host"].(string); ok {
		updates = append(updates, fmt.Sprintf("host = $%d", paramIdx))
		values = append(values, host)
		paramIdx++
	}

	if hostaddr, ok := args["hostaddr"].(string); ok {
		updates = append(updates, fmt.Sprintf("hostaddr = $%d", paramIdx))
		values = append(values, hostaddr)
		paramIdx++
	}

	if portFloat, ok := args["port"].(float64); ok {
		updates = append(updates, fmt.Sprintf("port = $%d", paramIdx))
		values = append(values, int(portFloat))
		paramIdx++
	}

	if databaseName, ok := args["databaseName"].(string); ok {
		updates = append(updates, fmt.Sprintf("database_name = $%d", paramIdx))
		values = append(values, databaseName)
		paramIdx++
	}

	if username, ok := args["username"].(string); ok {
		updates = append(updates, fmt.Sprintf("username = $%d", paramIdx))
		values = append(values, username)
		paramIdx++
	}

	if password, ok := args["password"].(string); ok {
		// Get current username for encryption
		var currentUsername string
		err := h.dbPool.QueryRow(ctx, "SELECT username FROM connections WHERE id = $1", connectionID).Scan(&currentUsername)
		if err != nil {
			return nil, fmt.Errorf("failed to get connection username: %w", err)
		}

		encryptedPassword, err := crypto.EncryptPassword(password, h.config.GetServerSecret(), currentUsername)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt password: %w", err)
		}
		updates = append(updates, fmt.Sprintf("password_encrypted = $%d", paramIdx))
		values = append(values, encryptedPassword)
		paramIdx++
	}

	if isShared, ok := args["isShared"].(bool); ok {
		updates = append(updates, fmt.Sprintf("is_shared = $%d", paramIdx))
		values = append(values, isShared)
		paramIdx++
	}

	if isMonitored, ok := args["isMonitored"].(bool); ok {
		updates = append(updates, fmt.Sprintf("is_monitored = $%d", paramIdx))
		values = append(values, isMonitored)
		paramIdx++
	}

	if sslmode, ok := args["sslmode"].(string); ok {
		updates = append(updates, fmt.Sprintf("sslmode = $%d", paramIdx))
		values = append(values, sslmode)
		paramIdx++
	}

	if sslcert, ok := args["sslcert"].(string); ok {
		updates = append(updates, fmt.Sprintf("sslcert = $%d", paramIdx))
		values = append(values, sslcert)
		paramIdx++
	}

	if sslkey, ok := args["sslkey"].(string); ok {
		updates = append(updates, fmt.Sprintf("sslkey = $%d", paramIdx))
		values = append(values, sslkey)
		paramIdx++
	}

	if sslrootcert, ok := args["sslrootcert"].(string); ok {
		updates = append(updates, fmt.Sprintf("sslrootcert = $%d", paramIdx))
		values = append(values, sslrootcert)
		paramIdx++
	}

	if len(updates) == 0 {
		return nil, fmt.Errorf("no fields to update")
	}

	// Add updated_at
	updates = append(updates, fmt.Sprintf("updated_at = $%d", paramIdx))
	values = append(values, time.Now())
	paramIdx++

	// Add connection ID to values
	values = append(values, connectionID)

	query := fmt.Sprintf(`
		UPDATE connections
		SET %s
		WHERE id = $%d
	`, strings.Join(updates, ", "), paramIdx)

	_, err = h.dbPool.Exec(ctx, query, values...)
	if err != nil {
		return nil, fmt.Errorf("failed to update connection: %w", err)
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("Connection %d updated successfully", connectionID),
			},
		},
	}, nil
}

// handleDeleteConnection executes the delete_connection tool
func (h *Handler) handleDeleteConnection(args map[string]interface{}) (interface{}, error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("authentication required")
	}

	// Get connection ID
	connectionID := int(args["id"].(float64)) //nolint:errcheck

	// Check ownership and permissions
	ctx := context.Background()
	var ownerUsername, ownerToken interface{}
	var connectionName string
	err := h.dbPool.QueryRow(ctx, `
		SELECT owner_username, owner_token, name
		FROM connections
		WHERE id = $1
	`, connectionID).Scan(&ownerUsername, &ownerToken, &connectionName)
	if err != nil {
		return nil, fmt.Errorf("connection not found: %w", err)
	}

	// Check authorization
	if !h.userInfo.IsSuperuser {
		if h.userInfo.IsServiceToken {
			tokenStr, ok := ownerToken.(string)
			if ownerToken == nil || !ok || tokenStr != h.userInfo.Username {
				return nil, fmt.Errorf("permission denied: can only delete own connections")
			}
		} else {
			usernameStr, ok := ownerUsername.(string)
			if ownerUsername == nil || !ok || usernameStr != h.userInfo.Username {
				return nil, fmt.Errorf("permission denied: can only delete own connections")
			}

			// Check privilege for non-superuser users
			var userID int
			err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1",
				h.userInfo.Username).Scan(&userID)
			if err != nil {
				return nil, fmt.Errorf("failed to get user ID: %w", err)
			}

			canAccess, err := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "delete_connection")
			if err != nil {
				return nil, fmt.Errorf("failed to check privileges: %w", err)
			}
			if !canAccess {
				return nil, fmt.Errorf("permission denied: insufficient privileges")
			}
		}
	}

	// Delete the connection
	_, err = h.dbPool.Exec(ctx, "DELETE FROM connections WHERE id = $1", connectionID)
	if err != nil {
		return nil, fmt.Errorf("failed to delete connection: %w", err)
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("Connection '%s' (ID %d) deleted successfully", connectionName, connectionID),
			},
		},
	}, nil
}

// handleExecuteQuery executes a SQL query on a database connection in read-only mode
func (h *Handler) handleExecuteQuery(args map[string]interface{}) (interface{}, error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("authentication required")
	}

	// Extract query (required)
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return nil, fmt.Errorf("query is required and must be a non-empty string")
	}

	// Extract optional connectionId - if not provided, use session context
	var connectionID int
	var useSessionContext bool
	if connIDFloat, ok := args["connectionId"].(float64); ok {
		connectionID = int(connIDFloat)
		// Block access to datastore (connection ID 0)
		if connectionID == 0 {
			return nil, fmt.Errorf("cannot use connection ID 0 (datastore) with execute_query. Use query_datastore tool for historical metrics instead")
		}
	} else {
		// No connectionId provided, use session context
		useSessionContext = true
		dbCtx := session.GetContext(h.userInfo.Username)
		if dbCtx == nil {
			return nil, fmt.Errorf("no database context set. Either provide connectionId parameter or use set_database_context first")
		}
		connectionID = dbCtx.ConnectionID
	}

	// Extract optional database name override
	var overrideDBName string
	if dbName, ok := args["databaseName"].(string); ok {
		overrideDBName = dbName
	} else if useSessionContext {
		// If using session context and no explicit override, use context's database name
		dbCtx := session.GetContext(h.userInfo.Username)
		if dbCtx != nil {
			overrideDBName = dbCtx.DatabaseName
		}
	}

	// Default and validate maxRows
	maxRows := 1000
	if mr, ok := args["maxRows"].(float64); ok {
		maxRows = int(mr)
	}
	if maxRows < 1 {
		maxRows = 1
	}
	if maxRows > 10000 {
		maxRows = 10000
	}

	ctx := context.Background()
	var connString string
	var connectionName string
	var targetDBName string

	// Get connection details from database
	var ownerUsername, ownerToken interface{}
	var host, hostaddr, databaseName, username, passwordEncrypted, sslmode interface{}
	var port int

	err := h.dbPool.QueryRow(ctx, `
		SELECT owner_username, owner_token, name, host, hostaddr, port,
			   database_name, username, password_encrypted, sslmode
		FROM connections
		WHERE id = $1
	`, connectionID).Scan(&ownerUsername, &ownerToken, &connectionName,
		&host, &hostaddr, &port, &databaseName, &username, &passwordEncrypted, &sslmode)
	if err != nil {
		return nil, fmt.Errorf("connection not found: %w", err)
	}

	// Check authorization (unless superuser)
	if !h.userInfo.IsSuperuser {
		canAccess := false

		if h.userInfo.IsServiceToken {
			// Service token - check if they own this connection
			tokenStr, ok := ownerToken.(string)
			canAccess = ownerToken != nil && ok && tokenStr == h.userInfo.Username
		} else {
			// Regular user - check if they own this connection OR have privilege via group
			usernameStr, ok := ownerUsername.(string)
			if ownerUsername != nil && ok && usernameStr == h.userInfo.Username {
				canAccess = true
			}

			// If not owned, check for privilege
			if !canAccess {
				var userID int
				err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1",
					h.userInfo.Username).Scan(&userID)
				if err == nil {
					// Check if user has execute_query privilege
					// Ignore error - if privilege check fails, treat as no access
					hasPrivilege, _ := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "execute_query") //nolint:errcheck
					if hasPrivilege {
						canAccess = true
					}

					// Also check if they have connection-level privileges
					if !canAccess {
						hasConnAccess, _ := privileges.CanAccessConnection(ctx, h.dbPool, userID, //nolint:errcheck
							connectionID, privileges.AccessLevelRead)
						if hasConnAccess {
							canAccess = true
						}
					}
				}
			}
		}

		if !canAccess {
			return nil, fmt.Errorf("permission denied: no access to this connection")
		}
	}

	// Decrypt password using owner username (matches how it was encrypted)
	usernameStr, _ := username.(string)          //nolint:errcheck
	passwordEnc, _ := passwordEncrypted.(string) //nolint:errcheck

	var password string

	// If password is empty/null, use empty string (e.g., trust auth)
	if passwordEnc == "" {
		password = ""
	} else {
		// Check if we have an owner username for decryption
		if ownerUsername != nil {
			ownerUsernameStr, _ := ownerUsername.(string) //nolint:errcheck
			if ownerUsernameStr != "" {
				// Decrypt using owner username
				var err error
				password, err = crypto.DecryptPassword(passwordEnc, h.config.GetServerSecret(), ownerUsernameStr)
				if err != nil {
					return nil, fmt.Errorf("failed to decrypt password: %w", err)
				}
			} else {
				// No owner username - use password as-is (legacy/backward compatibility)
				password = passwordEnc
			}
		} else {
			// No owner username - use password as-is (legacy/backward compatibility)
			password = passwordEnc
		}
	}

	// Build connection string
	hostStr, _ := host.(string)         //nolint:errcheck
	dbStr, _ := databaseName.(string) //nolint:errcheck

	// Use override database name if provided, otherwise use connection's default
	if overrideDBName != "" {
		targetDBName = overrideDBName
	} else {
		targetDBName = dbStr
	}

	sslmodeStr := "prefer"
	if ssl, ok := sslmode.(string); ok {
		sslmodeStr = ssl
	}

	connString = fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
		hostStr, port, targetDBName, usernameStr, password, sslmodeStr)

	if hostadr, ok := hostaddr.(string); ok && hostadr != "" {
		connString += fmt.Sprintf(" hostaddr=%s", hostadr)
	}

	// Connect to the target database
	conn, err := pgx.Connect(ctx, connString)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	defer conn.Close(ctx)

	// Set query timeout (30 seconds)
	timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Begin read-only transaction
	tx, err := conn.Begin(timeoutCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(timeoutCtx) //nolint:errcheck // Ignore error - read-only transaction
	}()

	// Set transaction to read-only
	_, err = tx.Exec(timeoutCtx, "SET TRANSACTION READ ONLY")
	if err != nil {
		return nil, fmt.Errorf("failed to set read-only mode: %w", err)
	}

	// Execute the query
	rows, err := tx.Query(timeoutCtx, query)
	if err != nil {
		return nil, fmt.Errorf("query execution failed: %w", err)
	}
	defer rows.Close()

	// Get column descriptions
	fieldDescriptions := rows.FieldDescriptions()
	columns := make([]string, len(fieldDescriptions))
	for i, fd := range fieldDescriptions {
		columns[i] = string(fd.Name)
	}

	// Collect rows
	var resultRows [][]interface{}
	rowCount := 0
	for rows.Next() {
		if rowCount >= maxRows {
			break
		}

		values, err := rows.Values()
		if err != nil {
			return nil, fmt.Errorf("failed to read row: %w", err)
		}

		// Convert byte arrays to strings for JSON serialization
		for i, val := range values {
			if b, ok := val.([]byte); ok {
				values[i] = string(b)
			}
		}

		resultRows = append(resultRows, values)
		rowCount++
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading rows: %w", err)
	}

	// Commit the read-only transaction (doesn't change anything but releases locks)
	if err := tx.Commit(timeoutCtx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Format the response
	truncated := rowCount >= maxRows && rows.Next()

	response := fmt.Sprintf("Query executed successfully on connection '%s' (ID: %d)", connectionName, connectionID)
	if targetDBName != "" {
		response += fmt.Sprintf(", database '%s'", targetDBName)
	}
	response += "\n\n"
	response += fmt.Sprintf("Columns: %v\n", columns)
	response += fmt.Sprintf("Rows returned: %d", rowCount)
	if truncated {
		response += fmt.Sprintf(" (truncated at maxRows limit of %d)", maxRows)
	}
	response += "\n\nResults:\n"

	// Convert to JSON for display
	resultJSON, err := json.MarshalIndent(map[string]interface{}{
		"columns": columns,
		"rows":    resultRows,
		"rowCount": rowCount,
		"truncated": truncated,
	}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to format results: %w", err)
	}

	response += string(resultJSON)

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": response,
			},
		},
	}, nil
}

// handleSetDatabaseContext sets the current database context for the user's session
func (h *Handler) handleSetDatabaseContext(args map[string]interface{}) (interface{}, error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("authentication required")
	}

	ctx := context.Background()

	// Get user ID from username (skip privilege check for service tokens - they always have access)
	var userID int
	if !h.userInfo.IsServiceToken {
		err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1",
			h.userInfo.Username).Scan(&userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}

		// Check MCP privilege
		//nolint:errcheck // Treating failure as "no access" is the correct security behavior
		hasAccess, _ := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "set_database_context")
		if !hasAccess {
			return nil, fmt.Errorf("access denied: you do not have permission to use set_database_context")
		}
	}

	// Extract connection ID
	connectionIDFloat, ok := args["connectionId"].(float64)
	if !ok {
		return nil, fmt.Errorf("connectionId is required and must be a number")
	}
	connectionID := int(connectionIDFloat)

	// Prevent setting context to datastore (connection ID 0)
	if connectionID == 0 {
		return nil, fmt.Errorf("cannot set database context to datastore (connection ID 0). Use query_datastore tool for historical metrics")
	}

	// Extract optional database name
	databaseName := ""
	if dbName, ok := args["databaseName"].(string); ok {
		databaseName = dbName
	}

	// Verify user has access to this connection (only for non-service tokens)
	if !h.userInfo.IsServiceToken {
		//nolint:errcheck // Treating failure as "no access" is the correct security behavior
		canAccess, _ := privileges.CanAccessConnection(ctx, h.dbPool, userID, connectionID, privileges.AccessLevelRead)
		if !canAccess {
			return nil, fmt.Errorf("access denied: you do not have permission to access connection ID %d", connectionID)
		}
	}

	// Set the context
	if err := session.SetContext(h.userInfo.Username, connectionID, databaseName); err != nil {
		return nil, fmt.Errorf("failed to set database context: %w", err)
	}

	logger.Infof("User %s set database context to connection %d, database %q", h.userInfo.Username, connectionID, databaseName)

	message := fmt.Sprintf("Database context set to connection ID %d", connectionID)
	if databaseName != "" {
		message += fmt.Sprintf(", database: %s", databaseName)
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": message + "\n\nAll subsequent execute_query calls will use this context by default until changed or cleared.",
			},
		},
	}, nil
}

// handleGetDatabaseContext retrieves the current database context for the user's session
func (h *Handler) handleGetDatabaseContext(args map[string]interface{}) (interface{}, error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("authentication required")
	}

	ctx := context.Background()

	// Get user ID from username (skip privilege check for service tokens - they always have access)
	if !h.userInfo.IsServiceToken {
		var userID int
		err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1",
			h.userInfo.Username).Scan(&userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}

		// Check MCP privilege
		//nolint:errcheck // Treating failure as "no access" is the correct security behavior
		hasAccess, _ := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "get_database_context")
		if !hasAccess {
			return nil, fmt.Errorf("access denied: you do not have permission to use get_database_context")
		}
	}

	// Get the context
	dbCtx := session.GetContext(h.userInfo.Username)
	if dbCtx == nil {
		return map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": "No database context is currently set.\n\nUse set_database_context to establish a working database before executing queries.",
				},
			},
		}, nil
	}

	message := fmt.Sprintf("Current database context:\n- Connection ID: %d\n", dbCtx.ConnectionID)
	if dbCtx.DatabaseName != "" {
		message += fmt.Sprintf("- Database Name: %s\n", dbCtx.DatabaseName)
	} else {
		message += "- Database Name: (using connection default)\n"
	}
	message += "\nAll execute_query calls without explicit connectionId will use this context."

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": message,
			},
		},
	}, nil
}

// handleClearDatabaseContext clears the current database context for the user's session
func (h *Handler) handleClearDatabaseContext(args map[string]interface{}) (interface{}, error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("authentication required")
	}

	ctx := context.Background()

	// Get user ID from username (skip privilege check for service tokens - they always have access)
	if !h.userInfo.IsServiceToken {
		var userID int
		err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1",
			h.userInfo.Username).Scan(&userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}

		// Check MCP privilege
		//nolint:errcheck // Treating failure as "no access" is the correct security behavior
		hasAccess, _ := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "clear_database_context")
		if !hasAccess {
			return nil, fmt.Errorf("access denied: you do not have permission to use clear_database_context")
		}
	}

	// Clear the context
	session.ClearContext(h.userInfo.Username)

	logger.Infof("User %s cleared database context", h.userInfo.Username)

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": "Database context cleared.\n\nYou must now explicitly specify connectionId and databaseName for execute_query calls.",
			},
		},
	}, nil
}

// handleQueryDatastore executes a query on the datastore containing historical metrics
func (h *Handler) handleQueryDatastore(args map[string]interface{}) (interface{}, error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("authentication required")
	}

	ctx := context.Background()

	// Get user ID from username (skip privilege check for service tokens - they always have access)
	if !h.userInfo.IsServiceToken {
		var userID int
		err := h.dbPool.QueryRow(ctx, "SELECT id FROM user_accounts WHERE username = $1",
			h.userInfo.Username).Scan(&userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user ID: %w", err)
		}

		// Check MCP privilege
		//nolint:errcheck // Treating failure as "no access" is the correct security behavior
		hasAccess, _ := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "query_datastore")
		if !hasAccess {
			return nil, fmt.Errorf("access denied: you do not have permission to use query_datastore")
		}
	}

	// Extract query
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return nil, fmt.Errorf("query is required and must be a non-empty string")
	}

	// Extract max rows (optional, default 1000, max 10000)
	maxRows := 1000
	if maxRowsFloat, ok := args["maxRows"].(float64); ok {
		maxRows = int(maxRowsFloat)
		if maxRows > 10000 {
			maxRows = 10000
		}
		if maxRows < 1 {
			maxRows = 1
		}
	}

	logger.Infof("User %s querying datastore with max rows %d", h.userInfo.Username, maxRows)

	// Execute query on the datastore (which is h.dbPool)
	// Start a read-only transaction
	tx, err := h.dbPool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		//nolint:errcheck // Rollback errors in defer are not critical
		tx.Rollback(ctx)
	}()

	// Set transaction to read-only
	if _, err := tx.Exec(ctx, "BEGIN READ ONLY"); err != nil {
		return nil, fmt.Errorf("failed to set read-only mode: %w", err)
	}

	// Execute the query
	rows, err := tx.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query execution failed: %w", err)
	}
	defer rows.Close()

	// Get column descriptions
	fieldDescriptions := rows.FieldDescriptions()
	columns := make([]string, len(fieldDescriptions))
	for i, fd := range fieldDescriptions {
		columns[i] = string(fd.Name)
	}

	// Fetch rows
	var resultRows [][]interface{}
	rowCount := 0
	truncated := false

	for rows.Next() {
		if rowCount >= maxRows {
			truncated = true
			break
		}

		values, err := rows.Values()
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		resultRows = append(resultRows, values)
		rowCount++
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	// Format response
	response := "Datastore Query Results\n"
	response += "======================\n\n"
	response += fmt.Sprintf("Query: %s\n\n", query)

	if truncated {
		response += fmt.Sprintf("⚠️  Results truncated to %d rows (use maxRows parameter to adjust)\n\n", maxRows)
	}

	// Format as JSON
	resultJSON, err := json.MarshalIndent(map[string]interface{}{
		"columns":   columns,
		"rows":      resultRows,
		"rowCount":  rowCount,
		"truncated": truncated,
	}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to format results: %w", err)
	}

	response += string(resultJSON)

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": response,
			},
		},
	}, nil
}

// handleReadResourceTool executes the read_resource tool
func (h *Handler) handleReadResourceTool(args map[string]interface{}) (interface{}, error) {
	// Check authentication
	if h.userInfo == nil || !h.userInfo.IsAuthenticated {
		return nil, fmt.Errorf("authentication required")
	}

	// Extract URI
	uri, ok := args["uri"].(string)
	if !ok || uri == "" {
		return nil, fmt.Errorf("uri is required and must be a non-empty string")
	}

	// Create a fake Request for handleReadResource
	paramsBytes, err := json.Marshal(map[string]interface{}{
		"uri": uri,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal params: %w", err)
	}

	req := Request{
		JSONRPC: JSONRPCVersion,
		Method:  "resources/read",
		Params:  paramsBytes,
		ID:      "tool-read-resource",
	}

	// Call handleReadResource
	resp, err := h.handleReadResource(req)
	if err != nil {
		return nil, fmt.Errorf("failed to read resource: %w", err)
	}

	// Check for error in response
	if resp.Error != nil {
		return nil, fmt.Errorf("resource read failed: %s", resp.Error.Message)
	}

	// Extract contents from the response
	if resultMap, ok := resp.Result.(map[string]interface{}); ok {
		if contents, ok := resultMap["contents"].([]map[string]interface{}); ok {
			// Format the contents as a readable text response
			var textParts []string
			for _, content := range contents {
				if text, ok := content["text"].(string); ok {
					textParts = append(textParts, text)
				}
			}

			responseText := strings.Join(textParts, "\n\n")
			if responseText == "" {
				responseText = fmt.Sprintf("Resource %s contains no data", uri)
			}

			return map[string]interface{}{
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": responseText,
					},
				},
			}, nil
		}
	}

	return nil, fmt.Errorf("unexpected response format from handleReadResource")
}

// FormatResponse formats a response as JSON
func FormatResponse(resp *Response) ([]byte, error) {
	data, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}
	return data, nil
}
