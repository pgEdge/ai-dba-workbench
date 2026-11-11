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
	"errors"
	"net"
	"strings"
	"unicode"
)

// forbiddenHostChars are the characters a connection host must never
// carry. Each one is structure, not data, somewhere a host travels: ','
// separates the entries of a libpq multi-host list in both the URI and
// keyword/value forms, '[' and ']' delimit an IPv6 literal in a URI,
// '/' starts the URI path (and makes a libpq host a Unix socket
// directory), '?' starts the query, '@' ends the userinfo, '#' starts a
// fragment and '%' starts a percent-escape or an IPv6 zone. A host
// carrying any of them can split one connection target into several or
// smuggle in a port or parameter that no validator looked at.
const forbiddenHostChars = ",[]/?@#%"

// ErrHostSyntax is returned when a host or hostaddr carries a character
// that libpq or pgx would read as connection-string structure.
var ErrHostSyntax = errors.New("host contains a character that is not allowed in a connection host")

// ErrHostAddrNotIP is returned when a hostaddr is not a literal IP
// address.
var ErrHostAddrNotIP = errors.New("hostaddr must be a literal IP address")

// CheckHostSyntax rejects a host name that carries a character from
// forbiddenHostChars, whitespace or a control character. It does no
// DNS lookup and no network-range check; callers that accept the host
// from a user must also run their SSRF checks.
func CheckHostSyntax(host string) error {
	if strings.ContainsAny(host, forbiddenHostChars) {
		return ErrHostSyntax
	}
	for _, r := range host {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return ErrHostSyntax
		}
	}
	return nil
}

// trimOneBracketPair removes a single enclosing pair of square
// brackets, as an IPv6 literal may carry, and nothing else, so a value
// such as "[[::1]]" keeps its inner brackets and is still rejected.
func trimOneBracketPair(s string) string {
	if len(s) >= 2 && s[0] == '[' && s[len(s)-1] == ']' {
		return s[1 : len(s)-1]
	}
	return s
}

// ParseHostAddr parses a libpq hostaddr value, which must be a literal
// IPv4 or IPv6 address, optionally wrapped in one pair of brackets. It
// returns the parsed address; net.ParseIP accepts no zone, port, list
// separator or other structure, so a value that parses cannot reshape
// the connection string it is placed into.
func ParseHostAddr(hostaddr string) (net.IP, error) {
	ip := net.ParseIP(trimOneBracketPair(hostaddr))
	if ip == nil {
		return nil, ErrHostAddrNotIP
	}
	return ip, nil
}

// CheckStoredTarget is the defensive check applied when a stored
// connection's host and hostaddr are about to be placed into a
// connection string. The API validates both on the way in, but a row
// written before that validation existed, or by another route, must
// still not be able to turn one target into a multi-host list or carry
// an injected port. The host may keep one enclosing bracket pair, which
// is stripped before the syntax check; a non-empty hostaddr must be a
// literal IP address.
func CheckStoredTarget(host, hostaddr string) error {
	if err := CheckHostSyntax(trimOneBracketPair(host)); err != nil {
		return err
	}
	if hostaddr != "" {
		if _, err := ParseHostAddr(hostaddr); err != nil {
			return err
		}
	}
	return nil
}
