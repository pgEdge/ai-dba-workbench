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

// systemMemoryDeleteDenied is the refusal, and the audited reason, when
// the caller lacks store_system_memory, in the words the REST delete
// route uses, so that both interfaces coalesce alike.
const systemMemoryDeleteDenied = "Permission denied: you do not have " +
	"permission to delete system memories"

// DeleteMemoryTool creates the delete_memory tool for removing a stored
// memory by ID. A system-scoped memory needs store_system_memory, as on
// the REST route, and a refusal is audited through record as
// memory.delete on memories/<id>; another user's memory is answered as
// a missing one, and neither is written or audited (issue #704).
func DeleteMemoryTool(memoryStore *memory.Store,
	rbacChecker *auth.RBACChecker, record DenialRecorder) Tool {

	return Tool{
		Definition: mcp.Tool{
			Name:               "delete_memory",
			Description:        "Delete a stored memory by its ID. The user can delete their own memories; deleting a system memory requires the store_system_memory permission, and attempts to delete another user's memory will return a not-found error.",
			CompactDescription: "Delete a stored memory by ID. Only the owning user can delete a memory; system memories need store_system_memory.",
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

			// Fetch the memory to check its scope and owner.
			mem, err := memoryStore.GetByID(ctx, id)
			if err != nil && !errors.Is(err, memory.ErrNotFound) {
				return mcp.NewToolError("Failed to delete memory")
			}

			switch {
			case mem == nil:
				err = memory.ErrNotFound
			case mem.Scope == "system":
				if !rbacChecker.HasAdminPermission(ctx, auth.PermStoreSystemMemory) {
					return record.refuse(ctx, deniedActionMemoryDelete,
						memoryDenialTarget(id), systemMemoryDeleteDenied)
				}
				err = memoryStore.DeleteByID(ctx, id)
			case mem.Username != username:
				err = memory.ErrNotFound
			default:
				err = memoryStore.Delete(ctx, id, username)
			}
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
