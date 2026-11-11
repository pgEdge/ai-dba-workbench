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

// NewWebhookNotifier creates a new webhook notifier. It enforces no
// address policy of its own: the manager gives it the shared delivery
// client, whose dialer refuses internal addresses unless
// notifications.allow_internal_networks is set.
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
		// The method is operator-supplied configuration that reaches
		// the log and notification_history.error_message from here, so
		// it is sanitized like any other borrowed text.
		return fmt.Errorf("invalid HTTP method %q: must be GET, POST, PUT or PATCH",
			sanitizeConfigEcho(method))
	}
	return nil
}

// Send implements Notifier.Send
//
// The endpoint URL may carry a credential in its path or query string,
// so no error returned from here wraps with %w anything net/http
// produced, and no text borrowed from the endpoint is echoed raw: both
// go through the helpers in sanitize.go.
func (n *webhookNotifier) Send(ctx context.Context, channel *database.NotificationChannel, payload *database.NotificationPayload) error {
	if err := n.Validate(channel); err != nil {
		return err
	}

	endpointURL := *channel.EndpointURL

	// No host check here. The SSRF policy is applied by the dialer of
	// the client this notifier is given (see dial_guard.go), to every
	// address it actually connects to; a check made here would resolve
	// the name separately and could be answered differently from the
	// dial (DNS rebinding).

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
		// With a non-nil context and a method Validate has already
		// accepted, the only way this fails is url.Parse rejecting the
		// endpoint URL, and that error quotes the raw stored string
		// back. Report nothing borrowed from it.
		return fmt.Errorf("failed to create request: the endpoint URL is malformed")
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
		return fmt.Errorf("failed to send webhook: %s", webhookTransportError(err, endpointURL))
	}
	defer resp.Body.Close()

	// Check response (2xx is success)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if readErr != nil {
			return fmt.Errorf("webhook returned %d (failed to read body: %s)",
				resp.StatusCode, sanitizeWebhookEcho(readErr.Error(), endpointURL))
		}
		return fmt.Errorf("webhook returned %d: %s",
			resp.StatusCode, sanitizeWebhookEcho(string(respBody), endpointURL))
	}

	return nil
}
