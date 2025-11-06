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
	"github.com/pgEdge/ai-workbench/server/src/logger"
	"github.com/pgEdge/ai-workbench/server/src/usermgmt"
)

// Handler processes MCP requests
type Handler struct {
	serverName    string
	serverVersion string
	initialized   bool
	dbPool        *pgxpool.Pool
}

// NewHandler creates a new MCP handler
func NewHandler(serverName, serverVersion string, dbPool *pgxpool.Pool) *Handler {
	return &Handler{
		serverName:    serverName,
		serverVersion: serverVersion,
		initialized:   false,
		dbPool:        dbPool,
	}
}

// HandleRequest processes an MCP request and returns a response
func (h *Handler) HandleRequest(data []byte) (*Response, error) {
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
	username, _ := args["username"].(string)
	email, _ := args["email"].(string)
	fullName, _ := args["fullName"].(string)
	password, _ := args["password"].(string)
	isSuperuser, _ := args["isSuperuser"].(bool)

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
	username, _ := args["username"].(string)

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
	username, _ := args["username"].(string)

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
	name, _ := args["name"].(string)
	isSuperuser, _ := args["isSuperuser"].(bool)

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
	name, _ := args["name"].(string)

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
	name, _ := args["name"].(string)

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

// FormatResponse formats a response as JSON
func FormatResponse(resp *Response) ([]byte, error) {
	data, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}
	return data, nil
}
