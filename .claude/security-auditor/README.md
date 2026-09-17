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
- Clearing an alert is a security-relevant decision, because a false
  resolution hides a live problem: the `clearWhenAbsent` flag on each
  `metricRegistry` entry in
  `alerter/src/internal/database/metric_registry.go` decides whether a
  metric that returns no row may clear its alert, and its default must
  stay `false` (a metric the registry does not know clears nothing).
  Clearing on absence is additionally gated on probe freshness: the
  entry's `probeName` must be reporting for the alert's connection, per
  `GetProbeStalenessByConnection`, before `resolveAbsentMetric` clears,
  so a stopped collector, a stalled probe or a disabled one leaves the
  alert active (issue #407).
- Visibility is per user: connections are scoped to their owner or to
  groups the user belongs to. Handlers do not hide existence: a caller
  without access to a connection gets 403, and several of them echo the
  requested connection ID in the message (see `handleTopQueries` and its
  neighbours in `internal/api/perf_summary_handlers.go`, and
  `getConnection` in `internal/api/connection_handlers.go`). 404 is
  reserved for resources that genuinely do not exist. Judge any
  enumeration concern against that convention rather than assuming a
  404-for-everything model.
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
  `writePasswordHashLocked` in `server/src/internal/auth/store.go`, the
  single path through which `UpdateUser` and `UpdateUserAtomic` write a
  hash. The rule is a condition on the `UPDATE` itself
  (`AND auth_source = 'local'`) followed by a `RowsAffected` check, not
  a check made beforehand, so the CLI linking the account in another
  process during the bcrypt computation cannot slip a hash onto a
  federated account; when no row matches, the store looks up
  `auth_source` only then to tell a missing user (no error) from a
  refused write. Without it a hash written onto a federated account
  lies dormant until `UnlinkFederatedIdentity` with `-restore-password`
  returns `auth_source` to local and makes it live. `updateUser` in
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

## The RBAC Audit Log

Administrative changes to users, tokens, groups and permissions are
recorded in the `audit_events` table of the SQLite auth store
(`server/src/internal/auth/audit.go`), with the acting principal, the
client address, before and after snapshots and a hash chain.

- Each row's hash covers the previous row's hash and every audited
  column except `id` and `hash_version`. Each row stores the rendering
  version it was hashed under in `hash_version`, and `auditHash`
  dispatches on it, so a format change adds a case rather than
  invalidating older rows, and an unknown version is reported as such
  and not as a broken chain. The version 1 rendering length-prefixes
  each field, so text cannot be shifted between two columns without
  changing the digest.
- `prev_hash` carries a unique index, so no two events can name the
  same predecessor and only one event can be the genesis row with an
  empty `prev_hash`. A forked chain is refused by the schema.
- A `BEFORE UPDATE` trigger makes rows immutable. It does not cover
  `DELETE`, which the retention purge needs.
- `ensureAuditSchema` re-runs the `IF NOT EXISTS` audit DDL on every
  open regardless of the recorded schema version, and
  `VerifyAuditChain` first asserts through `verifyAuditSchema` that the
  unique index and the trigger exist. Without both, a database stamped
  with the current version could run without either for good: an
  earlier draft did exactly that, and a store created between the
  commit that set the version and the one that added the index was
  never given it.
- `verifyAuditTail` compares `MAX(id)` with the `sqlite_sequence`
  entry and reports any disagreement: missing, above or below, and a
  missing `sqlite_sequence` table as well. `MAX(id)` and the sequence
  are read by one statement, so a server insert whilst the CLI, which
  opens its own store, is verifying cannot separate them.
- Snapshots never carry `password_hash`, and no token material reaches
  a row, a log line or a response.

Know the limits before crediting the chain in a report. It is an
unkeyed SHA-256 computed by the same server that stores it, and
`sqlite_sequence` is an ordinary writable table, so anyone with write
access to `auth.db` can alter the log and make both checks agree
again. The chain and the tail check raise the cost of careless or
accidental deletion; they are not a defence against a deliberate
attacker with filesystem access, and only an independent copy of the
events is. A verification that passes is not evidence that nothing
happened. Adding a key the server does not hold, or an anchor outside
the file, is an open design question rather than an oversight.

The audit write is fail-closed everywhere but one place: a mutation
whose event cannot be recorded is rolled back. The exception is
`disableForLockout` in `actor_store_users.go`, which commits the
failed-login lockout first and records `user.disable` afterwards in a
transaction of its own, logging rather than returning a failure. This
is deliberate. Sharing one transaction let an audit failure, a full
disk or a lock held past the busy timeout roll the lockout back and
hand a password-guessing attacker an account that stayed enabled. Do
not report the best-effort event as a missing fail-closed path; report
any *new* audited mutation that copies the pattern.

## Reporting

Findings go in the audit report returned to the primary agent, with
file path, symbol, severity and a concrete remediation. This agent
does not edit code or this knowledge base; the primary agent applies
fixes and updates this file when a decision above changes.
