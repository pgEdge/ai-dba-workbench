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
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// systemAlertsSchemaSQL gives the fixture's alerts table the shape the
// collector's v18 migration produces, so that it can hold system alerts.
const systemAlertsSchemaSQL = `
ALTER TABLE alerts DROP CONSTRAINT alerts_alert_type_check;
ALTER TABLE alerts ADD CONSTRAINT alerts_alert_type_check
    CHECK (alert_type IN ('threshold', 'anomaly', 'connection', 'system'));
ALTER TABLE alerts ALTER COLUMN connection_id DROP NOT NULL;
ALTER TABLE alerts ADD CONSTRAINT alerts_system_connection_check
    CHECK ((alert_type = 'system') = (connection_id IS NULL));
CREATE UNIQUE INDEX idx_alerts_system_open ON alerts (metric_name)
    WHERE alert_type = 'system' AND status <> 'cleared';
`

// systemAlertStatusSQL reads an alert's status and connection.
const systemAlertStatusSQL = `SELECT status, connection_id FROM alerts WHERE id = $1`

// oldSystemAlertSQL backdates an alert so that a reminder is due.
const oldSystemAlertSQL = `UPDATE alerts SET triggered_at = NOW() - INTERVAL '2 hours' WHERE id = $1`

// reminderChannelSQL turns reminders on for a channel.
const reminderChannelSQL = `UPDATE notification_channels
    SET reminder_interval_hours = 1, reminder_enabled = TRUE WHERE id = $1`

// newSystemAlertTestDatastore returns a full test datastore whose alerts
// table accepts system alerts.
func newSystemAlertTestDatastore(t *testing.T) (*Datastore, *pgxpool.Pool) {
	t.Helper()
	ds, pool, cleanup := newFullTestDatastore(t)
	t.Cleanup(cleanup)
	if _, err := pool.Exec(context.Background(), systemAlertsSchemaSQL); err != nil {
		t.Fatalf("apply system alert schema: %v", err)
	}
	return ds, pool
}

func systemAlert(key, title string) *Alert {
	object := "openai/text-embedding-3-small"
	details := `{"tier":"tier2"}`
	return &Alert{
		AlertType:      "threshold", // overwritten by CreateSystemAlert
		ConnectionID:   42,          // likewise
		ObjectName:     &object,
		MetricName:     &key,
		Severity:       "warning",
		Title:          title,
		Description:    "initial",
		AnomalyDetails: &details,
	}
}

func TestSystemAlertLifecycle(t *testing.T) {
	ds, pool := newSystemAlertTestDatastore(t)
	ctx := context.Background()
	key := "llm_provider_health.tier2.openai"

	if got, err := ds.GetOpenSystemAlert(ctx, key); err != nil || got != nil {
		t.Fatalf("GetOpenSystemAlert before create = %v, %v", got, err)
	}

	created, isNew, err := ds.CreateSystemAlert(ctx, systemAlert(key, "first"))
	if err != nil || !isNew {
		t.Fatalf("CreateSystemAlert = %v, %v", isNew, err)
	}
	if !created.IsSystem() || created.ConnectionID != 0 || created.Status != "active" {
		t.Errorf("created alert = %+v", created)
	}

	var status string
	var connID *int
	if err := pool.QueryRow(ctx, systemAlertStatusSQL, created.ID).Scan(&status, &connID); err != nil {
		t.Fatal(err)
	}
	if connID != nil {
		t.Errorf("stored connection_id = %d, want NULL", *connID)
	}

	// A second create for the key returns the open alert.
	again, isNew, err := ds.CreateSystemAlert(ctx, systemAlert(key, "second"))
	if err != nil || isNew || again.ID != created.ID || again.Title != "first" {
		t.Fatalf("duplicate create = %+v, %v, %v", again, isNew, err)
	}

	details := `{"tier":"tier2","last_error":"x"}`
	if err := ds.UpdateSystemAlert(ctx, created.ID, "updated", &details); err != nil {
		t.Fatalf("UpdateSystemAlert: %v", err)
	}
	got, err := ds.GetOpenSystemAlert(ctx, key)
	if err != nil || got == nil || got.Description != "updated" || got.LastUpdated == nil {
		t.Fatalf("after update = %+v, %v", got, err)
	}
	if got.ConnectionID != 0 || !strings.Contains(*got.AnomalyDetails, "last_error") {
		t.Errorf("scanned alert = %+v", got)
	}

	other, _, err := ds.CreateSystemAlert(ctx, systemAlert("llm_provider_health.tier3.anthropic", "t3"))
	if err != nil {
		t.Fatal(err)
	}
	// Keys outside the prefix, and cleared alerts, are not listed.
	if _, _, err := ds.CreateSystemAlert(ctx, systemAlert("other_system_key", "x")); err != nil {
		t.Fatal(err)
	}
	open, err := ds.GetOpenSystemAlerts(ctx, "llm_provider_health.")
	if err != nil || len(open) != 2 {
		t.Fatalf("GetOpenSystemAlerts = %d, %v", len(open), err)
	}
	// starts_with treats the prefix literally, so LIKE wildcards match
	// nothing.
	if open, err := ds.GetOpenSystemAlerts(ctx, "llm_provider_health%"); err != nil || len(open) != 0 {
		t.Errorf("wildcard prefix matched %d, %v", len(open), err)
	}

	if err := ds.ClearAlert(ctx, other.ID); err != nil {
		t.Fatal(err)
	}
	if open, err := ds.GetOpenSystemAlerts(ctx, "llm_provider_health."); err != nil || len(open) != 1 {
		t.Errorf("open after clear = %d, %v, want 1", len(open), err)
	}

	// Once cleared, the alert can no longer be rewritten, and the key
	// can be raised again.
	if err := ds.ClearAlert(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if err := ds.UpdateSystemAlert(ctx, created.ID, "stale", nil); !errors.Is(err, ErrSystemAlertNotOpen) {
		t.Errorf("UpdateSystemAlert on a cleared alert = %v, want ErrSystemAlertNotOpen", err)
	}
	if _, isNew, err := ds.CreateSystemAlert(ctx, systemAlert(key, "again")); err != nil || !isNew {
		t.Errorf("re-raise after clear = %v, %v", isNew, err)
	}

	// The active alert readers see the system alert with connection 0.
	all, err := ds.GetActiveAlerts(ctx)
	if err != nil {
		t.Fatalf("GetActiveAlerts: %v", err)
	}
	var systems int
	for _, a := range all {
		if a.IsSystem() {
			systems++
			if a.ConnectionID != 0 {
				t.Errorf("system alert connection = %d", a.ConnectionID)
			}
		}
	}
	if systems == 0 {
		t.Error("GetActiveAlerts returned no system alerts")
	}
}

func TestCreateSystemAlert_NeedsKey(t *testing.T) {
	ds := &Datastore{}
	for _, key := range []*string{nil, new(string)} {
		if _, _, err := ds.CreateSystemAlert(context.Background(), &Alert{MetricName: key}); err == nil {
			t.Error("CreateSystemAlert accepted an alert with no key")
		}
	}
}

func TestSystemAlertQueries_ErrorPaths(t *testing.T) {
	ds, cleanup := newClosedPoolDatastore(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := ds.GetOpenSystemAlert(ctx, "k"); err == nil {
		t.Error("GetOpenSystemAlert should error on a closed pool")
	}
	if _, err := ds.GetOpenSystemAlerts(ctx, "k"); err == nil {
		t.Error("GetOpenSystemAlerts should error on a closed pool")
	}
	if _, _, err := ds.CreateSystemAlert(ctx, systemAlert("k", "t")); err == nil {
		t.Error("CreateSystemAlert should error on a closed pool")
	}
	if err := ds.UpdateSystemAlert(ctx, 1, "d", nil); err == nil {
		t.Error("UpdateSystemAlert should error on a closed pool")
	}
}

func TestGetDueReminders_SystemAlert(t *testing.T) {
	ds, pool := newSystemAlertTestDatastore(t)
	ctx := context.Background()

	channelID := insertTestChannel(t, pool, "sys-ch", "slack", true)
	if _, err := pool.Exec(ctx, reminderChannelSQL, channelID); err != nil {
		t.Fatal(err)
	}
	alert, _, err := ds.CreateSystemAlert(ctx, systemAlert("llm_provider_health.tier2.openai", "t"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, oldSystemAlertSQL, alert.ID); err != nil {
		t.Fatal(err)
	}

	reminders, err := ds.GetDueReminders(ctx)
	if err != nil {
		t.Fatalf("GetDueReminders: %v", err)
	}
	if len(reminders) != 1 || reminders[0].Alert.ID != alert.ID ||
		reminders[0].Alert.ConnectionID != 0 || reminders[0].Channel.ID != channelID {
		t.Fatalf("reminders = %+v", reminders)
	}
}

func TestNullableConnectionID(t *testing.T) {
	if nullableConnectionID(0) != nil {
		t.Error("0 should map to NULL")
	}
	if got := nullableConnectionID(7); got == nil || *got != 7 {
		t.Errorf("7 mapped to %v", got)
	}
	seven := 7
	if connectionIDFromNullable(nil) != 0 || connectionIDFromNullable(&seven) != 7 {
		t.Error("connectionIDFromNullable")
	}
}
