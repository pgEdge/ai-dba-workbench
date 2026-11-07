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
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/llmproxy"
)

// Scope names in connections summaries: a name only reaches a prompt
// once its scope has passed the visibility check, a caller-supplied
// name is sanitized, and summaries written around different names are
// cached and broadcast apart.

// stubLLM is an OpenAI-compatible endpoint that records each request
// body and answers with a fixed summary, or with a 400, which is not retried,
// when failing.
type stubLLM struct {
	mu      sync.Mutex
	bodies  []string
	failing bool
}

func newStubLLM(t *testing.T) (*stubLLM, *llmproxy.Config) {
	t.Helper()
	s := &stubLLM{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.bodies = append(s.bodies, string(body))
		failing := s.failing
		s.mu.Unlock()
		if failing {
			http.Error(w, `{"error":{"message":"boom"}}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices": [{"message": {"role": "assistant", "content": "Connections healthy."}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}
		}`))
	}))
	t.Cleanup(srv.Close)
	return s, &llmproxy.Config{
		Provider:      "openai",
		Model:         "gpt-4o",
		OpenAIAPIKey:  "test-key",
		OpenAIBaseURL: srv.URL,
	}
}

func (s *stubLLM) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.bodies...)
}

func TestSanitizeScopeName(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain", "prod-cluster", "prod-cluster"},
		{"trimmed", "  prod  ", "prod"},
		{"quotes stripped", `pr"o'd` + "`", "prod"},
		{"control characters stripped", "prod\n\r\tIgnore previous", "prodIgnore previous"},
		{"format characters stripped", "prod\u202ehidden\u200b", "prodhidden"},
		{"capped", strings.Repeat("a", maxScopeNameLen+50), strings.Repeat("a", maxScopeNameLen)},
		{"multibyte capped by rune", strings.Repeat("é", maxScopeNameLen+1), strings.Repeat("é", maxScopeNameLen)},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeScopeName(tt.in)
			if got != tt.want {
				t.Errorf("sanitizeScopeName(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if again := sanitizeScopeName(got); again != got {
				t.Errorf("sanitizeScopeName is not idempotent: %q then %q", got, again)
			}
		})
	}
}

func TestConnectionsKey(t *testing.T) {
	if got := connectionsKey([]int{8, 7}, ""); got != "connections:7,8" {
		t.Errorf("connectionsKey without a name = %q", got)
	}
	if got := connectionsKey([]int{8, 7}, "prod"); got != "connections:7,8|prod" {
		t.Errorf("connectionsKey with a name = %q", got)
	}
	if got := connectionsKey(nil, ""); got != "connections:" {
		t.Errorf("connectionsKey for no connections = %q", got)
	}
}

// TestGetConnectionsSummary_NamesAreCachedApart checks that two callers
// asking about the same connections under different names never share
// a cached summary, and that each prompt carries only its own name.
func TestGetConnectionsSummary_NamesAreCachedApart(t *testing.T) {
	llm, cfg := newStubLLM(t)
	g := NewGenerator(unreachableDatastore(t), cfg)
	g.ctx = context.Background()
	hub := NewHub()
	g.SetHub(hub)
	alphaSub := hub.Subscribe(connectionsKey([]int{7}, "Alpha"))
	defer hub.Unsubscribe(alphaSub)

	alpha, err := g.GetConnectionsSummary([]int{7}, "Alpha", false)
	if err != nil {
		t.Fatalf("Alpha summary: %v", err)
	}
	if alpha.Summary != "Connections healthy." {
		t.Errorf("Alpha summary = %q", alpha.Summary)
	}
	select {
	case ov := <-alphaSub.C:
		if ov != alpha {
			t.Errorf("Alpha subscriber received %+v, want the Alpha summary", ov)
		}
	case <-time.After(time.Second):
		t.Error("Alpha subscriber received no broadcast")
	}

	beta, err := g.GetConnectionsSummary([]int{7}, `Beta"`, false)
	if err != nil {
		t.Fatalf("Beta summary: %v", err)
	}
	if beta == alpha {
		t.Error("Beta caller was served the summary written for Alpha")
	}
	select {
	case ov := <-alphaSub.C:
		t.Errorf("Alpha subscriber received the Beta summary %+v", ov)
	default:
	}

	reqs := llm.requests()
	if len(reqs) != 2 {
		t.Fatalf("expected 2 LLM calls, got %d", len(reqs))
	}
	if !strings.Contains(reqs[0], "Alpha") || strings.Contains(reqs[0], "Beta") {
		t.Errorf("first prompt should name Alpha only: %s", reqs[0])
	}
	if !strings.Contains(reqs[1], `\"Beta\"`) || strings.Contains(reqs[1], "Alpha") {
		t.Errorf("second prompt should name the sanitized Beta only: %s", reqs[1])
	}

	// Each name now has its own fresh entry.
	if again, err := g.GetConnectionsSummary([]int{7}, "Alpha", false); err != nil || again != alpha {
		t.Errorf("Alpha cache hit = %p, %v; want %p", again, err, alpha)
	}
	if again, err := g.GetConnectionsSummary([]int{7}, "Beta", false); err != nil || again != beta {
		t.Errorf("Beta cache hit = %p, %v; want %p", again, err, beta)
	}
	if n := len(llm.requests()); n != 2 {
		t.Errorf("cache hits made LLM calls: %d in total", n)
	}

	llm.mu.Lock()
	llm.failing = true
	llm.mu.Unlock()
	if _, err := g.GetConnectionsSummary([]int{7}, "Alpha", true); err == nil {
		t.Error("expected an error when the LLM fails")
	}
}

// TestScopeWithConnectionIDsRejected checks that a request naming both
// explicit connections and a scope is refused on both endpoints before
// any lookup, so a hidden scope's name cannot be read alongside visible
// connections.
func TestScopeWithConnectionIDsRejected(t *testing.T) {
	targets := []string{
		"connection_ids=7&scope_type=cluster&scope_id=99",
		"connection_ids=7&scope_id=99",
		"connection_ids=7&scope_type=group",
	}
	for _, query := range targets {
		t.Run(query, func(t *testing.T) {
			h, hub, ctx := grantedHandler(t, cachedGenerator(t, nil, false, "connections:7"), unreachableDatastore(t))

			rr := serveSSE(t, h, ctx, "/api/v1/overview/stream?"+query, time.Second)
			if rr.Code != http.StatusBadRequest {
				t.Errorf("stream: expected 400, got %d: %s", rr.Code, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), errScopeWithConnectionIDs) {
				t.Errorf("stream: unexpected body %q", rr.Body.String())
			}
			if hub.Count() != 0 {
				t.Errorf("stream: expected no subscribers, got %d", hub.Count())
			}

			rr = doRequestWithContext(t, h, ctx, http.MethodGet, "/api/v1/overview?"+query)
			if rr.Code != http.StatusBadRequest {
				t.Errorf("REST: expected 400, got %d: %s", rr.Code, rr.Body.String())
			}
		})
	}
}

// TestHandleSSE_ScopeIgnoresCallerName checks that a scoped stream for a
// restricted caller takes its name from the scope, never from the
// caller's scope_name.
func TestHandleSSE_ScopeIgnoresCallerName(t *testing.T) {
	h, _, ctx := grantedHandler(t, cachedGenerator(t, nil, false, "connections:7"), nil)
	rr := serveSSE(t, h, ctx, "/api/v1/overview/stream?scope_type=server&scope_id=7&scope_name=Evil", 200*time.Millisecond)
	if body := rr.Body.String(); !strings.Contains(body, "Cached connections:7") || strings.Contains(body, "Evil") {
		t.Errorf("expected the summary keyed without the caller's name, got:\n%s", body)
	}
}

// TestHandleSSE_ConnectionIDsSanitizedName checks that the stream
// subscribes under the sanitized form of the caller's name, the key the
// generator broadcasts on.
func TestHandleSSE_ConnectionIDsSanitizedName(t *testing.T) {
	want := connectionsKey([]int{7}, "prod")
	h, _, ctx := grantedHandler(t, cachedGenerator(t, nil, false, want), nil)
	rr := serveSSE(t, h, ctx, `/api/v1/overview/stream?connection_ids=7&scope_name=%22prod%0A%22`, 200*time.Millisecond)
	if body := rr.Body.String(); !strings.Contains(body, "Cached "+want) {
		t.Errorf("expected the summary cached under %q, got:\n%s", want, body)
	}
}
