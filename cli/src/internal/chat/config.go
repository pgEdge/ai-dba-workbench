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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds all configuration for the chat client
type Config struct {
	MCP         MCPConfig `yaml:"mcp"`
	LLM         LLMConfig `yaml:"llm"`
	UI          UIConfig  `yaml:"ui"`
	HistoryFile string    `yaml:"history_file"` // Path to chat history file
}

// ConfigOverrides tracks which config values were explicitly set via command-line flags
type ConfigOverrides struct {
	ProviderSet bool // LLM provider was explicitly set via flag
	ModelSet    bool // LLM model was explicitly set via flag
}

// MCPConfig holds MCP server connection configuration
type MCPConfig struct {
	URL      string `yaml:"url"`       // HTTP URL for MCP server
	AuthMode string `yaml:"auth_mode"` // token or user (default: user)
	Token    string `yaml:"token"`     // Authentication token (for token mode)
	Username string `yaml:"username"`  // Username (for user mode)
	Password string `yaml:"password"`  // Password (for user mode)
	TLS      bool   `yaml:"tls"`       // Use TLS/HTTPS
}

// LLMConfig holds LLM provider configuration
// Note: API keys are now configured on the server, not the CLI
type LLMConfig struct {
	Provider    string  `yaml:"provider"`    // anthropic, openai, or ollama
	Model       string  `yaml:"model"`       // Model to use (optional, server has defaults)
	MaxTokens   int     `yaml:"max_tokens"`  // Max tokens for response
	Temperature float64 `yaml:"temperature"` // Temperature for sampling
}

// UIConfig holds UI configuration
type UIConfig struct {
	NoColor               bool `yaml:"no_color"`                // Disable colored output
	DisplayStatusMessages bool `yaml:"display_status_messages"` // Display status messages during execution
	RenderMarkdown        bool `yaml:"render_markdown"`         // Render markdown with formatting and syntax highlighting
	Debug                 bool `yaml:"debug"`                   // Display debug messages (e.g., LLM token usage)
}

// LoadConfig loads configuration from file and defaults
func LoadConfig(configPath string) (*Config, error) {
	homeDir, _ := os.UserHomeDir()
	cfg := &Config{
		MCP: MCPConfig{
			URL:      "",
			AuthMode: "user",
			Token:    "", // Will be loaded separately
			Username: "",
			Password: "",
			TLS:      false,
		},
		LLM: LLMConfig{
			Provider:    "anthropic",
			Model:       "claude-sonnet-4-5-20250929",
			MaxTokens:   4096,
			Temperature: 0.7,
		},
		UI: UIConfig{
			NoColor:               false,
			DisplayStatusMessages: true, // Default to showing status messages
			RenderMarkdown:        true, // Default to rendering markdown
		},
		HistoryFile: filepath.Join(homeDir, ".ai-dba-workbench-cli-history"),
	}

	// Load from config file if provided
	if configPath != "" {
		if err := loadConfigFile(configPath, cfg); err != nil {
			return nil, fmt.Errorf("failed to load config file: %w", err)
		}
	} else {
		// Try default locations
		defaultPaths := []string{
			"/etc/pgedge/ai-dba-cli.yaml",
			filepath.Join(homeDir, ".ai-dba-cli.yaml"),
			".ai-dba-cli.yaml",
		}
		for _, path := range defaultPaths {
			if _, err := os.Stat(path); err == nil {
				if err := loadConfigFile(path, cfg); err == nil {
					break
				}
			}
		}
	}

	// Load authentication token from file
	cfg.MCP.Token = loadAuthToken()

	return cfg, nil
}

// loadConfigFile loads configuration from a YAML file
func loadConfigFile(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	return yaml.Unmarshal(data, cfg)
}

// loadAuthToken loads the authentication token from file
// Returns empty string if not found (will prompt if needed)
func loadAuthToken() string {
	homeDir, _ := os.UserHomeDir()
	tokenPath := filepath.Join(homeDir, ".ai-dba-cli-token")
	if data, err := os.ReadFile(tokenPath); err == nil {
		// Trim whitespace and newlines
		return strings.TrimSpace(string(data))
	}

	return ""
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Validate MCP URL is provided
	if c.MCP.URL == "" {
		return fmt.Errorf("mcp-url is required")
	}

	// Validate auth mode (token or user only, no-auth not supported)
	if c.MCP.AuthMode != "token" && c.MCP.AuthMode != "user" {
		return fmt.Errorf("invalid auth-mode: %s (must be token or user)", c.MCP.AuthMode)
	}

	// Validate LLM provider
	if c.LLM.Provider != "anthropic" && c.LLM.Provider != "openai" && c.LLM.Provider != "ollama" {
		return fmt.Errorf("invalid llm-provider: %s (must be anthropic, openai, or ollama)", c.LLM.Provider)
	}

	// Set default model if not specified
	if c.LLM.Model == "" {
		switch c.LLM.Provider {
		case "anthropic":
			c.LLM.Model = "claude-sonnet-4-5-20250929"
		case "openai":
			c.LLM.Model = "gpt-4o"
		case "ollama":
			c.LLM.Model = "qwen3-coder:latest"
		}
	}

	return nil
}

// IsProviderConfigured returns true if the provider is supported
// Note: API keys are now configured on the server, not the CLI
func (c *Config) IsProviderConfigured(provider string) bool {
	switch provider {
	case "anthropic", "openai", "ollama":
		return true
	default:
		return false
	}
}

// GetConfiguredProviders returns a list of supported providers
// in priority order: anthropic, openai, ollama
// Note: Actual API key configuration is now done on the server
func (c *Config) GetConfiguredProviders() []string {
	return []string{"anthropic", "openai", "ollama"}
}
