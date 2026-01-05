/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - MCP Protocol Implementation
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

# MCP Protocol Implementation

This document details the Model Context Protocol (MCP) implementation in the
pgEdge AI DBA Workbench server.

## Overview

The MCP server implements JSON-RPC 2.0 protocol to provide AI models with
structured access to PostgreSQL databases. It exposes tools, resources, and
prompts that can be used by AI assistants.

## Protocol Basics

### JSON-RPC 2.0 Structure

**Request:**
```json
{
    "jsonrpc": "2.0",
    "id": "request-123",
    "method": "tools/call",
    "params": {
        "name": "execute_query",
        "arguments": {
            "connection_id": 1,
            "query": "SELECT version()"
        }
    }
}
```

**Success Response:**
```json
{
    "jsonrpc": "2.0",
    "id": "request-123",
    "result": {
        "content": [
            {
                "type": "text",
                "text": "Query result: ..."
            }
        ]
    }
}
```

**Error Response:**
```json
{
    "jsonrpc": "2.0",
    "id": "request-123",
    "error": {
        "code": -32602,
        "message": "Invalid parameters",
        "data": "connection_id is required"
    }
}
```

## Handler Architecture

### Core Handler Structure

```go
type Handler struct {
    serverName    string
    serverVersion string
    initialized   bool
    dbPool        *pgxpool.Pool
    config        *config.Config
    userInfo      *UserInfo
}

func NewHandler(
    serverName, serverVersion string,
    dbPool *pgxpool.Pool,
    cfg *config.Config,
) *Handler {
    return &Handler{
        serverName:    serverName,
        serverVersion: serverVersion,
        initialized:   false,
        dbPool:        dbPool,
        config:        cfg,
    }
}
```

### Request Processing Flow

```go
func (h *Handler) HandleRequest(
    data []byte,
    bearerToken string,
) (*Response, error) {
    // 1. Parse JSON-RPC request
    var req Request
    if err := json.Unmarshal(data, &req); err != nil {
        return NewErrorResponse(nil, ParseError, "Parse error", err.Error()), nil
    }

    // 2. Validate JSON-RPC version
    if req.JSONRPC != JSONRPCVersion {
        return NewErrorResponse(req.ID, InvalidRequest,
            "Invalid JSON-RPC version", nil), nil
    }

    // 3. Authenticate (if required)
    requiresAuth := h.methodRequiresAuth(req.Method, req.Params)
    if requiresAuth {
        userInfo, err := h.validateToken(bearerToken)
        if err != nil || userInfo == nil || !userInfo.IsAuthenticated {
            return NewErrorResponse(req.ID, InvalidRequest,
                "Authentication required", nil), nil
        }
        h.userInfo = userInfo
    }

    // 4. Route to handler
    return h.routeRequest(req)
}
```

## Protocol Methods

### 1. initialize

Establishes the protocol session and exchanges capabilities.

**Request:**
```go
type InitializeParams struct {
    ProtocolVersion string                 `json:"protocolVersion"`
    Capabilities    map[string]interface{} `json:"capabilities"`
    ClientInfo      ClientInfo             `json:"clientInfo"`
}

type ClientInfo struct {
    Name    string `json:"name"`
    Version string `json:"version"`
}
```

**Response:**
```go
type InitializeResult struct {
    ProtocolVersion string                 `json:"protocolVersion"`
    Capabilities    map[string]interface{} `json:"capabilities"`
    ServerInfo      ServerInfo             `json:"serverInfo"`
}

type ServerInfo struct {
    Name    string `json:"name"`
    Version string `json:"version"`
}
```

**Implementation:**
```go
func (h *Handler) handleInitialize(req Request) (*Response, error) {
    var params InitializeParams
    if err := json.Unmarshal(req.Params, &params); err != nil {
        return NewErrorResponse(req.ID, InvalidParams,
            "Invalid parameters", err.Error()), nil
    }

    h.initialized = true

    result := InitializeResult{
        ProtocolVersion: "2024-11-05",
        Capabilities:    make(map[string]interface{}),
        ServerInfo: ServerInfo{
            Name:    h.serverName,
            Version: h.serverVersion,
        },
    }

    return NewResponse(req.ID, result), nil
}
```

**Authentication:** Not required

### 2. ping

Health check endpoint.

**Request:** No parameters

**Response:**
```json
{
    "status": "ok"
}
```

**Authentication:** Not required

### 3. resources/list

Lists available resources (users, tokens, connections).

**Response:**
```json
{
    "resources": [
        {
            "uri": "ai-workbench://users",
            "name": "User Accounts",
            "description": "List of all user accounts in the system",
            "mimeType": "application/json"
        }
    ]
}
```

**Authentication:** Required

### 4. resources/read

Retrieves resource data.

**Request:**
```json
{
    "uri": "ai-workbench://users"
}
```

**Response:**
```json
{
    "contents": [
        {
            "uri": "ai-workbench://users",
            "mimeType": "application/json",
            "text": "[{\"username\":\"admin\",...}]"
        }
    ]
}
```

**Authentication:** Required

**Authorization:** Requires privilege on the resource URI

### 5. tools/list

Lists available MCP tools.

**Response:**
```json
{
    "tools": [
        {
            "name": "execute_query",
            "description": "Execute a SQL query on a connection",
            "inputSchema": {
                "type": "object",
                "properties": {
                    "connection_id": {
                        "type": "integer",
                        "description": "The connection ID"
                    },
                    "query": {
                        "type": "string",
                        "description": "SQL query to execute"
                    }
                },
                "required": ["connection_id", "query"]
            }
        }
    ]
}
```

**Authentication:** Required

**Authorization:** Only tools the user has privileges for are returned

### 6. tools/call

Executes an MCP tool.

**Request:**
```json
{
    "name": "execute_query",
    "arguments": {
        "connection_id": 1,
        "query": "SELECT version()"
    }
}
```

**Response:**
```json
{
    "content": [
        {
            "type": "text",
            "text": "Query executed successfully. Results: [...]"
        }
    ]
}
```

**Authentication:** Required (except for `authenticate_user` tool)

**Authorization:** Checked per-tool

## MCP Tools

### Tool Definition Pattern

Each tool follows this structure:

```go
func (h *Handler) handleToolExecuteQuery(params map[string]interface{}) (
    *Response, error) {
    // 1. Validate parameters
    connectionID, err := validateIntParam(params, "connection_id")
    if err != nil {
        return nil, err
    }

    query, err := validateStringParam(params, "query")
    if err != nil {
        return nil, err
    }

    // 2. Check authorization
    canAccess, err := privileges.CanAccessConnection(
        ctx, h.dbPool, h.userInfo.UserID, connectionID,
        privileges.AccessLevelRead)
    if err != nil {
        return nil, fmt.Errorf("failed to check access: %w", err)
    }
    if !canAccess {
        return nil, fmt.Errorf("access denied to connection %d", connectionID)
    }

    // 3. Execute operation
    result, err := h.executeQueryOnConnection(connectionID, query)
    if err != nil {
        return nil, fmt.Errorf("query execution failed: %w", err)
    }

    // 4. Format response
    return h.formatToolResponse(result), nil
}
```

### Available Tools

**Database Operations:**
- `execute_query` - Execute arbitrary SQL
- `list_databases` - List databases in a connection
- `list_schemas` - List schemas in a database
- `list_tables` - List tables in a schema
- `describe_table` - Get table structure

**Connection Management:**
- `test_connection` - Test database connectivity
- `list_connections` - List available connections
- `get_connection` - Get connection details
- `create_connection` - Create new connection
- `update_connection` - Update connection
- `delete_connection` - Delete connection

**User Management:**
- `authenticate_user` - Authenticate and get token
- `create_user` - Create user account
- `update_user` - Update user account
- `delete_user` - Delete user account
- `list_users` - List user accounts

**Token Management:**
- `create_service_token` - Create service token
- `update_service_token` - Update service token
- `delete_service_token` - Delete service token
- `list_service_tokens` - List service tokens
- `create_user_token` - Create user token
- `list_user_tokens` - List user tokens
- `delete_user_token` - Delete user token

**Group Management:**
- `create_group` - Create group
- `update_group` - Update group
- `delete_group` - Delete group
- `list_groups` - List groups
- `add_group_member` - Add member to group
- `remove_group_member` - Remove member from group

**Privilege Management:**
- `grant_connection_access` - Grant group access to connection
- `revoke_connection_access` - Revoke group access to connection
- `grant_mcp_privilege` - Grant MCP privilege to group
- `revoke_mcp_privilege` - Revoke MCP privilege from group

## Authentication Flow

### Token Validation

```go
type UserInfo struct {
    UserID          int
    Username        string
    IsSuperuser     bool
    IsAuthenticated bool
    TokenType       string // "password", "service", "user"
}

func (h *Handler) validateToken(bearerToken string) (*UserInfo, error) {
    if bearerToken == "" {
        return nil, fmt.Errorf("no bearer token provided")
    }

    // Hash the provided token
    tokenHash := usermgmt.HashPassword(bearerToken)

    ctx := context.Background()

    // Try service token first
    var id int
    var name string
    var isSuperuser bool
    var expiresAt sql.NullTime
    err := h.dbPool.QueryRow(ctx, `
        SELECT id, name, is_superuser, expires_at
        FROM service_tokens
        WHERE token_hash = $1
    `, tokenHash).Scan(&id, &name, &isSuperuser, &expiresAt)

    if err == nil {
        // Check expiry
        if expiresAt.Valid && time.Now().After(expiresAt.Time) {
            return nil, fmt.Errorf("service token expired")
        }

        return &UserInfo{
            UserID:          id,
            Username:        name,
            IsSuperuser:     isSuperuser,
            IsAuthenticated: true,
            TokenType:       "service",
        }, nil
    }

    // Try user password next
    err = h.dbPool.QueryRow(ctx, `
        SELECT id, username, is_superuser, password_expiry
        FROM user_accounts
        WHERE password_hash = $1
    `, tokenHash).Scan(&id, &name, &isSuperuser, &expiresAt)

    if err == nil {
        // Check expiry
        if expiresAt.Valid && time.Now().After(expiresAt.Time) {
            return nil, fmt.Errorf("password expired")
        }

        return &UserInfo{
            UserID:          id,
            Username:        name,
            IsSuperuser:     isSuperuser,
            IsAuthenticated: true,
            TokenType:       "password",
        }, nil
    }

    // Try user token last
    err = h.dbPool.QueryRow(ctx, `
        SELECT ut.id, ua.username, ua.is_superuser, ut.expires_at
        FROM user_tokens ut
        JOIN user_accounts ua ON ut.user_id = ua.id
        WHERE ut.token_hash = $1
    `, tokenHash).Scan(&id, &name, &isSuperuser, &expiresAt)

    if err == nil {
        // Check expiry
        if expiresAt.Valid && time.Now().After(expiresAt.Time) {
            return nil, fmt.Errorf("user token expired")
        }

        return &UserInfo{
            UserID:          id,
            Username:        name,
            IsSuperuser:     isSuperuser,
            IsAuthenticated: true,
            TokenType:       "user",
        }, nil
    }

    return nil, fmt.Errorf("invalid token")
}
```

### Special Case: authenticate_user Tool

The `authenticate_user` tool is the only tool that doesn't require a bearer
token. It accepts username/password and returns a user token.

```go
func (h *Handler) handleToolAuthenticateUser(params map[string]interface{}) (
    *Response, error) {
    username, err := validateStringParam(params, "username")
    if err != nil {
        return nil, err
    }

    password, err := validateStringParam(params, "password")
    if err != nil {
        return nil, err
    }

    // Validate credentials
    passwordHash := usermgmt.HashPassword(password)

    var userID int
    var isSuperuser bool
    err = h.dbPool.QueryRow(ctx, `
        SELECT id, is_superuser
        FROM user_accounts
        WHERE username = $1 AND password_hash = $2
    `, username, passwordHash).Scan(&userID, &isSuperuser)

    if err != nil {
        return nil, fmt.Errorf("invalid credentials")
    }

    // Create user token
    message, token, err := usermgmt.CreateUserTokenNonInteractive(
        h.dbPool, username, nil, 90, h.config.MaxUserTokenLifetimeDays, nil)
    if err != nil {
        return nil, fmt.Errorf("failed to create token: %w", err)
    }

    return h.formatToolResponse(map[string]interface{}{
        "message": message,
        "token":   token,
    }), nil
}
```

## Authorization Flow

See `authentication-flow.md` for detailed RBAC implementation.

## Error Handling

### Error Codes

Following JSON-RPC 2.0 specification:

```go
const (
    ParseError     = -32700 // Invalid JSON
    InvalidRequest = -32600 // Invalid Request object
    MethodNotFound = -32601 // Method doesn't exist
    InvalidParams  = -32602 // Invalid parameters
    InternalError  = -32603 // Internal error
)
```

### Error Response Pattern

```go
func (h *Handler) handleToolCall(req Request) (*Response, error) {
    var params struct {
        Name      string                 `json:"name"`
        Arguments map[string]interface{} `json:"arguments"`
    }

    if err := json.Unmarshal(req.Params, &params); err != nil {
        return NewErrorResponse(req.ID, InvalidParams,
            "Invalid parameters", err.Error()), nil
    }

    // Route to tool handler
    result, err := h.routeToolCall(params.Name, params.Arguments)
    if err != nil {
        // Convert application error to MCP error
        return NewErrorResponse(req.ID, InternalError,
            "Tool execution failed", err.Error()), nil
    }

    return NewResponse(req.ID, result), nil
}
```

## HTTP Server Integration

The MCP handler is integrated with an HTTP server:

```go
type Server struct {
    config     *config.Config
    mcpHandler *mcp.Handler
    httpServer *http.Server
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    // Extract bearer token
    bearerToken := ""
    authHeader := r.Header.Get("Authorization")
    if strings.HasPrefix(authHeader, "Bearer ") {
        bearerToken = strings.TrimPrefix(authHeader, "Bearer ")
    }

    // Read request body
    body, err := io.ReadAll(r.Body)
    if err != nil {
        http.Error(w, "Failed to read request", http.StatusBadRequest)
        return
    }

    // Handle MCP request
    resp, err := s.mcpHandler.HandleRequest(body, bearerToken)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    // Write response
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(resp)
}
```

## Testing

### Unit Testing

Mock the database pool for handler testing:

```go
func TestHandleInitialize(t *testing.T) {
    handler := NewHandler("TestServer", "1.0.0", nil, nil)

    reqData := []byte(`{
        "jsonrpc": "2.0",
        "id": "test-1",
        "method": "initialize",
        "params": {}
    }`)

    resp, err := handler.HandleRequest(reqData, "")
    if err != nil {
        t.Fatalf("HandleRequest failed: %v", err)
    }

    if resp.Error != nil {
        t.Errorf("Expected no error, got: %v", resp.Error)
    }

    result, ok := resp.Result.(InitializeResult)
    if !ok {
        t.Fatalf("Result is not InitializeResult")
    }

    if result.ProtocolVersion != "2024-11-05" {
        t.Errorf("ProtocolVersion = %v, want 2024-11-05",
            result.ProtocolVersion)
    }
}
```

### Integration Testing

See `testing-strategy.md` for integration test patterns.

## Best Practices

1. **Always Validate Parameters:** Check required fields and types
2. **Always Check Authorization:** Even if user is authenticated
3. **Use Transactions:** For operations that modify multiple tables
4. **Return Structured Errors:** Include context in error messages
5. **Log Operations:** Log tool calls and results for audit trail
6. **Handle Context Cancellation:** Respect request cancellation
7. **Release Resources:** Always release database connections
8. **Sanitize Output:** Don't leak sensitive data in error messages
