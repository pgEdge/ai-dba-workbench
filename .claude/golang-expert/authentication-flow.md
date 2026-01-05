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

This document describes the authentication and authorization mechanisms
implemented in the pgEdge AI DBA Workbench server.

## Authentication

### Token Types

The system supports three types of authentication tokens:

1. **User Passwords:** User account passwords that can be used as bearer tokens
2. **Service Tokens:** Long-lived tokens for automated services
3. **User Tokens:** Time-limited tokens created by users for API access

All tokens are stored as SHA256 hashes in the database and never in plaintext.

### Token Storage Schema

```sql
-- User accounts
CREATE TABLE user_accounts (
    id SERIAL PRIMARY KEY,
    username TEXT UNIQUE NOT NULL,
    email TEXT NOT NULL,
    full_name TEXT NOT NULL,
    password_hash TEXT NOT NULL,  -- SHA256 hash
    password_expiry TIMESTAMP,
    is_superuser BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- Service tokens
CREATE TABLE service_tokens (
    id SERIAL PRIMARY KEY,
    name TEXT UNIQUE NOT NULL,
    token_hash TEXT UNIQUE NOT NULL,  -- SHA256 hash
    is_superuser BOOLEAN DEFAULT FALSE,
    note TEXT,
    expires_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- User tokens
CREATE TABLE user_tokens (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES user_accounts(id) ON DELETE CASCADE,
    name TEXT,
    token_hash TEXT UNIQUE NOT NULL,  -- SHA256 hash
    note TEXT,
    expires_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);
```

### Token Hashing

```go
func HashPassword(password string) string {
    hash := sha256.Sum256([]byte(password))
    return fmt.Sprintf("%x", hash)
}
```

**Security Note:** SHA256 is used for token hashing because tokens are
randomly generated with high entropy (32 bytes). For user passwords in
production, consider using bcrypt or Argon2 instead.

### Token Generation

```go
func GenerateToken() (string, error) {
    bytes := make([]byte, 32)
    if _, err := rand.Read(bytes); err != nil {
        return "", fmt.Errorf("failed to generate token: %w", err)
    }
    return base64.URLEncoding.EncodeToString(bytes), nil
}
```

**Token Format:** 43-character URL-safe base64 string (32 bytes of entropy)

### Authentication Validation

The server validates bearer tokens on each request:

```go
func (h *Handler) validateToken(bearerToken string) (*UserInfo, error) {
    if bearerToken == "" {
        return nil, fmt.Errorf("no bearer token provided")
    }

    // Hash the provided token
    tokenHash := usermgmt.HashPassword(bearerToken)
    ctx := context.Background()

    // Try service token first (most specific)
    userInfo, err := h.checkServiceToken(ctx, tokenHash)
    if err == nil {
        return userInfo, nil
    }

    // Try user password second
    userInfo, err = h.checkUserPassword(ctx, tokenHash)
    if err == nil {
        return userInfo, nil
    }

    // Try user token last
    userInfo, err = h.checkUserToken(ctx, tokenHash)
    if err == nil {
        return userInfo, nil
    }

    return nil, fmt.Errorf("invalid token")
}
```

### Token Expiry Handling

All token types support optional expiration:

```go
func (h *Handler) checkServiceToken(
    ctx context.Context,
    tokenHash string,
) (*UserInfo, error) {
    var id int
    var name string
    var isSuperuser bool
    var expiresAt sql.NullTime

    err := h.dbPool.QueryRow(ctx, `
        SELECT id, name, is_superuser, expires_at
        FROM service_tokens
        WHERE token_hash = $1
    `, tokenHash).Scan(&id, &name, &isSuperuser, &expiresAt)

    if err != nil {
        return nil, err
    }

    // Check expiry
    if expiresAt.Valid && time.Now().After(expiresAt.Time) {
        return nil, fmt.Errorf("token expired")
    }

    return &UserInfo{
        UserID:          id,
        Username:        name,
        IsSuperuser:     isSuperuser,
        IsAuthenticated: true,
        TokenType:       "service",
    }, nil
}
```

### User Token Lifecycle

User tokens provide a secure way for users to create API tokens without using
their password:

```go
// Create user token (requires authenticated session)
message, token, err := usermgmt.CreateUserTokenNonInteractive(
    pool,
    username,           // User creating the token
    &name,             // Optional token name
    90,                // Lifetime in days (0 = indefinite)
    maxLifetimeDays,   // Server-configured maximum (0 = no limit)
    &note,             // Optional note
)

// Token is returned ONCE to the user
fmt.Printf("Token: %s\n", token)
fmt.Println("Save this token - it cannot be retrieved again")
```

**Key Features:**
- Tokens created by authenticated users via MCP tool
- Configurable maximum lifetime (server-wide setting)
- Optional expiration (0 = indefinite, subject to max lifetime)
- Optional name and note for identification
- Tokens inherit user's privileges (not superuser unless user is)
- Can be listed and deleted by owning user

### HTTP Bearer Token Format

Clients authenticate using the HTTP Authorization header:

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

Only these methods don't require authentication:

1. `initialize` - Protocol handshake
2. `ping` - Health check
3. `tools/call` with `authenticate_user` tool - Initial login

All other methods require a valid bearer token.

## Authorization (RBAC)

### Role-Based Access Control Model

The system implements a flexible RBAC model with:

- **Users:** Individual user accounts or service tokens
- **Groups:** Collections of users and/or other groups (hierarchical)
- **Connections:** Database connections with owner and sharing settings
- **Privileges:** Permissions on connections and MCP items

### Database Schema

```sql
-- Groups
CREATE TABLE groups (
    id SERIAL PRIMARY KEY,
    name TEXT UNIQUE NOT NULL,
    description TEXT,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- Group memberships (supports nested groups)
CREATE TABLE group_memberships (
    id SERIAL PRIMARY KEY,
    parent_group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    member_user_id INTEGER REFERENCES user_accounts(id) ON DELETE CASCADE,
    member_group_id INTEGER REFERENCES groups(id) ON DELETE CASCADE,
    created_at TIMESTAMP DEFAULT NOW(),
    CONSTRAINT check_member CHECK (
        (member_user_id IS NOT NULL AND member_group_id IS NULL) OR
        (member_user_id IS NULL AND member_group_id IS NOT NULL)
    )
);

-- Connection privileges
CREATE TABLE connection_privileges (
    id SERIAL PRIMARY KEY,
    connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    access_level TEXT NOT NULL CHECK (access_level IN ('read', 'read_write')),
    created_at TIMESTAMP DEFAULT NOW(),
    UNIQUE(connection_id, group_id)
);

-- MCP privilege identifiers
CREATE TABLE mcp_privilege_identifiers (
    id SERIAL PRIMARY KEY,
    identifier TEXT UNIQUE NOT NULL,
    description TEXT,
    created_at TIMESTAMP DEFAULT NOW()
);

-- Group MCP privileges
CREATE TABLE group_mcp_privileges (
    id SERIAL PRIMARY KEY,
    group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    privilege_identifier_id INTEGER NOT NULL
        REFERENCES mcp_privilege_identifiers(id) ON DELETE CASCADE,
    created_at TIMESTAMP DEFAULT NOW(),
    UNIQUE(group_id, privilege_identifier_id)
);
```

### Privilege Checking Functions

#### Connection Access

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
        "SELECT is_superuser FROM user_accounts WHERE id = $1",
        userID).Scan(&isSuperuser)
    if err != nil {
        return false, fmt.Errorf("failed to check superuser status: %w", err)
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
        return false, fmt.Errorf("failed to check connection: %w", err)
    }

    // 3. If not shared, check ownership
    if !isShared {
        var ownerUsername *string
        err = pool.QueryRow(ctx,
            "SELECT owner_username FROM connections WHERE id = $1",
            connectionID).Scan(&ownerUsername)
        if err != nil {
            return false, fmt.Errorf("failed to check owner: %w", err)
        }

        var username string
        err = pool.QueryRow(ctx,
            "SELECT username FROM user_accounts WHERE id = $1",
            userID).Scan(&username)
        if err != nil {
            return false, fmt.Errorf("failed to get username: %w", err)
        }

        if ownerUsername != nil && *ownerUsername == username {
            return true, nil
        }

        return false, nil // Not shared and not owned by user
    }

    // 4. For shared connections, check group privileges
    var groupCount int
    err = pool.QueryRow(ctx,
        "SELECT COUNT(*) FROM connection_privileges WHERE connection_id = $1",
        connectionID).Scan(&groupCount)
    if err != nil {
        return false, fmt.Errorf("failed to count privileges: %w", err)
    }

    // If no groups assigned, deny access (security default)
    if groupCount == 0 {
        return false, nil
    }

    // 5. Get all groups user belongs to (recursive)
    userGroups, err := GetUserGroups(ctx, pool, userID)
    if err != nil {
        return false, fmt.Errorf("failed to get user groups: %w", err)
    }

    if len(userGroups) == 0 {
        return false, nil
    }

    // 6. Check if any of user's groups have required access
    query := `
        SELECT COUNT(*)
        FROM connection_privileges
        WHERE connection_id = $1
          AND group_id = ANY($2)
          AND (
              access_level = 'read_write'
              OR ($3 = 'read' AND access_level = 'read')
          )
    `

    var matchCount int
    err = pool.QueryRow(ctx, query, connectionID, userGroups,
        requestedLevel).Scan(&matchCount)
    if err != nil {
        return false, fmt.Errorf("failed to check privileges: %w", err)
    }

    return matchCount > 0, nil
}
```

**Access Levels:**
- `read`: Can query, view schema, test connection
- `read_write`: Can execute DML (INSERT, UPDATE, DELETE, DDL)

#### MCP Item Access

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
        "SELECT is_superuser FROM user_accounts WHERE id = $1",
        userID).Scan(&isSuperuser)
    if err != nil {
        return false, fmt.Errorf("failed to check superuser: %w", err)
    }
    if isSuperuser {
        return true, nil
    }

    // 2. Check if privilege identifier exists
    var privilegeID int
    err = pool.QueryRow(ctx,
        "SELECT id FROM mcp_privilege_identifiers WHERE identifier = $1",
        itemIdentifier).Scan(&privilegeID)
    if err != nil {
        // Privilege identifier doesn't exist, deny by default
        return false, fmt.Errorf("privilege identifier not found: %s",
            itemIdentifier)
    }

    // 3. Check if any groups have this privilege
    var groupCount int
    err = pool.QueryRow(ctx,
        "SELECT COUNT(*) FROM group_mcp_privileges WHERE privilege_identifier_id = $1",
        privilegeID).Scan(&groupCount)
    if err != nil {
        return false, fmt.Errorf("failed to count privileges: %w", err)
    }

    // If no groups assigned, deny access (security default)
    if groupCount == 0 {
        return false, nil
    }

    // 4. Get all groups user belongs to
    userGroups, err := GetUserGroups(ctx, pool, userID)
    if err != nil {
        return false, fmt.Errorf("failed to get user groups: %w", err)
    }

    if len(userGroups) == 0 {
        return false, nil
    }

    // 5. Check if any of user's groups have this privilege
    query := `
        SELECT COUNT(*)
        FROM group_mcp_privileges
        WHERE privilege_identifier_id = $1
          AND group_id = ANY($2)
    `

    var matchCount int
    err = pool.QueryRow(ctx, query, privilegeID, userGroups).Scan(&matchCount)
    if err != nil {
        return false, fmt.Errorf("failed to check privileges: %w", err)
    }

    return matchCount > 0, nil
}
```

**MCP Item Identifiers:**
- Tools: `tool:execute_query`, `tool:create_user`, etc.
- Resources: `resource:users`, `resource:connections`, etc.
- Prompts: `prompt:query_helper`, etc.

### Recursive Group Membership

Groups can contain other groups, creating a hierarchy. The system uses
recursive CTEs to resolve the full membership tree:

```go
func GetUserGroups(
    ctx context.Context,
    pool *pgxpool.Pool,
    userID int,
) ([]int, error) {
    query := `
        WITH RECURSIVE user_groups_recursive AS (
            -- Base case: direct group memberships
            SELECT parent_group_id as group_id
            FROM group_memberships
            WHERE member_user_id = $1

            UNION

            -- Recursive case: groups containing groups we're in
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
        return nil, fmt.Errorf("failed to query user groups: %w", err)
    }
    defer rows.Close()

    groupIDs := make([]int, 0)
    for rows.Next() {
        var groupID int
        if err := rows.Scan(&groupID); err != nil {
            return nil, fmt.Errorf("failed to scan group ID: %w", err)
        }
        groupIDs = append(groupIDs, groupID)
    }

    return groupIDs, rows.Err()
}
```

**Example Hierarchy:**
```
Engineering (group)
├── Backend Team (group)
│   ├── Alice (user)
│   └── Bob (user)
└── Frontend Team (group)
    └── Charlie (user)

If Alice is in "Backend Team" and "Backend Team" is in "Engineering":
- GetUserGroups(Alice) returns [Backend Team ID, Engineering ID]
- Alice inherits privileges from both groups
```

### Circular Reference Prevention

When adding group memberships, the system validates that no circular
references are created:

```go
func ValidateGroupHierarchy(
    ctx context.Context,
    pool *pgxpool.Pool,
    parentGroupID int,
    memberGroupID int,
) error {
    // A group cannot be a member of itself
    if parentGroupID == memberGroupID {
        return fmt.Errorf("a group cannot be a member of itself")
    }

    // Check if adding this membership would create a cycle
    query := `
        WITH RECURSIVE ancestor_groups AS (
            -- Base case: the group we want to add as parent
            SELECT $1::INTEGER as group_id

            UNION

            -- Recursive case: find groups containing this group
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
    err := pool.QueryRow(ctx, query, parentGroupID, memberGroupID).Scan(&matchCount)
    if err != nil {
        return fmt.Errorf("failed to validate hierarchy: %w", err)
    }

    if matchCount > 0 {
        return fmt.Errorf("adding this membership would create a circular reference")
    }

    return nil
}
```

### Superuser Privileges

Superusers bypass all authorization checks:

- Can access any connection (read and write)
- Can use any MCP tool
- Can view any resource
- Can manage all users, groups, and privileges

**Security Note:** Superuser status should be granted sparingly and only to
trusted administrators.

### Connection Ownership

Connections can be:

1. **Private (not shared):** Only accessible to the owner
2. **Shared with no groups:** Accessible to no one (except owner and superusers)
3. **Shared with groups:** Accessible to members of specified groups

```go
// Create private connection
INSERT INTO connections (name, owner_username, is_shared, ...)
VALUES ('my-db', 'alice', FALSE, ...);

// Share connection with groups
UPDATE connections SET is_shared = TRUE WHERE id = 1;
INSERT INTO connection_privileges (connection_id, group_id, access_level)
VALUES (1, 10, 'read'), (1, 11, 'read_write');
```

### MCP Privilege Seeding

The server seeds MCP privilege identifiers at startup:

```go
func SeedMCPPrivileges(ctx context.Context, pool *pgxpool.Pool) error {
    privileges := []struct {
        Identifier  string
        Description string
    }{
        {"tool:execute_query", "Execute SQL queries on connections"},
        {"tool:create_user", "Create new user accounts"},
        {"tool:delete_user", "Delete user accounts"},
        {"resource:users", "View user account list"},
        {"resource:connections", "View connection list"},
        // ... more privileges
    }

    for _, priv := range privileges {
        _, err := pool.Exec(ctx, `
            INSERT INTO mcp_privilege_identifiers (identifier, description)
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

```go
func (h *Handler) handleToolCall(req Request) (*Response, error) {
    // 1. Extract tool name and arguments
    var params struct {
        Name      string                 `json:"name"`
        Arguments map[string]interface{} `json:"arguments"`
    }
    if err := json.Unmarshal(req.Params, &params); err != nil {
        return NewErrorResponse(req.ID, InvalidParams,
            "Invalid parameters", err.Error()), nil
    }

    // 2. Check if user has privilege to use this tool
    privilegeID := fmt.Sprintf("tool:%s", params.Name)
    canAccess, err := privileges.CanAccessMCPItem(
        context.Background(), h.dbPool, h.userInfo.UserID, privilegeID)
    if err != nil {
        return NewErrorResponse(req.ID, InternalError,
            "Failed to check privileges", err.Error()), nil
    }
    if !canAccess {
        return NewErrorResponse(req.ID, InvalidRequest,
            "Access denied to tool", nil), nil
    }

    // 3. Route to tool handler (may have additional checks)
    result, err := h.routeToolCall(params.Name, params.Arguments)
    if err != nil {
        return NewErrorResponse(req.ID, InternalError,
            "Tool execution failed", err.Error()), nil
    }

    return NewResponse(req.ID, result), nil
}
```

### Resource Access Authorization

```go
func (h *Handler) handleReadResource(req Request) (*Response, error) {
    // 1. Extract URI
    var params struct {
        URI string `json:"uri"`
    }
    if err := json.Unmarshal(req.Params, &params); err != nil {
        return NewErrorResponse(req.ID, InvalidParams,
            "Invalid parameters", err.Error()), nil
    }

    // 2. Check resource access
    resourceID := params.URI // e.g., "ai-workbench://users"
    canAccess, err := privileges.CanAccessMCPItem(
        context.Background(), h.dbPool, h.userInfo.UserID, resourceID)
    if err != nil {
        return NewErrorResponse(req.ID, InternalError,
            "Failed to check privileges", err.Error()), nil
    }
    if !canAccess {
        return NewErrorResponse(req.ID, InvalidRequest,
            "Access denied to resource", nil), nil
    }

    // 3. Fetch resource data
    data, err := h.fetchResourceData(params.URI)
    if err != nil {
        return NewErrorResponse(req.ID, InternalError,
            "Failed to fetch resource", err.Error()), nil
    }

    return NewResponse(req.ID, data), nil
}
```

## Best Practices

1. **Always Hash Tokens:** Never store tokens in plaintext
2. **Use HTTPS:** Protect bearer tokens in transit
3. **Validate Expiry:** Check token expiration on every request
4. **Default Deny:** Deny access if privileges are unclear
5. **Log Auth Events:** Log authentication failures and privilege violations
6. **Rotate Tokens:** Encourage regular token rotation
7. **Limit Token Lifetime:** Set reasonable maximum lifetimes
8. **Use Read-Only Access:** Grant read_write only when needed
9. **Review Superusers:** Audit superuser accounts regularly
10. **Validate Group Hierarchies:** Prevent circular references

## Security Considerations

1. **Token Generation:** Use cryptographically secure random number generator
2. **Hash Algorithm:** Consider bcrypt/Argon2 for user passwords
3. **Token Transmission:** Always use HTTPS in production
4. **Token Storage:** Never log or display tokens after creation
5. **Rate Limiting:** Implement rate limiting for authentication attempts
6. **Session Invalidation:** Provide mechanism to revoke tokens
7. **Audit Logging:** Log all authentication and authorization events
8. **Least Privilege:** Grant minimum required privileges
9. **Group Review:** Regularly audit group memberships and privileges
10. **Token Rotation:** Implement token rotation policies
