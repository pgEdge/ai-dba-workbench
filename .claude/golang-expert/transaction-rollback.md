/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Transaction Rollback Contexts
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

# Rollback Contexts for pgx Transactions

This document records the project-wide rule for the context passed to
`tx.Rollback()`, settled by GitHub issue #381. Every pgx transaction in
the tree rolls back on a non-cancelable context; the rationale lives
here rather than being repeated at each call site.

## The Rule

A rollback is an unwind path, not part of the caller's work, so it must
never inherit the caller's cancellation:

```go
tx, err := pool.Begin(ctx)
if err != nil {
    return fmt.Errorf("begin transaction: %w", err)
}
defer tx.Rollback(context.Background()) //nolint:errcheck // no-op after commit; non-cancelable ctx (see contributing.md)
```

The rule covers every rollback, whether deferred directly, deferred
inside a cleanup closure, or issued inline on an error path. The
statements inside the transaction keep using the request-derived `ctx`;
only the rollback changes.

Rollbacks that take no context (the `database/sql` transactions in
`server/src/internal/auth/`) fall outside the rule, because that API
carries no context at all.

## Why Not the Request Context

The pgx v5 driver treats a rollback on a cancelled context as a failed
rollback, and a failed rollback is unrecoverable:

```go
// pgx v5 tx.go
_, err := tx.conn.Exec(ctx, "rollback")
tx.closed = true
if err != nil {
    // A rollback failure leaves the connection in an undefined state
    tx.conn.die()
    return err
}
```

When a browser closes a tab or an HTTP client times out, the request
context is cancelled whilst the transaction is still open. Passing that
context to `Rollback` means `Exec` fails immediately, `conn.die()` runs,
and the pooled connection is discarded whilst its transaction is still
open on the server. The related close-of-closed-channel panic is
tracked upstream as
[jackc/pgx#2470](https://github.com/jackc/pgx/issues/2470). The
practical symptom is a slow leak of pool connections left in an
aborted-transaction state, which is exactly what the sweep removed.

`context.Background()` is deliberate rather than
`context.WithoutCancel(ctx)`. The rollback needs no request values, and
a single obvious form makes the convention easy to scan for and easy to
enforce automatically.

## How the Rule Is Enforced

Two tests in `server/src/internal/database` lock the behaviour in:

- `rollback_context_integration_test.go` drives `UnacknowledgeAlert`
  with a pgx `QueryTracer` that cancels the request context the moment
  the last statement completes. It asserts that the `ROLLBACK` runs on
  an uncancelled context, and that the pooled backend PID is unchanged
  afterwards, which proves the connection was returned rather than
  destroyed.

- `rollback_convention_test.go` parses every non-test Go file under
  `server/src`, `collector/src`, and `alerter/src`, and fails when a
  single-argument `Rollback` call receives anything other than
  `context.Background()`. New transactions therefore cannot quietly
  reintroduce the bug.

The contributor-facing summary of the same rule lives under "Go Code"
in `docs/developer-guide/contributing.md`.
