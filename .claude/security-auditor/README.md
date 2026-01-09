# Security Auditor Knowledge Base

This directory contains security-specific documentation for the pgEdge AI DBA
Workbench project.

## Purpose

This knowledge base provides:

- Security-sensitive code locations
- Credential and secret handling patterns
- Attack surface documentation
- Previous security findings and resolutions
- Security testing guidance

## Documents

### [security-sensitive-areas.md](security-sensitive-areas.md)

High-risk code locations requiring careful review:

- Authentication and authorization code
- Input validation points
- Database query construction
- Credential handling
- Session management

### [credential-handling.md](credential-handling.md)

How credentials are managed in the system:

- Password hashing approach
- Token generation and storage
- Database connection credentials
- Encryption at rest

### [attack-surface.md](attack-surface.md)

External interfaces and input points:

- API endpoints
- MCP protocol inputs
- User-provided SQL queries
- Configuration inputs

### [security-checklist.md](security-checklist.md)

Security review checklist for common scenarios:

- New endpoint review
- Database query review
- Authentication change review
- Input handling review

## Quick Reference

### Critical Security Files

| File | Risk Level | Reason |
|------|------------|--------|
| `/server/src/auth/` | **Critical** | Authentication logic |
| `/server/src/mcp/tools/` | **High** | SQL execution |
| `/server/src/database/` | **High** | Database operations |
| `/collector/src/database/` | **High** | Database credentials |

### Security Patterns in Use

- Password hashing: bcrypt or SHA256 (see credential-handling.md)
- Token generation: crypto/rand for randomness
- SQL: Parameterized queries only (pgx)
- Session isolation: Per-user connection scoping

### Common Vulnerability Classes

1. **SQL Injection** - Query construction
2. **Authentication Bypass** - Token validation
3. **Authorization Bypass** - Privilege checking
4. **Credential Exposure** - Logging, error messages
5. **Session Hijacking** - Token management

## Document Updates

Update these documents when:

- New security-sensitive features are added
- Security vulnerabilities are found and fixed
- Security patterns change
- New attack vectors are identified

Last Updated: 2026-01-09
