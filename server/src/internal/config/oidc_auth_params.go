/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package config

import (
	"fmt"
	"sort"
	"strings"
)

// maxOIDCAuthParams and maxOIDCAuthParamLength bound http.auth.oidc.
// auth_params. The values travel in the authorization URL, which
// browsers and providers truncate or refuse beyond a few kilobytes, so
// anything larger is a mistake rather than a requirement.
const (
	maxOIDCAuthParams      = 16
	maxOIDCAuthParamLength = 256
)

// reservedOIDCAuthParams are the authorization request parameters the
// Workbench sets itself, or that would override them. An operator may not
// supply any of them through auth_params: replacing state, nonce or the
// PKCE challenge defeats the protections those parameters exist for,
// replacing redirect_uri, client_id, response_type or scope breaks the
// login or widens what is asked for, response_mode would deliver the
// callback in a form the server does not read, and request and
// request_uri carry a request object whose parameters take precedence
// over every one of the others at a provider that supports them.
var reservedOIDCAuthParams = map[string]bool{
	"state":                 true,
	"nonce":                 true,
	"code_challenge":        true,
	"code_challenge_method": true,
	"redirect_uri":          true,
	"client_id":             true,
	"response_type":         true,
	"scope":                 true,
	"response_mode":         true,
	"request":               true,
	"request_uri":           true,
}

// IsReservedOIDCAuthParam reports whether name is an authorization
// request parameter the Workbench controls itself, compared without
// regard to case so that "State" cannot slip past the check.
func IsReservedOIDCAuthParam(name string) bool {
	return reservedOIDCAuthParams[strings.ToLower(name)]
}

// validateOIDCAuthParams refuses an auth_params map that names a reserved
// parameter, uses a name that is not a plain token, or carries a value
// with control characters or of unreasonable length. Names are checked
// in sorted order so that the error is the same on every load.
func validateOIDCAuthParams(params map[string]string) error {
	if len(params) > maxOIDCAuthParams {
		return fmt.Errorf("http.auth.oidc.auth_params may hold at most %d parameters", maxOIDCAuthParams)
	}

	names := make([]string, 0, len(params))
	for name := range params {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if !isOIDCAuthParamName(name) {
			return fmt.Errorf("http.auth.oidc.auth_params: %q is not a valid parameter name; "+
				"use letters, digits, '_', '-' and '.' only", name)
		}
		if IsReservedOIDCAuthParam(name) {
			return fmt.Errorf("http.auth.oidc.auth_params: %q is set by the Workbench itself and cannot be overridden", name)
		}
		value := params[name]
		if len(value) > maxOIDCAuthParamLength {
			return fmt.Errorf("http.auth.oidc.auth_params: the value of %q is longer than %d bytes",
				name, maxOIDCAuthParamLength)
		}
		if strings.ContainsFunc(value, isControlRune) {
			return fmt.Errorf("http.auth.oidc.auth_params: the value of %q contains a control character", name)
		}
	}
	return nil
}

// isOIDCAuthParamName reports whether name is a non-empty run of ASCII
// letters, digits, '_', '-' and '.', which covers every parameter the
// OAuth and OpenID Connect specifications and the major providers define.
func isOIDCAuthParamName(name string) bool {
	if name == "" || len(name) > maxOIDCAuthParamLength {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_', r == '-', r == '.':
		default:
			return false
		}
	}
	return true
}

func isControlRune(r rune) bool {
	return r < 0x20 || r == 0x7f
}
