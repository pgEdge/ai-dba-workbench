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
| `server/src/internal/auth/federation.go` | Federated identity: linking, provisioning, group reconciliation and unlinking |
| `server/src/internal/oidc/` | OIDC discovery, the authorisation request, ID-token verification, claim extraction and the sealed login-state cookie |
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
- An API token's connection scope is a third constraint, intersected
  with both of the above. `CanAccessConnection` and
  `VisibleConnectionIDs` in `server/src/internal/auth/access.go` apply
  it on every path, ownership and sharing included, and it can only
  lower an access level, never raise one.
- `IsConnectionInTokenScope` treats a token with no scope rows as
  unrestricted, so an unscoped token inherits its owner's access in
  full.
- Token scope does not constrain a superuser: the superuser bypass
  returns before any scope check. That is deliberate, not an oversight.
- `http.auth.local.enabled` is enforced in exactly one place:
  `handleLogin` in `api/auth_handlers.go`, which refuses before the body
  is parsed when the flag is false. `AuthStore.AuthenticateUser`
  deliberately knows nothing about the flag, keeping its single meaning
  of "does this password verify"; `handleLogin` is its only caller
  outside the store, which is what makes one enforcement point
  sufficient. The refusal answers with the same body, status and headers
  as a wrong password, so it discloses nothing further about a
  credential, but it is not indistinguishable from one and does not need
  to be: the flag itself is public, reported as `local_enabled` by the
  capabilities endpoint.
- Only a local account may have a password written to it, enforced by
  `assertPasswordWritableLocked` at the store boundary and called from
  both `UpdateUser` and `UpdateUserAtomic`. Without it a hash written
  onto a federated account lies dormant until
  `UnlinkFederatedIdentity` with `-restore-password` returns
  `auth_source` to local and makes it live. `updateUser` in
  `api/rbac_user_handlers.go` checks the same condition first and
  answers 400, because that endpoint applies the password, enabled and
  superuser changes in one transaction and a late refusal would silently
  roll back the others.
- Every setting in the `http.auth` section is read once at start-up and
  held for the life of the process. A reload changes none of them:
  handlers keep by-value copies and `ReloadableConfig.Reload` only swaps
  the pointer, so `logRestartRequiredSettings` reports each one as
  requiring a restart. Advice that assumes an authentication setting can
  be tightened by SIGHUP is wrong.
- A federated account is matched on the pair `(issuer, subject)` and
  NEVER on username. `ExternalSubjectKey` in `auth/federation.go`
  builds the key and `ResolveFederatedUser` looks the account up by it
  alone, so an identity provider that reissues a username cannot hand
  one person another person's account. Any change that reintroduces a
  username lookup on the login path is a privilege-escalation bug, and
  this is the single most important invariant in the federated surface.
- `pkg/crypto`'s `EncryptGCM` and `DecryptGCM` accept any key length
  `aes.NewCipher` will take (16, 24 or 32 bytes), despite doc comments
  promising AES-256 and exactly 32 bytes. A short key therefore
  downgrades the cipher silently rather than failing, so every caller
  must check the length itself; `internal/oidc/state.go` does, in
  `SealState` and `OpenState`, against its own `keySize` constant.

## Reporting

Findings go in the audit report returned to the primary agent, with
file path, symbol, severity and a concrete remediation. This agent
does not edit code or this knowledge base; the primary agent applies
fixes and updates this file when a decision above changes.
