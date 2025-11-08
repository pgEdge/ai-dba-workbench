# MCP Resources Catalog

This document catalogs all available MCP resources, their purposes, and implementation details.

## Overview

Resources in MCP provide read-only access to server-side data. They use a URI-based addressing scheme with the custom `ai-workbench://` protocol.

**Key Concepts:**

- Resources are **read-only** - modifications should be done via tools
- Resources use **URIs** for identification
- Resources return **contents** which can be JSON, text, or other MIME types
- Resources support **listing** (discovery) and **reading** (retrieval)

## Resource Discovery

Clients discover available resources via the `resources/list` method.

**Request:**
```json
{
    "jsonrpc": "2.0",
    "id": "1",
    "method": "resources/list"
}
```

**Response:**
```json
{
    "jsonrpc": "2.0",
    "id": "1",
    "result": {
        "resources": [
            {
                "uri": "ai-workbench://users",
                "name": "User Accounts",
                "description": "List of all user accounts in the system",
                "mimeType": "application/json"
            },
            {
                "uri": "ai-workbench://service-tokens",
                "name": "Service Tokens",
                "description": "List of all service tokens in the system",
                "mimeType": "application/json"
            }
        ]
    }
}
```

**Handler:** `handleListResources()`

**Authentication:** Required (authenticated users only)

---

## Available Resources

### 1. User Accounts Resource

**URI:** `ai-workbench://users`

**Name:** User Accounts

**Description:** List of all user accounts in the system

**MIME Type:** `application/json`

**Authorization:** Authenticated users

**Implementation:**

The `handleReadResource()` function queries the database:

```sql
SELECT username, email, full_name, password_expiry, is_superuser,
       created_at, updated_at
FROM user_accounts
ORDER BY username
```

Each user account is returned as a separate content item with its own URI.

**Response Format:**
```json
{
    "contents": [
        {
            "uri": "ai-workbench://users/alice",
            "mimeType": "application/json",
            "text": "{\"username\": \"alice\", \"email\": \"alice@example.com\", \"fullName\": \"Alice Smith\", \"isSuperuser\": false, \"passwordExpiry\": null, \"createdAt\": \"2025-01-01T10:00:00Z\", \"updatedAt\": \"2025-01-01T10:00:00Z\"}"
        },
        {
            "uri": "ai-workbench://users/bob",
            "mimeType": "application/json",
            "text": "{\"username\": \"bob\", \"email\": \"bob@example.com\", \"fullName\": \"Bob Jones\", \"isSuperuser\": true, \"passwordExpiry\": \"2025-12-31T00:00:00Z\", \"createdAt\": \"2025-01-02T12:00:00Z\", \"updatedAt\": \"2025-01-02T12:00:00Z\"}"
        }
    ]
}
```

**Fields Exposed:**

- `username` - User's login name
- `email` - Email address
- `fullName` - Display name
- `isSuperuser` - Whether user has superuser privileges
- `passwordExpiry` - When password expires (null if no expiry)
- `createdAt` - Account creation timestamp
- `updatedAt` - Last update timestamp

**Security Note:** Password hashes are **not** included in the resource

---

### 2. Service Tokens Resource

**URI:** `ai-workbench://service-tokens`

**Name:** Service Tokens

**Description:** List of all service tokens in the system

**MIME Type:** `application/json`

**Authorization:** Authenticated users

**Implementation:**

Queries the database:

```sql
SELECT name, is_superuser, note, expires_at, created_at, updated_at
FROM service_tokens
ORDER BY name
```

Each service token is returned as a separate content item.

**Response Format:**
```json
{
    "contents": [
        {
            "uri": "ai-workbench://service-tokens/ci-token",
            "mimeType": "application/json",
            "text": "{\"name\": \"ci-token\", \"isSuperuser\": true, \"note\": \"CI/CD automation\", \"expiresAt\": null, \"createdAt\": \"2025-01-01T10:00:00Z\", \"updatedAt\": \"2025-01-01T10:00:00Z\"}"
        }
    ]
}
```

**Fields Exposed:**

- `name` - Token identifier
- `isSuperuser` - Whether token has superuser privileges
- `note` - Optional description
- `expiresAt` - Expiration timestamp (null if no expiry)
- `createdAt` - Token creation timestamp
- `updatedAt` - Last update timestamp

**Security Note:** Token values are **never** exposed via resources

---

## Resource Reading

To read a resource, use the `resources/read` method:

**Request:**
```json
{
    "jsonrpc": "2.0",
    "id": "2",
    "method": "resources/read",
    "params": {
        "uri": "ai-workbench://users"
    }
}
```

**Handler:** `handleReadResource()`

**Authentication:** Required

**Error Handling:**

If an unknown URI is requested:

```json
{
    "jsonrpc": "2.0",
    "id": "2",
    "error": {
        "code": -32602,
        "message": "Unknown resource URI"
    }
}
```

---

## URI Scheme

The resource URI scheme follows this pattern:

```
ai-workbench://<collection>[/<item-id>]
```

**Examples:**

- `ai-workbench://users` - Collection of all users
- `ai-workbench://users/alice` - Specific user (returned in contents)
- `ai-workbench://service-tokens` - Collection of all service tokens
- `ai-workbench://service-tokens/ci-token` - Specific token (returned in contents)

**Note:** Individual item URIs are generated by the read operation, not directly addressable.

---

## Implementation Details

### Handler Function

```go
func (h *Handler) handleReadResource(req Request) (*Response, error) {
    // 1. Parse URI from params
    var params struct {
        URI string `json:"uri"`
    }
    if err := json.Unmarshal(req.Params, &params); err != nil {
        return NewErrorResponse(req.ID, InvalidParams, "Invalid parameters", err.Error()), nil
    }

    // 2. Initialize contents array
    ctx := context.Background()
    var contents []map[string]interface{}

    // 3. Route based on URI
    switch params.URI {
    case "ai-workbench://users":
        // Query and format user accounts
        // ...

    case "ai-workbench://service-tokens":
        // Query and format service tokens
        // ...

    default:
        return NewErrorResponse(req.ID, InvalidParams, "Unknown resource URI", nil), nil
    }

    // 4. Return contents
    result := map[string]interface{}{
        "contents": contents,
    }
    return NewResponse(req.ID, result), nil
}
```

### Database Queries

Resources execute SQL queries directly using the database pool:

```go
rows, err := h.dbPool.Query(ctx, `
    SELECT username, email, full_name, password_expiry, is_superuser,
           created_at, updated_at
    FROM user_accounts
    ORDER BY username
`)
if err != nil {
    return NewErrorResponse(req.ID, InternalError,
        "Failed to query user accounts", err.Error()), nil
}
defer rows.Close()
```

### Content Formatting

Each database row is formatted as a JSON string within a content item:

```go
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
```

---

## Future Resource Additions

Potential resources to add:

1. **User Groups** - `ai-workbench://groups`
    - List all user groups
    - Show group memberships

2. **Connections** - `ai-workbench://connections`
    - List database connections
    - Show connection metadata (not credentials)

3. **User Tokens** - `ai-workbench://user-tokens/<username>`
    - List tokens for a specific user
    - Show token metadata (not token values)

4. **Privileges** - `ai-workbench://privileges/<group-id>`
    - Show all privileges for a group
    - Both connection and MCP privileges

5. **MCP Items** - `ai-workbench://mcp-items`
    - List all registered MCP privilege identifiers
    - Show which groups have access

---

## Adding New Resources

To add a new resource:

1. **Add resource definition** to `handleListResources()`
2. **Add URI case** to `handleReadResource()` switch statement
3. **Implement query logic** to fetch data
4. **Format contents** according to MCP spec
5. **Handle errors** gracefully
6. **Update this documentation**
7. **Write tests** for the new resource

### Example: Adding a Groups Resource

```go
// In handleListResources()
{
    "uri":         "ai-workbench://groups",
    "name":        "User Groups",
    "description": "List of all user groups in the system",
    "mimeType":    "application/json",
},

// In handleReadResource()
case "ai-workbench://groups":
    rows, err := h.dbPool.Query(ctx, `
        SELECT id, name, description, created_at, updated_at
        FROM user_groups
        ORDER BY name
    `)
    if err != nil {
        logger.Errorf("Failed to query user groups: %v", err)
        return NewErrorResponse(req.ID, InternalError,
            "Failed to query user groups", err.Error()), nil
    }
    defer rows.Close()

    for rows.Next() {
        var id int
        var name, description string
        var createdAt, updatedAt interface{}

        if err := rows.Scan(&id, &name, &description, &createdAt,
            &updatedAt); err != nil {
            logger.Errorf("Failed to scan user group: %v", err)
            continue
        }

        contents = append(contents, map[string]interface{}{
            "uri":      fmt.Sprintf("ai-workbench://groups/%d", id),
            "mimeType": "application/json",
            "text": fmt.Sprintf(`{"id": %d, "name": %q, "description": %q, "createdAt": %v, "updatedAt": %v}`,
                id, name, description, createdAt, updatedAt),
        })
    }
```

---

## Best Practices

1. **Always order results** for consistent output
2. **Use proper MIME types** (application/json for structured data)
3. **Generate unique URIs** for each content item
4. **Never expose sensitive data** (passwords, token values, etc.)
5. **Handle database errors** gracefully
6. **Log errors** but don't expose internal details to clients
7. **Use parameterized queries** to prevent SQL injection
8. **Consider pagination** for large result sets (future enhancement)

---

## Security Considerations

1. **Authentication required** - All resource access requires authentication
2. **No authorization by content** - Currently all authenticated users can read all resources
3. **Sensitive fields filtered** - Password hashes and token values excluded
4. **Read-only access** - Resources cannot modify data
5. **SQL injection protected** - Uses parameterized queries via pgx

### Future Security Enhancements

Consider implementing:

- **Resource-level authorization** - Control who can read which resources
- **Field-level filtering** - Show different fields based on user privileges
- **Audit logging** - Track resource access
- **Rate limiting** - Prevent resource enumeration attacks
