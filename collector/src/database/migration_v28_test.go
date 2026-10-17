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

// The v28 migration describes, in the metrics.pg_settings.setting column
// comment, how the shell-command settings such as archive_command are
// redacted, as described in GitHub issue #676. It rewrites no rows; the
// rewrite of rows stored by an older collector is migration 25's, which
// these tests also drive with shell-command values.

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

// readSettingComment returns the comment on metrics.pg_settings.setting.
func readSettingComment(t *testing.T, pool *pgxpool.Pool) *string {
	t.Helper()
	var comment *string
	if err := pool.QueryRow(context.Background(), `
		SELECT col_description('metrics.pg_settings'::regclass, attnum)
		FROM pg_attribute
		WHERE attrelid = 'metrics.pg_settings'::regclass AND attname = 'setting'
	`).Scan(&comment); err != nil {
		t.Fatalf("failed to read comment on setting: %v", err)
	}
	return comment
}

// TestMigrationV28_DescribesShellCommands clears the comment migration
// 28 sets, reruns the migration, and checks the comment is back. It also
// checks that a value an earlier migration 25 replaced in full is left
// as it is, since the migration rewrites no rows.
func TestMigrationV28_DescribesShellCommands(t *testing.T) {
	pool, cleanup := migrateV21TestSchema(t)
	defer cleanup()
	if pool == nil {
		return
	}

	if comment := readSettingComment(t, pool); comment == nil ||
		!strings.Contains(*comment, "program name") {
		t.Errorf("after Migrate, setting comment = %s, want it to mention the program name",
			fmtStrPtr(comment))
	}

	seedV25Settings(t, pool, []v25Row{
		{name: "archive_command", setting: "********", resetVal: "********",
			bootVal: strPtr("")},
	})
	if _, err := pool.Exec(context.Background(),
		`COMMENT ON COLUMN metrics.pg_settings.setting IS NULL`); err != nil {
		t.Fatalf("failed to clear comment: %v", err)
	}

	if err := runMigrationV28(t, pool, ""); err != nil {
		t.Fatalf("migration 28 failed: %v", err)
	}

	comment := readSettingComment(t, pool)
	if comment == nil || !strings.Contains(*comment, "archive_command") ||
		!strings.Contains(*comment, "program name") {
		t.Errorf("setting comment = %s, want it to describe the archive_command redaction",
			fmtStrPtr(comment))
	}

	rows, err := pool.Query(context.Background(),
		`SELECT setting FROM metrics.pg_settings WHERE name = 'archive_command'`)
	if err != nil {
		t.Fatalf("failed to read pg_settings: %v", err)
	}
	settings, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("failed to scan pg_settings: %v", err)
	}
	if len(settings) != 2 {
		t.Fatalf("got %d archive_command rows, want 2", len(settings))
	}
	for _, s := range settings {
		if s != "********" {
			t.Errorf("archive_command = %q, want it left as ********", s)
		}
	}
}

// TestMigrationV25_KeepsShellCommandProgramNames seeds shell-command
// rows in every value column, as a collector older than migration 25
// stored them, and checks that migration 25, which applies
// pgsettings.RedactValue from the collector that runs it, keeps each
// command's program name on a direct upgrade to this version.
func TestMigrationV25_KeepsShellCommandProgramNames(t *testing.T) {
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

	runMigrationV25(t, pool)

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

	checkSettings := func() {
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
	checkSettings()

	// A second run finds nothing left to change.
	runMigrationV25(t, pool)
	checkSettings()
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
			want:  "failed to comment pg_settings value columns",
		},
		{
			name:  "no column",
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
