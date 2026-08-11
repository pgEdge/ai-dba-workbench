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
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	pgllm "github.com/pgEdge/pgedge-go-llm-lib/llm"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/llmproxy"
)

// --- sortInts tests --------------------------------------------------------

func TestSortInts(t *testing.T) {
	tests := []struct {
		name  string
		input []int
		want  []int
	}{
		{
			name:  "already sorted",
			input: []int{1, 2, 3, 4, 5},
			want:  []int{1, 2, 3, 4, 5},
		},
		{
			name:  "reverse order",
			input: []int{5, 4, 3, 2, 1},
			want:  []int{1, 2, 3, 4, 5},
		},
		{
			name:  "single element",
			input: []int{42},
			want:  []int{42},
		},
		{
			name:  "empty slice",
			input: []int{},
			want:  []int{},
		},
		{
			name:  "duplicates",
			input: []int{3, 1, 3, 2, 1},
			want:  []int{1, 1, 2, 3, 3},
		},
		{
			name:  "two elements swapped",
			input: []int{9, 1},
			want:  []int{1, 9},
		},
		{
			name:  "negative values",
			input: []int{-3, 0, -1, 2},
			want:  []int{-3, -1, 0, 2},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sortInts(tc.input)
			if len(tc.input) != len(tc.want) {
				t.Fatalf("length mismatch: got %d, want %d", len(tc.input), len(tc.want))
			}
			for i := range tc.want {
				if tc.input[i] != tc.want[i] {
					t.Errorf("index %d: got %d, want %d", i, tc.input[i], tc.want[i])
				}
			}
		})
	}
}

// --- hasSignificantChange tests --------------------------------------------

func TestHasSignificantChange(t *testing.T) {
	g := &Generator{
		scopedCache: make(map[string]*scopedEntry),
	}

	base := func() *database.EstateSnapshot {
		return &database.EstateSnapshot{
			ServerTotal:       5,
			ServerOnline:      4,
			ServerOffline:     1,
			ServerWarning:     0,
			AlertTotal:        3,
			AlertCritical:     1,
			AlertWarning:      1,
			AlertInfo:         1,
			Servers:           []database.EstateServerSummary{},
			TopAlerts:         []database.EstateAlertSummary{},
			ActiveBlackouts:   []database.EstateBlackoutSummary{},
			UpcomingBlackouts: []database.EstateBlackoutSummary{},
			RecentEvents:      []database.EstateEventSummary{},
		}
	}

	t.Run("nil old snapshot is always significant", func(t *testing.T) {
		if !g.hasSignificantChange(nil, base()) {
			t.Error("expected true when old is nil")
		}
	})

	t.Run("identical snapshots are not significant", func(t *testing.T) {
		s := base()
		if g.hasSignificantChange(s, s) {
			t.Error("expected false when snapshots are identical")
		}
	})

	t.Run("server total changed", func(t *testing.T) {
		old := base()
		cur := base()
		cur.ServerTotal = 6
		if !g.hasSignificantChange(old, cur) {
			t.Error("expected true when server total changed")
		}
	})

	t.Run("server online changed", func(t *testing.T) {
		old := base()
		cur := base()
		cur.ServerOnline = 3
		if !g.hasSignificantChange(old, cur) {
			t.Error("expected true when server online changed")
		}
	})

	t.Run("server offline changed", func(t *testing.T) {
		old := base()
		cur := base()
		cur.ServerOffline = 2
		if !g.hasSignificantChange(old, cur) {
			t.Error("expected true when server offline changed")
		}
	})

	t.Run("server warning changed", func(t *testing.T) {
		old := base()
		cur := base()
		cur.ServerWarning = 1
		if !g.hasSignificantChange(old, cur) {
			t.Error("expected true when server warning changed")
		}
	})

	t.Run("alert total changed", func(t *testing.T) {
		old := base()
		cur := base()
		cur.AlertTotal = 5
		if !g.hasSignificantChange(old, cur) {
			t.Error("expected true when alert total changed")
		}
	})

	t.Run("alert critical changed", func(t *testing.T) {
		old := base()
		cur := base()
		cur.AlertCritical = 2
		if !g.hasSignificantChange(old, cur) {
			t.Error("expected true when alert critical changed")
		}
	})

	t.Run("alert warning changed", func(t *testing.T) {
		old := base()
		cur := base()
		cur.AlertWarning = 0
		if !g.hasSignificantChange(old, cur) {
			t.Error("expected true when alert warning changed")
		}
	})

	t.Run("alert info changed", func(t *testing.T) {
		old := base()
		cur := base()
		cur.AlertInfo = 2
		if !g.hasSignificantChange(old, cur) {
			t.Error("expected true when alert info changed")
		}
	})

	t.Run("active blackout count changed", func(t *testing.T) {
		old := base()
		cur := base()
		cur.ActiveBlackouts = []database.EstateBlackoutSummary{
			{Scope: "global", Reason: "maintenance"},
		}
		if !g.hasSignificantChange(old, cur) {
			t.Error("expected true when active blackouts changed")
		}
	})

	t.Run("upcoming blackout change alone is not significant", func(t *testing.T) {
		old := base()
		cur := base()
		cur.UpcomingBlackouts = []database.EstateBlackoutSummary{
			{Scope: "server", Reason: "patching"},
		}
		if g.hasSignificantChange(old, cur) {
			t.Error("expected false when only upcoming blackouts changed")
		}
	})
}

// --- extractTextFromResponse tests -----------------------------------------

// thinkingBlock is the content-block type a reasoning model emits for its
// chain of thought. The library has no named constant for it, so the tests
// use the wire value directly to reproduce the issue #399 response shape.
const thinkingBlock pgllm.ContentBlockType = "thinking"

func TestExtractTextFromResponse(t *testing.T) {
	tests := []struct {
		name    string
		content []pgllm.ContentBlock
		want    string
		wantErr bool
	}{
		{
			name:    "empty content is an error",
			content: nil,
			wantErr: true,
		},
		{
			name: "single text block",
			content: []pgllm.ContentBlock{
				{Type: pgllm.BlockText, Text: "Hello"},
			},
			want: "Hello",
		},
		{
			name: "multiple text blocks",
			content: []pgllm.ContentBlock{
				{Type: pgllm.BlockText, Text: "Hello "},
				{Type: pgllm.BlockText, Text: "World"},
			},
			want: "Hello World",
		},
		{
			name: "non-text block only is an error",
			content: []pgllm.ContentBlock{
				{Type: pgllm.BlockToolUse, Text: "ignored"},
			},
			wantErr: true,
		},
		{
			// The regression from issue #399: a reasoning model that
			// spends its whole budget thinking emits a thinking block
			// and no text block.
			name: "reasoning-only response is an error",
			content: []pgllm.ContentBlock{
				{Type: thinkingBlock, Text: "let me think about this at length"},
			},
			wantErr: true,
		},
		{
			name: "whitespace-only text is an error",
			content: []pgllm.ContentBlock{
				{Type: pgllm.BlockText, Text: "  \n\t "},
			},
			wantErr: true,
		},
		{
			name: "mixed blocks concatenate only text",
			content: []pgllm.ContentBlock{
				{Type: pgllm.BlockText, Text: "Part1"},
				{Type: pgllm.BlockToolUse, Text: "skip"},
				{Type: pgllm.BlockText, Text: "Part2"},
			},
			want: "Part1Part2",
		},
		{
			name: "text alongside thinking passes through unchanged",
			content: []pgllm.ContentBlock{
				{Type: thinkingBlock, Text: "thinking"},
				{Type: pgllm.BlockText, Text: " Estate is healthy. "},
			},
			want: " Estate is healthy. ",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := &pgllm.ChatResponse{Content: tc.content}
			got, err := extractTextFromResponse(resp)
			if tc.wantErr {
				if !errors.Is(err, llmproxy.ErrNoTextContent) {
					t.Fatalf("expected ErrNoTextContent, got %v", err)
				}
				if got != "" {
					t.Errorf("expected empty text on error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("expected %q, got %q", tc.want, got)
			}
		})
	}

	t.Run("nil response", func(t *testing.T) {
		got, err := extractTextFromResponse(nil)
		if !errors.Is(err, llmproxy.ErrNoTextContent) {
			t.Fatalf("expected ErrNoTextContent for nil response, got %v", err)
		}
		if got != "" {
			t.Errorf("expected empty string for nil response, got %q", got)
		}
	})
}

// --- evictScopedCacheLocked tests ------------------------------------------

func TestEvictScopedCacheLocked(t *testing.T) {
	g := &Generator{
		scopedCache: make(map[string]*scopedEntry),
	}

	// Fill the cache with scopedCacheMaxEntries + 5 entries.
	for i := 0; i < scopedCacheMaxEntries+5; i++ {
		key := strings.Repeat("x", 1) + string(rune('A'+i%26)) + strings.Repeat("y", i)
		g.scopedCache[key] = &scopedEntry{
			overview:   newTestOverview("test"),
			lastAccess: time.Now().UTC().Add(time.Duration(i) * time.Second),
		}
	}

	g.evictScopedCacheLocked()

	if len(g.scopedCache) != scopedCacheMaxEntries {
		t.Errorf("expected cache size %d after eviction, got %d",
			scopedCacheMaxEntries, len(g.scopedCache))
	}
}

func TestEvictScopedCacheLocked_RemovesOldest(t *testing.T) {
	g := &Generator{
		scopedCache: make(map[string]*scopedEntry),
	}

	now := time.Now().UTC()

	// Add exactly scopedCacheMaxEntries + 1 entries with known access
	// times. The oldest entry should be evicted.
	for i := 0; i <= scopedCacheMaxEntries; i++ {
		key := fmt.Sprintf("key:%d", i)
		g.scopedCache[key] = &scopedEntry{
			overview:   newTestOverview("test"),
			lastAccess: now.Add(time.Duration(i) * time.Second),
		}
	}

	g.evictScopedCacheLocked()

	// key:0 had the earliest lastAccess and should have been evicted.
	if _, ok := g.scopedCache["key:0"]; ok {
		t.Error("expected oldest entry 'key:0' to be evicted")
	}

	if len(g.scopedCache) != scopedCacheMaxEntries {
		t.Errorf("expected cache size %d, got %d",
			scopedCacheMaxEntries, len(g.scopedCache))
	}
}

// --- buildPrompt tests -----------------------------------------------------

func TestBuildPrompt_ContainsSnapshotData(t *testing.T) {
	s := &database.EstateSnapshot{
		ServerTotal:   5,
		ServerOnline:  4,
		ServerOffline: 1,
		ServerWarning: 0,
		AlertTotal:    2,
		AlertCritical: 1,
		AlertWarning:  1,
		AlertInfo:     0,
		Servers: []database.EstateServerSummary{
			{ID: 1, Name: "db-prod-1", Status: "offline"},
		},
		TopAlerts: []database.EstateAlertSummary{
			{Title: "High CPU", ServerName: "db-prod-1", Severity: "critical"},
		},
		ActiveBlackouts:   []database.EstateBlackoutSummary{},
		UpcomingBlackouts: []database.EstateBlackoutSummary{},
		RecentEvents:      []database.EstateEventSummary{},
	}

	system, data := buildPrompt(s)

	// The system prompt must contain the instruction text.
	if !strings.Contains(system, "PostgreSQL DBA assistant") {
		t.Error("system prompt missing instruction text")
	}

	// The data prompt must contain the snapshot values.
	checks := []string{
		"5 total",
		"4 online",
		"1 offline",
		"Offline server: db-prod-1",
		"2 total",
		"1 critical",
		"Critical alert on db-prod-1: High CPU",
	}

	for _, c := range checks {
		if !strings.Contains(data, c) {
			t.Errorf("data prompt missing expected text %q", c)
		}
	}
}

func TestBuildScopedPrompt_ContainsScopeContext(t *testing.T) {
	s := &database.EstateSnapshot{
		Servers:           []database.EstateServerSummary{},
		TopAlerts:         []database.EstateAlertSummary{},
		ActiveBlackouts:   []database.EstateBlackoutSummary{},
		UpcomingBlackouts: []database.EstateBlackoutSummary{},
		RecentEvents:      []database.EstateEventSummary{},
	}

	tests := []struct {
		scopeType string
		scopeName string
		contains  []string
	}{
		{
			scopeType: "server",
			scopeName: "db-prod-1",
			contains:  []string{"server", "db-prod-1"},
		},
		{
			scopeType: "cluster",
			scopeName: "east-cluster",
			contains:  []string{"cluster", "east-cluster"},
		},
		{
			scopeType: "group",
			scopeName: "production",
			contains:  []string{"group", "production"},
		},
		{
			scopeType: "connections",
			scopeName: "Custom Selection",
			contains:  []string{"selection", "Custom Selection"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.scopeType, func(t *testing.T) {
			system, data := buildScopedPrompt(s, tc.scopeType, tc.scopeName)
			combined := system + data
			for _, c := range tc.contains {
				if !strings.Contains(combined, c) {
					t.Errorf("scoped prompt for %s missing %q", tc.scopeType, c)
				}
			}
		})
	}
}

// --- GetOverview tests -----------------------------------------------------

func TestGetOverview_ReturnsNilWhenNotGenerated(t *testing.T) {
	g := &Generator{
		scopedCache: make(map[string]*scopedEntry),
	}

	if g.GetOverview() != nil {
		t.Error("expected nil when no overview has been generated")
	}
}

func TestGetOverview_ReturnsCurrent(t *testing.T) {
	g := &Generator{
		scopedCache: make(map[string]*scopedEntry),
	}

	expected := newTestOverview("test summary")
	g.mu.Lock()
	g.current = expected
	g.mu.Unlock()

	got := g.GetOverview()
	if got == nil {
		t.Fatal("expected non-nil overview")
	}
	if got.Summary != expected.Summary {
		t.Errorf("expected summary %q, got %q", expected.Summary, got.Summary)
	}
}

// --- containsRestart tests -------------------------------------------------

func TestContainsRestart(t *testing.T) {
	cutoff := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)

	t.Run("restart after cutoff returns true", func(t *testing.T) {
		snapshot := &database.EstateSnapshot{
			RecentEvents: []database.EstateEventSummary{
				{
					EventType:  "restart",
					OccurredAt: cutoff.Add(10 * time.Minute),
				},
			},
		}
		if !containsRestart(snapshot, cutoff) {
			t.Error("expected true when restart is after cutoff")
		}
	})

	t.Run("restart before cutoff returns false", func(t *testing.T) {
		snapshot := &database.EstateSnapshot{
			RecentEvents: []database.EstateEventSummary{
				{
					EventType:  "restart",
					OccurredAt: cutoff.Add(-10 * time.Minute),
				},
			},
		}
		if containsRestart(snapshot, cutoff) {
			t.Error("expected false when restart is before cutoff")
		}
	})

	t.Run("restart at exact cutoff returns false", func(t *testing.T) {
		snapshot := &database.EstateSnapshot{
			RecentEvents: []database.EstateEventSummary{
				{
					EventType:  "restart",
					OccurredAt: cutoff,
				},
			},
		}
		if containsRestart(snapshot, cutoff) {
			t.Error("expected false when restart is at exact cutoff (not strictly after)")
		}
	})

	t.Run("non-restart event after cutoff returns false", func(t *testing.T) {
		snapshot := &database.EstateSnapshot{
			RecentEvents: []database.EstateEventSummary{
				{
					EventType:  "config_change",
					OccurredAt: cutoff.Add(10 * time.Minute),
				},
			},
		}
		if containsRestart(snapshot, cutoff) {
			t.Error("expected false when only non-restart events are present")
		}
	})

	t.Run("empty events returns false", func(t *testing.T) {
		snapshot := &database.EstateSnapshot{
			RecentEvents: []database.EstateEventSummary{},
		}
		if containsRestart(snapshot, cutoff) {
			t.Error("expected false when no events")
		}
	})

	t.Run("mixed events with restart after cutoff returns true", func(t *testing.T) {
		snapshot := &database.EstateSnapshot{
			RecentEvents: []database.EstateEventSummary{
				{
					EventType:  "config_change",
					OccurredAt: cutoff.Add(5 * time.Minute),
				},
				{
					EventType:  "restart",
					OccurredAt: cutoff.Add(10 * time.Minute),
				},
			},
		}
		if !containsRestart(snapshot, cutoff) {
			t.Error("expected true when restart event exists after cutoff")
		}
	})
}

// --- forced refresh tests --------------------------------------------------

func TestForceRefresh_BypassesSignificantChangeCheck(t *testing.T) {
	// ForceRefresh calls refresh(true) which skips hasSignificantChange.
	// With a nil datastore, refresh(true) panics when it tries to fetch
	// the estate snapshot. This panic proves that the significant-change
	// gate was bypassed (refresh(false) with identical snapshots would
	// return early without touching the datastore).
	g := &Generator{
		scopedCache: make(map[string]*scopedEntry),
		ctx:         context.Background(),
	}

	// Set identical last and current snapshots so hasSignificantChange
	// would return false.
	g.mu.Lock()
	g.lastSnapshot = newTestSnapshot()
	g.mu.Unlock()

	panicked := generatorInvokePanics(func() {
		g.ForceRefresh()
	})
	if !panicked {
		t.Error("expected ForceRefresh to panic on nil datastore, proving it bypassed hasSignificantChange")
	}
}

func TestGetScopedSummary_ForceBypassesCache(t *testing.T) {
	g := &Generator{
		scopedCache: make(map[string]*scopedEntry),
		ctx:         context.Background(),
	}

	// Pre-populate a fresh cached entry for server:1.
	now := time.Now().UTC()
	g.mu.Lock()
	g.scopedCache["server:1"] = &scopedEntry{
		overview: &Overview{
			Summary:     "Cached server overview.",
			GeneratedAt: now,
			StaleAt:     now.Add(5 * time.Minute),
			Snapshot:    newTestSnapshot(),
		},
		lastAccess: now,
	}
	g.mu.Unlock()

	// force=false should return the cached entry without error.
	ov, err := g.GetScopedSummary("server", 1, false)
	if err != nil {
		t.Fatalf("expected no error for cached entry, got %v", err)
	}
	if ov.Summary != "Cached server overview." {
		t.Errorf("expected cached summary, got %q", ov.Summary)
	}

	// force=true should bypass the cache and attempt to regenerate.
	// With a nil datastore, fetchScopedSnapshot panics; catching the
	// panic proves the cache was bypassed.
	panicked := generatorInvokePanics(func() {
		_, _ = g.GetScopedSummary("server", 1, true)
	})
	if !panicked {
		t.Error("expected GetScopedSummary with force=true to panic on nil datastore, proving cache bypass")
	}
}

func TestGetConnectionsSummary_ForceBypassesCache(t *testing.T) {
	g := &Generator{
		scopedCache: make(map[string]*scopedEntry),
		ctx:         context.Background(),
	}

	// Pre-populate a fresh cached entry for connections:1,2.
	now := time.Now().UTC()
	g.mu.Lock()
	g.scopedCache["connections:1,2"] = &scopedEntry{
		overview: &Overview{
			Summary:     "Cached connections overview.",
			GeneratedAt: now,
			StaleAt:     now.Add(5 * time.Minute),
			Snapshot:    newTestSnapshot(),
		},
		lastAccess: now,
	}
	g.mu.Unlock()

	// force=false should return the cached entry without error.
	ov, err := g.GetConnectionsSummary([]int{1, 2}, "test", false)
	if err != nil {
		t.Fatalf("expected no error for cached entry, got %v", err)
	}
	if ov.Summary != "Cached connections overview." {
		t.Errorf("expected cached summary, got %q", ov.Summary)
	}

	// force=true should bypass the cache and attempt to regenerate.
	// With a nil datastore, GetConnectionsSnapshot panics; catching
	// the panic proves the cache was bypassed.
	panicked := generatorInvokePanics(func() {
		_, _ = g.GetConnectionsSummary([]int{1, 2}, "test", true)
	})
	if !panicked {
		t.Error("expected GetConnectionsSummary with force=true to panic on nil datastore, proving cache bypass")
	}
}

// generatorInvokePanics calls fn and returns true if fn panicked.
func generatorInvokePanics(fn func()) (panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			panicked = true
		}
	}()
	fn()
	return false
}

// --- hasSignificantChange restart tests ------------------------------------

func TestHasSignificantChange_RestartDetection(t *testing.T) {
	g := &Generator{
		scopedCache: make(map[string]*scopedEntry),
	}

	oldTime := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)

	t.Run("restart after old timestamp is significant", func(t *testing.T) {
		old := &database.EstateSnapshot{
			Timestamp:       oldTime,
			ServerTotal:     5,
			ServerOnline:    4,
			ServerOffline:   1,
			AlertTotal:      3,
			AlertCritical:   1,
			AlertWarning:    1,
			AlertInfo:       1,
			ActiveBlackouts: []database.EstateBlackoutSummary{},
			RecentEvents:    []database.EstateEventSummary{},
		}
		cur := &database.EstateSnapshot{
			Timestamp:       oldTime.Add(time.Minute),
			ServerTotal:     5,
			ServerOnline:    4,
			ServerOffline:   1,
			AlertTotal:      3,
			AlertCritical:   1,
			AlertWarning:    1,
			AlertInfo:       1,
			ActiveBlackouts: []database.EstateBlackoutSummary{},
			RecentEvents: []database.EstateEventSummary{
				{
					EventType:  "restart",
					OccurredAt: oldTime.Add(30 * time.Second),
				},
			},
		}
		if !g.hasSignificantChange(old, cur) {
			t.Error("expected true when restart event is newer than old snapshot")
		}
	})

	t.Run("restart before old timestamp is not significant", func(t *testing.T) {
		old := &database.EstateSnapshot{
			Timestamp:       oldTime,
			ServerTotal:     5,
			ServerOnline:    4,
			ServerOffline:   1,
			AlertTotal:      3,
			AlertCritical:   1,
			AlertWarning:    1,
			AlertInfo:       1,
			ActiveBlackouts: []database.EstateBlackoutSummary{},
			RecentEvents:    []database.EstateEventSummary{},
		}
		cur := &database.EstateSnapshot{
			Timestamp:       oldTime.Add(time.Minute),
			ServerTotal:     5,
			ServerOnline:    4,
			ServerOffline:   1,
			AlertTotal:      3,
			AlertCritical:   1,
			AlertWarning:    1,
			AlertInfo:       1,
			ActiveBlackouts: []database.EstateBlackoutSummary{},
			RecentEvents: []database.EstateEventSummary{
				{
					EventType:  "restart",
					OccurredAt: oldTime.Add(-10 * time.Minute),
				},
			},
		}
		if g.hasSignificantChange(old, cur) {
			t.Error("expected false when restart event is older than old snapshot")
		}
	})
}

// --- OnRestart callback and cache flush tests ------------------------------

func TestOnRestart_CallbackAndCacheFlush(t *testing.T) {
	g := &Generator{
		scopedCache: make(map[string]*scopedEntry),
	}

	// Populate scoped cache
	g.scopedCache["server:1"] = &scopedEntry{
		overview:   newTestOverview("server 1 overview"),
		lastAccess: time.Now().UTC(),
	}
	g.scopedCache["cluster:2"] = &scopedEntry{
		overview:   newTestOverview("cluster 2 overview"),
		lastAccess: time.Now().UTC(),
	}

	// Register callback
	callbackCalled := false
	g.OnRestart(func() {
		callbackCalled = true
	})

	// Simulate what refresh() does on restart detection
	g.mu.Lock()
	g.scopedCache = make(map[string]*scopedEntry)
	if g.onRestart != nil {
		g.onRestart()
	}
	g.mu.Unlock()

	if !callbackCalled {
		t.Error("expected onRestart callback to be called")
	}
	if len(g.scopedCache) != 0 {
		t.Errorf("expected empty scoped cache after restart, got %d entries",
			len(g.scopedCache))
	}
}

// --- createLLMClient tests -------------------------------------------------

func TestCreateLLMClient_Success(t *testing.T) {
	// A configured LLM provider yields a non-nil client. Use Ollama
	// since the factory only requires the URL to be present.
	g := NewGenerator(nil, &llmproxy.Config{
		Provider:  "ollama",
		Model:     "llama3",
		OllamaURL: "http://localhost:11434",
	})

	client, err := g.createLLMClient()
	if err != nil {
		t.Fatalf("expected no error when provider is configured, got %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client when provider is configured")
	}
	if client.Provider() != "ollama" {
		t.Errorf("expected ollama provider, got %q", client.Provider())
	}
}

func TestCreateLLMClient_AnthropicWithKey(t *testing.T) {
	// Confirm the field projection wires the API key through to the
	// library client.
	g := NewGenerator(nil, &llmproxy.Config{
		Provider:        "anthropic",
		Model:           "claude-3",
		AnthropicAPIKey: "test-key",
	})

	client, err := g.createLLMClient()
	if err != nil {
		t.Fatalf("expected no error when API key is supplied, got %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client when API key is supplied")
	}
	if client.Model() != "claude-3" {
		t.Errorf("expected model claude-3, got %q", client.Model())
	}
}

func TestCreateLLMClient_ProviderFieldProjection(t *testing.T) {
	// Each provider branch must wire its own API key and base URL into
	// the library options and yield a client reporting that provider.
	tests := []struct {
		name   string
		config *llmproxy.Config
	}{
		{
			name: "openai",
			config: &llmproxy.Config{
				Provider:      "openai",
				Model:         "gpt-4o",
				OpenAIAPIKey:  "test-key",
				OpenAIBaseURL: "https://api.openai.com/v1",
			},
		},
		{
			name: "gemini",
			config: &llmproxy.Config{
				Provider:     "gemini",
				Model:        "gemini-2.5-flash",
				GeminiAPIKey: "test-key",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGenerator(nil, tc.config)
			client, err := g.createLLMClient()
			if err != nil {
				t.Fatalf("expected no error for %s, got %v", tc.name, err)
			}
			if client == nil {
				t.Fatalf("expected non-nil client for %s", tc.name)
			}
			if client.Provider() != tc.name {
				t.Errorf("expected provider %q, got %q", tc.name, client.Provider())
			}
		})
	}
}

func TestGenerateSummaryFromPrompt_Success(t *testing.T) {
	// Drive the full happy path through the library client by pointing
	// an OpenAI-compatible provider at a stub server that returns a
	// chat completion. This exercises the Chat call and text extraction.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices": [{"message": {"role": "assistant", "content": "All servers healthy."}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
		}`))
	}))
	defer srv.Close()

	g := NewGenerator(nil, &llmproxy.Config{
		Provider:      "openai",
		Model:         "gpt-4o",
		OpenAIAPIKey:  "test-key",
		OpenAIBaseURL: srv.URL,
	})

	summary, err := g.generateSummaryFromPrompt(context.Background(), "system", "data")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if summary != "All servers healthy." {
		t.Errorf("expected summary text from stub, got %q", summary)
	}
}

func TestGenerateSummaryFromPrompt_ChatError(t *testing.T) {
	// A provider whose endpoint returns a non-retryable 4xx status makes
	// Chat fail immediately; the wrapped 'LLM chat failed' error must
	// surface to the caller. A 400 avoids the retry backoff that 5xx and
	// 429 would trigger, keeping the test fast.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer srv.Close()

	g := NewGenerator(nil, &llmproxy.Config{
		Provider:      "openai",
		Model:         "gpt-4o",
		OpenAIAPIKey:  "test-key",
		OpenAIBaseURL: srv.URL,
	})

	summary, err := g.generateSummaryFromPrompt(context.Background(), "system", "data")
	if err == nil {
		t.Fatal("expected an error when the LLM endpoint fails")
	}
	if summary != "" {
		t.Errorf("expected empty summary on error, got %q", summary)
	}
	if !strings.Contains(err.Error(), "LLM chat failed") {
		t.Errorf("expected 'LLM chat failed' in error, got %v", err)
	}
}

// TestGenerateSummaryFromPrompt_MaxTokensHonoursConfig verifies that the
// chat request carries the operator-configured llm.max_tokens, and falls
// back to llmproxy.DefaultAnalysisMaxTokens when the setting is unset or
// non-positive. Regression cover for issue #399, where a hardcoded
// 512-token cap starved reasoning models of output budget.
func TestGenerateSummaryFromPrompt_MaxTokensHonoursConfig(t *testing.T) {
	tests := []struct {
		name      string
		maxTokens int
		want      float64
	}{
		{"configured value used", 8192, 8192},
		{"unset falls back", 0, llmproxy.DefaultAnalysisMaxTokens},
		{"negative falls back", -5, llmproxy.DefaultAnalysisMaxTokens},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				var payload map[string]any
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Errorf("failed to decode request body: %v", err)
				}
				got = payload["max_tokens"]
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{
					"choices": [{"message": {"role": "assistant", "content": "ok"}, "finish_reason": "stop"}]
				}`))
			}))
			defer srv.Close()

			g := NewGenerator(nil, &llmproxy.Config{
				Provider:      "openai",
				Model:         "gpt-4o",
				OpenAIAPIKey:  "test-key",
				OpenAIBaseURL: srv.URL,
				MaxTokens:     tc.maxTokens,
			})

			if _, err := g.generateSummaryFromPrompt(context.Background(), "system", "data"); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			num, ok := got.(float64)
			if !ok {
				t.Fatalf("max_tokens missing or not a number in request: %#v", got)
			}
			if num != tc.want {
				t.Errorf("max_tokens: got %v, want %v", num, tc.want)
			}
		})
	}
}

// TestGenerateSummaryFromPrompt_NoTextContentIsError verifies that a
// response carrying no usable text (the shape a reasoning model produces
// when its thinking consumes the whole output budget) surfaces as an
// error rather than a silently empty summary.
func TestGenerateSummaryFromPrompt_NoTextContentIsError(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"empty string content", `""`},
		{"whitespace-only content", `"   \n  "`},
		{"null content", `null`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{
					"choices": [{"message": {"role": "assistant", "content": %s}, "finish_reason": "length"}]
				}`, tc.content)
			}))
			defer srv.Close()

			g := NewGenerator(nil, &llmproxy.Config{
				Provider:      "openai",
				Model:         "gpt-4o",
				OpenAIAPIKey:  "test-key",
				OpenAIBaseURL: srv.URL,
			})

			summary, err := g.generateSummaryFromPrompt(context.Background(), "system", "data")
			if !errors.Is(err, llmproxy.ErrNoTextContent) {
				t.Fatalf("expected ErrNoTextContent, got %v", err)
			}
			if summary != "" {
				t.Errorf("expected empty summary on error, got %q", summary)
			}
		})
	}
}

func TestGenerateSummaryFromPrompt_NoProviderError(t *testing.T) {
	// With no provider configured, createLLMClient fails and the wrapped
	// error must surface to the caller rather than panicking.
	g := NewGenerator(nil, &llmproxy.Config{})

	summary, err := g.generateSummaryFromPrompt(context.Background(), "system", "data")
	if err == nil {
		t.Fatal("expected an error when no provider is configured")
	}
	if summary != "" {
		t.Errorf("expected empty summary on error, got %q", summary)
	}
	if !strings.Contains(err.Error(), "no LLM provider configured") {
		t.Errorf("expected 'no LLM provider configured' in error, got %v", err)
	}
}

func TestCreateLLMClient_MissingProviderReturnsError(t *testing.T) {
	// An empty provider must not panic and must return an error so
	// callers can disable LLM features gracefully.
	g := NewGenerator(nil, &llmproxy.Config{})

	client, err := g.createLLMClient()
	if err == nil {
		t.Fatal("expected an error when provider is empty")
	}
	if client != nil {
		t.Errorf("expected nil client when provider is empty, got %T", client)
	}
}

func TestCreateLLMClient_UnknownProviderReturnsError(t *testing.T) {
	// A non-empty but unregistered provider passes the empty-provider
	// guard yet fails pgllm.NewClient; the construction error must surface
	// to the caller rather than yielding a client.
	g := NewGenerator(nil, &llmproxy.Config{
		Provider: "does-not-exist",
		Model:    "m",
	})

	client, err := g.createLLMClient()
	if err == nil {
		t.Fatal("expected an error for an unregistered provider")
	}
	if client != nil {
		t.Errorf("expected nil client for an unregistered provider, got %T", client)
	}
}

func TestCreateLLMClient_NilConfigReturnsError(t *testing.T) {
	// NewGenerator accepts a nil *llmproxy.Config when AI is disabled or the
	// LLM config is omitted. createLLMClient must not panic dereferencing the
	// nil config; it returns an error so the caller paths degrade gracefully.
	g := NewGenerator(nil, nil)

	client, err := g.createLLMClient()
	if err == nil {
		t.Fatal("expected an error when llmConfig is nil")
	}
	if client != nil {
		t.Errorf("expected nil client when llmConfig is nil, got %T", client)
	}
	if !strings.Contains(err.Error(), "no LLM provider configured") {
		t.Errorf("expected 'no LLM provider configured' error, got %v", err)
	}
}

func TestGenerateSummaryFromPrompt_NilConfigNoPanic(t *testing.T) {
	// The whole summary path must remain a graceful no-op when AI is
	// disabled (nil config): createLLMClient returns an error which the
	// caller wraps, and no panic occurs.
	g := NewGenerator(nil, nil)

	summary, err := g.generateSummaryFromPrompt(context.Background(), "system", "data")
	if err == nil {
		t.Fatal("expected an error when llmConfig is nil")
	}
	if summary != "" {
		t.Errorf("expected empty summary on nil config, got %q", summary)
	}
}

func TestCreateLLMClient_TimeoutSecondsHonoured(t *testing.T) {
	// A positive TimeoutSeconds on the underlying config must produce a
	// usable client; the timeout is applied to the library Options. The
	// library defers credential validation, so a valid provider with a
	// positive timeout yields a non-nil client without error.
	g := NewGenerator(nil, &llmproxy.Config{
		Provider: "ollama",
		Model:    "llama3",
		LLMConfig: &config.LLMConfig{
			TimeoutSeconds: 45,
		},
	})

	client, err := g.createLLMClient()
	if err != nil {
		t.Fatalf("expected no error with positive timeout, got %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client when timeout is configured")
	}
}

func TestCreateLLMClient_AnthropicMissingKeyConstructs(t *testing.T) {
	// The library defers credential validation to request time, so a
	// missing API key still yields a usable client object; the failure
	// only surfaces when Chat is called. This differs from the old chat
	// factory, which validated credentials at construction.
	g := NewGenerator(nil, &llmproxy.Config{
		Provider: "anthropic",
		Model:    "claude-3",
	})

	client, err := g.createLLMClient()
	if err != nil {
		t.Fatalf("expected no construction error, got %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client; credential validation is deferred")
	}
}

func TestCreateLLMClient_HeaderLoadErrorIsLogged(t *testing.T) {
	// A header file that does not exist makes getProviderHeaders return
	// an error. createLLMClient must log it and proceed; with a valid
	// provider it still returns a non-nil client. Capture os.Stderr to
	// confirm the error message was emitted, since the production code
	// writes the diagnostic with fmt.Fprintf(os.Stderr, ...).
	const headerPath = "/path/to/nonexistent/header/file"
	g := NewGenerator(nil, &llmproxy.Config{
		Provider:  "ollama",
		Model:     "llama3",
		OllamaURL: "http://localhost:11434",
		LLMConfig: &config.LLMConfig{
			CustomHeadersFiles: map[string]string{
				"X-Header": headerPath,
			},
		},
	})

	originalStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stderr = w

	// Read the captured output asynchronously so a full pipe buffer
	// cannot deadlock the goroutine writing to stderr.
	type readResult struct {
		data []byte
		err  error
	}
	done := make(chan readResult, 1)
	go func() {
		data, readErr := io.ReadAll(r)
		done <- readResult{data: data, err: readErr}
	}()

	client, _ := g.createLLMClient()

	// Restore stderr before any further test output and close the
	// writer so io.ReadAll returns.
	if closeErr := w.Close(); closeErr != nil {
		os.Stderr = originalStderr
		t.Fatalf("failed to close pipe writer: %v", closeErr)
	}
	os.Stderr = originalStderr

	if client == nil {
		t.Fatal("expected non-nil client; header error must not abort construction")
	}

	result := <-done
	if result.err != nil {
		t.Fatalf("failed to read captured stderr: %v", result.err)
	}
	output := string(result.data)
	if !strings.Contains(output, "Failed to get ollama provider headers") {
		t.Errorf("expected stderr to mention the header-load failure, got: %q", output)
	}
	if !strings.Contains(output, headerPath) {
		t.Errorf("expected stderr to mention header file path %q, got: %q", headerPath, output)
	}
}

func TestCreateLLMClient_HeadersWiredFromConfig(t *testing.T) {
	// A populated config-level LLMConfig with custom headers should
	// flow through getProviderHeaders into the library client without
	// returning an error. This exercises the non-nil branch of
	// getProviderHeaders.
	g := NewGenerator(nil, &llmproxy.Config{
		Provider:  "ollama",
		Model:     "llama3",
		OllamaURL: "http://localhost:11434",
		LLMConfig: &config.LLMConfig{
			OllamaCustomHeaders: map[string]string{"X-Test": "value"},
		},
	})

	client, err := g.createLLMClient()
	if err != nil {
		t.Fatalf("expected no error when headers are configured, got %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client when headers are configured")
	}
}
