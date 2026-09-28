/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package engine

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
)

// transportErr builds the error the LLM library returns when the HTTP
// round trip fails, wrapped as the library wraps it.
func transportErr(op string, inner error) error {
	return fmt.Errorf("send request: %w", &url.Error{
		Op:  op,
		URL: "http://user:pw@llm.internal.example:11434/api/embed?key=abc123",
		Err: inner,
	})
}

func dialErr(errno syscall.Errno) error {
	return &net.OpError{Op: "dial", Net: "tcp",
		Addr: &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 11434},
		Err:  os.NewSyscallError("connect", errno)}
}

type timeoutErr struct{}

func (timeoutErr) Error() string { return "i/o timeout" }
func (timeoutErr) Timeout() bool { return true }

// TestProviderErrorText_TransportFailures proves a transport failure is
// reduced to the operation and a category, never the endpoint.
func TestProviderErrorText_TransportFailures(t *testing.T) {
	tests := []struct {
		name  string
		op    string
		inner error
		want  string
	}{
		{"refused", "Post", dialErr(syscall.ECONNREFUSED), "POST to ollama endpoint failed: connection refused"},
		{"reset", "Post", dialErr(syscall.ECONNRESET), "connection reset"},
		{"host unreachable", "Post", dialErr(syscall.EHOSTUNREACH), "host unreachable"},
		{"net unreachable", "Post", dialErr(syscall.ENETUNREACH), "host unreachable"},
		{"dns", "Get", &net.DNSError{Err: "no such host", Name: "llm.internal.example"},
			"GET to ollama endpoint failed: host name lookup failed"},
		{"timeout", "Post", timeoutErr{}, "timed out"},
		{"deadline", "Post", context.DeadlineExceeded, "timed out"},
		{"cert verification", "Post", &tls.CertificateVerificationError{Err: errors.New("bad")},
			"TLS certificate verification failed"},
		{"unknown authority", "Post", x509.UnknownAuthorityError{}, "TLS certificate verification failed"},
		{"hostname", "Post", x509.HostnameError{Host: "llm.internal.example"}, "TLS certificate verification failed"},
		{"invalid cert", "Post", x509.CertificateInvalidError{}, "TLS certificate verification failed"},
		{"record header", "Post", tls.RecordHeaderError{Msg: "first record"}, "TLS handshake failed"},
		{"eof", "Post", io.EOF, "connection closed by the server"},
		{"unexpected eof", "Post", io.ErrUnexpectedEOF, "connection closed by the server"},
		{"other", "Post", errors.New("proxyconnect tcp: llm.internal.example"), "network error"},
		{"empty op", "", io.EOF, "Request to ollama endpoint failed"},
		{"odd op", "Po<st", io.EOF, "Request to ollama endpoint failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := providerErrorText(transportErr(tt.op, tt.inner), "ollama")
			if !strings.Contains(got, tt.want) {
				t.Errorf("got %q, want it to contain %q", got, tt.want)
			}
			for _, leak := range []string{"llm.internal.example", "192.0.2.10", "11434", "/api/embed", "abc123", "pw"} {
				if strings.Contains(got, leak) {
					t.Errorf("%q leaks %q", got, leak)
				}
			}
		})
	}
}

func TestProviderErrorText_NonTransportUnchanged(t *testing.T) {
	if got := providerErrorText(errors.New("API error (status 404): model not found"), "openai"); got != "API error (status 404): model not found" {
		t.Errorf("got %q", got)
	}
}

// TestRedactProviderError_CredentialsAndMarkup covers the basic
// credential, whole query strings, and the markup Slack and Mattermost
// would interpret.
func TestRedactProviderError_CredentialsAndMarkup(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		absent  []string
		present []string
	}{
		{"basic credential", "Authorization: Basic dXNlcjpwYXNz rejected",
			[]string{"dXNlcjpwYXNz"}, []string{"[REDACTED]"}},
		{"bearer lower case", "bearer abc.def", []string{"abc.def"}, []string{"bearer [REDACTED]"}},
		{"query string", "GET http://h.example/x?tenant=acme&sig=zz9 failed",
			[]string{"acme", "zz9"}, []string{"http://h.example/x?[REDACTED] failed"}},
		{"slack link", "see <https://evil.example|click here>",
			[]string{"<", ">"}, []string{"(https://evil.example|click here)"}},
		{"slack mention", "<!channel> ping", []string{"<!channel>"}, []string{"(!channel) ping"}},
		{"markdown link", "[click](https://evil.example)", []string{"[", "]"}, []string{"(click)(https://evil.example)"}},
		{"mattermost mention", "hello @channel", []string{"@channel"}, []string{"＠channel"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redactProviderError(tt.in, nil)
			for _, s := range tt.absent {
				if strings.Contains(got, s) {
					t.Errorf("%q still contains %q", got, s)
				}
			}
			for _, s := range tt.present {
				if !strings.Contains(got, s) {
					t.Errorf("%q lacks %q", got, s)
				}
			}
		})
	}
}

func TestRedactProviderLogError_KeepsEndpoint(t *testing.T) {
	got := redactProviderLogError("dial tcp 192.0.2.10:11434: connection refused; key=abc123\n", []string{"s3cret"})
	if !strings.Contains(got, "192.0.2.10:11434") || strings.Contains(got, "abc123") || strings.Contains(got, "\n") {
		t.Errorf("got %q", got)
	}
}

func TestBaseURLSecrets(t *testing.T) {
	got := baseURLSecrets("https://svc-user:p%40ss-word@llm.example/v1?api=tok-9999&x=1")
	for _, want := range []string{"svc-user:p%40ss-word", "svc-user", "p@ss-word", "p%40ss-word", "tok-9999"} {
		found := false
		for _, s := range got {
			if s == want {
				found = true
			}
		}
		if !found {
			t.Errorf("secrets %q lack %q", got, want)
		}
	}
	for i, s := range got {
		if len(s) < minDerivedSecretLen {
			t.Errorf("secret %q is shorter than the minimum", s)
		}
		if i > 0 && len(got[i-1]) < len(s) {
			t.Errorf("secrets not longest first: %q", got)
		}
	}
	if baseURLSecrets("http://[::1") != nil {
		t.Error("unparsable URL yielded secrets")
	}
	if len(baseURLSecrets("http://localhost:11434")) != 0 {
		t.Error("plain URL yielded secrets")
	}
	if s := baseURLSecrets("http://onlyuser@h.example"); len(s) != 1 || s[0] != "onlyuser" {
		t.Errorf("user without password: %q", s)
	}
}

func TestConfiguredSecrets_IncludesBaseURLSecrets(t *testing.T) {
	cfg := config.NewConfig()
	cfg.LLM.Ollama.BaseURL = "http://olly:hunter22@h.example:11434"
	cfg.LLM.Gemini.BaseURL = "https://g.example/v1?key=gem-secret"
	got := strings.Join(configuredSecrets(cfg), "\n")
	for _, want := range []string{"hunter22", "gem-secret"} {
		if !strings.Contains(got, want) {
			t.Errorf("configured secrets lack %q", want)
		}
	}
}

// TestProviderHealth_TransportFailureAlertHidesEndpoint drives the
// tracker end to end: the alert text names only a category, whilst the
// local log keeps the endpoint for the operator.
func TestProviderHealth_TransportFailureAlertHidesEndpoint(t *testing.T) {
	h := newTrackerHarness(1)
	h.secrets = []string{"pw"}
	err := transportErr("Post", dialErr(syscall.ECONNREFUSED))
	h.tracker.record(context.Background(), providerTierEmbedding, "openai",
		"text-embedding-3-small", err, false)
	alert := h.open()
	if alert == nil {
		t.Fatal("no alert raised")
	}
	if !strings.Contains(alert.Description, "POST to openai endpoint failed: connection refused") {
		t.Errorf("description %q", alert.Description)
	}
	if alert.AnomalyDetails == nil {
		t.Fatal("no details")
	}
	for _, text := range []string{alert.Description, *alert.AnomalyDetails} {
		if strings.Contains(text, "192.0.2.10") || strings.Contains(text, "llm.internal.example") {
			t.Errorf("alert text leaks the endpoint: %q", text)
		}
	}
	if !h.loggedContaining("192.0.2.10:11434") {
		t.Error("local log lacks the endpoint")
	}
	if h.loggedContaining("abc123") || h.loggedContaining(":pw@") {
		t.Error("local log repeats a credential")
	}

	// A different failure refreshes the open alert and logs it too.
	h.tracker.record(context.Background(), providerTierEmbedding, "openai",
		"text-embedding-3-small", transportErr("Post", io.EOF), false)
	if !strings.Contains(h.open().Description, "connection closed by the server") {
		t.Errorf("description not refreshed: %q", h.open().Description)
	}
}
