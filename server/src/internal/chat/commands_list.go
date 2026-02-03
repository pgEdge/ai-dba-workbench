/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package chat

import (
	"context"
	"fmt"
)

// handleListCommand handles /list commands
func (c *Client) handleListCommand(ctx context.Context, args []string) bool {
	if len(args) < 1 {
		c.ui.PrintError("Usage: /list <what>")
		c.ui.PrintSystemMessage("Available: models, databases")
		return true
	}

	what := args[0]

	switch what {
	case "models":
		return c.listModels(ctx)

	case "databases":
		return c.handleListDatabases(ctx)

	default:
		c.ui.PrintError(fmt.Sprintf("Unknown list target: %s", what))
		c.ui.PrintSystemMessage("Available: models, databases")
	}

	return true
}

// listModels lists available models from the current LLM provider
func (c *Client) listModels(ctx context.Context) bool {
	models, err := c.llm.ListModels(ctx)
	if err != nil {
		c.ui.PrintError(fmt.Sprintf("Failed to list models: %v", err))
		return true
	}

	if len(models) == 0 {
		c.ui.PrintSystemMessage("No models available")
		return true
	}

	c.ui.PrintSystemMessage(fmt.Sprintf("Available models from %s (%d):", c.config.LLM.Provider, len(models)))
	for _, model := range models {
		if model == c.config.LLM.Model {
			fmt.Printf("  * %s (current)\n", model)
		} else {
			fmt.Printf("    %s\n", model)
		}
	}

	return true
}
