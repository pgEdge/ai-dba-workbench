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
	"os"
	"sort"
	"strings"
)

// SlashCommand represents a parsed slash command
type SlashCommand struct {
	Command string
	Args    []string
}

// ParseSlashCommand parses a slash command from user input
func ParseSlashCommand(input string) *SlashCommand {
	if !strings.HasPrefix(input, "/") {
		return nil
	}

	// Remove the leading slash
	input = strings.TrimPrefix(input, "/")

	// Split into command and arguments, respecting quotes
	parts := parseQuotedArgs(input)
	if len(parts) == 0 {
		return nil
	}

	return &SlashCommand{
		Command: parts[0],
		Args:    parts[1:],
	}
}

// parseQuotedArgs splits a string into arguments, respecting quoted strings
func parseQuotedArgs(input string) []string {
	var args []string
	var current strings.Builder
	inQuote := false
	quoteChar := rune(0)

	// Convert to runes for proper Unicode handling
	runes := []rune(input)

	for i := 0; i < len(runes); i++ {
		r := runes[i]

		switch {
		case (r == '"' || r == '\'') && !inQuote:
			// Start of quoted string
			inQuote = true
			quoteChar = r
		case r == quoteChar && inQuote:
			// End of quoted string
			inQuote = false
			quoteChar = 0
		case r == ' ' && !inQuote:
			// Space outside quotes - end of argument
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		case r == '\\' && inQuote && i+1 < len(runes):
			// Escape sequence in quoted string
			next := runes[i+1]
			if next == quoteChar || next == '\\' {
				// Skip the backslash, include the escaped character
				current.WriteRune(next)
				i++ // Skip the next character since we've already processed it
			} else {
				// Not a valid escape sequence, include the backslash
				current.WriteRune(r)
			}
		default:
			// Regular character
			current.WriteRune(r)
		}
	}

	// Add the last argument if any
	if current.Len() > 0 {
		args = append(args, current.String())
	}

	return args
}

// HandleSlashCommand processes slash commands, returns true if handled
func (c *Client) HandleSlashCommand(ctx context.Context, cmd *SlashCommand) bool {
	if cmd == nil {
		return false
	}

	switch cmd.Command {
	case "help":
		c.printSlashHelp()
		return true

	case "clear":
		c.ui.ClearScreen()
		var serverVersion string
		if c.mcp != nil {
			_, serverVersion = c.mcp.GetServerInfo()
		}
		c.ui.PrintWelcome(ClientVersion, serverVersion)
		return true

	case "tools":
		c.ui.PrintSystemMessage(fmt.Sprintf("Available tools (%d):", len(c.tools)))
		// Sort tools alphabetically by name
		sortedTools := make([]struct{ Name, Desc string }, len(c.tools))
		for i, tool := range c.tools {
			sortedTools[i] = struct{ Name, Desc string }{tool.Name, getBriefDescription(tool.Description)}
		}
		sort.Slice(sortedTools, func(i, j int) bool {
			return sortedTools[i].Name < sortedTools[j].Name
		})
		for _, tool := range sortedTools {
			fmt.Printf("  - %s: %s\n", tool.Name, tool.Desc)
		}
		return true

	case "resources":
		c.ui.PrintSystemMessage(fmt.Sprintf("Available resources (%d):", len(c.resources)))
		// Sort resources alphabetically by name
		sortedResources := make([]struct{ Name, Desc string }, len(c.resources))
		for i, resource := range c.resources {
			sortedResources[i] = struct{ Name, Desc string }{resource.Name, resource.Description}
		}
		sort.Slice(sortedResources, func(i, j int) bool {
			return sortedResources[i].Name < sortedResources[j].Name
		})
		for _, resource := range sortedResources {
			fmt.Printf("  - %s: %s\n", resource.Name, resource.Desc)
		}
		return true

	case "prompts":
		c.ui.PrintSystemMessage(fmt.Sprintf("Available prompts (%d):", len(c.prompts)))
		// Sort prompts alphabetically by name
		sortedPrompts := make([]struct{ Name, Desc string }, len(c.prompts))
		for i, prompt := range c.prompts {
			sortedPrompts[i] = struct{ Name, Desc string }{prompt.Name, prompt.Description}
		}
		sort.Slice(sortedPrompts, func(i, j int) bool {
			return sortedPrompts[i].Name < sortedPrompts[j].Name
		})
		for _, prompt := range sortedPrompts {
			fmt.Printf("  - %s: %s\n", prompt.Name, prompt.Desc)
		}
		return true

	case "quit", "exit":
		c.ui.PrintSystemMessage("Goodbye!")
		os.Exit(0)
		return true

	case "set":
		return c.handleSetCommand(ctx, cmd.Args)

	case "show":
		return c.handleShowCommand(ctx, cmd.Args)

	case "list":
		return c.handleListCommand(ctx, cmd.Args)

	case "prompt":
		return c.handlePromptCommand(ctx, cmd.Args)

	case "history":
		return c.handleHistoryCommand(ctx, cmd.Args)

	case "new":
		return c.handleNewConversation(ctx)

	case "save":
		return c.handleSaveConversation(ctx)

	default:
		// Unknown slash command, let it be sent to LLM
		return false
	}
}

// printSlashHelp prints help for slash commands
func (c *Client) printSlashHelp() {
	help := `
Commands:
  /help                                Show this help message
  /clear                               Clear screen
  /tools                               List available MCP tools
  /resources                           List available MCP resources
  /prompts                             List available MCP prompts
  /quit, /exit                         Exit the chat client

Settings:
  /set color <on|off>                  Enable or disable colored output
  /set status-messages <on|off>        Enable or disable status messages
  /set markdown <on|off>               Enable or disable markdown rendering
  /set debug <on|off>                  Enable or disable debug messages
  /set llm-provider <provider>         Set LLM provider (anthropic, openai, ollama)
  /set llm-model <model>               Set LLM model to use
  /set database <name>                 Select a database connection
  /show color                          Show current color setting
  /show status-messages                Show current status messages setting
  /show markdown                       Show current markdown rendering setting
  /show debug                          Show current debug setting
  /show llm-provider                   Show current LLM provider
  /show llm-model                      Show current LLM model
  /show database                       Show current database connection
  /show settings                       Show all current settings
  /list models                         List available models from current LLM provider
  /list databases                      List available database connections

Prompts:
  /prompt <name> [arg=value ...]       Execute an MCP prompt with optional arguments
`

	// Add history commands only if running with authentication
	if c.conversations != nil {
		help += `
Conversation History (requires authentication):
  /new                                 Start a new conversation
  /save                                Save the current conversation
  /history                             List saved conversations
  /history load <id>                   Load a saved conversation
  /history rename <id> "new title"     Rename a saved conversation
  /history delete <id>                 Delete a saved conversation
  /history delete-all                  Delete all saved conversations
`
	}

	help += `
Examples:
  /set llm-provider openai
  /set llm-model gpt-4-turbo
  /set database mydb
  /list models
  /list databases
  /prompt explore-database
  /prompt setup-semantic-search query_text="product search"

Anything else you type will be sent to the LLM.
`
	fmt.Print(help)
}
