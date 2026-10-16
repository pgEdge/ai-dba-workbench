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
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Seed and read-back statements for the anomaly recovery query tests.
const (
	insertRecoveryAlertSQL = `
        INSERT INTO alerts
            (alert_type, connection_id, database_name, severity, title,
             description, status, metric_name, metric_value, anomaly_score,
             cleared_at, ai_analysis, ai_analysis_metric_value)
        VALUES ($1, $2, $3, 'warning', 't', 'd', $4, $5, 10, 5, $6,
                'cached analysis', 10)
        RETURNING id
    `

	selectRecoveryAlertSQL = `
        SELECT status, cleared_at IS NOT NULL, severity, metric_value,
               anomaly_score, last_updated IS NOT NULL, ai_analysis
        FROM alerts WHERE id = $1
    `
)

// recoveryAlertRow is the state selectRecoveryAlertSQL reads back.
type recoveryAlertRow struct {
	status         string
	hasClearedAt   bool
	severity       string
	metricValue    float64
	anomalyScore   float64
	hasLastUpdated bool
	aiAnalysis     *string
}

func insertRecoveryAlert(t *testing.T, pool *pgxpool.Pool, alertType string, connID int,
	dbName *string, status, metric string, clearedAt *time.Time) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(), insertRecoveryAlertSQL,
		alertType, connID, dbName, status, metric, clearedAt).Scan(&id); err != nil {
		t.Fatalf("insertRecoveryAlert: %v", err)
	}
	return id
}

func readRecoveryAlert(t *testing.T, pool *pgxpool.Pool, id int64) recoveryAlertRow {
	t.Helper()
	var r recoveryAlertRow
	if err := pool.QueryRow(context.Background(), selectRecoveryAlertSQL, id).Scan(
		&r.status, &r.hasClearedAt, &r.severity, &r.metricValue,
		&r.anomalyScore, &r.hasLastUpdated, &r.aiAnalysis); err != nil {
		t.Fatalf("readRecoveryAlert: %v", err)
	}
	return r
}

// TestListActiveAnomalyAlerts checks that only active anomaly alerts with
// a metric are listed, and in id order.
func TestListActiveAnomalyAlerts(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "recovery-list")
	db := "app"

	empty, err := ds.ListActiveAnomalyAlerts(ctx)
	if err != nil {
		t.Fatalf("ListActiveAnomalyAlerts (empty): %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected no alerts, got %d", len(empty))
	}

	first := insertRecoveryAlert(t, pool, "anomaly", connID, nil, "active", "metric_a", nil)
	second := insertRecoveryAlert(t, pool, "anomaly", connID, &db, "active", "metric_b", nil)
	insertRecoveryAlert(t, pool, "anomaly", connID, nil, "acknowledged", "metric_a", nil)
	cleared := time.Now()
	insertRecoveryAlert(t, pool, "anomaly", connID, nil, "cleared", "metric_a", &cleared)
	insertRecoveryAlert(t, pool, "threshold", connID, nil, "active", "metric_a", nil)

	alerts, err := ds.ListActiveAnomalyAlerts(ctx)
	if err != nil {
		t.Fatalf("ListActiveAnomalyAlerts: %v", err)
	}
	if len(alerts) != 2 {
		t.Fatalf("expected 2 active anomaly alerts, got %d", len(alerts))
	}
	if alerts[0].ID != first || alerts[1].ID != second {
		t.Errorf("got ids %d, %d; want %d, %d", alerts[0].ID, alerts[1].ID, first, second)
	}
	if alerts[1].DatabaseName == nil || *alerts[1].DatabaseName != db {
		t.Errorf("database name not scanned: %v", alerts[1].DatabaseName)
	}
	if alerts[0].MetricName == nil || *alerts[0].MetricName != "metric_a" {
		t.Errorf("metric name not scanned: %v", alerts[0].MetricName)
	}
}

// TestRefreshAnomalyAlert checks the refresh writes the new evaluation,
// drops a cached analysis written for another value, and leaves an alert
// that is no longer active alone.
func TestRefreshAnomalyAlert(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "recovery-refresh")

	t.Run("active alert is refreshed", func(t *testing.T) {
		id := insertRecoveryAlert(t, pool, "anomaly", connID, nil, "active", "metric_r", nil)
		updated, err := ds.RefreshAnomalyAlert(ctx, id, 42, 9.5, "critical")
		if err != nil {
			t.Fatalf("RefreshAnomalyAlert: %v", err)
		}
		if !updated {
			t.Fatal("expected the active alert to be updated")
		}
		got := readRecoveryAlert(t, pool, id)
		if got.metricValue != 42 || got.anomalyScore != 9.5 || got.severity != "critical" {
			t.Errorf("got value=%v score=%v severity=%s", got.metricValue, got.anomalyScore, got.severity)
		}
		if !got.hasLastUpdated {
			t.Error("expected last_updated to be set")
		}
		if got.aiAnalysis != nil {
			t.Errorf("expected cached analysis to be discarded, got %q", *got.aiAnalysis)
		}
		if got.status != "active" {
			t.Errorf("status = %s, want active", got.status)
		}
	})

	t.Run("unchanged value keeps cached analysis", func(t *testing.T) {
		id := insertRecoveryAlert(t, pool, "anomaly", connID, nil, "active", "metric_r", nil)
		if _, err := ds.RefreshAnomalyAlert(ctx, id, 10, 6, "warning"); err != nil {
			t.Fatalf("RefreshAnomalyAlert: %v", err)
		}
		if got := readRecoveryAlert(t, pool, id); got.aiAnalysis == nil {
			t.Error("expected cached analysis to survive an unchanged value")
		}
	})

	t.Run("acknowledged alert is not touched", func(t *testing.T) {
		id := insertRecoveryAlert(t, pool, "anomaly", connID, nil, "acknowledged", "metric_r", nil)
		updated, err := ds.RefreshAnomalyAlert(ctx, id, 42, 9.5, "critical")
		if err != nil {
			t.Fatalf("RefreshAnomalyAlert: %v", err)
		}
		if updated {
			t.Error("expected no update for an acknowledged alert")
		}
		if got := readRecoveryAlert(t, pool, id); got.metricValue != 10 || got.hasLastUpdated {
			t.Errorf("acknowledged alert changed: %+v", got)
		}
	})

	t.Run("threshold alert is not touched", func(t *testing.T) {
		id := insertRecoveryAlert(t, pool, "threshold", connID, nil, "active", "metric_r", nil)
		updated, err := ds.RefreshAnomalyAlert(ctx, id, 42, 9.5, "critical")
		if err != nil {
			t.Fatalf("RefreshAnomalyAlert: %v", err)
		}
		if updated {
			t.Error("expected no update for a threshold alert")
		}
	})
}

// TestClearActiveAnomalyAlert checks that only an active anomaly alert is
// cleared.
func TestClearActiveAnomalyAlert(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "recovery-clear")

	active := insertRecoveryAlert(t, pool, "anomaly", connID, nil, "active", "metric_c", nil)
	cleared, err := ds.ClearActiveAnomalyAlert(ctx, active)
	if err != nil {
		t.Fatalf("ClearActiveAnomalyAlert: %v", err)
	}
	if !cleared {
		t.Fatal("expected the active alert to be cleared")
	}
	if got := readRecoveryAlert(t, pool, active); got.status != "cleared" || !got.hasClearedAt {
		t.Errorf("got status=%s cleared_at set=%v", got.status, got.hasClearedAt)
	}

	// A second clear finds nothing to do.
	again, err := ds.ClearActiveAnomalyAlert(ctx, active)
	if err != nil {
		t.Fatalf("ClearActiveAnomalyAlert (again): %v", err)
	}
	if again {
		t.Error("expected a cleared alert not to be cleared again")
	}

	acked := insertRecoveryAlert(t, pool, "anomaly", connID, nil, "acknowledged", "metric_c", nil)
	ackCleared, err := ds.ClearActiveAnomalyAlert(ctx, acked)
	if err != nil {
		t.Fatalf("ClearActiveAnomalyAlert (acknowledged): %v", err)
	}
	if ackCleared {
		t.Error("expected an acknowledged alert to be left alone")
	}
	if got := readRecoveryAlert(t, pool, acked); got.status != "acknowledged" || got.hasClearedAt {
		t.Errorf("acknowledged alert changed: %+v", got)
	}
}

// TestGetRecentlyClearedAnomalyAlert checks the cooldown lookup matches on
// metric, connection and database, NULL-aware, within the window only.
func TestGetRecentlyClearedAnomalyAlert(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "recovery-cooldown")
	otherConn := insertTestConnection(t, pool, "recovery-cooldown-other")
	db := "app"
	otherDB := "other"
	recent := time.Now().Add(-time.Minute)
	old := time.Now().Add(-time.Hour)

	insertRecoveryAlert(t, pool, "anomaly", connID, nil, "cleared", "metric_w", &recent)
	insertRecoveryAlert(t, pool, "anomaly", connID, &db, "cleared", "metric_db", &recent)
	insertRecoveryAlert(t, pool, "anomaly", connID, nil, "cleared", "metric_old", &old)
	insertRecoveryAlert(t, pool, "threshold", connID, nil, "cleared", "metric_thr", &recent)

	cooldown := 5 * time.Minute
	tests := []struct {
		name   string
		metric string
		connID int
		db     *string
		want   bool
	}{
		{"recent connection-wide clear", "metric_w", connID, nil, true},
		{"other connection", "metric_w", otherConn, nil, false},
		{"database-scoped query misses connection-wide alert", "metric_w", connID, &db, false},
		{"recent database clear", "metric_db", connID, &db, true},
		{"other database", "metric_db", connID, &otherDB, false},
		{"connection-wide query misses database alert", "metric_db", connID, nil, false},
		{"clear outside the cooldown", "metric_old", connID, nil, false},
		{"threshold alert ignored", "metric_thr", connID, nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ds.GetRecentlyClearedAnomalyAlert(ctx, tc.metric, tc.connID, tc.db, cooldown)
			if err != nil {
				t.Fatalf("GetRecentlyClearedAnomalyAlert: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestAnomalyRecoveryQueriesReturnErrorOnClosedPool covers the error
// branches of anomaly_recovery_queries.go.
func TestAnomalyRecoveryQueriesReturnErrorOnClosedPool(t *testing.T) {
	ds, cleanup := newClosedPoolDatastore(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := ds.ListActiveAnomalyAlerts(ctx); err == nil {
		t.Error("ListActiveAnomalyAlerts should error on closed pool")
	}
	if _, err := ds.RefreshAnomalyAlert(ctx, 1, 1, 1, "info"); err == nil {
		t.Error("RefreshAnomalyAlert should error on closed pool")
	}
	if _, err := ds.ClearActiveAnomalyAlert(ctx, 1); err == nil {
		t.Error("ClearActiveAnomalyAlert should error on closed pool")
	}
	if _, err := ds.GetRecentlyClearedAnomalyAlert(ctx, "x", 1, nil, time.Minute); err == nil {
		t.Error("GetRecentlyClearedAnomalyAlert should error on closed pool")
	}
}
