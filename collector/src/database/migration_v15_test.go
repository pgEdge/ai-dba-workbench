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

	"github.com/jackc/pgx/v5/pgxpool"
)

// identityIndexName is the covering index that migration #15 adds for the
// windowed top-queries aggregation.
const identityIndexName = "idx_pg_stat_statements_identity_time"

// identityIndexDefinition reads the index definition recorded for the
// parent partitioned index, or reports that it is absent.
func identityIndexDefinition(
	ctx context.Context,
	t *testing.T,
	pool *pgxpool.Pool,
) (string, bool) {
	t.Helper()

	var def *string
	err := pool.QueryRow(ctx, `
		SELECT pg_get_indexdef(c.oid)
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'metrics'
		  AND c.relname = $1
	`, identityIndexName).Scan(&def)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return "", false
		}
		t.Fatalf("read %s definition: %v", identityIndexName, err)
	}
	if def == nil {
		return "", false
	}
	return *def, true
}

// assertIdentityIndexShape checks that the index carries the key columns in
// the order the identity window needs and the counters the aggregation
// reads as included columns. The column order is the whole point of the
// index, so the test pins it rather than merely checking that an index of
// that name exists.
func assertIdentityIndexShape(def string, t *testing.T) {
	t.Helper()

	const wantKeys = "(connection_id, queryid, userid, dbid, toplevel, " +
		"collected_at, database_name)"
	if !strings.Contains(def, wantKeys) {
		t.Errorf("index key columns = %q, want them to contain %q",
			def, wantKeys)
	}

	const wantInclude = "INCLUDE (calls, total_exec_time, rows, " +
		"shared_blks_hit, shared_blks_read, min_exec_time, max_exec_time)"
	if !strings.Contains(def, wantInclude) {
		t.Errorf("index definition = %q, want it to contain %q",
			def, wantInclude)
	}

	// The query text is deliberately absent: including it would roughly
	// triple the size of the index.
	if strings.Contains(def, "query)") || strings.Contains(def, "query,") {
		t.Errorf("index definition = %q, want it to exclude the query column",
			def)
	}
}

// TestMigrationV15_FreshSchemaHasIdentityIndex verifies that a schema built
// from scratch carries the covering index and its comment, since the
// consolidated migration #1 creates it alongside the table.
func TestMigrationV15_FreshSchemaHasIdentityIndex(t *testing.T) {
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

	def, ok := identityIndexDefinition(ctx, t, pool)
	if !ok {
		t.Fatalf("%s missing from a freshly migrated schema", identityIndexName)
	}
	assertIdentityIndexShape(def, t)

	var comment *string
	if err := pool.QueryRow(ctx, `
		SELECT obj_description(c.oid, 'pg_class')
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'metrics' AND c.relname = $1
	`, identityIndexName).Scan(&comment); err != nil {
		t.Fatalf("read %s comment: %v", identityIndexName, err)
	}
	if comment == nil || *comment == "" {
		t.Errorf("%s has no comment", identityIndexName)
	}
}

// TestMigrationV15_UpgradeAddsIndexToExistingSchema simulates an upgrade of
// an installation created before the index existed: it drops the index,
// rewinds schema_version past 15, attaches a weekly partition so the
// partitioned build path is exercised, and re-applies the migration.
func TestMigrationV15_UpgradeAddsIndexToExistingSchema(t *testing.T) {
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

	// Rewind to a pre-v15 state. Migrate compares against the highest
	// recorded version, so the schema_version rows have to go as well.
	if _, err := pool.Exec(ctx, `
		DROP INDEX IF EXISTS metrics.idx_pg_stat_statements_identity_time;
		DELETE FROM schema_version WHERE version >= 15;
	`); err != nil {
		t.Fatalf("rewind to pre-v15 state: %v", err)
	}
	if _, ok := identityIndexDefinition(ctx, t, pool); ok {
		t.Fatal("setup failure: index still present after the rewind")
	}

	const createPartition = `
		CREATE TABLE IF NOT EXISTS metrics.pg_stat_statements_20260914
		PARTITION OF metrics.pg_stat_statements
		FOR VALUES FROM ('2026-09-14') TO ('2026-09-21')
	`
	if _, err := pool.Exec(ctx, createPartition); err != nil {
		t.Fatalf("create partition: %v", err)
	}

	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("re-apply migrate: %v", err)
	}

	def, ok := identityIndexDefinition(ctx, t, pool)
	if !ok {
		t.Fatalf("%s missing after the upgrade", identityIndexName)
	}
	assertIdentityIndexShape(def, t)

	// Creating the index on the partitioned parent must have built a
	// matching index on the partition that already existed, otherwise the
	// aggregation would still sort the rows in that partition.
	var childCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM pg_class parent
		JOIN pg_namespace pn ON pn.oid = parent.relnamespace
		JOIN pg_inherits inh ON inh.inhparent = parent.oid
		JOIN pg_class child ON child.oid = inh.inhrelid
		WHERE pn.nspname = 'metrics'
		  AND parent.relname = $1
		  AND child.relkind = 'i'
	`, identityIndexName).Scan(&childCount); err != nil {
		t.Fatalf("count attached child indexes: %v", err)
	}
	if childCount != 1 {
		t.Errorf("attached child indexes = %d, want 1", childCount)
	}
}

// TestMigrationV15_RerunIsIdempotent verifies that re-applying the
// migration over a schema that already carries the index is a no-op rather
// than an error, which is what IF NOT EXISTS is there for.
func TestMigrationV15_RerunIsIdempotent(t *testing.T) {
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

	before, ok := identityIndexDefinition(ctx, t, pool)
	if !ok {
		t.Fatalf("%s missing from a freshly migrated schema", identityIndexName)
	}

	// Replay migration #15 over a schema that already has the index, the
	// way an interrupted upgrade would.
	if _, err := pool.Exec(ctx,
		`DELETE FROM schema_version WHERE version >= 15`); err != nil {
		t.Fatalf("rewind schema_version: %v", err)
	}
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("re-apply migrate: %v", err)
	}

	after, ok := identityIndexDefinition(ctx, t, pool)
	if !ok {
		t.Fatalf("%s missing after the replay", identityIndexName)
	}
	if before != after {
		t.Errorf("index definition changed on replay:\n before: %s\n after:  %s",
			before, after)
	}

	// Exactly one index of that name must exist on the parent table.
	var count int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'metrics' AND c.relname = $1
	`, identityIndexName).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", identityIndexName, err)
	}
	if count != 1 {
		t.Errorf("indexes named %s = %d, want 1", identityIndexName, count)
	}
}
