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

// webhookNotifier implements Notifier for generic REST webhooks
type webhookNotifier struct {
	httpClient *http.Client
	renderer   TemplateRenderer
}

// NewWebhookNotifier creates a new webhook notifier
func NewWebhookNotifier(httpClient *http.Client, renderer TemplateRenderer) Notifier {
	return &webhookNotifier{
		httpClient: httpClient,
		renderer:   renderer,
	}
}

// Type implements Notifier.Type
func (n *webhookNotifier) Type() database.NotificationChannelType {
	return database.ChannelTypeWebhook
}

// Validate implements Notifier.Validate
func (n *webhookNotifier) Validate(channel *database.NotificationChannel) error {
	if channel.EndpointURL == nil || *channel.EndpointURL == "" {
		return fmt.Errorf("webhook channel requires endpoint URL")
	}
	// Validate HTTP method
	method := channel.HTTPMethod
	if method == "" {
		method = "POST"
	}
	if method != "GET" && method != "POST" && method != "PUT" && method != "PATCH" {
		return fmt.Errorf("invalid HTTP method: %s", method)
	}
	return nil
}

// Send implements Notifier.Send
func (n *webhookNotifier) Send(ctx context.Context, channel *database.NotificationChannel, payload *database.NotificationPayload) error {
	if err := n.Validate(channel); err != nil {
		return err
	}

	endpointURL := *channel.EndpointURL
	method := channel.HTTPMethod
	if method == "" {
		method = "POST"
	}

	// Select template based on notification type
	var template, defaultTemplate string
	switch payload.NotificationType {
	case string(database.NotificationTypeAlertFire):
		template = deref(channel.TemplateAlertFire)
		defaultTemplate = DefaultWebhookAlertFireTemplate
	case string(database.NotificationTypeAlertClear):
		template = deref(channel.TemplateAlertClear)
		defaultTemplate = DefaultWebhookAlertClearTemplate
	case string(database.NotificationTypeReminder):
		template = deref(channel.TemplateReminder)
		defaultTemplate = DefaultWebhookReminderTemplate
	default:
		return fmt.Errorf("unknown notification type: %s", payload.NotificationType)
	}

	// Render template
	body, err := n.renderer.RenderJSON(template, payload, defaultTemplate)
	if err != nil {
		return fmt.Errorf("failed to render webhook template: %w", err)
	}

	// Create request
	var reqBody io.Reader
	if method != "GET" {
		reqBody = strings.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpointURL, reqBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set content type for non-GET requests
	if method != "GET" {
		req.Header.Set("Content-Type", "application/json")
	}

	// Add custom headers
	for key, value := range channel.Headers {
		req.Header.Set(key, value)
	}

	// Add authentication
	if channel.AuthType != nil && channel.AuthCredentials != nil {
		switch *channel.AuthType {
		case "basic":
			// AuthCredentials format: "username:password"
			creds := *channel.AuthCredentials
			parts := strings.SplitN(creds, ":", 2)
			if len(parts) == 2 {
				req.SetBasicAuth(parts[0], parts[1])
			}
		case "bearer":
			req.Header.Set("Authorization", "Bearer "+*channel.AuthCredentials)
		case "api_key":
			// AuthCredentials format: "header_name:api_key_value"
			parts := strings.SplitN(*channel.AuthCredentials, ":", 2)
			if len(parts) == 2 {
				req.Header.Set(parts[0], parts[1])
			}
		}
	}

	// Send request
	resp, err := n.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send webhook: %w", err)
	}
	defer resp.Body.Close()

	// Check response (2xx is success)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("webhook returned %d (failed to read body: %v)", resp.StatusCode, readErr)
		}
		return fmt.Errorf("webhook returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
