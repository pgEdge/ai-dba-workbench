# Credential Handling

This document describes how credentials and secrets are managed in the AI DBA
Workbench.

## Password Storage

### User Passwords

**Location:** `/server/src/auth/` and `/server/src/database/users.go`

**Approach:**

- Passwords hashed before storage
- Hash algorithm: bcrypt or SHA256 (verify current implementation)
- Salt: Included in bcrypt; separate for SHA256
- Never stored in plaintext

**Verification flow:**

```
User submits password
        │
        ▼
Hash submitted password
        │
        ▼
Compare with stored hash
(constant-time comparison)
        │
        ▼
Return success/failure
(no timing difference)
```

**Security requirements:**

- [ ] Use bcrypt with cost factor >= 10
- [ ] Or use SHA256 with unique salt per password
- [ ] Comparison must be constant-time
- [ ] Failed attempts must not reveal valid usernames

## Token Management

### Service Tokens

**Location:** Database table `service_tokens`

**Properties:**

- Long-lived tokens for automated access
- Generated with crypto/rand
- Stored as SHA256 hash (not plaintext)
- Associated with specific privileges

**Generation:**

```go
// Expected pattern
token := make([]byte, 32)
_, err := crypto_rand.Read(token)
// Store SHA256(token), return hex(token) to user
```

### User Tokens

**Location:** Database table `user_tokens`

**Properties:**

- May be time-limited
- Scoped to specific connections/MCP items
- Stored as SHA256 hash

**Scoping tables:**

- `user_token_connection_scope` - Limits connection access
- `user_token_mcp_scope` - Limits MCP tool/resource access

### Session Tokens

**Location:** `/server/src/auth/sessions.go`

**Properties:**

- Short-lived (session duration)
- Invalidated on logout
- Associated with user account

## Database Connection Credentials

### Storage

**Location:** Database table `connections`, `/collector/src/database/`

**Approach:**

- Connection passwords encrypted at rest
- Encryption key from configuration/environment
- Decrypted only when connection is established

**Flow:**

```
Password provided
        │
        ▼
Encrypt with AES/similar
        │
        ▼
Store encrypted blob
        │
        ▼
(On connection)
        │
        ▼
Decrypt with key
        │
        ▼
Use for PostgreSQL auth
        │
        ▼
Clear from memory
```

### Connection Strings

**Security requirements:**

- [ ] Never log full connection strings
- [ ] Sanitize connection errors (remove password)
- [ ] Clear credentials from memory after use
- [ ] Encryption key not in source code

## Secret Configuration

### Environment Variables

**Preferred method for secrets:**

```bash
# Server configuration
DATABASE_URL=postgres://...
ENCRYPTION_KEY=...
JWT_SECRET=...

# Never in code or config files
```

### Configuration Files

**If secrets must be in files:**

- [ ] File permissions restricted (600 or 400)
- [ ] Not committed to version control
- [ ] Encrypted if possible
- [ ] Loaded once at startup

## Logging Safety

### What Must NOT Be Logged

```go
// NEVER log these:
log.Printf("Password: %s", password)
log.Printf("Token: %s", token)
log.Printf("Connection: %s", connectionString)  // Contains password
log.Printf("Request body: %v", request)  // May contain credentials
```

### Safe Logging Patterns

```go
// Log sanitized versions:
log.Printf("Auth attempt for user: %s", username)
log.Printf("Connection to host: %s", hostname)  // No password
log.Printf("Token prefix: %s...", token[:8])  // Partial only for debugging
```

### Connection String Sanitization

```go
// Before logging connection errors:
func sanitizeConnString(conn string) string {
    // Remove password portion
    // postgres://user:PASSWORD@host/db -> postgres://user:***@host/db
}
```

## Error Message Safety

### External Error Messages

```go
// BAD - Leaks information
return fmt.Errorf("user '%s' not found", username)
return fmt.Errorf("invalid password for user '%s'", username)
return fmt.Errorf("connection to %s failed: %v", connStr, err)

// GOOD - Generic messages
return errors.New("invalid credentials")
return errors.New("authentication failed")
return errors.New("connection failed")
```

### Internal vs External

```go
// Log detailed error internally
log.Error("auth failed", "user", username, "reason", err)

// Return generic error to user
return errors.New("authentication failed")
```

## Key Rotation

### Token Rotation

- User tokens: User can regenerate
- Service tokens: Admin can regenerate
- Old tokens invalidated immediately

### Encryption Key Rotation

- Requires re-encryption of all stored credentials
- Process should be documented
- Zero-downtime rotation preferred

## Audit Requirements

### What to Audit

- [ ] Token creation/deletion
- [ ] Failed authentication attempts
- [ ] Privilege changes
- [ ] Connection credential changes
- [ ] Admin operations

### Audit Log Location

- Database audit tables (if implemented)
- Application logs (sanitized)

## Security Testing

### Credential Tests

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
