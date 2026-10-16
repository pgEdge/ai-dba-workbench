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
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// errInjectedStatement is the failure faultTx returns in place of the
// statement it was told to fail.
var errInjectedStatement = errors.New("injected statement failure")

// faultTx wraps a real transaction and fails its failAt'th Exec or
// QueryRow call without sending it to the server, so the transaction
// stays usable and the caller's error handling runs against real state.
type faultTx struct {
	pgx.Tx
	calls    int
	failAt   int
	injected bool
}

func (f *faultTx) fail() bool {
	f.calls++
	if f.calls == f.failAt {
		f.injected = true
		return true
	}
	return false
}

func (f *faultTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if f.fail() {
		return pgconn.CommandTag{}, errInjectedStatement
	}
	return f.Tx.Exec(ctx, sql, args...)
}

func (f *faultTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.fail() {
		return faultRow{}
	}
	return f.Tx.QueryRow(ctx, sql, args...)
}

// faultRow is the row faultTx returns for an injected QueryRow failure.
type faultRow struct{}

func (faultRow) Scan(...any) error { return errInjectedStatement }

// TestMigrationsReportStatementFailures fails each statement of the full
// migration chain in turn, on a fresh datastore inside a transaction that
// is rolled back afterwards, and checks that every failure a migration
// reports carries the underlying error rather than being lost.
func TestMigrationsReportStatementFailures(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	cleanupTestSchema(t, pool)
	defer cleanupTestSchema(t, pool)

	migrations := NewSchemaManager().migrations

	// runChain applies every migration through a faultTx that fails its
	// failAt'th call, and reports whether a failure was injected.
	runChain := func(failAt int) bool {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("failed to begin transaction: %v", err)
		}
		defer func() {
			if err := tx.Rollback(ctx); err != nil {
				t.Fatalf("rollback after failing call %d: %v", failAt, err)
			}
		}()

		ftx := &faultTx{Tx: tx, failAt: failAt}
		for _, m := range migrations {
			if err := m.Up(ftx); err != nil {
				if !errors.Is(err, errInjectedStatement) {
					t.Errorf("migration %d, failing call %d: error %v "+
						"does not wrap the injected failure", m.Version, failAt, err)
				}
				return ftx.injected
			}
		}
		if !ftx.injected {
			return false
		}
		// A migration tolerated the injected failure; the chain still
		// completing is acceptable, but nothing more is left to fail.
		return true
	}

	failed := 0
	for failAt := 1; runChain(failAt); failAt++ {
		failed++
		if failed > 10000 {
			t.Fatal("migration chain never completed without a failure")
		}
	}
	if failed == 0 {
		t.Fatal("no statement of the migration chain was failed")
	}
}
