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

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// engineIntegrationTestSchema creates the minimal tables required to exercise
// the engine's threshold evaluation, alert lifecycle, and cleanup logic.
// This schema mirrors the production tables but only includes columns and
// constraints needed by the engine code paths being tested.
const engineIntegrationTestSchema = `
DROP TABLE IF EXISTS alert_acknowledgments CASCADE;
DROP TABLE IF EXISTS alerts CASCADE;
DROP TABLE IF EXISTS alert_thresholds CASCADE;
DROP TABLE IF EXISTS alert_rules CASCADE;
DROP TABLE IF EXISTS blackouts CASCADE;
DROP TABLE IF EXISTS alerter_settings CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS cluster_groups CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;

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
    anomaly_details JSONB
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

-- Insert default alerter settings
INSERT INTO alerter_settings (id, retention_days, default_anomaly_enabled, default_anomaly_sensitivity,
    baseline_refresh_interval_mins, correlation_window_seconds)
VALUES (1, 30, true, 3.0, 60, 300);
`

const engineIntegrationTestTeardown = `
DROP TABLE IF EXISTS alert_acknowledgments CASCADE;
DROP TABLE IF EXISTS alerts CASCADE;
DROP TABLE IF EXISTS alert_thresholds CASCADE;
DROP TABLE IF EXISTS alert_rules CASCADE;
DROP TABLE IF EXISTS blackouts CASCADE;
DROP TABLE IF EXISTS alerter_settings CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
DROP TABLE IF EXISTS cluster_groups CASCADE;
`

// testDatastore wraps a pool for testing and satisfies the engine's needs.
// It is a minimal wrapper that embeds database.Datastore functionality
// without requiring the full config-based constructor.
type testDatastore struct {
	pool *pgxpool.Pool
}

// newEngineIntegrationTestEnv creates a test environment with schema and
// returns the datastore, pool, and cleanup function. The test is skipped
// if TEST_AI_WORKBENCH_SERVER is not set.
func newEngineIntegrationTestEnv(t *testing.T) (*database.Datastore, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping engine integration test")
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

	if _, err := pool.Exec(ctx, engineIntegrationTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create engine integration test schema: %v", err)
	}

	ds := database.NewTestDatastore(pool)

	cleanup := func() {
		if _, err := pool.Exec(context.Background(), engineIntegrationTestTeardown); err != nil {
			t.Logf("engine integration teardown failed: %v", err)
		}
		pool.Close()
	}

	return ds, pool, cleanup
}

// insertTestConnection creates a connection and returns its ID.
func insertTestConnection(t *testing.T, pool *pgxpool.Pool, name string) int {
	t.Helper()
	var id int
	err := pool.QueryRow(context.Background(),
		`INSERT INTO connections (name, enabled, is_monitored) VALUES ($1, TRUE, TRUE) RETURNING id`,
		name).Scan(&id)
	if err != nil {
		t.Fatalf("Failed to insert connection %q: %v", name, err)
	}
	return id
}

// insertTestAlertRule creates an alert rule and returns its ID.
func insertTestAlertRule(t *testing.T, pool *pgxpool.Pool, name, metricName, operator string, threshold float64, severity string) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO alert_rules (name, description, category, metric_name, default_operator,
		    default_threshold, default_severity, default_enabled, is_built_in)
		VALUES ($1, 'Test rule', 'test', $2, $3, $4, $5, TRUE, FALSE)
		RETURNING id
	`, name, metricName, operator, threshold, severity).Scan(&id)
	if err != nil {
		t.Fatalf("Failed to insert alert rule %q: %v", name, err)
	}
	return id
}

// insertTestAlert creates an alert and returns its ID.
func insertTestAlert(t *testing.T, pool *pgxpool.Pool, alertType string, ruleID *int64, connID int, severity, status, title string) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO alerts (alert_type, rule_id, connection_id, severity, title, description, status, triggered_at)
		VALUES ($1, $2, $3, $4, $5, 'Test description', $6, NOW())
		RETURNING id
	`, alertType, ruleID, connID, severity, title, status).Scan(&id)
	if err != nil {
		t.Fatalf("Failed to insert alert %q: %v", title, err)
	}
	return id
}

// insertTestAcknowledgment creates an acknowledgment for an alert.
func insertTestAcknowledgment(t *testing.T, pool *pgxpool.Pool, alertID int64, ackType string, falsePositive bool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO alert_acknowledgments (alert_id, acknowledged_by, acknowledge_type, message, false_positive)
		VALUES ($1, 'tester', $2, 'test message', $3)
	`, alertID, ackType, falsePositive)
	if err != nil {
		t.Fatalf("Failed to insert acknowledgment for alert %d: %v", alertID, err)
	}
}

// insertTestBlackout creates a blackout for a connection.
func insertTestBlackout(t *testing.T, pool *pgxpool.Pool, connID int, start, end time.Time, reason string) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO blackouts (scope, connection_id, start_time, end_time, reason, created_by)
		VALUES ('server', $1, $2, $3, $4, 'tester')
		RETURNING id
	`, connID, start, end, reason).Scan(&id)
	if err != nil {
		t.Fatalf("Failed to insert blackout for connection %d: %v", connID, err)
	}
	return id
}

// getAlertStatus reads the current status of an alert.
func getAlertStatus(t *testing.T, pool *pgxpool.Pool, alertID int64) string {
	t.Helper()
	var status string
	err := pool.QueryRow(context.Background(),
		`SELECT status FROM alerts WHERE id = $1`, alertID).Scan(&status)
	if err != nil {
		t.Fatalf("Failed to get alert status for %d: %v", alertID, err)
	}
	return status
}

// countAlerts counts alerts matching the given status.
func countAlerts(t *testing.T, pool *pgxpool.Pool, status string) int {
	t.Helper()
	var count int
	err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM alerts WHERE status = $1`, status).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count alerts with status %s: %v", status, err)
	}
	return count
}

// countAcknowledgments counts acknowledgments for an alert.
func countAcknowledgments(t *testing.T, pool *pgxpool.Pool, alertID int64) int {
	t.Helper()
	var count int
	err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM alert_acknowledgments WHERE alert_id = $1`, alertID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count acknowledgments for alert %d: %v", alertID, err)
	}
	return count
}

// newTestConfig creates a minimal config for testing.
func newTestConfig() *config.Config {
	return &config.Config{
		Threshold: config.ThresholdConfig{
			EvaluationIntervalSeconds: 60,
		},
		Baselines: config.BaselineConfig{
			RefreshIntervalSeconds: 3600,
		},
		Anomaly: config.AnomalyConfig{
			Enabled: false, // Disable anomaly detection for these tests
		},
	}
}

// TestEngine_AlertCRUD_Integration tests the basic alert CRUD operations
// through the engine's datastore.
func TestEngine_AlertCRUD_Integration(t *testing.T) {
	ds, pool, cleanup := newEngineIntegrationTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "crud-test-conn")
	ruleID := insertTestAlertRule(t, pool, "test_crud_rule", "test.metric", ">", 80.0, "warning")

	t.Run("CreateAlert", func(t *testing.T) {
		alert := &database.Alert{
			AlertType:    "threshold",
			RuleID:       &ruleID,
			ConnectionID: connID,
			Severity:     "warning",
			Title:        "Test Alert",
			Description:  "Test description",
			Status:       "active",
			TriggeredAt:  time.Now(),
		}

		err := ds.CreateAlert(ctx, alert)
		if err != nil {
			t.Fatalf("CreateAlert failed: %v", err)
		}
		if alert.ID == 0 {
			t.Error("CreateAlert did not set alert ID")
		}
	})

	t.Run("GetActiveAlerts", func(t *testing.T) {
		alerts, err := ds.GetActiveAlerts(ctx)
		if err != nil {
			t.Fatalf("GetActiveAlerts failed: %v", err)
		}
		if len(alerts) == 0 {
			t.Error("Expected at least one active alert")
		}
	})

	t.Run("ClearAlert", func(t *testing.T) {
		alertID := insertTestAlert(t, pool, "threshold", &ruleID, connID, "warning", "active", "To Be Cleared")

		err := ds.ClearAlert(ctx, alertID)
		if err != nil {
			t.Fatalf("ClearAlert failed: %v", err)
		}

		status := getAlertStatus(t, pool, alertID)
		if status != "cleared" {
			t.Errorf("Alert status = %q, want \"cleared\"", status)
		}
	})

	t.Run("GetAlert", func(t *testing.T) {
		alertID := insertTestAlert(t, pool, "threshold", &ruleID, connID, "critical", "active", "Get Test")

		alert, err := ds.GetAlert(ctx, alertID)
		if err != nil {
			t.Fatalf("GetAlert failed: %v", err)
		}
		if alert.ID != alertID {
			t.Errorf("GetAlert returned wrong ID: got %d, want %d", alert.ID, alertID)
		}
		if alert.Severity != "critical" {
			t.Errorf("GetAlert severity = %q, want \"critical\"", alert.Severity)
		}
	})
}

// TestEngine_AlertReactivation_Integration tests the alert reactivation logic
// that clears acknowledgments when an alert is reactivated.
func TestEngine_AlertReactivation_Integration(t *testing.T) {
	ds, pool, cleanup := newEngineIntegrationTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "reactivate-test-conn")
	ruleID := insertTestAlertRule(t, pool, "reactivate_rule", "reactivate.metric", ">", 50.0, "warning")

	t.Run("ReactivateAcknowledgedAlert", func(t *testing.T) {
		// Create an acknowledged alert
		alertID := insertTestAlert(t, pool, "threshold", &ruleID, connID, "warning", "acknowledged", "Acknowledged Alert")
		insertTestAcknowledgment(t, pool, alertID, "acknowledge", false)

		// Verify precondition
		ackCount := countAcknowledgments(t, pool, alertID)
		if ackCount != 1 {
			t.Fatalf("Precondition: expected 1 acknowledgment, got %d", ackCount)
		}

		// Reactivate
		err := ds.ReactivateAlert(ctx, alertID)
		if err != nil {
			t.Fatalf("ReactivateAlert failed: %v", err)
		}

		// Verify status changed
		status := getAlertStatus(t, pool, alertID)
		if status != "active" {
			t.Errorf("Alert status = %q, want \"active\"", status)
		}

		// Verify acknowledgment was deleted
		ackCount = countAcknowledgments(t, pool, alertID)
		if ackCount != 0 {
			t.Errorf("Acknowledgments after reactivation = %d, want 0", ackCount)
		}
	})

	t.Run("ReactivateActiveAlertIsNoOp", func(t *testing.T) {
		// Create an active alert (not acknowledged)
		alertID := insertTestAlert(t, pool, "threshold", &ruleID, connID, "warning", "active", "Already Active")

		// Reactivate should be a no-op
		err := ds.ReactivateAlert(ctx, alertID)
		if err != nil {
			t.Fatalf("ReactivateAlert failed: %v", err)
		}

		// Status should still be active
		status := getAlertStatus(t, pool, alertID)
		if status != "active" {
			t.Errorf("Alert status = %q, want \"active\"", status)
		}
	})
}

// TestEngine_BlackoutActive_Integration tests the blackout check functionality.
func TestEngine_BlackoutActive_Integration(t *testing.T) {
	ds, pool, cleanup := newEngineIntegrationTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "blackout-test-conn")

	t.Run("NoActiveBlackout", func(t *testing.T) {
		active, err := ds.IsBlackoutActive(ctx, &connID, nil)
		if err != nil {
			t.Fatalf("IsBlackoutActive failed: %v", err)
		}
		if active {
			t.Error("Expected no active blackout, got active")
		}
	})

	t.Run("ActiveBlackout", func(t *testing.T) {
		// Create a blackout that spans now
		start := time.Now().Add(-1 * time.Hour)
		end := time.Now().Add(1 * time.Hour)
		insertTestBlackout(t, pool, connID, start, end, "Maintenance")

		active, err := ds.IsBlackoutActive(ctx, &connID, nil)
		if err != nil {
			t.Fatalf("IsBlackoutActive failed: %v", err)
		}
		if !active {
			t.Error("Expected active blackout, got inactive")
		}
	})

	t.Run("ExpiredBlackout", func(t *testing.T) {
		connID2 := insertTestConnection(t, pool, "expired-blackout-conn")
		// Create an expired blackout
		start := time.Now().Add(-2 * time.Hour)
		end := time.Now().Add(-1 * time.Hour)
		insertTestBlackout(t, pool, connID2, start, end, "Past Maintenance")

		active, err := ds.IsBlackoutActive(ctx, &connID2, nil)
		if err != nil {
			t.Fatalf("IsBlackoutActive failed: %v", err)
		}
		if active {
			t.Error("Expected no active blackout for expired period")
		}
	})
}

// TestEngine_ConnectionErrorAlerts_Integration tests connection error alert
// creation and management.
func TestEngine_ConnectionErrorAlerts_Integration(t *testing.T) {
	ds, pool, cleanup := newEngineIntegrationTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "conn-error-test")

	t.Run("CreateConnectionAlert", func(t *testing.T) {
		alert, err := ds.CreateConnectionAlert(ctx, connID, "conn-error-test", "Connection refused")
		if err != nil {
			t.Fatalf("CreateConnectionAlert failed: %v", err)
		}
		if alert.ID == 0 {
			t.Error("CreateConnectionAlert did not set alert ID")
		}
		if alert.AlertType != "connection" {
			t.Errorf("Alert type = %q, want \"connection\"", alert.AlertType)
		}
		if alert.Severity != "critical" {
			t.Errorf("Alert severity = %q, want \"critical\"", alert.Severity)
		}
	})

	t.Run("GetActiveConnectionAlert", func(t *testing.T) {
		alertID, desc, found, err := ds.GetActiveConnectionAlert(ctx, connID)
		if err != nil {
			t.Fatalf("GetActiveConnectionAlert failed: %v", err)
		}
		if !found {
			t.Fatal("Expected to find active connection alert")
		}
		if alertID == 0 {
			t.Error("Alert ID should not be 0")
		}
		if desc != "Connection refused" {
			t.Errorf("Description = %q, want \"Connection refused\"", desc)
		}
	})

	t.Run("UpdateConnectionAlertDescription", func(t *testing.T) {
		alertID, _, found, err := ds.GetActiveConnectionAlert(ctx, connID)
		if err != nil || !found {
			t.Fatalf("Failed to get alert: found=%v, err=%v", found, err)
		}

		err = ds.UpdateConnectionAlertDescription(ctx, alertID, "Timeout after 30s")
		if err != nil {
			t.Fatalf("UpdateConnectionAlertDescription failed: %v", err)
		}

		_, desc, _, _ := ds.GetActiveConnectionAlert(ctx, connID)
		if desc != "Timeout after 30s" {
			t.Errorf("Description = %q, want \"Timeout after 30s\"", desc)
		}
	})
}

// TestEngine_AlertRules_Integration tests alert rule queries.
func TestEngine_AlertRules_Integration(t *testing.T) {
	ds, pool, cleanup := newEngineIntegrationTestEnv(t)
	defer cleanup()

	ctx := context.Background()

	// Insert some test rules
	insertTestAlertRule(t, pool, "enabled_rule_1", "metric.one", ">", 80.0, "warning")
	insertTestAlertRule(t, pool, "enabled_rule_2", "metric.two", "<", 20.0, "critical")

	// Insert a disabled rule
	_, err := pool.Exec(ctx, `
		INSERT INTO alert_rules (name, description, category, metric_name, default_operator,
		    default_threshold, default_severity, default_enabled, is_built_in)
		VALUES ('disabled_rule', 'Disabled', 'test', 'metric.disabled', '>', 90, 'info', FALSE, FALSE)
	`)
	if err != nil {
		t.Fatalf("Failed to insert disabled rule: %v", err)
	}

	t.Run("GetEnabledAlertRules", func(t *testing.T) {
		rules, err := ds.GetEnabledAlertRules(ctx)
		if err != nil {
			t.Fatalf("GetEnabledAlertRules failed: %v", err)
		}
		if len(rules) != 2 {
			t.Errorf("Expected 2 enabled rules, got %d", len(rules))
		}

		// Verify disabled rule is not included
		for _, rule := range rules {
			if rule.Name == "disabled_rule" {
				t.Error("Disabled rule should not be returned")
			}
		}
	})

	t.Run("GetAlertRuleByName", func(t *testing.T) {
		rule, err := ds.GetAlertRuleByName(ctx, "enabled_rule_1")
		if err != nil {
			t.Fatalf("GetAlertRuleByName failed: %v", err)
		}
		if rule.Name != "enabled_rule_1" {
			t.Errorf("Rule name = %q, want \"enabled_rule_1\"", rule.Name)
		}
		if rule.MetricName != "metric.one" {
			t.Errorf("Metric name = %q, want \"metric.one\"", rule.MetricName)
		}
	})
}

// TestEngine_EffectiveThreshold_Integration tests the threshold override
// resolution order (server > cluster > group > estate defaults).
func TestEngine_EffectiveThreshold_Integration(t *testing.T) {
	ds, pool, cleanup := newEngineIntegrationTestEnv(t)
	defer cleanup()

	ctx := context.Background()

	// Create hierarchy: group -> cluster -> connection
	var groupID, clusterID int
	err := pool.QueryRow(ctx, `INSERT INTO cluster_groups (name) VALUES ('Test Group') RETURNING id`).Scan(&groupID)
	if err != nil {
		t.Fatalf("Failed to create group: %v", err)
	}
	err = pool.QueryRow(ctx, `INSERT INTO clusters (name, group_id) VALUES ('Test Cluster', $1) RETURNING id`, groupID).Scan(&clusterID)
	if err != nil {
		t.Fatalf("Failed to create cluster: %v", err)
	}

	var connID int
	err = pool.QueryRow(ctx, `
		INSERT INTO connections (name, enabled, is_monitored, cluster_id)
		VALUES ('Threshold Test Conn', TRUE, TRUE, $1)
		RETURNING id
	`, clusterID).Scan(&connID)
	if err != nil {
		t.Fatalf("Failed to create connection: %v", err)
	}

	ruleID := insertTestAlertRule(t, pool, "threshold_test_rule", "test.metric", ">", 50.0, "warning")

	t.Run("DefaultFromRule", func(t *testing.T) {
		threshold, operator, severity, enabled := ds.GetEffectiveThreshold(ctx, ruleID, connID, nil)
		if !enabled {
			t.Error("Rule should be enabled by default")
		}
		if threshold != 50.0 {
			t.Errorf("Threshold = %v, want 50.0", threshold)
		}
		if operator != ">" {
			t.Errorf("Operator = %q, want \">\"", operator)
		}
		if severity != "warning" {
			t.Errorf("Severity = %q, want \"warning\"", severity)
		}
	})

	t.Run("ServerOverride", func(t *testing.T) {
		// Add server-level override
		_, err := pool.Exec(ctx, `
			INSERT INTO alert_thresholds (rule_id, scope, connection_id, operator, threshold, severity, enabled)
			VALUES ($1, 'server', $2, '>=', 75.0, 'critical', TRUE)
		`, ruleID, connID)
		if err != nil {
			t.Fatalf("Failed to insert server override: %v", err)
		}

		threshold, operator, severity, enabled := ds.GetEffectiveThreshold(ctx, ruleID, connID, nil)
		if !enabled {
			t.Error("Override should be enabled")
		}
		if threshold != 75.0 {
			t.Errorf("Threshold = %v, want 75.0", threshold)
		}
		if operator != ">=" {
			t.Errorf("Operator = %q, want \">=\"", operator)
		}
		if severity != "critical" {
			t.Errorf("Severity = %q, want \"critical\"", severity)
		}
	})
}

// TestEngine_AlerterSettings_Integration tests alerter settings retrieval.
func TestEngine_AlerterSettings_Integration(t *testing.T) {
	ds, _, cleanup := newEngineIntegrationTestEnv(t)
	defer cleanup()

	ctx := context.Background()

	settings, err := ds.GetAlerterSettings(ctx)
	if err != nil {
		t.Fatalf("GetAlerterSettings failed: %v", err)
	}

	if settings.RetentionDays != 30 {
		t.Errorf("RetentionDays = %d, want 30", settings.RetentionDays)
	}
	if !settings.DefaultAnomalyEnabled {
		t.Error("DefaultAnomalyEnabled should be true")
	}
	if settings.DefaultAnomalySensitivity != 3.0 {
		t.Errorf("DefaultAnomalySensitivity = %v, want 3.0", settings.DefaultAnomalySensitivity)
	}
}

// TestEngine_DeleteOldAlerts_Integration tests the retention cleanup logic.
func TestEngine_DeleteOldAlerts_Integration(t *testing.T) {
	ds, pool, cleanup := newEngineIntegrationTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "retention-test-conn")
	ruleID := insertTestAlertRule(t, pool, "retention_rule", "retention.metric", ">", 80.0, "warning")

	// Insert an old cleared alert
	_, err := pool.Exec(ctx, `
		INSERT INTO alerts (alert_type, rule_id, connection_id, severity, title, description, status, triggered_at, cleared_at)
		VALUES ('threshold', $1, $2, 'warning', 'Old Alert', 'Old', 'cleared', NOW() - INTERVAL '60 days', NOW() - INTERVAL '60 days')
	`, ruleID, connID)
	if err != nil {
		t.Fatalf("Failed to insert old alert: %v", err)
	}

	// Insert a recent alert
	insertTestAlert(t, pool, "threshold", &ruleID, connID, "warning", "cleared", "Recent Alert")

	// Delete alerts older than 30 days
	cutoff := time.Now().AddDate(0, 0, -30)
	deleted, err := ds.DeleteOldAlerts(ctx, cutoff)
	if err != nil {
		t.Fatalf("DeleteOldAlerts failed: %v", err)
	}

	if deleted != 1 {
		t.Errorf("Deleted = %d, want 1", deleted)
	}

	// Verify recent alert still exists
	var count int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM alerts WHERE title = 'Recent Alert'`).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count alerts: %v", err)
	}
	if count != 1 {
		t.Errorf("Recent alert count = %d, want 1", count)
	}
}

// TestEngine_MonitoredConnections_Integration tests monitored connection queries.
func TestEngine_MonitoredConnections_Integration(t *testing.T) {
	ds, pool, cleanup := newEngineIntegrationTestEnv(t)
	defer cleanup()

	ctx := context.Background()

	// Insert monitored connection with error
	_, err := pool.Exec(ctx, `
		INSERT INTO connections (name, enabled, is_monitored, connection_error)
		VALUES ('Error Connection', TRUE, TRUE, 'Connection refused')
	`)
	if err != nil {
		t.Fatalf("Failed to insert connection with error: %v", err)
	}

	// Insert monitored connection without error
	_, err = pool.Exec(ctx, `
		INSERT INTO connections (name, enabled, is_monitored, connection_error)
		VALUES ('Healthy Connection', TRUE, TRUE, NULL)
	`)
	if err != nil {
		t.Fatalf("Failed to insert healthy connection: %v", err)
	}

	// Insert non-monitored connection
	_, err = pool.Exec(ctx, `
		INSERT INTO connections (name, enabled, is_monitored, connection_error)
		VALUES ('Unmonitored Connection', TRUE, FALSE, 'Some error')
	`)
	if err != nil {
		t.Fatalf("Failed to insert unmonitored connection: %v", err)
	}

	connections, err := ds.GetMonitoredConnectionErrors(ctx)
	if err != nil {
		t.Fatalf("GetMonitoredConnectionErrors failed: %v", err)
	}

	if len(connections) != 2 {
		t.Errorf("Expected 2 monitored connections, got %d", len(connections))
	}

	// Verify unmonitored connection is not included
	for _, conn := range connections {
		if conn.Name == "Unmonitored Connection" {
			t.Error("Unmonitored connection should not be returned")
		}
	}
}

// TestEngine_UpdateAlertValues_Integration tests alert value updates.
func TestEngine_UpdateAlertValues_Integration(t *testing.T) {
	ds, pool, cleanup := newEngineIntegrationTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "update-values-conn")
	ruleID := insertTestAlertRule(t, pool, "update_values_rule", "update.metric", ">", 80.0, "warning")

	alertID := insertTestAlert(t, pool, "threshold", &ruleID, connID, "warning", "active", "Update Test")

	// Update values
	err := ds.UpdateAlertValues(ctx, alertID, 95.0, 85.0, ">=", "critical")
	if err != nil {
		t.Fatalf("UpdateAlertValues failed: %v", err)
	}

	// Verify updates
	var metricValue, thresholdValue float64
	var operator, severity string
	err = pool.QueryRow(ctx, `
		SELECT metric_value, threshold_value, operator, severity FROM alerts WHERE id = $1
	`, alertID).Scan(&metricValue, &thresholdValue, &operator, &severity)
	if err != nil {
		t.Fatalf("Failed to read updated values: %v", err)
	}

	if metricValue != 95.0 {
		t.Errorf("metric_value = %v, want 95.0", metricValue)
	}
	if thresholdValue != 85.0 {
		t.Errorf("threshold_value = %v, want 85.0", thresholdValue)
	}
	if operator != ">=" {
		t.Errorf("operator = %q, want \">=\"", operator)
	}
	if severity != "critical" {
		t.Errorf("severity = %q, want \"critical\"", severity)
	}
}

// TestEngine_GetActiveThresholdAlert_Integration tests finding existing
// active alerts for a rule/connection combination.
func TestEngine_GetActiveThresholdAlert_Integration(t *testing.T) {
	ds, pool, cleanup := newEngineIntegrationTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "active-threshold-conn")
	ruleID := insertTestAlertRule(t, pool, "active_threshold_rule", "active.metric", ">", 80.0, "warning")

	t.Run("NoExistingAlert", func(t *testing.T) {
		alert, err := ds.GetActiveThresholdAlert(ctx, ruleID, connID, nil)
		if err != nil {
			t.Fatalf("GetActiveThresholdAlert failed: %v", err)
		}
		if alert != nil {
			t.Error("Expected no alert, got one")
		}
	})

	t.Run("ExistingActiveAlert", func(t *testing.T) {
		insertTestAlert(t, pool, "threshold", &ruleID, connID, "warning", "active", "Active Threshold")

		alert, err := ds.GetActiveThresholdAlert(ctx, ruleID, connID, nil)
		if err != nil {
			t.Fatalf("GetActiveThresholdAlert failed: %v", err)
		}
		if alert == nil {
			t.Fatal("Expected to find alert")
		}
		if alert.Title != "Active Threshold" {
			t.Errorf("Alert title = %q, want \"Active Threshold\"", alert.Title)
		}
	})

	t.Run("ClearedAlertNotReturned", func(t *testing.T) {
		connID2 := insertTestConnection(t, pool, "cleared-threshold-conn")
		ruleID2 := insertTestAlertRule(t, pool, "cleared_threshold_rule", "cleared.metric", ">", 80.0, "warning")
		alertID := insertTestAlert(t, pool, "threshold", &ruleID2, connID2, "warning", "active", "To Clear")

		// Clear the alert
		err := ds.ClearAlert(ctx, alertID)
		if err != nil {
			t.Fatalf("ClearAlert failed: %v", err)
		}

		// Should not find cleared alert
		alert, err := ds.GetActiveThresholdAlert(ctx, ruleID2, connID2, nil)
		if err != nil {
			t.Fatalf("GetActiveThresholdAlert failed: %v", err)
		}
		if alert != nil {
			t.Error("Should not find cleared alert")
		}
	})
}
