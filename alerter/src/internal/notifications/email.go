/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package notifications

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/smtp"
	"strings"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// emailNotifier implements Notifier for SMTP email
type emailNotifier struct {
	secrets  SecretManager
	renderer TemplateRenderer
}

// NewEmailNotifier creates a new email notifier
func NewEmailNotifier(secrets SecretManager, renderer TemplateRenderer) Notifier {
	return &emailNotifier{
		secrets:  secrets,
		renderer: renderer,
	}
}

// Type implements Notifier.Type
func (n *emailNotifier) Type() database.NotificationChannelType {
	return database.ChannelTypeEmail
}

// Validate implements Notifier.Validate
func (n *emailNotifier) Validate(channel *database.NotificationChannel) error {
	if channel.SMTPHost == nil || *channel.SMTPHost == "" {
		return fmt.Errorf("email channel requires SMTP host")
	}
	if channel.FromAddress == nil || *channel.FromAddress == "" {
		return fmt.Errorf("email channel requires from address")
	}
	return nil
}

// Send implements Notifier.Send
// The channel should have Recipients populated (fetched separately from email_recipients table)
func (n *emailNotifier) Send(ctx context.Context, channel *database.NotificationChannel, payload *database.NotificationPayload) error {
	if err := n.Validate(channel); err != nil {
		return err
	}

	// Check for recipients
	if len(channel.Recipients) == 0 {
		return fmt.Errorf("email channel has no recipients")
	}

	// Collect enabled recipient addresses
	var toAddresses []string
	for _, r := range channel.Recipients {
		if r.Enabled {
			toAddresses = append(toAddresses, r.EmailAddress)
		}
	}
	if len(toAddresses) == 0 {
		return fmt.Errorf("email channel has no enabled recipients")
	}

	// Select template based on notification type
	var template, defaultTemplate string
	switch payload.NotificationType {
	case string(database.NotificationTypeAlertFire):
		template = deref(channel.TemplateAlertFire)
		defaultTemplate = DefaultEmailAlertFireTemplate
	case string(database.NotificationTypeAlertClear):
		template = deref(channel.TemplateAlertClear)
		defaultTemplate = DefaultEmailAlertClearTemplate
	case string(database.NotificationTypeReminder):
		template = deref(channel.TemplateReminder)
		defaultTemplate = DefaultEmailReminderTemplate
	default:
		return fmt.Errorf("unknown notification type: %s", payload.NotificationType)
	}

	// Render template (HTML body)
	body, err := n.renderer.Render(template, payload, defaultTemplate)
	if err != nil {
		return fmt.Errorf("failed to render email template: %w", err)
	}

	// Build email subject based on notification type
	var subject string
	switch payload.NotificationType {
	case string(database.NotificationTypeAlertFire):
		subject = fmt.Sprintf("[%s] Alert: %s", strings.ToUpper(payload.Severity), payload.AlertTitle)
	case string(database.NotificationTypeAlertClear):
		subject = fmt.Sprintf("[RESOLVED] %s", payload.AlertTitle)
	case string(database.NotificationTypeReminder):
		subject = fmt.Sprintf("[REMINDER #%d] %s", payload.ReminderCount, payload.AlertTitle)
	}

	// Build SMTP connection parameters
	smtpHost := *channel.SMTPHost
	smtpPort := channel.SMTPPort
	if smtpPort == 0 {
		smtpPort = 587
	}
	addr := fmt.Sprintf("%s:%d", smtpHost, smtpPort)

	// Decrypt password if present
	var smtpPassword string
	if channel.SMTPPassword != nil && *channel.SMTPPassword != "" {
		decrypted, err := n.secrets.Decrypt(*channel.SMTPPassword)
		if err != nil {
			return fmt.Errorf("failed to decrypt SMTP password: %w", err)
		}
		smtpPassword = decrypted
	}

	// Setup authentication if credentials provided
	var auth smtp.Auth
	if channel.SMTPUsername != nil && *channel.SMTPUsername != "" && smtpPassword != "" {
		auth = smtp.PlainAuth("", *channel.SMTPUsername, smtpPassword, smtpHost)
	}

	// Build from address with optional display name
	from := *channel.FromAddress
	fromHeader := from
	if channel.FromName != nil && *channel.FromName != "" {
		fromHeader = fmt.Sprintf("%s <%s>", *channel.FromName, from)
	}

	// Send email
	return n.sendEmail(ctx, addr, auth, from, fromHeader, toAddresses, subject, body, channel.SMTPUseTLS, smtpHost)
}

// sendEmail sends an HTML email via SMTP
func (n *emailNotifier) sendEmail(
	ctx context.Context,
	addr string,
	auth smtp.Auth,
	from string,
	fromHeader string,
	to []string,
	subject string,
	htmlBody string,
	useTLS bool,
	smtpHost string,
) error {
	// Build message with headers
	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("From: %s\r\n", fromHeader))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(to, ", ")))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(htmlBody)

	msgBytes := []byte(msg.String())

	if useTLS {
		return n.sendWithTLS(ctx, addr, auth, from, to, msgBytes, smtpHost)
	}
	return n.sendWithStartTLS(ctx, addr, auth, from, to, msgBytes, smtpHost)
}

// sendWithTLS sends email using implicit TLS (port 465)
func (n *emailNotifier) sendWithTLS(
	ctx context.Context,
	addr string,
	auth smtp.Auth,
	from string,
	to []string,
	msg []byte,
	smtpHost string,
) error {
	// Create TLS config
	tlsConfig := &tls.Config{
		ServerName: smtpHost,
		MinVersion: tls.VersionTLS12,
	}

	// Connect with TLS
	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("failed to connect with TLS: %w", err)
	}
	defer conn.Close()

	// Create SMTP client
	client, err := smtp.NewClient(conn, smtpHost)
	if err != nil {
		return fmt.Errorf("failed to create SMTP client: %w", err)
	}
	defer client.Close()

	// Authenticate if credentials provided
	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP authentication failed: %w", err)
		}
	}

	// Send the email
	return n.sendViaSMTPClient(client, from, to, msg)
}

// sendWithStartTLS sends email using STARTTLS (port 587 or 25)
func (n *emailNotifier) sendWithStartTLS(
	ctx context.Context,
	addr string,
	auth smtp.Auth,
	from string,
	to []string,
	msg []byte,
	smtpHost string,
) error {
	// Connect to SMTP server
	client, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}
	defer client.Close()

	// Say hello
	if err := client.Hello("localhost"); err != nil {
		return fmt.Errorf("SMTP HELLO failed: %w", err)
	}

	// Check if STARTTLS is supported and use it
	if ok, _ := client.Extension("STARTTLS"); ok {
		tlsConfig := &tls.Config{
			ServerName: smtpHost,
			MinVersion: tls.VersionTLS12,
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("STARTTLS failed: %w", err)
		}
	}

	// Authenticate if credentials provided
	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP authentication failed: %w", err)
		}
	}

	// Send the email
	return n.sendViaSMTPClient(client, from, to, msg)
}

// sendViaSMTPClient sends the email using an established SMTP client
func (n *emailNotifier) sendViaSMTPClient(client *smtp.Client, from string, to []string, msg []byte) error {
	// Set sender
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("SMTP MAIL FROM failed: %w", err)
	}

	// Set recipients
	for _, addr := range to {
		if err := client.Rcpt(addr); err != nil {
			return fmt.Errorf("SMTP RCPT TO failed for %s: %w", addr, err)
		}
	}

	// Send message body
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

	// Quit cleanly
	if err := client.Quit(); err != nil {
		// Log but don't fail - the email was sent
		return nil
	}

	return nil
}

// deref safely dereferences a string pointer, returning empty string if nil
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
