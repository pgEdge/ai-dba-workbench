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
	Summary     string                   `json:"summary"`
	GeneratedAt time.Time                `json:"generated_at"`
	StaleAt     time.Time                `json:"stale_at"`
	Snapshot    *database.EstateSnapshot `json:"snapshot"`
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
	ctx          context.Context
	cancel       context.CancelFunc
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
		g.refresh()

		ticker := time.NewTicker(tickInterval)
		defer ticker.Stop()
		for {
			select {
			case <-g.ctx.Done():
				return
			case <-ticker.C:
				g.refresh()
			}
		}
	}()
}

// Stop cancels the background goroutine.
func (g *Generator) Stop() {
	if g.cancel != nil {
		g.cancel()
	}
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
// otherwise a new summary is generated on demand.
func (g *Generator) GetScopedSummary(scopeType string, scopeID int) (*Overview, error) {
	key := fmt.Sprintf("%s:%d", scopeType, scopeID)

	// Check the cache first.
	g.mu.RLock()
	if entry, ok := g.scopedCache[key]; ok {
		if time.Now().UTC().Before(entry.overview.StaleAt) {
			entry.lastAccess = time.Now().UTC()
			g.mu.RUnlock()
			return entry.overview, nil
		}
	}
	g.mu.RUnlock()

	// Generate a fresh scoped overview.
	snapshot, scopeName, err := g.fetchScopedSnapshot(scopeType, scopeID)
	if err != nil {
		return nil, err
	}

	prompt := buildScopedPrompt(snapshot, scopeType, scopeName)
	summary, err := g.generateSummaryFromPrompt(g.ctx, prompt)
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

	fmt.Fprintf(os.Stderr,
		"Overview: generated scoped summary for %s at %s\n",
		key, now.Format(time.RFC3339))

	return overview, nil
}

// GetConnectionsSummary returns a cached or freshly generated overview
// for an explicit list of connection IDs. The scopeName is used in the
// LLM prompt to give context (e.g. "Spock Cluster: my-cluster"). The
// cache key is derived from the sorted connection IDs so that the same
// set of connections always hits the same cache entry regardless of the
// order in which the IDs were supplied.
func (g *Generator) GetConnectionsSummary(connectionIDs []int, scopeName string) (*Overview, error) {
	// Build a deterministic cache key from sorted connection IDs.
	sorted := make([]int, len(connectionIDs))
	copy(sorted, connectionIDs)
	sortInts(sorted)

	parts := make([]string, len(sorted))
	for i, id := range sorted {
		parts[i] = fmt.Sprintf("%d", id)
	}
	key := "connections:" + strings.Join(parts, ",")

	// Check the cache first.
	g.mu.RLock()
	if entry, ok := g.scopedCache[key]; ok {
		if time.Now().UTC().Before(entry.overview.StaleAt) {
			entry.lastAccess = time.Now().UTC()
			g.mu.RUnlock()
			return entry.overview, nil
		}
	}
	g.mu.RUnlock()

	// Generate a fresh snapshot from the explicit connection IDs.
	snapshot := g.datastore.GetConnectionsSnapshot(g.ctx, connectionIDs)

	prompt := buildScopedPrompt(snapshot, "connections", scopeName)
	summary, err := g.generateSummaryFromPrompt(g.ctx, prompt)
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

	fmt.Fprintf(os.Stderr,
		"Overview: generated connections summary for %s at %s\n",
		key, now.Format(time.RFC3339))

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
// changes, and generates a new LLM summary when warranted.
func (g *Generator) refresh() {
	snapshot, err := g.datastore.GetEstateSnapshot(g.ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: overview: failed to get estate snapshot: %v\n", err)
		return
	}

	g.mu.RLock()
	oldSnapshot := g.lastSnapshot
	g.mu.RUnlock()

	if !g.hasSignificantChange(oldSnapshot, snapshot) {
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

	g.mu.Lock()
	g.current = overview
	g.lastSnapshot = snapshot
	g.mu.Unlock()

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

	return false
}

// generateSummary builds a prompt from the snapshot data and sends it to
// the configured LLM provider. It returns the text summary or an error.
func (g *Generator) generateSummary(ctx context.Context, snapshot *database.EstateSnapshot) (string, error) {
	prompt := buildPrompt(snapshot)
	return g.generateSummaryFromPrompt(ctx, prompt)
}

// generateSummaryFromPrompt sends the given prompt to the configured
// LLM provider and returns the text response.
func (g *Generator) generateSummaryFromPrompt(ctx context.Context, prompt string) (string, error) {
	client := g.createLLMClient()
	if client == nil {
		return "", fmt.Errorf("no LLM provider configured")
	}

	messages := []chat.Message{
		{
			Role:    "user",
			Content: prompt,
		},
	}

	resp, err := client.Chat(ctx, messages, nil)
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
		)
	case "openai":
		return chat.NewOpenAIClient(
			g.llmConfig.OpenAIAPIKey,
			g.llmConfig.Model,
			llmMaxTokens,
			llmTemperature,
			false,
			g.llmConfig.OpenAIBaseURL,
		)
	case "gemini":
		return chat.NewGeminiClient(
			g.llmConfig.GeminiAPIKey,
			g.llmConfig.Model,
			llmMaxTokens,
			llmTemperature,
			false,
			g.llmConfig.GeminiBaseURL,
		)
	case "ollama":
		return chat.NewOllamaClient(
			g.llmConfig.OllamaURL,
			g.llmConfig.Model,
			false,
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

// buildPrompt constructs the system + estate-state prompt sent to the LLM.
func buildPrompt(s *database.EstateSnapshot) string {
	var b strings.Builder

	b.WriteString(`You are a PostgreSQL DBA assistant providing a brief estate overview.
Summarize the following estate state in 3-5 sentences. Be concise and
focus on what a DBA needs to know right now. Use plain language.
Focus on: server health, active alerts requiring attention, and any
ongoing or upcoming blackouts. If everything is healthy, say so briefly.
Do not use markdown formatting. Do not use bullet points or lists.

Estate State:
`)

	writeSnapshotData(&b, s)
	return b.String()
}

// buildScopedPrompt constructs a prompt tailored to a specific scope
// (server, cluster, group, or connections) rather than the entire estate.
func buildScopedPrompt(s *database.EstateSnapshot, scopeType, scopeName string) string {
	var b strings.Builder

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

	fmt.Fprintf(&b, `You are a PostgreSQL DBA assistant providing a brief status overview for a specific %s.
Summarize the following state of %s "%s" in 3-5 sentences. Be concise and
focus on what a DBA needs to know right now. Use plain language.
Focus on: server health, active alerts requiring attention, and any
ongoing or upcoming blackouts. If everything is healthy, say so briefly.
Do not use markdown formatting. Do not use bullet points or lists.

%s "%s" State:
`, scopeLabel, scopeLabel, scopeName, titleLabel, scopeName)

	writeSnapshotData(&b, s)
	return b.String()
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
