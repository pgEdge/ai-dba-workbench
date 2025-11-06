# MCP Resources

The MCP server exposes resources that provide read-only access to system data.
Resources enable AI assistants to browse and inspect available data without
making changes.

## Overview

Resources in MCP provide a way to expose structured data that AI assistants
can read. The pgEdge AI Workbench MCP server provides resources for user
accounts and service tokens.

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
- Auditing account settings and permissions
- Understanding the current system state
- Providing context to AI assistants for management tasks
