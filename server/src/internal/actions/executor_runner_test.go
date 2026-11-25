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

	"github.com/jackc/pgx/v5/pgconn"
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

// TestRunnerStopWhileSettingTimeouts covers a stop request that arrives
// after the timeouts are set and before the statement is sent. The
// backend is idle then, so a cancel request would be lost; instead the
// statement must not be sent at all.
func TestRunnerStopWhileSettingTimeouts(t *testing.T) {
	t.Parallel()
	f := newExecFixture(t, "_x_run6", time.Second, time.Minute, 10)
	mustExec(t, f.target, "CREATE TABLE t (id int)")
	for _, transactional := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		r := f.newRunner(t, ctx, nil)
		stop := context.AfterFunc(ctx, r.cancelInFlight)
		r.beforeDispatch = cancel
		st := &runState{results: newResults([]string{"INSERT INTO t VALUES (1)"}), transactional: transactional}
		r.state = st
		if transactional {
			r.runTransaction(st)
		} else {
			r.runIndividually(st)
		}
		stop()
		if st.results[0].Error != stoppedMessage || st.results[0].Applied || st.sent || st.unknown {
			t.Errorf("transactional=%v: %+v sent=%v", transactional, st.results, st.sent)
		}
		if n := f.count(t, "t"); n != 0 {
			t.Errorf("transactional=%v: the statement ran: %d rows", transactional, n)
		}
	}
}

// TestRunnerStopBeforeCommitRollsBack proves a stop request that arrives
// after the last statement of a transaction, when a cancel request has
// nothing to cancel, rolls the transaction back rather than committing.
func TestRunnerStopBeforeCommitRollsBack(t *testing.T) {
	t.Parallel()
	f := newExecFixture(t, "_x_run7", time.Second, time.Minute, 10)
	mustExec(t, f.target, "CREATE TABLE t (id int)")
	ctx, cancel := context.WithCancel(context.Background())
	r := f.newRunner(t, ctx, nil)
	if err := r.exec(context.Background(), "BEGIN"); err != nil {
		t.Fatal(err)
	}
	if err := r.exec(context.Background(), "INSERT INTO t VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	cancel()
	st := &runState{results: newResults([]string{"INSERT INTO t VALUES (1)"}), transactional: true}
	r.commit(st)
	if len(st.results) != 2 || st.results[1].Error != stoppedBeforeCommitMessage ||
		st.results[0].Applied || st.unknown {
		t.Fatalf("results = %+v", st.results)
	}
	if n := f.count(t, "t"); n != 0 || r.pgConn.TxStatus() != 'I' {
		t.Fatalf("rows = %d, tx = %c", n, r.pgConn.TxStatus())
	}
}

// TestRunnerCancelInFlightIdle proves cancelInFlight sends nothing when
// no statement is in flight.
func TestRunnerCancelInFlightIdle(t *testing.T) {
	t.Parallel()
	r := &stmtRunner{}
	r.cancelInFlight() // a nil connection would panic if it were used
}

// TestOutcomeUnknownTimeoutAfterStop proves a 57014 error counts as the
// stop's, and so leaves the outcome unknown, only when a cancel request
// was sent whilst the statement was in flight. PostgreSQL reports a
// statement timeout with the same code, so a stop that arrives after a
// statement has already timed out must leave the timeout, and its known
// outcome, in place.
func TestOutcomeUnknownTimeoutAfterStop(t *testing.T) {
	t.Parallel()
	timeout := &pgconn.PgError{Code: "57014", Message: "canceling statement due to statement timeout"}
	for _, tt := range []struct {
		name        string
		cancelSent  bool
		wantUnknown bool
	}{
		{"stop after the timeout", false, false},
		{"cancel sent in flight", true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			r := &stmtRunner{ctx: ctx, cancelSent: tt.cancelSent}
			st := &runState{results: newResults([]string{"UPDATE t SET a = 1"})}
			st.results[0].Error = timeout.Message
			got := r.outcomeUnknown(st, 0, true, timeout)
			if got != tt.wantUnknown || st.unknown != tt.wantUnknown {
				t.Fatalf("outcomeUnknown = %v, st.unknown = %v, want %v", got, st.unknown, tt.wantUnknown)
			}
			if !tt.wantUnknown && st.results[0].Error != timeout.Message {
				t.Errorf("error = %q, want the timeout kept", st.results[0].Error)
			}
		})
	}
}
