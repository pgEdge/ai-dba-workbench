/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package chat

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	pkgchat "github.com/pgedge/ai-workbench/pkg/chat"
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
Available Commands:

  Navigation:
    /help                              Show this help
    /clear                             Clear the screen
    /quit, /exit                       Exit the CLI

  LLM Settings:
    /list providers                    List available LLM providers
    /list models                       List available models for current provider
    /set provider <name>               Set LLM provider (anthropic, openai, ollama)
    /set model <name>                  Set LLM model
    /show provider                     Show current LLM provider
    /show model                        Show current LLM model

  Database Connections:
    /list connections                  List available database connections
    /list databases                    List databases on selected connection
    /set connection <id> [database]    Select a database connection
    /set connection none               Clear current connection
    /show connection                   Show current connection

  MCP Resources:
    /list tools                        List available MCP tools
    /list resources                    List available MCP resources
    /list prompts                      List available MCP prompts
    /prompt <name> [arg=value ...]     Execute an MCP prompt

  Display Settings:
    /set color <on|off>                Enable/disable colored output
    /set markdown <on|off>             Enable/disable markdown rendering
    /set status-messages <on|off>      Enable/disable status messages
    /set debug <on|off>                Enable/disable debug messages
    /show settings                     Show all current settings
`

	// Add history commands only if running with authentication
	if c.conversations != nil {
		help += `
  Conversation History:
    /history                           List saved conversations
    /history load <id>                 Load a saved conversation
    /history rename <id> "title"       Rename a saved conversation
    /history delete <id>               Delete a saved conversation
    /history delete-all                Delete all saved conversations
    /new                               Start a new conversation
    /save                              Save current conversation
`
	}

	help += `
Examples:
  /list providers
  /set provider anthropic
  /set model claude-sonnet-4-20250514
  /list connections
  /set connection 1
  /set connection 1 mydb
  /prompt explore-database

Anything else you type will be sent to the LLM.
`
	fmt.Print(help)
}

// handleSetCommand handles /set commands
func (c *Client) handleSetCommand(ctx context.Context, args []string) bool {
	if len(args) < 1 {
		c.ui.PrintError("Usage: /set <setting> <value>")
		c.ui.PrintSystemMessage("Available settings: provider, model, connection, color, status-messages, markdown, debug")
		return true
	}

	setting := args[0]
	value := ""
	if len(args) >= 2 {
		value = args[1]
	}

	switch setting {
	case "color", "colour": //nolint:misspell // British spelling intentionally supported
		if value == "" {
			c.ui.PrintError("Usage: /set color <on|off>")
			return true
		}
		return c.handleSetColor(value)

	case "status-messages":
		if value == "" {
			c.ui.PrintError("Usage: /set status-messages <on|off>")
			return true
		}
		return c.handleSetStatusMessages(value)

	case "markdown":
		if value == "" {
			c.ui.PrintError("Usage: /set markdown <on|off>")
			return true
		}
		return c.handleSetMarkdown(value)

	case "debug":
		if value == "" {
			c.ui.PrintError("Usage: /set debug <on|off>")
			return true
		}
		return c.handleSetDebug(value)

	case "provider":
		if value == "" {
			c.ui.PrintError("Usage: /set provider <name>")
			c.ui.PrintSystemMessage("Available providers: anthropic, openai, ollama")
			return true
		}
		return c.handleSetLLMProvider(value)

	case "model":
		if value == "" {
			c.ui.PrintError("Usage: /set model <name>")
			c.ui.PrintSystemMessage("Use '/list models' to see available models")
			return true
		}
		return c.handleSetLLMModel(value)

	case "connection":
		return c.handleSetConnection(ctx, args[1:])

	default:
		c.ui.PrintError(fmt.Sprintf("Unknown setting: %s", setting))
		c.ui.PrintSystemMessage("Available settings: provider, model, connection, color, status-messages, markdown, debug")
		return true
	}
}

// handleSetColor handles setting colored output on/off
func (c *Client) handleSetColor(value string) bool {
	value = strings.ToLower(value)

	switch value {
	case "on", "true", "1", "yes":
		c.config.UI.NoColor = false
		c.ui.SetNoColor(false)
		c.preferences.UI.Color = true
		c.ui.PrintSystemMessage("Colored output enabled")

	case "off", "false", "0", "no":
		c.config.UI.NoColor = true
		c.ui.SetNoColor(true)
		c.preferences.UI.Color = false
		c.ui.PrintSystemMessage("Colored output disabled")

	default:
		c.ui.PrintError(fmt.Sprintf("Invalid value for color: %s (use on or off)", value))
		return true
	}

	// Save preferences
	if err := SavePreferences(c.preferences); err != nil {
		c.ui.PrintError(fmt.Sprintf("Warning: Failed to save preferences: %v", err))
	}

	return true
}

// handleSetStatusMessages handles setting status messages on/off
func (c *Client) handleSetStatusMessages(value string) bool {
	value = strings.ToLower(value)

	switch value {
	case "on", "true", "1", "yes":
		c.config.UI.DisplayStatusMessages = true
		c.ui.DisplayStatusMessages = true
		c.preferences.UI.DisplayStatusMessages = true
		c.ui.PrintSystemMessage("Status messages enabled")

	case "off", "false", "0", "no":
		c.config.UI.DisplayStatusMessages = false
		c.ui.DisplayStatusMessages = false
		c.preferences.UI.DisplayStatusMessages = false
		c.ui.PrintSystemMessage("Status messages disabled")

	default:
		c.ui.PrintError(fmt.Sprintf("Invalid value for status-messages: %s (use on or off)", value))
		return true
	}

	// Save preferences
	if err := SavePreferences(c.preferences); err != nil {
		c.ui.PrintError(fmt.Sprintf("Warning: Failed to save preferences: %v", err))
	}

	return true
}

// handleSetMarkdown handles setting markdown rendering on/off
func (c *Client) handleSetMarkdown(value string) bool {
	value = strings.ToLower(value)

	switch value {
	case "on", "true", "1", "yes":
		c.config.UI.RenderMarkdown = true
		c.ui.RenderMarkdown = true
		c.preferences.UI.RenderMarkdown = true
		c.ui.PrintSystemMessage("Markdown rendering enabled")

	case "off", "false", "0", "no":
		c.config.UI.RenderMarkdown = false
		c.ui.RenderMarkdown = false
		c.preferences.UI.RenderMarkdown = false
		c.ui.PrintSystemMessage("Markdown rendering disabled")

	default:
		c.ui.PrintError(fmt.Sprintf("Invalid value for markdown: %s (use on or off)", value))
		return true
	}

	// Save preferences
	if err := SavePreferences(c.preferences); err != nil {
		c.ui.PrintError(fmt.Sprintf("Warning: Failed to save preferences: %v", err))
	}

	return true
}

// handleSetDebug handles setting debug mode on/off
func (c *Client) handleSetDebug(value string) bool {
	value = strings.ToLower(value)

	switch value {
	case "on", "true", "1", "yes":
		c.config.UI.Debug = true
		c.preferences.UI.Debug = true
		c.ui.PrintSystemMessage("Debug messages enabled")

	case "off", "false", "0", "no":
		c.config.UI.Debug = false
		c.preferences.UI.Debug = false
		c.ui.PrintSystemMessage("Debug messages disabled")

	default:
		c.ui.PrintError(fmt.Sprintf("Invalid value for debug: %s (use on or off)", value))
		return true
	}

	// Reinitialize LLM client with new debug setting
	if err := c.initializeLLM(); err != nil {
		c.ui.PrintError(fmt.Sprintf("Failed to reinitialize LLM: %v", err))
		return true
	}

	// Save preferences
	if err := SavePreferences(c.preferences); err != nil {
		c.ui.PrintError(fmt.Sprintf("Warning: Failed to save preferences: %v", err))
	}

	return true
}

// handleSetLLMProvider handles setting the LLM provider
func (c *Client) handleSetLLMProvider(provider string) bool {
	provider = strings.ToLower(provider)

	// Validate provider name
	validProviders := map[string]bool{
		"anthropic": true,
		"openai":    true,
		"ollama":    true,
	}

	if !validProviders[provider] {
		c.ui.PrintError(fmt.Sprintf("Invalid LLM provider: %s", provider))
		c.ui.PrintSystemMessage("Valid providers: anthropic, openai, ollama")
		return true
	}

	// Check if provider is configured
	if !c.config.IsProviderConfigured(provider) {
		c.ui.PrintError(fmt.Sprintf("Provider %s is not configured (missing API key or URL)", provider))
		return true
	}

	// NOTE: Do NOT save c.config.LLM.Model to preferences here!
	// c.config.LLM.Model may be a runtime fallback that differs from the user's
	// saved preference (e.g., if the API didn't return their preferred model).
	// The model preference is already stored in c.preferences.ProviderModels
	// and will be preserved across provider switches.

	// Update config to new provider
	c.config.LLM.Provider = provider

	// Clear model to trigger auto-selection in initializeLLM()
	c.config.LLM.Model = ""

	// Update preferences
	c.preferences.LastProvider = provider

	// Reinitialize LLM client (will auto-select model)
	if err := c.initializeLLM(); err != nil {
		c.ui.PrintError(fmt.Sprintf("Failed to initialize LLM: %v", err))
		return true
	}

	// Save preferences (model was already saved in initializeLLM)
	if err := SavePreferences(c.preferences); err != nil {
		c.ui.PrintError(fmt.Sprintf("Warning: Failed to save preferences: %v", err))
	}

	c.ui.PrintSystemMessage(fmt.Sprintf("LLM provider set to: %s (model: %s)", provider, c.config.LLM.Model))
	return true
}

// handleSetLLMModel handles setting the LLM model
func (c *Client) handleSetLLMModel(model string) bool {
	// Get available models to validate
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	availableModels, err := c.llm.ListModels(ctx)
	if err != nil {
		// If we can't validate, warn but allow the change
		c.ui.PrintSystemMessage(fmt.Sprintf(
			"Warning: Could not validate model (error: %v)", err))
	} else if !pkgchat.IsModelAvailable(model, availableModels) {
		c.ui.PrintError(fmt.Sprintf(
			"Model '%s' not available from %s", model, c.config.LLM.Provider))
		c.ui.PrintSystemMessage("Use /list models to see available models")
		return true
	}

	// Update config
	c.config.LLM.Model = model

	// Save model preference for current provider
	c.preferences.SetModelForProvider(c.config.LLM.Provider, model)

	// Reinitialize LLM client with the new model
	if err := c.initializeLLM(); err != nil {
		c.ui.PrintError(fmt.Sprintf("Failed to initialize LLM: %v", err))
		return true
	}

	// Save preferences
	if err := SavePreferences(c.preferences); err != nil {
		c.ui.PrintError(fmt.Sprintf("Warning: Failed to save preferences: %v", err))
	}

	c.ui.PrintSystemMessage(fmt.Sprintf("LLM model set to: %s (provider: %s)", model, c.config.LLM.Provider))
	return true
}

// handleShowCommand handles /show commands
func (c *Client) handleShowCommand(ctx context.Context, args []string) bool {
	if len(args) < 1 {
		c.ui.PrintError("Usage: /show <setting>")
		c.ui.PrintSystemMessage("Available settings: provider, model, connection, color, status-messages, markdown, debug, settings")
		return true
	}

	setting := args[0]

	switch setting {
	case "color", "colour": //nolint:misspell // British spelling intentionally supported
		status := "on"
		if c.config.UI.NoColor {
			status = "off"
		}
		c.ui.PrintSystemMessage(fmt.Sprintf("Colored output: %s", status))

	case "status-messages":
		status := "off"
		if c.config.UI.DisplayStatusMessages {
			status = "on"
		}
		c.ui.PrintSystemMessage(fmt.Sprintf("Status messages: %s", status))

	case "markdown":
		status := "off"
		if c.config.UI.RenderMarkdown {
			status = "on"
		}
		c.ui.PrintSystemMessage(fmt.Sprintf("Markdown rendering: %s", status))

	case "debug":
		status := "off"
		if c.config.UI.Debug {
			status = "on"
		}
		c.ui.PrintSystemMessage(fmt.Sprintf("Debug messages: %s", status))

	case "provider":
		c.ui.PrintSystemMessage(fmt.Sprintf("LLM provider: %s", c.config.LLM.Provider))

	case "model":
		c.ui.PrintSystemMessage(fmt.Sprintf("LLM model: %s", c.config.LLM.Model))

	case "connection":
		return c.showCurrentConnection(ctx)

	case "settings":
		c.printAllSettings()

	default:
		c.ui.PrintError(fmt.Sprintf("Unknown setting: %s", setting))
		c.ui.PrintSystemMessage("Available settings: provider, model, connection, color, status-messages, markdown, debug, settings")
	}

	return true
}

// printAllSettings prints all current settings
func (c *Client) printAllSettings() {
	fmt.Println("\nCurrent Settings:")
	fmt.Println("─────────────────────────────────────────────────")

	// UI Settings
	fmt.Println("\nUI:")
	statusMsg := "off"
	if c.config.UI.DisplayStatusMessages {
		statusMsg = "on"
	}
	fmt.Printf("  Status Messages:  %s\n", statusMsg)
	markdown := "off"
	if c.config.UI.RenderMarkdown {
		markdown = "on"
	}
	fmt.Printf("  Render Markdown:  %s\n", markdown)
	debug := "off"
	if c.config.UI.Debug {
		debug = "on"
	}
	fmt.Printf("  Debug Messages:   %s\n", debug)
	color := "on"
	if c.config.UI.NoColor {
		color = "off"
	}
	fmt.Printf("  Color:            %s\n", color)

	// LLM Settings
	fmt.Println("\nLLM:")
	fmt.Printf("  Provider:         %s\n", c.config.LLM.Provider)
	fmt.Printf("  Model:            %s\n", c.config.LLM.Model)
	fmt.Printf("  Max Tokens:       %d\n", c.config.LLM.MaxTokens)
	fmt.Printf("  Temperature:      %.2f\n", c.config.LLM.Temperature)

	// MCP Settings
	fmt.Println("\nMCP:")
	fmt.Printf("  URL:              %s\n", c.config.MCP.URL)
	fmt.Printf("  Auth Mode:        %s\n", c.config.MCP.AuthMode)

	fmt.Println("─────────────────────────────────────────────────")
}

// handleListCommand handles /list commands
func (c *Client) handleListCommand(ctx context.Context, args []string) bool {
	if len(args) < 1 {
		c.ui.PrintError("Usage: /list <what>")
		c.ui.PrintSystemMessage("Available: providers, models, tools, resources, prompts, connections, databases")
		return true
	}

	what := args[0]

	switch what {
	case "providers":
		return c.listProviders()

	case "models":
		return c.listModels(ctx)

	case "tools":
		return c.listTools()

	case "resources":
		return c.listResources()

	case "prompts":
		return c.listPrompts()

	case "connections":
		return c.listConnections(ctx)

	case "databases":
		return c.listDatabases(ctx)

	default:
		c.ui.PrintError(fmt.Sprintf("Unknown list target: %s", what))
		c.ui.PrintSystemMessage("Available: providers, models, tools, resources, prompts, connections, databases")
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

// listProviders lists available LLM providers
func (c *Client) listProviders() bool {
	providers := c.config.GetConfiguredProviders()
	currentProvider := c.config.LLM.Provider

	c.ui.PrintSystemMessage(fmt.Sprintf("Available LLM providers (%d):", len(providers)))
	for _, p := range providers {
		if p == currentProvider {
			fmt.Printf("  * %s (current)\n", p)
		} else {
			fmt.Printf("    %s\n", p)
		}
	}

	return true
}

// listTools lists available MCP tools
func (c *Client) listTools() bool {
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
}

// listResources lists available MCP resources
func (c *Client) listResources() bool {
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
}

// listPrompts lists available MCP prompts
func (c *Client) listPrompts() bool {
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
}

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

// handleHistoryCommand handles /history commands for conversation management
func (c *Client) handleHistoryCommand(ctx context.Context, args []string) bool {
	// Check if conversations are available (requires authentication)
	if c.conversations == nil {
		c.ui.PrintError("Conversation history is only available when running with authentication")
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

	c.ui.PrintSystemMessage(fmt.Sprintf("Loaded conversation: %s", conv.Title))
	c.ui.PrintSystemMessage(fmt.Sprintf("Messages: %d, Provider: %s, Model: %s",
		len(c.messages), c.config.LLM.Provider, c.config.LLM.Model))

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
func (c *Client) handleNewConversation(_ context.Context) bool {
	// Check if conversations are available (requires authentication)
	if c.conversations == nil {
		c.ui.PrintError("Conversation history is only available when running with authentication")
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
	// Check if conversations are available (requires authentication)
	if c.conversations == nil {
		c.ui.PrintError("Conversation history is only available when running with authentication")
		return true
	}

	if len(c.messages) == 0 {
		c.ui.PrintError("No messages to save")
		return true
	}

	var conv *Conversation
	var err error

	if c.currentConversationID != "" {
		// Update existing conversation
		conv, err = c.conversations.Update(ctx, c.currentConversationID,
			c.config.LLM.Provider, c.config.LLM.Model, "", c.messages)
	} else {
		// Create new conversation
		conv, err = c.conversations.Create(ctx,
			c.config.LLM.Provider, c.config.LLM.Model, "", c.messages)
	}

	if err != nil {
		c.ui.PrintError(fmt.Sprintf("Failed to save conversation: %v", err))
		return true
	}

	c.currentConversationID = conv.ID
	c.ui.PrintSystemMessage(fmt.Sprintf("Conversation saved: %s (ID: %s)", conv.Title, conv.ID))
	return true
}

// =============================================================================
// Connection Management Commands
// =============================================================================

// listConnections lists available database connections
func (c *Client) listConnections(ctx context.Context) bool {
	if c.mcp == nil {
		c.ui.PrintError("MCP client not available")
		return true
	}

	connections, err := c.mcp.ListConnections(ctx)
	if err != nil {
		c.ui.PrintError(fmt.Sprintf("Failed to list connections: %v", err))
		return true
	}

	if len(connections) == 0 {
		c.ui.PrintSystemMessage("No database connections available")
		return true
	}

	// Get current connection to mark it
	current, _ := c.mcp.GetCurrentConnection(ctx)

	c.ui.PrintSystemMessage(fmt.Sprintf("Available connections (%d):", len(connections)))
	fmt.Println()

	for _, conn := range connections {
		marker := "  "
		if current != nil && current.ConnectionID == conn.ID {
			marker = "* "
		}

		monitored := ""
		if conn.IsMonitored {
			monitored = " [monitored]"
		}

		fmt.Printf("%s%d: %s%s\n", marker, conn.ID, conn.Name, monitored)
		fmt.Printf("     Host: %s:%d, Database: %s\n", conn.Host, conn.Port, conn.DatabaseName)
	}

	fmt.Println()
	c.ui.PrintSystemMessage("Use '/set connection <id>' to select a connection")
	return true
}

// listDatabases lists databases on the currently selected connection
func (c *Client) listDatabases(ctx context.Context) bool {
	if c.mcp == nil {
		c.ui.PrintError("MCP client not available")
		return true
	}

	// Get current connection
	current, err := c.mcp.GetCurrentConnection(ctx)
	if err != nil {
		c.ui.PrintError(fmt.Sprintf("Failed to get current connection: %v", err))
		return true
	}

	if current == nil {
		c.ui.PrintError("No connection selected. Use '/list connections' to see available connections, then '/set connection <id>' to select one.")
		return true
	}

	databases, err := c.mcp.ListDatabases(ctx, current.ConnectionID)
	if err != nil {
		c.ui.PrintError(fmt.Sprintf("Failed to list databases: %v", err))
		return true
	}

	if len(databases) == 0 {
		c.ui.PrintSystemMessage("No databases found on this connection")
		return true
	}

	c.ui.PrintSystemMessage(fmt.Sprintf("Databases on %s (%d):", current.Name, len(databases)))
	fmt.Println()

	for _, db := range databases {
		marker := "  "
		if current.DatabaseName != nil && *current.DatabaseName == db.Name {
			marker = "* "
		}
		fmt.Printf("%s%s (owner: %s, size: %s, encoding: %s)\n",
			marker, db.Name, db.Owner, db.Size, db.Encoding)
	}

	fmt.Println()
	c.ui.PrintSystemMessage("Use '/set connection <id> <database-name>' to select a specific database")
	return true
}

// showCurrentConnection shows the current database connection
func (c *Client) showCurrentConnection(ctx context.Context) bool {
	if c.mcp == nil {
		c.ui.PrintError("MCP client not available")
		return true
	}

	current, err := c.mcp.GetCurrentConnection(ctx)
	if err != nil {
		c.ui.PrintError(fmt.Sprintf("Failed to get current connection: %v", err))
		return true
	}

	if current == nil {
		c.ui.PrintSystemMessage("No database connection selected")
		c.ui.PrintSystemMessage("Use '/list connections' to see available connections")
		c.ui.PrintSystemMessage("Use '/set connection <id>' to select one")
	} else {
		dbName := current.DatabaseName
		if dbName == nil {
			defaultDb := "(default)"
			dbName = &defaultDb
		}
		c.ui.PrintSystemMessage(fmt.Sprintf("Current connection: %s (%s:%d)", current.Name, current.Host, current.Port))
		c.ui.PrintSystemMessage(fmt.Sprintf("Database: %s", *dbName))
	}
	return true
}

// handleSetConnection handles /set connection commands
func (c *Client) handleSetConnection(ctx context.Context, args []string) bool {
	if c.mcp == nil {
		c.ui.PrintError("MCP client not available")
		return true
	}

	if len(args) < 1 {
		c.ui.PrintError("Usage: /set connection <id> [database]")
		c.ui.PrintSystemMessage("Use '/set connection none' to clear the current connection")
		c.ui.PrintSystemMessage("Use '/list connections' to see available connections")
		return true
	}

	// Check for "none" to clear connection
	if args[0] == "none" {
		if err := c.mcp.ClearCurrentConnection(ctx); err != nil {
			c.ui.PrintError(fmt.Sprintf("Failed to clear connection: %v", err))
			return true
		}
		c.ui.PrintSystemMessage("Connection cleared")

		// Inject a notification into the conversation history
		c.messages = append(c.messages, Message{
			Role: "user",
			Content: "[SYSTEM NOTICE: The database connection has been cleared. " +
				"No database is currently connected. " +
				"Any previous database-related information is no longer relevant.]",
		})

		return true
	}

	// Parse connection ID
	var connectionID int
	if _, err := fmt.Sscanf(args[0], "%d", &connectionID); err != nil {
		c.ui.PrintError(fmt.Sprintf("Invalid connection ID: %s (use a number or 'none')", args[0]))
		return true
	}

	// Optional database name
	var databaseName *string
	if len(args) > 1 {
		databaseName = &args[1]
	}

	// Set the connection
	result, err := c.mcp.SetCurrentConnection(ctx, connectionID, databaseName)
	if err != nil {
		c.ui.PrintError(fmt.Sprintf("Failed to select connection: %v", err))
		return true
	}

	dbMsg := ""
	dbName := "default database"
	if result.DatabaseName != nil {
		dbMsg = fmt.Sprintf(", database: %s", *result.DatabaseName)
		dbName = *result.DatabaseName
	}
	c.ui.PrintSystemMessage(fmt.Sprintf("Connected to: %s (%s:%d%s)", result.Name, result.Host, result.Port, dbMsg))

	// Inject a notification into the conversation history so the LLM knows the connection changed
	// This ensures the LLM doesn't continue referencing stale database context
	connectionNotice := fmt.Sprintf(
		"[SYSTEM NOTICE: The database connection has been changed. "+
			"You are now connected to server '%s' (%s:%d), database '%s'. "+
			"Any previous information about database schema, tables, or query results from other connections is no longer relevant. "+
			"Please use the current connection for all future queries.]",
		result.Name, result.Host, result.Port, dbName)

	c.messages = append(c.messages, Message{
		Role:    "user",
		Content: connectionNotice,
	})

	return true
}
