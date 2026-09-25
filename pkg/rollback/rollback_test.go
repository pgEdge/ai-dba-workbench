/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package rollback

import (
	"context"
	"errors"
	"testing"
	"time"
)

type ctxKey struct{}

// snapshot captures the observable state of a rollback context at the
// moment the rollback is issued, before Tx and ToSavepoint release it.
type snapshot struct {
	taken    bool
	err      error
	value    string
	deadline time.Time
	hasDL    bool
}

func snap(ctx context.Context) snapshot {
	s := snapshot{taken: true, err: ctx.Err()}
	s.value, _ = ctx.Value(ctxKey{}).(string)
	s.deadline, s.hasDL = ctx.Deadline()
	return s
}

// fakeTx snapshots the context it was rolled back on and returns err.
type fakeTx struct {
	seen snapshot
	err  error
}

func (f *fakeTx) Rollback(ctx context.Context) error {
	f.seen = snap(ctx)
	return f.err
}

// fakeExecer snapshots the context and records the SQL of each Exec.
type fakeExecer struct {
	seen snapshot
	sql  string
	err  error
}

func (f *fakeExecer) Exec(ctx context.Context, sql string, _ ...any) (struct{}, error) {
	f.seen = snap(ctx)
	f.sql = sql
	return struct{}{}, f.err
}

// canceledParent returns an already-canceled context carrying a value,
// which is the situation the helper exists for.
func canceledParent() context.Context {
	ctx := context.WithValue(context.Background(), ctxKey{}, "traced")
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	return ctx
}

// assertBounded checks the three properties every rollback context must
// have: not canceled with its parent, carrying the parent's values, and
// expiring within Timeout of when the rollback began (with a second of
// slack for the clock reads either side of the call).
func assertBounded(t *testing.T, got snapshot, start time.Time) {
	t.Helper()
	if !got.taken {
		t.Fatal("rollback was not issued")
	}
	if got.err != nil {
		t.Errorf("rollback context is canceled: %v", got.err)
	}
	if got.value != "traced" {
		t.Errorf("rollback context lost the parent's value: got %q", got.value)
	}
	if !got.hasDL {
		t.Fatal("rollback context has no deadline")
	}
	if remaining := got.deadline.Sub(start); remaining <= 0 || remaining > Timeout+time.Second {
		t.Errorf("deadline %v from start, want within (0, %v]", remaining, Timeout)
	}
}

func TestContext(t *testing.T) {
	start := time.Now()
	got, cancel := Context(canceledParent())
	assertBounded(t, snap(got), start)
	cancel()
	if got.Err() == nil {
		t.Error("cancel did not release the rollback context")
	}
}

func TestTx(t *testing.T) {
	want := errors.New("rollback failed")
	tests := []struct {
		name string
		err  error
	}{
		{name: "success", err: nil},
		{name: "error is returned", err: want},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tx := &fakeTx{err: tc.err}
			start := time.Now()
			if err := Tx(canceledParent(), tx); !errors.Is(err, tc.err) {
				t.Errorf("Tx error = %v, want %v", err, tc.err)
			}
			assertBounded(t, tx.seen, start)
		})
	}
}

func TestToSavepoint(t *testing.T) {
	want := errors.New("exec failed")
	tests := []struct {
		name    string
		spName  string
		err     error
		wantSQL string
	}{
		{name: "success", spName: "pgvector_setup", wantSQL: "ROLLBACK TO SAVEPOINT pgvector_setup"},
		{name: "error is returned", spName: "sp_1", err: want, wantSQL: "ROLLBACK TO SAVEPOINT sp_1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tx := &fakeExecer{err: tc.err}
			start := time.Now()
			if err := ToSavepoint(canceledParent(), tx, tc.spName); !errors.Is(err, tc.err) {
				t.Errorf("ToSavepoint error = %v, want %v", err, tc.err)
			}
			if tx.sql != tc.wantSQL {
				t.Errorf("SQL = %q, want %q", tx.sql, tc.wantSQL)
			}
			assertBounded(t, tx.seen, start)
		})
	}
}

func TestToSavepointRejectsInvalidName(t *testing.T) {
	for _, name := range []string{"", "1abc", "sp; DROP TABLE x", `"quoted"`, "a-b"} {
		tx := &fakeExecer{}
		err := ToSavepoint(context.Background(), tx, name)
		if err == nil {
			t.Errorf("ToSavepoint(%q) returned nil error", name)
		}
		if tx.seen.taken {
			t.Errorf("ToSavepoint(%q) issued SQL %q despite the invalid name", name, tx.sql)
		}
	}
}

func TestSimple(t *testing.T) {
	want := errors.New("simple rollback failed")
	tests := []struct {
		name string
		err  error
	}{
		{name: "success", err: nil},
		{name: "error is returned", err: want},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var seen snapshot
			var sql string
			start := time.Now()
			err := Simple(canceledParent(),
				func(ctx context.Context, statement string) error {
					seen = snap(ctx)
					sql = statement
					return tc.err
				})
			if !errors.Is(err, tc.err) {
				t.Errorf("Simple error = %v, want %v", err, tc.err)
			}
			if sql != "ROLLBACK" {
				t.Errorf("statement = %q, want %q", sql, "ROLLBACK")
			}
			assertBounded(t, seen, start)
		})
	}
}
