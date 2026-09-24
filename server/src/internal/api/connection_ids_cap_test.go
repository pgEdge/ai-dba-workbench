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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// connectionIDList renders n sequential connection IDs as the
// comma-separated value of a connection_ids query parameter.
func connectionIDList(n int) string {
	parts := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		parts = append(parts, fmt.Sprintf("%d", i))
	}
	return strings.Join(parts, ",")
}

// tooManyConnectionIDsError is the 400 body the cap returns. It is spelled
// out here rather than built from maxConnectionIDsPerRequest so that a
// change to either the limit or the wording fails the test loudly.
const tooManyConnectionIDsError = "Too many connection_ids: at most 100 are allowed per request"

func TestCheckConnectionIDCount(t *testing.T) {
	cases := []struct {
		name  string
		count int
		want  bool
	}{
		{name: "empty list", count: 0, want: true},
		{name: "well under the cap", count: 3, want: true},
		{name: "one below the cap", count: maxConnectionIDsPerRequest - 1, want: true},
		{name: "exactly the cap", count: maxConnectionIDsPerRequest, want: true},
		{name: "one over the cap", count: maxConnectionIDsPerRequest + 1},
		{name: "far over the cap", count: maxConnectionIDsPerRequest * 10},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ids := make([]int, tc.count)
			rec := httptest.NewRecorder()

			if got := CheckConnectionIDCount(rec, ids); got != tc.want {
				t.Fatalf("CheckConnectionIDCount() = %v, want %v", got, tc.want)
			}
			if tc.want {
				if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
					t.Errorf("accepted list wrote a response: status %d, body %q",
						rec.Code, rec.Body.String())
				}
				return
			}
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}
			if resp := decodeError(t, rec); resp.Error != tooManyConnectionIDsError {
				t.Errorf("error = %q, want %q", resp.Error, tooManyConnectionIDsError)
			}
		})
	}
}

// TestHandleMetricsQuery_ConnectionIDsCap checks the cap on
// /api/v1/metrics/query. The handler carries a nil datastore, so any
// per-connection database work would dereference it; a request that is
// rejected by the cap, or that fails later on the missing probe_name,
// proves that no such work was attempted.
func TestHandleMetricsQuery_ConnectionIDsCap(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "well under the cap",
			url:  "/api/v1/metrics/query?connection_ids=" + connectionIDList(3),
			want: "probe_name is required",
		},
		{
			name: "exactly at the cap",
			url: "/api/v1/metrics/query?connection_ids=" +
				connectionIDList(maxConnectionIDsPerRequest),
			want: "probe_name is required",
		},
		{
			name: "one over the cap",
			url: "/api/v1/metrics/query?connection_ids=" +
				connectionIDList(maxConnectionIDsPerRequest+1),
			want: tooManyConnectionIDsError,
		},
		{
			name: "single connection_id is unaffected",
			url:  "/api/v1/metrics/query?connection_id=1",
			want: "probe_name is required",
		},
		{
			name: "malformed connection_ids still fails the parse",
			url:  "/api/v1/metrics/query?connection_ids=1,abc",
			want: "Invalid connection_ids",
		},
		{
			name: "malformed connection_id still fails the parse",
			url:  "/api/v1/metrics/query?connection_id=abc",
			want: "Invalid connection_id",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			rec := httptest.NewRecorder()

			handler := &MetricsHandler{}
			handler.handleMetricsQuery(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}
			if resp := decodeError(t, rec); resp.Error != tc.want {
				t.Errorf("error = %q, want %q", resp.Error, tc.want)
			}
		})
	}
}

// TestHandlePerfSummary_ConnectionIDsCap checks the cap on
// /api/v1/metrics/performance-summary. As above the datastore is nil, and
// the accepted cases carry a deliberately invalid time_range so that they
// return before the handler reaches the pool: they assert only that the
// cap let them through.
func TestHandlePerfSummary_ConnectionIDsCap(t *testing.T) {
	const badRange = "&time_range=99z"

	cases := []struct {
		name    string
		url     string
		wantCap bool
	}{
		{
			name: "well under the cap",
			url: "/api/v1/metrics/performance-summary?connection_ids=" +
				connectionIDList(3) + badRange,
		},
		{
			name: "exactly at the cap",
			url: "/api/v1/metrics/performance-summary?connection_ids=" +
				connectionIDList(maxConnectionIDsPerRequest) + badRange,
		},
		{
			name: "one over the cap",
			url: "/api/v1/metrics/performance-summary?connection_ids=" +
				connectionIDList(maxConnectionIDsPerRequest+1) + badRange,
			wantCap: true,
		},
		{
			name: "single connection_id is unaffected",
			url:  "/api/v1/metrics/performance-summary?connection_id=1" + badRange,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			rec := httptest.NewRecorder()

			handler := &PerfSummaryHandler{}
			handler.handlePerfSummary(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}
			resp := decodeError(t, rec)
			if tc.wantCap && resp.Error != tooManyConnectionIDsError {
				t.Errorf("error = %q, want %q", resp.Error, tooManyConnectionIDsError)
			}
			if !tc.wantCap && resp.Error == tooManyConnectionIDsError {
				t.Errorf("request was rejected by the cap: %q", resp.Error)
			}
		})
	}
}

// TestHandlePerfSummary_ConnectionIDsCap_NoDatabaseWork runs an oversized
// request against a handler wired to a real pool and checks that the pool
// was never acquired from, which is the property the cap exists to
// guarantee: the rejection happens before the RBAC loop, the connection
// name lookups and the read-only transaction.
func TestHandlePerfSummary_ConnectionIDsCap_NoDatabaseWork(t *testing.T) {
	h, pool, cleanup := newPerfEndpointTestHandler(t)
	defer cleanup()

	before := pool.Stat().AcquireCount()

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/performance-summary?connection_ids="+
			connectionIDList(maxConnectionIDsPerRequest+1), nil)
	rec := httptest.NewRecorder()
	h.handlePerfSummary(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if resp := decodeError(t, rec); resp.Error != tooManyConnectionIDsError {
		t.Errorf("error = %q, want %q", resp.Error, tooManyConnectionIDsError)
	}
	if after := pool.Stat().AcquireCount(); after != before {
		t.Errorf("pool acquisitions = %d, want %d: the request did database work",
			after, before)
	}
}
