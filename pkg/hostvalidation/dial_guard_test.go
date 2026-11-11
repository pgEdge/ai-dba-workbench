/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package hostvalidation

import (
	"context"
	"errors"
	"net"
	"reflect"
	"strings"
	"testing"
)

func TestIsAlwaysBlockedIP(t *testing.T) {
	tests := []struct {
		ip      string
		blocked bool
	}{
		{"0.0.0.0", true},
		{"0.1.2.3", true},
		{"::", true},
		{"::ffff:0.0.0.0", true},
		{"169.254.169.254", true},
		{"169.254.1.1", true},
		{"::ffff:169.254.169.254", true},
		{"::a9fe:a9fe", true},
		{"64:ff9b::a9fe:a9fe", true},
		{"2002:a9fe:a9fe::1", true},
		{"fe80::1", true},
		{"ff02::1", true},
		{"224.0.0.1", true},
		{"fd00:ec2::254", true},
		{"100.100.100.200", true},
		// Ranges a monitored connection may legitimately use.
		{"127.0.0.1", false},
		{"::1", false},
		{"10.0.0.5", false},
		{"192.168.1.10", false},
		{"fd00::1", false},
		{"100.64.0.1", false},
		{"8.8.8.8", false},
		{"64:ff9b::808:808", false},
		{"2002:0808:0808::1", false},
		{"2001:db8::1", false},
	}
	for _, tt := range tests {
		ip := net.ParseIP(tt.ip)
		if ip == nil {
			t.Fatalf("failed to parse %s", tt.ip)
		}
		if got := IsAlwaysBlockedIP(ip); got != tt.blocked {
			t.Errorf("IsAlwaysBlockedIP(%s) = %v, want %v", tt.ip, got, tt.blocked)
		}
		if err := CheckAlwaysBlocked("h", ip); (err != nil) != tt.blocked {
			t.Errorf("CheckAlwaysBlocked(%s) = %v, want blocked=%v", tt.ip, err, tt.blocked)
		}
	}
	if !IsAlwaysBlockedIP(nil) {
		t.Error("IsAlwaysBlockedIP(nil) = false, want true")
	}
	if embeddedIPv4(net.IP{1, 2, 3}) != nil {
		t.Error("embeddedIPv4 of a malformed IP should be nil")
	}
}

// fixedLookup returns a resolver that answers every host with addrs.
func fixedLookup(addrs ...string) LookupFunc {
	return func(context.Context, string) ([]string, error) { return addrs, nil }
}

func TestGuardedLookup(t *testing.T) {
	ctx := context.Background()

	// The rebinding case: the host passed its save-time check, but now
	// resolves to the metadata service alongside a public address.
	got, err := GuardedLookup(CheckAlwaysBlocked,
		fixedLookup("169.254.169.254", "203.0.113.7", "[fe80::1]:5432", "198.51.100.1:6432"))(ctx, "db.example.com")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if want := []string{"203.0.113.7", "198.51.100.1:6432"}; !reflect.DeepEqual(got, want) {
		t.Errorf("addrs = %v, want %v", got, want)
	}

	// Every address refused, or unparseable: a fixed error naming
	// neither the host nor an address.
	_, err = GuardedLookup(nil, fixedLookup("169.254.169.254", "not-an-ip", "fe80::1%eth0"))(ctx, "db.example.com")
	if !errors.Is(err, ErrNoPermittedAddress) {
		t.Fatalf("err = %v, want ErrNoPermittedAddress", err)
	}
	if strings.Contains(err.Error(), "169.254") || strings.Contains(err.Error(), "example") {
		t.Errorf("error %q names the host or an address", err)
	}

	// A resolver error passes through unchanged.
	boom := errors.New("lookup failed")
	_, err = GuardedLookup(nil, func(context.Context, string) ([]string, error) {
		return nil, boom
	})(ctx, "db.example.com")
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want the resolver's error", err)
	}

	// The policy sees the host name pgx asked for.
	var seen string
	policy := func(host string, _ net.IP) error {
		seen = host
		return nil
	}
	if _, err := GuardedLookup(policy, fixedLookup("192.0.2.1"))(ctx, "named.example.com"); err != nil {
		t.Fatalf("err = %v", err)
	}
	if seen != "named.example.com" {
		t.Errorf("policy saw host %q", seen)
	}

	// With no resolver given, the system one is used; an IP literal
	// resolves to itself without touching DNS.
	got, err = GuardedLookup(nil, nil)(ctx, "127.0.0.1")
	if err != nil || len(got) != 1 || got[0] != "127.0.0.1" {
		t.Errorf("default resolver: got %v, %v", got, err)
	}
	if _, err := GuardedLookup(nil, nil)(ctx, "169.254.169.254"); !errors.Is(err, ErrNoPermittedAddress) {
		t.Errorf("default resolver, metadata literal: err = %v", err)
	}
}

func TestMustParseCIDRsPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("mustParseCIDRs did not panic on a bad CIDR")
		}
	}()
	mustParseCIDRs("not-a-cidr")
}
