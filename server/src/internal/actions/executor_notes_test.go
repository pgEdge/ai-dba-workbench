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
	"strings"
	"testing"
	"time"
)

// TestExecuteSequenceNoteOnRollback proves a statement that called
// nextval in a transaction that rolled back carries a note, because the
// sequence keeps its new value, whilst statements that did not call it,
// or never ran, carry none, and a committed run carries none at all.
func TestExecuteSequenceNoteOnRollback(t *testing.T) {
	t.Parallel()
	f := newExecFixture(t, "_x_seq", time.Second, time.Minute, 10)
	mustExec(t, f.target, "CREATE SEQUENCE s")
	mustExec(t, f.target, "CREATE TABLE t (id int)")

	a := f.run(t, "SELECT nextval('s')", "INSERT INTO t VALUES (1)", "SELECT 1/0", "SELECT setval('s', 50)")
	if a.ExecStatus != "failed" || len(a.Results) != 4 {
		t.Fatalf("action = %s %+v", a.ExecStatus, a.Results)
	}
	if a.Results[0].Note != sequenceNote || a.Results[0].Applied {
		t.Errorf("nextval result = %+v", a.Results[0])
	}
	for i, res := range a.Results[1:] {
		if res.Note != "" {
			t.Errorf("result %d has a note: %+v", i+1, res)
		}
	}
	// A fresh sequence also reads last_value 1, so is_called is what
	// shows the rolled-back nextval really advanced it.
	var last int64
	var called bool
	if err := f.target.QueryRow(context.Background(),
		"SELECT last_value, is_called FROM s").Scan(&last, &called); err != nil || last != 1 || !called {
		t.Errorf("last_value = %d, is_called = %v, %v", last, called, err)
	}

	a = f.run(t, "SELECT nextval('s')")
	if a.ExecStatus != "succeeded" || a.Results[0].Note != "" || !a.Results[0].Applied {
		t.Errorf("committed run = %s %+v", a.ExecStatus, a.Results)
	}
}

// TestExecuteConcurrentIndexNote proves a concurrent index build that
// fails carries a note about the invalid index it leaves, and that the
// index is indeed left invalid, whilst a build that succeeds carries
// none.
func TestExecuteConcurrentIndexNote(t *testing.T) {
	t.Parallel()
	f := newExecFixture(t, "_x_cic", time.Second, time.Minute, 10)
	mustExec(t, f.target, "CREATE TABLE d (a int)")
	mustExec(t, f.target, "INSERT INTO d VALUES (1), (1)")

	a := f.run(t, "CREATE UNIQUE INDEX CONCURRENTLY d_a ON d (a)")
	if a.ExecStatus != "failed" || a.Results[0].Note != invalidIndexNote || a.Results[0].Applied {
		t.Fatalf("failed build = %s %+v", a.ExecStatus, a.Results)
	}
	var valid bool
	if err := f.target.QueryRow(context.Background(),
		"SELECT indisvalid FROM pg_index WHERE indexrelid = 'd_a'::regclass").Scan(&valid); err != nil {
		t.Fatal(err)
	}
	if valid {
		t.Error("the failed build left a valid index")
	}

	a = f.run(t, "CREATE INDEX CONCURRENTLY d_b ON d (a)")
	if a.ExecStatus != "succeeded" || a.Results[0].Note != "" || !a.Results[0].Applied {
		t.Errorf("successful build = %s %+v", a.ExecStatus, a.Results)
	}
}

// TestNoteHelpers covers combining notes on one result, noting only the
// statements that change a sequence, and the wording for an unknown
// outcome, which must not claim the statement was rolled back.
func TestNoteHelpers(t *testing.T) {
	t.Parallel()
	res := StatementResult{Statement: "SELECT nextval('s')"}
	noteFailure(&res, false)
	if res.Note != sequenceNote {
		t.Errorf("note = %q", res.Note)
	}
	addNote(&res, invalidIndexNote)
	if res.Note != sequenceNote+"; "+invalidIndexNote {
		t.Errorf("combined note = %q", res.Note)
	}
	res = StatementResult{Statement: "SELECT nextval('s')"}
	noteFailure(&res, true)
	if res.Note != sequenceUnknownNote {
		t.Errorf("unknown note = %q", res.Note)
	}
	for _, unknown := range []bool{false, true} {
		rs := []StatementResult{{Statement: "SELECT 1"}, {Statement: "SELECT setval('s', 2)"}}
		noteSequencesKept(rs, unknown)
		if rs[0].Note != "" || rs[1].Note != sequenceNoteFor(unknown) {
			t.Errorf("unknown = %v: results = %+v", unknown, rs)
		}
	}
	if strings.Contains(sequenceUnknownNote, "rollback does not undo") {
		t.Error("the unknown-outcome note claims a rollback")
	}
}
