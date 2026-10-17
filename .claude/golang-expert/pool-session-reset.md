/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Pool Session Reset
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

# Session Reset on Monitored-Database Pools

Every pool built by `Client.ConnectTo`
(`server/src/internal/database/connection.go`) carries the hook below:
the monitored-database pools, which `ClientManager` keys by token and
connection so that they are shared by MCP tool calls with the same
token on the same connection, and the fallback pool from
`Client.Connect()`. Session state one call sets (`SET`,
`set_config(..., false)`, `SET ROLE`, temporary tables, prepared
statements, cursors, `LISTEN`, advisory locks) would otherwise be seen
by the next call to borrow the connection (issue #709).

## The Hook

`poolConfig.AfterRelease = resetSessionOnRelease`
(`server/src/internal/database/session_reset.go`). It runs
`sessionResetSteps`, `DISCARD ALL` and then `conn.DeallocateAll`,
bounded by
`sessionResetTimeout`, and returns false (pgxpool destroys the
connection) on any failure.

- `DISCARD ALL` resets settings to their session defaults, which
  include startup parameters, so `default_transaction_read_only=on`
  and `application_name` survive. Put any per-connection setting that
  must persist in `ConnConfig.RuntimeParams`, never in an
  `AfterConnect` `SET`, or the reset will undo it.
- `DeallocateAll` is required: pgx does not notice `DISCARD ALL`, and
  its statement cache would otherwise name dropped statements, failing
  the next cached query with SQLSTATE 26000. The integration test
  `TestSessionResetOnRelease_ClearsSessionState` catches this.
- pgxpool's `Conn.Release` destroys a connection in an open or failed
  transaction before calling `AfterRelease`; `resetSession` checks the
  transaction status again as a guard.
- pgxpool runs the hook in a goroutine, so the releasing caller does
  not wait; the cost is two round trips per release, and statements
  are re-prepared after every acquisition.

## What the Reset Does Not Clear

`DISCARD ALL` does not close named dblink connections or the
postgres_fdw connection cache (dblink can connect inside a read-only
transaction), and does not undo `LOAD` or state an extension keeps in
backend memory (orafce, pg_variables). These can pass between calls by
the same token on the same connection. A check that destroyed every
connection on a database with dblink or postgres_fdw installed was
rejected because it would disable pooling there; instead, the state
lives no longer than `MaxConnLifetime`, pgx's default of one hour,
which `ConnectTo` deliberately leaves in place
(`TestConnectToAppliesPoolConfig` pins it).

## Statement Timeouts

Tools set `statement_timeout` with `SET LOCAL` inside their
transaction (`internal/tools/transaction.go`), so the reset does not
affect them. A plain `SET` applied once per connection would be wiped
by the reset; apply such settings per acquisition or as a startup
parameter.

## Pools Without the Hook

`openQueryPool` (`internal/api/query_handlers.go`, one connection per
request) and `Datastore.ListDatabases` (temporary pool) close their
pools after one use, so no state can reach another caller. The
`Datastore` pool is the Workbench's own database, not a monitored one.
A new long-lived pool on a monitored database must set the hook.
