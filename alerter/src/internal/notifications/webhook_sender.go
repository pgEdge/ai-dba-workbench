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

// sendWebhookNotification sends a JSON payload to a webhook URL. It handles
// template selection, rendering, HTTP posting, and response validation. The
// serviceName parameter is used in error messages (e.g. "slack", "mattermost").
func sendWebhookNotification(
	ctx context.Context,
	httpClient *http.Client,
	renderer TemplateRenderer,
	serviceName string,
	webhookURL string,
	channel *database.NotificationChannel,
	payload *database.NotificationPayload,
) error {
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
	body, err := renderer.RenderJSON(template, payload, defaultTemplate)
	if err != nil {
		return fmt.Errorf("failed to render %s template: %w", serviceName, err)
	}

	// Send HTTP POST to webhook URL
	req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send %s notification: %w", serviceName, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if readErr != nil {
			return fmt.Errorf("%s webhook returned %d (failed to read body: %v)", serviceName, resp.StatusCode, readErr)
		}
		return fmt.Errorf("%s webhook returned %d: %s", serviceName, resp.StatusCode, string(respBody))
	}

	return nil
}
