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
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
)

// TestMigrationV13_AvailableMemoryColumnExists verifies that the v13 migration
// adds the available_memory column to the pg_sys_memory_info parent table with
// a bigint type, and that the column comment was recorded.
func TestMigrationV13_AvailableMemoryColumnExists(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	cleanupTestSchema(t, pool)
	defer cleanupTestSchema(t, pool)

	sm := NewSchemaManager()
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	got, ok := columnType(ctx, t, pool, "metrics", "pg_sys_memory_info", "available_memory")
	if !ok {
		t.Fatal("metrics.pg_sys_memory_info.available_memory missing after migration v13")
	}
	if got != "bigint" {
		t.Errorf("available_memory type = %q, want %q", got, "bigint")
	}

	var comment string
	if err := pool.QueryRow(ctx, `
		SELECT col_description('metrics.pg_sys_memory_info'::regclass,
			(SELECT attnum FROM pg_attribute
			 WHERE attrelid = 'metrics.pg_sys_memory_info'::regclass
			   AND attname = 'available_memory'))
	`).Scan(&comment); err != nil {
		t.Fatalf("read available_memory comment: %v", err)
	}
	if comment == "" {
		t.Error("available_memory column has no comment")
	}
}

// TestMigrationV13_ColumnCascadesToExistingPartition simulates an upgrade
// on a datastore that already has a pg_sys_memory_info partition: it
// rewinds to a pre-v13 state, attaches a weekly partition, re-applies the
// migration and confirms the new column appears on the child too.
func TestMigrationV13_ColumnCascadesToExistingPartition(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	cleanupTestSchema(t, pool)
	defer cleanupTestSchema(t, pool)

	sm := NewSchemaManager()
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("baseline migrate: %v", err)
	}

	// Drop the v13 column and every schema_version row from v13 onwards,
	// because Migrate compares against the highest recorded version and
	// would otherwise skip v13.
	if _, err := pool.Exec(ctx, `
		ALTER TABLE metrics.pg_sys_memory_info
			DROP COLUMN IF EXISTS available_memory;
		DELETE FROM schema_version WHERE version >= 13;
	`); err != nil {
		t.Fatalf("rewind to pre-v13 state: %v", err)
	}

	const partitionStart = "2026-09-14"
	const partitionEnd = "2026-09-21"
	const partitionSuffix = "20260914"
	parentIdent := pgx.Identifier{"metrics", "pg_sys_memory_info"}.Sanitize()
	childIdent := pgx.Identifier{
		"metrics", "pg_sys_memory_info_" + partitionSuffix,
	}.Sanitize()
	ddl := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s PARTITION OF %s "+
			"FOR VALUES FROM ('%s') TO ('%s')",
		childIdent, parentIdent, partitionStart, partitionEnd,
	)
	// This is not a SQL injection risk despite passing a non-literal DDL
	// string: ddl is built entirely from hardcoded test constants and from
	// identifiers produced by pgx's own Identifier.Sanitize(). No user
	// input or untrusted external data is involved.
	// nosemgrep: go_sql_rule-concat-sqli
	if _, err := pool.Exec(ctx, ddl); err != nil {
		t.Fatalf("create partition: %v", err)
	}

	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("re-apply migrate: %v", err)
	}

	if _, ok := columnType(ctx, t, pool, "metrics",
		"pg_sys_memory_info_"+partitionSuffix, "available_memory"); !ok {
		t.Error("child partition missing available_memory column after v13")
	}
}

// TestMigrationV13_Idempotent re-runs v13's Up over a column that already
// exists by rewinding only the schema_version rows, so a regression from
// ADD COLUMN IF NOT EXISTS to plain ADD COLUMN would fail here.
func TestMigrationV13_Idempotent(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	cleanupTestSchema(t, pool)
	defer cleanupTestSchema(t, pool)

	sm := NewSchemaManager()
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("first Migrate failed: %v", err)
	}

	if _, err := pool.Exec(ctx,
		`DELETE FROM schema_version WHERE version >= 13`); err != nil {
		t.Fatalf("rewind v13 schema_version row: %v", err)
	}

	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("second Migrate (re-running v13 Up) failed: %v", err)
	}

	if _, ok := columnType(ctx, t, pool, "metrics",
		"pg_sys_memory_info", "available_memory"); !ok {
		t.Error("available_memory missing after re-running v13 Up")
	}

	var v13Count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM schema_version WHERE version = 13`).Scan(&v13Count); err != nil {
		t.Fatalf("count v13 rows: %v", err)
	}
	if v13Count != 1 {
		t.Errorf("expected exactly one schema_version row for v13, got %d", v13Count)
	}
}
