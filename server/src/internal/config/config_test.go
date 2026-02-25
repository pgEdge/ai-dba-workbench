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
	"os"
	"path/filepath"
	"testing"

	"github.com/pgedge/ai-workbench/pkg/fileutil"
)

func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()

	// Test HTTP defaults
	if cfg.HTTP.Address != ":8080" {
		t.Errorf("Expected default address ':8080', got %s", cfg.HTTP.Address)
	}

	if cfg.HTTP.TLS.Enabled {
		t.Error("Expected TLS to be disabled by default")
	}

	// Test embedding defaults
	if cfg.Embedding.Enabled {
		t.Error("Expected embedding to be disabled by default")
	}
	if cfg.Embedding.Provider != "ollama" {
		t.Errorf("Expected default embedding provider 'ollama', got %s", cfg.Embedding.Provider)
	}

	// Test LLM defaults (LLM proxy is always enabled, no Enabled field)
	if cfg.LLM.MaxTokens != 4096 {
		t.Errorf("Expected default max tokens 4096, got %d", cfg.LLM.MaxTokens)
	}
	if cfg.LLM.Temperature != 0.7 {
		t.Errorf("Expected default temperature 0.7, got %f", cfg.LLM.Temperature)
	}

	// Test knowledgebase defaults
	if cfg.Knowledgebase.Enabled {
		t.Error("Expected knowledgebase to be disabled by default")
	}

	// Test rate limiting defaults
	if cfg.HTTP.Auth.RateLimitWindowMinutes != 15 {
		t.Errorf("Expected rate limit window 15 minutes, got %d", cfg.HTTP.Auth.RateLimitWindowMinutes)
	}
	if cfg.HTTP.Auth.RateLimitMaxAttempts != 10 {
		t.Errorf("Expected rate limit max attempts 10, got %d", cfg.HTTP.Auth.RateLimitMaxAttempts)
	}
}

func TestBuildConnectionString(t *testing.T) {
	tests := []struct {
		name     string
		config   DatabaseConfig
		expected string
	}{
		{
			name: "basic connection",
			config: DatabaseConfig{
				User:     "postgres",
				Host:     "localhost",
				Port:     5432,
				Database: "testdb",
			},
			expected: "postgres://postgres@localhost:5432/testdb",
		},
		{
			name: "with password",
			config: DatabaseConfig{
				User:     "postgres",
				Password: "secret123",
				Host:     "localhost",
				Port:     5432,
				Database: "testdb",
			},
			expected: "postgres://postgres:secret123@localhost:5432/testdb",
		},
		{
			name: "with sslmode",
			config: DatabaseConfig{
				User:     "postgres",
				Host:     "localhost",
				Port:     5432,
				Database: "testdb",
				SSLMode:  "require",
			},
			expected: "postgres://postgres@localhost:5432/testdb?sslmode=require",
		},
		{
			name: "full configuration",
			config: DatabaseConfig{
				User:     "admin",
				Password: "p@ssw0rd",
				Host:     "db.example.com",
				Port:     5433,
				Database: "production",
				SSLMode:  "verify-full",
			},
			expected: "postgres://admin:p@ssw0rd@db.example.com:5433/production?sslmode=verify-full",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.BuildConnectionString()
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestToolsConfig_IsToolEnabled(t *testing.T) {
	falseVal := false
	trueVal := true

	tests := []struct {
		name     string
		config   ToolsConfig
		toolName string
		expected bool
	}{
		{"nil value returns true", ToolsConfig{}, "query_database", true},
		{"explicit true", ToolsConfig{QueryDatabase: &trueVal}, "query_database", true},
		{"explicit false", ToolsConfig{QueryDatabase: &falseVal}, "query_database", false},
		{"unknown tool returns true", ToolsConfig{}, "unknown_tool", true},
		{"get_schema_info nil", ToolsConfig{}, "get_schema_info", true},
		{"similarity_search nil", ToolsConfig{}, "similarity_search", true},
		{"execute_explain nil", ToolsConfig{}, "execute_explain", true},
		{"generate_embedding nil", ToolsConfig{}, "generate_embedding", true},
		{"search_knowledgebase nil", ToolsConfig{}, "search_knowledgebase", true},
		{"count_rows nil", ToolsConfig{}, "count_rows", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.IsToolEnabled(tt.toolName)
			if result != tt.expected {
				t.Errorf("IsToolEnabled(%q): expected %v, got %v", tt.toolName, tt.expected, result)
			}
		})
	}
}

func TestResourcesConfig_IsResourceEnabled(t *testing.T) {
	falseVal := false
	trueVal := true

	tests := []struct {
		name        string
		config      ResourcesConfig
		resourceURI string
		expected    bool
	}{
		{"nil value returns true", ResourcesConfig{}, "pg://system_info", true},
		{"explicit true", ResourcesConfig{SystemInfo: &trueVal}, "pg://system_info", true},
		{"explicit false", ResourcesConfig{SystemInfo: &falseVal}, "pg://system_info", false},
		{"unknown resource returns true", ResourcesConfig{}, "pg://unknown", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.IsResourceEnabled(tt.resourceURI)
			if result != tt.expected {
				t.Errorf("IsResourceEnabled(%q): expected %v, got %v", tt.resourceURI, tt.expected, result)
			}
		})
	}
}

func TestPromptsConfig_IsPromptEnabled(t *testing.T) {
	// PromptsConfig currently has no built-in prompts; this test verifies
	// the infrastructure returns true for any prompt (future prompts will be enabled by default)
	tests := []struct {
		name       string
		config     PromptsConfig
		promptName string
		expected   bool
	}{
		{"any prompt returns true", PromptsConfig{}, "any-prompt", true},
		{"unknown prompt returns true", PromptsConfig{}, "unknown-prompt", true},
		{"future prompt returns true", PromptsConfig{}, "future-prompt", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.IsPromptEnabled(tt.promptName)
			if result != tt.expected {
				t.Errorf("IsPromptEnabled(%q): expected %v, got %v", tt.promptName, tt.expected, result)
			}
		})
	}
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid config",
			config: &Config{
				HTTP: HTTPConfig{Address: ":8080"},
			},
			expectError: false,
		},
		{
			name: "TLS without cert file",
			config: &Config{
				HTTP: HTTPConfig{
					TLS: TLSConfig{Enabled: true, KeyFile: "key.pem"},
				},
			},
			expectError: true,
			errorMsg:    "certificate file is required",
		},
		{
			name: "TLS without key file",
			config: &Config{
				HTTP: HTTPConfig{
					TLS: TLSConfig{Enabled: true, CertFile: "cert.pem"},
				},
			},
			expectError: true,
			errorMsg:    "key file is required",
		},
		{
			name: "database without user",
			config: &Config{
				HTTP: HTTPConfig{Address: ":8080"},
				Database: &DatabaseConfig{
					Host: "localhost",
					User: "",
				},
			},
			expectError: true,
			errorMsg:    "user is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.config)
			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errorMsg != "" && !contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestReadOptionalTrimmedFile(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()

	// Test reading valid file
	keyFile := filepath.Join(tmpDir, "api_key.txt")
	if err := os.WriteFile(keyFile, []byte("  test-api-key-123  \n"), 0600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	key, err := fileutil.ReadOptionalTrimmedFile(keyFile)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if key != "test-api-key-123" {
		t.Errorf("expected 'test-api-key-123', got %q", key)
	}

	// Test empty path
	key, err = fileutil.ReadOptionalTrimmedFile("")
	if err != nil {
		t.Errorf("unexpected error for empty path: %v", err)
	}
	if key != "" {
		t.Errorf("expected empty string for empty path, got %q", key)
	}

	// Test non-existent file (should return empty, not error)
	key, err = fileutil.ReadOptionalTrimmedFile(filepath.Join(tmpDir, "nonexistent.txt"))
	if err != nil {
		t.Errorf("unexpected error for non-existent file: %v", err)
	}
	if key != "" {
		t.Errorf("expected empty string for non-existent file, got %q", key)
	}
}

func TestConfigFileExists(t *testing.T) {
	tmpDir := t.TempDir()

	// Test existing file
	existingFile := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(existingFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	if !ConfigFileExists(existingFile) {
		t.Error("expected ConfigFileExists to return true for existing file")
	}

	// Test non-existent file
	if ConfigFileExists(filepath.Join(tmpDir, "nonexistent.yaml")) {
		t.Error("expected ConfigFileExists to return false for non-existent file")
	}
}

func TestSaveConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "subdir", "config.yaml")

	cfg := &Config{
		HTTP: HTTPConfig{
			Address: ":9090",
		},
		Database: &DatabaseConfig{
			Host: "localhost",
			Port: 5432,
			User: "testuser",
		},
	}

	// Test saving config (should create directory)
	if err := SaveConfig(configPath, cfg); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	// Verify file exists
	if !ConfigFileExists(configPath) {
		t.Error("config file should exist after save")
	}

	// Load and verify
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read saved config: %v", err)
	}
	if len(data) == 0 {
		t.Error("saved config file is empty")
	}
}

func TestLoadConfigWithTempFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create a minimal valid config file
	configContent := `
http:
    address: ":9000"
database:
    host: localhost
    port: 5432
    user: testuser
    database: test
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	// Load config
	flags := CLIFlags{ConfigFileSet: true, ConfigFile: configPath}
	cfg, err := LoadConfig(configPath, flags)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	// Verify loaded values
	if cfg.HTTP.Address != ":9000" {
		t.Errorf("expected address ':9000', got %q", cfg.HTTP.Address)
	}
	if cfg.Database == nil {
		t.Fatal("expected database config to be loaded")
	}
	if cfg.Database.Host != "localhost" {
		t.Errorf("expected database host 'localhost', got %q", cfg.Database.Host)
	}
}

func TestLoadConfigNonExistentFile(t *testing.T) {
	// Test with ConfigFileSet=true (should error)
	flags := CLIFlags{ConfigFileSet: true, ConfigFile: "/nonexistent/config.yaml"}
	_, err := LoadConfig("/nonexistent/config.yaml", flags)
	if err == nil {
		t.Error("expected error for non-existent config file with ConfigFileSet=true")
	}

	// Test with ConfigFileSet=false (should use defaults)
	// Disable auth to avoid token file validation error
	flags = CLIFlags{ConfigFileSet: false}
	cfg, err := LoadConfig("/nonexistent/config.yaml", flags)
	if err != nil {
		t.Errorf("unexpected error for non-existent config file with ConfigFileSet=false: %v", err)
	}
	if cfg == nil {
		t.Error("expected config to be returned")
	}
}

func TestGetDefaultConfigPath(t *testing.T) {
	// Test with a known binary path
	result := GetDefaultConfigPath("/usr/local/bin/pgedge-postgres-mcp")

	// If system path exists, it would return that instead
	// Just check that we get a .yaml file
	if filepath.Ext(result) != ".yaml" {
		t.Errorf("expected .yaml extension, got %q", result)
	}
}

func TestGetDefaultSecretPath(t *testing.T) {
	result := GetDefaultSecretPath("/usr/local/bin/pgedge-postgres-mcp")

	// If system path exists, it would return that instead
	// Just check that we get a .secret file
	if filepath.Ext(result) != ".secret" {
		t.Errorf("expected .secret extension, got %q", result)
	}
}

func TestMemoryEnabledByDefault(t *testing.T) {
	cfg := defaultConfig()
	if !cfg.Memory.Enabled {
		t.Error("Expected memory to be enabled by default")
	}
}

func TestMemoryEnabledEnvVar(t *testing.T) {
	// Save and restore the env var
	origVal, origSet := os.LookupEnv("PGEDGE_MEMORY_ENABLED")
	defer func() {
		if origSet {
			os.Setenv("PGEDGE_MEMORY_ENABLED", origVal)
		} else {
			os.Unsetenv("PGEDGE_MEMORY_ENABLED")
		}
	}()

	// Test disabling via env var
	os.Setenv("PGEDGE_MEMORY_ENABLED", "false")
	cfg, err := LoadConfig("", CLIFlags{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Memory.Enabled {
		t.Error("Expected memory to be disabled when PGEDGE_MEMORY_ENABLED=false")
	}

	// Test enabling via env var
	os.Setenv("PGEDGE_MEMORY_ENABLED", "true")
	cfg, err = LoadConfig("", CLIFlags{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Memory.Enabled {
		t.Error("Expected memory to be enabled when PGEDGE_MEMORY_ENABLED=true")
	}

	// Test that invalid values are ignored (default remains)
	os.Setenv("PGEDGE_MEMORY_ENABLED", "invalid")
	cfg, err = LoadConfig("", CLIFlags{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Memory.Enabled {
		t.Error("Expected memory to remain enabled when PGEDGE_MEMORY_ENABLED has invalid value")
	}
}

func TestMergeConfig(t *testing.T) {
	dest := defaultConfig()
	src := &Config{
		HTTP: HTTPConfig{
			Address: ":9090",
		},
		Database: &DatabaseConfig{
			Host: "newhost",
			Port: 5432,
			User: "newuser",
		},
		SecretFile: "/new/secret",
	}

	mergeConfig(dest, src)

	if dest.HTTP.Address != ":9090" {
		t.Errorf("expected address ':9090', got %q", dest.HTTP.Address)
	}
	if dest.Database == nil || dest.Database.Host != "newhost" {
		t.Error("expected database to be merged")
	}
	if dest.SecretFile != "/new/secret" {
		t.Errorf("expected SecretFile '/new/secret', got %q", dest.SecretFile)
	}
}

func TestApplyCLIFlags(t *testing.T) {
	cfg := defaultConfig()
	flags := CLIFlags{
		HTTPAddrSet: true,
		HTTPAddr:    ":7070",
		DBUserSet:   true,
		DBUser:      "cliuser",
	}

	applyCLIFlags(cfg, flags)

	if cfg.HTTP.Address != ":7070" {
		t.Errorf("expected address ':7070', got %q", cfg.HTTP.Address)
	}
	// Database should be created when DB flags are set
	if cfg.Database == nil {
		t.Fatal("expected database to be created")
	}
	if cfg.Database.User != "cliuser" {
		t.Errorf("expected user 'cliuser', got %q", cfg.Database.User)
	}
}
