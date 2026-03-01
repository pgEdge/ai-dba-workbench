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
	"fmt"
	"net"
	"strings"

	"github.com/pgedge/ai-workbench/pkg/hostvalidation"
)

// HostValidator validates database host addresses to prevent SSRF attacks.
// By default, it blocks connections to internal/private IP ranges unless
// explicitly allowed.
type HostValidator struct {
	// AllowInternalNetworks permits connections to RFC 1918 private addresses
	// and other internal network ranges. Default: false for security.
	AllowInternalNetworks bool

	// AllowedHosts is an optional allowlist of hosts that are always permitted.
	// Supports exact hostnames and CIDR notation for IP ranges.
	AllowedHosts []string

	// BlockedHosts is an optional blocklist of hosts that are never permitted.
	// Evaluated after AllowedHosts.
	BlockedHosts []string

	// parsedAllowed contains parsed CIDR networks from AllowedHosts
	parsedAllowed []*net.IPNet

	// parsedBlocked contains parsed CIDR networks from BlockedHosts
	parsedBlocked []*net.IPNet
}

// parseHostsToCIDR parses a slice of host strings into CIDR networks.
// It handles both CIDR notation (e.g., "10.0.0.0/8") and single IP addresses
// (which are converted to /32 for IPv4 or /128 for IPv6). Hostnames are
// skipped since they are checked by exact match elsewhere.
func parseHostsToCIDR(hosts []string) []*net.IPNet {
	result := make([]*net.IPNet, 0, len(hosts))
	for _, host := range hosts {
		if _, ipNet, err := net.ParseCIDR(host); err == nil {
			result = append(result, ipNet)
		} else if ip := net.ParseIP(host); ip != nil {
			// Single IP - convert to CIDR
			var ipNet *net.IPNet
			if ip.To4() != nil {
				_, ipNet, _ = net.ParseCIDR(host + "/32") //nolint:errcheck // IP already validated
			} else {
				_, ipNet, _ = net.ParseCIDR(host + "/128") //nolint:errcheck // IP already validated
			}
			if ipNet != nil {
				result = append(result, ipNet)
			}
		}
		// Hostnames are checked by exact match, not added to result
	}
	return result
}

// NewHostValidator creates a new HostValidator with the given configuration.
func NewHostValidator(allowInternal bool, allowedHosts, blockedHosts []string) *HostValidator {
	return &HostValidator{
		AllowInternalNetworks: allowInternal,
		AllowedHosts:          allowedHosts,
		BlockedHosts:          blockedHosts,
		parsedAllowed:         parseHostsToCIDR(allowedHosts),
		parsedBlocked:         parseHostsToCIDR(blockedHosts),
	}
}

// ValidateHost checks if a host is allowed for database connections.
// Returns nil if the host is allowed, or an error describing why it's blocked.
func (v *HostValidator) ValidateHost(host string) error {
	if host == "" {
		return fmt.Errorf("host cannot be empty")
	}

	// Normalize the host (remove any trailing dots, lowercase)
	host = strings.TrimSuffix(strings.ToLower(host), ".")

	// Check explicit blocklist first (hostnames)
	for _, blocked := range v.BlockedHosts {
		if strings.EqualFold(blocked, host) {
			return fmt.Errorf("host '%s' is in the blocklist", host)
		}
	}

	// Check if it's in the explicit allowlist (hostnames)
	for _, allowed := range v.AllowedHosts {
		if strings.EqualFold(allowed, host) {
			return nil // Explicitly allowed
		}
	}

	// Try to parse as IP address
	ip := net.ParseIP(host)
	if ip != nil {
		// Check IP blocklist
		for _, blocked := range v.parsedBlocked {
			if blocked.Contains(ip) {
				return fmt.Errorf("IP address '%s' is in a blocked range", host)
			}
		}

		// Check IP allowlist
		for _, allowed := range v.parsedAllowed {
			if allowed.Contains(ip) {
				return nil // Explicitly allowed
			}
		}

		// Check internal networks
		if !v.AllowInternalNetworks {
			if hostvalidation.IsPrivateIP(ip) {
				return fmt.Errorf("connections to internal IP address '%s' are not allowed", host)
			}
		}

		// IP is allowed
		return nil
	}

	// It's a hostname - resolve it and check each resolved IP
	// This prevents DNS rebinding attacks where a hostname resolves to internal IPs
	ips, err := net.LookupIP(host)
	if err != nil {
		// Can't resolve - block it for security. Allowing unresolvable hosts could
		// enable SSRF attacks where an attacker uses a hostname that this server
		// cannot resolve but the target database server can (via internal DNS).
		return fmt.Errorf("cannot resolve hostname '%s': DNS lookup failed", host)
	}

	for _, resolvedIP := range ips {
		// Check IP blocklist
		for _, blocked := range v.parsedBlocked {
			if blocked.Contains(resolvedIP) {
				return fmt.Errorf("hostname '%s' resolves to blocked IP address '%s'",
					host, resolvedIP.String())
			}
		}

		// Check IP allowlist - if any resolved IP is in allowlist, allow it
		for _, allowed := range v.parsedAllowed {
			if allowed.Contains(resolvedIP) {
				return nil
			}
		}

		// Check internal networks
		if !v.AllowInternalNetworks {
			if hostvalidation.IsPrivateIP(resolvedIP) {
				return fmt.Errorf("hostname '%s' resolves to internal IP address '%s'; "+
					"connections to internal networks are not allowed",
					host, resolvedIP.String())
			}
		}
	}

	return nil
}

// ValidatePort checks if a port number is valid for database connections.
// Returns nil if the port is valid, or an error describing why it's invalid.
func (v *HostValidator) ValidatePort(port int) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got %d", port)
	}

	// Block well-known non-database ports that are commonly targeted in SSRF attacks
	blockedPorts := map[int]string{
		22:   "SSH",
		25:   "SMTP",
		80:   "HTTP",
		110:  "POP3",
		143:  "IMAP",
		443:  "HTTPS",
		445:  "SMB",
		993:  "IMAPS",
		995:  "POP3S",
		6379: "Redis",
	}

	if service, blocked := blockedPorts[port]; blocked {
		return fmt.Errorf("port %d (%s) is not a typical database port and is blocked for security",
			port, service)
	}

	return nil
}

// DefaultHostValidator returns a validator with secure defaults:
// - Blocks internal network connections
// - No allowed/blocked host lists
func DefaultHostValidator() *HostValidator {
	return NewHostValidator(false, nil, nil)
}
