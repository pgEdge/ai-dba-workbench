/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package notifications

import (
	"context"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// Notifier is the interface for sending notifications through a specific channel type
type Notifier interface {
	// Send sends a notification through this channel
	Send(ctx context.Context, channel *database.NotificationChannel, payload *database.NotificationPayload) error

	// Type returns the channel type this notifier handles
	Type() database.NotificationChannelType

	// Validate validates the channel configuration for this notifier type
	Validate(channel *database.NotificationChannel) error
}

// SecretManager handles encryption/decryption of sensitive data stored in the database
type SecretManager interface {
	// Encrypt encrypts plaintext data for storage
	Encrypt(plaintext string) (string, error)

	// Decrypt decrypts ciphertext retrieved from storage
	Decrypt(ciphertext string) (string, error)
}

// TemplateRenderer renders notification templates with payload data
type TemplateRenderer interface {
	// Render renders a template string with the given payload
	// Returns the rendered string or default content if template is empty
	Render(templateStr string, payload *database.NotificationPayload, defaultTemplate string) (string, error)

	// RenderJSON renders a JSON template and validates the result is valid JSON
	RenderJSON(templateStr string, payload *database.NotificationPayload, defaultTemplate string) (string, error)
}

// NotificationManager orchestrates all notification operations
type NotificationManager interface {
	// SendAlertNotification queues notifications for an alert event
	SendAlertNotification(ctx context.Context, alert *database.Alert, notifType database.NotificationType) error

	// ProcessPendingNotifications processes queued and retry notifications
	ProcessPendingNotifications(ctx context.Context) error

	// ProcessReminders sends reminder notifications for active alerts
	ProcessReminders(ctx context.Context) error
}
