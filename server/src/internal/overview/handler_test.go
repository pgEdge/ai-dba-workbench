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
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
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
	return NewHandler(g, NewHub())
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

// --- intersectVisible tests -------------------------------------------------

// TestIntersectVisible is the unit-level guard for the #35 follow-up
// audit on scoped overview snapshots. scopeVisible computes the
// intersection of a cluster/group's member connection IDs with the
// caller's visible set and routes the summary generator through the
// intersected list so two users with different visibility never share
// a cache entry. The result is deduplicated and sorted in ascending
// order so that equivalent inputs (different order or with duplicates)
// yield the same cache key.
func TestIntersectVisible(t *testing.T) {
	tests := []struct {
		name    string
		members []int
		visible map[int]bool
		want    []int
	}{
		{
			name:    "full overlap sorted ascending",
			members: []int{3, 1, 2},
			visible: map[int]bool{1: true, 2: true, 3: true},
			want:    []int{1, 2, 3},
		},
		{
			name:    "one of three visible",
			members: []int{10, 20, 30},
			visible: map[int]bool{20: true},
			want:    []int{20},
		},
		{
			name:    "no members visible",
			members: []int{10, 20, 30},
			visible: map[int]bool{99: true},
			want:    []int{},
		},
		{
			name:    "empty member list",
			members: []int{},
			visible: map[int]bool{1: true},
			want:    []int{},
		},
		{
			name:    "nil visible map yields empty",
			members: []int{1, 2, 3},
			visible: nil,
			want:    []int{},
		},
		{
			name:    "duplicates are removed",
			members: []int{3, 1, 2, 1, 3},
			visible: map[int]bool{1: true, 2: true, 3: true},
			want:    []int{1, 2, 3},
		},
		{
			name:    "duplicates removed with partial visibility",
			members: []int{5, 3, 5, 1, 3},
			visible: map[int]bool{1: true, 3: true},
			want:    []int{1, 3},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := intersectVisible(tc.members, tc.visible)
			if len(got) != len(tc.want) {
				t.Fatalf("expected %d ids, got %d (%v)", len(tc.want), len(got), got)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("id[%d]: expected %d, got %d", i, tc.want[i], got[i])
				}
			}
		})
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

// --- SSE handler tests ------------------------------------------------------

// --- forced refresh handler tests -------------------------------------------

func TestHandleOverview_EstateWideRefresh(t *testing.T) {
	// When refresh=true is provided for the estate-wide overview the
	// handler calls ForceRefresh before serving the cached overview.
	// ForceRefresh calls refresh(true) which accesses the datastore;
	// with a nil datastore the call panics. Catching the panic proves
	// that ForceRefresh was invoked (cache bypass) rather than silently
	// returning the cached overview.
	h := newTestHandler()
	h.generator.ctx = context.Background()
	h.generator.mu.Lock()
	h.generator.current = newTestOverview("Cached estate overview.")
	h.generator.mu.Unlock()

	panicked := invokePanics(func() {
		doRequest(t, h, http.MethodGet, "/api/v1/overview?refresh=true")
	})
	if !panicked {
		t.Error("expected ForceRefresh to panic on nil datastore, proving cache bypass")
	}
}

func TestHandleOverview_ScopedRefreshTrue(t *testing.T) {
	// Verify that refresh=true for a scoped request bypasses the cache.
	// A fresh cached entry exists for server:1, so refresh=false would
	// return it. With refresh=true the generator attempts to regenerate
	// from the datastore, which panics on nil datastore.
	h := newTestHandler()
	h.generator.ctx = context.Background()

	// Pre-populate a fresh cached scoped entry.
	now := time.Now().UTC()
	h.generator.mu.Lock()
	h.generator.scopedCache["server:1"] = &scopedEntry{
		overview: &Overview{
			Summary:     "Cached scoped overview.",
			GeneratedAt: now,
			StaleAt:     now.Add(5 * time.Minute),
			Snapshot:    newTestSnapshot(),
		},
		lastAccess: now,
	}
	h.generator.mu.Unlock()

	// refresh=false should return the cached entry.
	rr := doRequest(t, h, http.MethodGet, "/api/v1/overview?scope_type=server&scope_id=1")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for cached scoped overview, got %d", rr.Code)
	}
	var resp Overview
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Summary != "Cached scoped overview." {
		t.Errorf("expected cached summary, got %q", resp.Summary)
	}

	// refresh=true should bypass the cache and attempt regeneration,
	// which panics on nil datastore.
	panicked := invokePanics(func() {
		doRequest(t, h, http.MethodGet, "/api/v1/overview?scope_type=server&scope_id=1&refresh=true")
	})
	if !panicked {
		t.Error("expected GetScopedSummary with force=true to panic on nil datastore, proving cache bypass")
	}
}

func TestHandleOverview_ConnectionsRefreshTrue(t *testing.T) {
	// Verify that refresh=true for a connections request bypasses the
	// cache. A fresh cached entry exists, so refresh=false returns it.
	// With refresh=true the generator tries to regenerate and panics.
	h := newTestHandler()
	h.generator.ctx = context.Background()

	now := time.Now().UTC()
	h.generator.mu.Lock()
	h.generator.scopedCache["connections:1,2"] = &scopedEntry{
		overview: &Overview{
			Summary:     "Cached connections overview.",
			GeneratedAt: now,
			StaleAt:     now.Add(5 * time.Minute),
			Snapshot:    newTestSnapshot(),
		},
		lastAccess: now,
	}
	h.generator.mu.Unlock()

	// refresh=false should return the cached entry.
	rr := doRequest(t, h, http.MethodGet, "/api/v1/overview?connection_ids=1,2")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for cached connections overview, got %d", rr.Code)
	}
	var resp Overview
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Summary != "Cached connections overview." {
		t.Errorf("expected cached summary, got %q", resp.Summary)
	}

	// refresh=true should bypass the cache and attempt regeneration,
	// which panics on nil datastore.
	panicked := invokePanics(func() {
		doRequest(t, h, http.MethodGet, "/api/v1/overview?connection_ids=1,2&refresh=true")
	})
	if !panicked {
		t.Error("expected GetConnectionsSummary with force=true to panic on nil datastore, proving cache bypass")
	}
}

// invokePanics calls fn and returns true if fn panicked, false otherwise.
func invokePanics(fn func()) (panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			panicked = true
		}
	}()
	fn()
	return false
}

// --- SSE handler tests ------------------------------------------------------

func TestHandleSSE_MethodNotAllowed(t *testing.T) {
	h := newTestHandler()
	methods := []string{http.MethodPost, http.MethodPut, http.MethodDelete}
	for _, m := range methods {
		t.Run(m, func(t *testing.T) {
			req := httptest.NewRequest(m, "/api/v1/overview/stream", nil)
			rr := httptest.NewRecorder()
			h.handleSSE(rr, req)
			if rr.Code != http.StatusMethodNotAllowed {
				t.Errorf("expected %d, got %d", http.StatusMethodNotAllowed, rr.Code)
			}
		})
	}
}

func TestHandleSSE_InvalidScopeParams(t *testing.T) {
	h := newTestHandler()
	tests := []struct {
		name  string
		query string
	}{
		{"missing scope_id", "?scope_type=server"},
		{"missing scope_type", "?scope_id=1"},
		{"invalid scope_type", "?scope_type=invalid&scope_id=1"},
		{"invalid scope_id", "?scope_type=server&scope_id=abc"},
		{"zero scope_id", "?scope_type=server&scope_id=0"},
		{"invalid connection_ids", "?connection_ids=abc"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/overview/stream"+tc.query, nil)
			rr := httptest.NewRecorder()
			h.handleSSE(rr, req)
			if rr.Code != http.StatusBadRequest {
				t.Errorf("expected %d, got %d", http.StatusBadRequest, rr.Code)
			}
		})
	}
}

func TestHandleSSE_ImmediateCachedOverview(t *testing.T) {
	// Use a real HTTP test server for proper SSE streaming.
	g := &Generator{
		scopedCache: make(map[string]*scopedEntry),
	}
	hub := NewHub()
	h := NewHandler(g, hub)

	// Set a cached estate overview.
	g.mu.Lock()
	g.current = newTestOverview("Estate is healthy.")
	g.mu.Unlock()

	// Create test server.
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/overview/stream", h.handleSSE)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Make SSE request with a context that times out.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/overview/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %q", resp.Header.Get("Content-Type"))
	}

	// Read until we get the first SSE event.
	scanner := bufio.NewScanner(resp.Body)
	var foundEvent bool
	var dataLine string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			dataLine = strings.TrimPrefix(line, "data: ")
			foundEvent = true
			break
		}
	}

	if !foundEvent {
		t.Fatal("did not receive an SSE event")
	}

	var overview Overview
	if err := json.Unmarshal([]byte(dataLine), &overview); err != nil {
		t.Fatalf("failed to parse SSE data: %v", err)
	}
	if overview.Summary != "Estate is healthy." {
		t.Errorf("expected summary 'Estate is healthy.', got %q", overview.Summary)
	}
}

func TestHandleSSE_BroadcastDelivery(t *testing.T) {
	g := &Generator{
		scopedCache: make(map[string]*scopedEntry),
	}
	hub := NewHub()
	h := NewHandler(g, hub)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/overview/stream", h.handleSSE)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/overview/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Wait briefly for the subscriber to register.
	time.Sleep(100 * time.Millisecond)

	// Broadcast an overview.
	ov := newTestOverview("New broadcast overview.")
	hub.Broadcast(ov, "")

	// Read the SSE event.
	scanner := bufio.NewScanner(resp.Body)
	var dataLine string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			dataLine = strings.TrimPrefix(line, "data: ")
			break
		}
	}

	var overview Overview
	if err := json.Unmarshal([]byte(dataLine), &overview); err != nil {
		t.Fatalf("failed to parse SSE data: %v", err)
	}
	if overview.Summary != "New broadcast overview." {
		t.Errorf("expected summary 'New broadcast overview.', got %q", overview.Summary)
	}
}

func TestHandleSSE_SubscriberCleanupOnDisconnect(t *testing.T) {
	g := &Generator{
		scopedCache: make(map[string]*scopedEntry),
	}
	hub := NewHub()
	h := NewHandler(g, hub)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/overview/stream", h.handleSSE)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Connect.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/overview/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	// Wait for subscriber to register.
	time.Sleep(100 * time.Millisecond)
	if hub.Count() != 1 {
		t.Fatalf("expected 1 subscriber, got %d", hub.Count())
	}

	// Disconnect by closing the response body and canceling context.
	resp.Body.Close()
	cancel()

	// Wait for cleanup.
	time.Sleep(200 * time.Millisecond)
	if hub.Count() != 0 {
		t.Errorf("expected 0 subscribers after disconnect, got %d", hub.Count())
	}
}

// --- RBAC estate-wide filtering tests --------------------------------------

// newRBACTestStore creates a throwaway SQLite auth store for RBAC tests.
// The caller must invoke the returned cleanup function when finished.
func newRBACTestStore(t *testing.T) (*auth.AuthStore, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "overview-rbac-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	store, err := auth.NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("NewAuthStore: %v", err)
	}
	return store, func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}
}

// doRequestWithContext sends an HTTP request with context to the handler.
func doRequestWithContext(t *testing.T, h *Handler, ctx context.Context, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil).WithContext(ctx)
	rr := httptest.NewRecorder()
	h.handleOverview(rr, req)
	return rr
}

func TestHandleOverview_EstateWideRBAC_SuperuserBypass(t *testing.T) {
	// When RBAC is configured but the caller is a superuser, the handler
	// must serve the estate-wide overview without RBAC filtering.
	store, cleanup := newRBACTestStore(t)
	defer cleanup()

	g := &Generator{
		scopedCache: make(map[string]*scopedEntry),
	}
	g.current = newTestOverview("Full estate overview.")

	rbac := auth.NewRBACChecker(store)
	// A zero-value Datastore is non-nil, which is enough to enter the
	// RBAC path. The superuser context causes VisibleConnectionIDs to
	// return (nil, true, nil) before the lister is called, so the nil
	// pool inside the Datastore is never touched.
	ds := &database.Datastore{}
	h := NewHandlerWithRBAC(g, NewHub(), rbac, ds)

	ctx := context.WithValue(context.Background(), auth.IsSuperuserContextKey, true)

	rr := doRequestWithContext(t, h, ctx, http.MethodGet, "/api/v1/overview")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var resp Overview
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Summary != "Full estate overview." {
		t.Errorf("expected full estate summary, got %q", resp.Summary)
	}
}

func TestHandleOverview_EstateWideRBAC_RestrictedCaller(t *testing.T) {
	// When RBAC is configured and the caller is a restricted non-superuser,
	// the handler must NOT serve the estate-wide overview. Instead it must
	// attempt to resolve the caller's visible connections (which involves
	// calling the datastore via the visibility lister). With a zero-value
	// Datastore the lister panics on the nil pool, proving that the RBAC
	// filtering path was entered.
	store, cleanup := newRBACTestStore(t)
	defer cleanup()

	if err := store.CreateUser("restricted", "Password1", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, err := store.GetUserID("restricted")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}

	g := &Generator{
		scopedCache: make(map[string]*scopedEntry),
	}
	g.current = newTestOverview("Full estate overview - should NOT be served.")

	rbac := auth.NewRBACChecker(store)
	ds := &database.Datastore{}
	h := NewHandlerWithRBAC(g, NewHub(), rbac, ds)

	ctx := context.WithValue(context.Background(), auth.IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, auth.UserIDContextKey, userID)
	ctx = context.WithValue(ctx, auth.UsernameContextKey, "restricted")

	panicked := invokePanics(func() {
		doRequestWithContext(t, h, ctx, http.MethodGet, "/api/v1/overview")
	})
	if !panicked {
		t.Error("expected resolveVisible to panic on nil datastore pool, " +
			"proving the handler entered the RBAC filtering path for restricted callers")
	}
}
