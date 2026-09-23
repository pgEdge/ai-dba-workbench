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
			// A documented limit, not a wanted behavior: without a
			// scheme there is nothing to anchor the host on. Nothing
			// reaches the redactor in this shape, because the senders
			// never echo a url.Parse failure, which is the only thing
			// that renders a raw stored URL. See redactURLPath.
			name: "bare host with no scheme is left alone",
			in:   "hooks.slack.com/services/T0/B0/XYZ unreachable",
			want: "hooks.slack.com/services/T0/B0/XYZ unreachable",
		},
		{
			// The other documented limit, kept honest for the same
			// reason: whitespace inside a stored path ends the run.
			name: "whitespace inside the path ends the run early",
			in:   "https://hooks.example.com/services/T0 B0/XYZ",
			want: "https://hooks.example.com<redacted> B0/XYZ",
		},
		{
			name: "userinfo is replaced and the host kept",
			in:   `Post "https://svc:hunter2@hooks.example.com/services/T0/B0": EOF`,
			want: `Post "https://<redacted>@hooks.example.com<redacted> EOF`,
		},
		{
			name: "userinfo is replaced when there is no path",
			in:   `Post "https://svc:hunter2@hooks.example.com": EOF`,
			want: `Post "https://<redacted>@hooks.example.com": EOF`,
		},
		{
			name: "an encoded at sign inside userinfo does not split it early",
			in:   "https://sv%40c:hunter2@hooks.example.com/services/T0",
			want: "https://<redacted>@hooks.example.com<redacted>",
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

// TestRedactURLPathNeverEmitsUserinfo is the property for the other
// half of the credential: the generic webhook channel supports basic
// authentication, so an operator may store the password in the URL.
func TestRedactURLPathNeverEmitsUserinfo(t *testing.T) {
	for _, c := range webhookRedactionContexts {
		t.Run(c.name, func(t *testing.T) {
			in := strings.ReplaceAll(c.format, "hooks.slack.com",
				"svc:hunter2@hooks.slack.com")
			in = strings.ReplaceAll(in, "%s", webhookSecretPath)
			got := redactURLPath(in)
			if strings.Contains(got, "hunter2") || strings.Contains(got, "svc:") {
				t.Errorf("redactURLPath(%q) = %q, want the userinfo replaced", in, got)
			}
			if !strings.Contains(got, "hooks.slack.com") {
				t.Errorf("redactURLPath(%q) = %q, want the host kept", in, got)
			}
		})
	}
}

func TestSanitizeWebhookEcho(t *testing.T) {
	t.Run("ordinary text is unchanged", func(t *testing.T) {
		if got := sanitizeWebhookEcho("connection refused", ""); got != "connection refused" {
			t.Errorf("sanitizeWebhookEcho() = %q", got)
		}
	})

	t.Run("a webhook path is redacted", func(t *testing.T) {
		in := "invalid_payload from https://hooks.slack.com" + webhookSecretPath
		got := sanitizeWebhookEcho(in, "")
		if strings.Contains(got, "T00000000") {
			t.Errorf("sanitizeWebhookEcho() = %q, want the path redacted", got)
		}
		if !strings.Contains(got, urlPathPlaceholder) {
			t.Errorf("sanitizeWebhookEcho() = %q, want the placeholder", got)
		}
	})

	t.Run("control characters are removed", func(t *testing.T) {
		got := sanitizeWebhookEcho("first\n[ERROR] forged\r\tx\x00y\x7fz", "")
		if strings.ContainsAny(got, "\n\r\t\x00\x7f") {
			t.Errorf("sanitizeWebhookEcho() = %q, want no control characters", got)
		}
		if !strings.Contains(got, "forged") {
			t.Errorf("sanitizeWebhookEcho() = %q", got)
		}
	})

	// A response body is wholly attacker-controlled, and these are the
	// characters beyond the ASCII controls that a log processor treats
	// as a line ending or that reorder what a reader sees.
	t.Run("c1, unicode separators and format characters are removed", func(t *testing.T) {
		for _, r := range []rune{'\u0085', '\u2028', '\u2029', '\u009b',
			'\u202e', '\u200b', '\u00ad'} {
			in := "head" + string(r) + "tail"
			got := sanitizeWebhookEcho(in, "")
			if strings.ContainsRune(got, r) {
				t.Errorf("sanitizeWebhookEcho(%q) = %q, want %U removed",
					in, got, r)
			}
			if !strings.Contains(got, "head") || !strings.Contains(got, "tail") {
				t.Errorf("sanitizeWebhookEcho(%q) = %q, want the text kept", in, got)
			}
		}
	})

	t.Run("a long body is capped", func(t *testing.T) {
		got := sanitizeWebhookEcho(strings.Repeat("A", 100000), "")
		if len(got) > maxEchoedBytes+len(echoTruncationMarker) {
			t.Errorf("sanitizeWebhookEcho() returned %d bytes, want at most %d",
				len(got), maxEchoedBytes+len(echoTruncationMarker))
		}
		if !strings.HasSuffix(got, echoTruncationMarker) {
			t.Errorf("sanitizeWebhookEcho() = %q, want the truncation marker", got)
		}
	})

	t.Run("the cap does not split a rune", func(t *testing.T) {
		got := sanitizeWebhookEcho(strings.Repeat("中", 500), "")
		if !utf8.ValidString(got) {
			t.Error("sanitizeWebhookEcho() produced invalid UTF-8")
		}
	})

	t.Run("invalid utf-8 is replaced", func(t *testing.T) {
		got := sanitizeWebhookEcho("head\xff\xfetail", "")
		if !utf8.ValidString(got) {
			t.Errorf("sanitizeWebhookEcho() = %q, want valid UTF-8", got)
		}
	})

	t.Run("empty", func(t *testing.T) {
		if got := sanitizeWebhookEcho("", ""); got != "" {
			t.Errorf("sanitizeWebhookEcho() = %q, want empty", got)
		}
	})
}

func TestWebhookTransportError(t *testing.T) {
	t.Run("plain error", func(t *testing.T) {
		got := webhookTransportError(fmt.Errorf("something broke"), "")
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
		got := webhookTransportError(urlErr, "")
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
		got := webhookTransportError(urlErr, "")
		if strings.Contains(got, "T00000000") || strings.Contains(got, "services") {
			t.Errorf("webhookTransportError() = %q, want no webhook path", got)
		}
	})

	t.Run("a nested url error inside a plain error is redacted", func(t *testing.T) {
		got := webhookTransportError(fmt.Errorf(
			"giving up: https://hooks.slack.com%s", webhookSecretPath), "")
		if strings.Contains(got, "T00000000") {
			t.Errorf("webhookTransportError() = %q, want no webhook path", got)
		}
	})
}

// TestSanitizeEcho_FoldRunsBeforeRedaction pins the order of the two
// steps: a control character inside the scheme separator must not hide
// the URL from redactURLPath and then be folded away afterwards, which
// would hand back a legible, unredacted URL.
func TestSanitizeEcho_FoldRunsBeforeRedaction(t *testing.T) {
	for _, sep := range []string{"\x00", "\n", "\u200b", "\u2028", "\u0085"} {
		in := "error: https:" + sep + "//hooks.slack.com" + webhookSecretPath
		got := sanitizeWebhookEcho(in, "")
		if strings.Contains(got, "T00000000") || strings.Contains(got, "XXXXXXXX") {
			t.Errorf("sanitizeWebhookEcho(%q) = %q, want the path redacted", in, got)
		}
		if !strings.Contains(got, "https://hooks.slack.com") {
			t.Errorf("sanitizeWebhookEcho(%q) = %q, want the host kept", in, got)
		}
	}
}

func TestSanitizeConfigEcho(t *testing.T) {
	if got := sanitizeConfigEcho("DELETE"); got != "DELETE" {
		t.Errorf("sanitizeConfigEcho() = %q, want it unchanged", got)
	}
	got := sanitizeConfigEcho("POST\n[ERROR] forged\r" + strings.Repeat("A", 1000))
	if strings.ContainsAny(got, "\n\r") {
		t.Errorf("sanitizeConfigEcho() = %q, want no line breaks", got)
	}
	if !strings.HasSuffix(got, echoTruncationMarker) {
		t.Errorf("sanitizeConfigEcho() = %q, want it capped", got)
	}
}

// TestWebhookRedactor_SchemelessEchoOfTheConfiguredURL is the case a
// syntax matcher cannot see: an endpoint or an intermediary answering
// with an error page that names the request path, or part of it, with
// no scheme in front.
func TestWebhookRedactor_SchemelessEchoOfTheConfiguredURL(t *testing.T) {
	webhookURL := "https://hooks.slack.com" + webhookSecretPath
	tests := []struct {
		name string
		body string
	}{
		{"the whole path", "404 no route for " + webhookSecretPath},
		{"host and path", "cannot reach hooks.slack.com" + webhookSecretPath},
		{"a partial path", "unknown hook B00000000/XXXXXXXXXXXXXXXXXXXXXXXX"},
		{"the secret segment alone", `{"error":"XXXXXXXXXXXXXXXXXXXXXXXX revoked"}`},
		{"a percent-encoded path", "no route for %2Fservices%2FT00000000%2FB00000000"},
		{"the path split by a control character", "no route for /services/T000\x0000000/B00000000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeWebhookEcho(tt.body, webhookURL)
			for _, fragment := range []string{"T00000000", "B00000000", "XXXXXXXX"} {
				if strings.Contains(got, fragment) {
					t.Errorf("sanitizeWebhookEcho(%q) = %q repeats %q", tt.body, got, fragment)
				}
			}
		})
	}

	t.Run("the host survives", func(t *testing.T) {
		got := sanitizeWebhookEcho("cannot reach hooks.slack.com"+webhookSecretPath, webhookURL)
		if !strings.Contains(got, "hooks.slack.com") {
			t.Errorf("sanitizeWebhookEcho() = %q, want the host kept", got)
		}
	})

	t.Run("a transport error is redacted the same way", func(t *testing.T) {
		got := webhookTransportError(fmt.Errorf("proxy said: bad path %s", webhookSecretPath), webhookURL)
		if strings.Contains(got, "T00000000") {
			t.Errorf("webhookTransportError() = %q, want no webhook path", got)
		}
	})
}

func TestWebhookRedactor_QueryFragmentAndUserinfo(t *testing.T) {
	webhookURL := "https://svc:hunter2pw@hooks.example.com/in?token=s3cr3tvalue&x=1#frag0123"
	body := "rejected token=s3cr3tvalue for user svc:hunter2pw, s3cr3tvalue, frag0123"
	got := sanitizeWebhookEcho(body, webhookURL)
	for _, fragment := range []string{"s3cr3tvalue", "hunter2pw", "frag0123"} {
		if strings.Contains(got, fragment) {
			t.Errorf("sanitizeWebhookEcho() = %q repeats %q", got, fragment)
		}
	}
}

func TestURLSecrets(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		if got := urlSecrets("  "); got != nil {
			t.Errorf("urlSecrets() = %q, want nil", got)
		}
	})

	t.Run("longest first and the host is kept", func(t *testing.T) {
		got := urlSecrets("https://hooks.slack.com:8443" + webhookSecretPath)
		for i := 1; i < len(got); i++ {
			if len(got[i]) > len(got[i-1]) {
				t.Fatalf("urlSecrets() = %q, want longest first", got)
			}
		}
		for _, secret := range got {
			if secret == "hooks.slack.com" || secret == "hooks.slack.com:8443" {
				t.Errorf("urlSecrets() = %q, want the host left out", got)
			}
			if len(secret) < minURLSecretBytes {
				t.Errorf("urlSecrets() returned %q, shorter than the minimum", secret)
			}
		}
		for _, want := range []string{webhookSecretPath, "XXXXXXXXXXXXXXXXXXXXXXXX", "T00000000"} {
			found := false
			for _, secret := range got {
				found = found || secret == want
			}
			if !found {
				t.Errorf("urlSecrets() = %q, want it to include %q", got, want)
			}
		}
	})

	t.Run("an unparsable url still yields its pieces", func(t *testing.T) {
		got := urlSecrets("hooks.example.com\x00" + webhookSecretPath + "%zz")
		found := false
		for _, secret := range got {
			found = found || secret == "T00000000"
		}
		if !found {
			t.Errorf("urlSecrets() = %q, want the path segments", got)
		}
	})

	t.Run("short pieces are left alone", func(t *testing.T) {
		for _, secret := range urlSecrets("https://h.example.com/a/b?x=1") {
			if secret == "a" || secret == "b" || secret == "x" || secret == "1" {
				t.Errorf("urlSecrets() returned the short piece %q", secret)
			}
		}
	})
}
