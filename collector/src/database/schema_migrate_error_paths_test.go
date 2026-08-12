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
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// TestMigrateRollsBackFailedMigration covers the migration-failure branch
// of Migrate. A synthetic migration whose Up function errors must leave
// the transaction rolled back and surface a wrapped error, rather than
// recording the migration as applied.
func TestMigrateRollsBackFailedMigration(t *testing.T) {
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	sm := &SchemaManager{
		migrations: []Migration{
			{
				Version:     999999,
				Description: "deliberately failing migration",
				Up: func(pgx.Tx) error {
					return errors.New("synthetic migration failure")
				},
			},
		},
	}

	err := sm.Migrate(conn)
	if err == nil {
		t.Fatal("Migrate with a failing migration returned nil; want error")
	}
	if !strings.Contains(err.Error(), "failed to apply migration 999999") {
		t.Errorf("Migrate error = %v, want the wrapped apply failure", err)
	}

	// The connection must remain usable, which is only true if the
	// rollback succeeded.
	var one int
	if err := conn.QueryRow(t.Context(), `SELECT 1`).Scan(&one); err != nil {
		t.Fatalf("connection unusable after rolled-back migration: %v", err)
	}
}

// TestMigrateRollsBackUnrecordableMigration covers the branch where the
// migration itself succeeds but the schema_version bookkeeping insert
// fails. A version beyond the range of the INTEGER column makes the
// insert fail deterministically without touching any real schema.
func TestMigrateRollsBackUnrecordableMigration(t *testing.T) {
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	sm := &SchemaManager{
		migrations: []Migration{
			{
				Version:     2147483648, // one past the INTEGER maximum
				Description: "unrecordable migration",
				Up:          func(pgx.Tx) error { return nil },
			},
		},
	}

	err := sm.Migrate(conn)
	if err == nil {
		t.Fatal("Migrate with an unrecordable migration returned nil; want error")
	}
	if !strings.Contains(err.Error(), "failed to record migration") {
		t.Errorf("Migrate error = %v, want the wrapped record failure", err)
	}

	var one int
	if err := conn.QueryRow(t.Context(), `SELECT 1`).Scan(&one); err != nil {
		t.Fatalf("connection unusable after rolled-back migration: %v", err)
	}
}
