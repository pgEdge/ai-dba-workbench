/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package smtpauth

import (
	"errors"
	"net"
	"net/smtp"
	"testing"
)

// peerConn is a net.Conn whose only behaviour is its remote address.
type peerConn struct {
	net.Conn
	remote net.Addr
}

func (c peerConn) RemoteAddr() net.Addr { return c.remote }

// stringAddr is a net.Addr that is not a *net.TCPAddr.
type stringAddr string

func (a stringAddr) Network() string { return "test" }
func (a stringAddr) String() string  { return string(a) }

func tcpPeer(ip string) net.Conn {
	return peerConn{remote: &net.TCPAddr{IP: net.ParseIP(ip), Port: 25}}
}

func TestIsLoopbackConn(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		conn net.Conn
		want bool
	}{
		{"nil conn", nil, false},
		{"nil remote address", peerConn{}, false},
		{"IPv4 loopback", tcpPeer("127.0.0.1"), true},
		{"other 127/8 address", tcpPeer("127.10.20.30"), true},
		{"IPv6 loopback", tcpPeer("::1"), true},
		{"IPv4-mapped loopback", tcpPeer("::ffff:127.0.0.1"), true},
		// Loopback embedded in other IPv6 forms is not loopback to
		// the kernel either, so it must fail closed.
		{"IPv4-compatible loopback", tcpPeer("::127.0.0.1"), false},
		{"NAT64 loopback", tcpPeer("64:ff9b::7f00:1"), false},
		{"public IPv4", tcpPeer("192.0.2.25"), false},
		{"public IPv6", tcpPeer("2001:db8::25"), false},
		{"private IPv4", tcpPeer("10.0.0.5"), false},
		{"unspecified", tcpPeer("0.0.0.0"), false},
		{"TCP address without IP", peerConn{remote: &net.TCPAddr{}}, false},
		{"non-TCP host and port", peerConn{remote: stringAddr("127.0.0.1:25")}, true},
		{"non-TCP bare address", peerConn{remote: stringAddr("::1")}, true},
		{"non-TCP public address", peerConn{remote: stringAddr("192.0.2.25:25")}, false},
		{"non-TCP name", peerConn{remote: stringAddr("localhost:25")}, false},
		{"pipe", peerConn{remote: stringAddr("pipe")}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsLoopbackConn(tt.conn); got != tt.want {
				t.Errorf("IsLoopbackConn() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlainAuthStart(t *testing.T) {
	t.Parallel()
	wantResp := "\x00Test User\x00not-a-real-password"
	tests := []struct {
		name    string
		host    string
		peer    string
		server  smtp.ServerInfo
		wantErr error
	}{
		{"TLS to a public address", "mail.example.com", "192.0.2.25",
			smtp.ServerInfo{Name: "mail.example.com", TLS: true}, nil},
		{"plaintext to a loopback address", "mail.example.com", "127.0.0.1",
			smtp.ServerInfo{Name: "mail.example.com"}, nil},
		{"plaintext to IPv6 loopback", "::1", "::1",
			smtp.ServerInfo{Name: "::1"}, nil},
		{"plaintext to a public address", "mail.example.com", "192.0.2.25",
			smtp.ServerInfo{Name: "mail.example.com"}, ErrUnencryptedConnection},
		// The case net/smtp gets wrong: the name says localhost, but
		// the address connected to is not loopback.
		{"plaintext to localhost resolved elsewhere", "localhost", "192.0.2.25",
			smtp.ServerInfo{Name: "localhost"}, ErrUnencryptedConnection},
		{"plaintext to 127.0.0.1 named but connected elsewhere", "127.0.0.1", "192.0.2.25",
			smtp.ServerInfo{Name: "127.0.0.1"}, ErrUnencryptedConnection},
		{"wrong host name over TLS", "mail.example.com", "192.0.2.25",
			smtp.ServerInfo{Name: "other.example.com", TLS: true}, errWrongHostName},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := PlainAuth("", "Test User", "not-a-real-password", tt.host, tcpPeer(tt.peer))
			proto, resp, err := a.Start(&tt.server)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Start() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if resp != nil {
					t.Errorf("Start() returned a response %q with an error", resp)
				}
				return
			}
			if proto != "PLAIN" || string(resp) != wantResp {
				t.Errorf("Start() = %q, %q; want PLAIN, %q", proto, resp, wantResp)
			}
		})
	}
}

func TestPlainAuthNilConn(t *testing.T) {
	t.Parallel()
	a := PlainAuth("", "Test User", "not-a-real-password", "localhost", nil)
	if _, _, err := a.Start(&smtp.ServerInfo{Name: "localhost"}); !errors.Is(err, ErrUnencryptedConnection) {
		t.Fatalf("Start() error = %v, want %v", err, ErrUnencryptedConnection)
	}
}

func TestPlainAuthNext(t *testing.T) {
	t.Parallel()
	a := PlainAuth("", "Test User", "not-a-real-password", "localhost", tcpPeer("127.0.0.1"))
	if resp, err := a.Next(nil, false); resp != nil || err != nil {
		t.Errorf("Next(more=false) = %q, %v; want nil, nil", resp, err)
	}
	if _, err := a.Next([]byte("challenge"), true); !errors.Is(err, errUnexpectedChallenge) {
		t.Errorf("Next(more=true) error = %v, want %v", err, errUnexpectedChallenge)
	}
}
