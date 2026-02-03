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
	"strings"
	"testing"
)

func TestNewHostValidator(t *testing.T) {
	tests := []struct {
		name          string
		allowInternal bool
		allowedHosts  []string
		blockedHosts  []string
	}{
		{
			name:          "default validator",
			allowInternal: false,
			allowedHosts:  nil,
			blockedHosts:  nil,
		},
		{
			name:          "allow internal networks",
			allowInternal: true,
			allowedHosts:  nil,
			blockedHosts:  nil,
		},
		{
			name:          "with allowed hosts",
			allowInternal: false,
			allowedHosts:  []string{"db.example.com", "192.168.1.0/24"},
			blockedHosts:  nil,
		},
		{
			name:          "with blocked hosts",
			allowInternal: false,
			allowedHosts:  nil,
			blockedHosts:  []string{"bad.example.com", "10.0.0.0/8"},
		},
		{
			name:          "with both lists",
			allowInternal: false,
			allowedHosts:  []string{"good.example.com"},
			blockedHosts:  []string{"bad.example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewHostValidator(tt.allowInternal, tt.allowedHosts, tt.blockedHosts)
			if v == nil {
				t.Fatal("NewHostValidator returned nil")
			}
			if v.AllowInternalNetworks != tt.allowInternal {
				t.Errorf("AllowInternalNetworks = %v, want %v",
					v.AllowInternalNetworks, tt.allowInternal)
			}
		})
	}
}

func TestDefaultHostValidator(t *testing.T) {
	v := DefaultHostValidator()
	if v == nil {
		t.Fatal("DefaultHostValidator returned nil")
	}
	if v.AllowInternalNetworks {
		t.Error("Default validator should not allow internal networks")
	}
	if len(v.AllowedHosts) != 0 {
		t.Error("Default validator should have empty allowed hosts")
	}
	if len(v.BlockedHosts) != 0 {
		t.Error("Default validator should have empty blocked hosts")
	}
}

func TestHostValidator_ValidateHost(t *testing.T) {
	tests := []struct {
		name          string
		allowInternal bool
		allowedHosts  []string
		blockedHosts  []string
		host          string
		wantErr       bool
		errContains   string
	}{
		// Empty host
		{
			name:    "empty host",
			host:    "",
			wantErr: true,
		},

		// Public IP addresses
		{
			name:    "public IPv4",
			host:    "8.8.8.8",
			wantErr: false,
		},

		// Internal IP addresses (blocked by default)
		{
			name:        "RFC1918 10.x blocked",
			host:        "10.0.0.1",
			wantErr:     true,
			errContains: "internal IP",
		},
		{
			name:        "RFC1918 172.16.x blocked",
			host:        "172.16.0.1",
			wantErr:     true,
			errContains: "internal IP",
		},
		{
			name:        "RFC1918 192.168.x blocked",
			host:        "192.168.1.1",
			wantErr:     true,
			errContains: "internal IP",
		},
		{
			name:        "loopback IPv4 blocked",
			host:        "127.0.0.1",
			wantErr:     true,
			errContains: "internal IP",
		},
		{
			name:        "loopback IPv6 blocked",
			host:        "::1",
			wantErr:     true,
			errContains: "internal IP",
		},
		{
			name:        "link-local blocked",
			host:        "169.254.1.1",
			wantErr:     true,
			errContains: "internal IP",
		},

		// Internal IP addresses allowed when enabled
		{
			name:          "RFC1918 allowed when internal enabled",
			allowInternal: true,
			host:          "192.168.1.1",
			wantErr:       false,
		},
		{
			name:          "loopback allowed when internal enabled",
			allowInternal: true,
			host:          "127.0.0.1",
			wantErr:       false,
		},

		// Explicit allowlist
		{
			name:         "IP in allowlist",
			allowedHosts: []string{"10.0.0.1"},
			host:         "10.0.0.1",
			wantErr:      false,
		},
		{
			name:         "IP in allowed CIDR",
			allowedHosts: []string{"10.0.0.0/24"},
			host:         "10.0.0.50",
			wantErr:      false,
		},
		{
			name:         "hostname in allowlist",
			allowedHosts: []string{"db.internal.example.com"},
			host:         "db.internal.example.com",
			wantErr:      false,
		},

		// Explicit blocklist
		{
			name:         "hostname in blocklist",
			blockedHosts: []string{"blocked.example.com"},
			host:         "blocked.example.com",
			wantErr:      true,
			errContains:  "blocklist",
		},
		{
			name:         "IP in blocked CIDR",
			blockedHosts: []string{"8.8.8.0/24"},
			host:         "8.8.8.8",
			wantErr:      true,
			errContains:  "blocked range",
		},

		// Hostname normalization
		{
			name:    "trailing dot removed",
			host:    "example.com.",
			wantErr: false,
		},
		{
			name:         "case insensitive allowlist match",
			allowedHosts: []string{"DB.EXAMPLE.COM"},
			host:         "db.example.com",
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewHostValidator(tt.allowInternal, tt.allowedHosts, tt.blockedHosts)
			err := v.ValidateHost(tt.host)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateHost(%q) = nil, want error", tt.host)
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ValidateHost(%q) error = %q, want error containing %q",
						tt.host, err.Error(), tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateHost(%q) = %v, want nil", tt.host, err)
				}
			}
		})
	}
}

func TestHostValidator_ValidatePort(t *testing.T) {
	v := DefaultHostValidator()

	tests := []struct {
		name        string
		port        int
		wantErr     bool
		errContains string
	}{
		// Valid database ports
		{
			name:    "PostgreSQL default port",
			port:    5432,
			wantErr: false,
		},
		{
			name:    "MySQL default port",
			port:    3306,
			wantErr: false,
		},
		{
			name:    "custom high port",
			port:    15432,
			wantErr: false,
		},
		{
			name:    "minimum valid port",
			port:    1,
			wantErr: false,
		},
		{
			name:    "maximum valid port",
			port:    65535,
			wantErr: false,
		},

		// Invalid port numbers
		{
			name:        "zero port",
			port:        0,
			wantErr:     true,
			errContains: "between 1 and 65535",
		},
		{
			name:        "negative port",
			port:        -1,
			wantErr:     true,
			errContains: "between 1 and 65535",
		},
		{
			name:        "port too high",
			port:        65536,
			wantErr:     true,
			errContains: "between 1 and 65535",
		},

		// Blocked ports (common non-database services)
		{
			name:        "SSH port blocked",
			port:        22,
			wantErr:     true,
			errContains: "SSH",
		},
		{
			name:        "SMTP port blocked",
			port:        25,
			wantErr:     true,
			errContains: "SMTP",
		},
		{
			name:        "HTTP port blocked",
			port:        80,
			wantErr:     true,
			errContains: "HTTP",
		},
		{
			name:        "HTTPS port blocked",
			port:        443,
			wantErr:     true,
			errContains: "HTTPS",
		},
		{
			name:        "Redis port blocked",
			port:        6379,
			wantErr:     true,
			errContains: "Redis",
		},
		{
			name:        "SMB port blocked",
			port:        445,
			wantErr:     true,
			errContains: "SMB",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidatePort(tt.port)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidatePort(%d) = nil, want error", tt.port)
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ValidatePort(%d) error = %q, want error containing %q",
						tt.port, err.Error(), tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("ValidatePort(%d) = %v, want nil", tt.port, err)
				}
			}
		})
	}
}

func TestHostValidator_CIDRParsing(t *testing.T) {
	tests := []struct {
		name         string
		allowedHosts []string
		blockedHosts []string
		testHost     string
		wantErr      bool
	}{
		{
			name:         "IPv4 CIDR in allowlist",
			allowedHosts: []string{"10.0.0.0/8"},
			testHost:     "10.1.2.3",
			wantErr:      false,
		},
		{
			name:         "IPv4 /32 in allowlist",
			allowedHosts: []string{"10.0.0.1/32"},
			testHost:     "10.0.0.1",
			wantErr:      false,
		},
		{
			name:         "IPv6 CIDR in blocklist",
			blockedHosts: []string{"2001:db8::/32"},
			testHost:     "2001:db8::1",
			wantErr:      true,
		},
		{
			name:         "single IP parsed as /32",
			allowedHosts: []string{"10.0.0.1"},
			testHost:     "10.0.0.1",
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewHostValidator(false, tt.allowedHosts, tt.blockedHosts)
			err := v.ValidateHost(tt.testHost)

			if tt.wantErr && err == nil {
				t.Errorf("ValidateHost(%q) = nil, want error", tt.testHost)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateHost(%q) = %v, want nil", tt.testHost, err)
			}
		})
	}
}

func TestHostValidator_InternalNetworksList(t *testing.T) {
	v := DefaultHostValidator()

	// Test that common internal ranges are blocked
	internalIPs := []string{
		"10.0.0.1",
		"10.255.255.255",
		"172.16.0.1",
		"172.31.255.255",
		"192.168.0.1",
		"192.168.255.255",
		"127.0.0.1",
		"127.255.255.255",
		"169.254.1.1",
		"100.64.0.1",   // Carrier-grade NAT
		"192.0.0.1",    // IETF protocol assignments
		"192.0.2.1",    // TEST-NET-1
		"198.51.100.1", // TEST-NET-2
		"203.0.113.1",  // TEST-NET-3
		"0.0.0.1",      // Current network
	}

	for _, ip := range internalIPs {
		err := v.ValidateHost(ip)
		if err == nil {
			t.Errorf("Expected internal IP %s to be blocked", ip)
		}
	}
}

func TestHostValidator_BlocklistTakesPrecedence(t *testing.T) {
	// When a host is in both allowlist and blocklist, blocklist should win
	v := NewHostValidator(false,
		[]string{"blocked.example.com"},
		[]string{"blocked.example.com"})

	err := v.ValidateHost("blocked.example.com")
	if err == nil {
		t.Error("Expected host in both lists to be blocked")
	}
}
