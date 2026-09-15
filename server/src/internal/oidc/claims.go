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

// maxClaimValueLength bounds, in runes, a value extracted from an ID
// token. Where an operator points the Workbench at a provider on which
// users edit their own profile, a display name or a group name is
// user-controlled text that ends up in the audit log, the user store and
// the browser, so it needs a ceiling well below whatever the provider
// happens to allow. 256 runes is longer than any real name or group, and
// comfortably above the 128-rune limit auth.ValidateUsername already
// imposes on a username.
const maxClaimValueLength = 256

// maxEmailClaimLength bounds the email claim specifically, and is larger
// than maxClaimValueLength because RFC 5321 permits a 64-octet local
// part and a 255-octet domain: a legal address can be 320 characters
// long. Bounding it at 256 would have rejected legitimate addresses, and
// a rejected address is not harmless, since an empty Identity.Email is
// what allowed_email_domains sees.
const maxEmailClaimLength = 320

// maxUsernameLength mirrors the limit auth.ValidateUsername enforces. It
// exists only so that describeUsernameProblem can say which rule was
// broken; auth.ValidateUsername remains the authority on whether a
// username is acceptable.
const maxUsernameLength = 128

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
// group that is too long or carries a forbidden character is dropped
// rather than failing the login, because each of those is an input to a
// later decision that is safe to make without it. Everything dropped is
// recorded on the Identity (DroppedClaims, EmailRejected,
// SkippedGroups, UnexpectedGroupsClaimShape) so that the caller can log
// it once per login, and so that a consumer of Email can tell "the
// provider sent nothing" from "the provider sent something unusable".
//
// The two rules differ by field. Every claim must be valid UTF-8,
// within its length ceiling and free of the character categories
// safeClaimValue rejects. Only the username additionally has to satisfy
// auth.ValidateUsername, which is what excludes Unicode Cf format
// characters from it; a display name, email address or group name may
// contain them, since a zero-width joiner is part of how some names are
// legitimately spelled.
func (p *Provider) identityFromClaims(issuer, subject string, claims map[string]any) (*Identity, error) {
	username, _ := takeStringClaim(claims, p.usernameClaim, maxClaimValueLength)
	if username == "" {
		return nil, fmt.Errorf("ID token has no usable %q claim to use as a username", p.usernameClaim)
	}
	if err := auth.ValidateUsername(username); err != nil {
		// The message names the claim, says which rule the value broke
		// and points at the configuration lever, so that an operator
		// whose provider issues usernames in some other form knows to
		// change http.auth.oidc.username_claim rather than file a bug.
		// It deliberately describes the offending character's class
		// rather than quoting the character, since this text reaches the
		// server log.
		return nil, fmt.Errorf(
			"ID token %q claim is not a usable username: it %s; "+
				"set http.auth.oidc.username_claim to a claim whose value is a valid username",
			p.usernameClaim, describeUsernameProblem(username))
	}

	var dropped []string

	displayName, displayNameRejected := takeStringClaim(claims, p.displayNameClaim, maxClaimValueLength)
	if displayNameRejected {
		dropped = append(dropped, p.displayNameClaim)
	}

	email, emailRejected := takeStringClaim(claims, emailClaim, maxEmailClaimLength)
	if emailRejected {
		dropped = append(dropped, emailClaim)
	}

	groups, skipped, unexpectedShape := stringsClaim(claims, p.groupsClaim)

	return &Identity{
		Issuer:                     issuer,
		Subject:                    subject,
		Username:                   username,
		DisplayName:                displayName,
		Email:                      email,
		EmailRejected:              emailRejected,
		Groups:                     groups,
		SkippedGroups:              skipped,
		UnexpectedGroupsClaimShape: unexpectedShape,
		DroppedClaims:              dropped,
	}, nil
}

// takeStringClaim returns the named claim as a bounded, safe string,
// along with whether the claim was present as a string but had to be
// rejected. It returns ("", false) when name is empty (the claim is not
// configured), when the claim is absent, and when it holds anything
// other than a string: a provider sending a number or an object where a
// username belongs is a misconfiguration, and coercing it to text would
// invent a value. Only the second return value distinguishes "the
// provider sent nothing" from "the provider sent something this package
// refused", which matters wherever an empty value is itself an input to
// a decision.
func takeStringClaim(claims map[string]any, name string, maxRunes int) (value string, rejected bool) {
	if name == "" {
		return "", false
	}

	raw, ok := claims[name].(string)
	if !ok {
		return "", false
	}

	safe := safeClaimValue(raw, maxRunes)
	return safe, safe == ""
}

// stringClaim is takeStringClaim without the rejection flag, for callers
// that have no use for it.
func stringClaim(claims map[string]any, name string, maxRunes int) string {
	value, _ := takeStringClaim(claims, name, maxRunes)
	return value
}

// safeClaimValue trims value and returns it only if it is safe to carry
// around in an Identity: valid UTF-8, no longer than maxRunes, and free
// of the character categories below. Anything else yields an empty
// string, which every caller treats as "this claim was not usable".
//
// Rejected are the Unicode Cc category, which is C0 (including NUL), C1
// and DEL, and the Zl and Zp categories, which are U+2028 LINE SEPARATOR
// and U+2029 PARAGRAPH SEPARATOR. All of them break a line, or truncate
// one, in something downstream: an interior newline is a log-injection
// hazard once the value reaches the audit log, and a log aggregator that
// splits on Unicode line terminators reads U+2028 the same way. TrimSpace
// removes them only at the ends, so an interior one has to be caught
// here. None appears in anyone's name.
//
// This rejection is load bearing for the log path, not merely for
// tidiness: these values reach the server log, sometimes inside an error
// message, and they are provider-supplied text that a user may control.
// Do not relax it.
//
// Format characters (the Unicode Cf category: zero-width joiners,
// bidirectional overrides and the like) are deliberately NOT rejected. A
// zero-width joiner appears in genuinely legitimate names in several
// scripts, a format character cannot inject a log line, and a cosmetic
// field must never be the reason a login fails. The username is the
// exception, and it is handled a layer up: identityFromClaims puts it
// through auth.ValidateUsername, whose allowlist of letters, digits and
// "_ . @ -" excludes format characters for its own reasons.
func safeClaimValue(value string, maxRunes int) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || !utf8.ValidString(trimmed) {
		return ""
	}
	if utf8.RuneCountInString(trimmed) > maxRunes {
		return ""
	}
	for _, r := range trimmed {
		if unicode.IsControl(r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) {
			return ""
		}
	}
	return trimmed
}

// describeUsernameProblem returns a phrase completing "it ..." that says
// why auth.ValidateUsername refused username. It is diagnostic only:
// auth.ValidateUsername decides, this function explains, and if the two
// ever disagree the fallback phrase is returned rather than a wrong one.
// The offending character's class is named rather than the character
// itself, because this text goes into the server log.
func describeUsernameProblem(username string) string {
	if count := utf8.RuneCountInString(username); count > maxUsernameLength {
		return fmt.Sprintf("is %d characters, over the %d-character limit",
			count, maxUsernameLength)
	}

	for index, r := range username {
		if index == 0 {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
				return "starts with " + runeClass(r) + ", not a letter or digit"
			}
			continue
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) &&
			r != '_' && r != '.' && r != '@' && r != '-' {
			return "contains " + runeClass(r) +
				", outside the permitted letters, digits and \"_ . @ -\""
		}
	}

	return "does not meet the username rules"
}

// runeClass names the Unicode category of r in words, for an operator
// reading a log line.
func runeClass(r rune) string {
	switch {
	case unicode.IsControl(r):
		return "a control character"
	case unicode.Is(unicode.Cf, r):
		return "a Unicode format character"
	case unicode.IsSpace(r):
		return "a whitespace character"
	case unicode.IsPunct(r):
		return "a punctuation character"
	case unicode.IsSymbol(r):
		return "a symbol character"
	default:
		return "a character"
	}
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
// reason takeStringClaim refuses to coerce: a group name the Workbench
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
		if safe := safeClaimValue(value, maxClaimValueLength); safe != "" {
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
		if safe := safeClaimValue(text, maxClaimValueLength); safe != "" {
			groups = append(groups, safe)
		} else {
			skipped++
		}
	}
	return groups, skipped
}
