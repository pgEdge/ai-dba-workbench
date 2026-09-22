/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Transaction Rollback Contexts
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

# Rolling Back pgx Transactions

This document records the project-wide rule for rolling back pgx
transactions, settled by GitHub issue #381 and reworked in PR #420.
Every rollback in the tree goes through the shared package
`github.com/pgedge/ai-workbench/pkg/rollback`; the rationale lives in
that package's doc comment and is summarised here, not repeated at each
call site.

## The Rule

A rollback is an unwind path, not part of the caller's work, so it must
not inherit the caller's cancellation, but it must still be bounded:

```go
tx, err := pool.Begin(ctx)
if err != nil {
    return fmt.Errorf("begin transaction: %w", err)
}
defer rollback.Tx(ctx, tx) //nolint:errcheck // no-op after commit
```

`rollback.Tx(ctx, tx)` calls `tx.Rollback` on a context built by
`rollback.Context(ctx)`, which is
`context.WithTimeout(context.WithoutCancel(ctx), rollback.Timeout)`
with `Timeout` fixed at five seconds. The timeout starts when the
helper runs, so a deferred `rollback.Tx` is correct however long the
transaction lasts; never build the rollback context at `Begin` time.
`Tx` returns the underlying error, so an inline error path that wants
to log a failed rollback can still do so from the returned value; the
collector's `Migrate` in `collector/src/database/schema.go` is the one
remaining caller that does. `StoreMetrics` in
`collector/src/probes/storage.go` used to as well, but its guard tested
an `err` that every later failure path shadowed, so the rollback never
ran; issue #424 replaced the whole block with the deferred one-liner
above.

Savepoint unwinds use `rollback.ToSavepoint(ctx, tx, name)`, which
issues `ROLLBACK TO SAVEPOINT <name>` on the same bounded context and
rejects any name that is not a plain unquoted SQL identifier. The
matching `SAVEPOINT` and `RELEASE SAVEPOINT` statements are the
caller's work and keep the caller's `ctx`. There are three users:
`runSavepointed` in `collector/src/database/schema.go`;
`validateStatement` in `server/src/internal/api/query_handlers.go`,
which takes a savepoint around each `EXPLAIN` so that one rejected
statement does not abort validation of the rest; and `TestQueryTool` in
`server/src/internal/tools/test_query.go`, which unwinds to a savepoint
after the whole-query `EXPLAIN` fails on a multiple-statement query
before retrying statement by statement.

The statements inside the transaction keep using the request-derived
`ctx`; only the rollback changes. Rollbacks that take no context (the
`database/sql` transactions in `server/src/internal/auth/`) fall
outside the rule, because that API carries no context at all.

## Why Not the Request Context, and Why Not Background

pgx v5 treats a rollback issued on a cancelled context as a failed
rollback and calls `conn.die()`, discarding the pooled connection. The
server tears the open transaction down when the socket closes, so the
cost is connection churn, plus the pool losing that slot until it
redials, rather than a leak; but a browser closing a tab or an HTTP
client timing out pays it on every unwind.

`context.Background()` avoids the churn but has no deadline, and pgconn
skips its context watcher when the context is literally `Background()`,
so a peer that acknowledges the `ROLLBACK` but never answers would pin
the pool slot and the goroutine with nothing but TCP keep-alive to
break it. The four HTTP handlers that derived their `ctx` with a
thirty-second timeout used to have their rollbacks bounded by that;
the helper restores a bound. `context.WithoutCancel` rather than
`Background()` also keeps the request's values (tracing, logging).

jackc/pgx#2470, once cited as the reason for the rule, tracked a
close-of-closed-channel panic in the same `die()` path against v5.7.0
and is closed; treat it as related history, not as the justification.

## How the Rule Is Enforced

`rollback.Scan(root)` in `pkg/rollback/scan.go` parses every non-test
Go file under `root` (skipping `vendor`, `node_modules` and the
`pkg/rollback` directory itself) and returns `path:line` for two
shapes of offender: a method call named `Rollback` taking exactly one
argument, and a call to `Exec`, `Query`, `QueryRow`, `Prepare` or
their `Context` variants whose SQL argument (a string literal, or a
concatenation whose leftmost operand is a literal) starts with
`ROLLBACK` case-insensitively. `rollback.Tx` takes two arguments and is
therefore not matched.

Each module runs the scanner over its own root in a test named
`TestNoDirectRollbacksInModule`, so the check runs under whichever
path-filtered CI workflow a change triggers:

- `pkg/rollback/scan_test.go` scans `..` (the `pkg` module);
- `server/src/internal/database/rollback_convention_test.go`,
  `collector/src/database/rollback_convention_test.go` and
  `alerter/src/internal/database/rollback_convention_test.go` each
  scan `../..` from their own directory.

The module root is found relative to the test file, never by walking
up to the repository `Makefile`. Note that `pkg/` has no path-filtered
workflow of its own: the root `Makefile` runs `cd pkg && go test ./...`,
but a `pkg`-only pull request triggers only `ci-docker.yml`.

Two integration tests lock the runtime behaviour in, both using a pgx
`QueryTracer` that cancels the request context the moment a chosen
statement completes and then records the context the `ROLLBACK` runs
on: `server/src/internal/database/rollback_context_integration_test.go`
drives `UnacknowledgeAlert`, and
`server/src/internal/tools/transaction_rollback_integration_test.go`
drives `BeginReadOnlyTx` and `BeginTx`. Both assert the rollback
context is not cancelled, carries a deadline within `rollback.Timeout`
of the call, and that the pooled backend PID is unchanged afterwards.

The contributor-facing summary of the same rule lives under "Go Code"
in `docs/developer-guide/contributing.md`.
