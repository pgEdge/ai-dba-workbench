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

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// parseHeadersEnvVar parses a comma-separated list of KEY=VALUE pairs.
// The first = separates key from value; subsequent = are part of the value.
// Malformed entries (no =) are skipped with a warning.
func parseHeadersEnvVar(envVal string) (map[string]string, error) {
	result := make(map[string]string)
	if envVal == "" {
		return result, nil
	}

	pairs := strings.Split(envVal, ",")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		idx := strings.Index(pair, "=")
		if idx == -1 {
			slog.Warn("skipping malformed header entry", "entry", pair)
			continue
		}
		key := strings.TrimSpace(pair[:idx])
		value := pair[idx+1:]
		if key == "" {
			continue
		}
		result[key] = value
	}
	return result, nil
}

// loadHeadersFromFiles reads header values from the specified file paths.
// Each file should contain the header value. Leading/trailing whitespace
// and newlines are trimmed.
func loadHeadersFromFiles(files map[string]string) (map[string]string, error) {
	result := make(map[string]string)
	for headerName, filePath := range files {
		content, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("reading header %q from file %q: %w", headerName, filePath, err)
		}
		result[headerName] = strings.TrimSpace(string(content))
	}
	return result, nil
}

// mergeHeaders merges two header maps. Values in override take precedence.
func mergeHeaders(base, override map[string]string) map[string]string {
	result := make(map[string]string)
	for k, v := range base {
		result[k] = v
	}
	for k, v := range override {
		result[k] = v
	}
	return result
}

// LoadCustomHeaders merges custom headers from all sources with precedence:
// environment variables > file-based headers > YAML config.
// Empty header values are skipped.
func (c *LLMConfig) LoadCustomHeaders() (map[string]string, error) {
	result := make(map[string]string)

	// Start with YAML headers (lowest precedence)
	for k, v := range c.CustomHeaders {
		if v != "" {
			result[k] = v
		}
	}

	// Load file-based headers (medium precedence)
	fileHeaders, err := loadHeadersFromFiles(c.CustomHeadersFiles)
	if err != nil {
		return nil, err
	}
	for k, v := range fileHeaders {
		if v != "" {
			result[k] = v
		}
	}

	// Load environment variable headers (highest precedence)
	envHeaders, err := parseHeadersEnvVar(os.Getenv("LLM_CUSTOM_HEADERS"))
	if err != nil {
		return nil, err
	}
	for k, v := range envHeaders {
		if v != "" {
			result[k] = v
		}
	}

	if len(result) > 0 {
		var headerNames []string
		for k := range result {
			headerNames = append(headerNames, k)
		}
		slog.Info("loaded custom LLM headers", "count", len(result), "headers", headerNames)
	}

	return result, nil
}

// GetProviderHeaders returns merged global + provider-specific headers.
// Provider headers override global headers when keys conflict.
func (c *LLMConfig) GetProviderHeaders(provider string) (map[string]string, error) {
	// Load global headers
	global, err := c.LoadCustomHeaders()
	if err != nil {
		return nil, err
	}

	// Get provider-specific YAML headers
	var providerYAML map[string]string
	switch provider {
	case "anthropic":
		providerYAML = c.AnthropicCustomHeaders
	case "openai":
		providerYAML = c.OpenAICustomHeaders
	case "gemini":
		providerYAML = c.GeminiCustomHeaders
	case "ollama":
		providerYAML = c.OllamaCustomHeaders
	}

	// Get provider-specific env var
	envVarName := fmt.Sprintf("LLM_%s_CUSTOM_HEADERS", strings.ToUpper(provider))
	providerEnv, err := parseHeadersEnvVar(os.Getenv(envVarName))
	if err != nil {
		return nil, err
	}

	// Merge: global < provider YAML < provider env
	result := mergeHeaders(global, providerYAML)
	result = mergeHeaders(result, providerEnv)

	return result, nil
}
