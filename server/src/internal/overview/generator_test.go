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
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/chat"
	"github.com/pgedge/ai-workbench/server/internal/database"
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

func TestExtractTextFromResponse(t *testing.T) {
	tests := []struct {
		name    string
		content []interface{}
		want    string
	}{
		{
			name:    "empty content",
			content: nil,
			want:    "",
		},
		{
			name: "single TextContent",
			content: []interface{}{
				chat.TextContent{Type: "text", Text: "Hello"},
			},
			want: "Hello",
		},
		{
			name: "multiple TextContent blocks",
			content: []interface{}{
				chat.TextContent{Type: "text", Text: "Hello "},
				chat.TextContent{Type: "text", Text: "World"},
			},
			want: "Hello World",
		},
		{
			name: "map-based text content",
			content: []interface{}{
				map[string]interface{}{
					"type": "text",
					"text": "From map",
				},
			},
			want: "From map",
		},
		{
			name: "mixed content types",
			content: []interface{}{
				chat.TextContent{Type: "text", Text: "Part1"},
				map[string]interface{}{
					"type": "text",
					"text": "Part2",
				},
			},
			want: "Part1Part2",
		},
		{
			name: "non-text map ignored",
			content: []interface{}{
				map[string]interface{}{
					"type": "tool_use",
					"name": "some_tool",
				},
			},
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := chat.LLMResponse{Content: tc.content}
			got := extractTextFromResponse(resp)
			if got != tc.want {
				t.Errorf("expected %q, got %q", tc.want, got)
			}
		})
	}
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

	prompt := buildPrompt(s)

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
		if !strings.Contains(prompt, c) {
			t.Errorf("prompt missing expected text %q", c)
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
			prompt := buildScopedPrompt(s, tc.scopeType, tc.scopeName)
			for _, c := range tc.contains {
				if !strings.Contains(prompt, c) {
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
