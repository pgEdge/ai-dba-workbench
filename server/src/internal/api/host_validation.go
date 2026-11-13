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
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"

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

	// lookupIP resolves a host name for ValidateHost; nil is
	// net.LookupIP. Tests set it to stand in for DNS.
	lookupIP func(host string) ([]net.IP, error)
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

	// An IPv6 literal may be stored with one enclosing pair of
	// brackets, as written by a route other than this API; the
	// collector and BuildConnectionString both strip that pair before
	// use (hostvalidation.BareHost).
	// The brackets are accepted only around an IPv6 literal, so they can
	// never wrap a host name or a list, and the checks below then see
	// the bare address.
	if bare := hostvalidation.BareHost(host); bare != host {
		if net.ParseIP(bare) == nil || !strings.Contains(bare, ":") {
			return fmt.Errorf("host %q: %w", host, hostvalidation.ErrHostSyntax)
		}
		host = bare
	}

	// Refuse a host carrying connection-string structure (a ',' list
	// separator, any other bracket, '/', '?', '@', '#', '%' or
	// whitespace) before anything else: such a host could become several
	// targets, or carry a port, that none of the checks below examined.
	if err := hostvalidation.CheckHostSyntax(host); err != nil {
		return fmt.Errorf("host %q: %w", host, err)
	}

	// Normalize the host (remove any trailing dots, lowercase)
	host = strings.TrimSuffix(strings.ToLower(host), ".")

	// Refuse an address that every connect path refuses before the
	// blocklist and allowlist are consulted, so that neither an
	// allowed_hosts entry nor allow_internal_networks lets a target be
	// saved that can never be connected to.
	ip := net.ParseIP(host)
	if ip != nil && hostvalidation.IsAlwaysBlockedIP(ip) {
		return fmt.Errorf("IP address '%s': %w", host, ErrAlwaysBlockedHost)
	}

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

	if ip != nil {
		switch v.classifyIP(ip) {
		case ipBlocked:
			return fmt.Errorf("IP address '%s' is in a blocked range", host)
		case ipInternal:
			return fmt.Errorf("connections to internal IP address '%s' are not allowed", host)
		}
		return nil
	}

	// It's a hostname: resolve it and check each address it resolves to
	// now. This check alone cannot stop DNS rebinding, since the name can
	// resolve to a different address when the connection is made; the
	// connect paths re-apply the same rules to the addresses pgx actually
	// dials (CheckDialAddr, wired in through Datastore.SetDialPolicy).
	lookup := v.lookupIP
	if lookup == nil {
		lookup = net.LookupIP
	}
	ips, err := lookup(host)
	if err != nil {
		// Can't resolve - block it for security. Allowing unresolvable hosts could
		// enable SSRF attacks where an attacker uses a hostname that this server
		// cannot resolve but the target database server can (via internal DNS).
		return fmt.Errorf("cannot resolve hostname '%s': DNS lookup failed", host)
	}

	// Check every address against the always-blocked floor before the
	// loop below, which stops at the first allow-listed address.
	for _, resolvedIP := range ips {
		if hostvalidation.IsAlwaysBlockedIP(resolvedIP) {
			return fmt.Errorf("hostname '%s' resolves to '%s': %w",
				host, resolvedIP.String(), ErrAlwaysBlockedHost)
		}
	}

	for _, resolvedIP := range ips {
		switch v.classifyIP(resolvedIP) {
		case ipBlocked:
			return fmt.Errorf("hostname '%s' resolves to blocked IP address '%s'",
				host, resolvedIP.String())
		case ipAllowListed:
			// If any resolved IP is in the allowlist, allow the host
			return nil
		case ipInternal:
			return fmt.Errorf("hostname '%s' resolves to internal IP address '%s'; "+
				"connections to internal networks are not allowed",
				host, resolvedIP.String())
		}
	}

	return nil
}

// ErrAlwaysBlockedHost is wrapped by ValidateHost and
// CheckAlwaysBlockedHost when a host is, or resolves to, an address that
// hostvalidation.IsAlwaysBlockedIP reports. Every connect path refuses
// such an address whatever the configuration says, so it is refused when
// a connection or notification channel is saved as well. Its message
// names neither the host nor the address, so it may be returned to the
// client.
var ErrAlwaysBlockedHost = errors.New(
	"unspecified, link-local and cloud metadata addresses are always blocked")

// alwaysBlockedLookupTimeout bounds the lookup CheckAlwaysBlockedHost
// makes for a host name.
const alwaysBlockedLookupTimeout = 5 * time.Second

// CheckAlwaysBlockedHost returns an error wrapping ErrAlwaysBlockedHost
// when host is an IP literal (optionally bracketed, optionally with an
// IPv6 zone) that hostvalidation.IsAlwaysBlockedIP reports, or a host
// name any of whose addresses it reports. A nil lookup is the system
// resolver. A failed lookup refuses nothing: the check rejects a target
// that can never be reached, and does not require a name to resolve
// from this host.
func CheckAlwaysBlockedHost(ctx context.Context, host string, lookup hostvalidation.LookupFunc) error {
	host = strings.TrimSuffix(hostvalidation.BareHost(host), ".")
	if host == "" {
		return nil
	}
	if ip := parseIPLiteral(host); ip != nil {
		if hostvalidation.IsAlwaysBlockedIP(ip) {
			return ErrAlwaysBlockedHost
		}
		return nil
	}
	if lookup == nil {
		lookup = net.DefaultResolver.LookupHost
	}
	ctx, cancel := context.WithTimeout(ctx, alwaysBlockedLookupTimeout)
	defer cancel()
	addrs, err := lookup(ctx, host)
	if err != nil {
		return nil
	}
	for _, addr := range addrs {
		if ip := parseIPLiteral(addr); ip != nil && hostvalidation.IsAlwaysBlockedIP(ip) {
			return ErrAlwaysBlockedHost
		}
	}
	return nil
}

// parseIPLiteral parses s as an IP address, dropping any IPv6 zone, and
// returns nil when s is not one.
func parseIPLiteral(s string) net.IP {
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return nil
	}
	return net.IP(addr.WithZone("").AsSlice())
}

// ipClass is classifyIP's verdict on one address.
type ipClass int

const (
	ipPermitted   ipClass = iota // no rule refuses it
	ipBlocked                    // in a blocked_hosts range
	ipAllowListed                // in an allowed_hosts range
	ipInternal                   // internal, and allow_internal_networks is off
)

// classifyIP applies the IP rules in the order ValidateHost has always
// used: the block-list, then the allow-list, then the internal ranges
// unless AllowInternalNetworks is set.
func (v *HostValidator) classifyIP(ip net.IP) ipClass {
	for _, blocked := range v.parsedBlocked {
		if blocked.Contains(ip) {
			return ipBlocked
		}
	}
	for _, allowed := range v.parsedAllowed {
		if allowed.Contains(ip) {
			return ipAllowListed
		}
	}
	if !v.AllowInternalNetworks && hostvalidation.IsPrivateIP(ip) {
		return ipInternal
	}
	return ipPermitted
}

// errDialAddrRefused is CheckDialAddr's refusal. It names neither the
// host nor the address, because connect errors can reach API and MCP
// responses.
var errDialAddrRefused = errors.New("address is not permitted by connection_security")

// CheckDialAddr is a hostvalidation.AddrPolicy that applies this
// validator's rules to ip, an address host resolved to when a monitored
// connection is opened, so that a host name which passed ValidateHost
// when it was saved cannot later be pointed somewhere it would not have
// passed (DNS rebinding). The rules are ValidateHost's: a host name in
// BlockedHosts refuses and one in AllowedHosts permits, whatever it
// resolves to; otherwise the address goes through the block-list, the
// allow-list and, unless AllowInternalNetworks is set, the internal
// ranges. Unlike ValidateHost, each address is judged on its own: an
// allow-listed address among a name's results permits only itself, not
// the other addresses the name resolved to.
//
// Before any of that, an address hostvalidation.IsAlwaysBlockedIP
// reports (unspecified, link-local or cloud metadata) is refused, so an
// allowed_hosts name, or allow_internal_networks, which covers
// link-local, cannot reach one.
func (v *HostValidator) CheckDialAddr(host string, ip net.IP) error {
	if hostvalidation.IsAlwaysBlockedIP(ip) {
		return errDialAddrRefused
	}
	name := strings.TrimSuffix(strings.ToLower(host), ".")
	for _, blocked := range v.BlockedHosts {
		if strings.EqualFold(blocked, name) {
			return errDialAddrRefused
		}
	}
	for _, allowed := range v.AllowedHosts {
		if strings.EqualFold(allowed, name) {
			return nil
		}
	}
	switch v.classifyIP(ip) {
	case ipBlocked, ipInternal:
		return errDialAddrRefused
	}
	return nil
}

// ValidateHostAddr checks a libpq hostaddr value for a database
// connection. The value must be a literal IP address (one enclosing
// bracket pair is allowed), and that address then passes through the
// same blocklist, allowlist and internal-range checks as ValidateHost,
// because the connection string prefers hostaddr over host: a hostaddr
// that skipped these checks would let any host name stand in front of
// an internal address.
func (v *HostValidator) ValidateHostAddr(hostaddr string) error {
	ip, err := hostvalidation.ParseHostAddr(hostaddr)
	if err != nil {
		return fmt.Errorf("hostaddr %q: %w", hostaddr, err)
	}
	return v.ValidateHost(ip.String())
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
