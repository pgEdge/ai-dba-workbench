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

// The v25 migration redacts passwords in statement text stored before
// the probes redacted it, as described in GitHub issue #643. These tests
// seed rows as an older collector would have stored them, rerun the
// migration, and check what is left.

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

// v25Texts are the statement texts seeded into every text column, with
// the text the migration should leave behind. The values are fake.
var v25Texts = []struct{ stored, want string }{
	{
		"ALTER ROLE app PASSWORD 'hunter2'",
		"ALTER ROLE app PASSWORD '********'",
	},
	{
		"CREATE SUBSCRIPTION s CONNECTION 'host=192.0.2.1 password=s3cret' PUBLICATION p",
		"CREATE SUBSCRIPTION s CONNECTION 'host=192.0.2.1 password=********' PUBLICATION p",
	},
	{
		"SELECT * FROM orders WHERE id = $1",
		"SELECT * FROM orders WHERE id = $1",
	},
	{
		// Matches the pre-filter but holds no secret.
		"SELECT passcode FROM t",
		"SELECT passcode FROM t",
	},
}

// v25CollectedAt is when the seeded rows were collected, in the week
// covered by the test partitions.
const v25CollectedAt = "2020-01-06 01:00:00+00"

// seedV25Statements creates a connection and partitions, then stores each
// text in pg_stat_activity, pg_stat_statements and both text columns of
// spock_exception_log.
func seedV25Statements(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	var connID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO connections
			(owner_username, name, host, port, database_name, username)
		VALUES ('v25-user', 'v25-server', '192.0.2.20', 5432, 'postgres', 'postgres')
		RETURNING id
	`).Scan(&connID); err != nil {
		t.Fatalf("failed to seed connection: %v", err)
	}

	for _, stmt := range []string{
		`CREATE TABLE metrics.pg_stat_activity_v25test
			PARTITION OF metrics.pg_stat_activity
			FOR VALUES FROM ('2020-01-06 00:00:00+00') TO ('2020-01-13 00:00:00+00')`,
		`CREATE TABLE metrics.pg_stat_statements_v25test
			PARTITION OF metrics.pg_stat_statements
			FOR VALUES FROM ('2020-01-06 00:00:00+00') TO ('2020-01-13 00:00:00+00')`,
		`CREATE TABLE metrics.spock_exception_log_v25test
			PARTITION OF metrics.spock_exception_log
			FOR VALUES FROM ('2020-01-06 00:00:00+00') TO ('2020-01-13 00:00:00+00')`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("failed to create partition: %v\nSQL: %s", err, stmt)
		}
	}

	for i, text := range v25Texts {
		if _, err := pool.Exec(ctx, `
			INSERT INTO metrics.pg_stat_activity
				(connection_id, pid, query, collected_at)
			VALUES ($1, $2, $3, $4)
		`, connID, 1000+i, text.stored, v25CollectedAt); err != nil {
			t.Fatalf("failed to seed pg_stat_activity: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO metrics.pg_stat_statements
				(connection_id, database_name, userid, dbid, queryid, query, collected_at)
			VALUES ($1, 'postgres', 10, 5, $2, $3, $4)
		`, connID, int64(i), text.stored, v25CollectedAt); err != nil {
			t.Fatalf("failed to seed pg_stat_statements: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO metrics.spock_exception_log
				(connection_id, database_name, collected_at, remote_origin,
				 remote_commit_ts, command_counter, retry_errored_at,
				 remote_xid, ddl_statement, error_message)
			VALUES ($1, 'postgres', $2, 1, $2, $3, $2, 1, $4, $4)
		`, connID, v25CollectedAt, i, text.stored); err != nil {
			t.Fatalf("failed to seed spock_exception_log: %v", err)
		}
	}

	// A NULL text is left alone.
	if _, err := pool.Exec(ctx, `
		INSERT INTO metrics.pg_stat_activity
			(connection_id, pid, query, collected_at)
		VALUES ($1, 1, NULL, $2)
	`, connID, v25CollectedAt); err != nil {
		t.Fatalf("failed to seed NULL query: %v", err)
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

// v25Columns names each statement-text column with the query that reads
// it back, ordered as the rows were seeded.
var v25Columns = []struct{ name, sql string }{
	{
		"pg_stat_activity.query",
		`SELECT query FROM metrics.pg_stat_activity WHERE pid >= 1000 ORDER BY pid`,
	},
	{
		"pg_stat_statements.query",
		`SELECT query FROM metrics.pg_stat_statements ORDER BY queryid`,
	},
	{
		"spock_exception_log.ddl_statement",
		`SELECT ddl_statement FROM metrics.spock_exception_log ORDER BY command_counter`,
	},
	{
		"spock_exception_log.error_message",
		`SELECT error_message FROM metrics.spock_exception_log ORDER BY command_counter`,
	},
}

// TestMigrationV25_RedactsStoredText seeds statement text in every text
// column, reruns the migration, and checks each value and column comment.
func TestMigrationV25_RedactsStoredText(t *testing.T) {
	pool, cleanup := migrateV21TestSchema(t)
	defer cleanup()
	if pool == nil {
		return
	}

	seedV25Statements(t, pool)
	runMigrationV25(t, pool)

	ctx := context.Background()
	for _, col := range v25Columns {
		got, err := queryPoolStrings(ctx, pool, col.sql)
		if err != nil {
			t.Fatalf("failed to read %s: %v", col.name, err)
		}
		if len(got) != len(v25Texts) {
			t.Fatalf("%s: got %d rows, want %d", col.name, len(got), len(v25Texts))
		}
		for i, text := range v25Texts {
			if got[i] != text.want {
				t.Errorf("%s row %d = %q, want %q", col.name, i, got[i], text.want)
			}
		}
	}

	var nullQuery *string
	if err := pool.QueryRow(ctx, `
		SELECT query FROM metrics.pg_stat_activity WHERE pid = 1
	`).Scan(&nullQuery); err != nil {
		t.Fatalf("failed to read NULL query: %v", err)
	}
	if nullQuery != nil {
		t.Errorf("NULL query became %q", *nullQuery)
	}

	// A second run finds nothing left to change.
	runMigrationV25(t, pool)

	for _, column := range []struct{ table, column string }{
		{"metrics.pg_stat_activity", "query"},
		{"metrics.pg_stat_statements", "query"},
		{"metrics.spock_exception_log", "ddl_statement"},
		{"metrics.spock_exception_log", "error_message"},
	} {
		var comment *string
		if err := pool.QueryRow(ctx, `
			SELECT col_description($1::regclass, attnum)
			FROM pg_attribute
			WHERE attrelid = $1::regclass AND attname = $2
		`, column.table, column.column).Scan(&comment); err != nil {
			t.Fatalf("failed to read comment on %s.%s: %v", column.table, column.column, err)
		}
		if comment == nil || !strings.Contains(*comment, "redacted") {
			t.Errorf("%s.%s comment = %s, want it to mention redaction",
				column.table, column.column, fmtStrPtr(comment))
		}
	}
}

// queryPoolStrings runs a query returning one text column on the pool.
func queryPoolStrings(ctx context.Context, pool *pgxpool.Pool, sql string) ([]string, error) {
	rows, err := pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
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

	seedV25Statements(t, pool)

	tests := []struct {
		name, setup, want string
	}{
		{
			name:  "no table",
			setup: `ALTER TABLE metrics.pg_stat_activity RENAME TO pg_stat_activity_hidden`,
			want:  "failed to read metrics.pg_stat_activity.query",
		},
		{
			name: "update rejected",
			setup: `
				CREATE FUNCTION pg_temp.v25_reject() RETURNS trigger
				LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'rejected'; END $$;
				CREATE TRIGGER v25_reject BEFORE UPDATE ON metrics.pg_stat_statements_v25test
				FOR EACH ROW EXECUTE FUNCTION pg_temp.v25_reject();`,
			want: "failed to redact metrics.pg_stat_statements.query",
		},
		{
			name: "missing column",
			setup: `
				DELETE FROM metrics.spock_exception_log;
				ALTER TABLE metrics.spock_exception_log DROP COLUMN error_message;`,
			want: "failed to read metrics.spock_exception_log.error_message",
		},
		{
			// Nothing is left to rewrite, and a role that may read the
			// tables but does not own them cannot comment on them.
			name: "comment fails",
			setup: `
				DELETE FROM metrics.pg_stat_activity;
				DELETE FROM metrics.pg_stat_statements;
				DELETE FROM metrics.spock_exception_log;
				CREATE ROLE v25_comment_reader;
				GRANT USAGE ON SCHEMA metrics TO v25_comment_reader;
				GRANT SELECT ON ALL TABLES IN SCHEMA metrics TO v25_comment_reader;
				SET LOCAL ROLE v25_comment_reader;`,
			want: "failed to comment statement text columns",
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
