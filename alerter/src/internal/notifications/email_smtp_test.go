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
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// smtpBehaviour configures fakeSMTPServer.
type smtpBehaviour struct {
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

// fakeSMTPServer is a minimal SMTP server on loopback.
type fakeSMTPServer struct {
	ln       net.Listener
	b        smtpBehaviour
	accepted atomic.Int32
	mu       sync.Mutex
	messages []string
}

func newFakeSMTPServer(t *testing.T, b smtpBehaviour) *fakeSMTPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &fakeSMTPServer{ln: ln, b: b}
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

func (s *fakeSMTPServer) port() int {
	addr, ok := s.ln.Addr().(*net.TCPAddr)
	if !ok {
		return 0
	}
	return addr.Port
}

func (s *fakeSMTPServer) serve(c net.Conn) {
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
			if s.b.startTLS != nil && !upgraded {
				reply("250-fake.example")
				reply("250-STARTTLS")
			} else {
				reply("250-fake.example")
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

func (s *fakeSMTPServer) messageCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.messages)
}

// testServerTLS returns a server TLS config and a pool trusting it. The
// certificate is net/http/httptest's, which is issued for example.com
// and 127.0.0.1, so the SMTP host in these tests is example.com.
func testServerTLS(t *testing.T) (*tls.Config, *x509.CertPool) {
	t.Helper()
	ts := httptest.NewTLSServer(http.NotFoundHandler())
	defer ts.Close()
	pool := x509.NewCertPool()
	pool.AddCert(ts.Certificate())
	return &tls.Config{Certificates: ts.TLS.Certificates, MinVersion: tls.VersionTLS12}, pool
}

// smtpTestNotifier returns an email notifier that sends to server,
// whatever port the channel names, through the relaxed delivery policy
// (the server is on loopback) and a resolver mapping example.com to it.
func smtpTestNotifier(server *fakeSMTPServer, pool *x509.CertPool) *emailNotifier {
	guarded := deliveryDialer(deliveryPolicy(true),
		fakeResolver(map[string][]string{"example.com": {"127.0.0.1"}}))
	port := strconv.Itoa(server.port())
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		return guarded(ctx, network, net.JoinHostPort(host, port))
	}
	n, ok := newEmailNotifierWithDialer("secret", &mockTemplateRenderer{}, dial).(*emailNotifier)
	if !ok {
		panic("newEmailNotifierWithDialer returned an unexpected type")
	}
	n.rootCAs = pool
	return n
}

func smtpTestChannel(port int, useTLS, withAuth bool) *database.NotificationChannel {
	ch := &database.NotificationChannel{
		SMTPHost:    strPtr("example.com"),
		SMTPPort:    port,
		SMTPUseTLS:  useTLS,
		FromAddress: strPtr("alerts@example.com"),
		FromName:    strPtr("Test User"),
		Recipients: []*database.EmailRecipient{
			{EmailAddress: "jane.doe@example.com", Enabled: true},
		},
	}
	if withAuth {
		ch.SMTPUsername = strPtr("Test User")
		ch.SMTPPassword = strPtr("not-a-real-password")
	}
	return ch
}

// TestEmailNotifier_SMTPDelivery drives both send paths end to end
// against the fake server, through the guarded dialer.
func TestEmailNotifier_SMTPDelivery(t *testing.T) {
	serverTLS, pool := testServerTLS(t)

	t.Run("implicit TLS with authentication", func(t *testing.T) {
		server := newFakeSMTPServer(t, smtpBehaviour{implicitTLS: serverTLS})
		n := smtpTestNotifier(server, pool)
		if err := n.Send(context.Background(), smtpTestChannel(465, true, true), createTestPayload()); err != nil {
			t.Fatalf("Send() = %v", err)
		}
		if server.messageCount() != 1 {
			t.Errorf("server received %d message(s), want 1", server.messageCount())
		}
	})

	t.Run("STARTTLS with authentication", func(t *testing.T) {
		server := newFakeSMTPServer(t, smtpBehaviour{startTLS: serverTLS})
		n := smtpTestNotifier(server, pool)
		if err := n.Send(context.Background(), smtpTestChannel(587, true, true), createTestPayload()); err != nil {
			t.Fatalf("Send() = %v", err)
		}
		if server.messageCount() != 1 {
			t.Errorf("server received %d message(s), want 1", server.messageCount())
		}
	})

	t.Run("a QUIT with no reply still counts as sent", func(t *testing.T) {
		server := newFakeSMTPServer(t, smtpBehaviour{fail: map[string]string{"QUIT": ""}})
		n := smtpTestNotifier(server, pool)
		if err := n.Send(context.Background(), smtpTestChannel(25, false, false), createTestPayload()); err != nil {
			t.Fatalf("Send() = %v", err)
		}
	})
}

// TestEmailNotifier_SMTPFailures checks each step's failure is reported
// as that step's error.
func TestEmailNotifier_SMTPFailures(t *testing.T) {
	serverTLS, pool := testServerTLS(t)

	tests := []struct {
		name     string
		b        smtpBehaviour
		port     int
		withAuth bool
		want     string
	}{
		{"implicit TLS handshake", smtpBehaviour{}, 465, false, "TLS handshake failed"},
		{"implicit TLS greeting", smtpBehaviour{implicitTLS: serverTLS, greeting: "554 go away"}, 465, false, "failed to create SMTP client"},
		{"implicit TLS authentication", smtpBehaviour{implicitTLS: serverTLS, fail: map[string]string{"AUTH": "535 no"}}, 465, true, "SMTP authentication failed"},
		{"greeting", smtpBehaviour{greeting: "554 go away"}, 587, false, "failed to create SMTP client"},
		{"hello", smtpBehaviour{fail: map[string]string{"EHLO": "500 no", "HELO": "500 no"}}, 587, false, "SMTP HELLO failed"},
		{"starttls", smtpBehaviour{startTLS: serverTLS, fail: map[string]string{"STARTTLS": "454 no"}}, 587, false, "STARTTLS failed"},
		// Without TLS, net/smtp refuses PLAIN to a host other than
		// localhost before the server is asked.
		{"authentication in the clear", smtpBehaviour{}, 587, true, "SMTP authentication failed"},
		{"mail from", smtpBehaviour{fail: map[string]string{"MAIL": "550 no"}}, 587, false, "SMTP MAIL FROM failed"},
		{"rcpt to", smtpBehaviour{fail: map[string]string{"RCPT": "550 no"}}, 587, false, "SMTP RCPT TO failed"},
		{"data", smtpBehaviour{fail: map[string]string{"DATA": "554 no"}}, 587, false, "SMTP DATA failed"},
		{"end of data", smtpBehaviour{fail: map[string]string{".": "554 no"}}, 587, false, "failed to close email body"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newFakeSMTPServer(t, tt.b)
			n := smtpTestNotifier(server, pool)
			err := n.Send(context.Background(), smtpTestChannel(tt.port, true, tt.withAuth), createTestPayload())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Send() = %v, want an error containing %q", err, tt.want)
			}
		})
	}
}

// TestEmailNotifier_SMTPHostForms delivers to an IPv6 literal saved with
// and without brackets (1.0.0 required the bracketed form, and the
// server still accepts it) and to a hostname. For each it checks the
// address handed to the dialer is one net.SplitHostPort accepts, and
// that TLS verification and PLAIN authentication succeed, which needs
// the bare host as the TLS ServerName and the SMTP auth host. The test
// certificate covers ::1 and example.com.
func TestEmailNotifier_SMTPHostForms(t *testing.T) {
	serverTLS, pool := testServerTLS(t)

	hosts := []struct {
		name     string
		host     string
		wantAddr string
	}{
		{"bracketed IPv6 literal", "[::1]", "[::1]:"},
		{"unbracketed IPv6 literal", "::1", "[::1]:"},
		{"hostname", "example.com", "example.com:"},
	}
	modes := []struct {
		name string
		port int
		b    smtpBehaviour
	}{
		{"implicit TLS", 465, smtpBehaviour{implicitTLS: serverTLS}},
		{"STARTTLS", 587, smtpBehaviour{startTLS: serverTLS}},
	}
	for _, h := range hosts {
		for _, m := range modes {
			t.Run(h.name+" over "+m.name, func(t *testing.T) {
				server := newFakeSMTPServer(t, m.b)
				n := smtpTestNotifier(server, pool)

				// Record the address Send dials, then connect to the
				// fake server on IPv4 loopback, so the test needs no
				// IPv6 on the host running it.
				var connectAddrs []string
				guarded := deliveryDialer(deliveryPolicy(true), fakeResolver(nil))
				serverAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(server.port()))
				n.dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
					connectAddrs = append(connectAddrs, addr)
					if _, _, err := net.SplitHostPort(addr); err != nil {
						return nil, err
					}
					return guarded(ctx, network, serverAddr)
				}

				ch := smtpTestChannel(m.port, true, true)
				ch.SMTPHost = strPtr(h.host)
				if err := n.Send(context.Background(), ch, createTestPayload()); err != nil {
					t.Fatalf("Send() = %v", err)
				}
				want := h.wantAddr + strconv.Itoa(m.port)
				if len(connectAddrs) != 1 || connectAddrs[0] != want {
					t.Errorf("Send connected to %q, want [%q]", connectAddrs, want)
				}
				if server.messageCount() != 1 {
					t.Errorf("server received %d message(s), want 1", server.messageCount())
				}
			})
		}
	}
}

func TestBareSMTPHost(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"[2001:db8::25]", "2001:db8::25"},
		{"2001:db8::25", "2001:db8::25"},
		{"smtp.example.com", "smtp.example.com"},
		{"[[2001:db8::25]]", "[2001:db8::25]"},
		{"[2001:db8::25", "[2001:db8::25"},
		{"2001:db8::25]", "2001:db8::25]"},
		{"[]", ""},
		{"[", "["},
		{"", ""},
	}
	for _, tt := range tests {
		if got := bareSMTPHost(tt.in); got != tt.want {
			t.Errorf("bareSMTPHost(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestEmailNotifier_ValidateEmptyBracketedHost(t *testing.T) {
	n := &emailNotifier{}
	ch := smtpTestChannel(587, true, false)
	ch.SMTPHost = strPtr("[]")
	if err := n.Validate(ch); err == nil || !strings.Contains(err.Error(), "requires SMTP host") {
		t.Fatalf("Validate() = %v, want an error requiring an SMTP host", err)
	}
}
