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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// These are the regression tests for the credential leak in issue #498.
// For Slack and Mattermost the whole webhook URL is the credential, and
// a generic webhook endpoint may carry one in its path or query string,
// so nothing the alerter logs or writes to
// notification_history.error_message may repeat it.

// assertNoWebhookURL fails the test when s repeats any distinctive part
// of webhookSecretPath.
func assertNoWebhookURL(t *testing.T, s string) {
	t.Helper()
	for _, fragment := range []string{"T00000000", "B00000000", "XXXXXXXX", "/services"} {
		if strings.Contains(s, fragment) {
			t.Errorf("error text %q repeats %q of the webhook URL", s, fragment)
		}
	}
}

// unroutableWebhookURL is a URL on a port nothing listens on, with a
// realistic secret path, so that a send against it fails in the
// transport with a *url.Error carrying the whole URL.
const unroutableWebhookURL = "http://127.0.0.1:1" + webhookSecretPath

func TestWebhookSenders_TransportErrorRedactsURL(t *testing.T) {
	tests := []struct {
		name     string
		notifier Notifier
		channel  *database.NotificationChannel
		wantText string
	}{
		{
			name:     "slack",
			notifier: NewSlackNotifier(http.DefaultClient, &mockTemplateRenderer{}),
			channel:  &database.NotificationChannel{WebhookURL: strPtr(unroutableWebhookURL)},
			wantText: "failed to send slack notification",
		},
		{
			name:     "mattermost",
			notifier: NewMattermostNotifier(http.DefaultClient, &mockTemplateRenderer{}),
			channel:  &database.NotificationChannel{WebhookURL: strPtr(unroutableWebhookURL)},
			wantText: "failed to send mattermost notification",
		},
		{
			name:     "generic webhook",
			notifier: NewWebhookNotifierAllowInternal(http.DefaultClient, &mockTemplateRenderer{}),
			channel:  &database.NotificationChannel{EndpointURL: strPtr(unroutableWebhookURL)},
			wantText: "failed to send webhook",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.notifier.Send(context.Background(), tt.channel, createTestPayload())
			if err == nil {
				t.Fatal("Send() expected a transport error")
			}
			if !strings.Contains(err.Error(), tt.wantText) {
				t.Errorf("Send() error = %q, want it to mention %q", err, tt.wantText)
			}
			assertNoWebhookURL(t, err.Error())
		})
	}
}

func TestWebhookSenders_CreateRequestErrorRedactsURL(t *testing.T) {
	// A NUL in the URL makes http.NewRequestWithContext fail with a
	// *url.Error that quotes the raw stored string back, scheme,
	// userinfo and all.
	malformed := "svc:hunter2@127.0.0.1\x00:1" + webhookSecretPath

	tests := []struct {
		name     string
		notifier Notifier
		channel  *database.NotificationChannel
	}{
		{
			name:     "slack",
			notifier: NewSlackNotifier(http.DefaultClient, &mockTemplateRenderer{}),
			channel:  &database.NotificationChannel{WebhookURL: strPtr(malformed)},
		},
		{
			name:     "generic webhook",
			notifier: NewWebhookNotifierAllowInternal(http.DefaultClient, &mockTemplateRenderer{}),
			channel:  &database.NotificationChannel{EndpointURL: strPtr(malformed)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.notifier.Send(context.Background(), tt.channel, createTestPayload())
			if err == nil {
				t.Fatal("Send() expected a request-creation error")
			}
			if !strings.Contains(err.Error(), "failed to create request") {
				t.Errorf("Send() error = %q, want a create-request failure", err)
			}
			if !strings.Contains(err.Error(), "malformed") {
				t.Errorf("Send() error = %q, want it to report a malformed URL", err)
			}
			if strings.Contains(err.Error(), "hunter2") {
				t.Errorf("Send() error = %q repeats the userinfo", err)
			}
			assertNoWebhookURL(t, err.Error())
		})
	}
}

// TestWebhookNotifier_SSRFBlockDoesNotEchoAParseFailure covers the
// SSRF-protection branch. It wraps hostvalidation.ValidateURLHost,
// which wraps url.Parse, and (*url.Error).Error renders the RAW input
// string: not a parsed URL, so it may carry no scheme for the redactor
// to anchor on, and it may carry a password in its userinfo. Nothing
// borrowed from a parse failure may reach the error.
func TestWebhookNotifier_SSRFBlockDoesNotEchoAParseFailure(t *testing.T) {
	tests := []struct {
		name        string
		endpointURL string
		forbidden   []string
	}{
		{
			name:        "a control character in the url",
			endpointURL: "http://webhook.example.com\x00" + webhookSecretPath,
			forbidden:   []string{"T00000000", "/services"},
		},
		{
			// The redactor cannot anchor on a host without a scheme, so
			// echoing this would hand over the whole path.
			name:        "no scheme and an invalid percent escape",
			endpointURL: "hooks.example.com" + webhookSecretPath + "%zz",
			forbidden:   []string{"T00000000", "/services", "hooks.example.com"},
		},
		{
			name:        "a password in the userinfo",
			endpointURL: "https://svc:hunter2@hooks.example.com" + webhookSecretPath + "%zz",
			forbidden:   []string{"hunter2", "T00000000", "/services"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notifier := NewWebhookNotifier(http.DefaultClient, &mockTemplateRenderer{})
			channel := &database.NotificationChannel{EndpointURL: strPtr(tt.endpointURL)}

			err := notifier.Send(context.Background(), channel, createTestPayload())
			if err == nil {
				t.Fatal("Send() expected the endpoint to be blocked")
			}
			if !strings.Contains(err.Error(), "SSRF protection") {
				t.Errorf("Send() error = %q, want it to mention SSRF protection", err)
			}
			if !strings.Contains(err.Error(), "malformed") {
				t.Errorf("Send() error = %q, want it to report a malformed URL", err)
			}
			for _, fragment := range tt.forbidden {
				if strings.Contains(err.Error(), fragment) {
					t.Errorf("Send() error = %q repeats %q of the endpoint URL",
						err, fragment)
				}
			}
			assertNoWebhookURL(t, err.Error())
		})
	}
}

// TestWebhookNotifier_SSRFBlockNamesTheHost is the other side of the
// branch above: a URL that parses but resolves somewhere internal is
// reported with the host, which is what the operator needs and is not
// the credential.
func TestWebhookNotifier_SSRFBlockNamesTheHost(t *testing.T) {
	notifier := NewWebhookNotifier(http.DefaultClient, &mockTemplateRenderer{})
	channel := &database.NotificationChannel{
		EndpointURL: strPtr("http://127.0.0.1" + webhookSecretPath),
	}

	err := notifier.Send(context.Background(), channel, createTestPayload())
	if err == nil {
		t.Fatal("Send() expected the endpoint to be blocked")
	}
	if !strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("Send() error = %q, want it to name the blocked host", err)
	}
	assertNoWebhookURL(t, err.Error())
}

// TestWebhookSenders_ResponseBodyIsSanitizedAndCapped covers the other
// half of the fix: a failing endpoint's body is borrowed text, so it is
// capped and stripped of control characters before it reaches the log
// or notification_history.
func TestWebhookSenders_ResponseBodyIsSanitizedAndCapped(t *testing.T) {
	body := strings.Repeat("A", 5000) + "\n[ERROR] forged log line\r\n" +
		"https://hooks.slack.com" + webhookSecretPath

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("failed to write the test response body: %v", err)
		}
	}))
	defer server.Close()

	tests := []struct {
		name     string
		notifier Notifier
		channel  *database.NotificationChannel
	}{
		{
			name:     "slack",
			notifier: NewSlackNotifier(server.Client(), &mockTemplateRenderer{}),
			channel:  &database.NotificationChannel{WebhookURL: strPtr(server.URL)},
		},
		{
			name:     "generic webhook",
			notifier: NewWebhookNotifierAllowInternal(server.Client(), &mockTemplateRenderer{}),
			channel:  &database.NotificationChannel{EndpointURL: strPtr(server.URL)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.notifier.Send(context.Background(), tt.channel, createTestPayload())
			if err == nil {
				t.Fatal("Send() expected an error for a 500 response")
			}
			got := err.Error()
			// The echoed body is capped; the surrounding wording is
			// short, so a generous allowance still proves the cap ran.
			if len(got) > maxEchoedBytes+len(echoTruncationMarker)+64 {
				t.Errorf("Send() error is %d bytes, want the body capped near %d",
					len(got), maxEchoedBytes)
			}
			if strings.ContainsAny(got, "\n\r") {
				t.Errorf("Send() error = %q, want no line breaks", got)
			}
		})
	}
}
