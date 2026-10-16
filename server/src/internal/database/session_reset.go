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
// released connection. DISCARD ALL and DEALLOCATE ALL are cheap, so a
// connection that cannot complete both within this time is unhealthy
// and is better destroyed than returned to the pool.
const sessionResetTimeout = 5 * time.Second

// resetSessionOnRelease is the pgxpool AfterRelease hook for pools on
// monitored databases (issue #709). Tool calls share pooled
// connections, so session state that one call leaves behind (a SET or
// set_config(..., false), a temporary table, a prepared statement, a
// LISTEN, an advisory lock) would otherwise apply to whichever caller
// borrows the connection next. Returning false tells pgxpool to
// destroy the connection rather than reuse it, which is the right
// outcome whenever the reset cannot be confirmed.
//
// pgxpool runs this hook in its own goroutine after Release returns,
// so the reset adds no latency to the releasing caller; the connection
// simply rejoins the pool one round trip or two later.
func resetSessionOnRelease(conn *pgx.Conn) bool {
	ctx, cancel := context.WithTimeout(context.Background(), sessionResetTimeout)
	defer cancel()

	if err := resetSession(ctx, conn); err != nil {
		log.Printf("[WARN] Discarding pooled database connection: %v", err)
		return false
	}
	return true
}

// resetSession returns conn to the state of a freshly opened session.
//
// DISCARD ALL resets every setting to its session default (which keeps
// the startup parameters, such as default_transaction_read_only and
// application_name, that the pool sets), drops temporary tables,
// closes cursors, unlistens, releases advisory locks and deallocates
// every prepared statement. pgx does not notice the last of these: its
// statement and description caches would still name statements the
// server has dropped, and the next cached query would fail. DeallocateAll
// clears those caches; its own DEALLOCATE ALL is redundant after
// DISCARD ALL, but pgx offers no way to clear the caches without it.
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

	if _, err := conn.PgConn().Exec(ctx, "DISCARD ALL").ReadAll(); err != nil {
		return fmt.Errorf("DISCARD ALL failed: %w", err)
	}

	if err := conn.DeallocateAll(ctx); err != nil {
		return fmt.Errorf("clearing the statement cache failed: %w", err)
	}

	return nil
}
