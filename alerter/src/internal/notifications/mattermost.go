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
	"io"
	"net/http"
	"strings"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// mattermostNotifier implements Notifier for Mattermost webhooks
type mattermostNotifier struct {
	httpClient *http.Client
	secrets    SecretManager
	renderer   TemplateRenderer
}

// NewMattermostNotifier creates a new Mattermost notifier
func NewMattermostNotifier(httpClient *http.Client, secrets SecretManager, renderer TemplateRenderer) Notifier {
	return &mattermostNotifier{
		httpClient: httpClient,
		secrets:    secrets,
		renderer:   renderer,
	}
}

// Type implements Notifier.Type
func (n *mattermostNotifier) Type() database.NotificationChannelType {
	return database.ChannelTypeMattermost
}

// Validate implements Notifier.Validate
// Checks that webhook URL is configured
func (n *mattermostNotifier) Validate(channel *database.NotificationChannel) error {
	if channel.WebhookURL == nil || *channel.WebhookURL == "" {
		return fmt.Errorf("mattermost channel requires webhook URL")
	}
	return nil
}

// Send implements Notifier.Send
// Mattermost webhooks are compatible with Slack format, so we use the same templates
func (n *mattermostNotifier) Send(ctx context.Context, channel *database.NotificationChannel, payload *database.NotificationPayload) error {
	// Validate channel
	if err := n.Validate(channel); err != nil {
		return err
	}

	// Get webhook URL (the WebhookURL field in the struct should already be
	// decrypted by the caller)
	webhookURL := *channel.WebhookURL

	// Select template based on notification type
	var template, defaultTemplate string
	switch payload.NotificationType {
	case string(database.NotificationTypeAlertFire):
		template = deref(channel.TemplateAlertFire)
		defaultTemplate = DefaultSlackAlertFireTemplate
	case string(database.NotificationTypeAlertClear):
		template = deref(channel.TemplateAlertClear)
		defaultTemplate = DefaultSlackAlertClearTemplate
	case string(database.NotificationTypeReminder):
		template = deref(channel.TemplateReminder)
		defaultTemplate = DefaultSlackReminderTemplate
	default:
		return fmt.Errorf("unknown notification type: %s", payload.NotificationType)
	}

	// Render template
	body, err := n.renderer.RenderJSON(template, payload, defaultTemplate)
	if err != nil {
		return fmt.Errorf("failed to render mattermost template: %w", err)
	}

	// Send HTTP POST to webhook URL
	req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send mattermost notification: %w", err)
	}
	defer resp.Body.Close()

	// Mattermost returns "ok" with 200 on success (same as Slack)
	if resp.StatusCode != http.StatusOK {
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("mattermost webhook returned %d (failed to read body: %v)", resp.StatusCode, readErr)
		}
		return fmt.Errorf("mattermost webhook returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
