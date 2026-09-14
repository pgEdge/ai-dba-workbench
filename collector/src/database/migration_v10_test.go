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

// TestMigrationV10_StatsResetColumnExists verifies that the v10 migration
// adds the stats_reset column to the pg_stat_statements parent table with
// a timestamptz type, and that the column comment was recorded.
func TestMigrationV10_StatsResetColumnExists(t *testing.T) {
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

	got, ok := columnType(ctx, t, pool, "metrics", "pg_stat_statements", "stats_reset")
	if !ok {
		t.Fatal("metrics.pg_stat_statements.stats_reset missing after migration v10")
	}
	if got != "timestamp with time zone" {
		t.Errorf("stats_reset type = %q, want %q", got, "timestamp with time zone")
	}

	var comment string
	if err := pool.QueryRow(ctx, `
		SELECT col_description('metrics.pg_stat_statements'::regclass,
			(SELECT attnum FROM pg_attribute
			 WHERE attrelid = 'metrics.pg_stat_statements'::regclass
			   AND attname = 'stats_reset'))
	`).Scan(&comment); err != nil {
		t.Fatalf("read stats_reset comment: %v", err)
	}
	if comment == "" {
		t.Error("stats_reset column has no comment")
	}
}

// TestMigrationV10_ColumnCascadesToExistingPartition simulates an upgrade
// on a datastore that already has a pg_stat_statements partition: it
// rewinds to a pre-v10 state, attaches a weekly partition, re-applies the
// migration and confirms the new column appears on the child too.
func TestMigrationV10_ColumnCascadesToExistingPartition(t *testing.T) {
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

	// Drop the v10 column and every schema_version row from v10 onwards,
	// because Migrate compares against the highest recorded version and
	// would otherwise skip v10.
	if _, err := pool.Exec(ctx, `
		ALTER TABLE metrics.pg_stat_statements
			DROP COLUMN IF EXISTS stats_reset;
		DELETE FROM schema_version WHERE version >= 10;
	`); err != nil {
		t.Fatalf("rewind to pre-v10 state: %v", err)
	}

	const partitionStart = "2026-09-14"
	const partitionEnd = "2026-09-21"
	const partitionSuffix = "20260914"
	parentIdent := pgx.Identifier{"metrics", "pg_stat_statements"}.Sanitize()
	childIdent := pgx.Identifier{
		"metrics", "pg_stat_statements_" + partitionSuffix,
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
		"pg_stat_statements_"+partitionSuffix, "stats_reset"); !ok {
		t.Error("child partition missing stats_reset column after v10")
	}
}

// TestMigrationV10_Idempotent re-runs v10's Up over a column that already
// exists by rewinding only the schema_version rows, so a regression from
// ADD COLUMN IF NOT EXISTS to plain ADD COLUMN would fail here.
func TestMigrationV10_Idempotent(t *testing.T) {
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
		`DELETE FROM schema_version WHERE version >= 10`); err != nil {
		t.Fatalf("rewind v10 schema_version row: %v", err)
	}

	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("second Migrate (re-running v10 Up) failed: %v", err)
	}

	if _, ok := columnType(ctx, t, pool, "metrics",
		"pg_stat_statements", "stats_reset"); !ok {
		t.Error("stats_reset missing after re-running v10 Up")
	}

	var v10Count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM schema_version WHERE version = 10`).Scan(&v10Count); err != nil {
		t.Fatalf("count v10 rows: %v", err)
	}
	if v10Count != 1 {
		t.Errorf("expected exactly one schema_version row for v10, got %d", v10Count)
	}
}
