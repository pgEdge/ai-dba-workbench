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

import "net/http"

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
// X-Forwarded-Proto, but that header is client-supplied text on any
// request that did not come through such a proxy, so it is consulted
// only when trustProxyHeaders says the deployment has a trusted proxy
// list configured. Without that guard, anyone could set the header and
// choose the flag themselves; that direction is harmless for Secure on
// its own, but the same answer also selects the "__Host-" state cookie
// name, and letting a caller pick which cookie name the server reads is
// not harmless at all.
func requestIsSecure(r *http.Request, tlsEnabled, trustProxyHeaders bool) bool {
	if tlsEnabled {
		return true
	}
	if r.TLS != nil {
		return true
	}
	if trustProxyHeaders && r.Header.Get("X-Forwarded-Proto") == "https" {
		return true
	}
	return false
}
