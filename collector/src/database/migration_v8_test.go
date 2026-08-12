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

// The v8 migration realigns three built-in alert rules with the metric
// semantics the alerter implements, as described in GitHub issue #406.
// These tests cover both the fresh-install path, where the seed data in
// the v1 migration already carries the new values, and the upgrade
// path, where the v8 statements rewrite rows seeded by an older build.

// migrationV8 returns the registered version 8 migration.
func migrationV8(t *testing.T) Migration {
	t.Helper()
	for _, m := range NewSchemaManager().migrations {
		if m.Version == 8 {
			return m
		}
	}
	t.Fatal("migration version 8 is not registered")
	return Migration{}
}

// TestMigrationV8_RegisteredAsLatest verifies the migration is wired
// into the schema manager and is the newest version, so a collector
// upgrade actually applies it.
func TestMigrationV8_RegisteredAsLatest(t *testing.T) {
	sm := NewSchemaManager()
	if got := sm.LatestVersion(); got != 8 {
		t.Errorf("LatestVersion() = %d, want 8", got)
	}
	if desc := migrationV8(t).Description; desc == "" {
		t.Error("migration 8 has an empty description")
	}
}

// TestMigrationV8_BuiltInRuleSemantics verifies that a freshly migrated
// datastore carries the corrected descriptions, units and threshold for
// the three rules the migration touches.
func TestMigrationV8_BuiltInRuleSemantics(t *testing.T) {
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
		name         string
		unit         string
		threshold    float64
		descContains string
	}{
		{
			name:         "checkpoint_warning",
			unit:         "checkpoints/hour",
			threshold:    12,
			descContains: "last hour",
		},
		{
			name:         "wal_archive_failed",
			unit:         "failures/hour",
			threshold:    0,
			descContains: "last hour",
		},
		{
			name:         "transaction_wraparound",
			unit:         "percent",
			threshold:    75,
			descContains: "wraparound limit",
		},
	}

	for _, tc := range cases {
		var (
			gotUnit      string
			gotThreshold float64
			gotDesc      string
		)
		err := pool.QueryRow(ctx, `
			SELECT metric_unit, default_threshold, description
			FROM alert_rules
			WHERE name = $1 AND is_built_in
		`, tc.name).Scan(&gotUnit, &gotThreshold, &gotDesc)
		if err != nil {
			t.Errorf("alert_rules row for %s missing: %v", tc.name, err)
			continue
		}
		if gotUnit != tc.unit {
			t.Errorf("%s: metric_unit = %q, want %q", tc.name, gotUnit,
				tc.unit)
		}
		if gotThreshold != tc.threshold {
			t.Errorf("%s: default_threshold = %v, want %v", tc.name,
				gotThreshold, tc.threshold)
		}
		if !strings.Contains(gotDesc, tc.descContains) {
			t.Errorf("%s: description = %q, want it to mention %q",
				tc.name, gotDesc, tc.descContains)
		}
	}

	// The unit column carries a comment explaining the windowed units.
	var comment string
	err := pool.QueryRow(ctx, `
		SELECT COALESCE(col_description('alert_rules'::regclass, attnum), '')
		FROM pg_attribute
		WHERE attrelid = 'alert_rules'::regclass
		  AND attname = 'metric_unit'
	`).Scan(&comment)
	if err != nil {
		t.Fatalf("failed to read alert_rules.metric_unit comment: %v", err)
	}
	if !strings.Contains(comment, "hour") {
		t.Errorf("alert_rules.metric_unit comment = %q, want it to "+
			"describe the windowed units", comment)
	}
}

// TestMigrationV8_ReportsStatementFailure drives the migration's error
// path by renaming the table it updates inside a transaction that is
// rolled back afterwards, so a failing statement is reported with
// context rather than swallowed.
func TestMigrationV8_ReportsStatementFailure(t *testing.T) {
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
		`ALTER TABLE alert_rules RENAME TO alert_rules_hidden`); err != nil {
		t.Fatalf("failed to hide alert_rules: %v", err)
	}

	err = migrationV8(t).Up(tx)
	if err == nil {
		t.Fatal("migration 8 succeeded without an alert_rules table")
	}
	if !strings.Contains(err.Error(), "realign built-in alert rule") {
		t.Errorf("migration 8 error = %q, want it to name the failure",
			err.Error())
	}
}

// TestMigrationV8_UpgradesLegacyRowsAndKeepsTuning re-runs the
// migration's statements over rows rewritten to look like an older
// install. The shipped checkpoint threshold of 50 must be replaced,
// while an operator's tuned value must survive untouched; that
// distinction is the whole reason the update is conditional.
func TestMigrationV8_UpgradesLegacyRowsAndKeepsTuning(t *testing.T) {
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

	// Rewind the three rules to the values an older collector seeded,
	// and add a fourth row standing in for an operator who tuned the
	// checkpoint threshold before upgrading.
	_, err := pool.Exec(ctx, `
		UPDATE alert_rules
		SET description = 'Checkpoints requested too frequently',
		    metric_unit = 'checkpoints',
		    default_threshold = 50
		WHERE name = 'checkpoint_warning';

		UPDATE alert_rules
		SET description = 'WAL archiving failures detected',
		    metric_unit = 'failures'
		WHERE name = 'wal_archive_failed';

		UPDATE alert_rules
		SET description = 'Transaction ID wraparound approaching'
		WHERE name = 'transaction_wraparound';

		INSERT INTO alert_rules (name, description, category, metric_name,
		    metric_unit, default_operator, default_threshold,
		    default_severity, default_enabled, is_built_in)
		VALUES ('checkpoint_warning_tuned', 'Tuned copy', 'wal',
		    'pg_stat_checkpointer.checkpoints_req_delta', 'checkpoints',
		    '>', 25, 'warning', TRUE, TRUE)
		ON CONFLICT (name) DO NOTHING;
	`)
	if err != nil {
		t.Fatalf("failed to rewind alert rules: %v", err)
	}

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

	if err := migrationV8(t).Up(tx); err != nil {
		t.Fatalf("migration 8 failed on legacy rows: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("failed to commit migration 8: %v", err)
	}

	var threshold float64
	var unit, desc string
	err = pool.QueryRow(ctx, `
		SELECT default_threshold, metric_unit, description
		FROM alert_rules WHERE name = 'checkpoint_warning'
	`).Scan(&threshold, &unit, &desc)
	if err != nil {
		t.Fatalf("failed to read checkpoint_warning: %v", err)
	}
	if threshold != 12 {
		t.Errorf("checkpoint_warning threshold = %v, want 12", threshold)
	}
	if unit != "checkpoints/hour" {
		t.Errorf("checkpoint_warning unit = %q, want checkpoints/hour", unit)
	}
	if !strings.Contains(desc, "max_wal_size") {
		t.Errorf("checkpoint_warning description = %q, want it to mention "+
			"max_wal_size", desc)
	}

	// The tuned row keeps its operator-chosen threshold, because the
	// update only rewrites rows still at the old shipped default.
	var tuned float64
	err = pool.QueryRow(ctx, `
		SELECT default_threshold FROM alert_rules
		WHERE name = 'checkpoint_warning_tuned'
	`).Scan(&tuned)
	if err != nil {
		t.Fatalf("failed to read the tuned rule: %v", err)
	}
	if tuned != 25 {
		t.Errorf("tuned checkpoint threshold = %v, want 25 (unchanged)",
			tuned)
	}

	for _, tc := range []struct {
		name string
		unit string
		want string
	}{
		{"wal_archive_failed", "failures/hour", "last hour"},
		{"transaction_wraparound", "percent", "wraparound limit"},
	} {
		var gotUnit, gotDesc string
		err = pool.QueryRow(ctx, `
			SELECT metric_unit, description FROM alert_rules WHERE name = $1
		`, tc.name).Scan(&gotUnit, &gotDesc)
		if err != nil {
			t.Errorf("failed to read %s: %v", tc.name, err)
			continue
		}
		if gotUnit != tc.unit {
			t.Errorf("%s: metric_unit = %q, want %q", tc.name, gotUnit,
				tc.unit)
		}
		if !strings.Contains(gotDesc, tc.want) {
			t.Errorf("%s: description = %q, want it to mention %q", tc.name,
				gotDesc, tc.want)
		}
	}
}
