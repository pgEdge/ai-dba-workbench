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
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
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
		body, _ := io.ReadAll(r.Body)
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
	// Port 1 is virtually guaranteed to refuse a connection.
	err := sendTestWebhook("http://127.0.0.1:1/", "slack")
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
		body, _ := io.ReadAll(r.Body)
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
		body, _ := io.ReadAll(r.Body)
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
	err := sendTestGenericWebhook("http://127.0.0.1:1/", http.MethodPost, nil, "", "")
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
				body, _ := io.ReadAll(r.Body)
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
