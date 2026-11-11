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
	"net"
	"testing"
)

// TestIsPrivateIP_UnspecifiedAndEmbedded covers the addresses the
// original list missed: the IPv6 unspecified address, which a dialler
// turns into localhost, and IPv6 forms that embed an IPv4 address.
func TestIsPrivateIP_UnspecifiedAndEmbedded(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		private bool
	}{
		{"IPv6 unspecified", "::", true},
		{"IPv4 unspecified", "0.0.0.0", true},
		{"IPv4-mapped unspecified", "::ffff:0.0.0.0", true},
		{"IPv4-mapped loopback", "::ffff:127.0.0.1", true},
		{"IPv4-compatible", "::a9fe:a9fe", true},
		{"NAT64 metadata", "64:ff9b::a9fe:a9fe", true},
		{"NAT64 public", "64:ff9b::808:808", true},
		{"local-use NAT64", "64:ff9b:1::1", true},
		{"6to4", "2002:a9fe:a9fe::1", true},
		{"IPv6 site-local", "fec0::1", true},
		{"IPv6 link-local multicast", "ff02::2", true},
		{"IPv6 global multicast", "ff0e::1", true},
		{"IPv6 public", "2001:4860:4860::8844", false},
		{"IPv4-mapped public", "::ffff:8.8.8.8", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP %s", tt.ip)
			}
			if got := IsPrivateIP(ip); got != tt.private {
				t.Errorf("IsPrivateIP(%s) = %v, want %v", tt.ip, got, tt.private)
			}
		})
	}
}

// TestValidateURLHost_Unspecified checks that a URL naming an
// unspecified or NAT64 address, which reaches localhost or the cloud
// metadata service, is refused.
func TestValidateURLHost_Unspecified(t *testing.T) {
	for _, u := range []string{
		"http://[::]/hook",
		"http://[::]:8080/hook",
		"http://[::ffff:0.0.0.0]/hook",
		"http://[64:ff9b::a9fe:a9fe]/latest/meta-data",
		"http://0.0.0.0/hook",
	} {
		if err := ValidateURLHost(u); err == nil {
			t.Errorf("ValidateURLHost(%q) = nil, want an error", u)
		}
	}
}
