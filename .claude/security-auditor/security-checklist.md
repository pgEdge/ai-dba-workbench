# Security Checklist

This document provides security review checklists for common development
scenarios.

## New MCP Tool Review

When reviewing a new MCP tool implementation:

### Input Validation

- [ ] All parameters have defined types
- [ ] String parameters have length limits
- [ ] Numeric parameters have range validation
- [ ] Optional parameters have safe defaults
- [ ] Unknown parameters are rejected

### Authorization

- [ ] Tool requires authentication
- [ ] User privileges are checked before execution
- [ ] Connection access is verified (if applicable)
- [ ] Superuser-only operations are properly restricted

### Database Operations

- [ ] All queries use parameterization (`$1`, `$2`, etc.)
- [ ] No string concatenation in SQL
- [ ] Query results filtered by authorization
- [ ] Transactions used where needed for consistency

### Error Handling

- [ ] Errors are logged with context (internally)
- [ ] Error messages to users are generic
- [ ] No sensitive data in error messages
- [ ] Failed operations don't leave inconsistent state

### Testing

- [ ] Unit tests for valid inputs
- [ ] Unit tests for invalid inputs
- [ ] Unit tests for authorization failures
- [ ] Integration tests with real database

## New API Endpoint Review

### Authentication

- [ ] Endpoint requires authentication (unless public)
- [ ] Token validation is performed
- [ ] Invalid tokens return 401
- [ ] Token type is appropriate for endpoint

### Input Handling

- [ ] Request body size is limited
- [ ] Content-Type is validated
- [ ] JSON parsing has depth/size limits
- [ ] All inputs are validated before use

### Response Security

- [ ] No sensitive data in responses (unless authorized)
- [ ] Error responses don't leak internal details
- [ ] Proper HTTP status codes used
- [ ] Security headers are set

### Rate Limiting

- [ ] Endpoint has appropriate rate limits
- [ ] Rate limit responses are informative
- [ ] Rate limiting cannot be bypassed

## Database Query Review

### SQL Injection Prevention

- [ ] Query uses parameterized statements
- [ ] No `fmt.Sprintf` or `+` for query building
- [ ] Table/column names are not from user input
- [ ] If dynamic identifiers needed, use allowlist

**Safe patterns:**

```go
// SAFE - parameterized
db.QueryRow(ctx, "SELECT * FROM users WHERE id = $1", id)

// SAFE - parameterized with multiple values
db.Query(ctx, "SELECT * FROM items WHERE status = $1 AND owner = $2", status, owner)
```

**Unsafe patterns:**

```go
// UNSAFE - string concatenation
db.QueryRow(ctx, "SELECT * FROM users WHERE id = " + id)

// UNSAFE - fmt.Sprintf
query := fmt.Sprintf("SELECT * FROM %s WHERE id = %d", table, id)
```

### Data Access

- [ ] Query only returns authorized data
- [ ] Sensitive columns excluded if not needed
- [ ] Results are filtered by user permissions
- [ ] Pagination limits prevent data dumps

### Performance/DoS

- [ ] Query has reasonable timeout
- [ ] Results are limited (LIMIT clause)
- [ ] Indexes exist for query patterns
- [ ] No unbounded operations

## Authentication Code Review

### Password Handling

- [ ] Passwords hashed with bcrypt (cost >= 10) or argon2
- [ ] Plain passwords never stored
- [ ] Plain passwords never logged
- [ ] Password comparison is constant-time

### Token Generation

- [ ] Tokens use crypto/rand (not math/rand)
- [ ] Token length is sufficient (>= 32 bytes)
- [ ] Tokens stored as hashes
- [ ] Token lookup is constant-time

### Session Management

- [ ] Sessions expire appropriately
- [ ] Session IDs regenerated on auth state change
- [ ] Logout invalidates session
- [ ] Concurrent session limits (if applicable)

### Error Messages

- [ ] "Invalid credentials" (not "invalid password" or "user not found")
- [ ] Timing is consistent for valid/invalid users
- [ ] No user enumeration via error messages
- [ ] Account lockout doesn't confirm user exists

## React Component Review

### XSS Prevention

- [ ] No `dangerouslySetInnerHTML` with user data
- [ ] User input is escaped in display
- [ ] URLs are validated before use in links
- [ ] Event handlers don't eval user input

### Sensitive Data

- [ ] Passwords not stored in state
- [ ] Tokens stored securely (httpOnly cookies preferred)
- [ ] Sensitive data cleared on logout
- [ ] No sensitive data in URL parameters

### API Calls

- [ ] CSRF protection on mutations
- [ ] Auth tokens sent securely
- [ ] Error responses handled safely
- [ ] Loading states don't expose sensitive info

## Configuration Review

### Secrets Management

- [ ] No secrets in source code
- [ ] No secrets in config files (use env vars)
- [ ] Default values don't include real secrets
- [ ] Secret rotation is possible

### Environment Variables

- [ ] Sensitive env vars documented
- [ ] Missing required vars cause startup failure
- [ ] Vars not logged at startup
- [ ] Production/development configs separated

### Defaults

- [ ] No default credentials
- [ ] Secure defaults for all options
- [ ] Debug mode off by default
- [ ] Verbose logging off by default

## Logging Review

### What to Log

- [ ] Authentication attempts (success/failure)
- [ ] Authorization failures
- [ ] Configuration changes
- [ ] Error conditions

### What NOT to Log

- [ ] Passwords (plain or hashed)
- [ ] API tokens
- [ ] Session tokens
- [ ] Full connection strings
- [ ] Personal data (unless required)

### Log Safety

- [ ] Log injection not possible
- [ ] Log files have appropriate permissions
- [ ] Log rotation configured
- [ ] Sensitive data masked/redacted

## Dependency Review

### Go Dependencies

```bash
# Check for vulnerabilities
go list -m all | nancy sleuth
govulncheck ./...
```

- [ ] All dependencies pinned to versions
- [ ] No known vulnerabilities
- [ ] Dependencies are actively maintained
- [ ] Minimal dependency footprint

### Node Dependencies

```bash
# Check for vulnerabilities
npm audit
```

- [ ] package-lock.json committed
- [ ] No high/critical vulnerabilities
- [ ] Dev dependencies not in production
- [ ] Regular dependency updates

## Pre-Commit Checklist

Before committing security-related changes:

- [ ] All tests pass
- [ ] No new linter warnings
- [ ] No secrets in diff
- [ ] Error messages reviewed
- [ ] Logging reviewed
- [ ] Documentation updated
- [ ] Security-focused code review requested
