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

import "time"

// NotificationChannelType represents the type of notification channel
type NotificationChannelType string

const (
	ChannelTypeSlack      NotificationChannelType = "slack"
	ChannelTypeMattermost NotificationChannelType = "mattermost"
	ChannelTypeWebhook    NotificationChannelType = "webhook"
	ChannelTypeEmail      NotificationChannelType = "email"
)

// NotificationType represents the type of notification
type NotificationType string

const (
	NotificationTypeAlertFire  NotificationType = "alert_fire"
	NotificationTypeAlertClear NotificationType = "alert_clear"
	NotificationTypeReminder   NotificationType = "reminder"
)

// NotificationStatus represents the delivery status
type NotificationStatus string

const (
	NotificationStatusPending  NotificationStatus = "pending"
	NotificationStatusSent     NotificationStatus = "sent"
	NotificationStatusFailed   NotificationStatus = "failed"
	NotificationStatusRetrying NotificationStatus = "retrying"
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

	// Slack/Mattermost (stored encrypted in DB)
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

	// Recipients - populated for email channels by the notification manager
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

// ConnectionNotificationChannel represents the link between a connection and channel
type ConnectionNotificationChannel struct {
	ID                            int64     `json:"id"`
	ConnectionID                  int       `json:"connection_id"`
	ChannelID                     int64     `json:"channel_id"`
	Enabled                       bool      `json:"enabled"`
	ReminderEnabledOverride       *bool     `json:"reminder_enabled_override,omitempty"`
	ReminderIntervalHoursOverride *int      `json:"reminder_interval_hours_override,omitempty"`
	CreatedAt                     time.Time `json:"created_at"`
}

// NotificationHistory represents a notification delivery record
type NotificationHistory struct {
	ID               int64                  `json:"id"`
	AlertID          *int64                 `json:"alert_id,omitempty"`
	ChannelID        *int64                 `json:"channel_id,omitempty"`
	ConnectionID     *int                   `json:"connection_id,omitempty"`
	NotificationType NotificationType       `json:"notification_type"`
	Status           NotificationStatus     `json:"status"`
	PayloadJSON      map[string]interface{} `json:"payload_json,omitempty"`
	ResponseCode     *int                   `json:"response_code,omitempty"`
	ResponseBody     *string                `json:"response_body,omitempty"`
	ErrorMessage     *string                `json:"error_message,omitempty"`
	AttemptCount     int                    `json:"attempt_count"`
	MaxAttempts      int                    `json:"max_attempts"`
	NextRetryAt      *time.Time             `json:"next_retry_at,omitempty"`
	CreatedAt        time.Time              `json:"created_at"`
	SentAt           *time.Time             `json:"sent_at,omitempty"`
}

// NotificationReminderState tracks reminder state for active alerts
type NotificationReminderState struct {
	ID             int64     `json:"id"`
	AlertID        int64     `json:"alert_id"`
	ChannelID      int64     `json:"channel_id"`
	LastReminderAt time.Time `json:"last_reminder_at"`
	ReminderCount  int       `json:"reminder_count"`
}

// NotificationPayload contains data for rendering notification templates
type NotificationPayload struct {
	// Alert info
	AlertID          int64      `json:"alert_id"`
	AlertType        string     `json:"alert_type"`
	AlertTitle       string     `json:"alert_title"`
	AlertDescription string     `json:"alert_description"`
	Severity         string     `json:"severity"`
	Status           string     `json:"status"`
	TriggeredAt      time.Time  `json:"triggered_at"`
	ClearedAt        *time.Time `json:"cleared_at,omitempty"`

	// Metric info
	MetricName     *string  `json:"metric_name,omitempty"`
	MetricValue    *float64 `json:"metric_value,omitempty"`
	ThresholdValue *float64 `json:"threshold_value,omitempty"`
	Operator       *string  `json:"operator,omitempty"`

	// Server info
	ConnectionID int     `json:"connection_id"`
	ServerName   string  `json:"server_name"`
	ServerHost   string  `json:"server_host"`
	ServerPort   int     `json:"server_port"`
	DatabaseName *string `json:"database_name,omitempty"`

	// Notification info
	NotificationType string    `json:"notification_type"`
	ReminderCount    int       `json:"reminder_count"`
	Timestamp        time.Time `json:"timestamp"`
	Duration         *string   `json:"duration,omitempty"` // Computed for cleared alerts
}
