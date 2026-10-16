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

// The v11 migration moves deadlocks_detected and temp_files_created onto
// the one hour window the alerter now evaluates them over, and retires
// the table_bloat_ratio rule, as described in GitHub issue #409. These
// tests cover the fresh-install path, where the seed data in the v1
// migration already carries the new values, and the upgrade path, where
// the v11 statements rewrite rows seeded by an older build and clear
// the retired rule's open alerts.

// migrationV11 returns the registered version 11 migration.
func migrationV11(t *testing.T) Migration {
	t.Helper()
	for _, m := range NewSchemaManager().migrations {
		if m.Version == 11 {
			return m
		}
	}
	t.Fatal("migration version 11 is not registered")
	return Migration{}
}

// TestMigrationV11_Registered verifies the migration is wired into the
// schema manager and will be reached by an upgrade. LatestVersion is
// asserted to be at least 11 rather than exactly 11 because later
// migrations are expected to follow.
func TestMigrationV11_Registered(t *testing.T) {
	sm := NewSchemaManager()
	if got := sm.LatestVersion(); got < 11 {
		t.Errorf("LatestVersion() = %d, want at least 11", got)
	}
	if desc := migrationV11(t).Description; desc == "" {
		t.Error("migration 11 has an empty description")
	}
}

// v11RuleState is the per-rule shape both the fresh-install and upgrade
// tests assert on.
type v11RuleState struct {
	unit         string
	threshold    float64
	enabled      bool
	descContains string
}

// v11ExpectedRules lists the values every migrated datastore must carry
// for the three rules the migration touches.
var v11ExpectedRules = map[string]v11RuleState{
	"deadlocks_detected": {
		unit:         "deadlocks/hour",
		threshold:    0,
		enabled:      true,
		descContains: "last hour",
	},
	"temp_files_created": {
		unit:         "files/hour",
		threshold:    100,
		enabled:      true,
		descContains: "work_mem",
	},
	"table_bloat_ratio": {
		unit:         "percent",
		threshold:    50,
		enabled:      false,
		descContains: "Retired",
	},
}

// assertV11Rules reads the three rules back and compares them with
// v11ExpectedRules.
func assertV11Rules(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	for name, want := range v11ExpectedRules {
		var (
			gotUnit      string
			gotThreshold float64
			gotEnabled   bool
			gotDesc      string
		)
		err := pool.QueryRow(ctx, `
			SELECT metric_unit, default_threshold, default_enabled, description
			FROM alert_rules
			WHERE name = $1 AND is_built_in
		`, name).Scan(&gotUnit, &gotThreshold, &gotEnabled, &gotDesc)
		if err != nil {
			t.Errorf("alert_rules row for %s missing: %v", name, err)
			continue
		}
		if gotUnit != want.unit {
			t.Errorf("%s: metric_unit = %q, want %q", name, gotUnit, want.unit)
		}
		if gotThreshold != want.threshold {
			t.Errorf("%s: default_threshold = %v, want %v", name,
				gotThreshold, want.threshold)
		}
		if gotEnabled != want.enabled {
			t.Errorf("%s: default_enabled = %v, want %v", name, gotEnabled,
				want.enabled)
		}
		if !strings.Contains(gotDesc, want.descContains) {
			t.Errorf("%s: description = %q, want it to mention %q", name,
				gotDesc, want.descContains)
		}
	}
}

// TestMigrationV11_BuiltInRuleSemantics verifies that a freshly migrated
// datastore carries the new descriptions and units for the two windowed
// rules, and seeds table_bloat_ratio disabled.
func TestMigrationV11_BuiltInRuleSemantics(t *testing.T) {
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

	assertV11Rules(t, pool)
}

// TestMigrationV11_ReportsStatementFailure drives the migration's error
// path by renaming the table it updates inside a transaction that is
// rolled back afterwards, so a failing statement is reported with
// context rather than swallowed.
func TestMigrationV11_ReportsStatementFailure(t *testing.T) {
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

	err = migrationV11(t).Up(tx)
	if err == nil {
		t.Fatal("migration 11 succeeded without an alert_rules table")
	}
	if !strings.Contains(err.Error(), "retire table_bloat_ratio") {
		t.Errorf("migration 11 error = %q, want it to name the failure",
			err.Error())
	}
}

// TestMigrationV11_UpgradesLegacyRowsAndClearsRetiredAlerts re-runs the
// migration's statements over rows rewritten to look like an older
// install, with a mix of alerts attached. The two windowed rules gain
// their new descriptions and units whilst keeping their thresholds,
// including an operator-tuned one; table_bloat_ratio is disabled and its
// active and acknowledged alerts are cleared, whilst an already cleared
// bloat alert and every alert on another rule are left untouched.
func TestMigrationV11_UpgradesLegacyRowsAndClearsRetiredAlerts(t *testing.T) {
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
	// with an operator-tuned temp file threshold that must survive.
	_, err := pool.Exec(ctx, `
		UPDATE alert_rules
		SET description = 'Deadlocks detected',
		    metric_unit = 'deadlocks'
		WHERE name = 'deadlocks_detected';

		UPDATE alert_rules
		SET description = 'Temporary files being created',
		    metric_unit = 'files',
		    default_threshold = 250
		WHERE name = 'temp_files_created';

		UPDATE alert_rules
		SET description = 'Table bloat ratio exceeds threshold',
		    default_enabled = TRUE
		WHERE name = 'table_bloat_ratio';
	`)
	if err != nil {
		t.Fatalf("failed to rewind alert rules: %v", err)
	}

	// Seed a connection and one alert per (rule, status) combination.
	var connectionID int
	err = pool.QueryRow(ctx, `
		INSERT INTO connections (name, host, port, database_name, username, password_encrypted, owner_username)
		VALUES ('v11-test', '127.0.0.1', 5432, 'postgres', 'postgres', '', 'v11-test-owner')
		RETURNING id
	`).Scan(&connectionID)
	if err != nil {
		t.Fatalf("failed to seed connection: %v", err)
	}

	type seededAlert struct {
		rule   string
		status string
	}
	seeds := []seededAlert{
		{"table_bloat_ratio", "active"},
		{"table_bloat_ratio", "acknowledged"},
		{"table_bloat_ratio", "cleared"},
		{"deadlocks_detected", "active"},
		{"temp_files_created", "acknowledged"},
	}
	ids := make(map[seededAlert]int64, len(seeds))
	for _, s := range seeds {
		var id int64
		err = pool.QueryRow(ctx, `
			INSERT INTO alerts (alert_type, rule_id, connection_id, severity, title, description, status, cleared_at)
			SELECT 'threshold', id, $2, 'warning', $1, 'seeded by test', $3,
			       CASE WHEN $3 = 'cleared' THEN '2026-01-01 00:00:00+00'::timestamptz END
			FROM alert_rules WHERE name = $1
			RETURNING id
		`, s.rule, connectionID, s.status).Scan(&id)
		if err != nil {
			t.Fatalf("failed to seed %s/%s alert: %v", s.rule, s.status, err)
		}
		ids[s] = id
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

	if err := migrationV11(t).Up(tx); err != nil {
		t.Fatalf("migration 11 failed on legacy rows: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("failed to commit migration 11: %v", err)
	}

	// Rules: descriptions, units and enabled state are rewritten, and
	// the tuned threshold is preserved because the migration never
	// touches thresholds.
	want := map[string]v11RuleState{}
	for name, st := range v11ExpectedRules {
		want[name] = st
	}
	tuned := want["temp_files_created"]
	tuned.threshold = 250
	want["temp_files_created"] = tuned
	for name, st := range want {
		var gotUnit, gotDesc string
		var gotThreshold float64
		var gotEnabled bool
		err = pool.QueryRow(ctx, `
			SELECT metric_unit, default_threshold, default_enabled, description
			FROM alert_rules WHERE name = $1
		`, name).Scan(&gotUnit, &gotThreshold, &gotEnabled, &gotDesc)
		if err != nil {
			t.Fatalf("failed to read %s: %v", name, err)
		}
		if gotUnit != st.unit {
			t.Errorf("%s: metric_unit = %q, want %q", name, gotUnit, st.unit)
		}
		if gotThreshold != st.threshold {
			t.Errorf("%s: default_threshold = %v, want %v", name,
				gotThreshold, st.threshold)
		}
		if gotEnabled != st.enabled {
			t.Errorf("%s: default_enabled = %v, want %v", name, gotEnabled,
				st.enabled)
		}
		if !strings.Contains(gotDesc, st.descContains) {
			t.Errorf("%s: description = %q, want it to mention %q", name,
				gotDesc, st.descContains)
		}
	}

	// Alerts: only the open bloat alerts change.
	wantStatus := map[seededAlert]string{
		{"table_bloat_ratio", "active"}:        "cleared",
		{"table_bloat_ratio", "acknowledged"}:  "cleared",
		{"table_bloat_ratio", "cleared"}:       "cleared",
		{"deadlocks_detected", "active"}:       "active",
		{"temp_files_created", "acknowledged"}: "acknowledged",
	}
	for s, id := range ids {
		var status string
		var clearedRecently *bool
		err = pool.QueryRow(ctx, `
			SELECT status, cleared_at > NOW() - INTERVAL '1 minute'
			FROM alerts WHERE id = $1
		`, id).Scan(&status, &clearedRecently)
		if err != nil {
			t.Fatalf("failed to read alert %d (%s/%s): %v", id, s.rule,
				s.status, err)
		}
		if status != wantStatus[s] {
			t.Errorf("%s/%s alert status = %q, want %q", s.rule, s.status,
				status, wantStatus[s])
		}
		switch {
		case s.rule == "table_bloat_ratio" && s.status != "cleared":
			// Cleared by the migration: cleared_at must be freshly set.
			if clearedRecently == nil || !*clearedRecently {
				t.Errorf("%s/%s alert cleared_at not set by migration",
					s.rule, s.status)
			}
		case s.status == "cleared":
			// Already cleared: the historical cleared_at is preserved.
			if clearedRecently == nil || *clearedRecently {
				t.Errorf("%s/%s alert cleared_at was rewritten", s.rule,
					s.status)
			}
		default:
			// Untouched open alerts on other rules stay open with no
			// cleared_at.
			if clearedRecently != nil {
				t.Errorf("%s/%s alert gained a cleared_at", s.rule, s.status)
			}
		}
	}
}
