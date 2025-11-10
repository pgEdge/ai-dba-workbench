/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// CLIConfig stores user preferences for the CLI
type CLIConfig struct {
	// AI Workbench MCP server URL
	ServerURL string `json:"server_url,omitempty"`

	// Preferred LLM provider: "anthropic" or "ollama"
	// Empty string means auto-detect (prefer Anthropic if API key is set)
	PreferredLLM string `json:"preferred_llm,omitempty"`

	// Anthropic configuration
	AnthropicAPIKey string `json:"anthropic_api_key,omitempty"`
	AnthropicModel  string `json:"anthropic_model,omitempty"`

	// Ollama configuration
	OllamaURL   string `json:"ollama_url,omitempty"`
	OllamaModel string `json:"ollama_model,omitempty"`
}

// getConfigPath returns the path to the CLI config file
func getConfigPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".ai-workbench-cli.json"), nil
}

// loadCLIConfig loads the CLI configuration from disk
func loadCLIConfig() (*CLIConfig, error) {
	configPath, err := getConfigPath()
	if err != nil {
		return nil, err
	}

	// If config file doesn't exist, return empty config
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return &CLIConfig{}, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config CLIConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}

// saveCLIConfig saves the CLI configuration to disk
func saveCLIConfig(config *CLIConfig) error {
	configPath, err := getConfigPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(config, "", "    ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// applyConfigToLLMConfig applies CLI config preferences to LLM config
// Priority: AI_CLI_* env vars > config file > defaults
func applyConfigToLLMConfig(cliConfig *CLIConfig, llmConfig *LLMConfig) {
	// Apply Anthropic API key from config if no AI_CLI_ env var is set
	if os.Getenv("AI_CLI_ANTHROPIC_API_KEY") != "" {
		llmConfig.AnthropicKey = os.Getenv("AI_CLI_ANTHROPIC_API_KEY")
	} else if cliConfig.AnthropicAPIKey != "" {
		llmConfig.AnthropicKey = cliConfig.AnthropicAPIKey
	}

	// Apply Anthropic model
	if os.Getenv("AI_CLI_ANTHROPIC_MODEL") != "" {
		llmConfig.AnthropicModel = os.Getenv("AI_CLI_ANTHROPIC_MODEL")
	} else if cliConfig.AnthropicModel != "" {
		llmConfig.AnthropicModel = cliConfig.AnthropicModel
	}

	// Apply Ollama URL
	if os.Getenv("AI_CLI_OLLAMA_URL") != "" {
		llmConfig.OllamaURL = os.Getenv("AI_CLI_OLLAMA_URL")
	} else if cliConfig.OllamaURL != "" {
		llmConfig.OllamaURL = cliConfig.OllamaURL
	}

	// Apply Ollama model
	if os.Getenv("AI_CLI_OLLAMA_MODEL") != "" {
		llmConfig.OllamaModel = os.Getenv("AI_CLI_OLLAMA_MODEL")
	} else if cliConfig.OllamaModel != "" {
		llmConfig.OllamaModel = cliConfig.OllamaModel
	}

	// Apply preferred LLM if set
	if cliConfig.PreferredLLM != "" {
		llmConfig.Provider = cliConfig.PreferredLLM
	}
}
