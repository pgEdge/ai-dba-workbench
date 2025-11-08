# MCP Tools Catalog

This document catalogs all available MCP tools, their purposes, authorization requirements, and implementation details.

## Tool Categories

1. **Authentication** (1 tool)
2. **User Management** (3 tools)
3. **Service Token Management** (3 tools)
4. **User Token Management** (3 tools)
5. **Group Management** (7 tools)
6. **Connection Privilege Management** (3 tools)
7. **MCP Privilege Management** (4 tools)
8. **Token Scope Management** (4 tools)

**Total: 28 tools**

---

## 1. Authentication Tools

### authenticate_user

**Purpose:** Authenticate a user and obtain a session token

**Authorization:** None required (public endpoint)

**Input Schema:**
```json
{
    "username": "string (required)",
    "password": "string (required)"
}
```

**Handler:** `handleAuthenticateUser()`

**Implementation Details:**
- Queries `user_accounts` table for username
- Verifies password hash using `usermgmt.HashPassword()`
- Checks password expiry if set
- Generates session token via `usermgmt.GenerateToken()`
- Creates session in `user_sessions` table with 24-hour expiration
- Returns session token to client

**Success Response:**
```json
{
    "content": [{
        "type": "text",
        "text": "Authentication successful. Session token: <token>\nExpires at: <timestamp>"
    }]
}
```

**Common Errors:**
- Invalid username or password
- Password has expired

---

## 2. User Management Tools

### create_user

**Purpose:** Create a new user account

**Authorization:**
- Superusers: Allowed
- Regular users: Requires `create_user` MCP privilege via group membership
- Service tokens: Must be superuser

**Input Schema:**
```json
{
    "username": "string (required)",
    "email": "string (required)",
    "fullName": "string (required)",
    "password": "string (required)",
    "isSuperuser": "boolean (optional, default: false)",
    "passwordExpiry": "string (optional, YYYY-MM-DD format)"
}
```

**Handler:** `handleCreateUser()`

**Implementation Details:**
- Delegates to `usermgmt.CreateUserNonInteractive()`
- Password is hashed before storage
- Email validation performed
- Username uniqueness enforced by database constraint

### update_user

**Purpose:** Update an existing user account

**Authorization:** Same as create_user (requires `update_user` privilege)

**Input Schema:**
```json
{
    "username": "string (required)",
    "email": "string (optional)",
    "fullName": "string (optional)",
    "password": "string (optional)",
    "isSuperuser": "boolean (optional)",
    "passwordExpiry": "string (optional, YYYY-MM-DD format)",
    "clearPasswordExpiry": "boolean (optional, default: false)"
}
```

**Handler:** `handleUpdateUser()`

**Implementation Details:**
- Only updates fields that are provided
- Password is hashed if provided
- `clearPasswordExpiry` flag sets expiry to NULL

### delete_user

**Purpose:** Delete a user account

**Authorization:** Same as create_user (requires `delete_user` privilege)

**Input Schema:**
```json
{
    "username": "string (required)"
}
```

**Handler:** `handleDeleteUser()`

**Implementation Details:**
- Delegates to `usermgmt.DeleteUserNonInteractive()`
- Cascading deletes handle related records

---

## 3. Service Token Management Tools

### create_service_token

**Purpose:** Create a service token for programmatic access

**Authorization:**
- Superusers: Allowed
- Regular users: Requires `create_service_token` MCP privilege
- Service tokens: Must be superuser

**Input Schema:**
```json
{
    "name": "string (required)",
    "isSuperuser": "boolean (optional, default: false)",
    "note": "string (optional)",
    "expiresAt": "string (optional, YYYY-MM-DD format)"
}
```

**Handler:** `handleCreateServiceToken()`

**Implementation Details:**
- Generates cryptographically secure token
- Stores hashed token in `service_tokens` table
- Returns plaintext token only once
- Token cannot be retrieved again after creation

**Success Response:**
```json
{
    "content": [{
        "type": "text",
        "text": "Service token '<name>' created successfully.\nToken: <token>\nIMPORTANT: Save this token now. You won't be able to see it again."
    }]
}
```

### update_service_token

**Purpose:** Update a service token's properties (not the token itself)

**Authorization:** Same as create_service_token (requires `update_service_token` privilege)

**Input Schema:**
```json
{
    "name": "string (required)",
    "isSuperuser": "boolean (optional)",
    "note": "string (optional)",
    "expiresAt": "string (optional, YYYY-MM-DD format)",
    "clearNote": "boolean (optional, default: false)",
    "clearExpiresAt": "boolean (optional, default: false)"
}
```

**Handler:** `handleUpdateServiceToken()`

**Note:** Cannot update the token value itself; must delete and recreate

### delete_service_token

**Purpose:** Delete a service token

**Authorization:** Same as create_service_token (requires `delete_service_token` privilege)

**Input Schema:**
```json
{
    "name": "string (required)"
}
```

**Handler:** `handleDeleteServiceToken()`

---

## 4. User Token Management Tools

### create_user_token

**Purpose:** Create a user token for personal CLI/API access

**Authorization:**
- Users can create tokens for themselves
- Superusers can create tokens for any user

**Input Schema:**
```json
{
    "username": "string (required)",
    "name": "string (optional)",
    "lifetimeDays": "integer (required, 0 = indefinite)",
    "note": "string (optional)"
}
```

**Handler:** `handleCreateUserToken()`

**Implementation Details:**
- Respects server's `max_user_token_lifetime_days` configuration
- Token is hashed before storage
- Owned by specific user, not transferable

### list_user_tokens

**Purpose:** List all tokens for a specific user

**Authorization:**
- Users can list their own tokens
- Superusers can list tokens for any user

**Input Schema:**
```json
{
    "username": "string (required)"
}
```

**Handler:** `handleListUserTokens()`

**Success Response:**
```json
{
    "content": [{
        "type": "text",
        "text": "User tokens for '<username>':\n<JSON array of tokens>"
    }]
}
```

### delete_user_token

**Purpose:** Delete a user token by ID

**Authorization:** Same as list_user_tokens

**Input Schema:**
```json
{
    "username": "string (required)",
    "tokenId": "integer (required)"
}
```

**Handler:** `handleDeleteUserToken()`

---

## 5. Group Management Tools

### create_user_group

**Purpose:** Create a new user group for organizing users and permissions

**Authorization:** Requires `create_user_group` MCP privilege

**Input Schema:**
```json
{
    "name": "string (required, unique)",
    "description": "string (optional)"
}
```

**Handler:** `handleCreateUserGroup()`

**Implementation Details:**
- Delegates to `groupmgmt.CreateUserGroup()`
- Returns the new group ID

### update_user_group

**Purpose:** Update an existing user group

**Authorization:** Requires `update_user_group` MCP privilege

**Input Schema:**
```json
{
    "groupId": "integer (required)",
    "name": "string (required)",
    "description": "string (required)"
}
```

**Handler:** `handleUpdateUserGroup()`

### delete_user_group

**Purpose:** Delete a user group (cascades to memberships and privileges)

**Authorization:** Requires `delete_user_group` MCP privilege

**Input Schema:**
```json
{
    "groupId": "integer (required)"
}
```

**Handler:** `handleDeleteUserGroup()`

**Warning:** Deletes all group memberships and privilege grants

### list_user_groups

**Purpose:** List all user groups in the system

**Authorization:** Requires `list_user_groups` MCP privilege

**Input Schema:**
```json
{}
```

**Handler:** `handleListUserGroups()`

### add_group_member

**Purpose:** Add a user or nested group as a member of a parent group

**Authorization:** Requires `add_group_member` MCP privilege

**Input Schema:**
```json
{
    "parentGroupId": "integer (required)",
    "memberUserId": "integer (optional, mutually exclusive with memberGroupId)",
    "memberGroupId": "integer (optional, mutually exclusive with memberUserId)"
}
```

**Handler:** `handleAddGroupMember()`

**Implementation Details:**
- Supports nested groups for hierarchical organization
- Must specify either memberUserId OR memberGroupId
- Prevents circular membership

### remove_group_member

**Purpose:** Remove a user or nested group from a parent group

**Authorization:** Requires `remove_group_member` MCP privilege

**Input Schema:**
```json
{
    "parentGroupId": "integer (required)",
    "memberUserId": "integer (optional)",
    "memberGroupId": "integer (optional)"
}
```

**Handler:** `handleRemoveGroupMember()`

### list_group_members

**Purpose:** List all members (users and nested groups) of a group

**Authorization:** Requires `list_group_members` MCP privilege

**Input Schema:**
```json
{
    "groupId": "integer (required)"
}
```

**Handler:** `handleListGroupMembers()`

### list_user_group_memberships

**Purpose:** List all groups a user belongs to (direct and indirect)

**Authorization:** Requires `list_user_group_memberships` MCP privilege

**Input Schema:**
```json
{
    "username": "string (required)"
}
```

**Handler:** `handleListUserGroupMemberships()`

**Implementation Details:**
- Uses recursive query to resolve nested group memberships
- Shows both direct and inherited memberships

---

## 6. Connection Privilege Management Tools

### grant_connection_privilege

**Purpose:** Grant a group access to a database connection

**Authorization:** Requires `grant_connection_privilege` MCP privilege

**Input Schema:**
```json
{
    "groupId": "number (required)",
    "connectionId": "number (required)",
    "accessLevel": "string (required, 'read' or 'read_write')"
}
```

**Handler:** `handleGrantConnectionPrivilege()`

**Access Levels:**
- `read` - Read-only access to connection
- `read_write` - Full read-write access

### revoke_connection_privilege

**Purpose:** Revoke a group's access to a connection

**Authorization:** Requires `revoke_connection_privilege` MCP privilege

**Input Schema:**
```json
{
    "groupId": "number (required)",
    "connectionId": "number (required)"
}
```

**Handler:** `handleRevokeConnectionPrivilege()`

### list_connection_privileges

**Purpose:** List all group privileges for a connection

**Authorization:** Requires `list_connection_privileges` MCP privilege

**Input Schema:**
```json
{
    "connectionId": "number (required)"
}
```

**Handler:** `handleListConnectionPrivileges()`

---

## 7. MCP Privilege Management Tools

### list_mcp_privilege_identifiers

**Purpose:** List all registered MCP privilege identifiers

**Authorization:** Requires `list_mcp_privilege_identifiers` MCP privilege

**Input Schema:**
```json
{}
```

**Handler:** `handleListMCPPrivilegeIdentifiers()`

**Implementation Details:**
- Reads from `mcp_privilege_identifiers` table
- Shows identifier, item_type, and description
- Identifiers are seeded at server startup

### grant_mcp_privilege

**Purpose:** Grant a group access to an MCP tool, resource, or prompt

**Authorization:** Requires `grant_mcp_privilege` MCP privilege

**Input Schema:**
```json
{
    "groupId": "number (required)",
    "privilegeIdentifier": "string (required)"
}
```

**Handler:** `handleGrantMCPPrivilege()`

**Example Identifiers:**
- `create_user`
- `delete_service_token`
- `list_user_groups`

### revoke_mcp_privilege

**Purpose:** Revoke a group's access to an MCP item

**Authorization:** Requires `revoke_mcp_privilege` MCP privilege

**Input Schema:**
```json
{
    "groupId": "number (required)",
    "privilegeIdentifier": "string (required)"
}
```

**Handler:** `handleRevokeMCPPrivilege()`

### list_group_mcp_privileges

**Purpose:** List all MCP privileges granted to a group

**Authorization:** Requires `list_group_mcp_privileges` MCP privilege

**Input Schema:**
```json
{
    "groupId": "number (required)"
}
```

**Handler:** `handleListGroupMCPPrivileges()`

---

## 8. Token Scope Management Tools

### set_token_connection_scope

**Purpose:** Limit a token's access to specific connections

**Authorization:** Superuser required

**Input Schema:**
```json
{
    "tokenId": "number (required)",
    "tokenType": "string (required, 'user' or 'service')",
    "connectionIds": "array of numbers (required)"
}
```

**Handler:** `handleSetTokenConnectionScope()`

**Use Case:** Restrict a token to only access specific database connections

### set_token_mcp_scope

**Purpose:** Limit a token's access to specific MCP tools

**Authorization:** Superuser required

**Input Schema:**
```json
{
    "tokenId": "number (required)",
    "tokenType": "string (required, 'user' or 'service')",
    "privilegeIdentifiers": "array of strings (required)"
}
```

**Handler:** `handleSetTokenMCPScope()`

**Use Case:** Restrict a token to only execute specific MCP tools

### get_token_scope

**Purpose:** Get the scope restrictions for a token

**Authorization:** Superuser required

**Input Schema:**
```json
{
    "tokenId": "number (required)",
    "tokenType": "string (required, 'user' or 'service')"
}
```

**Handler:** `handleGetTokenScope()`

### clear_token_scope

**Purpose:** Remove all scope restrictions for a token

**Authorization:** Superuser required

**Input Schema:**
```json
{
    "tokenId": "number (required)",
    "tokenType": "string (required, 'user' or 'service')"
}
```

**Handler:** `handleClearTokenScope()`

---

## Tool Calling Mechanism

All tools are invoked via the `tools/call` method:

```json
{
    "jsonrpc": "2.0",
    "id": "1",
    "method": "tools/call",
    "params": {
        "name": "create_user",
        "arguments": {
            "username": "alice",
            "email": "alice@example.com",
            "fullName": "Alice Smith",
            "password": "SecurePass123!"
        }
    }
}
```

The `handleCallTool()` function:
1. Unmarshals the tool name and arguments
2. Routes to the appropriate handler function via switch statement
3. Executes the tool handler
4. Returns result or error

## Common Response Format

Most tools return text responses:

```json
{
    "content": [{
        "type": "text",
        "text": "Operation completed successfully"
    }]
}
```

Tools that return structured data (like list operations) format as JSON within the text field.

## Authorization Pattern

Most tools follow this authorization pattern:

```go
func (h *Handler) handleToolName(args map[string]interface{}) (interface{}, error) {
    // 1. Check authentication
    if h.userInfo == nil || !h.userInfo.IsAuthenticated {
        return nil, fmt.Errorf("authentication required")
    }

    // 2. Superusers bypass privilege checks
    if !h.userInfo.IsSuperuser {
        // 3. Check if it's a service token (may have restrictions)
        if h.userInfo.IsServiceToken {
            return nil, fmt.Errorf("permission denied")
        }

        // 4. For regular users, check MCP privilege via group membership
        canAccess, err := privileges.CanAccessMCPItem(ctx, h.dbPool, userID, "tool_name")
        if err != nil || !canAccess {
            return nil, fmt.Errorf("permission denied: insufficient privileges")
        }
    }

    // 5. Execute tool logic
    // ...
}
```

## Adding New Tools

See the `extending-mcp.md` document for step-by-step instructions on adding new tools.
