# MCP Tools

The MCP server provides tools that enable AI assistants to perform operations
on the system. Tools are callable functions that can create, update, and delete
system resources.

## Overview

Tools in MCP allow AI assistants to make changes to the system. The pgEdge AI
Workbench MCP server provides tools for managing user accounts and service
tokens.

## Tool Discovery

To discover available tools, use the `tools/list` method:

Request:

```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/list"
}
```

Response:

```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "result": {
        "tools": [
            {
                "name": "create_user",
                "description": "Create a new user account",
                "inputSchema": { ... }
            },
            ...
        ]
    }
}
```

## Authentication

### authenticate_user

Authenticates a user and returns a session token.

**Note:** This tool does NOT require superuser privileges, as users need to
authenticate to obtain a session token.

#### Input Schema

```json
{
    "type": "object",
    "properties": {
        "username": {
            "type": "string",
            "description": "Username to authenticate"
        },
        "password": {
            "type": "string",
            "description": "Password for authentication"
        }
    },
    "required": ["username", "password"]
}
```

#### Example Usage

Request:

```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/call",
    "params": {
        "name": "authenticate_user",
        "arguments": {
            "username": "admin",
            "password": "password123"
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
        "content": [
            {
                "type": "text",
                "text": "Authentication successful. Session token: abc123...\nExpires at: 2025-11-07T16:00:00Z"
            }
        ]
    }
}
```

## User Management Tools

**IMPORTANT:** All user management tools require superuser privileges. See the
[Authentication Guide](../authentication.md) for details on superuser access
control.

### create_user

Creates a new user account. **Requires superuser privileges.**

#### Input Schema

```json
{
    "type": "object",
    "properties": {
        "username": {
            "type": "string",
            "description": "Username for the new user"
        },
        "email": {
            "type": "string",
            "description": "Email address for the new user"
        },
        "fullName": {
            "type": "string",
            "description": "Full name of the user"
        },
        "password": {
            "type": "string",
            "description": "Password for the new user"
        },
        "isSuperuser": {
            "type": "boolean",
            "description": "Whether the user should have superuser privileges",
            "default": false
        },
        "passwordExpiry": {
            "type": "string",
            "description": "Password expiry date (YYYY-MM-DD format, optional)"
        }
    },
    "required": ["username", "email", "fullName", "password"]
}
```

#### Example Usage

Request:

```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/call",
    "params": {
        "name": "create_user",
        "arguments": {
            "username": "jdoe",
            "email": "jdoe@example.com",
            "fullName": "John Doe",
            "password": "SecurePass123!",
            "isSuperuser": false,
            "passwordExpiry": "2026-01-15"
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
        "content": [
            {
                "type": "text",
                "text": "User 'jdoe' created successfully"
            }
        ]
    }
}
```

### update_user

Updates an existing user account. **Requires superuser privileges.**

Supports partial updates - only the fields provided will be changed.

#### Input Schema

```json
{
    "type": "object",
    "properties": {
        "username": {
            "type": "string",
            "description": "Username of the user to update"
        },
        "email": {
            "type": "string",
            "description": "New email address (optional)"
        },
        "fullName": {
            "type": "string",
            "description": "New full name (optional)"
        },
        "password": {
            "type": "string",
            "description": "New password (optional)"
        },
        "isSuperuser": {
            "type": "boolean",
            "description": "Update superuser status (optional)"
        },
        "passwordExpiry": {
            "type": "string",
            "description": "New password expiry date (YYYY-MM-DD format, optional)"
        },
        "clearPasswordExpiry": {
            "type": "boolean",
            "description": "Clear password expiry (optional)",
            "default": false
        }
    },
    "required": ["username"]
}
```

#### Example Usage

Request:

```json
{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "tools/call",
    "params": {
        "name": "update_user",
        "arguments": {
            "username": "jdoe",
            "email": "john.doe@example.com",
            "passwordExpiry": "2027-01-15"
        }
    }
}
```

Response:

```json
{
    "jsonrpc": "2.0",
    "id": 2,
    "result": {
        "content": [
            {
                "type": "text",
                "text": "User 'jdoe' updated successfully"
            }
        ]
    }
}
```

### delete_user

Deletes a user account. **Requires superuser privileges.**

#### Input Schema

```json
{
    "type": "object",
    "properties": {
        "username": {
            "type": "string",
            "description": "Username of the user to delete"
        }
    },
    "required": ["username"]
}
```

#### Example Usage

Request:

```json
{
    "jsonrpc": "2.0",
    "id": 3,
    "method": "tools/call",
    "params": {
        "name": "delete_user",
        "arguments": {
            "username": "jdoe"
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
        "content": [
            {
                "type": "text",
                "text": "User 'jdoe' deleted successfully"
            }
        ]
    }
}
```

## Service Token Management Tools

**IMPORTANT:** All service token management tools require superuser privileges.
See the [Authentication Guide](../authentication.md) for details on superuser
access control.

### create_service_token

Creates a new service token for API authentication. **Requires superuser
privileges.**

#### Input Schema

```json
{
    "type": "object",
    "properties": {
        "name": {
            "type": "string",
            "description": "Name for the service token"
        },
        "isSuperuser": {
            "type": "boolean",
            "description": "Whether the token should have superuser privileges",
            "default": false
        },
        "note": {
            "type": "string",
            "description": "Optional note about the token"
        },
        "expiresAt": {
            "type": "string",
            "description": "Expiry date (YYYY-MM-DD format, optional)"
        }
    },
    "required": ["name"]
}
```

#### Example Usage

Request:

```json
{
    "jsonrpc": "2.0",
    "id": 4,
    "method": "tools/call",
    "params": {
        "name": "create_service_token",
        "arguments": {
            "name": "api-integration",
            "isSuperuser": false,
            "note": "Token for external API integration",
            "expiresAt": "2026-01-15"
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
        "content": [
            {
                "type": "text",
                "text": "Service token 'api-integration' created successfully\nToken: AbCdEf123456...\nIMPORTANT: Save this token now. You won't be able to see it again."
            }
        ]
    }
}
```

**Important**: The token value is only shown once at creation time. Save it
securely as it cannot be retrieved later.

### update_service_token

Updates an existing service token. **Requires superuser privileges.**

Supports partial updates.

#### Input Schema

```json
{
    "type": "object",
    "properties": {
        "name": {
            "type": "string",
            "description": "Name of the service token to update"
        },
        "isSuperuser": {
            "type": "boolean",
            "description": "Update superuser status (optional)"
        },
        "note": {
            "type": "string",
            "description": "Update note (optional)"
        },
        "expiresAt": {
            "type": "string",
            "description": "New expiry date (YYYY-MM-DD format, optional)"
        },
        "clearNote": {
            "type": "boolean",
            "description": "Clear the note (optional)",
            "default": false
        },
        "clearExpiresAt": {
            "type": "boolean",
            "description": "Clear expiry date (optional)",
            "default": false
        }
    },
    "required": ["name"]
}
```

#### Example Usage

Request:

```json
{
    "jsonrpc": "2.0",
    "id": 5,
    "method": "tools/call",
    "params": {
        "name": "update_service_token",
        "arguments": {
            "name": "api-integration",
            "note": "Updated: Token for external API v2 integration",
            "expiresAt": "2027-01-15"
        }
    }
}
```

Response:

```json
{
    "jsonrpc": "2.0",
    "id": 5,
    "result": {
        "content": [
            {
                "type": "text",
                "text": "Service token 'api-integration' updated successfully"
            }
        ]
    }
}
```

### delete_service_token

Deletes a service token. **Requires superuser privileges.**

#### Input Schema

```json
{
    "type": "object",
    "properties": {
        "name": {
            "type": "string",
            "description": "Name of the service token to delete"
        }
    },
    "required": ["name"]
}
```

#### Example Usage

Request:

```json
{
    "jsonrpc": "2.0",
    "id": 6,
    "method": "tools/call",
    "params": {
        "name": "delete_service_token",
        "arguments": {
            "name": "api-integration"
        }
    }
}
```

Response:

```json
{
    "jsonrpc": "2.0",
    "id": 6,
    "result": {
        "content": [
            {
                "type": "text",
                "text": "Service token 'api-integration' deleted successfully"
            }
        ]
    }
}
```

## Error Handling

Tools return standard JSON-RPC error responses when operations fail:

```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "error": {
        "code": -32603,
        "message": "Tool execution failed",
        "data": "user 'jdoe' already exists"
    }
}
```

Common error scenarios:

- User or token already exists (during creation)
- User or token not found (during update/delete)
- Invalid input parameters (invalid date format, missing required fields)
- Database connection errors
- Permission denied (user or token lacks superuser privileges)
- Authentication failed (invalid or expired token)

## Security Considerations

- All tool operations (except `authenticate_user`) require authentication
- User management and service token management operations require superuser
  privileges
- Password hashing is performed server-side using SHA-256
- Service token values are generated cryptographically
- Session tokens inherit superuser status from the user account
- Service tokens have independent superuser status
- All operations are logged for audit purposes
- Rate limiting may apply to prevent abuse

For detailed information on authentication and authorization, see the
[Authentication Guide](../authentication.md).

## Best Practices

1. **Password Security**: Always use strong passwords when creating users
2. **Token Expiry**: Set expiration dates for service tokens
3. **Notes**: Add descriptive notes to service tokens for easier management
4. **Least Privilege**: Only grant superuser privileges when necessary
5. **Token Storage**: Save service token values immediately after creation
6. **Regular Audits**: Periodically review users and tokens using resources
