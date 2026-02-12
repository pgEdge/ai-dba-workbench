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

func TestSlackNotifier_Type(t *testing.T) {
	notifier := NewSlackNotifier(http.DefaultClient, &mockTemplateRenderer{})
	if got := notifier.Type(); got != database.ChannelTypeSlack {
		t.Errorf("Type() = %v, want %v", got, database.ChannelTypeSlack)
	}
}

func TestSlackNotifier_Validate(t *testing.T) {
	notifier := NewSlackNotifier(http.DefaultClient, &mockTemplateRenderer{})

	tests := []struct {
		name    string
		channel *database.NotificationChannel
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid channel",
			channel: &database.NotificationChannel{
				WebhookURL: strPtr("https://hooks.slack.com/services/T00/B00/XXX"),
			},
			wantErr: false,
		},
		{
			name: "missing webhook URL - nil",
			channel: &database.NotificationChannel{
				WebhookURL: nil,
			},
			wantErr: true,
			errMsg:  "slack channel requires webhook URL",
		},
		{
			name: "missing webhook URL - empty",
			channel: &database.NotificationChannel{
				WebhookURL: strPtr(""),
			},
			wantErr: true,
			errMsg:  "slack channel requires webhook URL",
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

func TestSlackNotifier_Send_Success(t *testing.T) {
	var receivedBody string
	var receivedContentType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			t.Errorf("Failed to read request body: %v", readErr)
		}
		receivedBody = string(body)

		if r.Method != http.MethodPost {
			t.Errorf("Expected POST method, got %s", r.Method)
		}

		w.WriteHeader(http.StatusOK)
		if _, writeErr := w.Write([]byte("ok")); writeErr != nil {
			t.Errorf("Failed to write response: %v", writeErr)
		}
	}))
	defer server.Close()

	renderer := &mockTemplateRenderer{
		renderJSONFunc: func(templateStr string, payload *database.NotificationPayload, defaultTemplate string) (string, error) {
			return `{"text": "Test alert: High CPU"}`, nil
		},
	}

	notifier := NewSlackNotifier(server.Client(), renderer)
	ctx := context.Background()

	channel := &database.NotificationChannel{
		WebhookURL: strPtr(server.URL),
	}

	payload := &database.NotificationPayload{
		NotificationType: string(database.NotificationTypeAlertFire),
		AlertTitle:       "High CPU",
		Severity:         "warning",
	}

	err := notifier.Send(ctx, channel, payload)
	if err != nil {
		t.Errorf("Send() unexpected error: %v", err)
	}

	if receivedContentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", receivedContentType)
	}

	if receivedBody != `{"text": "Test alert: High CPU"}` {
		t.Errorf("Unexpected request body: %s", receivedBody)
	}
}

func TestSlackNotifier_Send_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		// Error ignored in test handler as response may be discarded
		_, _ = w.Write([]byte("internal error")) //nolint:errcheck
	}))
	defer server.Close()

	notifier := NewSlackNotifier(server.Client(), &mockTemplateRenderer{})
	ctx := context.Background()

	channel := &database.NotificationChannel{
		WebhookURL: strPtr(server.URL),
	}

	payload := createTestPayload()

	err := notifier.Send(ctx, channel, payload)
	if err == nil {
		t.Error("Send() expected error for HTTP 500")
	}

	if !strings.Contains(err.Error(), "500") {
		t.Errorf("Error should contain status code 500: %v", err)
	}

	if !strings.Contains(err.Error(), "internal error") {
		t.Errorf("Error should contain response body: %v", err)
	}
}

func TestSlackNotifier_Send_ValidationError(t *testing.T) {
	notifier := NewSlackNotifier(http.DefaultClient, &mockTemplateRenderer{})
	ctx := context.Background()

	channel := &database.NotificationChannel{
		WebhookURL: nil,
	}

	payload := createTestPayload()

	err := notifier.Send(ctx, channel, payload)
	if err == nil {
		t.Error("Send() expected validation error")
	}

	if err.Error() != "slack channel requires webhook URL" {
		t.Errorf("Send() error = %q, want %q", err.Error(), "slack channel requires webhook URL")
	}
}

func TestSlackNotifier_Send_UnknownNotificationType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewSlackNotifier(server.Client(), &mockTemplateRenderer{})
	ctx := context.Background()

	channel := &database.NotificationChannel{
		WebhookURL: strPtr(server.URL),
	}

	payload := &database.NotificationPayload{
		NotificationType: "unknown_type",
	}

	err := notifier.Send(ctx, channel, payload)
	if err == nil {
		t.Error("Send() expected error for unknown notification type")
	}

	if !strings.Contains(err.Error(), "unknown notification type") {
		t.Errorf("Error should mention unknown notification type: %v", err)
	}
}

func TestSlackNotifier_Send_TemplateRendering(t *testing.T) {
	tests := []struct {
		name             string
		notificationType string
		expectedTemplate string
	}{
		{
			name:             "alert fire",
			notificationType: string(database.NotificationTypeAlertFire),
			expectedTemplate: DefaultSlackAlertFireTemplate,
		},
		{
			name:             "alert clear",
			notificationType: string(database.NotificationTypeAlertClear),
			expectedTemplate: DefaultSlackAlertClearTemplate,
		},
		{
			name:             "reminder",
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

			notifier := NewSlackNotifier(server.Client(), renderer)
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
				t.Errorf("Expected template %q, got %q", tt.expectedTemplate, usedTemplate)
			}
		})
	}
}

func TestSlackNotifier_Send_CustomTemplate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	customTemplate := `{"text": "Custom: {{.AlertTitle}}"}`
	usedTemplateStr := ""

	renderer := &mockTemplateRenderer{
		renderJSONFunc: func(templateStr string, payload *database.NotificationPayload, defaultTemplate string) (string, error) {
			usedTemplateStr = templateStr
			return `{"text": "Custom: Test"}`, nil
		},
	}

	notifier := NewSlackNotifier(server.Client(), renderer)
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

func TestSlackNotifier_Send_TemplateRenderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	renderer := &mockTemplateRenderer{
		renderJSONFunc: func(templateStr string, payload *database.NotificationPayload, defaultTemplate string) (string, error) {
			return "", &templateError{msg: "template syntax error"}
		},
	}

	notifier := NewSlackNotifier(server.Client(), renderer)
	ctx := context.Background()

	channel := &database.NotificationChannel{
		WebhookURL: strPtr(server.URL),
	}

	payload := createTestPayload()

	err := notifier.Send(ctx, channel, payload)
	if err == nil {
		t.Error("Send() expected error for template rendering failure")
	}

	if !strings.Contains(err.Error(), "failed to render slack template") {
		t.Errorf("Error should mention template rendering: %v", err)
	}
}

func TestSlackNotifier_Send_ConnectionError(t *testing.T) {
	notifier := NewSlackNotifier(http.DefaultClient, &mockTemplateRenderer{})
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

	if !strings.Contains(err.Error(), "failed to send slack notification") {
		t.Errorf("Error should mention slack notification failure: %v", err)
	}
}

func TestSlackNotifier_Send_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow server
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewSlackNotifier(server.Client(), &mockTemplateRenderer{})

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

// templateError is a simple error type for testing
type templateError struct {
	msg string
}

func (e *templateError) Error() string {
	return e.msg
}
