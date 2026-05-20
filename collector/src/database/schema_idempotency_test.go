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

// The tests in this file exercise re-runnability of the consolidated
// migration #1. The original consolidated migration carried a number of
// non-idempotent DDL statements (ALTER TABLE ... ADD CONSTRAINT, etc.)
// that aborted the migration transaction with duplicate_object the
// moment a partially populated datastore tried to re-run it. The bug
// surfaced in production as SQLSTATE 25P02 ("current transaction is
// aborted, commands ignored until end of transaction block") on the
// statement that followed the duplicate ADD CONSTRAINT, which made the
// failure mode confusing to diagnose. The tests below pin the
// behavior described in the changelog: a fresh database migrates
// cleanly, a clean database migrates again as a no-op, and a database
// that already carries a representative subset of the migration #1
// objects still migrates cleanly.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestMigrateFreshDatabase exercises the happy path. It is intentionally
// thinner than TestMigrateFromScratch because the focus here is on the
// re-runnability story; this single-shot test is the baseline that the
// re-run tests below compare against.
func TestMigrateFreshDatabase(t *testing.T) {
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
		t.Fatalf("fresh migration failed: %v", err)
	}

	// Migration #1 owns fk_anomaly_candidates_embedding when pgvector
	// is available. If the anomaly_embeddings table was created, the
	// FK on anomaly_candidates must also be in place; otherwise we
	// have a partial state that is exactly the bug this change fixes.
	if tableExists(t, pool, "anomaly_embeddings") &&
		!constraintExists(t, pool, "anomaly_candidates",
			"fk_anomaly_candidates_embedding") {
		t.Errorf(
			"anomaly_embeddings table exists but its FK to " +
				"anomaly_candidates is missing")
	}
}

// TestMigrateTwiceIsIdempotent is the direct regression test for the
// reported bug. The interesting case is when we force migration #1 to
// re-execute, which we do by clearing schema_version. The migration
// must still complete because every DDL inside it is now idempotent.
func TestMigrateTwiceIsIdempotent(t *testing.T) {
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
		t.Fatalf("first migration failed: %v", err)
	}

	// Clear schema_version so the migration runner re-applies every
	// migration. The DDL inside must now be idempotent against the
	// fully populated database left behind by the first run.
	if _, err := pool.Exec(ctx,
		`TRUNCATE TABLE schema_version`); err != nil {
		t.Fatalf("failed to clear schema_version: %v", err)
	}

	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("re-applying migrations failed: %v", err)
	}

	// Sanity check: every constraint added by migration #1 should
	// still be present after the re-run.
	expectConstraint(t, pool, "connections",
		"fk_connections_cluster_id")
	expectConstraint(t, pool, "probe_configs",
		"probe_configs_connection_id_fkey")
	expectConstraint(t, pool, "metrics.pg_stat_database",
		"fk_pg_stat_database_connection_id")
	expectConstraint(t, pool, "metrics.pg_stat_replication",
		"fk_pg_stat_replication_connection_id")
}

// TestMigratePartialState reproduces the original failure shape: half
// of the migration #1 tables exist, the schema_version row is missing,
// and the smoking-gun foreign key fk_anomaly_candidates_embedding is
// already in place. The migration must still complete because every
// duplicate-prone DDL is now guarded.
//
// The representative subset is intentionally narrow: anomaly_candidates
// and anomaly_embeddings with the FK already attached (the exact catalog
// state that produced the original 25P02 cascade) plus one unrelated
// table (conversations) so the migration runs past several objects
// before reaching the smoking gun.
func TestMigratePartialState(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	cleanupTestSchema(t, pool)
	defer cleanupTestSchema(t, pool)

	seedPartialState(ctx, t, pool)

	sm := NewSchemaManager()
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf(
			"migration against partial-state database failed: %v", err)
	}

	expectConstraint(t, pool, "anomaly_candidates",
		"fk_anomaly_candidates_embedding")
}

// TestMigrateConstraintAlreadyPresent isolates the exact catalog state
// that produced the user-reported failure: the FK is in place before
// migration #1 starts. Migration #1 re-attempts ADD CONSTRAINT and
// must not raise duplicate_object.
func TestMigrateConstraintAlreadyPresent(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	cleanupTestSchema(t, pool)
	defer cleanupTestSchema(t, pool)

	// First migrate normally so the catalog has every object
	// migration #1 expects to find. Then clear schema_version so the
	// runner re-applies and confirm the FK survives the second run.
	sm := NewSchemaManager()
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("initial migration failed: %v", err)
	}

	if !tableExists(t, pool, "anomaly_embeddings") {
		t.Skip("pgvector not available; FK absent by design")
	}

	if _, err := pool.Exec(ctx,
		`TRUNCATE TABLE schema_version`); err != nil {
		t.Fatalf("failed to clear schema_version: %v", err)
	}

	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("re-applying migrations failed: %v", err)
	}

	expectConstraint(t, pool, "anomaly_candidates",
		"fk_anomaly_candidates_embedding")
}

// TestAddConstraintIfMissingIdempotent exercises the helper directly so
// we have a focused unit test that does not require running the entire
// migration. The helper is called three times against a freshly created
// table; the second and third calls must be no-ops rather than raising
// duplicate_object.
func TestAddConstraintIfMissingIdempotent(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	if _, err := pool.Exec(ctx, `
		DROP TABLE IF EXISTS test_child;
		DROP TABLE IF EXISTS test_parent;
		CREATE TABLE test_parent (
			id SERIAL PRIMARY KEY
		);
		CREATE TABLE test_child (
			id SERIAL PRIMARY KEY,
			parent_id INTEGER
		);
	`); err != nil {
		t.Fatalf("failed to create test tables: %v", err)
	}
	defer func() {
		if _, err := pool.Exec(ctx,
			`DROP TABLE IF EXISTS test_child, test_parent`); err != nil {
			t.Logf("teardown: %v", err)
		}
	}()

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil {
			t.Logf("rollback: %v", err)
		}
	}()

	for i := 0; i < 3; i++ {
		if err := addConstraintIfMissing(ctx, tx,
			"test_child",
			"fk_test_child_parent",
			"FOREIGN KEY (parent_id) REFERENCES test_parent(id)",
		); err != nil {
			t.Fatalf("addConstraintIfMissing call %d failed: %v",
				i+1, err)
		}
	}

	// Confirm the constraint exists exactly once.
	var count int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_constraint
		WHERE conname = 'fk_test_child_parent'
		  AND conrelid = 'test_child'::regclass
	`).Scan(&count); err != nil {
		t.Fatalf("failed to count constraint: %v", err)
	}
	if count != 1 {
		t.Errorf(
			"expected exactly one fk_test_child_parent, got %d",
			count)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}

// TestAddConstraintIfMissingErrorPropagation confirms that a genuine
// DDL error (a constraint that names a column that does not exist on
// the target table) still surfaces as an error from the helper. The
// idempotency guard must not silently swallow real failures.
func TestAddConstraintIfMissingErrorPropagation(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	if _, err := pool.Exec(ctx, `
		DROP TABLE IF EXISTS test_err_table;
		CREATE TABLE test_err_table (
			id SERIAL PRIMARY KEY
		);
	`); err != nil {
		t.Fatalf("failed to create test table: %v", err)
	}
	defer func() {
		if _, err := pool.Exec(ctx,
			`DROP TABLE IF EXISTS test_err_table`); err != nil {
			t.Logf("teardown: %v", err)
		}
	}()

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil {
			t.Logf("rollback: %v", err)
		}
	}()

	err = addConstraintIfMissing(ctx, tx,
		"test_err_table",
		"chk_nonexistent_column",
		"CHECK (does_not_exist > 0)",
	)
	if err == nil {
		t.Fatal(
			"expected error from constraint referencing missing column")
	}
}

// TestAddConstraintIfMissingDuplicateObjectSuppressed exercises the
// defense-in-depth inner EXCEPTION handler in addConstraintIfMissing's
// generated DO block. The outer IF NOT EXISTS guard always skips the
// ALTER on a single-threaded re-run, so the inner handler exists to
// close a TOCTOU race window between the pg_constraint check and the
// ALTER TABLE in concurrent migration scenarios. To prove the handler
// works at the SQL level, this test issues a DO block that mimics the
// helper's inner BEGIN ... EXCEPTION WHEN duplicate_object ... END
// structure unconditionally (i.e. with the IF NOT EXISTS guard
// removed) against a table that already carries the constraint. The
// duplicate_object must be swallowed and the surrounding transaction
// must remain usable.
func TestAddConstraintIfMissingDuplicateObjectSuppressed(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	if _, err := pool.Exec(ctx, `
		DROP TABLE IF EXISTS test_dup_child;
		DROP TABLE IF EXISTS test_dup_parent;
		CREATE TABLE test_dup_parent (
			id SERIAL PRIMARY KEY
		);
		CREATE TABLE test_dup_child (
			id SERIAL PRIMARY KEY,
			parent_id INTEGER
		);
	`); err != nil {
		t.Fatalf("failed to create test tables: %v", err)
	}
	defer func() {
		if _, err := pool.Exec(ctx,
			`DROP TABLE IF EXISTS test_dup_child, test_dup_parent`); err != nil {
			t.Logf("teardown: %v", err)
		}
	}()

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil {
			t.Logf("rollback: %v", err)
		}
	}()

	// Seed the constraint via the helper. This puts the catalog in the
	// "constraint already exists" state that the EXCEPTION handler is
	// designed to tolerate.
	if err := addConstraintIfMissing(ctx, tx,
		"test_dup_child",
		"fk_test_dup_child_parent",
		"FOREIGN KEY (parent_id) REFERENCES test_dup_parent(id)",
	); err != nil {
		t.Fatalf("seed addConstraintIfMissing failed: %v", err)
	}

	// Issue a DO block that mirrors the helper's inner BEGIN ...
	// EXCEPTION WHEN duplicate_object END structure but skips the
	// pg_constraint guard, so the ALTER definitely fires against the
	// already-present constraint. The duplicate_object must be
	// swallowed and the outer transaction must remain usable.
	if _, err := tx.Exec(ctx, `
		DO $$
		BEGIN
			BEGIN
				ALTER TABLE test_dup_child
					ADD CONSTRAINT fk_test_dup_child_parent
					FOREIGN KEY (parent_id)
					REFERENCES test_dup_parent(id);
			EXCEPTION
				WHEN duplicate_object THEN
					NULL;
			END;
		END
		$$;
	`); err != nil {
		t.Fatalf(
			"DO block with EXCEPTION handler should swallow "+
				"duplicate_object, got: %v", err)
	}

	// Outer transaction must still be usable after the swallowed
	// duplicate_object. A poisoned tx would fail with SQLSTATE 25P02.
	var dummy int
	if err := tx.QueryRow(ctx, `SELECT 1`).Scan(&dummy); err != nil {
		t.Fatalf(
			"outer tx unusable after duplicate_object suppression: %v",
			err)
	}

	// The constraint must still be present exactly once; the swallowed
	// duplicate must not have removed or doubled it.
	var count int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_constraint
		WHERE conname = 'fk_test_dup_child_parent'
		  AND conrelid = 'test_dup_child'::regclass
	`).Scan(&count); err != nil {
		t.Fatalf("failed to count constraint: %v", err)
	}
	if count != 1 {
		t.Errorf(
			"expected exactly one fk_test_dup_child_parent, got %d",
			count)
	}

	// Without the EXCEPTION handler the equivalent unguarded ALTER
	// would raise SQLSTATE 42710 (duplicate_object). Confirm that
	// directly so the test pins the behavioral contrast: the helper's
	// generated DO block must contain the handler to remain
	// re-runnable when the IF NOT EXISTS guard is bypassed by a race.
	_, rawErr := tx.Exec(ctx, `
		ALTER TABLE test_dup_child
			ADD CONSTRAINT fk_test_dup_child_parent
			FOREIGN KEY (parent_id)
			REFERENCES test_dup_parent(id)
	`)
	if rawErr == nil {
		t.Fatal(
			"expected raw ALTER TABLE to raise duplicate_object " +
				"without the EXCEPTION handler")
	}
	var pgErr *pgconn.PgError
	if !errors.As(rawErr, &pgErr) || pgErr.Code != "42710" {
		t.Fatalf(
			"expected SQLSTATE 42710 (duplicate_object), got: %v",
			rawErr)
	}
}

// TestQuoteSQLLiteral verifies the small helper that escapes string
// literals embedded in the DO-block produced by addConstraintIfMissing.
// The helper is defensive (every in-tree caller passes a static
// identifier) but apostrophe escaping is still load-bearing.
func TestQuoteSQLLiteral(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"plain", "'plain'"},
		{"with'quote", "'with''quote'"},
		{"multiple'q'uotes", "'multiple''q''uotes'"},
		{"", "''"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got := quoteSQLLiteral(tc.in)
			if got != tc.want {
				t.Errorf(
					"quoteSQLLiteral(%q) = %q, want %q",
					tc.in, got, tc.want)
			}
		})
	}
}

// constraintExists returns true if the named constraint is attached to
// the given (possibly schema-qualified) table.
func constraintExists(t *testing.T, pool *pgxpool.Pool, table, name string) bool {
	t.Helper()
	ctx := context.Background()
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_constraint
			WHERE conname = $1
			  AND conrelid = $2::regclass
		)
	`, name, table).Scan(&exists)
	if err != nil {
		t.Fatalf(
			"failed to check constraint %s on %s: %v",
			name, table, err)
	}
	return exists
}

// expectConstraint asserts the named constraint is attached to the
// given table; useful as a one-liner from the re-run tests.
func expectConstraint(t *testing.T, pool *pgxpool.Pool, table, name string) {
	t.Helper()
	if !constraintExists(t, pool, table, name) {
		t.Errorf("expected constraint %s on %s, not found",
			name, table)
	}
}

// tableExists returns true if the table is visible in
// information_schema.tables (matches both public and explicit schemas).
func tableExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	ctx := context.Background()
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_name = $1
		)
	`, name).Scan(&exists)
	if err != nil {
		t.Fatalf("failed to check table %s: %v", name, err)
	}
	return exists
}

// seedPartialState pre-creates a subset of the migration #1 tables and
// installs the smoking-gun fk_anomaly_candidates_embedding constraint.
// The shape mirrors the catalog state reported in the original
// production failure: anomaly_candidates and anomaly_embeddings with
// the FK in place but no schema_version row. The table definitions
// match migration #1 exactly because a real-world partial state
// arises from a previous run that completed some CREATE TABLEs before
// failing on the FK; CREATE TABLE IF NOT EXISTS does not patch
// columns, so the seed must already carry every column the rest of
// migration #1 references.
func seedPartialState(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	statements := []string{
		`CREATE TABLE IF NOT EXISTS anomaly_candidates (
			id BIGSERIAL PRIMARY KEY,
			connection_id INTEGER NOT NULL,
			database_name TEXT,
			metric_name TEXT NOT NULL,
			metric_value REAL NOT NULL,
			z_score REAL NOT NULL,
			detected_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			context JSONB NOT NULL DEFAULT '{}',
			tier1_pass BOOLEAN NOT NULL DEFAULT FALSE,
			tier2_score REAL,
			tier2_pass BOOLEAN,
			tier3_result TEXT,
			tier3_pass BOOLEAN,
			tier3_error TEXT,
			final_decision TEXT CHECK (final_decision IN ('alert', 'suppress', 'pending')),
			alert_id BIGINT,
			embedding_id BIGINT,
			processed_at TIMESTAMPTZ
		)`,
		`CREATE TABLE IF NOT EXISTS anomaly_embeddings (
			id BIGSERIAL PRIMARY KEY,
			candidate_id BIGINT REFERENCES anomaly_candidates(id) ON DELETE CASCADE,
			model_name TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(candidate_id)
		)`,
		// The smoking-gun constraint: present before the migration
		// runs. The bug-fix guard must recognize this and skip the
		// ADD CONSTRAINT instead of raising duplicate_object.
		`ALTER TABLE anomaly_candidates
			ADD CONSTRAINT fk_anomaly_candidates_embedding
			FOREIGN KEY (embedding_id)
			REFERENCES anomaly_embeddings(id) ON DELETE SET NULL`,
		// A representative unrelated table that the migration will
		// have to step past before reaching the FK.
		`CREATE TABLE IF NOT EXISTS conversations (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL,
			title TEXT NOT NULL,
			provider TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			connection TEXT NOT NULL DEFAULT '',
			messages JSONB NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
	}
	for _, stmt := range statements {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf(
				"failed to seed partial state (%s): %v",
				firstLine(stmt), err)
		}
	}
}

// firstLine returns the first line of a multi-line string trimmed to
// 80 chars, used to label seed statements in test failures.
func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			s = s[:i]
			break
		}
	}
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

// TestRunPgVectorSetup_SuccessAndError exercises the SAVEPOINT helper
// directly. The happy-path subtest creates anomaly_candidates first
// then calls the helper; the error-path subtest deliberately leaves
// anomaly_candidates absent so the FK creation inside the helper
// fails. The SAVEPOINT wrapper must catch the failure, roll back, and
// leave the outer transaction usable for a follow-up Exec.
func TestRunPgVectorSetup_SuccessAndError(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	// Skip if pgvector is not available at all; the helper short-
	// circuits in the caller in that case so the SAVEPOINT path is
	// unreachable.
	if !pgVectorAvailable(t, pool) {
		t.Skip("pgvector extension not available")
	}

	t.Run("Success", func(t *testing.T) {
		dropTables(t, pool, "anomaly_embeddings", "anomaly_candidates")
		if _, err := pool.Exec(ctx, `
			CREATE TABLE anomaly_candidates (
				id BIGSERIAL PRIMARY KEY,
				embedding_id BIGINT
			)
		`); err != nil {
			t.Fatalf("seed: %v", err)
		}
		defer dropTables(t, pool,
			"anomaly_embeddings", "anomaly_candidates")

		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		defer func() {
			if err := tx.Rollback(ctx); err != nil {
				t.Logf("rollback: %v", err)
			}
		}()

		if err := runPgVectorSetup(ctx, tx); err != nil {
			t.Fatalf("runPgVectorSetup failed: %v", err)
		}
		// The outer tx must still be usable after the helper returns.
		var dummy int
		if err := tx.QueryRow(ctx, `SELECT 1`).Scan(&dummy); err != nil {
			t.Fatalf("tx unusable after success: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("Commit: %v", err)
		}
	})

	t.Run("ErrorRolledBack", func(t *testing.T) {
		// Force an inner failure: anomaly_candidates is missing, so
		// CREATE TABLE anomaly_embeddings (with a REFERENCES clause)
		// raises a relation-does-not-exist error. The SAVEPOINT path
		// must catch that, roll back, and surface the original
		// error.
		dropTables(t, pool, "anomaly_embeddings", "anomaly_candidates")

		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		defer func() {
			if err := tx.Rollback(ctx); err != nil {
				t.Logf("rollback: %v", err)
			}
		}()

		err = runPgVectorSetup(ctx, tx)
		if err == nil {
			t.Fatal(
				"expected inner pgvector setup to fail when " +
					"anomaly_candidates is missing")
		}
		// After rollback the outer tx must still be usable.
		var dummy int
		if err := tx.QueryRow(ctx, `SELECT 1`).Scan(&dummy); err != nil {
			t.Fatalf("tx unusable after rollback: %v", err)
		}
	})
}

// TestRunChatMemoryEmbeddingSetup_SuccessAndError mirrors
// TestRunPgVectorSetup_SuccessAndError for the chat_memories helper.
func TestRunChatMemoryEmbeddingSetup_SuccessAndError(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	if !pgVectorAvailable(t, pool) {
		t.Skip("pgvector extension not available")
	}
	if _, err := pool.Exec(ctx,
		`CREATE EXTENSION IF NOT EXISTS vector`); err != nil {
		t.Skipf("could not create vector extension: %v", err)
	}

	t.Run("Success", func(t *testing.T) {
		dropTables(t, pool, "chat_memories")
		if _, err := pool.Exec(ctx, `
			CREATE TABLE chat_memories (
				id BIGSERIAL PRIMARY KEY,
				username TEXT NOT NULL,
				scope TEXT NOT NULL DEFAULT 'user',
				category TEXT NOT NULL,
				content TEXT NOT NULL,
				pinned BOOLEAN NOT NULL DEFAULT FALSE,
				model_name TEXT NOT NULL DEFAULT '',
				created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
				updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
			)
		`); err != nil {
			t.Fatalf("seed: %v", err)
		}
		defer dropTables(t, pool, "chat_memories")

		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		defer func() {
			if err := tx.Rollback(ctx); err != nil {
				t.Logf("rollback: %v", err)
			}
		}()

		if err := runChatMemoryEmbeddingSetup(ctx, tx); err != nil {
			t.Fatalf("runChatMemoryEmbeddingSetup failed: %v", err)
		}
		var dummy int
		if err := tx.QueryRow(ctx, `SELECT 1`).Scan(&dummy); err != nil {
			t.Fatalf("tx unusable after success: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("Commit: %v", err)
		}
	})

	t.Run("ErrorRolledBack", func(t *testing.T) {
		// Force an inner failure: chat_memories table absent, so
		// ALTER TABLE raises "relation does not exist". SAVEPOINT
		// must catch the failure and the outer tx must remain
		// usable.
		dropTables(t, pool, "chat_memories")

		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		defer func() {
			if err := tx.Rollback(ctx); err != nil {
				t.Logf("rollback: %v", err)
			}
		}()

		err = runChatMemoryEmbeddingSetup(ctx, tx)
		if err == nil {
			t.Fatal(
				"expected inner chat_memories setup to fail " +
					"when the table is missing")
		}
		var dummy int
		if err := tx.QueryRow(ctx, `SELECT 1`).Scan(&dummy); err != nil {
			t.Fatalf("tx unusable after rollback: %v", err)
		}
	})
}

// pgVectorAvailable reports whether pg_available_extensions includes
// the vector extension, gating tests that need the runtime extension.
func pgVectorAvailable(t *testing.T, pool *pgxpool.Pool) bool {
	t.Helper()
	ctx := context.Background()
	var available bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_available_extensions
			WHERE name = 'vector'
		)
	`).Scan(&available)
	if err != nil {
		t.Fatalf("failed to check pgvector availability: %v", err)
	}
	return available
}

// fakeSavepointExecer is a hand-rolled savepointExecer used to drive
// runSavepointed through every branch (savepoint failure, body
// failure, rollback failure, release failure, success). Each Exec
// call appends the SQL to seen and consults execHandlers for the
// caller-supplied error for that specific statement.
type fakeSavepointExecer struct {
	seen         []string
	execHandlers map[string]error
}

func (f *fakeSavepointExecer) Exec(
	_ context.Context, sql string, _ ...any,
) (pgconn.CommandTag, error) {
	f.seen = append(f.seen, sql)
	for prefix, err := range f.execHandlers {
		if strings.HasPrefix(sql, prefix) {
			return pgconn.CommandTag{}, err
		}
	}
	return pgconn.CommandTag{}, nil
}

func TestRunSavepointed_Success(t *testing.T) {
	ctx := context.Background()
	tx := &fakeSavepointExecer{}

	var bodyCalled bool
	err := runSavepointed(ctx, tx, "test", func() error {
		bodyCalled = true
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bodyCalled {
		t.Error("body was not invoked")
	}
	wantSQL := []string{"SAVEPOINT test", "RELEASE SAVEPOINT test"}
	if !equalStringSlice(tx.seen, wantSQL) {
		t.Errorf("Exec calls = %v, want %v", tx.seen, wantSQL)
	}
}

func TestRunSavepointed_SavepointFails(t *testing.T) {
	ctx := context.Background()
	want := errors.New("savepoint exec failed")
	tx := &fakeSavepointExecer{
		execHandlers: map[string]error{"SAVEPOINT": want},
	}

	err := runSavepointed(ctx, tx, "test", func() error {
		t.Fatal("body must not be invoked when SAVEPOINT fails")
		return nil
	})
	if !errors.Is(err, want) {
		t.Errorf("error = %v, want wrap of %v", err, want)
	}
}

func TestRunSavepointed_BodyFails_RollbackReleaseSucceed(t *testing.T) {
	ctx := context.Background()
	bodyErr := errors.New("body broke")
	tx := &fakeSavepointExecer{}

	err := runSavepointed(ctx, tx, "test", func() error {
		return bodyErr
	})
	if !errors.Is(err, bodyErr) {
		t.Errorf("error = %v, want wrap of %v", err, bodyErr)
	}
	wantSQL := []string{
		"SAVEPOINT test",
		"ROLLBACK TO SAVEPOINT test",
		"RELEASE SAVEPOINT test",
	}
	if !equalStringSlice(tx.seen, wantSQL) {
		t.Errorf("Exec calls = %v, want %v", tx.seen, wantSQL)
	}
}

func TestRunSavepointed_BodyFails_RollbackAlsoFails(t *testing.T) {
	ctx := context.Background()
	bodyErr := errors.New("body broke")
	rollbackErr := errors.New("rollback broke")
	tx := &fakeSavepointExecer{
		execHandlers: map[string]error{
			"ROLLBACK TO SAVEPOINT": rollbackErr,
		},
	}

	err := runSavepointed(ctx, tx, "test", func() error {
		return bodyErr
	})
	if err == nil {
		t.Fatal("expected error when rollback fails")
	}
	if !errors.Is(err, bodyErr) {
		t.Errorf("expected joined error to wrap bodyErr, got %v", err)
	}
	if !errors.Is(err, rollbackErr) {
		t.Errorf("expected joined error to wrap rollbackErr, got %v", err)
	}
}

func TestRunSavepointed_BodyFails_ReleaseAlsoFails(t *testing.T) {
	ctx := context.Background()
	bodyErr := errors.New("body broke")
	releaseErr := errors.New("release broke")
	tx := &fakeSavepointExecer{
		execHandlers: map[string]error{
			"RELEASE SAVEPOINT": releaseErr,
		},
	}

	err := runSavepointed(ctx, tx, "test", func() error {
		return bodyErr
	})
	if err == nil {
		t.Fatal("expected error when release fails on the rollback path")
	}
	if !errors.Is(err, bodyErr) {
		t.Errorf("expected joined error to wrap bodyErr, got %v", err)
	}
	if !errors.Is(err, releaseErr) {
		t.Errorf("expected joined error to wrap releaseErr, got %v", err)
	}
}

func TestRunSavepointed_ReleaseFails_OnSuccessPath(t *testing.T) {
	ctx := context.Background()
	releaseErr := errors.New("release broke")
	tx := &fakeSavepointExecer{
		execHandlers: map[string]error{
			"RELEASE SAVEPOINT": releaseErr,
		},
	}

	err := runSavepointed(ctx, tx, "test", func() error {
		return nil
	})
	if !errors.Is(err, releaseErr) {
		t.Errorf("error = %v, want wrap of %v", err, releaseErr)
	}
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// dropTables issues a CASCADE drop of each named table; failures are
// logged but do not fail the test, because the helper is used in both
// pre-test setup (where the table may or may not exist) and post-test
// teardown (where the running transaction may have left the table in
// a tricky state).
func dropTables(t *testing.T, pool *pgxpool.Pool, tables ...string) {
	t.Helper()
	ctx := context.Background()
	for _, table := range tables {
		_, err := pool.Exec(ctx,
			"DROP TABLE IF EXISTS "+table+" CASCADE")
		if err != nil {
			t.Logf("drop %s: %v", table, err)
		}
	}
}
