/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package redact

import (
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ProviderMarker replaces each credential removed from an LLM provider's
// error text.
const ProviderMarker = "[REDACTED]"

// providerPatterns match credentials a provider error may repeat:
// a bearer or basic credential, the userinfo and the query string of a
// URL, a key or token parameter or header, and a key-shaped value from a
// known provider.
var providerPatterns = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`(?i)\b((?:bearer|basic)\s+)[A-Za-z0-9._~+/=-]+`), "${1}[REDACTED]"},
	// An '@' after a '?' or '#' before the path may end a userinfo whose
	// password holds that character, or may sit in a query value or
	// fragment of a URL with no path, which may itself go on to hold a
	// '/'. The two cannot be told apart, so the rest of the URL is
	// removed, host and path included. Matched before the query
	// pattern, which would otherwise split it at '?'.
	{regexp.MustCompile(`(://)[^/\s"'<>]*[?#][^/\s"'<>]*@[^\s"'<>]*`), "${1}[REDACTED]"},
	// The userinfo of a URL. Greedy, so that the match runs to the last
	// '@' before the host and a password holding an unescaped '@' is
	// removed whole.
	{regexp.MustCompile(`(://)[^/?#\s]+@`), "${1}[REDACTED]@"},
	{regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://[^\s?#"'<>]*)\?[^\s"'<>#]*`), "${1}?[REDACTED]"},
	{regexp.MustCompile(`(?i)((?:api[_-]?key|x-api-key|x-goog-api-key|access[_-]?token|token|key|secret|password|authorization)["']?\s*[:=]\s*["']?)[^\s"'&,;}]+`), "${1}[REDACTED]"},
	{regexp.MustCompile(`\b(?:sk|pa|pk)-[A-Za-z0-9_\-]{8,}`), "[REDACTED]"},
	{regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{20,}`), "[REDACTED]"},
}

// ProviderLogError prepares an LLM provider's error message for a log
// line. It deletes control and formatting characters, removes the
// secrets given, whether or not the secret or the message holds such a
// character where the other does not, and anything that looks like a
// credential, replacing each with ProviderMarker, and keeps endpoint
// details. The alerter and the server pass the API keys they hold and
// the BaseURLSecrets of each configured base URL.
func ProviderLogError(msg string, secrets []string) string {
	// Each secret is matched with the same characters deleted from it
	// against the message with them deleted, which catches it whether
	// the message repeats it with such a character, splits it with one
	// or repeats it without the one it holds. A deleted form is used
	// only when it is as long as a derived secret must be, or is the
	// secret unchanged, so that deletion cannot leave a short form that
	// matches ordinary text; any other secret is matched verbatim
	// against the message as given instead. Every match is recorded as a
	// span of the message, and overlapping or touching spans are
	// replaced as one, so that no secret's match can cut another's
	// apart, whatever the order of the secrets.
	msg = strings.ToValidUTF8(msg, "\uFFFD")
	deleted, offsets := deleteFormatCharsMapped(msg)
	var spans [][2]int
	for _, secret := range secrets {
		form := deleteFormatChars(secret)
		if form != "" && (len(form) >= minDerivedSecretLen || form == secret) {
			// Map each match in the deleted text back to the message,
			// ending just after the last byte the match kept.
			for _, s := range matchSpans(deleted, form) {
				spans = append(spans, [2]int{offsets[s[0]], offsets[s[1]-1] + 1})
			}
		} else if secret != "" {
			spans = append(spans, matchSpans(msg, secret)...)
		}
	}
	msg = deleteFormatChars(replaceSpans(msg, spans))
	for _, p := range providerPatterns {
		msg = p.re.ReplaceAllString(msg, p.repl)
	}
	return msg
}

// isFormatChar reports whether r is a control or formatting character,
// or a line or paragraph separator, which ProviderLogError keeps out of
// a log line.
func isFormatChar(r rune) bool {
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) ||
		r == '\u2028' || r == '\u2029'
}

// deleteFormatChars deletes each character isFormatChar reports from s.
func deleteFormatChars(s string) string {
	return strings.Map(func(r rune) rune {
		if isFormatChar(r) {
			return -1
		}
		return r
	}, s)
}

// deleteFormatCharsMapped deletes each character isFormatChar reports
// from s, which must be valid UTF-8, and returns for each byte of the
// result its offset in s.
func deleteFormatCharsMapped(s string) (string, []int) {
	var b strings.Builder
	offsets := make([]int, 0, len(s))
	for i, r := range s {
		if isFormatChar(r) {
			continue
		}
		size := utf8.RuneLen(r)
		for j := 0; j < size; j++ {
			offsets = append(offsets, i+j)
		}
		b.WriteString(s[i : i+size])
	}
	return b.String(), offsets
}

// matchSpans returns the byte span of every occurrence of sub in s,
// overlapping occurrences included.
func matchSpans(s, sub string) [][2]int {
	var spans [][2]int
	for start := 0; start < len(s); {
		i := strings.Index(s[start:], sub)
		if i < 0 {
			break
		}
		start += i
		spans = append(spans, [2]int{start, start + len(sub)})
		_, size := utf8.DecodeRuneInString(s[start:])
		start += size
	}
	return spans
}

// replaceSpans replaces each run of overlapping or touching spans of s
// with a single ProviderMarker.
func replaceSpans(s string, spans [][2]int) string {
	sort.Slice(spans, func(i, j int) bool { return spans[i][0] < spans[j][0] })
	var b strings.Builder
	pos := 0
	for i := 0; i < len(spans); {
		start, end := spans[i][0], spans[i][1]
		for i++; i < len(spans) && spans[i][0] <= end; i++ {
			end = max(end, spans[i][1])
		}
		b.WriteString(s[pos:start])
		b.WriteString(ProviderMarker)
		pos = end
	}
	b.WriteString(s[pos:])
	return b.String()
}

// minDerivedSecretLen is the shortest base_url userinfo or query value
// treated as a secret; removing a shorter one verbatim would mangle
// ordinary words in the error text.
const minDerivedSecretLen = 4

// BaseURLSecrets returns the userinfo and query values of a configured
// base URL, in decoded and escaped forms, longest first so that a value
// is removed before any shorter value inside it.
func BaseURLSecrets(base string) []string {
	u, err := url.Parse(base)
	if err != nil {
		return nil
	}
	var values []string
	if u.User != nil {
		values = append(values, u.User.String(), u.User.Username())
		if password, ok := u.User.Password(); ok {
			values = append(values, password)
		}
	}
	// Split the raw query by hand rather than with Query, which drops
	// a pair holding a ';' or a bad escape, and with it the secret.
	for _, pair := range strings.FieldsFunc(u.RawQuery, func(r rune) bool {
		return r == '&' || r == ';'
	}) {
		v := pair
		if _, after, found := strings.Cut(pair, "="); found {
			v = after
		}
		values = append(values, v)
		if decoded, err := url.QueryUnescape(v); err == nil {
			values = append(values, decoded)
		}
	}
	seen := map[string]bool{}
	var secrets []string
	for _, v := range values {
		for _, form := range []string{v, url.QueryEscape(v), url.PathEscape(v)} {
			if len(form) >= minDerivedSecretLen && !seen[form] {
				seen[form] = true
				secrets = append(secrets, form)
			}
		}
	}
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	return secrets
}
