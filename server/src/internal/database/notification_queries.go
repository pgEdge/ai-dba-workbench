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
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pgedge/ai-workbench/pkg/crypto"
)

// NotificationChannelType represents the type of notification channel
type NotificationChannelType string

const (
	ChannelTypeSlack      NotificationChannelType = "slack"
	ChannelTypeMattermost NotificationChannelType = "mattermost"
	ChannelTypeWebhook    NotificationChannelType = "webhook"
	ChannelTypeEmail      NotificationChannelType = "email"
)

// ValidChannelTypes contains all valid notification channel type values
var ValidChannelTypes = map[string]bool{
	string(ChannelTypeSlack):      true,
	string(ChannelTypeMattermost): true,
	string(ChannelTypeWebhook):    true,
	string(ChannelTypeEmail):      true,
}

// Sentinel errors for notification channel operations
var (
	ErrNotificationChannelNotFound = errors.New("notification channel not found")
	ErrEmailRecipientNotFound      = errors.New("email recipient not found")
)

// NotificationChannel represents a notification channel configuration
type NotificationChannel struct {
	ID            int64                   `json:"id"`
	OwnerUsername *string                 `json:"owner_username,omitempty"`
	OwnerToken    *string                 `json:"owner_token,omitempty"`
	Enabled       bool                    `json:"enabled"`
	ChannelType   NotificationChannelType `json:"channel_type"`
	Name          string                  `json:"name"`
	Description   *string                 `json:"description,omitempty"`

	// Slack/Mattermost
	WebhookURL *string `json:"webhook_url,omitempty"`

	// Webhook specific
	EndpointURL     *string           `json:"endpoint_url,omitempty"`
	HTTPMethod      string            `json:"http_method"`
	Headers         map[string]string `json:"headers,omitempty"`
	AuthType        *string           `json:"auth_type,omitempty"`
	AuthCredentials *string           `json:"auth_credentials,omitempty"`

	// Email specific
	SMTPHost     *string `json:"smtp_host,omitempty"`
	SMTPPort     int     `json:"smtp_port"`
	SMTPUsername *string `json:"smtp_username,omitempty"`
	SMTPPassword *string `json:"smtp_password,omitempty"`
	SMTPUseTLS   bool    `json:"smtp_use_tls"`
	FromAddress  *string `json:"from_address,omitempty"`
	FromName     *string `json:"from_name,omitempty"`

	// Templates
	TemplateAlertFire  *string `json:"template_alert_fire,omitempty"`
	TemplateAlertClear *string `json:"template_alert_clear,omitempty"`
	TemplateReminder   *string `json:"template_reminder,omitempty"`

	// Reminder settings
	ReminderEnabled       bool `json:"reminder_enabled"`
	ReminderIntervalHours int  `json:"reminder_interval_hours"`

	IsEstateDefault bool `json:"is_estate_default"`

	// Recipients - populated for email channels
	Recipients []*EmailRecipient `json:"recipients,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// EmailRecipient represents an email recipient in a channel
type EmailRecipient struct {
	ID           int64     `json:"id"`
	ChannelID    int64     `json:"channel_id"`
	EmailAddress string    `json:"email_address"`
	DisplayName  *string   `json:"display_name,omitempty"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
}

// encryptNotificationSecret encrypts a notification channel secret field.
func (d *Datastore) encryptNotificationSecret(value *string) (*string, error) {
	if value == nil || *value == "" {
		return value, nil
	}
	if d.serverSecret == "" {
		return nil, fmt.Errorf("server secret is required to encrypt notification secrets")
	}
	encrypted, err := crypto.EncryptPassword(*value, d.serverSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt: %w", err)
	}
	return &encrypted, nil
}

// decryptNotificationSecret decrypts a notification channel secret field.
// If decryption fails (e.g. the value is plaintext from before encryption
// was enabled), the original value is returned for backwards compatibility.
func (d *Datastore) decryptNotificationSecret(value *string) *string {
	if value == nil || *value == "" {
		return value
	}
	if d.serverSecret == "" {
		return value
	}
	decrypted, err := crypto.DecryptPassword(*value, d.serverSecret)
	if err != nil {
		return value
	}
	return &decrypted
}

// decryptNotificationChannelSecrets decrypts all secret fields on a
// notification channel in place.
func (d *Datastore) decryptNotificationChannelSecrets(c *NotificationChannel) {
	c.WebhookURL = d.decryptNotificationSecret(c.WebhookURL)
	c.AuthCredentials = d.decryptNotificationSecret(c.AuthCredentials)
	c.SMTPPassword = d.decryptNotificationSecret(c.SMTPPassword)
}

// ListNotificationChannels returns all notification channels ordered by name.
// For email channels, recipients are loaded and attached.
func (d *Datastore) ListNotificationChannels(ctx context.Context) ([]*NotificationChannel, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.pool.Query(ctx, `
        SELECT id, owner_username, owner_token, enabled, channel_type,
               name, description, webhook_url_encrypted, endpoint_url, http_method,
               headers_json, auth_type, auth_credentials_encrypted, smtp_host,
               smtp_port, smtp_username, smtp_password_encrypted, smtp_use_tls,
               from_address, from_name, template_alert_fire, template_alert_clear,
               template_reminder, reminder_enabled, reminder_interval_hours,
               is_estate_default, created_at, updated_at
        FROM notification_channels
        ORDER BY name
    `)
	if err != nil {
		return nil, fmt.Errorf("failed to query notification channels: %w", err)
	}
	defer rows.Close()

	var channels []*NotificationChannel
	for rows.Next() {
		c, scanErr := scanNotificationChannel(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("failed to scan notification channel: %w", scanErr)
		}
		d.decryptNotificationChannelSecrets(c)
		channels = append(channels, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating notification channels: %w", err)
	}
	if channels == nil {
		channels = []*NotificationChannel{}
	}

	// Load recipients for email channels
	for _, ch := range channels {
		if ch.ChannelType == ChannelTypeEmail {
			recipients, recipErr := d.listEmailRecipientsLocked(ctx, ch.ID)
			if recipErr != nil {
				return nil, fmt.Errorf("failed to load recipients for channel %d: %w", ch.ID, recipErr)
			}
			ch.Recipients = recipients
		}
	}

	return channels, nil
}

// GetNotificationChannel returns a single notification channel by ID.
// For email channels, recipients are loaded and attached.
func (d *Datastore) GetNotificationChannel(ctx context.Context, id int64) (*NotificationChannel, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	row := d.pool.QueryRow(ctx, `
        SELECT id, owner_username, owner_token, enabled, channel_type,
               name, description, webhook_url_encrypted, endpoint_url, http_method,
               headers_json, auth_type, auth_credentials_encrypted, smtp_host,
               smtp_port, smtp_username, smtp_password_encrypted, smtp_use_tls,
               from_address, from_name, template_alert_fire, template_alert_clear,
               template_reminder, reminder_enabled, reminder_interval_hours,
               is_estate_default, created_at, updated_at
        FROM notification_channels
        WHERE id = $1
    `, id)

	c, err := scanNotificationChannelRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotificationChannelNotFound
		}
		return nil, fmt.Errorf("failed to get notification channel: %w", err)
	}
	d.decryptNotificationChannelSecrets(c)

	// Load recipients for email channels
	if c.ChannelType == ChannelTypeEmail {
		recipients, recipErr := d.listEmailRecipientsLocked(ctx, c.ID)
		if recipErr != nil {
			return nil, fmt.Errorf("failed to load recipients for channel %d: %w", c.ID, recipErr)
		}
		c.Recipients = recipients
	}

	return c, nil
}

// CreateNotificationChannel inserts a new notification channel and sets its ID
// via RETURNING. The caller should set OwnerUsername before calling this method.
func (d *Datastore) CreateNotificationChannel(ctx context.Context, channel *NotificationChannel) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	channel.CreatedAt = now
	channel.UpdatedAt = now

	headersJSON, err := marshalHeaders(channel.Headers)
	if err != nil {
		return fmt.Errorf("failed to marshal headers: %w", err)
	}

	webhookURL, err := d.encryptNotificationSecret(channel.WebhookURL)
	if err != nil {
		return fmt.Errorf("failed to encrypt webhook URL: %w", err)
	}
	authCreds, err := d.encryptNotificationSecret(channel.AuthCredentials)
	if err != nil {
		return fmt.Errorf("failed to encrypt auth credentials: %w", err)
	}
	smtpPass, err := d.encryptNotificationSecret(channel.SMTPPassword)
	if err != nil {
		return fmt.Errorf("failed to encrypt SMTP password: %w", err)
	}

	err = d.pool.QueryRow(ctx, `
        INSERT INTO notification_channels (
            owner_username, owner_token, enabled, channel_type, name,
            description, webhook_url_encrypted, endpoint_url, http_method,
            headers_json, auth_type, auth_credentials_encrypted, smtp_host,
            smtp_port, smtp_username, smtp_password_encrypted, smtp_use_tls,
            from_address, from_name, template_alert_fire, template_alert_clear,
            template_reminder, reminder_enabled, reminder_interval_hours,
            is_estate_default, created_at, updated_at
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
                  $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27)
        RETURNING id, created_at, updated_at
    `, channel.OwnerUsername, channel.OwnerToken, channel.Enabled,
		channel.ChannelType, channel.Name, channel.Description, webhookURL,
		channel.EndpointURL, channel.HTTPMethod, headersJSON, channel.AuthType,
		authCreds, channel.SMTPHost, channel.SMTPPort, channel.SMTPUsername,
		smtpPass, channel.SMTPUseTLS, channel.FromAddress, channel.FromName,
		channel.TemplateAlertFire, channel.TemplateAlertClear, channel.TemplateReminder,
		channel.ReminderEnabled, channel.ReminderIntervalHours, channel.IsEstateDefault,
		channel.CreatedAt, channel.UpdatedAt).Scan(&channel.ID, &channel.CreatedAt, &channel.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create notification channel: %w", err)
	}

	return nil
}

// UpdateNotificationChannel updates an existing notification channel by ID.
func (d *Datastore) UpdateNotificationChannel(ctx context.Context, channel *NotificationChannel) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	headersJSON, err := marshalHeaders(channel.Headers)
	if err != nil {
		return fmt.Errorf("failed to marshal headers: %w", err)
	}

	webhookURL, err := d.encryptNotificationSecret(channel.WebhookURL)
	if err != nil {
		return fmt.Errorf("failed to encrypt webhook URL: %w", err)
	}
	authCreds, err := d.encryptNotificationSecret(channel.AuthCredentials)
	if err != nil {
		return fmt.Errorf("failed to encrypt auth credentials: %w", err)
	}
	smtpPass, err := d.encryptNotificationSecret(channel.SMTPPassword)
	if err != nil {
		return fmt.Errorf("failed to encrypt SMTP password: %w", err)
	}

	err = d.pool.QueryRow(ctx, `
        UPDATE notification_channels
        SET owner_username = $2, owner_token = $3, enabled = $4,
            channel_type = $5, name = $6, description = $7,
            webhook_url_encrypted = $8, endpoint_url = $9, http_method = $10,
            headers_json = $11, auth_type = $12,
            auth_credentials_encrypted = $13, smtp_host = $14, smtp_port = $15,
            smtp_username = $16, smtp_password_encrypted = $17,
            smtp_use_tls = $18, from_address = $19, from_name = $20,
            template_alert_fire = $21, template_alert_clear = $22,
            template_reminder = $23, reminder_enabled = $24,
            reminder_interval_hours = $25, is_estate_default = $26,
            updated_at = CURRENT_TIMESTAMP
        WHERE id = $1
        RETURNING updated_at
    `, channel.ID, channel.OwnerUsername, channel.OwnerToken,
		channel.Enabled, channel.ChannelType, channel.Name, channel.Description,
		webhookURL, channel.EndpointURL, channel.HTTPMethod, headersJSON,
		channel.AuthType, authCreds, channel.SMTPHost, channel.SMTPPort,
		channel.SMTPUsername, smtpPass, channel.SMTPUseTLS,
		channel.FromAddress, channel.FromName, channel.TemplateAlertFire,
		channel.TemplateAlertClear, channel.TemplateReminder, channel.ReminderEnabled,
		channel.ReminderIntervalHours, channel.IsEstateDefault).Scan(&channel.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotificationChannelNotFound
		}
		return fmt.Errorf("failed to update notification channel: %w", err)
	}

	return nil
}

// DeleteNotificationChannel deletes a notification channel by ID.
func (d *Datastore) DeleteNotificationChannel(ctx context.Context, id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	tag, err := d.pool.Exec(ctx, `DELETE FROM notification_channels WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete notification channel: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrNotificationChannelNotFound
	}

	return nil
}

// ListEmailRecipients returns all recipients for a notification channel.
func (d *Datastore) ListEmailRecipients(ctx context.Context, channelID int64) ([]*EmailRecipient, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.listEmailRecipientsLocked(ctx, channelID)
}

// listEmailRecipientsLocked returns all recipients for a channel. The caller
// must hold the read lock.
func (d *Datastore) listEmailRecipientsLocked(ctx context.Context, channelID int64) ([]*EmailRecipient, error) {
	rows, err := d.pool.Query(ctx, `
        SELECT id, channel_id, email_address, display_name, enabled, created_at
        FROM email_recipients
        WHERE channel_id = $1
        ORDER BY email_address
    `, channelID)
	if err != nil {
		return nil, fmt.Errorf("failed to query email recipients: %w", err)
	}
	defer rows.Close()

	var recipients []*EmailRecipient
	for rows.Next() {
		var r EmailRecipient
		if err := rows.Scan(&r.ID, &r.ChannelID, &r.EmailAddress,
			&r.DisplayName, &r.Enabled, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan email recipient: %w", err)
		}
		recipients = append(recipients, &r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating email recipients: %w", err)
	}
	if recipients == nil {
		recipients = []*EmailRecipient{}
	}

	return recipients, nil
}

// CreateEmailRecipient inserts a new email recipient and sets its ID via
// RETURNING.
func (d *Datastore) CreateEmailRecipient(ctx context.Context, recipient *EmailRecipient) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	recipient.CreatedAt = time.Now()

	err := d.pool.QueryRow(ctx, `
        INSERT INTO email_recipients (channel_id, email_address, display_name, enabled, created_at)
        VALUES ($1, $2, $3, $4, $5)
        RETURNING id, created_at
    `, recipient.ChannelID, recipient.EmailAddress, recipient.DisplayName,
		recipient.Enabled, recipient.CreatedAt).Scan(&recipient.ID, &recipient.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to create email recipient: %w", err)
	}

	return nil
}

// UpdateEmailRecipient updates an existing email recipient by ID.
func (d *Datastore) UpdateEmailRecipient(ctx context.Context, recipient *EmailRecipient) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	tag, err := d.pool.Exec(ctx, `
        UPDATE email_recipients
        SET email_address = $2, display_name = $3, enabled = $4
        WHERE id = $1
    `, recipient.ID, recipient.EmailAddress, recipient.DisplayName, recipient.Enabled)
	if err != nil {
		return fmt.Errorf("failed to update email recipient: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrEmailRecipientNotFound
	}

	return nil
}

// DeleteEmailRecipient deletes an email recipient by ID.
func (d *Datastore) DeleteEmailRecipient(ctx context.Context, id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	tag, err := d.pool.Exec(ctx, `DELETE FROM email_recipients WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete email recipient: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrEmailRecipientNotFound
	}

	return nil
}

// scanNotificationChannel scans a notification channel from a pgx.Rows row.
func scanNotificationChannel(rows pgx.Rows) (*NotificationChannel, error) {
	var c NotificationChannel
	var headersJSON []byte
	err := rows.Scan(
		&c.ID, &c.OwnerUsername, &c.OwnerToken,
		&c.Enabled, &c.ChannelType, &c.Name, &c.Description,
		&c.WebhookURL, &c.EndpointURL, &c.HTTPMethod, &headersJSON,
		&c.AuthType, &c.AuthCredentials, &c.SMTPHost, &c.SMTPPort,
		&c.SMTPUsername, &c.SMTPPassword, &c.SMTPUseTLS,
		&c.FromAddress, &c.FromName, &c.TemplateAlertFire,
		&c.TemplateAlertClear, &c.TemplateReminder, &c.ReminderEnabled,
		&c.ReminderIntervalHours, &c.IsEstateDefault, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}

	if len(headersJSON) > 0 {
		if err := json.Unmarshal(headersJSON, &c.Headers); err != nil {
			return nil, fmt.Errorf("failed to unmarshal headers: %w", err)
		}
	}

	return &c, nil
}

// scanNotificationChannelRow scans a notification channel from a pgx.Row.
func scanNotificationChannelRow(row pgx.Row) (*NotificationChannel, error) {
	var c NotificationChannel
	var headersJSON []byte
	err := row.Scan(
		&c.ID, &c.OwnerUsername, &c.OwnerToken,
		&c.Enabled, &c.ChannelType, &c.Name, &c.Description,
		&c.WebhookURL, &c.EndpointURL, &c.HTTPMethod, &headersJSON,
		&c.AuthType, &c.AuthCredentials, &c.SMTPHost, &c.SMTPPort,
		&c.SMTPUsername, &c.SMTPPassword, &c.SMTPUseTLS,
		&c.FromAddress, &c.FromName, &c.TemplateAlertFire,
		&c.TemplateAlertClear, &c.TemplateReminder, &c.ReminderEnabled,
		&c.ReminderIntervalHours, &c.IsEstateDefault, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}

	if len(headersJSON) > 0 {
		if err := json.Unmarshal(headersJSON, &c.Headers); err != nil {
			return nil, fmt.Errorf("failed to unmarshal headers: %w", err)
		}
	}

	return &c, nil
}

// ChannelOverride represents a notification channel with its scope-level override status
type ChannelOverride struct {
	ChannelID       int64   `json:"channel_id"`
	ChannelName     string  `json:"channel_name"`
	ChannelType     string  `json:"channel_type"`
	Description     *string `json:"description"`
	IsEstateDefault bool    `json:"is_estate_default"`
	HasOverride     bool    `json:"has_override"`
	OverrideEnabled *bool   `json:"override_enabled"`
}

// ChannelOverrideUpdate represents data for creating/updating a channel override
type ChannelOverrideUpdate struct {
	Enabled bool `json:"enabled"`
}

// GetChannelOverridesForServer returns all enabled channels with their server-level overrides.
func (d *Datastore) GetChannelOverridesForServer(ctx context.Context, connectionID int) ([]ChannelOverride, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.pool.Query(ctx, `
		SELECT nc.id, nc.name, nc.channel_type, nc.description, nc.is_estate_default,
		       nco.enabled
		FROM notification_channels nc
		LEFT JOIN notification_channel_overrides nco ON nco.channel_id = nc.id
		    AND nco.scope = 'server' AND nco.connection_id = $1
		WHERE nc.enabled = true
		ORDER BY nc.name`, connectionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query server channel overrides: %w", err)
	}
	defer rows.Close()

	return scanChannelOverrides(rows)
}

// GetChannelOverridesForCluster returns all enabled channels with their cluster-level overrides.
func (d *Datastore) GetChannelOverridesForCluster(ctx context.Context, clusterID int) ([]ChannelOverride, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.pool.Query(ctx, `
		SELECT nc.id, nc.name, nc.channel_type, nc.description, nc.is_estate_default,
		       nco.enabled
		FROM notification_channels nc
		LEFT JOIN notification_channel_overrides nco ON nco.channel_id = nc.id
		    AND nco.scope = 'cluster' AND nco.cluster_id = $1
		WHERE nc.enabled = true
		ORDER BY nc.name`, clusterID)
	if err != nil {
		return nil, fmt.Errorf("failed to query cluster channel overrides: %w", err)
	}
	defer rows.Close()

	return scanChannelOverrides(rows)
}

// GetChannelOverridesForGroup returns all enabled channels with their group-level overrides.
func (d *Datastore) GetChannelOverridesForGroup(ctx context.Context, groupID int) ([]ChannelOverride, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.pool.Query(ctx, `
		SELECT nc.id, nc.name, nc.channel_type, nc.description, nc.is_estate_default,
		       nco.enabled
		FROM notification_channels nc
		LEFT JOIN notification_channel_overrides nco ON nco.channel_id = nc.id
		    AND nco.scope = 'group' AND nco.group_id = $1
		WHERE nc.enabled = true
		ORDER BY nc.name`, groupID)
	if err != nil {
		return nil, fmt.Errorf("failed to query group channel overrides: %w", err)
	}
	defer rows.Close()

	return scanChannelOverrides(rows)
}

// scanChannelOverrides is a helper that scans rows from a channel overrides query.
func scanChannelOverrides(rows pgx.Rows) ([]ChannelOverride, error) {
	var overrides []ChannelOverride
	for rows.Next() {
		var o ChannelOverride
		var overrideEnabled *bool
		if err := rows.Scan(
			&o.ChannelID, &o.ChannelName, &o.ChannelType, &o.Description,
			&o.IsEstateDefault, &overrideEnabled,
		); err != nil {
			return nil, fmt.Errorf("failed to scan channel override: %w", err)
		}
		o.HasOverride = overrideEnabled != nil
		o.OverrideEnabled = overrideEnabled
		overrides = append(overrides, o)
	}
	if overrides == nil {
		overrides = []ChannelOverride{}
	}
	return overrides, rows.Err()
}

// UpsertChannelOverride creates or updates a notification channel override at the specified scope.
func (d *Datastore) UpsertChannelOverride(ctx context.Context, scope string, scopeID int, channelID int64, update ChannelOverrideUpdate) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	var query string
	var args []any

	switch scope {
	case "server":
		query = `
			INSERT INTO notification_channel_overrides (channel_id, scope, connection_id, enabled)
			VALUES ($1, 'server', $2, $3)
			ON CONFLICT (channel_id, connection_id) WHERE scope = 'server'
			DO UPDATE SET enabled = $3`
		args = []any{channelID, scopeID, update.Enabled}
	case "cluster":
		query = `
			INSERT INTO notification_channel_overrides (channel_id, scope, cluster_id, enabled)
			VALUES ($1, 'cluster', $2, $3)
			ON CONFLICT (channel_id, cluster_id) WHERE scope = 'cluster'
			DO UPDATE SET enabled = $3`
		args = []any{channelID, scopeID, update.Enabled}
	case "group":
		query = `
			INSERT INTO notification_channel_overrides (channel_id, scope, group_id, enabled)
			VALUES ($1, 'group', $2, $3)
			ON CONFLICT (channel_id, group_id) WHERE scope = 'group'
			DO UPDATE SET enabled = $3`
		args = []any{channelID, scopeID, update.Enabled}
	default:
		return fmt.Errorf("invalid scope: %s", scope)
	}

	_, err := d.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to upsert channel override: %w", err)
	}
	return nil
}

// DeleteChannelOverride removes a notification channel override at the specified scope.
func (d *Datastore) DeleteChannelOverride(ctx context.Context, scope string, scopeID int, channelID int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	var query string
	var args []any

	switch scope {
	case "server":
		query = `DELETE FROM notification_channel_overrides WHERE scope = 'server' AND channel_id = $1 AND connection_id = $2`
		args = []any{channelID, scopeID}
	case "cluster":
		query = `DELETE FROM notification_channel_overrides WHERE scope = 'cluster' AND channel_id = $1 AND cluster_id = $2`
		args = []any{channelID, scopeID}
	case "group":
		query = `DELETE FROM notification_channel_overrides WHERE scope = 'group' AND channel_id = $1 AND group_id = $2`
		args = []any{channelID, scopeID}
	default:
		return fmt.Errorf("invalid scope: %s", scope)
	}

	_, err := d.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to delete channel override: %w", err)
	}
	return nil
}

// marshalHeaders converts the headers map to JSON bytes for database storage.
// Returns nil if the map is empty or nil.
func marshalHeaders(headers map[string]string) ([]byte, error) {
	if len(headers) == 0 {
		return nil, nil
	}
	return json.Marshal(headers)
}
