/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package api

import (
	"net/http"
	"strings"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// requestIsSecure reports whether a cookie set in answer to this request
// should carry the Secure attribute, and is the single place that rule
// lives: every handler that sets a cookie decides the attribute through
// this function, so the trust placed in X-Forwarded-Proto cannot drift
// between them.
//
// Three things can establish it. The server may terminate TLS itself
// (tlsEnabled), in which case every request it serves is secure. Go's
// HTTP server sets r.TLS on a connection it terminated, which covers a
// server whose TLS was configured somewhere this caller cannot see.
// Finally, a reverse proxy that terminated TLS upstream can say so with
// X-Forwarded-Proto, which is honored whenever an extractor exists at
// all, whether or not the request came from a proxy on its trusted list.
//
// That is deliberately lenient, because the header is client-supplied
// text on any request that did not come through a proxy, and the
// question is what a lie costs. Over-applying Secure costs nothing: a
// browser refuses a Secure cookie delivered over plain HTTP, so a client
// that forges the header on an HTTP deployment breaks its own login and
// nobody else's. Under-applying it is the hazard the attribute exists to
// prevent, and that is what a rule demanding a configured trusted proxy
// list would do on every TLS-terminating deployment whose operator has
// not set http.trusted_proxies, which the shipped example configuration
// leaves commented out: the session cookie would silently lose Secure
// and a plain-HTTP request to the same host would carry the session
// token in clear. The stricter rule is applied where a lie would cost
// something, in requestIsProvablySecure.
//
// The header value is compared without regard to case: a proxy emitting
// "HTTPS" is describing a TLS-terminated deployment just as much as one
// emitting "https", and reading it as plain HTTP would quietly drop the
// Secure attribute, which is the dangerous direction to be wrong in.
func requestIsSecure(r *http.Request, tlsEnabled bool, ipExtractor *auth.IPExtractor) bool {
	if tlsEnabled || r.TLS != nil {
		return true
	}
	return ipExtractor != nil && forwardedProtoIsHTTPS(r)
}

// requestIsProvablySecure reports whether the request is known, rather
// than merely claimed, to have arrived over HTTPS. It is what decides
// whether a cookie may use the "__Host-" name prefix, which is the guard
// against a compromised sibling subdomain overwriting the OIDC state
// cookie.
//
// It differs from requestIsSecure in one respect: X-Forwarded-Proto is
// believed only when ipExtractor.TrustsRequest says this particular
// request arrived from a configured trusted proxy. The per-request check
// is the point. An extractor is constructed on every deployment, trusted
// proxy list or not, so "an extractor exists" would be true everywhere
// and would let any client set the header and pick the answer; and
// unlike the Secure attribute, the answer here selects which cookie name
// is written and read, so it is not one to let a client choose.
//
// Every request this reports true for, requestIsSecure also reports true
// for, so a prefixed cookie always carries the Secure attribute the
// prefix requires.
func requestIsProvablySecure(r *http.Request, tlsEnabled bool, ipExtractor *auth.IPExtractor) bool {
	if tlsEnabled || r.TLS != nil {
		return true
	}
	return ipExtractor.TrustsRequest(r) && forwardedProtoIsHTTPS(r)
}

// forwardedProtoIsHTTPS reports whether the request carries an
// X-Forwarded-Proto header naming HTTPS, in any letter case.
func forwardedProtoIsHTTPS(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
