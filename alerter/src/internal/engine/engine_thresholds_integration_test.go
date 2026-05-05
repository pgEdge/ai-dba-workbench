/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package engine

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// engineSpockTestSchema extends the alerter integration test schema with the
// metrics.* tables required to drive the six built-in Spock and replication
// slot alert rules end-to-end. The columns mirror the production v3 collector
// schema for the fields the registry queries actually read; columns the
// queries never reference are omitted.
//
// The metrics.* tables intentionally do not declare a foreign key to
// connections; production behavior allows brief orphan rows after a
// connection is deleted, and the metric queries do not join through the
// connections table for these particular metrics.
const engineSpockTestSchema = `
DROP TABLE IF EXISTS alert_acknowledgments CASCADE;
DROP TABLE IF EXISTS alerts CASCADE;
DROP TABLE IF EXISTS alert_thresholds CASCADE;
DROP TABLE IF EXISTS alert_rules CASCADE;
DROP TABLE IF EXISTS blackouts CASCADE;
DROP TABLE IF EXISTS alerter_settings CASCADE;
DROP TABLE IF EXISTS probe_availability CASCADE;
DROP TABLE IF EXISTS probe_configs CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS cluster_groups CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
DROP SCHEMA IF EXISTS metrics CASCADE;

CREATE TABLE cluster_groups (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL
);

CREATE TABLE clusters (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    group_id INTEGER REFERENCES cluster_groups(id) ON DELETE SET NULL
);

CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    is_monitored BOOLEAN NOT NULL DEFAULT TRUE,
    connection_error TEXT,
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE SET NULL
);

CREATE TABLE alerter_settings (
    id INTEGER PRIMARY KEY DEFAULT 1,
    retention_days INTEGER NOT NULL DEFAULT 30,
    default_anomaly_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    default_anomaly_sensitivity REAL NOT NULL DEFAULT 3.0,
    baseline_refresh_interval_mins INTEGER NOT NULL DEFAULT 60,
    correlation_window_seconds INTEGER NOT NULL DEFAULT 300,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE alert_rules (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    category VARCHAR(100) NOT NULL DEFAULT 'general',
    metric_name VARCHAR(255) NOT NULL,
    default_operator VARCHAR(10) NOT NULL DEFAULT '>',
    default_threshold REAL NOT NULL DEFAULT 0,
    default_severity VARCHAR(20) NOT NULL DEFAULT 'warning',
    default_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    required_extension VARCHAR(100),
    is_built_in BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE alert_thresholds (
    id BIGSERIAL PRIMARY KEY,
    rule_id BIGINT NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
    scope VARCHAR(20) NOT NULL DEFAULT 'server',
    connection_id INTEGER REFERENCES connections(id) ON DELETE CASCADE,
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE CASCADE,
    group_id INTEGER REFERENCES cluster_groups(id) ON DELETE CASCADE,
    database_name VARCHAR(255),
    operator VARCHAR(10) NOT NULL,
    threshold REAL NOT NULL,
    severity VARCHAR(20) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE alerts (
    id BIGSERIAL PRIMARY KEY,
    alert_type TEXT NOT NULL CHECK (alert_type IN ('threshold', 'anomaly', 'connection')),
    rule_id BIGINT REFERENCES alert_rules(id) ON DELETE SET NULL,
    connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    database_name TEXT,
    probe_name TEXT,
    object_name TEXT,
    metric_name TEXT,
    metric_value REAL,
    threshold_value REAL,
    operator TEXT,
    severity TEXT NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    correlation_id TEXT,
    status TEXT NOT NULL CHECK (status IN ('active', 'cleared', 'acknowledged')),
    triggered_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    cleared_at TIMESTAMPTZ,
    last_updated TIMESTAMPTZ,
    last_reevaluated_at TIMESTAMPTZ,
    reevaluation_count INTEGER NOT NULL DEFAULT 0,
    anomaly_score REAL,
    anomaly_details JSONB,
    ai_analysis TEXT,
    ai_analysis_metric_value REAL
);

CREATE TABLE alert_acknowledgments (
    id BIGSERIAL PRIMARY KEY,
    alert_id BIGINT NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
    acknowledged_by TEXT NOT NULL,
    acknowledged_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    acknowledge_type TEXT NOT NULL CHECK (acknowledge_type IN ('acknowledge', 'dismiss', 'false_positive')),
    message TEXT NOT NULL DEFAULT '',
    false_positive BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE blackouts (
    id BIGSERIAL PRIMARY KEY,
    scope VARCHAR(20) NOT NULL DEFAULT 'server',
    connection_id INTEGER REFERENCES connections(id) ON DELETE CASCADE,
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE CASCADE,
    group_id INTEGER REFERENCES cluster_groups(id) ON DELETE CASCADE,
    database_name TEXT,
    reason TEXT NOT NULL DEFAULT '',
    start_time TIMESTAMPTZ NOT NULL,
    end_time TIMESTAMPTZ NOT NULL,
    created_by TEXT NOT NULL DEFAULT 'system',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO alerter_settings (id, retention_days, default_anomaly_enabled, default_anomaly_sensitivity,
    baseline_refresh_interval_mins, correlation_window_seconds)
VALUES (1, 30, true, 3.0, 60, 300);

-- Empty probe_availability and probe_configs tables let evaluateMetricStaleness
-- run cleanly during the threshold evaluator. The columns mirror the production
-- collector schema; rows are never inserted by these tests so the staleness
-- query simply returns no entries.
CREATE TABLE probe_availability (
    connection_id INTEGER NOT NULL,
    probe_name TEXT NOT NULL,
    is_available BOOLEAN NOT NULL DEFAULT TRUE,
    last_collected TIMESTAMPTZ,
    PRIMARY KEY (connection_id, probe_name)
);

CREATE TABLE probe_configs (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    connection_id INTEGER,
    is_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    collection_interval_seconds INTEGER NOT NULL DEFAULT 60
);

CREATE SCHEMA metrics;

CREATE TABLE metrics.spock_exception_log (
    connection_id INTEGER NOT NULL,
    database_name TEXT NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    remote_origin OID NOT NULL,
    remote_commit_ts TIMESTAMPTZ NOT NULL,
    command_counter INTEGER NOT NULL,
    retry_errored_at TIMESTAMPTZ NOT NULL,
    remote_xid BIGINT NOT NULL,
    PRIMARY KEY (connection_id, database_name, collected_at, remote_origin,
                 remote_commit_ts, command_counter, retry_errored_at)
);

CREATE TABLE metrics.spock_resolutions (
    connection_id INTEGER NOT NULL,
    database_name TEXT NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    id INTEGER NOT NULL,
    node_name NAME NOT NULL,
    log_time TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (connection_id, database_name, collected_at, id, node_name)
);

CREATE TABLE metrics.pg_replication_slots (
    connection_id INTEGER NOT NULL,
    slot_name TEXT NOT NULL,
    active BOOLEAN,
    retained_bytes NUMERIC,
    collected_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (connection_id, collected_at, slot_name)
);
`

// engineSpockTestTeardown drops every object the schema creates so a
// subsequent test run starts from a clean slate.
const engineSpockTestTeardown = `
DROP SCHEMA IF EXISTS metrics CASCADE;
DROP TABLE IF EXISTS alert_acknowledgments CASCADE;
DROP TABLE IF EXISTS alerts CASCADE;
DROP TABLE IF EXISTS alert_thresholds CASCADE;
DROP TABLE IF EXISTS alert_rules CASCADE;
DROP TABLE IF EXISTS blackouts CASCADE;
DROP TABLE IF EXISTS alerter_settings CASCADE;
DROP TABLE IF EXISTS probe_availability CASCADE;
DROP TABLE IF EXISTS probe_configs CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
DROP TABLE IF EXISTS cluster_groups CASCADE;
`

// newEngineSpockTestEnv creates a test environment with the extended schema
// (alerter tables plus the metrics.* tables required by the Spock and slot
// alert rules) and returns the engine, datastore, pool, and cleanup function.
// The test is skipped when the integration database is unavailable.
//
// The engine is constructed with nil config and no notification manager so it
// can run evaluateThresholds and cleanResolvedAlerts without touching LLM or
// notification infrastructure.
func newEngineSpockTestEnv(t *testing.T) (*Engine, *database.Datastore, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping engine spock integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("Test database ping failed: %v", err)
	}

	if _, err := pool.Exec(ctx, engineSpockTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create engine spock test schema: %v", err)
	}

	ds := database.NewTestDatastore(pool)

	// Construct an Engine with nil config; the threshold evaluation code path
	// reads no config values, and queueNotification is a no-op when the
	// notification pool is nil. NewEngine is invoked with a nil config so the
	// LLM providers, notification manager, and notification worker pool stay
	// uninitialized.
	engine := NewEngine(nil, ds, false)

	cleanup := func() {
		if _, err := pool.Exec(context.Background(), engineSpockTestTeardown); err != nil {
			t.Logf("engine spock teardown failed: %v", err)
		}
		pool.Close()
	}

	return engine, ds, pool, cleanup
}

// seedSpockBuiltInRules inserts the six new built-in alert rules added by the
// collector v3 migration. The alerter integration test datastore is
// independent of the collector datastore; in production the collector seeds
// these rows into the same database, but in tests we need to seed them
// directly. The threshold values, operators, and severities mirror the
// constants in collector/src/database/schema.go for the v3 alert_rules seed.
//
// Returned map keys are the rule names; values are the inserted rule IDs.
func seedSpockBuiltInRules(t *testing.T, pool *pgxpool.Pool) map[string]int64 {
	t.Helper()

	rules := []struct {
		name        string
		description string
		category    string
		metric      string
		threshold   float64
		severity    string
	}{
		{
			name:        "spock_recent_exceptions_present",
			description: "At least one Spock exception was logged in the last 15 minutes.",
			category:    "replication",
			metric:      "spock_exception_log.recent_count",
			threshold:   1,
			severity:    "warning",
		},
		{
			name:        "spock_recent_exceptions_high",
			description: "Ten or more Spock exceptions were logged in the last 15 minutes.",
			category:    "replication",
			metric:      "spock_exception_log.recent_count",
			threshold:   10,
			severity:    "critical",
		},
		{
			name:        "spock_recent_resolutions_present",
			description: "At least one Spock conflict resolution occurred in the last 15 minutes.",
			category:    "replication",
			metric:      "spock_resolutions.recent_count",
			threshold:   1,
			severity:    "warning",
		},
		{
			name:        "spock_recent_resolutions_high",
			description: "Twenty-five or more Spock conflict resolutions occurred in the last 15 minutes.",
			category:    "replication",
			metric:      "spock_resolutions.recent_count",
			threshold:   25,
			severity:    "critical",
		},
		{
			name:        "replication_slot_retention_warn",
			description: "A replication slot is retaining at least 1 GiB of WAL.",
			category:    "replication",
			metric:      "pg_replication_slots.max_retained_bytes",
			threshold:   1073741824,
			severity:    "warning",
		},
		{
			name:        "replication_slot_retention_high",
			description: "A replication slot is retaining at least 10 GiB of WAL.",
			category:    "replication",
			metric:      "pg_replication_slots.max_retained_bytes",
			threshold:   10737418240,
			severity:    "critical",
		},
	}

	ids := make(map[string]int64, len(rules))
	for _, r := range rules {
		var id int64
		err := pool.QueryRow(context.Background(), `
			INSERT INTO alert_rules (name, description, category, metric_name,
			    default_operator, default_threshold, default_severity,
			    default_enabled, is_built_in)
			VALUES ($1, $2, $3, $4, '>=', $5, $6, TRUE, TRUE)
			RETURNING id
		`, r.name, r.description, r.category, r.metric, r.threshold, r.severity).Scan(&id)
		if err != nil {
			t.Fatalf("Failed to seed built-in rule %q: %v", r.name, err)
		}
		ids[r.name] = id
	}
	return ids
}

// disableAllRulesExcept disables every alert_rule whose name is not in keep.
// This isolates a single rule for evaluation so unrelated rules cannot fire
// alerts on the same connection during a fire/clear assertion.
func disableAllRulesExcept(t *testing.T, pool *pgxpool.Pool, keep ...string) {
	t.Helper()

	if _, err := pool.Exec(context.Background(),
		`UPDATE alert_rules SET default_enabled = FALSE WHERE name <> ALL($1)`,
		keep); err != nil {
		t.Fatalf("Failed to disable other rules: %v", err)
	}
}

// insertSpockExceptionRow writes a single metrics.spock_exception_log row
// for the engine integration tests. command_counter discriminates rows that
// share a (connection_id, collected_at) key.
func insertSpockExceptionRow(t *testing.T, pool *pgxpool.Pool, connID int,
	collectedAt time.Time, commandCounter int) {
	t.Helper()

	if _, err := pool.Exec(context.Background(), `
		INSERT INTO metrics.spock_exception_log (
		    connection_id, database_name, collected_at, remote_origin,
		    remote_commit_ts, command_counter, retry_errored_at, remote_xid
		) VALUES ($1, 'app', $2, 12345, $2, $3, $2, 999)
	`, connID, collectedAt, commandCounter); err != nil {
		t.Fatalf("Failed to insert spock_exception_log row: %v", err)
	}
}

// insertSpockResolutionRow writes a single metrics.spock_resolutions row.
func insertSpockResolutionRow(t *testing.T, pool *pgxpool.Pool, connID int,
	collectedAt time.Time, id int) {
	t.Helper()

	if _, err := pool.Exec(context.Background(), `
		INSERT INTO metrics.spock_resolutions (
		    connection_id, database_name, collected_at, id, node_name, log_time
		) VALUES ($1, 'app', $2, $3, 'node-a', $2)
	`, connID, collectedAt, id); err != nil {
		t.Fatalf("Failed to insert spock_resolutions row: %v", err)
	}
}

// insertReplicationSlotRow writes a single metrics.pg_replication_slots row.
func insertReplicationSlotRow(t *testing.T, pool *pgxpool.Pool, connID int,
	collectedAt time.Time, slotName string, active bool, retainedBytes int64) {
	t.Helper()

	if _, err := pool.Exec(context.Background(), `
		INSERT INTO metrics.pg_replication_slots (
		    connection_id, slot_name, active, retained_bytes, collected_at
		) VALUES ($1, $2, $3, $4, $5)
	`, connID, slotName, active, retainedBytes, collectedAt); err != nil {
		t.Fatalf("Failed to insert pg_replication_slots row: %v", err)
	}
}

// truncateSpockExceptionLog removes every row for the given connection so the
// metric query returns no rows for that connection (i.e., the metric drops
// out of latest-sample results entirely). cleanResolvedAlerts treats the
// missing entry as a resolved condition and clears the alert.
func truncateSpockExceptionLog(t *testing.T, pool *pgxpool.Pool, connID int) {
	t.Helper()

	if _, err := pool.Exec(context.Background(),
		`DELETE FROM metrics.spock_exception_log WHERE connection_id = $1`,
		connID); err != nil {
		t.Fatalf("Failed to truncate spock_exception_log for conn %d: %v", connID, err)
	}
}

// truncateSpockResolutions mirrors truncateSpockExceptionLog for the
// spock_resolutions table.
func truncateSpockResolutions(t *testing.T, pool *pgxpool.Pool, connID int) {
	t.Helper()

	if _, err := pool.Exec(context.Background(),
		`DELETE FROM metrics.spock_resolutions WHERE connection_id = $1`,
		connID); err != nil {
		t.Fatalf("Failed to truncate spock_resolutions for conn %d: %v", connID, err)
	}
}

// truncateReplicationSlots removes every row for the given connection so the
// metric query for max_retained_bytes returns no rows; cleanResolvedAlerts
// then clears any active retention alert. This deliberately models the
// "all slots disappeared" path; for the alternative "slots still exist but
// retention dropped" path tests can rewrite a newer sample with smaller
// retained_bytes values instead.
func truncateReplicationSlots(t *testing.T, pool *pgxpool.Pool, connID int) {
	t.Helper()

	if _, err := pool.Exec(context.Background(),
		`DELETE FROM metrics.pg_replication_slots WHERE connection_id = $1`,
		connID); err != nil {
		t.Fatalf("Failed to truncate pg_replication_slots for conn %d: %v", connID, err)
	}
}

// activeAlertForRule looks up the active threshold alert for the given rule
// and connection, returning nil when none exists. This is a thin convenience
// over GetActiveThresholdAlert that fails the test on query errors so callers
// can write linear assertions.
func activeAlertForRule(t *testing.T, ds *database.Datastore, ruleID int64, connID int) *database.Alert {
	t.Helper()

	alert, err := ds.GetActiveThresholdAlert(context.Background(), ruleID, connID, nil)
	if err != nil {
		t.Fatalf("GetActiveThresholdAlert(rule=%d, conn=%d) failed: %v", ruleID, connID, err)
	}
	return alert
}

// assertAlertFired asserts that an active alert exists for the given rule
// and connection with the expected severity. Returns the alert so callers
// can inspect additional fields.
func assertAlertFired(t *testing.T, ds *database.Datastore, ruleID int64, connID int, wantSeverity string) *database.Alert {
	t.Helper()

	alert := activeAlertForRule(t, ds, ruleID, connID)
	if alert == nil {
		t.Fatalf("expected active alert for rule %d on connection %d, found none", ruleID, connID)
	}
	if alert.Severity != wantSeverity {
		t.Errorf("alert severity = %q; want %q", alert.Severity, wantSeverity)
	}
	if alert.Status != "active" {
		t.Errorf("alert status = %q; want \"active\"", alert.Status)
	}
	return alert
}

// assertAlertCleared asserts that no active alert exists for the given rule
// and connection, and that the previously fired alert (identified by id) was
// transitioned to status='cleared'.
func assertAlertCleared(t *testing.T, ds *database.Datastore, pool *pgxpool.Pool, ruleID int64, connID int, alertID int64) {
	t.Helper()

	if alert := activeAlertForRule(t, ds, ruleID, connID); alert != nil {
		t.Fatalf("expected no active alert for rule %d on connection %d, found id=%d status=%q",
			ruleID, connID, alert.ID, alert.Status)
	}

	var status string
	if err := pool.QueryRow(context.Background(),
		`SELECT status FROM alerts WHERE id = $1`, alertID).Scan(&status); err != nil {
		t.Fatalf("Failed to read alert %d status: %v", alertID, err)
	}
	if status != "cleared" {
		t.Errorf("alert %d status = %q; want \"cleared\"", alertID, status)
	}
}

// TestEngine_SpockRecentExceptionsPresent_FireAndClear exercises the warning
// rule that fires when the latest sample of metrics.spock_exception_log holds
// at least one row. The test seeds a single row to drive the metric to 1,
// runs the threshold evaluator, asserts a warning alert fired, then deletes
// every row for the connection and runs the cleaner; the alert must clear.
func TestEngine_SpockRecentExceptionsPresent_FireAndClear(t *testing.T) {
	engine, ds, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	rules := seedSpockBuiltInRules(t, pool)
	disableAllRulesExcept(t, pool, "spock_recent_exceptions_present")

	connID := insertTestConnection(t, pool, "spock-exceptions-present-conn")
	sample := time.Now().UTC().Add(-1 * time.Minute)
	insertSpockExceptionRow(t, pool, connID, sample, 1)

	engine.evaluateThresholds(ctx)

	alert := assertAlertFired(t, ds, rules["spock_recent_exceptions_present"], connID, "warning")

	// Drive the metric below the threshold by removing every row for this
	// connection. cleanResolvedAlerts iterates active threshold alerts and
	// re-queries the metric; an empty result clears the alert.
	truncateSpockExceptionLog(t, pool, connID)
	engine.cleanResolvedAlerts(ctx)

	assertAlertCleared(t, ds, pool, rules["spock_recent_exceptions_present"], connID, alert.ID)
}

// TestEngine_SpockRecentExceptionsHigh_FireAndClear exercises the critical
// rule that fires when the latest sample of metrics.spock_exception_log holds
// ten or more rows. Ten rows drive the metric to exactly the threshold and
// the rule's '>=' operator means the value violates it.
func TestEngine_SpockRecentExceptionsHigh_FireAndClear(t *testing.T) {
	engine, ds, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	rules := seedSpockBuiltInRules(t, pool)
	disableAllRulesExcept(t, pool, "spock_recent_exceptions_high")

	connID := insertTestConnection(t, pool, "spock-exceptions-high-conn")
	sample := time.Now().UTC().Add(-1 * time.Minute)
	for i := 1; i <= 10; i++ {
		insertSpockExceptionRow(t, pool, connID, sample, i)
	}

	engine.evaluateThresholds(ctx)

	alert := assertAlertFired(t, ds, rules["spock_recent_exceptions_high"], connID, "critical")

	truncateSpockExceptionLog(t, pool, connID)
	engine.cleanResolvedAlerts(ctx)

	assertAlertCleared(t, ds, pool, rules["spock_recent_exceptions_high"], connID, alert.ID)
}

// TestEngine_SpockRecentResolutionsPresent_FireAndClear exercises the warning
// rule for spock_resolutions.recent_count >= 1.
func TestEngine_SpockRecentResolutionsPresent_FireAndClear(t *testing.T) {
	engine, ds, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	rules := seedSpockBuiltInRules(t, pool)
	disableAllRulesExcept(t, pool, "spock_recent_resolutions_present")

	connID := insertTestConnection(t, pool, "spock-resolutions-present-conn")
	sample := time.Now().UTC().Add(-1 * time.Minute)
	insertSpockResolutionRow(t, pool, connID, sample, 1)

	engine.evaluateThresholds(ctx)

	alert := assertAlertFired(t, ds, rules["spock_recent_resolutions_present"], connID, "warning")

	truncateSpockResolutions(t, pool, connID)
	engine.cleanResolvedAlerts(ctx)

	assertAlertCleared(t, ds, pool, rules["spock_recent_resolutions_present"], connID, alert.ID)
}

// TestEngine_SpockRecentResolutionsHigh_FireAndClear exercises the critical
// rule for spock_resolutions.recent_count >= 25.
func TestEngine_SpockRecentResolutionsHigh_FireAndClear(t *testing.T) {
	engine, ds, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	rules := seedSpockBuiltInRules(t, pool)
	disableAllRulesExcept(t, pool, "spock_recent_resolutions_high")

	connID := insertTestConnection(t, pool, "spock-resolutions-high-conn")
	sample := time.Now().UTC().Add(-1 * time.Minute)
	for i := 1; i <= 25; i++ {
		insertSpockResolutionRow(t, pool, connID, sample, i)
	}

	engine.evaluateThresholds(ctx)

	alert := assertAlertFired(t, ds, rules["spock_recent_resolutions_high"], connID, "critical")

	truncateSpockResolutions(t, pool, connID)
	engine.cleanResolvedAlerts(ctx)

	assertAlertCleared(t, ds, pool, rules["spock_recent_resolutions_high"], connID, alert.ID)
}

// TestEngine_ReplicationSlotRetentionWarn_FireAndClear exercises the warning
// rule for pg_replication_slots.max_retained_bytes >= 1 GiB.
//
// The fire phase seeds one slot retaining 2 GiB; the clear phase rewrites the
// sample so every slot retains 0 bytes (a smaller, newer sample), driving
// max_retained_bytes to 0 below the 1 GiB threshold. This exercises the
// "metric still reports but value resolved" branch of cleanResolvedAlerts
// rather than the "metric disappeared" branch.
func TestEngine_ReplicationSlotRetentionWarn_FireAndClear(t *testing.T) {
	engine, ds, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	rules := seedSpockBuiltInRules(t, pool)
	disableAllRulesExcept(t, pool, "replication_slot_retention_warn")

	connID := insertTestConnection(t, pool, "slot-retention-warn-conn")
	const twoGiB = int64(2) * 1024 * 1024 * 1024
	fireSample := time.Now().UTC().Add(-2 * time.Minute)
	insertReplicationSlotRow(t, pool, connID, fireSample, "slot_a", true, twoGiB)

	engine.evaluateThresholds(ctx)

	alert := assertAlertFired(t, ds, rules["replication_slot_retention_warn"], connID, "warning")

	// Replace the latest sample with one that has zero retained bytes. The
	// new sample wins MAX(collected_at) so the registry query reports 0,
	// well below the 1 GiB threshold.
	clearSample := time.Now().UTC()
	insertReplicationSlotRow(t, pool, connID, clearSample, "slot_a", true, 0)
	engine.cleanResolvedAlerts(ctx)

	assertAlertCleared(t, ds, pool, rules["replication_slot_retention_warn"], connID, alert.ID)
}

// TestEngine_ReplicationSlotRetentionHigh_FireAndClear exercises the critical
// rule for pg_replication_slots.max_retained_bytes >= 10 GiB.
//
// The clear phase deletes every slot row for the connection so the metric
// returns no rows for this connection_id; cleanResolvedAlerts treats that as
// a resolved condition and clears the alert. This complements the warn-rule
// test which exercises the "value dropped" path; together they cover both
// branches of cleanResolvedAlerts for the slot-retention metric.
func TestEngine_ReplicationSlotRetentionHigh_FireAndClear(t *testing.T) {
	engine, ds, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	rules := seedSpockBuiltInRules(t, pool)
	disableAllRulesExcept(t, pool, "replication_slot_retention_high")

	connID := insertTestConnection(t, pool, "slot-retention-high-conn")
	const fifteenGiB = int64(15) * 1024 * 1024 * 1024
	fireSample := time.Now().UTC().Add(-1 * time.Minute)
	insertReplicationSlotRow(t, pool, connID, fireSample, "slot_a", true, fifteenGiB)

	engine.evaluateThresholds(ctx)

	alert := assertAlertFired(t, ds, rules["replication_slot_retention_high"], connID, "critical")

	truncateReplicationSlots(t, pool, connID)
	engine.cleanResolvedAlerts(ctx)

	assertAlertCleared(t, ds, pool, rules["replication_slot_retention_high"], connID, alert.ID)
}

// TestEngine_SpockRecentExceptionsPresent_StaleSampleAutoClears proves that
// the freshness cutoff in spock_exception_log.recent_count's latest CTE
// auto-clears the alert when the most recent sample ages past 5 minutes.
//
// This models the production scenario where the source-side rolling window
// drains: the probe's Execute returns no rows for several cycles, Store
// short-circuits without writing anything, and the latest sample for the
// connection ages out of the cutoff. The clean-resolved-alerts pass must
// then transition the alert to status='cleared' even though the row that
// caused the alert is still present in the table.
//
// The test fires the alert with a fresh row, ages the sample by rewriting
// collected_at to 6 minutes ago (just outside the cutoff), and asserts the
// alert clears. Rewriting collected_at is more deterministic than waiting
// real time to elapse and exercises exactly the staleness-driven clear
// path the cutoff was added to enable.
func TestEngine_SpockRecentExceptionsPresent_StaleSampleAutoClears(t *testing.T) {
	engine, ds, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	rules := seedSpockBuiltInRules(t, pool)
	disableAllRulesExcept(t, pool, "spock_recent_exceptions_present")

	connID := insertTestConnection(t, pool, "spock-exceptions-stale-conn")
	fresh := time.Now().UTC().Add(-1 * time.Minute)
	insertSpockExceptionRow(t, pool, connID, fresh, 1)

	engine.evaluateThresholds(ctx)

	alert := assertAlertFired(t, ds, rules["spock_recent_exceptions_present"], connID, "warning")

	// Age the only row past the 5-minute freshness cutoff. The probe
	// short-circuits Store on empty results so a real probe never writes a
	// fresher row in this scenario; rewriting collected_at simulates the
	// passage of time without flaking on real-clock waits.
	stale := time.Now().UTC().Add(-6 * time.Minute)
	if _, err := pool.Exec(ctx, `
		UPDATE metrics.spock_exception_log
		   SET collected_at = $1
		 WHERE connection_id = $2
	`, stale, connID); err != nil {
		t.Fatalf("Failed to age spock_exception_log row: %v", err)
	}

	engine.cleanResolvedAlerts(ctx)

	assertAlertCleared(t, ds, pool, rules["spock_recent_exceptions_present"], connID, alert.ID)
}

// TestEngine_SpockRecentResolutionsPresent_StaleSampleAutoClears mirrors the
// exception_log auto-clear test for the resolutions table. The freshness
// cutoff applies symmetrically to both Spock recent_count metrics so a
// drained source-side window clears the resolutions alert too.
func TestEngine_SpockRecentResolutionsPresent_StaleSampleAutoClears(t *testing.T) {
	engine, ds, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	rules := seedSpockBuiltInRules(t, pool)
	disableAllRulesExcept(t, pool, "spock_recent_resolutions_present")

	connID := insertTestConnection(t, pool, "spock-resolutions-stale-conn")
	fresh := time.Now().UTC().Add(-1 * time.Minute)
	insertSpockResolutionRow(t, pool, connID, fresh, 1)

	engine.evaluateThresholds(ctx)

	alert := assertAlertFired(t, ds, rules["spock_recent_resolutions_present"], connID, "warning")

	stale := time.Now().UTC().Add(-6 * time.Minute)
	if _, err := pool.Exec(ctx, `
		UPDATE metrics.spock_resolutions
		   SET collected_at = $1
		 WHERE connection_id = $2
	`, stale, connID); err != nil {
		t.Fatalf("Failed to age spock_resolutions row: %v", err)
	}

	engine.cleanResolvedAlerts(ctx)

	assertAlertCleared(t, ds, pool, rules["spock_recent_resolutions_present"], connID, alert.ID)
}
