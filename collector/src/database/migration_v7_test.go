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
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// columnType reads the data type of a column from information_schema so
// the migration assertions do not depend on catalog OIDs.
func columnType(ctx context.Context, t *testing.T, pool *pgxpool.Pool,
	schema, table, column string) (string, bool) {
	t.Helper()
	var dataType string
	err := pool.QueryRow(ctx, `
		SELECT data_type
		FROM information_schema.columns
		WHERE table_schema = $1
		  AND table_name = $2
		  AND column_name = $3
	`, schema, table, column).Scan(&dataType)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false
		}
		t.Fatalf("read column type %s.%s.%s: %v", schema, table, column, err)
	}
	return dataType, true
}

// TestMigrationV7_RelationSizeColumnsExist verifies that the v7 migration
// adds the relation-size columns to both parent metrics tables with the
// expected data types. The fixture migrates a fresh test database to the
// latest schema version and inspects information_schema.
func TestMigrationV7_RelationSizeColumnsExist(t *testing.T) {
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

	cases := []struct {
		table    string
		column   string
		wantType string
	}{
		{"pg_stat_all_tables", "table_size", "bigint"},
		{"pg_stat_all_tables", "table_size_pretty", "text"},
		{"pg_stat_all_indexes", "index_size", "bigint"},
		{"pg_stat_all_indexes", "index_size_pretty", "text"},
	}
	for _, c := range cases {
		got, ok := columnType(ctx, t, pool, "metrics", c.table, c.column)
		if !ok {
			t.Errorf("metrics.%s.%s missing after migration v7", c.table, c.column)
			continue
		}
		if got != c.wantType {
			t.Errorf("metrics.%s.%s type = %q, want %q",
				c.table, c.column, got, c.wantType)
		}
	}
}

// TestMigrationV7_ColumnsCascadeToExistingPartition simulates an upgrade
// on a datastore that already has partitions: it pre-creates a weekly
// partition, applies v7's Up directly, then confirms the new columns
// appear on the child partition too. PostgreSQL propagates ADD COLUMN on
// the partitioned parent to existing partitions automatically; this test
// pins that behavior against the versions the project supports.
func TestMigrationV7_ColumnsCascadeToExistingPartition(t *testing.T) {
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

	// Drop the v7 columns and rewind the schema_version so the parent
	// tables look like a pre-v7 datastore, then attach a partition
	// before re-applying v7. This reproduces the real upgrade shape:
	// a partition exists at the moment ADD COLUMN runs on the parent.
	if _, err := pool.Exec(ctx, `
		ALTER TABLE metrics.pg_stat_all_tables
			DROP COLUMN IF EXISTS table_size,
			DROP COLUMN IF EXISTS table_size_pretty;
		ALTER TABLE metrics.pg_stat_all_indexes
			DROP COLUMN IF EXISTS index_size,
			DROP COLUMN IF EXISTS index_size_pretty;
		DELETE FROM schema_version WHERE version = 7;
	`); err != nil {
		t.Fatalf("rewind to pre-v7 state: %v", err)
	}

	const partitionStart = "2026-05-11"
	const partitionEnd = "2026-05-18"
	const partitionSuffix = "20260511"
	for _, table := range []string{"pg_stat_all_tables", "pg_stat_all_indexes"} {
		parentIdent := pgx.Identifier{"metrics", table}.Sanitize()
		childIdent := pgx.Identifier{
			"metrics", table + "_" + partitionSuffix,
		}.Sanitize()
		ddl := fmt.Sprintf(
			"CREATE TABLE IF NOT EXISTS %s PARTITION OF %s "+
				"FOR VALUES FROM ('%s') TO ('%s')",
			childIdent, parentIdent, partitionStart, partitionEnd,
		)
		if _, err := pool.Exec(ctx, ddl); err != nil {
			t.Fatalf("create partition %s_%s: %v", table, partitionSuffix, err)
		}
	}

	// Re-apply the migration; Migrate re-runs v7 because we deleted its
	// schema_version row above.
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("re-apply migrate: %v", err)
	}

	childCases := []struct {
		table  string
		column string
	}{
		{"pg_stat_all_tables_" + partitionSuffix, "table_size"},
		{"pg_stat_all_tables_" + partitionSuffix, "table_size_pretty"},
		{"pg_stat_all_indexes_" + partitionSuffix, "index_size"},
		{"pg_stat_all_indexes_" + partitionSuffix, "index_size_pretty"},
	}
	for _, c := range childCases {
		if _, ok := columnType(ctx, t, pool, "metrics", c.table, c.column); !ok {
			t.Errorf("child partition metrics.%s missing column %s after v7",
				c.table, c.column)
		}
	}
}

// TestMigrationV7_Idempotent verifies that v7 can be applied twice
// without error and records exactly one schema_version row.
func TestMigrationV7_Idempotent(t *testing.T) {
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
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("second Migrate failed: %v", err)
	}

	var v7Count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM schema_version WHERE version = 7`).Scan(&v7Count); err != nil {
		t.Fatalf("count v7 rows: %v", err)
	}
	if v7Count != 1 {
		t.Errorf("expected exactly one schema_version row for v7, got %d", v7Count)
	}
}
