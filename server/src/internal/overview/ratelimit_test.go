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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// superuserNamed returns a superuser request context for username, so
// that each test user draws on their own generation allowance.
func superuserNamed(username string) context.Context {
	return context.WithValue(superuserContext(), auth.UsernameContextKey, username)
}

// limitedHandler returns a superuser-admitting handler on g with the
// given limits applied.
func limitedHandler(t *testing.T, g *Generator, limits RateLimits) *Handler {
	t.Helper()
	h := newTestHandlerFor(t, g, NewHub())
	h.SetRateLimits(limits)
	return h
}

// staleEntry caches a summary under key whose stale time has passed.
func staleEntry(g *Generator, key, summary string) {
	ov := newTestOverview(summary)
	ov.StaleAt = time.Now().UTC().Add(-time.Minute)
	g.scopedCache[key] = &scopedEntry{overview: ov, lastAccess: time.Now().UTC()}
}

// assertRateLimited checks that rr is a 429 with a positive Retry-After.
func assertRateLimited(t *testing.T, rr *httptest.ResponseRecorder) {
	t.Helper()
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", rr.Code, rr.Body.String())
	}
	secs, err := strconv.Atoi(rr.Header().Get("Retry-After"))
	if err != nil || secs < 1 {
		t.Fatalf("expected a positive Retry-After, got %q", rr.Header().Get("Retry-After"))
	}
}

// In these tests the generator's datastore is unreachable, so every
// generation the limiter admits fails with a 500; a 500 therefore
// shows that a generation was attempted, and a 429 that it was not.

func TestRateLimit_GenerationsLimitedPerUser(t *testing.T) {
	g := cachedGenerator(t, nil, true)
	h := limitedHandler(t, g, RateLimits{GenerationsPerHour: 1, Burst: 2})
	target := "/api/v1/overview?scope_type=server&scope_id=1"

	for i := 0; i < 2; i++ {
		rr := doRequestWithContext(t, h, superuserNamed("alice"), http.MethodGet, target)
		if rr.Code != http.StatusInternalServerError {
			t.Fatalf("request %d within the burst: expected an attempted generation (500), got %d", i, rr.Code)
		}
	}
	assertRateLimited(t, doRequestWithContext(t, h, superuserNamed("alice"), http.MethodGet, target))

	// Another user has their own allowance.
	rr := doRequestWithContext(t, h, superuserNamed("bob"), http.MethodGet, target)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("bob was limited by alice's usage: got %d", rr.Code)
	}
}

func TestRateLimit_FreshCacheIsFree(t *testing.T) {
	g := cachedGenerator(t, nil, true, "server:1")
	h := limitedHandler(t, g, RateLimits{GenerationsPerHour: 1, Burst: 1})

	for i := 0; i < 5; i++ {
		rr := doRequestWithContext(t, h, superuserNamed("alice"),
			http.MethodGet, "/api/v1/overview?scope_type=server&scope_id=1")
		if got := decodeSummary(t, rr); got != "Cached server:1" {
			t.Fatalf("request %d: expected the cached summary, got %q", i, got)
		}
	}

	// The cache hits spent nothing, so a generation is still allowed.
	rr := doRequestWithContext(t, h, superuserNamed("alice"),
		http.MethodGet, "/api/v1/overview?scope_type=server&scope_id=2")
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected an attempted generation (500), got %d", rr.Code)
	}
}

func TestRateLimit_StaleCacheServedWhenLimited(t *testing.T) {
	g := cachedGenerator(t, nil, true)
	staleEntry(g, "server:1", "Stale server 1")
	h := limitedHandler(t, g, RateLimits{GenerationsPerHour: 1, Burst: 1})
	target := "/api/v1/overview?scope_type=server&scope_id=1"

	rr := doRequestWithContext(t, h, superuserNamed("alice"), http.MethodGet, target)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected the stale entry to trigger a generation (500), got %d", rr.Code)
	}
	rr = doRequestWithContext(t, h, superuserNamed("alice"), http.MethodGet, target)
	if got := decodeSummary(t, rr); got != "Stale server 1" {
		t.Fatalf("expected the stale summary once limited, got %q", got)
	}
}

func TestRateLimit_RefreshThrottled(t *testing.T) {
	g := cachedGenerator(t, nil, true, "server:1")
	h := limitedHandler(t, g, RateLimits{GenerationsPerHour: 100, Burst: 100, RefreshInterval: time.Hour})
	target := "/api/v1/overview?scope_type=server&scope_id=1&refresh=true"

	rr := doRequestWithContext(t, h, superuserNamed("alice"), http.MethodGet, target)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("first refresh: expected a forced generation (500), got %d", rr.Code)
	}
	rr = doRequestWithContext(t, h, superuserNamed("alice"), http.MethodGet, target)
	if got := decodeSummary(t, rr); got != "Cached server:1" {
		t.Fatalf("throttled refresh: expected the cached summary, got %q", got)
	}
}

func TestRateLimit_RefreshServesCacheWhenGenerationsUsedUp(t *testing.T) {
	g := cachedGenerator(t, nil, true, "server:1")
	h := limitedHandler(t, g, RateLimits{GenerationsPerHour: 1, Burst: 1})
	target := "/api/v1/overview?scope_type=server&scope_id=1&refresh=true"

	rr := doRequestWithContext(t, h, superuserNamed("alice"), http.MethodGet, target)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("first refresh: expected a forced generation (500), got %d", rr.Code)
	}
	rr = doRequestWithContext(t, h, superuserNamed("alice"), http.MethodGet, target)
	if got := decodeSummary(t, rr); got != "Cached server:1" {
		t.Fatalf("expected the cached summary once limited, got %q", got)
	}
}

func TestRateLimit_ConnectionIDs(t *testing.T) {
	g := cachedGenerator(t, nil, true)
	h := limitedHandler(t, g, RateLimits{GenerationsPerHour: 1, Burst: 1})
	target := "/api/v1/overview?connection_ids=2,1"

	rr := doRequestWithContext(t, h, superuserNamed("alice"), http.MethodGet, target)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected an attempted generation (500), got %d", rr.Code)
	}
	assertRateLimited(t, doRequestWithContext(t, h, superuserNamed("alice"), http.MethodGet, target))
}

func TestRateLimit_RestrictedPaths(t *testing.T) {
	g := cachedGenerator(t, nil, true)
	h, _, ctx := grantedHandler(t, g, nil)
	h.SetRateLimits(RateLimits{GenerationsPerHour: 1, Burst: 1})

	rr := doRequestWithContext(t, h, ctx, http.MethodGet, "/api/v1/overview")
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected an attempted generation (500), got %d", rr.Code)
	}
	assertRateLimited(t, doRequestWithContext(t, h, ctx, http.MethodGet, "/api/v1/overview"))
	assertRateLimited(t, doRequestWithContext(t, h, ctx, http.MethodGet,
		"/api/v1/overview?scope_type=server&scope_id=7"))
}

func TestRateLimit_EstateRefresh(t *testing.T) {
	tests := []struct {
		name   string
		limits RateLimits
	}{
		{"refresh throttled", RateLimits{GenerationsPerHour: 100, Burst: 100, RefreshInterval: time.Hour}},
		{"generations used up", RateLimits{GenerationsPerHour: 1, Burst: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := limitedHandler(t, &Generator{scopedCache: make(map[string]*scopedEntry)}, tt.limits)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/overview?refresh=true", nil).
				WithContext(superuserNamed("alice"))
			if !h.allowEstateRefresh(req) {
				t.Fatal("first estate refresh was refused")
			}
			if h.allowEstateRefresh(req) {
				t.Fatal("second estate refresh was allowed")
			}
		})
	}

	// Through the handler, a refused refresh still serves the current
	// estate overview, and never a 429.
	g := cachedGenerator(t, newTestOverview("Estate overview"), true)
	h := limitedHandler(t, g, RateLimits{GenerationsPerHour: 100, Burst: 100, RefreshInterval: time.Hour})
	for i := 0; i < 2; i++ {
		rr := doRequestWithContext(t, h, superuserNamed("alice"), http.MethodGet, "/api/v1/overview?refresh=true")
		if got := decodeSummary(t, rr); got != "Estate overview" {
			t.Fatalf("request %d: expected the estate overview, got %q", i, got)
		}
	}
}

func TestRateLimit_Unlimited(t *testing.T) {
	g := cachedGenerator(t, nil, true)
	h := limitedHandler(t, g, RateLimits{})
	for i := 0; i < 10; i++ {
		rr := doRequestWithContext(t, h, superuserNamed("alice"),
			http.MethodGet, "/api/v1/overview?scope_type=server&scope_id=1&refresh=true")
		if rr.Code != http.StatusInternalServerError {
			t.Fatalf("request %d: disabled limits refused a generation: got %d", i, rr.Code)
		}
	}
}

func TestRateLimit_SSE(t *testing.T) {
	g := cachedGenerator(t, newTestOverview("Estate overview"), true)
	staleEntry(g, "server:1", "Stale server 1")
	h := limitedHandler(t, g, RateLimits{GenerationsPerHour: 1, Burst: 1})
	ctx := superuserNamed("alice")

	// The first stream spends the only generation.
	rr := serveSSE(t, h, ctx, "/api/v1/overview/stream?scope_type=server&scope_id=2", 100*time.Millisecond)
	if got := rr.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("first stream: expected text/event-stream, got %q", got)
	}

	// A limited stream with a cached summary gets the stale summary.
	rr = serveSSE(t, h, ctx, "/api/v1/overview/stream?scope_type=server&scope_id=1", 100*time.Millisecond)
	if got := rr.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("stale stream: expected text/event-stream, got %q", got)
	}
	if !strings.Contains(rr.Body.String(), "Stale server 1") {
		t.Fatalf("stale stream: expected the stale summary, got %q", rr.Body.String())
	}

	// A limited stream with nothing cached is refused before it starts.
	rr = serveSSE(t, h, ctx, "/api/v1/overview/stream?scope_type=server&scope_id=3", 100*time.Millisecond)
	assertRateLimited(t, rr)

	// The estate stream generates nothing, so it is never limited.
	rr = serveSSE(t, h, ctx, "/api/v1/overview/stream", 100*time.Millisecond)
	if !strings.Contains(rr.Body.String(), "Estate overview") {
		t.Fatalf("estate stream: expected the estate overview, got %q", rr.Body.String())
	}
}

func TestRateLimit_SSEEmptyVisibleSetNotLimited(t *testing.T) {
	g := cachedGenerator(t, nil, true)
	h, _, ctx := grantedHandler(t, g, nil)
	h.SetRateLimits(RateLimits{GenerationsPerHour: 1, Burst: 1})
	h.limits.generate.Allow("granted")

	// Connection 8 is not visible to the user, so nothing is generated
	// and the stream is admitted even with no generations left.
	rr := serveSSE(t, h, ctx, "/api/v1/overview/stream?connection_ids=8", 100*time.Millisecond)
	if got := rr.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("expected text/event-stream, got %q (code %d)", got, rr.Code)
	}
}

func TestCacheKeys(t *testing.T) {
	if got := connectionsCacheKey([]int{3, 1, 2}); got != "connections:1,2,3" {
		t.Errorf("connectionsCacheKey = %q", got)
	}
	if got := connectionsCacheKey(nil); got != "connections:" {
		t.Errorf("connectionsCacheKey(nil) = %q", got)
	}
	if got := scopedCacheKey("cluster", 4); got != "cluster:4" {
		t.Errorf("scopedCacheKey = %q", got)
	}

	ids := []int{3, 1, 2}
	connectionsCacheKey(ids)
	if ids[0] != 3 {
		t.Error("connectionsCacheKey modified its input")
	}
}

// TestCacheKeysMatchGenerator checks that the limiter looks up the same
// cache keys the generator serves from: with no generations left, a
// request is only answered if the limiter found the fresh entry the
// generator then returns, so a key mismatch shows up as a 429.
func TestCacheKeysMatchGenerator(t *testing.T) {
	tests := []struct {
		name   string
		key    string
		target string
	}{
		{"connections", "connections:1,2", "/api/v1/overview?connection_ids=2,1,2"},
		{"server", "server:5", "/api/v1/overview?scope_type=server&scope_id=5"},
		{"cluster", "cluster:6", "/api/v1/overview?scope_type=cluster&scope_id=6"},
		{"group", "group:7", "/api/v1/overview?scope_type=group&scope_id=7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := cachedGenerator(t, nil, true, tt.key)
			h := limitedHandler(t, g, RateLimits{GenerationsPerHour: 1, Burst: 1})
			h.limits.generate.Allow("alice")

			rr := doRequestWithContext(t, h, superuserNamed("alice"), http.MethodGet, tt.target)
			if got := decodeSummary(t, rr); got != "Cached "+tt.key {
				t.Fatalf("expected the cached summary, got %q", got)
			}
		})
	}
}

func TestCachedSummary(t *testing.T) {
	g := cachedGenerator(t, nil, false, "server:1")
	staleEntry(g, "server:2", "Stale")

	if ov, fresh := g.cachedSummary("server:1"); ov == nil || !fresh {
		t.Errorf("server:1: got (%v, %v), want a fresh entry", ov, fresh)
	}
	if ov, fresh := g.cachedSummary("server:2"); ov == nil || fresh {
		t.Errorf("server:2: got (%v, %v), want a stale entry", ov, fresh)
	}
	if ov, fresh := g.cachedSummary("server:3"); ov != nil || fresh {
		t.Errorf("server:3: got (%v, %v), want nothing", ov, fresh)
	}
}

func TestWriteRateLimitedMinimumWait(t *testing.T) {
	rr := httptest.NewRecorder()
	writeRateLimited(rr, 200*time.Millisecond)
	if got := rr.Header().Get("Retry-After"); got != "1" {
		t.Errorf("Retry-After = %q, want 1", got)
	}
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("code = %d, want 429", rr.Code)
	}
}
