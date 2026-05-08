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
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// fullSchemaSetup creates the full set of tables required to exercise
// queries.go, alert_queries.go, anomaly_queries.go, and
// notification_queries.go. The schema mirrors the production
// collector schema for the columns these queries reference; columns
// unused by the queries are omitted for brevity. The schema is
// dropped on teardown so each test run starts from a clean slate.
//
// pgvector is required for anomaly_embeddings; tests that exercise
// FindSimilarAnomalies are skipped when pgvector is unavailable.
const fullSchemaSetup = `
DROP SCHEMA IF EXISTS metrics CASCADE;
DROP TABLE IF EXISTS notification_channel_overrides CASCADE;
DROP TABLE IF EXISTS notification_reminder_state CASCADE;
DROP TABLE IF EXISTS notification_history CASCADE;
DROP TABLE IF EXISTS connection_notification_channels CASCADE;
DROP TABLE IF EXISTS email_recipients CASCADE;
DROP TABLE IF EXISTS notification_channels CASCADE;
DROP TABLE IF EXISTS anomaly_embeddings CASCADE;
DROP TABLE IF EXISTS anomaly_candidates CASCADE;
DROP TABLE IF EXISTS metric_baselines CASCADE;
DROP TABLE IF EXISTS blackout_schedules CASCADE;
DROP TABLE IF EXISTS blackouts CASCADE;
DROP TABLE IF EXISTS alert_acknowledgments CASCADE;
DROP TABLE IF EXISTS alerts CASCADE;
DROP TABLE IF EXISTS alert_thresholds CASCADE;
DROP TABLE IF EXISTS alert_rules CASCADE;
DROP TABLE IF EXISTS probe_availability CASCADE;
DROP TABLE IF EXISTS probe_configs CASCADE;
DROP TABLE IF EXISTS alerter_settings CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
DROP TABLE IF EXISTS cluster_groups CASCADE;

CREATE TABLE cluster_groups (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL UNIQUE,
    description TEXT
);

CREATE TABLE clusters (
    id SERIAL PRIMARY KEY,
    group_id INTEGER REFERENCES cluster_groups(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    description TEXT
);

CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    host VARCHAR(255) NOT NULL DEFAULT 'localhost',
    port INTEGER NOT NULL DEFAULT 5432,
    is_monitored BOOLEAN NOT NULL DEFAULT TRUE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE SET NULL,
    connection_error TEXT
);

CREATE TABLE alerter_settings (
    id INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    retention_days INTEGER NOT NULL DEFAULT 90,
    default_anomaly_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    default_anomaly_sensitivity REAL NOT NULL DEFAULT 3.0,
    baseline_refresh_interval_mins INTEGER NOT NULL DEFAULT 60,
    correlation_window_seconds INTEGER NOT NULL DEFAULT 120,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO alerter_settings (id) VALUES (1);

CREATE TABLE probe_configs (
    id SERIAL PRIMARY KEY,
    connection_id INTEGER,
    is_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    name TEXT NOT NULL,
    collection_interval_seconds INTEGER NOT NULL DEFAULT 60
);

CREATE TABLE probe_availability (
    id BIGSERIAL PRIMARY KEY,
    connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    database_name TEXT,
    probe_name TEXT NOT NULL,
    extension_name TEXT,
    is_available BOOLEAN NOT NULL DEFAULT FALSE,
    last_checked TIMESTAMPTZ,
    last_collected TIMESTAMPTZ,
    unavailable_reason TEXT,
    UNIQUE(connection_id, database_name, probe_name)
);

CREATE TABLE alert_rules (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL,
    category TEXT NOT NULL,
    metric_name TEXT NOT NULL,
    default_operator TEXT NOT NULL,
    default_threshold REAL NOT NULL,
    default_severity TEXT NOT NULL,
    default_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    required_extension TEXT,
    is_built_in BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE alert_thresholds (
    id BIGSERIAL PRIMARY KEY,
    rule_id BIGINT NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
    connection_id INTEGER REFERENCES connections(id) ON DELETE CASCADE,
    database_name TEXT,
    operator TEXT NOT NULL,
    threshold REAL NOT NULL,
    severity TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    scope TEXT NOT NULL DEFAULT 'server',
    group_id INTEGER REFERENCES cluster_groups(id) ON DELETE CASCADE,
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE CASCADE,
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
    metric_name TEXT,
    metric_value REAL,
    threshold_value REAL,
    operator TEXT,
    severity TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    object_name TEXT,
    correlation_id TEXT,
    status TEXT NOT NULL CHECK (status IN ('active', 'cleared', 'acknowledged')),
    triggered_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    cleared_at TIMESTAMPTZ,
    last_updated TIMESTAMPTZ,
    anomaly_score REAL,
    anomaly_details JSONB,
    ai_analysis TEXT,
    ai_analysis_metric_value REAL,
    last_reevaluated_at TIMESTAMPTZ,
    reevaluation_count INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE alert_acknowledgments (
    id BIGSERIAL PRIMARY KEY,
    alert_id BIGINT NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
    acknowledged_by TEXT NOT NULL,
    acknowledged_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    acknowledge_type TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    false_positive BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE blackouts (
    id BIGSERIAL PRIMARY KEY,
    connection_id INTEGER REFERENCES connections(id) ON DELETE CASCADE,
    database_name TEXT,
    reason TEXT NOT NULL,
    start_time TIMESTAMPTZ NOT NULL,
    end_time TIMESTAMPTZ NOT NULL,
    created_by TEXT NOT NULL,
    scope TEXT NOT NULL DEFAULT 'server',
    group_id INTEGER REFERENCES cluster_groups(id) ON DELETE CASCADE,
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE blackout_schedules (
    id BIGSERIAL PRIMARY KEY,
    connection_id INTEGER REFERENCES connections(id) ON DELETE CASCADE,
    database_name TEXT,
    name TEXT NOT NULL,
    cron_expression TEXT NOT NULL,
    duration_minutes INTEGER NOT NULL,
    timezone TEXT NOT NULL DEFAULT 'UTC',
    reason TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_by TEXT NOT NULL,
    scope TEXT NOT NULL DEFAULT 'server',
    group_id INTEGER REFERENCES cluster_groups(id) ON DELETE CASCADE,
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE metric_baselines (
    id BIGSERIAL PRIMARY KEY,
    connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    database_name TEXT,
    metric_name TEXT NOT NULL,
    period_type TEXT NOT NULL,
    day_of_week INTEGER,
    hour_of_day INTEGER,
    mean REAL NOT NULL,
    stddev REAL NOT NULL,
    min REAL NOT NULL,
    max REAL NOT NULL,
    sample_count BIGINT NOT NULL DEFAULT 0,
    last_calculated TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX idx_metric_baselines_unique
    ON metric_baselines(
        connection_id,
        COALESCE(database_name, ''),
        metric_name,
        period_type,
        COALESCE(day_of_week, -1),
        COALESCE(hour_of_day, -1)
    );

CREATE TABLE anomaly_candidates (
    id BIGSERIAL PRIMARY KEY,
    connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    database_name TEXT,
    metric_name TEXT NOT NULL,
    metric_value REAL NOT NULL,
    z_score REAL NOT NULL,
    detected_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    context JSONB NOT NULL DEFAULT '{}',
    tier1_pass BOOLEAN NOT NULL DEFAULT FALSE,
    tier2_score REAL,
    tier2_pass BOOLEAN,
    tier3_result TEXT,
    tier3_pass BOOLEAN,
    tier3_error TEXT,
    final_decision TEXT,
    alert_id BIGINT REFERENCES alerts(id) ON DELETE SET NULL,
    embedding_id BIGINT,
    processed_at TIMESTAMPTZ
);

CREATE TABLE notification_channels (
    id BIGSERIAL PRIMARY KEY,
    owner_username VARCHAR(255),
    owner_token VARCHAR(255),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    channel_type TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    webhook_url_encrypted TEXT,
    endpoint_url TEXT,
    http_method TEXT DEFAULT 'POST',
    headers_json JSONB DEFAULT '{}',
    auth_type TEXT,
    auth_credentials_encrypted TEXT,
    smtp_host TEXT,
    smtp_port INTEGER DEFAULT 587,
    smtp_username TEXT,
    smtp_password_encrypted TEXT,
    smtp_use_tls BOOLEAN DEFAULT TRUE,
    from_address TEXT,
    from_name TEXT,
    template_alert_fire TEXT,
    template_alert_clear TEXT,
    template_reminder TEXT,
    reminder_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    reminder_interval_hours INTEGER DEFAULT 24,
    is_estate_default BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE email_recipients (
    id BIGSERIAL PRIMARY KEY,
    channel_id BIGINT NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
    email_address TEXT NOT NULL,
    display_name TEXT,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE connection_notification_channels (
    id BIGSERIAL PRIMARY KEY,
    connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    channel_id BIGINT NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    reminder_enabled_override BOOLEAN,
    reminder_interval_hours_override INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE notification_history (
    id BIGSERIAL PRIMARY KEY,
    alert_id BIGINT REFERENCES alerts(id) ON DELETE SET NULL,
    channel_id BIGINT REFERENCES notification_channels(id) ON DELETE SET NULL,
    connection_id INTEGER REFERENCES connections(id) ON DELETE SET NULL,
    notification_type TEXT NOT NULL,
    status TEXT NOT NULL,
    payload_json JSONB,
    response_code INTEGER,
    response_body TEXT,
    error_message TEXT,
    attempt_count INTEGER NOT NULL DEFAULT 1,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    next_retry_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    sent_at TIMESTAMPTZ
);

CREATE TABLE notification_reminder_state (
    id BIGSERIAL PRIMARY KEY,
    alert_id BIGINT NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
    channel_id BIGINT NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
    last_reminder_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    reminder_count INTEGER NOT NULL DEFAULT 0,
    CONSTRAINT alert_channel_reminder_unique UNIQUE (alert_id, channel_id)
);

CREATE TABLE notification_channel_overrides (
    id BIGSERIAL PRIMARY KEY,
    channel_id BIGINT NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
    scope TEXT NOT NULL,
    connection_id INTEGER REFERENCES connections(id) ON DELETE CASCADE,
    group_id INTEGER REFERENCES cluster_groups(id) ON DELETE CASCADE,
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE SCHEMA metrics;

CREATE TABLE metrics.pg_node_role (
    connection_id INTEGER NOT NULL,
    primary_role TEXT,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

// fullSchemaTeardown drops every object the schema creates so a
// subsequent test run starts from a clean slate.
const fullSchemaTeardown = `
DROP SCHEMA IF EXISTS metrics CASCADE;
DROP TABLE IF EXISTS notification_channel_overrides CASCADE;
DROP TABLE IF EXISTS notification_reminder_state CASCADE;
DROP TABLE IF EXISTS notification_history CASCADE;
DROP TABLE IF EXISTS connection_notification_channels CASCADE;
DROP TABLE IF EXISTS email_recipients CASCADE;
DROP TABLE IF EXISTS notification_channels CASCADE;
DROP TABLE IF EXISTS anomaly_embeddings CASCADE;
DROP TABLE IF EXISTS anomaly_candidates CASCADE;
DROP TABLE IF EXISTS metric_baselines CASCADE;
DROP TABLE IF EXISTS blackout_schedules CASCADE;
DROP TABLE IF EXISTS blackouts CASCADE;
DROP TABLE IF EXISTS alert_acknowledgments CASCADE;
DROP TABLE IF EXISTS alerts CASCADE;
DROP TABLE IF EXISTS alert_thresholds CASCADE;
DROP TABLE IF EXISTS alert_rules CASCADE;
DROP TABLE IF EXISTS probe_availability CASCADE;
DROP TABLE IF EXISTS probe_configs CASCADE;
DROP TABLE IF EXISTS alerter_settings CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
DROP TABLE IF EXISTS cluster_groups CASCADE;
`

// pgvectorAvailable reports whether the pgvector extension is installed
// in the current connection. The anomaly_embeddings table is created
// only when pgvector is present.
func pgvectorAvailable(ctx context.Context, pool *pgxpool.Pool) bool {
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM pg_extension WHERE extname = 'vector'
		)
	`).Scan(&exists)
	if err != nil {
		return false
	}
	return exists
}

// createAnomalyEmbeddingsTable creates anomaly_embeddings when pgvector
// is available. The table is required by StoreAnomalyEmbedding and
// FindSimilarAnomalies; the tests skip when it cannot be created.
func createAnomalyEmbeddingsTable(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		DROP TABLE IF EXISTS anomaly_embeddings CASCADE;
		CREATE EXTENSION IF NOT EXISTS vector;
		CREATE TABLE anomaly_embeddings (
			id BIGSERIAL PRIMARY KEY,
			candidate_id BIGINT REFERENCES anomaly_candidates(id) ON DELETE CASCADE,
			embedding vector(3),
			model_name TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(candidate_id)
		);
	`)
	return err
}

// newFullTestDatastore returns a Datastore backed by the integration
// test database with the full schema installed. The test is skipped
// when no test database is available.
func newFullTestDatastore(t *testing.T) (*Datastore, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping integration test")
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

	if _, err := pool.Exec(ctx, fullSchemaSetup); err != nil {
		pool.Close()
		t.Fatalf("Failed to create full test schema: %v", err)
	}

	ds := &Datastore{pool: pool, config: nil}

	cleanup := func() {
		if _, err := pool.Exec(context.Background(), fullSchemaTeardown); err != nil {
			t.Logf("full schema teardown failed: %v", err)
		}
		pool.Close()
	}

	return ds, pool, cleanup
}

// insertTestConnection inserts a connection row and returns its id.
// The connection has is_monitored=TRUE and enabled=TRUE by default.
func insertTestConnection(t *testing.T, pool *pgxpool.Pool, name string) int {
	t.Helper()
	var id int
	err := pool.QueryRow(context.Background(),
		`INSERT INTO connections (name) VALUES ($1) RETURNING id`,
		name).Scan(&id)
	if err != nil {
		t.Fatalf("Failed to insert connection %q: %v", name, err)
	}
	return id
}

// insertTestConnectionInCluster inserts a connection assigned to a
// cluster. Returns the connection id.
func insertTestConnectionInCluster(t *testing.T, pool *pgxpool.Pool, name string, clusterID int) int {
	t.Helper()
	var id int
	err := pool.QueryRow(context.Background(),
		`INSERT INTO connections (name, cluster_id) VALUES ($1, $2) RETURNING id`,
		name, clusterID).Scan(&id)
	if err != nil {
		t.Fatalf("Failed to insert connection %q: %v", name, err)
	}
	return id
}

// insertGroupAndCluster inserts a cluster_group and a clusters row in
// it, returning their generated ids.
func insertGroupAndCluster(t *testing.T, pool *pgxpool.Pool, groupName, clusterName string) (groupID, clusterID int) {
	t.Helper()
	ctx := context.Background()
	err := pool.QueryRow(ctx,
		`INSERT INTO cluster_groups (name) VALUES ($1) RETURNING id`,
		groupName).Scan(&groupID)
	if err != nil {
		t.Fatalf("Failed to insert group %q: %v", groupName, err)
	}
	err = pool.QueryRow(ctx,
		`INSERT INTO clusters (group_id, name) VALUES ($1, $2) RETURNING id`,
		groupID, clusterName).Scan(&clusterID)
	if err != nil {
		t.Fatalf("Failed to insert cluster %q: %v", clusterName, err)
	}
	return groupID, clusterID
}

// insertTestRule inserts an alert_rules row and returns its id.
func insertTestRule(t *testing.T, pool *pgxpool.Pool, name, metric, op string, threshold float64, severity string, enabled bool) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO alert_rules (name, description, category, metric_name,
		    default_operator, default_threshold, default_severity,
		    default_enabled, is_built_in)
		VALUES ($1, 'desc', 'cat', $2, $3, $4, $5, $6, TRUE)
		RETURNING id
	`, name, metric, op, threshold, severity, enabled).Scan(&id)
	if err != nil {
		t.Fatalf("Failed to insert rule: %v", err)
	}
	return id
}

// =============================================================================
// queries.go tests
// =============================================================================

func TestGetMonitoredConnectionErrors(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	// Insert two connections, one with an error.
	id1 := insertTestConnection(t, pool, "monitored-ok")
	if _, err := pool.Exec(ctx,
		`UPDATE connections SET connection_error = 'boom' WHERE id = $1`, id1); err != nil {
		t.Fatalf("update conn error: %v", err)
	}
	insertTestConnection(t, pool, "monitored-good")

	// Insert one not-monitored connection that must be excluded.
	if _, err := pool.Exec(ctx,
		`INSERT INTO connections (name, is_monitored) VALUES ('not-monitored', FALSE)`); err != nil {
		t.Fatalf("insert non-monitored: %v", err)
	}

	results, err := ds.GetMonitoredConnectionErrors(ctx)
	if err != nil {
		t.Fatalf("GetMonitoredConnectionErrors: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Canceled context should error.
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ds.GetMonitoredConnectionErrors(canceled); err == nil {
		t.Errorf("expected error from canceled context")
	}
}

func TestGetActiveConnectionAlert(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "conn1")

	// No alert -> not found, no error.
	if _, _, found, err := ds.GetActiveConnectionAlert(ctx, connID); err != nil || found {
		t.Errorf("expected no alert, got found=%v err=%v", found, err)
	}

	// Insert an active connection alert.
	var alertID int64
	err := pool.QueryRow(ctx, `
		INSERT INTO alerts (alert_type, connection_id, severity, title, description, status)
		VALUES ('connection', $1, 'critical', 'down', 'host unreachable', 'active')
		RETURNING id
	`, connID).Scan(&alertID)
	if err != nil {
		t.Fatalf("insert alert: %v", err)
	}

	gotID, gotDesc, found, err := ds.GetActiveConnectionAlert(ctx, connID)
	if err != nil {
		t.Fatalf("GetActiveConnectionAlert: %v", err)
	}
	if !found || gotID != alertID || gotDesc != "host unreachable" {
		t.Errorf("expected found alert id=%d desc=%q, got id=%d desc=%q found=%v",
			alertID, "host unreachable", gotID, gotDesc, found)
	}
}

func TestCreateAndUpdateConnectionAlert(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "conn-create")

	alert, err := ds.CreateConnectionAlert(ctx, connID, "myname", "boom!")
	if err != nil {
		t.Fatalf("CreateConnectionAlert: %v", err)
	}
	if alert.ID == 0 {
		t.Errorf("expected alert.ID to be set")
	}
	if alert.Description != "boom!" {
		t.Errorf("description = %q, want %q", alert.Description, "boom!")
	}
	if err := ds.UpdateConnectionAlertDescription(ctx, alert.ID, "new desc"); err != nil {
		t.Fatalf("UpdateConnectionAlertDescription: %v", err)
	}
	var got string
	if err := pool.QueryRow(ctx, `SELECT description FROM alerts WHERE id = $1`, alert.ID).Scan(&got); err != nil {
		t.Fatalf("verify select: %v", err)
	}
	if got != "new desc" {
		t.Errorf("description = %q, want %q", got, "new desc")
	}
}

func TestGetAlerterSettings(t *testing.T) {
	ds, _, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	settings, err := ds.GetAlerterSettings(ctx)
	if err != nil {
		t.Fatalf("GetAlerterSettings: %v", err)
	}
	if settings.ID != 1 {
		t.Errorf("ID = %d, want 1", settings.ID)
	}
	if settings.RetentionDays <= 0 {
		t.Errorf("RetentionDays = %d, want > 0", settings.RetentionDays)
	}
}

func TestGetEnabledAlertRules(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	insertTestRule(t, pool, "rule_a", "metric_a", ">", 10, "warning", true)
	insertTestRule(t, pool, "rule_b", "metric_b", ">", 20, "critical", true)
	insertTestRule(t, pool, "rule_disabled", "metric_c", ">", 30, "info", false)

	rules, err := ds.GetEnabledAlertRules(ctx)
	if err != nil {
		t.Fatalf("GetEnabledAlertRules: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 enabled rules, got %d", len(rules))
	}
}

func TestGetEffectiveThreshold(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	groupID, clusterID := insertGroupAndCluster(t, pool, "g1", "c1")
	connID := insertTestConnectionInCluster(t, pool, "c1-node", clusterID)
	ruleID := insertTestRule(t, pool, "rule_thr", "metric_thr", ">", 10, "warning", true)

	// 1) Falls back to defaults from alert_rules.
	thr, op, sev, en := ds.GetEffectiveThreshold(ctx, ruleID, connID, nil)
	if thr != 10 || op != ">" || sev != "warning" || !en {
		t.Errorf("default: got thr=%v op=%v sev=%v en=%v", thr, op, sev, en)
	}

	// 2) Group override applies.
	if _, err := pool.Exec(ctx, `
		INSERT INTO alert_thresholds (rule_id, scope, group_id, operator, threshold, severity, enabled)
		VALUES ($1, 'group', $2, '>=', 50, 'critical', TRUE)
	`, ruleID, groupID); err != nil {
		t.Fatalf("insert group threshold: %v", err)
	}
	thr, _, sev, _ = ds.GetEffectiveThreshold(ctx, ruleID, connID, nil)
	if thr != 50 || sev != "critical" {
		t.Errorf("group: got thr=%v sev=%v", thr, sev)
	}

	// 3) Cluster override beats group.
	if _, err := pool.Exec(ctx, `
		INSERT INTO alert_thresholds (rule_id, scope, cluster_id, operator, threshold, severity, enabled)
		VALUES ($1, 'cluster', $2, '<', 25, 'info', TRUE)
	`, ruleID, clusterID); err != nil {
		t.Fatalf("insert cluster threshold: %v", err)
	}
	thr, op, sev, _ = ds.GetEffectiveThreshold(ctx, ruleID, connID, nil)
	if thr != 25 || op != "<" || sev != "info" {
		t.Errorf("cluster: got thr=%v op=%v sev=%v", thr, op, sev)
	}

	// 4) Server override beats cluster.
	if _, err := pool.Exec(ctx, `
		INSERT INTO alert_thresholds (rule_id, scope, connection_id, operator, threshold, severity, enabled)
		VALUES ($1, 'server', $2, '==', 99, 'warning', TRUE)
	`, ruleID, connID); err != nil {
		t.Fatalf("insert server threshold: %v", err)
	}
	thr, op, _, _ = ds.GetEffectiveThreshold(ctx, ruleID, connID, nil)
	if thr != 99 || op != "==" {
		t.Errorf("server: got thr=%v op=%v", thr, op)
	}

	// 5) Unknown rule -> zero defaults.
	thr, op, sev, en = ds.GetEffectiveThreshold(ctx, 99999, connID, nil)
	if thr != 0 || op != "" || sev != "" || en {
		t.Errorf("unknown rule: got thr=%v op=%v sev=%v en=%v", thr, op, sev, en)
	}
}

func TestIsBlackoutActive(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	groupID, clusterID := insertGroupAndCluster(t, pool, "g-bk", "c-bk")
	connID := insertTestConnectionInCluster(t, pool, "node-bk", clusterID)

	// No blackout active.
	active, err := ds.IsBlackoutActive(ctx, &connID, nil)
	if err != nil || active {
		t.Errorf("expected inactive, got active=%v err=%v", active, err)
	}

	// Estate-scope blackout covers everything.
	if _, err := pool.Exec(ctx, `
		INSERT INTO blackouts (scope, reason, start_time, end_time, created_by)
		VALUES ('estate', 'maint', NOW() - INTERVAL '1 hour', NOW() + INTERVAL '1 hour', 'tester')
	`); err != nil {
		t.Fatalf("insert estate blackout: %v", err)
	}
	active, err = ds.IsBlackoutActive(ctx, &connID, nil)
	if err != nil || !active {
		t.Fatalf("estate blackout: expected active, got %v err %v", active, err)
	}

	// Clear and try cluster scope.
	if _, err := pool.Exec(ctx, `DELETE FROM blackouts`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO blackouts (scope, cluster_id, reason, start_time, end_time, created_by)
		VALUES ('cluster', $1, 'maint', NOW() - INTERVAL '1 hour', NOW() + INTERVAL '1 hour', 'tester')
	`, clusterID); err != nil {
		t.Fatalf("insert cluster blackout: %v", err)
	}
	active, err = ds.IsBlackoutActive(ctx, &connID, nil)
	if err != nil {
		t.Fatalf("IsBlackoutActive (cluster scope): %v", err)
	}
	if !active {
		t.Errorf("cluster scope should be active")
	}

	// Group scope.
	if _, err := pool.Exec(ctx, `DELETE FROM blackouts`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO blackouts (scope, group_id, reason, start_time, end_time, created_by)
		VALUES ('group', $1, 'maint', NOW() - INTERVAL '1 hour', NOW() + INTERVAL '1 hour', 'tester')
	`, groupID); err != nil {
		t.Fatalf("insert group blackout: %v", err)
	}
	active, err = ds.IsBlackoutActive(ctx, &connID, nil)
	if err != nil {
		t.Fatalf("IsBlackoutActive (group scope): %v", err)
	}
	if !active {
		t.Errorf("group scope should be active")
	}

	// Canceled context returns error.
	if _, err := pool.Exec(ctx, `DELETE FROM blackouts`); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ds.IsBlackoutActive(canceled, &connID, nil); err == nil {
		t.Errorf("canceled context should error")
	}
}

func TestDeleteOldAlertsAndCandidates(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "conn-old")

	old := time.Now().Add(-48 * time.Hour)
	cutoff := time.Now().Add(-24 * time.Hour)

	// Old cleared alert.
	if _, err := pool.Exec(ctx, `
		INSERT INTO alerts (alert_type, connection_id, severity, title, description, status, cleared_at)
		VALUES ('threshold', $1, 'warning', 'old', 'old', 'cleared', $2)
	`, connID, old); err != nil {
		t.Fatal(err)
	}
	// Recent alert that must not be deleted.
	if _, err := pool.Exec(ctx, `
		INSERT INTO alerts (alert_type, connection_id, severity, title, description, status, cleared_at)
		VALUES ('threshold', $1, 'warning', 'recent', 'recent', 'cleared', NOW())
	`, connID); err != nil {
		t.Fatal(err)
	}

	deleted, err := ds.DeleteOldAlerts(ctx, cutoff)
	if err != nil {
		t.Fatalf("DeleteOldAlerts: %v", err)
	}
	if deleted != 1 {
		t.Errorf("DeleteOldAlerts = %d, want 1", deleted)
	}

	// Anomaly candidates.
	if _, err := pool.Exec(ctx, `
		INSERT INTO anomaly_candidates (connection_id, metric_name, metric_value, z_score, processed_at)
		VALUES ($1, 'm', 1.0, 5.0, $2)
	`, connID, old); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO anomaly_candidates (connection_id, metric_name, metric_value, z_score, processed_at)
		VALUES ($1, 'm', 2.0, 6.0, NOW())
	`, connID); err != nil {
		t.Fatal(err)
	}
	deleted, err = ds.DeleteOldAnomalyCandidates(ctx, cutoff)
	if err != nil {
		t.Fatalf("DeleteOldAnomalyCandidates: %v", err)
	}
	if deleted != 1 {
		t.Errorf("DeleteOldAnomalyCandidates = %d, want 1", deleted)
	}
}

func TestGetProbeAvailability(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "probe-conn")
	if _, err := pool.Exec(ctx, `
		INSERT INTO probe_availability (connection_id, database_name, probe_name, is_available)
		VALUES ($1, '', 'probe_x', TRUE)
	`, connID); err != nil {
		t.Fatal(err)
	}
	pa, err := ds.GetProbeAvailability(ctx, connID, "probe_x")
	if err != nil {
		t.Fatalf("GetProbeAvailability: %v", err)
	}
	if pa.ProbeName != "probe_x" || !pa.IsAvailable {
		t.Errorf("got %+v", pa)
	}

	// Missing returns ErrNoRows.
	if _, err := ds.GetProbeAvailability(ctx, connID, "missing_probe"); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("expected ErrNoRows, got %v", err)
	}
}

func TestGetEnabledBlackoutSchedules(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "bs-conn")
	if _, err := pool.Exec(ctx, `
		INSERT INTO blackout_schedules (
			scope, connection_id, name, cron_expression, duration_minutes,
			timezone, reason, enabled, created_by
		) VALUES ('server', $1, 'sched1', '0 0 * * *', 60, 'UTC', 'maint', TRUE, 'tester')
	`, connID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO blackout_schedules (
			scope, connection_id, name, cron_expression, duration_minutes,
			timezone, reason, enabled, created_by
		) VALUES ('server', $1, 'sched2-disabled', '0 1 * * *', 60, 'UTC', 'maint', FALSE, 'tester')
	`, connID); err != nil {
		t.Fatal(err)
	}
	got, err := ds.GetEnabledBlackoutSchedules(ctx)
	if err != nil {
		t.Fatalf("GetEnabledBlackoutSchedules: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 enabled schedule, got %d", len(got))
	}
}

func TestCreateBlackout(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "bk-conn")
	bk := &Blackout{
		Scope:        "server",
		ConnectionID: &connID,
		Reason:       "test",
		StartTime:    time.Now(),
		EndTime:      time.Now().Add(1 * time.Hour),
		CreatedBy:    "tester",
		CreatedAt:    time.Now(),
	}
	if err := ds.CreateBlackout(ctx, bk); err != nil {
		t.Fatalf("CreateBlackout: %v", err)
	}
	if bk.ID == 0 {
		t.Errorf("expected ID to be set")
	}
}

func TestGetActiveConnections(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	insertTestConnection(t, pool, "active-1")
	insertTestConnection(t, pool, "active-2")
	if _, err := pool.Exec(ctx,
		`INSERT INTO connections (name, enabled) VALUES ('disabled-1', FALSE)`); err != nil {
		t.Fatal(err)
	}
	ids, err := ds.GetActiveConnections(ctx)
	if err != nil {
		t.Fatalf("GetActiveConnections: %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("expected 2 active connections, got %d", len(ids))
	}
}

func TestGetProbeStalenessByConnection(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "stale-conn")
	if _, err := pool.Exec(ctx, `
		INSERT INTO probe_configs (name, collection_interval_seconds, is_enabled, connection_id)
		VALUES ('probe_x', 60, TRUE, NULL)
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO probe_availability (connection_id, probe_name, is_available, last_collected)
		VALUES ($1, 'probe_x', TRUE, NOW() - INTERVAL '120 seconds')
	`, connID); err != nil {
		t.Fatal(err)
	}
	results, err := ds.GetProbeStalenessByConnection(ctx)
	if err != nil {
		t.Fatalf("GetProbeStalenessByConnection: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].StalenessRatio < 1.5 {
		t.Errorf("ratio = %v, want >= 1.5", results[0].StalenessRatio)
	}
}

func TestGetAlertRuleByName(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	insertTestRule(t, pool, "lookup_me", "metric_lookup", ">", 1, "info", true)

	rule, err := ds.GetAlertRuleByName(ctx, "lookup_me")
	if err != nil {
		t.Fatalf("GetAlertRuleByName: %v", err)
	}
	if rule.MetricName != "metric_lookup" {
		t.Errorf("got %q", rule.MetricName)
	}

	if _, err := ds.GetAlertRuleByName(ctx, "missing_rule"); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("expected ErrNoRows, got %v", err)
	}
}

func TestGetClusterPeers(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	_, clusterID := insertGroupAndCluster(t, pool, "gp", "cp")
	primaryID := insertTestConnectionInCluster(t, pool, "primary", clusterID)
	replicaID := insertTestConnectionInCluster(t, pool, "replica", clusterID)

	if _, err := pool.Exec(ctx, `
		INSERT INTO metrics.pg_node_role (connection_id, primary_role)
		VALUES ($1, 'replica')
	`, replicaID); err != nil {
		t.Fatal(err)
	}

	peers, err := ds.GetClusterPeers(ctx, primaryID)
	if err != nil {
		t.Fatalf("GetClusterPeers: %v", err)
	}
	if len(peers) != 1 || peers[0].ConnectionID != replicaID {
		t.Errorf("got peers=%+v", peers)
	}
	if peers[0].NodeRole != "replica" {
		t.Errorf("role = %q, want replica", peers[0].NodeRole)
	}

	// Connection with no cluster: empty result.
	soloID := insertTestConnection(t, pool, "solo")
	peers, err = ds.GetClusterPeers(ctx, soloID)
	if err != nil {
		t.Fatalf("GetClusterPeers solo: %v", err)
	}
	if len(peers) != 0 {
		t.Errorf("expected 0 peers, got %d", len(peers))
	}
}
