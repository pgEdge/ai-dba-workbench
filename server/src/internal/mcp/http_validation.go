/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/pgedge/ai-workbench/pkg/mcp"
)

// mcpEndpointPath is the single Streamable HTTP endpoint.
const mcpEndpointPath = "/mcp/v1"

// defaultHTTPProtocolVersion is the version the transport spec says to
// assume for a request without an MCP-Protocol-Version header.
const defaultHTTPProtocolVersion = "2025-03-26"

// protocolVersionHeader is the canonical form of MCP-Protocol-Version.
var protocolVersionHeader = http.CanonicalHeaderKey("MCP-Protocol-Version")

// maxEchoedVersionLen bounds how much of a rejected header value is
// echoed back in the error.
const maxEchoedVersionLen = 64

// requestProtocolVersion returns the protocol version a request
// declares and whether it is acceptable. An absent header means
// defaultHTTPProtocolVersion; an empty, repeated or unsupported one is
// refused, and the returned value is then what to report as requested.
func requestProtocolVersion(h http.Header) (string, bool) {
	values, present := h[protocolVersionHeader]
	if !present {
		return defaultHTTPProtocolVersion, true
	}
	requested := strings.Join(values, ", ")
	if len(requested) > maxEchoedVersionLen {
		requested = requested[:maxEchoedVersionLen]
	}
	if len(values) != 1 || !mcp.IsSupportedProtocolVersion(values[0]) {
		return requested, false
	}
	return values[0], true
}

// writeHTTPError writes a JSON-RPC error body with a non-200 status.
func writeHTTPError(w http.ResponseWriter, status int, id any, code int, message string, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(createErrorResponse(id, code, message, data)); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: Failed to encode error response: %v\n", err)
	}
}

// mcpOriginMiddleware refuses a request to the MCP endpoint whose
// Origin header is present but not allowed, as the transport spec
// requires against DNS rebinding. A request without Origin, which is
// what non-browser MCP clients send, passes. Other paths are untouched:
// the browser client calls the REST API, not this endpoint.
func mcpOriginMiddleware(allowedOrigin string) func(http.Handler) http.Handler {
	allowed := normalizeOrigin(allowedOrigin)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == mcpEndpointPath && !originAllowed(r.Header, allowedOrigin, allowed) {
				writeHTTPError(w, http.StatusForbidden, nil, -32600, "Forbidden: invalid Origin", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// originAllowed reports whether the request's Origin header is absent
// or matches the configured CORS origin. With no CORS origin there is
// no browser origin the endpoint serves, so any Origin is refused; the
// wildcard, which startup accepts only with auth disabled, allows all.
func originAllowed(h http.Header, configured, normalized string) bool {
	values, present := h["Origin"]
	if !present {
		return true
	}
	if configured == "*" {
		return true
	}
	if len(values) != 1 || normalized == "" {
		return false
	}
	return normalizeOrigin(values[0]) == normalized
}

// normalizeOrigin reduces an origin to lower-case scheme://host[:port],
// or "" when it has no http(s) scheme and host, as with "null".
func normalizeOrigin(origin string) string {
	u, err := url.Parse(strings.TrimSpace(origin))
	if err != nil || u.Host == "" {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	return scheme + "://" + strings.ToLower(u.Host)
}
