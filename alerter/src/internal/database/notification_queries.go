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
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// DueReminder represents an alert and channel that needs a reminder notification
type DueReminder struct {
	Alert   *Alert
	Channel *NotificationChannel
	State   *NotificationReminderState
}

// GetNotificationChannel retrieves a notification channel by ID
func (d *Datastore) GetNotificationChannel(ctx context.Context, id int64) (*NotificationChannel, error) {
	var channel NotificationChannel
	err := d.pool.QueryRow(ctx, `
        SELECT id, owner_username, owner_token, enabled, channel_type,
               name, description, webhook_url_encrypted AS webhook_url,
               endpoint_url, http_method,
               headers_json AS headers, auth_type,
               auth_credentials_encrypted AS auth_credentials,
               smtp_host, smtp_port, smtp_username,
               smtp_password_encrypted AS smtp_password,
               smtp_use_tls, from_address, from_name,
               template_alert_fire, template_alert_clear, template_reminder,
               reminder_enabled, reminder_interval_hours, is_estate_default,
               created_at, updated_at
        FROM notification_channels
        WHERE id = $1
    `, id).Scan(
		&channel.ID, &channel.OwnerUsername, &channel.OwnerToken,
		&channel.Enabled, &channel.ChannelType, &channel.Name, &channel.Description,
		&channel.WebhookURL, &channel.EndpointURL, &channel.HTTPMethod, &channel.Headers,
		&channel.AuthType, &channel.AuthCredentials, &channel.SMTPHost, &channel.SMTPPort,
		&channel.SMTPUsername, &channel.SMTPPassword, &channel.SMTPUseTLS,
		&channel.FromAddress, &channel.FromName, &channel.TemplateAlertFire,
		&channel.TemplateAlertClear, &channel.TemplateReminder, &channel.ReminderEnabled,
		&channel.ReminderIntervalHours, &channel.IsEstateDefault, &channel.CreatedAt,
		&channel.UpdatedAt)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("notification channel not found: %w", err)
		}
		return nil, fmt.Errorf("failed to get notification channel: %w", err)
	}
	return &channel, nil
}

// GetNotificationChannelsForConnection retrieves all enabled channels for a connection
// using hierarchical resolution: server override > cluster override > group override > estate default
func (d *Datastore) GetNotificationChannelsForConnection(ctx context.Context, connectionID int) ([]*NotificationChannel, error) {
	rows, err := d.pool.Query(ctx, `
        SELECT nc.id, nc.owner_username, nc.owner_token, nc.enabled,
               nc.channel_type, nc.name, nc.description,
               nc.webhook_url_encrypted AS webhook_url, nc.endpoint_url,
               nc.http_method, nc.headers_json AS headers,
               nc.auth_type, nc.auth_credentials_encrypted AS auth_credentials,
               nc.smtp_host, nc.smtp_port, nc.smtp_username,
               nc.smtp_password_encrypted AS smtp_password,
               nc.smtp_use_tls, nc.from_address, nc.from_name, nc.template_alert_fire,
               nc.template_alert_clear, nc.template_reminder, nc.reminder_enabled,
               nc.reminder_interval_hours, nc.is_estate_default, nc.created_at, nc.updated_at
        FROM notification_channels nc
        LEFT JOIN notification_channel_overrides svr
            ON svr.channel_id = nc.id
            AND svr.scope = 'server' AND svr.connection_id = $1
        LEFT JOIN notification_channel_overrides cls
            ON cls.channel_id = nc.id
            AND cls.scope = 'cluster'
            AND cls.cluster_id = (
                SELECT cluster_id FROM connections WHERE id = $1)
        LEFT JOIN notification_channel_overrides grp
            ON grp.channel_id = nc.id
            AND grp.scope = 'group'
            AND grp.group_id = (
                SELECT cl.group_id FROM connections c
                JOIN clusters cl ON c.cluster_id = cl.id
                WHERE c.id = $1)
        WHERE nc.enabled = true
          AND COALESCE(svr.enabled, cls.enabled,
                       grp.enabled, nc.is_estate_default) = true
        ORDER BY nc.name
    `, connectionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get notification channels for connection: %w", err)
	}
	defer rows.Close()

	var channels []*NotificationChannel
	for rows.Next() {
		var channel NotificationChannel
		err := rows.Scan(
			&channel.ID, &channel.OwnerUsername, &channel.OwnerToken,
			&channel.Enabled, &channel.ChannelType, &channel.Name, &channel.Description,
			&channel.WebhookURL, &channel.EndpointURL, &channel.HTTPMethod, &channel.Headers,
			&channel.AuthType, &channel.AuthCredentials, &channel.SMTPHost, &channel.SMTPPort,
			&channel.SMTPUsername, &channel.SMTPPassword, &channel.SMTPUseTLS,
			&channel.FromAddress, &channel.FromName, &channel.TemplateAlertFire,
			&channel.TemplateAlertClear, &channel.TemplateReminder, &channel.ReminderEnabled,
			&channel.ReminderIntervalHours, &channel.IsEstateDefault, &channel.CreatedAt,
			&channel.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan notification channel: %w", err)
		}
		channels = append(channels, &channel)
	}

	return channels, nil
}

// CreateNotificationChannel inserts a new notification channel
func (d *Datastore) CreateNotificationChannel(ctx context.Context, channel *NotificationChannel) error {
	return d.pool.QueryRow(ctx, `
        INSERT INTO notification_channels (
            owner_username, owner_token, enabled, channel_type, name,
            description, webhook_url_encrypted, endpoint_url, http_method,
            headers_json, auth_type, auth_credentials_encrypted, smtp_host,
            smtp_port, smtp_username, smtp_password_encrypted,
            smtp_use_tls, from_address, from_name, template_alert_fire,
            template_alert_clear, template_reminder, reminder_enabled,
            reminder_interval_hours, is_estate_default, created_at, updated_at
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
                  $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27)
        RETURNING id
    `, channel.OwnerUsername, channel.OwnerToken, channel.Enabled,
		channel.ChannelType, channel.Name, channel.Description, channel.WebhookURL,
		channel.EndpointURL, channel.HTTPMethod, channel.Headers, channel.AuthType,
		channel.AuthCredentials, channel.SMTPHost, channel.SMTPPort, channel.SMTPUsername,
		channel.SMTPPassword, channel.SMTPUseTLS, channel.FromAddress, channel.FromName,
		channel.TemplateAlertFire, channel.TemplateAlertClear, channel.TemplateReminder,
		channel.ReminderEnabled, channel.ReminderIntervalHours, channel.IsEstateDefault,
		channel.CreatedAt, channel.UpdatedAt).Scan(&channel.ID)
}

// UpdateNotificationChannel updates an existing notification channel
func (d *Datastore) UpdateNotificationChannel(ctx context.Context, channel *NotificationChannel) error {
	_, err := d.pool.Exec(ctx, `
        UPDATE notification_channels
        SET owner_username = $2, owner_token = $3, enabled = $4,
            channel_type = $5, name = $6, description = $7,
            webhook_url_encrypted = $8,
            endpoint_url = $9, http_method = $10, headers_json = $11,
            auth_type = $12, auth_credentials_encrypted = $13,
            smtp_host = $14, smtp_port = $15,
            smtp_username = $16, smtp_password_encrypted = $17,
            smtp_use_tls = $18,
            from_address = $19, from_name = $20, template_alert_fire = $21,
            template_alert_clear = $22, template_reminder = $23,
            reminder_enabled = $24, reminder_interval_hours = $25,
            is_estate_default = $26, updated_at = $27
        WHERE id = $1
    `, channel.ID, channel.OwnerUsername, channel.OwnerToken,
		channel.Enabled, channel.ChannelType, channel.Name, channel.Description,
		channel.WebhookURL, channel.EndpointURL, channel.HTTPMethod, channel.Headers,
		channel.AuthType, channel.AuthCredentials, channel.SMTPHost, channel.SMTPPort,
		channel.SMTPUsername, channel.SMTPPassword, channel.SMTPUseTLS,
		channel.FromAddress, channel.FromName, channel.TemplateAlertFire,
		channel.TemplateAlertClear, channel.TemplateReminder, channel.ReminderEnabled,
		channel.ReminderIntervalHours, channel.IsEstateDefault, channel.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to update notification channel: %w", err)
	}
	return nil
}

// DeleteNotificationChannel deletes a notification channel
func (d *Datastore) DeleteNotificationChannel(ctx context.Context, id int64) error {
	_, err := d.pool.Exec(ctx, `DELETE FROM notification_channels WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete notification channel: %w", err)
	}
	return nil
}

// GetEmailRecipients retrieves all enabled email recipients for a channel
func (d *Datastore) GetEmailRecipients(ctx context.Context, channelID int64) ([]*EmailRecipient, error) {
	rows, err := d.pool.Query(ctx, `
        SELECT id, channel_id, email_address, display_name, enabled, created_at
        FROM email_recipients
        WHERE channel_id = $1 AND enabled = true
        ORDER BY email_address
    `, channelID)
	if err != nil {
		return nil, fmt.Errorf("failed to get email recipients: %w", err)
	}
	defer rows.Close()

	var recipients []*EmailRecipient
	for rows.Next() {
		var recipient EmailRecipient
		err := rows.Scan(&recipient.ID, &recipient.ChannelID, &recipient.EmailAddress,
			&recipient.DisplayName, &recipient.Enabled, &recipient.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan email recipient: %w", err)
		}
		recipients = append(recipients, &recipient)
	}

	return recipients, nil
}

// CreateEmailRecipient inserts a new email recipient
func (d *Datastore) CreateEmailRecipient(ctx context.Context, recipient *EmailRecipient) error {
	return d.pool.QueryRow(ctx, `
        INSERT INTO email_recipients (channel_id, email_address, display_name, enabled, created_at)
        VALUES ($1, $2, $3, $4, $5)
        RETURNING id
    `, recipient.ChannelID, recipient.EmailAddress, recipient.DisplayName,
		recipient.Enabled, recipient.CreatedAt).Scan(&recipient.ID)
}

// DeleteEmailRecipient deletes an email recipient
func (d *Datastore) DeleteEmailRecipient(ctx context.Context, id int64) error {
	_, err := d.pool.Exec(ctx, `DELETE FROM email_recipients WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete email recipient: %w", err)
	}
	return nil
}

// LinkConnectionToChannel creates a link between a connection and notification channel
func (d *Datastore) LinkConnectionToChannel(ctx context.Context, link *ConnectionNotificationChannel) error {
	return d.pool.QueryRow(ctx, `
        INSERT INTO connection_notification_channels (
            connection_id, channel_id, enabled, reminder_enabled_override,
            reminder_interval_hours_override, created_at
        ) VALUES ($1, $2, $3, $4, $5, $6)
        RETURNING id
    `, link.ConnectionID, link.ChannelID, link.Enabled, link.ReminderEnabledOverride,
		link.ReminderIntervalHoursOverride, link.CreatedAt).Scan(&link.ID)
}

// UnlinkConnectionFromChannel removes the link between a connection and notification channel
func (d *Datastore) UnlinkConnectionFromChannel(ctx context.Context, connectionID int, channelID int64) error {
	_, err := d.pool.Exec(ctx, `
        DELETE FROM connection_notification_channels
        WHERE connection_id = $1 AND channel_id = $2
    `, connectionID, channelID)
	if err != nil {
		return fmt.Errorf("failed to unlink connection from channel: %w", err)
	}
	return nil
}

// GetConnectionChannelLinks retrieves all notification channel links for a connection
func (d *Datastore) GetConnectionChannelLinks(ctx context.Context, connectionID int) ([]*ConnectionNotificationChannel, error) {
	rows, err := d.pool.Query(ctx, `
        SELECT id, connection_id, channel_id, enabled, reminder_enabled_override,
               reminder_interval_hours_override, created_at
        FROM connection_notification_channels
        WHERE connection_id = $1
        ORDER BY id
    `, connectionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection channel links: %w", err)
	}
	defer rows.Close()

	var links []*ConnectionNotificationChannel
	for rows.Next() {
		var link ConnectionNotificationChannel
		err := rows.Scan(&link.ID, &link.ConnectionID, &link.ChannelID, &link.Enabled,
			&link.ReminderEnabledOverride, &link.ReminderIntervalHoursOverride, &link.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan connection channel link: %w", err)
		}
		links = append(links, &link)
	}

	return links, nil
}

// CreateNotificationHistory inserts a new notification history record
func (d *Datastore) CreateNotificationHistory(ctx context.Context, history *NotificationHistory) error {
	return d.pool.QueryRow(ctx, `
        INSERT INTO notification_history (
            alert_id, channel_id, connection_id, notification_type, status,
            payload_json, response_code, response_body, error_message,
            attempt_count, max_attempts, next_retry_at, created_at, sent_at
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
        RETURNING id
    `, history.AlertID, history.ChannelID, history.ConnectionID, history.NotificationType,
		history.Status, history.PayloadJSON, history.ResponseCode, history.ResponseBody,
		history.ErrorMessage, history.AttemptCount, history.MaxAttempts, history.NextRetryAt,
		history.CreatedAt, history.SentAt).Scan(&history.ID)
}

// UpdateNotificationHistory updates a notification history record
func (d *Datastore) UpdateNotificationHistory(ctx context.Context, history *NotificationHistory) error {
	_, err := d.pool.Exec(ctx, `
        UPDATE notification_history
        SET status = $2, response_code = $3, response_body = $4, error_message = $5,
            attempt_count = $6, next_retry_at = $7, sent_at = $8
        WHERE id = $1
    `, history.ID, history.Status, history.ResponseCode, history.ResponseBody,
		history.ErrorMessage, history.AttemptCount, history.NextRetryAt, history.SentAt)
	if err != nil {
		return fmt.Errorf("failed to update notification history: %w", err)
	}
	return nil
}

// GetPendingNotifications retrieves all notifications with status pending or retrying
// where next_retry_at is in the past or null
func (d *Datastore) GetPendingNotifications(ctx context.Context) ([]*NotificationHistory, error) {
	rows, err := d.pool.Query(ctx, `
        SELECT id, alert_id, channel_id, connection_id, notification_type, status,
               payload_json, response_code, response_body, error_message,
               attempt_count, max_attempts, next_retry_at, created_at, sent_at
        FROM notification_history
        WHERE status IN ('pending', 'retrying')
          AND (next_retry_at IS NULL OR next_retry_at <= NOW())
        ORDER BY created_at ASC
    `)
	if err != nil {
		return nil, fmt.Errorf("failed to get pending notifications: %w", err)
	}
	defer rows.Close()

	var notifications []*NotificationHistory
	for rows.Next() {
		var n NotificationHistory
		err := rows.Scan(&n.ID, &n.AlertID, &n.ChannelID, &n.ConnectionID,
			&n.NotificationType, &n.Status, &n.PayloadJSON, &n.ResponseCode,
			&n.ResponseBody, &n.ErrorMessage, &n.AttemptCount, &n.MaxAttempts,
			&n.NextRetryAt, &n.CreatedAt, &n.SentAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan notification history: %w", err)
		}
		notifications = append(notifications, &n)
	}

	return notifications, nil
}

// GetNotificationHistoryForAlert retrieves all notification history for an alert
func (d *Datastore) GetNotificationHistoryForAlert(ctx context.Context, alertID int64) ([]*NotificationHistory, error) {
	rows, err := d.pool.Query(ctx, `
        SELECT id, alert_id, channel_id, connection_id, notification_type, status,
               payload_json, response_code, response_body, error_message,
               attempt_count, max_attempts, next_retry_at, created_at, sent_at
        FROM notification_history
        WHERE alert_id = $1
        ORDER BY created_at DESC
    `, alertID)
	if err != nil {
		return nil, fmt.Errorf("failed to get notification history for alert: %w", err)
	}
	defer rows.Close()

	var notifications []*NotificationHistory
	for rows.Next() {
		var n NotificationHistory
		err := rows.Scan(&n.ID, &n.AlertID, &n.ChannelID, &n.ConnectionID,
			&n.NotificationType, &n.Status, &n.PayloadJSON, &n.ResponseCode,
			&n.ResponseBody, &n.ErrorMessage, &n.AttemptCount, &n.MaxAttempts,
			&n.NextRetryAt, &n.CreatedAt, &n.SentAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan notification history: %w", err)
		}
		notifications = append(notifications, &n)
	}

	return notifications, nil
}

// GetReminderState retrieves the reminder state for an alert and channel
func (d *Datastore) GetReminderState(ctx context.Context, alertID int64, channelID int64) (*NotificationReminderState, error) {
	var state NotificationReminderState
	err := d.pool.QueryRow(ctx, `
        SELECT id, alert_id, channel_id, last_reminder_at, reminder_count
        FROM notification_reminder_state
        WHERE alert_id = $1 AND channel_id = $2
    `, alertID, channelID).Scan(&state.ID, &state.AlertID, &state.ChannelID,
		&state.LastReminderAt, &state.ReminderCount)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get reminder state: %w", err)
	}
	return &state, nil
}

// UpsertReminderState inserts or updates a reminder state record
func (d *Datastore) UpsertReminderState(ctx context.Context, state *NotificationReminderState) error {
	err := d.pool.QueryRow(ctx, `
        INSERT INTO notification_reminder_state (alert_id, channel_id, last_reminder_at, reminder_count)
        VALUES ($1, $2, $3, $4)
        ON CONFLICT (alert_id, channel_id)
        DO UPDATE SET last_reminder_at = $3, reminder_count = $4
        RETURNING id
    `, state.AlertID, state.ChannelID, state.LastReminderAt, state.ReminderCount).Scan(&state.ID)
	if err != nil {
		return fmt.Errorf("failed to upsert reminder state: %w", err)
	}
	return nil
}

// DeleteReminderStatesForAlert deletes all reminder states for an alert
func (d *Datastore) DeleteReminderStatesForAlert(ctx context.Context, alertID int64) error {
	_, err := d.pool.Exec(ctx, `
        DELETE FROM notification_reminder_state WHERE alert_id = $1
    `, alertID)
	if err != nil {
		return fmt.Errorf("failed to delete reminder states for alert: %w", err)
	}
	return nil
}

// GetDueReminders retrieves alerts that need reminder notifications
func (d *Datastore) GetDueReminders(ctx context.Context) ([]DueReminder, error) {
	rows, err := d.pool.Query(ctx, `
        SELECT
            -- Alert fields
            a.id, a.alert_type, a.rule_id, a.connection_id, a.database_name,
            a.probe_name, a.metric_name, a.metric_value, a.threshold_value,
            a.operator, a.severity, a.title, a.description, a.correlation_id,
            a.status, a.triggered_at, a.cleared_at, a.anomaly_score, a.anomaly_details,
            -- Channel fields
            nc.id, nc.owner_username, nc.owner_token, nc.enabled,
            nc.channel_type, nc.name, nc.description,
            nc.webhook_url_encrypted AS webhook_url, nc.endpoint_url,
            nc.http_method, nc.headers_json AS headers,
            nc.auth_type, nc.auth_credentials_encrypted AS auth_credentials,
            nc.smtp_host, nc.smtp_port, nc.smtp_username,
            nc.smtp_password_encrypted AS smtp_password,
            nc.smtp_use_tls, nc.from_address, nc.from_name, nc.template_alert_fire,
            nc.template_alert_clear, nc.template_reminder, nc.reminder_enabled,
            nc.reminder_interval_hours, nc.is_estate_default, nc.created_at, nc.updated_at,
            -- Reminder state fields (may be NULL for first reminder)
            nrs.id, nrs.alert_id, nrs.channel_id, nrs.last_reminder_at, nrs.reminder_count
        FROM alerts a
        JOIN notification_channels nc ON nc.enabled = true
        LEFT JOIN notification_channel_overrides svr
            ON svr.channel_id = nc.id
            AND svr.scope = 'server' AND svr.connection_id = a.connection_id
        LEFT JOIN notification_channel_overrides cls
            ON cls.channel_id = nc.id
            AND cls.scope = 'cluster'
            AND cls.cluster_id = (
                SELECT cluster_id FROM connections WHERE id = a.connection_id)
        LEFT JOIN notification_channel_overrides grp
            ON grp.channel_id = nc.id
            AND grp.scope = 'group'
            AND grp.group_id = (
                SELECT cl.group_id FROM connections c
                JOIN clusters cl ON c.cluster_id = cl.id
                WHERE c.id = a.connection_id)
        LEFT JOIN notification_reminder_state nrs ON a.id = nrs.alert_id AND nc.id = nrs.channel_id
        WHERE a.status = 'active'
          AND nc.enabled = true
          AND COALESCE(svr.enabled, cls.enabled,
                       grp.enabled, nc.is_estate_default) = true
          AND nc.reminder_enabled = true
          AND (
              -- No reminder sent yet and alert is old enough
              (nrs.id IS NULL AND a.triggered_at <= NOW() - INTERVAL '1 hour' *
                  nc.reminder_interval_hours)
              -- Or last reminder was long enough ago
              OR (nrs.last_reminder_at <= NOW() - INTERVAL '1 hour' *
                  nc.reminder_interval_hours)
          )
        ORDER BY a.triggered_at ASC
    `)
	if err != nil {
		return nil, fmt.Errorf("failed to get due reminders: %w", err)
	}
	defer rows.Close()

	var reminders []DueReminder
	for rows.Next() {
		var alert Alert
		var channel NotificationChannel
		var stateID, stateAlertID, stateChannelID *int64
		var stateLastReminderAt *time.Time
		var stateReminderCount *int

		err := rows.Scan(
			// Alert fields
			&alert.ID, &alert.AlertType, &alert.RuleID, &alert.ConnectionID,
			&alert.DatabaseName, &alert.ProbeName, &alert.MetricName, &alert.MetricValue,
			&alert.ThresholdValue, &alert.Operator, &alert.Severity, &alert.Title,
			&alert.Description, &alert.CorrelationID, &alert.Status, &alert.TriggeredAt,
			&alert.ClearedAt, &alert.AnomalyScore, &alert.AnomalyDetails,
			// Channel fields
			&channel.ID, &channel.OwnerUsername, &channel.OwnerToken,
			&channel.Enabled, &channel.ChannelType, &channel.Name, &channel.Description,
			&channel.WebhookURL, &channel.EndpointURL, &channel.HTTPMethod, &channel.Headers,
			&channel.AuthType, &channel.AuthCredentials, &channel.SMTPHost, &channel.SMTPPort,
			&channel.SMTPUsername, &channel.SMTPPassword, &channel.SMTPUseTLS,
			&channel.FromAddress, &channel.FromName, &channel.TemplateAlertFire,
			&channel.TemplateAlertClear, &channel.TemplateReminder, &channel.ReminderEnabled,
			&channel.ReminderIntervalHours, &channel.IsEstateDefault, &channel.CreatedAt,
			&channel.UpdatedAt,
			// Reminder state fields
			&stateID, &stateAlertID, &stateChannelID, &stateLastReminderAt, &stateReminderCount,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan due reminder: %w", err)
		}

		var state *NotificationReminderState
		if stateID != nil {
			state = &NotificationReminderState{
				ID:             *stateID,
				AlertID:        *stateAlertID,
				ChannelID:      *stateChannelID,
				LastReminderAt: *stateLastReminderAt,
				ReminderCount:  *stateReminderCount,
			}
		}

		reminders = append(reminders, DueReminder{
			Alert:   &alert,
			Channel: &channel,
			State:   state,
		})
	}

	return reminders, nil
}

// GetConnectionInfo retrieves basic connection info for notification payloads
func (d *Datastore) GetConnectionInfo(ctx context.Context, connectionID int) (name, host string, port int, err error) {
	err = d.pool.QueryRow(ctx, `
        SELECT name, host, port
        FROM connections
        WHERE id = $1
    `, connectionID).Scan(&name, &host, &port)

	if err != nil {
		if err == pgx.ErrNoRows {
			return "", "", 0, fmt.Errorf("connection not found: %w", err)
		}
		return "", "", 0, fmt.Errorf("failed to get connection info: %w", err)
	}
	return name, host, port, nil
}
