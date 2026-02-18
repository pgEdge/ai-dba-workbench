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

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		private bool
	}{
		{"loopback IPv4", "127.0.0.1", true},
		{"loopback IPv6", "::1", true},
		{"RFC1918 10.x", "10.0.0.1", true},
		{"RFC1918 172.16.x", "172.16.0.1", true},
		{"RFC1918 192.168.x", "192.168.1.1", true},
		{"link-local", "169.254.1.1", true},
		{"carrier-grade NAT", "100.64.0.1", true},
		{"multicast", "224.0.0.1", true},
		{"public IP", "8.8.8.8", false},
		{"public IP 2", "1.1.1.1", false},
		{"public IP 3", "93.184.216.34", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP %s", tt.ip)
			}
			got := IsPrivateIP(ip)
			if got != tt.private {
				t.Errorf("IsPrivateIP(%s) = %v, want %v", tt.ip, got, tt.private)
			}
		})
	}
}

func TestValidateURLHost_Literals(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"loopback URL", "http://127.0.0.1/hook", true},
		{"private 10.x URL", "https://10.0.0.5:8080/webhook", true},
		{"private 192.168 URL", "http://192.168.1.1/notify", true},
		{"public IP URL", "https://8.8.8.8/webhook", false},
		{"empty hostname", "http:///path", true},
		{"invalid URL", "://bad", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateURLHost(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateURLHost(%q) error = %v, wantErr %v",
					tt.url, err, tt.wantErr)
			}
		})
	}
}
