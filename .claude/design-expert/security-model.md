# Security Model: pgEdge AI DBA Workbench

This document describes the security design, threat model, and secure coding
practices for the pgEdge AI DBA Workbench.

## Security Goals

1. **Isolation**: Maintain strict isolation between user sessions and data
2. **Least Privilege**: Users and tokens have minimum necessary access
3. **Defense in Depth**: Multiple layers of security controls
4. **Injection Prevention**: Prevent SQL injection and other injection attacks
5. **Audit Trail**: Maintain clear record of who accessed what
6. **Credential Protection**: Secure storage and transmission of sensitive data

## Threat Model

### Threat Actors

1. **External Attackers**: Unauthorized access to the system
2. **Malicious Users**: Authenticated users attempting privilege escalation
3. **Compromised Tokens**: Stolen or leaked authentication tokens
4. **Insider Threats**: Authorized users accessing unauthorized data
5. **Supply Chain**: Compromised dependencies or build tools

### Assets to Protect

1. **Database Credentials**: Connection strings with passwords
2. **User Passwords**: Hashed user account passwords
3. **Authentication Tokens**: Session and API tokens
4. **Monitoring Data**: Historical metrics and configuration snapshots
5. **Server Secret**: Installation-specific encryption key

### Trust Boundaries

1. **Network Boundary**: HTTP/HTTPS endpoints
2. **Authentication Boundary**: Token validation
3. **Authorization Boundary**: Privilege checking
4. **Component Boundary**: Collector <-> Datastore <-> Server
5. **User Boundary**: Isolation between users

## Authentication Architecture

### Token Types and Lifecycle

#### Session Tokens (user_sessions)

**Purpose**: Interactive user sessions through web client or CLI.

**Lifecycle**:
1. User calls authenticate_user tool with username/password
2. Server validates credentials against user_accounts table
3. Server generates cryptographically random token
4. Token stored in user_sessions with 24-hour expiry
5. Token returned to client for use in Authorization header
6. Token validated on each subsequent request
7. Token deleted on explicit logout or automatic 24-hour expiry

**Security Properties**:
- 24-hour maximum lifetime (automatic expiry)
- Explicit logout invalidates token immediately
- Multiple concurrent sessions per user allowed
- Token hash stored (not plaintext)

#### User Tokens (user_tokens)

**Purpose**: Long-lived API credentials owned by user accounts.

**Lifecycle**:
1. Authenticated user calls create_user_token tool
2. Server generates cryptographically random token
3. Token stored in user_tokens with optional expiry
4. Plaintext token returned ONCE to user
5. Only hash stored in database
6. User includes token in Authorization header for API calls
7. Token validated against hash on each request

**Security Properties**:
- Optional expiry (can be indefinite)
- User owns multiple tokens for different purposes
- Can be scoped to subset of owner's privileges
- Revocable without affecting other tokens
- Plaintext token shown only at creation

#### Service Tokens (service_tokens)

**Purpose**: Standalone tokens for automation, not tied to user accounts.

**Lifecycle**:
1. Superuser uses CLI to create service token
2. CLI generates cryptographically random token
3. Token stored in service_tokens with optional expiry
4. Plaintext token output to CLI
5. Only hash stored in database
6. Service includes token in Authorization header
7. Token validated against hash on each request

**Security Properties**:
- Not tied to user account lifecycle
- Has own is_superuser flag
- Can be scoped to specific connections and MCP items
- Only superusers can create via CLI
- Optional expiry

### Token Format

**Format**: Cryptographically random, high-entropy strings.

**Properties**:
- Sufficient length to prevent brute force (128+ bits entropy)
- URL-safe characters
- Generated using crypto/rand (not math/rand)
- No embedded metadata (prevents information leakage)

**Storage**: SHA-256 hash stored in database, never plaintext.

### Password Security

**User Passwords**:
- SHA-256 hashed before storage
- Optional expiry timestamp (password_expiry)
- Constraint: password_hash must not be empty
- Transmitted only during authentication (over HTTPS in production)

**Monitored Connection Passwords**:
- Encrypted using server_secret + owner username/token name
- Stored in connections.pg_password_encrypted
- Decrypted only when establishing monitored connection
- Never transmitted in responses

**Server Secret**:
- Per-installation secret in configuration file
- Used as encryption key material
- NEVER stored in database
- Required configuration value (no default)

## Authorization Architecture

### Access Control Model

**Model**: Role-Based Access Control (RBAC) with hierarchical groups.

**Key Principles**:
1. Privileges granted to groups, not individual users
2. Users gain privileges through group membership
3. Groups can contain users and other groups (nesting)
4. Transitive membership resolved via recursive CTEs
5. Superusers bypass all privilege checks

### Privilege Types

#### Connection Privileges

**Scope**: Access to monitored database connections.

**Access Levels**:
- `read`: Query database, read metrics, view configuration
- `read_write`: Read access + execute write operations

**Grant Model**:
- Privileges granted to groups via connection_privileges table
- User must be in granted group (direct or transitive)
- Check function: privileges.CanAccessConnection()

**Special Cases**:
- Private connections: Only owner or superusers
- Shared connections with no groups: DENIED (fail-safe)
- Shared connections with groups: Granted groups only
- Superusers: Always granted

#### MCP Item Privileges

**Scope**: Access to MCP tools, resources, and prompts.

**Identifier System**:
- Each tool/resource/prompt has unique string identifier
- Identifiers registered in mcp_privilege_identifiers table
- Server startup seeds all known identifiers
- Groups granted access via group_mcp_privileges table

**Check Function**: privileges.CanAccessMCPItem()

**Special Cases**:
- Unregistered identifier: DENIED (fail-safe)
- No groups granted identifier: DENIED (fail-safe)
- User in granted group: ALLOWED
- Superusers: Always granted

### Token Scoping

**Purpose**: Restrict token to subset of owner's privileges.

**Mechanism**:
- token_connection_scope: Limit connections token can access
- token_mcp_scope: Limit MCP items token can access

**Semantics**:
- Empty scope table (no rows): Token has full owner access
- Rows in scope table: Token restricted to listed items
- Scope cannot grant more than owner has

**Use Cases**:
- Read-only automation token (scoped to specific connections)
- Monitoring-only token (scoped to metric resources)
- Single-purpose tokens (minimal privilege)

### Privilege Resolution Algorithm

For connection access:

```
1. If user.is_superuser -> ALLOW
2. If connection.is_shared = false:
   a. If connection.owner_username = user.username -> ALLOW
   b. Else -> DENY
3. If connection.is_shared = true:
   a. If no groups have connection privilege -> DENY (fail-safe)
   b. user_groups = GetUserGroups(user_id) (recursive)
   c. If any user_group has privilege with sufficient access_level -> ALLOW
   d. Else -> DENY
```

For MCP item access:

```
1. If user.is_superuser -> ALLOW
2. If identifier not in mcp_privilege_identifiers -> DENY (fail-safe)
3. If no groups granted identifier -> DENY (fail-safe)
4. user_groups = GetUserGroups(user_id) (recursive)
5. If any user_group granted identifier -> ALLOW
6. Else -> DENY
```

For token scoping:

```
1. Check if user/owner has access (algorithm above)
2. If token has scope rows:
   a. If item not in token scope -> DENY
3. Return user/owner access result
```

## Secure Coding Practices

### SQL Injection Prevention

**Rule**: ALWAYS use parameterized queries.

**Good**:
```go
_, err := conn.Exec(ctx, "SELECT * FROM users WHERE id = $1", userID)
```

**Bad**:
```go
// NEVER DO THIS
query := fmt.Sprintf("SELECT * FROM users WHERE id = %d", userID)
_, err := conn.Exec(ctx, query)
```

**Exception**: MCP tool for arbitrary SQL execution (by design).

**Table/Column Names**:
- Never from user input
- From probe definitions or hard-coded constants
- Use #nosec G201 comment when necessary for linter

### Input Validation

**Rule**: Validate all inputs at the API boundary.

**Validations**:
- Non-empty strings where required (CHECK constraints)
- Email format validation
- Username character restrictions
- Token format validation
- SQL injection patterns (defense in depth)

**Example**:
```sql
CONSTRAINT chk_username_not_empty CHECK (username <> '')
CONSTRAINT chk_email_not_empty CHECK (email <> '')
```

### Authentication Enforcement

**Rule**: Every MCP method except initialize, ping, and authenticate_user
MUST validate bearer token.

**Implementation** (mcp/handler.go):
```go
requiresAuth := true
switch req.Method {
case "initialize", "ping":
    requiresAuth = false
case "tools/call":
    // Check if it's the authenticate_user tool
    if toolName == "authenticate_user" {
        requiresAuth = false
    }
}

if requiresAuth {
    userInfo, err := h.validateToken(bearerToken)
    if err != nil || userInfo == nil {
        return NewErrorResponse(req.ID, InvalidRequest,
            "Authentication required", nil), nil
    }
}
```

**Critical**: Default to requiring authentication.

### Authorization Enforcement

**Rule**: Check privileges before executing any privileged operation.

**Pattern**:
```go
// Check connection access
canAccess, err := privileges.CanAccessConnection(
    ctx, pool, userID, connectionID, privileges.AccessLevelRead)
if err != nil {
    return fmt.Errorf("privilege check failed: %w", err)
}
if !canAccess {
    return fmt.Errorf("access denied")
}

// Check MCP item access
canAccess, err = privileges.CanAccessMCPItem(
    ctx, pool, userID, "tool:query_database")
if err != nil {
    return fmt.Errorf("privilege check failed: %w", err)
}
if !canAccess {
    return fmt.Errorf("access denied")
}
```

**Critical**: Check privileges in the handler, not just at API boundary.

### Password Handling

**Rules**:
1. Hash passwords before storage (SHA-256)
2. Never log passwords
3. Never include passwords in error messages
4. Clear password variables after use
5. Transmit only over HTTPS in production

**Example**:
```go
// Hash password
hash := sha256.Sum256([]byte(password))
passwordHash := hex.EncodeToString(hash[:])

// Clear plaintext password
password = ""

// Store only hash
_, err := conn.Exec(ctx,
    "INSERT INTO user_accounts (username, password_hash) VALUES ($1, $2)",
    username, passwordHash)
```

### Encryption Key Management

**Server Secret**:
- Stored only in configuration file
- Loaded at startup
- Held in memory during runtime
- Combined with username/token name for per-credential encryption
- Never logged or transmitted

**Connection Password Encryption**:
```go
// Encrypt
encryptedPassword := database.EncryptPassword(
    password, config.ServerSecret, ownerUsername)

// Decrypt
password, err := database.DecryptPassword(
    encryptedPassword, config.ServerSecret, ownerUsername)
```

**Key Rotation**: Not currently supported (future enhancement).

### Error Message Security

**Rule**: Don't leak sensitive information in error messages.

**Good**:
```go
return fmt.Errorf("authentication failed")
```

**Bad**:
```go
// NEVER DO THIS - leaks username validity
return fmt.Errorf("password incorrect for user %s", username)
```

**Database Errors**: Wrap database errors without exposing schema details.

### Rate Limiting

**Status**: NOT IMPLEMENTED (future enhancement).

**Recommendation**:
- Limit authentication attempts per IP/username
- Token bucket for API requests
- Exponential backoff for failed logins

### Session Management

**Session Tokens**:
- 24-hour maximum lifetime (hard limit)
- Stored with expiry timestamp
- Automatic cleanup of expired sessions
- Explicit logout support

**Token Cleanup**:
- Expired user_sessions regularly purged
- Expired service_tokens marked invalid
- Expired user_tokens marked invalid

### Audit Logging

**What to Log**:
- Authentication attempts (success and failure)
- Authorization failures
- Token creation/revocation
- Group membership changes
- Privilege grants
- Connection creation/modification
- Configuration changes

**What NOT to Log**:
- Passwords (plaintext or hashed)
- Authentication tokens
- Database passwords
- Server secret

**Log Format**:
```
level=INFO msg="User authenticated" username="alice" session_id=123
level=WARN msg="Access denied" username="bob" resource="connection:5"
level=ERROR msg="Authentication failed" username="charlie"
```

## Network Security

### TLS/SSL Configuration

**Production Requirement**: HTTPS with valid TLS certificates.

**Configuration**:
```
tls = true
tls_cert = /path/to/cert.pem
tls_key = /path/to/key.pem
tls_chain = /path/to/chain.pem
```

**Certificate Requirements**:
- Valid certificate from trusted CA
- Covers server hostname
- Reasonable expiry (not too long)
- Strong key length (2048+ bits RSA or ECC)

**HTTP Mode**: Only for development and testing (never production).

### PostgreSQL Connections

**Datastore Connection**:
- Support all PostgreSQL SSL modes
- Configurable: pg_sslmode, pg_sslcert, pg_sslkey, pg_sslrootcert
- Prefer encrypted connections (sslmode=prefer or require)

**Monitored Connections**:
- Same SSL options as datastore
- Credentials encrypted in database
- Connections established on-demand
- Connection pooling for efficiency

### Bearer Token Transmission

**Requirement**: Always in Authorization header.

**Format**:
```
Authorization: Bearer <token>
```

**Protection**:
- HTTPS in production (encrypted transmission)
- Never in URL query parameters
- Never in logs
- Never in error messages

## Database Security

### Connection Ownership

**Rule**: Users can only access connections they own or are explicitly
granted access to.

**Enforcement**:
1. Private connections: owner_username must match user
2. Shared connections: user must be in granted group
3. Superusers: bypass ownership checks

### Data Isolation

**Rule**: Users cannot access other users' private data.

**Isolation Points**:
1. Connections: owner_username check
2. Metrics: connection-level access control
3. Sessions: user_id foreign key
4. Tokens: user_id foreign key

**PostgreSQL Row-Level Security**: NOT USED (application-level enforcement).

### Schema Permissions

**Datastore Users**:
- Collector: Read/write to metrics schema
- Server: Read from metrics, read/write to config tables
- Users: No direct database access (only through MCP server)

**Principle**: Application users have no direct PostgreSQL credentials.

### Injection Prevention

**SQL Injection**:
- Parameterized queries (ALWAYS)
- Input validation (defense in depth)
- Least privilege (minimize damage from successful injection)

**Command Injection**:
- No shell command execution from user input
- Validate all file paths
- No eval() or equivalent

**Path Traversal**:
- Validate file paths for TLS certificates
- No user-controlled file paths

## Security Testing

### Security Test Coverage

**Required Tests**:
1. Authentication bypass attempts
2. Authorization bypass attempts
3. SQL injection attempts
4. Token theft/replay scenarios
5. Privilege escalation attempts
6. Cross-user data access attempts

**Example** (from integration tests):
```go
// Test non-superuser cannot create shared connection
response := callTool(t, userToken, "create_connection", params)
assertError(t, response, "superuser required")

// Test user cannot access other user's connection
response = callTool(t, aliceToken, "query_database", bobConnectionParams)
assertError(t, response, "access denied")
```

### Penetration Testing

**Recommendation**: Periodic penetration testing by security professionals.

**Focus Areas**:
- Authentication/authorization bypasses
- Injection vulnerabilities
- Privilege escalation
- Data exfiltration
- Token theft

## Security Monitoring

### Indicators of Compromise

**Monitor For**:
- Repeated authentication failures
- Authorization failures for superuser operations
- Token usage from unexpected IPs
- Unusual query patterns
- Excessive data retrieval

**Alerting**: NOT IMPLEMENTED (future enhancement).

### Logging Requirements

**Security-Relevant Events**:
- All authentication attempts
- All authorization failures
- All privilege changes
- All token creation/revocation
- All connection creation/modification

**Log Retention**: Follow organizational security policy.

## Vulnerability Management

### Dependency Management

**Process**:
1. Track all dependencies (go.mod, package.json)
2. Regular dependency updates
3. Security advisory monitoring
4. Automated dependency scanning (Dependabot)

**Go Modules**:
```bash
go list -m all  # List dependencies
go mod tidy     # Clean unused dependencies
```

### Security Updates

**Process**:
1. Monitor security advisories for Go, PostgreSQL, dependencies
2. Assess impact and urgency
3. Test security updates in staging
4. Deploy to production with change control

**Critical Updates**: Expedited process for actively exploited vulnerabilities.

### Disclosure Policy

**Reporting**: Security vulnerabilities should be reported to pgEdge security
team (process TBD).

**Handling**:
1. Acknowledge receipt
2. Assess severity and impact
3. Develop and test fix
4. Coordinate disclosure timing
5. Release fix and advisory

## Compliance Considerations

### Audit Trail

**Requirements**:
- Who accessed what connection
- When privileges were granted/revoked
- What configuration changes were made
- Who created what tokens

**Implementation**:
- created_at/updated_at timestamps on all tables
- Foreign keys to user_accounts preserve identity
- schema_version tracks schema changes
- Application logging captures operations

### Data Retention

**Configurable Per Probe**:
- retention_days in probe_configs table
- Automatic partition dropping based on retention
- Exception: Change-tracked probes preserve latest data

**Compliance**: Follow organizational data retention policies.

### Encryption

**At Rest**:
- Connection passwords encrypted in database
- PostgreSQL can use tablespace encryption (TDE)
- Disk encryption (LUKS, BitLocker) recommended

**In Transit**:
- HTTPS for MCP server (production)
- PostgreSQL SSL for all connections (recommended)

**Key Management**: Server secret in configuration file (improve in future).

## Security Checklist

Before deployment:

- [ ] All passwords stored as hashes
- [ ] Server secret configured and unique
- [ ] HTTPS enabled with valid certificates
- [ ] PostgreSQL SSL mode configured
- [ ] Superuser accounts documented and minimal
- [ ] Default passwords changed
- [ ] Service tokens documented and audited
- [ ] Security logging enabled
- [ ] Dependency versions current
- [ ] Security tests passing
- [ ] Penetration test completed (if required)

During operation:

- [ ] Monitor authentication failures
- [ ] Monitor authorization failures
- [ ] Review audit logs regularly
- [ ] Update dependencies regularly
- [ ] Rotate tokens periodically
- [ ] Review superuser grants quarterly
- [ ] Test backup/restore procedures
- [ ] Incident response plan documented

## Known Limitations

1. **No Rate Limiting**: Vulnerable to brute force attacks
2. **No Key Rotation**: Server secret cannot be rotated
3. **No MFA**: Only password authentication
4. **No IP Whitelisting**: No network-level access control
5. **Application-Level Security**: No PostgreSQL RLS

Future enhancements should address these limitations.

---

**Version**: 1.0
**Last Updated**: 2025-11-08
**Status**: Living Document
