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

// The v16 migration seeds the probe_unavailable built-in alert rule,
// which reports a probe that was collecting and has stopped being
// available; see GitHub issue #512. These tests cover the fresh-install
// path, the upgrade path over an installation that already carries an
// operator-edited rule of that name, and the migration's error path.

// probeUnavailableRuleSelectSQL reads back every column the seed sets.
// It is a named constant for the same reason as the SQL constants in the
// alerter tests: the Codacy/Semgrep go_sql_rule-concat-sqli rule flags
// inline multi-line SQL, and the value is still bound via $1.
const probeUnavailableRuleSelectSQL = `
	SELECT description, category, metric_name, metric_unit, default_operator,
	       default_threshold, default_severity, default_enabled,
	       required_extension, is_built_in
	FROM alert_rules
	WHERE name = $1
`

// migrationV16 returns the registered version 16 migration.
func migrationV16(t *testing.T) Migration {
	t.Helper()
	for _, m := range NewSchemaManager().migrations {
		if m.Version == 16 {
			return m
		}
	}
	t.Fatal("migration version 16 is not registered")
	return Migration{}
}

// v16Rule is the seeded shape of the probe_unavailable rule.
type v16Rule struct {
	description string
	category    string
	metricName  string
	unit        *string
	operator    string
	threshold   float64
	severity    string
	enabled     bool
	extension   *string
	builtIn     bool
}

// readProbeUnavailableRule reads the probe_unavailable row, failing the
// test when it is absent.
func readProbeUnavailableRule(t *testing.T, pool *pgxpool.Pool) v16Rule {
	t.Helper()

	var got v16Rule
	err := pool.QueryRow(context.Background(), probeUnavailableRuleSelectSQL,
		"probe_unavailable").Scan(&got.description, &got.category, &got.metricName,
		&got.unit, &got.operator, &got.threshold, &got.severity, &got.enabled,
		&got.extension, &got.builtIn)
	if err != nil {
		t.Fatalf("alert_rules row for probe_unavailable missing: %v", err)
	}
	return got
}

// TestMigrationV16_Registered verifies the migration is wired into the
// schema manager and will be reached by an upgrade. LatestVersion is
// asserted to be at least 16 rather than exactly 16 because later
// migrations are expected to follow.
func TestMigrationV16_Registered(t *testing.T) {
	sm := NewSchemaManager()
	if got := sm.LatestVersion(); got < 16 {
		t.Errorf("LatestVersion() = %d, want at least 16", got)
	}
	if desc := migrationV16(t).Description; desc == "" {
		t.Error("migration 16 has an empty description")
	}
}

// TestMigrationV16_SeedsProbeUnavailableRule verifies that a freshly
// migrated datastore carries the rule with the values the alerter's
// evaluator depends on: the metric the evaluator reports, and an
// operator and threshold under which an unavailable probe (value 0)
// violates and an available one (value 1) does not.
func TestMigrationV16_SeedsProbeUnavailableRule(t *testing.T) {
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

	got := readProbeUnavailableRule(t, pool)

	if got.category != "availability" {
		t.Errorf("category = %q, want %q", got.category, "availability")
	}
	if got.metricName != "probe_available" {
		t.Errorf("metric_name = %q, want %q", got.metricName, "probe_available")
	}
	if got.unit != nil {
		t.Errorf("metric_unit = %q, want NULL", *got.unit)
	}
	if got.operator != "<" {
		t.Errorf("default_operator = %q, want %q", got.operator, "<")
	}
	if got.threshold != 1 {
		t.Errorf("default_threshold = %v, want 1", got.threshold)
	}
	if got.severity != "warning" {
		t.Errorf("default_severity = %q, want %q", got.severity, "warning")
	}
	if !got.enabled {
		t.Error("default_enabled = false, want true")
	}
	if got.extension != nil {
		t.Errorf("required_extension = %q, want NULL", *got.extension)
	}
	if !got.builtIn {
		t.Error("is_built_in = false, want true")
	}
	if !strings.Contains(got.description, "collecting") {
		t.Errorf("description = %q, want it to say the probe was collecting",
			got.description)
	}
}

// TestMigrationV16_PreservesOperatorEdits replays the migration over an
// installation that already carries the rule with operator edits, the way
// an interrupted upgrade would. ON CONFLICT (name) DO NOTHING must leave
// every edited value alone rather than resetting it to the seed.
func TestMigrationV16_PreservesOperatorEdits(t *testing.T) {
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
		UPDATE alert_rules
		SET default_severity = 'critical',
		    default_enabled = FALSE
		WHERE name = 'probe_unavailable';

		DELETE FROM schema_version WHERE version >= 16;
	`); err != nil {
		t.Fatalf("rewind to a pre-v16 state: %v", err)
	}

	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("re-apply migrate: %v", err)
	}

	got := readProbeUnavailableRule(t, pool)
	if got.severity != "critical" {
		t.Errorf("default_severity = %q, want the operator's %q",
			got.severity, "critical")
	}
	if got.enabled {
		t.Error("default_enabled = true, want the operator's false")
	}

	// Exactly one row of that name must exist: the unique constraint the
	// ON CONFLICT clause names is what makes the replay a no-op.
	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM alert_rules WHERE name = $1`,
		"probe_unavailable").Scan(&count); err != nil {
		t.Fatalf("count probe_unavailable rules: %v", err)
	}
	if count != 1 {
		t.Errorf("probe_unavailable rows = %d, want 1", count)
	}
}

// TestMigrationV16_ReportsStatementFailure drives the migration's error
// path by renaming the table it seeds into, inside a transaction that is
// rolled back afterwards, so a failing statement is reported with context
// rather than swallowed.
func TestMigrationV16_ReportsStatementFailure(t *testing.T) {
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

	err = migrationV16(t).Up(tx)
	if err == nil {
		t.Fatal("migration 16 succeeded without an alert_rules table")
	}
	if !strings.Contains(err.Error(), "probe_unavailable") {
		t.Errorf("migration 16 error = %q, want it to name the failure",
			err.Error())
	}
}
