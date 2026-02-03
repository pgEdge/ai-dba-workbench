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
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/chzyer/readline"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
)

// Client is the main chat client
type Client struct {
	config                *Config
	ui                    *UI
	mcp                   MCPClient
	llm                   LLMClient
	messages              []Message
	tools                 []mcp.Tool
	resources             []mcp.Resource
	prompts               []mcp.Prompt
	preferences           *Preferences
	conversations         *ConversationsClient
	currentConversationID string
}

// NewClient creates a new chat client
func NewClient(cfg *Config, overrides *ConfigOverrides) (*Client, error) {
	// Load user preferences
	prefs, err := LoadPreferences()
	if err != nil {
		// Log error but don't fail - use defaults
		fmt.Fprintf(os.Stderr, "Warning: Failed to load preferences: %v\n", err)
		prefs = getDefaultPreferences()
	}

	// Apply UI preferences from saved prefs
	cfg.UI.DisplayStatusMessages = prefs.UI.DisplayStatusMessages
	cfg.UI.RenderMarkdown = prefs.UI.RenderMarkdown
	cfg.UI.Debug = prefs.UI.Debug
	// Color preference (inverted: Color=true means NoColor=false)
	// Only apply if not already set by environment variable NO_COLOR
	if os.Getenv("NO_COLOR") == "" {
		cfg.UI.NoColor = !prefs.UI.Color
	}

	// === PROVIDER SELECTION LOGIC ===
	// Priority: flags > saved provider (if configured) > first configured provider
	if !overrides.ProviderSet {
		// Check if saved provider is configured
		if prefs.LastProvider != "" && cfg.IsProviderConfigured(prefs.LastProvider) {
			cfg.LLM.Provider = prefs.LastProvider
		} else {
			// Use first configured provider (anthropic > openai > ollama)
			configuredProviders := cfg.GetConfiguredProviders()
			if len(configuredProviders) == 0 {
				return nil, fmt.Errorf("no LLM provider configured (set API key for anthropic, openai, or ollama URL)")
			}
			cfg.LLM.Provider = configuredProviders[0]
		}
	}

	// Update prefs with actual provider being used
	prefs.LastProvider = cfg.LLM.Provider

	// === MODEL SELECTION ===
	// If model not set via flag, clear it so initializeLLM() will auto-select
	// based on saved preferences and available models from the provider
	if !overrides.ModelSet {
		cfg.LLM.Model = ""
	}

	ui := NewUI(cfg.UI.NoColor, cfg.UI.RenderMarkdown)
	ui.DisplayStatusMessages = cfg.UI.DisplayStatusMessages
	return &Client{
		config:      cfg,
		ui:          ui,
		messages:    []Message{},
		preferences: prefs,
	}, nil
}

// sanitizeTerminal ensures the terminal is in a sane state.
// This fixes issues if a previous run exited without restoring terminal settings
// (e.g., if the program crashed while in raw mode).
func (c *Client) sanitizeTerminal() {
	// Use stty sane to reset terminal to a sensible state
	// This is a no-op if terminal is already in a good state
	cmd := exec.Command("stty", "sane")
	cmd.Stdin = os.Stdin
	_ = cmd.Run() //nolint:errcheck // Best-effort terminal reset, errors are expected on non-TTY
}

// Run starts the chat client
func (c *Client) Run(ctx context.Context) error {
	// Ensure terminal is in a sane state at startup
	// This fixes issues if a previous run exited without restoring terminal settings
	c.sanitizeTerminal()

	// Connect to MCP server
	if err := c.connectToMCP(ctx); err != nil {
		return fmt.Errorf("failed to connect to MCP server: %w", err)
	}
	defer c.mcp.Close()

	// Initialize MCP connection
	if err := c.mcp.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize MCP connection: %w", err)
	}

	// Get available tools
	tools, err := c.mcp.ListTools(ctx)
	if err != nil {
		return fmt.Errorf("failed to list tools: %w", err)
	}
	c.tools = tools

	// Get available resources
	resources, err := c.mcp.ListResources(ctx)
	if err != nil {
		// Don't fail if resources are not supported by the server
		// Just log the error and continue
		if c.config.UI.Debug {
			fmt.Fprintf(os.Stderr, "Warning: Failed to list resources: %v\n", err)
		}
		c.resources = []mcp.Resource{}
	} else {
		c.resources = resources
	}

	// Get available prompts
	prompts, err := c.mcp.ListPrompts(ctx)
	if err != nil {
		// Don't fail if prompts are not supported by the server
		// Just log the error and continue
		if c.config.UI.Debug {
			fmt.Fprintf(os.Stderr, "Warning: Failed to list prompts: %v\n", err)
		}
		c.prompts = []mcp.Prompt{}
	} else {
		c.prompts = prompts
	}

	// Restore saved database preference for this server
	c.restoreDatabasePreference(ctx)

	// Initialize LLM client
	if err := c.initializeLLM(); err != nil {
		return fmt.Errorf("failed to initialize LLM: %w", err)
	}

	// Print welcome message with version info
	serverName, serverVersion := c.mcp.GetServerInfo()
	c.ui.PrintWelcome(ClientVersion, serverVersion)
	c.ui.PrintSystemMessage(fmt.Sprintf("Connected to %s (%d tools, %d resources, %d prompts)", serverName, len(c.tools), len(c.resources), len(c.prompts)))
	c.ui.PrintSystemMessage(fmt.Sprintf("Using LLM: %s (%s)", c.config.LLM.Provider, c.config.LLM.Model))

	// Display current database
	if databases, current, err := c.mcp.ListDatabases(ctx); err == nil && len(databases) > 0 {
		c.ui.PrintSystemMessage(fmt.Sprintf("Database: %s", current))
	}

	c.ui.PrintSeparator()

	// Start chat loop
	return c.chatLoop(ctx)
}

// PrefixCompleter implements readline.AutoCompleter for prefix-based history
type PrefixCompleter struct {
}

// Do implements the AutoCompleter interface for prefix-based history completion
func (pc *PrefixCompleter) Do(line []rune, pos int) (newLine [][]rune, length int) {
	// Get current line text
	lineStr := string(line[:pos])

	// If line is empty, don't suggest anything
	if lineStr == "" {
		return nil, 0
	}

	// This is called for Tab completion - we don't want to interfere with that
	// We only want to filter history on up/down arrows, which readline handles differently
	return nil, 0
}

// chatLoop runs the interactive chat loop
func (c *Client) chatLoop(ctx context.Context) error {
	// Use history file from config
	historyFile := c.config.HistoryFile

	// Configure readline with custom prompt
	rl, err := readline.NewEx(&readline.Config{
		Prompt:                 c.ui.GetPrompt(),
		HistoryFile:            historyFile,
		HistoryLimit:           1000,
		DisableAutoSaveHistory: false,
		InterruptPrompt:        "^C",
		EOFPrompt:              "exit",
		HistorySearchFold:      true, // Enable case-insensitive history search
		// Unfortunately, chzyer/readline doesn't support prefix-based history filtering
		// on up/down arrows natively. Users can use Ctrl+R for reverse search.
	})
	if err != nil {
		return fmt.Errorf("failed to initialize readline: %w", err)
	}
	defer rl.Close()

	// Monitor context cancellation in a goroutine
	go func() {
		<-ctx.Done()
		rl.Close() // Closing readline will cause Readline() to return an error
	}()

	// Main readline loop
	for {
		// This blocks until user provides input
		line, err := rl.Readline()

		if err != nil {
			// Handle various exit conditions
			if err == readline.ErrInterrupt || err == io.EOF {
				fmt.Println()
				c.ui.PrintSystemMessage("Goodbye!")
				return nil
			}
			// Check if context was canceled
			if ctx.Err() != nil {
				fmt.Println()
				c.ui.PrintSystemMessage("Goodbye!")
				return nil
			}
			return fmt.Errorf("readline error: %w", err)
		}

		userInput := strings.TrimSpace(line)
		if userInput == "" {
			continue
		}

		// Check for slash commands (all CLI commands start with /)
		if cmd := ParseSlashCommand(userInput); cmd != nil {
			if c.HandleSlashCommand(ctx, cmd) {
				continue // Command was handled
			}
			// Unknown slash command - inform user
			c.ui.PrintError(fmt.Sprintf("Unknown command: /%s (type /help for available commands)", cmd.Command))
			continue
		}

		// Everything else goes to the LLM
		if err := c.processQuery(ctx, userInput); err != nil {
			c.ui.PrintError(err.Error())
		}

		c.ui.PrintSeparator()
		// Readline will automatically display the prompt on the next iteration
	}
}
