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
	"fmt"
	"net"
	"net/url"
)

// internalNetworks defines RFC 1918 private address ranges and other
// internal networks that should be blocked for SSRF protection.
var internalNetworks = []string{
	"10.0.0.0/8",      // RFC 1918 Class A private
	"172.16.0.0/12",   // RFC 1918 Class B private
	"192.168.0.0/16",  // RFC 1918 Class C private
	"127.0.0.0/8",     // Loopback
	"169.254.0.0/16",  // Link-local
	"::1/128",         // IPv6 loopback
	"fc00::/7",        // IPv6 unique local
	"fe80::/10",       // IPv6 link-local
	"0.0.0.0/8",       // Current network (invalid as destination)
	"100.64.0.0/10",   // Carrier-grade NAT (RFC 6598)
	"192.0.0.0/24",    // IETF protocol assignments
	"192.0.2.0/24",    // TEST-NET-1
	"198.51.100.0/24", // TEST-NET-2
	"203.0.113.0/24",  // TEST-NET-3
	"224.0.0.0/4",     // Multicast
	"240.0.0.0/4",     // Reserved for future use
}

// parsedInternalNetworks contains pre-parsed internal network CIDRs.
var parsedInternalNetworks []*net.IPNet

func init() {
	parsedInternalNetworks = make([]*net.IPNet, 0, len(internalNetworks))
	for _, cidr := range internalNetworks {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err == nil {
			parsedInternalNetworks = append(parsedInternalNetworks, ipNet)
		}
	}
}

// IsPrivateIP checks whether the given IP address belongs to a private,
// loopback, link-local, or otherwise internal network range.
func IsPrivateIP(ip net.IP) bool {
	for _, internal := range parsedInternalNetworks {
		if internal.Contains(ip) {
			return true
		}
	}
	return false
}

// ValidateURLHost resolves the hostname from a URL and checks that none
// of the resolved IP addresses belong to internal network ranges. This
// prevents SSRF attacks where user-supplied URLs target internal services.
// Returns nil if the URL targets a public address, or an error if the
// host resolves to a private/internal address.
func ValidateURLHost(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	hostname := parsed.Hostname()
	if hostname == "" {
		return fmt.Errorf("URL has no hostname")
	}

	// Check if the hostname is a literal IP address
	if ip := net.ParseIP(hostname); ip != nil {
		if IsPrivateIP(ip) {
			return fmt.Errorf(
				"URL host %q resolves to internal IP address %s; "+
					"requests to internal networks are blocked",
				hostname, ip.String())
		}
		return nil
	}

	// Resolve the hostname and check each IP
	ips, err := net.LookupIP(hostname)
	if err != nil {
		return fmt.Errorf("cannot resolve hostname %q: %w", hostname, err)
	}

	for _, ip := range ips {
		if IsPrivateIP(ip) {
			return fmt.Errorf(
				"URL host %q resolves to internal IP address %s; "+
					"requests to internal networks are blocked",
				hostname, ip.String())
		}
	}

	return nil
}
