/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package notifications

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"
)

// This file is duplicated between
// alerter/src/internal/notifications/sanitize_test.go and
// server/src/internal/api/sanitize_test.go, for the same reason
// sanitize.go itself is: the alerter and the server are separate Go
// modules. Apart from the package clause the two copies must stay
// character-for-character identical.

// webhookSecretPath is the path of a plausible Slack incoming-webhook
// URL. For Slack and Mattermost the whole URL is the credential, so
// this string is what must never survive redaction.
const webhookSecretPath = "/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX"

func TestRedactURLPath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "slack url in a transport error",
			in: `Post "https://hooks.slack.com` + webhookSecretPath +
				`": dial tcp 1.2.3.4:443: connect: connection refused`,
			want: `Post "https://hooks.slack.com<redacted> dial tcp 1.2.3.4:443: ` +
				`connect: connection refused`,
		},
		{
			name: "mattermost url in a transport error",
			in: `Post "https://mattermost.example.com/hooks/abcdefghijklmnopqrstuvwxyz": ` +
				`context deadline exceeded`,
			want: `Post "https://mattermost.example.com<redacted> context deadline exceeded`,
		},
		{
			name: "query string is redacted with the path",
			in:   "https://webhook.example.com/notify?token=s3cr3t&channel=ops failed",
			want: "https://webhook.example.com<redacted> failed",
		},
		{
			name: "fragment is redacted with the path",
			in:   "https://webhook.example.com#s3cr3t",
			want: "https://webhook.example.com<redacted>",
		},
		{
			name: "host with no path is left alone",
			in:   `Post "https://hooks.slack.com": dial tcp: lookup failed`,
			want: `Post "https://hooks.slack.com": dial tcp: lookup failed`,
		},
		{
			name: "bare host with no scheme is left alone",
			in:   "hooks.slack.com/services/T0/B0/XYZ unreachable",
			want: "hooks.slack.com/services/T0/B0/XYZ unreachable",
		},
		{
			name: "several urls in one string",
			in:   "tried https://a.example.com/one then https://b.example.com/two",
			want: "tried https://a.example.com<redacted> then https://b.example.com<redacted>",
		},
		{
			name: "a double quote inside the path does not end the run",
			in:   `Post "https://webhook.example.com/hooks/ab"cd/ef": EOF`,
			want: `Post "https://webhook.example.com<redacted> EOF`,
		},
		{
			name: "a closing paren inside the path does not end the run",
			in:   "(https://webhook.example.com/hooks/a(b)c)",
			want: "(https://webhook.example.com<redacted>",
		},
		{
			name: "a control character inside the url does not end the host",
			in:   "parse \"http://webhook.example.com\x00/hooks/secret\": invalid",
			want: "parse \"http://webhook.example.com\x00<redacted> invalid",
		},
		{
			name: "a newline ends the run",
			in:   "https://webhook.example.com/hooks/secret\nnext line",
			want: "https://webhook.example.com<redacted>\nnext line",
		},
		{
			name: "a trailing slash is redacted",
			in:   "https://webhook.example.com/",
			want: "https://webhook.example.com<redacted>",
		},
		{
			name: "scheme with nothing after it",
			in:   "https://",
			want: "https://",
		},
		{
			name: "a bare separator",
			in:   "://",
			want: "://",
		},
		{
			name: "empty",
			in:   "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := redactURLPath(tt.in); got != tt.want {
				t.Errorf("redactURLPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// webhookRedactionContexts are the shapes a webhook URL actually turns
// up in when it escapes into text. Each carries one or more %s
// placeholders for the secret path.
var webhookRedactionContexts = []struct {
	name   string
	format string
}{
	{"url error", `Post "https://hooks.slack.com%s": dial tcp: i/o timeout`},
	{"bare url", "https://hooks.slack.com%s"},
	{"url in prose", "proxy rejected https://hooks.slack.com%s after 3 tries"},
	{"parenthesised", "(https://hooks.slack.com%s)"},
	{"bracketed", "[https://hooks.slack.com%s]"},
	{"angle bracketed", "<https://hooks.slack.com%s>"},
	{"single quoted", "'https://hooks.slack.com%s'"},
	{"backquoted", "`https://hooks.slack.com%s`"},
	{"comma separated", "tried https://hooks.slack.com%s, giving up"},
	{"semicolon separated", "url=https://hooks.slack.com%s; retrying"},
	{"own line", "request failed\nhttps://hooks.slack.com%s\nretrying"},
	{"tab separated", "url\thttps://hooks.slack.com%s\tfailed"},
	{"twice", "https://hooks.slack.com%s then https://hooks.slack.com%s"},
}

// TestRedactURLPathNeverEmitsThePath is the property the redactor
// exists for: whatever surrounds a webhook URL, its path must not
// survive into the output. This is the guard against the redactor
// failing open on an input shape nobody anticipated.
func TestRedactURLPathNeverEmitsThePath(t *testing.T) {
	for _, c := range webhookRedactionContexts {
		t.Run(c.name, func(t *testing.T) {
			in := strings.ReplaceAll(c.format, "%s", webhookSecretPath)
			got := redactURLPath(in)
			if strings.Contains(got, "T00000000") || strings.Contains(got, "XXXXXXXX") {
				t.Errorf("redactURLPath(%q) = %q, want no part of the path", in, got)
			}
			if !strings.Contains(got, urlPathPlaceholder) {
				t.Errorf("redactURLPath(%q) = %q, want the placeholder", in, got)
			}
		})
	}
}

// TestRedactURLPathNeverEmitsAPathCharacter varies the path itself
// rather than the text around it, over every printable ASCII character
// a percent-decoded path could hold, so that no character can be
// mistaken for the end of the run.
func TestRedactURLPathNeverEmitsAPathCharacter(t *testing.T) {
	for c := byte('!'); c <= '~'; c++ {
		path := "/hooks/AA" + string(c) + "BBsecretCC"
		in := `Post "https://hooks.example.com` + path + `": EOF`
		got := redactURLPath(in)
		if strings.Contains(got, "secretCC") {
			t.Errorf("redactURLPath(%q) = %q, leaked the tail of the path", in, got)
		}
		if !strings.Contains(got, urlPathPlaceholder) {
			t.Errorf("redactURLPath(%q) = %q, want the placeholder", in, got)
		}
	}
}

func TestSanitizeWebhookEcho(t *testing.T) {
	t.Run("ordinary text is unchanged", func(t *testing.T) {
		if got := sanitizeWebhookEcho("connection refused"); got != "connection refused" {
			t.Errorf("sanitizeWebhookEcho() = %q", got)
		}
	})

	t.Run("a webhook path is redacted", func(t *testing.T) {
		in := "invalid_payload from https://hooks.slack.com" + webhookSecretPath
		got := sanitizeWebhookEcho(in)
		if strings.Contains(got, "T00000000") {
			t.Errorf("sanitizeWebhookEcho() = %q, want the path redacted", got)
		}
		if !strings.Contains(got, urlPathPlaceholder) {
			t.Errorf("sanitizeWebhookEcho() = %q, want the placeholder", got)
		}
	})

	t.Run("control characters become spaces", func(t *testing.T) {
		got := sanitizeWebhookEcho("first\n[ERROR] forged\r\tx\x00y\x7fz")
		if strings.ContainsAny(got, "\n\r\t\x00\x7f") {
			t.Errorf("sanitizeWebhookEcho() = %q, want no control characters", got)
		}
		if !strings.Contains(got, "forged") {
			t.Errorf("sanitizeWebhookEcho() = %q", got)
		}
	})

	t.Run("a long body is capped", func(t *testing.T) {
		got := sanitizeWebhookEcho(strings.Repeat("A", 100000))
		if len(got) > maxEchoedBytes+len(echoTruncationMarker) {
			t.Errorf("sanitizeWebhookEcho() returned %d bytes, want at most %d",
				len(got), maxEchoedBytes+len(echoTruncationMarker))
		}
		if !strings.HasSuffix(got, echoTruncationMarker) {
			t.Errorf("sanitizeWebhookEcho() = %q, want the truncation marker", got)
		}
	})

	t.Run("the cap does not split a rune", func(t *testing.T) {
		got := sanitizeWebhookEcho(strings.Repeat("中", 500))
		if !utf8.ValidString(got) {
			t.Error("sanitizeWebhookEcho() produced invalid UTF-8")
		}
	})

	t.Run("invalid utf-8 is replaced", func(t *testing.T) {
		got := sanitizeWebhookEcho("head\xff\xfetail")
		if !utf8.ValidString(got) {
			t.Errorf("sanitizeWebhookEcho() = %q, want valid UTF-8", got)
		}
	})

	t.Run("empty", func(t *testing.T) {
		if got := sanitizeWebhookEcho(""); got != "" {
			t.Errorf("sanitizeWebhookEcho() = %q, want empty", got)
		}
	})
}

func TestWebhookTransportError(t *testing.T) {
	t.Run("plain error", func(t *testing.T) {
		got := webhookTransportError(fmt.Errorf("something broke"))
		if got != "something broke" {
			t.Errorf("webhookTransportError() = %q, want %q", got, "something broke")
		}
	})

	t.Run("url error is reduced to op and cause", func(t *testing.T) {
		urlErr := &url.Error{
			Op:  "Post",
			URL: "https://hooks.slack.com" + webhookSecretPath,
			Err: fmt.Errorf("dial tcp: connection refused"),
		}
		got := webhookTransportError(urlErr)
		if strings.Contains(got, "T00000000") || strings.Contains(got, "services") {
			t.Errorf("webhookTransportError() = %q, want no webhook path", got)
		}
		if !strings.Contains(got, "Post request failed") {
			t.Errorf("webhookTransportError() = %q, want it to name the operation", got)
		}
		if !strings.Contains(got, "connection refused") {
			t.Errorf("webhookTransportError() = %q, want it to carry the cause", got)
		}
	})

	t.Run("url error with a nil cause falls back", func(t *testing.T) {
		urlErr := &url.Error{
			Op:  "Post",
			URL: "https://hooks.slack.com" + webhookSecretPath,
		}
		got := webhookTransportError(urlErr)
		if strings.Contains(got, "T00000000") || strings.Contains(got, "services") {
			t.Errorf("webhookTransportError() = %q, want no webhook path", got)
		}
	})

	t.Run("a nested url error inside a plain error is redacted", func(t *testing.T) {
		got := webhookTransportError(fmt.Errorf(
			"giving up: https://hooks.slack.com%s", webhookSecretPath))
		if strings.Contains(got, "T00000000") {
			t.Errorf("webhookTransportError() = %q, want no webhook path", got)
		}
	})
}
