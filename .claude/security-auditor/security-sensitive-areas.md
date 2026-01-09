# Security-Sensitive Areas

This document identifies code locations that require careful security review.

## Critical Risk Areas

### Authentication (`/server/src/auth/`)

**Risk Level: CRITICAL**

Files requiring security focus:

| File | Security Concern |
|------|------------------|
| `tokens.go` | Token generation, validation, timing attacks |
| `sessions.go` | Session fixation, expiration, hijacking |
| `middleware.go` | Authentication bypass, header injection |
| `rbac.go` | Privilege escalation, authorization bypass |

**Review checklist:**

- [ ] Token generation uses crypto/rand
- [ ] Token comparison is constant-time
- [ ] Session expiration is enforced
- [ ] Failed auth attempts are rate-limited
- [ ] Error messages don't leak user existence

### SQL Execution (`/server/src/mcp/tools/`)

**Risk Level: CRITICAL**

Any tool that executes SQL queries:

| Tool | Security Concern |
|------|------------------|
| `query_execute` | SQL injection, data exfiltration |
| `query_explain` | Information disclosure |
| Connection tools | Credential handling |

**Review checklist:**

- [ ] All queries use parameterization
- [ ] User input is never concatenated into SQL
- [ ] Query results are filtered by authorization
- [ ] Sensitive data is not logged

### Database Operations (`/server/src/database/`)

**Risk Level: HIGH**

| File | Security Concern |
|------|------------------|
| `connections.go` | Credential storage, encryption |
| `users.go` | Password handling, user enumeration |
| `tokens.go` | Token storage, lookup timing |

**Review checklist:**

- [ ] Passwords are properly hashed
- [ ] Credentials encrypted at rest
- [ ] No plaintext credentials in logs
- [ ] Connection strings sanitized in errors

## High Risk Areas

### MCP Protocol Handler (`/server/src/mcp/`)

**Risk Level: HIGH**

| File | Security Concern |
|------|------------------|
| `handler.go` | Input validation, request parsing |
| `protocol.go` | Type confusion, parsing errors |
| `errors.go` | Information disclosure |

**Review checklist:**

- [ ] All input validated before use
- [ ] Error messages are generic externally
- [ ] Request size limits enforced
- [ ] Malformed requests handled safely

### Collector Database Access (`/collector/src/database/`)

**Risk Level: HIGH**

| File | Security Concern |
|------|------------------|
| `pool.go` | Connection credential handling |
| `schema.go` | Migration security |
| Probe queries | Query safety |

**Review checklist:**

- [ ] Monitored DB credentials protected
- [ ] Probe queries are read-only
- [ ] Connection errors don't leak credentials

## Medium Risk Areas

### Client Input Handling (`/client/src/`)

**Risk Level: MEDIUM**

| Location | Security Concern |
|----------|------------------|
| Form components | XSS, input validation |
| API services | CSRF, token handling |
| Query editor | SQL display (not injection - server-side) |

**Review checklist:**

- [ ] No dangerouslySetInnerHTML with user data
- [ ] API tokens stored securely
- [ ] CSRF protection on mutations
- [ ] Input sanitized before display

### Configuration (`/*/config/`)

**Risk Level: MEDIUM**

| Concern | Check |
|---------|-------|
| Secret loading | Environment variables preferred |
| Default values | No default credentials |
| Logging config | Sensitive data not logged |

## Code Patterns to Flag

### Always Flag

```go
// SQL string concatenation - ALWAYS VULNERABLE
query := "SELECT * FROM users WHERE id = " + userID
query := fmt.Sprintf("SELECT * FROM users WHERE id = %s", userID)

// Weak random for security purposes
import "math/rand"  // Should be crypto/rand for tokens

// Credential in code
password := "hardcoded_password"
apiKey := "sk-..."

// Logging sensitive data
log.Printf("User password: %s", password)
log.Printf("Connection string: %s", connStr)  // May contain password
```

### Review Carefully

```go
// Error messages with internal details
return fmt.Errorf("user %s not found in database %s", username, dbName)

// Non-constant-time comparison for secrets
if token == storedToken {  // Timing attack possible

// File operations with user input
os.Open(userProvidedPath)  // Path traversal risk

// Command execution
exec.Command(userInput)  // Command injection risk
```

### Acceptable Patterns

```go
// Parameterized queries - SAFE
row := db.QueryRow(ctx, "SELECT * FROM users WHERE id = $1", userID)

// Crypto random - SAFE
token := make([]byte, 32)
crypto_rand.Read(token)

// Constant-time comparison - SAFE
subtle.ConstantTimeCompare([]byte(token), []byte(storedToken))

// Password hashing - SAFE
bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
```

## Security Boundaries

### Trust Boundaries

```
UNTRUSTED                    TRUST BOUNDARY                    TRUSTED
─────────────────────────────────────────────────────────────────────────
User input                        │
API requests        ────────────▶ │ ────────────▶  Validated data
MCP protocol                      │                Server logic
                                  │                Database queries
                                  │
External DBs        ◀──────────── │ ◀────────────  Query results
                                  │                (filtered by auth)
```

### Data Classification

| Classification | Examples | Handling |
|----------------|----------|----------|
| **Secret** | Passwords, tokens, API keys | Encrypted, never logged |
| **Sensitive** | User emails, connection strings | Encrypted at rest |
| **Internal** | Query results, metrics | Access controlled |
| **Public** | Schema info, tool names | No restrictions |

## Incident Response Locations

If a security issue is found, these files may need updates:

| Issue Type | Files to Check |
|------------|----------------|
| Auth bypass | `/server/src/auth/`, all MCP tools |
| SQL injection | All `*_test.go` files, MCP tools |
| Credential leak | Logging configuration, error handlers |
| Session issue | `/server/src/auth/sessions.go` |
