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

This document details the Model Context Protocol (MCP) implementation
in the pgEdge AI DBA Workbench server. For the complete tools and
resources catalog, see `mcp-tools-catalog.md`.

## Overview

The MCP server implements JSON-RPC 2.0 over HTTP/HTTPS with support
for POST requests and Server-Sent Events (SSE). It exposes tools,
resources, and prompts that AI assistants use to interact with
PostgreSQL databases.

**Protocol Version:** 2024-11-05

**Key Files:**

- `/server/src/mcp/protocol.go` - Protocol types and constructors
- `/server/src/mcp/handler.go` - Request routing and tool handlers
- `/server/src/server/server.go` - HTTP/SSE server
- `/server/src/privileges/privileges.go` - Authorization logic
- `/server/src/privileges/seed.go` - MCP privilege registration
- `/server/src/usermgmt/usermgmt.go` - User and token management
- `/server/src/groupmgmt/groupmgmt.go` - Group management

## JSON-RPC Foundation

### Request and Response Types

```go
type Request struct {
    JSONRPC string          `json:"jsonrpc"`  // Always "2.0"
    ID      interface{}     `json:"id,omitempty"`
    Method  string          `json:"method"`
    Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
    JSONRPC string      `json:"jsonrpc"`
    ID      interface{} `json:"id,omitempty"`
    Result  interface{} `json:"result,omitempty"`
    Error   *Error      `json:"error,omitempty"`
}

type Error struct {
    Code    int         `json:"code"`
    Message string      `json:"message"`
    Data    interface{} `json:"data,omitempty"`
}
```

### Error Codes

```go
const (
    ParseError     = -32700 // Invalid JSON
    InvalidRequest = -32600 // Invalid Request object
    MethodNotFound = -32601 // Method doesn't exist
    InvalidParams  = -32602 // Invalid parameters
    InternalError  = -32603 // Internal error
)
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
```

### Request Processing Flow

1. Parse JSON-RPC request from raw bytes.
2. Validate JSON-RPC version is "2.0".
3. Check whether the method requires authentication.
4. If authentication is required, validate the bearer token.
5. Route to the appropriate method handler.
6. Return JSON-RPC response.

```go
func (h *Handler) HandleRequest(
    data []byte, bearerToken string,
) (*Response, error)
```

### Method Routing

```go
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
    return NewErrorResponse(req.ID, MethodNotFound,
        "Method not found", nil), nil
}
```

## Transport Mechanisms

### HTTP POST (/mcp endpoint)

1. Client sends JSON-RPC request as POST body.
2. Server processes request synchronously.
3. Server returns JSON-RPC response.

### Server-Sent Events (/sse endpoint)

1. Client establishes SSE connection.
2. Server sends `event: connected` acknowledgment.
3. Client sends JSON-RPC requests as lines in body.
4. Server sends responses as `event: message` SSE messages.
5. Connection persists until client disconnects.

### HTTP Server Integration

```go
type Server struct {
    config     *config.Config
    mcpHandler *mcp.Handler
    httpServer *http.Server
}
```

The server extracts the bearer token from the `Authorization`
header and passes it to `HandleRequest()`.

## Authentication

### Token Transmission

All authenticated requests include a bearer token:

```http
POST /mcp HTTP/1.1
Authorization: Bearer <token-value>
Content-Type: application/json
```

### Authentication Exemptions

The following operations do not require authentication:

- The `initialize` method handles the protocol handshake.
- The `ping` method provides a health check endpoint.
- The `POST /api/auth/login` endpoint handles initial login.

### Token Validation

The `validateToken()` function checks tokens against two sources
in order: API tokens and session tokens.

```go
func (h *Handler) validateToken(
    token string,
) (*UserInfo, error)
```

**Source 1: API Tokens (unified `tokens` table)**

- Token is hashed via `usermgmt.HashPassword()`.
- Joined with `users` table to get owner identity.
- Expiration is checked if `expires_at` is set.
- Service accounts (`is_service_account = TRUE`) authenticate
  exclusively through API tokens.

**Source 2: User Sessions (`user_sessions` table)**

- Direct token comparison (not hashed).
- Joined with `users` table for superuser status.
- Default 24-hour expiration.
- Updates `last_used_at` on successful validation.

### UserInfo Structure

```go
type UserInfo struct {
    IsAuthenticated bool
    IsSuperuser     bool
    Username        string
    IsServiceToken  bool
}
```

### Authentication via HTTP API

**Endpoint:** `POST /api/auth/login`

**Request:**
```json
{
    "username": "alice",
    "password": "SecurePassword123!"
}
```

**Response:**
```json
{
    "success": true,
    "session_token": "<token>",
    "expires_at": "2024-11-15T09:30:00Z",
    "message": "Authentication successful"
}
```

### Test Mode

When `dbPool` is nil during unit tests, the server bypasses
authentication entirely, allowing tool logic testing without
database dependencies.

## Authorization

### Authorization Levels

1. **Unauthenticated** - `initialize`, `ping`, login only.
2. **Authenticated User** - Resource reads, self-service tokens.
3. **Privileged User** - Operations granted via group membership;
   checked by `privileges.CanAccessMCPItem()`.
4. **Superuser** - All operations; bypasses all privilege checks.
   Superuser status comes from the owning user account.

### Authorization Patterns

**Pattern 1: Superuser Only**

Token scope management tools use this pattern:

```go
if !h.userInfo.IsSuperuser {
    return nil, fmt.Errorf(
        "permission denied: superuser privileges required")
}
```

**Pattern 2: Superuser or Privilege Check**

User, token, group, and privilege management tools use this:

```go
if !h.userInfo.IsSuperuser {
    canAccess, err := privileges.CanAccessMCPItem(
        ctx, h.dbPool, userID, "tool_name")
    if err != nil || !canAccess {
        return nil, fmt.Errorf(
            "permission denied: insufficient privileges")
    }
}
```

**Pattern 3: Self-Service or Superuser**

User token management uses this pattern; users manage their own
tokens, while superusers manage anyone's tokens.

### Group-Based Privilege System

**Core tables:**

- `mcp_privilege_identifiers` - Registered privileges; seeded at
  startup via `privileges.SeedMCPPrivileges()`.
- `user_groups` - Named collections of users and nested groups.
- `group_memberships` - Links users/groups to parent groups;
  supports recursive resolution.
- `group_mcp_privileges` - Grants specific MCP privileges to
  groups; all members inherit them.

**Privilege Resolution via `CanAccessMCPItem()`:**

1. Return `true` if user is superuser.
2. Verify the privilege identifier exists.
3. Check whether any groups hold the privilege.
4. Use recursive CTE to resolve all user's groups.
5. Check whether any resolved group holds the privilege.

**Group Membership Resolution (recursive CTE):**

```sql
WITH RECURSIVE user_groups_recursive AS (
    SELECT parent_group_id AS group_id
    FROM group_memberships
    WHERE member_user_id = $1
    UNION
    SELECT gm.parent_group_id
    FROM group_memberships gm
    INNER JOIN user_groups_recursive ugr
        ON gm.member_group_id = ugr.group_id
)
SELECT DISTINCT group_id
FROM user_groups_recursive;
```

### Token Scoping

Token scoping provides restrictions beyond user privileges:

- **Connection scope** limits tokens to specific database
  connections (`token_connection_scope` table).
- **MCP scope** limits tokens to specific MCP tools
  (`token_mcp_scope` table).

Only superusers can manage token scopes.

## Extending the MCP Server

### Adding a New Tool (Checklist)

1. Register the privilege identifier in
   `/server/src/privileges/seed.go`.
2. Define the tool schema in `handleListTools()` in
   `/server/src/mcp/handler.go`.
3. Add routing case in `handleCallTool()`.
4. Implement the handler function.
5. Include authentication and authorization checks.
6. Write unit tests in `handler_test.go`.
7. Write integration tests in `/server/src/integration/`.
8. Update `mcp-tools-catalog.md`.

### Tool Handler Template

```go
func (h *Handler) handleNewTool(
    args map[string]interface{},
) (interface{}, error) {
    // 1. Check authentication
    if h.userInfo == nil || !h.userInfo.IsAuthenticated {
        return nil, fmt.Errorf("authentication required")
    }

    // 2. Parse arguments
    paramFloat, ok := args["param"].(float64)
    if !ok {
        return nil, fmt.Errorf("invalid param parameter")
    }
    param := int(paramFloat)

    // 3. Check authorization
    if !h.userInfo.IsSuperuser {
        ctx := context.Background()
        var userID int
        err := h.dbPool.QueryRow(ctx,
            "SELECT id FROM users WHERE username = $1",
            h.userInfo.Username).Scan(&userID)
        if err != nil {
            return nil, fmt.Errorf(
                "failed to get user ID: %w", err)
        }
        canAccess, err := privileges.CanAccessMCPItem(
            ctx, h.dbPool, userID, "new_tool")
        if err != nil || !canAccess {
            return nil, fmt.Errorf(
                "permission denied: insufficient privileges")
        }
    }

    // 4. Execute tool logic
    // ...

    // 5. Return result
    return map[string]interface{}{
        "content": []map[string]interface{}{
            {"type": "text", "text": "Success"},
        },
    }, nil
}
```

### Adding a New Resource

1. Define the resource in `handleListResources()`.
2. Add URI case to `handleReadResource()`.
3. Implement the query and formatting logic.
4. Ensure sensitive data (passwords, tokens) is excluded.
5. Write tests and update `mcp-tools-catalog.md`.

### Adding a New Prompt

1. Add the prompt definition to `handleListPrompts()`.
2. Implement `handleGetPrompt()` with a `prompts/get` route.
3. Use template strings with argument substitution.

### Modifying Existing Tools

- Adding optional parameters is safe for backwards compatibility.
- Adding required parameters is a breaking change; add as
  optional first, then require in a future version.
- Changing response format is a breaking change; prefer adding
  a new tool and deprecating the old one.

## Testing MCP Components

### Unit Test Pattern

```go
func TestHandleToolName(t *testing.T) {
    handler := NewHandler("TestServer", "1.0.0", nil, nil)
    handler.userInfo = &UserInfo{
        IsAuthenticated: true,
        IsSuperuser:     true,
        Username:        "testuser",
    }

    args := map[string]interface{}{
        "param": float64(1),
    }

    result, err := handler.handleToolName(args)
    if err != nil {
        t.Fatalf("handleToolName failed: %v", err)
    }

    resultMap, ok := result.(map[string]interface{})
    if !ok {
        t.Fatalf("Result is not a map")
    }
    // Verify content structure...
}
```

### Key Test Categories

- **Protocol tests** - Initialize, ping, invalid JSON, wrong
  version.
- **Authentication tests** - Missing token, expired token,
  invalid token.
- **Authorization tests** - Superuser access, non-superuser
  denied, privilege-granted access.
- **Tool handler tests** - Valid parameters, missing parameters,
  invalid types.
- **Integration tests** - Full request flow with real database.

### Integration Test Setup

```go
func setupTestDatabase(t *testing.T) (
    *pgxpool.Pool, *config.Config,
) {
    cfg := config.NewConfig()
    cfg.PgHost = os.Getenv("TEST_DB_HOST")
    // ... configure test database ...
    pool, err := database.Connect(cfg)
    if err != nil {
        t.Fatalf("Failed to connect: %v", err)
    }
    ctx := context.Background()
    privileges.SeedMCPPrivileges(ctx, pool)
    return pool, cfg
}
```

### Coverage Targets

- Overall: 80% minimum
- Critical paths (auth, authorization): 90%+
- Handler functions: 85%+
- Protocol functions: 90%+

### Running Tests

```bash
# Unit tests
cd server/src && go test ./mcp/...

# Integration tests
cd server/src && go test ./integration/... -v

# Coverage report
cd server/src && go test ./mcp/... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

## Best Practices

1. Always validate parameters before processing.
2. Always check authorization even if user is authenticated.
3. Use transactions for operations that modify multiple tables.
4. Return structured errors with context.
5. Log tool calls for audit trail.
6. Handle context cancellation for SSE connections.
7. Release database connections promptly.
8. Never leak sensitive data in error messages.
9. Use parameterized queries to prevent SQL injection.
10. Follow four-space indentation and include copyright headers.
