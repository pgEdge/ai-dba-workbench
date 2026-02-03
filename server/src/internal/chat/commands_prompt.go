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
	"strings"
)

// handlePromptCommand handles /prompt commands
func (c *Client) handlePromptCommand(ctx context.Context, args []string) bool {
	if len(args) < 1 {
		c.ui.PrintError("Usage: /prompt <name> [arg=value ...]")
		c.ui.PrintSystemMessage("Use 'prompts' command to list available prompts")
		return true
	}

	promptName := args[0]

	// Parse arguments in key=value format
	promptArgs := make(map[string]string)
	for _, arg := range args[1:] {
		parts := strings.SplitN(arg, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			// Quotes are already removed by parseQuotedArgs
			promptArgs[key] = value
		} else {
			c.ui.PrintError(fmt.Sprintf("Invalid argument format: %s (expected key=value)", arg))
			return true
		}
	}

	// Execute the prompt
	c.ui.PrintSystemMessage(fmt.Sprintf("Executing prompt: %s", promptName))

	result, err := c.mcp.GetPrompt(ctx, promptName, promptArgs)
	if err != nil {
		c.ui.PrintError(fmt.Sprintf("Failed to execute prompt: %v", err))
		return true
	}

	// Display the prompt description if available
	if result.Description != "" {
		c.ui.PrintSystemMessage(result.Description)
	}

	// Add prompt messages to conversation history
	// The prompt result contains messages that guide the LLM through a workflow
	for _, msg := range result.Messages {
		if msg.Role == "user" {
			// Add user message from prompt
			c.messages = append(c.messages, Message{
				Role:    "user",
				Content: msg.Content.Text,
			})
		} else if msg.Role == "assistant" {
			// Add assistant message from prompt (less common but supported)
			c.messages = append(c.messages, Message{
				Role:    "assistant",
				Content: msg.Content.Text,
			})
		}
	}

	c.ui.PrintSystemMessage("Prompt loaded. Starting workflow execution...")
	c.ui.PrintSeparator()

	// Automatically process the prompt through the LLM
	// This triggers the agentic loop with the loaded prompt instructions
	if err := c.processQuery(ctx, ""); err != nil {
		c.ui.PrintError(err.Error())
	}

	return true
}
