/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package config

import "testing"

// TestUseCompactDescriptions covers the explicit settings and the "auto"
// mode, which resolves the configured provider's endpoint and enables
// compact descriptions only when that endpoint is a loopback address.
func TestUseCompactDescriptions(t *testing.T) {
	tests := []struct {
		name string
		cfg  LLMConfig
		want bool
	}{
		{"explicit true", LLMConfig{CompactToolDescriptions: "TRUE", Provider: "anthropic"}, true},
		{"explicit false", LLMConfig{CompactToolDescriptions: "false", Provider: "ollama"}, false},
		{"auto ollama default URL", LLMConfig{CompactToolDescriptions: "auto", Provider: "ollama"}, true},
		{"empty ollama remote URL", LLMConfig{Provider: "ollama", OllamaURL: "http://ollama.example.com:11434"}, false},
		{"openai default URL", LLMConfig{Provider: "openai"}, false},
		{"openai localhost base URL", LLMConfig{Provider: "openai", OpenAIBaseURL: "http://localhost:8000/v1"}, true},
		{"anthropic default URL", LLMConfig{Provider: "anthropic"}, false},
		{"anthropic loopback base URL", LLMConfig{Provider: "anthropic", AnthropicBaseURL: "http://127.0.0.1:9000"}, true},
		{"gemini default URL", LLMConfig{Provider: "gemini"}, false},
		{"gemini IPv6 loopback base URL", LLMConfig{Provider: "gemini", GeminiBaseURL: "http://[::1]:8080"}, true},
		{"unknown provider", LLMConfig{Provider: "unknown"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.UseCompactDescriptions(); got != tt.want {
				t.Errorf("UseCompactDescriptions() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestIsLocalhostURL covers each branch of the loopback check.
func TestIsLocalhostURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"empty", "", false},
		{"unparseable", "http://[::1", false},
		{"localhost mixed case", "http://LocalHost:11434", true},
		{"127 prefix", "http://127.0.0.2:8080", true},
		{"IPv6 loopback", "http://[::1]:8080", true},
		{"unspecified IPv4", "http://0.0.0.0:8080", true},
		{"public IP", "http://192.0.2.1:8080", false},
		{"hostname", "https://api.example.com/v1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isLocalhostURL(tt.url); got != tt.want {
				t.Errorf("isLocalhostURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}
