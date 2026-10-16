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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The v28 migration redacts the shell-command settings, such as
// archive_command, in pg_settings rows stored before the probe redacted
// them, as described in GitHub issue #676. These tests seed rows as an
// older collector would have stored them, rerun the migration, and check
// what is left.

// migrationV28 returns the registered version 28 migration.
func migrationV28(t *testing.T) Migration {
	t.Helper()
	for _, m := range NewSchemaManager().migrations {
		if m.Version == 28 {
			return m
		}
	}
	t.Fatal("migration version 28 is not registered")
	return Migration{}
}

// TestMigrationV28_Registered verifies the migration is wired into the
// schema manager and will be reached by an upgrade.
func TestMigrationV28_Registered(t *testing.T) {
	if got := NewSchemaManager().LatestVersion(); got < 28 {
		t.Errorf("LatestVersion() = %d, want at least 28", got)
	}
	if desc := migrationV28(t).Description; desc == "" {
		t.Error("migration 28 has an empty description")
	}
}

// runMigrationV28 applies the migration in a transaction, after running
// setup in it, and commits only if the migration succeeds.
func runMigrationV28(t *testing.T, pool *pgxpool.Pool, setup string) error {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil &&
			!strings.Contains(err.Error(), "closed") {
			t.Logf("rollback failed: %v", err)
		}
	}()
	if setup != "" {
		if _, err := tx.Exec(ctx, setup); err != nil {
			t.Fatalf("failed to prepare schema: %v", err)
		}
	}
	if err := migrationV28(t).Up(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("failed to commit migration 28: %v", err)
	}
	return nil
}

// TestMigrationV28_RedactsShellCommands seeds shell-command rows in
// every value column, reruns the migration, and checks each column of
// each row.
func TestMigrationV28_RedactsShellCommands(t *testing.T) {
	pool, cleanup := migrateV21TestSchema(t)
	defer cleanup()
	if pool == nil {
		return
	}

	seedV25Settings(t, pool, []v25Row{
		{
			name:     "archive_command",
			setting:  "PGPASSWORD=s3cret pgbackrest --stanza=main archive-push %p",
			resetVal: "(cd /archive && cp %p %f)",
			bootVal:  strPtr(""),
		},
		{
			name:     "restore_command",
			setting:  "aws s3 cp s3://bucket/%f %p",
			resetVal: "aws ********",
			bootVal:  nil,
		},
		{
			name:     "archive_cleanup_command",
			setting:  "pg_archivecleanup /archive %r",
			resetVal: "",
			bootVal:  strPtr(""),
		},
		{
			name:     "recovery_end_command",
			setting:  "/usr/local/bin/notify.sh",
			resetVal: "/usr/local/bin/notify.sh",
			bootVal:  strPtr(""),
		},
		{
			name:     "work_mem",
			setting:  "4096",
			resetVal: "4096",
			bootVal:  strPtr("4096"),
		},
	})

	if err := runMigrationV28(t, pool, ""); err != nil {
		t.Fatalf("migration 28 failed: %v", err)
	}

	want := map[string][3]*string{
		"archive_command": {
			strPtr("PGPASSWORD=******** pgbackrest ********"),
			strPtr("********"),
			strPtr(""),
		},
		"restore_command": {
			strPtr("aws ********"), strPtr("aws ********"), nil,
		},
		"archive_cleanup_command": {
			strPtr("pg_archivecleanup ********"), strPtr(""), strPtr(""),
		},
		"recovery_end_command": {
			strPtr("/usr/local/bin/notify.sh"),
			strPtr("/usr/local/bin/notify.sh"),
			strPtr(""),
		},
		"work_mem": {strPtr("4096"), strPtr("4096"), strPtr("4096")},
	}

	checkV28Settings := func() {
		t.Helper()
		rows, err := pool.Query(context.Background(), `
			SELECT name, setting, reset_val, boot_val
			FROM metrics.pg_settings
			ORDER BY name, collected_at
		`)
		if err != nil {
			t.Fatalf("failed to read pg_settings: %v", err)
		}
		type got struct {
			name  string
			value [3]*string
		}
		results, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (got, error) {
			var g got
			err := r.Scan(&g.name, &g.value[0], &g.value[1], &g.value[2])
			return g, err
		})
		if err != nil {
			t.Fatalf("failed to scan pg_settings: %v", err)
		}
		if len(results) != 2*len(want) {
			t.Fatalf("got %d rows, want %d", len(results), 2*len(want))
		}
		columns := []string{"setting", "reset_val", "boot_val"}
		for _, r := range results {
			w := want[r.name]
			for i := range columns {
				if !equalStrPtr(r.value[i], w[i]) {
					t.Errorf("%s.%s = %s, want %s", r.name, columns[i],
						fmtStrPtr(r.value[i]), fmtStrPtr(w[i]))
				}
			}
		}
	}
	checkV28Settings()

	// A second run finds nothing left to change.
	if err := runMigrationV28(t, pool, ""); err != nil {
		t.Fatalf("second run of migration 28 failed: %v", err)
	}
	checkV28Settings()

	var comment *string
	if err := pool.QueryRow(context.Background(), `
		SELECT col_description('metrics.pg_settings'::regclass, attnum)
		FROM pg_attribute
		WHERE attrelid = 'metrics.pg_settings'::regclass AND attname = 'setting'
	`).Scan(&comment); err != nil {
		t.Fatalf("failed to read comment on setting: %v", err)
	}
	if comment == nil || !strings.Contains(*comment, "archive_command") {
		t.Errorf("metrics.pg_settings.setting comment = %s, want it to mention archive_command",
			fmtStrPtr(comment))
	}
}

// TestMigrationV28_ReportsFailures drives each error path of the
// migration by breaking the schema inside a rolled-back transaction.
func TestMigrationV28_ReportsFailures(t *testing.T) {
	pool, cleanup := migrateV21TestSchema(t)
	defer cleanup()
	if pool == nil {
		return
	}

	tests := []struct {
		name, setup, want string
	}{
		{
			name:  "no table",
			setup: `ALTER TABLE metrics.pg_settings RENAME TO pg_settings_hidden`,
			want:  "failed to redact stored pg_settings values",
		},
		{
			name:  "comment fails",
			setup: `ALTER TABLE metrics.pg_settings DROP COLUMN setting`,
			want:  "failed to comment pg_settings value columns",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runMigrationV28(t, pool, tt.setup)
			if err == nil {
				t.Fatal("migration 28 succeeded on a broken schema")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("migration 28 error = %q, want it to contain %q",
					err.Error(), tt.want)
			}
		})
	}
}
