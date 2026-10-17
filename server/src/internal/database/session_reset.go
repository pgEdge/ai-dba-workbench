/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package database

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
)

// sessionResetTimeout bounds the work resetSessionOnRelease does on a
// released connection. Each reset step is a cheap statement, so a
// connection that cannot complete them all within this time is
// unhealthy and is better destroyed than returned to the pool.
const sessionResetTimeout = 5 * time.Second

// resetSessionOnRelease is the pgxpool AfterRelease hook for pools on
// monitored databases (issue #709). Tool calls made with the same
// token on the same connection share a pool (see ClientManager), so
// session state that one call leaves behind (a SET or
// set_config(..., false), a temporary table, a prepared statement, a
// LISTEN, an advisory lock) would otherwise apply to whichever caller
// borrows the connection next. Returning false tells pgxpool to
// destroy the connection rather than reuse it, which is the right
// outcome whenever the reset cannot be confirmed.
//
// pgxpool runs this hook in its own goroutine after Release returns,
// so the reset adds no latency to the releasing caller; the connection
// simply rejoins the pool two round trips later.
func resetSessionOnRelease(conn *pgx.Conn) bool {
	ctx, cancel := context.WithTimeout(context.Background(), sessionResetTimeout)
	defer cancel()

	if err := resetSession(ctx, conn); err != nil {
		log.Printf("[WARN] Discarding pooled database connection: %v", err)
		return false
	}
	return true
}

// sessionResetStep is one statement, or pgx call, of resetSession.
type sessionResetStep struct {
	what string
	run  func(context.Context, *pgx.Conn) error
}

// sessionResetSteps run in order; the first failure stops the reset.
//
// DISCARD ALL resets every setting to its session default (which keeps
// the startup parameters, such as default_transaction_read_only and
// application_name, that the pool sets), resets the role, drops
// temporary tables, closes cursors (WITH HOLD included), unlistens,
// releases advisory locks, forgets sequence currval state and
// deallocates every prepared statement. pgx does not notice the last
// of these: its statement and description caches would still name
// statements the server has dropped, and the next cached query would
// fail. DeallocateAll clears those caches; its own DEALLOCATE ALL is
// redundant after DISCARD ALL, but pgx offers no way to clear the
// caches without it.
//
// DISCARD ALL does not undo LOAD, nor any backend-local state an
// extension keeps for itself (orafce or pg_variables, for example), and
// it does not close dblink or postgres_fdw connections to other
// servers. Such state can carry over only between calls that share a
// pool, which ClientManager keys by token and connection, and lasts no
// longer than the pool's MaxConnLifetime (pgx's default of one hour,
// which ConnectTo does not override).
var sessionResetSteps = []sessionResetStep{
	{"DISCARD ALL", discardAll},
	{"clearing the statement cache", func(ctx context.Context, conn *pgx.Conn) error {
		return conn.DeallocateAll(ctx)
	}},
}

// resetSession returns conn to the state of a freshly opened session,
// or returns an error if it cannot.
//
// DISCARD ALL cannot run inside a transaction block. pgxpool already
// destroys a connection released in an open or failed transaction
// before calling AfterRelease, so the transaction status check here is
// a guard in case pgxpool stops doing so, rather than a path the pool
// is expected to take.
func resetSession(ctx context.Context, conn *pgx.Conn) error {
	if status := conn.PgConn().TxStatus(); status != 'I' {
		return fmt.Errorf("connection is not idle (transaction status %q)", status)
	}

	for _, step := range sessionResetSteps {
		if err := step.run(ctx, conn); err != nil {
			return fmt.Errorf("%s failed: %w", step.what, err)
		}
	}
	return nil
}

func discardAll(ctx context.Context, conn *pgx.Conn) error {
	_, err := conn.PgConn().Exec(ctx, "DISCARD ALL").ReadAll()
	return err
}
