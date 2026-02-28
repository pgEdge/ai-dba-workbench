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

// slackNotifier implements Notifier for Slack webhooks
type slackNotifier struct {
	httpClient *http.Client
	renderer   TemplateRenderer
}

// NewSlackNotifier creates a new Slack notifier
func NewSlackNotifier(httpClient *http.Client, renderer TemplateRenderer) Notifier {
	return &slackNotifier{
		httpClient: httpClient,
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
	if err := n.Validate(channel); err != nil {
		return err
	}

	return sendWebhookNotification(ctx, n.httpClient, n.renderer, "slack", *channel.WebhookURL, channel, payload)
}
