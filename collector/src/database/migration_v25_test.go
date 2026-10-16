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

// The v25 migration redacts credentials in pg_settings rows stored
// before the probe redacted them, as described in GitHub issue #643.
// These tests seed rows as an older collector would have stored them,
// rerun the migration, and check what is left.

// migrationV25 returns the registered version 25 migration.
func migrationV25(t *testing.T) Migration {
	t.Helper()
	for _, m := range NewSchemaManager().migrations {
		if m.Version == 25 {
			return m
		}
	}
	t.Fatal("migration version 25 is not registered")
	return Migration{}
}

// TestMigrationV25_Registered verifies the migration is wired into the
// schema manager and will be reached by an upgrade.
func TestMigrationV25_Registered(t *testing.T) {
	if got := NewSchemaManager().LatestVersion(); got < 25 {
		t.Errorf("LatestVersion() = %d, want at least 25", got)
	}
	if desc := migrationV25(t).Description; desc == "" {
		t.Error("migration 25 has an empty description")
	}
}

// v25Row is one seeded pg_settings row.
type v25Row struct {
	name, setting, resetVal string
	bootVal                 *string
}

// seedV25Settings creates a connection and a partition, then stores rows
// for two snapshots with the values an unredacting collector stored.
func seedV25Settings(t *testing.T, pool *pgxpool.Pool, rows []v25Row) {
	t.Helper()
	ctx := context.Background()

	var connID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO connections
			(owner_username, name, host, port, database_name, username)
		VALUES ('v25-user', 'v25-standby', '192.0.2.10', 5432, 'postgres', 'postgres')
		RETURNING id
	`).Scan(&connID); err != nil {
		t.Fatalf("failed to seed connection: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		CREATE TABLE metrics.pg_settings_v25test
			PARTITION OF metrics.pg_settings
			FOR VALUES FROM ('2020-01-06 00:00:00+00') TO ('2020-01-13 00:00:00+00')
	`); err != nil {
		t.Fatalf("failed to create partition: %v", err)
	}

	for _, ts := range []string{"2020-01-06 01:00:00+00", "2020-01-07 01:00:00+00"} {
		for _, r := range rows {
			if _, err := pool.Exec(ctx, `
				INSERT INTO metrics.pg_settings
					(connection_id, collected_at, name, setting, reset_val, boot_val)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, connID, ts, r.name, r.setting, r.resetVal, r.bootVal); err != nil {
				t.Fatalf("failed to seed %s: %v", r.name, err)
			}
		}
	}
}

// runMigrationV25 applies the migration in a committed transaction.
func runMigrationV25(t *testing.T, pool *pgxpool.Pool) {
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
	if err := migrationV25(t).Up(tx); err != nil {
		t.Fatalf("migration 25 failed: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("failed to commit migration 25: %v", err)
	}
}

func strPtr(s string) *string { return &s }

// TestMigrationV25_RedactsStoredValues seeds credential-bearing rows in
// every value column, reruns the migration, and checks each column of
// each row.
func TestMigrationV25_RedactsStoredValues(t *testing.T) {
	pool, cleanup := migrateV21TestSchema(t)
	defer cleanup()
	if pool == nil {
		return
	}

	seedV25Settings(t, pool, []v25Row{
		{
			name:     "primary_conninfo",
			setting:  "host=192.0.2.1 user=replicator password=s3cret",
			resetVal: "postgresql://replicator:0ld@192.0.2.1/postgres",
			bootVal:  strPtr(""),
		},
		{
			name:     "ssl_passphrase_command",
			setting:  "echo s3cret",
			resetVal: "echo s3cret",
			bootVal:  nil,
		},
		{
			name:     "myext.password",
			setting:  "********",
			resetVal: "s3cret",
			bootVal:  strPtr("bootsecret"),
		},
		{
			name:     "work_mem",
			setting:  "password=not-a-secret-here",
			resetVal: "4096",
			bootVal:  strPtr("4096"),
		},
	})

	runMigrationV25(t, pool)

	want := map[string][3]*string{
		"primary_conninfo": {
			strPtr("host=192.0.2.1 user=replicator password=********"),
			strPtr("postgresql://replicator:********@192.0.2.1/postgres"),
			strPtr(""),
		},
		"ssl_passphrase_command": {strPtr("********"), strPtr("********"), nil},
		"myext.password":         {strPtr("********"), strPtr("********"), strPtr("********")},
		"work_mem": {
			strPtr("password=not-a-secret-here"), strPtr("4096"), strPtr("4096"),
		},
	}

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

	// A second run finds nothing left to change.
	runMigrationV25(t, pool)

	for _, column := range columns {
		var comment *string
		if err := pool.QueryRow(context.Background(), `
			SELECT col_description('metrics.pg_settings'::regclass, attnum)
			FROM pg_attribute
			WHERE attrelid = 'metrics.pg_settings'::regclass AND attname = $1
		`, column).Scan(&comment); err != nil {
			t.Fatalf("failed to read comment on %s: %v", column, err)
		}
		if comment == nil || !strings.Contains(*comment, "redacted") {
			t.Errorf("metrics.pg_settings.%s comment = %s, want it to mention redaction",
				column, fmtStrPtr(comment))
		}
	}
}

func equalStrPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func fmtStrPtr(s *string) string {
	if s == nil {
		return "NULL"
	}
	return "'" + *s + "'"
}

// runMigrationV25Broken applies the migration after breaking the schema
// with setup, inside a transaction that is rolled back, and returns the
// migration's error.
func runMigrationV25Broken(t *testing.T, pool *pgxpool.Pool, setup string) error {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil {
			t.Logf("rollback failed: %v", err)
		}
	}()
	if _, err := tx.Exec(ctx, setup); err != nil {
		t.Fatalf("failed to prepare schema: %v", err)
	}
	return migrationV25(t).Up(tx)
}

// TestMigrationV25_ReportsFailures drives each error path of the
// migration by breaking the schema inside a rolled-back transaction.
func TestMigrationV25_ReportsFailures(t *testing.T) {
	pool, cleanup := migrateV21TestSchema(t)
	defer cleanup()
	if pool == nil {
		return
	}

	seedV25Settings(t, pool, []v25Row{{
		name:     "primary_conninfo",
		setting:  "host=192.0.2.1 password=s3cret",
		resetVal: "host=192.0.2.1 password=s3cret",
		bootVal:  strPtr(""),
	}})

	tests := []struct {
		name, setup, want string
	}{
		{
			name:  "no table",
			setup: `ALTER TABLE metrics.pg_settings RENAME TO pg_settings_hidden`,
			want:  "failed to list stored settings",
		},
		{
			name:  "missing value column",
			setup: `ALTER TABLE metrics.pg_settings DROP COLUMN boot_val`,
			want:  "failed to read values of primary_conninfo",
		},
		{
			name: "update rejected",
			setup: `
				CREATE FUNCTION pg_temp.v25_reject() RETURNS trigger
				LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'rejected'; END $$;
				CREATE TRIGGER v25_reject BEFORE UPDATE ON metrics.pg_settings_v25test
				FOR EACH ROW EXECUTE FUNCTION pg_temp.v25_reject();`,
			want: "failed to redact a value of primary_conninfo",
		},
		{
			name: "comment fails",
			setup: `
				DELETE FROM metrics.pg_settings;
				ALTER TABLE metrics.pg_settings DROP COLUMN boot_val;`,
			want: "failed to comment pg_settings value columns",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runMigrationV25Broken(t, pool, tt.setup)
			if err == nil {
				t.Fatal("migration 25 succeeded on a broken schema")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("migration 25 error = %q, want it to contain %q",
					err.Error(), tt.want)
			}
		})
	}
}
