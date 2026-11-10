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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/llmproxy"
)

// Status notices that stop the AI Overview card waiting on a broadcast
// that never comes (issue #808).

// sseDataLines returns the data payloads of the events in an SSE body.
func sseDataLines(body string) []string {
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "data: ") {
			lines = append(lines, strings.TrimPrefix(line, "data: "))
		}
	}
	return lines
}

// decodeStatus decodes a payload as a generatingResponse.
func decodeStatus(t *testing.T, data string) generatingResponse {
	t.Helper()
	var resp generatingResponse
	if err := json.Unmarshal([]byte(data), &resp); err != nil {
		t.Fatalf("failed to decode status %q: %v", data, err)
	}
	return resp
}

// failedGenerator returns a generator whose first estate refresh failed.
func failedGenerator(t *testing.T) *Generator {
	t.Helper()
	g := cachedGenerator(t, nil, false)
	g.estateFailed = true
	return g
}

func TestHandleSSE_EstateInitialEvent(t *testing.T) {
	tests := []struct {
		name       string
		g          *Generator
		wantStatus string
		wantError  string
		wantID     bool
	}{
		{"no summary yet", cachedGenerator(t, nil, false), statusGenerating, "", false},
		{"first refresh failed", failedGenerator(t), statusError, generationFailedMessage, false},
		{"cached summary", cachedGenerator(t, newTestOverview("Estate."), false), "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandlerFor(t, tt.g, NewHub())
			body := serveSSE(t, h, superuserContext(), "/api/v1/overview/stream", 200*time.Millisecond).Body.String()

			data := sseDataLines(body)
			if len(data) != 1 {
				t.Fatalf("expected one event, got %d:\n%s", len(data), body)
			}
			if hasID := strings.Contains(body, "id: "); hasID != tt.wantID {
				t.Errorf("event id present = %v, want %v:\n%s", hasID, tt.wantID, body)
			}
			if tt.wantStatus == "" {
				var ov Overview
				if err := json.Unmarshal([]byte(data[0]), &ov); err != nil {
					t.Fatalf("failed to decode overview: %v", err)
				}
				if ov.Summary != "Estate." {
					t.Errorf("expected cached summary, got %q", ov.Summary)
				}
				return
			}
			got := decodeStatus(t, data[0])
			if got.Status != tt.wantStatus || got.Error != tt.wantError || got.Summary != nil {
				t.Errorf("status = %+v, want status %q error %q", got, tt.wantStatus, tt.wantError)
			}
		})
	}
}

func TestHandleSSE_NoVisibleConnectionsSendsEmpty(t *testing.T) {
	h, _ := newNilRBACHandler(nil)
	body := serveSSE(t, h, superuserContext(), "/api/v1/overview/stream", 200*time.Millisecond).Body.String()
	data := sseDataLines(body)
	if len(data) != 1 {
		t.Fatalf("expected one event, got %d:\n%s", len(data), body)
	}
	if got := decodeStatus(t, data[0]); got.Status != statusEmpty {
		t.Errorf("expected status %q, got %+v", statusEmpty, got)
	}
	if strings.Contains(body, "must NOT be served") {
		t.Errorf("estate overview sent to a caller who can see nothing:\n%s", body)
	}
}

// TestHandleSSE_DropsDuplicateDelivery checks that an overview the
// stream has just sent is not sent again when the hub broadcasts it.
func TestHandleSSE_DropsDuplicateDelivery(t *testing.T) {
	current := newTestOverview("Estate.")
	hub := NewHub()
	h := newTestHandlerFor(t, cachedGenerator(t, current, false), hub)

	go func() {
		for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
			if hub.Count() > 0 {
				// Give the handler time to send the initial event; the
				// subscriber buffer holds one message at a time.
				time.Sleep(50 * time.Millisecond)
				hub.Broadcast(current, "")
				time.Sleep(50 * time.Millisecond)
				hub.Broadcast(newTestOverview("Newer."), "")
				return
			}
		}
	}()

	body := serveSSE(t, h, superuserContext(), "/api/v1/overview/stream", 500*time.Millisecond).Body.String()
	data := sseDataLines(body)
	if len(data) != 2 {
		t.Fatalf("expected the initial and the newer event, got %d:\n%s", len(data), body)
	}
	if !strings.Contains(data[1], "Newer.") {
		t.Errorf("expected the newer overview second, got %s", data[1])
	}
}

func TestHandleOverview_EstateStatusResponses(t *testing.T) {
	tests := []struct {
		name string
		g    *Generator
		want generatingResponse
	}{
		{"generating", cachedGenerator(t, nil, false), generatingResponse{Status: statusGenerating}},
		{"error", failedGenerator(t), generatingResponse{Status: statusError, Error: generationFailedMessage}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandlerFor(t, tt.g, NewHub())
			rr := doRequest(t, h, "GET", "/api/v1/overview")
			if got := decodeStatus(t, rr.Body.String()); got != tt.want {
				t.Errorf("response = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestRecordEstateFailure(t *testing.T) {
	t.Run("broadcasts while no summary exists", func(t *testing.T) {
		g := cachedGenerator(t, nil, false)
		hub := NewHub()
		g.SetHub(hub)
		sub := hub.Subscribe("")
		defer hub.Unsubscribe(sub)

		g.recordEstateFailure()

		select {
		case ov := <-sub.C:
			if ov.Status != statusError || ov.Error != generationFailedMessage {
				t.Errorf("unexpected notice %+v", ov)
			}
		default:
			t.Fatal("expected an error notice")
		}
		if got := g.EstateStatus(); got.Status != statusError {
			t.Errorf("EstateStatus = %+v, want an error notice", got)
		}
	})

	t.Run("keeps an existing summary", func(t *testing.T) {
		current := newTestOverview("Estate.")
		g := cachedGenerator(t, current, false)
		hub := NewHub()
		g.SetHub(hub)
		sub := hub.Subscribe("")
		defer hub.Unsubscribe(sub)

		g.recordEstateFailure()

		select {
		case ov := <-sub.C:
			t.Errorf("unexpected broadcast %+v", ov)
		default:
		}
		if got := g.EstateStatus(); got != current {
			t.Errorf("EstateStatus = %+v, want the cached summary", got)
		}
	})

	t.Run("no hub", func(t *testing.T) {
		g := cachedGenerator(t, nil, false)
		g.recordEstateFailure()
		if !g.estateFailed {
			t.Error("expected the failure to be recorded")
		}
	})
}

// TestRefresh_EstateFailureThenRecovery drives refresh against an
// unreachable datastore, whose snapshot is empty but valid, first with
// no LLM provider and then with a stub provider.
func TestRefresh_EstateFailureThenRecovery(t *testing.T) {
	g := NewGenerator(unreachableDatastore(t), &llmproxy.Config{})
	g.ctx = context.Background()
	hub := NewHub()
	g.SetHub(hub)
	sub := hub.Subscribe("")
	defer hub.Unsubscribe(sub)

	g.refresh(false)
	if got := g.EstateStatus(); got.Status != statusError {
		t.Fatalf("EstateStatus after failure = %+v, want an error notice", got)
	}
	if ov := <-sub.C; ov.Status != statusError {
		t.Errorf("expected an error broadcast, got %+v", ov)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices": [{"message": {"role": "assistant", "content": "Estate healthy."}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}
		}`))
	}))
	defer srv.Close()
	g.llmConfig = &llmproxy.Config{
		Provider:      "openai",
		Model:         "gpt-4o",
		OpenAIAPIKey:  "test-key",
		OpenAIBaseURL: srv.URL,
	}

	g.refresh(false)
	got := g.EstateStatus()
	if got.Status != "" || got.Summary != "Estate healthy." {
		t.Fatalf("EstateStatus after recovery = %+v, want the summary", got)
	}
	if ov := <-sub.C; ov != got {
		t.Errorf("expected the summary broadcast, got %+v", ov)
	}

	// An unchanged estate keeps the summary without regenerating.
	g.refresh(false)
	if g.EstateStatus() != got {
		t.Error("an unchanged refresh replaced the summary")
	}

	// A later failure leaves the summary in place and broadcasts nothing.
	g.llmConfig = &llmproxy.Config{}
	g.refresh(true)
	if g.EstateStatus() != got {
		t.Error("a failed refresh replaced the existing summary")
	}
	select {
	case ov := <-sub.C:
		t.Errorf("unexpected broadcast %+v", ov)
	default:
	}
}

func TestPublishEstateOverview_Restart(t *testing.T) {
	g := cachedGenerator(t, nil, false, "server:1")
	g.estateFailed = true
	hub := NewHub()
	g.SetHub(hub)
	sub := hub.Subscribe("")
	defer hub.Unsubscribe(sub)
	restarts := 0
	g.OnRestart(func() { restarts++ })

	now := time.Now().UTC()
	old := &database.EstateSnapshot{Timestamp: now.Add(-time.Minute)}
	snapshot := &database.EstateSnapshot{
		Timestamp:    now,
		RecentEvents: []database.EstateEventSummary{{EventType: "restart", OccurredAt: now}},
	}
	g.publishEstateOverview(old, snapshot, "Restarted.")

	got := g.EstateStatus()
	if got.Summary != "Restarted." || !got.RestartDetected {
		t.Errorf("EstateStatus = %+v, want the restart summary", got)
	}
	if g.estateFailed {
		t.Error("expected the failure flag to clear")
	}
	if len(g.scopedCache) != 0 || restarts != 1 {
		t.Errorf("scoped cache %d entries, %d restart callbacks; want 0 and 1", len(g.scopedCache), restarts)
	}
	if ov := <-sub.C; ov != got {
		t.Errorf("expected the summary broadcast, got %+v", ov)
	}
}

// TestHandleSSE_ScopedOpensWithGenerating checks that a scoped stream
// whose summary is not freshly cached opens with a generating notice
// before its result, and that a fresh cache hit is sent on its own.
func TestHandleSSE_ScopedOpensWithGenerating(t *testing.T) {
	stale := func(t *testing.T) *Generator {
		g := cachedGenerator(t, nil, true, "server:1")
		g.scopedCache["server:1"].overview.StaleAt = time.Now().UTC().Add(-time.Minute)
		return g
	}
	tests := []struct {
		name   string
		g      func(t *testing.T) *Generator
		target string
		want   []string
	}{
		{
			name:   "fresh server scope",
			g:      func(t *testing.T) *Generator { return cachedGenerator(t, nil, false, "server:1") },
			target: "/api/v1/overview/stream?scope_type=server&scope_id=1",
			want:   []string{`"summary":"Cached server:1"`},
		},
		{
			name:   "uncached server scope",
			g:      func(t *testing.T) *Generator { return cachedGenerator(t, nil, true) },
			target: "/api/v1/overview/stream?scope_type=server&scope_id=1",
			want:   []string{`"status":"generating"`, `"status":"error"`},
		},
		{
			name:   "stale server scope",
			g:      stale,
			target: "/api/v1/overview/stream?scope_type=server&scope_id=1",
			want:   []string{`"status":"generating"`, `"status":"error"`},
		},
		{
			name:   "fresh connection list",
			g:      func(t *testing.T) *Generator { return cachedGenerator(t, nil, false, "connections:7") },
			target: "/api/v1/overview/stream?connection_ids=7",
			want:   []string{`"summary":"Cached connections:7"`},
		},
		{
			name:   "uncached connection list",
			g:      func(t *testing.T) *Generator { return cachedGenerator(t, nil, true) },
			target: "/api/v1/overview/stream?connection_ids=7",
			want:   []string{`"status":"generating"`, `"status":"error"`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandlerFor(t, tt.g(t), NewHub())
			body := serveSSE(t, h, superuserContext(), tt.target, 300*time.Millisecond).Body.String()
			data := sseDataLines(body)
			if len(data) != len(tt.want) {
				t.Fatalf("expected %d events, got %d:\n%s", len(tt.want), len(data), body)
			}
			for i, want := range tt.want {
				if !strings.Contains(data[i], want) {
					t.Errorf("event %d: expected %s, got %s", i, want, data[i])
				}
			}
		})
	}
}
