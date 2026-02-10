/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package overview

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/database"
)

// --- helpers ---------------------------------------------------------------

// newTestSnapshot returns a minimal EstateSnapshot for test use.
func newTestSnapshot() *database.EstateSnapshot {
	return &database.EstateSnapshot{
		Timestamp:         time.Now().UTC(),
		ServerTotal:       3,
		ServerOnline:      2,
		ServerOffline:     1,
		ServerWarning:     0,
		AlertTotal:        2,
		AlertCritical:     1,
		AlertWarning:      1,
		AlertInfo:         0,
		Servers:           []database.EstateServerSummary{},
		TopAlerts:         []database.EstateAlertSummary{},
		ActiveBlackouts:   []database.EstateBlackoutSummary{},
		UpcomingBlackouts: []database.EstateBlackoutSummary{},
		RecentEvents:      []database.EstateEventSummary{},
	}
}

// newTestOverview wraps a snapshot in an Overview with valid timestamps.
func newTestOverview(summary string) *Overview {
	now := time.Now().UTC()
	return &Overview{
		Summary:     summary,
		GeneratedAt: now,
		StaleAt:     now.Add(5 * time.Minute),
		Snapshot:    newTestSnapshot(),
	}
}

// newTestHandler creates a Handler backed by a stubbed Generator whose
// fields are set directly. The Generator has no datastore or LLM config,
// so only GetOverview (reading g.current) works without further setup.
func newTestHandler() *Handler {
	g := &Generator{
		scopedCache: make(map[string]*scopedEntry),
	}
	return NewHandler(g)
}

// doRequest sends an HTTP request to the handler and returns the recorder.
func doRequest(t *testing.T, h *Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	rr := httptest.NewRecorder()
	h.handleOverview(rr, req)
	return rr
}

// --- handler tests ---------------------------------------------------------

func TestHandleOverview_MethodNotAllowed(t *testing.T) {
	h := newTestHandler()

	methods := []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}
	for _, m := range methods {
		t.Run(m, func(t *testing.T) {
			rr := doRequest(t, h, m, "/api/v1/overview")
			if rr.Code != http.StatusMethodNotAllowed {
				t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, rr.Code)
			}
		})
	}
}

func TestHandleOverview_EstateWide(t *testing.T) {
	h := newTestHandler()
	h.generator.mu.Lock()
	h.generator.current = newTestOverview("All systems healthy.")
	h.generator.mu.Unlock()

	rr := doRequest(t, h, http.MethodGet, "/api/v1/overview")

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var resp Overview
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Summary != "All systems healthy." {
		t.Errorf("unexpected summary: %s", resp.Summary)
	}
}

func TestHandleOverview_EstateWideGenerating(t *testing.T) {
	h := newTestHandler()
	// current is nil by default, so the handler should return "generating".

	rr := doRequest(t, h, http.MethodGet, "/api/v1/overview")

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var resp generatingResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Status != "generating" {
		t.Errorf("expected status 'generating', got %q", resp.Status)
	}
	if resp.Summary != nil {
		t.Errorf("expected nil summary, got %v", resp.Summary)
	}
}

func TestHandleOverview_ContentTypeAndLinkHeaders(t *testing.T) {
	h := newTestHandler()

	rr := doRequest(t, h, http.MethodGet, "/api/v1/overview")

	ct := rr.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	link := rr.Header().Get("Link")
	expected := fmt.Sprintf("<%s>; rel=\"service-desc\"", openAPISpecPath)
	if link != expected {
		t.Errorf("expected Link header %q, got %q", expected, link)
	}
}

func TestHandleOverview_InvalidScopeType(t *testing.T) {
	h := newTestHandler()

	rr := doRequest(t, h, http.MethodGet, "/api/v1/overview?scope_type=invalid&scope_id=1")

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestHandleOverview_MissingScopeID(t *testing.T) {
	h := newTestHandler()

	rr := doRequest(t, h, http.MethodGet, "/api/v1/overview?scope_type=server")

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestHandleOverview_MissingScopeType(t *testing.T) {
	h := newTestHandler()

	rr := doRequest(t, h, http.MethodGet, "/api/v1/overview?scope_id=5")

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestHandleOverview_InvalidScopeID(t *testing.T) {
	h := newTestHandler()

	tests := []struct {
		name  string
		query string
	}{
		{"non-numeric", "?scope_type=server&scope_id=abc"},
		{"negative", "?scope_type=server&scope_id=-1"},
		{"zero", "?scope_type=server&scope_id=0"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := doRequest(t, h, http.MethodGet, "/api/v1/overview"+tc.query)
			if rr.Code != http.StatusBadRequest {
				t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
			}
		})
	}
}

func TestHandleOverview_InvalidConnectionIDs(t *testing.T) {
	h := newTestHandler()

	tests := []struct {
		name  string
		query string
	}{
		{"non-numeric", "?connection_ids=abc,def"},
		{"mixed", "?connection_ids=1,abc,3"},
		{"negative", "?connection_ids=-1,2"},
		{"zero", "?connection_ids=0,1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := doRequest(t, h, http.MethodGet, "/api/v1/overview"+tc.query)
			if rr.Code != http.StatusBadRequest {
				t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
			}
		})
	}
}

func TestHandleOverview_EmptyConnectionIDs(t *testing.T) {
	// An empty connection_ids parameter is treated as absent by the
	// Go URL query parser, so the handler falls through to the
	// estate-wide path and returns "generating" (200).
	h := newTestHandler()

	rr := doRequest(t, h, http.MethodGet, "/api/v1/overview?connection_ids=")

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d for empty connection_ids, got %d",
			http.StatusOK, rr.Code)
	}
}

func TestHandleOverview_WhitespaceOnlyConnectionIDs(t *testing.T) {
	h := newTestHandler()

	rr := doRequest(t, h, http.MethodGet, "/api/v1/overview?connection_ids=%20%20")

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status %d for whitespace-only connection_ids, got %d",
			http.StatusBadRequest, rr.Code)
	}
}

// --- parseConnectionIDs tests -----------------------------------------------

func TestParseConnectionIDs(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []int
		wantErr bool
	}{
		{
			name:  "single ID",
			input: "1",
			want:  []int{1},
		},
		{
			name:  "multiple IDs",
			input: "1,3,5",
			want:  []int{1, 3, 5},
		},
		{
			name:  "whitespace around IDs",
			input: " 2 , 4 , 6 ",
			want:  []int{2, 4, 6},
		},
		{
			name:  "trailing comma ignored",
			input: "7,8,",
			want:  []int{7, 8},
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "whitespace only",
			input:   "   ",
			wantErr: true,
		},
		{
			name:    "non-numeric",
			input:   "abc",
			wantErr: true,
		},
		{
			name:    "mixed valid and invalid",
			input:   "1,abc,3",
			wantErr: true,
		},
		{
			name:    "negative ID",
			input:   "-1,2",
			wantErr: true,
		},
		{
			name:    "zero ID",
			input:   "0,1",
			wantErr: true,
		},
		{
			name:    "all commas",
			input:   ",,,",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseConnectionIDs(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error for input %q, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for input %q: %v", tc.input, err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("expected %d IDs, got %d", len(tc.want), len(got))
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("ID[%d]: expected %d, got %d", i, tc.want[i], got[i])
				}
			}
		})
	}
}
