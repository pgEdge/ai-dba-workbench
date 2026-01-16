# MCP Protocol Implementation

This document describes how the Model Context Protocol (MCP) is implemented in the pgEdge AI DBA Workbench MCP server.

## Overview

The MCP server implements the JSON-RPC 2.0 specification over HTTP/HTTPS with support for both one-off POST requests and Server-Sent Events (SSE) for persistent connections.

**Key Files:**

- `/server/src/mcp/protocol.go` - Core protocol types and constructors
- `/server/src/mcp/handler.go` - Request routing and tool execution
- `/server/src/server/server.go` - HTTP/HTTPS server with SSE support
- `/server/src/main.go` - Application entry point

## Protocol Version

The server implements MCP protocol version **2024-11-05**.

## JSON-RPC Foundation

### Request Structure

```go
type Request struct {
    JSONRPC string          `json:"jsonrpc"`  // Always "2.0"
    ID      interface{}     `json:"id,omitempty"`
    Method  string          `json:"method"`
    Params  json.RawMessage `json:"params,omitempty"`
}
```

### Response Structure

```go
type Response struct {
    JSONRPC string      `json:"jsonrpc"`  // Always "2.0"
    ID      interface{} `json:"id,omitempty"`
    Result  interface{} `json:"result,omitempty"`
    Error   *Error      `json:"error,omitempty"`
}
```

### Error Structure

```go
type Error struct {
    Code    int         `json:"code"`
    Message string      `json:"message"`
    Data    interface{} `json:"data,omitempty"`
}
```

## Standard Error Codes

- `-32700` - Parse error (invalid JSON)
- `-32600` - Invalid request (not a valid Request object)
- `-32601` - Method not found
- `-32602` - Invalid method parameters
- `-32603` - Internal JSON-RPC error

## Supported MCP Methods

### Protocol Methods

1. **initialize** - Negotiate capabilities and exchange version info
2. **ping** - Health check endpoint

### Resource Methods

3. **resources/list** - List available resources
4. **resources/read** - Read resource contents

### Tool Methods

5. **tools/list** - List available tools
6. **tools/call** - Execute a tool

### Prompt Methods

7. **prompts/list** - List available prompts (currently empty)

## Request Flow

### 1. Request Reception

The server accepts requests via two endpoints:

- `/mcp` - One-off POST requests
- `/sse` - Server-Sent Events for persistent connections

Both endpoints extract the bearer token from the `Authorization` header:

```
Authorization: Bearer <token>
```

### 2. Request Parsing

```go
func (h *Handler) HandleRequest(data []byte, bearerToken string) (*Response, error)
```

- Unmarshals JSON to `Request` struct
- Validates JSON-RPC version is "2.0"
- Logs request method and ID

### 3. Authentication Check

Authentication is **required** for all methods except:

- `initialize`
- `ping`
- `POST /api/auth/login` (HTTP API, not MCP)

The authentication flow:

1. If `dbPool` is nil (unit test mode), skip authentication
2. If bearer token is empty for protected methods, return error
3. Call `validateToken()` to verify token against:
    - `service_tokens` table
    - `user_tokens` table
    - `user_sessions` table
4. Check token expiration
5. Populate `UserInfo` struct with authentication status

### 4. Method Routing

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
    return NewErrorResponse(req.ID, MethodNotFound, "Method not found", nil), nil
}
```

### 5. Response Formatting

The `FormatResponse()` function marshals the response to JSON for transmission.

## Initialize Handshake

The `initialize` method is the first call made by MCP clients:

**Request:**
```json
{
    "jsonrpc": "2.0",
    "id": "1",
    "method": "initialize",
    "params": {
        "protocolVersion": "2024-11-05",
        "capabilities": {},
        "clientInfo": {
            "name": "Client Name",
            "version": "1.0.0"
        }
    }
}
```

**Response:**
```json
{
    "jsonrpc": "2.0",
    "id": "1",
    "result": {
        "protocolVersion": "2024-11-05",
        "capabilities": {},
        "serverInfo": {
            "name": "pgEdge AI DBA Workbench MCP Server",
            "version": "0.1.0"
        }
    }
}
```

The handler tracks initialization state via the `initialized` boolean field.

## Transport Mechanisms

### HTTP POST (/mcp endpoint)

1. Client sends JSON-RPC request as POST body
2. Server processes request
3. Server returns JSON-RPC response synchronously

### Server-Sent Events (/sse endpoint)

1. Client establishes SSE connection
2. Server sends connection acknowledgment:
   ```
   event: connected
   data: {"status":"connected"}
   ```
3. Client sends JSON-RPC requests as lines in the request body
4. Server sends responses as SSE messages:
   ```
   event: message
   data: <JSON-RPC response>
   ```
5. Connection persists until client disconnects or context is canceled

## Token Validation

The `validateToken()` method performs multi-source authentication:

```go
func (h *Handler) validateToken(token string) (*UserInfo, error)
```

**Token Sources (checked in order):**

1. **Service Tokens** (`service_tokens` table)
    - Hash token and compare against `token_hash`
    - Check `expires_at` if set
    - Return superuser status

2. **User Tokens** (`user_tokens` table)
    - Hash token and compare against `token_hash`
    - Join with `user_accounts` to get username and superuser status
    - Check `expires_at` if set

3. **User Sessions** (`user_sessions` table)
    - Direct token comparison (not hashed)
    - Join with `user_accounts` to get superuser status
    - Check `expires_at`
    - Update `last_used_at` timestamp on successful validation

**UserInfo Structure:**
```go
type UserInfo struct {
    IsAuthenticated bool
    IsSuperuser     bool
    Username        string      // Empty for service tokens
    IsServiceToken  bool
}
```

## Error Handling Best Practices

1. **Always validate input parameters** before processing
2. **Use appropriate error codes** from JSON-RPC spec
3. **Include descriptive error messages** in the `message` field
4. **Optionally include error details** in the `data` field
5. **Log errors** with appropriate severity levels
6. **Never expose sensitive information** in error messages

Example error response:
```go
return NewErrorResponse(req.ID, InvalidParams,
    "Invalid parameters", err.Error()), nil
```

## Security Considerations

1. **Token transmission** - Always use HTTPS in production
2. **Token storage** - Service and user tokens are hashed in database
3. **Session tokens** - Stored unhashed but with expiration
4. **Input validation** - All request parameters are validated
5. **SQL injection protection** - Uses parameterized queries via pgx
6. **Authentication bypass** - Only allowed for initialize, ping, and /api/auth/login

## Performance Optimizations

1. **Connection pooling** - Uses pgxpool for database connections
2. **Efficient JSON parsing** - Uses json.RawMessage for deferred parsing
3. **Context awareness** - Respects context cancellation for SSE
4. **Read/write timeouts** - Configured on HTTP server

## Testing the Protocol

Basic protocol test example:

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
        t.Errorf("Wrong protocol version")
    }
}
```

## Extending the Protocol

To add new MCP methods:

1. **Define the method handler** in handler.go
2. **Add routing case** in HandleRequest switch statement
3. **Define request/response types** if complex
4. **Add authentication logic** if required
5. **Register in privilege system** if it's a protected tool
6. **Write unit tests** for the new method
7. **Update this documentation**
