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
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// Manager implements NotificationManager
type Manager struct {
	datastore  *database.Datastore
	config     *config.NotificationsConfig
	secrets    SecretManager
	renderer   TemplateRenderer
	notifiers  map[database.NotificationChannelType]Notifier
	httpClient *http.Client
	debug      bool

	// Logging function
	log func(format string, args ...interface{})
}

// NewManager creates a new notification manager
func NewManager(ds *database.Datastore, cfg *config.NotificationsConfig, debug bool, logFunc func(string, ...interface{})) (*Manager, error) {
	// Return nil if notifications are disabled
	if cfg == nil || !cfg.Enabled {
		return nil, nil
	}

	// Load secret key from file
	secretKey, err := LoadSecretKey(cfg.SecretFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load notification secret key: %w", err)
	}

	secrets, err := NewSecretManager(secretKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create secret manager: %w", err)
	}

	renderer := NewTemplateRenderer()

	httpClient := &http.Client{
		Timeout: time.Duration(cfg.HTTPTimeoutSeconds) * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:       cfg.HTTPMaxIdleConns,
			IdleConnTimeout:    30 * time.Second,
			DisableCompression: true,
		},
	}

	// Set defaults
	if httpClient.Timeout == 0 {
		httpClient.Timeout = 30 * time.Second
	}

	m := &Manager{
		datastore:  ds,
		config:     cfg,
		secrets:    secrets,
		renderer:   renderer,
		httpClient: httpClient,
		debug:      debug,
		log:        logFunc,
		notifiers:  make(map[database.NotificationChannelType]Notifier),
	}

	// Register notifiers
	m.notifiers[database.ChannelTypeSlack] = NewSlackNotifier(httpClient, secrets, renderer)
	m.notifiers[database.ChannelTypeMattermost] = NewMattermostNotifier(httpClient, secrets, renderer)
	m.notifiers[database.ChannelTypeWebhook] = NewWebhookNotifier(httpClient, secrets, renderer)
	m.notifiers[database.ChannelTypeEmail] = NewEmailNotifier(secrets, renderer)

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
	serverName, serverHost, serverPort, err := m.datastore.GetConnectionInfo(ctx, alert.ConnectionID)
	if err != nil {
		m.log("WARNING: Failed to get connection info: %v", err)
		serverName = fmt.Sprintf("Connection %d", alert.ConnectionID)
		serverHost = "unknown"
		serverPort = 5432
	}

	// Build payload
	payload := m.buildPayload(alert, notifType, serverName, serverHost, serverPort)

	// Queue notification for each channel
	for _, channel := range channels {
		// Create history record
		history := &database.NotificationHistory{
			AlertID:          &alert.ID,
			ChannelID:        &channel.ID,
			ConnectionID:     &alert.ConnectionID,
			NotificationType: notifType,
			Status:           database.NotificationStatusPending,
			AttemptCount:     0,
			MaxAttempts:      m.config.MaxRetryAttempts,
			CreatedAt:        time.Now(),
		}

		if err := m.datastore.CreateNotificationHistory(ctx, history); err != nil {
			m.log("ERROR: Failed to create notification history: %v", err)
			continue
		}

		// Try to send immediately
		m.processNotification(ctx, history, channel, payload)
	}

	return nil
}

// ProcessPendingNotifications implements NotificationManager.ProcessPendingNotifications
func (m *Manager) ProcessPendingNotifications(ctx context.Context) error {
	if m == nil {
		return nil
	}

	pending, err := m.datastore.GetPendingNotifications(ctx)
	if err != nil {
		return fmt.Errorf("failed to get pending notifications: %w", err)
	}

	for _, history := range pending {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Get channel
		if history.ChannelID == nil {
			continue
		}
		channel, err := m.datastore.GetNotificationChannel(ctx, *history.ChannelID)
		if err != nil {
			m.log("ERROR: Failed to get channel %d: %v", *history.ChannelID, err)
			continue
		}

		// Get alert for payload
		if history.AlertID == nil {
			continue
		}
		alert, err := m.datastore.GetAlert(ctx, *history.AlertID)
		if err != nil {
			m.log("ERROR: Failed to get alert %d: %v", *history.AlertID, err)
			continue
		}

		// Get connection info
		connectionID := alert.ConnectionID
		serverName, serverHost, serverPort, err := m.datastore.GetConnectionInfo(ctx, connectionID)
		if err != nil {
			m.debugLog("Failed to get connection info for %d: %v", connectionID, err)
		}

		// Build payload
		payload := m.buildPayload(alert, history.NotificationType, serverName, serverHost, serverPort)

		// Process
		m.processNotification(ctx, history, channel, payload)
	}

	return nil
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
		serverName, serverHost, serverPort, err := m.datastore.GetConnectionInfo(ctx, reminder.Alert.ConnectionID)
		if err != nil {
			m.debugLog("Failed to get connection info for %d: %v", reminder.Alert.ConnectionID, err)
		}

		// Build payload with reminder count
		payload := m.buildPayload(reminder.Alert, database.NotificationTypeReminder, serverName, serverHost, serverPort)
		payload.ReminderCount = reminder.State.ReminderCount + 1

		// Create history record
		history := &database.NotificationHistory{
			AlertID:          &reminder.Alert.ID,
			ChannelID:        &reminder.Channel.ID,
			ConnectionID:     &reminder.Alert.ConnectionID,
			NotificationType: database.NotificationTypeReminder,
			Status:           database.NotificationStatusPending,
			AttemptCount:     0,
			MaxAttempts:      m.config.MaxRetryAttempts,
			CreatedAt:        time.Now(),
		}

		if err := m.datastore.CreateNotificationHistory(ctx, history); err != nil {
			m.log("ERROR: Failed to create reminder history: %v", err)
			continue
		}

		// Process notification
		if m.processNotification(ctx, history, reminder.Channel, payload) {
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

// processNotification sends a notification and updates history
// Returns true if successful
func (m *Manager) processNotification(ctx context.Context, history *database.NotificationHistory, channel *database.NotificationChannel, payload *database.NotificationPayload) bool {
	history.AttemptCount++

	// Get notifier for channel type
	notifier, ok := m.notifiers[channel.ChannelType]
	if !ok {
		history.Status = database.NotificationStatusFailed
		errMsg := fmt.Sprintf("unknown channel type: %s", channel.ChannelType)
		history.ErrorMessage = &errMsg
		if err := m.datastore.UpdateNotificationHistory(ctx, history); err != nil {
			m.log("ERROR: Failed to update notification history: %v", err)
		}
		return false
	}

	// Decrypt sensitive fields
	m.decryptChannelSecrets(channel)

	// For email channels, fetch recipients
	if channel.ChannelType == database.ChannelTypeEmail {
		recipients, err := m.datastore.GetEmailRecipients(ctx, channel.ID)
		if err != nil {
			m.log("ERROR: Failed to get email recipients: %v", err)
		}
		channel.Recipients = recipients
	}

	// Send notification
	err := notifier.Send(ctx, channel, payload)

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

	if updateErr := m.datastore.UpdateNotificationHistory(ctx, history); updateErr != nil {
		m.log("ERROR: Failed to update notification history: %v", updateErr)
	}
	return err == nil
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

// decryptChannelSecrets decrypts sensitive fields in the channel
func (m *Manager) decryptChannelSecrets(channel *database.NotificationChannel) {
	// Decrypt webhook URL for Slack/Mattermost
	if channel.WebhookURL != nil && *channel.WebhookURL != "" {
		decrypted, err := m.secrets.Decrypt(*channel.WebhookURL)
		if err == nil {
			channel.WebhookURL = &decrypted
		}
	}

	// Decrypt auth credentials for webhooks
	if channel.AuthCredentials != nil && *channel.AuthCredentials != "" {
		decrypted, err := m.secrets.Decrypt(*channel.AuthCredentials)
		if err == nil {
			channel.AuthCredentials = &decrypted
		}
	}

	// Decrypt SMTP password for email
	if channel.SMTPPassword != nil && *channel.SMTPPassword != "" {
		decrypted, err := m.secrets.Decrypt(*channel.SMTPPassword)
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
func (m *Manager) debugLog(format string, args ...interface{}) {
	if m.debug && m.log != nil {
		m.log("DEBUG: "+format, args...)
	}
}
