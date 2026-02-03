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

// slackNotifier implements Notifier for Slack webhooks
type slackNotifier struct {
	httpClient *http.Client
	secrets    SecretManager
	renderer   TemplateRenderer
}

// NewSlackNotifier creates a new Slack notifier
func NewSlackNotifier(httpClient *http.Client, secrets SecretManager, renderer TemplateRenderer) Notifier {
	return &slackNotifier{
		httpClient: httpClient,
		secrets:    secrets,
		renderer:   renderer,
	}
}

// Type implements Notifier.Type
func (n *slackNotifier) Type() database.NotificationChannelType {
	return database.ChannelTypeSlack
}

// Validate implements Notifier.Validate
// Checks that webhook URL is configured
func (n *slackNotifier) Validate(channel *database.NotificationChannel) error {
	if channel.WebhookURL == nil || *channel.WebhookURL == "" {
		return fmt.Errorf("slack channel requires webhook URL")
	}
	return nil
}

// Send implements Notifier.Send
func (n *slackNotifier) Send(ctx context.Context, channel *database.NotificationChannel, payload *database.NotificationPayload) error {
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
		return fmt.Errorf("failed to render slack template: %w", err)
	}

	// Send HTTP POST to webhook URL
	req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send slack notification: %w", err)
	}
	defer resp.Body.Close()

	// Slack returns "ok" with 200 on success
	if resp.StatusCode != http.StatusOK {
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("slack webhook returned %d (failed to read body: %v)", resp.StatusCode, readErr)
		}
		return fmt.Errorf("slack webhook returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
