/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/pgedge/ai-workbench/pkg/embedding"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
	"github.com/pgedge/ai-workbench/server/internal/memory"
)

// RecallMemoriesTool creates the recall_memories tool for searching stored memories
// using semantic similarity and returning pinned memories.
func RecallMemoriesTool(memoryStore *memory.Store, cfg *config.Config) Tool {
	return Tool{
		Definition: mcp.Tool{
			Name: "recall_memories",
			Description: `Search Ellie's stored memories using semantic similarity. ` +
				`This tool retrieves previously saved memories that are most relevant ` +
				`to the given query by comparing embedding vectors. Pinned memories ` +
				`are always included in the results regardless of the query. ` +
				`Use the category filter to narrow results to a specific type ` +
				`(e.g., "preference", "fact", "instruction", "context"). ` +
				`Use the scope filter to limit results to "user" (personal) ` +
				`or "system" (shared) memories.`,
			CompactDescription: `Search stored memories by semantic similarity; pinned memories are always included.`,
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "The search query text for semantic matching",
					},
					"category": map[string]interface{}{
						"type":        "string",
						"description": "Filter by category (e.g., \"preference\", \"fact\", \"instruction\", \"context\")",
					},
					"scope": map[string]interface{}{
						"type":        "string",
						"description": "Filter by scope: \"user\" for personal memories or \"system\" for shared memories",
					},
					"limit": map[string]interface{}{
						"type":        "number",
						"description": "Maximum number of results to return (default 10)",
					},
				},
				Required: []string{"query"},
			},
		},
		Handler: func(args map[string]interface{}) (mcp.ToolResponse, error) {
			// Extract and validate the query parameter
			query, ok := args["query"].(string)
			if !ok || query == "" {
				return mcp.NewToolError("Missing or invalid 'query' parameter")
			}
			query = strings.TrimSpace(query)
			if query == "" {
				return mcp.NewToolError("'query' parameter cannot be empty or whitespace-only")
			}

			// Extract optional parameters
			var category string
			if c, ok := args["category"].(string); ok {
				category = strings.TrimSpace(c)
			}

			var scope string
			if s, ok := args["scope"].(string); ok {
				scope = strings.TrimSpace(s)
			}

			limit := 10
			if l, ok := args["limit"].(float64); ok && l > 0 {
				limit = int(l)
			}

			// Extract context from args (injected by registry.Execute)
			ctx, ok := args["__context"].(context.Context)
			if !ok {
				ctx = context.Background()
			}

			// Get username from the authentication context
			username := auth.GetUsernameFromContext(ctx)
			if username == "" {
				return mcp.NewToolError("Unable to determine the current user from the session context")
			}

			// Guard against nil memory store
			if memoryStore == nil {
				return mcp.NewToolError("Memory store is not configured")
			}

			// Generate an embedding for the query text
			var queryEmbedding []float32
			if cfg.Embedding.Enabled {
				embCfg := embedding.Config{
					Provider:      cfg.Embedding.Provider,
					Model:         cfg.Embedding.Model,
					VoyageAPIKey:  cfg.Embedding.VoyageAPIKey,
					VoyageBaseURL: cfg.Embedding.VoyageBaseURL,
					OpenAIAPIKey:  cfg.Embedding.OpenAIAPIKey,
					OpenAIBaseURL: cfg.Embedding.OpenAIBaseURL,
					OllamaURL:     cfg.Embedding.OllamaURL,
				}

				provider, err := embedding.NewProvider(embCfg)
				if err == nil {
					vector, err := provider.Embed(ctx, query)
					if err == nil && len(vector) > 0 {
						queryEmbedding = float64sToFloat32s(vector)
					}
					// If embedding generation fails, fall through to text search
				}
			}
			// When queryEmbedding is nil, Search falls back to ILIKE text matching

			// Search for memories matching the query
			searchResults, err := memoryStore.Search(ctx, username, query, category, scope, limit, queryEmbedding)
			if err != nil {
				return mcp.NewToolError(fmt.Sprintf("Failed to search memories: %v", err))
			}

			// Retrieve pinned memories (always included)
			pinnedMemories, err := memoryStore.GetPinned(ctx, username)
			if err != nil {
				return mcp.NewToolError(fmt.Sprintf("Failed to retrieve pinned memories: %v", err))
			}

			// Merge results: pinned first, then search results (deduplicated)
			pinnedIDs := make(map[int64]bool, len(pinnedMemories))
			var final []memory.Memory
			for i := range pinnedMemories {
				pinnedIDs[pinnedMemories[i].ID] = true
				final = append(final, pinnedMemories[i])
			}
			for i := range searchResults {
				if !pinnedIDs[searchResults[i].ID] {
					final = append(final, searchResults[i])
				}
			}

			// Format output for LLM consumption
			var sb strings.Builder
			sb.WriteString("Memory Recall Results\n")
			sb.WriteString(strings.Repeat("=", 50))
			sb.WriteString("\n")
			sb.WriteString(fmt.Sprintf("Query: %s\n", query))
			if category != "" {
				sb.WriteString(fmt.Sprintf("Category Filter: %s\n", category))
			}
			if scope != "" {
				sb.WriteString(fmt.Sprintf("Scope Filter: %s\n", scope))
			}
			if queryEmbedding != nil {
				sb.WriteString("Search Mode: semantic similarity\n")
			} else {
				sb.WriteString("Search Mode: text matching\n")
			}
			sb.WriteString(fmt.Sprintf("Total Results: %d\n", len(final)))
			sb.WriteString("\n")

			if len(final) == 0 {
				sb.WriteString("No memories found matching the query.\n")
				return mcp.NewToolSuccess(sb.String())
			}

			for i := range final {
				sb.WriteString(fmt.Sprintf("--- Memory #%d ---\n", i+1))
				sb.WriteString(fmt.Sprintf("  ID:       %d\n", final[i].ID))
				sb.WriteString(fmt.Sprintf("  Scope:    %s\n", final[i].Scope))
				sb.WriteString(fmt.Sprintf("  Category: %s\n", final[i].Category))
				if final[i].Pinned {
					sb.WriteString("  Pinned:   yes\n")
				} else {
					sb.WriteString("  Pinned:   no\n")
				}
				sb.WriteString(fmt.Sprintf("  Created:  %s\n", final[i].CreatedAt.Format("2006-01-02 15:04:05 UTC")))
				sb.WriteString(fmt.Sprintf("  Content:\n    %s\n", final[i].Content))
				sb.WriteString("\n")
			}

			return mcp.NewToolSuccess(sb.String())
		},
	}
}
