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
		{"pg_stat_all_indexes", "index_size", "bigint"},
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
	// Every row from v7 onwards is removed, because Migrate compares
	// against the highest recorded version and would otherwise skip v7.
	if _, err := pool.Exec(ctx, `
		ALTER TABLE metrics.pg_stat_all_tables
			DROP COLUMN IF EXISTS table_size;
		ALTER TABLE metrics.pg_stat_all_indexes
			DROP COLUMN IF EXISTS index_size;
		DELETE FROM schema_version WHERE version >= 7;
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
		// This is not a SQL injection risk despite passing a non-literal
		// DDL string: ddl is built entirely from hardcoded test constants
		// (partitionStart, partitionEnd, partitionSuffix) and from
		// childIdent/parentIdent, which are produced by pgx's own
		// Identifier.Sanitize() safe identifier-quoting. No user input or
		// untrusted external data is involved in this construction.
		// nosemgrep: go_sql_rule-concat-sqli
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
		{"pg_stat_all_indexes_" + partitionSuffix, "index_size"},
	}
	for _, c := range childCases {
		if _, ok := columnType(ctx, t, pool, "metrics", c.table, c.column); !ok {
			t.Errorf("child partition metrics.%s missing column %s after v7",
				c.table, c.column)
		}
	}
}

// TestMigrationV7_Idempotent verifies that v7's Up statements are truly
// idempotent: they can execute a second time against columns that already
// exist without error. To exercise this, the test rewinds only the
// schema_version row for v7 WITHOUT dropping the columns the first run
// added, then re-runs Migrate so v7's Up executes again over the existing
// columns. This genuinely fails if the ADD COLUMN IF NOT EXISTS statements
// were changed to plain ADD COLUMN (which would error "column already
// exists" on the second execution). A plain double-Migrate would not catch
// that regression, because Migrate skips any version already recorded in
// schema_version and would never run Up twice.
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

	// Rewind the schema_version rows from v7 onwards, leaving the columns
	// the first run added in place. This forces Migrate to re-run v7's Up
	// against already-present columns, exercising real idempotency.
	// Migrate compares against the highest recorded version, so every
	// later row has to go as well or v7 would simply be skipped.
	if _, err := pool.Exec(ctx,
		`DELETE FROM schema_version WHERE version >= 7`); err != nil {
		t.Fatalf("rewind v7 schema_version row: %v", err)
	}

	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("second Migrate (re-running v7 Up) failed: %v", err)
	}

	// Confirm the re-run left the columns intact and recorded exactly one
	// schema_version row for v7.
	for _, c := range []struct {
		table  string
		column string
	}{
		{"pg_stat_all_tables", "table_size"},
		{"pg_stat_all_indexes", "index_size"},
	} {
		if _, ok := columnType(ctx, t, pool, "metrics", c.table, c.column); !ok {
			t.Errorf("metrics.%s.%s missing after re-running v7 Up",
				c.table, c.column)
		}
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
