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

func TestWebhookNotifier_Type(t *testing.T) {
	notifier := NewWebhookNotifier(http.DefaultClient, &mockTemplateRenderer{})
	if got := notifier.Type(); got != database.ChannelTypeWebhook {
		t.Errorf("Type() = %v, want %v", got, database.ChannelTypeWebhook)
	}
}

func TestWebhookNotifier_Validate(t *testing.T) {
	notifier := NewWebhookNotifier(http.DefaultClient, &mockTemplateRenderer{})

	tests := []struct {
		name    string
		channel *database.NotificationChannel
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid channel - POST",
			channel: &database.NotificationChannel{
				EndpointURL: strPtr("https://api.example.com/webhook"),
				HTTPMethod:  "POST",
			},
			wantErr: false,
		},
		{
			name: "valid channel - GET",
			channel: &database.NotificationChannel{
				EndpointURL: strPtr("https://api.example.com/webhook"),
				HTTPMethod:  "GET",
			},
			wantErr: false,
		},
		{
			name: "valid channel - PUT",
			channel: &database.NotificationChannel{
				EndpointURL: strPtr("https://api.example.com/webhook"),
				HTTPMethod:  "PUT",
			},
			wantErr: false,
		},
		{
			name: "valid channel - PATCH",
			channel: &database.NotificationChannel{
				EndpointURL: strPtr("https://api.example.com/webhook"),
				HTTPMethod:  "PATCH",
			},
			wantErr: false,
		},
		{
			name: "valid channel - default method",
			channel: &database.NotificationChannel{
				EndpointURL: strPtr("https://api.example.com/webhook"),
				HTTPMethod:  "", // Should default to POST
			},
			wantErr: false,
		},
		{
			name: "missing endpoint URL - nil",
			channel: &database.NotificationChannel{
				EndpointURL: nil,
			},
			wantErr: true,
			errMsg:  "webhook channel requires endpoint URL",
		},
		{
			name: "missing endpoint URL - empty",
			channel: &database.NotificationChannel{
				EndpointURL: strPtr(""),
			},
			wantErr: true,
			errMsg:  "webhook channel requires endpoint URL",
		},
		{
			name: "invalid HTTP method",
			channel: &database.NotificationChannel{
				EndpointURL: strPtr("https://api.example.com/webhook"),
				HTTPMethod:  "DELETE",
			},
			wantErr: true,
			errMsg:  "invalid HTTP method: DELETE",
		},
		{
			name: "invalid HTTP method - lowercase",
			channel: &database.NotificationChannel{
				EndpointURL: strPtr("https://api.example.com/webhook"),
				HTTPMethod:  "post", // Case sensitive
			},
			wantErr: true,
			errMsg:  "invalid HTTP method: post",
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

func TestWebhookNotifier_Send_POSTSuccess(t *testing.T) {
	var receivedBody string
	var receivedContentType string
	var receivedMethod string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedContentType = r.Header.Get("Content-Type")
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			t.Errorf("Failed to read request body: %v", readErr)
		}
		receivedBody = string(body)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	renderer := &mockTemplateRenderer{
		renderJSONFunc: func(templateStr string, payload *database.NotificationPayload, defaultTemplate string) (string, error) {
			return `{"event": "alert", "title": "Test"}`, nil
		},
	}

	notifier := NewWebhookNotifier(server.Client(), renderer)
	ctx := context.Background()

	channel := &database.NotificationChannel{
		EndpointURL: strPtr(server.URL),
		HTTPMethod:  "POST",
	}

	payload := createTestPayload()

	err := notifier.Send(ctx, channel, payload)
	if err != nil {
		t.Errorf("Send() unexpected error: %v", err)
	}

	if receivedMethod != "POST" {
		t.Errorf("Expected POST method, got %s", receivedMethod)
	}

	if receivedContentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", receivedContentType)
	}

	if receivedBody != `{"event": "alert", "title": "Test"}` {
		t.Errorf("Unexpected request body: %s", receivedBody)
	}
}

func TestWebhookNotifier_Send_GETMethod(t *testing.T) {
	var receivedMethod string
	var receivedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		var readErr error
		receivedBody, readErr = io.ReadAll(r.Body)
		if readErr != nil {
			t.Errorf("Failed to read request body: %v", readErr)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewWebhookNotifier(server.Client(), &mockTemplateRenderer{})
	ctx := context.Background()

	channel := &database.NotificationChannel{
		EndpointURL: strPtr(server.URL),
		HTTPMethod:  "GET",
	}

	payload := createTestPayload()

	err := notifier.Send(ctx, channel, payload)
	if err != nil {
		t.Errorf("Send() unexpected error: %v", err)
	}

	if receivedMethod != "GET" {
		t.Errorf("Expected GET method, got %s", receivedMethod)
	}

	// GET requests should not have a body
	if len(receivedBody) > 0 {
		t.Errorf("GET request should not have body, got: %s", string(receivedBody))
	}
}

func TestWebhookNotifier_Send_DefaultMethod(t *testing.T) {
	var receivedMethod string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewWebhookNotifier(server.Client(), &mockTemplateRenderer{})
	ctx := context.Background()

	channel := &database.NotificationChannel{
		EndpointURL: strPtr(server.URL),
		HTTPMethod:  "", // Should default to POST
	}

	payload := createTestPayload()

	err := notifier.Send(ctx, channel, payload)
	if err != nil {
		t.Errorf("Send() unexpected error: %v", err)
	}

	if receivedMethod != "POST" {
		t.Errorf("Expected default POST method, got %s", receivedMethod)
	}
}

func TestWebhookNotifier_Send_CustomHeaders(t *testing.T) {
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewWebhookNotifier(server.Client(), &mockTemplateRenderer{})
	ctx := context.Background()

	channel := &database.NotificationChannel{
		EndpointURL: strPtr(server.URL),
		HTTPMethod:  "POST",
		Headers: map[string]string{
			"X-Custom-Header": "custom-value",
			"X-API-Version":   "v1",
		},
	}

	payload := createTestPayload()

	err := notifier.Send(ctx, channel, payload)
	if err != nil {
		t.Errorf("Send() unexpected error: %v", err)
	}

	if receivedHeaders.Get("X-Custom-Header") != "custom-value" {
		t.Errorf("Expected X-Custom-Header: custom-value, got %s", receivedHeaders.Get("X-Custom-Header"))
	}

	if receivedHeaders.Get("X-API-Version") != "v1" {
		t.Errorf("Expected X-API-Version: v1, got %s", receivedHeaders.Get("X-API-Version"))
	}
}

func TestWebhookNotifier_Send_BasicAuth(t *testing.T) {
	var receivedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewWebhookNotifier(server.Client(), &mockTemplateRenderer{})
	ctx := context.Background()

	channel := &database.NotificationChannel{
		EndpointURL:     strPtr(server.URL),
		HTTPMethod:      "POST",
		AuthType:        strPtr("basic"),
		AuthCredentials: strPtr("myuser:mypassword"),
	}

	payload := createTestPayload()

	err := notifier.Send(ctx, channel, payload)
	if err != nil {
		t.Errorf("Send() unexpected error: %v", err)
	}

	if !strings.HasPrefix(receivedAuth, "Basic ") {
		t.Errorf("Expected Basic auth header, got %s", receivedAuth)
	}
}

func TestWebhookNotifier_Send_BearerAuth(t *testing.T) {
	var receivedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewWebhookNotifier(server.Client(), &mockTemplateRenderer{})
	ctx := context.Background()

	channel := &database.NotificationChannel{
		EndpointURL:     strPtr(server.URL),
		HTTPMethod:      "POST",
		AuthType:        strPtr("bearer"),
		AuthCredentials: strPtr("my-secret-token"),
	}

	payload := createTestPayload()

	err := notifier.Send(ctx, channel, payload)
	if err != nil {
		t.Errorf("Send() unexpected error: %v", err)
	}

	if receivedAuth != "Bearer my-secret-token" {
		t.Errorf("Expected Bearer auth header, got %s", receivedAuth)
	}
}

func TestWebhookNotifier_Send_APIKeyAuth(t *testing.T) {
	var receivedAPIKey string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAPIKey = r.Header.Get("X-API-Key")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewWebhookNotifier(server.Client(), &mockTemplateRenderer{})
	ctx := context.Background()

	channel := &database.NotificationChannel{
		EndpointURL:     strPtr(server.URL),
		HTTPMethod:      "POST",
		AuthType:        strPtr("api_key"),
		AuthCredentials: strPtr("X-API-Key:my-api-key-12345"),
	}

	payload := createTestPayload()

	err := notifier.Send(ctx, channel, payload)
	if err != nil {
		t.Errorf("Send() unexpected error: %v", err)
	}

	if receivedAPIKey != "my-api-key-12345" {
		t.Errorf("Expected API key header, got %s", receivedAPIKey)
	}
}

func TestWebhookNotifier_Send_2xxSuccess(t *testing.T) {
	// Test various 2xx status codes are treated as success
	successCodes := []int{200, 201, 202, 204}

	for _, code := range successCodes {
		t.Run(http.StatusText(code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
			}))
			defer server.Close()

			notifier := NewWebhookNotifier(server.Client(), &mockTemplateRenderer{})
			ctx := context.Background()

			channel := &database.NotificationChannel{
				EndpointURL: strPtr(server.URL),
				HTTPMethod:  "POST",
			}

			payload := createTestPayload()

			err := notifier.Send(ctx, channel, payload)
			if err != nil {
				t.Errorf("Send() unexpected error for %d: %v", code, err)
			}
		})
	}
}

func TestWebhookNotifier_Send_HTTPErrors(t *testing.T) {
	// Test various non-2xx status codes result in errors
	errorCodes := []int{300, 400, 401, 403, 404, 500, 502, 503}

	for _, code := range errorCodes {
		t.Run(http.StatusText(code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
				// Error ignored in test handler as response may be discarded
				_, _ = w.Write([]byte("error response")) //nolint:errcheck
			}))
			defer server.Close()

			notifier := NewWebhookNotifier(server.Client(), &mockTemplateRenderer{})
			ctx := context.Background()

			channel := &database.NotificationChannel{
				EndpointURL: strPtr(server.URL),
				HTTPMethod:  "POST",
			}

			payload := createTestPayload()

			err := notifier.Send(ctx, channel, payload)
			if err == nil {
				t.Errorf("Send() expected error for %d", code)
			}

			if !strings.Contains(err.Error(), "error response") {
				t.Errorf("Error should contain response body: %v", err)
			}
		})
	}
}

func TestWebhookNotifier_Send_ValidationError(t *testing.T) {
	notifier := NewWebhookNotifier(http.DefaultClient, &mockTemplateRenderer{})
	ctx := context.Background()

	channel := &database.NotificationChannel{
		EndpointURL: nil,
	}

	payload := createTestPayload()

	err := notifier.Send(ctx, channel, payload)
	if err == nil {
		t.Error("Send() expected validation error")
	}

	if err.Error() != "webhook channel requires endpoint URL" {
		t.Errorf("Send() error = %q", err.Error())
	}
}

func TestWebhookNotifier_Send_UnknownNotificationType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewWebhookNotifier(server.Client(), &mockTemplateRenderer{})
	ctx := context.Background()

	channel := &database.NotificationChannel{
		EndpointURL: strPtr(server.URL),
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

func TestWebhookNotifier_Send_TemplateRendering(t *testing.T) {
	tests := []struct {
		name             string
		notificationType string
		expectedTemplate string
	}{
		{
			name:             "alert fire",
			notificationType: string(database.NotificationTypeAlertFire),
			expectedTemplate: DefaultWebhookAlertFireTemplate,
		},
		{
			name:             "alert clear",
			notificationType: string(database.NotificationTypeAlertClear),
			expectedTemplate: DefaultWebhookAlertClearTemplate,
		},
		{
			name:             "reminder",
			notificationType: string(database.NotificationTypeReminder),
			expectedTemplate: DefaultWebhookReminderTemplate,
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
					return `{"event": "test"}`, nil
				},
			}

			notifier := NewWebhookNotifier(server.Client(), renderer)
			ctx := context.Background()

			channel := &database.NotificationChannel{
				EndpointURL: strPtr(server.URL),
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
				t.Errorf("Expected template %q", tt.expectedTemplate)
			}
		})
	}
}

func TestWebhookNotifier_Send_CustomTemplate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	customTemplate := `{"custom": true, "alert": "{{.AlertTitle}}"}`
	usedTemplateStr := ""

	renderer := &mockTemplateRenderer{
		renderJSONFunc: func(templateStr string, payload *database.NotificationPayload, defaultTemplate string) (string, error) {
			usedTemplateStr = templateStr
			return `{"custom": true, "alert": "Test"}`, nil
		},
	}

	notifier := NewWebhookNotifier(server.Client(), renderer)
	ctx := context.Background()

	channel := &database.NotificationChannel{
		EndpointURL:       strPtr(server.URL),
		TemplateAlertFire: strPtr(customTemplate),
	}

	payload := createTestPayload()

	err := notifier.Send(ctx, channel, payload)
	if err != nil {
		t.Errorf("Send() unexpected error: %v", err)
	}

	if usedTemplateStr != customTemplate {
		t.Errorf("Expected custom template to be used, got %q", usedTemplateStr)
	}
}

func TestWebhookNotifier_Send_TemplateRenderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	renderer := &mockTemplateRenderer{
		renderJSONFunc: func(templateStr string, payload *database.NotificationPayload, defaultTemplate string) (string, error) {
			return "", &templateError{msg: "template error"}
		},
	}

	notifier := NewWebhookNotifier(server.Client(), renderer)
	ctx := context.Background()

	channel := &database.NotificationChannel{
		EndpointURL: strPtr(server.URL),
	}

	payload := createTestPayload()

	err := notifier.Send(ctx, channel, payload)
	if err == nil {
		t.Error("Send() expected error for template rendering failure")
	}

	if !strings.Contains(err.Error(), "failed to render webhook template") {
		t.Errorf("Error should mention webhook template rendering: %v", err)
	}
}

func TestWebhookNotifier_Send_ConnectionError(t *testing.T) {
	notifier := NewWebhookNotifier(http.DefaultClient, &mockTemplateRenderer{})
	ctx := context.Background()

	channel := &database.NotificationChannel{
		EndpointURL: strPtr("http://localhost:1"),
	}

	payload := createTestPayload()

	err := notifier.Send(ctx, channel, payload)
	if err == nil {
		t.Error("Send() expected connection error")
	}

	if !strings.Contains(err.Error(), "failed to send webhook") {
		t.Errorf("Error should mention webhook failure: %v", err)
	}
}

func TestWebhookNotifier_Send_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewWebhookNotifier(server.Client(), &mockTemplateRenderer{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	channel := &database.NotificationChannel{
		EndpointURL: strPtr(server.URL),
	}

	payload := createTestPayload()

	err := notifier.Send(ctx, channel, payload)
	if err == nil {
		t.Error("Send() expected context cancellation error")
	}
}

func TestWebhookNotifier_Send_PUTMethod(t *testing.T) {
	var receivedMethod string
	var receivedBody string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			t.Errorf("Failed to read request body: %v", readErr)
		}
		receivedBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewWebhookNotifier(server.Client(), &mockTemplateRenderer{})
	ctx := context.Background()

	channel := &database.NotificationChannel{
		EndpointURL: strPtr(server.URL),
		HTTPMethod:  "PUT",
	}

	payload := createTestPayload()

	err := notifier.Send(ctx, channel, payload)
	if err != nil {
		t.Errorf("Send() unexpected error: %v", err)
	}

	if receivedMethod != "PUT" {
		t.Errorf("Expected PUT method, got %s", receivedMethod)
	}

	// PUT should include body
	if receivedBody == "" {
		t.Error("PUT request should have body")
	}
}

func TestWebhookNotifier_Send_PATCHMethod(t *testing.T) {
	var receivedMethod string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewWebhookNotifier(server.Client(), &mockTemplateRenderer{})
	ctx := context.Background()

	channel := &database.NotificationChannel{
		EndpointURL: strPtr(server.URL),
		HTTPMethod:  "PATCH",
	}

	payload := createTestPayload()

	err := notifier.Send(ctx, channel, payload)
	if err != nil {
		t.Errorf("Send() unexpected error: %v", err)
	}

	if receivedMethod != "PATCH" {
		t.Errorf("Expected PATCH method, got %s", receivedMethod)
	}
}

func TestWebhookNotifier_Send_NoContentTypeForGET(t *testing.T) {
	var receivedContentType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewWebhookNotifier(server.Client(), &mockTemplateRenderer{})
	ctx := context.Background()

	channel := &database.NotificationChannel{
		EndpointURL: strPtr(server.URL),
		HTTPMethod:  "GET",
	}

	payload := createTestPayload()

	err := notifier.Send(ctx, channel, payload)
	if err != nil {
		t.Errorf("Send() unexpected error: %v", err)
	}

	// GET requests should not have Content-Type set by our code
	if receivedContentType != "" {
		t.Errorf("GET request should not have Content-Type, got %s", receivedContentType)
	}
}
