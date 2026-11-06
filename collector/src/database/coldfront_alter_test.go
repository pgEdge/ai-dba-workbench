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
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// commentsRow answers metricsCommentsQuery with fixed statements, or
// fails with err.
type commentsRow struct {
	stmts []string
	err   error
}

func (r commentsRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != 1 {
		return errors.New("commentsRow scans into one destination")
	}
	d, ok := dest[0].(*[]string)
	if !ok {
		return errors.New("commentsRow has an unexpected destination")
	}
	*d = r.stmts
	return nil
}

// restoredComments is what the catalog reports for every view in
// TestColdFrontAltersRestoreComments.
var restoredComments = []string{
	"COMMENT ON VIEW metrics.v IS 'it''s a view'",
	"COMMENT ON COLUMN metrics.v.pid IS 'backend pid'",
}

// TestColdFrontAltersRestoreComments runs migrations 7, 10, 12 and 13
// under ColdFront against a recordingTx whose catalog reports a view
// comment and a column comment, and checks that each ALTER TABLE is
// preceded by the read and followed by those statements. ColdFront drops
// and recreates the view on ALTER TABLE, losing its comments.
func TestColdFrontAltersRestoreComments(t *testing.T) {
	sm := NewSchemaManager(WithColdFrontMetrics(true))
	tx := &recordingTx{queryRow: func(sql string) pgx.Row {
		if strings.Contains(sql, "COMMENT ON VIEW %I.%I IS %L") {
			return commentsRow{stmts: restoredComments}
		}
		return falseRow{}
	}}
	for _, m := range sm.migrations {
		switch m.Version {
		case 7, 10, 12, 13:
			tx.migration = m.Version
			if err := m.Up(tx); err != nil {
				t.Fatalf("migration %d: %v", m.Version, err)
			}
		}
	}

	altered := map[string]bool{}
	for i, s := range tx.stmts {
		m := metricsAlterTarget.FindStringSubmatch(squash(s.sql))
		if m == nil {
			continue
		}
		view := m[1]
		altered[view] = true
		if i == 0 || !strings.Contains(tx.stmts[i-1].sql, "pg_description") ||
			!reflect.DeepEqual(tx.stmts[i-1].args, []any{view}) {
			t.Errorf("migration %d: comments of metrics.%s were not read before the ALTER", s.migration, view)
		}
		if i+2 >= len(tx.stmts) {
			t.Fatalf("migration %d: nothing follows the ALTER of metrics.%s", s.migration, view)
		}
		got := []string{tx.stmts[i+1].sql, tx.stmts[i+2].sql}
		if !reflect.DeepEqual(got, restoredComments) {
			t.Errorf("migration %d: after the ALTER of metrics.%s got %q, want %q", s.migration, view, got, restoredComments)
		}
	}
	wantAltered := map[string]bool{
		"pg_stat_all_tables": true, "pg_stat_all_indexes": true, "pg_stat_statements": true,
		"pg_stat_activity": true, "pg_sys_memory_info": true,
	}
	if !reflect.DeepEqual(altered, wantAltered) {
		t.Errorf("altered views = %v, want %v", altered, wantAltered)
	}
}

func TestExecMetricsAlter(t *testing.T) {
	ctx := context.Background()
	const block = `ALTER TABLE metrics.pg_stat_wal ADD COLUMN IF NOT EXISTS x BIGINT;
		COMMENT ON COLUMN metrics.pg_stat_wal.x IS 'x'`
	errBoom := errors.New("boom")

	// Default mode: the block verbatim, no catalog read.
	tx := &recordingTx{}
	if err := (&SchemaManager{}).execMetricsAlter(ctx, tx, block); err != nil {
		t.Fatal(err)
	}
	if len(tx.stmts) != 1 || tx.stmts[0].sql != block {
		t.Errorf("default mode issued %+v", tx.stmts)
	}

	cf := &SchemaManager{useColdFrontForMetrics: true}

	// A view with no comments has nothing to restore.
	tx = &recordingTx{queryRow: func(string) pgx.Row { return commentsRow{} }}
	if err := cf.execMetricsAlter(ctx, tx, block); err != nil {
		t.Fatal(err)
	}
	if len(tx.stmts) != 3 {
		t.Errorf("issued %d statements, want the read, the ALTER and the column comment", len(tx.stmts))
	}

	tests := []struct {
		name    string
		row     commentsRow
		execErr func(string) error
		want    string
	}{
		{"catalog read fails", commentsRow{err: errBoom}, nil, "failed to read the comments on metrics.pg_stat_wal"},
		{"alter fails", commentsRow{}, func(s string) error {
			if strings.HasPrefix(s, "ALTER") {
				return errBoom
			}
			return nil
		}, "boom"},
		{"restore fails", commentsRow{stmts: []string{"COMMENT ON VIEW metrics.pg_stat_wal IS 'v'"}}, func(s string) error {
			if strings.HasPrefix(s, "COMMENT ON VIEW") {
				return errBoom
			}
			return nil
		}, "failed to restore a comment on metrics.pg_stat_wal"},
		{"other statement fails", commentsRow{}, func(s string) error {
			if strings.HasPrefix(s, "COMMENT ON COLUMN") {
				return errBoom
			}
			return nil
		}, "boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := tt.row
			tx := &recordingTx{execErr: tt.execErr, queryRow: func(string) pgx.Row { return row }}
			err := cf.execMetricsAlter(ctx, tx, block)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("execMetricsAlter error = %v, want one containing %q", err, tt.want)
			}
		})
	}
}

// helperTx is the transaction EnsureColdFrontInsertHelper runs in: it
// records each Exec, failing the one at position failExec, answers the
// ownership query from owned and foreign, and records the commit.
type helperTx struct {
	pgx.Tx
	execs     []string
	failExec  int
	owned     bool
	foreign   []string
	queryErr  error
	commitErr error
	committed bool
}

func (h *helperTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	h.execs = append(h.execs, sql)
	if len(h.execs) == h.failExec {
		return pgconn.CommandTag{}, errors.New("permission denied for database")
	}
	return pgconn.CommandTag{}, nil
}

func (h *helperTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return ownersRow{h}
}

func (h *helperTx) Commit(context.Context) error {
	if h.commitErr != nil {
		return h.commitErr
	}
	h.committed = true
	return nil
}

func (h *helperTx) Rollback(context.Context) error { return nil }

// ownersRow answers coldFrontHelperOwnersQuery.
type ownersRow struct{ h *helperTx }

func (r ownersRow) Scan(dest ...any) error {
	if r.h.queryErr != nil {
		return r.h.queryErr
	}
	owner, ok1 := dest[0].(*string)
	owned, ok2 := dest[1].(*bool)
	foreign, ok3 := dest[2].(*[]string)
	if !ok1 || !ok2 || !ok3 {
		return errors.New("ownersRow has unexpected destinations")
	}
	*owner, *owned, *foreign = "intruder", r.h.owned, r.h.foreign
	return nil
}

func TestEnsureColdFrontInsertHelper(t *testing.T) {
	ctx := context.Background()

	c := &scriptedConn{}
	if err := EnsureColdFrontInsertHelper(ctx, c); err != nil {
		t.Fatal(err)
	}
	if !c.tx.committed || len(c.tx.execs) != 3 {
		t.Fatalf("issued %d statements, committed %v", len(c.tx.execs), c.tx.committed)
	}
	if c.tx.execs[0] != createColdFrontHelperSchemaSQL ||
		!strings.Contains(c.tx.execs[1], "REVOKE ALL ON SCHEMA workbench_coldfront FROM PUBLIC") {
		t.Errorf("statements = %q", c.tx.execs[:2])
	}
	functions := c.tx.execs[2]
	for _, want := range []string{
		"CREATE OR REPLACE FUNCTION workbench_coldfront.insert_rows(",
		"CREATE OR REPLACE FUNCTION workbench_coldfront.insert_rows_sql(",
		"CREATE OR REPLACE FUNCTION workbench_coldfront.literal(",
		"FROM PUBLIC;",
		"'INSERT /* ai_dba_wb_internal */ INTO %s (%s) VALUES %s'",
		"SET DateStyle = 'ISO, YMD'",
	} {
		if !strings.Contains(functions, want) {
			t.Errorf("helper functions lack %q", want)
		}
	}
	// No function may catch an error: a PL/pgSQL EXCEPTION block runs its body
	// in a subtransaction, and pg_duckdb refuses every write in one, which
	// failed every probe on a live ColdFront datastore.
	if regexp.MustCompile(`(?i)\bEXCEPTION\s+WHEN\b`).MatchString(functions) {
		t.Error("a helper function has an EXCEPTION handler, which pg_duckdb refuses as a subtransaction")
	}
	if strings.Contains(functions, "@@MARKER@@") || strings.Contains(functions, "public.") {
		t.Error("helper functions keep the marker placeholder or name the public schema")
	}

	tests := []struct {
		name string
		conn *scriptedConn
		want string
	}{
		{"begin fails", &scriptedConn{beginErr: errors.New("conn closed")}, "failed to begin"},
		{"schema create fails", &scriptedConn{tx: &helperTx{owned: true, failExec: 1}}, "failed to create the workbench_coldfront schema"},
		{"owner query fails", &scriptedConn{tx: &helperTx{queryErr: errors.New("boom")}}, "failed to check the owner"},
		{"schema owned by another role", &scriptedConn{tx: &helperTx{}}, "owned by role intruder"},
		{"foreign function", &scriptedConn{tx: &helperTx{owned: true, foreign: []string{"insert_rows(text, jsonb) owned by intruder"}}},
			"functions owned by other roles (insert_rows(text, jsonb) owned by intruder)"},
		{"function create fails", &scriptedConn{tx: &helperTx{owned: true, failExec: 3}}, "failed to create the functions"},
		{"commit fails", &scriptedConn{tx: &helperTx{owned: true, commitErr: errors.New("boom")}}, "failed to commit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := EnsureColdFrontInsertHelper(ctx, tt.conn)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("EnsureColdFrontInsertHelper error = %v, want one containing %q", err, tt.want)
			}
			if tt.conn.tx != nil && tt.conn.tx.committed {
				t.Error("committed after a failure")
			}
		})
	}

	// PrepareMetricsStorage stops at a failed namespace step.
	c = &scriptedConn{
		scriptedQuerier: scriptedQuerier{rows: []scriptedRow{extensionRow(true, true, true), relationsRow(nil, nil)}},
		failExec:        1, execErr: errors.New("404"),
	}
	if err := PrepareMetricsStorage(ctx, c, true); err == nil || len(c.execs) != 1 || c.tx != nil {
		t.Errorf("PrepareMetricsStorage after a failed namespace step: err = %v, execs = %q", err, c.execs)
	}
}
