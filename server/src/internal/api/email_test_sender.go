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
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// sendTestEmail sends a test email via SMTP to verify channel configuration.
// It mirrors the alerter's SMTP logic but is self-contained with no
// external dependencies beyond the standard library.
func sendTestEmail(
	smtpHost string,
	smtpPort int,
	smtpUsername string,
	smtpPassword string,
	useTLS bool,
	fromAddress string,
	fromName string,
	toAddresses []string,
) error {
	addr := fmt.Sprintf("%s:%d", smtpHost, smtpPort)

	// Setup authentication if credentials provided
	var auth smtp.Auth
	if smtpUsername != "" && smtpPassword != "" {
		auth = smtp.PlainAuth("", smtpUsername, smtpPassword, smtpHost)
	}

	// Build From header with optional display name
	fromHeader := fromAddress
	if fromName != "" {
		fromHeader = fmt.Sprintf("%s <%s>", fromName, fromAddress)
	}

	// Build the test email message
	msg := buildTestEmailMessage(fromHeader, toAddresses)

	// Select the sending strategy based on port and TLS setting
	if smtpPort == 465 && useTLS {
		return sendWithImplicitTLS(addr, auth, fromAddress, toAddresses, msg, smtpHost)
	}
	if useTLS {
		return sendWithSTARTTLS(addr, auth, fromAddress, toAddresses, msg, smtpHost)
	}
	return sendPlainSMTP(addr, auth, fromAddress, toAddresses, msg, smtpHost)
}

// buildTestEmailMessage constructs the MIME headers and HTML body for the
// test email.
func buildTestEmailMessage(fromHeader string, toAddresses []string) []byte {
	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("From: %s\r\n", fromHeader))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(toAddresses, ", ")))
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
func sendWithImplicitTLS(
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
	}

	dialer := &net.Dialer{Timeout: 30 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("failed to connect with TLS: %w", err)
	}
	defer conn.Close()

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

// sendWithSTARTTLS sends email using STARTTLS (typically port 587 or 25).
func sendWithSTARTTLS(
	addr string,
	auth smtp.Auth,
	from string,
	to []string,
	msg []byte,
	smtpHost string,
) error {
	conn, err := net.DialTimeout("tcp", addr, 30*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}

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

// sendPlainSMTP sends email over a plain (unencrypted) SMTP connection.
func sendPlainSMTP(
	addr string,
	auth smtp.Auth,
	from string,
	to []string,
	msg []byte,
	smtpHost string,
) error {
	conn, err := net.DialTimeout("tcp", addr, 30*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}

	client, err := smtp.NewClient(conn, smtpHost)
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to create SMTP client: %w", err)
	}
	defer client.Close()

	if err := client.Hello("localhost"); err != nil {
		return fmt.Errorf("SMTP HELLO failed: %w", err)
	}

	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP authentication failed: %w", err)
		}
	}

	return sendViaSMTPClient(client, from, to, msg)
}

// sendViaSMTPClient sends the email using an established SMTP client.
func sendViaSMTPClient(client *smtp.Client, from string, to []string, msg []byte) error {
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("SMTP MAIL FROM failed: %w", err)
	}

	for _, addr := range to {
		if err := client.Rcpt(addr); err != nil {
			return fmt.Errorf("SMTP RCPT TO failed for %s: %w", addr, err)
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
