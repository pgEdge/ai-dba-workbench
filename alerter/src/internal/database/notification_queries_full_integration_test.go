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

func TestCreateUpdateDeleteNotificationChannel(t *testing.T) {
	ds, _, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	owner := "tester"
	desc := "desc"
	method := "POST"
	now := time.Now()
	ch := &NotificationChannel{
		OwnerUsername:         &owner,
		Enabled:               true,
		ChannelType:           ChannelTypeWebhook,
		Name:                  "create-test",
		Description:           &desc,
		HTTPMethod:            method,
		Headers:               map[string]string{"X-Hdr": "v"},
		SMTPPort:              587,
		SMTPUseTLS:            true,
		ReminderEnabled:       true,
		ReminderIntervalHours: 4,
		IsEstateDefault:       false,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	if err := ds.CreateNotificationChannel(ctx, ch); err != nil {
		t.Fatalf("CreateNotificationChannel: %v", err)
	}
	if ch.ID == 0 {
		t.Fatal("expected ID set")
	}

	ch.Name = "create-test-renamed"
	ch.UpdatedAt = time.Now()
	if err := ds.UpdateNotificationChannel(ctx, ch); err != nil {
		t.Fatalf("UpdateNotificationChannel: %v", err)
	}
	got, err := ds.GetNotificationChannel(ctx, ch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "create-test-renamed" {
		t.Errorf("got name=%q", got.Name)
	}

	if err := ds.DeleteNotificationChannel(ctx, ch.ID); err != nil {
		t.Fatalf("DeleteNotificationChannel: %v", err)
	}
	if _, err := ds.GetNotificationChannel(ctx, ch.ID); err == nil {
		t.Errorf("expected error after delete")
	}

	// Canceled context: errors propagate.
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := ds.UpdateNotificationChannel(canceled, ch); err == nil {
		t.Errorf("update canceled: expected error")
	}
	if err := ds.DeleteNotificationChannel(canceled, 1); err == nil {
		t.Errorf("delete canceled: expected error")
	}
}

func TestEmailRecipients(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	channelID := insertTestChannel(t, pool, "email-ch", "email", false)

	// Empty.
	got, err := ds.GetEmailRecipients(ctx, channelID)
	if err != nil {
		t.Fatalf("GetEmailRecipients: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0, got %d", len(got))
	}

	// Create.
	r := &EmailRecipient{
		ChannelID:    channelID,
		EmailAddress: "a@example.com",
		Enabled:      true,
		CreatedAt:    time.Now(),
	}
	if err := ds.CreateEmailRecipient(ctx, r); err != nil {
		t.Fatalf("CreateEmailRecipient: %v", err)
	}
	if r.ID == 0 {
		t.Fatal("expected ID")
	}

	// Disabled recipients are excluded.
	disabled := &EmailRecipient{ChannelID: channelID, EmailAddress: "b@example.com", Enabled: false, CreatedAt: time.Now()}
	if err := ds.CreateEmailRecipient(ctx, disabled); err != nil {
		t.Fatal(err)
	}
	got, err = ds.GetEmailRecipients(ctx, channelID)
	if err != nil {
		t.Fatalf("GetEmailRecipients (after disabled insert): %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 enabled recipient, got %d", len(got))
	}

	// Delete.
	if err := ds.DeleteEmailRecipient(ctx, r.ID); err != nil {
		t.Fatalf("DeleteEmailRecipient: %v", err)
	}
	got, err = ds.GetEmailRecipients(ctx, channelID)
	if err != nil {
		t.Fatalf("GetEmailRecipients (after delete): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 after delete, got %d", len(got))
	}

	// Canceled context paths.
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ds.GetEmailRecipients(canceled, channelID); err == nil {
		t.Errorf("expected cancel error")
	}
	if err := ds.DeleteEmailRecipient(canceled, 1); err == nil {
		t.Errorf("expected delete cancel error")
	}
}

func TestConnectionChannelLink(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "lk-conn")
	channelID := insertTestChannel(t, pool, "lk-ch", "slack", false)

	link := &ConnectionNotificationChannel{
		ConnectionID: connID,
		ChannelID:    channelID,
		Enabled:      true,
		CreatedAt:    time.Now(),
	}
	if err := ds.LinkConnectionToChannel(ctx, link); err != nil {
		t.Fatalf("LinkConnectionToChannel: %v", err)
	}
	if link.ID == 0 {
		t.Fatal("expected link ID")
	}

	links, err := ds.GetConnectionChannelLinks(ctx, connID)
	if err != nil {
		t.Fatalf("GetConnectionChannelLinks: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}

	if err := ds.UnlinkConnectionFromChannel(ctx, connID, channelID); err != nil {
		t.Fatalf("UnlinkConnectionFromChannel: %v", err)
	}
	links, err = ds.GetConnectionChannelLinks(ctx, connID)
	if err != nil {
		t.Fatalf("GetConnectionChannelLinks (after unlink): %v", err)
	}
	if len(links) != 0 {
		t.Errorf("expected 0 links after unlink")
	}

	// Cancel paths.
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := ds.UnlinkConnectionFromChannel(canceled, connID, channelID); err == nil {
		t.Errorf("expected unlink cancel")
	}
	if _, err := ds.GetConnectionChannelLinks(canceled, connID); err == nil {
		t.Errorf("expected cancel error")
	}
}

func TestNotificationHistoryLifecycle(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "nh-conn")
	channelID := insertTestChannel(t, pool, "nh-ch", "webhook", false)

	// Insert an alert to reference.
	var alertID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO alerts (alert_type, connection_id, severity, title, description, status)
		VALUES ('threshold', $1, 'warning', 't', 'd', 'active') RETURNING id
	`, connID).Scan(&alertID); err != nil {
		t.Fatal(err)
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
		t.Fatal("expected ID")
	}

	hist.Status = NotificationStatusSent
	now := time.Now()
	hist.SentAt = &now
	if err := ds.UpdateNotificationHistory(ctx, hist); err != nil {
		t.Fatalf("UpdateNotificationHistory: %v", err)
	}

	// History for alert.
	got, err := ds.GetNotificationHistoryForAlert(ctx, alertID)
	if err != nil {
		t.Fatalf("GetNotificationHistoryForAlert: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1, got %d", len(got))
	}

	// Pending notifications: insert another with status 'pending'.
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
		t.Fatal(err)
	}
	pendList, err := ds.GetPendingNotifications(ctx)
	if err != nil {
		t.Fatalf("GetPendingNotifications: %v", err)
	}
	if len(pendList) != 1 {
		t.Errorf("expected 1 pending, got %d", len(pendList))
	}

	// Canceled context.
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := ds.UpdateNotificationHistory(canceled, hist); err == nil {
		t.Errorf("expected update cancel")
	}
	if _, err := ds.GetPendingNotifications(canceled); err == nil {
		t.Errorf("expected pending cancel")
	}
	if _, err := ds.GetNotificationHistoryForAlert(canceled, alertID); err == nil {
		t.Errorf("expected history cancel")
	}
}

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
		t.Fatal(err)
	}

	// Initially nil.
	got, err := ds.GetReminderState(ctx, alertID, channelID)
	if err != nil {
		t.Fatalf("GetReminderState: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}

	// Upsert (insert).
	state := &NotificationReminderState{
		AlertID:        alertID,
		ChannelID:      channelID,
		LastReminderAt: time.Now(),
		ReminderCount:  1,
	}
	if err := ds.UpsertReminderState(ctx, state); err != nil {
		t.Fatalf("UpsertReminderState insert: %v", err)
	}
	if state.ID == 0 {
		t.Fatal("expected ID set")
	}

	// Upsert (update).
	state.ReminderCount = 2
	state.LastReminderAt = time.Now()
	if err := ds.UpsertReminderState(ctx, state); err != nil {
		t.Fatalf("UpsertReminderState update: %v", err)
	}

	// Get back.
	got, err = ds.GetReminderState(ctx, alertID, channelID)
	if err != nil {
		t.Fatalf("GetReminderState: %v", err)
	}
	if got.ReminderCount != 2 {
		t.Errorf("count = %d, want 2", got.ReminderCount)
	}

	// DeleteReminderStatesForAlert.
	if err := ds.DeleteReminderStatesForAlert(ctx, alertID); err != nil {
		t.Fatalf("DeleteReminderStatesForAlert: %v", err)
	}
	got, err = ds.GetReminderState(ctx, alertID, channelID)
	if err != nil {
		t.Fatalf("GetReminderState (after delete): %v", err)
	}
	if got != nil {
		t.Errorf("expected nil after delete")
	}

	// Canceled context paths.
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := ds.UpsertReminderState(canceled, state); err == nil {
		t.Errorf("expected upsert cancel")
	}
	if err := ds.DeleteReminderStatesForAlert(canceled, alertID); err == nil {
		t.Errorf("expected delete cancel")
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
