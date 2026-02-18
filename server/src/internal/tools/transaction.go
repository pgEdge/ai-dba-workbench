/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package tools

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
)

// ReadOnlyTx wraps a pgx.Tx with a commit helper that tracks whether
// the transaction has been committed.
type ReadOnlyTx struct {
	Tx        pgx.Tx
	ctx       context.Context
	committed bool
}

// Commit commits the read-only transaction and marks it as committed so
// the deferred rollback becomes a no-op.
func (r *ReadOnlyTx) Commit() error {
	err := r.Tx.Commit(r.ctx)
	if err == nil {
		r.committed = true
	}
	return err
}

// BeginReadOnlyTx starts a read-only transaction on the given pool with
// panic recovery and a statement timeout. The caller must defer the
// returned cleanup function and call Commit() on the ReadOnlyTx when
// the work is done.
//
// On success the returned ReadOnlyTx has:
//   - A deferred cleanup that fires on panic (re-panics after rollback)
//     or when the transaction was not committed.
//   - SET TRANSACTION READ ONLY already applied.
//   - SET LOCAL statement_timeout = '10s' already applied.
//
// When an error occurs during setup, the function returns a non-nil
// errResp containing the MCP error response to return to the caller.
func BeginReadOnlyTx(ctx context.Context, pool *pgxpool.Pool) (rot *ReadOnlyTx, errResp *mcp.ToolResponse, cleanup func()) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		resp, _ := mcp.NewToolError(fmt.Sprintf("Failed to begin transaction: %v", err)) //nolint:errcheck // NewToolError always succeeds
		return nil, &resp, func() {}
	}

	rot = &ReadOnlyTx{Tx: tx, ctx: ctx}

	cleanup = func() {
		if r := recover(); r != nil {
			_ = tx.Rollback(ctx) //nolint:errcheck // Best effort cleanup on panic
			panic(r)
		}
		if !rot.committed {
			_ = tx.Rollback(ctx) //nolint:errcheck // rollback in defer after commit is expected to fail
		}
	}

	// Set transaction to read-only
	_, err = tx.Exec(ctx, "SET TRANSACTION READ ONLY")
	if err != nil {
		_ = tx.Rollback(ctx)                                                                     //nolint:errcheck // cleanup on setup failure
		resp, _ := mcp.NewToolError(fmt.Sprintf("Failed to set transaction read-only: %v", err)) //nolint:errcheck // NewToolError always succeeds
		return nil, &resp, func() {}
	}

	// Defense-in-depth: limit query execution time
	_, err = tx.Exec(ctx, "SET LOCAL statement_timeout = '10s'")
	if err != nil {
		_ = tx.Rollback(ctx)                                                                 //nolint:errcheck // cleanup on setup failure
		resp, _ := mcp.NewToolError(fmt.Sprintf("Failed to set statement timeout: %v", err)) //nolint:errcheck // NewToolError always succeeds
		return nil, &resp, func() {}
	}

	return rot, nil, cleanup
}
