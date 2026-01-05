# MCP Authentication and Authorization

This document describes the authentication and authorization mechanisms in the pgEdge AI DBA Workbench MCP server.

## Overview

The MCP server implements a comprehensive multi-tier authentication and authorization system:

1. **Token-based authentication** - Via bearer tokens
2. **Multi-source token validation** - Service tokens, user tokens, and session tokens
3. **Role-based access control (RBAC)** - Superusers vs. regular users
4. **Group-based privileges** - Fine-grained access via group membership
5. **Token scoping** - Additional restrictions on token capabilities

---

## Authentication Flow

### 1. Token Transmission

All authenticated requests must include a bearer token in the Authorization header:

```http
POST /mcp HTTP/1.1
Host: localhost:8080
Authorization: Bearer <token-value>
Content-Type: application/json

{
    "jsonrpc": "2.0",
    "id": "1",
    "method": "tools/call",
    "params": {
        "name": "create_user",
        "arguments": { ... }
    }
}
```

The server extracts the token using:

```go
func extractBearerToken(r *http.Request) string {
    authHeader := r.Header.Get("Authorization")
    if authHeader == "" {
        return ""
    }

    const bearerPrefix = "Bearer "
    if len(authHeader) > len(bearerPrefix) &&
       authHeader[:len(bearerPrefix)] == bearerPrefix {
        return authHeader[len(bearerPrefix):]
    }

    return ""
}
```

### 2. Authentication Exemptions

The following operations do **not** require authentication:

- `initialize` - Protocol handshake
- `ping` - Health check
- `tools/call` with `authenticate_user` - Initial login

All other operations require a valid bearer token.

### 3. Token Validation

The `validateToken()` function checks tokens against three sources:

```go
func (h *Handler) validateToken(token string) (*UserInfo, error)
```

**Validation Sources (checked in order):**

#### a. Service Tokens

- Table: `service_tokens`
- Token storage: Hashed via `usermgmt.HashPassword()`
- Expiration: Optional `expires_at` timestamp
- Identity: Token name (no specific user)
- Privileges: Can have superuser status

```go
tokenHash := usermgmt.HashPassword(token)
var expiresAt interface{}
var isSuperuser bool
err := h.dbPool.QueryRow(ctx, `
    SELECT expires_at, is_superuser
    FROM service_tokens
    WHERE token_hash = $1
`, tokenHash).Scan(&expiresAt, &isSuperuser)
```

#### b. User Tokens

- Table: `user_tokens`
- Token storage: Hashed via `usermgmt.HashPassword()`
- Expiration: Optional `expires_at` timestamp
- Identity: Associated with specific user account
- Privileges: Inherits user's superuser status and group memberships

```go
var username string
err = h.dbPool.QueryRow(ctx, `
    SELECT ua.username, ut.expires_at, ua.is_superuser
    FROM user_tokens ut
    JOIN user_accounts ua ON ut.user_id = ua.id
    WHERE ut.token_hash = $1
`, tokenHash).Scan(&username, &expiresAt, &isSuperuser)
```

#### c. User Sessions

- Table: `user_sessions`
- Token storage: **Plaintext** (not hashed)
- Expiration: Required `expires_at` timestamp (default 24 hours)
- Identity: Associated with username
- Privileges: Inherits user's superuser status and group memberships
- Side effect: Updates `last_used_at` on successful validation

```go
err = h.dbPool.QueryRow(ctx, `
    SELECT us.username, us.expires_at, ua.is_superuser
    FROM user_sessions us
    JOIN user_accounts ua ON us.username = ua.username
    WHERE us.session_token = $1
`, token).Scan(&username, &expiresAt, &isSuperuser)

// Update last_used_at
_, err = h.dbPool.Exec(ctx, `
    UPDATE user_sessions
    SET last_used_at = $1
    WHERE session_token = $2
`, time.Now(), token)
```

### 4. UserInfo Structure

Successful validation returns a `UserInfo` struct:

```go
type UserInfo struct {
    IsAuthenticated bool
    IsSuperuser     bool
    Username        string      // Empty for service tokens
    IsServiceToken  bool
}
```

This struct is stored in the handler for use by tool handlers:

```go
h.userInfo = userInfo
```

---

## Authorization Levels

### Level 1: Unauthenticated

**Allowed Operations:**
- `initialize`
- `ping`
- `tools/call` with `authenticate_user` tool

**Access:** Public

### Level 2: Authenticated User

**Allowed Operations:**
- All resource reads
- Self-service operations:
    - `create_user_token` (own tokens only)
    - `list_user_tokens` (own tokens only)
    - `delete_user_token` (own tokens only)

**Access:** Any valid bearer token

### Level 3: Privileged User (via Groups)

**Allowed Operations:**
- Operations granted via group membership
- Checked via `privileges.CanAccessMCPItem()`

**Access:** Regular user + group membership with specific MCP privileges

### Level 4: Superuser

**Allowed Operations:**
- All operations (bypasses all privilege checks)

**Access:** User account or service token with `is_superuser = true`

---

## Group-Based Privilege System

### Privilege Architecture

1. **MCP Privilege Identifiers** (`mcp_privilege_identifiers` table)
    - Pre-registered at server startup via `privileges.SeedMCPPrivileges()`
    - One entry per tool, resource, or prompt
    - Includes identifier, item_type, and description

2. **User Groups** (`user_groups` table)
    - Named collections of users and nested groups
    - Hierarchical (groups can contain groups)

3. **Group Memberships** (`group_memberships` table)
    - Links users or groups to parent groups
    - Supports recursive resolution

4. **Group MCP Privileges** (`group_mcp_privileges` table)
    - Grants specific MCP privileges to groups
    - All group members inherit the privilege

### Privilege Resolution

The `CanAccessMCPItem()` function determines if a user can access an MCP item:

```go
func CanAccessMCPItem(ctx context.Context, pool *pgxpool.Pool,
    userID int, itemIdentifier string) (bool, error)
```

**Algorithm:**

1. **Check if user is superuser**
    - If yes, return `true` (superusers bypass all checks)

2. **Verify privilege identifier exists**
    - Look up identifier in `mcp_privilege_identifiers` table
    - If not found, return `false` (deny by default)

3. **Check if any groups have been granted the privilege**
    - Count groups in `group_mcp_privileges` for this identifier
    - If count is 0, return `false` (deny by default for security)

4. **Get all groups the user belongs to**
    - Use recursive CTE to resolve nested group memberships
    - Returns array of group IDs

5. **Check if user's groups have the privilege**
    - Query if any of user's groups have been granted the privilege
    - Return `true` if match found, `false` otherwise

### Group Membership Resolution

The `GetUserGroups()` function recursively resolves group membership:

```sql
WITH RECURSIVE user_groups_recursive AS (
    -- Base case: direct group memberships for this user
    SELECT parent_group_id as group_id
    FROM group_memberships
    WHERE member_user_id = $1

    UNION

    -- Recursive case: groups that contain groups we're already in
    SELECT gm.parent_group_id
    FROM group_memberships gm
    INNER JOIN user_groups_recursive ugr ON gm.member_group_id = ugr.group_id
)
SELECT DISTINCT group_id
FROM user_groups_recursive
ORDER BY group_id;
```

This allows hierarchical group structures like:

```
Administrators Group
├── DB Admins Group
│   └── Alice (user)
└── Bob (user)

Operations Group
├── DB Admins Group (nested)
└── Charlie (user)
```

In this example:
- Alice belongs to: DB Admins Group, Administrators Group, Operations Group
- Bob belongs to: Administrators Group
- Charlie belongs to: Operations Group

---

## Authorization Patterns

### Pattern 1: Superuser Only

Used for highly privileged operations:

```go
func (h *Handler) handleSensitiveOperation(args map[string]interface{}) (interface{}, error) {
    // Check authentication
    if h.userInfo == nil || !h.userInfo.IsAuthenticated {
        return nil, fmt.Errorf("authentication required")
    }

    // Require superuser
    if !h.userInfo.IsSuperuser {
        return nil, fmt.Errorf("permission denied: superuser privileges required")
    }

    // Execute operation
    // ...
}
```

**Used by:**
- Token scope management tools (by default)

### Pattern 2: Superuser or Privilege Check

Most common pattern for administrative tools:

```go
func (h *Handler) handleAdminOperation(args map[string]interface{}) (interface{}, error) {
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
        err := h.dbPool.QueryRow(ctx,
            "SELECT id FROM user_accounts WHERE username = $1",
            h.userInfo.Username).Scan(&userID)
        if err != nil {
            return nil, fmt.Errorf("failed to get user ID: %w", err)
        }

        // Check if user has privilege
        canAccess, err := privileges.CanAccessMCPItem(ctx, h.dbPool,
            userID, "operation_name")
        if err != nil {
            return nil, fmt.Errorf("failed to check privileges: %w", err)
        }
        if !canAccess {
            return nil, fmt.Errorf("permission denied: insufficient privileges")
        }
    }

    // Execute operation
    // ...
}
```

**Used by:**
- User management tools
- Service token management tools
- Group management tools
- Privilege management tools

### Pattern 3: Self-Service or Superuser

Used for user-specific operations:

```go
func (h *Handler) handleSelfServiceOperation(args map[string]interface{}) (interface{}, error) {
    // Check authentication
    if h.userInfo == nil || !h.userInfo.IsAuthenticated {
        return nil, fmt.Errorf("authentication required")
    }

    username, _ := args["username"].(string)

    // Check authorization: users can only access their own data unless superuser
    if h.userInfo.Username != username && !h.userInfo.IsSuperuser {
        return nil, fmt.Errorf("permission denied: can only access your own data")
    }

    // Execute operation
    // ...
}
```

**Used by:**
- User token management (create, list, delete)

---

## Token Scoping (Advanced)

Token scoping provides additional restrictions beyond user privileges.

### Scope Types

1. **Connection Scope** - Limit token to specific database connections
2. **MCP Scope** - Limit token to specific MCP tools

### Scope Tables

- `user_token_connection_scope` - Connection restrictions for user tokens
- `service_token_connection_scope` - Connection restrictions for service tokens
- `user_token_mcp_scope` - MCP restrictions for user tokens
- `service_token_mcp_scope` - MCP restrictions for service tokens

### Scope Enforcement

Scope enforcement must be implemented in the relevant handlers (future work):

1. **For connection operations:**
    - Check if token has connection scope defined
    - If yes, verify requested connection is in scope
    - If not in scope, deny access

2. **For MCP tool calls:**
    - Check if token has MCP scope defined
    - If yes, verify requested tool is in scope
    - If not in scope, deny access

### Scope Management

Scopes are managed via tools:

- `set_token_connection_scope` - Define connection restrictions
- `set_token_mcp_scope` - Define MCP restrictions
- `get_token_scope` - View current restrictions
- `clear_token_scope` - Remove all restrictions

**Note:** Currently only superusers can manage token scopes.

---

## Security Best Practices

### Token Management

1. **Service tokens** - Use for automation, not interactive use
2. **User tokens** - Use for CLI/API access, set expiration
3. **Session tokens** - Use for web UI, short lifetime (24 hours)

### Token Storage

1. **Service tokens** - Hashed with secure algorithm
2. **User tokens** - Hashed with secure algorithm
3. **Session tokens** - Stored plaintext but with mandatory expiration

### Token Transmission

1. **Always use HTTPS** in production
2. **Never log token values** (log only hashes or IDs)
3. **Use bearer token scheme** per HTTP spec
4. **Implement rate limiting** to prevent brute force (future work)

### Privilege Design

1. **Default deny** - No privileges granted unless explicitly assigned
2. **Least privilege** - Grant minimum necessary permissions
3. **Group-based** - Use groups rather than individual user grants
4. **Hierarchical** - Use nested groups for organizational structure
5. **Audit trail** - Log privilege changes (future work)

### Password Management

1. **Hash all passwords** using bcrypt or similar
2. **Support password expiry** for compliance
3. **Enforce password complexity** (future work)
4. **Prevent password reuse** (future work)

---

## Testing Authentication

### Test Mode

When `dbPool` is nil (unit tests), authentication is bypassed:

```go
requiresAuth := true
if h.dbPool == nil {
    // No database pool, skip authentication (unit test mode)
    requiresAuth = false
}
```

This allows testing tool logic without database dependencies.

### Integration Tests

Integration tests should:

1. Create test users with known passwords
2. Authenticate to obtain session tokens
3. Use tokens in subsequent requests
4. Test both authorized and unauthorized access
5. Verify privilege enforcement
6. Clean up test users and tokens

---

## Common Authentication Errors

### "Authentication required"

**Cause:** No bearer token provided for protected endpoint

**Solution:** Include `Authorization: Bearer <token>` header

### "Authentication failed"

**Cause:** Invalid or expired token

**Solution:** Obtain a new token via `authenticate_user` tool

### "Permission denied: superuser privileges required"

**Cause:** Operation requires superuser, but user is not superuser

**Solution:** Use a superuser account or service token

### "Permission denied: insufficient privileges"

**Cause:** User lacks required MCP privilege via group membership

**Solution:** Grant privilege to user's group or make user a member of privileged group

### "Password has expired"

**Cause:** User account's password_expiry has passed

**Solution:** Update user password (requires admin or self-service)

---

## Future Enhancements

1. **OAuth 2.0 support** - Integration with external identity providers
2. **SAML support** - Enterprise SSO
3. **Multi-factor authentication (MFA)** - Additional security layer
4. **API key rotation** - Automatic token renewal
5. **Session management UI** - View and revoke active sessions
6. **Audit logging** - Track all authentication events
7. **Rate limiting** - Prevent brute force attacks
8. **IP allowlisting** - Restrict token usage by source IP
9. **Token scope enforcement** - Implement scope checks in handlers
10. **Resource-level authorization** - Control resource access per user/group
