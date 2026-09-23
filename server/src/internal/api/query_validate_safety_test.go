/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// safetyVictimTable is the table the statement-smuggling payloads try
// to drop; it must survive every validation request.
const safetyVictimTable = "query_validate_victim"

// TestValidateQuery_SmuggledCommandsAreNeverRun sends SQL that the
// statement splitter cannot divide correctly, carrying a COMMIT that
// would end the read-only transaction and a DROP TABLE after it. The
// extended query protocol refuses to prepare more than one command,
// so none of it may run and none of it may be reported as valid.
func TestValidateQuery_SmuggledCommandsAreNeverRun(t *testing.T) {
	h, pool, target, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	ctx := context.Background()
	connID := seedQueryExecConnection(t, pool, target, target.host, target.port)

	payloads := map[string]string{
		"quoted identifier with an apostrophe and a commented placeholder": "-- $1\n" +
			`SELECT 1 AS "a'b"; COMMIT; DROP TABLE ` + safetyVictimTable,
		"quoted identifier with an apostrophe": `SELECT 1 AS "a'b"; COMMIT; DROP TABLE ` +
			safetyVictimTable,
		"escaped quote in an E literal": `SELECT E'\''; COMMIT; DROP TABLE ` +
			safetyVictimTable,
		"escaped quote in an E literal with a placeholder": `SELECT E'\'', $1; COMMIT; DROP TABLE ` +
			safetyVictimTable,
	}

	for name, payload := range payloads {
		t.Run(name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, "CREATE TABLE IF NOT EXISTS "+
				safetyVictimTable+" (a int)"); err != nil {
				t.Fatalf("failed to create the victim table: %v", err)
			}
			defer func() {
				_, _ = pool.Exec(context.Background(),
					"DROP TABLE IF EXISTS "+safetyVictimTable)
			}()

			body, err := json.Marshal(map[string]string{"query": payload})
			if err != nil {
				t.Fatalf("failed to encode the request: %v", err)
			}
			rec := postValidate(t, h, connID, string(body))

			if rec.Code == http.StatusOK {
				resp := decodeValidate(t, rec)
				if resp.Valid {
					t.Errorf("valid = true for a smuggled COMMIT (statements %+v)",
						resp.Statements)
				}
			}

			var exists bool
			if err := pool.QueryRow(ctx,
				"SELECT to_regclass($1) IS NOT NULL",
				safetyVictimTable).Scan(&exists); err != nil {
				t.Fatalf("failed to look up the victim table: %v", err)
			}
			if !exists {
				t.Fatalf("the victim table was dropped: validation ran a smuggled command")
			}
		})
	}
}

// slowPlanFunction is an IMMUTABLE function the planner folds into a
// constant, so planning a call to it runs its pg_sleep.
const slowPlanFunction = "query_validate_slow_plan"

// createSlowPlanFunction creates slowPlanFunction and registers its
// removal with the test.
func createSlowPlanFunction(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	if _, err := pool.Exec(context.Background(),
		"CREATE OR REPLACE FUNCTION "+slowPlanFunction+
			"() RETURNS int IMMUTABLE LANGUAGE plpgsql AS "+
			"$$BEGIN PERFORM pg_sleep(2); RETURN 1; END$$"); err != nil {
		t.Fatalf("failed to create the slow planning function: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			"DROP FUNCTION IF EXISTS "+slowPlanFunction+"()")
	})
}

// TestValidateStatement_TimeoutIsNotInvalid covers a statement that
// hits statement_timeout whilst it is being planned: running out of
// time says nothing about the SQL, so it is reported as unsupported
// rather than as a rejection the repair loop would try to fix.
func TestValidateStatement_TimeoutIsNotInvalid(t *testing.T) {
	_, pool, _, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	createSlowPlanFunction(t, pool)

	ctx := context.Background()

	poolConn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("failed to acquire a connection: %v", err)
	}
	defer poolConn.Release()

	tx, err := poolConn.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout = '100ms'"); err != nil {
		t.Fatalf("failed to set the statement timeout: %v", err)
	}

	got := validateStatement(ctx, tx, poolConn.Conn().PgConn(),
		"SELECT "+slowPlanFunction+"()", false)
	assertStatement(t, got, validationUnsupported, "timed out")

	// The savepoint must have unwound the cancellation, leaving the
	// transaction usable for the next statement.
	next := validateStatement(ctx, tx, poolConn.Conn().PgConn(), "SELECT 1", false)
	assertStatement(t, next, validationValid, "")
}

// TestValidateStatements_RequestTimeoutIsAnError covers the whole
// request running out of time part-way through: the statements that
// were never checked must not be reported as valid, so the request
// fails instead.
func TestValidateStatements_RequestTimeoutIsAnError(t *testing.T) {
	_, pool, _, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	createSlowPlanFunction(t, pool)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	results, err := validateStatements(ctx, pool,
		[]string{"SELECT " + slowPlanFunction + "()", "SELECT 1"})
	if err == nil {
		t.Fatalf("validateStatements succeeded after the request timed out: %+v",
			results)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want one wrapping context.DeadlineExceeded", err)
	}
}

// TestValidationInterrupted covers the checks made after each
// statement.
func TestValidationInterrupted(t *testing.T) {
	stopped, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name     string
		ctx      context.Context
		txStatus byte
		wantErr  bool
	}{
		{"inside the transaction", context.Background(), 'T', false},
		{"inside a failed transaction", context.Background(), 'E', false},
		{"transaction ended", context.Background(), txStatusIdle, true},
		{"request ended", stopped, 'T', true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validationInterrupted(tt.ctx, tt.txStatus)
			if (err != nil) != tt.wantErr {
				t.Errorf("validationInterrupted() = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidateStatement_SavepointRejected covers a transaction that is
// already aborted, where the savepoint itself is refused: the
// statement cannot be checked, so it is reported as unsupported rather
// than as valid or invalid.
func TestValidateStatement_SavepointRejected(t *testing.T) {
	_, pool, _, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	ctx := context.Background()
	poolConn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("failed to acquire a connection: %v", err)
	}
	defer poolConn.Release()

	tx, err := poolConn.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	if _, err := tx.Exec(ctx, "SELECT 1/0"); err == nil {
		t.Fatal("expected the division by zero to fail")
	}

	got := validateStatement(ctx, tx, poolConn.Conn().PgConn(), "SELECT 1", false)
	assertStatement(t, got, validationUnsupported, "savepoint")
}

// TestValidateStatements_ClosedPool covers a pool that can no longer
// hand out a connection, which is an error rather than a result.
func TestValidateStatements_ClosedPool(t *testing.T) {
	_, pool, _, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	cfg := pool.Config()
	closed, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("failed to open a second pool: %v", err)
	}
	closed.Close()

	if _, err := validateStatements(context.Background(), closed,
		[]string{"SELECT 1"}); err == nil {
		t.Fatal("validateStatements succeeded on a closed pool")
	}
}
