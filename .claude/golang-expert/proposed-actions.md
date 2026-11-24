# Proposed Actions (the Write Path)

Issue #774. No code path may run a write on a monitored connection
except `server/src/internal/actions`, acting on a `proposed_actions`
row.

- `POST /api/v1/connections/{id}/query` refuses a write (including a
  side-effecting function call, see `sql-classification.md`) with 409
  and `requires_action`; the `confirmed` field no longer exists. Never
  re-add a direct write branch to the handler.
- `actions.Store` persists rows; `actions` review (a stub reviewer
  until #768) sets `review_status` (`pending`, `passed`, `vetoed`);
  `actions.Executor` runs the stored statements and nothing else.
  The client sends an action id to execute, never SQL.
- Handlers live in `api/action_handlers.go`; the MCP tool is
  `tools/propose_action.go` (non-public, needs `HasWriteAccess`).
- A conversation id in the MCP call metadata is checked with the same
  `ConversationOwnership` as the REST path (wired in
  `cmd/mcp-server/actions_wiring.go`, so the conversation store must
  start before `initActions`). A conversation the caller does not own,
  an oversized id, no username or a nil check refuses the call; never
  drop the id silently. Model and provider over 128 bytes are dropped.
- Execute re-checks `HasWriteAccess`; revocation gives 403 and the row
  stays `not_started`. Two concurrent executes: one wins, the other
  gets 409. `Store.Approve` is one-shot; an approved or overridden
  action that has not started is retried without `Approve`, only by
  the recorded `approved_by` or a superuser, and only through the same
  kind of request (an overridden one needs `override` and `confirm`
  again). A superuser's retry of someone else's approval goes through
  `Store.Reapprove`, which rewrites `approved_by`/`approved_via`/
  `approved_at` only if the row still holds the approval it read, so
  the record names who actually started the run. `ClaimForExecution`
  is the atomic gate and sets `executed_at` (the start time).
- Every superuser decision in the handler (evidence redaction, retry)
  uses the scope-aware `actionAccess.IsSuperuser`
  (`auth.RBACChecker.IsSuperuser`), never the raw context flag, so a
  token narrowed to some connections does not act as a superuser.
- `GET` and `execute` return `ActionHandler.viewFor`: evidence and
  `conversation_id` only to the proposer or a superuser; statements,
  rationale, review reasons and results as stored only to the proposer
  or the approver. Everyone else, superusers included, gets
  `maskActionText`: statements through `metrics.RedactStatementText`,
  free text and result cells through `redact.Text`, and the
  preview/undo/verification/outcome documents nil. Mask a copy; never
  the slices shared with the stored action. Any new route returning an
  `actions.Action` must go through `viewFor`, and
  `tools/datastore_masking_routes_test.go` checks the GET route.
- A revision (`ParentActionID` set) whose review failed is left
  `pending`; `Store.Create` deletes such an untouched child of the same
  proposer (pending, unapproved, not started) in the same transaction
  before inserting, so a retry is not refused by
  `idx_proposed_actions_parent`. `review()` maps a lost `SetReview`
  (`ErrNotFound`) on a revision to `ErrBadRevision`.
- Override of a veto: signed-in session only
  (`!auth.IsAPITokenFromContext`), `HasWriteAccess`, and `confirm` must
  equal the connection name exactly.
- Transaction choice is code, not a model: one transaction when every
  statement allows it, otherwise non-transactional (`transactional`
  false), stopping at the first error. Defaults: `lock_timeout` 5s,
  `statement_timeout` 5m, 3 attempts per chain (`actions` config).
  Config validation rejects either timeout below 1ms (PostgreSQL
  turns a sub-millisecond value into 0, which disables it).
- `sqllex` decides whether a REINDEX must run outside a transaction
  from the target kind word (DATABASE, SCHEMA, SYSTEM) and
  CONCURRENTLY, as a keyword or an option; never from a name, which
  may be any of those words. In an option list the last CONCURRENTLY
  wins, as in PostgreSQL. A quoted TRUE or ON ('on', E'on', "ON",
  $$true$$) turns it on, as PostgreSQL accepts those; any other value,
  or a quoted one spelled with escapes, reads as off, which is safe
  because PostgreSQL then refuses the statement inside the transaction.
  `maskKeepingValues` keeps each quoted value as a word (1 or X).
- `Store.MarkInterruptedUnknown` runs once at startup and marks every
  `executing` row `unknown`. This assumes one server per datastore;
  keep the assumption documented in the user and configuration docs.
  `FinishExecution` accepts `executing`, or an `unknown` row with no
  `results` (the sweep's mark), so a run that is still going when the
  sweep runs still records its outcome; a second finish is
  `ErrInvalidState`.
- `Executor.Start` claims on `context.WithoutCancel(ctx)` bounded by
  `claimTimeout`, so a client disconnect cannot strand the claim.
  Each claim writes a fresh `claim_token` (UUID); when the UPDATE
  errors, `Store.claim` reads the row back on a detached context and
  counts the claim only if it is `executing` under that token, so a
  commit whose reply was lost still gets a run and another server's
  claim is never taken over. Test it through `claim` with a fixed token.
  Outcome writes retry until `Shutdown`'s context ends
  (`shutdownDone`), not merely until `stopping` closes.
- A statement that calls `nextval`/`setval` (matched as a call by
  `sqllex.ChangesSequence`) gets `sequenceNote` when rolled back, or
  `sequenceUnknownNote` when the outcome is unknown.
- Collector schema 33 adds the table; `MinCollectorSchemaVersion` in
  `server/src/internal/database/health.go` must stay in step.
