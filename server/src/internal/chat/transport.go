/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package chat

import "net/http"

// HeaderTransport wraps http.RoundTripper to inject custom headers.
type HeaderTransport struct {
	inner   http.RoundTripper
	headers map[string]string
}

// NewHeaderTransport creates a transport that injects the given headers.
func NewHeaderTransport(headers map[string]string) *HeaderTransport {
	return &HeaderTransport{
		inner:   http.DefaultTransport,
		headers: headers,
	}
}

// RoundTrip implements http.RoundTripper.
func (t *HeaderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	for k, v := range t.headers {
		if clone.Header.Get(k) == "" {
			clone.Header.Set(k, v)
		}
	}
	return t.inner.RoundTrip(clone)
}
