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
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// commentsRow answers metricsCommentsQuery with fixed comments, or fails
// with err.
type commentsRow struct {
	view       *string
	cols, text []string
	err        error
}

func (r commentsRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != 3 {
		return errors.New("commentsRow scans into three destinations")
	}
	v, ok1 := dest[0].(**string)
	c, ok2 := dest[1].(*[]string)
	d, ok3 := dest[2].(*[]string)
	if !ok1 || !ok2 || !ok3 {
		return errors.New("commentsRow has unexpected destinations")
	}
	*v, *c, *d = r.view, r.cols, r.text
	return nil
}

// TestColdFrontAltersRestoreComments runs migrations 7, 10, 12 and 13
// under ColdFront against a recordingTx whose catalog reports a view
// comment and a column comment on every metrics view, and checks that
// each ALTER TABLE is followed by statements setting both again.
// ColdFront drops and recreates the view on ALTER TABLE, losing its
// comments, which the stub-based test cannot reproduce.
func TestColdFrontAltersRestoreComments(t *testing.T) {
	sm := NewSchemaManager(WithColdFrontMetrics(true))
	tx := &recordingTx{queryRow: func(sql string) pgx.Row {
		if strings.Contains(sql, "obj_description") {
			return commentsRow{view: strPtr("it's a view"), cols: []string{"pid"}, text: []string{"backend pid"}}
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
		want := []string{
			"COMMENT ON VIEW metrics." + view + " IS 'it''s a view'",
			"COMMENT ON COLUMN metrics." + view + ".pid IS 'backend pid'",
		}
		if i == 0 || !strings.Contains(tx.stmts[i-1].sql, "obj_description") {
			t.Errorf("migration %d: comments of metrics.%s were not read before the ALTER", s.migration, view)
		}
		if i+2 >= len(tx.stmts) {
			t.Fatalf("migration %d: nothing follows the ALTER of metrics.%s", s.migration, view)
		}
		got := []string{squash(tx.stmts[i+1].sql), squash(tx.stmts[i+2].sql)}
		// pgx.Identifier quotes every part.
		for j := range got {
			got[j] = strings.ReplaceAll(got[j], `"`, "")
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("migration %d: after the ALTER of metrics.%s got %q, want %q", s.migration, view, got, want)
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

	// A view with no comments, or one the catalog does not know, has
	// nothing to restore.
	for _, row := range []commentsRow{{}, {err: pgx.ErrNoRows}} {
		tx = &recordingTx{queryRow: func(string) pgx.Row { return row }}
		if err := cf.execMetricsAlter(ctx, tx, block); err != nil {
			t.Fatal(err)
		}
		if len(tx.stmts) != 3 {
			t.Errorf("issued %d statements, want the read, the ALTER and the column comment", len(tx.stmts))
		}
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
		{"restore fails", commentsRow{view: strPtr("v")}, func(s string) error {
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

func TestMetricsCommentStatementsIgnoresUnpairedNames(t *testing.T) {
	row := commentsRow{cols: []string{"a", "b"}, text: []string{"only a"}}
	stmts, err := metricsCommentStatements(context.Background(),
		&recordingTx{queryRow: func(string) pgx.Row { return row }}, "v")
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) != 1 {
		t.Errorf("metricsCommentStatements = %q, want one column comment", stmts)
	}
}

func TestEnsureColdFrontInsertHelper(t *testing.T) {
	c := &scriptedConn{}
	if err := EnsureColdFrontInsertHelper(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if len(c.execs) != 1 || !strings.Contains(c.execs[0], "CREATE OR REPLACE FUNCTION public.workbench_coldfront_insert") {
		t.Errorf("issued %q", c.execs)
	}

	c = &scriptedConn{failExec: 1, execErr: errors.New("permission denied")}
	err := EnsureColdFrontInsertHelper(context.Background(), c)
	if err == nil || !strings.Contains(err.Error(), "workbench_coldfront_insert") || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("EnsureColdFrontInsertHelper error = %v", err)
	}

	// PrepareMetricsStorage stops at a failed namespace step.
	c = &scriptedConn{
		scriptedQuerier: scriptedQuerier{rows: []scriptedRow{extensionRow(true, true, true), relationsRow(nil, nil)}},
		failExec:        1, execErr: errors.New("404"),
	}
	if err := PrepareMetricsStorage(context.Background(), c, true); err == nil || len(c.execs) != 1 {
		t.Errorf("PrepareMetricsStorage after a failed namespace step: err = %v, execs = %q", err, c.execs)
	}
}
