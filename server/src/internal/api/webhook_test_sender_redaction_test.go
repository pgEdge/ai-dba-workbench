/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// These are the regression tests for the credential leak in issue #498.
// For Slack and Mattermost the whole webhook URL is the credential, and
// a generic webhook endpoint may carry one in its path or query string,
// so nothing the "send test" handler logs may repeat it.

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

func TestSendTestWebhook_TransportErrorRedactsURL(t *testing.T) {
	err := sendTestWebhook(unroutableWebhookURL, "Slack")
	if err == nil {
		t.Fatal("expected an error for an unreachable host")
	}
	if !strings.Contains(err.Error(), "failed to send request") {
		t.Errorf("error = %q, want a send-request failure", err)
	}
	assertNoWebhookURL(t, err.Error())
}

func TestSendTestGenericWebhook_TransportErrorRedactsURL(t *testing.T) {
	err := sendTestGenericWebhook(unroutableWebhookURL, http.MethodPost, nil, "", "")
	if err == nil {
		t.Fatal("expected an error for an unreachable host")
	}
	if !strings.Contains(err.Error(), "failed to send request") {
		t.Errorf("error = %q, want a send-request failure", err)
	}
	assertNoWebhookURL(t, err.Error())
}

// TestSendTestWebhook_CreateRequestErrorRedactsURL drives the
// http.NewRequest failure branch. Its *url.Error renders the RAW input
// string, which may carry no scheme for the redactor to anchor on and
// may carry a password in its userinfo, so nothing borrowed from it may
// reach the error at all.
func TestSendTestWebhook_CreateRequestErrorRedactsURL(t *testing.T) {
	malformed := "svc:hunter2@127.0.0.1\x00:1" + webhookSecretPath

	t.Run("slack or mattermost", func(t *testing.T) {
		err := sendTestWebhook(malformed, "Slack")
		if err == nil {
			t.Fatal("expected an error for a malformed URL")
		}
		if !strings.Contains(err.Error(), "failed to create request") {
			t.Errorf("error = %q, want a create-request failure", err)
		}
		if !strings.Contains(err.Error(), "malformed") {
			t.Errorf("error = %q, want it to report a malformed URL", err)
		}
		if strings.Contains(err.Error(), "hunter2") {
			t.Errorf("error = %q repeats the userinfo", err)
		}
		assertNoWebhookURL(t, err.Error())
	})

	t.Run("generic webhook", func(t *testing.T) {
		err := sendTestGenericWebhook(malformed, http.MethodPost, nil, "", "")
		if err == nil {
			t.Fatal("expected an error for a malformed URL")
		}
		if !strings.Contains(err.Error(), "failed to create request") {
			t.Errorf("error = %q, want a create-request failure", err)
		}
		if !strings.Contains(err.Error(), "malformed") {
			t.Errorf("error = %q, want it to report a malformed URL", err)
		}
		if strings.Contains(err.Error(), "hunter2") {
			t.Errorf("error = %q repeats the userinfo", err)
		}
		assertNoWebhookURL(t, err.Error())
	})
}

// TestSendTestWebhook_SchemeLessURLIsNotEchoed covers the stored URL
// that never had a scheme. The redactor anchors on "://", so it cannot
// mask such a path; the call site is what stops that mattering, by
// never echoing a parse failure.
func TestSendTestWebhook_SchemeLessURLIsNotEchoed(t *testing.T) {
	malformed := "hooks.example.com" + webhookSecretPath + "%zz"

	t.Run("slack or mattermost", func(t *testing.T) {
		err := sendTestWebhook(malformed, "Slack")
		if err == nil {
			t.Fatal("expected an error for a malformed URL")
		}
		assertNoWebhookURL(t, err.Error())
	})

	t.Run("generic webhook", func(t *testing.T) {
		err := sendTestGenericWebhook(malformed, http.MethodPost, nil, "", "")
		if err == nil {
			t.Fatal("expected an error for a malformed URL")
		}
		assertNoWebhookURL(t, err.Error())
	})
}

// TestSendTestWebhook_ResponseBodyIsSanitizedAndCapped covers the other
// half of the fix: a failing endpoint's body is borrowed text, so it is
// capped and stripped of control characters before it reaches the log.
func TestSendTestWebhook_ResponseBodyIsSanitizedAndCapped(t *testing.T) {
	body := strings.Repeat("A", 5000) + "\n[ERROR] forged log line\r\n" +
		"https://hooks.slack.com" + webhookSecretPath

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	tests := []struct {
		name string
		send func() error
	}{
		{
			name: "slack or mattermost",
			send: func() error { return sendTestWebhook(server.URL, "Slack") },
		},
		{
			name: "generic webhook",
			send: func() error {
				return sendTestGenericWebhook(server.URL, http.MethodPost, nil, "", "")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.send()
			if err == nil {
				t.Fatal("expected an error for a 500 response")
			}
			got := err.Error()
			if len(got) > maxEchoedBytes+len(echoTruncationMarker)+64 {
				t.Errorf("error is %d bytes, want the body capped near %d",
					len(got), maxEchoedBytes)
			}
			if strings.ContainsAny(got, "\n\r") {
				t.Errorf("error = %q, want no line breaks", got)
			}
			assertNoWebhookURL(t, got)
		})
	}
}
