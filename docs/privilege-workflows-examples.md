# Common Workflows and Examples

This document provides step-by-step examples for common privilege management
scenarios in the pgEdge AI Workbench.

## Table of Contents

- [Initial Setup](#initial-setup)
- [Setting Up Team Structure](#setting-up-team-structure)
- [Managing Database Access](#managing-database-access)
- [Managing Tool Access](#managing-tool-access)
- [Creating Limited-Privilege Tokens](#creating-limited-privilege-tokens)
- [Onboarding New Users](#onboarding-new-users)
- [Offboarding Users](#offboarding-users)
- [Troubleshooting Access Issues](#troubleshooting-access-issues)

## Initial Setup

### Scenario: First-Time Configuration

You're setting up the privilege system for the first time. You need to create
your organizational structure and assign initial privileges.

**Step 1: Create top-level groups**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "create_user_group",
    "arguments": {
      "name": "all-staff",
      "description": "All company employees"
    }
  }
}
```

Response: `{"group_id": 1}`

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tools/call",
  "params": {
    "name": "create_user_group",
    "arguments": {
      "name": "engineering",
      "description": "Engineering department"
    }
  }
}
```

Response: `{"group_id": 2}`

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": {
    "name": "create_user_group",
    "arguments": {
      "name": "operations",
      "description": "Operations department"
    }
  }
}
```

Response: `{"group_id": 3}`

**Step 2: Create nested groups within engineering**

```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "method": "tools/call",
  "params": {
    "name": "create_user_group",
    "arguments": {
      "name": "backend-team",
      "description": "Backend developers"
    }
  }
}
```

Response: `{"group_id": 4}`

```json
{
  "jsonrpc": "2.0",
  "id": 5,
  "method": "tools/call",
  "params": {
    "name": "create_user_group",
    "arguments": {
      "name": "frontend-team",
      "description": "Frontend developers"
    }
  }
}
```

Response: `{"group_id": 5}`

**Step 3: Build the hierarchy**

```json
{
  "jsonrpc": "2.0",
  "id": 6,
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

```json
{
  "jsonrpc": "2.0",
  "id": 7,
  "method": "tools/call",
  "params": {
    "name": "add_group_member",
    "arguments": {
      "parent_group_id": 1,
      "member_group_id": 3
    }
  }
}
```

```json
{
  "jsonrpc": "2.0",
  "id": 8,
  "method": "tools/call",
  "params": {
    "name": "add_group_member",
    "arguments": {
      "parent_group_id": 2,
      "member_group_id": 4
    }
  }
}
```

```json
{
  "jsonrpc": "2.0",
  "id": 9,
  "method": "tools/call",
  "params": {
    "name": "add_group_member",
    "arguments": {
      "parent_group_id": 2,
      "member_group_id": 5
    }
  }
}
```

**Result:** You now have this hierarchy:

```
all-staff (1)
├── engineering (2)
│   ├── backend-team (4)
│   └── frontend-team (5)
└── operations (3)
```

## Setting Up Team Structure

### Scenario: Engineering Team with Database Access

You want to give your engineering team appropriate access to development and
staging databases.

**Context:**

- Development database (connection_id: 10)
- Staging database (connection_id: 11)
- Production database (connection_id: 12)
- Backend team should have read_write to dev and staging
- Frontend team should have read to dev and staging
- No one should have direct prod access

**Step 1: Grant backend team privileges**

```json
{
  "jsonrpc": "2.0",
  "id": 10,
  "method": "tools/call",
  "params": {
    "name": "grant_connection_privilege",
    "arguments": {
      "group_id": 4,
      "connection_id": 10,
      "access_level": "read_write"
    }
  }
}
```

```json
{
  "jsonrpc": "2.0",
  "id": 11,
  "method": "tools/call",
  "params": {
    "name": "grant_connection_privilege",
    "arguments": {
      "group_id": 4,
      "connection_id": 11,
      "access_level": "read_write"
    }
  }
}
```

**Step 2: Grant frontend team privileges**

```json
{
  "jsonrpc": "2.0",
  "id": 12,
  "method": "tools/call",
  "params": {
    "name": "grant_connection_privilege",
    "arguments": {
      "group_id": 5,
      "connection_id": 10,
      "access_level": "read"
    }
  }
}
```

```json
{
  "jsonrpc": "2.0",
  "id": 13,
  "method": "tools/call",
  "params": {
    "name": "grant_connection_privilege",
    "arguments": {
      "group_id": 5,
      "connection_id": 11,
      "access_level": "read"
    }
  }
}
```

**Step 3: Create DBA group with production access**

```json
{
  "jsonrpc": "2.0",
  "id": 14,
  "method": "tools/call",
  "params": {
    "name": "create_user_group",
    "arguments": {
      "name": "dba-team",
      "description": "Database administrators"
    }
  }
}
```

Response: `{"group_id": 6}`

```json
{
  "jsonrpc": "2.0",
  "id": 15,
  "method": "tools/call",
  "params": {
    "name": "grant_connection_privilege",
    "arguments": {
      "group_id": 6,
      "connection_id": 12,
      "access_level": "read_write"
    }
  }
}
```

**Result:**

- Backend developers: read_write to dev and staging
- Frontend developers: read to dev and staging
- DBAs: read_write to production

## Managing Database Access

### Scenario: Granting Temporary Production Access

A backend developer needs temporary read access to production to debug an issue.

**Initial state:**

- User Alice (user_id: 20) is in backend-team (group_id: 4)
- backend-team has no production access
- Production DB (connection_id: 12)

**Option 1: Create a temporary group**

```json
{
  "jsonrpc": "2.0",
  "id": 16,
  "method": "tools/call",
  "params": {
    "name": "create_user_group",
    "arguments": {
      "name": "prod-debug-temp",
      "description": "Temporary production debugging access"
    }
  }
}
```

Response: `{"group_id": 7}`

```json
{
  "jsonrpc": "2.0",
  "id": 17,
  "method": "tools/call",
  "params": {
    "name": "add_group_member",
    "arguments": {
      "parent_group_id": 7,
      "member_user_id": 20
    }
  }
}
```

```json
{
  "jsonrpc": "2.0",
  "id": 18,
  "method": "tools/call",
  "params": {
    "name": "grant_connection_privilege",
    "arguments": {
      "group_id": 7,
      "connection_id": 12,
      "access_level": "read"
    }
  }
}
```

**When done, revoke access:**

```json
{
  "jsonrpc": "2.0",
  "id": 19,
  "method": "tools/call",
  "params": {
    "name": "delete_user_group",
    "arguments": {
      "group_id": 7
    }
  }
}
```

**Option 2: Add user directly to DBA group temporarily**

```json
{
  "jsonrpc": "2.0",
  "id": 20,
  "method": "tools/call",
  "params": {
    "name": "add_group_member",
    "arguments": {
      "parent_group_id": 6,
      "member_user_id": 20
    }
  }
}
```

**When done, remove from DBA group:**

```json
{
  "jsonrpc": "2.0",
  "id": 21,
  "method": "tools/call",
  "params": {
    "name": "remove_group_member",
    "arguments": {
      "parent_group_id": 6,
      "member_user_id": 20
    }
  }
}
```

**Best Practice:** Option 1 is preferred because it creates an audit trail and
makes the temporary nature explicit.

## Managing Tool Access

### Scenario: Restricting Administrative Tools

You want to ensure only designated admins can create/delete users and manage
groups.

**Step 1: Create admin group**

```json
{
  "jsonrpc": "2.0",
  "id": 22,
  "method": "tools/call",
  "params": {
    "name": "create_user_group",
    "arguments": {
      "name": "admins",
      "description": "System administrators"
    }
  }
}
```

Response: `{"group_id": 8}`

**Step 2: Grant administrative tool privileges**

```json
{
  "jsonrpc": "2.0",
  "id": 23,
  "method": "tools/call",
  "params": {
    "name": "grant_mcp_privilege",
    "arguments": {
      "group_id": 8,
      "privilege_identifier": "create_user"
    }
  }
}
```

```json
{
  "jsonrpc": "2.0",
  "id": 24,
  "method": "tools/call",
  "params": {
    "name": "grant_mcp_privilege",
    "arguments": {
      "group_id": 8,
      "privilege_identifier": "update_user"
    }
  }
}
```

```json
{
  "jsonrpc": "2.0",
  "id": 25,
  "method": "tools/call",
  "params": {
    "name": "grant_mcp_privilege",
    "arguments": {
      "group_id": 8,
      "privilege_identifier": "delete_user"
    }
  }
}
```

```json
{
  "jsonrpc": "2.0",
  "id": 26,
  "method": "tools/call",
  "params": {
    "name": "grant_mcp_privilege",
    "arguments": {
      "group_id": 8,
      "privilege_identifier": "create_user_group"
    }
  }
}
```

```json
{
  "jsonrpc": "2.0",
  "id": 27,
  "method": "tools/call",
  "params": {
    "name": "grant_mcp_privilege",
    "arguments": {
      "group_id": 8,
      "privilege_identifier": "delete_user_group"
    }
  }
}
```

```json
{
  "jsonrpc": "2.0",
  "id": 28,
  "method": "tools/call",
  "params": {
    "name": "grant_mcp_privilege",
    "arguments": {
      "group_id": 8,
      "privilege_identifier": "grant_connection_privilege"
    }
  }
}
```

```json
{
  "jsonrpc": "2.0",
  "id": 29,
  "method": "tools/call",
  "params": {
    "name": "grant_mcp_privilege",
    "arguments": {
      "group_id": 8,
      "privilege_identifier": "grant_mcp_privilege"
    }
  }
}
```

**Step 3: Add admin users**

```json
{
  "jsonrpc": "2.0",
  "id": 30,
  "method": "tools/call",
  "params": {
    "name": "add_group_member",
    "arguments": {
      "parent_group_id": 8,
      "member_user_id": 1
    }
  }
}
```

**Step 4: Grant everyone access to read-only tools**

```json
{
  "jsonrpc": "2.0",
  "id": 31,
  "method": "tools/call",
  "params": {
    "name": "grant_mcp_privilege",
    "arguments": {
      "group_id": 1,
      "privilege_identifier": "list_user_groups"
    }
  }
}
```

```json
{
  "jsonrpc": "2.0",
  "id": 32,
  "method": "tools/call",
  "params": {
    "name": "grant_mcp_privilege",
    "arguments": {
      "group_id": 1,
      "privilege_identifier": "list_user_group_memberships"
    }
  }
}
```

**Result:**

- Only admins can create/delete users and groups
- Only admins can grant privileges
- Everyone can view group information

## Creating Limited-Privilege Tokens

### Scenario: CI/CD Pipeline Token

You need to create a service token for your CI/CD pipeline that can only:

- Access the test database (connection_id: 15)
- Run database migrations (but no other admin tools)

**Step 1: Create the service token**

```json
{
  "jsonrpc": "2.0",
  "id": 33,
  "method": "tools/call",
  "params": {
    "name": "create_service_token",
    "arguments": {
      "token_name": "ci-pipeline",
      "is_superuser": false
    }
  }
}
```

Response:

```json
{
  "token_id": 50,
  "token_value": "svc_abc123..."
}
```

**Step 2: Create a group for CI/CD access**

```json
{
  "jsonrpc": "2.0",
  "id": 34,
  "method": "tools/call",
  "params": {
    "name": "create_user_group",
    "arguments": {
      "name": "ci-access",
      "description": "Access for CI/CD pipelines"
    }
  }
}
```

Response: `{"group_id": 9}`

**Step 3: Grant connection access to group**

```json
{
  "jsonrpc": "2.0",
  "id": 35,
  "method": "tools/call",
  "params": {
    "name": "grant_connection_privilege",
    "arguments": {
      "group_id": 9,
      "connection_id": 15,
      "access_level": "read_write"
    }
  }
}
```

**Step 4: Add service token to group**

Since service tokens don't automatically belong to groups, you would need to
implement group assignment for service tokens, or use token scoping instead.

**Alternative approach using token scoping:**

```json
{
  "jsonrpc": "2.0",
  "id": 36,
  "method": "tools/call",
  "params": {
    "name": "set_token_connection_scope",
    "arguments": {
      "token_id": 50,
      "token_type": "service",
      "connection_ids": [15]
    }
  }
}
```

**Result:** The service token can only access the test database, implementing
least-privilege access for the CI/CD pipeline.

### Scenario: Developer Personal Token with Limited Scope

Developer Alice wants a personal token for her local development environment
that only accesses the dev database.

**Step 1: Create user token**

```json
{
  "jsonrpc": "2.0",
  "id": 37,
  "method": "tools/call",
  "params": {
    "name": "create_user_token",
    "arguments": {
      "user_id": 20,
      "token_name": "alice-local-dev"
    }
  }
}
```

Response:

```json
{
  "token_id": 51,
  "token_value": "usr_xyz789..."
}
```

**Step 2: Scope token to dev database only**

```json
{
  "jsonrpc": "2.0",
  "id": 38,
  "method": "tools/call",
  "params": {
    "name": "set_token_connection_scope",
    "arguments": {
      "token_id": 51,
      "token_type": "user",
      "connection_ids": [10]
    }
  }
}
```

**Result:** Even though Alice might have access to staging through her group
membership, this token can only access the dev database.

## Onboarding New Users

### Scenario: New Backend Developer Joining

A new developer joins your backend team and needs appropriate access.

**Step 1: Create user account**

```json
{
  "jsonrpc": "2.0",
  "id": 39,
  "method": "tools/call",
  "params": {
    "name": "create_user",
    "arguments": {
      "username": "charlie",
      "email": "charlie@example.com",
      "password": "temporaryPassword123",
      "is_superuser": false
    }
  }
}
```

Response: `{"user_id": 25}`

**Step 2: Add to backend team**

```json
{
  "jsonrpc": "2.0",
  "id": 40,
  "method": "tools/call",
  "params": {
    "name": "add_group_member",
    "arguments": {
      "parent_group_id": 4,
      "member_user_id": 25
    }
  }
}
```

**Step 3: Verify access**

```json
{
  "jsonrpc": "2.0",
  "id": 41,
  "method": "tools/call",
  "params": {
    "name": "list_user_group_memberships",
    "arguments": {
      "user_id": 25
    }
  }
}
```

Response:

```json
{
  "groups": [
    {"group_id": 4, "group_name": "backend-team", "membership_type": "direct"},
    {"group_id": 2, "group_name": "engineering", "membership_type": "indirect"},
    {"group_id": 1, "group_name": "all-staff", "membership_type": "indirect"}
  ]
}
```

**Result:** Charlie automatically inherits:

- Backend team's read_write access to dev and staging databases
- Engineering department's privileges
- All-staff privileges

## Offboarding Users

### Scenario: Developer Leaving the Company

Developer Bob is leaving and you need to revoke all access.

**Step 1: List user's tokens**

```json
{
  "jsonrpc": "2.0",
  "id": 42,
  "method": "tools/call",
  "params": {
    "name": "list_user_tokens",
    "arguments": {
      "user_id": 6
    }
  }
}
```

Response:

```json
{
  "tokens": [
    {"token_id": 20, "token_name": "bob-laptop"},
    {"token_id": 21, "token_name": "bob-mobile"}
  ]
}
```

**Step 2: Delete all user tokens**

```json
{
  "jsonrpc": "2.0",
  "id": 43,
  "method": "tools/call",
  "params": {
    "name": "delete_user_token",
    "arguments": {
      "token_id": 20
    }
  }
}
```

```json
{
  "jsonrpc": "2.0",
  "id": 44,
  "method": "tools/call",
  "params": {
    "name": "delete_user_token",
    "arguments": {
      "token_id": 21
    }
  }
}
```

**Step 3: Delete user account**

```json
{
  "jsonrpc": "2.0",
  "id": 45,
  "method": "tools/call",
  "params": {
    "name": "delete_user",
    "arguments": {
      "user_id": 6
    }
  }
}
```

**Result:** Bob's account and all tokens are removed. All group memberships are
automatically deleted via CASCADE.

## Troubleshooting Access Issues

### Scenario: User Cannot Access a Connection

User reports they cannot access a database connection they should have access
to.

**Step 1: Check user's group memberships**

```json
{
  "jsonrpc": "2.0",
  "id": 46,
  "method": "tools/call",
  "params": {
    "name": "list_user_group_memberships",
    "arguments": {
      "user_id": 20
    }
  }
}
```

**Step 2: Check connection privileges**

```json
{
  "jsonrpc": "2.0",
  "id": 47,
  "method": "tools/call",
  "params": {
    "name": "list_connection_privileges",
    "arguments": {
      "connection_id": 10
    }
  }
}
```

**Step 3: If using a token, check token scope**

```json
{
  "jsonrpc": "2.0",
  "id": 48,
  "method": "tools/call",
  "params": {
    "name": "get_token_scope",
    "arguments": {
      "token_id": 51,
      "token_type": "user"
    }
  }
}
```

**Common issues:**

1. **User not in any group with access** - Add user to appropriate group
2. **Token scoped to different connections** - Update token scope or remove
   scope restrictions
3. **User has read but needs read_write** - Grant higher access level to user's
   group

### Scenario: User Can Access Something They Shouldn't

User has access to a connection or tool they shouldn't have.

**Step 1: Check user's group memberships**

```json
{
  "jsonrpc": "2.0",
  "id": 49,
  "method": "tools/call",
  "params": {
    "name": "list_user_group_memberships",
    "arguments": {
      "user_id": 20
    }
  }
}
```

**Step 2: For each group, check what privileges it has**

```json
{
  "jsonrpc": "2.0",
  "id": 50,
  "method": "tools/call",
  "params": {
    "name": "list_group_mcp_privileges",
    "arguments": {
      "group_id": 4
    }
  }
}
```

**Step 3: Remove from inappropriate group or revoke privilege**

Option 1 - Remove user from group:

```json
{
  "jsonrpc": "2.0",
  "id": 51,
  "method": "tools/call",
  "params": {
    "name": "remove_group_member",
    "arguments": {
      "parent_group_id": 4,
      "member_user_id": 20
    }
  }
}
```

Option 2 - Revoke privilege from group:

```json
{
  "jsonrpc": "2.0",
  "id": 52,
  "method": "tools/call",
  "params": {
    "name": "revoke_connection_privilege",
    "arguments": {
      "group_id": 4,
      "connection_id": 12
    }
  }
}
```

**Best Practice:** Review group hierarchies carefully. A user might have
unexpected access through indirect membership (e.g., backend-team → engineering
→ all-staff).
