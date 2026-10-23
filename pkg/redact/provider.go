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
)

// ProviderMarker replaces each credential removed from an LLM provider's
// error text.
const ProviderMarker = "[REDACTED]"

// providerPatterns match credentials a provider error may repeat:
// a bearer or basic credential, the query string of a URL, a key or
// token parameter or header, a key-shaped value from a known provider,
// and the userinfo of a URL.
var providerPatterns = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`(?i)\b((?:bearer|basic)\s+)[A-Za-z0-9._~+/=-]+`), "${1}[REDACTED]"},
	{regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://[^\s?#"'<>]*)\?[^\s"'<>#]*`), "${1}?[REDACTED]"},
	{regexp.MustCompile(`(?i)((?:api[_-]?key|x-api-key|x-goog-api-key|access[_-]?token|token|key|secret|password|authorization)["']?\s*[:=]\s*["']?)[^\s"'&,;}]+`), "${1}[REDACTED]"},
	{regexp.MustCompile(`\b(?:sk|pa|pk)-[A-Za-z0-9_\-]{8,}`), "[REDACTED]"},
	{regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{20,}`), "[REDACTED]"},
	{regexp.MustCompile(`(://)[^/@\s]+@`), "${1}[REDACTED]@"},
}

// ProviderLogError prepares an LLM provider's error message for a log
// line. It deletes control and formatting characters, removes the
// secrets given verbatim and anything that looks like a credential,
// replacing each with ProviderMarker, and keeps endpoint details. The
// alerter and the server pass the API keys they hold and the
// BaseURLSecrets of each configured base URL.
func ProviderLogError(msg string, secrets []string) string {
	msg = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) ||
			r == '\u2028' || r == '\u2029' {
			return -1
		}
		return r
	}, strings.ToValidUTF8(msg, "\uFFFD"))
	for _, secret := range secrets {
		if secret != "" {
			msg = strings.ReplaceAll(msg, secret, ProviderMarker)
		}
	}
	for _, p := range providerPatterns {
		msg = p.re.ReplaceAllString(msg, p.repl)
	}
	return msg
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
	// Query keeps the pairs that parse and drops malformed ones.
	for _, vs := range u.Query() {
		values = append(values, vs...)
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
