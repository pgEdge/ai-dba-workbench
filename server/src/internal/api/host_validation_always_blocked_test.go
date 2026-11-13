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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// alwaysBlockedLiterals are addresses hostvalidation.IsAlwaysBlockedIP
// reports, in the forms a user could type: IPv4, IPv6, IPv4-mapped and
// bracketed.
var alwaysBlockedLiterals = []string{
	"0.0.0.0",
	"169.254.169.254",
	"169.254.1.1",
	"100.100.100.200",
	"::",
	"fe80::1",
	"[fe80::1]",
	"fd00:ec2::254",
	"::ffff:169.254.169.254",
}

// TestValidateHost_AlwaysBlockedWhateverTheSettings checks that an
// always-blocked literal is refused with ErrAlwaysBlockedHost however
// permissive the configuration: allow_internal_networks on, and the
// address listed in allowed_hosts both exactly and as a range.
func TestValidateHost_AlwaysBlockedWhateverTheSettings(t *testing.T) {
	validators := map[string]*HostValidator{
		"default":        NewHostValidator(false, nil, nil),
		"allow internal": NewHostValidator(true, nil, nil),
		"allow-listed": NewHostValidator(true, []string{
			"0.0.0.0/0", "::/0", "169.254.169.254", "fe80::1", "100.100.100.200",
		}, nil),
	}
	for vname, v := range validators {
		for _, host := range alwaysBlockedLiterals {
			t.Run(vname+"/"+host, func(t *testing.T) {
				err := v.ValidateHost(host)
				if !errors.Is(err, ErrAlwaysBlockedHost) {
					t.Fatalf("ValidateHost(%q) = %v, want ErrAlwaysBlockedHost", host, err)
				}
				// ValidateHostAddr shares the check.
				err = v.ValidateHostAddr(host)
				if !errors.Is(err, ErrAlwaysBlockedHost) {
					t.Fatalf("ValidateHostAddr(%q) = %v, want ErrAlwaysBlockedHost", host, err)
				}
			})
		}
	}
}

// TestValidateHost_AlwaysBlockedLeavesOtherAddressesAlone checks that
// the floor does not refuse addresses allow_internal_networks permits.
func TestValidateHost_AlwaysBlockedLeavesOtherAddressesAlone(t *testing.T) {
	v := NewHostValidator(true, nil, nil)
	for _, host := range []string{"127.0.0.1", "::1", "10.0.0.1", "192.0.2.1", "2001:db8::1"} {
		if err := v.ValidateHost(host); err != nil {
			t.Errorf("ValidateHost(%q) = %v, want nil", host, err)
		}
	}
}

// TestValidateHost_NameResolvingToAlwaysBlocked checks that a host name
// is refused when any of its addresses is always blocked, even when
// another of them is allow-listed, which would otherwise end the check.
func TestValidateHost_NameResolvingToAlwaysBlocked(t *testing.T) {
	v := NewHostValidator(true, []string{"192.0.2.0/24"}, nil)
	v.lookupIP = func(host string) ([]net.IP, error) {
		switch host {
		case "metadata.example.com":
			return []net.IP{net.ParseIP("192.0.2.10"), net.ParseIP("169.254.169.254")}, nil
		case "db.example.com":
			return []net.IP{net.ParseIP("192.0.2.10"), net.ParseIP("10.0.0.5")}, nil
		}
		return nil, errors.New("no such host")
	}

	err := v.ValidateHost("metadata.example.com")
	if !errors.Is(err, ErrAlwaysBlockedHost) {
		t.Fatalf("ValidateHost(metadata.example.com) = %v, want ErrAlwaysBlockedHost", err)
	}
	if !strings.Contains(err.Error(), "169.254.169.254") {
		t.Errorf("logged error %q should name the resolved address", err)
	}
	if err := v.ValidateHost("db.example.com"); err != nil {
		t.Errorf("ValidateHost(db.example.com) = %v, want nil", err)
	}
	if err := v.ValidateHost("missing.example.com"); err == nil ||
		errors.Is(err, ErrAlwaysBlockedHost) {
		t.Errorf("ValidateHost(missing.example.com) = %v, want a lookup failure", err)
	}
}

// TestValidateHost_AllowListedNameIsNotResolved records that a host
// name listed in allowed_hosts is still accepted without a lookup; the
// connect-time guard applies the floor to whatever it resolves to.
func TestValidateHost_AllowListedNameIsNotResolved(t *testing.T) {
	v := NewHostValidator(false, []string{"db.example.com"}, nil)
	v.lookupIP = func(string) ([]net.IP, error) {
		t.Fatal("an allow-listed name must not be resolved")
		return nil, nil
	}
	if err := v.ValidateHost("db.example.com"); err != nil {
		t.Fatalf("ValidateHost = %v, want nil", err)
	}
}

// TestCheckAlwaysBlockedHost covers the check the notification channel
// save path makes.
func TestCheckAlwaysBlockedHost(t *testing.T) {
	lookup := func(_ context.Context, host string) ([]string, error) {
		switch host {
		case "metadata.example.com":
			return []string{"192.0.2.10", "fe80::1%eth0"}, nil
		case "public.example.com":
			return []string{"192.0.2.10", "not-an-address"}, nil
		}
		return nil, errors.New("no such host")
	}

	cases := []struct {
		host    string
		blocked bool
	}{
		{"", false},
		{"192.0.2.1", false},
		{"10.0.0.1", false},
		{"::1", false},
		{"fe80::1%eth0", true},
		{"metadata.example.com", true},
		{"metadata.example.com.", true},
		{"public.example.com", false},
		{"missing.example.com", false},
	}
	for _, host := range alwaysBlockedLiterals {
		cases = append(cases, struct {
			host    string
			blocked bool
		}{host, true})
	}
	for _, tc := range cases {
		t.Run(tc.host, func(t *testing.T) {
			err := CheckAlwaysBlockedHost(context.Background(), tc.host, lookup)
			if got := errors.Is(err, ErrAlwaysBlockedHost); got != tc.blocked {
				t.Fatalf("CheckAlwaysBlockedHost(%q) = %v, want blocked=%v", tc.host, err, tc.blocked)
			}
		})
	}

	// A nil lookup is the system resolver; localhost is loopback.
	if err := CheckAlwaysBlockedHost(context.Background(), "localhost", nil); err != nil {
		t.Errorf("CheckAlwaysBlockedHost(localhost) = %v, want nil", err)
	}
}

// TestValidateConnectionTarget_AlwaysBlockedMessage checks the 400 a
// connection save gets: the always-blocked reason for such an address,
// and the plain message for every other refusal.
func TestValidateConnectionTarget_AlwaysBlockedMessage(t *testing.T) {
	h := &ConnectionHandler{hostValidator: NewHostValidator(true, nil, nil)}
	str := func(s string) *string { return &s }

	cases := []struct {
		name           string
		host, hostaddr *string
		want           string
	}{
		{"host", str("169.254.169.254"), nil,
			"Invalid host: " + ErrAlwaysBlockedHost.Error()},
		{"hostaddr", nil, str("[fe80::1]"),
			"Invalid hostaddr: " + ErrAlwaysBlockedHost.Error()},
		{"other host refusal", str("db,example.com"), nil, "Invalid host"},
		{"other hostaddr refusal", nil, str("db.example.com"), "Invalid hostaddr"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			if h.validateConnectionTarget(rec, tc.host, tc.hostaddr) {
				t.Fatal("validateConnectionTarget accepted the target")
			}
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			var resp ErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.Error != tc.want {
				t.Errorf("Error = %q, want %q", resp.Error, tc.want)
			}
		})
	}
}

// alwaysBlockedChannelCases are channel bodies, for create and update
// alike, aimed at an always-blocked target, with the expected error.
var alwaysBlockedChannelCases = []struct {
	name string
	body string
	want string
}{
	{
		name: "smtp_host literal",
		body: `{"channel_type":"email","name":"e","smtp_host":"169.254.169.254","from_address":"alerts@example.com"}`,
		want: "Invalid smtp_host: " + ErrAlwaysBlockedHost.Error(),
	},
	{
		name: "smtp_host bracketed IPv6",
		body: `{"channel_type":"email","name":"e","smtp_host":"[fe80::1]","from_address":"alerts@example.com"}`,
		want: "Invalid smtp_host: " + ErrAlwaysBlockedHost.Error(),
	},
	{
		name: "webhook_url",
		body: `{"channel_type":"slack","name":"s","webhook_url":"http://[fd00:ec2::254]/hook"}`,
		want: "Invalid webhook_url: " + ErrAlwaysBlockedHost.Error(),
	},
	{
		name: "endpoint_url resolving to metadata",
		body: `{"channel_type":"webhook","name":"w","endpoint_url":"https://metadata.example.com/x"}`,
		want: "Invalid endpoint_url: " + ErrAlwaysBlockedHost.Error(),
	},
}

// stubChannelLookup resolves metadata.example.com to a link-local
// address and refuses every other name.
func stubChannelLookup(_ context.Context, host string) ([]string, error) {
	if host == "metadata.example.com" {
		return []string{"169.254.169.254"}, nil
	}
	return nil, errors.New("no such host")
}

func badRequestError(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	var resp ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp.Error
}

// TestCreateChannel_RefusesAlwaysBlockedTargets checks that a channel
// aimed at an always-blocked address is refused at create time, even
// with allow_internal_networks on, before anything is stored (the
// handler has no datastore). The response must not echo the URL.
func TestCreateChannel_RefusesAlwaysBlockedTargets(t *testing.T) {
	authStore, cleanupStore := newAuthStoreForChannelTests(t)
	defer cleanupStore()
	userID := setupUserWithPermission(t, authStore, "ch_floor",
		auth.PermManageNotificationChannels)
	grantAllConnections(t, authStore, userID)
	checker := auth.NewRBACChecker(authStore)
	handler := NewNotificationChannelHandlerWithSecurity(nil, authStore, checker, true, nil, nil)
	handler.lookupHost = stubChannelLookup

	cases := append(alwaysBlockedChannelCases[:len(alwaysBlockedChannelCases):len(alwaysBlockedChannelCases)],
		struct {
			name string
			body string
			want string
		}{
			// An unparseable URL is not this check's business and is
			// skipped; the refused smtp_host is still reported.
			name: "unparseable webhook_url is skipped",
			body: `{"channel_type":"slack","name":"s","webhook_url":"http://[::1","smtp_host":"0.0.0.0"}`,
			want: "Invalid smtp_host: " + ErrAlwaysBlockedHost.Error(),
		})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/notification-channels",
				bytes.NewReader([]byte(tc.body)))
			req.Header.Set("Content-Type", "application/json")
			req = withUser(req, userID)
			req = withUsername(req, "ch_floor")
			rec := httptest.NewRecorder()
			handler.handleChannels(rec, req)

			if got := badRequestError(t, rec); got != tc.want {
				t.Errorf("Error = %q, want %q", got, tc.want)
			}
			if strings.Contains(rec.Body.String(), "/hook") || strings.Contains(rec.Body.String(), "/x") {
				t.Errorf("response echoes the URL: %s", rec.Body.String())
			}
		})
	}
}

// TestUpdateChannel_RefusesAlwaysBlockedTargets checks the update path,
// and that an update leaving the targets alone is not re-checked.
func TestUpdateChannel_RefusesAlwaysBlockedTargets(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()
	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()
	handler.lookupHost = stubChannelLookup

	id := createTestChannel(t, ds, "floor-update", nil, nil, nil, nil)

	for _, tc := range alwaysBlockedChannelCases {
		t.Run(tc.name, func(t *testing.T) {
			rec := putChannel(t, handler, userID, id, tc.body)
			if got := badRequestError(t, rec); got != tc.want {
				t.Errorf("Error = %q, want %q", got, tc.want)
			}
		})
	}

	rec := putChannel(t, handler, userID, id, `{"description":"renamed","smtp_host":"smtp.example.com"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}
