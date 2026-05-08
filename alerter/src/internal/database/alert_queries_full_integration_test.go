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
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// insertActiveThresholdAlert inserts a threshold alert and returns its
// id. Tests use this to set up a persistent active alert state.
func insertActiveThresholdAlert(t *testing.T, pool *pgxpool.Pool, connID int, ruleID int64, dbName *string) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO alerts (
			alert_type, rule_id, connection_id, database_name, severity,
			title, description, status, metric_name, metric_value
		) VALUES ('threshold', $1, $2, $3, 'warning', 'thr-alert',
		    'desc', 'active', 'cpu', 50)
		RETURNING id
	`, ruleID, connID, dbName).Scan(&id)
	if err != nil {
		t.Fatalf("insertActiveThresholdAlert: %v", err)
	}
	return id
}

func TestGetActiveThresholdAlert(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "tha-conn")
	ruleID := insertTestRule(t, pool, "tha_rule", "cpu", ">", 80, "warning", true)

	// No alert.
	got, err := ds.GetActiveThresholdAlert(ctx, ruleID, connID, nil)
	if err != nil {
		t.Fatalf("GetActiveThresholdAlert (none): %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}

	// Insert an active alert and read it back.
	id := insertActiveThresholdAlert(t, pool, connID, ruleID, nil)
	got, err = ds.GetActiveThresholdAlert(ctx, ruleID, connID, nil)
	if err != nil {
		t.Fatalf("GetActiveThresholdAlert: %v", err)
	}
	if got == nil || got.ID != id {
		t.Errorf("got %+v, want id=%d", got, id)
	}

	// Database-scoped alert: matching nil dbName must not find it.
	dbName := "my_db"
	insertActiveThresholdAlert(t, pool, connID, ruleID, &dbName)
	gotDB, err := ds.GetActiveThresholdAlert(ctx, ruleID, connID, &dbName)
	if err != nil {
		t.Fatalf("GetActiveThresholdAlert dbname: %v", err)
	}
	if gotDB == nil {
		t.Fatal("expected db-scoped alert")
	}

	// Cancelled context: error.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ds.GetActiveThresholdAlert(cancelled, ruleID, connID, nil); err == nil {
		t.Errorf("expected cancellation error")
	}
}

func TestGetActiveAnomalyAlert(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "anom-conn")

	// Missing returns ErrNoRows.
	if _, err := ds.GetActiveAnomalyAlert(ctx, "metric_a", connID, nil); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("expected ErrNoRows, got %v", err)
	}

	// Insert anomaly alert.
	if _, err := pool.Exec(ctx, `
		INSERT INTO alerts (alert_type, connection_id, severity, title, description,
		    status, metric_name)
		VALUES ('anomaly', $1, 'warning', 't', 'd', 'active', 'metric_a')
	`, connID); err != nil {
		t.Fatal(err)
	}
	got, err := ds.GetActiveAnomalyAlert(ctx, "metric_a", connID, nil)
	if err != nil {
		t.Fatalf("GetActiveAnomalyAlert: %v", err)
	}
	if got == nil {
		t.Fatal("expected alert")
	}
}

func TestGetRecentlyClearedAlert(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "rc-conn")
	ruleID := insertTestRule(t, pool, "rc_rule", "metric_rc", ">", 1, "warning", true)

	// Cleared 5 seconds ago, cooldown of 60 seconds: should be true.
	if _, err := pool.Exec(ctx, `
		INSERT INTO alerts (alert_type, rule_id, connection_id, severity, title, description, status, cleared_at)
		VALUES ('threshold', $1, $2, 'warning', 't', 'd', 'cleared', NOW() - INTERVAL '5 seconds')
	`, ruleID, connID); err != nil {
		t.Fatal(err)
	}
	exists, err := ds.GetRecentlyClearedAlert(ctx, ruleID, connID, nil, 60*time.Second)
	if err != nil {
		t.Fatalf("GetRecentlyClearedAlert: %v", err)
	}
	if !exists {
		t.Errorf("expected exists=true")
	}

	// Cooldown of 1 second: now false.
	exists, err = ds.GetRecentlyClearedAlert(ctx, ruleID, connID, nil, 1*time.Second)
	if err != nil {
		t.Fatalf("GetRecentlyClearedAlert tight: %v", err)
	}
	if exists {
		t.Errorf("expected exists=false with 1s cooldown")
	}

	// Cancelled context.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ds.GetRecentlyClearedAlert(cancelled, ruleID, connID, nil, 60*time.Second); err == nil {
		t.Errorf("expected error")
	}
}

func TestGetReevaluationSuppressedAlert(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "rs-conn")

	// No matching alerts.
	exists, err := ds.GetReevaluationSuppressedAlert(ctx, "metric_rs", connID, nil, 60*time.Second)
	if err != nil {
		t.Fatalf("GetReevaluationSuppressedAlert: %v", err)
	}
	if exists {
		t.Errorf("expected false initially")
	}

	// Insert a cleared anomaly alert with reevaluation_count > 0.
	if _, err := pool.Exec(ctx, `
		INSERT INTO alerts (alert_type, connection_id, severity, title, description,
		    status, metric_name, cleared_at, reevaluation_count)
		VALUES ('anomaly', $1, 'warning', 't', 'd', 'cleared', 'metric_rs',
		    NOW() - INTERVAL '5 seconds', 1)
	`, connID); err != nil {
		t.Fatal(err)
	}
	exists, _ = ds.GetReevaluationSuppressedAlert(ctx, "metric_rs", connID, nil, 60*time.Second)
	if !exists {
		t.Errorf("expected true within cooldown")
	}

	// Cancelled context.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ds.GetReevaluationSuppressedAlert(cancelled, "x", connID, nil, time.Second); err == nil {
		t.Errorf("expected error")
	}
}

func TestGetFalsePositiveSuppressedAlert(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "fp-conn")

	exists, err := ds.GetFalsePositiveSuppressedAlert(ctx, "metric_fp", connID, nil, 60*time.Second)
	if err != nil {
		t.Fatalf("GetFalsePositiveSuppressedAlert: %v", err)
	}
	if exists {
		t.Errorf("expected false initially")
	}

	// Insert acknowledged anomaly alert with false-positive ack.
	var alertID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO alerts (alert_type, connection_id, severity, title, description,
		    status, metric_name)
		VALUES ('anomaly', $1, 'warning', 't', 'd', 'acknowledged', 'metric_fp')
		RETURNING id
	`, connID).Scan(&alertID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO alert_acknowledgments (alert_id, acknowledged_by, acknowledge_type,
		    false_positive, acknowledged_at)
		VALUES ($1, 'tester', 'false_positive', TRUE, NOW() - INTERVAL '5 seconds')
	`, alertID); err != nil {
		t.Fatal(err)
	}
	exists, _ = ds.GetFalsePositiveSuppressedAlert(ctx, "metric_fp", connID, nil, 60*time.Second)
	if !exists {
		t.Errorf("expected true with false-positive ack")
	}

	// Cancelled context.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ds.GetFalsePositiveSuppressedAlert(cancelled, "x", connID, nil, time.Second); err == nil {
		t.Errorf("expected cancellation error")
	}
}

func TestUpdateAlertValuesAndCreateAlert(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "u-conn")
	ruleID := insertTestRule(t, pool, "u_rule", "cpu", ">", 50, "warning", true)
	val := 70.0
	thr := 50.0
	op := ">"
	a := &Alert{
		AlertType:      "threshold",
		RuleID:         &ruleID,
		ConnectionID:   connID,
		MetricName:     stringPtr("cpu"),
		MetricValue:    &val,
		ThresholdValue: &thr,
		Operator:       &op,
		Severity:       "warning",
		Title:          "test",
		Description:    "d",
		Status:         "active",
		TriggeredAt:    time.Now(),
	}
	if err := ds.CreateAlert(ctx, a); err != nil {
		t.Fatalf("CreateAlert: %v", err)
	}
	if a.ID == 0 {
		t.Fatal("expected ID set")
	}

	if err := ds.UpdateAlertValues(ctx, a.ID, 80, 50, ">", "critical"); err != nil {
		t.Fatalf("UpdateAlertValues: %v", err)
	}
	var sev string
	var mv float64
	if err := pool.QueryRow(ctx, `SELECT severity, metric_value FROM alerts WHERE id = $1`, a.ID).Scan(&sev, &mv); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if sev != "critical" || mv != 80 {
		t.Errorf("got sev=%q mv=%v", sev, mv)
	}
}

func TestGetActiveAlertsAndGetAlert(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "ga-conn")
	ruleID := insertTestRule(t, pool, "ga_rule", "x", ">", 1, "warning", true)
	id1 := insertActiveThresholdAlert(t, pool, connID, ruleID, nil)
	id2 := insertActiveThresholdAlert(t, pool, connID, ruleID, nil)

	alerts, err := ds.GetActiveAlerts(ctx)
	if err != nil {
		t.Fatalf("GetActiveAlerts: %v", err)
	}
	if len(alerts) != 2 {
		t.Errorf("expected 2 alerts, got %d", len(alerts))
	}

	got, err := ds.GetAlert(ctx, id1)
	if err != nil {
		t.Fatalf("GetAlert: %v", err)
	}
	if got.ID != id1 {
		t.Errorf("got id %d", got.ID)
	}

	// Missing alert returns ErrNoRows.
	if _, err := ds.GetAlert(ctx, 99999); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("expected ErrNoRows, got %v", err)
	}

	_ = id2
}

func TestClearAlertAndUpdateAlertReevaluation(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "clr-conn")
	ruleID := insertTestRule(t, pool, "clr_rule", "x", ">", 1, "warning", true)
	id := insertActiveThresholdAlert(t, pool, connID, ruleID, nil)

	if err := ds.ClearAlert(ctx, id); err != nil {
		t.Fatalf("ClearAlert: %v", err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM alerts WHERE id = $1`, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "cleared" {
		t.Errorf("status = %q", status)
	}

	// UpdateAlertReevaluation increments count.
	if err := ds.UpdateAlertReevaluation(ctx, id); err != nil {
		t.Fatalf("UpdateAlertReevaluation: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT reevaluation_count FROM alerts WHERE id = $1`, id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("reevaluation_count = %d, want 1", count)
	}
}

func TestGetAlertsByClusterAndConnection(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	_, clusterID := insertGroupAndCluster(t, pool, "g-ac", "c-ac")
	conn1 := insertTestConnectionInCluster(t, pool, "c-ac-1", clusterID)
	conn2 := insertTestConnectionInCluster(t, pool, "c-ac-2", clusterID)

	ruleID := insertTestRule(t, pool, "ac_rule", "x", ">", 1, "warning", true)
	insertActiveThresholdAlert(t, pool, conn1, ruleID, nil)
	insertActiveThresholdAlert(t, pool, conn2, ruleID, nil)

	// GetAlertsByCluster (excludes alerts on the queried connection).
	gotCluster, err := ds.GetAlertsByCluster(ctx, conn1)
	if err != nil {
		t.Fatalf("GetAlertsByCluster: %v", err)
	}
	if len(gotCluster) != 1 || gotCluster[0].ConnectionID != conn2 {
		t.Errorf("expected 1 alert from peer, got %+v", gotCluster)
	}

	// GetAlertsByConnection.
	gotConn, err := ds.GetAlertsByConnection(ctx, conn1)
	if err != nil {
		t.Fatalf("GetAlertsByConnection: %v", err)
	}
	if len(gotConn) != 1 || gotConn[0].ConnectionID != conn1 {
		t.Errorf("expected 1 alert for own connection, got %+v", gotConn)
	}

	// Cancelled context.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ds.GetAlertsByCluster(cancelled, conn1); err == nil {
		t.Errorf("expected cancel error")
	}
	if _, err := ds.GetAlertsByConnection(cancelled, conn1); err == nil {
		t.Errorf("expected cancel error")
	}
}

func TestGetActiveAlertsCancelled(t *testing.T) {
	ds, _, cleanup := newFullTestDatastore(t)
	defer cleanup()

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ds.GetActiveAlerts(cancelled); err == nil {
		t.Errorf("expected cancel error")
	}
}

// stringPtr returns a pointer to s.
func stringPtr(s string) *string {
	return &s
}
