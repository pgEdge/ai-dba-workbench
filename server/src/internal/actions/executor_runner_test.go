/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package actions

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type execFunc = func(ctx context.Context, sql string) error

// newRunner returns a stmtRunner on a connection acquired from the
// fixture's target, with exec wrapped by wrap when it is not nil.
func (f *execFixture) newRunner(t *testing.T, ctx context.Context, wrap func(execFunc) execFunc) *stmtRunner {
	t.Helper()
	conn, err := f.target.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Release)
	pgConn := conn.Conn().PgConn()
	exec := execFunc(func(c context.Context, sql string) error {
		return pgConn.Exec(c, sql).Close()
	})
	if wrap != nil {
		exec = wrap(exec)
	}
	return &stmtRunner{
		ctx: ctx, execCtx: context.Background(), pgConn: pgConn, rowLimit: 10,
		settings: "SET lock_timeout = 1000", exec: exec,
	}
}

func TestRunnerStopAndSettingsFailure(t *testing.T) {
	t.Parallel()
	f := newExecFixture(t, "_x_run1", time.Second, time.Minute, 10)
	stopped, cancel := context.WithCancel(context.Background())
	cancel()
	r := f.newRunner(t, stopped, nil)
	results := newResults([]string{"SELECT 1"})
	if ran, err := r.step(results, 0); ran || err != nil || results[0].Error != stoppedMessage {
		t.Fatalf("stopped step: ran=%v err=%v %+v", ran, err, results[0])
	}

	r = f.newRunner(t, context.Background(), func(execFunc) execFunc {
		return func(context.Context, string) error { return errors.New("boom") }
	})
	results = newResults([]string{"SELECT 1"})
	if ran, _ := r.step(results, 0); ran || !strings.Contains(results[0].Error, "timeouts") {
		t.Fatalf("settings failure: %+v", results[0])
	}
	st := &runState{results: newResults([]string{"SELECT 1"}), transactional: true}
	r.runTransaction(st)
	if !strings.Contains(st.results[0].Error, "start a transaction") {
		t.Fatalf("begin failure: %+v", st.results)
	}
}

func TestRunnerConnectionLostIsUnknown(t *testing.T) {
	t.Parallel()
	f := newExecFixture(t, "_x_run2", time.Second, time.Minute, 10)
	// exec reports success so the statement itself is sent on a closed
	// connection, which fails without a server verdict.
	r := f.newRunner(t, context.Background(), func(execFunc) execFunc {
		return func(context.Context, string) error { return nil }
	})
	if err := r.pgConn.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := &runState{results: newResults([]string{"SELECT 1", "SELECT 2"})}
	r.runIndividually(st)
	if !st.unknown || st.results[0].Applied || !strings.Contains(st.results[0].Error, "unknown") {
		t.Fatalf("lost connection: %+v unknown=%v", st.results, st.unknown)
	}
}

func TestRunnerCommitWithoutVerdictIsUnknown(t *testing.T) {
	t.Parallel()
	f := newExecFixture(t, "_x_run3", time.Second, time.Minute, 10)
	r := f.newRunner(t, context.Background(), func(real execFunc) execFunc {
		return func(c context.Context, sql string) error {
			if sql == "COMMIT" {
				return errors.New("connection reset")
			}
			return real(c, sql)
		}
	})
	st := &runState{results: newResults([]string{"SELECT 1"}), transactional: true}
	r.runTransaction(st)
	if !st.unknown || len(st.results) != 2 || st.results[1].Statement != "COMMIT" || st.results[0].Applied {
		t.Fatalf("commit without verdict: %+v unknown=%v", st.results, st.unknown)
	}
}

func TestRunnerOpenTransactionIsRolledBack(t *testing.T) {
	t.Parallel()
	f := newExecFixture(t, "_x_run4", time.Second, time.Minute, 10)
	r := f.newRunner(t, context.Background(), nil)
	if err := r.exec(context.Background(), "BEGIN"); err != nil {
		t.Fatal(err)
	}
	st := &runState{results: newResults([]string{"SELECT 1", "SELECT 2"})}
	r.runIndividually(st)
	if st.results[0].Applied || st.results[0].Error == "" || r.pgConn.TxStatus() != 'I' {
		t.Fatalf("open transaction: %+v tx=%c", st.results, r.pgConn.TxStatus())
	}
}

func TestRunnerStopBetweenStatementsIsNotUnknown(t *testing.T) {
	t.Parallel()
	f := newExecFixture(t, "_x_run5", time.Second, time.Minute, 10)
	stopped, cancel := context.WithCancel(context.Background())
	cancel()
	r := f.newRunner(t, stopped, nil)
	st := &runState{results: newResults([]string{"SELECT 1"})}
	r.runIndividually(st)
	if st.unknown || st.results[0].Error != stoppedMessage {
		t.Fatalf("stop between statements: %+v", st)
	}
}

func TestClearAppliedClearsRows(t *testing.T) {
	t.Parallel()
	rs := []StatementResult{{Applied: true, CommandTag: "INSERT 0 1", RowsAffected: 1,
		Columns: []string{"a"}, Rows: [][]string{{"1"}}, Truncated: true}}
	clearApplied(rs)
	if rs[0].Applied || rs[0].Columns != nil || rs[0].Rows != nil || rs[0].Truncated || rs[0].CommandTag != "" {
		t.Fatalf("not cleared: %+v", rs[0])
	}
}

func TestRollbackQuietlyLogsFailure(t *testing.T) {
	t.Parallel()
	rollbackQuietly(context.Background(), func(context.Context, string) error { return errors.New("gone") })
}

func TestExecuteMissingAction(t *testing.T) {
	t.Parallel()
	store, _, _ := newTestStore(t, "_x_miss")
	e := NewExecutor(store, nil, time.Second, time.Minute, 1)
	e.execute(context.Background(), &runningAction{}, 987654)
}
