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
	"sync"
)

// processQuery handles sending a query to the LLM and processing the response,
// including agentic tool calling loops.
func (c *Client) processQuery(ctx context.Context, query string) error {
	const maxAgenticLoops = 50 // Maximum iterations to prevent infinite loops

	// Add user message to conversation history (skip if empty, used for prompts)
	if query != "" {
		c.messages = append(c.messages, Message{
			Role:    "user",
			Content: query,
		})
	}

	// Create a cancellable context for this request
	// This allows the user to cancel with Escape key
	reqCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// WaitGroup tracks background goroutines (thinking animation, escape listener)
	// so we can wait for them to finish and restore terminal state reliably.
	var wg sync.WaitGroup

	// Start thinking animation
	thinkingDone := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		c.ui.ShowThinking(reqCtx, thinkingDone)
	}()

	// Start listening for Escape key to cancel the request
	wg.Add(1)
	go func() {
		defer wg.Done()
		ListenForEscape(ctx, thinkingDone, cancel)
	}()

	// waitForGoroutines closes thinkingDone and waits for all background
	// goroutines to finish, ensuring terminal state is properly restored.
	waitForGoroutines := func() {
		close(thinkingDone)
		wg.Wait()
	}

	// Agentic loop (allow up to maxAgenticLoops iterations for complex queries)
	for iteration := 0; iteration < maxAgenticLoops; iteration++ {
		// Compact message history to prevent token overflow
		compactedMessages := c.compactMessages(c.messages)

		// Get response from LLM with compacted history
		response, err := c.llm.Chat(reqCtx, compactedMessages, c.tools)
		if err != nil {
			waitForGoroutines()
			// Check if this was a user cancellation (Escape key)
			if reqCtx.Err() == context.Canceled && ctx.Err() == nil {
				// User canceled with Escape - keep the query in history
				// but don't save the Escape keypress
				c.ui.PrintCanceled()
				return nil // Return without error to continue the chat loop
			}
			return fmt.Errorf("LLM error: %w", err)
		}

		// Check if LLM wants to use tools
		if response.StopReason == "tool_use" {
			// Extract tool uses from the response
			var toolUses []ToolUse

			for _, item := range response.Content {
				if v, ok := item.(ToolUse); ok {
					toolUses = append(toolUses, v)
				}
			}

			// Add assistant's message to history
			c.messages = append(c.messages, Message{
				Role:    "assistant",
				Content: response.Content,
			})

			// Execute all tool calls
			toolResults := []ToolResult{}
			for _, toolUse := range toolUses {
				// Wait for current goroutines before printing tool info
				waitForGoroutines()
				c.ui.PrintToolExecution(toolUse.Name, toolUse.Input)

				// Start new thinking animation and escape listener
				thinkingDone = make(chan struct{})
				wg.Add(1)
				go func() {
					defer wg.Done()
					c.ui.ShowThinking(reqCtx, thinkingDone)
				}()
				wg.Add(1)
				go func() {
					defer wg.Done()
					ListenForEscape(ctx, thinkingDone, cancel)
				}()

				// Update waitForGoroutines to use the new thinkingDone
				waitForGoroutines = func() {
					close(thinkingDone)
					wg.Wait()
				}

				result, err := c.mcp.CallTool(reqCtx, toolUse.Name, toolUse.Input)
				if err != nil {
					// Check if this was a user cancellation (Escape key)
					if reqCtx.Err() == context.Canceled && ctx.Err() == nil {
						waitForGoroutines()
						// User canceled with Escape - keep the query in history
						// but don't save the Escape keypress
						c.ui.PrintCanceled()
						return nil
					}
					toolResults = append(toolResults, ToolResult{
						Type:      "tool_result",
						ToolUseID: toolUse.ID,
						Content:   fmt.Sprintf("Error: %v", err),
						IsError:   true,
					})
				} else {
					toolResults = append(toolResults, ToolResult{
						Type:      "tool_result",
						ToolUseID: toolUse.ID,
						Content:   result.Content,
						IsError:   result.IsError,
					})

					// Refresh tool list after successful manage_connections operation
					// This ensures we get the updated tool list when database connection changes
					if toolUse.Name == "manage_connections" && !result.IsError {
						if newTools, err := c.mcp.ListTools(reqCtx); err == nil {
							c.tools = newTools
						}
					}
				}
			}

			// Add tool results to conversation
			c.messages = append(c.messages, Message{
				Role:    "user",
				Content: toolResults,
			})

			// Continue the loop to get final response
			continue
		}

		// Got final response
		waitForGoroutines()

		// Extract and display text content
		var textParts []string
		for _, item := range response.Content {
			if text, ok := item.(TextContent); ok {
				textParts = append(textParts, text.Text)
			}
		}

		finalText := strings.Join(textParts, "\n")
		c.ui.PrintAssistantResponse(finalText)

		// Add assistant's response to history
		c.messages = append(c.messages, Message{
			Role:    "assistant",
			Content: finalText,
		})

		return nil
	}

	waitForGoroutines()
	return fmt.Errorf("reached maximum number of tool calls (%d)", maxAgenticLoops)
}

// SavePreferences saves the current preferences to disk
func (c *Client) SavePreferences() error {
	if c.preferences == nil {
		return nil
	}

	// Just save preferences as-is. The /set commands already update both
	// c.preferences and c.config, and save immediately. We don't want to
	// overwrite c.preferences.LastProvider from c.config here because
	// c.config may have been loaded from file with different values.
	return SavePreferences(c.preferences)
}
