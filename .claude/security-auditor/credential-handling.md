# Credential Handling

This document describes how credentials and secrets are
managed in the AI DBA Workbench.

## Password Storage

### User Passwords

The server stores user passwords in the `users` table.

The password storage implementation resides in
`/server/src/auth/` and `/server/src/database/users.go`.

The system uses the following approach:

- The server hashes passwords before storage.
- The hash algorithm is bcrypt or SHA256; verify the
  current implementation for the active algorithm.
- Bcrypt includes a salt; SHA256 uses a separate salt.
- The system never stores passwords in plaintext.

The verification flow follows these steps:

```
User submits password
        |
        v
Hash submitted password
        |
        v
Compare with stored hash
(constant-time comparison)
        |
        v
Return success/failure
(no timing difference)
```

The following security requirements apply:

- Use bcrypt with a cost factor of 10 or higher.
- Alternatively, use SHA256 with a unique salt per password.
- Comparison must use constant-time operations.
- Failed attempts must not reveal valid usernames.

## Token Management

### API Tokens

The server stores all API tokens in a unified `tokens`
table. Each token has an `owner_id` that references a user
in the `users` table; the `owner_id` column is `NOT NULL`.

The token management implementation resides in the database
layer.

The system supports the following token properties:

- All tokens are stored as SHA256 hashes, never in
  plaintext.
- Each token references an owning user or service account
  via `owner_id`.
- Tokens support optional expiration via `expires_at`.
- The `annotation` field describes the token's purpose.
- Superuser status comes from the owning user account,
  never from the token itself.

Service accounts are users with `is_service_account = TRUE`
and an empty `password_hash`. These accounts cannot log in
with a password; tokens are their only authentication
method.

The `CreateToken` function generates a new API token for
a specified owner.

In the following example, the function creates a token
with cryptographically secure random bytes:

```go
// Expected pattern
token := make([]byte, 32)
_, err := crypto_rand.Read(token)
// Store SHA256(token), return hex(token) to user
```

The following scope tables restrict token capabilities:

- The `token_connection_scope` table limits which database
  connections a token can access.
- The `token_mcp_scope` table limits which MCP tools and
  resources a token can use.

### Session Tokens

The server manages session tokens for interactive users.

The session token implementation resides in
`/server/src/auth/sessions.go`.

The system supports the following session properties:

- Session tokens are short-lived for the session duration.
- The server invalidates session tokens on logout.
- Each session token references a user account.

## Database Connection Credentials

### Storage

The server stores database connection credentials in the
`connections` table. The collector stores credentials in
`/collector/src/database/`.

The system uses the following approach:

- Connection passwords are encrypted at rest.
- The encryption key comes from configuration or the
  environment.
- The server decrypts passwords only when establishing
  a connection.

The encryption flow follows these steps:

```
Password provided
        |
        v
Encrypt with AES/similar
        |
        v
Store encrypted blob
        |
        v
(On connection)
        |
        v
Decrypt with key
        |
        v
Use for PostgreSQL auth
        |
        v
Clear from memory
```

### Connection Strings

The following security requirements apply to connection
strings:

- Never log full connection strings.
- Sanitize connection errors to remove passwords.
- Clear credentials from memory after use.
- Store encryption keys outside of source code.

## Secret Configuration

### Environment Variables

Environment variables are the preferred method for storing
secrets.

```bash
# Server configuration
DATABASE_URL=postgres://...
ENCRYPTION_KEY=...
JWT_SECRET=...

# Never in code or config files
```

### Configuration Files

The following requirements apply when secrets must reside
in configuration files:

- Restrict file permissions to 600 or 400.
- Do not commit secret files to version control.
- Encrypt configuration files when possible.
- Load secrets once at startup.

## Logging Safety

### What Must Not Be Logged

The following items must never appear in log output:

```go
// NEVER log these:
log.Printf("Password: %s", password)
log.Printf("Token: %s", token)
log.Printf("Connection: %s", connectionString)
log.Printf("Request body: %v", request)
```

### Safe Logging Patterns

The following patterns demonstrate safe logging practices:

```go
// Log sanitized versions:
log.Printf("Auth attempt for user: %s", username)
log.Printf("Connection to host: %s", hostname)
log.Printf("Token prefix: %s...", token[:8])
```

### Connection String Sanitization

The server sanitizes connection strings before logging.

```go
// Before logging connection errors:
func sanitizeConnString(conn string) string {
    // Remove password portion
    // postgres://user:PASSWORD@host/db
    //   -> postgres://user:***@host/db
}
```

## Error Message Safety

### External Error Messages

The following examples show safe and unsafe error messages:

```go
// BAD - Leaks information
return fmt.Errorf("user '%s' not found", username)
return fmt.Errorf(
    "invalid password for user '%s'", username,
)
return fmt.Errorf(
    "connection to %s failed: %v", connStr, err,
)

// GOOD - Generic messages
return errors.New("invalid credentials")
return errors.New("authentication failed")
return errors.New("connection failed")
```

### Internal vs External

The server separates internal logging from external error
messages.

```go
// Log detailed error internally
log.Error("auth failed", "user", username, "reason", err)

// Return generic error to user
return errors.New("authentication failed")
```

## Key Rotation

### Token Rotation

The unified token model supports the following rotation
capabilities:

- Token owners can regenerate their own API tokens.
- Superusers can regenerate tokens for any user or service
  account.
- The server invalidates old tokens immediately upon
  rotation.

### Encryption Key Rotation

The following requirements apply to encryption key
rotation:

- Key rotation requires re-encryption of all stored
  credentials.
- The rotation process should be documented.
- The system should support zero-downtime rotation.

## Audit Requirements

### What to Audit

The following events require audit logging:

- Token creation and deletion events.
- Failed authentication attempts.
- Privilege changes for users and groups.
- Connection credential changes.
- Administrative operations.

### Audit Log Location

The system stores audit logs in the following locations:

- Database audit tables store structured audit records.
- Application logs store sanitized event records.

## Security Testing

### Credential Tests

The following tests verify credential handling:

```go
// Test that passwords are hashed
func TestPasswordNotStoredPlaintext(t *testing.T)

// Test token generation randomness
func TestTokenRandomness(t *testing.T)

// Test credential not in logs
func TestNoCredentialsInLogs(t *testing.T)

// Test error message safety
func TestGenericErrorMessages(t *testing.T)
```
