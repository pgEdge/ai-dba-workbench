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

- A notification channel's URL is itself a credential (Slack and
  Mattermost incoming webhooks, a Telegram bot token, a generic webhook
  endpoint holding a token in its path, query string or userinfo), so
  no error in a sender wraps with `%w` anything `net/http` or `net/url`
  produced, and every piece of borrowed text goes through
  `sanitizeEcho` first. `(*url.Error).Error` renders the *raw input
  string* rather than a parsed URL and masks no password, so a parse
  failure is never echoed at all, sanitised or otherwise. `sanitize.go`
  is duplicated between `alerter/src/internal/notifications` and
  `server/src/internal/api` and the two copies must stay byte-identical
  below the copyright header; check with
  `diff <(tail -n +11 <a>) <(tail -n +11 <b>)`.

## The RBAC Audit Log

Administrative changes to users, tokens, groups and permissions are
recorded in the `audit_events` table of the SQLite auth store
(`server/src/internal/auth/audit.go`), with the acting principal, the
client address, before and after snapshots and a hash chain.

- Each row's hash covers the previous row's hash, its own
  `hash_version` and every other audited column except `id`. Each row
  stores the rendering version it was hashed under, and `auditHash`
  dispatches on it, but a format change is not free: version 2 left
  every version 1 row unverifiable, and an upgraded database has to be
  re-chained. A version `auditHash` will not compute is reported as
  tampering, wrapped in `ErrAuditChainBroken` in `VerifyAuditChain`,
  because the version is a value in the file and relabelling a row must
  not put it beyond the verifier. Both renderings length-prefix each
  field, so text cannot be shifted between two columns without changing
  the digest.
- Version 1 is an unkeyed SHA-256. No row this build writes carries
  it, and `auditHash` refuses to compute it at all, returning
  `ErrAuditUnkeyedRow`: the only code that still computes it is
  `verifyLegacyAuditChain`, which reports on a log the re-chain is
  about to replace. Version 2 is an HMAC-SHA256 under
  `DeriveAuditKey(serverSecret)`, so a chain cannot be recomputed
  without the server secret. `NewAuthStore` refuses an empty or
  under-32-byte key, and every caller, the CLI included, must supply
  one.
- Two rules stop the keying being downgraded away, which without them
  would be the `alg: none` attack: the version is inside the digest, so
  a version 2 row cannot be relabelled and recomputed as version 1, and
  `VerifyAuditChain` refuses a version that falls as the chain
  advances. Behind both, `ensureNoUnkeyedAuditRows` runs on every open
  and refuses to open a database holding any version 1 row at all.
- The re-chain is only for a log wholly inherited at upgrade, and both
  `ensureNoUnkeyedAuditRows` and `auditRechainPlan` test that shape
  through `checkNoKeyedAuditRows`: any keyed row at all alongside an
  unkeyed one means unkeyed rows were written into a log that was
  already keyed, and the refusal names that case and points at a
  restore rather than at `-rechain-audit-log`. Row order is not
  consulted, and must not be: `id` is `INTEGER PRIMARY KEY
  AUTOINCREMENT`, which constrains only the values SQLite assigns, so
  an explicit `INSERT` at id 0 or -1 sorts below every genuine row and
  satisfies any test phrased as a prefix (VULN-307). Without the check,
  an attacker who rewrote the log, relabelled every row version 1 and
  recomputed the unkeyed chain (which needs no secret) could rely on
  the start-up refusal to pressure an operator into signing the
  forgery. `-confirm-rechain` additionally refuses a plan whose
  `LegacyChainOK` is false, so an unattended run cannot sign a log the
  command has just reported does not recompute.
  `verifyLegacyAuditChain` carries the same downgrade check as
  `VerifyAuditChain`, so `LegacyChainOK` cannot be true for a log with
  a version 1 row above a keyed one.
- The approved plan is bound to the rows that get signed.
  `AuditRechainPlan` carries `Events`, `LowestID` and `HighestID`, and
  `checkAuditRechainPlanStillHolds` re-reads all three as the first
  statement inside the rewrite transaction, which holds the write lock,
  and returns `ErrAuditRechainChanged` if any has moved. `s.mu` is no
  help here: the adversary is another writer on the same file, and the
  confirmation prompt can wait indefinitely.
- The one way past that gate is `RechainAuditLog`, driven by the
  operator command `-rechain-audit-log`. It opens through the
  unexported `newAuthStore` with `allowUnkeyedAuditRows`, shows the
  operator the figures, requires confirmation, and then re-hashes every
  row as version 2 in one transaction, re-linking each `prev_hash` to
  the new hash of the row before it and appending an `audit.rechain`
  event. It drops and re-creates the append-only trigger inside that
  transaction; SQLite makes DDL transactional, `_txlock=immediate`
  holds the write lock from BEGIN, and WAL readers see the last
  committed snapshot, so no other connection ever observes the table
  without the trigger and a rollback restores both rows and trigger.
- There is deliberately no boundary machinery between inherited and
  keyed rows. Three earlier designs kept a signed watermark, and each
  provided a route to have the server sign a forgery: the last one let
  the retention purge re-anchor the boundary onto a backdated row.
  `PurgeAuditEvents` now deletes and records its own event, and writes
  no hash over a row it did not create. Treat any proposal that has an
  unattended path re-sign existing rows as that bug returning.
- `PurgeAuditEvents` deletes a contiguous id-prefix and can express
  nothing else: it reads `cut = MIN(id) WHERE occurred_at >= ?`, checks
  the prefix, then runs `DELETE ... WHERE id < cut`. `occurred_at` is
  attacker-writable. The append-only trigger stops an UPDATE changing
  an `id`, but `id` is not in the HMAC, so a writer can delete a
  genuine row and re-insert it at another id and it still verifies; an
  id-prefix is a log prefix only because `verifyAuditPurgePrefix`
  requires the rows to link in id order from the recorded head. A
  row inserted at a chosen id can only move the boundary earlier,
  never past the oldest row in the window. Deleting by
  timestamp alone
  made retention, which runs unattended every five minutes, a
  suffix-deletion oracle: backdated newest rows were deleted, the chain
  relinked to the last survivor, and the appended `audit.purge` event
  raised `MAX(id)` back into agreement with `sqlite_sequence`, which is
  exactly what `verifyAuditTail` reads. Where no row is inside the
  window the subquery yields NULL and nothing is deleted, deliberately:
  an empty log would hand the same oracle back, and a fixed cut-off
  such as 0 would delete rows inserted at negative ids. Any change that filters
  the purge on a column a writer of `auth.db` controls reopens this.
- Verification distinguishes a wrong key from tampering, because the
  responses differ. A failing row is `ErrAuditKeyMismatch` (CLI exit 3)
  only if no row outside the history has verified yet (`keyProven`)
  and `looksLikeKeyChange` sees a linked run of failing rows followed
  by the end of the log or by rows that ALL verify and link, with
  `verifyAuditTail` passing. It walks the whole log, deliberately: the
  re-anchor's history runs to the last failing row anywhere, so a
  verdict drawn from the leading rows let `-confirm-rechain` launder a
  later deletion. `classifyUnverifiedRow`, and the purge's
  `auditPurgeUnverified`, also never report a key mismatch when a verified anchor recording a head exists
  (`anchorFound`): the verifier returns on the first bad row before the
  history-digest and head checks, so asking would let one edit just
  past an anchor's head or history launder a deletion there (both fixed
  in #502; the A-to-B-to-A secret rotation falls to the interactive
  path). `scanAuditForReanchor` re-derives the shape
  (`failsAfterVerified`) and clears `KeyMismatch` if a row was written
  between the verify and the scan, and `reanchorAuditLogTx` re-runs
  `verifyAuditTail` under the write lock on a key mismatch; otherwise it is `ErrAuditChainBroken`, and downgrades and unkeyed
  rows are exit 2 as well. An attacker who rewrites the oldest rows
  keeping their links therefore gets the key-mismatch wording; exit 3
  is advice, not proof of innocence.
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
  missing `sqlite_sequence` table as well, each wrapped in
  `ErrAuditChainBroken` so `-verify-audit-log` exits with the tampering
  status. `MAX(id)` and the sequence are read by one statement, so a
  server insert whilst the CLI, which opens its own store, is verifying
  cannot separate them.
- Snapshots never carry `password_hash`, and no token material reaches
  a row, a log line or a response.

Know the limits before crediting the chain in a report, and say them
plainly rather than crediting the design with more than it does.

- An attacker holding both `auth.db` and the server secret can forge
  the log freely, since the key is derived from the same secret the
  server reads. Only a copy of the events somewhere the server cannot
  reach defends against that, and there is no anchor outside the file.
- Truncating the tail needs write access to `auth.db` alone, no
  secret. `sqlite_sequence`, which `verifyAuditTail` compares the
  newest id against, has no trigger or other protection, so deleting
  the newest rows and one `UPDATE sqlite_sequence SET seq = <new
  MAX(id)>` passes verification. Do not credit the key with defending
  the tail.
- Head deletion (#502) is caught against the head the newest purge
  event recorded (`oldest_retained_hash`); a record-less purge event
  never overrides an older recorded one, in the verifier or in
  `auditPurgeStart`. Only on a log whose purge events all predate the
  record does the weak fallback apply (a missing predecessor passes
  if any `audit.purge` row survives). The purge refuses, and retention
  stalls, on a prefix that does not verify and link from the recorded
  head, including for benign causes such as a rotated secret, until an
  operator re-anchors with `-rechain-audit-log`.
- The re-anchor (`audit_reanchor.go`) appends one keyed `audit.rechain`
  event and accepts every row through the last failing one as history,
  bound only by an unkeyed SHA-256 digest and count. History rows lose
  attribution: anything done to them before the re-anchor is accepted,
  and only later changes are detectable. An older anchor re-inserted
  into the log is refused by `checkAuditAnchorLinks` in the purge and
  by the link check in the verifier; the newest anchor recording a
  head, which must verify, wins. `-confirm-rechain` re-anchors
  unattended only on a key mismatch, which is forgeable (see above):
  editing every row through the newest anchor, keeping stored hashes,
  makes that anchor unverifiable and the log rotation-shaped, so a
  scripted run accepts rewritten rows reaching back past the newest
  purge, which may be minutes old. There is no `-previous-secret-file`
  to prove them. `previous_*` head fields are
  signed into the event only when the previous anchor verified, and the
  CLI prints file-sourced hashes only if they are 64 lowercase hex
  (`auditPlanHash`).
- Rows deleted and later restored at their original ids from a copy
  leave no trace; that needs an anchor outside the file.
- `verifyAuditSchema` refuses any trigger other than
  `audit_events_no_update` whose table is `audit_events` or whose SQL
  mentions it, and `recordAudit` fails unless the INSERT wrote exactly
  one row, against `RAISE(IGNORE)` triggers silently dropping events.
  `prev_hash` stored as a BLOB escapes the unique index (BLOB and TEXT
  compare unequal); the id-order walk still rejects a fork, and no
  `typeof` check exists yet.
- A re-chain attests the database exactly as it stood at the moment it
  ran, and says nothing about anything before it. On an upgraded
  installation the keyed chain starts at that point.
- Nothing verifies at start-up. `VerifyAuditLog` has one non-test
  caller, `verifyAuditLogCommand` under `-verify-audit-log`, so on an
  installation that never runs it the chain is never checked.

A verification that passes is not evidence that nothing happened.

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
