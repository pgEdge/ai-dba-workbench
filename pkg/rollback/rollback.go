/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package rollback holds the project-wide convention for rolling back
// pgx transactions, settled in issue #381.
//
// A rollback is an unwind path, not part of the caller's work, so it
// must not inherit the caller's cancellation. pgx v5 treats a rollback
// issued on a cancelled context as a failed rollback and calls
// conn.die(), discarding the pooled connection; the server tears the
// open transaction down when the socket closes, so the cost is
// connection churn (and the pool losing that slot until it redials)
// rather than a leak, but every request that is cancelled
// mid-transaction pays it.
//
// A bare context.Background() avoids the churn but is deadline-free,
// and pgconn skips its context watcher when the context is literally
// Background(), so a peer that acknowledges the ROLLBACK but never
// answers would pin the pool slot and the calling goroutine with
// nothing but TCP keep-alive to break it. Context therefore returns a
// context that is both non-cancelable and bounded: it keeps the
// caller's values (tracing, logging) through context.WithoutCancel and
// expires after Timeout.
//
// The timeout starts when Context is called, so it must be created at
// the moment of the rollback, never at Begin time. Tx and ToSavepoint
// do this for the two shapes of rollback in the tree; Context is for
// anything else. Every module runs Scan over its own source tree in a
// test, so a rollback that bypasses this package fails CI.
//
// Related history: jackc/pgx#2470 tracked a close-of-closed-channel
// panic in the same die() path against v5.7.0 and is closed.
package rollback

import (
	"context"
	"fmt"
	"regexp"
	"time"
)

// Timeout bounds every rollback issued through this package.
const Timeout = 5 * time.Second

// Rollbacker is the subset of pgx.Tx that Tx needs.
type Rollbacker interface {
	Rollback(ctx context.Context) error
}

// Execer is the subset of pgx.Tx that ToSavepoint needs. T is the
// command-tag type returned by Exec (pgconn.CommandTag for pgx), kept
// generic so this package does not depend on pgx.
type Execer[T any] interface {
	Exec(ctx context.Context, sql string, args ...any) (T, error)
}

// savepointName matches an unquoted SQL identifier, which is the only
// form of savepoint name this package will interpolate into SQL.
var savepointName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Context returns a context for rolling back work that began under
// ctx: it keeps ctx's values but not its cancellation, and expires
// after Timeout. Callers must call cancel once the rollback returns.
func Context(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), Timeout)
}

// Tx rolls tx back on Context(ctx) and returns the rollback's error.
// It is safe to defer directly, because the timeout only starts when
// the deferred call runs:
//
//	defer rollback.Tx(ctx, tx) //nolint:errcheck // no-op after commit
//
// After a successful Commit the rollback returns pgx.ErrTxClosed,
// which deferred callers ignore.
func Tx(ctx context.Context, tx Rollbacker) error {
	rbCtx, cancel := Context(ctx)
	defer cancel()
	return tx.Rollback(rbCtx)
}

// ToSavepoint issues ROLLBACK TO SAVEPOINT name on Context(ctx). The
// name must be an unquoted SQL identifier; anything else is rejected
// before any SQL is sent. The matching SAVEPOINT and RELEASE SAVEPOINT
// statements are part of the caller's work and keep the caller's ctx.
func ToSavepoint[T any](ctx context.Context, tx Execer[T], name string) error {
	if !savepointName.MatchString(name) {
		return fmt.Errorf("invalid savepoint name %q", name)
	}
	rbCtx, cancel := Context(ctx)
	defer cancel()
	_, err := tx.Exec(rbCtx, "ROLLBACK TO SAVEPOINT "+name)
	return err
}
