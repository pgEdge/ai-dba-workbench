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

func TestIsPrivateIP_AdditionalRanges(t *testing.T) {
    tests := []struct {
        name    string
        ip      string
        private bool
    }{
        // IPv6 tests
        {"IPv6 loopback", "::1", true},
        {"IPv6 unique local", "fd00::1", true},
        {"IPv6 link-local", "fe80::1", true},
        {"IPv6 public", "2001:4860:4860::8888", false},

        // Edge cases within ranges
        {"10.0.0.0 start", "10.0.0.0", true},
        {"10.255.255.255 end", "10.255.255.255", true},
        {"172.16.0.0 start", "172.16.0.0", true},
        {"172.31.255.255 end", "172.31.255.255", true},
        {"172.15.255.255 before range", "172.15.255.255", false},
        {"172.32.0.0 after range", "172.32.0.0", false},
        {"192.168.0.0 start", "192.168.0.0", true},
        {"192.168.255.255 end", "192.168.255.255", true},

        // Special ranges
        {"current network", "0.0.0.1", true},
        {"TEST-NET-1", "192.0.2.1", true},
        {"TEST-NET-2", "198.51.100.1", true},
        {"TEST-NET-3", "203.0.113.1", true},
        {"IETF protocol", "192.0.0.1", true},
        {"reserved", "240.0.0.1", true},
        {"multicast", "239.255.255.255", true},

        // Public IPs
        {"Cloudflare DNS", "1.1.1.1", false},
        {"Quad9 DNS", "9.9.9.9", false},
        {"Random public IP", "203.0.114.1", false},
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

func TestValidateURLHost_IPv6Literals(t *testing.T) {
    tests := []struct {
        name    string
        url     string
        wantErr bool
    }{
        {"IPv6 loopback", "http://[::1]:8080/hook", true},
        {"IPv6 private", "http://[fd00::1]/webhook", true},
        {"IPv6 link-local", "http://[fe80::1]/webhook", true},
        {"IPv6 public", "http://[2001:4860:4860::8888]/webhook", false},
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

func TestValidateURLHost_URLFormats(t *testing.T) {
    tests := []struct {
        name    string
        url     string
        wantErr bool
    }{
        {"with port", "https://8.8.8.8:443/path", false},
        {"with query", "https://8.8.8.8/path?query=1", false},
        {"with fragment", "https://8.8.8.8/path#fragment", false},
        {"with auth", "https://user:pass@8.8.8.8/path", false},
        {"private with port", "https://192.168.1.1:443/path", true},
        {"ftp scheme", "ftp://8.8.8.8/file", false},
        {"custom scheme", "myapp://8.8.8.8/callback", false},
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

func TestValidateURLHost_ErrorMessages(t *testing.T) {
    // Test that error messages contain useful information
    err := ValidateURLHost("http://127.0.0.1/path")
    if err == nil {
        t.Fatal("expected error")
    }
    errMsg := err.Error()
    if !containsAll(errMsg, "127.0.0.1", "internal") {
        t.Errorf("error message should mention IP and internal: %s", errMsg)
    }

    err = ValidateURLHost("http:///path")
    if err == nil {
        t.Fatal("expected error for empty hostname")
    }
    if !containsAll(err.Error(), "hostname") {
        t.Errorf("error message should mention hostname: %s", err.Error())
    }
}

// containsAll checks if s contains all substrings
func containsAll(s string, substrings ...string) bool {
    for _, sub := range substrings {
        if !contains(s, sub) {
            return false
        }
    }
    return true
}

// contains checks if s contains substr (case-insensitive)
func contains(s, substr string) bool {
    return len(s) >= len(substr) && (s == substr ||
        len(s) > 0 && containsCI(s, substr))
}

func containsCI(s, substr string) bool {
    for i := 0; i <= len(s)-len(substr); i++ {
        if eqCI(s[i:i+len(substr)], substr) {
            return true
        }
    }
    return false
}

func eqCI(a, b string) bool {
    if len(a) != len(b) {
        return false
    }
    for i := 0; i < len(a); i++ {
        ca, cb := a[i], b[i]
        if ca >= 'A' && ca <= 'Z' {
            ca += 'a' - 'A'
        }
        if cb >= 'A' && cb <= 'Z' {
            cb += 'a' - 'A'
        }
        if ca != cb {
            return false
        }
    }
    return true
}

func TestValidateURLHost_DNSResolution(t *testing.T) {
    // Test with a hostname that should resolve (requires network)
    // Using well-known public hostnames
    t.Run("google.com resolves to public IP", func(t *testing.T) {
        err := ValidateURLHost("https://google.com/webhook")
        if err != nil {
            errStr := err.Error()
            if containsCI(errStr, "cannot resolve") ||
                containsCI(errStr, "no such host") ||
                containsCI(errStr, "i/o timeout") ||
                containsCI(errStr, "temporary failure") {
                t.Skipf("skipping network-dependent DNS check: %v", err)
            }
            t.Fatalf("unexpected validation error for public hostname: %v", err)
        }
    })

    t.Run("localhost resolves to private IP", func(t *testing.T) {
        err := ValidateURLHost("https://localhost/webhook")
        // localhost should resolve to 127.0.0.1 which is private
        if err == nil {
            t.Fatal("expected error for localhost")
        }
        errStr := err.Error()
        if containsCI(errStr, "cannot resolve") || containsCI(errStr, "no such host") {
            t.Skipf("skipping localhost DNS-dependent check: %v", err)
        }
        if !containsCI(errStr, "internal IP") && !containsCI(errStr, "private") {
            t.Fatalf("expected private-IP rejection for localhost, got: %v", err)
        }
    })

    t.Run("nonexistent hostname", func(t *testing.T) {
        err := ValidateURLHost("https://this-hostname-definitely-does-not-exist-12345.invalid/webhook")
        if err == nil {
            t.Fatal("expected error for nonexistent hostname")
        }
        if !containsCI(err.Error(), "cannot resolve") {
            t.Errorf("expected 'cannot resolve' in error, got: %v", err)
        }
    })
}
