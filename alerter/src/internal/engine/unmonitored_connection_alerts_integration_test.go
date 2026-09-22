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
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// Seed and inspection statements for the unmonitored connection tests.
// They are named constants for the same reason as those in the staleness
// tests: the Codacy/Semgrep go_sql_rule-concat-sqli rule flags inline
// multi-line SQL, and every value here is still bound via $N.
const (
	unmonitoredAlertInsertSQL = `
        INSERT INTO alerts
            (alert_type, connection_id, metric_name, metric_value,
             threshold_value, operator, severity, title, description, status,
             triggered_at)
        VALUES ($1, $2, $3, 42, 10, '>', 'warning', $4, $5, 'active', NOW())
        RETURNING id
    `

	unmonitoredConnectionUpdateSQL = `
        UPDATE connections SET is_monitored = FALSE WHERE id = $1
    `

	unmonitoredAlertSelectSQL = `
        SELECT status, description, cleared_at
        FROM alerts
        WHERE id = $1
    `

	dropConnectionsTableSQL = `DROP TABLE connections CASCADE`
)

// unmonitoredAlertRow is the projection the assertions below read: the
// status says whether the alert was retired, cleared_at whether retention
// will ever reap it, and the description whether its history explains why
// it ended.
type unmonitoredAlertRow struct {
	status      string
	description string
	clearedAt   *time.Time
}

// insertUnmonitoredTestAlert inserts one active alert of the given type on
// a connection and returns its id.
func insertUnmonitoredTestAlert(t *testing.T, pool *pgxpool.Pool, connID int,
	alertType, title, description string) int64 {
	t.Helper()

	var id int64
	err := pool.QueryRow(context.Background(), unmonitoredAlertInsertSQL,
		alertType, connID, "connection_count", title, description).Scan(&id)
	if err != nil {
		t.Fatalf("failed to insert %s alert: %v", alertType, err)
	}
	return id
}

// readUnmonitoredAlert reads one alert's status, description and clear time.
func readUnmonitoredAlert(t *testing.T, pool *pgxpool.Pool, alertID int64) unmonitoredAlertRow {
	t.Helper()

	var got unmonitoredAlertRow
	if err := pool.QueryRow(context.Background(), unmonitoredAlertSelectSQL, alertID).
		Scan(&got.status, &got.description, &got.clearedAt); err != nil {
		t.Fatalf("failed to read alert %d: %v", alertID, err)
	}
	return got
}

// stopMonitoringConnection turns off monitoring for a connection, as an
// operator does from the servers page.
func stopMonitoringConnection(t *testing.T, pool *pgxpool.Pool, connID int) {
	t.Helper()

	tag, err := pool.Exec(context.Background(), unmonitoredConnectionUpdateSQL, connID)
	if err != nil {
		t.Fatalf("failed to stop monitoring connection %d: %v", connID, err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("stopping monitoring of connection %d affected %d rows, want 1",
			connID, tag.RowsAffected())
	}
}

// TestAlertsClearedWhenConnectionUnmonitored is the regression test for
// issue #500. An operator who stops monitoring a server must not be left
// with its alerts active for ever, and the alerts must say why they
// ended, of whatever type they are, without a burst of clear
// notifications announcing conditions nothing resolved.
func TestAlertsClearedWhenConnectionUnmonitored(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	capture := installStalenessNotificationCapture(t, engine)
	connID := insertTestConnection(t, pool, "unmonitored-clear")

	thresholdID := insertUnmonitoredTestAlert(t, pool, connID, "threshold",
		"too many connections", "Connections are above the threshold")
	anomalyID := insertUnmonitoredTestAlert(t, pool, connID, "anomaly",
		"unusual connection count", "The connection count is unusual")

	stopMonitoringConnection(t, pool, connID)

	engine.cleanResolvedAlerts(ctx)

	for _, id := range []int64{thresholdID, anomalyID} {
		got := readUnmonitoredAlert(t, pool, id)
		if got.status != "cleared" {
			t.Errorf("alert %d status = %q, want \"cleared\"; an unmonitored "+
				"connection must not leave its alerts active", id, got.status)
		}
		if got.clearedAt == nil {
			t.Errorf("alert %d cleared_at is NULL, want it set so retention reaps it", id)
		}
		if !strings.HasPrefix(got.description, unmonitoredClearDescriptionPrefix) {
			t.Errorf("alert %d description = %q, want it to open by saying monitoring stopped",
				id, got.description)
		}
		if !strings.Contains(got.description, "unmonitored-clear") {
			t.Errorf("alert %d description = %q, want it to name the server", id, got.description)
		}
	}

	// The threshold alert's original wording is kept after the new
	// opening, because the condition that raised it is still the most
	// useful thing in the record.
	got := readUnmonitoredAlert(t, pool, thresholdID)
	if !strings.Contains(got.description, "Connections are above the threshold") {
		t.Errorf("alert description = %q, want it to keep the original wording",
			got.description)
	}

	if counts := capture.drain(t); counts[database.NotificationTypeAlertClear] != 0 {
		t.Errorf("clear notifications = %d, want 0; nothing resolved these conditions",
			counts[database.NotificationTypeAlertClear])
	}
}

// TestAlertsUntouchedWhileConnectionMonitored confirms the check is
// confined to the connections an operator has actually stopped
// monitoring: everything else must reach the ordinary resolution path
// unchanged.
func TestAlertsUntouchedWhileConnectionMonitored(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	capture := installStalenessNotificationCapture(t, engine)
	monitoredID := insertTestConnection(t, pool, "still-monitored")
	unmonitoredID := insertTestConnection(t, pool, "no-longer-monitored")

	keptID := insertUnmonitoredTestAlert(t, pool, monitoredID, "connection",
		"server unreachable", "The server cannot be reached")
	clearedID := insertUnmonitoredTestAlert(t, pool, unmonitoredID, "connection",
		"server unreachable", "The server cannot be reached")

	stopMonitoringConnection(t, pool, unmonitoredID)

	engine.cleanResolvedAlerts(ctx)

	kept := readUnmonitoredAlert(t, pool, keptID)
	if kept.status != "active" {
		t.Errorf("alert on the monitored connection has status %q, want \"active\"",
			kept.status)
	}
	if kept.description != "The server cannot be reached" {
		t.Errorf("alert on the monitored connection has description %q, want it unchanged",
			kept.description)
	}

	if cleared := readUnmonitoredAlert(t, pool, clearedID); cleared.status != "cleared" {
		t.Errorf("alert on the unmonitored connection has status %q, want \"cleared\"",
			cleared.status)
	}

	if counts := capture.drain(t); counts[database.NotificationTypeAlertClear] != 0 {
		t.Errorf("clear notifications = %d, want 0", counts[database.NotificationTypeAlertClear])
	}
}

// TestAlertsStayActiveWhenMonitoredSetUnreadable covers the failure path.
// A datastore that cannot answer which connections are monitored says
// nothing about whether an operator turned any of them off, so every
// alert must be left exactly as it was rather than cleared on a guess.
func TestAlertsStayActiveWhenMonitoredSetUnreadable(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	capture := installStalenessNotificationCapture(t, engine)
	connID := insertTestConnection(t, pool, "unreadable-monitored-set")
	alertID := insertUnmonitoredTestAlert(t, pool, connID, "connection",
		"server unreachable", "The server cannot be reached")

	// Dropping the connections table takes its foreign key with it and
	// leaves the alert rows in place, so the read of the monitored set
	// fails whilst the rest of the pass still runs.
	if _, err := pool.Exec(ctx, dropConnectionsTableSQL); err != nil {
		t.Fatalf("failed to drop the connections table: %v", err)
	}

	engine.cleanResolvedAlerts(ctx)

	got := readUnmonitoredAlert(t, pool, alertID)
	if got.status != "active" {
		t.Errorf("alert status = %q, want \"active\"; a failed read must not clear alerts",
			got.status)
	}
	if got.description != "The server cannot be reached" {
		t.Errorf("alert description = %q, want it unchanged", got.description)
	}
	if counts := capture.drain(t); counts[database.NotificationTypeAlertClear] != 0 {
		t.Errorf("clear notifications = %d, want 0", counts[database.NotificationTypeAlertClear])
	}
}

// TestUnmonitoredSnapshotReadsOnce confirms the pass resolves the
// unmonitored set once and reuses it, rather than issuing a query per
// alert, and that a nil snapshot is tolerated by the helper the way the
// staleness snapshot is.
func TestUnmonitoredSnapshotReadsOnce(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	installStalenessNotificationCapture(t, engine)
	connID := insertTestConnection(t, pool, "snapshot-reuse")
	stopMonitoringConnection(t, pool, connID)

	snapshot := &unmonitoredConnectionSnapshot{}
	first, err := snapshot.get(ctx, engine)
	if err != nil {
		t.Fatalf("reading the unmonitored set: %v", err)
	}
	if _, found := first[connID]; !found {
		t.Fatalf("connection %d missing from the unmonitored set %v", connID, first)
	}

	// A second connection stopped after the snapshot was taken must not
	// appear in it, which is what proves the read happened once.
	other := insertTestConnection(t, pool, "snapshot-reuse-second")
	stopMonitoringConnection(t, pool, other)

	second, err := snapshot.get(ctx, engine)
	if err != nil {
		t.Fatalf("re-reading the unmonitored set: %v", err)
	}
	if _, found := second[other]; found {
		t.Errorf("connection %d appeared in a snapshot taken before it was "+
			"unmonitored; the set is being re-read per alert", other)
	}

	// A caller outside a pass may pass no snapshot at all.
	alert := &database.Alert{ID: 1, ConnectionID: connID, Description: "d"}
	if cleared := engine.clearAlertForUnmonitoredConnection(ctx, alert, nil); !cleared {
		t.Error("clearAlertForUnmonitoredConnection with a nil snapshot = false, want true")
	}
}

// TestUnmonitoredClearDescription checks the wording in isolation, since
// it is what an operator reads in the alert history months later.
func TestUnmonitoredClearDescription(t *testing.T) {
	got := unmonitoredClearDescription("db-1", "Disk usage is above 90%")

	if !strings.HasPrefix(got, unmonitoredClearDescriptionPrefix) {
		t.Errorf("description = %q, want it to open with %q", got,
			unmonitoredClearDescriptionPrefix)
	}
	for _, want := range []string{"db-1", "Disk usage is above 90%", "without the condition"} {
		if !strings.Contains(got, want) {
			t.Errorf("description = %q, want it to contain %q", got, want)
		}
	}
}

// Triggers that reject one kind of write to the alerts table each, so the
// two failure branches of the unmonitored clear can be driven apart: a
// description that cannot be written must not stop the alert being
// closed, and a clear that cannot be written must leave the alert as it
// was.
const (
	createRejectDescriptionUpdatesFuncSQL = `
        CREATE OR REPLACE FUNCTION reject_description_updates() RETURNS trigger AS $$
        BEGIN
            IF NEW.description IS DISTINCT FROM OLD.description THEN
                RAISE EXCEPTION 'description updates are rejected by this test';
            END IF;
            RETURN NEW;
        END;
        $$ LANGUAGE plpgsql
    `

	createRejectDescriptionUpdatesTriggerSQL = `
        CREATE TRIGGER reject_description_updates
            BEFORE UPDATE ON alerts
            FOR EACH ROW EXECUTE FUNCTION reject_description_updates()
    `

	dropRejectDescriptionUpdatesSQL = `
        DROP FUNCTION IF EXISTS reject_description_updates() CASCADE
    `

	createRejectStatusUpdatesFuncSQL = `
        CREATE OR REPLACE FUNCTION reject_status_updates() RETURNS trigger AS $$
        BEGIN
            IF NEW.status IS DISTINCT FROM OLD.status THEN
                RAISE EXCEPTION 'status updates are rejected by this test';
            END IF;
            RETURN NEW;
        END;
        $$ LANGUAGE plpgsql
    `

	createRejectStatusUpdatesTriggerSQL = `
        CREATE TRIGGER reject_status_updates
            BEFORE UPDATE ON alerts
            FOR EACH ROW EXECUTE FUNCTION reject_status_updates()
    `

	dropRejectStatusUpdatesSQL = `
        DROP FUNCTION IF EXISTS reject_status_updates() CASCADE
    `
)

// installRejectingTrigger creates one of the trigger functions above and
// its trigger, removing both when the test ends.
func installRejectingTrigger(t *testing.T, pool *pgxpool.Pool, function, trigger, drop string) {
	t.Helper()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, function); err != nil {
		t.Fatalf("failed to create the rejecting trigger function: %v", err)
	}
	if _, err := pool.Exec(ctx, trigger); err != nil {
		t.Fatalf("failed to install the rejecting trigger: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), drop); err != nil {
			t.Logf("failed to drop the rejecting trigger: %v", err)
		}
	})
}

// TestAlertClearedWhenDescriptionWriteFails covers the branch where the
// explanation cannot be recorded. An alert stuck active for ever is the
// fault issue #500 is about, and a description that still reads as it did
// when the alert was raised is a far smaller one, so the alert clears
// regardless.
func TestAlertClearedWhenDescriptionWriteFails(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	capture := installStalenessNotificationCapture(t, engine)
	connID := insertTestConnection(t, pool, "description-write-fails")
	alertID := insertUnmonitoredTestAlert(t, pool, connID, "connection",
		"server unreachable", "The server cannot be reached")

	stopMonitoringConnection(t, pool, connID)
	installRejectingTrigger(t, pool, createRejectDescriptionUpdatesFuncSQL,
		createRejectDescriptionUpdatesTriggerSQL, dropRejectDescriptionUpdatesSQL)

	engine.cleanResolvedAlerts(ctx)

	got := readUnmonitoredAlert(t, pool, alertID)
	if got.status != "cleared" {
		t.Errorf("alert status = %q, want \"cleared\" even when the description "+
			"cannot be written", got.status)
	}
	if got.description != "The server cannot be reached" {
		t.Errorf("alert description = %q, want the original wording", got.description)
	}
	if counts := capture.drain(t); counts[database.NotificationTypeAlertClear] != 0 {
		t.Errorf("clear notifications = %d, want 0", counts[database.NotificationTypeAlertClear])
	}
}

// TestAlertStaysActiveWhenClearWriteFails covers the other failure, where
// the clear itself cannot be written. The alert must be left active for
// the next pass to retry, and that next pass must not nest one
// explanation inside another in the description.
func TestAlertStaysActiveWhenClearWriteFails(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	capture := installStalenessNotificationCapture(t, engine)
	connID := insertTestConnection(t, pool, "clear-write-fails")
	alertID := insertUnmonitoredTestAlert(t, pool, connID, "connection",
		"server unreachable", "The server cannot be reached")

	stopMonitoringConnection(t, pool, connID)
	installRejectingTrigger(t, pool, createRejectStatusUpdatesFuncSQL,
		createRejectStatusUpdatesTriggerSQL, dropRejectStatusUpdatesSQL)

	engine.cleanResolvedAlerts(ctx)

	first := readUnmonitoredAlert(t, pool, alertID)
	if first.status != "active" {
		t.Errorf("alert status = %q, want \"active\" when the clear cannot be written",
			first.status)
	}
	if !strings.HasPrefix(first.description, unmonitoredClearDescriptionPrefix) {
		t.Fatalf("alert description = %q, want the explanation written", first.description)
	}

	engine.cleanResolvedAlerts(ctx)

	second := readUnmonitoredAlert(t, pool, alertID)
	if second.description != first.description {
		t.Errorf("description on the second pass = %q, want it unchanged at %q; a "+
			"retried clear must not nest one explanation inside the next",
			second.description, first.description)
	}
	if counts := capture.drain(t); counts[database.NotificationTypeAlertClear] != 0 {
		t.Errorf("clear notifications = %d, want 0", counts[database.NotificationTypeAlertClear])
	}
}

// TestUnmonitoredClearFailureStillClaimsAlert pins the contract that
// keeps a failed clear from falling through to the ordinary resolution
// path. clearAlertForUnmonitoredConnection reports true for an alert on
// an unmonitored connection whether or not the clear could be written,
// because cleanResolvedAlerts skips checkAlertResolved on a true verdict
// alone: were it to report false here, a threshold alert whose condition
// looks resolved would be cleared by that path in the same pass and
// would queue the clear notification this one exists to suppress.
func TestUnmonitoredClearFailureStillClaimsAlert(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	capture := installStalenessNotificationCapture(t, engine)
	connID := insertTestConnection(t, pool, "clear-failure-claims")
	alertID := insertUnmonitoredTestAlert(t, pool, connID, "threshold",
		"connection count high", "Connections above the threshold")

	stopMonitoringConnection(t, pool, connID)
	installRejectingTrigger(t, pool, createRejectStatusUpdatesFuncSQL,
		createRejectStatusUpdatesTriggerSQL, dropRejectStatusUpdatesSQL)

	alert := &database.Alert{
		ID:           alertID,
		ConnectionID: connID,
		AlertType:    "threshold",
		Description:  "Connections above the threshold",
	}

	if !engine.clearAlertForUnmonitoredConnection(ctx, alert,
		&unmonitoredConnectionSnapshot{}) {
		t.Error("clearAlertForUnmonitoredConnection = false after a failed clear, " +
			"want true so that the resolution path does not clear and notify")
	}
	if counts := capture.drain(t); counts[database.NotificationTypeAlertClear] != 0 {
		t.Errorf("clear notifications = %d, want 0", counts[database.NotificationTypeAlertClear])
	}
}
