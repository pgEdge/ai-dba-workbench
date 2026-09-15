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

// requestIsSecure reports whether a request reached the server over
// HTTPS, and is the single place the rule lives: every handler that sets
// a cookie decides the Secure attribute through this function, so the
// trust placed in X-Forwarded-Proto cannot drift between them.
//
// Three things can establish it. The server may terminate TLS itself
// (tlsEnabled), in which case every request it serves is secure. Go's
// HTTP server sets r.TLS on a connection it terminated, which covers a
// server whose TLS was configured somewhere this caller cannot see.
// Finally, a reverse proxy that terminated TLS upstream can say so with
// X-Forwarded-Proto.
//
// That last one is client-supplied text on any request that did not
// actually come through such a proxy, so it is consulted only when
// ipExtractor.TrustsRequest says this particular request arrived from a
// configured trusted proxy. The per-request check is the point: an
// extractor is constructed on every deployment, trusted proxy list or
// not, so "an extractor exists" would be true everywhere and would let
// any client set the header and pick the answer. That matters more than
// the Secure attribute alone, because the same answer also selects which
// state cookie name is written and read, and the "__Host-" prefixed name
// is what stops a compromised sibling subdomain overwriting the cookie.
//
// The header value is compared without regard to case: a proxy emitting
// "HTTPS" is describing a TLS-terminated deployment just as much as one
// emitting "https", and reading it as plain HTTP would quietly drop both
// the Secure attribute and the cookie prefix, which is the dangerous
// direction to be wrong in.
func requestIsSecure(r *http.Request, tlsEnabled bool, ipExtractor *auth.IPExtractor) bool {
	if tlsEnabled {
		return true
	}
	if r.TLS != nil {
		return true
	}
	if ipExtractor.TrustsRequest(r) &&
		strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return true
	}
	return false
}
