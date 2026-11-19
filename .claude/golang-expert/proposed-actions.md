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
  again). `ClaimForExecution` is the atomic gate and sets
  `executed_at` (the start time).
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
  CONCURRENTLY, as a keyword or an option not set false/off/0; never
  from a name, which may be any of those words.
- `Store.MarkInterruptedUnknown` runs once at startup and marks every
  `executing` row `unknown`. This assumes one server per datastore;
  keep the assumption documented in the user and configuration docs.
- Collector schema 33 adds the table; `MinCollectorSchemaVersion` in
  `server/src/internal/database/health.go` must stay in step.
