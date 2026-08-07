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
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// insertTestChannel inserts a notification_channels row with the
// minimum data needed for tests and returns its id.
func insertTestChannel(t *testing.T, pool *pgxpool.Pool, name, channelType string, isEstateDefault bool) int64 {
	t.Helper()
	var id int64
	owner := "tester"
	err := pool.QueryRow(context.Background(), `
		INSERT INTO notification_channels (
			owner_username, enabled, channel_type, name, http_method,
			headers_json, smtp_port, smtp_use_tls, reminder_enabled,
			reminder_interval_hours, is_estate_default
		) VALUES ($1, TRUE, $2, $3, 'POST', '{}', 587, TRUE, TRUE, 1, $4)
		RETURNING id
	`, owner, channelType, name, isEstateDefault).Scan(&id)
	if err != nil {
		t.Fatalf("insertTestChannel: %v", err)
	}
	return id
}

func TestGetNotificationChannel(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	id := insertTestChannel(t, pool, "primary-channel", "slack", true)

	got, err := ds.GetNotificationChannel(ctx, id)
	if err != nil {
		t.Fatalf("GetNotificationChannel: %v", err)
	}
	if got.ID != id || got.Name != "primary-channel" {
		t.Errorf("got %+v", got)
	}

	// Missing returns wrapped error.
	if _, err := ds.GetNotificationChannel(ctx, 99999); err == nil ||
		!strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got %v", err)
	}
}

func TestGetNotificationChannelsForConnection(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "nccfc-conn")

	// One estate-default channel must show up.
	insertTestChannel(t, pool, "default-1", "slack", true)
	// Non-default and no override: filtered out by the WHERE clause.
	insertTestChannel(t, pool, "non-default", "webhook", false)

	got, err := ds.GetNotificationChannelsForConnection(ctx, connID)
	if err != nil {
		t.Fatalf("GetNotificationChannelsForConnection: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 channel, got %d", len(got))
	}

	// Add a server-scope override for the non-default channel; it should
	// now be included.
	nondefault := insertTestChannel(t, pool, "non-default-2", "webhook", false)
	if _, err := pool.Exec(ctx, `
		INSERT INTO notification_channel_overrides (channel_id, scope, connection_id, enabled)
		VALUES ($1, 'server', $2, TRUE)
	`, nondefault, connID); err != nil {
		t.Fatal(err)
	}
	got, err = ds.GetNotificationChannelsForConnection(ctx, connID)
	if err != nil {
		t.Fatalf("GetNotificationChannelsForConnection: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 channels (default + override), got %d", len(got))
	}

	// Canceled context.
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ds.GetNotificationChannelsForConnection(canceled, connID); err == nil {
		t.Errorf("expected cancel error")
	}
}

func TestGetDueRemindersAndConnectionInfo(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "dr-conn")
	if _, err := pool.Exec(ctx, `UPDATE connections SET host = 'h.example', port = 5555 WHERE id = $1`, connID); err != nil {
		t.Fatal(err)
	}
	channelID := insertTestChannel(t, pool, "dr-ch", "slack", true)
	// Make sure reminder_interval_hours is non-zero.
	if _, err := pool.Exec(ctx, `UPDATE notification_channels SET reminder_interval_hours = 1, reminder_enabled = TRUE WHERE id = $1`, channelID); err != nil {
		t.Fatal(err)
	}

	// Insert an active alert that triggered 2 hours ago, beyond the
	// reminder interval. The is_estate_default channel should match.
	if _, err := pool.Exec(ctx, `
		INSERT INTO alerts (alert_type, connection_id, severity, title, description, status, triggered_at)
		VALUES ('threshold', $1, 'warning', 't', 'd', 'active', NOW() - INTERVAL '2 hours')
	`, connID); err != nil {
		t.Fatal(err)
	}

	reminders, err := ds.GetDueReminders(ctx)
	if err != nil {
		t.Fatalf("GetDueReminders: %v", err)
	}
	if len(reminders) != 1 {
		t.Fatalf("expected 1 due reminder, got %d", len(reminders))
	}
	if reminders[0].State != nil {
		t.Errorf("expected nil reminder state for first reminder, got %+v", reminders[0].State)
	}

	// Insert reminder state to exercise the populated state path.
	if _, err := pool.Exec(ctx, `
		INSERT INTO notification_reminder_state (alert_id, channel_id, last_reminder_at, reminder_count)
		VALUES ($1, $2, NOW() - INTERVAL '2 hours', 1)
	`, reminders[0].Alert.ID, channelID); err != nil {
		t.Fatal(err)
	}
	reminders, err = ds.GetDueReminders(ctx)
	if err != nil {
		t.Fatalf("GetDueReminders 2: %v", err)
	}
	if len(reminders) != 1 || reminders[0].State == nil {
		t.Errorf("expected 1 reminder with state, got %+v", reminders)
	}

	// Connection info.
	name, host, port, err := ds.GetConnectionInfo(ctx, connID)
	if err != nil {
		t.Fatalf("GetConnectionInfo: %v", err)
	}
	if name != "dr-conn" || host != "h.example" || port != 5555 {
		t.Errorf("got name=%q host=%q port=%d", name, host, port)
	}

	// Missing connection.
	if _, _, _, err := ds.GetConnectionInfo(ctx, 99999); err == nil {
		t.Errorf("expected error for missing conn")
	}

	// Canceled context.
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ds.GetDueReminders(canceled); err == nil {
		t.Errorf("expected cancel error")
	}
	if _, _, _, err := ds.GetConnectionInfo(canceled, connID); err == nil {
		t.Errorf("expected info cancel")
	}
}
