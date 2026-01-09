/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/pgedge/ai-workbench/cli/internal/chat"
)

func main() {
	// Command line flags
	configFile := flag.String("config", "", "Path to configuration file")
	showVersion := flag.Bool("version", false, "Show version and exit")
	mcpURL := flag.String("mcp-url", "", "MCP server URL")
	mcpToken := flag.String("mcp-token", "", "MCP server authentication token (for token mode)")
	mcpUsername := flag.String("mcp-username", "", "MCP server username (for user mode)")
	mcpPassword := flag.String("mcp-password", "", "MCP server password (for user mode)")
	llmProvider := flag.String("llm-provider", "", "LLM provider: anthropic, openai, or ollama (default: anthropic)")
	llmModel := flag.String("llm-model", "", "LLM model to use")
	noColor := flag.Bool("no-color", false, "Disable colored output")

	flag.Parse()

	// Show version
	if *showVersion {
		fmt.Printf("pgEdge NLA CLI v%s\n", chat.ClientVersion)
		return
	}

	// Load configuration
	cfg, err := chat.LoadConfig(*configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading configuration: %v\n", err)
		os.Exit(1)
	}

	// Track which flags were explicitly set
	overrides := &chat.ConfigOverrides{
		ProviderSet: (*llmProvider != ""),
		ModelSet:    (*llmModel != ""),
	}

	// Override config with command line flags
	if *mcpURL != "" {
		cfg.MCP.URL = *mcpURL
	}
	if *mcpToken != "" {
		cfg.MCP.Token = *mcpToken
	}
	if *mcpUsername != "" {
		cfg.MCP.Username = *mcpUsername
	}
	if *mcpPassword != "" {
		cfg.MCP.Password = *mcpPassword
	}
	if *llmProvider != "" {
		cfg.LLM.Provider = *llmProvider
	}
	if *llmModel != "" {
		cfg.LLM.Model = *llmModel
	}
	if *noColor {
		cfg.UI.NoColor = true
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		os.Exit(1)
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\n\nReceived interrupt signal. Shutting down...")
		cancel()
	}()

	// Create and run chat client
	client, err := chat.NewClient(cfg, overrides)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating chat client: %v\n", err)
		os.Exit(1)
	}

	// Save preferences on exit (normal or interrupted)
	defer func() {
		if err := client.SavePreferences(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to save preferences: %v\n", err)
		}
	}()

	if err := client.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error running chat client: %v\n", err)
		os.Exit(1)
	}
}
