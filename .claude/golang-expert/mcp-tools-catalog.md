/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - MCP Tools and Resources Catalog
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

# MCP Tools and Resources Catalog

This document catalogs all MCP tools and resources. For protocol
implementation details, see `mcp-implementation.md`.

## Tools Overview

The server provides 27 tools in 7 categories. All tools are
invoked via the `tools/call` JSON-RPC method and return text
content responses.

**Common Response Format:**

```json
{
    "content": [{
        "type": "text",
        "text": "Operation result or JSON data"
    }]
}
```

---

## 1. User Management (3 tools)

### create_user

- **Purpose:** Create a new user account.
- **Auth:** Superuser or `create_user` MCP privilege.
- **Handler:** `handleCreateUser()`
- **Required:** `username`, `email`, `fullName`, `password`
- **Optional:** `isSuperuser` (bool), `passwordExpiry` (YYYY-MM-DD)

### update_user

- **Purpose:** Update an existing user account.
- **Auth:** Superuser or `update_user` MCP privilege.
- **Handler:** `handleUpdateUser()`
- **Required:** `username`
- **Optional:** `email`, `fullName`, `password`, `isSuperuser`,
  `passwordExpiry`, `clearPasswordExpiry` (bool)

### delete_user

- **Purpose:** Delete a user account.
- **Auth:** Superuser or `delete_user` MCP privilege.
- **Handler:** `handleDeleteUser()`
- **Required:** `username`

---

## 2. Service Token Management (3 tools)

### create_service_token

- **Purpose:** Create a service token for programmatic access.
- **Auth:** Superuser or `create_service_token` MCP privilege.
- **Handler:** `handleCreateServiceToken()`
- **Required:** `name`
- **Optional:** `isSuperuser` (bool), `note`, `expiresAt`
  (YYYY-MM-DD)
- **Note:** Returns plaintext token only once; cannot be
  retrieved again.

### update_service_token

- **Purpose:** Update a service token's properties.
- **Auth:** Superuser or `update_service_token` MCP privilege.
- **Handler:** `handleUpdateServiceToken()`
- **Required:** `name`
- **Optional:** `isSuperuser`, `note`, `expiresAt`,
  `clearNote` (bool), `clearExpiresAt` (bool)
- **Note:** Cannot update the token value; delete and recreate.

### delete_service_token

- **Purpose:** Delete a service token.
- **Auth:** Superuser or `delete_service_token` MCP privilege.
- **Handler:** `handleDeleteServiceToken()`
- **Required:** `name`

---

## 3. User Token Management (3 tools)

### create_user_token

- **Purpose:** Create a personal CLI/API token.
- **Auth:** Self-service (own tokens) or superuser.
- **Handler:** `handleCreateUserToken()`
- **Required:** `username`, `lifetimeDays` (int, 0 = indefinite)
- **Optional:** `name`, `note`
- **Note:** Respects `max_user_token_lifetime_days` config.

### list_user_tokens

- **Purpose:** List tokens for a user.
- **Auth:** Self-service (own tokens) or superuser.
- **Handler:** `handleListUserTokens()`
- **Required:** `username`

### delete_user_token

- **Purpose:** Delete a user token by ID.
- **Auth:** Self-service (own tokens) or superuser.
- **Handler:** `handleDeleteUserToken()`
- **Required:** `username`, `tokenId` (int)

---

## 4. Group Management (7 tools)

### create_user_group

- **Purpose:** Create a user group.
- **Auth:** Superuser or `create_user_group` MCP privilege.
- **Handler:** `handleCreateUserGroup()`
- **Required:** `name`
- **Optional:** `description`

### update_user_group

- **Purpose:** Update a user group.
- **Auth:** Superuser or `update_user_group` MCP privilege.
- **Handler:** `handleUpdateUserGroup()`
- **Required:** `groupId` (int), `name`, `description`

### delete_user_group

- **Purpose:** Delete a user group (cascades memberships and
  privileges).
- **Auth:** Superuser or `delete_user_group` MCP privilege.
- **Handler:** `handleDeleteUserGroup()`
- **Required:** `groupId` (int)

### list_user_groups

- **Purpose:** List all user groups.
- **Auth:** Superuser or `list_user_groups` MCP privilege.
- **Handler:** `handleListUserGroups()`
- **Required:** None

### add_group_member

- **Purpose:** Add a user or nested group to a parent group.
- **Auth:** Superuser or `add_group_member` MCP privilege.
- **Handler:** `handleAddGroupMember()`
- **Required:** `parentGroupId` (int)
- **Optional:** `memberUserId` (int) or `memberGroupId` (int);
  mutually exclusive.

### remove_group_member

- **Purpose:** Remove a user or nested group from a parent
  group.
- **Auth:** Superuser or `remove_group_member` MCP privilege.
- **Handler:** `handleRemoveGroupMember()`
- **Required:** `parentGroupId` (int)
- **Optional:** `memberUserId` (int) or `memberGroupId` (int)

### list_group_members

- **Purpose:** List members of a group.
- **Auth:** Superuser or `list_group_members` MCP privilege.
- **Handler:** `handleListGroupMembers()`
- **Required:** `groupId` (int)

### list_user_group_memberships

- **Purpose:** List all groups a user belongs to (direct and
  indirect via recursive resolution).
- **Auth:** Superuser or `list_user_group_memberships` MCP
  privilege.
- **Handler:** `handleListUserGroupMemberships()`
- **Required:** `username`

---

## 5. Connection Privilege Management (3 tools)

### grant_connection_privilege

- **Purpose:** Grant a group access to a database connection.
- **Auth:** Superuser or `grant_connection_privilege` MCP
  privilege.
- **Handler:** `handleGrantConnectionPrivilege()`
- **Required:** `groupId` (int), `connectionId` (int),
  `accessLevel` ("read" or "read_write")

### revoke_connection_privilege

- **Purpose:** Revoke a group's access to a connection.
- **Auth:** Superuser or `revoke_connection_privilege` MCP
  privilege.
- **Handler:** `handleRevokeConnectionPrivilege()`
- **Required:** `groupId` (int), `connectionId` (int)

### list_connection_privileges

- **Purpose:** List group privileges for a connection.
- **Auth:** Superuser or `list_connection_privileges` MCP
  privilege.
- **Handler:** `handleListConnectionPrivileges()`
- **Required:** `connectionId` (int)

---

## 6. MCP Privilege Management (4 tools)

### list_mcp_privilege_identifiers

- **Purpose:** List all registered MCP privilege identifiers.
- **Auth:** Superuser or `list_mcp_privilege_identifiers` MCP
  privilege.
- **Handler:** `handleListMCPPrivilegeIdentifiers()`
- **Required:** None
- **Note:** Identifiers are seeded at startup via
  `privileges.SeedMCPPrivileges()`.

### grant_mcp_privilege

- **Purpose:** Grant a group access to an MCP item.
- **Auth:** Superuser or `grant_mcp_privilege` MCP privilege.
- **Handler:** `handleGrantMCPPrivilege()`
- **Required:** `groupId` (int), `privilegeIdentifier` (string)

### revoke_mcp_privilege

- **Purpose:** Revoke a group's access to an MCP item.
- **Auth:** Superuser or `revoke_mcp_privilege` MCP privilege.
- **Handler:** `handleRevokeMCPPrivilege()`
- **Required:** `groupId` (int), `privilegeIdentifier` (string)

### list_group_mcp_privileges

- **Purpose:** List MCP privileges granted to a group.
- **Auth:** Superuser or `list_group_mcp_privileges` MCP
  privilege.
- **Handler:** `handleListGroupMCPPrivileges()`
- **Required:** `groupId` (int)

---

## 7. Token Scope Management (4 tools)

All token scope tools require **superuser** access.

### set_token_connection_scope

- **Purpose:** Limit a token to specific connections.
- **Handler:** `handleSetTokenConnectionScope()`
- **Required:** `tokenId` (int), `tokenType` ("user" or
  "service"), `connectionIds` (array of int)

### set_token_mcp_scope

- **Purpose:** Limit a token to specific MCP tools.
- **Handler:** `handleSetTokenMCPScope()`
- **Required:** `tokenId` (int), `tokenType` ("user" or
  "service"), `privilegeIdentifiers` (array of string)

### get_token_scope

- **Purpose:** Get scope restrictions for a token.
- **Handler:** `handleGetTokenScope()`
- **Required:** `tokenId` (int), `tokenType` ("user" or
  "service")

### clear_token_scope

- **Purpose:** Remove all scope restrictions for a token.
- **Handler:** `handleClearTokenScope()`
- **Required:** `tokenId` (int), `tokenType` ("user" or
  "service")

---

## Resources

Resources provide read-only access via the `resources/list` and
`resources/read` MCP methods. The URI scheme follows the pattern
`ai-workbench://<collection>`.

### User Accounts

- **URI:** `ai-workbench://users`
- **Auth:** Authenticated users.
- **Fields:** `username`, `email`, `fullName`, `isSuperuser`,
  `passwordExpiry`, `createdAt`, `updatedAt`
- **Security:** Password hashes are excluded.

### Service Tokens

- **URI:** `ai-workbench://service-tokens`
- **Auth:** Authenticated users.
- **Fields:** `name`, `isSuperuser`, `note`, `expiresAt`,
  `createdAt`, `updatedAt`
- **Security:** Token values are excluded.

### Resource Implementation Pattern

Resources are read-only and route by URI in
`handleReadResource()`:

```go
switch params.URI {
case "ai-workbench://users":
    // Query users table, format as JSON content items
case "ai-workbench://service-tokens":
    // Query service_tokens table, format as JSON
default:
    return NewErrorResponse(req.ID, InvalidParams,
        "Unknown resource URI", nil), nil
}
```

Each row becomes a separate content item with its own URI
(e.g., `ai-workbench://users/alice`).
