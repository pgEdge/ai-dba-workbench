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
)

// The v17 migration backfills NULL connections.description values and
// makes the column NOT NULL; see GitHub issue #540. These tests cover the
// fresh-install shape, the upgrade path over a row holding NULL, and the
// migration's error path.

// descriptionNullableSQL reports whether connections.description accepts
// NULL and what its default is. It is a named constant because the
// Codacy/Semgrep go_sql_rule-concat-sqli rule flags inline multi-line SQL.
const descriptionNullableSQL = `
	SELECT is_nullable, COALESCE(column_default, '')
	FROM information_schema.columns
	WHERE table_schema = 'public'
	  AND table_name = 'connections'
	  AND column_name = 'description'
`

// migrationV17 returns the registered version 17 migration.
func migrationV17(t *testing.T) Migration {
	t.Helper()
	for _, m := range NewSchemaManager().migrations {
		if m.Version == 17 {
			return m
		}
	}
	t.Fatal("migration version 17 is not registered")
	return Migration{}
}

// TestMigrationV17_Registered verifies the migration is wired into the
// schema manager and will be reached by an upgrade.
func TestMigrationV17_Registered(t *testing.T) {
	sm := NewSchemaManager()
	if got := sm.LatestVersion(); got < 17 {
		t.Errorf("LatestVersion() = %d, want at least 17", got)
	}
	if desc := migrationV17(t).Description; desc == "" {
		t.Error("migration 17 has an empty description")
	}
}

// TestMigrationV17_DescriptionNotNull verifies that a freshly migrated
// datastore rejects a NULL description, still applies the empty-string
// default when the column is omitted, and documents the column.
func TestMigrationV17_DescriptionNotNull(t *testing.T) {
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

	var nullable, def string
	if err := pool.QueryRow(ctx, descriptionNullableSQL).Scan(&nullable, &def); err != nil {
		t.Fatalf("read connections.description metadata: %v", err)
	}
	if nullable != "NO" {
		t.Errorf("connections.description is_nullable = %q, want NO", nullable)
	}
	if !strings.HasPrefix(def, "''") {
		t.Errorf("connections.description default = %q, want ''", def)
	}

	var got string
	if err := pool.QueryRow(ctx, `
		INSERT INTO connections (name, host, database_name, username, owner_username)
		VALUES ('v17-default', 'db.example.com', 'postgres', 'postgres', 'test-user')
		RETURNING description
	`).Scan(&got); err != nil {
		t.Fatalf("insert without description: %v", err)
	}
	if got != "" {
		t.Errorf("omitted description = %q, want the empty string", got)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO connections (name, description, host, database_name, username, owner_username)
		VALUES ('v17-null', NULL, 'db.example.com', 'postgres', 'postgres', 'test-user')
	`); err == nil {
		t.Error("inserting a NULL description succeeded, want a NOT NULL violation")
	}

	var comment string
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(col_description('connections'::regclass, attnum), '')
		FROM pg_attribute
		WHERE attrelid = 'connections'::regclass
		  AND attname = 'description'
	`).Scan(&comment); err != nil {
		t.Fatalf("read description comment: %v", err)
	}
	if !strings.Contains(comment, "empty string") {
		t.Errorf("connections.description comment = %q, want it to mention the empty string", comment)
	}
}

// TestMigrationV17_BackfillsNullDescriptions rewinds an installation to
// its pre-v17 shape, stores a connection with a NULL description the way
// the old server did, then re-applies the migration and checks the row
// was backfilled rather than left to break the connection list.
func TestMigrationV17_BackfillsNullDescriptions(t *testing.T) {
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

	if _, err := pool.Exec(ctx, `
		ALTER TABLE connections ALTER COLUMN description DROP NOT NULL;

		INSERT INTO connections (name, description, host, database_name, username, owner_username)
		VALUES ('v17-null', NULL, 'db.example.com', 'postgres', 'postgres', 'test-user'),
		       ('v17-kept', 'kept as is', 'db.example.com', 'postgres', 'postgres', 'test-user');

		DELETE FROM schema_version WHERE version >= 17;
	`); err != nil {
		t.Fatalf("rewind to a pre-v17 state: %v", err)
	}

	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("re-apply migrate: %v", err)
	}

	want := map[string]string{"v17-null": "", "v17-kept": "kept as is"}
	for name, wantDesc := range want {
		var got string
		if err := pool.QueryRow(ctx,
			`SELECT description FROM connections WHERE name = $1`,
			name).Scan(&got); err != nil {
			t.Fatalf("read description of %s: %v", name, err)
		}
		if got != wantDesc {
			t.Errorf("%s description = %q, want %q", name, got, wantDesc)
		}
	}

	var nullable, def string
	if err := pool.QueryRow(ctx, descriptionNullableSQL).Scan(&nullable, &def); err != nil {
		t.Fatalf("read connections.description metadata: %v", err)
	}
	if nullable != "NO" {
		t.Errorf("connections.description is_nullable = %q after upgrade, want NO", nullable)
	}

	var v17Count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM schema_version WHERE version = 17`).Scan(&v17Count); err != nil {
		t.Fatalf("count v17 rows: %v", err)
	}
	if v17Count != 1 {
		t.Errorf("expected exactly one schema_version row for v17, got %d", v17Count)
	}
}

// TestMigrationV17_ReportsStatementFailure drives the migration's error
// path by renaming the table it alters, inside a transaction that is
// rolled back afterwards, so a failing statement is reported with context
// rather than swallowed.
func TestMigrationV17_ReportsStatementFailure(t *testing.T) {
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

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil {
			t.Logf("rollback failed: %v", err)
		}
	}()

	if _, err := tx.Exec(ctx,
		`ALTER TABLE connections RENAME TO connections_hidden`); err != nil {
		t.Fatalf("failed to hide connections: %v", err)
	}

	err = migrationV17(t).Up(tx)
	if err == nil {
		t.Fatal("migration 17 succeeded without a connections table")
	}
	if !strings.Contains(err.Error(), "connections.description") {
		t.Errorf("migration 17 error = %q, want it to name the failure",
			err.Error())
	}
}
