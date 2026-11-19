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
- Execute re-checks `HasWriteAccess`; revocation gives 403 and the row
  stays `not_started`. Two concurrent executes: one wins, the other
  gets 409.
- Override of a veto: signed-in session only
  (`!auth.IsAPITokenFromContext`), `HasWriteAccess`, and `confirm` must
  equal the connection name exactly.
- Transaction choice is code, not a model: one transaction when every
  statement allows it, otherwise non-transactional (`transactional`
  false), stopping at the first error. Defaults: `lock_timeout` 5s,
  `statement_timeout` 5m, 3 attempts per chain (`actions` config).
- `Store.MarkInterruptedUnknown` runs once at startup and marks every
  `executing` row `unknown`. This assumes one server per datastore;
  keep the assumption documented in the user and configuration docs.
- Collector schema 33 adds the table; `MinCollectorSchemaVersion` in
  `server/src/internal/database/health.go` must stay in step.
