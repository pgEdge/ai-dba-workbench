/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package notifications

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/alerter/internal/database"
	"github.com/pgedge/ai-workbench/alerter/internal/testdsn"
)

// managerTestSchemaName isolates the tables these tests create from the
// public tables other packages' integration tests create.
const managerTestSchemaName = "notifications_manager_test"

// managerTestSchemaSQL creates the tables the Manager's datastore calls
// read and write, with the alerts table in the shape the collector's v19
// migration gives it, so that it holds system alerts. Some foreign keys
// are left out so that a test can point a row at something missing and
// drive the Manager's error handling.
const managerTestSchemaSQL = `
DROP SCHEMA IF EXISTS notifications_manager_test CASCADE;
CREATE SCHEMA notifications_manager_test;

CREATE TABLE cluster_groups (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL UNIQUE
);

CREATE TABLE clusters (
    id SERIAL PRIMARY KEY,
    group_id INTEGER REFERENCES cluster_groups(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL
);

CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    host VARCHAR(255) NOT NULL DEFAULT 'localhost',
    port INTEGER NOT NULL DEFAULT 5432,
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE SET NULL
);

CREATE TABLE alerts (
    id BIGSERIAL PRIMARY KEY,
    alert_type TEXT NOT NULL
        CHECK (alert_type IN ('threshold', 'anomaly', 'connection', 'system')),
    rule_id BIGINT,
    connection_id INTEGER,
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
    status TEXT NOT NULL,
    triggered_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    cleared_at TIMESTAMPTZ,
    last_updated TIMESTAMPTZ,
    anomaly_score REAL,
    anomaly_details JSONB,
    CHECK ((alert_type = 'system') = (connection_id IS NULL))
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
    telegram_bot_token_encrypted TEXT,
    telegram_chat_id TEXT,
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

CREATE TABLE notification_channel_overrides (
    id BIGSERIAL PRIMARY KEY,
    channel_id BIGINT NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
    scope TEXT NOT NULL,
    connection_id INTEGER REFERENCES connections(id) ON DELETE CASCADE,
    group_id INTEGER REFERENCES cluster_groups(id) ON DELETE CASCADE,
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE notification_history (
    id BIGSERIAL PRIMARY KEY,
    alert_id BIGINT,
    channel_id BIGINT,
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
`

const managerTestTeardownSQL = `DROP SCHEMA IF EXISTS notifications_manager_test CASCADE`

const managerInsertChannelSQL = `
INSERT INTO notification_channels (channel_type, name, is_estate_default,
                                   reminder_enabled, reminder_interval_hours)
VALUES ($1, $2, TRUE, TRUE, 1)
RETURNING id`

const managerInsertConnectionSQL = `
INSERT INTO connections (name, host, port) VALUES ('mgr-conn', 'db.example.com', 6543)
RETURNING id`

const managerInsertAlertSQL = `
INSERT INTO alerts (alert_type, connection_id, metric_name, severity, title,
                    description, status, triggered_at)
VALUES ($1, $2, 'llm_provider_health.tier2.openai', 'warning', 'Provider failing',
        'details', 'active', NOW() - INTERVAL '2 hours')
RETURNING id`

const managerHistorySQL = `
SELECT status, connection_id, attempt_count
FROM notification_history WHERE alert_id = $1 AND channel_id IS NOT NULL
ORDER BY id DESC LIMIT 1`

const managerRetryNowSQL = `UPDATE notification_history SET next_retry_at = NOW() - INTERVAL '1 minute'`

const managerOrphanHistorySQL = `
INSERT INTO notification_history (alert_id, channel_id, notification_type, status)
VALUES ($1, NULL, 'alert_fire', 'pending'), (NULL, $2, 'alert_fire', 'pending')`

const managerMissingTargetsSQL = `
INSERT INTO notification_history (alert_id, channel_id, notification_type, status)
VALUES (888888, $1, 'alert_fire', 'pending'), ($2, 888888, 'alert_fire', 'pending')`

const managerPendingForAlertSQL = `
INSERT INTO notification_history (alert_id, channel_id, notification_type, status)
VALUES ($1, $2, 'alert_fire', 'pending')`

const managerReminderCountSQL = `SELECT reminder_count FROM notification_reminder_state WHERE alert_id = $1`

const managerDisableChannelsSQL = `UPDATE notification_channels SET enabled = FALSE`

// recordingNotifier records every payload it is asked to send and fails
// while err is set.
type recordingNotifier struct {
	mu       sync.Mutex
	err      error
	payloads []*database.NotificationPayload
}

func (r *recordingNotifier) Send(_ context.Context, _ *database.NotificationChannel, p *database.NotificationPayload) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.payloads = append(r.payloads, p)
	return r.err
}

func (r *recordingNotifier) Type() database.NotificationChannelType {
	return database.ChannelTypeSlack
}

func (r *recordingNotifier) Validate(*database.NotificationChannel) error { return nil }

func (r *recordingNotifier) last(t *testing.T) *database.NotificationPayload {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.payloads) == 0 {
		t.Fatal("nothing was sent")
	}
	return r.payloads[len(r.payloads)-1]
}

// managerTestEnv builds a Manager on a real datastore whose only notifier
// is a recordingNotifier for Slack channels.
func managerTestEnv(t *testing.T) (*Manager, *recordingNotifier, *pgxpool.Pool) {
	t.Helper()
	dsn := testdsn.Require(t, "the notification manager integration test")

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = managerTestSchemaName
	cfg.MaxConns = 2
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("Test database ping failed: %v", err)
	}
	if _, err := pool.Exec(context.Background(), managerTestSchemaSQL); err != nil {
		pool.Close()
		t.Fatalf("create manager test schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), managerTestTeardownSQL); err != nil {
			t.Logf("manager test teardown: %v", err)
		}
		pool.Close()
	})

	notifier := &recordingNotifier{}
	m := &Manager{
		datastore: database.NewTestDatastore(pool),
		config:    &config.NotificationsConfig{MaxRetryAttempts: 2},
		notifiers: map[database.NotificationChannelType]Notifier{
			database.ChannelTypeSlack: notifier,
			database.ChannelTypeEmail: notifier,
		},
		log: func(string, ...any) {},
	}
	return m, notifier, pool
}

func insertManagerAlert(t *testing.T, pool *pgxpool.Pool, alertType string, connID any) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(), managerInsertAlertSQL, alertType, connID).Scan(&id); err != nil {
		t.Fatalf("insert alert: %v", err)
	}
	return id
}

func historyRow(t *testing.T, pool *pgxpool.Pool, alertID int64) (status string, connID *int, attempts int) {
	t.Helper()
	if err := pool.QueryRow(context.Background(), managerHistorySQL, alertID).Scan(&status, &connID, &attempts); err != nil {
		t.Fatalf("read history: %v", err)
	}
	return status, connID, attempts
}

// TestManager_SystemAlertNotificationLifecycle sends, retries and
// reminds for a system alert, which has no connection, through the
// estate default channel.
func TestManager_SystemAlertNotificationLifecycle(t *testing.T) {
	m, notifier, pool := managerTestEnv(t)
	ctx := context.Background()

	orig := hostname
	hostname = func() (string, error) { return "alerter-host", nil }
	t.Cleanup(func() { hostname = orig })

	var channelID int64
	if err := pool.QueryRow(ctx, managerInsertChannelSQL, "slack", "estate").Scan(&channelID); err != nil {
		t.Fatal(err)
	}
	alertID := insertManagerAlert(t, pool, database.AlertTypeSystem, nil)
	alert, err := m.datastore.GetAlert(ctx, alertID)
	if err != nil {
		t.Fatal(err)
	}

	// The first send fails, so the history row waits for a retry.
	notifier.err = errors.New("slack unavailable")
	if err := m.SendAlertNotification(ctx, alert, database.NotificationTypeAlertFire); err != nil {
		t.Fatalf("SendAlertNotification: %v", err)
	}
	p := notifier.last(t)
	if p.ServerName != SystemAlertServerName || p.ServerHost != "alerter-host" || p.ServerPort != 0 {
		t.Errorf("payload server = %q %q %d", p.ServerName, p.ServerHost, p.ServerPort)
	}
	status, connID, _ := historyRow(t, pool, alertID)
	if status != string(database.NotificationStatusRetrying) || connID != nil {
		t.Fatalf("history after failed send = %s, connection %v", status, connID)
	}

	// Rows with no channel or no alert are passed over.
	if _, err := pool.Exec(ctx, managerOrphanHistorySQL, alertID, channelID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, managerRetryNowSQL); err != nil {
		t.Fatal(err)
	}
	notifier.err = nil
	if err := m.ProcessPendingNotifications(ctx); err != nil {
		t.Fatalf("ProcessPendingNotifications: %v", err)
	}
	if status, _, attempts := historyRow(t, pool, alertID); status != string(database.NotificationStatusSent) || attempts != 2 {
		t.Errorf("history after retry = %s after %d attempts", status, attempts)
	}

	if err := m.ProcessReminders(ctx); err != nil {
		t.Fatalf("ProcessReminders: %v", err)
	}
	if p := notifier.last(t); p.ReminderCount != 1 || p.ServerName != SystemAlertServerName {
		t.Errorf("reminder payload = %+v", p)
	}
	var count int
	if err := pool.QueryRow(ctx, managerReminderCountSQL, alertID).Scan(&count); err != nil || count != 1 {
		t.Errorf("reminder count = %d, %v", count, err)
	}
}

// TestManager_ConnectionAlertNotification checks that an alert on a
// connection still names that connection and records it in the history.
func TestManager_ConnectionAlertNotification(t *testing.T) {
	m, notifier, pool := managerTestEnv(t)
	ctx := context.Background()

	var channelID, connID int64
	if err := pool.QueryRow(ctx, managerInsertChannelSQL, "slack", "estate").Scan(&channelID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, managerInsertConnectionSQL).Scan(&connID); err != nil {
		t.Fatal(err)
	}
	alertID := insertManagerAlert(t, pool, "threshold", connID)
	alert, err := m.datastore.GetAlert(ctx, alertID)
	if err != nil {
		t.Fatal(err)
	}

	if err := m.SendAlertNotification(ctx, alert, database.NotificationTypeAlertFire); err != nil {
		t.Fatal(err)
	}
	if p := notifier.last(t); p.ServerName != "mgr-conn" || p.ServerPort != 6543 {
		t.Errorf("payload server = %q %d", p.ServerName, p.ServerPort)
	}
	status, histConn, _ := historyRow(t, pool, alertID)
	if status != string(database.NotificationStatusSent) || histConn == nil || int64(*histConn) != connID {
		t.Errorf("history = %s, connection %v", status, histConn)
	}

	// An alert whose connection cannot be read falls back to a
	// placeholder name; its history row cannot be written either, so
	// nothing is sent.
	sent := len(notifier.payloads)
	ghost := &database.Alert{ID: 999999, AlertType: "threshold", ConnectionID: 999999}
	if err := m.SendAlertNotification(ctx, ghost, database.NotificationTypeAlertFire); err != nil {
		t.Fatal(err)
	}
	if len(notifier.payloads) != sent {
		t.Error("a notification was sent without a history row")
	}
}

// TestManager_NotificationFailurePaths covers the channel type with no
// notifier, a send that exhausts its attempts, and datastore failures.
func TestManager_NotificationFailurePaths(t *testing.T) {
	m, notifier, pool := managerTestEnv(t)
	ctx := context.Background()

	var channelID int64
	if err := pool.QueryRow(ctx, managerInsertChannelSQL, "webhook", "no notifier").Scan(&channelID); err != nil {
		t.Fatal(err)
	}
	alertID := insertManagerAlert(t, pool, database.AlertTypeSystem, nil)
	alert, err := m.datastore.GetAlert(ctx, alertID)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SendAlertNotification(ctx, alert, database.NotificationTypeAlertFire); err != nil {
		t.Fatal(err)
	}
	if status, _, _ := historyRow(t, pool, alertID); status != string(database.NotificationStatusFailed) {
		t.Errorf("history for an unknown channel type = %s, want failed", status)
	}
	// A due reminder through that channel is not recorded as sent.
	if err := m.ProcessReminders(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, managerReminderCountSQL, alertID).Scan(&count); err == nil {
		t.Errorf("reminder state recorded %d for a failed reminder", count)
	}

	// A send that fails on its last attempt is marked failed.
	if _, err := pool.Exec(ctx, managerDisableChannelsSQL); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, managerInsertChannelSQL, "slack", "flaky").Scan(&channelID); err != nil {
		t.Fatal(err)
	}
	m.config.MaxRetryAttempts = 1
	notifier.err = errors.New("down")
	if err := m.SendAlertNotification(ctx, alert, database.NotificationTypeAlertFire); err != nil {
		t.Fatal(err)
	}
	if status, _, _ := historyRow(t, pool, alertID); status != string(database.NotificationStatusFailed) {
		t.Errorf("history after the last attempt = %s, want failed", status)
	}

	// With every channel disabled there is nothing to send.
	if _, err := pool.Exec(ctx, managerDisableChannelsSQL); err != nil {
		t.Fatal(err)
	}
	sent := len(notifier.payloads)
	if err := m.SendAlertNotification(ctx, alert, database.NotificationTypeAlertFire); err != nil {
		t.Fatal(err)
	}
	if len(notifier.payloads) != sent {
		t.Error("sent with no enabled channel")
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := m.SendAlertNotification(canceled, alert, database.NotificationTypeAlertFire); err == nil {
		t.Error("SendAlertNotification ignored a datastore failure")
	}
	if err := m.ProcessPendingNotifications(canceled); err == nil {
		t.Error("ProcessPendingNotifications ignored a datastore failure")
	}
	if err := m.ProcessReminders(canceled); err == nil {
		t.Error("ProcessReminders ignored a datastore failure")
	}
}

// TestManager_MissingRowsAreSkipped points pending notifications and a
// due reminder at rows that do not exist, which the Manager logs and
// passes over, and at an email channel, whose recipients it reads.
func TestManager_MissingRowsAreSkipped(t *testing.T) {
	m, notifier, pool := managerTestEnv(t)
	ctx := context.Background()

	var channelID int64
	if err := pool.QueryRow(ctx, managerInsertChannelSQL, "email", "mail").Scan(&channelID); err != nil {
		t.Fatal(err)
	}
	// A threshold alert whose connection has gone.
	alertID := insertManagerAlert(t, pool, "threshold", 777)
	if _, err := pool.Exec(ctx, managerMissingTargetsSQL, channelID, alertID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, managerPendingForAlertSQL, alertID, channelID); err != nil {
		t.Fatal(err)
	}

	if err := m.ProcessPendingNotifications(ctx); err != nil {
		t.Fatalf("ProcessPendingNotifications: %v", err)
	}
	if len(notifier.payloads) != 1 {
		t.Fatalf("sent %d, want only the notification with an alert and channel", len(notifier.payloads))
	}
	if p := notifier.last(t); p.ServerName != "" {
		t.Errorf("server name for a missing connection = %q, want empty", p.ServerName)
	}

	// The reminder's history row cannot reference the missing
	// connection, so the reminder is not sent.
	if err := m.ProcessReminders(ctx); err != nil {
		t.Fatalf("ProcessReminders: %v", err)
	}
	if len(notifier.payloads) != 1 {
		t.Errorf("sent %d, want no reminder without a history row", len(notifier.payloads))
	}
}
