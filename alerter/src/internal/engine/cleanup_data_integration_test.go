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
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// These tests cover the retention half of cleanup.go: cleanupOldData and
// the failure branches of cleanResolvedAlerts and clearResolvedAlert. The
// engine logs and continues on every failure here, because a retention
// sweep that cannot run is not a reason to stop the alerter, so each test
// asserts the surviving state rather than a returned error.

const (
	// createAnomalyCandidatesTableSQL carries the two columns
	// DeleteOldAnomalyCandidates reads. The Spock test schema does not
	// create the table, so the tests that exercise the candidate sweep
	// create it themselves.
	createAnomalyCandidatesTableSQL = `
        CREATE TABLE anomaly_candidates (
            id BIGSERIAL PRIMARY KEY,
            connection_id INTEGER NOT NULL,
            metric_name TEXT NOT NULL,
            metric_value REAL,
            processed_at TIMESTAMPTZ
        )
    `

	insertProcessedCandidateSQL = `
        INSERT INTO anomaly_candidates
            (connection_id, metric_name, metric_value, processed_at)
        VALUES ($1, 'pg_stat_activity.count', 1, $2)
    `

	insertClearedAlertSQL = `
        INSERT INTO alerts
            (alert_type, connection_id, metric_name, metric_value,
             threshold_value, operator, severity, title, description,
             status, cleared_at)
        VALUES ('threshold', $1, 'pg_stat_activity.count', 1, 1, '>',
                'warning', 'aged alert', 'aged alert', 'cleared', $2)
        RETURNING id
    `

	insertActiveAnomalyAlertSQL = `
        INSERT INTO alerts
            (alert_type, connection_id, metric_name, metric_value,
             threshold_value, operator, severity, title, description, status)
        VALUES ('anomaly', $1, 'pg_stat_activity.count', 5, 1, '>',
                'warning', 'anomaly alert', 'anomaly alert', 'active')
        RETURNING id
    `

	countAlertsSQL      = `SELECT COUNT(*) FROM alerts`
	countCandidatesSQL  = `SELECT COUNT(*) FROM anomaly_candidates`
	setRetentionDaysSQL = `UPDATE alerter_settings SET retention_days = $1 WHERE id = 1`
)

// countRows runs a COUNT(*) statement and returns the result.
func countRows(t *testing.T, pool *pgxpool.Pool, sql string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql).Scan(&n); err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	return n
}

// TestCleanupOldData_DeletesAgedRows seeds one cleared alert and one
// processed anomaly candidate either side of the retention cutoff and
// asserts that the sweep removes only the aged rows.
func TestCleanupOldData_DeletesAgedRows(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, createAnomalyCandidatesTableSQL); err != nil {
		t.Fatalf("failed to create anomaly_candidates: %v", err)
	}
	if _, err := pool.Exec(ctx, setRetentionDaysSQL, 30); err != nil {
		t.Fatalf("failed to set retention_days: %v", err)
	}
	connID := insertTestConnection(t, pool, "cleanup-retention")

	old := time.Now().UTC().AddDate(0, 0, -60)
	recent := time.Now().UTC().AddDate(0, 0, -1)
	for _, ts := range []time.Time{old, recent} {
		var id int64
		if err := pool.QueryRow(ctx, insertClearedAlertSQL, connID, ts).Scan(&id); err != nil {
			t.Fatalf("failed to insert cleared alert: %v", err)
		}
		if _, err := pool.Exec(ctx, insertProcessedCandidateSQL, connID, ts); err != nil {
			t.Fatalf("failed to insert anomaly candidate: %v", err)
		}
	}

	engine.cleanupOldData(ctx)

	if got := countRows(t, pool, countAlertsSQL); got != 1 {
		t.Errorf("alerts remaining = %d, want 1 (only the recent one)", got)
	}
	if got := countRows(t, pool, countCandidatesSQL); got != 1 {
		t.Errorf("anomaly candidates remaining = %d, want 1 (only the recent one)", got)
	}
}

// TestCleanupOldData_SettingsFailureAborts drives the settings-read
// failure: with alerter_settings gone there is no retention period to
// work from, so the sweep must return without deleting anything.
func TestCleanupOldData_SettingsFailureAborts(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "cleanup-no-settings")
	var id int64
	if err := pool.QueryRow(ctx, insertClearedAlertSQL, connID,
		time.Now().UTC().AddDate(0, 0, -60)).Scan(&id); err != nil {
		t.Fatalf("failed to insert cleared alert: %v", err)
	}
	if _, err := pool.Exec(ctx, `DROP TABLE alerter_settings`); err != nil {
		t.Fatalf("failed to drop alerter_settings: %v", err)
	}

	engine.cleanupOldData(ctx)

	if got := countRows(t, pool, countAlertsSQL); got != 1 {
		t.Errorf("alerts remaining = %d, want 1 (nothing deleted)", got)
	}
}

// TestCleanupOldData_DeleteFailuresAreLogged drives both delete
// failures at once: anomaly_candidates never exists in this schema and
// alerts is dropped, so each delete fails and the sweep still returns.
func TestCleanupOldData_DeleteFailuresAreLogged(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP TABLE alerts CASCADE`); err != nil {
		t.Fatalf("failed to drop alerts: %v", err)
	}

	// No panic and no hang is the assertion; the errors are logged.
	engine.cleanupOldData(ctx)
}

// TestCleanResolvedAlerts_LookupFailureReturns drives the
// GetActiveAlerts failure branch with an already-canceled context.
func TestCleanResolvedAlerts_LookupFailureReturns(t *testing.T) {
	engine, _, _, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	engine.cleanResolvedAlerts(ctx)
}

// TestCleanResolvedAlerts_SkipsNonThresholdAlerts asserts that an
// anomaly alert is left alone by the threshold cleaner: anomaly alerts
// are cleared by the re-evaluation path, not by a metric comparison.
func TestCleanResolvedAlerts_SkipsNonThresholdAlerts(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "cleanup-anomaly-alert")
	var alertID int64
	if err := pool.QueryRow(ctx, insertActiveAnomalyAlertSQL, connID).Scan(&alertID); err != nil {
		t.Fatalf("failed to insert anomaly alert: %v", err)
	}

	engine.cleanResolvedAlerts(ctx)

	if status := alertStatus(t, pool, alertID); status != "active" {
		t.Errorf("anomaly alert status = %q, want \"active\"", status)
	}
}

// TestClearResolvedAlert_WriteFailureKeepsAlert drives the ClearAlert
// failure branch: the alert stays as it is and no clear notification is
// queued, so the next cleaner pass can try again.
func TestClearResolvedAlert_WriteFailureKeepsAlert(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	capture := installNotificationCapture(t, engine)
	connID := insertTestConnection(t, pool, "cleanup-clear-fails")
	var alertID int64
	if err := pool.QueryRow(ctx, insertActiveAnomalyAlertSQL, connID).Scan(&alertID); err != nil {
		t.Fatalf("failed to insert alert: %v", err)
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	engine.clearResolvedAlert(canceled, &database.Alert{
		ID:             alertID,
		Title:          "clear fails",
		MetricName:     strPtr("pg_stat_activity.count"),
		Operator:       strPtr(">"),
		ThresholdValue: float64Ptr(1),
		ConnectionID:   connID,
	}, 0)

	if status := alertStatus(t, pool, alertID); status != "active" {
		t.Errorf("alert status after a failed clear = %q, want \"active\"", status)
	}
	select {
	case job := <-capture.jobs:
		t.Errorf("a notification was queued despite the failed clear: %s", job.notifTyp)
	case <-time.After(200 * time.Millisecond):
	}
}
