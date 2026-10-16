/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package connstring

import (
	"errors"
	"net/url"
	"strings"
)

// RedactedPassword replaces every password value that RedactPasswords
// removes. It matches the placeholder used elsewhere in the Workbench for
// masked credentials.
const RedactedPassword = "********"

// errMalformed reports a connection string libpq itself would reject.
var errMalformed = errors.New("malformed connection string")

// uriPrefixes are the URI schemes libpq recognizes, matched
// case-sensitively as libpq's uri_prefix_length does.
var uriPrefixes = []string{"postgresql://", "postgres://"}

// isPasswordKey reports whether a connection keyword carries a secret:
// password is the server password and sslpassword the passphrase for the
// client certificate key. libpq keywords are case-sensitive and would
// reject other spellings, but PostgreSQL stores a GUC such as
// primary_conninfo without validating it, so other spellings are
// matched too.
func isPasswordKey(key string) bool {
	switch strings.ToLower(key) {
	case "password", "sslpassword":
		return true
	default:
		return false
	}
}

// RedactPasswords returns conninfo with the value of every password and
// sslpassword parameter replaced by RedactedPassword, leaving the rest of
// the string as written. It accepts both libpq formats: keyword=value
// pairs (with single-quoted values and backslash escapes) and
// postgresql:// or postgres:// URIs, where a password can appear in the
// user information or as a query parameter.
//
// A string that cannot be parsed as either format is returned as
// RedactedPassword in full, so a malformed value can never leak a
// password that a parser has failed to find. The empty string is
// returned unchanged. The function is idempotent.
func RedactPasswords(conninfo string) string {
	var (
		out string
		err error
	)
	if hasURIPrefix(conninfo) {
		out = redactURI(conninfo)
	} else {
		out, err = redactKeywordValue(conninfo)
	}
	if err != nil {
		return RedactedPassword
	}
	return out
}

// hasURIPrefix reports whether s starts with a scheme libpq treats as a
// connection URI.
func hasURIPrefix(s string) bool {
	for _, p := range uriPrefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// isConnSpace matches the characters C's isspace accepts, which libpq
// uses to separate keyword=value pairs.
func isConnSpace(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	default:
		return false
	}
}

// span locates one value inside a connection string, with its decoded
// form.
type span struct {
	start, end int
	decoded    string
}

// redactKeywordValue redacts a keyword=value connection string, following
// the tokenizing rules of libpq's conninfo_parse. It returns errMalformed
// wherever libpq would reject the string.
func redactKeywordValue(s string) (string, error) {
	type replacement struct {
		span
		with string
	}
	var reps []replacement

	i, n := 0, len(s)
	for {
		for i < n && isConnSpace(s[i]) {
			i++
		}
		if i >= n {
			break
		}

		keyStart := i
		for i < n && s[i] != '=' && !isConnSpace(s[i]) {
			i++
		}
		key := s[keyStart:i]
		for i < n && isConnSpace(s[i]) {
			i++
		}
		if i >= n || s[i] != '=' {
			return "", errMalformed
		}
		i++
		for i < n && isConnSpace(s[i]) {
			i++
		}

		val, next, err := scanKeywordValue(s, i)
		if err != nil {
			return "", err
		}
		i = next

		switch {
		case isPasswordKey(key):
			reps = append(reps, replacement{val, RedactedPassword})
		case strings.EqualFold(key, "dbname") && looksLikeConnString(val.decoded):
			// libpq can expand a dbname that is itself a connection
			// string, so redact one nested level as well.
			if inner := RedactPasswords(val.decoded); inner != val.decoded {
				reps = append(reps, replacement{val, "'" + EscapeValue(inner) + "'"})
			}
		}
	}

	if len(reps) == 0 {
		return s, nil
	}
	var b strings.Builder
	last := 0
	for _, r := range reps {
		b.WriteString(s[last:r.start])
		b.WriteString(r.with)
		last = r.end
	}
	b.WriteString(s[last:])
	return b.String(), nil
}

// scanKeywordValue reads one value starting at i, quoted or not, and
// returns its span and the index just past it. As in libpq, a backslash
// escapes the next character in either form, an unquoted value ends at
// whitespace, and an unterminated quoted value is an error.
func scanKeywordValue(s string, i int) (span, int, error) {
	n := len(s)
	start := i
	var dec strings.Builder

	if i < n && s[i] == '\'' {
		i++
		for {
			if i >= n {
				return span{}, 0, errMalformed
			}
			switch s[i] {
			case '\\':
				i++
				if i < n {
					dec.WriteByte(s[i])
					i++
				}
			case '\'':
				i++
				return span{start, i, dec.String()}, i, nil
			default:
				dec.WriteByte(s[i])
				i++
			}
		}
	}

	for i < n && !isConnSpace(s[i]) {
		if s[i] == '\\' {
			i++
			if i >= n {
				break
			}
		}
		dec.WriteByte(s[i])
		i++
	}
	return span{start, i, dec.String()}, i, nil
}

// looksLikeConnString mirrors libpq's recognized_connection_string: a
// URI, or any string containing '='.
func looksLikeConnString(s string) bool {
	return hasURIPrefix(s) || strings.Contains(s, "=")
}

// redactURI redacts a postgresql:// or postgres:// URI. The password can
// sit in the user information (user:password@host) and in the query
// string as password= or sslpassword=. libpq takes the user information
// to be everything before the first '@' that precedes any '/', and the
// query string to start at the first '?' after the host list.
func redactURI(s string) string {
	prefixLen := 0
	for _, p := range uriPrefixes {
		if strings.HasPrefix(s, p) {
			prefixLen = len(p)
			break
		}
	}

	var b strings.Builder
	b.WriteString(s[:prefixLen])
	rest := s[prefixLen:]

	if at := strings.IndexAny(rest, "@/"); at >= 0 && rest[at] == '@' {
		userinfo := rest[:at]
		if colon := strings.IndexByte(userinfo, ':'); colon >= 0 && colon < len(userinfo)-1 {
			userinfo = userinfo[:colon+1] + RedactedPassword
		}
		b.WriteString(userinfo)
		b.WriteByte('@')
		rest = rest[at+1:]
	}

	q := queryStart(rest)
	if q < 0 {
		b.WriteString(rest)
		return b.String()
	}
	b.WriteString(rest[:q+1])

	params := strings.Split(rest[q+1:], "&")
	for i, param := range params {
		eq := strings.IndexByte(param, '=')
		if eq < 0 {
			continue
		}
		key := param[:eq]
		if decoded, err := url.PathUnescape(key); err == nil {
			key = decoded
		}
		if isPasswordKey(key) {
			params[i] = param[:eq+1] + RedactedPassword
		}
	}
	b.WriteString(strings.Join(params, "&"))
	return b.String()
}

// queryStart returns the index of the '?' that opens the query string in
// the part of a URI after the user information, or -1 when there is
// none. A '?' inside a bracketed IPv6 host is skipped, as libpq skips to
// the closing ']'.
func queryStart(s string) int {
	inBrackets := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '[':
			inBrackets = true
		case ']':
			inBrackets = false
		case '?':
			if !inBrackets {
				return i
			}
		}
	}
	return -1
}
