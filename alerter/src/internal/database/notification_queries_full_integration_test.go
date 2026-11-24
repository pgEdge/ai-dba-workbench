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
// UpdateNotificationHistory and ClaimPendingNotification: a row is
// owned by whoever created or last claimed it until its lease, set by
// the database's clock, runs out; only one attempt can claim a due row;
// and only the attempt holding the current lease can record an outcome
// (issue #866). The original also asserted through
// GetNotificationHistoryForAlert, removed as unreachable, so that leg
// is checked with SQL instead.
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
	newHistory := func(typ NotificationType) *NotificationHistory {
		return &NotificationHistory{
			AlertID:          &alertID,
			ChannelID:        &channelID,
			ConnectionID:     &connID,
			NotificationType: typ,
			Status:           NotificationStatusPending,
			PayloadJSON:      map[string]any{"a": 1},
			MaxAttempts:      3,
			CreatedAt:        time.Now(),
		}
	}
	// leaseLeft is how long the row's lease has to run by the
	// database's clock.
	leaseLeft := func(id int64) time.Duration {
		t.Helper()
		var secs float64
		if err := pool.QueryRow(ctx,
			"SELECT EXTRACT(EPOCH FROM next_retry_at - NOW())::float8 FROM notification_history WHERE id = $1",
			id).Scan(&secs); err != nil {
			t.Fatalf("reading lease: %v", err)
		}
		return time.Duration(secs * float64(time.Second))
	}
	expire := func(id int64) {
		t.Helper()
		if _, err := pool.Exec(ctx,
			"UPDATE notification_history SET next_retry_at = NOW() - INTERVAL '1 minute' WHERE id = $1",
			id); err != nil {
			t.Fatalf("expiring lease: %v", err)
		}
	}
	claim := func() *NotificationHistory {
		t.Helper()
		got, err := ds.ClaimPendingNotification(ctx, time.Minute)
		if err != nil {
			t.Fatalf("ClaimPendingNotification: %v", err)
		}
		return got
	}

	// A new row is owned by its creator for the lease, by the
	// database's clock, whatever NextRetryAt the caller set.
	hist := newHistory(NotificationTypeAlertFire)
	past := time.Now().Add(-time.Hour)
	hist.NextRetryAt = &past
	if err := ds.CreateNotificationHistory(ctx, hist, time.Minute); err != nil {
		t.Fatalf("CreateNotificationHistory: %v", err)
	}
	if hist.ID == 0 || hist.NextRetryAt == nil {
		t.Fatalf("created row has ID %d, lease %v", hist.ID, hist.NextRetryAt)
	}
	if left := leaseLeft(hist.ID); left <= 50*time.Second || left > time.Minute {
		t.Errorf("new row's lease has %v left, want about a minute", left)
	}
	if got := claim(); got != nil {
		t.Errorf("claimed row %d while its creator owned it", got.ID)
	}

	// The creator records its outcome with the lease it was given; an
	// attempt holding any other lease cannot.
	created := *hist.NextRetryAt
	hist.Status = NotificationStatusSent
	now := time.Now()
	hist.SentAt = &now
	hist.AttemptCount = 1
	stale := created.Add(-time.Second)
	if owned, err := ds.UpdateNotificationHistory(ctx, hist, &stale); err != nil || owned {
		t.Errorf("UpdateNotificationHistory with a stale lease = %v, %v; want false", owned, err)
	}
	var status string
	if err := pool.QueryRow(ctx,
		"SELECT status FROM notification_history WHERE id = $1",
		hist.ID).Scan(&status); err != nil {
		t.Fatalf("reading back status: %v", err)
	}
	if status != string(NotificationStatusPending) {
		t.Errorf("status after a stale update = %q, want %q", status, NotificationStatusPending)
	}
	if owned, err := ds.UpdateNotificationHistory(ctx, hist, &created); err != nil || !owned {
		t.Fatalf("UpdateNotificationHistory with the row's lease = %v, %v; want true", owned, err)
	}
	if err := pool.QueryRow(ctx,
		"SELECT status FROM notification_history WHERE id = $1",
		hist.ID).Scan(&status); err != nil {
		t.Fatalf("reading back status: %v", err)
	}
	if status != string(NotificationStatusSent) {
		t.Errorf("status = %q, want %q", status, NotificationStatusSent)
	}

	// A sent row is never claimed, even once it is past due.
	expire(hist.ID)
	if got := claim(); got != nil {
		t.Errorf("claimed sent row %d", got.ID)
	}

	// Once its creator's lease runs out, a pending row is claimed by
	// exactly one attempt, which takes a new lease, and then cannot be
	// claimed again until that lease runs out too.
	older := newHistory(NotificationTypeReminder)
	if err := ds.CreateNotificationHistory(ctx, older, time.Minute); err != nil {
		t.Fatalf("CreateNotificationHistory (older): %v", err)
	}
	newer := newHistory(NotificationTypeReminder)
	newer.CreatedAt = older.CreatedAt.Add(time.Second)
	if err := ds.CreateNotificationHistory(ctx, newer, time.Minute); err != nil {
		t.Fatalf("CreateNotificationHistory (newer): %v", err)
	}
	creatorLease := *newer.NextRetryAt
	expire(newer.ID)
	expire(older.ID)
	first := claim()
	if first == nil || first.ID != older.ID {
		t.Fatalf("first claim = %+v, want the older row %d", first, older.ID)
	}
	if first.NextRetryAt == nil || first.Status != NotificationStatusPending {
		t.Errorf("claimed row = %+v, want pending with a lease", first)
	}
	if left := leaseLeft(first.ID); left <= 50*time.Second || left > time.Minute {
		t.Errorf("claimed row's lease has %v left, want about a minute", left)
	}
	second := claim()
	if second == nil || second.ID != newer.ID {
		t.Fatalf("second claim = %+v, want the newer row %d", second, newer.ID)
	}
	if got := claim(); got != nil {
		t.Errorf("claimed row %d a second time while its lease held", got.ID)
	}

	// The row's creator, which outran its lease, can no longer record
	// an outcome; the attempt that claimed it can.
	newer.Status = NotificationStatusSent
	if owned, err := ds.UpdateNotificationHistory(ctx, newer, &creatorLease); err != nil || owned {
		t.Errorf("update by the outrun creator = %v, %v; want false", owned, err)
	}
	second.Status = NotificationStatusRetrying
	second.AttemptCount = 1
	if owned, err := ds.UpdateNotificationHistory(ctx, second, second.NextRetryAt); err != nil || !owned {
		t.Errorf("update by the claiming attempt = %v, %v; want true", owned, err)
	}

	// A due row whose outcome another session is writing at that
	// moment, so that the row is locked, is passed over rather than
	// waited for, and is claimable once the lock is released.
	expire(older.ID)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.Exec(ctx,
		"SELECT 1 FROM notification_history WHERE id = $1 FOR UPDATE", older.ID); err != nil {
		t.Fatalf("locking row: %v", err)
	}
	if got := claim(); got != nil {
		t.Errorf("claimed row %d while another session held its lock", got.ID)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if got := claim(); got == nil || got.ID != older.ID {
		t.Errorf("claim after the lock was released = %+v, want row %d", got, older.ID)
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ds.UpdateNotificationHistory(canceled, hist, hist.NextRetryAt); err == nil {
		t.Error("expected a cancel error from UpdateNotificationHistory")
	}
	if _, err := ds.ClaimPendingNotification(canceled, time.Minute); err == nil {
		t.Error("expected a cancel error from ClaimPendingNotification")
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

// =============================================================================
// Telegram column mapping (issue #475)
// =============================================================================

// The three queries that hydrate a NotificationChannel scan positionally
// into a hand-written argument list, and telegram_chat_id now sits
// directly between telegram_bot_token_encrypted and template_alert_fire.
// All three are nullable text, so a SELECT list and a scan list that
// drift apart produce no type error at all: the alerter would simply
// send every Telegram message to whatever landed in TelegramChatID. The
// markers below are distinct per column so any such swap shows up as a
// value mismatch rather than a silent pass.
//
// The bot token is deliberately stored as opaque ciphertext-looking
// text. These queries alias telegram_bot_token_encrypted straight into
// the struct field and perform no decryption, so the value must come
// back byte for byte.
const (
	telegramTestBotToken      = "ENCRYPTED-BOT-TOKEN-MARKER"
	telegramTestChatID        = "-1001234567890"
	telegramTestFireTemplate  = "TEMPLATE-ALERT-FIRE-MARKER"
	telegramTestClearTemplate = "TEMPLATE-ALERT-CLEAR-MARKER"
)

// insertTelegramTestChannel inserts a telegram channel whose adjacent
// nullable text columns each carry a distinct marker, and returns its
// id.
func insertTelegramTestChannel(t *testing.T, pool *pgxpool.Pool, name string,
	isEstateDefault bool) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO notification_channels (
			owner_username, enabled, channel_type, name, http_method,
			headers_json, smtp_port, smtp_use_tls,
			telegram_bot_token_encrypted, telegram_chat_id,
			template_alert_fire, template_alert_clear,
			reminder_enabled, reminder_interval_hours, is_estate_default
		) VALUES ('tester', TRUE, 'telegram', $1, 'POST', '{}', 587, TRUE,
			$2, $3, $4, $5, TRUE, 1, $6)
		RETURNING id
	`, name, telegramTestBotToken, telegramTestChatID, telegramTestFireTemplate,
		telegramTestClearTemplate, isEstateDefault).Scan(&id)
	if err != nil {
		t.Fatalf("insertTelegramTestChannel: %v", err)
	}
	return id
}

// assertTelegramChannelFields checks that every marker came back in the
// field that owns it.
func assertTelegramChannelFields(t *testing.T, query string, channel *NotificationChannel) {
	t.Helper()
	if channel == nil {
		t.Fatalf("%s: channel is nil", query)
	}
	if channel.ChannelType != ChannelTypeTelegram {
		t.Errorf("%s: ChannelType = %q, want telegram", query, channel.ChannelType)
	}
	for _, f := range []struct {
		name string
		got  *string
		want string
	}{
		{"TelegramBotToken", channel.TelegramBotToken, telegramTestBotToken},
		{"TelegramChatID", channel.TelegramChatID, telegramTestChatID},
		{"TemplateAlertFire", channel.TemplateAlertFire, telegramTestFireTemplate},
		{"TemplateAlertClear", channel.TemplateAlertClear, telegramTestClearTemplate},
	} {
		if f.got == nil {
			t.Errorf("%s: %s = nil, want %q", query, f.name, f.want)
			continue
		}
		if *f.got != f.want {
			t.Errorf("%s: %s = %q, want %q", query, f.name, *f.got, f.want)
		}
	}
}

func TestGetNotificationChannel_TelegramFields(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	id := insertTelegramTestChannel(t, pool, "tg-single", true)

	got, err := ds.GetNotificationChannel(ctx, id)
	if err != nil {
		t.Fatalf("GetNotificationChannel: %v", err)
	}
	assertTelegramChannelFields(t, "GetNotificationChannel", got)
}

func TestGetNotificationChannelsForConnection_TelegramFields(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "tg-conn")
	insertTelegramTestChannel(t, pool, "tg-for-connection", true)

	got, err := ds.GetNotificationChannelsForConnection(ctx, connID)
	if err != nil {
		t.Fatalf("GetNotificationChannelsForConnection: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(got))
	}
	assertTelegramChannelFields(t, "GetNotificationChannelsForConnection", got[0])
}

func TestGetDueReminders_TelegramFields(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "tg-reminder-conn")
	insertTelegramTestChannel(t, pool, "tg-reminder", true)

	if _, err := pool.Exec(ctx, `
		INSERT INTO alerts (alert_type, connection_id, severity, title,
			description, status, triggered_at)
		VALUES ('threshold', $1, 'warning', 't', 'd', 'active',
			NOW() - INTERVAL '2 hours')
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
	assertTelegramChannelFields(t, "GetDueReminders", reminders[0].Channel)
}

// TestGetNotificationChannelsForConnection_ScanError covers the scan
// failure branch, which no other test reaches. SMTPPort is a plain int,
// so a NULL smtp_port column has nowhere to go and pgx reports a scan
// error; the function must surface it rather than returning a partly
// built channel list.
func TestGetNotificationChannelsForConnection_ScanError(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "scan-error-conn")
	id := insertTestChannel(t, pool, "scan-error-channel", "slack", true)
	if _, err := pool.Exec(ctx,
		`UPDATE notification_channels SET smtp_port = NULL WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}

	got, err := ds.GetNotificationChannelsForConnection(ctx, connID)
	if err == nil {
		t.Fatalf("expected a scan error, got %d channel(s)", len(got))
	}
	if !strings.Contains(err.Error(), "failed to scan notification channel") {
		t.Errorf("error = %v, want it to report the scan failure", err)
	}
	if got != nil {
		t.Errorf("channels = %+v, want nil on error", got)
	}
}
