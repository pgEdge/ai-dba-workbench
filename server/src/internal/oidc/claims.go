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
)

// emailClaim is the standard OpenID Connect claim carrying the end
// user's email address. It is read regardless of which claim the
// operator configured as the username, because parts of the Workbench
// (allowed_email_domains, for one) want the address itself rather than
// whatever the username happens to be.
const emailClaim = "email"

// identityFromClaims turns the decoded claims of a verified ID token
// into an Identity. issuer and subject come from the verified token
// rather than from the claim map, so they cannot be influenced by a
// claim of the same name appearing twice in a decoded payload.
//
// An absent or empty username claim is an error: there is nothing to key
// a Workbench user on, and quietly provisioning a user with an empty
// name would be worse than refusing the login.
func (p *Provider) identityFromClaims(issuer, subject string, claims map[string]any) (*Identity, error) {
	username := stringClaim(claims, p.usernameClaim)
	if username == "" {
		return nil, fmt.Errorf("ID token has no usable %q claim to use as a username", p.usernameClaim)
	}

	return &Identity{
		Issuer:      issuer,
		Subject:     subject,
		Username:    username,
		DisplayName: stringClaim(claims, p.displayNameClaim),
		Email:       stringClaim(claims, emailClaim),
		Groups:      stringsClaim(claims, p.groupsClaim),
	}, nil
}

// stringClaim returns the named claim as a trimmed string. It returns an
// empty string when name is empty (the claim is not configured), when
// the claim is absent, or when it holds anything other than a string: a
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
	return strings.TrimSpace(value)
}

// stringsClaim returns the named claim as a slice of strings, tolerating
// the shapes identity providers actually send for a groups claim: an
// array of strings, an array of any holding strings (what a JSON decode
// into map[string]any always produces), or a single bare string for a
// user in exactly one group. An absent claim, an unconfigured claim name
// and a claim of any other shape all yield nil rather than an error,
// because group membership is advisory: a user with no groups simply
// gets no group-derived privileges.
//
// Non-string elements of an array are skipped rather than stringified,
// for the same reason stringClaim refuses to coerce: a group name that
// the Workbench invented cannot match anything an operator configured.
func stringsClaim(claims map[string]any, name string) []string {
	if name == "" {
		return nil
	}

	switch value := claims[name].(type) {
	case []string:
		return collectStrings(value)
	case []any:
		return collectAnyStrings(value)
	case string:
		return collectStrings([]string{value})
	default:
		return nil
	}
}

func collectStrings(values []string) []string {
	var result []string
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func collectAnyStrings(values []any) []string {
	var result []string
	for _, value := range values {
		text, ok := value.(string)
		if !ok {
			continue
		}
		if trimmed := strings.TrimSpace(text); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
