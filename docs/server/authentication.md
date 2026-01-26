# Authentication Guide

The MCP server includes built-in authentication with two methods: service tokens
for machine-to-machine communication and user accounts for interactive
authentication.

## Overview

- **Service Tokens**: Long-lived tokens for machine-to-machine communication
  (direct HTTP/HTTPS access)
- **User Accounts**: Interactive authentication with session tokens
- **Authentication is required** in HTTP/HTTPS mode
- **SHA256/Bcrypt hashing** for secure credential storage
- **Token expiration** with automatic cleanup
- **Per-token connection isolation** for multi-user security
- **Bearer token authentication** using HTTP Authorization header
- **Rate limiting**: Per-IP protection against brute force attacks
- **Account lockout**: Automatic account disabling after failed attempts

## Authentication Storage

Authentication data is stored in a SQLite database (`auth.db`) within the data
directory. By default, this is `./data/auth.db` relative to the server binary.

The auth store contains:

- **Users table**: Usernames, bcrypt password hashes, status, and metadata
- **Service tokens table**: Token hashes, expiry dates, and annotations
- **User tokens table**: Personal API tokens created by users
- **Session tokens**: In-memory storage for 24-hour session validity

## User Account Management

User accounts provide interactive authentication with session-based access.
Users authenticate with username and password to receive a 24-hour session
token.

### Adding Users

**Interactive mode**:

```bash
./bin/ai-dba-server -add-user
```

You'll be prompted for:

- Username (required)
- Password (hidden input, with confirmation)
- Annotation/note (optional)

**Non-interactive mode**:

```bash
./bin/ai-dba-server -add-user \
  -username alice \
  -password "SecurePassword123!" \
  -user-note "Alice Smith - Developer"
```

### Listing Users

```bash
./bin/ai-dba-server -list-users
```

Output:

```
Users:
==========================================================================================
Username             Created                   Last Login           Status      Annotation
------------------------------------------------------------------------------------------
alice                2024-10-30 10:15          2024-11-14 09:30     Enabled     Developer
bob                  2024-10-15 14:20          Never                Enabled     Admin
charlie              2024-09-01 08:00          2024-10-10 16:45     DISABLED    Former emp
==========================================================================================
```

### Updating Users

```bash
# Interactive update
./bin/ai-dba-server -update-user -username alice

# Update password from command line (less secure)
./bin/ai-dba-server -update-user \
  -username alice \
  -password "NewPassword456!"

# Update annotation only
./bin/ai-dba-server -update-user \
  -username alice \
  -user-note "Alice Smith - Senior Developer"
```

### Enabling and Disabling Users

```bash
# Disable a user account (prevents login)
./bin/ai-dba-server -disable-user -username charlie

# Re-enable a user account (also resets failed login attempts)
./bin/ai-dba-server -enable-user -username charlie
```

### Deleting Users

```bash
# Delete user (with confirmation prompt)
./bin/ai-dba-server -delete-user -username charlie
```

## Service Token Management

Service tokens provide direct API access for machine-to-machine communication.
Each token gets its own isolated database connection pool.

### Adding Tokens

**Interactive mode**:

```bash
./bin/ai-dba-server -add-token
```

You'll be prompted for:

- Annotation/note (optional)
- Expiry duration (e.g., "30d", "1y", "never")

**Non-interactive mode**:

```bash
# Standard token with 90-day expiry
./bin/ai-dba-server -add-token \
  -token-note "Production API" \
  -token-expiry "90d"

# Token that never expires (use with caution)
./bin/ai-dba-server -add-token \
  -token-note "CI/CD Pipeline" \
  -token-expiry "never"

# Superuser token (bypasses all access checks)
./bin/ai-dba-server -add-token \
  -token-note "Admin Token" \
  -token-expiry "30d" \
  -superuser
```

**Output**:

```
======================================================================
Token created successfully!
======================================================================

Token: O9ms9jqTfUdy-DIjvpFWeqd_yH_NEj7me0mgOnOjGdQ=
Hash:  b3f805a4c2e7d9f1...
ID:    1
Note:  Production API
Expires: 2025-01-28T10:15:30-05:00
======================================================================

IMPORTANT: Save this token securely - it will not be shown again!
Use it in API requests with: Authorization: Bearer <token>
======================================================================
```

### Token Expiry Formats

- `30d` - 30 days
- `1y` - 1 year
- `2w` - 2 weeks
- `12h` - 12 hours
- `1m` - 1 month (30 days)
- `never` - Token never expires (use with caution)

### Listing Tokens

```bash
./bin/ai-dba-server -list-tokens
```

Output:

```
Service Tokens:
==========================================================================================
ID     Hash Prefix        Expires              Status     Annotation
------------------------------------------------------------------------------------------
1      b3f805a4c2e7d9f1   2025-01-28 10:15     Active     Production API
2      7a2f19d8e1c4b5a3   Never                Active     CI/CD Pipeline
3      9c8d7e6f5a4b3c2d   2024-10-15 14:20     EXPIRED    Old Test Token
==========================================================================================
```

### Removing Tokens

```bash
# Remove by token ID
./bin/ai-dba-server -remove-token 1

# Remove by hash prefix (minimum 8 characters)
./bin/ai-dba-server -remove-token b3f805a4
```

## Authentication Flow

### For Interactive Applications (User Authentication)

1. **Authenticate with username/password** using the login API:

   ```bash
   curl -X POST http://localhost:8080/api/v1/auth/login \
     -H "Content-Type: application/json" \
     -d '{
       "username": "alice",
       "password": "SecurePassword123!"
     }'
   ```

2. **Receive session token** in the response:

   ```json
   {
     "success": true,
     "session_token": "AQz9XfK...",
     "expires_at": "2024-11-15T09:30:00Z",
     "message": "Authentication successful"
   }
   ```

3. **Use session token** for subsequent requests:

   ```bash
   curl -X POST http://localhost:8080/mcp/v1 \
     -H "Authorization: Bearer AQz9XfK..." \
     -H "Content-Type: application/json" \
     -d '{"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": {}}'
   ```

### For Machine-to-Machine (Service Tokens)

Use the service token directly in all requests:

```bash
curl -X POST http://localhost:8080/mcp/v1 \
  -H "Authorization: Bearer O9ms9jqTfUdy-DIjvpFWeqd_yH_NEj7me0mgOnOjGdQ=" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": {}}'
```

## Rate Limiting and Account Lockout

### Rate Limiting

The server tracks failed authentication attempts per IP address:

- **Default**: 10 failed attempts per 15-minute window
- Applies to all authentication endpoints
- Automatic cleanup of old attempt records

### Account Lockout

When a valid username is provided, the server tracks failed login attempts:

- **Default**: Disabled (0 = no lockout)
- Configurable via `max_failed_attempts_before_lockout`
- Locked accounts must be re-enabled by an administrator

### Configuration

```yaml
http:
  auth:
    enabled: true
    # Rate limiting settings
    rate_limit_window_minutes: 15
    rate_limit_max_attempts: 10
    # Account lockout settings
    max_failed_attempts_before_lockout: 5  # 0 = disabled
    # User token restrictions
    max_user_token_days: 90  # 0 = unlimited
```

### Recovering Locked Accounts

```bash
# Re-enable a locked account (also resets failed attempts)
./bin/ai-dba-server -enable-user -username alice
```

## Security Best Practices

### Password Security

- Enforce minimum complexity requirements
- Never log or display passwords
- Always use HTTPS in production
- Regularly prompt users to update passwords

### Token Security

- Don't store tokens in version control
- Use environment variables for application secrets
- Use different tokens for different services/users
- Set appropriate expiry times
- Regularly audit and remove unused tokens

### Session Management

- Store session tokens securely (not in localStorage for web apps)
- Re-authenticate before tokens expire
- Implement proper logout with token deletion
- Monitor for suspicious activity

## Error Responses

| Error Type | JSON Response | HTTP Status |
|------------|---------------|-------------|
| Missing Token | `{ "error": "Unauthorized" }` | 401 Unauthorized |
| Invalid Token | `{ "error": "Unauthorized" }` | 401 Unauthorized |
| Expired Token | `{ "error": "Unauthorized" }` | 401 Unauthorized |
| Rate Limited | `{ "error": "Too many requests" }` | 429 Too Many Requests |

For security reasons, specific error details are not exposed.

## Health Endpoint

The `/health` endpoint is always accessible without authentication:

```bash
curl http://localhost:8080/health
```
