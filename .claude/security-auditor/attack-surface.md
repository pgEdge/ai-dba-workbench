# Attack Surface

This document describes the external interfaces and input points of the AI DBA
Workbench.

## API Endpoints

### MCP Protocol Endpoint

**Endpoint:** `POST /mcp`

**Risk Level:** CRITICAL

**Inputs:**

- JSON-RPC request body
- Authentication token (header)
- MCP method name
- Method parameters

**Attack vectors:**

| Vector | Description | Mitigation |
|--------|-------------|------------|
| SQL Injection | Via tool parameters | Parameterized queries |
| Auth Bypass | Invalid/forged tokens | Token validation |
| Privilege Escalation | Accessing unauthorized resources | RBAC checks |
| DoS | Large requests, slow queries | Rate limiting, timeouts |

**Input validation required:**

- [ ] Request size limits
- [ ] JSON parsing with limits
- [ ] Method name whitelist
- [ ] Parameter type validation
- [ ] String length limits

### Authentication Endpoints

**Endpoints:** Login, token management

**Risk Level:** CRITICAL

**Attack vectors:**

| Vector | Description | Mitigation |
|--------|-------------|------------|
| Brute Force | Password guessing | Rate limiting, lockout |
| Credential Stuffing | Reused passwords | Monitoring, 2FA |
| Session Fixation | Reusing session IDs | Regenerate on auth |
| Timing Attack | Username enumeration | Constant-time operations |

## MCP Tools

### Query Execution Tools

**Tools:** `query_execute`, `query_explain`

**Risk Level:** CRITICAL

**Inputs:**

- SQL query string
- Connection identifier
- Query parameters

**Attack vectors:**

| Vector | Description | Mitigation |
|--------|-------------|------------|
| SQL Injection | Malicious SQL | Parameterized queries |
| Data Exfiltration | Unauthorized data access | Authorization checks |
| DoS | Resource-intensive queries | Timeouts, query analysis |
| Privilege Escalation | DDL/DML on restricted tables | Read-only connections |

### Connection Management Tools

**Tools:** `connection_create`, `connection_update`, etc.

**Risk Level:** HIGH

**Inputs:**

- Connection parameters (host, port, database)
- Credentials (username, password)
- Configuration options

**Attack vectors:**

| Vector | Description | Mitigation |
|--------|-------------|------------|
| SSRF | Connecting to internal hosts | Host allowlist |
| Credential Theft | Exposing stored credentials | Encryption, access control |
| Injection | Via connection parameters | Input validation |

### User and Privilege Tools

**Tools:** User management, group management, privilege assignment

**Risk Level:** HIGH

**Inputs:**

- User identifiers
- Group identifiers
- Privilege specifications

**Attack vectors:**

| Vector | Description | Mitigation |
|--------|-------------|------------|
| Privilege Escalation | Granting excess privileges | Authorization checks |
| Account Takeover | Modifying other users | Ownership verification |
| Denial of Service | Deleting critical accounts | Protected accounts |

## Data Collection Interface

### Collector to Monitored Database

**Risk Level:** HIGH

**Attack vectors:**

| Vector | Description | Mitigation |
|--------|-------------|------------|
| Credential Exposure | Leaked DB credentials | Encryption, secure storage |
| Data Injection | Malicious metric data | Input validation |
| DoS | Overloading monitored DB | Rate limiting, timeouts |

### Collector to Server Database

**Risk Level:** MEDIUM

**Attack vectors:**

| Vector | Description | Mitigation |
|--------|-------------|------------|
| Data Tampering | Modified metrics | Data integrity checks |
| Schema Injection | Via migration | Controlled migrations |

## Client-Side Attack Surface

### User Input Points

**Risk Level:** MEDIUM

| Input | Location | Risk |
|-------|----------|------|
| Login form | Auth pages | Credential handling |
| Query editor | Query page | Display (not injection) |
| Connection form | Connection page | SSRF setup |
| Settings | Settings page | Configuration tampering |

**Attack vectors:**

| Vector | Description | Mitigation |
|--------|-------------|------------|
| XSS | Malicious script injection | Output encoding, CSP |
| CSRF | Unauthorized actions | CSRF tokens |
| Clickjacking | UI redressing | X-Frame-Options |

### Stored Data

| Data | Storage | Risk |
|------|---------|------|
| Auth tokens | localStorage/memory | Token theft |
| User preferences | localStorage | Low risk |
| Query history | Client state | Information disclosure |

## External Dependencies

### PostgreSQL Driver (pgx)

**Risk:** Vulnerabilities in driver code

**Mitigation:**

- Keep updated
- Monitor CVEs
- Use parameterized queries

### React/Node Dependencies

**Risk:** Supply chain attacks, known vulnerabilities

**Mitigation:**

- Regular `npm audit`
- Lockfile verification
- Dependency review

### Go Dependencies

**Risk:** Vulnerable dependencies

**Mitigation:**

- Regular `go mod tidy`
- Vulnerability scanning
- Minimal dependencies

## Network Attack Surface

### Exposed Ports

| Port | Service | Exposure |
|------|---------|----------|
| HTTP/HTTPS | MCP Server | Internet/Internal |
| PostgreSQL | Server DB | Internal only |
| PostgreSQL | Monitored DBs | Internal only |

### TLS Requirements

- [ ] All external traffic over HTTPS
- [ ] TLS 1.2+ required
- [ ] Strong cipher suites only
- [ ] Certificate validation

## Input Validation Summary

### All Inputs Must Be Validated

| Input Type | Validation |
|------------|------------|
| Strings | Length limits, character whitelist |
| Numbers | Range validation, type checking |
| SQL | Parameterization only |
| JSON | Schema validation, depth limits |
| URLs | Protocol whitelist, host validation |
| File paths | Disallowed (no file operations from user input) |

### Validation Location

```
Client-side validation (UX only, not security)
         │
         ▼
Server-side validation (REQUIRED for security)
         │
         ▼
Database constraints (Defense in depth)
```

## Rate Limiting Requirements

| Endpoint/Action | Limit | Window |
|-----------------|-------|--------|
| Login attempts | 5 | 15 minutes |
| Token generation | 10 | 1 hour |
| Query execution | 100 | 1 minute |
| Connection creation | 10 | 1 hour |

## Security Headers

Required HTTP headers:

```
Content-Security-Policy: default-src 'self'
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
X-XSS-Protection: 1; mode=block
Strict-Transport-Security: max-age=31536000
```
