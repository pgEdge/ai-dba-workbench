# Extending the MCP Server

This guide provides step-by-step instructions for adding new capabilities to the MCP server.

## Table of Contents

1. [Adding a New Tool](#adding-a-new-tool)
2. [Adding a New Resource](#adding-a-new-resource)
3. [Adding a New Prompt](#adding-a-new-prompt)
4. [Modifying Existing Tools](#modifying-existing-tools)
5. [Testing Your Changes](#testing-your-changes)

---

## Adding a New Tool

Tools are the primary way to add functionality to the MCP server. They execute actions and return results.

### Step 1: Design Your Tool

Before coding, define:

1. **Tool name** - Should be descriptive and follow snake_case convention
2. **Purpose** - Clear one-line description
3. **Input schema** - Required and optional parameters with types
4. **Authorization requirements** - Who can execute this tool?
5. **Output format** - What will the tool return?

**Example Design:**

```
Name: export_database_schema
Purpose: Export the schema of a database connection as SQL DDL
Input:
  - connectionId (integer, required) - ID of the connection
  - includeData (boolean, optional) - Include sample data
Authorization: User must have read access to the connection
Output: SQL DDL as text
```

### Step 2: Register the Privilege Identifier

Add your tool to the privilege system in `/server/src/privileges/seed.go`:

```go
func GetDefaultMCPPrivileges() []MCPPrivilege {
    return []MCPPrivilege{
        // ... existing privileges ...

        // Your new tool
        {
            Identifier:  "export_database_schema",
            ItemType:    "tool",
            Description: "Export the schema of a database connection as SQL DDL",
        },
    }
}
```

**Important:** This makes the tool available for privilege assignment.

### Step 3: Define the Tool in handleListTools

Add your tool definition to `/server/src/mcp/handler.go` in the `handleListTools()` function:

```go
func (h *Handler) handleListTools(req Request) (*Response, error) {
    tools := []map[string]interface{}{
        // ... existing tools ...

        {
            "name":        "export_database_schema",
            "description": "Export the schema of a database connection as SQL DDL",
            "inputSchema": map[string]interface{}{
                "type": "object",
                "properties": map[string]interface{}{
                    "connectionId": map[string]interface{}{
                        "type":        "integer",
                        "description": "ID of the database connection",
                    },
                    "includeData": map[string]interface{}{
                        "type":        "boolean",
                        "description": "Include sample data in export",
                        "default":     false,
                    },
                },
                "required": []string{"connectionId"},
            },
        },
    }
    // ...
}
```

**Note:** Follow JSON Schema specification for the `inputSchema`.

### Step 4: Add Tool Routing

Add a case to the switch statement in `handleCallTool()`:

```go
func (h *Handler) handleCallTool(req Request) (*Response, error) {
    // ... parameter unmarshaling ...

    switch params.Name {
    // ... existing cases ...

    case "export_database_schema":
        result, err = h.handleExportDatabaseSchema(params.Arguments)

    default:
        // ... error handling ...
    }
    // ...
}
```

### Step 5: Implement the Tool Handler

Create the handler function in `/server/src/mcp/handler.go`:

```go
// handleExportDatabaseSchema executes the export_database_schema tool
func (h *Handler) handleExportDatabaseSchema(args map[string]interface{}) (interface{}, error) {
    // 1. CHECK AUTHENTICATION
    if h.userInfo == nil || !h.userInfo.IsAuthenticated {
        return nil, fmt.Errorf("authentication required")
    }

    // 2. PARSE ARGUMENTS
    connectionIDFloat, ok := args["connectionId"].(float64)
    if !ok {
        return nil, fmt.Errorf("invalid connectionId parameter")
    }
    connectionID := int(connectionIDFloat)

    includeData := false
    if val, ok := args["includeData"].(bool); ok {
        includeData = val
    }

    // 3. CHECK AUTHORIZATION
    // For connection-based operations, check connection access
    if !h.userInfo.IsSuperuser {
        if h.dbPool == nil {
            return nil, fmt.Errorf("permission denied")
        }

        // Get user ID
        ctx := context.Background()
        var userID int
        err := h.dbPool.QueryRow(ctx,
            "SELECT id FROM user_accounts WHERE username = $1",
            h.userInfo.Username).Scan(&userID)
        if err != nil {
            return nil, fmt.Errorf("failed to get user ID: %w", err)
        }

        // Check connection access
        canAccess, err := privileges.CanAccessConnection(ctx, h.dbPool,
            userID, connectionID, privileges.AccessLevelRead)
        if err != nil || !canAccess {
            return nil, fmt.Errorf("permission denied: insufficient access to connection")
        }
    }

    // 4. EXECUTE TOOL LOGIC
    ctx := context.Background()

    // Get connection details from database
    var host, database, username string
    var port int
    err := h.dbPool.QueryRow(ctx, `
        SELECT host, port, database, username
        FROM connections
        WHERE id = $1
    `, connectionID).Scan(&host, &port, &database, &username)
    if err != nil {
        return nil, fmt.Errorf("failed to get connection details: %w", err)
    }

    // Perform schema export (implementation depends on your needs)
    schema, err := exportSchema(host, port, database, username, includeData)
    if err != nil {
        return nil, fmt.Errorf("failed to export schema: %w", err)
    }

    // 5. RETURN RESULT
    return map[string]interface{}{
        "content": []map[string]interface{}{
            {
                "type": "text",
                "text": schema,
            },
        },
    }, nil
}
```

### Step 6: Implement Helper Functions

If your tool needs helper functions, add them to appropriate packages:

```go
// In a separate package like /server/src/schema/export.go
package schema

func ExportSchema(host string, port int, database string,
    username string, includeData bool) (string, error) {
    // Connect to database
    // Query information_schema or pg_catalog
    // Generate DDL statements
    // Optionally include sample data
    // Return SQL as string
}
```

### Step 7: Write Unit Tests

Create tests in `/server/src/mcp/handler_test.go`:

```go
func TestHandleExportDatabaseSchema(t *testing.T) {
    handler := NewHandler("TestServer", "1.0.0", nil, nil)

    // Mock userInfo for authentication
    handler.userInfo = &UserInfo{
        IsAuthenticated: true,
        IsSuperuser:     true,
        Username:        "testuser",
        IsServiceToken:  false,
    }

    args := map[string]interface{}{
        "connectionId": float64(1),
        "includeData":  false,
    }

    result, err := handler.handleExportDatabaseSchema(args)
    if err != nil {
        t.Fatalf("handleExportDatabaseSchema failed: %v", err)
    }

    // Verify result structure
    resultMap, ok := result.(map[string]interface{})
    if !ok {
        t.Fatalf("Result is not a map")
    }

    content, ok := resultMap["content"].([]map[string]interface{})
    if !ok || len(content) == 0 {
        t.Fatalf("Invalid content structure")
    }

    if content[0]["type"] != "text" {
        t.Errorf("Expected text content")
    }
}
```

### Step 8: Test Authorization

Add authorization tests:

```go
func TestExportDatabaseSchemaUnauthorized(t *testing.T) {
    handler := NewHandler("TestServer", "1.0.0", nil, nil)

    // No authentication
    handler.userInfo = nil

    args := map[string]interface{}{
        "connectionId": float64(1),
    }

    _, err := handler.handleExportDatabaseSchema(args)
    if err == nil {
        t.Errorf("Expected authentication error")
    }
    if !strings.Contains(err.Error(), "authentication required") {
        t.Errorf("Wrong error message: %v", err)
    }
}
```

### Step 9: Write Integration Tests

Create integration tests in `/server/src/integration/`:

```go
// export_schema_integration_test.go
func TestExportSchemaIntegration(t *testing.T) {
    // Set up test database
    // Create test connection
    // Authenticate as test user
    // Call export_database_schema tool
    // Verify schema output
    // Clean up
}
```

### Step 10: Update Documentation

Add your tool to `/Users/dpage/git/ai-workbench/.claude/mcp-expert/tools-catalog.md`:

```markdown
### export_database_schema

**Purpose:** Export the schema of a database connection as SQL DDL

**Authorization:** User must have read access to the connection

**Input Schema:**
...
```

---

## Adding a New Resource

Resources provide read-only access to server data.

### Step 1: Design Your Resource

Define:

1. **URI scheme** - Follow `ai-workbench://<collection>` pattern
2. **Resource name** - Human-readable name
3. **Description** - What data does this resource provide?
4. **MIME type** - Usually `application/json`
5. **Data structure** - What fields will be exposed?

### Step 2: Add to Resource List

In `/server/src/mcp/handler.go`, add to `handleListResources()`:

```go
func (h *Handler) handleListResources(req Request) (*Response, error) {
    resources := []map[string]interface{}{
        // ... existing resources ...

        {
            "uri":         "ai-workbench://connections",
            "name":        "Database Connections",
            "description": "List of all database connections (metadata only)",
            "mimeType":    "application/json",
        },
    }
    // ...
}
```

### Step 3: Implement Resource Reading

Add a case to `handleReadResource()`:

```go
func (h *Handler) handleReadResource(req Request) (*Response, error) {
    // ... parameter parsing ...

    switch params.URI {
    // ... existing cases ...

    case "ai-workbench://connections":
        rows, err := h.dbPool.Query(ctx, `
            SELECT id, name, host, port, database, username,
                   is_shared, created_at, updated_at
            FROM connections
            ORDER BY name
        `)
        if err != nil {
            logger.Errorf("Failed to query connections: %v", err)
            return NewErrorResponse(req.ID, InternalError,
                "Failed to query connections", err.Error()), nil
        }
        defer rows.Close()

        for rows.Next() {
            var id, port int
            var name, host, database, username string
            var isShared bool
            var createdAt, updatedAt interface{}

            if err := rows.Scan(&id, &name, &host, &port, &database,
                &username, &isShared, &createdAt, &updatedAt); err != nil {
                logger.Errorf("Failed to scan connection: %v", err)
                continue
            }

            // DO NOT include password in resource!
            contents = append(contents, map[string]interface{}{
                "uri":      fmt.Sprintf("ai-workbench://connections/%d", id),
                "mimeType": "application/json",
                "text": fmt.Sprintf(`{"id": %d, "name": %q, "host": %q, "port": %d, "database": %q, "username": %q, "isShared": %t, "createdAt": %v, "updatedAt": %v}`,
                    id, name, host, port, database, username, isShared,
                    createdAt, updatedAt),
            })
        }

    default:
        return NewErrorResponse(req.ID, InvalidParams,
            "Unknown resource URI", nil), nil
    }
    // ...
}
```

### Step 4: Security Review

**Critical:** Ensure sensitive data is not exposed:

- NO passwords or credentials
- NO API keys or tokens
- NO personally identifiable information (unless authorized)
- Consider field-level access control

### Step 5: Write Tests

```go
func TestHandleReadConnectionsResource(t *testing.T) {
    // Test requires database integration
    // Set up test connections
    // Read resource
    // Verify structure
    // Verify passwords are NOT included
    // Clean up
}
```

### Step 6: Update Documentation

Add to `/Users/dpage/git/ai-workbench/.claude/mcp-expert/resources-catalog.md`.

---

## Adding a New Prompt

Prompts are pre-defined templates that LLM clients can use.

### Step 1: Design Your Prompt

Define:

1. **Prompt name** - Descriptive identifier
2. **Description** - What does this prompt help with?
3. **Arguments** - What parameters does the prompt accept?
4. **Template** - The prompt text with placeholders

### Step 2: Add to Prompt List

Currently, `handleListPrompts()` returns an empty list. To add prompts:

```go
func (h *Handler) handleListPrompts(req Request) (*Response, error) {
    prompts := []map[string]interface{}{
        {
            "name":        "analyze_query_performance",
            "description": "Help analyze a slow SQL query",
            "arguments": []map[string]interface{}{
                {
                    "name":        "query",
                    "description": "The SQL query to analyze",
                    "required":    true,
                },
                {
                    "name":        "executionTime",
                    "description": "Current execution time in milliseconds",
                    "required":    false,
                },
            },
        },
    }

    result := map[string]interface{}{
        "prompts": prompts,
    }
    logger.Info("Listed prompts")
    return NewResponse(req.ID, result), nil
}
```

### Step 3: Implement Prompt Rendering

Add a new method `prompts/get` to your protocol implementation:

```go
case "prompts/get":
    return h.handleGetPrompt(req)
```

Then implement the handler:

```go
func (h *Handler) handleGetPrompt(req Request) (*Response, error) {
    var params struct {
        Name      string                 `json:"name"`
        Arguments map[string]interface{} `json:"arguments"`
    }
    if err := json.Unmarshal(req.Params, &params); err != nil {
        return NewErrorResponse(req.ID, InvalidParams,
            "Invalid parameters", err.Error()), nil
    }

    switch params.Name {
    case "analyze_query_performance":
        query, _ := params.Arguments["query"].(string)
        execTime, _ := params.Arguments["executionTime"].(string)

        promptText := fmt.Sprintf(`Analyze this SQL query for performance issues:

Query:
%s

Current execution time: %s ms

Please identify:
1. Potential indexing improvements
2. Query structure inefficiencies
3. Recommended optimizations
`, query, execTime)

        result := map[string]interface{}{
            "description": "Analysis of query performance",
            "messages": []map[string]interface{}{
                {
                    "role":    "user",
                    "content": map[string]interface{}{
                        "type": "text",
                        "text": promptText,
                    },
                },
            },
        }
        return NewResponse(req.ID, result), nil

    default:
        return NewErrorResponse(req.ID, MethodNotFound,
            "Prompt not found", nil), nil
    }
}
```

---

## Modifying Existing Tools

### Safe Modification Process

1. **Review existing tests** - Understand current behavior
2. **Add tests for new behavior** - Test-driven development
3. **Make minimal changes** - Reduce risk of breaking changes
4. **Update documentation** - Keep it in sync
5. **Test backwards compatibility** - Don't break existing clients

### Adding Optional Parameters

Safe - doesn't break existing clients:

```go
// Before
"required": []string{"username", "email"}

// After (adding optional parameter)
"properties": map[string]interface{}{
    "username": {...},
    "email": {...},
    "phoneNumber": {  // New optional field
        "type":        "string",
        "description": "Optional phone number",
    },
},
"required": []string{"username", "email"}  // Same required fields
```

### Adding Required Parameters

Breaking change - update with care:

```go
// This breaks existing clients!
"required": []string{"username", "email", "phoneNumber"}
```

**Better approach:**
1. Add as optional parameter first
2. Update all clients
3. Make required in next major version

### Changing Response Format

Breaking change - avoid if possible:

```go
// Before
return map[string]interface{}{
    "content": []map[string]interface{}{
        {"type": "text", "text": message},
    },
}

// After (breaking!)
return map[string]interface{}{
    "status": "success",
    "message": message,
}
```

**Better approach:**
1. Add new tool with new format
2. Deprecate old tool
3. Remove old tool in next major version

---

## Testing Your Changes

### Unit Tests

Run unit tests:

```bash
cd /Users/dpage/git/ai-workbench/server
go test ./src/mcp/...
```

### Integration Tests

Run integration tests:

```bash
cd /Users/dpage/git/ai-workbench/server
go test ./src/integration/... -v
```

### Manual Testing

1. **Start the server:**
    ```bash
    cd /Users/dpage/git/ai-workbench/server
    ./mcp-server -config server.conf -v
    ```

2. **Test with curl:**
    ```bash
    # Initialize
    curl -X POST http://localhost:8080/mcp \
      -H "Content-Type: application/json" \
      -d '{"jsonrpc":"2.0","id":"1","method":"initialize","params":{}}'

    # Authenticate
    curl -X POST http://localhost:8080/api/auth/login \
      -H "Content-Type: application/json" \
      -d '{"username":"admin","password":"password"}'

    # Call your new tool
    curl -X POST http://localhost:8080/mcp \
      -H "Content-Type: application/json" \
      -H "Authorization: Bearer <token-from-auth>" \
      -d '{"jsonrpc":"2.0","id":"3","method":"tools/call","params":{"name":"your_new_tool","arguments":{...}}}'
    ```

3. **Test with MCP client:**
    Use the official MCP Inspector or Claude Desktop to test.

### Linting

```bash
cd /Users/dpage/git/ai-workbench/server
make lint
```

### Coverage

```bash
cd /Users/dpage/git/ai-workbench/server
make coverage
```

---

## Best Practices

### Code Style

1. **Follow four-space indentation** (project standard)
2. **Add copyright headers** to all new files
3. **Use descriptive variable names**
4. **Add comments for complex logic**
5. **Keep functions focused** - single responsibility

### Error Handling

1. **Validate all inputs** before processing
2. **Use appropriate error codes** (ParseError, InvalidParams, etc.)
3. **Log errors** with proper severity
4. **Don't expose sensitive info** in error messages
5. **Return user-friendly messages**

### Security

1. **Always authenticate** (unless explicitly public)
2. **Check authorization** before executing
3. **Validate user input** to prevent injection
4. **Use parameterized queries** always
5. **Audit sensitive operations** (future: add logging)

### Performance

1. **Use connection pooling** (already configured)
2. **Avoid N+1 queries** - use joins or batch queries
3. **Add database indexes** for frequently queried fields
4. **Consider pagination** for large result sets
5. **Set appropriate timeouts**

### Documentation

1. **Update tool catalog** with new tools
2. **Update resource catalog** with new resources
3. **Document authorization requirements** clearly
4. **Provide examples** of usage
5. **Keep docs in sync** with code

---

## Checklist for New Tools

- [ ] Tool design documented
- [ ] Privilege identifier registered in `seed.go`
- [ ] Tool definition added to `handleListTools()`
- [ ] Tool routing added to `handleCallTool()`
- [ ] Tool handler implemented
- [ ] Authentication check implemented
- [ ] Authorization check implemented
- [ ] Input validation implemented
- [ ] Error handling implemented
- [ ] Unit tests written
- [ ] Integration tests written
- [ ] Tests passing
- [ ] Linting passing
- [ ] Documentation updated
- [ ] Manual testing completed
- [ ] Code reviewed

---

## Getting Help

If you encounter issues:

1. **Check logs** - Use `-v` flag for verbose logging
2. **Review existing tests** - See how similar features are tested
3. **Read MCP spec** - https://modelcontextprotocol.io/
4. **Check this documentation** - Especially authentication.md and protocol-implementation.md
5. **Ask for code review** - Have another developer review your changes
