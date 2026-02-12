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
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

func TestMattermostNotifier_Type(t *testing.T) {
	notifier := NewMattermostNotifier(http.DefaultClient, &mockTemplateRenderer{})
	if got := notifier.Type(); got != database.ChannelTypeMattermost {
		t.Errorf("Type() = %v, want %v", got, database.ChannelTypeMattermost)
	}
}

func TestMattermostNotifier_Validate(t *testing.T) {
	notifier := NewMattermostNotifier(http.DefaultClient, &mockTemplateRenderer{})

	tests := []struct {
		name    string
		channel *database.NotificationChannel
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid channel",
			channel: &database.NotificationChannel{
				WebhookURL: strPtr("https://mattermost.example.com/hooks/xxx"),
			},
			wantErr: false,
		},
		{
			name: "missing webhook URL - nil",
			channel: &database.NotificationChannel{
				WebhookURL: nil,
			},
			wantErr: true,
			errMsg:  "mattermost channel requires webhook URL",
		},
		{
			name: "missing webhook URL - empty",
			channel: &database.NotificationChannel{
				WebhookURL: strPtr(""),
			},
			wantErr: true,
			errMsg:  "mattermost channel requires webhook URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := notifier.Validate(tt.channel)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error, got nil")
				} else if err.Error() != tt.errMsg {
					t.Errorf("Validate() error = %q, want %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
			}
		})
	}
}

func TestMattermostNotifier_Send_Success(t *testing.T) {
	var receivedBody string
	var receivedContentType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("Failed to read request body: %v", err)
		}
		receivedBody = string(body)

		if r.Method != http.MethodPost {
			t.Errorf("Expected POST method, got %s", r.Method)
		}

		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("ok")); err != nil {
			t.Errorf("Failed to write response: %v", err)
		}
	}))
	defer server.Close()

	renderer := &mockTemplateRenderer{
		renderJSONFunc: func(templateStr string, payload *database.NotificationPayload, defaultTemplate string) (string, error) {
			return `{"text": "Test alert: High Memory"}`, nil
		},
	}

	notifier := NewMattermostNotifier(server.Client(), renderer)
	ctx := context.Background()

	channel := &database.NotificationChannel{
		WebhookURL: strPtr(server.URL),
	}

	payload := &database.NotificationPayload{
		NotificationType: string(database.NotificationTypeAlertFire),
		AlertTitle:       "High Memory",
		Severity:         "critical",
	}

	err := notifier.Send(ctx, channel, payload)
	if err != nil {
		t.Errorf("Send() unexpected error: %v", err)
	}

	if receivedContentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", receivedContentType)
	}

	if receivedBody != `{"text": "Test alert: High Memory"}` {
		t.Errorf("Unexpected request body: %s", receivedBody)
	}
}

func TestMattermostNotifier_Send_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		// Error ignored in test handler as response may be discarded
		_, _ = w.Write([]byte("bad request")) //nolint:errcheck
	}))
	defer server.Close()

	notifier := NewMattermostNotifier(server.Client(), &mockTemplateRenderer{})
	ctx := context.Background()

	channel := &database.NotificationChannel{
		WebhookURL: strPtr(server.URL),
	}

	payload := createTestPayload()

	err := notifier.Send(ctx, channel, payload)
	if err == nil {
		t.Error("Send() expected error for HTTP 400")
	}

	if !strings.Contains(err.Error(), "400") {
		t.Errorf("Error should contain status code 400: %v", err)
	}

	if !strings.Contains(err.Error(), "bad request") {
		t.Errorf("Error should contain response body: %v", err)
	}
}

func TestMattermostNotifier_Send_ValidationError(t *testing.T) {
	notifier := NewMattermostNotifier(http.DefaultClient, &mockTemplateRenderer{})
	ctx := context.Background()

	channel := &database.NotificationChannel{
		WebhookURL: nil,
	}

	payload := createTestPayload()

	err := notifier.Send(ctx, channel, payload)
	if err == nil {
		t.Error("Send() expected validation error")
	}

	if err.Error() != "mattermost channel requires webhook URL" {
		t.Errorf("Send() error = %q, want %q", err.Error(), "mattermost channel requires webhook URL")
	}
}

func TestMattermostNotifier_Send_UnknownNotificationType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewMattermostNotifier(server.Client(), &mockTemplateRenderer{})
	ctx := context.Background()

	channel := &database.NotificationChannel{
		WebhookURL: strPtr(server.URL),
	}

	payload := &database.NotificationPayload{
		NotificationType: "invalid_type",
	}

	err := notifier.Send(ctx, channel, payload)
	if err == nil {
		t.Error("Send() expected error for unknown notification type")
	}

	if !strings.Contains(err.Error(), "unknown notification type") {
		t.Errorf("Error should mention unknown notification type: %v", err)
	}
}

func TestMattermostNotifier_Send_UsesSlackTemplates(t *testing.T) {
	// Mattermost uses Slack-compatible templates
	tests := []struct {
		name             string
		notificationType string
		expectedTemplate string
	}{
		{
			name:             "alert fire uses Slack template",
			notificationType: string(database.NotificationTypeAlertFire),
			expectedTemplate: DefaultSlackAlertFireTemplate,
		},
		{
			name:             "alert clear uses Slack template",
			notificationType: string(database.NotificationTypeAlertClear),
			expectedTemplate: DefaultSlackAlertClearTemplate,
		},
		{
			name:             "reminder uses Slack template",
			notificationType: string(database.NotificationTypeReminder),
			expectedTemplate: DefaultSlackReminderTemplate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			usedTemplate := ""
			renderer := &mockTemplateRenderer{
				renderJSONFunc: func(templateStr string, payload *database.NotificationPayload, defaultTemplate string) (string, error) {
					usedTemplate = defaultTemplate
					return `{"text": "test"}`, nil
				},
			}

			notifier := NewMattermostNotifier(server.Client(), renderer)
			ctx := context.Background()

			channel := &database.NotificationChannel{
				WebhookURL: strPtr(server.URL),
			}

			payload := &database.NotificationPayload{
				NotificationType: tt.notificationType,
				AlertTitle:       "Test Alert",
			}

			err := notifier.Send(ctx, channel, payload)
			if err != nil {
				t.Errorf("Send() unexpected error: %v", err)
			}

			if usedTemplate != tt.expectedTemplate {
				t.Errorf("Expected Slack template to be used for Mattermost")
			}
		})
	}
}

func TestMattermostNotifier_Send_CustomTemplate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	customTemplate := `{"text": "Mattermost Custom: {{.AlertTitle}}"}`
	usedTemplateStr := ""

	renderer := &mockTemplateRenderer{
		renderJSONFunc: func(templateStr string, payload *database.NotificationPayload, defaultTemplate string) (string, error) {
			usedTemplateStr = templateStr
			return `{"text": "Mattermost Custom: Test"}`, nil
		},
	}

	notifier := NewMattermostNotifier(server.Client(), renderer)
	ctx := context.Background()

	channel := &database.NotificationChannel{
		WebhookURL:        strPtr(server.URL),
		TemplateAlertFire: strPtr(customTemplate),
	}

	payload := &database.NotificationPayload{
		NotificationType: string(database.NotificationTypeAlertFire),
		AlertTitle:       "Test",
	}

	err := notifier.Send(ctx, channel, payload)
	if err != nil {
		t.Errorf("Send() unexpected error: %v", err)
	}

	if usedTemplateStr != customTemplate {
		t.Errorf("Expected custom template to be used, got %q", usedTemplateStr)
	}
}

func TestMattermostNotifier_Send_TemplateRenderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	renderer := &mockTemplateRenderer{
		renderJSONFunc: func(templateStr string, payload *database.NotificationPayload, defaultTemplate string) (string, error) {
			return "", &templateError{msg: "template parse error"}
		},
	}

	notifier := NewMattermostNotifier(server.Client(), renderer)
	ctx := context.Background()

	channel := &database.NotificationChannel{
		WebhookURL: strPtr(server.URL),
	}

	payload := createTestPayload()

	err := notifier.Send(ctx, channel, payload)
	if err == nil {
		t.Error("Send() expected error for template rendering failure")
	}

	if !strings.Contains(err.Error(), "failed to render mattermost template") {
		t.Errorf("Error should mention mattermost template rendering: %v", err)
	}
}

func TestMattermostNotifier_Send_ConnectionError(t *testing.T) {
	notifier := NewMattermostNotifier(http.DefaultClient, &mockTemplateRenderer{})
	ctx := context.Background()

	channel := &database.NotificationChannel{
		// Invalid URL that will fail to connect
		WebhookURL: strPtr("http://localhost:1"),
	}

	payload := createTestPayload()

	err := notifier.Send(ctx, channel, payload)
	if err == nil {
		t.Error("Send() expected connection error")
	}

	if !strings.Contains(err.Error(), "failed to send mattermost notification") {
		t.Errorf("Error should mention mattermost notification failure: %v", err)
	}
}

func TestMattermostNotifier_Send_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewMattermostNotifier(server.Client(), &mockTemplateRenderer{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	channel := &database.NotificationChannel{
		WebhookURL: strPtr(server.URL),
	}

	payload := createTestPayload()

	err := notifier.Send(ctx, channel, payload)
	if err == nil {
		t.Error("Send() expected context cancellation error")
	}
}

func TestMattermostNotifier_Send_ResponseBodyReadError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100") // Lie about content length
		w.WriteHeader(http.StatusForbidden)
		// Error ignored in test handler as response may be discarded
		_, _ = w.Write([]byte("short")) //nolint:errcheck
	}))
	defer server.Close()

	notifier := NewMattermostNotifier(server.Client(), &mockTemplateRenderer{})
	ctx := context.Background()

	channel := &database.NotificationChannel{
		WebhookURL: strPtr(server.URL),
	}

	payload := createTestPayload()

	err := notifier.Send(ctx, channel, payload)
	if err == nil {
		t.Error("Send() expected error for non-200 response")
	}

	// The error should contain the status code
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("Error should contain status code 403: %v", err)
	}
}
