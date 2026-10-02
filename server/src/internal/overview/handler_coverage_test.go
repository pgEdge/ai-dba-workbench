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
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// Branch coverage for the overview handler's RBAC paths: a restricted
// caller (a user granted connection 7 only), visibility and generation
// failures, and response encoding failures.

// failingWriter is a ResponseWriter whose body writes always fail. It
// does not implement http.Flusher.
type failingWriter struct {
	header http.Header
	code   int
}

func newFailingWriter() *failingWriter {
	return &failingWriter{header: make(http.Header)}
}

func (f *failingWriter) Header() http.Header { return f.header }

func (f *failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func (f *failingWriter) WriteHeader(code int) { f.code = code }

// cachedGenerator returns a generator with the given scoped cache
// entries and estate overview. When failing is true its datastore is
// unreachable and it has no LLM configuration, so any summary that is
// not cached fails.
func cachedGenerator(t *testing.T, current *Overview, failing bool, keys ...string) *Generator {
	t.Helper()
	g := &Generator{
		scopedCache: make(map[string]*scopedEntry),
		ctx:         context.Background(),
	}
	if failing {
		g.datastore = unreachableDatastore(t)
	}
	g.current = current
	for _, key := range keys {
		g.scopedCache[key] = &scopedEntry{
			overview:   newTestOverview("Cached " + key),
			lastAccess: time.Now().UTC(),
		}
	}
	return g
}

// grantedHandler builds a handler with a real checker and returns it
// with its hub and the context of a user granted connection 7 only.
func grantedHandler(t *testing.T, g *Generator, ds *database.Datastore) (*Handler, *Hub, context.Context) {
	t.Helper()
	store, cleanup := newRBACTestStore(t)
	t.Cleanup(cleanup)
	ctx := grantedUserContext(t, store, 7)
	hub := NewHub()
	return NewHandlerWithRBAC(g, hub, auth.NewRBACChecker(store), ds), hub, ctx
}

// decodeSummary decodes rr as an Overview and returns its summary.
func decodeSummary(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp Overview
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return resp.Summary
}

func TestHandleOverview_Restricted_Paths(t *testing.T) {
	tests := []struct {
		name        string
		target      string
		cached      []string
		failingGen  bool
		failingDS   bool
		wantCode    int
		wantSummary string
	}{
		{
			name:        "connection_ids filtered and deduplicated",
			target:      "/api/v1/overview?connection_ids=8,7,7",
			cached:      []string{"connections:7"},
			wantCode:    http.StatusOK,
			wantSummary: "Cached connections:7",
		},
		{
			name:      "connection_ids visibility failure",
			target:    "/api/v1/overview?connection_ids=7",
			failingDS: true,
			wantCode:  http.StatusInternalServerError,
		},
		{
			name:       "connection_ids summary failure",
			target:     "/api/v1/overview?connection_ids=7",
			failingGen: true,
			wantCode:   http.StatusInternalServerError,
		},
		{
			name:        "visible server scope",
			target:      "/api/v1/overview?scope_type=server&scope_id=7",
			cached:      []string{"connections:7"},
			wantCode:    http.StatusOK,
			wantSummary: "Cached connections:7",
		},
		{
			name:       "visible server scope summary failure",
			target:     "/api/v1/overview?scope_type=server&scope_id=7",
			failingGen: true,
			wantCode:   http.StatusInternalServerError,
		},
		{
			name:     "invisible server scope",
			target:   "/api/v1/overview?scope_type=server&scope_id=8",
			wantCode: http.StatusNotFound,
		},
		{
			name:      "scope visibility failure",
			target:    "/api/v1/overview?scope_type=server&scope_id=7",
			failingDS: true,
			wantCode:  http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := cachedGenerator(t, newTestOverview("Full estate - must NOT be served."),
				tt.failingGen, tt.cached...)
			var ds *database.Datastore
			if tt.failingDS {
				ds = unreachableDatastore(t)
			}
			h, _, ctx := grantedHandler(t, g, ds)
			rr := doRequestWithContext(t, h, ctx, http.MethodGet, tt.target)
			if rr.Code != tt.wantCode {
				t.Fatalf("expected status %d, got %d: %s", tt.wantCode, rr.Code, rr.Body.String())
			}
			if tt.wantSummary != "" {
				if got := decodeSummary(t, rr); got != tt.wantSummary {
					t.Errorf("expected summary %q, got %q", tt.wantSummary, got)
				}
			}
		})
	}
}

func TestHandleOverview_Superuser_ConnectionIDsSummaryFailure(t *testing.T) {
	h := newTestHandlerFor(t, cachedGenerator(t, nil, true), NewHub())
	rr := doRequest(t, h, http.MethodGet, "/api/v1/overview?connection_ids=1")
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestHandleOverview_EncodeFailures drives every response-encoding error
// branch; each must write nothing further rather than panic.
func TestHandleOverview_EncodeFailures(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		current *Overview
		cached  []string
		ctx     string // "superuser", "granted" or "nil" (nil checker)
	}{
		{"empty connection_ids result", "/api/v1/overview?connection_ids=1", nil, nil, "nil"},
		{"connection_ids summary", "/api/v1/overview?connection_ids=1", nil, []string{"connections:1"}, "superuser"},
		{"empty estate-wide result", "/api/v1/overview", nil, nil, "nil"},
		{"restricted estate-wide summary", "/api/v1/overview", nil, []string{"connections:7"}, "granted"},
		{"scoped summary", "/api/v1/overview?scope_type=server&scope_id=1", nil, []string{"server:1"}, "superuser"},
		{"estate generating", "/api/v1/overview", nil, nil, "superuser"},
		{"estate overview", "/api/v1/overview", newTestOverview("Estate."), nil, "superuser"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := cachedGenerator(t, tt.current, false, tt.cached...)
			var h *Handler
			var ctx context.Context
			switch tt.ctx {
			case "granted":
				h, _, ctx = grantedHandler(t, g, nil)
			case "nil":
				h = NewHandlerWithRBAC(g, NewHub(), nil, nil)
				ctx = superuserContext()
			default:
				h = newTestHandlerFor(t, g, NewHub())
				ctx = superuserContext()
			}
			w := newFailingWriter()
			req := httptest.NewRequest(http.MethodGet, tt.target, nil).WithContext(ctx)
			h.handleOverview(w, req)
			if w.code != 0 && w.code != http.StatusOK {
				t.Errorf("expected no error status, got %d", w.code)
			}
		})
	}
}

func TestScopeVisible_UnknownScopeType(t *testing.T) {
	h, _, ctx := grantedHandler(t, cachedGenerator(t, nil, false), nil)
	rr := httptest.NewRecorder()
	ids, unrestricted, ok := h.scopeVisible(ctx, rr, "bogus", 1)
	if ok || unrestricted || ids != nil {
		t.Errorf("expected denial, got ids=%v unrestricted=%v ok=%v", ids, unrestricted, ok)
	}
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rr.Code)
	}
}

// serveSSE runs handleSSE until it returns or the timeout elapses, and
// returns the recorder.
func serveSSE(t *testing.T, h *Handler, ctx context.Context, target string, timeout time.Duration) *httptest.ResponseRecorder {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	rr := httptest.NewRecorder()
	h.handleSSE(rr, httptest.NewRequest(http.MethodGet, target, nil).WithContext(ctx))
	return rr
}

func TestHandleSSE_StreamingNotSupported(t *testing.T) {
	h := newTestHandlerFor(t, cachedGenerator(t, nil, false), NewHub())
	w := newFailingWriter()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview/stream", nil).
		WithContext(superuserContext())
	h.handleSSE(w, req)
	if w.code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.code)
	}
}

func TestHandleSSE_RequestErrors(t *testing.T) {
	tests := []struct {
		name      string
		target    string
		failingDS bool
		wantCode  int
	}{
		{"bad connection_ids", "/api/v1/overview/stream?connection_ids=abc", false, http.StatusBadRequest},
		{"connection_ids visibility failure", "/api/v1/overview/stream?connection_ids=7", true, http.StatusInternalServerError},
		{"scope_type only", "/api/v1/overview/stream?scope_type=server", false, http.StatusBadRequest},
		{"scope_id only", "/api/v1/overview/stream?scope_id=1", false, http.StatusBadRequest},
		{"bad scope_type", "/api/v1/overview/stream?scope_type=bogus&scope_id=1", false, http.StatusBadRequest},
		{"bad scope_id", "/api/v1/overview/stream?scope_type=server&scope_id=0", false, http.StatusBadRequest},
		{"invisible scope", "/api/v1/overview/stream?scope_type=server&scope_id=8", false, http.StatusNotFound},
		{"estate-wide visibility failure", "/api/v1/overview/stream", true, http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ds *database.Datastore
			if tt.failingDS {
				ds = unreachableDatastore(t)
			}
			h, hub, ctx := grantedHandler(t, cachedGenerator(t, nil, false), ds)
			rr := serveSSE(t, h, ctx, tt.target, 2*time.Second)
			if rr.Code != tt.wantCode {
				t.Errorf("expected status %d, got %d: %s", tt.wantCode, rr.Code, rr.Body.String())
			}
			if hub.Count() != 0 {
				t.Errorf("expected no subscribers, got %d", hub.Count())
			}
		})
	}
}

// TestHandleSSE_GenerationPaths opens streams that start background
// generation, both cached (success) and uncached on a failing generator
// (the goroutine logs the error). The stream runs until its context
// times out, which leaves the generation goroutine time to finish.
func TestHandleSSE_GenerationPaths(t *testing.T) {
	tests := []struct {
		name       string
		target     string
		superuser  bool
		cached     []string
		failingGen bool
		wantKey    string
	}{
		{
			name:    "restricted connection_ids",
			target:  "/api/v1/overview/stream?connection_ids=7,8,7&scope_name=mine",
			cached:  []string{"connections:7"},
			wantKey: "connections:7",
		},
		{
			name:       "restricted connection_ids generation failure",
			target:     "/api/v1/overview/stream?connection_ids=7",
			failingGen: true,
			wantKey:    "connections:7",
		},
		{
			name:    "restricted server scope",
			target:  "/api/v1/overview/stream?scope_type=server&scope_id=7",
			cached:  []string{"connections:7"},
			wantKey: "connections:7",
		},
		{
			name:    "restricted estate-wide",
			target:  "/api/v1/overview/stream",
			cached:  []string{"connections:7"},
			wantKey: "connections:7",
		},
		{
			name:      "unrestricted server scope",
			target:    "/api/v1/overview/stream?scope_type=server&scope_id=1",
			superuser: true,
			cached:    []string{"server:1"},
			wantKey:   "server:1",
		},
		{
			name:       "unrestricted server scope generation failure",
			target:     "/api/v1/overview/stream?scope_type=server&scope_id=1",
			superuser:  true,
			failingGen: true,
			wantKey:    "server:1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := cachedGenerator(t, nil, tt.failingGen, tt.cached...)
			var h *Handler
			var hub *Hub
			var ctx context.Context
			if tt.superuser {
				hub = NewHub()
				h = newTestHandlerFor(t, g, hub)
				ctx = superuserContext()
			} else {
				h, hub, ctx = grantedHandler(t, g, nil)
			}

			keyCh := make(chan string, 1)
			go func() {
				for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
					hub.mu.RLock()
					for sub := range hub.subscribers {
						keyCh <- sub.scopeKey
						hub.mu.RUnlock()
						return
					}
					hub.mu.RUnlock()
				}
				keyCh <- "<none>"
			}()

			rr := serveSSE(t, h, ctx, tt.target, 300*time.Millisecond)
			if got := rr.Header().Get("Content-Type"); got != "text/event-stream" {
				t.Errorf("expected text/event-stream, got %q", got)
			}
			if key := <-keyCh; key != tt.wantKey {
				t.Errorf("expected scope key %q, got %q", tt.wantKey, key)
			}
		})
	}
}

// TestHandleSSE_SubscriptionClosed checks that the stream ends when the
// hub closes the subscriber's channel.
func TestHandleSSE_SubscriptionClosed(t *testing.T) {
	hub := NewHub()
	h := newTestHandlerFor(t, cachedGenerator(t, nil, false), hub)

	go func() {
		for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
			hub.mu.RLock()
			var sub *Subscriber
			for s := range hub.subscribers {
				sub = s
			}
			hub.mu.RUnlock()
			if sub != nil {
				hub.Unsubscribe(sub)
				return
			}
		}
	}()

	start := time.Now()
	serveSSE(t, h, superuserContext(), "/api/v1/overview/stream", 5*time.Second)
	if elapsed := time.Since(start); elapsed >= 5*time.Second {
		t.Errorf("stream did not end when the subscription closed")
	}
}
