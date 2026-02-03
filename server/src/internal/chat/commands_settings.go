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
	"time"
)

// handleSetCommand handles /set commands
func (c *Client) handleSetCommand(ctx context.Context, args []string) bool {
	if len(args) < 2 {
		c.ui.PrintError("Usage: /set <setting> <value>")
		c.ui.PrintSystemMessage("Available settings: status-messages, markdown, debug, llm-provider, llm-model, database")
		return true
	}

	setting := args[0]
	value := args[1]

	switch setting {
	case "color", "colour": //nolint:misspell // British spelling intentionally supported
		return c.handleSetColor(value)

	case "status-messages":
		return c.handleSetStatusMessages(value)

	case "markdown":
		return c.handleSetMarkdown(value)

	case "debug":
		return c.handleSetDebug(value)

	case "llm-provider":
		return c.handleSetLLMProvider(value)

	case "llm-model":
		return c.handleSetLLMModel(value)

	case "database":
		return c.handleSetDatabase(ctx, value)

	default:
		c.ui.PrintError(fmt.Sprintf("Unknown setting: %s", setting))
		c.ui.PrintSystemMessage("Available settings: color, status-messages, markdown, debug, llm-provider, llm-model, database")
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

	// Save current model for current provider before switching
	if c.config.LLM.Provider != "" && c.config.LLM.Model != "" {
		c.preferences.SetModelForProvider(c.config.LLM.Provider, c.config.LLM.Model)
	}

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
	} else if !isModelAvailable(model, availableModels) {
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
		c.ui.PrintSystemMessage("Available settings: color, status-messages, markdown, debug, llm-provider, llm-model, database, settings")
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

	case "llm-provider":
		c.ui.PrintSystemMessage(fmt.Sprintf("LLM provider: %s", c.config.LLM.Provider))

	case "llm-model":
		c.ui.PrintSystemMessage(fmt.Sprintf("LLM model: %s", c.config.LLM.Model))

	case "database":
		return c.handleShowDatabase(ctx)

	case "settings":
		c.printAllSettings()

	default:
		c.ui.PrintError(fmt.Sprintf("Unknown setting: %s", setting))
		c.ui.PrintSystemMessage("Available settings: color, status-messages, markdown, debug, llm-provider, llm-model, database, settings")
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
	fmt.Printf("  Mode:             %s\n", c.config.MCP.Mode)
	if c.config.MCP.Mode == "http" {
		fmt.Printf("  URL:              %s\n", c.config.MCP.URL)
		fmt.Printf("  Auth Mode:        %s\n", c.config.MCP.AuthMode)
	} else {
		fmt.Printf("  Server Path:      %s\n", c.config.MCP.ServerPath)
	}

	fmt.Println("─────────────────────────────────────────────────")
}
