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
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/alerter/internal/database"
	"github.com/pgedge/ai-workbench/pkg/crypto"
)

// SendTimeout is the longest one notification send may take, from
// connecting to the endpoint to its last reply. processNotification
// applies it to every send, whatever other sends are under way.
const SendTimeout = 30 * time.Second

// recordTimeout bounds writing a send's outcome to its
// notification_history row. The write has a context of its own, so a
// send that used up its caller's deadline is still recorded.
const recordTimeout = 10 * time.Second

// DeliveryTimeout is how long an attempt owns the notification_history
// rows it sends: the rows SendAlertNotification and ProcessReminders
// create, and each row the pending-retry worker claims. An attempt
// sends within SendTimeout and then records its outcome within
// recordTimeout. Ownership is held by the row's next_retry_at, set by
// the database's clock, which also decides when a row is due, so the
// alerter's clock cannot shorten it; a row whose outcome was never
// recorded (the alerter stopped, say) is retried once it passes.
const DeliveryTimeout = SendTimeout + recordTimeout

// attemptContext returns the context for an attempt on rows claimed for
// DeliveryTimeout no earlier than claimedAt: it ends recordTimeout
// before the claim can, so that the attempt's outcome is written while
// the attempt still owns its rows. claimedAt is read from the
// alerter's clock before the database sets the lease from its own, so
// the deadline comes before the lease runs out, in elapsed time,
// however far apart the two clocks are.
func attemptContext(ctx context.Context, claimedAt time.Time) (context.Context, context.CancelFunc) {
	return context.WithDeadline(ctx, claimedAt.Add(DeliveryTimeout-recordTimeout))
}

// Manager implements NotificationManager
type Manager struct {
	datastore    *database.Datastore
	config       *config.NotificationsConfig
	serverSecret string
	renderer     TemplateRenderer
	notifiers    map[database.NotificationChannelType]Notifier
	httpClient   *http.Client
	debug        bool

	// Logging function
	log func(format string, args ...any)
}

// NewManager creates a new notification manager
func NewManager(ds *database.Datastore, cfg *config.NotificationsConfig, debug bool, logFunc func(string, ...any)) (*Manager, error) {
	// Return nil if notifications are disabled
	if cfg == nil || !cfg.Enabled {
		return nil, nil
	}

	// Load server secret from file (plain text, same format as server
	// secret). When SecretFile is empty, ResolveServerSecret falls back
	// to the shared default locations (per-user config dir, then
	// /etc/pgedge/ai-dba-alerter.secret), matching the collector and
	// server precedence.
	serverSecret, err := cfg.ResolveServerSecret()
	if err != nil {
		return nil, err
	}

	renderer := NewTemplateRenderer()

	// Every delivery, HTTP and SMTP, connects through the same guarded
	// dialer, so the address policy holds on the address actually
	// connected to. See dial_guard.go.
	dial := deliveryDialer(deliveryPolicy(cfg.AllowInternalNetworks), nil)
	httpClient := newDeliveryHTTPClient(cfg, dial)

	m := &Manager{
		datastore:    ds,
		config:       cfg,
		serverSecret: serverSecret,
		renderer:     renderer,
		httpClient:   httpClient,
		debug:        debug,
		log:          logFunc,
		notifiers:    make(map[database.NotificationChannelType]Notifier),
	}

	// Register notifiers
	m.notifiers[database.ChannelTypeSlack] = NewSlackNotifier(httpClient, renderer)
	m.notifiers[database.ChannelTypeMattermost] = NewMattermostNotifier(httpClient, renderer)
	m.notifiers[database.ChannelTypeTelegram] = NewTelegramNotifier(httpClient, renderer)
	m.notifiers[database.ChannelTypeWebhook] = NewWebhookNotifier(httpClient, renderer)
	m.notifiers[database.ChannelTypeEmail] = newEmailNotifierWithDialer(serverSecret, renderer, dial)

	return m, nil
}

// SendAlertNotification implements NotificationManager.SendAlertNotification
// Queues notifications for all channels linked to the alert's connection
func (m *Manager) SendAlertNotification(ctx context.Context, alert *database.Alert, notifType database.NotificationType) error {
	if m == nil {
		return nil // notifications disabled
	}

	// Get channels linked to this connection
	channels, err := m.datastore.GetNotificationChannelsForConnection(ctx, alert.ConnectionID)
	if err != nil {
		return fmt.Errorf("failed to get notification channels: %w", err)
	}

	if len(channels) == 0 {
		m.debugLog("No notification channels configured for connection %d", alert.ConnectionID)
		return nil
	}

	// Get connection info for the payload
	serverName, serverHost, serverPort, err := m.connectionInfo(ctx, alert)
	if err != nil {
		m.log("WARNING: Failed to get connection info: %v", err)
		serverName = fmt.Sprintf("Connection %d", alert.ConnectionID)
		serverHost = "unknown"
		serverPort = 5432
	}

	// Build payload
	payload := m.buildPayload(alert, notifType, serverName, serverHost, serverPort)

	// Create every channel's history row before sending to any of
	// them, so that a slow channel cannot use up ctx before a later
	// channel has a row (issue #836). Each row is created owned by this
	// call for DeliveryTimeout, which keeps the pending-retry worker,
	// which runs on its own schedule, from claiming and sending it a
	// second time while the send below is under way.
	claimedAt := time.Now()
	type delivery struct {
		history *database.NotificationHistory
		channel *database.NotificationChannel
	}
	deliveries := make([]delivery, 0, len(channels))
	for _, channel := range channels {
		history := &database.NotificationHistory{
			AlertID:          &alert.ID,
			ChannelID:        &channel.ID,
			ConnectionID:     historyConnectionID(alert),
			NotificationType: notifType,
			Status:           database.NotificationStatusPending,
			AttemptCount:     0,
			MaxAttempts:      m.config.MaxRetryAttempts,
			CreatedAt:        time.Now(),
		}

		if err := m.datastore.CreateNotificationHistory(ctx, history, DeliveryTimeout); err != nil {
			m.log("ERROR: Failed to create notification history: %v", err)
			continue
		}
		deliveries = append(deliveries, delivery{history: history, channel: channel})
	}

	// Send to every channel at once, so that no channel waits on
	// another. The sends end in time for their outcomes to be recorded
	// while this call owns the rows; processNotification records each
	// with a context of its own, so a send that ran out of time becomes
	// a failed attempt for the retry worker like any other.
	sendCtx, cancel := attemptContext(ctx, claimedAt)
	defer cancel()
	var wg sync.WaitGroup
	for _, d := range deliveries {
		wg.Go(func() {
			m.processNotification(sendCtx, d.history, d.channel, payload)
		})
	}
	wg.Wait()

	return nil
}

// ProcessPendingNotifications implements
// NotificationManager.ProcessPendingNotifications. It claims due rows
// one at a time, each just before it is sent, so that every row's
// claim covers its own attempt however long the earlier ones took, and
// so that a row another attempt owns (an immediate send under way,
// say) is never sent (issue #866).
func (m *Manager) ProcessPendingNotifications(ctx context.Context) error {
	if m == nil {
		return nil
	}

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		claimedAt := time.Now()
		history, err := m.datastore.ClaimPendingNotification(ctx, DeliveryTimeout)
		if err != nil {
			return fmt.Errorf("failed to claim a pending notification: %w", err)
		}
		if history == nil {
			return nil
		}

		attemptCtx, cancel := attemptContext(ctx, claimedAt)
		m.retryNotification(attemptCtx, history)
		cancel()
	}
}

// retryNotification sends a pending notification that the retry worker
// has claimed. A row it cannot send, because its channel or alert is
// missing, keeps its claim and is tried again once the claim runs out.
func (m *Manager) retryNotification(ctx context.Context, history *database.NotificationHistory) {
	if history.ChannelID == nil || history.AlertID == nil {
		return
	}
	channel, err := m.datastore.GetNotificationChannel(ctx, *history.ChannelID)
	if err != nil {
		m.log("ERROR: Failed to get channel %d: %v", *history.ChannelID, err)
		return
	}
	alert, err := m.datastore.GetAlert(ctx, *history.AlertID)
	if err != nil {
		m.log("ERROR: Failed to get alert %d: %v", *history.AlertID, err)
		return
	}

	serverName, serverHost, serverPort, err := m.connectionInfo(ctx, alert)
	if err != nil {
		m.debugLog("Failed to get connection info for %d: %v", alert.ConnectionID, err)
	}

	payload := m.buildPayload(alert, history.NotificationType, serverName, serverHost, serverPort)
	m.processNotification(ctx, history, channel, payload)
}

// ProcessReminders implements NotificationManager.ProcessReminders
func (m *Manager) ProcessReminders(ctx context.Context) error {
	if m == nil {
		return nil
	}

	dueReminders, err := m.datastore.GetDueReminders(ctx)
	if err != nil {
		return fmt.Errorf("failed to get due reminders: %w", err)
	}

	for _, reminder := range dueReminders {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Get connection info
		serverName, serverHost, serverPort, err := m.connectionInfo(ctx, reminder.Alert)
		if err != nil {
			m.debugLog("Failed to get connection info for %d: %v", reminder.Alert.ConnectionID, err)
		}

		// Build payload with reminder count
		payload := m.buildPayload(reminder.Alert, database.NotificationTypeReminder, serverName, serverHost, serverPort)

		// Initialize state if nil (first reminder for this alert/channel)
		if reminder.State == nil {
			reminder.State = &database.NotificationReminderState{
				AlertID:       reminder.Alert.ID,
				ChannelID:     reminder.Channel.ID,
				ReminderCount: 0,
			}
		}

		payload.ReminderCount = reminder.State.ReminderCount + 1

		// Create the history record, owned by this attempt so that the
		// pending-retry worker does not send it as well.
		claimedAt := time.Now()
		history := &database.NotificationHistory{
			AlertID:          &reminder.Alert.ID,
			ChannelID:        &reminder.Channel.ID,
			ConnectionID:     historyConnectionID(reminder.Alert),
			NotificationType: database.NotificationTypeReminder,
			Status:           database.NotificationStatusPending,
			AttemptCount:     0,
			MaxAttempts:      m.config.MaxRetryAttempts,
			CreatedAt:        time.Now(),
		}

		if err := m.datastore.CreateNotificationHistory(ctx, history, DeliveryTimeout); err != nil {
			m.log("ERROR: Failed to create reminder history: %v", err)
			continue
		}

		attemptCtx, cancel := attemptContext(ctx, claimedAt)
		sent := m.processNotification(attemptCtx, history, reminder.Channel, payload)
		cancel()
		if sent {
			// Update reminder state
			reminder.State.LastReminderAt = time.Now()
			reminder.State.ReminderCount++
			if err := m.datastore.UpsertReminderState(ctx, reminder.State); err != nil {
				m.log("ERROR: Failed to update reminder state: %v", err)
			}
		}
	}

	return nil
}

// SystemAlertServerName is the server name a notification carries for a
// system alert, which concerns the alerter itself rather than a monitored
// server. See GitHub issue #582.
const SystemAlertServerName = "AI DBA Workbench alerter"

// hostname is os.Hostname, replaceable in tests.
var hostname = os.Hostname

// connectionInfo returns the server a notification about alert should
// name. A system alert has no connection, so it names the alerter and
// the host it runs on, with port 0, instead of looking a connection up.
func (m *Manager) connectionInfo(ctx context.Context, alert *database.Alert) (name, host string, port int, err error) {
	if alert.IsSystem() {
		host, err := hostname()
		if err != nil || host == "" {
			host = "localhost"
		}
		return SystemAlertServerName, host, 0, nil
	}
	return m.datastore.GetConnectionInfo(ctx, alert.ConnectionID)
}

// historyConnectionID is the connection a notification_history row for
// alert records: none for a system alert, whose connection_id is NULL.
func historyConnectionID(alert *database.Alert) *int {
	if alert.IsSystem() {
		return nil
	}
	id := alert.ConnectionID
	return &id
}

// processNotification sends a notification and updates history, whose
// row the caller owns: history.NextRetryAt on entry is the attempt's
// lease, which recording the outcome checks.
// Returns true if successful
func (m *Manager) processNotification(ctx context.Context, history *database.NotificationHistory, channel *database.NotificationChannel, payload *database.NotificationPayload) bool {
	lease := history.NextRetryAt
	history.AttemptCount++

	// Get notifier for channel type
	notifier, ok := m.notifiers[channel.ChannelType]
	if !ok {
		history.Status = database.NotificationStatusFailed
		errMsg := fmt.Sprintf("unknown channel type: %s", channel.ChannelType)
		history.ErrorMessage = &errMsg
		m.recordOutcome(ctx, history, lease)
		return false
	}

	// Decrypt sensitive fields
	m.decryptChannelSecrets(channel)

	// Each attempt has its own deadline, covering the recipient lookup
	// and the send, whatever ctx carries: the pending-retry and reminder
	// workers pass a context with none, and without one a stalled
	// datastore or endpoint would hold the worker indefinitely (the
	// guarded dial would also give each address its full dial timeout).
	sendCtx, cancel := context.WithTimeout(ctx, SendTimeout)

	// For email channels, fetch recipients
	if channel.ChannelType == database.ChannelTypeEmail {
		recipients, err := m.datastore.GetEmailRecipients(sendCtx, channel.ID)
		if err != nil {
			m.log("ERROR: Failed to get email recipients: %v", err)
		}
		channel.Recipients = recipients
	}

	// Send notification.
	err := notifier.Send(sendCtx, channel, payload)
	cancel()

	now := time.Now()
	if err == nil {
		history.Status = database.NotificationStatusSent
		history.SentAt = &now
		m.debugLog("Notification sent successfully to %s channel %s", channel.ChannelType, channel.Name)
	} else {
		m.log("ERROR: Failed to send notification: %v", err)
		errMsg := err.Error()
		history.ErrorMessage = &errMsg

		if history.AttemptCount >= history.MaxAttempts {
			history.Status = database.NotificationStatusFailed
		} else {
			history.Status = database.NotificationStatusRetrying
			// Calculate next retry time with exponential backoff
			backoffMinutes := m.getBackoffMinutes(history.AttemptCount)
			nextRetry := now.Add(time.Duration(backoffMinutes) * time.Minute)
			history.NextRetryAt = &nextRetry
		}
	}

	m.recordOutcome(ctx, history, lease)
	return err == nil
}

// recordOutcome writes history's outcome to its row, if the attempt's
// lease still holds it. The write runs on a context that ctx's
// cancellation and deadline do not reach, bounded by recordTimeout
// instead, so a send that used up ctx is still recorded rather than
// left for the retry worker to send again as if it had never been
// attempted. An attempt that outran its lease, so that another attempt
// has claimed the row since, leaves the row to that attempt.
func (m *Manager) recordOutcome(ctx context.Context, history *database.NotificationHistory, lease *time.Time) {
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)
	defer cancel()
	owned, err := m.datastore.UpdateNotificationHistory(recordCtx, history, lease)
	if err != nil {
		m.log("ERROR: Failed to update notification history: %v", err)
		return
	}
	if !owned {
		m.log("WARNING: Notification %d was claimed by another attempt before this attempt's outcome (%s) was recorded; the row keeps the other attempt's outcome",
			history.ID, history.Status)
	}
}

// buildPayload creates a NotificationPayload from an alert
func (m *Manager) buildPayload(alert *database.Alert, notifType database.NotificationType, serverName, serverHost string, serverPort int) *database.NotificationPayload {
	payload := &database.NotificationPayload{
		AlertID:          alert.ID,
		AlertType:        string(alert.AlertType),
		AlertTitle:       alert.Title,
		AlertDescription: alert.Description,
		Severity:         alert.Severity,
		Status:           alert.Status,
		TriggeredAt:      alert.TriggeredAt,
		ClearedAt:        alert.ClearedAt,
		ConnectionID:     alert.ConnectionID,
		ServerName:       serverName,
		ServerHost:       serverHost,
		ServerPort:       serverPort,
		DatabaseName:     alert.DatabaseName,
		NotificationType: string(notifType),
		Timestamp:        time.Now(),
	}

	// Add metric info if available
	if alert.MetricName != nil {
		payload.MetricName = alert.MetricName
	}
	if alert.MetricValue != nil {
		payload.MetricValue = alert.MetricValue
	}
	if alert.ThresholdValue != nil {
		payload.ThresholdValue = alert.ThresholdValue
	}
	if alert.Operator != nil {
		payload.Operator = alert.Operator
	}

	// Calculate duration if cleared
	if alert.ClearedAt != nil {
		duration := alert.ClearedAt.Sub(alert.TriggeredAt)
		durationStr := formatDuration(duration)
		payload.Duration = &durationStr
	}

	return payload
}

// decryptChannelSecrets decrypts sensitive fields in the channel: the
// Slack/Mattermost webhook URL, the webhook auth credentials, the SMTP
// password and the Telegram bot token. If decryption fails (e.g.
// plaintext or legacy data), the original value is kept.
func (m *Manager) decryptChannelSecrets(channel *database.NotificationChannel) {
	// Decrypt webhook URL for Slack/Mattermost
	if channel.WebhookURL != nil && *channel.WebhookURL != "" {
		decrypted, err := crypto.DecryptPassword(*channel.WebhookURL, m.serverSecret)
		if err == nil {
			channel.WebhookURL = &decrypted
		}
	}

	// Decrypt bot token for Telegram
	if channel.TelegramBotToken != nil && *channel.TelegramBotToken != "" {
		decrypted, err := crypto.DecryptPassword(*channel.TelegramBotToken, m.serverSecret)
		if err == nil {
			channel.TelegramBotToken = &decrypted
		}
	}

	// Decrypt auth credentials for webhooks
	if channel.AuthCredentials != nil && *channel.AuthCredentials != "" {
		decrypted, err := crypto.DecryptPassword(*channel.AuthCredentials, m.serverSecret)
		if err == nil {
			channel.AuthCredentials = &decrypted
		}
	}

	// Decrypt SMTP password for email
	if channel.SMTPPassword != nil && *channel.SMTPPassword != "" {
		decrypted, err := crypto.DecryptPassword(*channel.SMTPPassword, m.serverSecret)
		if err == nil {
			channel.SMTPPassword = &decrypted
		}
	}
}

// getBackoffMinutes returns the backoff duration for a retry attempt
func (m *Manager) getBackoffMinutes(attempt int) int {
	backoffs := m.config.RetryBackoffMinutes
	if len(backoffs) == 0 {
		backoffs = []int{5, 15, 60} // defaults
	}

	if attempt <= 0 {
		attempt = 1
	}
	idx := attempt - 1
	if idx >= len(backoffs) {
		idx = len(backoffs) - 1
	}
	return backoffs[idx]
}

// debugLog logs debug messages if debug mode is enabled
func (m *Manager) debugLog(format string, args ...any) {
	if m.debug && m.log != nil {
		m.log("DEBUG: "+format, args...)
	}
}
