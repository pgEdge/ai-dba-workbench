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
	"testing"
)

// TestMigrationV10_QueryIDColumnExists verifies that the v10 migration adds
// query_id to the partitioned activity table as a bigint, matching the type
// of metrics.pg_stat_statements.queryid it is joined against.
func TestMigrationV10_QueryIDColumnExists(t *testing.T) {
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

	got, ok := columnType(ctx, t, pool, "metrics", "pg_stat_activity", "query_id")
	if !ok {
		t.Fatal("metrics.pg_stat_activity.query_id missing after migration v10")
	}
	if got != "bigint" {
		t.Errorf("metrics.pg_stat_activity.query_id type = %q, want bigint", got)
	}

	var comment string
	if err := pool.QueryRow(ctx, `
		SELECT col_description('metrics.pg_stat_activity'::regclass, attnum)
		FROM pg_attribute
		WHERE attrelid = 'metrics.pg_stat_activity'::regclass
		  AND attname = 'query_id'
	`).Scan(&comment); err != nil {
		t.Fatalf("read query_id comment: %v", err)
	}
	if comment == "" {
		t.Error("metrics.pg_stat_activity.query_id has no COMMENT")
	}
}

// TestMigrationV10_Idempotent rewinds only the schema_version rows from v10
// onwards, leaving the column in place, so that Migrate re-runs v10's Up
// over an existing column. This fails if ADD COLUMN IF NOT EXISTS is ever
// weakened to a plain ADD COLUMN.
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

	if _, ok := columnType(ctx, t, pool, "metrics", "pg_stat_activity", "query_id"); !ok {
		t.Error("metrics.pg_stat_activity.query_id missing after re-running v10 Up")
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
