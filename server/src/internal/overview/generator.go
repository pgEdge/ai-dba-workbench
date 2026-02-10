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
)

// Overview holds the current AI-generated estate summary and metadata.
type Overview struct {
	Summary     string                   `json:"summary"`
	GeneratedAt time.Time                `json:"generated_at"`
	StaleAt     time.Time                `json:"stale_at"`
	Snapshot    *database.EstateSnapshot `json:"snapshot"`
}

// Generator periodically gathers estate state from the datastore, detects
// meaningful changes, and calls the LLM to produce a natural-language
// summary that is served to clients via the REST API.
type Generator struct {
	datastore    *database.Datastore
	llmConfig    *llmproxy.Config
	mu           sync.RWMutex
	current      *Overview
	lastSnapshot *database.EstateSnapshot
	ctx          context.Context
	cancel       context.CancelFunc
}

// NewGenerator creates a new overview generator. It does not start any
// background goroutines; call Start to begin periodic generation.
func NewGenerator(datastore *database.Datastore, llmConfig *llmproxy.Config) *Generator {
	return &Generator{
		datastore: datastore,
		llmConfig: llmConfig,
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

// GetOverview returns the current overview in a thread-safe manner.
// It returns nil if no overview has been generated yet.
func (g *Generator) GetOverview() *Overview {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.current
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
func (g *Generator) hasSignificantChange(old, new *database.EstateSnapshot) bool {
	// First snapshot is always significant.
	if old == nil {
		return true
	}

	// Server count changes (online/offline/warning).
	if old.ServerOnline != new.ServerOnline ||
		old.ServerOffline != new.ServerOffline ||
		old.ServerWarning != new.ServerWarning ||
		old.ServerTotal != new.ServerTotal {
		return true
	}

	// Alert total changed.
	if old.AlertTotal != new.AlertTotal {
		return true
	}

	// Alert severity distribution changed.
	if old.AlertCritical != new.AlertCritical ||
		old.AlertWarning != new.AlertWarning ||
		old.AlertInfo != new.AlertInfo {
		return true
	}

	// Active blackout count changed.
	if len(old.ActiveBlackouts) != len(new.ActiveBlackouts) {
		return true
	}

	return false
}

// generateSummary builds a prompt from the snapshot data and sends it to
// the configured LLM provider. It returns the text summary or an error.
func (g *Generator) generateSummary(ctx context.Context, snapshot *database.EstateSnapshot) (string, error) {
	prompt := buildPrompt(snapshot)

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
		)
	case "openai":
		return chat.NewOpenAIClient(
			g.llmConfig.OpenAIAPIKey,
			g.llmConfig.Model,
			llmMaxTokens,
			llmTemperature,
			false,
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

	// Server summary
	fmt.Fprintf(&b, "- Servers: %d total (%d online, %d offline, %d with warnings)\n",
		s.ServerTotal, s.ServerOnline, s.ServerOffline, s.ServerWarning)

	// List offline servers by name
	for _, srv := range s.Servers {
		if srv.Status == "offline" {
			fmt.Fprintf(&b, "- Offline server: %s\n", srv.Name)
		}
	}

	// Alert summary
	fmt.Fprintf(&b, "- Active Alerts: %d total (%d critical, %d warning, %d info)\n",
		s.AlertTotal, s.AlertCritical, s.AlertWarning, s.AlertInfo)

	// List critical alerts with server names
	for _, alert := range s.TopAlerts {
		if alert.Severity == "critical" {
			fmt.Fprintf(&b, "- Critical alert on %s: %s\n", alert.ServerName, alert.Title)
		}
	}

	// Active blackouts
	fmt.Fprintf(&b, "- Active Blackouts: %d\n", len(s.ActiveBlackouts))
	for _, bo := range s.ActiveBlackouts {
		fmt.Fprintf(&b, "- Active blackout (%s): %s\n", bo.Scope, bo.Reason)
	}

	// Upcoming blackouts
	fmt.Fprintf(&b, "- Upcoming Blackouts (next 24h): %d\n", len(s.UpcomingBlackouts))
	for _, bo := range s.UpcomingBlackouts {
		fmt.Fprintf(&b, "- Upcoming blackout (%s): %s\n", bo.Scope, bo.Reason)
	}

	// Recent events
	fmt.Fprintf(&b, "- Recent Events (last 24h): %d\n", len(s.RecentEvents))
	for _, ev := range s.RecentEvents {
		fmt.Fprintf(&b, "- %s on %s: %s\n", ev.EventType, ev.ServerName, ev.Title)
	}

	return b.String()
}
