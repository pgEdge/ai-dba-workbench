# Authentication

## Overview

The pgEdge AI DBA Workbench implements bearer token-based authentication to
secure access to the MCP server and its resources. This document describes
the authentication system, token types, and how to use authentication with
the CLI and API.

## Authentication Architecture

### Token Types

The system supports two types of authentication tokens:

1. **Session Tokens** - Temporary tokens issued after username/password
   authentication
    - Valid for 24 hours from creation
    - Automatically updated on each use
    - Stored in the `user_sessions` table
    - Obtained via the `authenticate_user` tool
    - Inherit superuser status from the associated user account

2. **Service Tokens** - Long-lived tokens for automated systems
    - Optional expiration date
    - Manually created by administrators
    - Stored in the `service_tokens` table
    - Created via the `create_service_token` tool
    - Can be created with or without superuser privileges

### Authorization Levels

The system implements role-based access control through superuser privileges:

- **Regular Users/Tokens** - Can use read-only tools and resources
- **Superusers** - Can perform administrative operations including:
    - Creating, updating, and deleting user accounts
    - Creating, updating, and deleting service tokens
    - All operations available to regular users

**Tools Requiring Superuser Privileges:**

- `create_user` - Create new user accounts
- `update_user` - Modify existing user accounts
- `delete_user` - Remove user accounts
- `create_service_token` - Generate new service tokens
- `update_service_token` - Modify existing service tokens
- `delete_service_token` - Revoke service tokens

**Note:** The `authenticate_user` tool does not require superuser privileges,
as users need to authenticate to obtain a session token.

### Authentication Flow

```
┌─────────┐                 ┌─────────┐                 ┌──────────┐
│   CLI   │                 │  Server │                 │ Database │
└────┬────┘                 └────┬────┘                 └────┬─────┘
     │                           │                           │
     │ 1. Request with username/ │                           │
     │    password (or token)    │                           │
     ├──────────────────────────>│                           │
     │                           │                           │
     │                           │ 2. Validate credentials   │
     │                           ├──────────────────────────>│
     │                           │                           │
     │                           │ 3. Return user info       │
     │                           │<──────────────────────────┤
     │                           │                           │
     │                           │ 4. Generate session token │
     │                           │                           │
     │                           │ 5. Store session          │
     │                           ├──────────────────────────>│
     │                           │                           │
     │ 6. Return session token   │                           │
     │<──────────────────────────┤                           │
     │                           │                           │
     │ 7. Subsequent requests    │                           │
     │    with Bearer token      │                           │
     ├──────────────────────────>│                           │
     │                           │                           │
     │                           │ 8. Validate token         │
     │                           ├──────────────────────────>│
     │                           │                           │
     │                           │ 9. Return validation      │
     │                           │<──────────────────────────┤
     │                           │                           │
     │ 10. Response              │                           │
     │<──────────────────────────┤                           │
     │                           │                           │
```

## Using Authentication

### CLI Authentication

The CLI supports three methods of authentication (checked in this order):

1. **Command-line token** - Provide a token via `--token` flag
2. **Token file** - Store token in `~/.pgedge-ai-workbench-token`
3. **Interactive login** - Prompted for username and password

#### Using a Token

```bash
# Provide token via command-line
ai-cli --token <your-token> list-tools

# Or store in file
echo "your-token-here" > ~/.pgedge-ai-workbench-token
ai-cli list-tools
```

#### Interactive Login

If no token is found, the CLI will prompt for credentials:

```bash
$ ai-cli list-tools
Username: admin
Password: ********
# Session token is automatically used for the request
```

#### Exempt Commands

The following commands do not require authentication:

- `ping` - Server health check
- `--version` - Show CLI version

### API Authentication

When using the HTTP/HTTPS API directly, include the bearer token in the
Authorization header:

```http
POST /mcp HTTP/1.1
Host: localhost:8080
Content-Type: application/json
Authorization: Bearer <your-token>

{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/list",
    "params": {}
}
```

## Token Management

### Creating User Accounts

User accounts are created using the `create_user` tool (requires superuser):

```bash
# Create regular user
echo '{
  "username": "newuser",
  "email": "user@example.com",
  "fullName": "New User",
  "password": "securepassword"
}' | ai-cli run-tool create_user

# Create user with superuser privileges
echo '{
  "username": "admin2",
  "email": "admin2@example.com",
  "fullName": "Administrator Two",
  "password": "securepassword",
  "isSuperuser": true
}' | ai-cli run-tool create_user
```

### Creating Service Tokens

Service tokens are created using the `create_service_token` tool (requires
superuser):

```bash
# Create regular service token
echo '{"name": "ci_pipeline", "note": "CI/CD automation"}' | \
  ai-cli run-tool create_service_token

# Create service token with superuser privileges
echo '{
  "name": "admin_token",
  "note": "Administrative automation",
  "isSuperuser": true
}' | ai-cli run-tool create_service_token

# Create token with expiration
echo '{
  "name": "temp_token",
  "note": "Temporary token",
  "expires_at": "2025-12-31T23:59:59Z"
}' | ai-cli run-tool create_service_token
```

### Updating User Passwords

Passwords can be updated using the `update_user` tool:

```bash
echo '{
  "username": "admin",
  "password": "new-secure-password"
}' | ai-cli run-tool update_user
```

### Revoking Access

#### Delete User Sessions

To revoke all active sessions for a user, delete the user account (this
cascades to sessions):

```bash
echo '{"username": "olduser"}' | ai-cli run-tool delete_user
```

#### Delete Service Tokens

To revoke a service token:

```bash
echo '{"token": "the-token-to-delete"}' | \
  ai-cli run-tool delete_service_token
```

## Database Schema

### user_accounts Table

```sql
CREATE TABLE user_accounts (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    email TEXT NOT NULL,
    is_superuser BOOLEAN NOT NULL DEFAULT FALSE,
    full_name TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    password_expiry TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

**Key Fields:**

- `is_superuser` - Grants administrative privileges for user management

### user_sessions Table

```sql
CREATE TABLE user_sessions (
    session_token TEXT PRIMARY KEY,
    username TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    last_used_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_username FOREIGN KEY (username)
        REFERENCES user_accounts(username) ON DELETE CASCADE
);
```

**Note:** Session tokens inherit the `is_superuser` status from the associated
user account.

### service_tokens Table

```sql
CREATE TABLE service_tokens (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    token_hash TEXT NOT NULL UNIQUE,
    is_superuser BOOLEAN NOT NULL DEFAULT FALSE,
    note TEXT,
    expires_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

**Key Fields:**

- `is_superuser` - Grants administrative privileges for token operations

## Security Considerations

### Password Storage

- Passwords are hashed using SHA-256 before storage
- Password hashes are never transmitted or logged
- Plain-text passwords are only held in memory during authentication

### Token Storage

- Service tokens are hashed using SHA-256 in the database
- Session tokens are stored as plain text (they are randomly generated)
- Tokens should be treated as credentials and protected accordingly

### Token Transmission

- Tokens are transmitted via HTTPS in the Authorization header
- Tokens should never be logged or displayed in plain text
- Use TLS/SSL for all production deployments

### Session Management

- Sessions expire after 24 hours
- The `last_used_at` timestamp is updated on each request
- Expired sessions are not automatically cleaned up (manual cleanup required)

### Best Practices

1. **Use service tokens for automation** - Avoid storing user passwords in
   scripts
2. **Enable TLS in production** - Always use HTTPS for production deployments
3. **Rotate tokens regularly** - Create new service tokens and delete old ones
   periodically
4. **Use strong passwords** - Enforce password complexity requirements for user
   accounts
5. **Monitor authentication logs** - Review server logs for failed
   authentication attempts
6. **Limit token scope** - Create separate service tokens for different systems
7. **Store tokens securely** - Use secure storage mechanisms for token files

## Troubleshooting

### Authentication Failed Error

If you receive "Authentication failed" errors:

1. Verify the username and password are correct
2. Check if the user account exists in the database
3. Verify the password has not expired
4. Check server logs for detailed error messages

### Token Validation Failed

If token validation fails:

1. Verify the token is correct and not modified
2. Check if the token has expired
3. Verify the token exists in the database
4. Ensure the Authorization header is properly formatted

### Connection Refused

If you cannot connect to the server:

1. Verify the server is running
2. Check the server URL and port
3. Verify network connectivity
4. Check firewall rules

### Permission Denied Error

If you receive "permission denied: superuser privileges required" errors:

1. **Verify Superuser Status**
    - Check if your user account has superuser privileges
    - Query the database: `SELECT is_superuser FROM user_accounts WHERE
      username = 'your_username'`
    - For service tokens: `SELECT is_superuser FROM service_tokens WHERE name =
      'token_name'`

2. **Grant Superuser Privileges**
    - Have an existing superuser update your account using `update_user` tool
    - Or directly update the database as a database administrator

3. **Use Correct Token**
    - Ensure you're using a token that has superuser privileges
    - Session tokens inherit privileges from the user account
    - Service tokens have their own independent superuser status

4. **Affected Operations**
    - User management: `create_user`, `update_user`, `delete_user`
    - Token management: `create_service_token`, `update_service_token`,
      `delete_service_token`

**Example Error:**

```
Error: MCP error -32603: permission denied: superuser privileges required
```

## API Reference

### authenticate_user Tool

Authenticates a user and returns a session token.

**Input:**
```json
{
    "username": "admin",
    "password": "password123"
}
```

**Output:**
```json
{
    "content": [
        {
            "type": "text",
            "text": "Authentication successful. Session token: abc123...\nExpires at: 2025-11-07T16:00:00Z"
        }
    ]
}
```

### create_user Tool

Creates a new user account. **Requires superuser privileges.**

**Input:**
```json
{
    "username": "newuser",
    "email": "user@example.com",
    "fullName": "New User",
    "password": "securepass",
    "isSuperuser": false,
    "passwordExpiry": "2026-12-31T23:59:59Z"
}
```

**Note:** The `isSuperuser` field determines whether the new user has
administrative privileges.

### update_user Tool

Updates an existing user account. **Requires superuser privileges.**

**Input:**
```json
{
    "username": "existinguser",
    "email": "newemail@example.com",
    "isSuperuser": true
}
```

### delete_user Tool

Deletes a user account. **Requires superuser privileges.**

**Input:**
```json
{
    "username": "userToDelete"
}
```

### create_service_token Tool

Creates a new service token. **Requires superuser privileges.**

**Input:**
```json
{
    "name": "ci_pipeline",
    "note": "CI/CD automation",
    "isSuperuser": false,
    "expiresAt": "2026-12-31T23:59:59Z"
}
```

**Output:**
```json
{
    "content": [
        {
            "type": "text",
            "text": "Service token created: xyz789..."
        }
    ]
}
```

**Note:** The `isSuperuser` field determines whether the token has
administrative privileges.

### update_service_token Tool

Updates an existing service token. **Requires superuser privileges.**

**Input:**
```json
{
    "name": "existing_token",
    "note": "Updated description",
    "isSuperuser": true
}
```

### delete_service_token Tool

Deletes a service token. **Requires superuser privileges.**

**Input:**
```json
{
    "name": "tokenToDelete"
}
```

## See Also

- [CLI Documentation](cli/index.md) - CLI usage and examples
- [Server Documentation](server/index.md) - Server configuration and API
- [Security Best Practices](server/index.md#security) - Security guidelines
