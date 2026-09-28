/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/config"
)

// TestNewLLMProxyConfigTemperature guards issue #551 end to end: the
// temperature loaded from the config file must reach the llmproxy
// configuration, with an explicit 0 kept and an unset value falling
// back to the default.
func TestNewLLMProxyConfigTemperature(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want float64
	}{
		{name: "unset uses default", yaml: "llm:\n    provider: ollama\n", want: 0.7},
		{name: "explicit zero is kept", yaml: "llm:\n    temperature: 0\n", want: 0},
		{name: "explicit non-zero", yaml: "llm:\n    temperature: 0.4\n", want: 0.4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(configPath, []byte(tt.yaml), 0600); err != nil {
				t.Fatalf("failed to write config file: %v", err)
			}
			cfg, err := config.LoadConfig(configPath, config.CLIFlags{ConfigFileSet: true, ConfigFile: configPath})
			if err != nil {
				t.Fatalf("failed to load config: %v", err)
			}
			got := newLLMProxyConfig(&cfg.LLM)
			if got.Temperature == nil || *got.Temperature != tt.want {
				t.Errorf("Temperature = %v, want %v", got.Temperature, tt.want)
			}
		})
	}
}

func TestNewLLMProxyConfigCopiesFields(t *testing.T) {
	llm := &config.LLMConfig{
		Provider:         "anthropic",
		Model:            "test-model",
		AnthropicAPIKey:  "ak",
		AnthropicBaseURL: "https://anthropic.example.com",
		OpenAIAPIKey:     "ok",
		OpenAIBaseURL:    "https://openai.example.com",
		GeminiAPIKey:     "gk",
		GeminiBaseURL:    "https://gemini.example.com",
		OllamaURL:        "http://ollama.example.com",
		MaxTokens:        1234,
	}
	got := newLLMProxyConfig(llm)
	if got.Provider != "anthropic" || got.Model != "test-model" ||
		got.AnthropicAPIKey != "ak" || got.AnthropicBaseURL != "https://anthropic.example.com" ||
		got.OpenAIAPIKey != "ok" || got.OpenAIBaseURL != "https://openai.example.com" ||
		got.GeminiAPIKey != "gk" || got.GeminiBaseURL != "https://gemini.example.com" ||
		got.OllamaURL != "http://ollama.example.com" || got.MaxTokens != 1234 {
		t.Errorf("fields not copied: %+v", got)
	}
	if got.LLMConfig != llm {
		t.Error("LLMConfig must point at the source configuration")
	}
	if got.UseCompactDescriptions != llm.UseCompactDescriptions() {
		t.Error("UseCompactDescriptions not resolved from the source configuration")
	}
	if got.MemoryStore != nil || got.AuthStore != nil || got.CompactDescriptions != nil {
		t.Error("path-specific fields must be left for the caller to set")
	}
}
