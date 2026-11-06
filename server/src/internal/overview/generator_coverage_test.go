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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Generator and handler paths that need no database: the background
// loop, scoped generation failures, scope-name lookups, route
// registration and the visibility lister's error path.

// TestGeneratorStartStop runs the background loop against an
// unreachable datastore with no LLM configured, so the first refresh
// fails and is broadcast to estate subscribers, then stops it.
func TestGeneratorStartStop(t *testing.T) {
	g := NewGenerator(unreachableDatastore(t), nil)
	hub := NewHub()
	g.SetHub(hub)
	sub := hub.Subscribe("")
	defer hub.Unsubscribe(sub)

	g.Start(context.Background())
	defer g.Stop()

	select {
	case ov := <-sub.C:
		if ov.Status != statusError {
			t.Errorf("expected an error notice, got %+v", ov)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no broadcast from the first refresh")
	}
	g.Stop()
	if g.ctx.Err() == nil {
		t.Error("expected Stop to cancel the generator context")
	}
}

func TestGeneratorStopBeforeStart(t *testing.T) {
	NewGenerator(nil, nil).Stop()
}

// TestGetScopedSummary_Failures checks that a scoped summary fails for
// an unknown scope type and when the datastore cannot be read.
func TestGetScopedSummary_Failures(t *testing.T) {
	g := NewGenerator(unreachableDatastore(t), nil)
	g.ctx = context.Background()
	for _, scopeType := range []string{"server", "cluster", "group", "bogus"} {
		t.Run(scopeType, func(t *testing.T) {
			ov, err := g.GetScopedSummary(scopeType, 1, false)
			if err == nil {
				t.Fatalf("expected an error, got %+v", ov)
			}
		})
	}
}

// TestGetConnectionsSummary_NoLLM checks that a connections summary
// fails, and caches nothing, when no LLM provider is configured.
func TestGetConnectionsSummary_NoLLM(t *testing.T) {
	g := NewGenerator(unreachableDatastore(t), nil)
	g.ctx = context.Background()
	if _, err := g.GetConnectionsSummary([]int{1}, "", false); err == nil {
		t.Fatal("expected an error without an LLM provider")
	}
	if len(g.scopedCache) != 0 {
		t.Errorf("expected nothing cached, got %d entries", len(g.scopedCache))
	}
}

func TestResolveScopeName_Failures(t *testing.T) {
	h := NewHandlerWithRBAC(cachedGenerator(t, nil, false), NewHub(), nil, unreachableDatastore(t))
	for _, scopeType := range []string{"server", "cluster", "group", "bogus"} {
		if got := h.resolveScopeName(context.Background(), scopeType, 1); got != "" {
			t.Errorf("resolveScopeName(%q) = %q, want empty", scopeType, got)
		}
	}
	h.datastore = nil
	if got := h.resolveScopeName(context.Background(), "server", 1); got != "" {
		t.Errorf("resolveScopeName with no datastore = %q, want empty", got)
	}
}

func TestRegisterRoutes(t *testing.T) {
	h := newTestHandlerFor(t, cachedGenerator(t, nil, false), NewHub())
	mux := http.NewServeMux()
	var wrapped []string
	h.RegisterRoutes(mux, func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			wrapped = append(wrapped, r.URL.Path)
			next(w, r)
		}
	})
	for _, path := range []string{"/api/v1/overview", "/api/v1/overview/stream"} {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, path, nil))
		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: expected 405, got %d", path, rr.Code)
		}
	}
	if len(wrapped) != 2 {
		t.Errorf("expected both routes to pass through the auth wrapper, got %v", wrapped)
	}
}

func TestOverviewVisibilityLister(t *testing.T) {
	if newOverviewVisibilityLister(nil) != nil {
		t.Error("expected a nil lister for a nil datastore")
	}
	lister := newOverviewVisibilityLister(unreachableDatastore(t))
	if _, err := lister.GetAllConnections(context.Background()); err == nil {
		t.Error("expected an error from an unreachable datastore")
	}
}
