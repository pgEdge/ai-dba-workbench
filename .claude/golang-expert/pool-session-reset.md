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

The server's pools on monitored databases (`Client.ConnectTo` in
`server/src/internal/database/connection.go`) are shared by every MCP
tool call. Session state one call sets (`SET`, `set_config(..., false)`,
temporary tables, prepared statements, `LISTEN`, advisory locks) would
otherwise be seen by the next caller to borrow the connection (issue
#709).

## The Hook

`poolConfig.AfterRelease = resetSessionOnRelease`
(`server/src/internal/database/session_reset.go`). It runs
`DISCARD ALL` and then `conn.DeallocateAll`, bounded by
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

## Pools Without the Hook

`openQueryPool` (`internal/api/query_handlers.go`, one connection per
request) and `Datastore.ListDatabases` (temporary pool) close their
pools after one use, so no state can reach another caller. The
`Datastore` pool is the Workbench's own database, not a monitored one.
A new long-lived pool on a monitored database must set the hook.
