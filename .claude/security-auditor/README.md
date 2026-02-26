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
| `/server/src/internal/auth/` | **Critical** | Authentication logic |
| `/server/src/internal/tools/` | **High** | SQL execution |
| `/server/src/internal/database/` | **High** | Database operations |
| `/collector/src/database/` | **High** | Database credentials |
| `/alerter/src/` | **High** | Alert processing and notifications |

### Security Patterns in Use

- Password hashing: bcrypt (cost 12); token hashing: SHA256
- Token generation: crypto/rand for randomness
- SQL: Parameterized queries (pgx for PostgreSQL, database/sql for SQLite auth store)
- Session isolation: Per-user connection scoping

### Common Vulnerability Classes

1. **SQL Injection** - Query construction
2. **Authentication Bypass** - Token validation
3. **Authorization Bypass** - Privilege checking
4. **Credential Exposure** - Logging, error messages
5. **Session Hijacking** - Token management

Last Updated: 2026-02-26
