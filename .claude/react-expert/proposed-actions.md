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
