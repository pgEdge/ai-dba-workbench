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

func TestEmbeddedIPv4(t *testing.T) {
	tests := []struct {
		ip   string
		want string // "" for no embedded address
	}{
		{"::c000:201", "192.0.2.1"},
		{"::ffff:0:192.0.2.1", "192.0.2.1"},
		{"64:ff9b::c000:201", "192.0.2.1"},
		// The RFC 8215 local-use prefix is not decoded here, in any
		// layout; localUseNAT64Readings reads it instead.
		{"64:ff9b:1:c000:2:100::", ""},
		{"64:ff9b:1:a9fe:a9:fe00::", ""},
		{"64:ff9b:1::c633:6407", ""},
		{"64:ff9b:1:fffe::a9fe:a9fe", ""},
		{"2002:c000:201::1", "192.0.2.1"},
		{"192.0.2.1", ""},
		{"::ffff:192.0.2.1", ""}, // IPv4-mapped: To4 unwraps it
		{"::1:ffff:0:c000:201", ""},
		{"::ffff:1:c000:201", ""},
		{"64:ff9b:2:c000:2:100::", ""},
		{"2001:db8::1", ""},
	}
	for _, tt := range tests {
		ip := net.ParseIP(tt.ip)
		if ip == nil {
			t.Fatalf("failed to parse %s", tt.ip)
		}
		got := embeddedIPv4(ip)
		if tt.want == "" {
			if got != nil {
				t.Errorf("embeddedIPv4(%s) = %s, want nil", tt.ip, got)
			}
			continue
		}
		if !got.Equal(net.ParseIP(tt.want)) {
			t.Errorf("embeddedIPv4(%s) = %v, want %s", tt.ip, got, tt.want)
		}
	}
}

// TestGuardedDialContext_TranslatedMetadata checks that the floor holds
// for the well-known NAT64 and SIIT forms of the metadata address under
// a policy that permits everything, as allow_internal_networks does.
func TestGuardedDialContext_TranslatedMetadata(t *testing.T) {
	permitAll := func(string, net.IP) error { return nil }
	dial := func(context.Context, string, string) (net.Conn, error) {
		t.Error("dial called for an always-blocked address")
		return nil, errors.New("unexpected dial")
	}
	for _, addr := range []string{
		"[64:ff9b::a9fe:a9fe]:80",
		"[::ffff:0:169.254.169.254]:80",
	} {
		_, err := GuardedDialContext(permitAll, nil, dial)(context.Background(), "tcp", addr)
		if !errors.Is(err, ErrNoPermittedAddress) {
			t.Errorf("dial %s: err = %v, want ErrNoPermittedAddress", addr, err)
		}
	}
}

// localUseAddr embeds v4 in 64:ff9b:1::/48 at the RFC 6052 offsets of
// the given prefix length (48, 56, 64 or 96), with a zero suffix.
func localUseAddr(t *testing.T, prefixLen int, v4 string) net.IP {
	t.Helper()
	offsets := map[int][]int{
		48: {6, 7, 9, 10},
		56: {7, 9, 10, 11},
		64: {9, 10, 11, 12},
		96: {12, 13, 14, 15},
	}[prefixLen]
	b := net.ParseIP(v4).To4()
	if offsets == nil || b == nil {
		t.Fatalf("bad layout /%d or address %s", prefixLen, v4)
	}
	ip := make(net.IP, net.IPv6len)
	copy(ip, net.ParseIP("64:ff9b:1::"))
	for i, off := range offsets {
		ip[off] = b[i]
	}
	return ip
}

// TestLocalUseNAT64 pins how the RFC 8215 local-use prefix,
// 64:ff9b:1::/48, is treated: the always-blocked floor refuses an
// address whose reading at any RFC 6052 offset is a metadata address,
// and leaves every other alone, and the internal-network policy refuses
// every address in it.
func TestLocalUseNAT64(t *testing.T) {
	layouts := []int{48, 56, 64, 96}

	t.Run("metadata under every layout is refused", func(t *testing.T) {
		for _, layout := range layouts {
			for _, v4 := range []string{"169.254.169.254", "100.100.100.200", "169.254.0.1"} {
				ip := localUseAddr(t, layout, v4)
				if !IsAlwaysBlockedIP(ip) {
					t.Errorf("IsAlwaysBlockedIP(%s) (%s under /%d) = false, want true", ip, v4, layout)
				}
			}
		}
		for _, addr := range []string{
			"64:ff9b:1::a9fe:a9fe",       // 169.254.169.254 under 64:ff9b:1::/96
			"64:ff9b:1:fffe::a9fe:a9fe",  // 169.254.169.254 under 64:ff9b:1:fffe::/96
			"64:ff9b:1:a9fe:a9:fe00::",   // 169.254.169.254 in the /48 layout
			"64:ff9b:1:a9:fe:a9fe::",     // 169.254.169.254 in the /56 layout
			"64:ff9b:1:0:a9:fea9:fe00:0", // 169.254.169.254 in the /64 layout
			"64:ff9b:1::6464:64c8",       // 100.100.100.200 under 64:ff9b:1::/96
			"64:ff9b:1:6464:64:c800::",   // 100.100.100.200 in the /48 layout
			"64:ff9b:1:64:64:64c8::",     // 100.100.100.200 in the /56 layout
			"64:ff9b:1:0:64:6464:c800:0", // 100.100.100.200 in the /64 layout
			// A nonzero u-octet does not hide the /48 reading.
			"64:ff9b:1:a9fe:ffa9:fe00::",
		} {
			ip := net.ParseIP(addr)
			if !IsAlwaysBlockedIP(ip) {
				t.Errorf("IsAlwaysBlockedIP(%s) = false, want true", addr)
			}
			if err := CheckAlwaysBlocked("h", ip); err == nil {
				t.Errorf("CheckAlwaysBlocked(%s) = nil, want a refusal", addr)
			}
		}
	})

	t.Run("public and private under every layout pass the floor", func(t *testing.T) {
		for _, layout := range layouts {
			for _, v4 := range []string{"198.51.100.7", "8.8.8.8", "10.0.0.5", "192.168.1.10", "0.0.0.0"} {
				ip := localUseAddr(t, layout, v4)
				if IsAlwaysBlockedIP(ip) {
					t.Errorf("IsAlwaysBlockedIP(%s) (%s under /%d) = true, want false", ip, v4, layout)
				}
				if !IsPrivateIP(ip) {
					t.Errorf("IsPrivateIP(%s) = false, want true", ip)
				}
			}
		}
		for _, addr := range []string{
			"64:ff9b:1::c633:6407",       // 198.51.100.7 under 64:ff9b:1::/96
			"64:ff9b:1::a00:5",           // 10.0.0.5 under 64:ff9b:1::/96
			"64:ff9b:1::",                // 0.0.0.0 under 64:ff9b:1::/96
			"64:ff9b:1:fffe::c633:6407",  // 198.51.100.7 under 64:ff9b:1:fffe::/96
			"64:ff9b:1:ffff:ffff:ffff::", // top of the /48
		} {
			ip := net.ParseIP(addr)
			if IsAlwaysBlockedIP(ip) {
				t.Errorf("IsAlwaysBlockedIP(%s) = true, want false", addr)
			}
			if !IsPrivateIP(ip) {
				t.Errorf("IsPrivateIP(%s) = false, want true", addr)
			}
			if err := CheckNotInternal("h", ip); err == nil {
				t.Errorf("CheckNotInternal(%s) = nil, want a refusal", addr)
			}
		}
	})

	// The false positives the code comment records: a destination of the
	// form x.169.254.y under the /48 or /56 layout, or x.y.169.254 under
	// the /48 layout, is read as link-local at another offset.
	t.Run("recorded false positives", func(t *testing.T) {
		for _, tc := range []struct {
			layout  int
			v4      string
			blocked bool
		}{
			{48, "198.169.254.7", true},
			{48, "198.51.169.254", true},
			{56, "198.169.254.7", true},
			{56, "198.51.169.254", false},
			{64, "198.169.254.7", false},
			{96, "198.169.254.7", false},
			{96, "198.51.169.254", false},
		} {
			ip := localUseAddr(t, tc.layout, tc.v4)
			if got := IsAlwaysBlockedIP(ip); got != tc.blocked {
				t.Errorf("IsAlwaysBlockedIP(%s) (%s under /%d) = %v, want %v",
					ip, tc.v4, tc.layout, got, tc.blocked)
			}
		}
	})

	t.Run("the guarded dial refuses metadata under a permissive policy", func(t *testing.T) {
		permitAll := func(string, net.IP) error { return nil }
		dial := func(context.Context, string, string) (net.Conn, error) {
			t.Error("dial called for an always-blocked address")
			return nil, errors.New("unexpected dial")
		}
		_, err := GuardedDialContext(permitAll, nil, dial)(context.Background(), "tcp", "[64:ff9b:1:fffe::a9fe:a9fe]:80")
		if !errors.Is(err, ErrNoPermittedAddress) {
			t.Errorf("err = %v, want ErrNoPermittedAddress", err)
		}
	})

	// Just outside the /48 on either side is neither.
	for _, addr := range []string{"64:ff9b:0:ffff::1", "64:ff9b:2::1", "64:ff9b:2:a9fe:a9:fe00::"} {
		ip := net.ParseIP(addr)
		if IsAlwaysBlockedIP(ip) || IsPrivateIP(ip) {
			t.Errorf("%s: blocked=%v private=%v, want neither",
				addr, IsAlwaysBlockedIP(ip), IsPrivateIP(ip))
		}
	}
	if localUseNAT64Readings(net.ParseIP("192.0.2.1")) != nil {
		t.Error("localUseNAT64Readings of an IPv4 address should be nil")
	}
	if localUseNAT64Readings(net.IP{1, 2, 3}) != nil {
		t.Error("localUseNAT64Readings of a malformed IP should be nil")
	}
}

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
		// SIIT forms of the metadata, Alibaba metadata and unspecified
		// addresses.
		{"::ffff:0:169.254.169.254", true},
		{"::ffff:0:100.100.100.200", true},
		{"::ffff:0:0.0.0.0", true},
		{"fe80::1", true},
		{"ff02::1", true},
		{"224.0.0.1", true},
		{"fd00:ec2::254", true},
		{"fd00:ec2::23", true},  // AWS EKS Pod Identity Agent
		{"fd20:ce::254", true},  // GCP metadata, IPv6-only instances
		{"fd00:ec2::22", false}, // adjacent to the metadata addresses
		{"fd20:ce::253", false},
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
		{"64:ff9b:1::c633:6407", false}, // local-use NAT64: left to the internal policy
		{"::ffff:0:192.0.2.1", false},
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

// TestGuardedLookup_AlwaysBlockedFloor checks that a policy permitting
// everything, as an allowed_hosts name match does, still cannot reach an
// always-blocked address, by name or as an IP literal.
func TestGuardedLookup_AlwaysBlockedFloor(t *testing.T) {
	ctx := context.Background()
	permitAll := func(string, net.IP) error { return nil }

	got, err := GuardedLookup(permitAll,
		fixedLookup("169.254.169.254", "0.0.0.0", "fd00:ec2::254", "203.0.113.7"))(ctx, "allowed.example.com")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if want := []string{"203.0.113.7"}; !reflect.DeepEqual(got, want) {
		t.Errorf("addrs = %v, want %v", got, want)
	}

	if _, err := GuardedLookup(permitAll, fixedLookup("169.254.169.254"))(ctx, "allowed.example.com"); !errors.Is(err, ErrNoPermittedAddress) {
		t.Errorf("metadata only: err = %v, want ErrNoPermittedAddress", err)
	}

	dialled := false
	dial := func(context.Context, string, string) (net.Conn, error) {
		dialled = true
		return nil, errors.New("dialled")
	}
	for _, addr := range []string{"169.254.169.254:80", "[fe80::1]:80", "0.0.0.0:80"} {
		_, err := GuardedDialContext(permitAll, nil, dial)(ctx, "tcp", addr)
		if !errors.Is(err, ErrNoPermittedAddress) {
			t.Errorf("%s: err = %v, want ErrNoPermittedAddress", addr, err)
		}
	}
	if dialled {
		t.Error("an always-blocked IP literal was dialled")
	}
}

func TestCheckNotInternal(t *testing.T) {
	tests := []struct {
		ip      string
		blocked bool
	}{
		{"169.254.169.254", true},
		{"0.0.0.0", true},
		{"127.0.0.1", true},
		{"::1", true},
		{"10.0.0.5", true},
		{"192.168.1.10", true},
		{"fd00::1", true},
		{"100.64.0.1", true},
		{"::ffff:10.0.0.1", true},
		{"8.8.8.8", false},
		{"2606:4700:4700::1111", false},
	}
	for _, tt := range tests {
		err := CheckNotInternal("h", net.ParseIP(tt.ip))
		if (err != nil) != tt.blocked {
			t.Errorf("CheckNotInternal(%s) = %v, want blocked=%v", tt.ip, err, tt.blocked)
		}
	}
}

// recordingDial returns a dial function that records each address it
// is asked for and fails every one with errDialRecorded, and a pointer
// to the record.
func recordingDial() (DialContextFunc, *[]string) {
	var seen []string
	return func(_ context.Context, _, addr string) (net.Conn, error) {
		seen = append(seen, addr)
		return nil, errDialRecorded
	}, &seen
}

var errDialRecorded = errors.New("dial recorded")

func TestGuardedDialContext(t *testing.T) {
	ctx := context.Background()

	t.Run("a name resolving to a blocked address is never dialled", func(t *testing.T) {
		dial, seen := recordingDial()
		guarded := GuardedDialContext(CheckNotInternal, fixedLookup("10.0.0.1", "169.254.169.254"), dial)
		_, err := guarded(ctx, "tcp", "hooks.example.com:443")
		if !errors.Is(err, ErrNoPermittedAddress) {
			t.Fatalf("err = %v, want ErrNoPermittedAddress", err)
		}
		if len(*seen) != 0 {
			t.Errorf("dialled %v", *seen)
		}
		if strings.Contains(err.Error(), "10.0.0.1") || strings.Contains(err.Error(), "example") {
			t.Errorf("error %q names the host or an address", err)
		}
	})

	t.Run("only permitted addresses are dialled, in order, on addr's port", func(t *testing.T) {
		dial, seen := recordingDial()
		guarded := GuardedDialContext(CheckNotInternal,
			fixedLookup("127.0.0.1", "203.0.113.7", "[2001:db8::1]:9", "8.8.8.8"), dial)
		_, err := guarded(ctx, "tcp", "hooks.example.com:443")
		if !errors.Is(err, errDialRecorded) {
			t.Fatalf("err = %v, want the last dial error", err)
		}
		// 203.0.113.7 is TEST-NET-3, which IsPrivateIP refuses; the
		// resolver's own port on the IPv6 entry is replaced by addr's.
		if want := []string{"[2001:db8::1]:443", "8.8.8.8:443"}; !reflect.DeepEqual(*seen, want) {
			t.Errorf("dialled %v, want %v", *seen, want)
		}
	})

	t.Run("every permitted address is tried until one connects", func(t *testing.T) {
		var seen []string
		client, server := net.Pipe()
		defer func() { _ = client.Close() }()
		defer func() { _ = server.Close() }()
		dial := func(_ context.Context, _, addr string) (net.Conn, error) {
			seen = append(seen, addr)
			if addr == "8.8.8.8:25" {
				return nil, errDialRecorded
			}
			return client, nil
		}
		guarded := GuardedDialContext(CheckAlwaysBlocked,
			fixedLookup("169.254.169.254", "8.8.8.8", "1.1.1.1:99", "9.9.9.9"), dial)
		conn, err := guarded(ctx, "tcp", "smtp.example.com:25")
		if err != nil || conn != client {
			t.Fatalf("conn, err = %v, %v", conn, err)
		}
		if want := []string{"8.8.8.8:25", "1.1.1.1:25"}; !reflect.DeepEqual(seen, want) {
			t.Errorf("dialled %v, want %v", seen, want)
		}
	})

	t.Run("an IP literal is judged without resolving", func(t *testing.T) {
		dial, seen := recordingDial()
		resolve := func(context.Context, string) ([]string, error) {
			t.Error("resolve called for an IP literal")
			return nil, nil
		}
		guarded := GuardedDialContext(CheckNotInternal, resolve, dial)
		if _, err := guarded(ctx, "tcp", "[::1]:80"); !errors.Is(err, ErrNoPermittedAddress) {
			t.Errorf("loopback literal: err = %v", err)
		}
		if _, err := guarded(ctx, "tcp", "8.8.8.8:80"); !errors.Is(err, errDialRecorded) {
			t.Errorf("public literal: err = %v", err)
		}
		if want := []string{"8.8.8.8:80"}; !reflect.DeepEqual(*seen, want) {
			t.Errorf("dialled %v, want %v", *seen, want)
		}
	})

	t.Run("a resolver error and a malformed address pass through", func(t *testing.T) {
		boom := errors.New("lookup failed")
		guarded := GuardedDialContext(nil, func(context.Context, string) ([]string, error) {
			return nil, boom
		}, nil)
		if _, err := guarded(ctx, "tcp", "hooks.example.com:443"); !errors.Is(err, boom) {
			t.Errorf("err = %v, want the resolver's error", err)
		}
		if _, err := guarded(ctx, "tcp", "no-port"); err == nil {
			t.Error("malformed addr: err = nil")
		}
	})

	t.Run("a cancelled context stops the attempts", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		dial, seen := recordingDial()
		guarded := GuardedDialContext(nil, fixedLookup("8.8.8.8", "1.1.1.1"), dial)
		if _, err := guarded(cctx, "tcp", "hooks.example.com:443"); !errors.Is(err, errDialRecorded) {
			t.Errorf("err = %v", err)
		}
		if len(*seen) != 1 {
			t.Errorf("dialled %v, want one attempt", *seen)
		}
	})

	t.Run("the defaults dial a permitted literal for real", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		defer func() { _ = ln.Close() }()
		conn, err := GuardedDialContext(nil, nil, nil)(ctx, "tcp", ln.Addr().String())
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		_ = conn.Close()
	})
}

func TestMustParseCIDRsPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("mustParseCIDRs did not panic on a bad CIDR")
		}
	}()
	mustParseCIDRs("not-a-cidr")
}
