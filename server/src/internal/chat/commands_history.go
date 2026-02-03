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

// handleHistoryCommand handles /history commands for conversation management
func (c *Client) handleHistoryCommand(ctx context.Context, args []string) bool {
	// Check if conversations are available (HTTP mode with authentication)
	if c.conversations == nil {
		c.ui.PrintError("Conversation history is only available when running with authentication (HTTP mode)")
		return true
	}

	// No args - list conversations
	if len(args) == 0 {
		return c.listConversations(ctx)
	}

	subcommand := args[0]

	switch subcommand {
	case "list":
		return c.listConversations(ctx)

	case "load":
		if len(args) < 2 {
			c.ui.PrintError("Usage: /history load <conversation-id>")
			return true
		}
		return c.loadConversation(ctx, args[1])

	case "rename":
		if len(args) < 3 {
			c.ui.PrintError("Usage: /history rename <conversation-id> \"new title\"")
			return true
		}
		// Join remaining args as the title (in case it wasn't quoted)
		title := strings.Join(args[2:], " ")
		return c.renameConversation(ctx, args[1], title)

	case "delete":
		if len(args) < 2 {
			c.ui.PrintError("Usage: /history delete <conversation-id>")
			return true
		}
		return c.deleteConversation(ctx, args[1])

	case "delete-all":
		return c.deleteAllConversations(ctx)

	default:
		c.ui.PrintError(fmt.Sprintf("Unknown history subcommand: %s", subcommand))
		c.ui.PrintSystemMessage("Available: list, load, rename, delete, delete-all")
		return true
	}
}

// listConversations lists all saved conversations
func (c *Client) listConversations(ctx context.Context) bool {
	conversations, err := c.conversations.List(ctx)
	if err != nil {
		c.ui.PrintError(fmt.Sprintf("Failed to list conversations: %v", err))
		return true
	}

	if len(conversations) == 0 {
		c.ui.PrintSystemMessage("No saved conversations")
		return true
	}

	c.ui.PrintSystemMessage(fmt.Sprintf("Saved conversations (%d):", len(conversations)))
	fmt.Println()

	for _, conv := range conversations {
		// Format the date
		dateStr := conv.UpdatedAt.Local().Format("Jan 02, 15:04")

		// Mark current conversation
		current := ""
		if conv.ID == c.currentConversationID {
			current = " (current)"
		}

		// Show connection if available
		connection := ""
		if conv.Connection != "" {
			connection = fmt.Sprintf(" [%s]", conv.Connection)
		}

		fmt.Printf("  %s%s%s\n", conv.ID, current, connection)
		fmt.Printf("    Title: %s\n", conv.Title)
		fmt.Printf("    Updated: %s\n", dateStr)
		if conv.Preview != "" {
			preview := conv.Preview
			if len(preview) > 60 {
				preview = preview[:57] + "..."
			}
			fmt.Printf("    Preview: %s\n", preview)
		}
		fmt.Println()
	}

	return true
}

// loadConversation loads a saved conversation
func (c *Client) loadConversation(ctx context.Context, id string) bool {
	conv, err := c.conversations.Get(ctx, id)
	if err != nil {
		c.ui.PrintError(fmt.Sprintf("Failed to load conversation: %v", err))
		return true
	}

	// Convert stored messages to client messages
	c.messages = make([]Message, 0, len(conv.Messages))
	for _, msg := range conv.Messages {
		c.messages = append(c.messages, Message{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	// Update current conversation ID
	c.currentConversationID = conv.ID

	// Restore provider and model if they were saved
	if conv.Provider != "" && c.config.IsProviderConfigured(conv.Provider) {
		if conv.Provider != c.config.LLM.Provider {
			c.config.LLM.Provider = conv.Provider
			c.config.LLM.Model = conv.Model
			if err := c.initializeLLM(); err != nil {
				c.ui.PrintError(fmt.Sprintf("Warning: Failed to restore LLM provider: %v", err))
			}
		} else if conv.Model != "" && conv.Model != c.config.LLM.Model {
			c.config.LLM.Model = conv.Model
			if err := c.initializeLLM(); err != nil {
				c.ui.PrintError(fmt.Sprintf("Warning: Failed to restore LLM model: %v", err))
			}
		}
	}

	// Restore database connection if different
	if conv.Connection != "" {
		if _, current, err := c.mcp.ListDatabases(ctx); err == nil {
			if current != conv.Connection {
				if err := c.mcp.SelectDatabase(ctx, conv.Connection); err != nil {
					c.ui.PrintError(fmt.Sprintf("Warning: Failed to restore database connection: %v", err))
				} else {
					// Refresh capabilities after database change
					if err := c.refreshCapabilities(ctx); err != nil {
						c.ui.PrintError(fmt.Sprintf("Warning: Failed to refresh capabilities: %v", err))
					}
				}
			}
		}
	}

	c.ui.PrintSystemMessage(fmt.Sprintf("Loaded conversation: %s", conv.Title))
	c.ui.PrintSystemMessage(fmt.Sprintf("Messages: %d, Provider: %s, Model: %s",
		len(c.messages), c.config.LLM.Provider, c.config.LLM.Model))

	// Show current database connection
	if _, current, err := c.mcp.ListDatabases(ctx); err == nil && current != "" {
		c.ui.PrintSystemMessage(fmt.Sprintf("Database: %s", current))
	}

	// Replay the conversation history with muted colors
	if len(c.messages) > 0 {
		fmt.Println()
		c.ui.PrintHistorySeparator("Conversation History")
		fmt.Println()

		for _, msg := range c.messages {
			// Extract text content from the message
			var text string
			switch content := msg.Content.(type) {
			case string:
				text = content
			default:
				// Skip non-text messages (tool calls, tool results, etc.)
				continue
			}

			if text == "" {
				continue
			}

			switch msg.Role {
			case "user":
				c.ui.PrintHistoricUserMessage(text)
			case "assistant":
				c.ui.PrintHistoricAssistantMessage(text)
			}
		}

		fmt.Println()
		c.ui.PrintHistorySeparator("End of History")
		fmt.Println()
	}

	return true
}

// renameConversation renames a saved conversation
func (c *Client) renameConversation(ctx context.Context, id, title string) bool {
	if err := c.conversations.Rename(ctx, id, title); err != nil {
		c.ui.PrintError(fmt.Sprintf("Failed to rename conversation: %v", err))
		return true
	}

	c.ui.PrintSystemMessage(fmt.Sprintf("Conversation renamed to: %s", title))
	return true
}

// deleteConversation deletes a saved conversation
func (c *Client) deleteConversation(ctx context.Context, id string) bool {
	if err := c.conversations.Delete(ctx, id); err != nil {
		c.ui.PrintError(fmt.Sprintf("Failed to delete conversation: %v", err))
		return true
	}

	// Clear current conversation ID if we deleted the current one
	if id == c.currentConversationID {
		c.currentConversationID = ""
	}

	c.ui.PrintSystemMessage("Conversation deleted")
	return true
}

// deleteAllConversations deletes all saved conversations
func (c *Client) deleteAllConversations(ctx context.Context) bool {
	count, err := c.conversations.DeleteAll(ctx)
	if err != nil {
		c.ui.PrintError(fmt.Sprintf("Failed to delete conversations: %v", err))
		return true
	}

	c.currentConversationID = ""
	c.ui.PrintSystemMessage(fmt.Sprintf("Deleted %d conversation(s)", count))
	return true
}

// handleNewConversation starts a new conversation
func (c *Client) handleNewConversation(ctx context.Context) bool {
	// Check if conversations are available (HTTP mode with authentication)
	if c.conversations == nil {
		c.ui.PrintError("Conversation history is only available when running with authentication (HTTP mode)")
		return true
	}

	// Clear current conversation
	c.messages = []Message{}
	c.currentConversationID = ""

	c.ui.PrintSystemMessage("Started new conversation")
	return true
}

// handleSaveConversation saves the current conversation
func (c *Client) handleSaveConversation(ctx context.Context) bool {
	// Check if conversations are available (HTTP mode with authentication)
	if c.conversations == nil {
		c.ui.PrintError("Conversation history is only available when running with authentication (HTTP mode)")
		return true
	}

	if len(c.messages) == 0 {
		c.ui.PrintError("No messages to save")
		return true
	}

	// Get current database connection
	connection := ""
	if _, current, err := c.mcp.ListDatabases(ctx); err == nil {
		connection = current
	}

	var conv *Conversation
	var err error

	if c.currentConversationID != "" {
		// Update existing conversation
		conv, err = c.conversations.Update(ctx, c.currentConversationID,
			c.config.LLM.Provider, c.config.LLM.Model, connection, c.messages)
	} else {
		// Create new conversation
		conv, err = c.conversations.Create(ctx,
			c.config.LLM.Provider, c.config.LLM.Model, connection, c.messages)
	}

	if err != nil {
		c.ui.PrintError(fmt.Sprintf("Failed to save conversation: %v", err))
		return true
	}

	c.currentConversationID = conv.ID
	c.ui.PrintSystemMessage(fmt.Sprintf("Conversation saved: %s (ID: %s)", conv.Title, conv.ID))
	return true
}
