/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package smtpauth provides the SMTP PLAIN authentication that the
// alerter's email notifier and the server's test send share.
//
// net/smtp's own PlainAuth sends credentials over an unencrypted
// connection whenever the server name is exactly "localhost",
// "127.0.0.1" or "::1". It judges by the name alone, and Go's resolver
// does not pin "localhost" to loopback: it takes the name from
// /etc/hosts and then from DNS, so a changed /etc/hosts, or a host with
// no localhost entry whose DNS answers for the name, could send the
// credentials off the machine in the clear (issue #843).
//
// PlainAuth here judges by the connection instead: it sends the
// credentials when the session is TLS, or when the address actually
// connected to is a loopback address, so traffic cannot leave the host. The
// host name plays no part in that decision.
package smtpauth

import (
	"errors"
	"net"
	"net/smtp"
)

// ErrUnencryptedConnection is returned, before any credentials are
// sent, when PLAIN authentication is attempted over a connection that
// is neither TLS nor to a loopback address.
var ErrUnencryptedConnection = errors.New(
	"refusing to send SMTP credentials over an unencrypted connection to a non-loopback address")

// errWrongHostName mirrors net/smtp's check that the client is talking
// to the host the credentials were meant for.
var errWrongHostName = errors.New("wrong host name")

// errUnexpectedChallenge mirrors net/smtp: PLAIN sends everything in
// its initial response, so a further challenge is a protocol error.
var errUnexpectedChallenge = errors.New("unexpected server challenge")

type plainAuth struct {
	identity, username, password string
	host                         string
	loopback                     bool
}

// PlainAuth returns an smtp.Auth implementing the PLAIN mechanism (RFC
// 4616) for host, as net/smtp's PlainAuth does, except that without TLS
// it sends the credentials only when conn's remote address is a
// loopback address. conn is the connection the SMTP client was created
// on, as returned by the dialer; for an implicit TLS session it may be
// the raw connection or the TLS one, since both report the same remote
// address. A nil conn counts as not loopback.
func PlainAuth(identity, username, password, host string, conn net.Conn) smtp.Auth {
	return &plainAuth{
		identity: identity,
		username: username,
		password: password,
		host:     host,
		loopback: IsLoopbackConn(conn),
	}
}

// Start implements smtp.Auth. Nothing in ServerInfo can be trusted
// without TLS, including the advertised mechanisms, so the decision
// rests only on server.TLS and the address connected to.
func (a *plainAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if !server.TLS && !a.loopback {
		return "", nil, ErrUnencryptedConnection
	}
	if server.Name != a.host {
		return "", nil, errWrongHostName
	}
	resp := []byte(a.identity + "\x00" + a.username + "\x00" + a.password)
	return "PLAIN", resp, nil
}

// Next implements smtp.Auth.
func (a *plainAuth) Next(_ []byte, more bool) ([]byte, error) {
	if more {
		return nil, errUnexpectedChallenge
	}
	return nil, nil
}

// IsLoopbackConn reports whether conn's remote address is a loopback
// address (127.0.0.0/8, ::1, or an IPv4-mapped form of either). It
// looks only at the address connected to, never at a host name, and
// reports false for a nil conn, a nil remote address, or one that is
// not an IP address.
func IsLoopbackConn(conn net.Conn) bool {
	if conn == nil {
		return false
	}
	addr := conn.RemoteAddr()
	if addr == nil {
		return false
	}
	var ip net.IP
	switch a := addr.(type) {
	case *net.TCPAddr:
		ip = a.IP
	default:
		host, _, err := net.SplitHostPort(addr.String())
		if err != nil {
			host = addr.String()
		}
		ip = net.ParseIP(host)
	}
	return ip != nil && ip.IsLoopback()
}
