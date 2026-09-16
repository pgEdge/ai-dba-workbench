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
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// testTelegramToken is a syntactically realistic bot token. Tests assert
// that it never appears in an error string.
const testTelegramToken = "123456789:AAFakeTokenValueForTestsOnly_0123456789"

// newTestTelegramNotifier builds a notifier pointed at an httptest
// server. apiBaseURL is unexported and deliberately not settable from
// configuration, so the test reaches it through the concrete type.
func newTestTelegramNotifier(t *testing.T, client *http.Client, renderer TemplateRenderer, baseURL string) Notifier {
	t.Helper()
	n, ok := NewTelegramNotifier(client, renderer).(*telegramNotifier)
	if !ok {
		t.Fatalf("NewTelegramNotifier returned unexpected type")
	}
	n.apiBaseURL = baseURL
	return n
}

// telegramTestChannel returns a fully configured Telegram channel.
func telegramTestChannel() *database.NotificationChannel {
	return &database.NotificationChannel{
		ChannelType:      database.ChannelTypeTelegram,
		TelegramBotToken: strPtr(testTelegramToken),
		TelegramChatID:   strPtr("-1001234567890"),
	}
}

// okHandler writes the Bot API's success envelope.
func okHandler(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":42}}`)) //nolint:errcheck
}

func TestTelegramNotifier_Type(t *testing.T) {
	notifier := NewTelegramNotifier(http.DefaultClient, &mockTemplateRenderer{})
	if got := notifier.Type(); got != database.ChannelTypeTelegram {
		t.Errorf("Type() = %v, want %v", got, database.ChannelTypeTelegram)
	}
}

// TestTelegramNotifier_DefaultsToPublicAPIHost locks in that the
// notifier talks to api.telegram.org unless a test overrides it.
func TestTelegramNotifier_DefaultsToPublicAPIHost(t *testing.T) {
	n, ok := NewTelegramNotifier(http.DefaultClient, &mockTemplateRenderer{}).(*telegramNotifier)
	if !ok {
		t.Fatal("NewTelegramNotifier returned unexpected type")
	}
	if n.apiBaseURL != telegramAPIBaseURL {
		t.Errorf("apiBaseURL = %q, want %q", n.apiBaseURL, telegramAPIBaseURL)
	}
	if telegramAPIBaseURL != "https://api.telegram.org" {
		t.Errorf("telegramAPIBaseURL = %q, want https://api.telegram.org",
			telegramAPIBaseURL)
	}
}

func TestTelegramNotifier_Validate(t *testing.T) {
	notifier := NewTelegramNotifier(http.DefaultClient, &mockTemplateRenderer{})

	tests := []struct {
		name    string
		channel *database.NotificationChannel
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid channel",
			channel: telegramTestChannel(),
			wantErr: false,
		},
		{
			name: "missing bot token - nil",
			channel: &database.NotificationChannel{
				TelegramChatID: strPtr("-100"),
			},
			wantErr: true,
			errMsg:  "telegram channel requires a bot token",
		},
		{
			name: "missing bot token - empty",
			channel: &database.NotificationChannel{
				TelegramBotToken: strPtr(""),
				TelegramChatID:   strPtr("-100"),
			},
			wantErr: true,
			errMsg:  "telegram channel requires a bot token",
		},
		{
			name: "missing chat ID - nil",
			channel: &database.NotificationChannel{
				TelegramBotToken: strPtr(testTelegramToken),
			},
			wantErr: true,
			errMsg:  "telegram channel requires a chat ID",
		},
		{
			name: "missing chat ID - empty",
			channel: &database.NotificationChannel{
				TelegramBotToken: strPtr(testTelegramToken),
				TelegramChatID:   strPtr(""),
			},
			wantErr: true,
			errMsg:  "telegram channel requires a chat ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := notifier.Validate(tt.channel)
			if tt.wantErr {
				if err == nil {
					t.Fatal("Validate() expected error, got nil")
				}
				if err.Error() != tt.errMsg {
					t.Errorf("Validate() error = %q, want %q", err.Error(), tt.errMsg)
				}
				return
			}
			if err != nil {
				t.Errorf("Validate() unexpected error: %v", err)
			}
		})
	}
}

// TestTelegramNotifier_Send_RequestShape covers the request path, method,
// content type and JSON body of a successful send.
func TestTelegramNotifier_Send_RequestShape(t *testing.T) {
	var (
		gotPath        string
		gotMethod      string
		gotContentType string
		gotBody        []byte
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read request body: %v", err)
		}
		gotBody = body
		okHandler(w)
	}))
	defer server.Close()

	renderer := &mockTemplateRenderer{
		renderHTMLFunc: func(string, *database.NotificationPayload, string) (string, error) {
			return "🔴 <b>Alert: High Memory</b>", nil
		},
	}

	notifier := newTestTelegramNotifier(t, server.Client(), renderer, server.URL)

	err := notifier.Send(context.Background(), telegramTestChannel(), createTestPayload())
	if err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	wantPath := "/bot" + testTelegramToken + "/sendMessage"
	if gotPath != wantPath {
		t.Errorf("request path = %q, want %q", gotPath, wantPath)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("request method = %q, want POST", gotMethod)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}

	var decoded telegramSendMessageRequest
	if err := json.Unmarshal(gotBody, &decoded); err != nil {
		t.Fatalf("request body is not valid JSON: %v (%s)", err, gotBody)
	}
	if decoded.ChatID != "-1001234567890" {
		t.Errorf("chat_id = %q, want %q", decoded.ChatID, "-1001234567890")
	}
	if decoded.Text != "🔴 <b>Alert: High Memory</b>" {
		t.Errorf("text = %q, want the rendered message", decoded.Text)
	}
	if decoded.ParseMode != "HTML" {
		t.Errorf("parse_mode = %q, want HTML", decoded.ParseMode)
	}
	if !decoded.DisableWebPagePreview {
		t.Error("disable_web_page_preview = false, want true")
	}

	// chat_id must be a JSON string, not a number.
	var raw map[string]any
	if err := json.Unmarshal(gotBody, &raw); err != nil {
		t.Fatalf("request body is not a JSON object: %v", err)
	}
	if _, ok := raw["chat_id"].(string); !ok {
		t.Errorf("chat_id should be a JSON string, got %T", raw["chat_id"])
	}
}

// TestTelegramNotifier_Send_SelectsTemplatePerType checks that each
// notification type picks its matching Telegram default, and that a
// per-channel override is passed through.
func TestTelegramNotifier_Send_SelectsTemplatePerType(t *testing.T) {
	tests := []struct {
		name             string
		notificationType string
		override         *string
		wantDefault      string
		wantTemplate     string
	}{
		{
			name:             "alert fire",
			notificationType: string(database.NotificationTypeAlertFire),
			wantDefault:      DefaultTelegramAlertFireTemplate,
		},
		{
			name:             "alert clear",
			notificationType: string(database.NotificationTypeAlertClear),
			wantDefault:      DefaultTelegramAlertClearTemplate,
		},
		{
			name:             "reminder",
			notificationType: string(database.NotificationTypeReminder),
			wantDefault:      DefaultTelegramReminderTemplate,
		},
		{
			name:             "alert fire with channel override",
			notificationType: string(database.NotificationTypeAlertFire),
			override:         strPtr("<b>custom {{.AlertTitle}}</b>"),
			wantDefault:      DefaultTelegramAlertFireTemplate,
			wantTemplate:     "<b>custom {{.AlertTitle}}</b>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotBody []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotBody, _ = io.ReadAll(r.Body) //nolint:errcheck
				okHandler(w)
			}))
			defer server.Close()

			var gotDefault, gotTemplate string
			renderer := &mockTemplateRenderer{
				renderHTMLFunc: func(templateStr string, _ *database.NotificationPayload, defaultTemplate string) (string, error) {
					gotTemplate = templateStr
					gotDefault = defaultTemplate
					return "<b>rendered</b>", nil
				},
			}

			channel := telegramTestChannel()
			channel.TemplateAlertFire = tt.override

			payload := createTestPayload()
			payload.NotificationType = tt.notificationType

			notifier := newTestTelegramNotifier(t, server.Client(), renderer, server.URL)
			if err := notifier.Send(context.Background(), channel, payload); err != nil {
				t.Fatalf("Send() unexpected error: %v", err)
			}

			if gotDefault != tt.wantDefault {
				t.Errorf("default template = %q, want the matching Telegram default", gotDefault)
			}
			if gotTemplate != tt.wantTemplate {
				t.Errorf("channel template = %q, want %q", gotTemplate, tt.wantTemplate)
			}

			var decoded telegramSendMessageRequest
			if err := json.Unmarshal(gotBody, &decoded); err != nil {
				t.Fatalf("request body is not valid JSON: %v", err)
			}
			if decoded.Text != "<b>rendered</b>" {
				t.Errorf("text = %q, want the rendered message", decoded.Text)
			}
		})
	}
}

// TestTelegramNotifier_Send_RendersDefaultTemplates runs the real
// renderer over each default template and asserts the message text
// carries the information the Slack defaults carry.
func TestTelegramNotifier_Send_RendersDefaultTemplates(t *testing.T) {
	triggered := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	cleared := triggered.Add(90 * time.Minute)
	metric := "cache_hit_ratio"
	dbName := "orders"

	tests := []struct {
		name             string
		notificationType string
		wantSubstrings   []string
	}{
		{
			name:             "alert fire",
			notificationType: string(database.NotificationTypeAlertFire),
			wantSubstrings: []string{
				"<b>Alert: Disk nearly full</b>",
				"test-server",
				"<code>localhost:5432</code>",
				"critical",
				"<code>orders</code>",
				"<code>cache_hit_ratio</code>",
				"Only 3% left",
			},
		},
		{
			name:             "alert clear",
			notificationType: string(database.NotificationTypeAlertClear),
			wantSubstrings: []string{
				"✅ <b>Resolved: Disk nearly full</b>",
				"<b>Duration:</b> 1h 30m",
				"test-server",
			},
		},
		{
			name:             "reminder",
			notificationType: string(database.NotificationTypeReminder),
			wantSubstrings: []string{
				"⏰ <b>Reminder: Disk nearly full</b>",
				"<b>Reminder:</b> #7",
				"2026-03-01 09:00:00 UTC",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotBody []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotBody, _ = io.ReadAll(r.Body) //nolint:errcheck
				okHandler(w)
			}))
			defer server.Close()

			payload := &database.NotificationPayload{
				AlertID:          9,
				AlertType:        "metric",
				AlertTitle:       "Disk nearly full",
				AlertDescription: "Only 3% left",
				Severity:         "critical",
				Status:           "active",
				TriggeredAt:      triggered,
				MetricName:       &metric,
				DatabaseName:     &dbName,
				ConnectionID:     1,
				ServerName:       "test-server",
				ServerHost:       "localhost",
				ServerPort:       5432,
				NotificationType: tt.notificationType,
				ReminderCount:    7,
				Timestamp:        triggered,
			}
			if tt.notificationType == string(database.NotificationTypeAlertClear) {
				payload.ClearedAt = &cleared
			}

			notifier := newTestTelegramNotifier(t, server.Client(),
				NewTemplateRenderer(), server.URL)
			if err := notifier.Send(context.Background(), telegramTestChannel(), payload); err != nil {
				t.Fatalf("Send() unexpected error: %v", err)
			}

			var decoded telegramSendMessageRequest
			if err := json.Unmarshal(gotBody, &decoded); err != nil {
				t.Fatalf("request body is not valid JSON: %v", err)
			}
			for _, want := range tt.wantSubstrings {
				if !strings.Contains(decoded.Text, want) {
					t.Errorf("message text is missing %q\ngot:\n%s", want, decoded.Text)
				}
			}
			if n := len([]rune(decoded.Text)); n > telegramMaxMessageRunes {
				t.Errorf("message text is %d runes, over the %d limit", n,
					telegramMaxMessageRunes)
			}
		})
	}
}

// TestTelegramNotifier_Send_EscapesMarkupInPayload checks that markup
// characters arriving in alert text reach Telegram as entities while the
// template's own tags survive.
func TestTelegramNotifier_Send_EscapesMarkupInPayload(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body) //nolint:errcheck
		okHandler(w)
	}))
	defer server.Close()

	payload := createTestPayload()
	payload.AlertTitle = `idx_a<b & "c" > d`

	notifier := newTestTelegramNotifier(t, server.Client(), NewTemplateRenderer(), server.URL)
	if err := notifier.Send(context.Background(), telegramTestChannel(), payload); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	var decoded telegramSendMessageRequest
	if err := json.Unmarshal(gotBody, &decoded); err != nil {
		t.Fatalf("request body is not valid JSON: %v", err)
	}

	for _, want := range []string{"idx_a&lt;b", "&amp;", "&gt; d", "&#34;c&#34;"} {
		if !strings.Contains(decoded.Text, want) {
			t.Errorf("message text is missing %q\ngot:\n%s", want, decoded.Text)
		}
	}
	// The template's own markup must survive unescaped.
	if !strings.Contains(decoded.Text, "<b>Alert: ") {
		t.Errorf("template markup was escaped away\ngot:\n%s", decoded.Text)
	}
	// The raw title must not survive verbatim.
	if strings.Contains(decoded.Text, `a<b`) {
		t.Errorf("payload markup was not escaped\ngot:\n%s", decoded.Text)
	}
}

func TestTelegramNotifier_Send_APIErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Telegram frequently reports failures with a 400, but the
		// authoritative signal is ok:false in the body.
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte( //nolint:errcheck
			`{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`))
	}))
	defer server.Close()

	notifier := newTestTelegramNotifier(t, server.Client(), &mockTemplateRenderer{}, server.URL)

	err := notifier.Send(context.Background(), telegramTestChannel(), createTestPayload())
	if err == nil {
		t.Fatal("Send() expected an error for ok:false")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should carry error_code: %v", err)
	}
	if !strings.Contains(err.Error(), "chat not found") {
		t.Errorf("error should carry description: %v", err)
	}
}

// TestTelegramNotifier_Send_APIErrorWithHTTP200 covers the case the Bot
// API documents but the status code hides: ok:false alongside a 200.
func TestTelegramNotifier_Send_APIErrorWithHTTP200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte( //nolint:errcheck
			`{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":30}}`))
	}))
	defer server.Close()

	notifier := newTestTelegramNotifier(t, server.Client(), &mockTemplateRenderer{}, server.URL)

	err := notifier.Send(context.Background(), telegramTestChannel(), createTestPayload())
	if err == nil {
		t.Fatal("Send() expected an error for ok:false with a 200 status")
	}
	if !strings.Contains(err.Error(), "429") ||
		!strings.Contains(err.Error(), "Too Many Requests") {
		t.Errorf("error should carry error_code and description: %v", err)
	}
}

func TestTelegramNotifier_Send_NonJSONErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>502 Bad Gateway</html>")) //nolint:errcheck
	}))
	defer server.Close()

	notifier := newTestTelegramNotifier(t, server.Client(), &mockTemplateRenderer{}, server.URL)

	err := notifier.Send(context.Background(), telegramTestChannel(), createTestPayload())
	if err == nil {
		t.Fatal("Send() expected an error for an undecodable 502")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("error should contain the status code: %v", err)
	}
}

// TestTelegramNotifier_Send_UndecodableSuccessStatus covers a 200 whose
// body is not the documented envelope: the send must not be reported as
// delivered.
func TestTelegramNotifier_Send_UndecodableSuccessStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json at all")) //nolint:errcheck
	}))
	defer server.Close()

	notifier := newTestTelegramNotifier(t, server.Client(), &mockTemplateRenderer{}, server.URL)

	err := notifier.Send(context.Background(), telegramTestChannel(), createTestPayload())
	if err == nil {
		t.Fatal("Send() expected an error for an undecodable 200 body")
	}
	if !strings.Contains(err.Error(), "decode telegram response") {
		t.Errorf("error should mention the decode failure: %v", err)
	}
}

func TestTelegramNotifier_Send_ResponseBodyReadError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100") // Lie about content length
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("short")) //nolint:errcheck
	}))
	defer server.Close()

	notifier := newTestTelegramNotifier(t, server.Client(), &mockTemplateRenderer{}, server.URL)

	err := notifier.Send(context.Background(), telegramTestChannel(), createTestPayload())
	if err == nil {
		t.Fatal("Send() expected an error for a truncated response body")
	}
	if !strings.Contains(err.Error(), "failed to read body") {
		t.Errorf("error should mention the read failure: %v", err)
	}
	assertNoToken(t, err.Error())
}

func TestTelegramNotifier_Send_ValidationError(t *testing.T) {
	notifier := NewTelegramNotifier(http.DefaultClient, &mockTemplateRenderer{})

	err := notifier.Send(context.Background(),
		&database.NotificationChannel{}, createTestPayload())
	if err == nil {
		t.Fatal("Send() expected a validation error")
	}
	if err.Error() != "telegram channel requires a bot token" {
		t.Errorf("Send() error = %q, want the bot token message", err.Error())
	}
}

func TestTelegramNotifier_Send_UnknownNotificationType(t *testing.T) {
	notifier := NewTelegramNotifier(http.DefaultClient, &mockTemplateRenderer{})

	payload := createTestPayload()
	payload.NotificationType = "invalid_type"

	err := notifier.Send(context.Background(), telegramTestChannel(), payload)
	if err == nil {
		t.Fatal("Send() expected an error for an unknown notification type")
	}
	if !strings.Contains(err.Error(), "unknown notification type") {
		t.Errorf("error should mention the unknown type: %v", err)
	}
}

func TestTelegramNotifier_Send_TemplateRenderError(t *testing.T) {
	renderer := &mockTemplateRenderer{
		renderHTMLFunc: func(string, *database.NotificationPayload, string) (string, error) {
			return "", &templateError{msg: "template parse error"}
		},
	}

	notifier := NewTelegramNotifier(http.DefaultClient, renderer)

	err := notifier.Send(context.Background(), telegramTestChannel(), createTestPayload())
	if err == nil {
		t.Fatal("Send() expected a rendering error")
	}
	if !strings.Contains(err.Error(), "failed to render telegram template") {
		t.Errorf("error should mention telegram template rendering: %v", err)
	}
}

func TestTelegramNotifier_Send_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		okHandler(w)
	}))
	defer server.Close()

	notifier := newTestTelegramNotifier(t, server.Client(), &mockTemplateRenderer{}, server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := notifier.Send(ctx, telegramTestChannel(), createTestPayload())
	if err == nil {
		t.Fatal("Send() expected a context cancellation error")
	}
	assertNoToken(t, err.Error())
}

// assertNoToken fails the test if the bot token, or any recognizable
// fragment of it, appears in s.
func assertNoToken(t *testing.T, s string) {
	t.Helper()
	if strings.Contains(s, testTelegramToken) {
		t.Errorf("bot token leaked into error text: %s", s)
	}
	if strings.Contains(s, "AAFakeTokenValueForTestsOnly") {
		t.Errorf("bot token secret part leaked into error text: %s", s)
	}
}

// TestTelegramNotifier_Send_TransportErrorRedactsToken is the security
// regression test: a failing request must not put the bearer credential
// into the alerter log or into notification_history.error_message.
func TestTelegramNotifier_Send_TransportErrorRedactsToken(t *testing.T) {
	// Port 1 is not listening, so the transport fails and net/http
	// reports a *url.Error carrying the full request URL.
	notifier := newTestTelegramNotifier(t, http.DefaultClient,
		&mockTemplateRenderer{}, "http://127.0.0.1:1")

	err := notifier.Send(context.Background(), telegramTestChannel(), createTestPayload())
	if err == nil {
		t.Fatal("Send() expected a transport error")
	}
	assertNoToken(t, err.Error())
	if !strings.Contains(err.Error(), "failed to send telegram notification") {
		t.Errorf("error should name the failure: %v", err)
	}
}

// TestTelegramNotifier_Send_RequestBuildErrorRedactsToken drives the
// http.NewRequestWithContext failure path with a base URL containing a
// control character, which makes the URL unparsable.
func TestTelegramNotifier_Send_RequestBuildErrorRedactsToken(t *testing.T) {
	notifier := newTestTelegramNotifier(t, http.DefaultClient,
		&mockTemplateRenderer{}, "http://127.0.0.1:1/\x7f")

	err := notifier.Send(context.Background(), telegramTestChannel(), createTestPayload())
	if err == nil {
		t.Fatal("Send() expected a request construction error")
	}
	assertNoToken(t, err.Error())
	if !strings.Contains(err.Error(), "failed to create telegram request") {
		t.Errorf("error should name the failure: %v", err)
	}
}

// TestTelegramNotifier_Send_APIErrorDescriptionIsRedacted covers a
// server that echoes the request URL back in the description.
func TestTelegramNotifier_Send_APIErrorDescriptionIsRedacted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		body := map[string]any{
			"ok":          false,
			"error_code":  401,
			"description": "Unauthorized for https://api.telegram.org" + r.URL.Path,
		}
		_ = json.NewEncoder(w).Encode(body) //nolint:errcheck
	}))
	defer server.Close()

	notifier := newTestTelegramNotifier(t, server.Client(), &mockTemplateRenderer{}, server.URL)

	err := notifier.Send(context.Background(), telegramTestChannel(), createTestPayload())
	if err == nil {
		t.Fatal("Send() expected an error for ok:false")
	}
	assertNoToken(t, err.Error())
	if !strings.Contains(err.Error(), "/bot<redacted>/") {
		t.Errorf("error should carry the redacted path: %v", err)
	}
}

func TestRedactTelegramToken(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no token",
			in:   "connection refused",
			want: "connection refused",
		},
		{
			name: "full url",
			in:   `Post "https://api.telegram.org/bot123:ABC/sendMessage": dial failed`,
			want: `Post "https://api.telegram.org/bot<redacted>/sendMessage": dial failed`,
		},
		{
			name: "two occurrences",
			in:   "/bot111:AAA/sendMessage and /bot222:BBB/getMe",
			want: "/bot<redacted>/sendMessage and /bot<redacted>/getMe",
		},
		{
			// Fail closed: a missing terminating slash does not
			// excuse printing the credential.
			name: "prefix without a closing slash is redacted",
			in:   "/bot123:ABC",
			want: "/bot<redacted>",
		},
		{
			name: "token at end of string",
			in:   "https://api.telegram.org/bot123:ABC",
			want: "https://api.telegram.org/bot<redacted>",
		},
		{
			// The shape *url.Error produces when the path is
			// truncated. The `":` that follows the token is swallowed
			// with it, because a token may legally contain both of
			// those characters and neither may therefore end the
			// redacted run. Over-redacting two bytes of boilerplate is
			// the price of never under-redacting a credential; do not
			// "fix" this expectation by widening the terminator class.
			name: "token followed by a double quote",
			in:   `Post "https://api.telegram.org/bot123:ABC": EOF`,
			want: `Post "https://api.telegram.org/bot<redacted> EOF`,
		},
		{
			name: "token followed by whitespace",
			in:   "proxy rejected https://api.telegram.org/bot123:ABC after 3 tries",
			want: "proxy rejected https://api.telegram.org/bot<redacted> after 3 tries",
		},
		{
			// The closing paren goes the same way, and for the same
			// reason.
			name: "token followed by a closing paren",
			in:   "(https://api.telegram.org/bot123:ABC)",
			want: "(https://api.telegram.org/bot<redacted>",
		},
		{
			// VULN-002 in one line: every one of these characters was
			// in the terminator class, and every one of them is a
			// character the server's validator lets a token contain,
			// so a token carrying one printed everything after it.
			name: "token containing quoting and bracketing characters",
			in: `Post "https://api.telegram.org/bot` + telegramPunctuationToken +
				`/sendMessage": dial failed`,
			want: `Post "https://api.telegram.org/bot<redacted>/sendMessage": dial failed`,
		},
		{
			// The same token with nothing after it: the run reaches the
			// end of the string and is still replaced whole.
			name: "token containing quoting characters at end of string",
			in:   "https://api.telegram.org/bot" + telegramPunctuationToken,
			want: "https://api.telegram.org/bot<redacted>",
		},
		{
			name: "only the first occurrence has a trailing slash",
			in:   "/bot111:AAA/sendMessage then /bot222:BBB",
			want: "/bot<redacted>/sendMessage then /bot<redacted>",
		},
		{
			// An empty token run is not a secret, so it is left as it
			// stands rather than turned into a misleading placeholder.
			name: "bare prefix",
			in:   "/bot",
			want: "/bot",
		},
		{
			name: "bare prefix with a trailing slash",
			in:   "/bot/",
			want: "/bot/",
		},
		{
			name: "empty",
			in:   "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := redactTelegramToken(tt.in); got != tt.want {
				t.Errorf("redactTelegramToken(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// telegramRedactionContexts are the shapes a bot token actually turns up
// in when it escapes into text: a *url.Error, a truncated URL, a quoted
// or bracketed URL, a query string, a log line. Each carries one or more
// %s placeholders for the token.
var telegramRedactionContexts = []struct {
	name   string
	format string
}{
	{"url error", `Post "https://api.telegram.org/bot%s/sendMessage": dial tcp: i/o timeout`},
	{"truncated url error", `Post "https://api.telegram.org/bot%s": EOF`},
	{"bare url", "https://api.telegram.org/bot%s"},
	{"url in prose", "proxy rejected https://api.telegram.org/bot%s after 3 tries"},
	{"parenthesised", "(https://api.telegram.org/bot%s)"},
	{"bracketed", "[https://api.telegram.org/bot%s]"},
	{"angle bracketed", "<https://api.telegram.org/bot%s>"},
	{"single quoted", "'https://api.telegram.org/bot%s'"},
	{"backquoted", "`https://api.telegram.org/bot%s`"},
	{"comma separated", "tried https://api.telegram.org/bot%s, giving up"},
	{"semicolon separated", "url=https://api.telegram.org/bot%s; retrying"},
	{"query string", "https://api.telegram.org/bot%s?timeout=5"},
	{"fragment", "https://api.telegram.org/bot%s#frag"},
	{"own line", "request failed\nhttps://api.telegram.org/bot%s\nretrying"},
	{"tab separated", "url\thttps://api.telegram.org/bot%s\tfailed"},
	{"twice", "https://api.telegram.org/bot%s/getMe then https://api.telegram.org/bot%s"},
}

// TestRedactTelegramTokenNeverEmitsTheToken is the property the helper
// exists for: whatever surrounds a realistic token, the token itself
// must not survive into the output. This is the guard against the
// redactor failing open on an input shape nobody anticipated.
func TestRedactTelegramTokenNeverEmitsTheToken(t *testing.T) {
	for _, c := range telegramRedactionContexts {
		t.Run(c.name, func(t *testing.T) {
			in := strings.ReplaceAll(c.format, "%s", testTelegramToken)
			got := redactTelegramToken(in)
			assertNoToken(t, got)
			if !strings.Contains(got, "/bot<redacted>") {
				t.Errorf("redactTelegramToken(%q) = %q, want the placeholder", in, got)
			}
		})
	}
}

// telegramPunctuationToken carries every quoting and bracketing
// character that the old terminator class treated as the end of a
// token. The server's validator lets a token body contain all of them,
// which is exactly why none of them may end a redacted run.
const telegramPunctuationToken = "123456789:AA'BB\"CC;DD,EE)FF]GG<HH>II`JJ"

// telegramTokenBodyAlphabet is every byte a bot token body may legally
// contain: printable ASCII minus the characters the server's
// telegramBotTokenPattern excludes. Fuzzing over exactly this set is
// the point of the test below - the redactor's terminator class has to
// be a subset of the excluded characters, so every byte in this
// alphabet must be swallowed into the redacted run rather than end it.
var telegramTokenBodyAlphabet = buildTelegramTokenBodyAlphabet()

// buildTelegramTokenBodyAlphabet derives the alphabet from the excluded
// set rather than listing it, so that the two cannot drift apart.
func buildTelegramTokenBodyAlphabet() []byte {
	const excluded = "/?#\"'`<>)],;"
	var out []byte
	for c := byte('!'); c <= '~'; c++ {
		if !strings.ContainsRune(excluded, rune(c)) {
			out = append(out, c)
		}
	}
	return out
}

// telegramFuzzTokensPerContext is how many random tokens each
// surrounding context is exercised with.
const telegramFuzzTokensPerContext = 500

// telegramMinLeakRun is the shortest run of the token body that counts
// as a leak. Three characters of a token are guesswork; four are not.
const telegramMinLeakRun = 4

// randomTelegramToken builds a <digits>:<body> token whose body is
// drawn from the full set of characters a token may contain.
func randomTelegramToken(rng *rand.Rand) string {
	var b strings.Builder
	for i := 0; i < 1+rng.Intn(10); i++ {
		b.WriteByte(byte('0' + rng.Intn(10)))
	}
	b.WriteByte(':')
	for i := 0; i < 8+rng.Intn(40); i++ {
		b.WriteByte(telegramTokenBodyAlphabet[rng.Intn(len(telegramTokenBodyAlphabet))])
	}
	return b.String()
}

// longestTokenLeak returns the longest run of at least
// telegramMinLeakRun bytes of the token's body that survived into out,
// or "" when none did.
//
// Runs that also occur in contextOnly - the surrounding text with the
// token removed - are ignored: a random body can coincide with "http"
// or "tries" by chance, and that is the context showing through rather
// than the credential.
func longestTokenLeak(token, out, contextOnly string) string {
	body := token
	if i := strings.IndexByte(token, ':'); i >= 0 {
		body = token[i+1:]
	}
	best := ""
	for i := 0; i < len(body); i++ {
		for j := len(body); j-i > len(best) && j-i >= telegramMinLeakRun; j-- {
			sub := body[i:j]
			if strings.Contains(out, sub) && !strings.Contains(contextOnly, sub) {
				best = sub
				break
			}
		}
	}
	return best
}

// TestRedactTelegramTokenNeverEmitsAFuzzedToken is the half of the
// property that TestRedactTelegramTokenNeverEmitsTheToken misses: that
// one varies the surrounding context around a fixed alphanumeric
// token, so it never exercised a token containing a character the
// redactor mistook for a terminator. This one varies the token as well,
// over the whole alphabet the server's validator accepts, and asserts
// that no run of the body long enough to be worth anything survives.
//
// The seed is fixed so a failure is reproducible; a failure prints the
// token, the input and the output.
func TestRedactTelegramTokenNeverEmitsAFuzzedToken(t *testing.T) {
	rng := rand.New(rand.NewSource(20260916))

	for _, c := range telegramRedactionContexts {
		t.Run(c.name, func(t *testing.T) {
			contextOnly := strings.ReplaceAll(c.format, "%s", "")
			for i := 0; i < telegramFuzzTokensPerContext; i++ {
				token := randomTelegramToken(rng)
				in := strings.ReplaceAll(c.format, "%s", token)
				got := redactTelegramToken(in)

				if leak := longestTokenLeak(token, got, contextOnly); leak != "" {
					t.Fatalf("redactTelegramToken leaked %q of the token body\n"+
						"  token: %q\n  in:    %q\n  out:   %q", leak, token, in, got)
				}
				if !strings.Contains(got, "/bot<redacted>") {
					t.Fatalf("redactTelegramToken(%q) = %q, want the placeholder", in, got)
				}
			}
		})
	}
}

func TestTelegramTransportError(t *testing.T) {
	t.Run("plain error", func(t *testing.T) {
		got := telegramTransportError(fmt.Errorf("something broke"))
		if got != "something broke" {
			t.Errorf("telegramTransportError() = %q, want %q", got, "something broke")
		}
	})

	t.Run("url error is reduced to op and cause", func(t *testing.T) {
		urlErr := &url.Error{
			Op:  "Post",
			URL: "https://api.telegram.org/bot" + testTelegramToken + "/sendMessage",
			Err: fmt.Errorf("dial tcp: connection refused"),
		}
		got := telegramTransportError(urlErr)
		assertNoToken(t, got)
		if !strings.Contains(got, "Post request failed") {
			t.Errorf("telegramTransportError() = %q, want it to name the operation", got)
		}
		if !strings.Contains(got, "connection refused") {
			t.Errorf("telegramTransportError() = %q, want it to carry the cause", got)
		}
	})

	t.Run("url error with a nil cause falls back", func(t *testing.T) {
		urlErr := &url.Error{
			Op:  "Post",
			URL: "https://api.telegram.org/bot" + testTelegramToken + "/sendMessage",
		}
		got := telegramTransportError(urlErr)
		assertNoToken(t, got)
	})
}

func TestTruncateTelegramText(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		wantRunes int
		truncated bool
	}{
		{
			name:      "short text is unchanged",
			in:        "hello",
			wantRunes: 5,
		},
		{
			name:      "exactly at the limit is unchanged",
			in:        strings.Repeat("a", telegramMaxMessageRunes),
			wantRunes: telegramMaxMessageRunes,
		},
		{
			name:      "one over the limit is truncated",
			in:        strings.Repeat("a", telegramMaxMessageRunes+1),
			wantRunes: telegramMaxMessageRunes,
			truncated: true,
		},
		{
			name:      "well over the limit is truncated",
			in:        strings.Repeat("a", telegramMaxMessageRunes*3),
			wantRunes: telegramMaxMessageRunes,
			truncated: true,
		},
		{
			name: "multi-byte runes are not split",
			// Each 🔴 is four bytes and one rune, so the byte length is
			// far over the limit while the rune count is only just over.
			in:        strings.Repeat("🔴", telegramMaxMessageRunes+10),
			wantRunes: telegramMaxMessageRunes,
			truncated: true,
		},
		{
			name:      "empty",
			in:        "",
			wantRunes: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateTelegramText(tt.in)

			if n := len([]rune(got)); n != tt.wantRunes {
				t.Errorf("truncateTelegramText() produced %d runes, want %d", n, tt.wantRunes)
			}
			if !utf8.ValidString(got) {
				t.Error("truncateTelegramText() produced invalid UTF-8")
			}
			if strings.ContainsRune(got, utf8.RuneError) &&
				!strings.ContainsRune(tt.in, utf8.RuneError) {
				t.Error("truncateTelegramText() split a multi-byte rune")
			}
			if tt.truncated {
				if !strings.HasSuffix(got, telegramTruncationMarker) {
					t.Errorf("truncated text should end with %q, got %q",
						telegramTruncationMarker, got[max(0, len(got)-16):])
				}
			} else if got != tt.in {
				t.Errorf("truncateTelegramText() = %q, want it unchanged", got)
			}
		})
	}
}

// TestTelegramNotifier_Send_TruncatesOverlongText checks that the send
// path applies the limit, not just the helper.
func TestTelegramNotifier_Send_TruncatesOverlongText(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body) //nolint:errcheck
		okHandler(w)
	}))
	defer server.Close()

	renderer := &mockTemplateRenderer{
		renderHTMLFunc: func(string, *database.NotificationPayload, string) (string, error) {
			return strings.Repeat("🔴", telegramMaxMessageRunes+500), nil
		},
	}

	notifier := newTestTelegramNotifier(t, server.Client(), renderer, server.URL)
	if err := notifier.Send(context.Background(), telegramTestChannel(), createTestPayload()); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	var decoded telegramSendMessageRequest
	if err := json.Unmarshal(gotBody, &decoded); err != nil {
		t.Fatalf("request body is not valid JSON: %v", err)
	}
	if n := len([]rune(decoded.Text)); n != telegramMaxMessageRunes {
		t.Errorf("sent text is %d runes, want %d", n, telegramMaxMessageRunes)
	}
	if !strings.HasSuffix(decoded.Text, telegramTruncationMarker) {
		t.Error("sent text should end with the truncation marker")
	}
}

// TestTelegramNotifier_Send_MarshalsRatherThanConcatenates checks that a
// rendered message containing quotes and braces cannot break the JSON
// envelope.
func TestTelegramNotifier_Send_MarshalsRatherThanConcatenates(t *testing.T) {
	hostile := `" , "chat_id": "attacker", "x": "` + "\n\t" + `{}`

	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body) //nolint:errcheck
		okHandler(w)
	}))
	defer server.Close()

	renderer := &mockTemplateRenderer{
		renderHTMLFunc: func(string, *database.NotificationPayload, string) (string, error) {
			return hostile, nil
		},
	}

	notifier := newTestTelegramNotifier(t, server.Client(), renderer, server.URL)
	if err := notifier.Send(context.Background(), telegramTestChannel(), createTestPayload()); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	var decoded telegramSendMessageRequest
	if err := json.Unmarshal(gotBody, &decoded); err != nil {
		t.Fatalf("request body is not valid JSON: %v", err)
	}
	if decoded.ChatID != "-1001234567890" {
		t.Errorf("chat_id was overwritten: %q", decoded.ChatID)
	}
	if decoded.Text != hostile {
		t.Errorf("text = %q, want it round-tripped verbatim", decoded.Text)
	}
}

// =============================================================================
// H-001: truncation must not split markup, and a message Telegram
// cannot parse must not be lost
// =============================================================================

// assertNoDanglingMarkup fails when s ends inside an HTML entity or an
// HTML tag - the two shapes Telegram rejects with
// "can't parse entities" and "can't find end tag".
func assertNoDanglingMarkup(t *testing.T, s string) {
	t.Helper()
	tail := s[max(0, len(s)-32):]
	if i := strings.LastIndexByte(s, '<'); i >= 0 && !strings.ContainsRune(s[i:], '>') {
		t.Errorf("text ends inside a tag: ...%q", tail)
	}
	if i := strings.LastIndexByte(s, '&'); i >= 0 && !strings.ContainsRune(s[i:], ';') &&
		isTelegramEntityBody(s[i+1:]) {
		t.Errorf("text ends inside an entity: ...%q", tail)
	}
}

// TestTruncateTelegramText_DoesNotSplitMarkup is the regression test for
// H-001. The text handed to the truncator has already been escaped and
// rendered, so the last rune inside the limit lands in the middle of
// "&amp;" or of "<code>" for any message of the wrong length. Telegram
// then refuses the whole message with a 400, the queue retries it twice
// and gives up, and the operator never sees the alert.
func TestTruncateTelegramText_DoesNotSplitMarkup(t *testing.T) {
	// The marker is a single rune, so this is where the naive cut falls.
	const keep = telegramMaxMessageRunes - 1

	tests := []struct {
		name string
		in   string
		// wantRunes is the rune count of the result.
		wantRunes int
		// allowDangling marks the case where the rewind is deliberately
		// not taken because the markup is too far back to be something
		// truncation created.
		allowDangling bool
	}{
		{
			name: "named entity straddling the cut",
			// The cut would land after "&a".
			in:        strings.Repeat("a", keep-2) + "&amp;" + strings.Repeat("b", 50),
			wantRunes: keep - 2 + 1,
		},
		{
			name: "numeric entity straddling the cut",
			// The cut would land after "&#3".
			in:        strings.Repeat("a", keep-3) + "&#39;" + strings.Repeat("b", 50),
			wantRunes: keep - 3 + 1,
		},
		{
			name:      "entity ending exactly at the cut is kept",
			in:        strings.Repeat("a", keep-5) + "&amp;" + strings.Repeat("b", 50),
			wantRunes: keep + 1,
		},
		{
			name: "tag straddling the cut",
			// The cut would land after "<co".
			in:        strings.Repeat("a", keep-3) + "<code>x</code>" + strings.Repeat("b", 50),
			wantRunes: keep - 3 + 1,
		},
		{
			name:      "closed tag before the cut is kept whole",
			in:        strings.Repeat("a", keep-20) + "<b>bold</b>" + strings.Repeat("c", 200),
			wantRunes: keep + 1,
		},
		{
			name:      "a literal ampersand is not mistaken for an entity",
			in:        strings.Repeat("a", keep-3) + "& b" + strings.Repeat("c", 50),
			wantRunes: keep + 1,
		},
		{
			name: "an unterminated tag beyond the rewind window is left alone",
			// A '<' this far back is not something truncation split;
			// rewinding to it would throw the message away. Send's
			// plain-text retry is the net under this case.
			in:            strings.Repeat("a", keep-telegramMaxTagBytes-10) + "<" + strings.Repeat("b", 4000),
			wantRunes:     keep + 1,
			allowDangling: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateTelegramText(tt.in)

			if n := utf8.RuneCountInString(got); n != tt.wantRunes {
				t.Errorf("result is %d runes, want %d", n, tt.wantRunes)
			}
			if n := utf8.RuneCountInString(got); n > telegramMaxMessageRunes {
				t.Errorf("result is %d runes, over the %d limit", n, telegramMaxMessageRunes)
			}
			if !strings.HasSuffix(got, telegramTruncationMarker) {
				t.Errorf("result should end with the truncation marker")
			}
			body := strings.TrimSuffix(got, telegramTruncationMarker)
			if !strings.HasPrefix(tt.in, body) {
				t.Error("result is not a prefix of the input")
			}
			if !tt.allowDangling {
				assertNoDanglingMarkup(t, body)
			}
		})
	}
}

// TestBackOffTelegramMarkup covers the helper directly, including the
// inputs the Send-level tests cannot reach conveniently.
func TestBackOffTelegramMarkup(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{"empty", "", 0},
		{"no markup", "plain text", 10},
		{"complete entity", "a&amp;", 6},
		{"split entity", "a&am", 1},
		{"bare ampersand at the end", "a&", 1},
		{"ampersand followed by a space", "a& ", 3},
		{"complete tag", "a<b>", 4},
		{"split tag", "a<cod", 1},
		{"entity inside a split tag", "a<a href=x&am", 1},
		{"entity after a closed tag", "<b>x&#3", 4},
		{"entity beyond the window", "&" + strings.Repeat("a", telegramMaxEntityBytes), telegramMaxEntityBytes + 1},
		{"tag beyond the window", "<" + strings.Repeat("a", telegramMaxTagBytes), telegramMaxTagBytes + 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := backOffTelegramMarkup(tt.in); got != tt.want {
				t.Errorf("backOffTelegramMarkup(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

// TestTelegramByteOffsetOfRune pins the []rune-free offset helper.
func TestTelegramByteOffsetOfRune(t *testing.T) {
	tests := []struct {
		name string
		s    string
		n    int
		want int
	}{
		{"ascii", "abcd", 2, 2},
		{"multi-byte", "a🔴b", 2, 5},
		{"zero", "abc", 0, 0},
		{"past the end", "abc", 9, 3},
		{"exactly the end", "abc", 3, 3},
		{"empty", "", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := telegramByteOffsetOfRune(tt.s, tt.n); got != tt.want {
				t.Errorf("telegramByteOffsetOfRune(%q, %d) = %d, want %d", tt.s, tt.n, got, tt.want)
			}
		})
	}
}

// telegramScriptedServer answers each request with the next scripted
// response and records the raw request bodies.
type telegramScriptedServer struct {
	*httptest.Server
	mu     sync.Mutex
	bodies [][]byte
}

// requests returns the raw bodies received so far.
func (s *telegramScriptedServer) requests() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]byte(nil), s.bodies...)
}

// newTelegramScriptedServer starts a server that plays the given
// responses in order. A request beyond the script fails the test.
func newTelegramScriptedServer(t *testing.T, responses ...func(http.ResponseWriter)) *telegramScriptedServer {
	t.Helper()
	s := &telegramScriptedServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read request body: %v", err)
		}
		s.mu.Lock()
		s.bodies = append(s.bodies, body)
		index := len(s.bodies) - 1
		s.mu.Unlock()

		if index >= len(responses) {
			t.Errorf("unexpected request %d; the script has %d response(s)",
				index+1, len(responses))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		responses[index](w)
	}))
	t.Cleanup(s.Close)
	return s
}

// telegramFailure scripts one ok:false response.
func telegramFailure(status, code int, description string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"ok":          false,
			"error_code":  code,
			"description": description,
		})
	}
}

// telegramSuccess scripts one ok:true response.
func telegramSuccess() func(http.ResponseWriter) {
	return func(w http.ResponseWriter) { okHandler(w) }
}

// TestTelegramNotifier_Send_RetriesAsPlainTextOnParseFailure is the
// other half of H-001. A 400 about the markup is not a transport
// failure, so the queue would retry it twice more and then mark the
// notification failed - the alert is never delivered. Sending the same
// text once more with no parse_mode gets it through as plain text.
func TestTelegramNotifier_Send_RetriesAsPlainTextOnParseFailure(t *testing.T) {
	descriptions := []struct {
		name string
		text string
	}{
		{
			name: "split entity",
			text: `Bad Request: can't parse entities: Unsupported start tag "am" at byte offset 4051`,
		},
		{
			name: "unclosed tag",
			text: `Bad Request: can't find end tag corresponding to start tag "code"`,
		},
		{
			name: "different capitalization",
			text: "Bad Request: Can't Parse Entities: unexpected end of string",
		},
	}

	for _, d := range descriptions {
		t.Run(d.name, func(t *testing.T) {
			server := newTelegramScriptedServer(t,
				telegramFailure(http.StatusBadRequest, 400, d.text),
				telegramSuccess(),
			)

			renderer := &mockTemplateRenderer{
				renderHTMLFunc: func(string, *database.NotificationPayload, string) (string, error) {
					return "<code>broken", nil
				},
			}
			notifier := newTestTelegramNotifier(t, server.Client(), renderer, server.URL)

			if err := notifier.Send(context.Background(), telegramTestChannel(),
				createTestPayload()); err != nil {
				t.Fatalf("Send() = %v, want the plain-text retry to succeed", err)
			}

			bodies := server.requests()
			if len(bodies) != 2 {
				t.Fatalf("server saw %d request(s), want 2", len(bodies))
			}

			var first, second map[string]any
			if err := json.Unmarshal(bodies[0], &first); err != nil {
				t.Fatalf("first request body is not JSON: %v", err)
			}
			if err := json.Unmarshal(bodies[1], &second); err != nil {
				t.Fatalf("second request body is not JSON: %v", err)
			}
			if first["parse_mode"] != "HTML" {
				t.Errorf("first attempt parse_mode = %v, want HTML", first["parse_mode"])
			}
			if _, present := second["parse_mode"]; present {
				t.Errorf("retry carried parse_mode = %v, want the field omitted",
					second["parse_mode"])
			}
			if first["text"] != second["text"] {
				t.Errorf("retry changed the text: %v vs %v", first["text"], second["text"])
			}
		})
	}
}

// TestTelegramNotifier_Send_DoesNotRetryOtherFailures keeps the
// fallback narrow: anything that is not Telegram complaining about the
// markup must fail on the first attempt, so the queue's own retry and
// backoff stay in charge.
func TestTelegramNotifier_Send_DoesNotRetryOtherFailures(t *testing.T) {
	cases := []struct {
		name        string
		status      int
		code        int
		description string
	}{
		{"blocked bot", http.StatusForbidden, 403, "Forbidden: bot was blocked by the user"},
		{"unknown chat", http.StatusBadRequest, 400, "Bad Request: chat not found"},
		{"message too long", http.StatusBadRequest, 400, "Bad Request: message is too long"},
		{"parse wording with a non-400 code", http.StatusOK, 409,
			"Conflict: can't parse entities"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := newTelegramScriptedServer(t,
				telegramFailure(tc.status, tc.code, tc.description))

			notifier := newTestTelegramNotifier(t, server.Client(),
				&mockTemplateRenderer{}, server.URL)

			err := notifier.Send(context.Background(), telegramTestChannel(), createTestPayload())
			if err == nil {
				t.Fatal("Send() = nil, want the API failure to be reported")
			}
			if !strings.Contains(err.Error(), tc.description) {
				t.Errorf("error = %v, want it to carry the description", err)
			}
			if n := len(server.requests()); n != 1 {
				t.Errorf("server saw %d request(s), want 1", n)
			}
		})
	}
}

// TestTelegramNotifier_Send_PlainTextRetryFailureIsReported checks the
// error when the fallback fails too: both causes have to be visible or
// the operator is debugging blind.
func TestTelegramNotifier_Send_PlainTextRetryFailureIsReported(t *testing.T) {
	server := newTelegramScriptedServer(t,
		telegramFailure(http.StatusBadRequest, 400, "Bad Request: can't find end tag"),
		telegramFailure(http.StatusBadRequest, 400, "Bad Request: chat not found"),
	)

	notifier := newTestTelegramNotifier(t, server.Client(), &mockTemplateRenderer{}, server.URL)

	err := notifier.Send(context.Background(), telegramTestChannel(), createTestPayload())
	if err == nil {
		t.Fatal("Send() = nil, want the retry failure to be reported")
	}
	if n := len(server.requests()); n != 2 {
		t.Errorf("server saw %d request(s), want 2", n)
	}
	for _, want := range []string{"can't find end tag", "chat not found", "plain-text retry"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to mention %q", err, want)
		}
	}
	assertNoToken(t, err.Error())
}

// TestTelegramAPIErrorIsParseFailure pins the classification directly,
// including the boundary cases the send-level tests do not reach.
func TestTelegramAPIErrorIsParseFailure(t *testing.T) {
	tests := []struct {
		name        string
		code        int
		description string
		want        bool
	}{
		{"parse entities", 400, "Bad Request: can't parse entities: foo", true},
		{"end tag", 400, "Bad Request: can't find end tag", true},
		{"upper case", 400, "BAD REQUEST: CAN'T PARSE ENTITIES", true},
		{"wrong code", 403, "Bad Request: can't parse entities", false},
		{"other 400", 400, "Bad Request: chat not found", false},
		{"empty description", 400, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &telegramAPIError{Code: tt.code, Description: tt.description}
			if got := e.isParseFailure(); got != tt.want {
				t.Errorf("isParseFailure() = %v, want %v", got, tt.want)
			}
			if !strings.Contains(e.Error(), tt.description) {
				t.Errorf("Error() = %q, want it to carry the description", e.Error())
			}
		})
	}
}

// =============================================================================
// H-002: remote text must not reach the log or the database unbounded
// =============================================================================

func TestSanitizeTelegramEcho(t *testing.T) {
	t.Run("short text is unchanged", func(t *testing.T) {
		if got := sanitizeTelegramEcho("connection refused"); got != "connection refused" {
			t.Errorf("sanitizeTelegramEcho() = %q", got)
		}
	})

	t.Run("the token is still redacted", func(t *testing.T) {
		in := `Post "https://api.telegram.org/bot` + testTelegramToken + `/sendMessage": EOF`
		got := sanitizeTelegramEcho(in)
		assertNoToken(t, got)
		if !strings.Contains(got, "/bot<redacted>") {
			t.Errorf("sanitizeTelegramEcho() = %q, want the placeholder", got)
		}
	})

	t.Run("control characters become spaces", func(t *testing.T) {
		got := sanitizeTelegramEcho("first\n[ERROR] forged\r\tx\x00y\x7fz")
		if strings.ContainsAny(got, "\n\r\t\x00\x7f") {
			t.Errorf("sanitizeTelegramEcho() = %q, want no control characters", got)
		}
		if got != "first [ERROR] forged  x y z" {
			t.Errorf("sanitizeTelegramEcho() = %q", got)
		}
	})

	t.Run("long text is capped", func(t *testing.T) {
		got := sanitizeTelegramEcho(strings.Repeat("A", 100000))
		if len(got) > telegramMaxEchoedBytes+len(telegramEchoTruncationMarker) {
			t.Errorf("sanitizeTelegramEcho() returned %d bytes, want at most %d",
				len(got), telegramMaxEchoedBytes+len(telegramEchoTruncationMarker))
		}
		if !strings.HasSuffix(got, telegramEchoTruncationMarker) {
			t.Error("capped text should end with the truncation marker")
		}
	})

	t.Run("the cap does not split a rune", func(t *testing.T) {
		// 256 is not a multiple of three, so a naive cut would land
		// inside one of these.
		got := sanitizeTelegramEcho(strings.Repeat("中", 500))
		if !utf8.ValidString(got) {
			t.Error("sanitizeTelegramEcho() produced invalid UTF-8")
		}
	})

	t.Run("invalid UTF-8 is replaced", func(t *testing.T) {
		got := sanitizeTelegramEcho("head\xff\xfetail")
		if !utf8.ValidString(got) {
			t.Errorf("sanitizeTelegramEcho() = %q, want valid UTF-8", got)
		}
	})

	t.Run("empty", func(t *testing.T) {
		if got := sanitizeTelegramEcho(""); got != "" {
			t.Errorf("sanitizeTelegramEcho() = %q, want empty", got)
		}
	})
}

// TestTelegramNotifier_Send_BoundsEchoedResponseBody is the regression
// test for H-002 on the body path: a captive portal or a hostile
// endpoint can answer with as much text as it likes, and all of it
// would otherwise land in the alerter log and in
// notification_history.error_message once per attempt.
func TestTelegramNotifier_Send_BoundsEchoedResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>\n" + strings.Repeat("captive portal ", 20000))) //nolint:errcheck
	}))
	defer server.Close()

	notifier := newTestTelegramNotifier(t, server.Client(), &mockTemplateRenderer{}, server.URL)

	err := notifier.Send(context.Background(), telegramTestChannel(), createTestPayload())
	if err == nil {
		t.Fatal("Send() = nil, want an error for a non-JSON 502")
	}
	assertBoundedEcho(t, err.Error())
}

// TestTelegramNotifier_Send_BoundsEchoedDescription is the same
// regression on the description path, which a hostile endpoint controls
// just as completely.
func TestTelegramNotifier_Send_BoundsEchoedDescription(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"ok":          false,
			"error_code":  400,
			"description": "\n[ERROR] forged log line\n" + strings.Repeat("x", 500000),
		})
	}))
	defer server.Close()

	notifier := newTestTelegramNotifier(t, server.Client(), &mockTemplateRenderer{}, server.URL)

	err := notifier.Send(context.Background(), telegramTestChannel(), createTestPayload())
	if err == nil {
		t.Fatal("Send() = nil, want an error for ok:false")
	}
	assertBoundedEcho(t, err.Error())
}

// assertBoundedEcho fails when an error string carries more borrowed
// text than telegramMaxEchoedBytes allows, or carries a newline a
// hostile endpoint could use to forge a log line.
func assertBoundedEcho(t *testing.T, s string) {
	t.Helper()
	// The error's own wording, plus one bounded echo.
	const slack = 128
	if len(s) > telegramMaxEchoedBytes+len(telegramEchoTruncationMarker)+slack {
		t.Errorf("error is %d bytes, want the echoed text bounded at %d",
			len(s), telegramMaxEchoedBytes)
	}
	if strings.ContainsAny(s, "\n\r") {
		t.Errorf("error carries a line break, which forges log lines: %q", s)
	}
}
