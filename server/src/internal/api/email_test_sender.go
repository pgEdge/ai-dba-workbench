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
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"

	"github.com/pgedge/ai-workbench/pkg/hostvalidation"
)

// sanitizeSMTPHeader removes CR/LF characters from an SMTP header or
// envelope value. Stripping control characters prevents SMTP command and
// MIME header injection when user-supplied values are written to the
// wire.
func sanitizeSMTPHeader(s string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(s)
}

// testEmail describes one test email: the SMTP server to send it
// through, the credentials to authenticate with, and the envelope.
// Empty Username or Password means no authentication; empty FromName
// means a bare From address.
type testEmail struct {
	Host        string
	Port        int
	Username    string
	Password    string
	UseTLS      bool
	FromAddress string
	FromName    string
	To          []string
}

// sendTestEmail sends a test email via SMTP to verify channel configuration.
// It mirrors the alerter's SMTP logic, including connecting through the
// guarded dialer, so the address policy holds on the address actually
// connected to. When the guard refuses every address the SMTP host
// resolves to, the error wraps hostvalidation.ErrNoPermittedAddress.
//
// The whole exchange is bounded by testSendTimeout: an SMTP server that
// accepts the connection and then stalls cannot hold the request open.
func (s *notificationTestSender) sendTestEmail(ctx context.Context, e testEmail) error {
	ctx, cancel := context.WithTimeout(ctx, testSendTimeout)
	defer cancel()

	// An IPv6 literal may be stored with one bracket pair; the TLS
	// server name and JoinHostPort both need it bare, as the alerter's
	// bareSMTPHost does.
	smtpHost := hostvalidation.BareHost(e.Host)
	fromAddress := e.FromAddress
	toAddresses := e.To

	// JoinHostPort brackets an IPv6 literal, which the guarded dialer
	// needs to split the address again.
	addr := net.JoinHostPort(smtpHost, strconv.Itoa(e.Port))

	// Setup authentication if credentials provided
	var auth smtp.Auth
	if e.Username != "" && e.Password != "" {
		auth = smtp.PlainAuth("", e.Username, e.Password, smtpHost)
	}

	// Build From header with optional display name
	fromHeader := fromAddress
	if e.FromName != "" {
		fromHeader = fmt.Sprintf("%s <%s>", e.FromName, fromAddress)
	}

	// Build the test email message
	msg := buildTestEmailMessage(fromHeader, toAddresses)

	// As in the alerter, port 465 with TLS on uses implicit TLS, and
	// every other combination upgrades with STARTTLS when the server
	// advertises it, whether or not TLS is turned on.
	if e.Port == 465 && e.UseTLS {
		return s.sendWithImplicitTLS(ctx, addr, auth, fromAddress, toAddresses, msg, smtpHost)
	}
	return s.sendWithSTARTTLS(ctx, addr, auth, fromAddress, toAddresses, msg, smtpHost)
}

// dialSMTP connects to addr through the guarded dialer and ties the
// connection to ctx: its read and write deadline is ctx's deadline, and
// it is closed as soon as ctx is done, so a server that stops answering
// part-way through the exchange fails the send instead of blocking it.
// The caller must call the returned release function when it has
// finished with the connection.
func (s *notificationTestSender) dialSMTP(ctx context.Context, addr string) (net.Conn, func(), error) {
	conn, err := s.dial(ctx, "tcp", addr)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to SMTP server: %w", err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			_ = conn.Close() //nolint:errcheck // already failing
			return nil, nil, fmt.Errorf("failed to connect to SMTP server: %w", err)
		}
	}
	stop := context.AfterFunc(ctx, func() {
		_ = conn.Close() //nolint:errcheck // unblocks a stalled read or write
	})
	return conn, func() { stop() }, nil
}

// buildTestEmailMessage constructs the MIME headers and HTML body for the
// test email.
func buildTestEmailMessage(fromHeader string, toAddresses []string) []byte {
	var msg strings.Builder
	fmt.Fprintf(&msg, "From: %s\r\n", sanitizeSMTPHeader(fromHeader))
	sanitizedTo := make([]string, len(toAddresses))
	for i, addr := range toAddresses {
		sanitizedTo[i] = sanitizeSMTPHeader(addr)
	}
	fmt.Fprintf(&msg, "To: %s\r\n", strings.Join(sanitizedTo, ", "))
	msg.WriteString("Subject: AI DBA Workbench - Test Email\r\n")
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n")
	msg.WriteString("\r\n")
	msg.WriteString("<html><body>")
	msg.WriteString("<p>This is a test email sent from the AI DBA Workbench ")
	msg.WriteString("to verify your SMTP configuration.</p>")
	msg.WriteString("</body></html>")

	return []byte(msg.String())
}

// sendWithImplicitTLS sends email using implicit TLS (port 465).
func (s *notificationTestSender) sendWithImplicitTLS(
	ctx context.Context,
	addr string,
	auth smtp.Auth,
	from string,
	to []string,
	msg []byte,
	smtpHost string,
) error {
	tlsConfig := &tls.Config{
		ServerName: smtpHost,
		MinVersion: tls.VersionTLS12,
		RootCAs:    s.rootCAs,
	}

	// Connect through the guarded dialer, then wrap with TLS
	rawConn, release, err := s.dialSMTP(ctx, addr)
	if err != nil {
		return err
	}
	defer release()
	conn := tls.Client(rawConn, tlsConfig)
	defer conn.Close()
	if err := conn.HandshakeContext(ctx); err != nil {
		return fmt.Errorf("TLS handshake failed: %w", err)
	}

	client, err := smtp.NewClient(conn, smtpHost)
	if err != nil {
		return fmt.Errorf("failed to create SMTP client: %w", err)
	}
	defer client.Close()

	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP authentication failed: %w", err)
		}
	}

	return sendViaSMTPClient(client, from, to, msg)
}

// sendWithSTARTTLS sends email over a plain connection that is upgraded
// with STARTTLS when the server advertises it (typically port 587 or 25).
func (s *notificationTestSender) sendWithSTARTTLS(
	ctx context.Context,
	addr string,
	auth smtp.Auth,
	from string,
	to []string,
	msg []byte,
	smtpHost string,
) error {
	// Connect to the SMTP server through the guarded dialer
	conn, release, err := s.dialSMTP(ctx, addr)
	if err != nil {
		return err
	}
	defer release()

	client, err := smtp.NewClient(conn, smtpHost)
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to create SMTP client: %w", err)
	}
	defer client.Close()

	if err := client.Hello("localhost"); err != nil {
		return fmt.Errorf("SMTP HELLO failed: %w", err)
	}

	if ok, _ := client.Extension("STARTTLS"); ok {
		tlsConfig := &tls.Config{
			ServerName: smtpHost,
			MinVersion: tls.VersionTLS12,
			RootCAs:    s.rootCAs,
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("STARTTLS failed: %w", err)
		}
	}

	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP authentication failed: %w", err)
		}
	}

	return sendViaSMTPClient(client, from, to, msg)
}

// sendViaSMTPClient sends the email using an established SMTP client.
// SMTP envelope addresses are sanitized before being issued as commands
// to prevent SMTP command injection via CR/LF sequences.
func sendViaSMTPClient(client *smtp.Client, from string, to []string, msg []byte) error {
	if err := client.Mail(sanitizeSMTPHeader(from)); err != nil { //nolint:gosec // G707: address sanitized via sanitizeSMTPHeader
		return fmt.Errorf("SMTP MAIL FROM failed: %w", err)
	}

	for _, addr := range to {
		safeAddr := sanitizeSMTPHeader(addr)
		if err := client.Rcpt(safeAddr); err != nil { //nolint:gosec // G707: address sanitized via sanitizeSMTPHeader
			return fmt.Errorf("SMTP RCPT TO failed for %s: %w", safeAddr, err)
		}
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA failed: %w", err)
	}

	_, err = w.Write(msg)
	if err != nil {
		return fmt.Errorf("failed to write email body: %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("failed to close email body: %w", err)
	}

	if err := client.Quit(); err != nil {
		// The email was sent; a quit error is not a delivery failure
		return nil
	}

	return nil
}
