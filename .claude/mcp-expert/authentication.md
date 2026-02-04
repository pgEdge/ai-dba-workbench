# MCP Authentication and Authorization

This document describes the authentication and authorization
mechanisms in the pgEdge AI DBA Workbench MCP server.

## Overview

The MCP server implements a multi-tier authentication and
authorization system.

The system includes the following features:

- Token-based authentication via bearer tokens.
- Two-source token validation for API tokens and session tokens.
- Role-based access control with superusers and regular users.
- Group-based privileges for fine-grained access control.
- Token scoping for additional capability restrictions.

---

## Authentication Flow

### 1. Token Transmission

All authenticated requests must include a bearer token in the
Authorization header.

In the following example, the request includes a bearer token
for authentication:

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

The server extracts the token from the Authorization header.

In the following example, the `extractBearerToken` function
parses the header value:

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

The following operations do not require authentication:

- The `initialize` method handles the protocol handshake.
- The `ping` method provides a health check endpoint.
- The `POST /api/auth/login` endpoint handles initial login.

All other operations require a valid bearer token.

### 3. Token Validation

The `validateToken()` function checks tokens against two sources:
API tokens and session tokens.

```go
func (h *Handler) validateToken(
    token string,
) (*UserInfo, error)
```

#### a. API Tokens

The server stores all API tokens in a unified `tokens` table.
Each token references an owning user via `owner_id`.

- Table: `tokens`
- Token storage: Hashed via `usermgmt.HashPassword()`
- Expiration: Optional `expires_at` timestamp
- Identity: The owning user provides the identity.
- Privileges: The owning user determines superuser status.

In the following example, the server validates an API token
and retrieves the owner's privileges:

```go
tokenHash := usermgmt.HashPassword(token)
var ownerID int
var username string
var isSuperuser bool
var isEnabled bool
var expiresAt interface{}
err := h.dbPool.QueryRow(ctx, `
    SELECT t.expires_at, u.id, u.username,
           u.is_superuser, u.is_enabled
    FROM tokens t
    JOIN users u ON t.owner_id = u.id
    WHERE t.token_hash = $1
`, tokenHash).Scan(
    &expiresAt, &ownerID, &username,
    &isSuperuser, &isEnabled,
)
```

Service accounts are users with `is_service_account = TRUE`
and an empty `password_hash`. These accounts authenticate
exclusively through API tokens.

#### b. User Sessions

The server also validates session tokens for interactive users.

- Table: `user_sessions`
- Token storage: Plaintext (not hashed)
- Expiration: Required `expires_at` (default 24 hours)
- Identity: The session references a username.
- Privileges: The associated user determines superuser status.
- Side effect: The server updates `last_used_at` on success.

In the following example, the server validates a session token:

```go
err = h.dbPool.QueryRow(ctx, `
    SELECT us.username, us.expires_at, u.is_superuser
    FROM user_sessions us
    JOIN users u ON us.username = u.username
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

Successful validation returns a `UserInfo` struct.

```go
type UserInfo struct {
    IsAuthenticated bool
    IsSuperuser     bool
    Username        string
}
```

The handler stores this struct for use by tool handlers:

```go
h.userInfo = userInfo
```

---

## Authorization Levels

### Level 1: Unauthenticated

The following operations require no authentication:

- The `initialize` method handles the protocol handshake.
- The `ping` method provides a health check endpoint.
- The `POST /api/auth/login` endpoint handles initial login.

### Level 2: Authenticated User

An authenticated user can perform the following operations:

- All resource reads are available to authenticated users.
- The `create_token` tool creates tokens for the user.
- The `list_tokens` tool lists tokens owned by the user.
- The `delete_token` tool deletes tokens owned by the user.

Any valid bearer token grants this access level.

### Level 3: Privileged User (via Groups)

Group membership grants access to additional operations.
The `privileges.CanAccessMCPItem()` function checks access.

### Level 4: Superuser

Superusers can perform all operations; they bypass all
privilege checks. Superuser status always comes from the
owning user account, never from the token itself.

A user with `is_superuser = TRUE` in the `users` table
grants superuser privileges to all tokens that user owns.

---

## Group-Based Privilege System

### Privilege Architecture

The privilege system consists of the following components:

- The `mcp_privilege_identifiers` table stores registered
  privileges; the server seeds these at startup via
  `privileges.SeedMCPPrivileges()`.
- The `user_groups` table defines named collections of users
  and nested groups in a hierarchical structure.
- The `group_memberships` table links users or groups to parent
  groups and supports recursive resolution.
- The `group_mcp_privileges` table grants specific MCP
  privileges to groups; all group members inherit them.

### Privilege Resolution

The `CanAccessMCPItem()` function determines whether a user
can access an MCP item.

```go
func CanAccessMCPItem(
    ctx context.Context,
    pool *pgxpool.Pool,
    userID int,
    itemIdentifier string,
) (bool, error)
```

The function follows this algorithm:

1. Check whether the user is a superuser; if so, return
   `true` because superusers bypass all checks.
2. Verify the privilege identifier exists in the
   `mcp_privilege_identifiers` table; return `false` if
   the identifier is not found.
3. Count groups in `group_mcp_privileges` for the identifier;
   return `false` if no groups hold the privilege.
4. Use a recursive CTE to resolve all groups the user belongs
   to; the query returns an array of group IDs.
5. Check whether any of the user's groups hold the privilege;
   return `true` if a match exists.

### Group Membership Resolution

The `GetUserGroups()` function recursively resolves group
membership.

In the following example, the recursive CTE resolves all
group memberships for a user:

```sql
WITH RECURSIVE user_groups_recursive AS (
    -- Base case: direct group memberships for this user
    SELECT parent_group_id AS group_id
    FROM group_memberships
    WHERE member_user_id = $1

    UNION

    -- Recursive case: groups containing our groups
    SELECT gm.parent_group_id
    FROM group_memberships gm
    INNER JOIN user_groups_recursive ugr
        ON gm.member_group_id = ugr.group_id
)
SELECT DISTINCT group_id
FROM user_groups_recursive
ORDER BY group_id;
```

This query allows hierarchical group structures.

In the following example, the hierarchy shows nested groups:

```
Administrators Group
+-- DB Admins Group
|   +-- Alice (user)
+-- Bob (user)

Operations Group
+-- DB Admins Group (nested)
+-- Charlie (user)
```

In this example, the resolved memberships are:

- Alice belongs to DB Admins, Administrators, and Operations.
- Bob belongs to Administrators only.
- Charlie belongs to Operations only.

---

## Authorization Patterns

### Pattern 1: Superuser Only

This pattern restricts operations to superusers.

In the following example, the handler requires superuser
privileges:

```go
func (h *Handler) handleSensitiveOperation(
    args map[string]interface{},
) (interface{}, error) {
    // Check authentication
    if h.userInfo == nil || !h.userInfo.IsAuthenticated {
        return nil, fmt.Errorf("authentication required")
    }

    // Require superuser
    if !h.userInfo.IsSuperuser {
        return nil, fmt.Errorf(
            "permission denied: superuser privileges required",
        )
    }

    // Execute operation
    // ...
}
```

Token scope management tools use this pattern by default.

### Pattern 2: Superuser or Privilege Check

This pattern allows access via superuser status or group
membership.

In the following example, the handler checks privileges via
group membership for non-superuser users:

```go
func (h *Handler) handleAdminOperation(
    args map[string]interface{},
) (interface{}, error) {
    if h.userInfo == nil || !h.userInfo.IsAuthenticated {
        return nil, fmt.Errorf(
            "permission denied: superuser privileges required",
        )
    }

    // Superusers bypass all privilege checks
    if !h.userInfo.IsSuperuser {
        if h.dbPool == nil {
            return nil, fmt.Errorf(
                "permission denied: superuser privileges required",
            )
        }

        // Check privileges via group membership
        ctx := context.Background()

        var userID int
        err := h.dbPool.QueryRow(ctx,
            "SELECT id FROM users WHERE username = $1",
            h.userInfo.Username).Scan(&userID)
        if err != nil {
            return nil, fmt.Errorf(
                "failed to get user ID: %w", err,
            )
        }

        canAccess, err := privileges.CanAccessMCPItem(
            ctx, h.dbPool, userID, "operation_name",
        )
        if err != nil {
            return nil, fmt.Errorf(
                "failed to check privileges: %w", err,
            )
        }
        if !canAccess {
            return nil, fmt.Errorf(
                "permission denied: insufficient privileges",
            )
        }
    }

    // Execute operation
    // ...
}
```

The following tool categories use this pattern:

- User management tools check group-based privileges.
- Token management tools check group-based privileges.
- Group management tools check group-based privileges.
- Privilege management tools check group-based privileges.

### Pattern 3: Self-Service or Superuser

This pattern allows users to manage their own resources.

In the following example, the handler permits access to the
user's own data or to superusers:

```go
func (h *Handler) handleSelfServiceOperation(
    args map[string]interface{},
) (interface{}, error) {
    if h.userInfo == nil || !h.userInfo.IsAuthenticated {
        return nil, fmt.Errorf("authentication required")
    }

    username, _ := args["username"].(string)

    // Users can only access their own data
    if h.userInfo.Username != username &&
       !h.userInfo.IsSuperuser {
        return nil, fmt.Errorf(
            "permission denied: can only access your own data",
        )
    }

    // Execute operation
    // ...
}
```

Token management operations use this pattern; users create,
list, and delete their own tokens.

---

## Token Scoping (Advanced)

Token scoping provides additional restrictions beyond user
privileges.

### Scope Types

The system supports two scope types:

- Connection scope limits a token to specific database
  connections.
- MCP scope limits a token to specific MCP tools.

### Scope Tables

The system uses two unified scope tables:

- The `token_connection_scope` table stores connection
  restrictions for tokens.
- The `token_mcp_scope` table stores MCP restrictions
  for tokens.

### Scope Enforcement

Scope enforcement checks the following conditions:

1. For connection operations, the handler checks whether
   the token has a connection scope defined; if the
   requested connection falls outside the scope, the
   handler denies access.
2. For MCP tool calls, the handler checks whether the
   token has an MCP scope defined; if the requested tool
   falls outside the scope, the handler denies access.

### Scope Management

The following tools manage token scopes:

- The `set_token_connection_scope` tool defines connection
  restrictions.
- The `set_token_mcp_scope` tool defines MCP restrictions.
- The `get_token_scope` tool displays current restrictions.
- The `clear_token_scope` tool removes all restrictions.

Only superusers can manage token scopes.

---

## Security Best Practices

### Token Management

The system supports the following token categories:

- API tokens owned by regular users provide CLI and API
  access; these tokens should have a set expiration.
- API tokens owned by service accounts provide automated
  access; service accounts cannot log in with a password.
- Session tokens provide web UI access with a short
  lifetime of 24 hours.

### Token Storage

The system stores tokens with the following methods:

- API tokens use a secure hashing algorithm for storage.
- Session tokens use plaintext storage with mandatory
  expiration.

### Token Transmission

The following practices apply to token transmission:

- Always use HTTPS in production environments.
- Never log token values; log only hashes or IDs.
- Use the bearer token scheme per the HTTP specification.
- Implement rate limiting to prevent brute force attacks.

### Privilege Design

The following principles guide privilege design:

- Default deny grants no privileges unless explicitly
  assigned.
- Least privilege grants the minimum necessary permissions.
- Group-based access uses groups rather than individual
  user grants.
- Hierarchical groups support organizational structure.
- Audit trails log privilege changes for accountability.

### Password Management

The following requirements apply to password management:

- Hash all passwords using bcrypt or a similar algorithm.
- Support password expiry for compliance requirements.
- Enforce password complexity rules in future releases.
- Prevent password reuse in future releases.

---

## Testing Authentication

### Test Mode

When `dbPool` is nil during unit tests, the server bypasses
authentication.

In the following example, the handler skips authentication
in test mode:

```go
requiresAuth := true
if h.dbPool == nil {
    // No database pool; skip authentication
    requiresAuth = false
}
```

This approach allows testing tool logic without database
dependencies.

### Integration Tests

Integration tests should follow these steps:

1. Create test users with known passwords.
2. Authenticate to obtain session tokens.
3. Use the tokens in subsequent requests.
4. Test both authorized and unauthorized access.
5. Verify privilege enforcement.
6. Clean up test users and tokens.

---

## Common Authentication Errors

### "Authentication required"

This error occurs when no bearer token is provided for a
protected endpoint. Include an `Authorization: Bearer`
header with a valid token.

### "Authentication failed"

This error occurs when a token is invalid or expired.
Obtain a new token via the `POST /api/auth/login` endpoint.

### "Permission denied: superuser privileges required"

This error occurs when an operation requires superuser
privileges. Use an account with `is_superuser = TRUE` or
a token owned by a superuser.

### "Permission denied: insufficient privileges"

This error occurs when a user lacks the required MCP
privilege. Grant the privilege to the user's group or add
the user to a privileged group.

### "Password has expired"

This error occurs when the user's `password_expiry` has
passed. Update the user password through an admin or
self-service operation.

---

## Future Enhancements

The following enhancements are planned:

- OAuth 2.0 support for external identity providers.
- SAML support for enterprise SSO integration.
- Multi-factor authentication for additional security.
- API key rotation for automatic token renewal.
- Session management UI for viewing and revoking sessions.
- Audit logging for tracking all authentication events.
- Rate limiting for preventing brute force attacks.
- IP allowlisting for restricting token usage by source.
- Token scope enforcement in handler implementations.
- Resource-level authorization for per-user access control.
