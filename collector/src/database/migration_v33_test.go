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
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// Migration 33 adds proposed_actions, which records every write Ellie or
// a person proposes, its review verdict, approval and execution outcome
// (issue #774).

// migrationV33 returns the registered version 33 migration.
func migrationV33(t *testing.T) Migration {
	t.Helper()
	for _, m := range NewSchemaManager().migrations {
		if m.Version == 33 {
			return m
		}
	}
	t.Fatal("migration version 33 is not registered")
	return Migration{}
}

// TestMigration33ProposedActions exercises the table after the full
// migration set has run, and runs migration 33 again to prove it is
// idempotent.
func TestMigration33ProposedActions(t *testing.T) {
	tx, cleanup := beginV23Tx(t)
	defer cleanup()
	if tx == nil {
		return
	}
	ctx := context.Background()

	if got := NewSchemaManager().LatestVersion(); got < 33 {
		t.Fatalf("LatestVersion() = %d, want at least 33", got)
	}
	if migrationV33(t).Description == "" {
		t.Error("migration 33 has an empty description")
	}
	if err := migrationV33(t).Up(tx); err != nil {
		t.Fatalf("second run of migration 33 failed: %v", err)
	}

	var connID int
	if err := tx.QueryRow(ctx, `
		INSERT INTO connections (name, host, port, database_name, username, owner_username)
		VALUES ('v33-test', 'localhost', 5432, 'postgres', 'postgres', 'tester')
		RETURNING id`).Scan(&connID); err != nil {
		t.Fatalf("failed to seed connection: %v", err)
	}

	// Each constrained column rejects an unknown value; a savepoint keeps
	// the transaction usable after each expected failure.
	cases := []struct{ name, cols, vals string }{
		{"review_status", "review_status, source, proposed_via", "'bogus', 'chat_tool', 'session'"},
		{"source", "review_status, source, proposed_via", "'pending', 'bogus', 'session'"},
		{"proposed_via", "review_status, source, proposed_via", "'pending', 'chat_tool', 'bogus'"},
		{"approval", "review_status, source, proposed_via, approval", "'pending', 'chat_tool', 'session', 'bogus'"},
		{"approved_via", "review_status, source, proposed_via, approved_via", "'pending', 'chat_tool', 'session', 'bogus'"},
		{"exec_status", "review_status, source, proposed_via, exec_status", "'pending', 'chat_tool', 'session', 'bogus'"},
	}
	for _, c := range cases {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatalf("savepoint: %v", err)
		}
		_, err = sp.Exec(ctx, `
			INSERT INTO proposed_actions (connection_id, statements, proposed_by, `+c.cols+`)
			VALUES ($1, '["SELECT 1"]'::jsonb, 'tester', `+c.vals+`)`, connID)
		if rbErr := sp.Rollback(ctx); rbErr != nil {
			t.Fatalf("savepoint rollback: %v", rbErr)
		}
		if err == nil || !strings.Contains(err.Error(), "violates check constraint") {
			t.Errorf("%s: err = %v, want a check violation", c.name, err)
		}
	}

	// Alert deletion nulls alert_id; connection deletion cascades.
	var alertID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO alerts (alert_type, connection_id, title, description, severity, status)
		VALUES ('threshold', $1, 'v33', 'v33', 'warning', 'active')
		RETURNING id`, connID).Scan(&alertID); err != nil {
		t.Fatalf("failed to seed alert: %v", err)
	}
	var actionID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO proposed_actions
		    (connection_id, statements, source, proposed_by, proposed_via,
		     review_status, alert_id)
		VALUES ($1, '["SELECT 1"]'::jsonb, 'alert_analysis', 'tester', 'token',
		        'passed', $2)
		RETURNING id`, connID, alertID).Scan(&actionID); err != nil {
		t.Fatalf("failed to insert action: %v", err)
	}
	var approval, execStatus string
	if err := tx.QueryRow(ctx,
		`SELECT approval, exec_status FROM proposed_actions WHERE id = $1`,
		actionID).Scan(&approval, &execStatus); err != nil {
		t.Fatalf("failed to read defaults: %v", err)
	}
	if approval != "none" || execStatus != "not_started" {
		t.Errorf("defaults = %q/%q, want none/not_started", approval, execStatus)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM alerts WHERE id = $1`, alertID); err != nil {
		t.Fatalf("failed to delete alert: %v", err)
	}
	var alertNull bool
	if err := tx.QueryRow(ctx,
		`SELECT alert_id IS NULL FROM proposed_actions WHERE id = $1`,
		actionID).Scan(&alertNull); err != nil || !alertNull {
		t.Errorf("alert_id null after alert delete = %v (err %v), want true", alertNull, err)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM connections WHERE id = $1`, connID); err != nil {
		t.Fatalf("failed to delete connection: %v", err)
	}
	var remaining int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM proposed_actions WHERE id = $1`,
		actionID).Scan(&remaining); err != nil || remaining != 0 {
		t.Errorf("actions after connection delete = %d (err %v), want 0", remaining, err)
	}

	assertV33Comments(t, tx)
	assertV33Indexes(t, tx)
}

// assertV33Comments checks the table and every column carry a comment.
func assertV33Comments(t *testing.T, tx pgx.Tx) {
	t.Helper()
	ctx := context.Background()
	var tableComment *string
	if err := tx.QueryRow(ctx,
		`SELECT obj_description('proposed_actions'::regclass, 'pg_class')`).Scan(&tableComment); err != nil {
		t.Fatalf("failed to read table comment: %v", err)
	}
	if tableComment == nil || *tableComment == "" {
		t.Error("proposed_actions has no table comment")
	}
	rows, err := tx.Query(ctx, `
		SELECT attname FROM pg_attribute
		WHERE attrelid = 'proposed_actions'::regclass AND attnum > 0 AND NOT attisdropped
		  AND col_description(attrelid, attnum) IS NULL`)
	if err != nil {
		t.Fatalf("failed to query column comments: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		t.Errorf("column %s has no comment", name)
	}
}

// assertV33Indexes checks the required indexes exist.
func assertV33Indexes(t *testing.T, tx pgx.Tx) {
	t.Helper()
	rows, err := tx.Query(context.Background(),
		`SELECT indexdef FROM pg_indexes WHERE tablename = 'proposed_actions'`)
	if err != nil {
		t.Fatalf("failed to list indexes: %v", err)
	}
	defer rows.Close()
	var defs []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			t.Fatalf("scan: %v", err)
		}
		defs = append(defs, d)
	}
	for _, want := range []string{
		"(connection_id, created_at DESC)",
		"(conversation_id)",
		"(parent_action_id)",
		"WHERE (exec_status = 'executing'::text)",
	} {
		found := false
		for _, d := range defs {
			if strings.Contains(d, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("no index matching %q in %v", want, defs)
		}
	}
}
