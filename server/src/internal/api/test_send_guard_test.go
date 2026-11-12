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
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/pkg/hostvalidation"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// These tests stand in for DNS with a fake resolver, so a name can be
// made to resolve, at connect time, to whatever address a test needs:
// the DNS rebinding case, in which a channel host that passed
// hostValidator.ValidateHost answers with an internal or metadata
// address when the test send connects.

// fakeTestSendResolver answers each host from a fixed table, and fails
// for a host it does not know, so that no test falls through to real
// DNS.
func fakeTestSendResolver(table map[string][]string) hostvalidation.LookupFunc {
	return func(_ context.Context, host string) ([]string, error) {
		if addrs, ok := table[host]; ok {
			return addrs, nil
		}
		return nil, fmt.Errorf("fake resolver: no entry for %q", host)
	}
}

// loopbackTestSender is the sender the sender-level tests use: it
// permits loopback, where httptest servers listen, and resolves no name
// at all, so a test that names a host fails rather than reaching DNS.
var loopbackTestSender = newNotificationTestSender(hostvalidation.CheckAlwaysBlocked,
	fakeTestSendResolver(nil))

// strictTestSender is a sender under the default server policy
// (connection_security with nothing set) that resolves names from table.
func strictTestSender(table map[string][]string) *notificationTestSender {
	return newNotificationTestSender(testSendPolicy(NewHostValidator(false, nil, nil)),
		fakeTestSendResolver(table))
}

// testSendHits is an HTTP server counting the requests that reach it.
type testSendHits struct {
	*httptest.Server
	hits atomic.Int32
}

func newTestSendHits(t *testing.T, body string) *testSendHits {
	t.Helper()
	h := &testSendHits{}
	h.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.hits.Add(1)
		_, _ = fmt.Fprint(w, body)
	}))
	t.Cleanup(h.Close)
	return h
}

// urlFor returns the server's URL with its host replaced by host, so
// that a request names host and the resolver decides where it goes.
func (h *testSendHits) urlFor(t *testing.T, host, path string) string {
	t.Helper()
	u, err := url.Parse(h.URL)
	if err != nil {
		t.Fatalf("parse %q: %v", h.URL, err)
	}
	return "http://" + net.JoinHostPort(host, u.Port()) + path
}

// assertGenericRefusal checks err is the guard's refusal and names
// neither the host nor any address.
func assertGenericRefusal(t *testing.T, err error, mentions ...string) {
	t.Helper()
	if !errors.Is(err, hostvalidation.ErrNoPermittedAddress) {
		t.Fatalf("error = %v, want hostvalidation.ErrNoPermittedAddress", err)
	}
	for _, m := range mentions {
		if strings.Contains(err.Error(), m) {
			t.Errorf("error %q names %q", err, m)
		}
	}
}

// TestTestSendPolicy checks the floor is applied before the
// connection_security rules, so neither an allow-listed name nor
// allow_internal_networks reaches a metadata address.
func TestTestSendPolicy(t *testing.T) {
	tests := []struct {
		name          string
		allowInternal bool
		allowed       []string
		blocked       []string
		host          string
		ip            string
		wantErr       bool
	}{
		{name: "public", host: "hooks.example.test", ip: "8.8.8.8"},
		{name: "private refused by default", host: "hooks.example.test", ip: "10.0.0.5", wantErr: true},
		{name: "loopback refused by default", host: "hooks.example.test", ip: "127.0.0.1", wantErr: true},
		{name: "private with allow_internal_networks", allowInternal: true, host: "hooks.example.test", ip: "10.0.0.5"},
		{name: "metadata with allow_internal_networks", allowInternal: true, host: "hooks.example.test", ip: "169.254.169.254", wantErr: true},
		{name: "unspecified with allow_internal_networks", allowInternal: true, host: "hooks.example.test", ip: "0.0.0.0", wantErr: true},
		{name: "allow-listed name at a private address", allowed: []string{"hooks.example.test"}, host: "hooks.example.test", ip: "10.0.0.5"},
		{name: "allow-listed name at metadata", allowed: []string{"hooks.example.test"}, host: "hooks.example.test", ip: "169.254.169.254", wantErr: true},
		{name: "allow-listed link-local range", allowed: []string{"169.254.0.0/16"}, host: "hooks.example.test", ip: "169.254.169.254", wantErr: true},
		{name: "block-listed public range", blocked: []string{"8.8.8.0/24"}, host: "hooks.example.test", ip: "8.8.8.8", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := testSendPolicy(NewHostValidator(tt.allowInternal, tt.allowed, tt.blocked))
			err := policy(tt.host, net.ParseIP(tt.ip))
			if (err != nil) != tt.wantErr {
				t.Fatalf("policy(%q, %s) = %v, wantErr %v", tt.host, tt.ip, err, tt.wantErr)
			}
		})
	}
}

// TestNotificationTestSender_NoProxy pins the transport's settings: no
// proxy, so the guard checks the channel's address rather than a
// proxy's, and the guarded dial function.
func TestNotificationTestSender_NoProxy(t *testing.T) {
	s := strictTestSender(nil)
	tr, ok := s.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport is %T, want *http.Transport", s.client.Transport)
	}
	if tr.Proxy != nil {
		t.Error("transport has a proxy function, want none")
	}
	if tr.DialContext == nil {
		t.Error("transport has no DialContext, want the guarded dialer")
	}
	if s.client.CheckRedirect == nil {
		t.Error("client follows redirects, want them refused")
	}
}

// TestNotificationTestSender_HTTPRebinding checks each HTTP sender
// connects only where the policy permits, whatever the channel's host
// resolves to at connect time.
func TestNotificationTestSender_HTTPRebinding(t *testing.T) {
	const host = "hooks.example.test"
	const path = "/services/T00000000/B00000000/XXXXXXXX"

	senders := map[string]func(s *notificationTestSender, target string) error{
		"slack": func(s *notificationTestSender, target string) error {
			return s.sendTestWebhook(context.Background(), target, "Slack")
		},
		"generic webhook": func(s *notificationTestSender, target string) error {
			return s.sendTestGenericWebhook(context.Background(), target, http.MethodPost, nil, "", "")
		},
	}

	for name, send := range senders {
		t.Run(name, func(t *testing.T) {
			for _, rebound := range []string{"10.0.0.5", "127.0.0.1", "169.254.169.254", "::1"} {
				t.Run("rebound to "+rebound, func(t *testing.T) {
					server := newTestSendHits(t, "ok")
					s := strictTestSender(map[string][]string{host: {rebound}})
					err := send(s, server.urlFor(t, host, path))
					assertGenericRefusal(t, err, host, rebound, "T00000000")
					if server.hits.Load() != 0 {
						t.Errorf("server received %d request(s), want none", server.hits.Load())
					}
				})
			}

			t.Run("permitted address is reached through the resolver", func(t *testing.T) {
				server := newTestSendHits(t, "ok")
				// allow_internal_networks permits loopback, and the
				// first address, being block-listed, is skipped.
				v := NewHostValidator(true, nil, []string{"10.0.0.0/8"})
				s := newNotificationTestSender(testSendPolicy(v),
					fakeTestSendResolver(map[string][]string{host: {"10.0.0.5", "127.0.0.1"}}))
				if err := send(s, server.urlFor(t, host, path)); err != nil {
					t.Fatalf("send = %v, want nil", err)
				}
				if server.hits.Load() != 1 {
					t.Errorf("server received %d request(s), want 1", server.hits.Load())
				}
			})

			t.Run("IP literal is judged without resolving", func(t *testing.T) {
				server := newTestSendHits(t, "ok")
				err := send(strictTestSender(nil), server.URL+path)
				assertGenericRefusal(t, err, "127.0.0.1", "T00000000")
			})

			t.Run("resolver failure is a send failure, not a refusal", func(t *testing.T) {
				err := send(strictTestSender(nil), "http://unknown.example.test"+path)
				if err == nil || isRefusedTarget(err) {
					t.Fatalf("send = %v, want a non-refusal error", err)
				}
				assertNoWebhookURL(t, err.Error())
			})
		})
	}
}

// TestNotificationTestSender_TelegramRebinding checks the Telegram
// sender, whose host is a constant, is held to the policy too.
func TestNotificationTestSender_TelegramRebinding(t *testing.T) {
	const host = "api.telegram.test"
	server := newTestSendHits(t, `{"ok":true,"result":{"message_id":1}}`)
	withTelegramBaseURL(t, strings.TrimSuffix(server.urlFor(t, host, "/"), "/"))

	err := strictTestSender(map[string][]string{host: {"169.254.169.254"}}).
		sendTestTelegram(context.Background(), telegramProbeToken, "-1001234567890")
	assertGenericRefusal(t, err, host, "169.254.169.254", telegramProbeToken)
	if server.hits.Load() != 0 {
		t.Errorf("server received %d request(s), want none", server.hits.Load())
	}

	permissive := newNotificationTestSender(testSendPolicy(NewHostValidator(true, nil, nil)),
		fakeTestSendResolver(map[string][]string{host: {"127.0.0.1"}}))
	if err := permissive.sendTestTelegram(context.Background(), telegramProbeToken, "-1001234567890"); err != nil {
		t.Fatalf("sendTestTelegram = %v, want nil", err)
	}
	if server.hits.Load() != 1 {
		t.Errorf("server received %d request(s), want 1", server.hits.Load())
	}
}

// =============================================================================
// SMTP
// =============================================================================

// smtpFakeBehaviour configures smtpFake.
type smtpFakeBehaviour struct {
	// greeting replaces the "220" banner.
	greeting string
	// implicitTLS wraps every connection in TLS before the banner, as
	// on port 465.
	implicitTLS *tls.Config
	// startTLS offers the STARTTLS extension, upgrading with this.
	startTLS *tls.Config
	// fail maps a command verb (EHLO, HELO, STARTTLS, AUTH, MAIL, RCPT,
	// DATA, and "." for the end of the message body) to the reply sent
	// in place of success. QUIT mapped to "" closes without a reply.
	fail map[string]string
}

// smtpFake is a minimal SMTP server on loopback.
type smtpFake struct {
	ln       net.Listener
	b        smtpFakeBehaviour
	accepted atomic.Int32
	mu       sync.Mutex
	messages []string
}

func newSMTPFake(t *testing.T, b smtpFakeBehaviour) *smtpFake {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &smtpFake{ln: ln, b: b}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			s.accepted.Add(1)
			go s.serve(c)
		}
	}()
	return s
}

func (s *smtpFake) port() string {
	_, port, _ := net.SplitHostPort(s.ln.Addr().String()) //nolint:errcheck // a listener address always splits
	return port
}

func (s *smtpFake) messageCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.messages)
}

func (s *smtpFake) serve(c net.Conn) {
	if s.b.implicitTLS != nil {
		c = tls.Server(c, s.b.implicitTLS)
	}
	defer func() { c.Close() }()
	r := bufio.NewReader(c)
	reply := func(line string) { _, _ = fmt.Fprintf(c, "%s\r\n", line) }
	failed := func(verb string) bool {
		if line, ok := s.b.fail[verb]; ok {
			reply(line)
			return true
		}
		return false
	}

	if s.b.greeting != "" {
		reply(s.b.greeting)
		return
	}
	reply("220 fake.example ESMTP")
	upgraded := false
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		verb := strings.ToUpper(strings.Fields(strings.TrimSpace(line) + " x")[0])
		switch verb {
		case "EHLO":
			if failed(verb) {
				continue
			}
			reply("250-fake.example")
			if s.b.startTLS != nil && !upgraded {
				reply("250-STARTTLS")
			}
			reply("250 AUTH PLAIN")
		case "STARTTLS":
			if failed(verb) {
				continue
			}
			reply("220 ready")
			tc := tls.Server(c, s.b.startTLS)
			if err := tc.Handshake(); err != nil {
				return
			}
			c = tc
			r = bufio.NewReader(c)
			upgraded = true
		case "AUTH":
			if !failed(verb) {
				reply("235 accepted")
			}
		case "DATA":
			if failed(verb) {
				continue
			}
			reply("354 go ahead")
			var msg strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				msg.WriteString(l)
			}
			if failed(".") {
				continue
			}
			s.mu.Lock()
			s.messages = append(s.messages, msg.String())
			s.mu.Unlock()
			reply("250 queued")
		case "QUIT":
			if _, ok := s.b.fail[verb]; !ok {
				reply("221 bye")
			}
			return
		default:
			if !failed(verb) {
				reply("250 ok")
			}
		}
	}
}

// smtpFakeTLS returns a server TLS config and a pool trusting it. The
// certificate is net/http/httptest's, which is issued for example.com
// and 127.0.0.1, so the SMTP host in these tests is example.com.
func smtpFakeTLS(t *testing.T) (*tls.Config, *x509.CertPool) {
	t.Helper()
	ts := httptest.NewTLSServer(http.NotFoundHandler())
	defer ts.Close()
	pool := x509.NewCertPool()
	pool.AddCert(ts.Certificate())
	return &tls.Config{Certificates: ts.TLS.Certificates, MinVersion: tls.VersionTLS12}, pool
}

// smtpTestSender returns a sender that reaches server, whatever port the
// channel names, through a guarded dialer under policy that resolves
// example.com to resolveTo.
func smtpTestSender(server *smtpFake, pool *x509.CertPool, policy hostvalidation.AddrPolicy, resolveTo string) *notificationTestSender {
	s := newNotificationTestSender(policy,
		fakeTestSendResolver(map[string][]string{"example.com": {resolveTo}}))
	guarded := s.dial
	port := server.port()
	s.dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		return guarded(ctx, network, net.JoinHostPort(host, port))
	}
	s.rootCAs = pool
	return s
}

// permissiveTestSendPolicy permits loopback, where the fake listens.
var permissiveTestSendPolicy = testSendPolicy(NewHostValidator(true, nil, nil))

// sendSMTPTest sends the test email to example.com on port.
func sendSMTPTest(s *notificationTestSender, port int, useTLS, withAuth bool) error {
	user, pass := "", ""
	if withAuth {
		user, pass = "Test User", "not-a-real-password"
	}
	return s.sendTestEmail(context.Background(), testEmail{
		Host: "example.com", Port: port, Username: user, Password: pass, UseTLS: useTLS,
		FromAddress: "alerts@example.com", FromName: "Test User",
		To: []string{"jane.doe@example.com"},
	})
}

// TestSendTestEmail_Delivery drives each send path end to end against
// the fake server, through the guarded dialer.
func TestSendTestEmail_Delivery(t *testing.T) {
	serverTLS, pool := smtpFakeTLS(t)
	tests := []struct {
		name     string
		b        smtpFakeBehaviour
		port     int
		useTLS   bool
		withAuth bool
	}{
		{"implicit TLS with authentication", smtpFakeBehaviour{implicitTLS: serverTLS}, 465, true, true},
		{"STARTTLS with authentication", smtpFakeBehaviour{startTLS: serverTLS}, 587, true, true},
		{"STARTTLS not offered", smtpFakeBehaviour{}, 587, true, false},
		{"plain", smtpFakeBehaviour{}, 25, false, false},
		// As in the alerter, an advertised STARTTLS is used with TLS
		// off too; PLAIN authentication succeeding to a host other than
		// localhost shows the session was upgraded.
		{"STARTTLS offered with TLS off", smtpFakeBehaviour{startTLS: serverTLS}, 25, false, true},
		{"a QUIT with no reply still counts as sent", smtpFakeBehaviour{fail: map[string]string{"QUIT": ""}}, 25, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newSMTPFake(t, tt.b)
			s := smtpTestSender(server, pool, permissiveTestSendPolicy, "127.0.0.1")
			if err := sendSMTPTest(s, tt.port, tt.useTLS, tt.withAuth); err != nil {
				t.Fatalf("sendTestEmail = %v", err)
			}
			if server.messageCount() != 1 {
				t.Errorf("server received %d message(s), want 1", server.messageCount())
			}
		})
	}
}

// TestSendTestEmail_Failures checks each step's failure is reported as
// that step's error.
func TestSendTestEmail_Failures(t *testing.T) {
	serverTLS, pool := smtpFakeTLS(t)
	tests := []struct {
		name     string
		b        smtpFakeBehaviour
		port     int
		useTLS   bool
		withAuth bool
		want     string
	}{
		{"implicit TLS handshake", smtpFakeBehaviour{}, 465, true, false, "TLS handshake failed"},
		{"implicit TLS greeting", smtpFakeBehaviour{implicitTLS: serverTLS, greeting: "554 go away"}, 465, true, false, "failed to create SMTP client"},
		{"implicit TLS authentication", smtpFakeBehaviour{implicitTLS: serverTLS, fail: map[string]string{"AUTH": "535 no"}}, 465, true, true, "SMTP authentication failed"},
		{"STARTTLS greeting", smtpFakeBehaviour{greeting: "554 go away"}, 587, true, false, "failed to create SMTP client"},
		{"STARTTLS hello", smtpFakeBehaviour{fail: map[string]string{"EHLO": "500 no", "HELO": "500 no"}}, 587, true, false, "SMTP HELLO failed"},
		{"STARTTLS refused", smtpFakeBehaviour{startTLS: serverTLS, fail: map[string]string{"STARTTLS": "454 no"}}, 587, true, false, "STARTTLS failed"},
		// Without TLS, net/smtp refuses PLAIN to a host other than
		// localhost before the server is asked.
		{"STARTTLS authentication in the clear", smtpFakeBehaviour{}, 587, true, true, "SMTP authentication failed"},
		{"plain greeting", smtpFakeBehaviour{greeting: "554 go away"}, 25, false, false, "failed to create SMTP client"},
		{"plain hello", smtpFakeBehaviour{fail: map[string]string{"EHLO": "500 no", "HELO": "500 no"}}, 25, false, false, "SMTP HELLO failed"},
		{"plain authentication", smtpFakeBehaviour{}, 25, false, true, "SMTP authentication failed"},
		{"STARTTLS refused with TLS off", smtpFakeBehaviour{startTLS: serverTLS, fail: map[string]string{"STARTTLS": "454 no"}}, 25, false, false, "STARTTLS failed"},
		{"mail from", smtpFakeBehaviour{fail: map[string]string{"MAIL": "550 no"}}, 25, false, false, "SMTP MAIL FROM failed"},
		{"rcpt to", smtpFakeBehaviour{fail: map[string]string{"RCPT": "550 no"}}, 25, false, false, "SMTP RCPT TO failed"},
		{"data", smtpFakeBehaviour{fail: map[string]string{"DATA": "554 no"}}, 25, false, false, "SMTP DATA failed"},
		{"end of data", smtpFakeBehaviour{fail: map[string]string{".": "554 no"}}, 25, false, false, "failed to close email body"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newSMTPFake(t, tt.b)
			s := smtpTestSender(server, pool, permissiveTestSendPolicy, "127.0.0.1")
			err := sendSMTPTest(s, tt.port, tt.useTLS, tt.withAuth)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("sendTestEmail = %v, want an error containing %q", err, tt.want)
			}
		})
	}
}

// TestSendTestEmail_Rebinding checks every SMTP path refuses an SMTP
// host that resolves, at connect time, to an address the policy
// refuses, without connecting and without naming the host or address.
func TestSendTestEmail_Rebinding(t *testing.T) {
	_, pool := smtpFakeTLS(t)
	for _, tc := range []struct {
		name   string
		port   int
		useTLS bool
	}{
		{"implicit TLS", 465, true},
		{"STARTTLS", 587, true},
		{"plain", 25, false},
	} {
		for _, rebound := range []string{"10.0.0.5", "127.0.0.1", "169.254.169.254"} {
			t.Run(tc.name+" rebound to "+rebound, func(t *testing.T) {
				server := newSMTPFake(t, smtpFakeBehaviour{})
				s := smtpTestSender(server, pool, testSendPolicy(NewHostValidator(false, nil, nil)), rebound)
				err := sendSMTPTest(s, tc.port, tc.useTLS, false)
				assertGenericRefusal(t, err, "example.com", rebound)
				if server.accepted.Load() != 0 {
					t.Errorf("server accepted %d connection(s), want none", server.accepted.Load())
				}
			})
		}
	}
}

// TestSendTestEmail_IPv6LiteralAddress checks the address handed to the
// dialer brackets an IPv6 literal host, so it can be split again.
func TestSendTestEmail_IPv6LiteralAddress(t *testing.T) {
	var got string
	s := &notificationTestSender{dial: func(_ context.Context, _, addr string) (net.Conn, error) {
		got = addr
		return nil, errors.New("no connection made")
	}}
	_ = s.sendTestEmail(context.Background(), testEmail{ //nolint:errcheck // only the address matters
		Host: "2001:db8::25", Port: 25,
		FromAddress: "alerts@example.com", To: []string{"jane.doe@example.com"},
	})
	if got != "[2001:db8::25]:25" {
		t.Errorf("dial address %q, want %q", got, "[2001:db8::25]:25")
	}

	// A host stored in one bracket pair is sent to bare, not nested.
	got = ""
	_ = s.sendTestEmail(context.Background(), testEmail{ //nolint:errcheck // only the address matters
		Host: "[2001:db8::25]", Port: 25,
		FromAddress: "alerts@example.com", To: []string{"jane.doe@example.com"},
	})
	if got != "[2001:db8::25]:25" {
		t.Errorf("bracketed host: dial address %q, want %q", got, "[2001:db8::25]:25")
	}
}

// stallingSMTPSender returns a sender whose every connection reaches a
// loopback listener that accepts and then never says anything, as an
// SMTP server that has stopped answering would.
func stallingSMTPSender(t *testing.T) *notificationTestSender {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	var mu sync.Mutex
	var held []net.Conn
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		for _, c := range held {
			c.Close()
		}
	})
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			held = append(held, c)
			mu.Unlock()
		}
	}()
	return &notificationTestSender{dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", ln.Addr().String())
	}}
}

// TestSendTestEmail_StalledServer checks that an SMTP server which
// accepts the connection and then goes quiet cannot hold a test send
// open, on the implicit TLS path (the handshake) and the STARTTLS path
// (the greeting) alike: the connection's deadline is the context's, and
// cancelling the context closes the connection even before then.
func TestSendTestEmail_StalledServer(t *testing.T) {
	for _, tc := range []struct {
		name   string
		port   int
		useTLS bool
	}{
		{"implicit TLS", 465, true},
		{"STARTTLS", 587, true},
		{"TLS off", 25, false},
	} {
		send := func(ctx context.Context) error {
			return stallingSMTPSender(t).sendTestEmail(ctx, testEmail{
				Host: "example.com", Port: tc.port, UseTLS: tc.useTLS,
				FromAddress: "alerts@example.com", To: []string{"jane.doe@example.com"},
			})
		}
		t.Run(tc.name+" deadline", func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			start := time.Now()
			if err := send(ctx); err == nil {
				t.Fatal("expected the stalled send to fail")
			}
			if elapsed := time.Since(start); elapsed > 5*time.Second {
				t.Errorf("send took %v, want it bounded by the deadline", elapsed)
			}
		})
		t.Run(tc.name+" cancel", func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			timer := time.AfterFunc(200*time.Millisecond, cancel)
			defer timer.Stop()
			start := time.Now()
			if err := send(ctx); err == nil {
				t.Fatal("expected the cancelled send to fail")
			}
			if elapsed := time.Since(start); elapsed > 5*time.Second {
				t.Errorf("send took %v, want it ended by the cancellation", elapsed)
			}
		})
	}
}

// deadlineRefusingConn is a connection whose SetDeadline fails.
type deadlineRefusingConn struct {
	net.Conn
	closed atomic.Bool
}

func (c *deadlineRefusingConn) SetDeadline(time.Time) error {
	return errors.New("deadline not supported")
}

func (c *deadlineRefusingConn) Close() error {
	c.closed.Store(true)
	return c.Conn.Close()
}

// TestDialSMTP_DeadlineFailure checks that a connection whose deadline
// cannot be set is closed and reported as a connect failure rather than
// used without a bound.
func TestDialSMTP_DeadlineFailure(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	conn := &deadlineRefusingConn{Conn: client}
	s := &notificationTestSender{dial: func(context.Context, string, string) (net.Conn, error) {
		return conn, nil
	}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, _, err := s.dialSMTP(ctx, "example.com:25"); err == nil ||
		!strings.Contains(err.Error(), "failed to connect to SMTP server") {
		t.Fatalf("dialSMTP = %v, want a connect failure", err)
	}
	if !conn.closed.Load() {
		t.Error("connection left open")
	}
}

// =============================================================================
// Handler
// =============================================================================

// TestTestChannel_RebindingRefusedGenerically drives the endpoint with a
// channel host that passes hostValidator.ValidateHost, because it is
// allow-listed by name, and then resolves to the metadata address when
// the test send connects. The guard refuses it, and the handler answers
// as it does a failed host check, naming neither host nor address.
func TestTestChannel_RebindingRefusedGenerically(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()
	authStore, cleanupAuth := newAuthStoreForChannelTests(t)
	defer cleanupAuth()
	userID := setupUserWithPermission(t, authStore, "channel_admin",
		auth.PermManageNotificationChannels)
	grantAllConnections(t, authStore, userID)

	const hookHost = "hooks.example.test"
	const smtpHost = "smtp.example.test"
	const metadata = "169.254.169.254"
	handler := NewNotificationChannelHandlerWithSecurity(ds, authStore, auth.NewRBACChecker(authStore),
		false, []string{hookHost, smtpHost}, nil)
	handler.testSender = newNotificationTestSender(testSendPolicy(handler.hostValidator),
		fakeTestSendResolver(map[string][]string{hookHost: {metadata}, smtpHost: {metadata}}))

	hookURL := "http://" + hookHost + "/services/T00000000/B00000000/XXXXXXXX"
	slack := createTypedTestChannel(t, ds, "rebind-slack", database.ChannelTypeSlack, ptr(hookURL), nil)
	generic := createTypedTestChannel(t, ds, "rebind-webhook", database.ChannelTypeWebhook, nil, ptr(hookURL))
	owner := "channel_admin"
	email := &database.NotificationChannel{
		OwnerUsername: &owner, Enabled: true, ChannelType: database.ChannelTypeEmail,
		Name: "rebind-email", HTTPMethod: "POST", SMTPPort: 25,
		SMTPHost: ptr(smtpHost), FromAddress: ptr("alerts@example.com"),
	}
	if err := ds.CreateNotificationChannel(context.Background(), email); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := ds.CreateEmailRecipient(context.Background(),
		&database.EmailRecipient{ChannelID: email.ID, EmailAddress: "jane.doe@example.com", Enabled: true}); err != nil {
		t.Fatalf("CreateEmailRecipient: %v", err)
	}

	for _, tc := range []struct {
		name string
		id   int64
		want string
	}{
		{"slack", slack, "Invalid webhook host"},
		{"generic webhook", generic, "Invalid endpoint host"},
		{"email", email.ID, "Invalid SMTP host"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := postChannelTest(t, handler, userID, tc.id)
			body := rec.Body.String()
			for _, leak := range []string{hookHost, smtpHost, metadata, "T00000000"} {
				if strings.Contains(body, leak) {
					t.Errorf("response names %q; body=%s", leak, body)
				}
			}
			assertTestChannelError(t, handler, userID, tc.id, http.StatusBadRequest, tc.want)
		})
	}

	// Telegram's host is a constant, so a refusal there is a send
	// failure like any other.
	const tgHost = "api.telegram.test"
	withTelegramBaseURL(t, "http://"+tgHost)
	handler.testSender = newNotificationTestSender(testSendPolicy(handler.hostValidator),
		fakeTestSendResolver(map[string][]string{tgHost: {metadata}}))
	token := telegramHandlerToken
	chatID := "-1001234567890"
	tg := createTelegramChannel(t, ds, "rebind-telegram", &token, &chatID)
	assertTestChannelError(t, handler, userID, tg, http.StatusBadGateway,
		"Failed to send test Telegram message")
}

// TestNewNotificationChannelHandler_TestSenderUsesValidator checks the
// constructor wires the test sender to the handler's own validator: a
// block-listed public address is refused at connect time.
func TestNewNotificationChannelHandler_TestSenderUsesValidator(t *testing.T) {
	h := NewNotificationChannelHandlerWithSecurity(nil, nil, nil, true, nil, []string{"127.0.0.0/8"})
	if h.testSender == nil {
		t.Fatal("testSender is nil")
	}
	server := newTestSendHits(t, "ok")
	err := h.testSender.sendTestWebhook(context.Background(), server.URL+"/hook", "Slack")
	assertGenericRefusal(t, err, "127.0.0.1")
	if server.hits.Load() != 0 {
		t.Errorf("server received %d request(s), want none", server.hits.Load())
	}

	permissive := NewNotificationChannelHandlerWithSecurity(nil, nil, nil, true, nil, nil)
	if err := permissive.testSender.sendTestWebhook(context.Background(), server.URL+"/hook", "Slack"); err != nil {
		t.Fatalf("sendTestWebhook = %v, want nil", err)
	}
}

// TestTestChannel_BracketedSMTPHost checks that an email channel whose
// IPv6 literal host was saved in one bracket pair, as version 1.0.0
// required, passes the host check in its bare form and is sent to that
// address, instead of being refused as an invalid host.
func TestTestChannel_BracketedSMTPHost(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()
	authStore, cleanupAuth := newAuthStoreForChannelTests(t)
	defer cleanupAuth()
	userID := setupUserWithPermission(t, authStore, "channel_admin",
		auth.PermManageNotificationChannels)
	grantAllConnections(t, authStore, userID)

	handler := NewNotificationChannelHandlerWithSecurity(ds, authStore, auth.NewRBACChecker(authStore),
		true, nil, nil)
	var dialled atomic.Value
	handler.testSender = &notificationTestSender{dial: func(_ context.Context, _, addr string) (net.Conn, error) {
		dialled.Store(addr)
		return nil, errors.New("no connection made")
	}}

	owner := "channel_admin"
	email := &database.NotificationChannel{
		OwnerUsername: &owner, Enabled: true, ChannelType: database.ChannelTypeEmail,
		Name: "bracketed-email", HTTPMethod: "POST", SMTPPort: 25,
		SMTPHost: ptr("[2001:db8::25]"), FromAddress: ptr("alerts@example.com"),
	}
	if err := ds.CreateNotificationChannel(context.Background(), email); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := ds.CreateEmailRecipient(context.Background(),
		&database.EmailRecipient{ChannelID: email.ID, EmailAddress: "jane.doe@example.com", Enabled: true}); err != nil {
		t.Fatalf("CreateEmailRecipient: %v", err)
	}

	assertTestChannelError(t, handler, userID, email.ID, http.StatusBadGateway, "Failed to send test email")
	if got, _ := dialled.Load().(string); got != "[2001:db8::25]:25" {
		t.Errorf("dial address %q, want %q", got, "[2001:db8::25]:25")
	}
}
