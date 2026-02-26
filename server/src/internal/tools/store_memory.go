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
	"log"
	"strings"

	"github.com/pgedge/ai-workbench/pkg/embedding"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
	"github.com/pgedge/ai-workbench/server/internal/memory"
)

// StoreMemoryTool creates the store_memory tool for persisting memories
// that Ellie can recall in future conversations.
func StoreMemoryTool(memoryStore *memory.Store, cfg *config.Config, rbacChecker *auth.RBACChecker) Tool {
	return Tool{
		Definition: mcp.Tool{
			Name: "store_memory",
			Description: `Store a persistent memory that Ellie can recall in future conversations. ` +
				`Each memory has a category for organization (e.g., "preference", "fact", "instruction", "context"), ` +
				`a scope that controls visibility ("user" for private or "system" for all users), ` +
				`and an optional pinned flag that causes the memory to be automatically included ` +
				`in every conversation.`,
			CompactDescription: `Store a persistent memory with category, scope, and optional pinned flag for future recall.`,
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"content": map[string]interface{}{
						"type":        "string",
						"description": "The memory text to store",
					},
					"category": map[string]interface{}{
						"type":        "string",
						"description": `Category for organizing memories (e.g., "preference", "fact", "instruction", "context")`,
					},
					"scope": map[string]interface{}{
						"type":        "string",
						"description": `Visibility scope: "user" (private to the current user) or "system" (visible to all users). Defaults to "user".`,
					},
					"pinned": map[string]interface{}{
						"type":        "boolean",
						"description": "If true, this memory is automatically included in every conversation. Defaults to false.",
					},
				},
				Required: []string{"content", "category"},
			},
		},
		Handler: func(args map[string]interface{}) (mcp.ToolResponse, error) {
			// Extract and validate the content parameter
			content, ok := args["content"].(string)
			if !ok || content == "" {
				return mcp.NewToolError("Missing or invalid 'content' parameter")
			}
			content = strings.TrimSpace(content)
			if content == "" {
				return mcp.NewToolError("'content' parameter cannot be empty or whitespace-only")
			}

			// Extract and validate the category parameter
			category, ok := args["category"].(string)
			if !ok || category == "" {
				return mcp.NewToolError("Missing or invalid 'category' parameter")
			}
			category = strings.TrimSpace(category)
			if category == "" {
				return mcp.NewToolError("'category' parameter cannot be empty or whitespace-only")
			}

			// Extract optional scope parameter with default
			scope := "user"
			if s, ok := args["scope"].(string); ok && s != "" {
				scope = strings.TrimSpace(s)
			}
			if scope != "user" && scope != "system" {
				return mcp.NewToolError("'scope' must be either \"user\" or \"system\"")
			}

			// Extract optional pinned parameter with default
			pinned := false
			if p, ok := args["pinned"].(bool); ok {
				pinned = p
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

			// Check RBAC permission for system-scoped memories
			if scope == "system" {
				if rbacChecker == nil || !rbacChecker.HasAdminPermission(ctx, auth.PermStoreSystemMemory) {
					return mcp.NewToolError("Permission denied: you do not have permission to store system-scoped memories")
				}
			}

			// Guard against nil memory store
			if memoryStore == nil {
				return mcp.NewToolError("Memory store is not configured")
			}

			// Generate an embedding for the content text
			var embeddingVec []float32
			var modelName string
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
				if err != nil {
					log.Printf("WARNING: store_memory: failed to initialize embedding provider: %v", err)
				} else {
					vector, err := provider.Embed(ctx, content)
					if err != nil {
						log.Printf("WARNING: store_memory: failed to generate embedding: %v", err)
					} else if len(vector) > 0 {
						embeddingVec = float64sToFloat32s(vector)
						modelName = provider.ModelName()
					}
				}
			}

			// Store the memory
			mem, err := memoryStore.Store(ctx, username, scope, category, content, pinned, embeddingVec, modelName)
			if err != nil {
				return mcp.NewToolError(fmt.Sprintf("Failed to store memory: %v", err))
			}

			// Format success response
			var sb strings.Builder
			sb.WriteString("Memory Stored Successfully\n")
			sb.WriteString(strings.Repeat("=", 50))
			sb.WriteString("\n\n")
			sb.WriteString(fmt.Sprintf("ID:       %d\n", mem.ID))
			sb.WriteString(fmt.Sprintf("Scope:    %s\n", mem.Scope))
			sb.WriteString(fmt.Sprintf("Category: %s\n", mem.Category))
			if mem.Pinned {
				sb.WriteString("Pinned:   yes\n")
			} else {
				sb.WriteString("Pinned:   no\n")
			}
			if embeddingVec != nil {
				sb.WriteString(fmt.Sprintf("Embedding: generated (%d dimensions, model: %s)\n", len(embeddingVec), modelName))
			} else {
				sb.WriteString("Embedding: none (embedding generation unavailable)\n")
			}
			sb.WriteString(fmt.Sprintf("Created:  %s\n", mem.CreatedAt.Format("2006-01-02 15:04:05 UTC")))
			sb.WriteString(fmt.Sprintf("\nContent:\n  %s\n", mem.Content))

			return mcp.NewToolSuccess(sb.String())
		},
	}
}

// float64sToFloat32s converts a slice of float64 values to float32.
func float64sToFloat32s(src []float64) []float32 {
	dst := make([]float32, len(src))
	for i, v := range src {
		dst[i] = float32(v)
	}
	return dst
}
