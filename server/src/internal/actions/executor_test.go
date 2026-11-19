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
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/server/internal/testutil/scratchdb"
)

// execFixture holds a store, a scratch target database and an executor
// wired to both.
type execFixture struct {
	store  *Store
	target *pgxpool.Pool
	exec   *Executor
	connID int
}

// newExecFixture brings up the datastore and a separate target database
// whose name is derived from suffix, so concurrent runs do not collide.
func newExecFixture(t *testing.T, suffix string, lock, stmt time.Duration, rowLimit int) *execFixture {
	t.Helper()
	store, _, connID := newTestStore(t, suffix+"_ds")
	target := scratchdb.New(t, suffix+"_tg", 8)
	opener := func(ctx context.Context, _ int, _ *string) (*pgxpool.Pool, error) {
		cfg := target.Config().Copy()
		cfg.MaxConns = 1
		return pgxpool.NewWithConfig(ctx, cfg)
	}
	e := NewExecutor(store, opener, lock, stmt, rowLimit)
	t.Cleanup(e.Wait)
	return &execFixture{store: store, target: target, exec: e, connID: connID}
}

// propose creates a reviewed action ready to execute.
func (f *execFixture) propose(t *testing.T, stmts ...string) int64 {
	t.Helper()
	ctx := context.Background()
	n := sampleAction(f.connID, "alice")
	n.Statements = stmts
	a, err := f.store.Create(ctx, n)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := f.store.SetReview(ctx, a.ID, "passed", Verdict{Decision: "pass", Danger: "low", Reviewer: "test"}); err != nil {
		t.Fatalf("set review: %v", err)
	}
	return a.ID
}

// await polls until the action leaves executing, with a deadline.
func awaitDone(t *testing.T, s *Store, id int64) *Action {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		a, err := s.Get(context.Background(), id)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if a.ExecStatus != "executing" {
			return a
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("action still executing at deadline")
	return nil
}

func (f *execFixture) run(t *testing.T, stmts ...string) *Action {
	t.Helper()
	id := f.propose(t, stmts...)
	if err := f.exec.Start(context.Background(), id); err != nil {
		t.Fatalf("start: %v", err)
	}
	return awaitDone(t, f.store, id)
}

func (f *execFixture) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := f.target.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func mustExec(t *testing.T, p *pgxpool.Pool, sql string) {
	t.Helper()
	if _, err := p.Exec(context.Background(), sql); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func TestExecuteTransactionalRollsBackOnError(t *testing.T) {
	f := newExecFixture(t, "_x_rb", time.Second, time.Minute, 100)
	mustExec(t, f.target, "CREATE TABLE t (a int PRIMARY KEY)")
	a := f.run(t, "INSERT INTO t VALUES (1)", "INSERT INTO t VALUES (1)", "INSERT INTO t VALUES (2)")
	if a.ExecStatus != "failed" || a.Transactional == nil || !*a.Transactional {
		t.Fatalf("status=%s transactional=%v", a.ExecStatus, a.Transactional)
	}
	if n := f.count(t, "t"); n != 0 {
		t.Fatalf("rows after rollback = %d, want 0", n)
	}
	if len(a.Results) != 3 {
		t.Fatalf("results = %d, want 3", len(a.Results))
	}
	for i, r := range a.Results {
		if r.Applied {
			t.Errorf("result %d reported applied", i)
		}
	}
	if a.Results[1].Error == "" || a.Results[0].Error != "" || a.Results[2].Error != "" {
		t.Errorf("error should be on statement 2 only: %+v", a.Results)
	}
}

func TestExecuteTransactionalCommits(t *testing.T) {
	f := newExecFixture(t, "_x_ok", time.Second, time.Minute, 100)
	mustExec(t, f.target, "CREATE TABLE t (a int)")
	a := f.run(t, "INSERT INTO t VALUES (1); INSERT INTO t VALUES (2)", "  ")
	if a.ExecStatus != "succeeded" || a.Transactional == nil || !*a.Transactional {
		t.Fatalf("status=%s transactional=%v results=%+v", a.ExecStatus, a.Transactional, a.Results)
	}
	if n := f.count(t, "t"); n != 2 {
		t.Fatalf("rows = %d, want 2", n)
	}
	if len(a.Results) != 2 || !a.Results[0].Applied || a.Results[0].CommandTag != "INSERT 0 1" || a.Results[0].RowsAffected != 1 {
		t.Fatalf("unexpected results: %+v", a.Results)
	}
}

func TestExecuteNonTransactionalMixed(t *testing.T) {
	f := newExecFixture(t, "_x_nt", time.Second, time.Minute, 100)
	mustExec(t, f.target, "CREATE TABLE t (a int PRIMARY KEY)")
	a := f.run(t, "INSERT INTO t VALUES (1)", "VACUUM t", "INSERT INTO t VALUES (1)", "INSERT INTO t VALUES (3)")
	if a.ExecStatus != "failed" || a.Transactional == nil || *a.Transactional {
		t.Fatalf("status=%s transactional=%v", a.ExecStatus, a.Transactional)
	}
	if n := f.count(t, "t"); n != 1 {
		t.Fatalf("rows = %d, want 1 (first insert kept, later ones not run)", n)
	}
	want := []bool{true, true, false, false}
	if len(a.Results) != 4 {
		t.Fatalf("results = %d, want 4", len(a.Results))
	}
	for i, r := range a.Results {
		if r.Applied != want[i] {
			t.Errorf("result %d applied=%v want %v", i, r.Applied, want[i])
		}
	}
	if a.Results[2].Error == "" || a.Results[3].Error != "" {
		t.Errorf("unexpected errors: %+v", a.Results)
	}
}

func TestExecuteAppliesTimeouts(t *testing.T) {
	f := newExecFixture(t, "_x_to", 200*time.Millisecond, 5*time.Minute, 100)
	ctx := context.Background()
	mustExec(t, f.target, "CREATE TABLE t (a int)")
	holder, err := f.target.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = holder.Rollback(ctx) }() //nolint:errcheck // test cleanup
	if _, err := holder.Exec(ctx, "LOCK TABLE t IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatalf("lock: %v", err)
	}
	a := f.run(t, "INSERT INTO t VALUES (1)")
	if a.ExecStatus != "failed" || len(a.Results) != 1 || !strings.Contains(a.Results[0].Error, "lock timeout") {
		t.Fatalf("expected lock timeout failure, got %s %+v", a.ExecStatus, a.Results)
	}
}

func TestExecuteStatementTimeout(t *testing.T) {
	f := newExecFixture(t, "_x_st", time.Second, 200*time.Millisecond, 100)
	a := f.run(t, "SELECT pg_sleep(30)")
	if a.ExecStatus != "failed" || !strings.Contains(a.Results[0].Error, "statement timeout") {
		t.Fatalf("expected statement timeout, got %s %+v", a.ExecStatus, a.Results)
	}
}

func TestExecuteCapturesTagsAndRows(t *testing.T) {
	f := newExecFixture(t, "_x_rt", time.Second, time.Minute, 2)
	mustExec(t, f.target, "CREATE TABLE t (a int, b text)")
	mustExec(t, f.target, "INSERT INTO t SELECT g, 'v' || g FROM generate_series(1, 5) g")
	a := f.run(t, "UPDATE t SET b = NULL WHERE a <= 4 RETURNING a, b", "SELECT 1 AS one")
	if a.ExecStatus != "succeeded" || len(a.Results) != 2 {
		t.Fatalf("status=%s results=%+v", a.ExecStatus, a.Results)
	}
	r := a.Results[0]
	if r.CommandTag != "UPDATE 4" || r.RowsAffected != 4 {
		t.Errorf("tag=%q rows=%d", r.CommandTag, r.RowsAffected)
	}
	if len(r.Columns) != 2 || r.Columns[0] != "a" || len(r.Rows) != 2 || !r.Truncated {
		t.Errorf("columns/rows/truncated wrong: %+v", r)
	}
	if r.Rows[0][1] != "NULL" {
		t.Errorf("NULL rendering = %q", r.Rows[0][1])
	}
	if s := a.Results[1]; len(s.Rows) != 1 || s.Rows[0][0] != "1" || s.Truncated {
		t.Errorf("select result wrong: %+v", s)
	}
}

func TestExecuteOnce(t *testing.T) {
	f := newExecFixture(t, "_x_once", time.Second, time.Minute, 100)
	id := f.propose(t, "SELECT 1")
	if err := f.exec.Start(context.Background(), id); err != nil {
		t.Fatalf("first start: %v", err)
	}
	if err := f.exec.Start(context.Background(), id); !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("second start = %v, want ErrAlreadyStarted", err)
	}
	awaitDone(t, f.store, id)
	if err := f.exec.Start(context.Background(), id); !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("start after finish = %v, want ErrAlreadyStarted", err)
	}
}

func TestStartRefusesUnreviewed(t *testing.T) {
	f := newExecFixture(t, "_x_unrev", time.Second, time.Minute, 100)
	a, err := f.store.Create(context.Background(), sampleAction(f.connID, "alice"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.exec.Start(context.Background(), a.ID); !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("start = %v, want ErrAlreadyStarted", err)
	}
}

func TestCancel(t *testing.T) {
	f := newExecFixture(t, "_x_cancel", time.Second, time.Minute, 100)
	mustExec(t, f.target, "CREATE TABLE t (a int)")
	id := f.propose(t, "INSERT INTO t VALUES (1)", "SELECT pg_sleep(60)")
	if err := f.exec.Start(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for !f.exec.Cancel(id) {
		if time.Now().After(deadline) {
			t.Fatal("cancel never found the running action")
		}
		time.Sleep(20 * time.Millisecond)
	}
	a := awaitDone(t, f.store, id)
	if a.ExecStatus != "cancelled" { //nolint:misspell // value fixed by the table CHECK
		t.Fatalf("status = %s", a.ExecStatus)
	}
	if n := f.count(t, "t"); n != 0 {
		t.Fatalf("rows = %d, want 0 after a stopped transaction", n)
	}
	if f.exec.Cancel(id) {
		t.Fatal("cancel of a finished action should report false")
	}
}

func TestExecuteOpenerFailure(t *testing.T) {
	store, _, connID := newTestStore(t, "_x_open")
	opener := func(context.Context, int, *string) (*pgxpool.Pool, error) {
		return nil, errors.New("dial postgres://user:secret@host/db failed")
	}
	e := NewExecutor(store, opener, time.Second, time.Minute, 10)
	t.Cleanup(e.Wait)
	ctx := context.Background()
	n := sampleAction(connID, "alice")
	n.Statements = []string{"SELECT 1", "SELECT 2"}
	a, err := store.Create(ctx, n)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetReview(ctx, a.ID, "passed", Verdict{Decision: "pass", Danger: "low", Reviewer: "t"}); err != nil {
		t.Fatal(err)
	}
	if err := e.Start(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	got := awaitDone(t, store, a.ID)
	if got.ExecStatus != "failed" || len(got.Results) != 2 || got.Results[0].Error == "" || got.Results[0].Applied {
		t.Fatalf("unexpected: %s %+v", got.ExecStatus, got.Results)
	}
	if strings.Contains(got.Results[0].Error, "secret") {
		t.Fatalf("credentials leaked: %q", got.Results[0].Error)
	}
}

func TestExecuteRefusesTransactionControlAndEncoding(t *testing.T) {
	f := newExecFixture(t, "_x_guard", time.Second, time.Minute, 100)
	mustExec(t, f.target, "CREATE TABLE t (a int)")

	a := f.run(t, "INSERT INTO t VALUES (1)", "COMMIT")
	if a.ExecStatus != "failed" || a.Results[1].Error == "" || a.Results[0].Applied {
		t.Fatalf("transaction control: %s %+v", a.ExecStatus, a.Results)
	}
	if n := f.count(t, "t"); n != 0 {
		t.Fatalf("rows = %d", n)
	}

	b := f.run(t, "VACUUM t", "SET client_encoding = 'SJIS'", "INSERT INTO t VALUES (2)")
	if b.ExecStatus != "failed" || b.Results[1].Error == "" || b.Results[2].Applied {
		t.Fatalf("encoding guard: %s %+v", b.ExecStatus, b.Results)
	}
	if n := f.count(t, "t"); n != 0 {
		t.Fatalf("rows = %d", n)
	}
}

func TestExecuteRefusesAmbiguousSplit(t *testing.T) {
	// The two readings of a plain quoted literal split this text
	// differently, so it is refused rather than guessed at.
	f := newExecFixture(t, "_x_amb", time.Second, time.Minute, 100)
	a := f.run(t, `SELECT 'a\'; SELECT 2; --'`)
	if a.ExecStatus != "failed" || len(a.Results) != 1 || a.Results[0].Applied || a.Results[0].Error == "" {
		t.Fatalf("ambiguous statement: %s %+v", a.ExecStatus, a.Results)
	}
}

func TestExecuteCommitFailure(t *testing.T) {
	f := newExecFixture(t, "_x_commit", time.Second, time.Minute, 100)
	mustExec(t, f.target, "CREATE TABLE d (a int, CONSTRAINT d_u UNIQUE (a) DEFERRABLE INITIALLY DEFERRED)")
	a := f.run(t, "INSERT INTO d VALUES (1)", "INSERT INTO d VALUES (1)")
	if a.ExecStatus != "failed" || a.Results[1].Error == "" || a.Results[0].Applied || a.Results[1].Applied {
		t.Fatalf("commit failure: %s %+v", a.ExecStatus, a.Results)
	}
	if n := f.count(t, "d"); n != 0 {
		t.Fatalf("rows = %d", n)
	}
}

// runWithOpener runs a reviewed one-statement action through an executor
// built on opener and returns the finished action.
func runWithOpener(t *testing.T, opener Opener, rowLimit int) *Action {
	t.Helper()
	store, _, connID := newTestStore(t, "_x_op")
	e := NewExecutor(store, opener, time.Second, time.Minute, rowLimit)
	t.Cleanup(e.Wait)
	ctx := context.Background()
	n := sampleAction(connID, "alice")
	n.Statements = []string{"SELECT 1"}
	a, err := store.Create(ctx, n)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetReview(ctx, a.ID, "passed", Verdict{Decision: "pass", Danger: "low", Reviewer: "t"}); err != nil {
		t.Fatal(err)
	}
	if err := e.Start(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	return awaitDone(t, store, a.ID)
}

func TestExecuteRecoversFromPanic(t *testing.T) {
	got := runWithOpener(t, func(context.Context, int, *string) (*pgxpool.Pool, error) {
		panic("boom")
	}, 0)
	if got.ExecStatus != "failed" || len(got.Results) != 1 || got.Results[0].Error == "" {
		t.Fatalf("unexpected: %s %+v", got.ExecStatus, got.Results)
	}
}

func TestExecuteAcquireFailure(t *testing.T) {
	got := runWithOpener(t, func(ctx context.Context, _ int, _ *string) (*pgxpool.Pool, error) {
		cfg, err := pgxpool.ParseConfig(os.Getenv("TEST_AI_WORKBENCH_SERVER"))
		if err != nil {
			return nil, err
		}
		p, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			return nil, err
		}
		p.Close()
		return p, nil
	}, 5)
	if got.ExecStatus != "failed" || got.Results[0].Error == "" {
		t.Fatalf("unexpected: %s %+v", got.ExecStatus, got.Results)
	}
}

func TestNewExecutorDefaultRowLimit(t *testing.T) {
	if e := NewExecutor(nil, nil, 0, 0, 0); e.rowLimit != defaultRowLimit {
		t.Fatalf("rowLimit = %d", e.rowLimit)
	}
	if e := NewExecutor(nil, nil, 0, 0, 7); e.rowLimit != 7 {
		t.Fatalf("rowLimit = %d", e.rowLimit)
	}
}

func TestStartStoreError(t *testing.T) {
	store, pool, _ := newTestStore(t, "_x_ste")
	pool.Close()
	e := NewExecutor(store, nil, time.Second, time.Minute, 1)
	if err := e.Start(context.Background(), 1); err == nil || errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("Start = %v, want store error", err)
	}
}

func TestSafeExecError(t *testing.T) {
	if got := safeExecError(context.DeadlineExceeded); got == "" {
		t.Fatal("empty message")
	}
	if got := safeExecError(context.Canceled); got == "" {
		t.Fatal("empty message")
	}
	if got := safeExecError(&pgconn.PgError{Message: "boom"}); got != "boom" {
		t.Fatalf("pg error = %q", got)
	}
	if got := safeExecError(errors.New("password=hunter2")); strings.Contains(got, "hunter2") {
		t.Fatalf("leaked: %q", got)
	}
}

type fakeConn struct {
	enc string
	tx  byte
}

func (f fakeConn) ParameterStatus(string) string { return f.enc }
func (f fakeConn) TxStatus() byte                { return f.tx }

func TestConnStateMessages(t *testing.T) {
	if m := openTransactionMessage(fakeConn{"UTF8", 'I'}); m != "" {
		t.Errorf("idle: %q", m)
	}
	if m := openTransactionMessage(fakeConn{"UTF8", 'T'}); m == "" {
		t.Error("open transaction not reported")
	}
	if m := encodingMessage(fakeConn{"UTF8", 'I'}); m != "" {
		t.Errorf("utf8: %q", m)
	}
	if m := encodingMessage(fakeConn{"SJIS", 'I'}); m == "" {
		t.Error("encoding change not reported")
	}
}
