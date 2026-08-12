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
	"fmt"
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

// TestMigrateReportsUnreadableVersion covers the branch where the current
// schema version cannot be read at all. A schema_version table without a
// version column reproduces the drift that distinguishes this case from a
// missing table, which Migrate tolerates.
func TestMigrateReportsUnreadableVersion(t *testing.T) {
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	ctx := t.Context()
	if _, err := conn.Exec(ctx,
		`DROP TABLE IF EXISTS schema_version CASCADE`); err != nil {
		t.Fatalf("failed to drop schema_version: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`CREATE TABLE schema_version (unexpected_column integer)`); err != nil {
		t.Fatalf("failed to create bogus schema_version: %v", err)
	}
	// Registered last so it runs first, whilst the pool is still open:
	// leaving the drifted table behind would break every later test.
	defer func() {
		if _, err := pool.Exec(context.Background(),
			`DROP TABLE IF EXISTS schema_version CASCADE`); err != nil {
			t.Errorf("failed to drop the drifted schema_version table: %v", err)
		}
	}()

	err := NewSchemaManager().Migrate(conn)
	if err == nil {
		t.Fatal("Migrate with an unreadable schema_version returned nil; want error")
	}
	if !strings.Contains(err.Error(), "failed to get current version") {
		t.Errorf("Migrate error = %v, want the wrapped version lookup failure", err)
	}
}

// TestMigrateLogsFailedRollback covers the branch that reports a rollback
// which itself fails. A migration that closes the underlying connection
// before returning its error leaves the rollback with nothing to talk to,
// which is the same situation a dropped network connection produces.
func TestMigrateLogsFailedRollback(t *testing.T) {
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	sm := &SchemaManager{
		migrations: []Migration{
			{
				Version:     999998,
				Description: "migration that breaks its own connection",
				Up: func(tx pgx.Tx) error {
					if err := tx.Conn().Close(context.Background()); err != nil {
						return fmt.Errorf("closing connection: %w", err)
					}
					return errors.New("synthetic failure on a dead connection")
				},
			},
		},
	}

	err := sm.Migrate(conn)
	if err == nil {
		t.Fatal("Migrate over a closed connection returned nil; want error")
	}
	if !strings.Contains(err.Error(), "failed to apply migration 999998") {
		t.Errorf("Migrate error = %v, want the wrapped apply failure", err)
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
