/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestIPExtractorTrustsRequest covers the per-request trust decision the
// cookie handlers read before honoring X-Forwarded-Proto. It has to
// answer the same question ExtractIP answers, since an extractor is
// built on every deployment and so its mere existence says nothing about
// whether this request came through a proxy worth believing.
func TestIPExtractorTrustsRequest(t *testing.T) {
	request := func(remoteAddr string) *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback", nil)
		req.RemoteAddr = remoteAddr
		return req
	}

	cases := map[string]struct {
		extractor *IPExtractor
		request   *http.Request
		want      bool
	}{
		"from a listed proxy": {
			extractor: NewIPExtractor([]string{"10.0.0.0/8"}),
			request:   request("10.1.2.3:4000"),
			want:      true,
		},
		"from a bare address listed without a prefix length": {
			extractor: NewIPExtractor([]string{"10.1.2.3"}),
			request:   request("10.1.2.3:4000"),
			want:      true,
		},
		"from somewhere else": {
			extractor: NewIPExtractor([]string{"10.0.0.0/8"}),
			request:   request("203.0.113.9:4000"),
		},
		"no proxies configured": {
			extractor: NewIPExtractor(nil),
			request:   request("10.1.2.3:4000"),
		},
		"no extractor at all": {
			request: request("10.1.2.3:4000"),
		},
		"no request": {
			extractor: NewIPExtractor([]string{"10.0.0.0/8"}),
		},
		"an unparseable remote address": {
			extractor: NewIPExtractor([]string{"10.0.0.0/8"}),
			request:   request(""),
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			if got := testCase.extractor.TrustsRequest(testCase.request); got != testCase.want {
				t.Errorf("TrustsRequest = %v, want %v", got, testCase.want)
			}
		})
	}
}
