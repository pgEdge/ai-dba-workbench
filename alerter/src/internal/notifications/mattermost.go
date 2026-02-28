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

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// mattermostNotifier implements Notifier for Mattermost webhooks
type mattermostNotifier struct {
	httpClient *http.Client
	renderer   TemplateRenderer
}

// NewMattermostNotifier creates a new Mattermost notifier
func NewMattermostNotifier(httpClient *http.Client, renderer TemplateRenderer) Notifier {
	return &mattermostNotifier{
		httpClient: httpClient,
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
	if err := n.Validate(channel); err != nil {
		return err
	}

	return sendWebhookNotification(ctx, n.httpClient, n.renderer, "mattermost", *channel.WebhookURL, channel, payload)
}
