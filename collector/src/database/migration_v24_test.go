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
)

// Migration 24 masks the secrets that metrics.pg_hba_file_rules stored
// before the pg_hba_file_rules probe masked them (issue #680). The
// expected values are the ones TestMaskHBAError and TestMaskHBAOptions
// in the probes package expect, so the history matches what the probe
// now collects.

// migrationV24 returns the registered version 24 migration.
func migrationV24(t *testing.T) Migration {
	t.Helper()
	for _, m := range NewSchemaManager().migrations {
		if m.Version == 24 {
			return m
		}
	}
	t.Fatal("migration version 24 is not registered")
	return Migration{}
}

// hbaV24Row is a stored rule before and after the migration.
type hbaV24Row struct {
	rule        int
	options     []string
	errText     *string
	wantOptions []string
	wantError   *string
}

func v24Str(s string) *string { return &s }

// hbaV24Rows seeds every case the migration handles.
func hbaV24Rows() []hbaV24Row {
	return []hbaV24Row{
		{
			rule: 1,
			options: []string{"ldapserver=ldap.example.com",
				"ldapbinddn=cn=reader", "ldapbindpasswd=S3cret"},
			wantOptions: []string{"ldapserver=ldap.example.com",
				"ldapbinddn=cn=reader", "ldapbindpasswd=********"},
		},
		{
			rule:        2,
			options:     []string{"radiusservers=radius.example.com", "radiussecrets=a=b,S3cret"},
			wantOptions: []string{"radiusservers=radius.example.com", "radiussecrets=********"},
		},
		{
			rule:        3,
			options:     []string{"map=office", "include_realm=false"},
			wantOptions: []string{"map=office", "include_realm=false"},
		},
		{
			rule:        4,
			options:     []string{"ldapbindpasswd=********"},
			wantOptions: []string{"ldapbindpasswd=********"},
		},
		{
			rule:      5,
			errText:   v24Str("authentication option not in name=value format: ldapbindpasswdS3cret"),
			wantError: v24Str("authentication option not in name=value format: ********"),
		},
		{
			rule:      6,
			errText:   v24Str(`invalid IP address "ldapbindpasswd=S3cret": Name or service not known`),
			wantError: v24Str(`invalid IP address "********"`),
		},
		{
			rule:      7,
			errText:   v24Str(`invalid connection type "a"S3cret"`),
			wantError: v24Str(`invalid connection type "********"`),
		},
		{
			rule:      8,
			errText:   v24Str(`authentication method "ldap" requires argument "ldapbasedn", "ldapprefix", or "ldapsuffix" to be set`),
			wantError: v24Str(`authentication method "********"`),
		},
		{
			rule:      9,
			errText:   v24Str("end-of-line before IP address specification"),
			wantError: v24Str("end-of-line before IP address specification"),
		},
		{
			rule: 10,
			errText: v24Str("could not open file \"/etc/postgresql/a.conf\": No such file or directory\n" +
				"authentication option not in name=value format: radiussecretsS3cret"),
			wantError: v24Str("could not open file \"********\"\n" +
				"authentication option not in name=value format: ********"),
		},
		{
			rule:      11,
			errText:   v24Str("authentication option not in name=value format: ********"),
			wantError: v24Str("authentication option not in name=value format: ********"),
		},
		{
			rule: 12,
		},
	}
}

// seedV24Rows opens a transaction on a fully migrated schema, which the
// returned cleanup rolls back, and stores hbaV24Rows in a partition of
// metrics.pg_hba_file_rules created for the purpose.
func seedV24Rows(t *testing.T) (pgx.Tx, func()) {
	t.Helper()
	pool, cleanup := migrateV21TestSchema(t)
	if pool == nil {
		return nil, cleanup
	}
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		cleanup()
		t.Fatalf("failed to begin transaction: %v", err)
	}
	done := func() {
		if err := tx.Rollback(ctx); err != nil &&
			!strings.Contains(err.Error(), "closed") {
			t.Logf("rollback failed: %v", err)
		}
		cleanup()
	}
	if _, err := tx.Exec(ctx, `
		CREATE TABLE metrics.pg_hba_file_rules_20200106
		PARTITION OF metrics.pg_hba_file_rules
		FOR VALUES FROM ('2020-01-06 00:00:00+00') TO ('2020-01-13 00:00:00+00')
	`); err != nil {
		done()
		t.Fatalf("failed to create partition: %v", err)
	}
	var connectionID int
	if err := tx.QueryRow(ctx, `
		INSERT INTO connections (name, host, port, database_name, username, password_encrypted, owner_username)
		VALUES ('v24-test', 'db.example.com', 5432, 'postgres', 'postgres', '', 'v24-test-owner')
		RETURNING id
	`).Scan(&connectionID); err != nil {
		done()
		t.Fatalf("failed to seed connection: %v", err)
	}
	for _, r := range hbaV24Rows() {
		if _, err := tx.Exec(ctx, `
			INSERT INTO metrics.pg_hba_file_rules
			    (connection_id, collected_at, rule_number, line_number,
			     options, error)
			VALUES ($1, '2020-01-07 00:00:00+00', $2, $2, $3, $4)
		`, connectionID, r.rule, r.options, r.errText); err != nil {
			done()
			t.Fatalf("failed to seed rule %d: %v", r.rule, err)
		}
	}
	return tx, done
}

// hbaV24Ctids returns each stored rule's ctid, which changes when an
// UPDATE writes the row.
func hbaV24Ctids(t *testing.T, tx pgx.Tx) map[int]string {
	t.Helper()
	rows, err := tx.Query(context.Background(), `
		SELECT rule_number, ctid::text FROM metrics.pg_hba_file_rules`)
	if err != nil {
		t.Fatalf("failed to read ctids: %v", err)
	}
	defer rows.Close()
	ctids := map[int]string{}
	for rows.Next() {
		var rule int
		var ctid string
		if err := rows.Scan(&rule, &ctid); err != nil {
			t.Fatalf("failed to scan ctid: %v", err)
		}
		ctids[rule] = ctid
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("failed to read ctids: %v", err)
	}
	return ctids
}

// TestMigrationV24_Registered verifies the migration is wired into the
// schema manager with a description.
func TestMigrationV24_Registered(t *testing.T) {
	if got := NewSchemaManager().LatestVersion(); got < 24 {
		t.Errorf("LatestVersion() = %d, want at least 24", got)
	}
	if desc := migrationV24(t).Description; desc == "" {
		t.Error("migration 24 has an empty description")
	}
}

// TestMigrationV24_MasksStoredSecrets verifies that the migration masks
// the stored options and errors as the probe does, rewrites only the
// rows that held a secret, and changes nothing when re-run.
func TestMigrationV24_MasksStoredSecrets(t *testing.T) {
	tx, cleanup := seedV24Rows(t)
	defer cleanup()
	if tx == nil {
		return
	}
	ctx := context.Background()
	before := hbaV24Ctids(t, tx)

	if err := migrationV24(t).Up(tx); err != nil {
		t.Fatalf("migration 24 failed: %v", err)
	}

	str := func(s *string) string {
		if s == nil {
			return "<nil>"
		}
		return *s
	}
	after := hbaV24Ctids(t, tx)
	for _, r := range hbaV24Rows() {
		var options []string
		var errText *string
		if err := tx.QueryRow(ctx, `
			SELECT options, error FROM metrics.pg_hba_file_rules
			WHERE rule_number = $1`, r.rule).Scan(&options, &errText); err != nil {
			t.Fatalf("failed to read rule %d: %v", r.rule, err)
		}
		if strings.Join(options, "|") != strings.Join(r.wantOptions, "|") {
			t.Errorf("rule %d options = %q, want %q", r.rule, options, r.wantOptions)
		}
		if (options == nil) != (r.wantOptions == nil) {
			t.Errorf("rule %d options = %#v, want %#v", r.rule, options, r.wantOptions)
		}
		if str(errText) != str(r.wantError) {
			t.Errorf("rule %d error = %q, want %q", r.rule, str(errText), str(r.wantError))
		}
		needed := strings.Join(r.options, "|") != strings.Join(r.wantOptions, "|") ||
			str(r.errText) != str(r.wantError)
		if rewritten := before[r.rule] != after[r.rule]; rewritten != needed {
			t.Errorf("rule %d rewritten = %v, want %v", r.rule, rewritten, needed)
		}
	}

	if err := migrationV24(t).Up(tx); err != nil {
		t.Fatalf("re-running migration 24 failed: %v", err)
	}
	again := hbaV24Ctids(t, tx)
	for rule, ctid := range after {
		if again[rule] != ctid {
			t.Errorf("re-run rewrote rule %d", rule)
		}
	}
}

// TestMigrationV24_ReportsFailures drives both error returns by
// renaming the column each UPDATE writes.
func TestMigrationV24_ReportsFailures(t *testing.T) {
	tests := []struct {
		column string
		want   string
	}{
		{"options", "failed to mask secret options"},
		{"error", "failed to mask error text"},
	}
	for _, tt := range tests {
		t.Run(tt.column, func(t *testing.T) {
			tx, cleanup := seedV24Rows(t)
			defer cleanup()
			if tx == nil {
				return
			}
			if _, err := tx.Exec(context.Background(),
				"ALTER TABLE metrics.pg_hba_file_rules RENAME COLUMN "+
					pgx.Identifier{tt.column}.Sanitize()+" TO renamed"); err != nil {
				t.Fatalf("failed to rename %s: %v", tt.column, err)
			}
			err := migrationV24(t).Up(tx)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("migration 24 error = %v, want %q", err, tt.want)
			}
		})
	}
}
