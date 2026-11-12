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
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/alerter/internal/database"
	"github.com/pgedge/ai-workbench/pkg/hostvalidation"
)

// These tests stand in for DNS with a fake resolver, so a name can be
// made to resolve, at dial time, to whatever address a test needs: the
// DNS rebinding case, in which a host that passed its check when the
// channel was saved answers with an internal address when the alert is
// delivered.

// fakeResolver answers each host from a fixed table, and fails for a
// host it does not know, so that no test falls through to real DNS.
func fakeResolver(table map[string][]string) hostvalidation.LookupFunc {
	return func(_ context.Context, host string) ([]string, error) {
		if addrs, ok := table[host]; ok {
			return addrs, nil
		}
		return nil, fmt.Errorf("fake resolver: no entry for %q", host)
	}
}

// testDeliveryClient is the manager's delivery client, built with the
// default (allowInternal false) or relaxed delivery policy and the given
// resolver (nil for the system one).
func testDeliveryClient(allowInternal bool, resolve hostvalidation.LookupFunc) *http.Client {
	return newDeliveryHTTPClient(&config.NotificationsConfig{},
		deliveryDialer(deliveryPolicy(allowInternal), resolve))
}

// hitCounter is an HTTP server that counts the requests reaching it.
type hitCounter struct {
	*httptest.Server
	hits atomic.Int32
}

func newHitCounter(t *testing.T) *hitCounter {
	t.Helper()
	h := &hitCounter{}
	h.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.hits.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`)) //nolint:errcheck
	}))
	t.Cleanup(h.Close)
	return h
}

func (h *hitCounter) port(t *testing.T) string {
	t.Helper()
	u, err := url.Parse(h.URL)
	if err != nil {
		t.Fatalf("parse %q: %v", h.URL, err)
	}
	return u.Port()
}

// assertGenericRefusal fails the test unless err is the dialer's fixed
// refusal, naming none of forbidden.
func assertGenericRefusal(t *testing.T, err error, forbidden ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected the delivery to be refused, got success")
	}
	if !strings.Contains(err.Error(), hostvalidation.ErrNoPermittedAddress.Error()) {
		t.Errorf("error = %q, want the generic refusal", err)
	}
	for _, f := range forbidden {
		if strings.Contains(err.Error(), f) {
			t.Errorf("error = %q names %q", err, f)
		}
	}
}

func TestDeliveryPolicy(t *testing.T) {
	tests := []struct {
		ip            string
		strictBlocks  bool
		relaxedBlocks bool
	}{
		{"8.8.8.8", false, false},
		{"127.0.0.1", true, false},
		{"10.0.0.1", true, false},
		{"192.168.1.1", true, false},
		{"fd00::1", true, false},
		{"169.254.169.254", true, true},
		{"0.0.0.0", true, true},
		{"fe80::1", true, true},
	}
	for _, tt := range tests {
		ip := net.ParseIP(tt.ip)
		if got := deliveryPolicy(false)("h", ip) != nil; got != tt.strictBlocks {
			t.Errorf("default policy refuses %s = %v, want %v", tt.ip, got, tt.strictBlocks)
		}
		if got := deliveryPolicy(true)("h", ip) != nil; got != tt.relaxedBlocks {
			t.Errorf("allow_internal_networks policy refuses %s = %v, want %v",
				tt.ip, got, tt.relaxedBlocks)
		}
	}
}

// TestDeliveryClient_RebindingRefusedAtDial sends through every HTTP
// channel to a name that resolves, at dial time, to the loopback
// address a live server listens on. Under the default policy the server
// must never be reached; with allow_internal_networks set the same
// delivery succeeds, which shows that it was the dialer, and not a
// failed lookup, that refused it.
func TestDeliveryClient_RebindingRefusedAtDial(t *testing.T) {
	target := newHitCounter(t)
	resolve := fakeResolver(map[string][]string{
		"hooks.example.com":    {"127.0.0.1"},
		"api.telegram.example": {"127.0.0.1"},
	})
	base := "http://hooks.example.com:" + target.port(t)
	payload := createTestPayload()

	send := func(client *http.Client, channelType database.NotificationChannelType) error {
		renderer := NewTemplateRenderer()
		switch channelType {
		case database.ChannelTypeSlack:
			return NewSlackNotifier(client, renderer).Send(context.Background(),
				&database.NotificationChannel{WebhookURL: strPtr(base + webhookSecretPath)}, payload)
		case database.ChannelTypeMattermost:
			return NewMattermostNotifier(client, renderer).Send(context.Background(),
				&database.NotificationChannel{WebhookURL: strPtr(base + webhookSecretPath)}, payload)
		case database.ChannelTypeWebhook:
			return NewWebhookNotifier(client, renderer).Send(context.Background(),
				&database.NotificationChannel{EndpointURL: strPtr(base + webhookSecretPath)}, payload)
		default:
			n, ok := NewTelegramNotifier(client, renderer).(*telegramNotifier)
			if !ok {
				t.Fatal("NewTelegramNotifier returned an unexpected type")
			}
			n.apiBaseURL = "http://api.telegram.example:" + target.port(t)
			return n.Send(context.Background(), telegramTestChannel(), payload)
		}
	}

	channelTypes := []database.NotificationChannelType{
		database.ChannelTypeSlack,
		database.ChannelTypeMattermost,
		database.ChannelTypeWebhook,
		database.ChannelTypeTelegram,
	}
	for _, ct := range channelTypes {
		t.Run(string(ct), func(t *testing.T) {
			err := send(testDeliveryClient(false, resolve), ct)
			assertGenericRefusal(t, err, "127.0.0.1", "T00000000")
			if n := target.hits.Load(); n != 0 {
				t.Fatalf("the internal server was reached %d time(s)", n)
			}

			if err := send(testDeliveryClient(true, resolve), ct); err != nil {
				t.Fatalf("allow_internal_networks: Send() = %v", err)
			}
			if n := target.hits.Swap(0); n != 1 {
				t.Fatalf("allow_internal_networks: server reached %d time(s), want 1", n)
			}
		})
	}
}

// TestDeliveryClient_RedirectToBlockedRefusedAtDial follows a redirect
// with the delivery transport, which the shared client never does (see
// TestManager_HTTPClientRefusesRedirects), to show that the dialer is a
// second line: the redirect target is a name resolving to the metadata
// address, refused even with allow_internal_networks set, and the
// redirect from the permitted loopback server is never completed.
func TestDeliveryClient_RedirectToBlockedRefusedAtDial(t *testing.T) {
	var metadataDials atomic.Int32
	resolve := func(ctx context.Context, host string) ([]string, error) {
		if host == "metadata.example" {
			metadataDials.Add(1)
			return []string{"169.254.169.254"}, nil
		}
		return nil, fmt.Errorf("fake resolver: no entry for %q", host)
	}
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://metadata.example/latest/meta-data/", http.StatusFound)
	}))
	defer redirector.Close()

	delivery := testDeliveryClient(true, resolve)
	following := &http.Client{Transport: delivery.Transport, Timeout: delivery.Timeout}

	resp, err := following.Get(redirector.URL + "/hook")
	if err == nil {
		resp.Body.Close()
		t.Fatal("the redirect to the metadata address was followed")
	}
	if !errors.Is(err, hostvalidation.ErrNoPermittedAddress) {
		t.Errorf("err = %v, want ErrNoPermittedAddress", err)
	}
	if metadataDials.Load() != 1 {
		t.Errorf("redirect target resolved %d time(s), want 1", metadataDials.Load())
	}
}

func TestNewDeliveryHTTPClient(t *testing.T) {
	client := newDeliveryHTTPClient(&config.NotificationsConfig{HTTPMaxIdleConns: 4}, nil)
	if client.Timeout == 0 {
		t.Error("a zero configured timeout was not defaulted")
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport is %T", client.Transport)
	}
	if transport.Proxy != nil {
		t.Error("the delivery transport uses a proxy, which the dial guard could not check")
	}
	if transport.MaxIdleConns != 4 {
		t.Errorf("MaxIdleConns = %d, want 4", transport.MaxIdleConns)
	}
}

// TestManager_DeliveryPolicyFollowsConfig checks NewManager wires the
// configured policy into both the HTTP client and the email notifier.
func TestManager_DeliveryPolicyFollowsConfig(t *testing.T) {
	secretPath := filepath.Join(t.TempDir(), "valid.secret")
	if err := os.WriteFile(secretPath, []byte("server-secret-value\n"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	for _, allowInternal := range []bool{false, true} {
		t.Run(fmt.Sprintf("allow_internal_networks=%v", allowInternal), func(t *testing.T) {
			m, err := NewManager(nil, &config.NotificationsConfig{
				Enabled:               true,
				SecretFile:            secretPath,
				AllowInternalNetworks: allowInternal,
			}, false, nil)
			if err != nil {
				t.Fatalf("NewManager() = %v", err)
			}
			email, ok := m.notifiers[database.ChannelTypeEmail].(*emailNotifier)
			if !ok {
				t.Fatal("registered email notifier has an unexpected type")
			}
			transport, ok := m.httpClient.Transport.(*http.Transport)
			if !ok {
				t.Fatalf("Transport is %T", m.httpClient.Transport)
			}
			dials := map[string]hostvalidation.DialContextFunc{
				"email": email.dial,
				"http":  transport.DialContext,
			}
			for name, dial := range dials {
				conn, err := dial(context.Background(), "tcp", ln.Addr().String())
				if allowInternal {
					if err != nil {
						t.Errorf("%s dial to loopback: %v", name, err)
						continue
					}
					conn.Close()
				} else if !errors.Is(err, hostvalidation.ErrNoPermittedAddress) {
					if conn != nil {
						conn.Close()
					}
					t.Errorf("%s dial to loopback: err = %v, want refusal", name, err)
				}
			}
		})
	}
}

// TestEmailNotifier_RebindingRefusedAtDial is the SMTP counterpart of
// TestDeliveryClient_RebindingRefusedAtDial: the SMTP host resolves at
// dial time to the loopback address a live SMTP server listens on.
// Under the default policy no connection is made on either the implicit
// TLS (port 465) or the STARTTLS path; with allow_internal_networks set
// the message is delivered.
func TestEmailNotifier_RebindingRefusedAtDial(t *testing.T) {
	server := newFakeSMTPServer(t, smtpBehaviour{})
	resolve := fakeResolver(map[string][]string{"smtp.example.com": {"127.0.0.1"}})

	channel := func(port int, useTLS bool) *database.NotificationChannel {
		return &database.NotificationChannel{
			SMTPHost:    strPtr("smtp.example.com"),
			SMTPPort:    port,
			SMTPUseTLS:  useTLS,
			FromAddress: strPtr("alerts@example.com"),
			Recipients: []*database.EmailRecipient{
				{EmailAddress: "jane.doe@example.com", Enabled: true},
			},
		}
	}
	strict := newEmailNotifierWithDialer("secret", &mockTemplateRenderer{},
		deliveryDialer(deliveryPolicy(false), resolve))

	t.Run("implicit TLS", func(t *testing.T) {
		err := strict.Send(context.Background(), channel(465, true), createTestPayload())
		assertGenericRefusal(t, err, "127.0.0.1")
	})
	t.Run("STARTTLS", func(t *testing.T) {
		err := strict.Send(context.Background(), channel(server.port(), true), createTestPayload())
		assertGenericRefusal(t, err, "127.0.0.1")
		if n := server.accepted.Load(); n != 0 {
			t.Fatalf("the internal SMTP server accepted %d connection(s)", n)
		}
	})
	t.Run("allow_internal_networks delivers", func(t *testing.T) {
		relaxed := newEmailNotifierWithDialer("secret", &mockTemplateRenderer{},
			deliveryDialer(deliveryPolicy(true), resolve))
		if err := relaxed.Send(context.Background(), channel(server.port(), true), createTestPayload()); err != nil {
			t.Fatalf("Send() = %v", err)
		}
		server.mu.Lock()
		defer server.mu.Unlock()
		if len(server.messages) != 1 {
			t.Fatalf("server received %d message(s), want 1", len(server.messages))
		}
	})
	t.Run("the metadata address stays refused", func(t *testing.T) {
		relaxed := newEmailNotifierWithDialer("secret", &mockTemplateRenderer{},
			deliveryDialer(deliveryPolicy(true),
				fakeResolver(map[string][]string{"smtp.example.com": {"169.254.169.254"}})))
		err := relaxed.Send(context.Background(), channel(587, false), createTestPayload())
		assertGenericRefusal(t, err, "169.254")
	})
}

// TestNewEmailNotifier_DefaultPolicy checks the exported constructor
// refuses an internal SMTP server, so a caller outside the manager gets
// the default policy rather than none.
func TestNewEmailNotifier_DefaultPolicy(t *testing.T) {
	n, ok := NewEmailNotifier("secret", &mockTemplateRenderer{}).(*emailNotifier)
	if !ok {
		t.Fatal("NewEmailNotifier returned an unexpected type")
	}
	if _, err := n.dial(context.Background(), "tcp", "127.0.0.1:25"); !errors.Is(err, hostvalidation.ErrNoPermittedAddress) {
		t.Errorf("dial to loopback: err = %v, want refusal", err)
	}
}
