/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package oidc

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// emailClaim is the standard OpenID Connect claim carrying the end
// user's email address. It is read regardless of which claim the
// operator configured as the username, because parts of the Workbench
// (allowed_email_domains, for one) want the address itself rather than
// whatever the username happens to be.
const emailClaim = "email"

// maxClaimValueLength bounds, in runes, any single value extracted from
// an ID token. Where an operator points the Workbench at a provider on
// which users edit their own profile, a display name or a group name is
// user-controlled text that ends up in the audit log, the user store and
// the browser, so it needs a ceiling well below whatever the provider
// happens to allow. 256 runes is longer than any real name, address or
// group, and comfortably above the 128-rune limit
// auth.ValidateUsername already imposes on a username.
const maxClaimValueLength = 256

// identityFromClaims turns the decoded claims of a verified ID token
// into an Identity. issuer and subject come from the verified token
// rather than from the claim map, so they cannot be influenced by a
// claim of the same name appearing twice in a decoded payload.
//
// An absent, empty or unusable username claim is an error: there is
// nothing to key a Workbench user on, and quietly provisioning a user
// with an empty or malformed name would be worse than refusing the
// login. The username must satisfy exactly the rule the local user
// store applies (auth.ValidateUsername), so that a federated login
// cannot create a username that "-add-user" would have refused.
//
// The remaining values fail soft: a display name, email address or
// group that is too long or carries a control character is dropped
// rather than failing the login, because each of those is an input to a
// later decision that is safe to make without it. Dropped groups are
// counted on the Identity so that the caller can log the fact once.
//
// The two rules therefore differ by field. Every claim must be valid
// UTF-8, within maxClaimValueLength and free of Unicode Cc characters
// (C0, C1 and DEL). Only the username additionally has to satisfy
// auth.ValidateUsername, which is what excludes Unicode Cf format
// characters from it; a display name, email address or group name may
// contain them, since a zero-width joiner is part of how some names are
// legitimately spelled.
func (p *Provider) identityFromClaims(issuer, subject string, claims map[string]any) (*Identity, error) {
	username := stringClaim(claims, p.usernameClaim)
	if username == "" {
		return nil, fmt.Errorf("ID token has no usable %q claim to use as a username", p.usernameClaim)
	}
	if err := auth.ValidateUsername(username); err != nil {
		return nil, fmt.Errorf("ID token %q claim is not a usable username: %w", p.usernameClaim, err)
	}

	groups, skipped, unexpectedShape := stringsClaim(claims, p.groupsClaim)

	return &Identity{
		Issuer:                     issuer,
		Subject:                    subject,
		Username:                   username,
		DisplayName:                stringClaim(claims, p.displayNameClaim),
		Email:                      stringClaim(claims, emailClaim),
		Groups:                     groups,
		SkippedGroups:              skipped,
		UnexpectedGroupsClaimShape: unexpectedShape,
	}, nil
}

// stringClaim returns the named claim as a bounded, control-character
// free string. It returns an empty string when name is empty (the claim
// is not configured), when the claim is absent, when it holds anything
// other than a string, or when safeClaimValue rejects its contents. A
// provider sending a number or an object where a username belongs is a
// misconfiguration, and coercing it to text would invent a username.
func stringClaim(claims map[string]any, name string) string {
	if name == "" {
		return ""
	}

	value, ok := claims[name].(string)
	if !ok {
		return ""
	}
	return safeClaimValue(value)
}

// safeClaimValue trims value and returns it only if it is safe to carry
// around in an Identity: valid UTF-8, no longer than
// maxClaimValueLength runes, and free of control characters. Anything
// else yields an empty string, which every caller treats as "this claim
// was not usable".
//
// Control characters are the Unicode Cc category, which is C0
// (including NUL), C1 and DEL. They are rejected for every claim,
// because a newline or a NUL is a log-injection and truncation hazard
// once the value reaches the audit log or the user store, and no real
// name, address or group contains one.
//
// Format characters (the Unicode Cf category: zero-width joiners,
// bidirectional overrides and the like) are deliberately NOT rejected
// here. A zero-width joiner appears in genuinely legitimate names in
// several scripts, a format character cannot inject a log line, and a
// cosmetic field must never be the reason a login fails. The username
// is the exception, and it is handled a layer up: identityFromClaims
// puts it through auth.ValidateUsername, whose allowlist of letters,
// digits and "_ . @ -" excludes format characters for its own reasons.
func safeClaimValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || !utf8.ValidString(trimmed) {
		return ""
	}
	if utf8.RuneCountInString(trimmed) > maxClaimValueLength {
		return ""
	}
	for _, r := range trimmed {
		if unicode.IsControl(r) {
			return ""
		}
	}
	return trimmed
}

// stringsClaim returns the named claim as a slice of strings, tolerating
// the shapes identity providers actually send for a groups claim: an
// array of strings, an array of any holding strings (what a JSON decode
// into map[string]any always produces), or a single bare string for a
// user in exactly one group.
//
// It returns the usable groups, a count of the values it had to skip
// (elements that were not strings, and strings safeClaimValue refused),
// and, when the claim was neither an array nor a string, a description
// of the shape it actually saw. Nothing here is an error: group
// membership is an authorization input, so dropping what cannot be read
// under-privileges the user, whereas failing the login would turn one
// malformed element into an outage for every user of that provider.
// Reporting the skipped count and the unexpected shape is what makes the
// difference visible, and the caller logs it once per login.
//
// Non-string elements are skipped rather than stringified, for the same
// reason stringClaim refuses to coerce: a group name the Workbench
// invented cannot match anything an operator configured.
func stringsClaim(claims map[string]any, name string) (groups []string, skipped int, unexpectedShape string) {
	if name == "" {
		return nil, 0, ""
	}

	value, present := claims[name]
	if !present || value == nil {
		return nil, 0, ""
	}

	switch typed := value.(type) {
	case []string:
		groups, skipped = collectStrings(typed)
		return groups, skipped, ""
	case []any:
		groups, skipped = collectAnyStrings(typed)
		return groups, skipped, ""
	case string:
		groups, skipped = collectStrings([]string{typed})
		return groups, skipped, ""
	default:
		return nil, 0, fmt.Sprintf("%T", value)
	}
}

func collectStrings(values []string) (groups []string, skipped int) {
	for _, value := range values {
		if safe := safeClaimValue(value); safe != "" {
			groups = append(groups, safe)
		} else {
			skipped++
		}
	}
	return groups, skipped
}

func collectAnyStrings(values []any) (groups []string, skipped int) {
	for _, value := range values {
		text, ok := value.(string)
		if !ok {
			skipped++
			continue
		}
		if safe := safeClaimValue(text); safe != "" {
			groups = append(groups, safe)
		} else {
			skipped++
		}
	}
	return groups, skipped
}
