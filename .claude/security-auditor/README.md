# Security Auditor Knowledge Base

This file records where the security-sensitive code in the pgEdge AI
DBA Workbench lives and which design decisions an audit should take as
given. It is a map, not a checklist: verify every entry against the
code before relying on it, and correct the entry when the code has
moved.

## Security-Sensitive Locations

| Location | Why it matters |
|----------|----------------|
| `server/src/internal/auth/` | Users, groups, tokens, permissions and the RBAC checker, backed by SQLite |
| `server/src/internal/api/` | HTTP handlers; every mutating handler carries an authorisation gate (see `../golang-expert/rbac-patterns.md`) |
| `server/src/internal/tools/` | MCP tools, including ones that execute SQL against monitored databases |
| `server/src/internal/database/` | Datastore queries; identifier interpolation is allow-listed and quoted |
| `server/src/internal/llmproxy/` | Outbound proxy to LLM providers with its own `authorize` step and provider API keys |
| `pkg/crypto/` | Encryption of stored monitored-database passwords (AES-GCM) |
| `collector/src/database/` | Decrypts credentials through `pkg/crypto` to open monitored connections |
| `alerter/src/` | Alert evaluation and outbound notifications (email, webhook, messaging) |
| `client/src/` | Bearer token handling, admin panels and any rendering of server-supplied text |

## Design Decisions to Audit Against

- Passwords are hashed with bcrypt at cost 12 (`DefaultBcryptCost` in
  `auth/store.go`); a timing-equaliser dummy hash is compared when a
  username does not exist.
- Session and service tokens are 32 random bytes from `crypto/rand`
  and are stored as SHA-256 hashes; the raw token is shown once.
- Clients authenticate with a bearer token in the `Authorization`
  header, with a `session_token` cookie as the fallback
  (`ExtractBearerToken` in `auth/middleware.go`). There is no
  httpOnly-cookie-only mode; advice assuming one does not apply.
- SQL against Postgres is parameterised with pgx (`$1`); the SQLite
  auth store uses `database/sql` with `?` placeholders. Relation and
  column names that must be interpolated are validated against a
  live-discovered allow-list and quoted (see
  `../golang-expert/metrics-queries.md`).
- The exception to the injection rule is deliberate: MCP tools that
  run operator-supplied SQL execute it as written, under the
  monitored connection's own privileges.
- Monitored-database credentials are encrypted at rest with a
  server-side secret and only decrypted in the collector when a pool
  is opened.
- Visibility is per user: connections are scoped to their owner or to
  groups the user belongs to, and GET handlers return 404 rather than
  403 for resources the caller cannot see.

## Reporting

Findings go in the audit report returned to the primary agent, with
file path, symbol, severity and a concrete remediation. This agent
does not edit code or this knowledge base; the primary agent applies
fixes and updates this file when a decision above changes.
