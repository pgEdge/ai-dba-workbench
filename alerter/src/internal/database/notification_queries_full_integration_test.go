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
	"time"

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

// insertTestEmailRecipient seeds a recipient directly, standing in for
// the CreateEmailRecipient helper this change removed as unreachable.
func insertTestEmailRecipient(t *testing.T, pool *pgxpool.Pool, channelID int64, address string, enabled bool) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO email_recipients
			(channel_id, email_address, enabled)
		VALUES ($1, $2, $3)
		RETURNING id
	`, channelID, address, enabled).Scan(&id)
	if err != nil {
		t.Fatalf("failed to insert email recipient: %v", err)
	}
	return id
}

// TestEmailRecipients covers GetEmailRecipients, which survives this
// change. The original test drove it through CreateEmailRecipient and
// DeleteEmailRecipient, both removed here as unreachable, so the rows
// are seeded and removed with SQL instead; deleting the test outright
// would have taken the reader's error and filter coverage with it.
func TestEmailRecipients(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	channelID := insertTestChannel(t, pool, "email-ch", "email", false)

	got, err := ds.GetEmailRecipients(ctx, channelID)
	if err != nil {
		t.Fatalf("GetEmailRecipients: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 recipients, got %d", len(got))
	}

	enabledID := insertTestEmailRecipient(t, pool, channelID, "a@example.com", true)
	insertTestEmailRecipient(t, pool, channelID, "b@example.com", false)

	// Disabled recipients must be excluded by the reader.
	got, err = ds.GetEmailRecipients(ctx, channelID)
	if err != nil {
		t.Fatalf("GetEmailRecipients (with a disabled row): %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 enabled recipient, got %d", len(got))
	}
	if got[0].EmailAddress != "a@example.com" {
		t.Errorf("address = %q, want a@example.com", got[0].EmailAddress)
	}

	if _, err := pool.Exec(ctx,
		"DELETE FROM email_recipients WHERE id = $1",
		enabledID); err != nil {
		t.Fatalf("deleting recipient: %v", err)
	}
	got, err = ds.GetEmailRecipients(ctx, channelID)
	if err != nil {
		t.Fatalf("GetEmailRecipients (after delete): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 after delete, got %d", len(got))
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ds.GetEmailRecipients(canceled, channelID); err == nil {
		t.Error("expected a cancel error from GetEmailRecipients")
	}
}

// TestNotificationHistoryLifecycle covers CreateNotificationHistory,
// UpdateNotificationHistory and GetPendingNotifications, all of which
// survive this change. The original also asserted through
// GetNotificationHistoryForAlert, removed here as unreachable, so that
// leg is checked with SQL instead.
func TestNotificationHistoryLifecycle(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "nh-conn")
	channelID := insertTestChannel(t, pool, "nh-ch", "webhook", false)

	var alertID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO alerts (alert_type, connection_id, severity, title, description, status)
		VALUES ('threshold', $1, 'warning', 't', 'd', 'active') RETURNING id
	`, connID).Scan(&alertID); err != nil {
		t.Fatalf("seeding alert: %v", err)
	}

	hist := &NotificationHistory{
		AlertID:          &alertID,
		ChannelID:        &channelID,
		ConnectionID:     &connID,
		NotificationType: NotificationTypeAlertFire,
		Status:           NotificationStatusPending,
		PayloadJSON:      map[string]any{"a": 1},
		AttemptCount:     0,
		MaxAttempts:      3,
		CreatedAt:        time.Now(),
	}
	if err := ds.CreateNotificationHistory(ctx, hist); err != nil {
		t.Fatalf("CreateNotificationHistory: %v", err)
	}
	if hist.ID == 0 {
		t.Fatal("expected an ID to be assigned")
	}

	hist.Status = NotificationStatusSent
	now := time.Now()
	hist.SentAt = &now
	if err := ds.UpdateNotificationHistory(ctx, hist); err != nil {
		t.Fatalf("UpdateNotificationHistory: %v", err)
	}

	var status string
	if err := pool.QueryRow(ctx,
		"SELECT status FROM notification_history WHERE id = $1",
		hist.ID).Scan(&status); err != nil {
		t.Fatalf("reading back status: %v", err)
	}
	if status != string(NotificationStatusSent) {
		t.Errorf("status = %q, want %q", status, NotificationStatusSent)
	}

	pending := &NotificationHistory{
		AlertID:          &alertID,
		ChannelID:        &channelID,
		ConnectionID:     &connID,
		NotificationType: NotificationTypeReminder,
		Status:           NotificationStatusPending,
		AttemptCount:     0,
		MaxAttempts:      3,
		CreatedAt:        time.Now(),
	}
	if err := ds.CreateNotificationHistory(ctx, pending); err != nil {
		t.Fatalf("CreateNotificationHistory (pending): %v", err)
	}

	pendList, err := ds.GetPendingNotifications(ctx)
	if err != nil {
		t.Fatalf("GetPendingNotifications: %v", err)
	}
	if len(pendList) != 1 {
		t.Errorf("expected 1 pending notification, got %d", len(pendList))
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := ds.UpdateNotificationHistory(canceled, hist); err == nil {
		t.Error("expected a cancel error from UpdateNotificationHistory")
	}
	if _, err := ds.GetPendingNotifications(canceled); err == nil {
		t.Error("expected a cancel error from GetPendingNotifications")
	}
}

// TestReminderState covers UpsertReminderState and
// DeleteReminderStatesForAlert, both of which survive. GetReminderState
// went with this change, so the stored row is read back with SQL.
func TestReminderState(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "rs-conn")
	channelID := insertTestChannel(t, pool, "rs-ch", "slack", false)

	var alertID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO alerts (alert_type, connection_id, severity, title, description, status)
		VALUES ('threshold', $1, 'warning', 't', 'd', 'active') RETURNING id
	`, connID).Scan(&alertID); err != nil {
		t.Fatalf("seeding alert: %v", err)
	}

	reminderCount := func() (int, bool) {
		t.Helper()
		var count int
		err := pool.QueryRow(ctx, `
			SELECT reminder_count FROM notification_reminder_state
			WHERE alert_id = $1 AND channel_id = $2
		`, alertID, channelID).Scan(&count)
		if err != nil {
			return 0, false
		}
		return count, true
	}

	if _, found := reminderCount(); found {
		t.Fatal("expected no reminder state before the first upsert")
	}

	state := &NotificationReminderState{
		AlertID:        alertID,
		ChannelID:      channelID,
		LastReminderAt: time.Now(),
		ReminderCount:  1,
	}
	if err := ds.UpsertReminderState(ctx, state); err != nil {
		t.Fatalf("UpsertReminderState (insert): %v", err)
	}
	if state.ID == 0 {
		t.Fatal("expected an ID to be assigned")
	}

	// The second call must update in place rather than insert again.
	state.ReminderCount = 2
	state.LastReminderAt = time.Now()
	if err := ds.UpsertReminderState(ctx, state); err != nil {
		t.Fatalf("UpsertReminderState (update): %v", err)
	}

	count, found := reminderCount()
	if !found {
		t.Fatal("expected reminder state after upsert")
	}
	if count != 2 {
		t.Errorf("reminder_count = %d, want 2", count)
	}

	var rows int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM notification_reminder_state WHERE alert_id = $1
	`, alertID).Scan(&rows); err != nil {
		t.Fatalf("counting reminder states: %v", err)
	}
	if rows != 1 {
		t.Errorf("expected 1 reminder state row, got %d", rows)
	}

	if err := ds.DeleteReminderStatesForAlert(ctx, alertID); err != nil {
		t.Fatalf("DeleteReminderStatesForAlert: %v", err)
	}
	if _, found := reminderCount(); found {
		t.Error("expected no reminder state after delete")
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := ds.UpsertReminderState(canceled, state); err == nil {
		t.Error("expected a cancel error from UpsertReminderState")
	}
	if err := ds.DeleteReminderStatesForAlert(canceled, alertID); err == nil {
		t.Error("expected a cancel error from DeleteReminderStatesForAlert")
	}
}
