# Proposed Actions in the Client

Issue #774. A write is never sent to `/query` for execution.

- `RunnableCodeBlock` posts the SQL to `/query`; a 409 with
  `requires_action` shows the write confirmation panel, and Execute
  there mounts `ActionBlock` with `sql` and a `source`
  (`chat_block`, `alert_analysis`, `query_analysis`,
  `server_analysis` or `chart_analysis`). There is no `confirmed` flag.
- `ActionBlock` (`components/shared`) creates the action with
  `execute: true`, or follows one given by `action` or `actionId`
  (from a `propose_action` tool result in `ChatMessage`). It polls via
  `hooks/useAction.ts` and never resends SQL: execute and cancel take
  the action id only.
- UI copy is asserted by tests; keep it in step with the user guide
  (`docs/user-guide/ai/ai-analysis.md`, Reviewed Write Actions):
  "Supervisor reviewing…", "Approved by Ellie's supervisor", "Vetoed
  by Ellie's supervisor", "Running…", "Succeeded", "Failed",
  "Cancelled", "Outcome unknown: check the database", and the buttons
  Execute, Retry, Cancel and "Override and run".
- `OverrideActionDialog` requires the exact connection name before
  "Run anyway" enables, and clears the field on every opening.
- Status values mirror the server: `review_status` pending, passed,
  vetoed; `approval` none, approved, overridden; `exec_status`
  not_started, executing, succeeded, failed, cancelled, unknown.
- `ChatMessage` shows one `ActionBlock` per revision chain, keyed by
  action id (`ChatPanel/proposedActionBlocks.ts`). Chains are linked by
  `ToolActivity.revisesActionId`, recorded from the `propose_action`
  call's `revises_action_id` argument in `chatAgenticLoop.ts`.
  `ActionBlock` keys its start guard by the action id, so a changed id
  loads afresh and a stale polled action for another id is ignored.
- Retry is offered only where the server accepts it: `approval` is
  `approved` and the user is `approved_by` or a superuser, or
  `overridden` (`approved_by` is the overrider) with the same test, in
  which case Retry reopens `OverrideActionDialog` and sends the typed
  confirm with `override`. "Override and run" is for `approval: none`.
- `useAction.execute` keeps the refusal error shown while it re-fetches
  after a 409 or 503 (`load(id, true)`); `ActionBlock` shows an error
  for `sql` given without a `source`.
