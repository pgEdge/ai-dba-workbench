# Authentication Guide

The MCP server includes built-in authentication with two methods:
API tokens for programmatic access and user accounts for interactive
authentication.

## Overview

- API tokens provide long-lived credentials for programmatic access
  via direct HTTP/HTTPS connections.
- User accounts provide interactive authentication with session
  tokens.
- Service accounts enable non-interactive, automated use.
- Authentication is required in HTTP/HTTPS mode.
- SHA256 and Bcrypt hashing provides secure credential storage.
- Token expiration with automatic cleanup manages token lifecycle.
- Per-token connection isolation ensures multi-user security.
- Bearer token authentication uses the HTTP Authorization header.
- Per-IP rate limiting protects against brute force attacks.
- Automatic account lockout disables accounts after failed
  attempts.

## Authentication Storage

Authentication data is stored in a SQLite database (`auth.db`)
within the data directory. By default, the database resides at
`./data/auth.db` relative to the server binary.

The auth store contains the following components:

- The users table stores usernames, bcrypt password hashes,
  service account flags, and superuser status.
- The tokens table stores API token hashes, expiry dates, and
  owner references for all token types.
- The groups table stores named groups for organizing users
  and assigning permissions.
- The group members table tracks user and nested group
  memberships within each group.
- The token scope tables restrict tokens to specific
  connections, MCP privileges, and admin permissions.
- Session tokens use in-memory storage for 24-hour session
  validity.

## User Account Management

User accounts provide interactive authentication with session-based
access. Users authenticate with a username and password to receive
a 24-hour session token.

### Adding Users

In the following example, the `-add-user` flag starts interactive
mode:

```bash
./bin/ai-dba-server -add-user
```

You will be prompted for the following information:

- Username (required).
- Password (hidden input, with confirmation).
- Annotation or note (optional).

In the following example, the command creates a user in
non-interactive mode:

```bash
./bin/ai-dba-server -add-user \
  -username alice \
  -password "SecurePassword123!" \
  -user-note "Alice Smith - Developer"
```

### Listing Users

In the following example, the `-list-users` flag displays all user
accounts:

```bash
./bin/ai-dba-server -list-users
```

Output:

```
Users:
==========================================================================================
Username             Created                   Last Login           Status     Notes
------------------------------------------------------------------------------------------
alice                2024-10-30 10:15          2024-11-14 09:30     Enabled    Developer
bob                  2024-10-15 14:20          Never                Enabled    Admin
charlie              2024-09-01 08:00          2024-10-10 16:45     DISABLED   Former
==========================================================================================
```

### Updating Users

In the following examples, the `-update-user` flag modifies an
existing user account:

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

In the following examples, the `-disable-user` and `-enable-user`
flags control account access:

```bash
# Disable a user account (prevents login)
./bin/ai-dba-server -disable-user -username charlie

# Re-enable a user account (also resets failed login attempts)
./bin/ai-dba-server -enable-user -username charlie
```

### Deleting Users

In the following example, the `-delete-user` flag removes a user
account:

```bash
# Delete user (with confirmation prompt)
./bin/ai-dba-server -delete-user -username charlie
```

## Service Account Management

Service accounts are non-interactive users that authenticate
exclusively via API tokens. A service account cannot log in with
a password.

### Creating Service Accounts

In the following example, the `-add-service-account` flag creates
a new service account:

```bash
./bin/ai-dba-server -add-service-account
```

You will be prompted for the following information:

- Username (required).
- Annotation or note (optional).

### Service Account Properties

Service accounts share the following characteristics:

- A service account authenticates only via API tokens.
- Service accounts can be members of groups and receive RBAC
  privileges just like regular users.
- Service accounts can hold superuser status.
- The server rejects password-based login for service accounts.

## API Token Management

API tokens provide programmatic access for both service accounts
and regular users. Each token belongs to a specific user and
inherits its authorization context from that user. Superuser
status is always determined by the owning user account.

### Adding Tokens

In the following example, the `-add-token` flag starts interactive
mode:

```bash
./bin/ai-dba-server -add-token
```

You will be prompted for the following information:

- Owner username (required).
- Annotation or note (optional).
- Expiry duration (e.g., "30d", "1y", "never").

In the following examples, the command creates tokens in
non-interactive mode:

```bash
# Create token for a user
./bin/ai-dba-server -add-token \
  -user alice \
  -token-note "CI/CD Pipeline" \
  -token-expiry "90d"

# Create token for a service account
./bin/ai-dba-server -add-token \
  -user svc-bot \
  -token-note "Automation" \
  -token-expiry "never"
```

The command displays the token upon creation:

```
===========================================================
Token created successfully!
===========================================================

Token: O9ms9jqTfUdy-DIjvpFWeqd_yH_NEj7me0mgOnOjGdQ=
Hash:  b3f805a4c2e7d9f1...
ID:    1
Owner: alice
Note:  CI/CD Pipeline
Expires: 2025-01-28T10:15:30-05:00
===========================================================

IMPORTANT: Save this token securely - it will not be shown
again! Use it in API requests with:
Authorization: Bearer <token>
===========================================================
```

### Token Expiry Formats

The following expiry formats are supported:

- `30d` specifies 30 days.
- `1y` specifies 1 year.
- `2w` specifies 2 weeks.
- `12h` specifies 12 hours.
- `1m` specifies 1 month (30 days).
- `never` specifies that the token never expires.

### Listing Tokens

In the following example, the `-list-tokens` flag displays all
API tokens:

```bash
./bin/ai-dba-server -list-tokens
```

Output:

```
Tokens:
==================================================================================================================================
ID     Hash Prefix        Owner            Superuser  Service    Expires              Status     Notes
----------------------------------------------------------------------------------------------------------------------------------
1      b3f805a4c2e7d9f1   alice            No         No         2025-01-28 10:15     Active     CI/CD Pipeline
2      7a2f19d8e1c4b5a3   svc-bot          No         Yes        Never                Active     Automation
3      9c8d7e6f5a4b3c2d   bob              No         No         2024-10-15 14:20     EXPIRED    Temp access
==================================================================================================================================
```

### Removing Tokens

In the following examples, the `-remove-token` flag deletes an
API token by its ID or hash prefix:

```bash
# Remove by token ID
./bin/ai-dba-server -remove-token 1

# Remove by hash prefix (minimum 8 characters)
./bin/ai-dba-server -remove-token b3f805a4
```

## Role-Based Access Control (RBAC)

The server enforces access control through groups, privileges,
and token scopes. Administrators manage these settings through
the admin panel or the REST API.

### Groups

Groups organize users and assign shared permissions. Each
group can contain users, service accounts, or other groups.
Nested groups inherit the parent group's privileges.

Administrators assign two types of privileges to groups:

- Connection privileges grant access to monitored database
  connections with a specified access level.
- MCP privileges grant access to specific MCP tools such as
  `query_database` or `get_schema_info`.

### Admin Permissions

Admin permissions control access to management operations
in the admin panel and REST API. The system defines the
following eight admin permissions:

- `manage_connections` allows creating, editing, and
  deleting monitored database connections.
- `manage_groups` allows creating, editing, and deleting
  RBAC groups and their memberships.
- `manage_permissions` allows granting and revoking
  privileges on groups.
- `manage_users` allows creating, editing, and deleting
  user accounts and service accounts.
- `manage_token_scopes` allows viewing and modifying token
  scope restrictions.
- `manage_blackouts` allows creating, editing, and
  deleting maintenance blackout windows.
- `manage_probes` allows configuring probe frequency,
  retention, and enabled state.
- `manage_alert_rules` allows configuring alert rule
  defaults and per-connection overrides.

Superusers bypass all permission checks and have full
access to every operation.

### Token Scopes

Token scopes restrict a token to a subset of the owning
user's permissions. A token without scope restrictions
inherits the full access of its owner. The system supports
three scope types:

- Connection scopes limit the token to specific database
  connections with a per-connection access level of `read`
  or `read_write`.
- MCP privilege scopes limit the token to specific MCP
  tools.
- Admin permission scopes limit the token to specific
  admin operations.

The effective access for a scoped token equals the
intersection of the owner's group-level access and the
token scope. A token cannot exceed the access level of
its owner.

### Wildcard Scopes

Each scope type supports a wildcard option that grants
access to all items of that type:

- "All Connections" uses `connection_id=0` to match every
  connection the owner can access.
- "All MCP Privileges" uses `id=0` to match every MCP
  privilege the owner holds.
- "All Admin Permissions" uses `*` to match every admin
  permission the owner holds.

Wildcard scopes simplify token creation when broad access
is acceptable.

### Connection Access Levels

Connection privileges and token scopes support two access
levels:

- `read` allows read-only queries against the connection.
- `read_write` allows both read and write operations.

A token scope can further restrict access to `read` even
when the owning user has `read_write` access through
group membership. The system always enforces the more
restrictive level.

### Admin Panel

The admin panel provides a graphical interface for managing
RBAC settings. Access the admin panel by clicking the
Settings gear icon in the web client. The panel contains
three tabs:

- The Users tab lists all users with expandable rows that
  show effective permissions including group memberships,
  connection privileges, MCP privileges, and admin
  permissions.
- The Groups tab lists all groups with expandable rows that
  show members, connection privileges, MCP privileges, and
  admin permissions.
- The Tokens tab lists all tokens with expandable rows that
  show the token scope including connection access levels,
  MCP privileges, and admin permissions.

The Tokens tab also displays API usage examples with sample
`curl` commands for common operations.

## Authentication Flow

### For Interactive Applications (User Authentication)

1. Authenticate with a username and password using the login API:

   ```bash
   curl -X POST http://localhost:8080/api/v1/auth/login \
     -H "Content-Type: application/json" \
     -d '{
       "username": "alice",
       "password": "SecurePassword123!"
     }'
   ```

2. Receive the session token in the response:

   ```json
   {
     "success": true,
     "session_token": "AQz9XfK...",
     "expires_at": "2024-11-15T09:30:00Z",
     "message": "Authentication successful"
   }
   ```

3. Use the session token for subsequent requests:

   ```bash
   curl -X POST http://localhost:8080/mcp/v1 \
     -H "Authorization: Bearer AQz9XfK..." \
     -H "Content-Type: application/json" \
     -d '{"jsonrpc": "2.0", "id": 2, "method": "tools/list"}'
   ```

### For Machine-to-Machine (API Tokens)

Create API tokens for service accounts or regular users via
the server command line. Use the token directly in all
requests as a Bearer token.

In the following example, an API token authenticates a request:

```bash
curl -X POST http://localhost:8080/mcp/v1 \
  -H "Authorization: Bearer O9ms9jqTfUdy-DIjvpFWeqd_yH_NEj7me0mgOnOjGdQ=" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}'
```

## Rate Limiting and Account Lockout

### Rate Limiting

The server tracks failed authentication attempts per IP address.

- The default allows 10 failed attempts per 15-minute window.
- Rate limiting applies to all authentication endpoints.
- The server performs automatic cleanup of old attempt records.

### Account Lockout

When a valid username is provided, the server tracks failed
login attempts.

- The default is disabled (0 means no lockout).
- The `max_failed_attempts_before_lockout` setting controls
  the threshold.
- An administrator must re-enable locked accounts.

### Configuration

In the following example, the configuration file sets
authentication options:

```yaml
http:
  auth:
    enabled: true
    # Rate limiting settings
    rate_limit_window_minutes: 15
    rate_limit_max_attempts: 10
    # Account lockout settings
    max_failed_attempts_before_lockout: 5  # 0 = disabled
    # API token restrictions
    # Superuser-owned tokens are exempt from this limit
    max_user_token_days: 90  # 0 = unlimited
```

### Recovering Locked Accounts

In the following example, the `-enable-user` flag re-enables a
locked account:

```bash
# Re-enable a locked account (also resets failed attempts)
./bin/ai-dba-server -enable-user -username alice
```

## Security Best Practices

### Password Security

- Enforce minimum complexity requirements for passwords.
- Never log or display passwords in output.
- Always use HTTPS in production environments.
- Regularly prompt users to update their passwords.

### Token Security

- Do not store API tokens in version control.
- Use environment variables for application secrets.
- Assign different tokens to different services and users.
- Set appropriate expiry times for each token.
- Regularly audit and remove unused tokens.
- Use service accounts for automated workflows instead of
  personal user tokens.

### Session Management

- Store session tokens securely in the application.
- Re-authenticate before session tokens expire.
- Implement proper logout with token deletion.
- Monitor the server logs for suspicious activity.

## Error Responses

The server returns standard HTTP error codes for authentication
failures.

| Error Type | JSON Response | HTTP Status |
|------------|---------------|-------------|
| Missing Token | `{ "error": "Unauthorized" }` | 401 |
| Invalid Token | `{ "error": "Unauthorized" }` | 401 |
| Expired Token | `{ "error": "Unauthorized" }` | 401 |
| Rate Limited | `{ "error": "Too many requests" }` | 429 |

The server does not expose specific error details for security
reasons.

## Health Endpoint

The `/health` endpoint is always accessible without
authentication.

In the following example, the `curl` command checks server
health:

```bash
curl http://localhost:8080/health
```
