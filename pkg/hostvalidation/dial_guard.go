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
)

// AddrPolicy decides whether a connection to host may be made at ip, one
// of the addresses host resolved to when the connection was opened. A
// nil error permits the address.
type AddrPolicy func(host string, ip net.IP) error

// LookupFunc has the signature of pgconn.LookupFunc, so a GuardedLookup
// result can be assigned to pgconn.Config.LookupFunc without this
// package importing pgx.
type LookupFunc = func(ctx context.Context, host string) ([]string, error)

// ErrNoPermittedAddress is returned by a GuardedLookup function when every
// address the host resolved to is refused. It names neither the host nor
// any address, because connect errors reach API responses and the
// connections.connection_error column.
var ErrNoPermittedAddress = errors.New(
	"host resolves only to addresses that connections are not permitted to")

// errAlwaysBlocked is CheckAlwaysBlocked's refusal.
var errAlwaysBlocked = errors.New("address is in an always-blocked range")

// alwaysBlockedNetworks are ranges no monitored connection has a reason
// to reach: the unspecified addresses, which a dialler treats as the
// local host; link-local unicast, which holds the cloud instance
// metadata services; and the metadata addresses some clouds place
// elsewhere.
var alwaysBlockedNetworks = mustParseCIDRs(
	"0.0.0.0/8",          // "this network"; 0.0.0.0 reaches localhost
	"::/128",             // IPv6 unspecified; reaches localhost
	"169.254.0.0/16",     // IPv4 link-local, incl. 169.254.169.254 (AWS, GCP, Azure, OCI, ...)
	"fe80::/10",          // IPv6 link-local
	"fd00:ec2::254/128",  // AWS instance metadata over IPv6
	"100.100.100.200/32", // Alibaba Cloud instance metadata
)

func mustParseCIDRs(cidrs ...string) []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic("hostvalidation: bad CIDR " + c)
		}
		nets = append(nets, n)
	}
	return nets
}

// embeddedIPv4 returns the IPv4 address that an IPv6 address carries
// in one of the forms a translator or tunnel can turn back into an IPv4
// destination, or nil when ip is none of them:
//
//   - IPv4-compatible, ::/96 (RFC 4291, deprecated): bytes 12-15;
//   - SIIT IPv4-translated, ::ffff:0:0:0/96 (RFC 2765), written
//     ::ffff:0:a.b.c.d: bytes 12-15;
//   - NAT64 well-known prefix, 64:ff9b::/96 (RFC 6052): bytes 12-15;
//   - NAT64 local-use prefix, 64:ff9b:1::/48 (RFC 8215), in the RFC 6052
//     /48 layout: bytes 6-7 and 9-10, with byte 8 (bits 64-71) the
//     reserved octet. Its value is not checked, so a translator that
//     ignores it cannot be used to slip past the caller;
//   - 6to4, 2002::/16 (RFC 3056): bytes 2-5.
//
// An IPv4-mapped address (::ffff:a.b.c.d) needs no special case:
// net.IP.To4 already unwraps it, and net.IPNet.Contains with it.
func embeddedIPv4(ip net.IP) net.IP {
	if ip.To4() != nil {
		return nil
	}
	ip16 := ip.To16()
	if ip16 == nil {
		return nil
	}
	isZero := func(b []byte) bool {
		for _, x := range b {
			if x != 0 {
				return false
			}
		}
		return true
	}
	isNAT64 := ip16[0] == 0x00 && ip16[1] == 0x64 && ip16[2] == 0xff && ip16[3] == 0x9b
	switch {
	case isZero(ip16[:12]):
		return net.IPv4(ip16[12], ip16[13], ip16[14], ip16[15])
	case isZero(ip16[:8]) && ip16[8] == 0xff && ip16[9] == 0xff && isZero(ip16[10:12]):
		return net.IPv4(ip16[12], ip16[13], ip16[14], ip16[15])
	case isNAT64 && isZero(ip16[4:12]):
		return net.IPv4(ip16[12], ip16[13], ip16[14], ip16[15])
	case isNAT64 && ip16[4] == 0x00 && ip16[5] == 0x01:
		return net.IPv4(ip16[6], ip16[7], ip16[9], ip16[10])
	case ip16[0] == 0x20 && ip16[1] == 0x02:
		return net.IPv4(ip16[2], ip16[3], ip16[4], ip16[5])
	}
	return nil
}

// IsAlwaysBlockedIP reports whether ip is one no monitored connection
// may be opened to, whatever the deployment's other settings: an
// unspecified address, a link-local address (unicast or multicast), a
// known cloud metadata address, or an IPv6 address embedding any of
// these IPv4 addresses.
func IsAlwaysBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if ip.IsLoopback() {
		// ::1 would otherwise read as the IPv4-compatible 0.0.0.1.
		return false
	}
	for _, n := range alwaysBlockedNetworks {
		if n.Contains(ip) {
			return true
		}
	}
	if v4 := embeddedIPv4(ip); v4 != nil {
		return IsAlwaysBlockedIP(v4)
	}
	return false
}

// CheckAlwaysBlocked is an AddrPolicy refusing the addresses
// IsAlwaysBlockedIP reports, and permitting every other. It is the floor
// for a process that has no configured connection policy.
func CheckAlwaysBlocked(_ string, ip net.IP) error {
	if IsAlwaysBlockedIP(ip) {
		return errAlwaysBlocked
	}
	return nil
}

// permits reports whether a connection to host may be made at ip: the
// address must not be one IsAlwaysBlockedIP reports, and policy must
// permit it. The always-blocked check comes first so that no policy,
// however permissive (an allowed_hosts name match, or
// allow_internal_networks, which covers link-local), can reach an
// unspecified, link-local or cloud metadata address.
func permits(policy AddrPolicy, host string, ip net.IP) bool {
	return !IsAlwaysBlockedIP(ip) && policy(host, ip) == nil
}

// GuardedLookup wraps resolve, the lookup function a connection would
// otherwise use (net.DefaultResolver.LookupHost when nil), so that only
// the addresses policy permits are returned for pgx to connect to. The
// addresses IsAlwaysBlockedIP reports are refused before policy is
// consulted, whatever policy would say. pgx calls its
// LookupFunc for every non-socket host, IP literals included, and dials
// exactly the addresses it returns, so this closes the gap between a
// host name being checked when a connection is saved and it being
// resolved again, perhaps to a different address, when it is used.
//
// An entry pgx would read as "ip:port" is checked by its IP. An entry
// that is not an IP address at all is dropped, since it cannot be
// checked. When nothing is left, ErrNoPermittedAddress is returned; a
// lookup error from resolve is returned unchanged.
func GuardedLookup(policy AddrPolicy, resolve LookupFunc) LookupFunc {
	if policy == nil {
		policy = CheckAlwaysBlocked
	}
	if resolve == nil {
		resolve = net.DefaultResolver.LookupHost
	}
	return func(ctx context.Context, host string) ([]string, error) {
		addrs, err := resolve(ctx, host)
		if err != nil {
			return nil, err
		}
		permitted := make([]string, 0, len(addrs))
		for _, addr := range addrs {
			ipText := addr
			if h, _, splitErr := net.SplitHostPort(addr); splitErr == nil {
				ipText = h
			}
			// A zone ("fe80::1%eth0") is only valid on a link-local
			// address, which is refused anyway; ParseIP rejects it.
			ip := net.ParseIP(ipText)
			if ip == nil {
				continue
			}
			if !permits(policy, host, ip) {
				continue
			}
			permitted = append(permitted, addr)
		}
		if len(permitted) == 0 {
			return nil, ErrNoPermittedAddress
		}
		return permitted, nil
	}
}

// errInternal is CheckNotInternal's refusal.
var errInternal = errors.New("address is in an internal range")

// CheckNotInternal is an AddrPolicy refusing every address IsPrivateIP
// or IsAlwaysBlockedIP reports, and permitting every other. It is the
// policy for outbound connections that should reach public addresses
// only, where nothing has been configured to say otherwise.
func CheckNotInternal(_ string, ip net.IP) error {
	if IsAlwaysBlockedIP(ip) || IsPrivateIP(ip) {
		return errInternal
	}
	return nil
}

// DialContextFunc has the signature of net.Dialer.DialContext and of
// http.Transport.DialContext.
type DialContextFunc = func(ctx context.Context, network, addr string) (net.Conn, error)

// GuardedDialContext returns a dial function that resolves the host in
// addr itself, through GuardedLookup(policy, resolve), and hands dial
// only the permitted addresses, as IP literals, trying each in turn
// until one connects. The policy is therefore applied to the address
// actually connected to, whatever the host resolved to when it was last
// checked (DNS rebinding), and to every connection a client opens with
// it, including one made to follow a redirect.
//
// A host that is already an IP literal is judged directly, without
// calling resolve. Either way, an address IsAlwaysBlockedIP reports is
// refused before policy is consulted. A nil policy is CheckAlwaysBlocked, a nil
// resolve is net.DefaultResolver.LookupHost and a nil dial is a zero
// net.Dialer's DialContext. When no address is permitted the error is
// ErrNoPermittedAddress, which names neither the host nor any address;
// an error from resolve or from the last dial attempt is returned
// unchanged.
func GuardedDialContext(policy AddrPolicy, resolve LookupFunc, dial DialContextFunc) DialContextFunc {
	if policy == nil {
		policy = CheckAlwaysBlocked
	}
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	lookup := GuardedLookup(policy, resolve)
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		var addrs []string
		if ip := net.ParseIP(host); ip != nil {
			if !permits(policy, host, ip) {
				return nil, ErrNoPermittedAddress
			}
			addrs = []string{host}
		} else {
			addrs, err = lookup(ctx, host)
			if err != nil {
				return nil, err
			}
		}
		var lastErr error
		for _, a := range addrs {
			// GuardedLookup keeps an "ip:port" entry from a custom
			// resolver as it was; the port dialled is always addr's.
			if h, _, splitErr := net.SplitHostPort(a); splitErr == nil {
				a = h
			}
			conn, dialErr := dial(ctx, network, net.JoinHostPort(a, port))
			if dialErr == nil {
				return conn, nil
			}
			lastErr = dialErr
			if ctx.Err() != nil {
				break
			}
		}
		return nil, lastErr
	}
}
