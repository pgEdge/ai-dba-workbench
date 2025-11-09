# MCP Resources

The MCP server exposes resources that provide read-only access to system data.
Resources enable AI assistants to browse and inspect available data without
making changes.

## Overview

Resources in MCP provide a way to expose structured data that AI assistants
can read. The pgEdge AI Workbench MCP server provides resources for user
accounts, service tokens, user groups, group memberships, user tokens,
connection privileges, and MCP privileges.

## Available Resources

### User Accounts Resource

- **URI**: `ai-workbench://users`
- **Description**: List of all user accounts in the system
- **MIME Type**: `application/json`

#### Listing Users

Request:

```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "resources/read",
    "params": {
        "uri": "ai-workbench://users"
    }
}
```

Response:

```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "result": {
        "contents": [
            {
                "uri": "ai-workbench://users/admin",
                "mimeType": "application/json",
                "text": "{\"username\": \"admin\", \"email\": \"admin@example.com\", \"fullName\": \"Administrator\", \"isSuperuser\": true, \"passwordExpiry\": null, \"createdAt\": \"2025-01-15T10:30:00Z\", \"updatedAt\": \"2025-01-15T10:30:00Z\"}"
            }
        ]
    }
}
```

#### User Data Fields

Each user entry contains:

- `username` (string) - Unique username
- `email` (string) - User's email address
- `fullName` (string) - Full name of the user
- `isSuperuser` (boolean) - Whether the user has superuser privileges
- `passwordExpiry` (timestamp or null) - When the password expires
- `createdAt` (timestamp) - When the account was created
- `updatedAt` (timestamp) - When the account was last updated

### Service Tokens Resource

- **URI**: `ai-workbench://service-tokens`
- **Description**: List of all service tokens in the system
- **MIME Type**: `application/json`

#### Listing Service Tokens

Request:

```json
{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "resources/read",
    "params": {
        "uri": "ai-workbench://service-tokens"
    }
}
```

Response:

```json
{
    "jsonrpc": "2.0",
    "id": 2,
    "result": {
        "contents": [
            {
                "uri": "ai-workbench://service-tokens/api-integration",
                "mimeType": "application/json",
                "text": "{\"name\": \"api-integration\", \"isSuperuser\": false, \"note\": \"API access token\", \"expiresAt\": \"2026-01-15T00:00:00Z\", \"createdAt\": \"2025-01-15T10:30:00Z\", \"updatedAt\": \"2025-01-15T10:30:00Z\"}"
            }
        ]
    }
}
```

#### Service Token Data Fields

Each service token entry contains:

- `name` (string) - Unique token name
- `isSuperuser` (boolean) - Whether the token has superuser privileges
- `note` (string or null) - Optional note about the token
- `expiresAt` (timestamp or null) - When the token expires
- `createdAt` (timestamp) - When the token was created
- `updatedAt` (timestamp) - When the token was last updated

**Note**: The actual token value is never exposed through resources. It is
only shown once when the token is created.

### User Groups Resource

- **URI**: `ai-workbench://groups`
- **Description**: List of all user groups in the system
- **MIME Type**: `application/json`

Returns all user groups with their details.

### User Tokens Resource

- **URI**: `ai-workbench://users/{username}/tokens`
- **Description**: List of tokens for a specific user
- **MIME Type**: `application/json`

Returns all tokens belonging to the specified user. Users can only access
their own tokens unless they are superusers.

### Group Members Resource

- **URI**: `ai-workbench://groups/{groupId}/members`
- **Description**: List of members (users and groups) in a group
- **MIME Type**: `application/json`

Returns all direct members of the specified group, including both users and
nested groups.

### User Group Memberships Resource

- **URI**: `ai-workbench://users/{username}/groups`
- **Description**: List of groups a user belongs to
- **MIME Type**: `application/json`

Returns all groups (both direct and inherited through hierarchy) that the
specified user is a member of.

### Connection Privileges Resource

- **URI**: `ai-workbench://connections/{connectionId}/privileges`
- **Description**: List of group privileges for a database connection
- **MIME Type**: `application/json`

Returns all groups that have been granted access to the specified database
connection, along with their access levels.

### MCP Privilege Identifiers Resource

- **URI**: `ai-workbench://mcp-privileges`
- **Description**: List of all registered MCP privilege identifiers
- **MIME Type**: `application/json`

Returns all registered MCP tools, resources, and prompts that can be granted
to groups for access control.

### Group MCP Privileges Resource

- **URI**: `ai-workbench://groups/{groupId}/mcp-privileges`
- **Description**: List of MCP privileges granted to a group
- **MIME Type**: `application/json`

Returns all MCP privileges (tools, resources, prompts) that have been granted
to the specified group.

## Resource Discovery

To discover available resources, use the `resources/list` method:

Request:

```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "resources/list"
}
```

Response:

```json
{
    "jsonrpc": "2.0",
    "id": 1,
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
            },
            {
                "uri": "ai-workbench://groups",
                "name": "User Groups",
                "description": "List of all user groups in the system",
                "mimeType": "application/json"
            },
            {
                "uri": "ai-workbench://users/{username}/tokens",
                "name": "User Tokens",
                "description": "List of tokens for a specific user",
                "mimeType": "application/json"
            },
            {
                "uri": "ai-workbench://groups/{groupId}/members",
                "name": "Group Members",
                "description": "List of members (users and groups) in a group",
                "mimeType": "application/json"
            },
            {
                "uri": "ai-workbench://users/{username}/groups",
                "name": "User Group Memberships",
                "description": "List of groups a user belongs to",
                "mimeType": "application/json"
            },
            {
                "uri": "ai-workbench://connections/{connectionId}/privileges",
                "name": "Connection Privileges",
                "description": "List of group privileges for a database connection",
                "mimeType": "application/json"
            },
            {
                "uri": "ai-workbench://mcp-privileges",
                "name": "MCP Privilege Identifiers",
                "description": "List of all registered MCP privilege identifiers",
                "mimeType": "application/json"
            },
            {
                "uri": "ai-workbench://groups/{groupId}/mcp-privileges",
                "name": "Group MCP Privileges",
                "description": "List of MCP privileges granted to a group",
                "mimeType": "application/json"
            }
        ]
    }
}
```

## Security Considerations

- Resources provide read-only access to system data
- Authentication and authorization apply to resource access
- Sensitive information (password hashes, token values) is never exposed
- Resource access is logged for audit purposes

## Use Cases

Resources are useful for:

- Browsing available user accounts and their properties
- Inspecting service token configurations
- Viewing user groups and their hierarchies
- Checking user token assignments and group memberships
- Reviewing connection access privileges for groups
- Auditing MCP privilege assignments
- Understanding the current system state
- Providing context to AI assistants for management tasks
