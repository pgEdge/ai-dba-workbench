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
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// This file holds the redact/cap/sanitize helpers every notification
// sender uses on text it borrowed from the far end of an HTTP exchange.
//
// The whole file is duplicated between
// alerter/src/internal/notifications/sanitize.go and
// server/src/internal/api/sanitize.go; the alerter and the server are
// separate Go modules, so it cannot be shared. Apart from the package
// clause the two copies must stay character-for-character identical -
// diff them after any change.
//
// Why any of it exists: for Telegram the bot token sits in the request
// path, and for Slack, Mattermost and the generic webhook channel the
// whole webhook URL is itself the credential. Anything net/http reports
// about a failed request repeats that URL verbatim, and the alerter
// logs what a send returns and writes it to
// notification_history.error_message while the server logs it, both of
// which are read by people who are not entitled to the credential. So
// no error built from a failed send wraps the original with %w:
// borrowed text goes through a sanitizer here instead.

// maxEchoedBytes bounds how much borrowed text any error built from an
// HTTP exchange may repeat. See sanitizeEcho.
const maxEchoedBytes = 256

// echoTruncationMarker marks text sanitizeEcho cut short.
const echoTruncationMarker = "…"

// telegramMaxEchoedBytes and telegramEchoTruncationMarker are the
// Telegram-spelled names of the two constants above, kept because the
// Telegram sanitizer's tests, which predate the channel-agnostic
// helpers, refer to them.
const (
	telegramMaxEchoedBytes       = maxEchoedBytes
	telegramEchoTruncationMarker = echoTruncationMarker
)

// sanitizeEcho prepares a string that came from the far end of an HTTP
// call - a response body, an API description, a transport error - for
// inclusion in an error a caller will record. Every borrowed string
// interpolated into an error goes through here; that rule is what makes
// the property easy to check.
//
// It does three things, in this order, all of which are load-bearing:
//
//   - Deletes control and formatting characters, so a hostile or
//     broken endpoint cannot inject newlines and forge log lines.
//     Invalid UTF-8 is replaced at the same time, which also keeps the
//     value storable in a Postgres text column. The class is wider than
//     the ASCII controls on purpose: a response body is wholly
//     attacker-controlled, and U+0085, U+2028 and U+2029 end a line for
//     a good number of log processors whilst the bidi formatting
//     characters can reorder what the notification-history view shows.
//   - Applies redact, which removes whichever part of a URL is the
//     credential for this channel, because the text may repeat the
//     request URL.
//   - Caps the result at maxEchoedBytes. What is read and what is
//     echoed are deliberately separate limits: a body is read under a
//     1 MiB io.LimitReader, but a captive portal or a hostile endpoint
//     would otherwise put the whole megabyte into the log and the
//     database on every one of the three delivery attempts.
//
// The characters are deleted rather than mapped to a space, and before
// redaction rather than after, so that none of them can split a URL
// into pieces a redactor does not recognize: "https:\x00//host/secret"
// has no "://" for redactURLPath to anchor on, and a space in place of
// the NUL would still have none, whereas deleting it restores the URL
// for the redactor to find. Redaction runs before the cap so the cap
// can never slice a credential run and leave the tail of it visible.
func sanitizeEcho(s string, redact func(string) string) string {
	s = strings.Map(func(r rune) rune {
		// unicode.IsControl covers the C0 and C1 ranges, DEL and
		// U+0085; Cf is the format class, which holds the bidi
		// overrides and the zero-width joiners; U+2028 and U+2029 are
		// in neither, being Zl and Zp.
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) ||
			r == '\u2028' || r == '\u2029' {
			return -1
		}
		return r
	}, s)
	s = redact(s)
	if len(s) <= maxEchoedBytes {
		return s
	}
	cut := maxEchoedBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + echoTruncationMarker
}

// transportError renders a transport failure without the request URL.
// *url.Error stringifies as `Op "URL": Err`, which would leak the
// credential, so only its operation and cause are reported, and the
// cause goes through redact as well because a proxy or redirect error
// can repeat the URL inside it.
func transportError(err error, redact func(string) string) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return fmt.Sprintf("%s request failed: %s", urlErr.Op,
			sanitizeEcho(urlErr.Err.Error(), redact))
	}
	return sanitizeEcho(err.Error(), redact)
}

// sanitizeTelegramEcho is sanitizeEcho with the Telegram bot-token
// redactor.
func sanitizeTelegramEcho(s string) string {
	return sanitizeEcho(s, redactTelegramToken)
}

// telegramTransportError is transportError with the Telegram bot-token
// redactor.
func telegramTransportError(err error) string {
	return transportError(err, redactTelegramToken)
}

// sanitizeConfigEcho is sanitizeEcho with no redaction, for a value an
// operator stored in a channel's configuration that is not itself a
// credential but still reaches the log and the notification history
// when it is rejected, such as an HTTP method.
func sanitizeConfigEcho(s string) string {
	return sanitizeEcho(s, func(s string) string { return s })
}

// sanitizeWebhookEcho is sanitizeEcho with the webhook redactor for the
// channel's own configured URL, for the Slack, Mattermost and generic
// webhook channels. See webhookRedactor.
func sanitizeWebhookEcho(s, webhookURL string) string {
	return sanitizeEcho(s, webhookRedactor(webhookURL))
}

// webhookTransportError is transportError with the webhook redactor for
// the channel's own configured URL, for the Slack, Mattermost and
// generic webhook channels. See webhookRedactor.
func webhookTransportError(err error, webhookURL string) string {
	return transportError(err, webhookRedactor(webhookURL))
}

// minURLSecretBytes is the shortest whole URL component - a path, a
// query string, a password - that webhookRedactor removes wherever it
// appears. Anything shorter is not a credential worth the name, and
// removing every occurrence of, say, a one-character password would
// shred the rest of the message.
const minURLSecretBytes = 4

// minURLSecretTokenBytes is the shortest single piece of a URL - a path
// segment, a query value - that webhookRedactor removes wherever it
// appears. Removing a common word such as "services" from an echoed
// body as well costs nothing; leaving a Slack or Mattermost secret
// segment in place because a proxy quoted only part of the path would
// not.
const minURLSecretTokenBytes = 6

// webhookRedactor returns a redactor for text borrowed from an HTTP
// exchange with the webhook at webhookURL.
//
// It applies redactURLPath, which recognizes any URL by its "://", and
// then removes every literal occurrence of the configured URL's own
// credential-bearing parts: the whole URL, its path, its query string,
// its fragment, its userinfo, and each individual path segment and
// query value, in both their escaped and unescaped spellings. The
// second pass is the one that matters for a response body. A proxy, a
// WAF or the endpoint itself may well answer with an error page naming
// the request path with no scheme in front of it - "no route for
// /services/T.../B.../XXX" - and a matcher that works from URL syntax
// alone cannot see that. Anchoring on the value actually sent can.
//
// The host is the one part of the URL that is kept, as redactURLPath
// keeps it: an operator reading a failed-delivery message needs to know
// which endpoint was unreachable, and the host alone is not the
// credential. With an empty webhookURL this is redactURLPath alone.
func webhookRedactor(webhookURL string) func(string) string {
	secrets := urlSecrets(webhookURL)
	return func(s string) string {
		s = redactURLPath(s)
		for _, secret := range secrets {
			s = strings.ReplaceAll(s, secret, urlPathPlaceholder)
		}
		return s
	}
}

// urlSecrets lists the parts of rawURL that webhookRedactor removes
// wherever they appear, longest first so that a whole path is replaced
// in one piece before any of its segments is considered on its own.
func urlSecrets(rawURL string) []string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil
	}
	components := []string{rawURL}
	if unescaped, err := url.PathUnescape(rawURL); err == nil {
		components = append(components, unescaped)
	}
	var host, hostname string
	if u, err := url.Parse(rawURL); err == nil {
		host, hostname = u.Host, u.Hostname()
		components = append(components, u.EscapedPath(), u.Path,
			u.RawQuery, u.EscapedFragment(), u.Fragment)
		if query, err := url.QueryUnescape(u.RawQuery); err == nil {
			components = append(components, query)
		}
		if u.User != nil {
			components = append(components, u.User.String(),
				u.User.Username())
			if password, ok := u.User.Password(); ok {
				components = append(components, password)
			}
		}
	}

	seen := make(map[string]bool)
	var secrets []string
	add := func(secret string, minBytes int) {
		if len(secret) < minBytes || secret == host ||
			secret == hostname || seen[secret] {
			return
		}
		seen[secret] = true
		secrets = append(secrets, secret)
	}
	for _, component := range components {
		add(component, minURLSecretBytes)
		// Split into the pieces a partial echo would most plausibly
		// quote: path segments, query keys and values, and the halves
		// of any userinfo.
		for _, token := range strings.FieldsFunc(component, isURLSecretDelimiter) {
			add(token, minURLSecretTokenBytes)
		}
	}
	sort.SliceStable(secrets, func(i, j int) bool {
		return len(secrets[i]) > len(secrets[j])
	})
	return secrets
}

// isURLSecretDelimiter reports whether r separates the pieces of a URL
// that urlSecrets considers one at a time.
func isURLSecretDelimiter(r rune) bool {
	return strings.ContainsRune("/?#&=;:@", r)
}

// redactTelegramToken replaces the bot token in any text that may have
// come from a URL with a fixed placeholder.
//
// The token is a bearer credential and it sits in the request path, so
// anything net/http reports about a failed request - a *url.Error, a
// redirect message, a proxy error - repeats it verbatim, as does a
// description the Bot API echoes back.
//
// The redactor fails closed. Once "/bot" is found the token run that
// follows is always replaced, whether or not the terminating '/' of
// "/bot<token>/sendMessage" is present: a truncated URL in a proxy
// error, a description echoing a partial path, or a caller building a
// different Bot API URL must never be able to carry a live credential
// through untouched. Do not reintroduce a bail-out that copies the
// remainder of the string out verbatim.
func redactTelegramToken(s string) string {
	const prefix = "/bot"
	var b strings.Builder
	for {
		i := strings.Index(s, prefix)
		if i < 0 {
			break
		}
		// The token runs from just after the prefix to the first
		// character that cannot appear in a token interpolated into a
		// URL path, or to the end of the string when none follows.
		rest := s[i+len(prefix):]
		j := 0
		for j < len(rest) && !isTelegramTokenTerminator(rest[j]) {
			j++
		}
		b.WriteString(s[:i])
		if j == 0 {
			// An empty run is not a credential: the literal text
			// "/bot", or "/bot/" with nothing between the slashes,
			// carries no token and redacting it would only mislead.
			// Emit it unchanged and keep scanning after it.
			b.WriteString(prefix)
		} else {
			b.WriteString("/bot<redacted>")
		}
		// Whatever terminated the run is left in place, so the '/' of
		// the usual "/bot<token>/sendMessage" survives in the output.
		s = rest[j:]
	}
	b.WriteString(s)
	return b.String()
}

// isTelegramTokenTerminator reports whether c ends a bot token that was
// interpolated into a URL path.
//
// The invariant: this class must be a SUBSET of the characters the
// server's telegramBotTokenPattern forbids inside a token. Any
// character a token may legally contain has to be swallowed into the
// redacted run; if it ends the run instead, redaction stops in the
// middle of the credential and prints the rest of it verbatim. The
// class is therefore exactly whitespace and control characters, which
// cannot appear in a URL at all, plus the three path delimiters '/',
// '?' and '#'.
//
// Do NOT add "defensive" quoting or bracketing characters - the double
// quote, the apostrophe, the backquote, the angle brackets, the closing
// paren, the closing square bracket, the comma or the semicolon - on
// the grounds that they surround a URL in an error or a log line. An
// earlier version did exactly that, reasoning about the text around a
// token rather than about the token itself, and any token containing
// one of them leaked its whole tail. Over-redacting the boilerplate
// that follows a token is free; under-redacting the token is not.
func isTelegramTokenTerminator(c byte) bool {
	// Space and everything below it: all ASCII whitespace and control
	// characters. UTF-8 continuation bytes are >= 0x80, so a multi-byte
	// character is never mistaken for a terminator.
	if c <= ' ' {
		return true
	}
	switch c {
	case '/', '?', '#':
		return true
	}
	return false
}

// urlPathPlaceholder replaces the redacted path, query and fragment of
// a URL.
const urlPathPlaceholder = "<redacted>"

// redactURLPath replaces everything after the host of every URL in s
// with a fixed placeholder.
//
// For Slack and Mattermost the whole incoming-webhook URL is the
// credential - there is no separate token - and a generic webhook
// endpoint may carry one in its path or query string just as easily.
// There is no "/bot"-style marker to anchor on, as there is for
// Telegram, so the entire path, query and fragment is treated as
// sensitive and the host is all that survives. Keeping the host is
// deliberate: an operator reading a failed-delivery message needs to
// know which endpoint was unreachable, and the host alone is not the
// credential.
//
// Any userinfo in the authority is replaced too, because the generic
// webhook channel supports basic authentication and an operator may
// perfectly well store the credential as "https://user:pass@host/path".
// The authority is split at the LAST '@' of the run, since RFC 3986
// requires a literal '@' inside userinfo to be written %40.
//
// Two limits, which is why the senders never call this on its own but
// through webhookRedactor, which also removes the configured URL's own
// parts wherever they appear:
//
//   - A URL is recognized by "://" alone, so scheme-less text - a bare
//     "hooks.example.com/services/XXX", or an error page naming only the
//     request path - passes through untouched. Nothing validates that a
//     stored webhook_url or endpoint_url carries a scheme, and
//     (*url.Error).Error renders the raw input string rather than a
//     parsed URL, so the senders also never echo a url.Parse failure;
//     they report a fixed "the URL is malformed" instead.
//   - Whitespace inside a stored path ends the redacted run early and
//     leaves the tail of the path in the clear.
//
// Like the Telegram redactor, this one fails closed, and its terminator
// class is the same: the redacted run ends only at whitespace, never at
// a quote, paren, bracket, comma or semicolon. Those characters
// surround a URL in an error message, but they may equally appear
// inside a percent-decoded path, and ending the run at one would print
// the rest of the credential verbatim. Over-redacting the boilerplate
// that follows a URL is free; under-redacting the URL is not.
func redactURLPath(s string) string {
	const scheme = "://"
	var b strings.Builder
	for {
		i := strings.Index(s, scheme)
		if i < 0 {
			break
		}
		// The host runs from just after "://" to the first path
		// delimiter, or to the first character that cannot appear in a
		// URL at all.
		rest := s[i+len(scheme):]
		host := 0
		for host < len(rest) && !isURLHostTerminator(rest[host]) {
			host++
		}
		b.WriteString(s[:i+len(scheme)])
		// The host run spans the whole authority, so any userinfo in
		// front of the host is a credential that has to go. Split at
		// the last '@': a literal '@' inside userinfo must be written
		// %40, so the last one is always the authority delimiter.
		hostRun := rest[:host]
		if at := strings.LastIndexByte(hostRun, '@'); at >= 0 {
			b.WriteString(urlPathPlaceholder)
			b.WriteString(hostRun[at:])
		} else {
			b.WriteString(hostRun)
		}
		if host == len(rest) || isURLPathTerminator(rest[host]) {
			// Host only: nothing followed it that could be a path, a
			// query or a fragment, so there is nothing to redact.
			s = rest[host:]
			continue
		}
		// A path, query or fragment follows: redact the whole run up to
		// the next character that cannot appear in a URL.
		end := host
		for end < len(rest) && !isURLPathTerminator(rest[end]) {
			end++
		}
		b.WriteString(urlPathPlaceholder)
		s = rest[end:]
	}
	b.WriteString(s)
	return b.String()
}

// isURLHostTerminator reports whether c ends the host of a URL: one of
// the three path delimiters, or a character that cannot appear in a URL
// at all.
func isURLHostTerminator(c byte) bool {
	if isURLPathTerminator(c) {
		return true
	}
	switch c {
	case '/', '?', '#':
		return true
	}
	return false
}

// isURLPathTerminator reports whether c ends a redacted URL run.
//
// The class is exactly ASCII whitespace, which is what separates a URL
// from the text around it in every error net/http and net/url produce.
// See redactURLPath for why no quoting or bracketing character belongs
// in it.
//
// The other ASCII control characters are deliberately NOT terminators,
// which is the one place this class differs from
// isTelegramTokenTerminator. A control character cannot appear in a URL
// that was actually dispatched, and ending the host at one would leave
// the path after it unredacted, so it is swallowed into the redacted
// run instead, which costs nothing. This is belt and braces rather than
// load-bearing: net/url renders its errors with %q, which escapes such
// a byte to the four literal characters \x00 before the redactor ever
// sees it.
func isURLPathTerminator(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	}
	return false
}
