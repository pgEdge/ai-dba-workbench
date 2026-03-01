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
	"errors"
	"fmt"
	"math"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
	"github.com/pgedge/ai-workbench/server/internal/memory"
)

// DeleteMemoryTool creates the delete_memory tool for removing a stored memory by ID
func DeleteMemoryTool(memoryStore *memory.Store) Tool {
	return Tool{
		Definition: mcp.Tool{
			Name:               "delete_memory",
			Description:        "Delete a stored memory by its ID. The user can only delete their own memories; attempts to delete another user's memory will return a not-found error.",
			CompactDescription: "Delete a stored memory by ID. Only the owning user can delete a memory.",
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]any{
					"id": map[string]any{
						"type":        "number",
						"description": "The ID of the memory to delete",
					},
				},
				Required: []string{"id"},
			},
		},
		Handler: func(args map[string]any) (mcp.ToolResponse, error) {
			// Extract and validate the id parameter
			idFloat, ok := args["id"].(float64)
			if !ok {
				return mcp.NewToolError("Missing or invalid 'id' parameter")
			}
			if math.IsNaN(idFloat) || math.IsInf(idFloat, 0) || idFloat != math.Trunc(idFloat) || idFloat < 1 {
				return mcp.NewToolError("'id' must be a positive integer")
			}
			id := int64(idFloat)

			// Extract context from args (injected by registry.Execute)
			ctx, ok := args["__context"].(context.Context)
			if !ok {
				ctx = context.Background()
			}

			// Get username from context
			username := auth.GetUsernameFromContext(ctx)
			if username == "" {
				return mcp.NewToolError("Unable to determine the current user")
			}

			// Guard against nil memory store
			if memoryStore == nil {
				return mcp.NewToolError("Memory store is not configured")
			}

			// Delete the memory
			err := memoryStore.Delete(ctx, id, username)
			if err != nil {
				if errors.Is(err, memory.ErrNotFound) {
					return mcp.NewToolError(fmt.Sprintf("Memory with ID %d was not found or does not belong to you", id))
				}
				return mcp.NewToolError("Failed to delete memory")
			}

			return mcp.NewToolSuccess(fmt.Sprintf("Memory with ID %d has been deleted", id))
		},
	}
}
