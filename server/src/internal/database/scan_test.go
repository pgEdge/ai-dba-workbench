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
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// fakeRows implements pgx.Rows for unit-testing scanAll. Only the methods
// scanAll touches (Next, Scan, Err, Close) carry real behavior; the rest
// satisfy the interface and panic if invoked, which would surface an
// accidental dependency on the unused parts of the API.
type fakeRows struct {
	rows      [][]any // one slice of column values per remaining row
	scanErrs  []error // optional per-row scan errors (same length as rows when set)
	finalErr  error   // error returned by Err() after iteration completes
	cur       []any   // values for the current row, set by Next()
	curErr    error   // scan error for the current row
	advanced  int     // number of times Next() returned true
	closed    bool    // true after Close() is called
	closeHits int     // number of times Close() has been called
}

func (f *fakeRows) Next() bool {
	if len(f.rows) == 0 {
		return false
	}
	f.cur = f.rows[0]
	f.rows = f.rows[1:]
	if len(f.scanErrs) > 0 {
		f.curErr = f.scanErrs[0]
		f.scanErrs = f.scanErrs[1:]
	} else {
		f.curErr = nil
	}
	f.advanced++
	return true
}

func (f *fakeRows) Scan(dest ...any) error {
	if f.curErr != nil {
		return f.curErr
	}
	if len(dest) != len(f.cur) {
		return errors.New("fakeRows: column count mismatch")
	}
	for i, d := range dest {
		switch p := d.(type) {
		case *int:
			v, ok := f.cur[i].(int)
			if !ok {
				return errors.New("fakeRows: expected int")
			}
			*p = v
		case *string:
			v, ok := f.cur[i].(string)
			if !ok {
				return errors.New("fakeRows: expected string")
			}
			*p = v
		default:
			return errors.New("fakeRows: unsupported dest type")
		}
	}
	return nil
}

func (f *fakeRows) Err() error { return f.finalErr }
func (f *fakeRows) Close()     { f.closed = true; f.closeHits++ }
func (f *fakeRows) CommandTag() pgconn.CommandTag {
	panic("CommandTag not implemented by fakeRows")
}
func (f *fakeRows) FieldDescriptions() []pgconn.FieldDescription {
	panic("FieldDescriptions not implemented by fakeRows")
}
func (f *fakeRows) Values() ([]any, error) {
	panic("Values not implemented by fakeRows")
}
func (f *fakeRows) RawValues() [][]byte {
	panic("RawValues not implemented by fakeRows")
}
func (f *fakeRows) Conn() *pgx.Conn { return nil }

type scanRow struct {
	ID   int
	Name string
}

func TestScanAll(t *testing.T) {
	t.Run("happy path returns slice and closes rows", func(t *testing.T) {
		rows := &fakeRows{rows: [][]any{
			{1, "one"},
			{2, "two"},
			{3, "three"},
		}}

		got, err := scanAll(rows, func(r pgx.Rows, out *scanRow) error {
			return r.Scan(&out.ID, &out.Name)
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("expected 3 rows, got %d", len(got))
		}
		want := []scanRow{{1, "one"}, {2, "two"}, {3, "three"}}
		for i, r := range got {
			if r != want[i] {
				t.Errorf("row %d: got %+v want %+v", i, r, want[i])
			}
		}
		if !rows.closed {
			t.Error("rows.Close was not called")
		}
	})

	t.Run("empty result returns non-nil zero-length slice", func(t *testing.T) {
		rows := &fakeRows{rows: nil}
		got, err := scanAll(rows, func(r pgx.Rows, out *scanRow) error {
			return r.Scan(&out.ID, &out.Name)
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// The helper guarantees a non-nil empty slice so callers do not
		// have to normalize the result and JSON encoders emit `[]`
		// rather than `null` for empty result sets.
		if got == nil {
			t.Fatal("expected non-nil slice for empty result, got nil")
		}
		if len(got) != 0 {
			t.Errorf("expected zero-length slice for empty result, got %v", got)
		}
		if !rows.closed {
			t.Error("rows.Close was not called on empty path")
		}
	})

	t.Run("scan callback error aborts and surfaces error", func(t *testing.T) {
		sentinel := errors.New("scan boom")
		rows := &fakeRows{
			rows:     [][]any{{1, "one"}, {2, "two"}},
			scanErrs: []error{nil, sentinel},
		}
		got, err := scanAll(rows, func(r pgx.Rows, out *scanRow) error {
			return r.Scan(&out.ID, &out.Name)
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("expected sentinel error, got %v", err)
		}
		if got != nil {
			t.Errorf("expected nil slice on error, got %v", got)
		}
		if !rows.closed {
			t.Error("rows.Close was not called when scan errored")
		}
		if rows.advanced != 2 {
			t.Errorf("expected to advance 2 rows before aborting, got %d", rows.advanced)
		}
	})

	t.Run("callback returned error short-circuits even without rows.Scan", func(t *testing.T) {
		sentinel := errors.New("custom callback failure")
		rows := &fakeRows{rows: [][]any{{1, "one"}, {2, "two"}}}
		got, err := scanAll(rows, func(r pgx.Rows, out *scanRow) error {
			// Ignore rows.Scan entirely; the helper must still propagate
			// any error the callback returns and stop iterating.
			return sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("expected sentinel error, got %v", err)
		}
		if got != nil {
			t.Errorf("expected nil slice, got %v", got)
		}
		if rows.advanced != 1 {
			t.Errorf("expected exactly one Next() call before aborting, got %d", rows.advanced)
		}
		if !rows.closed {
			t.Error("rows.Close was not called when callback errored")
		}
	})

	t.Run("rows.Err non-nil after loop returned", func(t *testing.T) {
		sentinel := errors.New("rows-level failure")
		rows := &fakeRows{
			rows:     [][]any{{1, "one"}},
			finalErr: sentinel,
		}
		got, err := scanAll(rows, func(r pgx.Rows, out *scanRow) error {
			return r.Scan(&out.ID, &out.Name)
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("expected sentinel rows.Err, got %v", err)
		}
		if got != nil {
			t.Errorf("expected nil slice when rows.Err fires, got %v", got)
		}
		if !rows.closed {
			t.Error("rows.Close was not called after rows.Err check")
		}
	})

	t.Run("rows.Close is deferred even on early return", func(t *testing.T) {
		rows := &fakeRows{rows: [][]any{{1, "one"}}}
		_, _ = scanAll(rows, func(r pgx.Rows, out *scanRow) error {
			return errors.New("explode immediately")
		})
		if rows.closeHits != 1 {
			t.Errorf("expected Close to be called exactly once, got %d", rows.closeHits)
		}
	})
}
