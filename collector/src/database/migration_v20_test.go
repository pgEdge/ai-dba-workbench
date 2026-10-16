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

// The v20 migration indexes anomaly_candidates for the alerter's batched
// retention delete and drops the duplicate idx_anomaly_embeddings_candidate;
// see GitHub issue #615. The SQL lives in named constants because the
// Codacy/Semgrep go_sql_rule-concat-sqli rule flags inline multi-line SQL.

const v20IndexSQL = `
	SELECT i.indexdef,
	       COALESCE(obj_description(format('public.%I', i.indexname)::regclass, 'pg_class'), '')
	FROM pg_indexes i
	WHERE i.schemaname = 'public'
	  AND i.tablename = 'anomaly_candidates'
	  AND i.indexname = $1
`

const v20IndexExistsSQL = `
	SELECT EXISTS (
		SELECT 1 FROM pg_indexes
		WHERE schemaname = 'public' AND indexname = $1
	)
`

const v20TableExistsSQL = `SELECT to_regclass('public.anomaly_embeddings') IS NOT NULL`

const v20RewindSQL = `
	DROP INDEX IF EXISTS idx_anomaly_candidates_processed;
	DROP INDEX IF EXISTS idx_anomaly_candidates_embedding;
	DROP INDEX IF EXISTS idx_anomaly_candidates_alert;
	DELETE FROM schema_version WHERE version >= 20;
`

const v20RecreateDuplicateSQL = `
	CREATE INDEX idx_anomaly_embeddings_candidate
		ON public.anomaly_embeddings(candidate_id)
`

// v20Indexes lists each index the migration creates with the column it
// keys on, the partial predicate it carries, and a phrase its comment
// must contain.
var v20Indexes = []struct {
	name, column, predicate, comment string
}{
	{"idx_anomaly_candidates_processed", "processed_at", "processed_at IS NOT NULL", "retention"},
	{"idx_anomaly_candidates_embedding", "embedding_id", "embedding_id IS NOT NULL", "fk_anomaly_candidates_embedding"},
	{"idx_anomaly_candidates_alert", "alert_id", "alert_id IS NOT NULL", "alert_id"},
}

// migrationV20 returns the registered version 20 migration.
func migrationV20(t *testing.T) Migration {
	t.Helper()
	for _, m := range NewSchemaManager().migrations {
		if m.Version == 20 {
			return m
		}
	}
	t.Fatal("migration version 20 is not registered")
	return Migration{}
}

// TestMigrationV20_Registered verifies the migration is wired into the
// schema manager.
func TestMigrationV20_Registered(t *testing.T) {
	if got := NewSchemaManager().LatestVersion(); got < 20 {
		t.Errorf("LatestVersion() = %d, want at least 20", got)
	}
	if desc := migrationV20(t).Description; desc == "" {
		t.Error("migration 20 has an empty description")
	}
}

// v20IndexExists reports whether the named index exists in public.
func v20IndexExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(context.Background(), v20IndexExistsSQL, name).Scan(&exists); err != nil {
		t.Fatalf("check index %s: %v", name, err)
	}
	return exists
}

// v20HasEmbeddings reports whether anomaly_embeddings exists, which it
// does only where pgvector is installed.
func v20HasEmbeddings(t *testing.T, pool *pgxpool.Pool) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(context.Background(), v20TableExistsSQL).Scan(&exists); err != nil {
		t.Fatalf("check anomaly_embeddings: %v", err)
	}
	return exists
}

// v20AssertSchema checks the indexes the migration leaves behind.
func v20AssertSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	for _, idx := range v20Indexes {
		var def, comment string
		if err := pool.QueryRow(ctx, v20IndexSQL, idx.name).Scan(&def, &comment); err != nil {
			t.Errorf("index %s missing: %v", idx.name, err)
			continue
		}
		if !strings.Contains(def, "("+idx.column+")") {
			t.Errorf("index %s does not key on %s: %s", idx.name, idx.column, def)
		}
		if !strings.Contains(def, idx.predicate) {
			t.Errorf("index %s missing predicate %q: %s", idx.name, idx.predicate, def)
		}
		if !strings.Contains(comment, idx.comment) {
			t.Errorf("index %s comment = %q, want it to mention %q", idx.name, comment, idx.comment)
		}
	}

	if v20IndexExists(t, pool, "idx_anomaly_embeddings_candidate") {
		t.Error("idx_anomaly_embeddings_candidate still exists")
	}
	if v20HasEmbeddings(t, pool) &&
		!v20IndexExists(t, pool, "anomaly_embeddings_candidate_id_key") {
		t.Error("anomaly_embeddings_candidate_id_key is missing; candidate_id would be unindexed")
	}
}

// TestMigrationV20_FreshInstall verifies a clean migration creates the
// retention indexes and never creates the duplicate embeddings index.
func TestMigrationV20_FreshInstall(t *testing.T) {
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	cleanupTestSchema(t, pool)
	defer cleanupTestSchema(t, pool)

	if err := NewSchemaManager().Migrate(conn); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}
	v20AssertSchema(t, pool)
}

// TestMigrationV20_Upgrade rewinds to the pre-v20 shape, including the
// duplicate embeddings index older installs carry, re-applies the
// migration, and checks the result.
func TestMigrationV20_Upgrade(t *testing.T) {
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

	if _, err := pool.Exec(ctx, v20RewindSQL); err != nil {
		t.Fatalf("rewind to a pre-v20 state: %v", err)
	}
	if v20HasEmbeddings(t, pool) {
		if _, err := pool.Exec(ctx, v20RecreateDuplicateSQL); err != nil {
			t.Fatalf("recreate duplicate index: %v", err)
		}
	} else {
		t.Log("pgvector unavailable; duplicate index drop not exercised")
	}

	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("re-apply migrate: %v", err)
	}
	v20AssertSchema(t, pool)

	// Re-running the migration on the migrated schema is a no-op.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := migrationV20(t).Up(tx); err != nil {
		t.Errorf("re-run migration 20: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Logf("rollback failed: %v", err)
	}
}

// TestMigrationV20_ReportsStatementFailure drives the migration's error
// path by hiding anomaly_candidates inside a rolled-back transaction.
func TestMigrationV20_ReportsStatementFailure(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	cleanupTestSchema(t, pool)
	defer cleanupTestSchema(t, pool)

	if err := NewSchemaManager().Migrate(conn); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil {
			t.Logf("rollback failed: %v", err)
		}
	}()

	if _, err := tx.Exec(ctx, `ALTER TABLE anomaly_candidates RENAME TO anomaly_candidates_hidden`); err != nil {
		t.Fatalf("failed to hide anomaly_candidates: %v", err)
	}

	err = migrationV20(t).Up(tx)
	if err == nil {
		t.Fatal("migration 20 succeeded without an anomaly_candidates table")
	}
	if !strings.Contains(err.Error(), "retention") {
		t.Errorf("migration 20 error = %q, want it to name the failure", err.Error())
	}
}
