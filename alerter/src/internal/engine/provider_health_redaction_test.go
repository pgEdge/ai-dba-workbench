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
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"testing"

	pgllm "github.com/pgEdge/pgedge-go-llm-lib/llm"
	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/alerter/internal/llm"
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

// flattenedErr builds the *pgllm.ProviderError the TypeSafe and Voyage
// providers return when the HTTP round trip fails: the transport error's
// text, with no status code.
func flattenedErr(inner error) error {
	return &pgllm.ProviderError{
		Err:      pgllm.ErrProviderError,
		Message:  transportErr("Post", inner).Error(),
		Provider: "typesafe",
	}
}

// TestProviderErrorText_FlattenedTransportFailures proves a transport
// failure the library has flattened into a ProviderError is reduced to
// a category, never the endpoint (security review VULN-001).
func TestProviderErrorText_FlattenedTransportFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"refused", flattenedErr(dialErr(syscall.ECONNREFUSED)), "connection refused"},
		{"reset", flattenedErr(dialErr(syscall.ECONNRESET)), "connection reset"},
		{"host unreachable", flattenedErr(dialErr(syscall.EHOSTUNREACH)), "host unreachable"},
		{"net unreachable", flattenedErr(dialErr(syscall.ENETUNREACH)), "host unreachable"},
		{"dns", flattenedErr(&net.DNSError{Err: "no such host", Name: "llm.internal.example"}),
			"host name lookup failed"},
		{"dns misbehaving", flattenedErr(&net.DNSError{Err: "server misbehaving", Name: "llm.internal.example"}),
			"host name lookup failed"},
		{"timeout", flattenedErr(timeoutErr{}), "timed out"},
		{"client timeout", flattenedErr(errors.New("(Client.Timeout exceeded while awaiting headers)")), "timed out"},
		{"deadline", flattenedErr(context.DeadlineExceeded), "timed out"},
		{"cert", flattenedErr(&tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}),
			"TLS certificate verification failed"},
		{"handshake", flattenedErr(tls.RecordHeaderError{Msg: "tls: first record does not look like a TLS handshake"}),
			"TLS handshake failed"},
		{"eof", flattenedErr(io.EOF), "connection closed by the server"},
		{"other", flattenedErr(errors.New("proxyconnect tcp: llm.internal.example")), "network error"},
		{"create request", &pgllm.ProviderError{Err: pgllm.ErrProviderError,
			Message: `create request: parse "http://llm.internal.example:11434/api": bad`}, "network error"},
		{"URL without the prefix", &pgllm.ProviderError{Err: pgllm.ErrProviderError,
			Message: "fetching http://192.0.2.10:11434/api/embed failed"}, "network error"},
		{"wrapped", fmt.Errorf("classify: %w", flattenedErr(dialErr(syscall.ECONNREFUSED))), "connection refused"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := providerErrorText(tt.err, "typesafe")
			if want := "Request to typesafe endpoint failed: " + tt.want; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
			for _, leak := range []string{"llm.internal.example", "192.0.2.10", "11434", "/api", "abc123", "pw"} {
				if strings.Contains(got, leak) {
					t.Errorf("%q leaks %q", got, leak)
				}
			}
		})
	}
}

// TestProviderErrorText_ProviderErrorsKept proves a ProviderError that is
// not a flattened transport failure keeps its text.
func TestProviderErrorText_ProviderErrorsKept(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"bad response", &pgllm.ProviderError{Err: pgllm.ErrProviderError,
			Message: "decision response has no answers", Provider: "typesafe"}},
		{"with a status", &pgllm.ProviderError{Err: pgllm.ErrProviderError, StatusCode: 502,
			Message: "send request: bad gateway", Provider: "typesafe"}},
		{"other sentinel", &pgllm.ProviderError{Err: pgllm.ErrInvalidRequest,
			Message: "send request: refused", Provider: "typesafe"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := providerErrorText(tt.err, "typesafe"); got != tt.err.Error() {
				t.Errorf("got %q, want %q", got, tt.err.Error())
			}
		})
	}
}

// refusingTransport fails every request as a refused connection to the
// address in the request's URL, as a dial to an unreachable endpoint
// would, without waiting on the network.
type refusingTransport struct{}

func (refusingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	addr, err := net.ResolveTCPAddr("tcp", req.URL.Host)
	if err != nil {
		return nil, err
	}
	return nil, &net.OpError{Op: "dial", Net: "tcp", Addr: addr,
		Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
}

// libClientDecider is a decision provider that calls a library client
// directly, so a test can give the client its own HTTP client and no
// retries.
type libClientDecider struct {
	client   pgllm.Client
	provider string
}

func (d *libClientDecider) ClassifyDecision(ctx context.Context, input string, _ float64) (llm.DecisionVerdict, error) {
	_, err := d.client.Decide(ctx, pgllm.DecisionRequest{
		Model: "dec-model",
		Input: input,
		Questions: []pgllm.DecisionQuestion{{
			Name: "real", Kind: pgllm.DecisionYesNo, Instructions: "Is this real?"}},
	})
	return llm.DecisionVerdict{}, err
}

func (d *libClientDecider) Ping(ctx context.Context) error { return d.client.Ping(ctx) }
func (d *libClientDecider) ModelName() string              { return "dec-model" }
func (d *libClientDecider) ProviderName() string           { return d.provider }

// closedLocalURL returns a URL for a local port nothing listens on.
func closedLocalURL(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return "http://" + addr + "/v1"
}

// TestProviderHealth_DecisionTransportFailureHidesEndpoint drives each
// decision provider in the library at an unreachable endpoint, through
// the health tracker, and checks that the alert names only a category
// whilst the local log keeps the endpoint (security review VULN-001).
func TestProviderHealth_DecisionTransportFailureHidesEndpoint(t *testing.T) {
	for _, provider := range []string{"typesafe", "openai"} {
		for _, endpoint := range []string{"unreachable", "closed local port"} {
			t.Run(provider+" "+endpoint, func(t *testing.T) {
				opts := pgllm.Options{
					APIKey: "test-key-0123456789",
					Retry:  pgllm.RetryConfig{Disabled: true},
				}
				var host string
				if endpoint == "unreachable" {
					opts.BaseURL = "http://192.0.2.1:9/v1"
					opts.HTTPClient = &http.Client{Transport: refusingTransport{}}
					host = "192.0.2.1:9"
				} else {
					opts.BaseURL = closedLocalURL(t)
					host = strings.TrimSuffix(strings.TrimPrefix(opts.BaseURL, "http://"), "/v1")
				}
				client, err := pgllm.NewClient(provider, opts)
				if err != nil {
					t.Fatalf("NewClient: %v", err)
				}
				h := newTrackerHarness(1)
				dec := &healthTrackingDecision{inner: &libClientDecider{client: client, provider: provider},
					provider: provider, tracker: h.tracker}

				_, callErr := dec.ClassifyDecision(context.Background(), "x", 0.5)
				if callErr == nil {
					t.Fatal("the call to an unreachable endpoint succeeded")
				}
				alert := h.store.openFor(providerHealthKey(providerTierDecision, provider))
				if alert == nil {
					t.Fatal("no alert raised")
				}
				if alert.AnomalyDetails == nil {
					t.Fatal("no details")
				}
				hostName, _, _ := strings.Cut(host, ":")
				for _, text := range []string{alert.Description, *alert.AnomalyDetails} {
					if !strings.Contains(text, provider+" endpoint failed: connection refused") {
						t.Errorf("alert text %q does not name the category (call error %q)", text, callErr)
					}
					if strings.Contains(text, hostName) || strings.Contains(text, "/v1") {
						t.Errorf("alert text leaks the endpoint: %q", text)
					}
				}
				if !h.loggedContaining(host) {
					t.Errorf("local log lacks the endpoint %s: %v", host, h.logged)
				}
			})
		}
	}
}
