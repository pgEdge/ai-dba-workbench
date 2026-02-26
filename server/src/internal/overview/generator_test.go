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
