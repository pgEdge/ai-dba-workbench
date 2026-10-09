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

// The v21 migration retunes four built-in alert rules, as described in
// GitHub issue #616. These tests cover the fresh-install path, where the
// seed data in the v1 migration already carries the new values, and the
// upgrade path, where the v21 statements rewrite rows seeded by an
// older build but leave operator-tuned defaults and per-scope
// alert_thresholds overrides alone.

// migrationV21 returns the registered version 21 migration.
func migrationV21(t *testing.T) Migration {
	t.Helper()
	for _, m := range NewSchemaManager().migrations {
		if m.Version == 21 {
			return m
		}
	}
	t.Fatal("migration version 21 is not registered")
	return Migration{}
}

// TestMigrationV21_Registered verifies the migration is wired into the
// schema manager and will be reached by an upgrade.
func TestMigrationV21_Registered(t *testing.T) {
	if got := NewSchemaManager().LatestVersion(); got < 21 {
		t.Errorf("LatestVersion() = %d, want at least 21", got)
	}
	if desc := migrationV21(t).Description; desc == "" {
		t.Error("migration 21 has an empty description")
	}
}

// v21RuleState is the per-rule shape the tests assert on.
type v21RuleState struct {
	threshold    float64
	severity     string
	descContains string
}

// v21ExpectedRules lists the values every migrated datastore must carry
// for the four rules the migration touches.
var v21ExpectedRules = map[string]v21RuleState{
	"cache_hit_ratio_low": {
		threshold:    50,
		severity:     "info",
		descContains: "100 blocks per second",
	},
	"dead_tuple_ratio": {
		threshold:    50,
		severity:     "warning",
		descContains: "10000 dead tuples",
	},
	"autovacuum_not_running": {
		threshold:    1,
		severity:     "warning",
		descContains: "30 minutes",
	},
	"checkpoint_warning": {
		threshold:    12,
		severity:     "warning",
		descContains: "not in recovery",
	},
}

// assertV21Rules reads the rules back and compares them with want.
func assertV21Rules(t *testing.T, pool *pgxpool.Pool,
	want map[string]v21RuleState) {
	t.Helper()
	for name, st := range want {
		var (
			gotThreshold float64
			gotSeverity  string
			gotDesc      string
		)
		err := pool.QueryRow(context.Background(), `
			SELECT default_threshold, default_severity, description
			FROM alert_rules
			WHERE name = $1 AND is_built_in
		`, name).Scan(&gotThreshold, &gotSeverity, &gotDesc)
		if err != nil {
			t.Errorf("alert_rules row for %s missing: %v", name, err)
			continue
		}
		if gotThreshold != st.threshold {
			t.Errorf("%s: default_threshold = %v, want %v", name,
				gotThreshold, st.threshold)
		}
		if gotSeverity != st.severity {
			t.Errorf("%s: default_severity = %q, want %q", name,
				gotSeverity, st.severity)
		}
		if !strings.Contains(gotDesc, st.descContains) {
			t.Errorf("%s: description = %q, want it to mention %q", name,
				gotDesc, st.descContains)
		}
	}
}

// migrateV21TestSchema rebuilds the test schema at the latest version.
func migrateV21TestSchema(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return nil, func() {}
	}
	cleanupTestSchema(t, pool)
	if err := NewSchemaManager().Migrate(conn); err != nil {
		conn.Release()
		cleanupTestSchema(t, pool)
		pool.Close()
		t.Fatalf("Failed to migrate: %v", err)
	}
	return pool, func() {
		conn.Release()
		cleanupTestSchema(t, pool)
		pool.Close()
	}
}

// runMigrationV21 applies the migration's statements in a committed
// transaction.
func runMigrationV21(t *testing.T, pool *pgxpool.Pool) {
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
	if err := migrationV21(t).Up(tx); err != nil {
		t.Fatalf("migration 21 failed: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("failed to commit migration 21: %v", err)
	}
}

// TestMigrationV21_FreshInstall verifies that a freshly migrated
// datastore carries the new defaults and descriptions, and that the
// columns the migration changes are documented.
func TestMigrationV21_FreshInstall(t *testing.T) {
	pool, cleanup := migrateV21TestSchema(t)
	defer cleanup()
	if pool == nil {
		return
	}

	assertV21Rules(t, pool, v21ExpectedRules)

	for _, column := range []string{"default_threshold", "default_severity"} {
		var comment *string
		if err := pool.QueryRow(context.Background(), `
			SELECT col_description('alert_rules'::regclass, attnum)
			FROM pg_attribute
			WHERE attrelid = 'alert_rules'::regclass AND attname = $1
		`, column).Scan(&comment); err != nil {
			t.Fatalf("failed to read comment on %s: %v", column, err)
		}
		if comment == nil || *comment == "" {
			t.Errorf("alert_rules.%s has no COMMENT", column)
		}
	}
}

// TestMigrationV21_ReportsStatementFailure drives the migration's error
// path by renaming the table it updates inside a transaction that is
// rolled back afterwards.
func TestMigrationV21_ReportsStatementFailure(t *testing.T) {
	pool, cleanup := migrateV21TestSchema(t)
	defer cleanup()
	if pool == nil {
		return
	}

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

	if _, err := tx.Exec(ctx,
		`ALTER TABLE alert_rules RENAME TO alert_rules_hidden`); err != nil {
		t.Fatalf("failed to hide alert_rules: %v", err)
	}

	err = migrationV21(t).Up(tx)
	if err == nil {
		t.Fatal("migration 21 succeeded without an alert_rules table")
	}
	if !strings.Contains(err.Error(), "retune built-in alert rule defaults") {
		t.Errorf("migration 21 error = %q, want it to name the failure",
			err.Error())
	}
}

// TestMigrationV21_UpgradesOldDefaults rewinds the rules to the values
// an older collector seeded and checks the migration moves them on.
func TestMigrationV21_UpgradesOldDefaults(t *testing.T) {
	pool, cleanup := migrateV21TestSchema(t)
	defer cleanup()
	if pool == nil {
		return
	}

	if _, err := pool.Exec(context.Background(), `
		UPDATE alert_rules
		SET default_threshold = 80, default_severity = 'warning',
		    description = 'Buffer cache hit ratio below threshold'
		WHERE name = 'cache_hit_ratio_low';

		UPDATE alert_rules
		SET default_threshold = 20, description = 'Dead tuple ratio too high'
		WHERE name = 'dead_tuple_ratio';

		UPDATE alert_rules
		SET description = 'Table has dead tuples exceeding the autovacuum threshold but has not been vacuumed'
		WHERE name = 'autovacuum_not_running';

		UPDATE alert_rules
		SET description = 'Requested checkpoints in the last hour exceed the threshold'
		WHERE name = 'checkpoint_warning';
	`); err != nil {
		t.Fatalf("failed to rewind alert rules: %v", err)
	}

	runMigrationV21(t, pool)
	assertV21Rules(t, pool, v21ExpectedRules)
}

// TestMigrationV21_KeepsOperatorTuning checks that defaults an operator
// has changed survive, column by column, and that per-scope overrides
// in alert_thresholds are not touched.
func TestMigrationV21_KeepsOperatorTuning(t *testing.T) {
	pool, cleanup := migrateV21TestSchema(t)
	defer cleanup()
	if pool == nil {
		return
	}
	ctx := context.Background()

	// cache_hit_ratio_low has a tuned threshold but the old severity, so
	// only the severity moves; dead_tuple_ratio has a tuned threshold;
	// autovacuum_not_running has a tuned severity the migration must not
	// reset.
	if _, err := pool.Exec(ctx, `
		UPDATE alert_rules
		SET default_threshold = 70, default_severity = 'warning'
		WHERE name = 'cache_hit_ratio_low';

		UPDATE alert_rules
		SET default_threshold = 30
		WHERE name = 'dead_tuple_ratio';

		UPDATE alert_rules
		SET default_severity = 'critical'
		WHERE name = 'autovacuum_not_running';
	`); err != nil {
		t.Fatalf("failed to tune alert rules: %v", err)
	}

	var connectionID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO connections (name, host, port, database_name, username, password_encrypted, owner_username)
		VALUES ('v21-test', '127.0.0.1', 5432, 'postgres', 'postgres', '', 'v21-test-owner')
		RETURNING id
	`).Scan(&connectionID); err != nil {
		t.Fatalf("failed to seed connection: %v", err)
	}
	var overrideID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO alert_thresholds
		    (rule_id, connection_id, operator, threshold, severity, scope)
		SELECT id, $1, '<', 80, 'warning', 'server'
		FROM alert_rules WHERE name = 'cache_hit_ratio_low'
		RETURNING id
	`, connectionID).Scan(&overrideID); err != nil {
		t.Fatalf("failed to seed alert_thresholds override: %v", err)
	}

	runMigrationV21(t, pool)

	want := map[string]v21RuleState{}
	for name, st := range v21ExpectedRules {
		want[name] = st
	}
	cache := want["cache_hit_ratio_low"]
	cache.threshold = 70
	want["cache_hit_ratio_low"] = cache
	dead := want["dead_tuple_ratio"]
	dead.threshold = 30
	want["dead_tuple_ratio"] = dead
	autovacuum := want["autovacuum_not_running"]
	autovacuum.severity = "critical"
	want["autovacuum_not_running"] = autovacuum
	assertV21Rules(t, pool, want)

	var threshold float64
	var severity string
	if err := pool.QueryRow(ctx, `
		SELECT threshold, severity FROM alert_thresholds WHERE id = $1
	`, overrideID).Scan(&threshold, &severity); err != nil {
		t.Fatalf("failed to read alert_thresholds override: %v", err)
	}
	if threshold != 80 || severity != "warning" {
		t.Errorf("alert_thresholds override = (%v, %q), want (80, \"warning\")",
			threshold, severity)
	}
}
