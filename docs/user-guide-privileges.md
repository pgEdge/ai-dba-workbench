# User Guide: Groups and Privilege Management

This guide explains how to use the pgEdge AI Workbench privilege management
system to control access to database connections and MCP tools.

## Table of Contents

- [Overview](#overview)
- [Core Concepts](#core-concepts)
- [Getting Started](#getting-started)
- [Managing User Groups](#managing-user-groups)
- [Managing Privileges](#managing-privileges)
- [Token Scoping](#token-scoping)
- [Security Model](#security-model)
- [Best Practices](#best-practices)

## Overview

The pgEdge AI Workbench provides a comprehensive privilege management system
that allows you to:

- Organize users into hierarchical groups
- Control access to database connections with read/read_write levels
- Control access to MCP tools, resources, and prompts
- Restrict API tokens to subsets of owner's privileges
- Inherit privileges through nested group membership

This system enables you to implement least-privilege access control, where
users and tokens only have access to the resources they need.

## Core Concepts

### User Groups

**User groups** are containers that organize users and other groups into a
hierarchy. Groups can contain:

- Individual users
- Other groups (nested membership)

When a user belongs to a group, they inherit all privileges assigned to that
group. If a group is a member of another group, users in the child group
inherit privileges from both groups.

### Privileges

**Privileges** grant access to specific resources:

- **Connection privileges** - Grant access to database connections with either
  `read` or `read_write` permission levels
- **MCP privileges** - Grant access to specific MCP tools, resources, or
  prompts

Privileges are always assigned to groups, never directly to users. Users gain
privileges through group membership.

### Tokens

**Tokens** are API credentials that allow programmatic access to the MCP
server:

- **User tokens** - Belong to a specific user account
- **Service tokens** - Standalone tokens not tied to a user

Tokens inherit privileges from their owner (for user tokens) or can be assigned
group memberships (for service tokens).

### Token Scoping

**Token scoping** further restricts a token's access to a subset of what its
owner has access to:

- **Connection scope** - Limit token to specific database connections
- **MCP scope** - Limit token to specific MCP tools/resources/prompts

Scoping enables you to create tokens with minimal privileges for specific tasks.

### Superusers

**Superusers** bypass all privilege checks and have unrestricted access to all
resources. The `is_superuser` flag is set on user accounts and service tokens.

## Getting Started

### Prerequisites

All privilege management operations require superuser privileges. You must
authenticate as a superuser to create groups, grant privileges, or manage
token scopes.

### Check Current Privileges

To see what groups exist in your system:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "list_user_groups"
  }
}
```

To see what groups a specific user belongs to:

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tools/call",
  "params": {
    "name": "list_user_group_memberships",
    "arguments": {
      "user_id": 123
    }
  }
}
```

## Managing User Groups

### Creating Groups

Create a new user group:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
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

Response:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "group_id": 1,
    "name": "developers",
    "description": "Development team members"
  }
}
```

### Adding Members to Groups

Add a user to a group:

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tools/call",
  "params": {
    "name": "add_group_member",
    "arguments": {
      "parent_group_id": 1,
      "member_user_id": 123
    }
  }
}
```

Add a group as a member of another group (nested membership):

```json
{
  "jsonrpc": "2.0",
  "id": 3,
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

**Note:** The system prevents circular references. You cannot create a cycle
like A → B → C → A or self-references like A → A.

### Viewing Group Members

List all direct members of a group:

```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "method": "tools/call",
  "params": {
    "name": "list_group_members",
    "arguments": {
      "group_id": 1
    }
  }
}
```

Response:

```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "result": {
    "group_id": 1,
    "group_name": "developers",
    "members": [
      {
        "member_type": "user",
        "user_id": 123,
        "username": "alice",
        "added_at": "2025-01-15T10:30:00Z"
      },
      {
        "member_type": "group",
        "group_id": 2,
        "group_name": "frontend-devs",
        "added_at": "2025-01-15T11:00:00Z"
      }
    ]
  }
}
```

### Removing Members

Remove a user from a group:

```json
{
  "jsonrpc": "2.0",
  "id": 5,
  "method": "tools/call",
  "params": {
    "name": "remove_group_member",
    "arguments": {
      "parent_group_id": 1,
      "member_user_id": 123
    }
  }
}
```

### Updating Groups

Update a group's name or description:

```json
{
  "jsonrpc": "2.0",
  "id": 6,
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

### Deleting Groups

Delete a group (this will CASCADE delete all memberships and privileges):

```json
{
  "jsonrpc": "2.0",
  "id": 7,
  "method": "tools/call",
  "params": {
    "name": "delete_user_group",
    "arguments": {
      "group_id": 1
    }
  }
}
```

**Warning:** Deleting a group removes all associated memberships and
privileges. Users will immediately lose any access they had through this group.

## Managing Privileges

### Connection Privileges

Connection privileges grant access to database connections with either `read`
or `read_write` permission levels.

#### Granting Connection Access

Grant a group read access to a connection:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "grant_connection_privilege",
    "arguments": {
      "group_id": 1,
      "connection_id": 5,
      "access_level": "read"
    }
  }
}
```

Grant read_write access:

```json
{
  "jsonrpc": "2.0",
  "id": 2,
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

**Access Levels:**

- `read` - Can query data but not modify it
- `read_write` - Can both query and modify data

**Note:** Calling `grant_connection_privilege` multiple times for the same
group and connection will update the access level (upsert behavior).

#### Listing Connection Privileges

List all groups that have access to a connection:

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": {
    "name": "list_connection_privileges",
    "arguments": {
      "connection_id": 5
    }
  }
}
```

Response:

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "connection_id": 5,
    "connection_name": "production-db",
    "privileges": [
      {
        "group_id": 1,
        "group_name": "developers",
        "access_level": "read",
        "granted_at": "2025-01-15T10:30:00Z"
      },
      {
        "group_id": 2,
        "group_name": "dba-team",
        "access_level": "read_write",
        "granted_at": "2025-01-15T11:00:00Z"
      }
    ]
  }
}
```

#### Revoking Connection Access

Remove a group's access to a connection:

```json
{
  "jsonrpc": "2.0",
  "id": 4,
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

### MCP Privileges

MCP privileges grant access to specific MCP tools, resources, and prompts.

#### Listing Available Privilege Identifiers

See all available MCP items that can be assigned privileges:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "list_mcp_privilege_identifiers"
  }
}
```

Response:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "total_count": 29,
    "identifiers": [
      {
        "id": 1,
        "identifier": "create_user",
        "item_type": "tool",
        "description": "Create a new user account"
      },
      {
        "id": 2,
        "identifier": "delete_user",
        "item_type": "tool",
        "description": "Delete a user account"
      }
      // ... more items
    ]
  }
}
```

#### Granting MCP Access

Grant a group access to a specific MCP item:

```json
{
  "jsonrpc": "2.0",
  "id": 2,
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

#### Listing Group MCP Privileges

List all MCP privileges assigned to a group:

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": {
    "name": "list_group_mcp_privileges",
    "arguments": {
      "group_id": 1
    }
  }
}
```

Response:

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "group_id": 1,
    "group_name": "developers",
    "privileges": [
      {
        "privilege_id": 1,
        "identifier": "create_user",
        "item_type": "tool",
        "description": "Create a new user account",
        "granted_at": "2025-01-15T10:30:00Z"
      },
      {
        "privilege_id": 5,
        "identifier": "list_user_groups",
        "item_type": "tool",
        "description": "List all user groups",
        "granted_at": "2025-01-15T11:00:00Z"
      }
    ]
  }
}
```

#### Revoking MCP Access

Remove a group's access to an MCP item:

```json
{
  "jsonrpc": "2.0",
  "id": 4,
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

## Token Scoping

Token scoping allows you to restrict a token's access to a subset of what its
owner can access. This implements the principle of least privilege for API
tokens.

### Connection Scoping

Limit a token to specific database connections:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "set_token_connection_scope",
    "arguments": {
      "token_id": 10,
      "token_type": "user",
      "connection_ids": [1, 3, 5]
    }
  }
}
```

**Token Types:**

- `user` - For user tokens created via `create_user_token`
- `service` - For service tokens created via `create_service_token`

### MCP Scoping

Limit a token to specific MCP tools/resources/prompts:

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tools/call",
  "params": {
    "name": "set_token_mcp_scope",
    "arguments": {
      "token_id": 10,
      "token_type": "user",
      "privilege_identifiers": ["create_user", "list_user_groups"]
    }
  }
}
```

### Viewing Token Scope

Get the current scope restrictions for a token:

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": {
    "name": "get_token_scope",
    "arguments": {
      "token_id": 10,
      "token_type": "user"
    }
  }
}
```

Response:

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "token_id": 10,
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
      }
    ]
  }
}
```

### Clearing Token Scope

Remove all scope restrictions from a token:

```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "method": "tools/call",
  "params": {
    "name": "clear_token_scope",
    "arguments": {
      "token_id": 10,
      "token_type": "user"
    }
  }
}
```

**Note:** Clearing scope doesn't grant additional privileges - the token still
respects its owner's group-based privileges. It only removes the additional
restrictions imposed by scoping.

## Security Model

### Access Control Flow

When a user or token attempts to access a resource, the system checks:

1. **Superuser Bypass** - If the user/token is a superuser, grant access
   immediately
2. **Token Scope** - If using a token with scope restrictions, verify the
   resource is in scope
3. **Group Privileges** - Check if the user belongs to any group that has been
   granted the required privilege
4. **Default Behavior** - If no groups have been assigned the privilege,
   access is PUBLIC (backwards compatible)

### Privilege Inheritance

Users inherit privileges through group membership:

```
All Staff Group (has privilege A)
    ↓
Engineering Group (has privilege B)
    ↓
Frontend Team (has privilege C)
    ↓
Alice (user)
```

In this hierarchy, Alice inherits privileges A, B, and C through her membership
in the Frontend Team group.

### Deny by Default

Once ANY group is assigned a privilege, that resource becomes restricted and
requires explicit group membership:

**Example:**

- Initially, `create_user` tool is available to everyone (no groups assigned)
- Grant `create_user` privilege to "admins" group
- Now ONLY members of "admins" group can use `create_user`
- All other users are denied access

### Connection Access Levels

Connection privileges have two levels:

- `read` - Sufficient for read-only operations (SELECT queries)
- `read_write` - Required for write operations (INSERT, UPDATE, DELETE, DDL)

A user requesting `read_write` access must have a privilege granting
`read_write` level. A user with `read` privilege will be denied `read_write`
access.

### Token Scoping Rules

Token scoping further restricts access:

- Token owner has access to connections [1, 2, 3, 4, 5]
- Token is scoped to connections [2, 4]
- Token can ONLY access connections [2, 4]

If token is scoped to resources the owner doesn't have access to, those
resources remain inaccessible.

## Best Practices

### Group Organization

**Hierarchical Structure:**

Create groups that mirror your organizational structure:

```
all-staff
├── engineering
│   ├── backend-team
│   ├── frontend-team
│   └── data-team
└── operations
    ├── dba-team
    └── sre-team
```

**Benefit:** Users automatically inherit privileges from parent groups.

### Privilege Assignment

**Grant to Groups, Not Users:**

Always assign privileges to groups rather than individual users. This makes
privilege management more maintainable.

**Use Descriptive Names:**

- Good: "dba-team", "readonly-analysts", "production-access"
- Bad: "group1", "team-a", "misc"

### Token Management

**Scope All Application Tokens:**

Service tokens used by applications should always be scoped to only the
resources they need:

```json
{
  "name": "set_token_connection_scope",
  "arguments": {
    "token_id": 15,
    "token_type": "service",
    "connection_ids": [3]
  }
}
```

**Regular Audits:**

Periodically review:

- Which groups exist and who's in them
- What privileges each group has
- What tokens exist and their scope

### Security

**Minimize Superusers:**

Only grant superuser status to administrators who truly need unrestricted
access.

**Principle of Least Privilege:**

Grant the minimum access required:

- Use `read` instead of `read_write` when possible
- Scope tokens narrowly
- Remove unused privileges promptly

**Review Group Hierarchies:**

Ensure nested group membership makes sense - users inherit privileges from all
parent groups in the hierarchy.

### Connection Privileges

**Separate Environments:**

Create different groups for each environment:

- `dev-access` group → development databases
- `staging-access` group → staging databases
- `prod-readonly` group → production databases (read only)
- `prod-dba` group → production databases (read_write)

### MCP Privileges

**Administrative Tools:**

Restrict access to administrative MCP tools to appropriate groups:

```
admins group:
  - create_user
  - delete_user
  - create_user_group
  - grant_connection_privilege
  - grant_mcp_privilege

users group:
  - list_user_groups
  - list_user_group_memberships
```

This ensures regular users cannot perform administrative actions.
