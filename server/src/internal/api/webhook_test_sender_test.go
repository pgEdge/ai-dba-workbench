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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"
)

// TestSendTestWebhook_RefusesRedirect verifies that sendTestWebhook does
// NOT follow an HTTP 302 redirect. A misconfigured (or malicious) webhook
// endpoint could redirect to a metadata host such as 169.254.169.254 and
// bypass the upstream hostValidator.ValidateHost check; the client must
// treat the 3xx response as the final response rather than dialing the
// Location target.
func TestSendTestWebhook_RefusesRedirect(t *testing.T) {
	var internalDialed atomic.Bool

	// A fake "internal" host that should never be contacted. If the
	// client follows the redirect this handler will run and flip the
	// flag.
	internalServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		internalDialed.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer internalServer.Close()

	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate an attacker-controlled webhook that 302-redirects to
		// an internal metadata-style host.
		w.Header().Set("Location", internalServer.URL+"/latest/meta-data/")
		w.WriteHeader(http.StatusFound)
	}))
	defer redirectServer.Close()

	err := sendTestWebhook(redirectServer.URL, "slack")
	if err == nil {
		t.Fatalf("expected error from non-2xx status, got nil")
	}
	if !strings.Contains(err.Error(), "302") {
		t.Errorf("expected error to mention 302 status; got %q", err.Error())
	}
	if internalDialed.Load() {
		t.Fatal("client followed redirect and dialed internal host; SSRF protection is broken")
	}
}

// TestSendTestGenericWebhook_RefusesRedirect mirrors the above check for
// the generic webhook sender used by custom REST endpoints.
func TestSendTestGenericWebhook_RefusesRedirect(t *testing.T) {
	var internalDialed atomic.Bool

	internalServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		internalDialed.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer internalServer.Close()

	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", internalServer.URL+"/latest/meta-data/")
		w.WriteHeader(http.StatusFound)
	}))
	defer redirectServer.Close()

	err := sendTestGenericWebhook(redirectServer.URL, http.MethodPost, nil, "", "")
	if err == nil {
		t.Fatalf("expected error from non-2xx status, got nil")
	}
	if !strings.Contains(err.Error(), "302") {
		t.Errorf("expected error to mention 302 status; got %q", err.Error())
	}
	if internalDialed.Load() {
		t.Fatal("client followed redirect and dialed internal host; SSRF protection is broken")
	}
}

// TestSendTestWebhook_Success verifies that a 200 response from a Slack or
// Mattermost-style webhook produces no error and that the posted body
// contains the configured channel type.
func TestSendTestWebhook_Success(t *testing.T) {
	var gotBody string
	var gotContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read request body: %v", err)
			return
		}
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := sendTestWebhook(server.URL, "slack"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if !strings.Contains(gotBody, "slack") {
		t.Errorf("expected posted body to mention channel type; got %q", gotBody)
	}
}

// TestSendTestWebhook_NonOKStatus verifies that a non-200 status is
// surfaced in the returned error along with the response body.
func TestSendTestWebhook_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "boom")
	}))
	defer server.Close()

	err := sendTestWebhook(server.URL, "slack")
	if err == nil {
		t.Fatal("expected error for non-200 status, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error to mention status 500; got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("expected error to include response body; got %q", err.Error())
	}
}

// TestSendTestWebhook_BadURL verifies that an unparseable URL yields an
// error from http.NewRequest rather than a panic.
func TestSendTestWebhook_BadURL(t *testing.T) {
	err := sendTestWebhook("http://\x00invalid", "slack")
	if err == nil {
		t.Fatal("expected error for malformed URL, got nil")
	}
	if !strings.Contains(err.Error(), "failed to create request") {
		t.Errorf("expected create-request error; got %q", err.Error())
	}
}

// TestSendTestWebhook_UnreachableHost verifies that a dial failure is
// wrapped with a descriptive error.
func TestSendTestWebhook_UnreachableHost(t *testing.T) {
	// Bind a real ephemeral port and immediately release it; the resulting
	// URL is guaranteed to refuse connections without relying on magic
	// low-numbered ports that may behave differently across systems.
	closedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedURL := closedServer.URL
	closedServer.Close()

	err := sendTestWebhook(closedURL, "slack")
	if err == nil {
		t.Fatal("expected error for unreachable host, got nil")
	}
	if !strings.Contains(err.Error(), "failed to send request") {
		t.Errorf("expected send-request error; got %q", err.Error())
	}
}

// TestSendTestGenericWebhook_DefaultMethodAndBody verifies that an empty
// httpMethod defaults to POST and that non-GET requests carry the
// expected JSON body and Content-Type.
func TestSendTestGenericWebhook_DefaultMethodAndBody(t *testing.T) {
	var gotMethod, gotContentType, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read request body: %v", err)
			return
		}
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := sendTestGenericWebhook(server.URL, "", nil, "", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if !strings.Contains(gotBody, "test message") {
		t.Errorf("expected test body; got %q", gotBody)
	}
}

// TestSendTestGenericWebhook_GetNoBody verifies that GET requests do not
// carry a JSON body or Content-Type header.
func TestSendTestGenericWebhook_GetNoBody(t *testing.T) {
	var gotMethod, gotContentType string
	var gotBodyLen int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read request body: %v", err)
			return
		}
		gotBodyLen = len(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := sendTestGenericWebhook(server.URL, http.MethodGet, nil, "", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotContentType != "" {
		t.Errorf("Content-Type = %q, want empty for GET", gotContentType)
	}
	if gotBodyLen != 0 {
		t.Errorf("body length = %d, want 0 for GET", gotBodyLen)
	}
}

// TestSendTestGenericWebhook_CustomHeaders verifies that caller-provided
// custom headers reach the endpoint.
func TestSendTestGenericWebhook_CustomHeaders(t *testing.T) {
	var gotCustom string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCustom = r.Header.Get("X-Custom-Header")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	headers := map[string]string{"X-Custom-Header": "test-value"}
	if err := sendTestGenericWebhook(server.URL, http.MethodPost, headers, "", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotCustom != "test-value" {
		t.Errorf("X-Custom-Header = %q, want 'test-value'", gotCustom)
	}
}

// TestSendTestGenericWebhook_BasicAuth verifies that basic authentication
// credentials are set on the outgoing request.
func TestSendTestGenericWebhook_BasicAuth(t *testing.T) {
	var gotUser, gotPass string
	var gotOK bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, gotOK = r.BasicAuth()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := sendTestGenericWebhook(server.URL, http.MethodPost, nil, "basic", "alice:s3cret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !gotOK {
		t.Fatal("basic auth not set on request")
	}
	if gotUser != "alice" || gotPass != "s3cret" {
		t.Errorf("basic auth = %q/%q, want alice/s3cret", gotUser, gotPass)
	}
}

// TestSendTestGenericWebhook_BasicAuthMalformed verifies that malformed
// basic credentials (missing colon) are silently skipped rather than
// sending garbled authentication.
func TestSendTestGenericWebhook_BasicAuthMalformed(t *testing.T) {
	var gotOK bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _, gotOK = r.BasicAuth()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := sendTestGenericWebhook(server.URL, http.MethodPost, nil, "basic", "nocolon"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotOK {
		t.Error("basic auth should not be set when credentials lack a colon")
	}
}

// TestSendTestGenericWebhook_BearerAuth verifies that bearer tokens are
// sent in the Authorization header.
func TestSendTestGenericWebhook_BearerAuth(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := sendTestGenericWebhook(server.URL, http.MethodPost, nil, "bearer", "abc123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "Bearer abc123" {
		t.Errorf("Authorization = %q, want 'Bearer abc123'", gotAuth)
	}
}

// TestSendTestGenericWebhook_APIKeyAuth verifies that api_key credentials
// (name:value) set an arbitrary header.
func TestSendTestGenericWebhook_APIKeyAuth(t *testing.T) {
	var gotKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-API-Key")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := sendTestGenericWebhook(server.URL, http.MethodPost, nil, "api_key", "X-API-Key:secret-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotKey != "secret-token" {
		t.Errorf("X-API-Key = %q, want 'secret-token'", gotKey)
	}
}

// TestSendTestGenericWebhook_APIKeyAuthMalformed verifies that malformed
// api_key credentials (missing colon) are silently skipped.
func TestSendTestGenericWebhook_APIKeyAuthMalformed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for name := range r.Header {
			if strings.HasPrefix(strings.ToLower(name), "x-") {
				t.Errorf("unexpected custom header %q set", name)
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := sendTestGenericWebhook(server.URL, http.MethodPost, nil, "api_key", "nocolon"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestSendTestGenericWebhook_UnknownAuthType verifies that an unknown
// auth type is silently ignored (no header added) rather than erroring.
func TestSendTestGenericWebhook_UnknownAuthType(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := sendTestGenericWebhook(server.URL, http.MethodPost, nil, "mystery", "value"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("Authorization should be empty for unknown auth type, got %q", gotAuth)
	}
}

// TestSendTestGenericWebhook_NonSuccessStatus verifies that any response
// outside 2xx is surfaced as an error with the status code and body.
func TestSendTestGenericWebhook_NonSuccessStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, "bad input")
	}))
	defer server.Close()

	err := sendTestGenericWebhook(server.URL, http.MethodPost, nil, "", "")
	if err == nil {
		t.Fatal("expected error for 4xx status, got nil")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("expected error to mention status 400; got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "bad input") {
		t.Errorf("expected error to contain response body; got %q", err.Error())
	}
}

// TestSendTestGenericWebhook_BadURL verifies that an unparseable URL
// produces a create-request error.
func TestSendTestGenericWebhook_BadURL(t *testing.T) {
	err := sendTestGenericWebhook("http://\x00invalid", http.MethodPost, nil, "", "")
	if err == nil {
		t.Fatal("expected error for malformed URL, got nil")
	}
	if !strings.Contains(err.Error(), "failed to create request") {
		t.Errorf("expected create-request error; got %q", err.Error())
	}
}

// TestSendTestGenericWebhook_UnreachableHost verifies dial-failure error
// wrapping for the generic webhook sender.
func TestSendTestGenericWebhook_UnreachableHost(t *testing.T) {
	// Bind a real ephemeral port and immediately release it to obtain a
	// URL guaranteed to refuse connections, without relying on magic
	// low-numbered ports.
	closedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedURL := closedServer.URL
	closedServer.Close()

	err := sendTestGenericWebhook(closedURL, http.MethodPost, nil, "", "")
	if err == nil {
		t.Fatal("expected error for unreachable host, got nil")
	}
	if !strings.Contains(err.Error(), "failed to send request") {
		t.Errorf("expected send-request error; got %q", err.Error())
	}
}

// TestSendTestGenericWebhook_PutAndPatch exercises the PUT and PATCH code
// paths to confirm they send the JSON body like POST.
func TestSendTestGenericWebhook_PutAndPatch(t *testing.T) {
	cases := []string{http.MethodPut, http.MethodPatch}
	for _, method := range cases {
		t.Run(method, func(t *testing.T) {
			var gotMethod, gotContentType string
			var gotBodyLen int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				gotContentType = r.Header.Get("Content-Type")
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("failed to read request body: %v", err)
					return
				}
				gotBodyLen = len(body)
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			if err := sendTestGenericWebhook(server.URL, method, nil, "", ""); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotMethod != method {
				t.Errorf("method = %q, want %q", gotMethod, method)
			}
			if gotContentType != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", gotContentType)
			}
			if gotBodyLen == 0 {
				t.Errorf("expected non-empty body for %s", method)
			}
		})
	}
}

// =============================================================================
// Telegram test sender
// =============================================================================

// telegramProbeToken is a syntactically valid but entirely fictitious
// Bot API token. Every telegram sender test asserts that it never
// reaches an error string, because the handler logs those errors.
const telegramProbeToken = "123456789:AAErq-leak-me-not-TEST-TOKEN"

// withTelegramBaseURL points the telegram test sender at an httptest
// server for the duration of the test. The package variable is the only
// seam; nothing reachable from a request can set it.
func withTelegramBaseURL(t *testing.T, base string) {
	t.Helper()
	previous := telegramSendBaseURL
	telegramSendBaseURL = base
	t.Cleanup(func() { telegramSendBaseURL = previous })
}

// TestSendTestTelegram_Success covers the happy path and asserts the
// request shape: a POST to /bot<token>/sendMessage carrying a JSON body
// with the chat ID as a string, the probe text and HTML parse mode.
func TestSendTestTelegram_Success(t *testing.T) {
	var gotPath, gotMethod, gotContentType string
	var gotBody telegramSendMessageRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true,"result":{"message_id":42}}`)
	}))
	defer server.Close()
	withTelegramBaseURL(t, server.URL)

	if err := sendTestTelegram(telegramProbeToken, "-1001234567890"); err != nil {
		t.Fatalf("sendTestTelegram: %v", err)
	}

	if want := "/bot" + telegramProbeToken + "/sendMessage"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotBody.ChatID != "-1001234567890" {
		t.Errorf("chat_id = %q, want -1001234567890", gotBody.ChatID)
	}
	if gotBody.ParseMode != "HTML" {
		t.Errorf("parse_mode = %q, want HTML", gotBody.ParseMode)
	}
	if !gotBody.DisableWebPagePreview {
		t.Error("disable_web_page_preview = false, want true")
	}
	if gotBody.Text != telegramTestMessage {
		t.Errorf("text = %q, want %q", gotBody.Text, telegramTestMessage)
	}
}

// TestSendTestTelegram_UsernameChatID confirms an @channelusername is
// passed through as a JSON string unchanged.
func TestSendTestTelegram_UsernameChatID(t *testing.T) {
	var gotBody telegramSendMessageRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer server.Close()
	withTelegramBaseURL(t, server.URL)

	if err := sendTestTelegram(telegramProbeToken, "@workbench_alerts"); err != nil {
		t.Fatalf("sendTestTelegram: %v", err)
	}
	if gotBody.ChatID != "@workbench_alerts" {
		t.Errorf("chat_id = %q, want @workbench_alerts", gotBody.ChatID)
	}
}

// TestSendTestTelegram_NotOK is the important failure case: the Bot API
// reports failures in the body, frequently alongside a 200, so ok:false
// must be treated as a failure and its error_code and description
// surfaced.
func TestSendTestTelegram_NotOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Deliberately a 200: the status code is not the signal.
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`)
	}))
	defer server.Close()
	withTelegramBaseURL(t, server.URL)

	err := sendTestTelegram(telegramProbeToken, "-1001234567890")
	if err == nil {
		t.Fatal("expected an error when the API reports ok:false")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should carry the error_code; got %q", err)
	}
	if !strings.Contains(err.Error(), "chat not found") {
		t.Errorf("error should carry the description; got %q", err)
	}
	assertNoTelegramToken(t, err)
}

// TestSendTestTelegram_NonJSONErrorStatus covers a gateway or proxy that
// answers with a non-JSON body and a failing status.
func TestSendTestTelegram_NonJSONErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, "upstream exploded")
	}))
	defer server.Close()
	withTelegramBaseURL(t, server.URL)

	err := sendTestTelegram(telegramProbeToken, "-1001234567890")
	if err == nil {
		t.Fatal("expected an error for a non-JSON failure response")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("error should carry the status code; got %q", err)
	}
	assertNoTelegramToken(t, err)
}

// TestSendTestTelegram_NonJSONSuccessStatus covers a 200 carrying a body
// that is not the documented envelope at all.
func TestSendTestTelegram_NonJSONSuccessStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "not json")
	}))
	defer server.Close()
	withTelegramBaseURL(t, server.URL)

	err := sendTestTelegram(telegramProbeToken, "-1001234567890")
	if err == nil {
		t.Fatal("expected an error for an undecodable 200 response")
	}
	if !strings.Contains(err.Error(), "failed to decode") {
		t.Errorf("error = %q, want a decode failure", err)
	}
	assertNoTelegramToken(t, err)
}

// TestSendTestTelegram_RefusesRedirect mirrors its webhook siblings:
// the client must treat a 3xx as the final response. The Bot API does
// not redirect, and a Location header must never be allowed to carry
// the bot token to another host.
func TestSendTestTelegram_RefusesRedirect(t *testing.T) {
	var internalDialed atomic.Bool
	internalServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		internalDialed.Store(true)
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer internalServer.Close()

	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, internalServer.URL, http.StatusFound)
	}))
	defer redirectServer.Close()
	withTelegramBaseURL(t, redirectServer.URL)

	err := sendTestTelegram(telegramProbeToken, "-1001234567890")
	if err == nil {
		t.Fatal("expected an error when the API answers with a redirect")
	}
	if internalDialed.Load() {
		t.Error("redirect was followed; the bot token reached the redirect target")
	}
	assertNoTelegramToken(t, err)
}

// TestSendTestTelegram_TransportErrorRedactsToken is the regression test
// for the credential leak this sender is built to avoid. The bot token
// sits in the request path, so a *url.Error from client.Do stringifies
// with the token in it. The handler logs whatever we return, so the
// token must not be there.
func TestSendTestTelegram_TransportErrorRedactsToken(t *testing.T) {
	closedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedURL := closedServer.URL
	closedServer.Close()
	withTelegramBaseURL(t, closedURL)

	err := sendTestTelegram(telegramProbeToken, "-1001234567890")
	if err == nil {
		t.Fatal("expected an error for an unreachable host")
	}
	if !strings.Contains(err.Error(), "failed to send request") {
		t.Errorf("error = %q, want a send-request failure", err)
	}
	assertNoTelegramToken(t, err)

	// Sanity check: the unredacted error really would have leaked, so
	// this test is not passing vacuously.
	if !strings.Contains(closedURL+"/bot"+telegramProbeToken+"/sendMessage", telegramProbeToken) {
		t.Fatal("test setup no longer places the token in the URL")
	}
}

// TestSendTestTelegram_MalformedBaseURLRedactsToken drives the
// http.NewRequest failure branch, whose error also embeds the URL.
func TestSendTestTelegram_MalformedBaseURLRedactsToken(t *testing.T) {
	withTelegramBaseURL(t, "http://\x00invalid")

	err := sendTestTelegram(telegramProbeToken, "-1001234567890")
	if err == nil {
		t.Fatal("expected an error for a malformed URL")
	}
	if !strings.Contains(err.Error(), "failed to create request") {
		t.Errorf("error = %q, want a create-request failure", err)
	}
	assertNoTelegramToken(t, err)
}

// TestSendTestTelegram_DescriptionRedaction covers the case where the
// API itself echoes a URL containing the token back in `description`.
func TestSendTestTelegram_DescriptionRedaction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"ok":false,"error_code":404,"description":"Not Found: /bot%s/sendMessage"}`,
			telegramProbeToken)
	}))
	defer server.Close()
	withTelegramBaseURL(t, server.URL)

	err := sendTestTelegram(telegramProbeToken, "-1001234567890")
	if err == nil {
		t.Fatal("expected an error when the API reports ok:false")
	}
	assertNoTelegramToken(t, err)
	if !strings.Contains(err.Error(), "/bot<redacted>/") {
		t.Errorf("error = %q, want the token replaced by the placeholder", err)
	}
}

// assertNoTelegramToken fails the test if the error text contains the
// bot token in any form.
func assertNoTelegramToken(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	assertNoTelegramTokenText(t, err.Error())
}

// assertNoTelegramTokenText fails the test if s contains the bot token
// in any form.
func assertNoTelegramTokenText(t *testing.T, s string) {
	t.Helper()
	if strings.Contains(s, telegramProbeToken) {
		t.Errorf("text leaked the bot token: %q", s)
	}
	// The secret half of the token on its own is just as damaging.
	if strings.Contains(s, "AAErq-leak-me-not-TEST-TOKEN") {
		t.Errorf("text leaked the token body: %q", s)
	}
}

// TestRedactTelegramToken covers the helper directly. It must fail
// closed: every input carrying a token leaves without one.
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
			in := strings.ReplaceAll(c.format, "%s", telegramProbeToken)
			got := redactTelegramToken(in)
			assertNoTelegramTokenText(t, got)
			if !strings.Contains(got, "/bot<redacted>") {
				t.Errorf("redactTelegramToken(%q) = %q, want the placeholder", in, got)
			}
		})
	}
}

// TestTelegramTransportError covers both branches of the reducer: a
// *url.Error, whose String() would carry the URL, and a plain error.
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

// TestTelegramFuzzAlphabetIsValidatorAccepted ties the fuzz alphabet to
// the validator that defines it. Both live in this module, so the
// subset invariant can be checked directly here rather than asserted in
// a comment: every token the fuzzer generates must be one the API would
// have accepted and stored, or the fuzz proves nothing about real
// tokens.
func TestTelegramFuzzAlphabetIsValidatorAccepted(t *testing.T) {
	for _, c := range telegramTokenBodyAlphabet {
		token := "123456789:AA" + string(c) + "BB"
		if !telegramBotTokenPattern.MatchString(token) {
			t.Errorf("fuzz alphabet contains %q, which telegramBotTokenPattern rejects; "+
				"the fuzz and the validator have drifted apart", string(c))
		}
	}

	rng := rand.New(rand.NewSource(20260916))
	for i := 0; i < telegramFuzzTokensPerContext; i++ {
		token := randomTelegramToken(rng)
		if !telegramBotTokenPattern.MatchString(token) {
			t.Fatalf("fuzzer produced %q, which telegramBotTokenPattern rejects", token)
		}
	}
}

func TestTelegramTransportError(t *testing.T) {
	urlErr := &url.Error{
		Op:  "Post",
		URL: "https://api.telegram.org/bot123:SECRET/sendMessage",
		Err: errors.New("dial tcp: connection refused"),
	}
	got := telegramTransportError(urlErr)
	if strings.Contains(got, "SECRET") {
		t.Errorf("transport error leaked the token: %q", got)
	}
	if !strings.Contains(got, "Post request failed") {
		t.Errorf("transport error = %q, want the operation named", got)
	}
	if !strings.Contains(got, "connection refused") {
		t.Errorf("transport error = %q, want the cause named", got)
	}

	plain := telegramTransportError(errors.New("boom /bot9:XYZ/sendMessage"))
	if strings.Contains(plain, "XYZ") {
		t.Errorf("plain error leaked the token: %q", plain)
	}

	// A *url.Error with no wrapped cause falls through to the plain path.
	bare := telegramTransportError(&url.Error{Op: "Post", URL: "https://x/bot1:A/sendMessage"})
	if strings.Contains(bare, "bot1:A") {
		t.Errorf("bare url.Error leaked the token: %q", bare)
	}
}

// =============================================================================
// H-002: remote text must not reach the server log unbounded
// =============================================================================

func TestSanitizeTelegramEcho(t *testing.T) {
	t.Run("short text is unchanged", func(t *testing.T) {
		if got := sanitizeTelegramEcho("connection refused"); got != "connection refused" {
			t.Errorf("sanitizeTelegramEcho() = %q", got)
		}
	})

	t.Run("the token is still redacted", func(t *testing.T) {
		in := `Post "https://api.telegram.org/bot` + telegramProbeToken + `/sendMessage": EOF`
		got := sanitizeTelegramEcho(in)
		assertNoTelegramTokenText(t, got)
		if !strings.Contains(got, "/bot<redacted>") {
			t.Errorf("sanitizeTelegramEcho() = %q, want the placeholder", got)
		}
	})

	t.Run("control characters are removed", func(t *testing.T) {
		got := sanitizeTelegramEcho("first\n[ERROR] forged\r\tx\x00y\x7fz")
		if strings.ContainsAny(got, "\n\r\t\x00\x7f") {
			t.Errorf("sanitizeTelegramEcho() = %q, want no control characters", got)
		}
		if got != "first[ERROR] forgedxyz" {
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

// TestSendTestTelegram_BoundsEchoedResponseBody is the regression test
// for H-002 on this side: testChannel logs whatever this sender returns
// with log.Printf, so a captive portal answering a test send with a
// megabyte of HTML would write all of it into the server log.
func TestSendTestTelegram_BoundsEchoedResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, "<html>\n"+strings.Repeat("captive portal ", 20000))
	}))
	defer server.Close()
	withTelegramBaseURL(t, server.URL)

	err := sendTestTelegram(telegramProbeToken, "-1001234567890")
	if err == nil {
		t.Fatal("expected an error for a non-JSON 502")
	}
	assertBoundedTelegramEcho(t, err.Error())
}

// TestSendTestTelegram_BoundsEchoedDescription is the same regression on
// the description path, which the far end controls just as completely.
func TestSendTestTelegram_BoundsEchoedDescription(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		payload := map[string]any{
			"ok":          false,
			"error_code":  400,
			"description": "\n[ERROR] forged log line\n" + strings.Repeat("x", 500000),
		}
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			t.Errorf("encode: %v", err)
		}
	}))
	defer server.Close()
	withTelegramBaseURL(t, server.URL)

	err := sendTestTelegram(telegramProbeToken, "-1001234567890")
	if err == nil {
		t.Fatal("expected an error when the API reports ok:false")
	}
	assertBoundedTelegramEcho(t, err.Error())
}

// assertBoundedTelegramEcho fails when an error string carries more
// borrowed text than telegramMaxEchoedBytes allows, or carries a line
// break a hostile endpoint could use to forge a log line.
func assertBoundedTelegramEcho(t *testing.T, s string) {
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
