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
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/chat"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/llmproxy"
	"golang.org/x/sync/singleflight"
)

const (
	// tickInterval is how often the generator checks for estate changes.
	tickInterval = 60 * time.Second

	// staleDuration is how long an overview is considered fresh.
	staleDuration = 5 * time.Minute

	// llmMaxTokens caps the summary length for concise output.
	llmMaxTokens = 512

	// llmTemperature controls response creativity; low for factual output.
	llmTemperature = 0.3

	// scopedCacheMaxEntries limits the number of cached scoped summaries
	// to prevent unbounded memory growth. When the limit is reached the
	// oldest entries are evicted.
	scopedCacheMaxEntries = 100
)

// Overview holds the current AI-generated estate summary and metadata.
type Overview struct {
	Summary         string                   `json:"summary"`
	GeneratedAt     time.Time                `json:"generated_at"`
	StaleAt         time.Time                `json:"stale_at"`
	Snapshot        *database.EstateSnapshot `json:"snapshot"`
	RestartDetected bool                     `json:"restart_detected,omitempty"`
}

// scopedEntry is a cached scoped overview together with the time it
// was last accessed, used for LRU-style eviction.
type scopedEntry struct {
	overview   *Overview
	lastAccess time.Time
}

// Generator periodically gathers estate state from the datastore, detects
// meaningful changes, and calls the LLM to produce a natural-language
// summary that is served to clients via the REST API. It also supports
// on-demand generation of scoped summaries for individual servers,
// clusters, and groups.
type Generator struct {
	datastore    *database.Datastore
	llmConfig    *llmproxy.Config
	mu           sync.RWMutex
	current      *Overview
	lastSnapshot *database.EstateSnapshot
	scopedCache  map[string]*scopedEntry
	inflight     singleflight.Group
	ctx          context.Context
	cancel       context.CancelFunc
	onRestart    func()
	hub          *Hub
}

// NewGenerator creates a new overview generator. It does not start any
// background goroutines; call Start to begin periodic generation.
func NewGenerator(datastore *database.Datastore, llmConfig *llmproxy.Config) *Generator {
	return &Generator{
		datastore:   datastore,
		llmConfig:   llmConfig,
		scopedCache: make(map[string]*scopedEntry),
	}
}

// Start begins the background goroutine that periodically generates the
// estate overview. On first invocation it generates immediately, then
// refreshes on a fixed ticker interval.
func (g *Generator) Start(ctx context.Context) {
	g.ctx, g.cancel = context.WithCancel(ctx)

	go func() {
		// Generate immediately on startup.
		g.refresh(false)

		ticker := time.NewTicker(tickInterval)
		defer ticker.Stop()
		for {
			select {
			case <-g.ctx.Done():
				return
			case <-ticker.C:
				g.refresh(false)
			}
		}
	}()
}

// ForceRefresh unconditionally regenerates the estate-wide overview,
// bypassing the significant-change check. The result is broadcast to
// SSE clients via the hub so connected subscribers receive the update.
func (g *Generator) ForceRefresh() {
	g.refresh(true)
}

// Stop cancels the background goroutine.
func (g *Generator) Stop() {
	if g.cancel != nil {
		g.cancel()
	}
}

// SetHub assigns the SSE hub used for broadcasting overview updates
// to connected clients.
func (g *Generator) SetHub(hub *Hub) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.hub = hub
}

// OnRestart registers a callback that is invoked when a PostgreSQL
// restart is detected during a refresh cycle. The callback runs
// under the generator's write lock, so it must not call back into
// the generator.
func (g *Generator) OnRestart(fn func()) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.onRestart = fn
}

// containsRestart reports whether the snapshot contains any restart
// events that occurred strictly after the given cutoff time.
func containsRestart(snapshot *database.EstateSnapshot, after time.Time) bool {
	for _, ev := range snapshot.RecentEvents {
		if ev.EventType == "restart" && ev.OccurredAt.After(after) {
			return true
		}
	}
	return false
}

// GetOverview returns the current estate-wide overview in a thread-safe
// manner. It returns nil if no overview has been generated yet.
func (g *Generator) GetOverview() *Overview {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.current
}

// GetScopedSummary returns a cached or freshly generated overview for
// the given scope. The scopeType must be "server", "cluster", or
// "group" and scopeID is the corresponding database identifier. Cached
// entries are returned when they are still within the stale duration;
// otherwise a new summary is generated on demand. Concurrent requests
// for the same scope are deduplicated so that only one LLM call is
// made; subsequent callers wait for and share the result.
func (g *Generator) GetScopedSummary(scopeType string, scopeID int, force bool) (*Overview, error) {
	key := fmt.Sprintf("%s:%d", scopeType, scopeID)

	// Fast path: return cached entry if still fresh.
	if !force {
		g.mu.RLock()
		if entry, ok := g.scopedCache[key]; ok {
			if time.Now().UTC().Before(entry.overview.StaleAt) {
				entry.lastAccess = time.Now().UTC()
				g.mu.RUnlock()
				return entry.overview, nil
			}
		}
		g.mu.RUnlock()
	}

	// Deduplicate concurrent requests for the same key.
	result, err, _ := g.inflight.Do(key, func() (interface{}, error) {
		// Re-check cache; another goroutine may have populated it
		// while we waited for the singleflight slot.
		if !force {
			g.mu.RLock()
			if entry, ok := g.scopedCache[key]; ok {
				if time.Now().UTC().Before(entry.overview.StaleAt) {
					entry.lastAccess = time.Now().UTC()
					g.mu.RUnlock()
					return entry.overview, nil
				}
			}
			g.mu.RUnlock()
		}

		// Generate a fresh scoped overview.
		snapshot, scopeName, err := g.fetchScopedSnapshot(scopeType, scopeID)
		if err != nil {
			return nil, err
		}

		system, data := buildScopedPrompt(snapshot, scopeType, scopeName)
		summary, err := g.generateSummaryFromPrompt(g.ctx, system, data)
		if err != nil {
			return nil, fmt.Errorf("failed to generate scoped summary: %w", err)
		}

		now := time.Now().UTC()
		overview := &Overview{
			Summary:     summary,
			GeneratedAt: now,
			StaleAt:     now.Add(staleDuration),
			Snapshot:    snapshot,
		}

		// Store in cache with eviction.
		g.mu.Lock()
		g.scopedCache[key] = &scopedEntry{
			overview:   overview,
			lastAccess: now,
		}
		g.evictScopedCacheLocked()
		g.mu.Unlock()

		if g.hub != nil {
			g.hub.Broadcast(overview, key)
		}

		fmt.Fprintf(os.Stderr,
			"Overview: generated scoped summary for %s at %s\n",
			key, now.Format(time.RFC3339))

		return overview, nil
	})
	if err != nil {
		return nil, err
	}
	overview, ok := result.(*Overview)
	if !ok {
		return nil, fmt.Errorf("unexpected singleflight result type")
	}
	return overview, nil
}

// GetConnectionsSummary returns a cached or freshly generated overview
// for an explicit list of connection IDs. The scopeName is used in the
// LLM prompt to give context (e.g. "Spock Cluster: my-cluster"). The
// cache key is derived from the sorted connection IDs so that the same
// set of connections always hits the same cache entry regardless of the
// order in which the IDs were supplied. Concurrent requests for the
// same set of connections are deduplicated so that only one LLM call
// is made.
func (g *Generator) GetConnectionsSummary(connectionIDs []int, scopeName string, force bool) (*Overview, error) {
	// Build a deterministic cache key from sorted connection IDs.
	sorted := make([]int, len(connectionIDs))
	copy(sorted, connectionIDs)
	sortInts(sorted)

	parts := make([]string, len(sorted))
	for i, id := range sorted {
		parts[i] = fmt.Sprintf("%d", id)
	}
	key := "connections:" + strings.Join(parts, ",")

	// Fast path: return cached entry if still fresh.
	if !force {
		g.mu.RLock()
		if entry, ok := g.scopedCache[key]; ok {
			if time.Now().UTC().Before(entry.overview.StaleAt) {
				entry.lastAccess = time.Now().UTC()
				g.mu.RUnlock()
				return entry.overview, nil
			}
		}
		g.mu.RUnlock()
	}

	// Deduplicate concurrent requests for the same key.
	result, err, _ := g.inflight.Do(key, func() (interface{}, error) {
		// Re-check cache; another goroutine may have populated it
		// while we waited for the singleflight slot.
		if !force {
			g.mu.RLock()
			if entry, ok := g.scopedCache[key]; ok {
				if time.Now().UTC().Before(entry.overview.StaleAt) {
					entry.lastAccess = time.Now().UTC()
					g.mu.RUnlock()
					return entry.overview, nil
				}
			}
			g.mu.RUnlock()
		}

		// Generate a fresh snapshot from the explicit connection IDs.
		snapshot := g.datastore.GetConnectionsSnapshot(g.ctx, connectionIDs)

		system, data := buildScopedPrompt(snapshot, "connections", scopeName)
		summary, err := g.generateSummaryFromPrompt(g.ctx, system, data)
		if err != nil {
			return nil, fmt.Errorf("failed to generate connections summary: %w", err)
		}

		now := time.Now().UTC()
		overview := &Overview{
			Summary:     summary,
			GeneratedAt: now,
			StaleAt:     now.Add(staleDuration),
			Snapshot:    snapshot,
		}

		// Store in cache with eviction.
		g.mu.Lock()
		g.scopedCache[key] = &scopedEntry{
			overview:   overview,
			lastAccess: now,
		}
		g.evictScopedCacheLocked()
		g.mu.Unlock()

		if g.hub != nil {
			g.hub.Broadcast(overview, key)
		}

		fmt.Fprintf(os.Stderr,
			"Overview: generated connections summary for %s at %s\n",
			key, now.Format(time.RFC3339))

		return overview, nil
	})
	if err != nil {
		return nil, err
	}
	overview, ok := result.(*Overview)
	if !ok {
		return nil, fmt.Errorf("unexpected singleflight result type")
	}
	return overview, nil
}

// sortInts sorts a slice of ints in ascending order using insertion sort.
// The slices are small (typically under 20 elements) so a simple
// algorithm avoids importing sort.
func sortInts(a []int) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j-1] > a[j]; j-- {
			a[j-1], a[j] = a[j], a[j-1]
		}
	}
}

// fetchScopedSnapshot retrieves the snapshot and scope name for the
// given scope type and ID from the datastore.
func (g *Generator) fetchScopedSnapshot(scopeType string, scopeID int) (*database.EstateSnapshot, string, error) {
	switch scopeType {
	case "server":
		return g.datastore.GetServerSnapshot(g.ctx, scopeID)
	case "cluster":
		return g.datastore.GetClusterSnapshot(g.ctx, scopeID)
	case "group":
		return g.datastore.GetGroupSnapshot(g.ctx, scopeID)
	default:
		return nil, "", fmt.Errorf("unknown scope type: %s", scopeType)
	}
}

// evictScopedCacheLocked removes the least-recently-accessed entries
// when the cache exceeds its maximum size. The caller must hold g.mu
// in write mode.
func (g *Generator) evictScopedCacheLocked() {
	for len(g.scopedCache) > scopedCacheMaxEntries {
		var oldestKey string
		var oldestTime time.Time
		first := true
		for k, v := range g.scopedCache {
			if first || v.lastAccess.Before(oldestTime) {
				oldestKey = k
				oldestTime = v.lastAccess
				first = false
			}
		}
		delete(g.scopedCache, oldestKey)
	}
}

// refresh fetches the current estate snapshot, checks for significant
// changes, and generates a new LLM summary when warranted. When force
// is true the significant-change check is skipped and a new summary is
// always generated.
func (g *Generator) refresh(force bool) {
	snapshot, err := g.datastore.GetEstateSnapshot(g.ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: overview: failed to get estate snapshot: %v\n", err)
		return
	}

	g.mu.RLock()
	oldSnapshot := g.lastSnapshot
	g.mu.RUnlock()

	if !force && !g.hasSignificantChange(oldSnapshot, snapshot) {
		return
	}

	summary, err := g.generateSummary(g.ctx, snapshot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: overview: failed to generate summary: %v\n", err)
		return
	}

	now := time.Now().UTC()
	overview := &Overview{
		Summary:     summary,
		GeneratedAt: now,
		StaleAt:     now.Add(staleDuration),
		Snapshot:    snapshot,
	}

	// Detect restarts: if the new snapshot has restart events newer
	// than the previous snapshot, flush all caches and notify.
	restartDetected := oldSnapshot != nil && containsRestart(snapshot, oldSnapshot.Timestamp)
	if restartDetected {
		overview.RestartDetected = true
	}

	g.mu.Lock()
	g.current = overview
	g.lastSnapshot = snapshot
	if restartDetected {
		g.scopedCache = make(map[string]*scopedEntry)
		if g.onRestart != nil {
			g.onRestart()
		}
	}
	g.mu.Unlock()

	if g.hub != nil {
		g.hub.Broadcast(overview, "")
	}

	if restartDetected {
		fmt.Fprintf(os.Stderr, "Overview: restart detected, flushed all caches\n")
	}
	fmt.Fprintf(os.Stderr, "Overview: generated new summary at %s\n", now.Format(time.RFC3339))
}

// hasSignificantChange compares two snapshots and returns true when the
// estate state has changed enough to warrant regenerating the overview.
func (g *Generator) hasSignificantChange(old, current *database.EstateSnapshot) bool {
	// First snapshot is always significant.
	if old == nil {
		return true
	}

	// Server count changes (online/offline/warning).
	if old.ServerOnline != current.ServerOnline ||
		old.ServerOffline != current.ServerOffline ||
		old.ServerWarning != current.ServerWarning ||
		old.ServerTotal != current.ServerTotal {
		return true
	}

	// Alert total changed.
	if old.AlertTotal != current.AlertTotal {
		return true
	}

	// Alert severity distribution changed.
	if old.AlertCritical != current.AlertCritical ||
		old.AlertWarning != current.AlertWarning ||
		old.AlertInfo != current.AlertInfo {
		return true
	}

	// Active blackout count changed.
	if len(old.ActiveBlackouts) != len(current.ActiveBlackouts) {
		return true
	}

	// A restart event newer than the previous snapshot triggers
	// regeneration even when server/alert counts remain unchanged.
	if containsRestart(current, old.Timestamp) {
		return true
	}

	return false
}

// generateSummary builds a prompt from the snapshot data and sends it to
// the configured LLM provider. It returns the text summary or an error.
func (g *Generator) generateSummary(ctx context.Context, snapshot *database.EstateSnapshot) (string, error) {
	system, data := buildPrompt(snapshot)
	return g.generateSummaryFromPrompt(ctx, system, data)
}

// generateSummaryFromPrompt sends the given system instruction and user
// data to the configured LLM provider and returns the text response.
// Separating the instruction from the data ensures that smaller models
// respect the formatting rules delivered via the system prompt.
func (g *Generator) generateSummaryFromPrompt(ctx context.Context, system, data string) (string, error) {
	client := g.createLLMClient()
	if client == nil {
		return "", fmt.Errorf("no LLM provider configured")
	}

	messages := []chat.Message{
		{
			Role:    "user",
			Content: data,
		},
	}

	resp, err := client.Chat(ctx, messages, nil, system)
	if err != nil {
		return "", fmt.Errorf("LLM chat failed: %w", err)
	}

	return extractTextFromResponse(resp), nil
}

// createLLMClient builds the appropriate chat client based on the
// configured LLM provider. Returns nil when no provider is configured.
func (g *Generator) createLLMClient() chat.LLMClient {
	switch g.llmConfig.Provider {
	case "anthropic":
		return chat.NewAnthropicClient(
			g.llmConfig.AnthropicAPIKey,
			g.llmConfig.Model,
			llmMaxTokens,
			llmTemperature,
			false,
			g.llmConfig.AnthropicBaseURL,
			g.llmConfig.UseCompactDescriptions,
		)
	case "openai":
		return chat.NewOpenAIClient(
			g.llmConfig.OpenAIAPIKey,
			g.llmConfig.Model,
			llmMaxTokens,
			llmTemperature,
			false,
			g.llmConfig.OpenAIBaseURL,
			g.llmConfig.UseCompactDescriptions,
		)
	case "gemini":
		return chat.NewGeminiClient(
			g.llmConfig.GeminiAPIKey,
			g.llmConfig.Model,
			llmMaxTokens,
			llmTemperature,
			false,
			g.llmConfig.GeminiBaseURL,
			g.llmConfig.UseCompactDescriptions,
		)
	case "ollama":
		return chat.NewOllamaClient(
			g.llmConfig.OllamaURL,
			g.llmConfig.Model,
			false,
			g.llmConfig.UseCompactDescriptions,
		)
	default:
		return nil
	}
}

// extractTextFromResponse walks the LLM response content blocks and
// concatenates all text content into a single string.
func extractTextFromResponse(resp chat.LLMResponse) string {
	var parts []string
	for _, item := range resp.Content {
		switch v := item.(type) {
		case chat.TextContent:
			parts = append(parts, v.Text)
		case map[string]interface{}:
			if t, ok := v["type"].(string); ok && t == "text" {
				if text, ok := v["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
	}
	return strings.Join(parts, "")
}

// buildPrompt constructs the system instruction and user data sent to
// the LLM. The system string contains behavioral instructions; the
// data string contains the estate snapshot for the user message.
func buildPrompt(s *database.EstateSnapshot) (system, data string) {
	system = `You are a PostgreSQL DBA assistant providing a brief estate overview.
Summarize the following estate state in 3-5 sentences. Be concise and
focus on what a DBA needs to know right now. Use plain language.
Focus on: server health, active alerts requiring attention, and any
ongoing or upcoming blackouts. If everything is healthy, say so briefly.
Do not use markdown formatting. Do not use bullet points or lists.
Do not introduce yourself or use any greeting. Do not use casual or
conversational language. Write in a professional, impersonal, third-person
technical style. State facts directly without preamble.`

	var b strings.Builder
	b.WriteString("Estate State:\n")
	writeSnapshotData(&b, s)
	data = b.String()
	return system, data
}

// buildScopedPrompt constructs a system instruction and user data
// tailored to a specific scope (server, cluster, group, or connections)
// rather than the entire estate.
func buildScopedPrompt(s *database.EstateSnapshot, scopeType, scopeName string) (system, data string) {
	scopeLabel := scopeType
	switch scopeType {
	case "server":
		scopeLabel = "server"
	case "cluster":
		scopeLabel = "cluster"
	case "group":
		scopeLabel = "group"
	case "connections":
		scopeLabel = "selection"
	}

	titleLabel := strings.ToUpper(scopeLabel[:1]) + scopeLabel[1:]

	system = fmt.Sprintf(`You are a PostgreSQL DBA assistant providing a brief status overview for a specific %s.
Summarize the following state of %s "%s" in 3-5 sentences. Be concise and
focus on what a DBA needs to know right now. Use plain language.
Focus on: server health, active alerts requiring attention, and any
ongoing or upcoming blackouts. If everything is healthy, say so briefly.
Do not use markdown formatting. Do not use bullet points or lists.
Do not introduce yourself or use any greeting. Do not use casual or
conversational language. Write in a professional, impersonal, third-person
technical style. State facts directly without preamble.`, scopeLabel, scopeLabel, scopeName)

	var b strings.Builder
	fmt.Fprintf(&b, "%s \"%s\" State:\n", titleLabel, scopeName)
	writeSnapshotData(&b, s)
	data = b.String()
	return system, data
}

// writeSnapshotData appends the snapshot data lines to the builder.
// This is shared between estate-wide and scoped prompt builders.
func writeSnapshotData(b *strings.Builder, s *database.EstateSnapshot) {
	// Server summary
	fmt.Fprintf(b, "- Servers: %d total (%d online, %d offline, %d with warnings)\n",
		s.ServerTotal, s.ServerOnline, s.ServerOffline, s.ServerWarning)

	// List offline servers by name
	for _, srv := range s.Servers {
		if srv.Status == "offline" {
			fmt.Fprintf(b, "- Offline server: %s\n", srv.Name)
		}
	}

	// Alert summary
	fmt.Fprintf(b, "- Active Alerts: %d total (%d critical, %d warning, %d info)\n",
		s.AlertTotal, s.AlertCritical, s.AlertWarning, s.AlertInfo)

	// List critical alerts with server names
	for _, alert := range s.TopAlerts {
		if alert.Severity == "critical" {
			fmt.Fprintf(b, "- Critical alert on %s: %s\n", alert.ServerName, alert.Title)
		}
	}

	// Active blackouts
	fmt.Fprintf(b, "- Active Blackouts: %d\n", len(s.ActiveBlackouts))
	for _, bo := range s.ActiveBlackouts {
		fmt.Fprintf(b, "- Active blackout (%s): %s\n", bo.Scope, bo.Reason)
	}

	// Upcoming blackouts
	fmt.Fprintf(b, "- Upcoming Blackouts (next 24h): %d\n", len(s.UpcomingBlackouts))
	for _, bo := range s.UpcomingBlackouts {
		fmt.Fprintf(b, "- Upcoming blackout (%s): %s\n", bo.Scope, bo.Reason)
	}

	// Recent events
	fmt.Fprintf(b, "- Recent Events (last 24h): %d\n", len(s.RecentEvents))
	for _, ev := range s.RecentEvents {
		fmt.Fprintf(b, "- %s on %s: %s\n", ev.EventType, ev.ServerName, ev.Title)
	}
}
