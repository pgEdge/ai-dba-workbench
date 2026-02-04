/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Authentication and Authorization
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

# Authentication and Authorization Flow

This document describes the authentication and authorization
mechanisms implemented in the pgEdge AI DBA Workbench server.

## Authentication

### Token Types

The system supports two types of authentication credentials:

- User passwords allow interactive login through the web
  interface; the server issues a session token on success.
- API tokens provide programmatic access for users and
  service accounts; each token references an owning user.

All tokens are stored as SHA256 hashes in the database;
the system never stores tokens in plaintext.

### Token Storage Schema

The following schema defines the tables for authentication.

```sql
-- User accounts (includes service accounts)
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT UNIQUE NOT NULL,
    email TEXT NOT NULL,
    full_name TEXT NOT NULL,
    password_hash TEXT NOT NULL,  -- SHA256 hash; empty for
                                 -- service accounts
    password_expiry DATETIME,
    is_superuser BOOLEAN DEFAULT FALSE,
    is_enabled BOOLEAN DEFAULT TRUE,
    is_service_account BOOLEAN DEFAULT FALSE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Unified API tokens
CREATE TABLE tokens (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    token_hash TEXT UNIQUE NOT NULL,  -- SHA256 hash
    owner_id INTEGER NOT NULL REFERENCES users(id),
    annotation TEXT DEFAULT '',
    expires_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

Service accounts are users with `is_service_account = TRUE`
and an empty `password_hash`. These accounts cannot log in
with a password; API tokens are their only authentication
method.

### Token Hashing

The `HashPassword` function produces a SHA256 hash of the
provided token or password.

```go
func HashPassword(password string) string {
    hash := sha256.Sum256([]byte(password))
    return fmt.Sprintf("%x", hash)
}
```

SHA256 is appropriate for token hashing because tokens use
high entropy from 32 bytes of random data. For user
passwords in production, consider bcrypt or Argon2 instead.

### Token Generation

The `GenerateToken` function creates a cryptographically
secure random token.

```go
func GenerateToken() (string, error) {
    bytes := make([]byte, 32)
    if _, err := rand.Read(bytes); err != nil {
        return "", fmt.Errorf(
            "failed to generate token: %w", err,
        )
    }
    return base64.URLEncoding.EncodeToString(bytes), nil
}
```

The function produces a 43-character URL-safe base64 string
with 32 bytes of entropy.

### Authentication Validation

The server validates bearer tokens on each request by
checking two sources: API tokens and session tokens.

In the following example, the `validateToken` function
checks both sources:

```go
func (h *Handler) validateToken(
    bearerToken string,
) (*UserInfo, error) {
    if bearerToken == "" {
        return nil, fmt.Errorf("no bearer token provided")
    }

    // Hash the provided token
    tokenHash := usermgmt.HashPassword(bearerToken)
    ctx := context.Background()

    // Try API token first
    userInfo, err := h.checkToken(ctx, tokenHash)
    if err == nil {
        return userInfo, nil
    }

    // Try session token
    userInfo, err = h.checkSession(ctx, bearerToken)
    if err == nil {
        return userInfo, nil
    }

    return nil, fmt.Errorf("invalid token")
}
```

### Token Expiry Handling

The `checkToken` function validates an API token and
retrieves the owning user's privileges.

In the following example, the function queries the unified
`tokens` table and joins with `users` for authorization:

```go
func (h *Handler) checkToken(
    ctx context.Context,
    tokenHash string,
) (*UserInfo, error) {
    var ownerID int
    var username string
    var isSuperuser bool
    var isEnabled bool
    var expiresAt sql.NullTime

    err := h.dbPool.QueryRow(ctx, `
        SELECT u.id, u.username, u.is_superuser,
               u.is_enabled, t.expires_at
        FROM tokens t
        JOIN users u ON t.owner_id = u.id
        WHERE t.token_hash = $1
    `, tokenHash).Scan(
        &ownerID, &username, &isSuperuser,
        &isEnabled, &expiresAt,
    )

    if err != nil {
        return nil, err
    }

    // Check if owning user is enabled
    if !isEnabled {
        return nil, fmt.Errorf("user account disabled")
    }

    // Check token expiry
    if expiresAt.Valid && time.Now().After(expiresAt.Time) {
        return nil, fmt.Errorf("token expired")
    }

    return &UserInfo{
        UserID:          ownerID,
        Username:        username,
        IsSuperuser:     isSuperuser,
        IsAuthenticated: true,
    }, nil
}
```

The function checks whether the owning user is enabled
before granting access. Superuser status always comes from
the owning user account.

### Token Lifecycle

The `CreateToken` function creates a new API token for the
specified owner.

In the following example, the function generates a token
and stores its hash in the database:

```go
// Create an API token for the specified owner
message, token, err := usermgmt.CreateToken(
    pool,
    ownerUsername,       // User or service account
    annotation,          // Description of the token
    requestedExpiry,     // Lifetime in days (0 = no expiry)
    maxLifetimeDays,     // Server-configured maximum
)

// The token value is returned once to the caller
fmt.Printf("Token: %s\n", token)
fmt.Println("Save this token; it cannot be retrieved again")
```

The function supports the following features:

- Any user or service account can own API tokens.
- The server enforces a configurable maximum lifetime.
- An expiry of zero creates a token with no expiration,
  subject to the server maximum.
- The annotation field describes the token's purpose.
- Tokens inherit the owning user's privileges and
  superuser status.
- Token owners can list and delete their own tokens.

### HTTP Bearer Token Format

Clients authenticate using the HTTP Authorization header.

In the following example, the request includes a bearer
token:

```http
POST /mcp HTTP/1.1
Host: localhost:8080
Authorization: Bearer <token>
Content-Type: application/json

{
    "jsonrpc": "2.0",
    "id": "1",
    "method": "tools/list"
}
```

### Unauthenticated Methods

The following methods do not require authentication:

- The `initialize` method handles the protocol handshake.
- The `ping` method provides a health check endpoint.
- The `POST /api/auth/login` endpoint handles initial login.

All other methods require a valid bearer token.

## Authorization (RBAC)

### Role-Based Access Control Model

The system implements a flexible RBAC model with the
following components:

- Users represent individual accounts; service accounts
  are also users with `is_service_account = TRUE`.
- Groups are collections of users or other groups in a
  hierarchical structure.
- Connections represent database connections with owner
  and sharing settings.
- Privileges define permissions on connections and MCP
  items.

### Database Schema

The following schema defines the authorization tables.

```sql
-- Groups
CREATE TABLE groups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE NOT NULL,
    description TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Group memberships (supports nested groups)
CREATE TABLE group_memberships (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    parent_group_id INTEGER NOT NULL
        REFERENCES groups(id) ON DELETE CASCADE,
    member_user_id INTEGER
        REFERENCES users(id) ON DELETE CASCADE,
    member_group_id INTEGER
        REFERENCES groups(id) ON DELETE CASCADE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT check_member CHECK (
        (member_user_id IS NOT NULL
         AND member_group_id IS NULL)
        OR
        (member_user_id IS NULL
         AND member_group_id IS NOT NULL)
    )
);

-- Connection privileges
CREATE TABLE connection_privileges (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    connection_id INTEGER NOT NULL
        REFERENCES connections(id) ON DELETE CASCADE,
    group_id INTEGER NOT NULL
        REFERENCES groups(id) ON DELETE CASCADE,
    access_level TEXT NOT NULL
        CHECK (access_level IN ('read', 'read_write')),
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(connection_id, group_id)
);

-- MCP privilege identifiers
CREATE TABLE mcp_privilege_identifiers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    identifier TEXT UNIQUE NOT NULL,
    description TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Group MCP privileges
CREATE TABLE group_mcp_privileges (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id INTEGER NOT NULL
        REFERENCES groups(id) ON DELETE CASCADE,
    privilege_identifier_id INTEGER NOT NULL
        REFERENCES mcp_privilege_identifiers(id)
        ON DELETE CASCADE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(group_id, privilege_identifier_id)
);
```

### Token Scoping

Token scoping restricts a token's capabilities beyond the
owning user's privileges. The system uses two unified scope
tables.

```sql
-- Connection scope for tokens
CREATE TABLE token_connection_scope (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    token_id INTEGER NOT NULL
        REFERENCES tokens(id) ON DELETE CASCADE,
    connection_id INTEGER NOT NULL
        REFERENCES connections(id) ON DELETE CASCADE,
    UNIQUE(token_id, connection_id)
);

-- MCP scope for tokens
CREATE TABLE token_mcp_scope (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    token_id INTEGER NOT NULL
        REFERENCES tokens(id) ON DELETE CASCADE,
    privilege_identifier_id INTEGER NOT NULL
        REFERENCES mcp_privilege_identifiers(id)
        ON DELETE CASCADE,
    UNIQUE(token_id, privilege_identifier_id)
);
```

These tables apply to all tokens regardless of whether the
owning user is a regular user or a service account.

### Privilege Checking Functions

#### Connection Access

The `CanAccessConnection` function determines whether a
user can access a specific connection.

```go
func CanAccessConnection(
    ctx context.Context,
    pool *pgxpool.Pool,
    userID int,
    connectionID int,
    requestedLevel AccessLevel,
) (bool, error) {
    // 1. Check if user is superuser (bypass all checks)
    var isSuperuser bool
    err := pool.QueryRow(ctx,
        "SELECT is_superuser FROM users WHERE id = $1",
        userID).Scan(&isSuperuser)
    if err != nil {
        return false, fmt.Errorf(
            "failed to check superuser status: %w", err,
        )
    }
    if isSuperuser {
        return true, nil
    }

    // 2. Check if connection is shared
    var isShared bool
    err = pool.QueryRow(ctx,
        "SELECT is_shared FROM connections WHERE id = $1",
        connectionID).Scan(&isShared)
    if err != nil {
        return false, fmt.Errorf(
            "failed to check connection: %w", err,
        )
    }

    // 3. If not shared, check ownership
    if !isShared {
        var ownerUsername *string
        err = pool.QueryRow(ctx,
            `SELECT owner_username
             FROM connections WHERE id = $1`,
            connectionID).Scan(&ownerUsername)
        if err != nil {
            return false, fmt.Errorf(
                "failed to check owner: %w", err,
            )
        }

        var username string
        err = pool.QueryRow(ctx,
            "SELECT username FROM users WHERE id = $1",
            userID).Scan(&username)
        if err != nil {
            return false, fmt.Errorf(
                "failed to get username: %w", err,
            )
        }

        if ownerUsername != nil &&
           *ownerUsername == username {
            return true, nil
        }

        return false, nil
    }

    // 4. For shared connections, check group privileges
    var groupCount int
    err = pool.QueryRow(ctx,
        `SELECT COUNT(*)
         FROM connection_privileges
         WHERE connection_id = $1`,
        connectionID).Scan(&groupCount)
    if err != nil {
        return false, fmt.Errorf(
            "failed to count privileges: %w", err,
        )
    }

    if groupCount == 0 {
        return false, nil
    }

    // 5. Get all groups user belongs to (recursive)
    userGroups, err := GetUserGroups(ctx, pool, userID)
    if err != nil {
        return false, fmt.Errorf(
            "failed to get user groups: %w", err,
        )
    }

    if len(userGroups) == 0 {
        return false, nil
    }

    // 6. Check group access level
    query := `
        SELECT COUNT(*)
        FROM connection_privileges
        WHERE connection_id = $1
          AND group_id = ANY($2)
          AND (
              access_level = 'read_write'
              OR ($3 = 'read'
                  AND access_level = 'read')
          )
    `

    var matchCount int
    err = pool.QueryRow(
        ctx, query, connectionID,
        userGroups, requestedLevel,
    ).Scan(&matchCount)
    if err != nil {
        return false, fmt.Errorf(
            "failed to check privileges: %w", err,
        )
    }

    return matchCount > 0, nil
}
```

The function supports two access levels:

- The `read` level allows queries, schema views, and
  connection tests.
- The `read_write` level allows DML operations including
  INSERT, UPDATE, DELETE, and DDL statements.

#### MCP Item Access

The `CanAccessMCPItem` function determines whether a user
can access a specific MCP tool, resource, or prompt.

```go
func CanAccessMCPItem(
    ctx context.Context,
    pool *pgxpool.Pool,
    userID int,
    itemIdentifier string,
) (bool, error) {
    // 1. Check if user is superuser
    var isSuperuser bool
    err := pool.QueryRow(ctx,
        "SELECT is_superuser FROM users WHERE id = $1",
        userID).Scan(&isSuperuser)
    if err != nil {
        return false, fmt.Errorf(
            "failed to check superuser: %w", err,
        )
    }
    if isSuperuser {
        return true, nil
    }

    // 2. Check if privilege identifier exists
    var privilegeID int
    err = pool.QueryRow(ctx,
        `SELECT id FROM mcp_privilege_identifiers
         WHERE identifier = $1`,
        itemIdentifier).Scan(&privilegeID)
    if err != nil {
        return false, fmt.Errorf(
            "privilege identifier not found: %s",
            itemIdentifier,
        )
    }

    // 3. Check if any groups have this privilege
    var groupCount int
    err = pool.QueryRow(ctx,
        `SELECT COUNT(*)
         FROM group_mcp_privileges
         WHERE privilege_identifier_id = $1`,
        privilegeID).Scan(&groupCount)
    if err != nil {
        return false, fmt.Errorf(
            "failed to count privileges: %w", err,
        )
    }

    if groupCount == 0 {
        return false, nil
    }

    // 4. Get all groups user belongs to
    userGroups, err := GetUserGroups(ctx, pool, userID)
    if err != nil {
        return false, fmt.Errorf(
            "failed to get user groups: %w", err,
        )
    }

    if len(userGroups) == 0 {
        return false, nil
    }

    // 5. Check if user's groups have the privilege
    query := `
        SELECT COUNT(*)
        FROM group_mcp_privileges
        WHERE privilege_identifier_id = $1
          AND group_id = ANY($2)
    `

    var matchCount int
    err = pool.QueryRow(
        ctx, query, privilegeID, userGroups,
    ).Scan(&matchCount)
    if err != nil {
        return false, fmt.Errorf(
            "failed to check privileges: %w", err,
        )
    }

    return matchCount > 0, nil
}
```

The function uses the following MCP item identifiers:

- Tools use the format `tool:execute_query`.
- Resources use the format `resource:users`.
- Prompts use the format `prompt:query_helper`.

### Recursive Group Membership

Groups can contain other groups to create a hierarchy. The
system uses recursive CTEs to resolve the full membership
tree.

```go
func GetUserGroups(
    ctx context.Context,
    pool *pgxpool.Pool,
    userID int,
) ([]int, error) {
    query := `
        WITH RECURSIVE user_groups_recursive AS (
            -- Base case: direct group memberships
            SELECT parent_group_id AS group_id
            FROM group_memberships
            WHERE member_user_id = $1

            UNION

            -- Recursive case: nested groups
            SELECT gm.parent_group_id
            FROM group_memberships gm
            INNER JOIN user_groups_recursive ugr
                ON gm.member_group_id = ugr.group_id
        )
        SELECT DISTINCT group_id
        FROM user_groups_recursive
        ORDER BY group_id;
    `

    rows, err := pool.Query(ctx, query, userID)
    if err != nil {
        return nil, fmt.Errorf(
            "failed to query user groups: %w", err,
        )
    }
    defer rows.Close()

    groupIDs := make([]int, 0)
    for rows.Next() {
        var groupID int
        if err := rows.Scan(&groupID); err != nil {
            return nil, fmt.Errorf(
                "failed to scan group ID: %w", err,
            )
        }
        groupIDs = append(groupIDs, groupID)
    }

    return groupIDs, rows.Err()
}
```

In the following example, the hierarchy resolves group
memberships for nested groups:

```
Engineering (group)
+-- Backend Team (group)
|   +-- Alice (user)
|   +-- Bob (user)
+-- Frontend Team (group)
    +-- Charlie (user)

If Alice is in Backend Team and Backend Team is in
Engineering:
- GetUserGroups(Alice) returns
  [Backend Team ID, Engineering ID]
- Alice inherits privileges from both groups
```

### Circular Reference Prevention

The system validates group hierarchies to prevent circular
references.

```go
func ValidateGroupHierarchy(
    ctx context.Context,
    pool *pgxpool.Pool,
    parentGroupID int,
    memberGroupID int,
) error {
    if parentGroupID == memberGroupID {
        return fmt.Errorf(
            "a group cannot be a member of itself",
        )
    }

    query := `
        WITH RECURSIVE ancestor_groups AS (
            SELECT $1::INTEGER AS group_id

            UNION

            SELECT gm.parent_group_id
            FROM group_memberships gm
            INNER JOIN ancestor_groups ag
                ON gm.member_group_id = ag.group_id
        )
        SELECT COUNT(*)
        FROM ancestor_groups
        WHERE group_id = $2;
    `

    var matchCount int
    err := pool.QueryRow(
        ctx, query, parentGroupID, memberGroupID,
    ).Scan(&matchCount)
    if err != nil {
        return fmt.Errorf(
            "failed to validate hierarchy: %w", err,
        )
    }

    if matchCount > 0 {
        return fmt.Errorf(
            "adding this membership would create "
            + "a circular reference",
        )
    }

    return nil
}
```

### Superuser Privileges

Superusers bypass all authorization checks. A superuser
can perform the following actions:

- Access any connection with read and write permissions.
- Use any MCP tool without privilege checks.
- View any resource without restrictions.
- Manage all users, groups, and privileges.

Superuser status is determined by `is_superuser` on the
`users` table. API tokens inherit superuser status from
the owning user account.

Grant superuser status sparingly; only trusted
administrators should have this privilege.

### Connection Ownership

Connections support three access modes:

- Private connections are accessible only to the owner.
- Shared connections with no groups are accessible only
  to the owner and superusers.
- Shared connections with groups are accessible to members
  of the specified groups.

In the following example, the SQL statements create and
share a connection:

```sql
-- Create a private connection
INSERT INTO connections (name, owner_username, is_shared)
VALUES ('my-db', 'alice', FALSE);

-- Share the connection with groups
UPDATE connections SET is_shared = TRUE WHERE id = 1;
INSERT INTO connection_privileges
    (connection_id, group_id, access_level)
VALUES (1, 10, 'read'), (1, 11, 'read_write');
```

### MCP Privilege Seeding

The server seeds MCP privilege identifiers at startup.

In the following example, the `SeedMCPPrivileges` function
registers privilege identifiers:

```go
func SeedMCPPrivileges(
    ctx context.Context,
    pool *pgxpool.Pool,
) error {
    privileges := []struct {
        Identifier  string
        Description string
    }{
        {
            "tool:execute_query",
            "Execute SQL queries on connections",
        },
        {
            "tool:create_user",
            "Create new user accounts",
        },
        {
            "tool:delete_user",
            "Delete user accounts",
        },
        {
            "resource:users",
            "View user account list",
        },
        {
            "resource:connections",
            "View connection list",
        },
        // ... more privileges
    }

    for _, priv := range privileges {
        _, err := pool.Exec(ctx, `
            INSERT INTO mcp_privilege_identifiers
                (identifier, description)
            VALUES ($1, $2)
            ON CONFLICT (identifier) DO NOTHING
        `, priv.Identifier, priv.Description)
        if err != nil {
            return err
        }
    }

    return nil
}
```

## Authorization Flow in MCP Handlers

### Tool Call Authorization

In the following example, the handler checks MCP tool
privileges before executing the tool:

```go
func (h *Handler) handleToolCall(
    req Request,
) (*Response, error) {
    var params struct {
        Name      string                 `json:"name"`
        Arguments map[string]interface{} `json:"arguments"`
    }
    if err := json.Unmarshal(
        req.Params, &params,
    ); err != nil {
        return NewErrorResponse(
            req.ID, InvalidParams,
            "Invalid parameters", err.Error(),
        ), nil
    }

    privilegeID := fmt.Sprintf("tool:%s", params.Name)
    canAccess, err := privileges.CanAccessMCPItem(
        context.Background(), h.dbPool,
        h.userInfo.UserID, privilegeID,
    )
    if err != nil {
        return NewErrorResponse(
            req.ID, InternalError,
            "Failed to check privileges", err.Error(),
        ), nil
    }
    if !canAccess {
        return NewErrorResponse(
            req.ID, InvalidRequest,
            "Access denied to tool", nil,
        ), nil
    }

    result, err := h.routeToolCall(
        params.Name, params.Arguments,
    )
    if err != nil {
        return NewErrorResponse(
            req.ID, InternalError,
            "Tool execution failed", err.Error(),
        ), nil
    }

    return NewResponse(req.ID, result), nil
}
```

### Resource Access Authorization

In the following example, the handler checks resource
privileges before returning data:

```go
func (h *Handler) handleReadResource(
    req Request,
) (*Response, error) {
    var params struct {
        URI string `json:"uri"`
    }
    if err := json.Unmarshal(
        req.Params, &params,
    ); err != nil {
        return NewErrorResponse(
            req.ID, InvalidParams,
            "Invalid parameters", err.Error(),
        ), nil
    }

    resourceID := params.URI
    canAccess, err := privileges.CanAccessMCPItem(
        context.Background(), h.dbPool,
        h.userInfo.UserID, resourceID,
    )
    if err != nil {
        return NewErrorResponse(
            req.ID, InternalError,
            "Failed to check privileges", err.Error(),
        ), nil
    }
    if !canAccess {
        return NewErrorResponse(
            req.ID, InvalidRequest,
            "Access denied to resource", nil,
        ), nil
    }

    data, err := h.fetchResourceData(params.URI)
    if err != nil {
        return NewErrorResponse(
            req.ID, InternalError,
            "Failed to fetch resource", err.Error(),
        ), nil
    }

    return NewResponse(req.ID, data), nil
}
```

## Best Practices

The following best practices apply to authentication and
authorization:

- Always hash tokens before storage in the database.
- Use HTTPS to protect bearer tokens in transit.
- Validate token expiry on every request.
- Deny access by default if privileges are unclear.
- Log authentication failures and privilege violations.
- Encourage regular token rotation for security.
- Set reasonable maximum lifetimes for tokens.
- Grant `read_write` access only when necessary.
- Audit superuser accounts on a regular basis.
- Validate group hierarchies to prevent circular references.

## Security Considerations

The following security considerations apply to the system:

- Use a cryptographically secure random number generator
  for token generation.
- Consider bcrypt or Argon2 for hashing user passwords.
- Always use HTTPS for token transmission in production.
- Never log or display tokens after creation.
- Implement rate limiting for authentication attempts.
- Provide mechanisms to revoke tokens when compromised.
- Log all authentication and authorization events.
- Grant the minimum required privileges to each user.
- Regularly audit group memberships and privileges.
- Implement token rotation policies for long-lived tokens.
