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
	"testing"
)

// hostileHostAddrs are hostaddr values the audit showed reshaping a
// connection string: the second closes the IPv6 bracket early, so
// net.JoinHostPort turns it into a two-host list with an injected port.
var hostileHostAddrs = []string{
	"127.0.0.1]:6379,[127.0.0.1",
	"10.0.0.1,10.0.0.2",
	"db.example.com",
	"169.254.169.254:6379",
	"fe80::1%eth0",
	"[[::1]]",
	"/var/run/postgresql",
	" 192.0.2.1",
	"",
}

func TestCheckHostSyntax(t *testing.T) {
	good := []string{"db.example.com", "192.0.2.1", "::1", "2001:db8::1",
		"host-name_1.example.com", "db.example.com."}
	for _, h := range good {
		if err := CheckHostSyntax(h); err != nil {
			t.Errorf("CheckHostSyntax(%q) = %v, want nil", h, err)
		}
	}
	bad := []string{"a,b", "[::1]", "a]b", "a[b", "/tmp", "a?b", "u@h",
		"h#f", "fe80::1%eth0", "a b", "a\tb", "a\nb", "a\x00b",
		"h x", "127.0.0.1]:6379,[127.0.0.1"}
	for _, h := range bad {
		if err := CheckHostSyntax(h); !errors.Is(err, ErrHostSyntax) {
			t.Errorf("CheckHostSyntax(%q) = %v, want ErrHostSyntax", h, err)
		}
	}
}

func TestParseHostAddr(t *testing.T) {
	cases := map[string]string{
		"192.0.2.1":       "192.0.2.1",
		"2001:db8::1":     "2001:db8::1",
		"[2001:db8::1]":   "2001:db8::1",
		"169.254.169.254": "169.254.169.254",
	}
	for in, want := range cases {
		ip, err := ParseHostAddr(in)
		if err != nil {
			t.Errorf("ParseHostAddr(%q) error = %v", in, err)
			continue
		}
		if ip.String() != want {
			t.Errorf("ParseHostAddr(%q) = %s, want %s", in, ip, want)
		}
	}
	for _, in := range hostileHostAddrs {
		if _, err := ParseHostAddr(in); !errors.Is(err, ErrHostAddrNotIP) {
			t.Errorf("ParseHostAddr(%q) = %v, want ErrHostAddrNotIP", in, err)
		}
	}
}

func TestCheckStoredTarget(t *testing.T) {
	ok := []struct{ host, hostaddr string }{
		{"db.example.com", ""},
		{"db.example.com", "192.0.2.1"},
		{"[2001:db8::1]", ""},
		{"2001:db8::1", "[2001:db8::1]"},
	}
	for _, c := range ok {
		if err := CheckStoredTarget(c.host, c.hostaddr); err != nil {
			t.Errorf("CheckStoredTarget(%q, %q) = %v, want nil",
				c.host, c.hostaddr, err)
		}
	}
	bad := []struct{ host, hostaddr string }{
		{"a,b", ""},
		{"[[::1]]", ""},
		{"127.0.0.1]:6379,[127.0.0.1", ""},
		{"/tmp", ""},
		{"db.example.com", "127.0.0.1]:6379,[127.0.0.1"},
		{"db.example.com", "other.example.com"},
		{"db.example.com", "10.0.0.1,10.0.0.2"},
	}
	for _, c := range bad {
		if err := CheckStoredTarget(c.host, c.hostaddr); err == nil {
			t.Errorf("CheckStoredTarget(%q, %q) = nil, want error",
				c.host, c.hostaddr)
		}
	}
}
