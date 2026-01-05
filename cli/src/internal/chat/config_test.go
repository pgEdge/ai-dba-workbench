/*-------------------------------------------------------------------------
*
 * pgEdge Natural Language Agent
*
* Portions copyright (c) 2025 - 2026, pgEdge, Inc.
* This software is released under The PostgreSQL License
*
*-------------------------------------------------------------------------
*/

package chat

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_Defaults(t *testing.T) {
	// Load config with no file
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Check defaults
	if cfg.LLM.Provider != "anthropic" {
		t.Errorf("Expected LLM provider 'anthropic', got '%s'", cfg.LLM.Provider)
	}

	if cfg.LLM.MaxTokens != 4096 {
		t.Errorf("Expected MaxTokens 4096, got %d", cfg.LLM.MaxTokens)
	}

	if cfg.LLM.Temperature != 0.7 {
		t.Errorf("Expected Temperature 0.7, got %f", cfg.LLM.Temperature)
	}
}

func TestLoadConfig_File(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.yaml")

	configContent := `
mcp:
  url: http://test.example.com:8080
  token: test-token

llm:
  provider: ollama
  model: test-model
  ollama_url: http://localhost:11434
  max_tokens: 2048
  temperature: 0.5

ui:
  no_color: true
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Load config from file
	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Check file values
	if cfg.MCP.URL != "http://test.example.com:8080" {
		t.Errorf("Expected MCP URL 'http://test.example.com:8080', got '%s'", cfg.MCP.URL)
	}

	if cfg.LLM.Provider != "ollama" {
		t.Errorf("Expected LLM provider 'ollama', got '%s'", cfg.LLM.Provider)
	}

	if cfg.LLM.Model != "test-model" {
		t.Errorf("Expected LLM model 'test-model', got '%s'", cfg.LLM.Model)
	}

	if cfg.LLM.MaxTokens != 2048 {
		t.Errorf("Expected MaxTokens 2048, got %d", cfg.LLM.MaxTokens)
	}

	if cfg.LLM.Temperature != 0.5 {
		t.Errorf("Expected Temperature 0.5, got %f", cfg.LLM.Temperature)
	}

	if !cfg.UI.NoColor {
		t.Error("Expected NoColor to be true")
	}
}

func TestValidate_HTTPMode(t *testing.T) {
	cfg := &Config{
		MCP: MCPConfig{
			URL:      "http://localhost:8080",
			AuthMode: "token",
		},
		LLM: LLMConfig{
			Provider: "ollama",
			Model:    "llama3",
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate failed: %v", err)
	}
}

func TestValidate_MissingURL(t *testing.T) {
	cfg := &Config{
		MCP: MCPConfig{
			// URL is missing
		},
		LLM: LLMConfig{
			Provider:        "anthropic",
			AnthropicAPIKey: "test-key",
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Error("Expected validation error for missing URL")
	}
}

func TestValidate_InvalidProvider(t *testing.T) {
	cfg := &Config{
		MCP: MCPConfig{
			URL: "http://localhost:8080",
		},
		LLM: LLMConfig{
			Provider: "invalid",
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Error("Expected validation error for invalid provider")
	}
}

func TestValidate_MissingAPIKey(t *testing.T) {
	cfg := &Config{
		MCP: MCPConfig{
			URL: "http://localhost:8080",
		},
		LLM: LLMConfig{
			Provider: "anthropic",
			// APIKey is missing
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Error("Expected validation error for missing API key for Anthropic")
	}
}
