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
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestMigrationV3_SpockMetricsTablesExist verifies that the v3 migration
// creates metrics.spock_exception_log and metrics.spock_resolutions as
// partitioned tables keyed on collected_at.
//
// The fixture migrates a fresh test database to the latest schema version
// then asserts the tables are present, partitioned, and own the documented
// per-connection foreign key.
func TestMigrationV3_SpockMetricsTablesExist(t *testing.T) {
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

	// Both metrics tables must exist and be partitioned by collected_at.
	for _, table := range []string{"spock_exception_log", "spock_resolutions"} {
		var exists bool
		err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_tables
				WHERE schemaname = 'metrics'
				  AND tablename = $1
			)
		`, table).Scan(&exists)
		if err != nil {
			t.Fatalf("failed to check existence of metrics.%s: %v", table, err)
		}
		if !exists {
			t.Errorf("metrics.%s was not created", table)
			continue
		}

		var isPartitioned bool
		err = pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_class c
				JOIN pg_namespace n ON n.oid = c.relnamespace
				WHERE n.nspname = 'metrics'
				  AND c.relname = $1
				  AND c.relkind = 'p'
			)
		`, table).Scan(&isPartitioned)
		if err != nil {
			t.Fatalf("failed to check partitioning of metrics.%s: %v", table, err)
		}
		if !isPartitioned {
			t.Errorf("metrics.%s is not partitioned", table)
		}

		// The partition key should be (collected_at). We verify by reading
		// pg_partitioned_table.
		var partKey string
		err = pool.QueryRow(ctx, `
			SELECT pg_get_partkeydef(c.oid)
			FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = 'metrics'
			  AND c.relname = $1
		`, table).Scan(&partKey)
		if err != nil {
			t.Fatalf("failed to read partition key for metrics.%s: %v", table, err)
		}
		if partKey != "RANGE (collected_at)" {
			t.Errorf("metrics.%s partition key = %q, want RANGE (collected_at)", table, partKey)
		}

		// The conn-time index documented in the design must exist.
		expectedIndex := "idx_" + table + "_conn_time"
		var idxExists bool
		err = pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_indexes
				WHERE schemaname = 'metrics'
				  AND tablename = $1
				  AND indexname = $2
			)
		`, table, expectedIndex).Scan(&idxExists)
		if err != nil {
			t.Fatalf("failed to check index %s: %v", expectedIndex, err)
		}
		if !idxExists {
			t.Errorf("expected index %s on metrics.%s, but it was not created", expectedIndex, table)
		}
	}

	// Verify a representative subset of the documented column shapes.
	exceptionColumns := map[string]string{
		"connection_id":    "integer",
		"collected_at":     "timestamp with time zone",
		"remote_origin":    "oid",
		"remote_commit_ts": "timestamp with time zone",
		"command_counter":  "integer",
		"retry_errored_at": "timestamp with time zone",
		"remote_xid":       "bigint",
		"local_tup":        "jsonb",
		"remote_old_tup":   "jsonb",
		"remote_new_tup":   "jsonb",
		"error_message":    "text",
	}
	assertColumnTypes(t, pool, "spock_exception_log", exceptionColumns)

	resolutionColumns := map[string]string{
		"connection_id":    "integer",
		"collected_at":     "timestamp with time zone",
		"id":               "integer",
		"node_name":        "name",
		"log_time":         "timestamp with time zone",
		"local_tuple":      "text",
		"local_xid":        "text",
		"remote_xid":       "text",
		"remote_lsn":       "text",
		"remote_timestamp": "timestamp with time zone",
	}
	assertColumnTypes(t, pool, "spock_resolutions", resolutionColumns)
}

// assertColumnTypes asserts that each column in the given metrics table
// matches the expected information_schema data_type. It logs a clear
// failure for each missing or mistyped column.
func assertColumnTypes(t *testing.T, pool *pgxpool.Pool, table string, expected map[string]string) {
	t.Helper()
	ctx := context.Background()
	for col, wantType := range expected {
		var gotType string
		err := pool.QueryRow(ctx, `
			SELECT data_type
			FROM information_schema.columns
			WHERE table_schema = 'metrics'
			  AND table_name = $1
			  AND column_name = $2
		`, table, col).Scan(&gotType)
		if err != nil {
			t.Errorf("metrics.%s column %q lookup failed: %v", table, col, err)
			continue
		}
		if gotType != wantType {
			t.Errorf("metrics.%s.%s data_type = %q, want %q", table, col, gotType, wantType)
		}
	}
}

// TestMigrationV3_SpockProbeConfigsSeeded verifies the migration seeds the
// two new global probe_configs rows with the documented defaults.
func TestMigrationV3_SpockProbeConfigsSeeded(t *testing.T) {
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

	expected := []struct {
		name     string
		interval int
		retain   int
	}{
		{"spock_exception_log", 60, 7},
		{"spock_resolutions", 60, 7},
	}

	for _, want := range expected {
		var (
			gotConnNull bool
			gotEnabled  bool
			gotInterval int
			gotRetain   int
			gotDesc     string
		)
		err := pool.QueryRow(ctx, `
			SELECT connection_id IS NULL,
			       is_enabled,
			       collection_interval_seconds,
			       retention_days,
			       description
			FROM probe_configs
			WHERE name = $1
		`, want.name).Scan(&gotConnNull, &gotEnabled, &gotInterval, &gotRetain, &gotDesc)
		if err != nil {
			t.Errorf("probe_configs row for %s missing: %v", want.name, err)
			continue
		}
		if !gotConnNull {
			t.Errorf("probe_configs.%s: connection_id should be NULL", want.name)
		}
		if !gotEnabled {
			t.Errorf("probe_configs.%s: is_enabled should be TRUE", want.name)
		}
		if gotInterval != want.interval {
			t.Errorf("probe_configs.%s: collection_interval_seconds = %d, want %d",
				want.name, gotInterval, want.interval)
		}
		if gotRetain != want.retain {
			t.Errorf("probe_configs.%s: retention_days = %d, want %d",
				want.name, gotRetain, want.retain)
		}
		if gotDesc == "" {
			t.Errorf("probe_configs.%s: description must not be empty", want.name)
		}
	}
}

// TestMigrationV3_SpockAlertRulesSeeded verifies the migration seeds the
// six new built-in alert rules with the thresholds and severities
// documented in the design.
func TestMigrationV3_SpockAlertRulesSeeded(t *testing.T) {
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

	type ruleSpec struct {
		name              string
		category          string
		metricName        string
		operator          string
		threshold         float64
		severity          string
		requiredExtension *string
	}
	spock := "spock"
	rules := []ruleSpec{
		{"spock_recent_exceptions_present", "replication", "spock_exception_log.recent_count", ">=", 1, "warning", &spock},
		{"spock_recent_exceptions_high", "replication", "spock_exception_log.recent_count", ">=", 10, "critical", &spock},
		{"spock_recent_resolutions_present", "replication", "spock_resolutions.recent_count", ">=", 1, "warning", &spock},
		{"spock_recent_resolutions_high", "replication", "spock_resolutions.recent_count", ">=", 25, "critical", &spock},
		{"replication_slot_retention_warn", "replication", "pg_replication_slots.max_retained_bytes", ">=", 1073741824, "warning", nil},
		{"replication_slot_retention_high", "replication", "pg_replication_slots.max_retained_bytes", ">=", 10737418240, "critical", nil},
	}

	for _, want := range rules {
		var (
			gotCategory  string
			gotMetric    string
			gotOperator  string
			gotThreshold float64
			gotSeverity  string
			gotEnabled   bool
			gotBuiltIn   bool
			gotReqExtPtr *string
		)
		err := pool.QueryRow(ctx, `
			SELECT category,
			       metric_name,
			       default_operator,
			       default_threshold,
			       default_severity,
			       default_enabled,
			       is_built_in,
			       required_extension
			FROM alert_rules
			WHERE name = $1
		`, want.name).Scan(&gotCategory, &gotMetric, &gotOperator, &gotThreshold,
			&gotSeverity, &gotEnabled, &gotBuiltIn, &gotReqExtPtr)
		if err != nil {
			t.Errorf("alert_rules row %q missing: %v", want.name, err)
			continue
		}
		if gotCategory != want.category {
			t.Errorf("%s: category = %q, want %q", want.name, gotCategory, want.category)
		}
		if gotMetric != want.metricName {
			t.Errorf("%s: metric_name = %q, want %q", want.name, gotMetric, want.metricName)
		}
		if gotOperator != want.operator {
			t.Errorf("%s: default_operator = %q, want %q", want.name, gotOperator, want.operator)
		}
		if gotThreshold != want.threshold {
			t.Errorf("%s: default_threshold = %v, want %v", want.name, gotThreshold, want.threshold)
		}
		if gotSeverity != want.severity {
			t.Errorf("%s: default_severity = %q, want %q", want.name, gotSeverity, want.severity)
		}
		if !gotEnabled {
			t.Errorf("%s: default_enabled must be TRUE", want.name)
		}
		if !gotBuiltIn {
			t.Errorf("%s: is_built_in must be TRUE", want.name)
		}
		switch {
		case want.requiredExtension == nil && gotReqExtPtr != nil:
			t.Errorf("%s: required_extension = %q, want NULL", want.name, *gotReqExtPtr)
		case want.requiredExtension != nil && gotReqExtPtr == nil:
			t.Errorf("%s: required_extension is NULL, want %q", want.name, *want.requiredExtension)
		case want.requiredExtension != nil && gotReqExtPtr != nil && *want.requiredExtension != *gotReqExtPtr:
			t.Errorf("%s: required_extension = %q, want %q", want.name,
				*gotReqExtPtr, *want.requiredExtension)
		}
	}
}

// TestMigrationV3_Idempotent verifies running Migrate twice does not error
// and yields the same schema_version row count after the second pass.
func TestMigrationV3_Idempotent(t *testing.T) {
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
		t.Fatalf("first Migrate failed: %v", err)
	}

	var firstMax int
	if err := pool.QueryRow(ctx, `SELECT MAX(version) FROM schema_version`).Scan(&firstMax); err != nil {
		t.Fatalf("failed to read schema_version after first migrate: %v", err)
	}

	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("second Migrate failed: %v", err)
	}

	var secondMax int
	if err := pool.QueryRow(ctx, `SELECT MAX(version) FROM schema_version`).Scan(&secondMax); err != nil {
		t.Fatalf("failed to read schema_version after second migrate: %v", err)
	}

	if firstMax != secondMax {
		t.Errorf("schema_version max changed across runs: first=%d second=%d", firstMax, secondMax)
	}

	// V3 must be present in both runs.
	var v3Count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM schema_version WHERE version = 3`).Scan(&v3Count); err != nil {
		t.Fatalf("failed to count v3 rows in schema_version: %v", err)
	}
	if v3Count != 1 {
		t.Errorf("expected exactly one schema_version row for v3, got %d", v3Count)
	}

	// Re-running must not duplicate seed rows.
	for _, ruleName := range []string{
		"spock_recent_exceptions_present",
		"spock_recent_exceptions_high",
		"spock_recent_resolutions_present",
		"spock_recent_resolutions_high",
		"replication_slot_retention_warn",
		"replication_slot_retention_high",
	} {
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM alert_rules WHERE name = $1`, ruleName).Scan(&n); err != nil {
			t.Fatalf("failed to count alert_rules row %s: %v", ruleName, err)
		}
		if n != 1 {
			t.Errorf("alert_rules %s: expected 1 row after idempotent migrate, got %d", ruleName, n)
		}
	}
	for _, probeName := range []string{"spock_exception_log", "spock_resolutions"} {
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM probe_configs WHERE name = $1`, probeName).Scan(&n); err != nil {
			t.Fatalf("failed to count probe_configs row %s: %v", probeName, err)
		}
		if n != 1 {
			t.Errorf("probe_configs %s: expected 1 row after idempotent migrate, got %d", probeName, n)
		}
	}
}
