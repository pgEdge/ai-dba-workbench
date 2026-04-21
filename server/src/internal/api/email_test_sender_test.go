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
	"strings"
	"testing"
)

func TestSanitizeSMTPHeader(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty", "", ""},
		{"plain address", "user@example.com", "user@example.com"},
		{"with display name", "Alice <alice@example.com>", "Alice <alice@example.com>"},
		{"strips CR", "user@example.com\rInjected", "user@example.comInjected"},
		{"strips LF", "user@example.com\nInjected", "user@example.comInjected"},
		{"strips CRLF", "user@example.com\r\nInjected", "user@example.comInjected"},
		{
			name:     "command injection attempt",
			input:    "user@example.com\r\nRCPT TO:<evil@attacker.com>",
			expected: "user@example.comRCPT TO:<evil@attacker.com>",
		},
		{
			name:     "bare newline injection",
			input:    "alice@example.com\nBcc: evil@attacker.com",
			expected: "alice@example.comBcc: evil@attacker.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeSMTPHeader(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeSMTPHeader(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestBuildTestEmailMessage(t *testing.T) {
	from := "Alice <alice@example.com>"
	to := []string{"bob@example.com", "carol@example.com"}

	msg := string(buildTestEmailMessage(from, to))

	if !strings.Contains(msg, "From: Alice <alice@example.com>\r\n") {
		t.Errorf("expected From header, got: %q", msg)
	}
	if !strings.Contains(msg, "To: bob@example.com, carol@example.com\r\n") {
		t.Errorf("expected To header, got: %q", msg)
	}
	if !strings.Contains(msg, "Subject: AI DBA Workbench - Test Email\r\n") {
		t.Errorf("expected Subject header, got: %q", msg)
	}
	if !strings.Contains(msg, "MIME-Version: 1.0\r\n") {
		t.Errorf("expected MIME-Version, got: %q", msg)
	}
	if !strings.Contains(msg, "Content-Type: text/html; charset=\"UTF-8\"\r\n") {
		t.Errorf("expected Content-Type, got: %q", msg)
	}
	if !strings.Contains(msg, "<html><body>") {
		t.Errorf("expected HTML body, got: %q", msg)
	}
}

func TestBuildTestEmailMessage_SanitizesInjectionAttempts(t *testing.T) {
	// Attacker-controlled addresses with CRLF should have control
	// characters stripped before writing headers to prevent header
	// injection. buildTestEmailMessage does not itself sanitize, but
	// its callers pass values through sanitizeSMTPHeader first; verify
	// that the sanitizer on its own removes the dangerous sequences so
	// that any subsequent writer cannot inject a new header line.
	rawFrom := "Alice\r\nBcc: attacker@evil.com <alice@example.com>"
	rawTo := "bob@example.com\r\nBcc: other-attacker@evil.com"

	safeFrom := sanitizeSMTPHeader(rawFrom)
	safeTo := sanitizeSMTPHeader(rawTo)

	if strings.ContainsAny(safeFrom, "\r\n") {
		t.Errorf("sanitized From still contains CR/LF: %q", safeFrom)
	}
	if strings.ContainsAny(safeTo, "\r\n") {
		t.Errorf("sanitized To still contains CR/LF: %q", safeTo)
	}

	msg := string(buildTestEmailMessage(safeFrom, []string{safeTo}))
	// The only \r\n sequences in the resulting message must be the
	// legitimate header separators; a raw injected header line would
	// appear as "Bcc:..." immediately after a \r\n sequence.
	if strings.Contains(msg, "\r\nBcc:") {
		t.Errorf("injected Bcc header appeared after CRLF: %q", msg)
	}
}
