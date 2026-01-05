# User Token Management

This document explains how to create and manage user-owned API tokens for
command-line and programmatic access to the pgEdge AI DBA Workbench MCP server.

## Overview

User tokens are API tokens that individual users can create for their own
command-line applications and scripts. Unlike service tokens (which are
system-wide and require superuser privileges to manage), user tokens:

- Are owned by individual users
- Can be created and managed by the owning user
- Inherit the `is_superuser` flag from the owning user
- Are automatically deleted when the user account is deleted
- Support configurable expiration times (or indefinite lifetime)

## Configuration

The maximum lifetime for user tokens is controlled by the
`max_user_token_lifetime_days` configuration option in the server
configuration file.

### Configuration Option

```
# Maximum lifetime for user tokens in days
# 0 = indefinite lifetime allowed
# >0 = maximum number of days a token can be valid
# Default: 90 days
max_user_token_lifetime_days = 90
```

**Examples:**

```
# Allow tokens up to 180 days
max_user_token_lifetime_days = 180

# Allow indefinite lifetime tokens
max_user_token_lifetime_days = 0

# Restrict tokens to 30 days maximum
max_user_token_lifetime_days = 30
```

## MCP Tools

User tokens are managed through three MCP server tools:

### create_user_token

Creates a new user token for command-line API access.

**Parameters:**
- `username` (string, required): Username who will own this token
- `lifetimeDays` (integer, required): Token lifetime in days (0 = indefinite,
    subject to server maximum)
- `name` (string, optional): Descriptive name for the token
- `note` (string, optional): Additional information about the token

**Authorization:**
- Users can create tokens for themselves
- Superusers can create tokens for any user

**Returns:**
- Success message with expiration information
- The generated token (displayed only once)

**Example:**

```json
{
    "name": "create_user_token",
    "arguments": {
        "username": "alice",
        "name": "my-cli-token",
        "lifetimeDays": 30,
        "note": "Token for automation scripts"
    }
}
```

**Response:**

```
User token created successfully for 'alice' (expires in 30 days)
Token: awb_user_a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6
IMPORTANT: Save this token now. You won't be able to see it again.
```

### list_user_tokens

Lists all tokens for a specific user.

**Parameters:**
- `username` (string, required): Username whose tokens to list

**Authorization:**
- Users can list their own tokens
- Superusers can list any user's tokens

**Returns:**
- Array of token metadata (id, name, note, expires_at, created_at,
    is_expired)
- Does not return the actual token values (only viewable at creation time)

**Example:**

```json
{
    "name": "list_user_tokens",
    "arguments": {
        "username": "alice"
    }
}
```

**Response:**

```json
User tokens for 'alice':
[
  {
    "id": 42,
    "name": "my-cli-token",
    "note": "Token for automation scripts",
    "expires_at": "2025-12-07 14:23:45",
    "created_at": "2025-11-07 14:23:45",
    "is_expired": false
  },
  {
    "id": 43,
    "name": "backup-token",
    "note": null,
    "expires_at": null,
    "created_at": "2025-10-15 09:12:30",
    "is_expired": false
  }
]
```

### delete_user_token

Deletes a user token by ID.

**Parameters:**
- `username` (string, required): Username who owns the token
- `tokenId` (integer, required): ID of the token to delete (from
    `list_user_tokens`)

**Authorization:**
- Users can delete their own tokens
- Superusers can delete any user's tokens

**Returns:**
- Success confirmation message

**Example:**

```json
{
    "name": "delete_user_token",
    "arguments": {
        "username": "alice",
        "tokenId": 42
    }
}
```

**Response:**

```
User token 42 deleted successfully
```

## Usage Examples

### Creating a Token for Personal Use

1. Authenticate as a user:

```json
{
    "name": "authenticate_user",
    "arguments": {
        "username": "alice",
        "password": "your-password"
    }
}
```

2. Create a token with 90-day expiration:

```json
{
    "name": "create_user_token",
    "arguments": {
        "username": "alice",
        "name": "desktop-cli",
        "lifetimeDays": 90,
        "note": "Token for daily CLI operations"
    }
}
```

3. Save the returned token securely

### Creating an Indefinite Token (if allowed by server)

If the server is configured with `max_user_token_lifetime_days = 0`:

```json
{
    "name": "create_user_token",
    "arguments": {
        "username": "alice",
        "name": "long-term-automation",
        "lifetimeDays": 0
    }
}
```

### Managing Tokens

List your tokens:

```json
{
    "name": "list_user_tokens",
    "arguments": {
        "username": "alice"
    }
}
```

Delete an expired or unused token:

```json
{
    "name": "delete_user_token",
    "arguments": {
        "username": "alice",
        "tokenId": 42
    }
}
```

### Superuser Creating Token for Another User

Superusers can create tokens for any user:

```json
{
    "name": "create_user_token",
    "arguments": {
        "username": "bob",
        "name": "admin-created-token",
        "lifetimeDays": 7,
        "note": "Temporary token for project work"
    }
}
```

## Using User Tokens

Once created, user tokens can be used as bearer tokens in HTTP requests to the
MCP server:

```bash
curl -H "Authorization: Bearer awb_user_a1b2c3d4..." \
     https://your-server/mcp \
     -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'
```

Or in MCP client configuration:

```json
{
    "mcpServers": {
        "ai-workbench": {
            "url": "https://your-server/mcp",
            "headers": {
                "Authorization": "Bearer awb_user_a1b2c3d4..."
            }
        }
    }
}
```

## Token Properties

### Lifetime

- Tokens can have a configured lifetime from 1 to N days, where N is set by
    `max_user_token_lifetime_days`
- Set `lifetimeDays` to 0 for indefinite lifetime (only if server allows it)
- Expired tokens cannot be used for authentication
- The `is_expired` field in token listings indicates current status

### Ownership and Permissions

- Each token is owned by a specific user account
- Tokens inherit the `is_superuser` flag from their owner
- If a user's `is_superuser` flag changes, their tokens' permissions change
    accordingly
- When a user account is deleted, all their tokens are automatically deleted
    (CASCADE)

### Security

- Token values are hashed using bcrypt before storage
- Tokens are only displayed once at creation time
- Tokens cannot be retrieved after creation (only metadata is available)
- Lost tokens must be deleted and recreated

## Best Practices

1. **Use Descriptive Names**: Give tokens meaningful names like
    "jenkins-ci" or "backup-script" to identify their purpose

2. **Add Notes**: Document what each token is used for in the `note` field

3. **Regular Cleanup**: Periodically list and delete unused or expired tokens

4. **Appropriate Lifetimes**: Use shorter lifetimes (7-30 days) for
    high-privilege tokens, longer lifetimes (90+ days) or indefinite for
    low-privilege automation

5. **Token Rotation**: For security-critical applications, rotate tokens
    regularly by creating new ones and deleting old ones

6. **Secure Storage**: Store tokens securely in environment variables or
    secret management systems, never in source code

## Differences from Service Tokens

| Feature | User Tokens | Service Tokens |
|---------|-------------|----------------|
| Created by | Users (for themselves) or superusers | Superusers only |
| Ownership | Owned by a user account | System-wide |
| Permissions | Inherit from user's is_superuser | Configured per token |
| Cascade delete | Deleted when user is deleted | Independent |
| Lifetime | Configurable with server maximum | Configurable, no maximum |
| Use case | Personal CLI/API access | System integration, services |

## Troubleshooting

### "Permission denied: can only create tokens for your own account"

You are trying to create a token for another user without superuser
privileges. Users can only create tokens for themselves.

### "lifetime_days must be between 1 and 90"

The server's `max_user_token_lifetime_days` is set to 90, and you requested a
longer lifetime. Either reduce the lifetime or ask an administrator to adjust
the server configuration.

### "Token not found or does not belong to user"

When deleting a token, ensure:
- You're using the correct token ID from `list_user_tokens`
- You have permission to delete this user's tokens
- The token hasn't already been deleted

### Token authentication failing

Check:
- Token hasn't expired (check `is_expired` field in token list)
- Token is being sent correctly in Authorization header
- User account hasn't been deleted or disabled

## See Also

- [Configuration Reference](config-reference.md) - All server configuration
    options
- [MCP Server Documentation](index.md) - Complete MCP server documentation
- [Service Token Management](service-tokens.md) - Managing service tokens
