# API Reference: Privilege Management Tools

This document provides a complete reference for all 22 MCP tools related to
user, token, group, and privilege management in the pgEdge AI Workbench.

**Note:** List-oriented operations (like listing groups, group members, user
tokens, etc.) are now available as MCP Resources rather than tools. See
[MCP Resources documentation](server/mcp-resources.md) for details on using
resources for read-only data access.

## Table of Contents

- [User Management](#user-management)
- [Service Token Management](#service-token-management)
- [User Token Management](#user-token-management)
- [Group Management](#group-management)
- [Connection Privilege Management](#connection-privilege-management)
- [MCP Privilege Management](#mcp-privilege-management)
- [Token Scope Management](#token-scope-management)

## User Management

### authenticate_user

Authenticate a user with username and password.

**Authorization:** No authentication required (this is the authentication
endpoint)

**Input Schema:**

```json
{
  "username": "string (required)",
  "password": "string (required)"
}
```

**Example Request:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "authenticate_user",
    "arguments": {
      "username": "alice",
      "password": "secret123"
    }
  }
}
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "success": true,
    "user_id": 5,
    "username": "alice",
    "email": "alice@example.com",
    "is_superuser": false,
    "session_token": "tok_abc123..."
  }
}
```

**Errors:**

- "invalid username or password" - Authentication failed
- "account is disabled" - User account is not active

---

### create_user

Create a new user account.

**Authorization:** Requires superuser privileges or group membership granting
`create_user` privilege

**Input Schema:**

```json
{
  "username": "string (required)",
  "email": "string (required)",
  "password": "string (required)",
  "is_superuser": "boolean (optional, default: false)"
}
```

**Example Request:**

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tools/call",
  "params": {
    "name": "create_user",
    "arguments": {
      "username": "bob",
      "email": "bob@example.com",
      "password": "securepass456",
      "is_superuser": false
    }
  }
}
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "user_id": 6,
    "username": "bob",
    "email": "bob@example.com",
    "is_superuser": false,
    "created_at": "2025-01-15T10:30:00Z"
  }
}
```

**Errors:**

- "permission denied: insufficient privileges" - User lacks required privileges
- "username already exists" - Username is already taken
- "email already exists" - Email is already registered

---

### update_user

Update an existing user account.

**Authorization:** Requires superuser privileges or group membership granting
`update_user` privilege

**Input Schema:**

```json
{
  "user_id": "integer (required)",
  "email": "string (optional)",
  "password": "string (optional)",
  "is_superuser": "boolean (optional)"
}
```

**Example Request:**

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": {
    "name": "update_user",
    "arguments": {
      "user_id": 6,
      "email": "bob.smith@example.com",
      "is_superuser": false
    }
  }
}
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "success": true,
    "user_id": 6,
    "message": "User updated successfully"
  }
}
```

**Errors:**

- "permission denied: insufficient privileges" - User lacks required privileges
- "user not found" - User ID does not exist

---

### delete_user

Delete a user account.

**Authorization:** Requires superuser privileges or group membership granting
`delete_user` privilege

**Input Schema:**

```json
{
  "user_id": "integer (required)"
}
```

**Example Request:**

```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "method": "tools/call",
  "params": {
    "name": "delete_user",
    "arguments": {
      "user_id": 6
    }
  }
}
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "result": {
    "success": true,
    "message": "User deleted successfully"
  }
}
```

**Errors:**

- "permission denied: insufficient privileges" - User lacks required privileges
- "user not found" - User ID does not exist

---

## Service Token Management

### create_service_token

Create a new service token.

**Authorization:** Requires superuser privileges or group membership granting
`create_service_token` privilege

**Input Schema:**

```json
{
  "token_name": "string (required)",
  "is_superuser": "boolean (optional, default: false)"
}
```

**Example Request:**

```json
{
  "jsonrpc": "2.0",
  "id": 5,
  "method": "tools/call",
  "params": {
    "name": "create_service_token",
    "arguments": {
      "token_name": "ci-pipeline-token",
      "is_superuser": false
    }
  }
}
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 5,
  "result": {
    "token_id": 10,
    "token_name": "ci-pipeline-token",
    "token_value": "svc_abc123def456...",
    "is_superuser": false,
    "created_at": "2025-01-15T10:30:00Z"
  }
}
```

**Note:** The `token_value` is only returned on creation. Store it securely -
it cannot be retrieved later.

**Errors:**

- "permission denied: insufficient privileges" - User lacks required privileges
- "token name already exists" - Token name must be unique

---

### update_service_token

Update an existing service token.

**Authorization:** Requires superuser privileges or group membership granting
`update_service_token` privilege

**Input Schema:**

```json
{
  "token_id": "integer (required)",
  "token_name": "string (optional)",
  "is_superuser": "boolean (optional)"
}
```

**Example Request:**

```json
{
  "jsonrpc": "2.0",
  "id": 6,
  "method": "tools/call",
  "params": {
    "name": "update_service_token",
    "arguments": {
      "token_id": 10,
      "token_name": "ci-deployment-token"
    }
  }
}
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 6,
  "result": {
    "success": true,
    "token_id": 10,
    "message": "Service token updated successfully"
  }
}
```

**Errors:**

- "permission denied: insufficient privileges" - User lacks required privileges
- "token not found" - Token ID does not exist

---

### delete_service_token

Delete a service token.

**Authorization:** Requires superuser privileges or group membership granting
`delete_service_token` privilege

**Input Schema:**

```json
{
  "token_id": "integer (required)"
}
```

**Example Request:**

```json
{
  "jsonrpc": "2.0",
  "id": 7,
  "method": "tools/call",
  "params": {
    "name": "delete_service_token",
    "arguments": {
      "token_id": 10
    }
  }
}
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 7,
  "result": {
    "success": true,
    "message": "Service token deleted successfully"
  }
}
```

**Errors:**

- "permission denied: insufficient privileges" - User lacks required privileges
- "token not found" - Token ID does not exist

---

## User Token Management

### create_user_token

Create a new user token (personal access token).

**Authorization:** Requires superuser privileges or group membership granting
`create_user_token` privilege

**Input Schema:**

```json
{
  "user_id": "integer (required)",
  "token_name": "string (required)"
}
```

**Example Request:**

```json
{
  "jsonrpc": "2.0",
  "id": 8,
  "method": "tools/call",
  "params": {
    "name": "create_user_token",
    "arguments": {
      "user_id": 5,
      "token_name": "alice-dev-token"
    }
  }
}
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 8,
  "result": {
    "token_id": 15,
    "user_id": 5,
    "token_name": "alice-dev-token",
    "token_value": "usr_xyz789abc123...",
    "created_at": "2025-01-15T10:30:00Z"
  }
}
```

**Note:** The `token_value` is only returned on creation. Store it securely -
it cannot be retrieved later.

**Errors:**

- "permission denied: insufficient privileges" - User lacks required privileges
- "user not found" - User ID does not exist

---

### list_user_tokens

**This operation has been converted to an MCP Resource.**

Use the resource URI `ai-workbench://users/{username}/tokens` to list user
tokens. See [MCP Resources documentation](server/mcp-resources.md) for details.

---

### delete_user_token

Delete a user token.

**Authorization:** Requires superuser privileges or group membership granting
`delete_user_token` privilege

**Input Schema:**

```json
{
  "token_id": "integer (required)"
}
```

**Example Request:**

```json
{
  "jsonrpc": "2.0",
  "id": 10,
  "method": "tools/call",
  "params": {
    "name": "delete_user_token",
    "arguments": {
      "token_id": 15
    }
  }
}
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 10,
  "result": {
    "success": true,
    "message": "User token deleted successfully"
  }
}
```

**Errors:**

- "permission denied: insufficient privileges" - User lacks required privileges
- "token not found" - Token ID does not exist

---

## Group Management

### create_user_group

Create a new user group.

**Authorization:** Requires superuser privileges or group membership granting
`create_user_group` privilege

**Input Schema:**

```json
{
  "name": "string (required)",
  "description": "string (optional)"
}
```

**Example Request:**

```json
{
  "jsonrpc": "2.0",
  "id": 11,
  "method": "tools/call",
  "params": {
    "name": "create_user_group",
    "arguments": {
      "name": "developers",
      "description": "Development team members"
    }
  }
}
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 11,
  "result": {
    "group_id": 1,
    "name": "developers",
    "description": "Development team members",
    "created_at": "2025-01-15T10:30:00Z"
  }
}
```

**Errors:**

- "permission denied: insufficient privileges" - User lacks required privileges
- "group name already exists" - Group name must be unique

---

### update_user_group

Update an existing user group.

**Authorization:** Requires superuser privileges or group membership granting
`update_user_group` privilege

**Input Schema:**

```json
{
  "group_id": "integer (required)",
  "name": "string (optional)",
  "description": "string (optional)"
}
```

**Example Request:**

```json
{
  "jsonrpc": "2.0",
  "id": 12,
  "method": "tools/call",
  "params": {
    "name": "update_user_group",
    "arguments": {
      "group_id": 1,
      "name": "engineering",
      "description": "Engineering department"
    }
  }
}
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 12,
  "result": {
    "success": true,
    "group_id": 1,
    "message": "User group updated successfully"
  }
}
```

**Errors:**

- "permission denied: insufficient privileges" - User lacks required privileges
- "group not found" - Group ID does not exist

---

### delete_user_group

Delete a user group (CASCADE deletes all memberships and privileges).

**Authorization:** Requires superuser privileges or group membership granting
`delete_user_group` privilege

**Input Schema:**

```json
{
  "group_id": "integer (required)"
}
```

**Example Request:**

```json
{
  "jsonrpc": "2.0",
  "id": 13,
  "method": "tools/call",
  "params": {
    "name": "delete_user_group",
    "arguments": {
      "group_id": 1
    }
  }
}
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 13,
  "result": {
    "success": true,
    "message": "User group deleted successfully"
  }
}
```

**Note:** This will CASCADE delete all group memberships and privileges
associated with this group.

**Errors:**

- "permission denied: insufficient privileges" - User lacks required privileges
- "group not found" - Group ID does not exist

---

### list_user_groups

**This operation has been converted to an MCP Resource.**

Use the resource URI `ai-workbench://groups` to list user groups. See
[MCP Resources documentation](server/mcp-resources.md) for details.

---

### add_group_member

Add a user or group as a member of a group.

**Authorization:** Requires superuser privileges or group membership granting
`add_group_member` privilege

**Input Schema:**

```json
{
  "parent_group_id": "integer (required)",
  "member_user_id": "integer (optional, mutually exclusive with member_group_id)",
  "member_group_id": "integer (optional, mutually exclusive with member_user_id)"
}
```

**Note:** Exactly one of `member_user_id` or `member_group_id` must be
provided.

**Example Request (adding a user):**

```json
{
  "jsonrpc": "2.0",
  "id": 15,
  "method": "tools/call",
  "params": {
    "name": "add_group_member",
    "arguments": {
      "parent_group_id": 1,
      "member_user_id": 5
    }
  }
}
```

**Example Request (adding a group):**

```json
{
  "jsonrpc": "2.0",
  "id": 16,
  "method": "tools/call",
  "params": {
    "name": "add_group_member",
    "arguments": {
      "parent_group_id": 1,
      "member_group_id": 2
    }
  }
}
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 15,
  "result": {
    "success": true,
    "message": "Member added to group successfully"
  }
}
```

**Errors:**

- "permission denied: insufficient privileges" - User lacks required privileges
- "would create circular reference" - Adding this group would create a cycle
- "member already in group" - Membership already exists
- "must specify exactly one of member_user_id or member_group_id" - Invalid
  parameters

---

### remove_group_member

Remove a user or group from a group.

**Authorization:** Requires superuser privileges or group membership granting
`remove_group_member` privilege

**Input Schema:**

```json
{
  "parent_group_id": "integer (required)",
  "member_user_id": "integer (optional, mutually exclusive with member_group_id)",
  "member_group_id": "integer (optional, mutually exclusive with member_user_id)"
}
```

**Note:** Exactly one of `member_user_id` or `member_group_id` must be
provided.

**Example Request:**

```json
{
  "jsonrpc": "2.0",
  "id": 17,
  "method": "tools/call",
  "params": {
    "name": "remove_group_member",
    "arguments": {
      "parent_group_id": 1,
      "member_user_id": 5
    }
  }
}
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 17,
  "result": {
    "success": true,
    "message": "Member removed from group successfully"
  }
}
```

**Errors:**

- "permission denied: insufficient privileges" - User lacks required privileges
- "membership not found" - Member is not in the group
- "must specify exactly one of member_user_id or member_group_id" - Invalid
  parameters

---

### list_group_members

**This operation has been converted to an MCP Resource.**

Use the resource URI `ai-workbench://groups/{groupId}/members` to list group
members. See [MCP Resources documentation](server/mcp-resources.md) for details.

---

### list_user_group_memberships

**This operation has been converted to an MCP Resource.**

Use the resource URI `ai-workbench://users/{username}/groups` to list user
group memberships. See [MCP Resources documentation](server/mcp-resources.md)
for details.

---

## Connection Privilege Management

### grant_connection_privilege

Grant a group access to a database connection.

**Authorization:** Requires superuser privileges or group membership granting
`grant_connection_privilege` privilege

**Input Schema:**

```json
{
  "group_id": "integer (required)",
  "connection_id": "integer (required)",
  "access_level": "string (required, one of: 'read', 'read_write')"
}
```

**Example Request:**

```json
{
  "jsonrpc": "2.0",
  "id": 20,
  "method": "tools/call",
  "params": {
    "name": "grant_connection_privilege",
    "arguments": {
      "group_id": 1,
      "connection_id": 5,
      "access_level": "read_write"
    }
  }
}
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 20,
  "result": {
    "success": true,
    "group_id": 1,
    "connection_id": 5,
    "access_level": "read_write",
    "message": "Connection privilege granted successfully"
  }
}
```

**Note:** This operation uses UPSERT behavior. If the privilege already exists,
the access level will be updated.

**Errors:**

- "permission denied: insufficient privileges" - User lacks required privileges
- "invalid access_level" - Must be 'read' or 'read_write'
- "group not found" - Group ID does not exist
- "connection not found" - Connection ID does not exist

---

### revoke_connection_privilege

Revoke a group's access to a database connection.

**Authorization:** Requires superuser privileges or group membership granting
`revoke_connection_privilege` privilege

**Input Schema:**

```json
{
  "group_id": "integer (required)",
  "connection_id": "integer (required)"
}
```

**Example Request:**

```json
{
  "jsonrpc": "2.0",
  "id": 21,
  "method": "tools/call",
  "params": {
    "name": "revoke_connection_privilege",
    "arguments": {
      "group_id": 1,
      "connection_id": 5
    }
  }
}
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 21,
  "result": {
    "success": true,
    "message": "Connection privilege revoked successfully"
  }
}
```

**Errors:**

- "permission denied: insufficient privileges" - User lacks required privileges
- "privilege not found" - No privilege exists for this group/connection pair

---

### list_connection_privileges

**This operation has been converted to an MCP Resource.**

Use the resource URI `ai-workbench://connections/{connectionId}/privileges` to
list connection privileges. See [MCP Resources documentation](server/mcp-resources.md)
for details.

---

## MCP Privilege Management

### list_mcp_privilege_identifiers

**This operation has been converted to an MCP Resource.**

Use the resource URI `ai-workbench://mcp-privileges` to list MCP privilege
identifiers. See [MCP Resources documentation](server/mcp-resources.md) for details.

---

### grant_mcp_privilege

Grant a group access to a specific MCP item (tool, resource, or prompt).

**Authorization:** Requires superuser privileges or group membership granting
`grant_mcp_privilege` privilege

**Input Schema:**

```json
{
  "group_id": "integer (required)",
  "privilege_identifier": "string (required)"
}
```

**Example Request:**

```json
{
  "jsonrpc": "2.0",
  "id": 24,
  "method": "tools/call",
  "params": {
    "name": "grant_mcp_privilege",
    "arguments": {
      "group_id": 1,
      "privilege_identifier": "create_user"
    }
  }
}
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 24,
  "result": {
    "success": true,
    "group_id": 1,
    "privilege_identifier": "create_user",
    "message": "MCP privilege granted successfully"
  }
}
```

**Errors:**

- "permission denied: insufficient privileges" - User lacks required privileges
- "privilege identifier not found" - Invalid identifier (not registered)
- "privilege already granted" - Group already has this privilege
- "group not found" - Group ID does not exist

---

### revoke_mcp_privilege

Revoke a group's access to an MCP item.

**Authorization:** Requires superuser privileges or group membership granting
`revoke_mcp_privilege` privilege

**Input Schema:**

```json
{
  "group_id": "integer (required)",
  "privilege_identifier": "string (required)"
}
```

**Example Request:**

```json
{
  "jsonrpc": "2.0",
  "id": 25,
  "method": "tools/call",
  "params": {
    "name": "revoke_mcp_privilege",
    "arguments": {
      "group_id": 1,
      "privilege_identifier": "create_user"
    }
  }
}
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 25,
  "result": {
    "success": true,
    "message": "MCP privilege revoked successfully"
  }
}
```

**Errors:**

- "permission denied: insufficient privileges" - User lacks required privileges
- "privilege not found" - Group does not have this privilege

---

### list_group_mcp_privileges

**This operation has been converted to an MCP Resource.**

Use the resource URI `ai-workbench://groups/{groupId}/mcp-privileges` to list
group MCP privileges. See [MCP Resources documentation](server/mcp-resources.md)
for details.

---

## Token Scope Management

### set_token_connection_scope

Limit a token to specific database connections.

**Authorization:** Requires superuser privileges or group membership granting
`set_token_connection_scope` privilege

**Input Schema:**

```json
{
  "token_id": "integer (required)",
  "token_type": "string (required, one of: 'user', 'service')",
  "connection_ids": "array of integers (required)"
}
```

**Example Request:**

```json
{
  "jsonrpc": "2.0",
  "id": 27,
  "method": "tools/call",
  "params": {
    "name": "set_token_connection_scope",
    "arguments": {
      "token_id": 15,
      "token_type": "user",
      "connection_ids": [1, 3, 5]
    }
  }
}
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 27,
  "result": {
    "success": true,
    "token_id": 15,
    "token_type": "user",
    "connection_count": 3,
    "message": "Token connection scope set successfully"
  }
}
```

**Note:** This operation replaces any existing connection scope. Pass an empty
array to remove all connection restrictions.

**Errors:**

- "permission denied: insufficient privileges" - User lacks required privileges
- "invalid token_type" - Must be 'user' or 'service'
- "token not found" - Token ID does not exist

---

### set_token_mcp_scope

Limit a token to specific MCP items (tools, resources, prompts).

**Authorization:** Requires superuser privileges or group membership granting
`set_token_mcp_scope` privilege

**Input Schema:**

```json
{
  "token_id": "integer (required)",
  "token_type": "string (required, one of: 'user', 'service')",
  "privilege_identifiers": "array of strings (required)"
}
```

**Example Request:**

```json
{
  "jsonrpc": "2.0",
  "id": 28,
  "method": "tools/call",
  "params": {
    "name": "set_token_mcp_scope",
    "arguments": {
      "token_id": 15,
      "token_type": "user",
      "privilege_identifiers": ["create_user", "list_user_groups"]
    }
  }
}
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 28,
  "result": {
    "success": true,
    "token_id": 15,
    "token_type": "user",
    "privilege_count": 2,
    "message": "Token MCP scope set successfully"
  }
}
```

**Note:** This operation replaces any existing MCP scope. Pass an empty array
to remove all MCP restrictions.

**Errors:**

- "permission denied: insufficient privileges" - User lacks required privileges
- "invalid token_type" - Must be 'user' or 'service'
- "token not found" - Token ID does not exist
- "privilege identifier not found" - Invalid identifier in array

---

### get_token_scope

Retrieve the current scope restrictions for a token.

**Authorization:** Requires superuser privileges or group membership granting
`get_token_scope` privilege

**Input Schema:**

```json
{
  "token_id": "integer (required)",
  "token_type": "string (required, one of: 'user', 'service')"
}
```

**Example Request:**

```json
{
  "jsonrpc": "2.0",
  "id": 29,
  "method": "tools/call",
  "params": {
    "name": "get_token_scope",
    "arguments": {
      "token_id": 15,
      "token_type": "user"
    }
  }
}
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 29,
  "result": {
    "token_id": 15,
    "token_type": "user",
    "connections": [
      {
        "id": 1,
        "connection_id": 1,
        "connection_name": "dev-db",
        "created_at": "2025-01-15T10:30:00Z"
      },
      {
        "id": 2,
        "connection_id": 3,
        "connection_name": "staging-db",
        "created_at": "2025-01-15T10:30:00Z"
      }
    ],
    "mcp_items": [
      {
        "id": 1,
        "privilege_id": 1,
        "identifier": "create_user",
        "item_type": "tool",
        "description": "Create a new user account",
        "created_at": "2025-01-15T10:30:00Z"
      },
      {
        "id": 2,
        "privilege_id": 5,
        "identifier": "list_user_groups",
        "item_type": "tool",
        "description": "List all user groups",
        "created_at": "2025-01-15T10:30:00Z"
      }
    ]
  }
}
```

**Errors:**

- "permission denied: insufficient privileges" - User lacks required privileges
- "invalid token_type" - Must be 'user' or 'service'
- "token not found" - Token ID does not exist

---

### clear_token_scope

Remove all scope restrictions from a token.

**Authorization:** Requires superuser privileges or group membership granting
`clear_token_scope` privilege

**Input Schema:**

```json
{
  "token_id": "integer (required)",
  "token_type": "string (required, one of: 'user', 'service')"
}
```

**Example Request:**

```json
{
  "jsonrpc": "2.0",
  "id": 30,
  "method": "tools/call",
  "params": {
    "name": "clear_token_scope",
    "arguments": {
      "token_id": 15,
      "token_type": "user"
    }
  }
}
```

**Example Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 30,
  "result": {
    "success": true,
    "message": "Token scope cleared successfully"
  }
}
```

**Note:** Clearing scope doesn't grant additional privileges - the token still
respects its owner's group-based privileges. It only removes the additional
restrictions imposed by scoping.

**Errors:**

- "permission denied: insufficient privileges" - User lacks required privileges
- "invalid token_type" - Must be 'user' or 'service'
- "token not found" - Token ID does not exist
