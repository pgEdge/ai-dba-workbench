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
	"sort"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/pgedge/ai-workbench/pkg/rollback"
)

// errInjectedStatement is the error failingTx returns in place of the
// statement it has been told to fail.
var errInjectedStatement = errors.New("injected statement failure")

// failingTx wraps a real transaction and fails the failAt'th statement
// (counting from one) sent through Exec, Query or QueryRow, passing every
// other statement through to the database. The migrations never see a
// real database error that way, so the transaction stays usable after
// the injected failure and the statements the error path runs next (a
// ROLLBACK TO SAVEPOINT, for example) behave as they would in production.
type failingTx struct {
	pgx.Tx
	failAt int
	calls  int
}

// fail counts a statement and reports whether it is the one to fail.
func (f *failingTx) fail() bool {
	f.calls++
	return f.calls == f.failAt
}

func (f *failingTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if f.fail() {
		return pgconn.CommandTag{}, errInjectedStatement
	}
	return f.Tx.Exec(ctx, sql, args...)
}

func (f *failingTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.fail() {
		return nil, errInjectedStatement
	}
	return f.Tx.Query(ctx, sql, args...)
}

func (f *failingTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.fail() {
		return failedRow{}
	}
	return f.Tx.QueryRow(ctx, sql, args...)
}

// failedRow is the row QueryRow returns for an injected failure.
type failedRow struct{}

func (failedRow) Scan(...any) error { return errInjectedStatement }

// TestMigrationsHandleEachStatementFailure applies every registered
// migration in order against a clean database, and before applying each
// one runs it again and again in a transaction that is rolled back, each
// time failing the next statement it issues. That drives the error
// branch after every statement in every migration, which no real
// database would reach on demand, and checks that each one either
// returns an error or deliberately carries on (the optional pgvector
// setup logs its failure and continues, for example). Stepping stops
// once a run issues fewer statements than the one it was told to fail.
func TestMigrationsHandleEachStatementFailure(t *testing.T) {
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	cleanupTestSchema(t, pool)
	defer cleanupTestSchema(t, pool)

	ctx := context.Background()
	sm := NewSchemaManager()
	sort.Slice(sm.migrations, func(i, j int) bool {
		return sm.migrations[i].Version < sm.migrations[j].Version
	})

	// Migrate records each migration in schema_version, which migration
	// 1 creates; record them here the same way so the schema is left as
	// Migrate would leave it.
	const recordVersionSQL = `INSERT INTO schema_version (version, description) VALUES ($1, $2) ON CONFLICT (version) DO NOTHING`

	for _, m := range sm.migrations {
		failures := 0
		for failAt := 1; ; failAt++ {
			tx, err := conn.Begin(ctx)
			if err != nil {
				t.Fatalf("migration %d: begin: %v", m.Version, err)
			}
			ftx := &failingTx{Tx: tx, failAt: failAt}
			upErr := m.Up(ftx)
			if err := rollback.Tx(ctx, tx); err != nil {
				t.Fatalf("migration %d, statement %d: rollback: %v",
					m.Version, failAt, err)
			}
			if ftx.calls < failAt {
				// This run finished before reaching the statement it
				// was told to fail, so every statement has been tried.
				if upErr != nil {
					t.Fatalf("migration %d failed without an injected failure: %v",
						m.Version, upErr)
				}
				break
			}
			if upErr != nil {
				if !errors.Is(upErr, errInjectedStatement) {
					t.Errorf("migration %d, statement %d: error %v does not wrap the injected failure",
						m.Version, failAt, upErr)
				}
				failures++
			}
		}
		if failures == 0 {
			t.Errorf("migration %d returned no error for any failed statement", m.Version)
		}

		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatalf("migration %d: begin: %v", m.Version, err)
		}
		if err := m.Up(tx); err != nil {
			if rbErr := rollback.Tx(ctx, tx); rbErr != nil {
				t.Logf("migration %d: rollback: %v", m.Version, rbErr)
			}
			t.Fatalf("migration %d: apply: %v", m.Version, err)
		}
		if _, err := tx.Exec(ctx, recordVersionSQL, m.Version, m.Description); err != nil {
			if rbErr := rollback.Tx(ctx, tx); rbErr != nil {
				t.Logf("migration %d: rollback: %v", m.Version, rbErr)
			}
			t.Fatalf("migration %d: record version: %v", m.Version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("migration %d: commit: %v", m.Version, err)
		}
	}
}
