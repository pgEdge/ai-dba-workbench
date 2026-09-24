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
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pgedge/ai-workbench/pkg/fileutil"
	"gopkg.in/yaml.v3"
)

// writeTempConfig writes content to a config.yaml file inside a fresh
// t.TempDir() and returns its path.
func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}
	return path
}

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
	if cfg.LLM.TimeoutSeconds != 120 {
		t.Errorf("Expected default LLM timeout 120 seconds, got %d", cfg.LLM.TimeoutSeconds)
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
			expected: "postgres://admin:p%40ssw0rd@db.example.com:5433/production?sslmode=verify-full",
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

// TestBuildConnectionStringRoundTrip is the authoritative correctness
// check for userinfo encoding. For each password containing characters
// with special meaning in a URL userinfo component, it builds the DSN,
// parses it back with url.Parse, and asserts the recovered user and
// password match the originals byte-for-byte. This proves the encoding
// is correct regardless of the exact percent-encoded representation.
func TestBuildConnectionStringRoundTrip(t *testing.T) {
	const user = "admin@corp"

	passwords := []string{
		"p@ssw0rd",
		"p:ss/w?rd",
		"pa ss",
		"p%40 word",
		"p#ss&w=rd",
	}

	for _, pw := range passwords {
		pw := pw
		t.Run(pw, func(t *testing.T) {
			cfg := DatabaseConfig{
				User:     user,
				Password: pw,
				Host:     "db.example.com",
				Port:     5432,
				Database: "production",
				SSLMode:  "require",
			}

			dsn := cfg.BuildConnectionString()

			parsed, err := url.Parse(dsn)
			if err != nil {
				t.Fatalf("url.Parse(%q) returned error: %v", dsn, err)
			}

			if got := parsed.User.Username(); got != user {
				t.Errorf("username = %q, want %q (dsn=%q)", got, user, dsn)
			}

			gotPW, hasPW := parsed.User.Password()
			if !hasPW {
				t.Fatalf("parsed DSN has no password component (dsn=%q)", dsn)
			}
			if gotPW != pw {
				t.Errorf("password = %q, want %q (dsn=%q)", gotPW, pw, dsn)
			}

			// pgconn.ParseConfig validates the DSN the way pgx itself
			// does; it parses only and never opens a connection, so it
			// is safe to run without a database.
			if _, err := pgconn.ParseConfig(dsn); err != nil {
				t.Errorf("pgconn.ParseConfig(%q) returned error: %v", dsn, err)
			}
		})
	}
}

// TestBuildConnectionStringNoPassword confirms that an empty effective
// password yields a DSN carrying the username but no password component,
// preserving pgx's .pgpass fallback, and that the DSN remains parseable.
func TestBuildConnectionStringNoPassword(t *testing.T) {
	cfg := DatabaseConfig{
		User:     "admin@corp",
		Host:     "db.example.com",
		Port:     5432,
		Database: "production",
	}

	dsn := cfg.BuildConnectionString()

	if strings.Contains(dsn, ":@") {
		t.Errorf("DSN must not contain an empty password component: %q", dsn)
	}

	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("url.Parse(%q) returned error: %v", dsn, err)
	}

	if got := parsed.User.Username(); got != cfg.User {
		t.Errorf("username = %q, want %q (dsn=%q)", got, cfg.User, dsn)
	}

	if _, hasPW := parsed.User.Password(); hasPW {
		t.Errorf("expected no password component, but one was present: %q", dsn)
	}

	if _, err := pgconn.ParseConfig(dsn); err != nil {
		t.Errorf("pgconn.ParseConfig(%q) returned error: %v", dsn, err)
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

// TestLoadAPIKeysFromFilesNotConfigured verifies that leaving every
// *_file path empty is not an error and leaves the keys untouched.
func TestLoadAPIKeysFromFilesNotConfigured(t *testing.T) {
	cfg := &Config{}
	if err := loadAPIKeysFromFiles(cfg); err != nil {
		t.Fatalf("loadAPIKeysFromFiles with no files: %v", err)
	}
	if cfg.Embedding.VoyageAPIKey != "" || cfg.LLM.AnthropicAPIKey != "" {
		t.Error("expected keys to remain empty when no files configured")
	}
}

// TestLoadAPIKeysFromFilesMissingErrors verifies that a configured
// key file that cannot be read now produces a propagated error rather
// than being silently swallowed. Each provider field is exercised so
// every error branch is covered.
func TestLoadAPIKeysFromFilesMissingErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.key")

	cases := []struct {
		name  string
		apply func(*Config)
	}{
		{"embedding voyage", func(c *Config) { c.Embedding.VoyageAPIKeyFile = missing }},
		{"embedding openai", func(c *Config) { c.Embedding.OpenAIAPIKeyFile = missing }},
		{"embedding gemini", func(c *Config) { c.Embedding.GeminiAPIKeyFile = missing }},
		{"llm anthropic", func(c *Config) { c.LLM.AnthropicAPIKeyFile = missing }},
		{"llm openai", func(c *Config) { c.LLM.OpenAIAPIKeyFile = missing }},
		{"llm gemini", func(c *Config) { c.LLM.GeminiAPIKeyFile = missing }},
		{"kb voyage", func(c *Config) { c.Knowledgebase.EmbeddingVoyageAPIKeyFile = missing }},
		{"kb openai", func(c *Config) { c.Knowledgebase.EmbeddingOpenAIAPIKeyFile = missing }},
		{"kb gemini", func(c *Config) { c.Knowledgebase.EmbeddingGeminiAPIKeyFile = missing }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{}
			tc.apply(cfg)
			if err := loadAPIKeysFromFiles(cfg); err == nil {
				t.Errorf("expected error for missing %s key file", tc.name)
			}
		})
	}
}

// TestLoadAPIKeysFromFilesAllProviders verifies every provider's
// success-assignment branch loads the key from disk when a path is
// configured and the field is otherwise empty.
func TestLoadAPIKeysFromFilesAllProviders(t *testing.T) {
	tmpDir := t.TempDir()
	write := func(name, value string) string {
		p := filepath.Join(tmpDir, name)
		if err := os.WriteFile(p, []byte(value+"\n"), 0600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return p
	}

	cfg := &Config{
		Embedding: EmbeddingConfig{
			VoyageAPIKeyFile: write("e-voyage", "e-voyage-key"),
			OpenAIAPIKeyFile: write("e-openai", "e-openai-key"),
			GeminiAPIKeyFile: write("e-gemini", "e-gemini-key"),
		},
		LLM: LLMConfig{
			AnthropicAPIKeyFile: write("l-anthropic", "l-anthropic-key"),
			OpenAIAPIKeyFile:    write("l-openai", "l-openai-key"),
			GeminiAPIKeyFile:    write("l-gemini", "l-gemini-key"),
		},
		Knowledgebase: KnowledgebaseConfig{
			EmbeddingVoyageAPIKeyFile: write("kb-voyage", "kb-voyage-key"),
			EmbeddingOpenAIAPIKeyFile: write("kb-openai", "kb-openai-key"),
			EmbeddingGeminiAPIKeyFile: write("kb-gemini", "kb-gemini-key"),
		},
	}

	if err := loadAPIKeysFromFiles(cfg); err != nil {
		t.Fatalf("loadAPIKeysFromFiles: %v", err)
	}

	checks := []struct {
		name string
		got  string
		want string
	}{
		{"Embedding.VoyageAPIKey", cfg.Embedding.VoyageAPIKey, "e-voyage-key"},
		{"Embedding.OpenAIAPIKey", cfg.Embedding.OpenAIAPIKey, "e-openai-key"},
		{"Embedding.GeminiAPIKey", cfg.Embedding.GeminiAPIKey, "e-gemini-key"},
		{"LLM.AnthropicAPIKey", cfg.LLM.AnthropicAPIKey, "l-anthropic-key"},
		{"LLM.OpenAIAPIKey", cfg.LLM.OpenAIAPIKey, "l-openai-key"},
		{"LLM.GeminiAPIKey", cfg.LLM.GeminiAPIKey, "l-gemini-key"},
		{"Knowledgebase.EmbeddingVoyageAPIKey",
			cfg.Knowledgebase.EmbeddingVoyageAPIKey, "kb-voyage-key"},
		{"Knowledgebase.EmbeddingOpenAIAPIKey",
			cfg.Knowledgebase.EmbeddingOpenAIAPIKey, "kb-openai-key"},
		{"Knowledgebase.EmbeddingGeminiAPIKey",
			cfg.Knowledgebase.EmbeddingGeminiAPIKey, "kb-gemini-key"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

// TestLoadAPIKeysFromFilesEmptyErrors verifies that a configured but
// empty key file is now reported as an error.
func TestLoadAPIKeysFromFilesEmptyErrors(t *testing.T) {
	emptyPath := filepath.Join(t.TempDir(), "empty.key")
	if err := os.WriteFile(emptyPath, []byte("\n"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg := &Config{Embedding: EmbeddingConfig{VoyageAPIKeyFile: emptyPath}}
	if err := loadAPIKeysFromFiles(cfg); err == nil {
		t.Error("expected error for empty key file")
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

// TestGetDefaultConfigPath verifies the wrapper returns "" when
// neither the per-user config dir nor /etc/pgedge holds a server
// config file. The system-wide fallback is redirected at a
// directory guaranteed not to exist so the assertion is exact and
// not influenced by whatever the test host has in /etc/pgedge.
func TestGetDefaultConfigPath(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("HOME", base)
	t.Setenv("AppData", base)
	fileutil.SetSystemConfigDirForTest(t, filepath.Join(base, "absent-etc-pgedge"))

	result := GetDefaultConfigPath("/usr/local/bin/pgedge-postgres-mcp")
	if result != "" {
		t.Errorf("expected empty path, got %q", result)
	}
}

// TestGetDefaultConfigPath_UserDirHit verifies the wrapper returns
// the per-user config path when one is present.
func TestGetDefaultConfigPath_UserDirHit(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("HOME", base)
	t.Setenv("AppData", base)

	userDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("os.UserConfigDir() error: %v", err)
	}
	pgedgeDir := filepath.Join(userDir, "pgedge")
	if err := os.MkdirAll(pgedgeDir, 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	expected := filepath.Join(pgedgeDir, "ai-dba-server.yaml")
	if err := os.WriteFile(expected, []byte("http:\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if got := GetDefaultConfigPath(""); got != expected {
		t.Errorf("GetDefaultConfigPath = %q, want %q", got, expected)
	}
}

// TestGetDefaultSecretPath verifies the wrapper returns "" when no
// candidate secret file is present. As with TestGetDefaultConfigPath
// the system fallback is redirected so the assertion is exact.
func TestGetDefaultSecretPath(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("HOME", base)
	t.Setenv("AppData", base)
	fileutil.SetSystemConfigDirForTest(t, filepath.Join(base, "absent-etc-pgedge"))

	result := GetDefaultSecretPath()
	if result != "" {
		t.Errorf("expected empty path, got %q", result)
	}
}

// TestGetDefaultSecretPath_UserDirHit verifies the wrapper returns
// the per-user secret path when one is present.
func TestGetDefaultSecretPath_UserDirHit(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("HOME", base)
	t.Setenv("AppData", base)

	userDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("os.UserConfigDir() error: %v", err)
	}
	pgedgeDir := filepath.Join(userDir, "pgedge")
	if err := os.MkdirAll(pgedgeDir, 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	expected := filepath.Join(pgedgeDir, "ai-dba-server.secret")
	if err := os.WriteFile(expected, []byte("super-secret"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if got := GetDefaultSecretPath(); got != expected {
		t.Errorf("GetDefaultSecretPath = %q, want %q", got, expected)
	}
}

func TestMemoryEnabledByDefault(t *testing.T) {
	cfg := defaultConfig()
	if !cfg.Memory.IsEnabled() {
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
	if cfg.Memory.IsEnabled() {
		t.Error("Expected memory to be disabled when PGEDGE_MEMORY_ENABLED=false")
	}

	// Test enabling via env var
	os.Setenv("PGEDGE_MEMORY_ENABLED", "true")
	cfg, err = LoadConfig("", CLIFlags{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Memory.IsEnabled() {
		t.Error("Expected memory to be enabled when PGEDGE_MEMORY_ENABLED=true")
	}

	// Test that invalid values are ignored (default remains)
	os.Setenv("PGEDGE_MEMORY_ENABLED", "invalid")
	cfg, err = LoadConfig("", CLIFlags{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Memory.IsEnabled() {
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

func TestLoadConfigDataDir(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name        string
		setupFile   func() string // returns file path
		expected    string
		expectError bool
	}{
		{
			name: "config with data_dir",
			setupFile: func() string {
				path := filepath.Join(tmpDir, "with_data_dir.yaml")
				content := `
http:
    address: ":8080"
data_dir: /custom/data/path
`
				if err := os.WriteFile(path, []byte(content), 0644); err != nil {
					t.Fatalf("failed to write test file: %v", err)
				}
				return path
			},
			expected:    "/custom/data/path",
			expectError: false,
		},
		{
			name: "config without data_dir",
			setupFile: func() string {
				path := filepath.Join(tmpDir, "without_data_dir.yaml")
				content := `
http:
    address: ":8080"
`
				if err := os.WriteFile(path, []byte(content), 0644); err != nil {
					t.Fatalf("failed to write test file: %v", err)
				}
				return path
			},
			expected:    "",
			expectError: false,
		},
		{
			name: "non-existent file",
			setupFile: func() string {
				return filepath.Join(tmpDir, "nonexistent.yaml")
			},
			expected:    "",
			expectError: false,
		},
		{
			name: "empty path",
			setupFile: func() string {
				return ""
			},
			expected:    "",
			expectError: false,
		},
		{
			name: "empty file",
			setupFile: func() string {
				path := filepath.Join(tmpDir, "empty.yaml")
				if err := os.WriteFile(path, []byte(""), 0644); err != nil {
					t.Fatalf("failed to write test file: %v", err)
				}
				return path
			},
			expected:    "",
			expectError: false,
		},
		{
			name: "invalid YAML",
			setupFile: func() string {
				path := filepath.Join(tmpDir, "invalid.yaml")
				content := `
this: is: not: valid: yaml: {{{{
`
				if err := os.WriteFile(path, []byte(content), 0644); err != nil {
					t.Fatalf("failed to write test file: %v", err)
				}
				return path
			},
			expected:    "",
			expectError: true,
		},
		{
			name: "data_dir with relative path",
			setupFile: func() string {
				path := filepath.Join(tmpDir, "relative_data_dir.yaml")
				content := `
data_dir: ./relative/path
`
				if err := os.WriteFile(path, []byte(content), 0644); err != nil {
					t.Fatalf("failed to write test file: %v", err)
				}
				return path
			},
			expected:    "./relative/path",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.setupFile()
			result, err := LoadConfigDataDir(path)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result != tt.expected {
					t.Errorf("expected %q, got %q", tt.expected, result)
				}
			}
		})
	}
}

// TestMergeConfigGeminiEmbedding verifies that the merge logic copies
// the new Gemini fields from the source EmbeddingConfig to the
// destination, mirroring the behavior for Voyage and OpenAI.
func TestMergeConfigGeminiEmbedding(t *testing.T) {
	dest := defaultConfig()
	src := &Config{
		Embedding: EmbeddingConfig{
			Enabled:          true,
			Provider:         "gemini",
			Model:            "gemini-embedding-001",
			GeminiAPIKey:     "test-gemini-key",
			GeminiAPIKeyFile: "/path/to/gemini-key",
			GeminiBaseURL:    "https://gemini.example.com",
		},
	}

	mergeConfig(dest, src)

	if dest.Embedding.Provider != "gemini" {
		t.Errorf("expected provider 'gemini', got %q", dest.Embedding.Provider)
	}
	if dest.Embedding.Model != "gemini-embedding-001" {
		t.Errorf("expected model 'gemini-embedding-001', got %q", dest.Embedding.Model)
	}
	if dest.Embedding.GeminiAPIKey != "test-gemini-key" {
		t.Errorf("expected GeminiAPIKey 'test-gemini-key', got %q",
			dest.Embedding.GeminiAPIKey)
	}
	if dest.Embedding.GeminiAPIKeyFile != "/path/to/gemini-key" {
		t.Errorf("expected GeminiAPIKeyFile '/path/to/gemini-key', got %q",
			dest.Embedding.GeminiAPIKeyFile)
	}
	if dest.Embedding.GeminiBaseURL != "https://gemini.example.com" {
		t.Errorf("expected GeminiBaseURL 'https://gemini.example.com', got %q",
			dest.Embedding.GeminiBaseURL)
	}
}

// TestMergeConfigGeminiKnowledgebase verifies that the merge logic
// copies the new Gemini fields from the source KnowledgebaseConfig to
// the destination.
func TestMergeConfigGeminiKnowledgebase(t *testing.T) {
	dest := defaultConfig()
	src := &Config{
		Knowledgebase: KnowledgebaseConfig{
			Enabled:                   true,
			DatabasePath:              "/tmp/kb.db",
			EmbeddingProvider:         "gemini",
			EmbeddingModel:            "gemini-embedding-001",
			EmbeddingGeminiAPIKey:     "kb-gemini-key",
			EmbeddingGeminiAPIKeyFile: "/path/to/kb-gemini-key",
			EmbeddingGeminiBaseURL:    "https://gemini.example.com",
		},
	}

	mergeConfig(dest, src)

	if dest.Knowledgebase.EmbeddingProvider != "gemini" {
		t.Errorf("expected provider 'gemini', got %q",
			dest.Knowledgebase.EmbeddingProvider)
	}
	if dest.Knowledgebase.EmbeddingGeminiAPIKey != "kb-gemini-key" {
		t.Errorf("expected EmbeddingGeminiAPIKey 'kb-gemini-key', got %q",
			dest.Knowledgebase.EmbeddingGeminiAPIKey)
	}
	if dest.Knowledgebase.EmbeddingGeminiAPIKeyFile != "/path/to/kb-gemini-key" {
		t.Errorf("expected EmbeddingGeminiAPIKeyFile '/path/to/kb-gemini-key', got %q",
			dest.Knowledgebase.EmbeddingGeminiAPIKeyFile)
	}
	if dest.Knowledgebase.EmbeddingGeminiBaseURL != "https://gemini.example.com" {
		t.Errorf("expected EmbeddingGeminiBaseURL 'https://gemini.example.com', got %q",
			dest.Knowledgebase.EmbeddingGeminiBaseURL)
	}
}

// TestLoadAPIKeysFromFilesGeminiEmbedding verifies the secret-loading
// path picks up the Gemini key from disk when GeminiAPIKey is unset
// and GeminiAPIKeyFile points at a readable file.
func TestLoadAPIKeysFromFilesGeminiEmbedding(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "gemini.key")
	if err := os.WriteFile(keyPath, []byte("file-loaded-gemini-key\n"), 0600); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	cfg := &Config{
		Embedding: EmbeddingConfig{
			GeminiAPIKeyFile: keyPath,
		},
	}

	if err := loadAPIKeysFromFiles(cfg); err != nil {
		t.Fatalf("loadAPIKeysFromFiles: %v", err)
	}

	if cfg.Embedding.GeminiAPIKey != "file-loaded-gemini-key" {
		t.Errorf("expected GeminiAPIKey 'file-loaded-gemini-key', got %q",
			cfg.Embedding.GeminiAPIKey)
	}
}

// TestLoadAPIKeysFromFilesGeminiKnowledgebase verifies the
// knowledgebase secret-loading path picks up the Gemini key from disk.
func TestLoadAPIKeysFromFilesGeminiKnowledgebase(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "kb-gemini.key")
	if err := os.WriteFile(keyPath, []byte("kb-file-gemini-key\n"), 0600); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	cfg := &Config{
		Knowledgebase: KnowledgebaseConfig{
			EmbeddingGeminiAPIKeyFile: keyPath,
		},
	}

	if err := loadAPIKeysFromFiles(cfg); err != nil {
		t.Fatalf("loadAPIKeysFromFiles: %v", err)
	}

	if cfg.Knowledgebase.EmbeddingGeminiAPIKey != "kb-file-gemini-key" {
		t.Errorf("expected EmbeddingGeminiAPIKey 'kb-file-gemini-key', got %q",
			cfg.Knowledgebase.EmbeddingGeminiAPIKey)
	}
}

// TestLoadAPIKeysFromFilesGeminiPreservesExisting verifies the
// secret-loading code does not overwrite an explicit Gemini API key.
func TestLoadAPIKeysFromFilesGeminiPreservesExisting(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "gemini.key")
	if err := os.WriteFile(keyPath, []byte("file-key\n"), 0600); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	cfg := &Config{
		Embedding: EmbeddingConfig{
			GeminiAPIKey:     "preset-key",
			GeminiAPIKeyFile: keyPath,
		},
		Knowledgebase: KnowledgebaseConfig{
			EmbeddingGeminiAPIKey:     "preset-kb-key",
			EmbeddingGeminiAPIKeyFile: keyPath,
		},
	}

	if err := loadAPIKeysFromFiles(cfg); err != nil {
		t.Fatalf("loadAPIKeysFromFiles: %v", err)
	}

	if cfg.Embedding.GeminiAPIKey != "preset-key" {
		t.Errorf("expected existing Embedding key to be preserved, got %q",
			cfg.Embedding.GeminiAPIKey)
	}
	if cfg.Knowledgebase.EmbeddingGeminiAPIKey != "preset-kb-key" {
		t.Errorf("expected existing Knowledgebase key to be preserved, got %q",
			cfg.Knowledgebase.EmbeddingGeminiAPIKey)
	}
}

// TestLoadConfigGeminiFromYAML verifies the full LoadConfig pipeline
// correctly unmarshals Gemini embedding fields from YAML, including
// the api-key-from-file indirection.
func TestLoadConfigGeminiFromYAML(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "gemini.key")
	if err := os.WriteFile(keyPath, []byte("yaml-gemini-key"), 0600); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}
	kbKeyPath := filepath.Join(tmpDir, "kb-gemini.key")
	if err := os.WriteFile(kbKeyPath, []byte("yaml-kb-gemini-key"), 0600); err != nil {
		t.Fatalf("failed to write kb key file: %v", err)
	}

	configPath := filepath.Join(tmpDir, "config.yaml")
	configContent := `
embedding:
    enabled: true
    provider: gemini
    model: gemini-embedding-001
    gemini_api_key_file: ` + keyPath + `
    gemini_base_url: https://gemini.example.com
knowledgebase:
    enabled: true
    database_path: /tmp/kb.db
    embedding_provider: gemini
    embedding_model: gemini-embedding-001
    embedding_gemini_api_key_file: ` + kbKeyPath + `
    embedding_gemini_base_url: https://gemini.example.com
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadConfig(configPath, CLIFlags{ConfigFileSet: true, ConfigFile: configPath})
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.Embedding.Provider != "gemini" {
		t.Errorf("expected Embedding.Provider 'gemini', got %q",
			cfg.Embedding.Provider)
	}
	if cfg.Embedding.GeminiAPIKey != "yaml-gemini-key" {
		t.Errorf("expected Embedding.GeminiAPIKey 'yaml-gemini-key', got %q",
			cfg.Embedding.GeminiAPIKey)
	}
	if cfg.Embedding.GeminiBaseURL != "https://gemini.example.com" {
		t.Errorf("expected Embedding.GeminiBaseURL 'https://gemini.example.com', got %q",
			cfg.Embedding.GeminiBaseURL)
	}
	if cfg.Knowledgebase.EmbeddingProvider != "gemini" {
		t.Errorf("expected Knowledgebase.EmbeddingProvider 'gemini', got %q",
			cfg.Knowledgebase.EmbeddingProvider)
	}
	if cfg.Knowledgebase.EmbeddingGeminiAPIKey != "yaml-kb-gemini-key" {
		t.Errorf("expected Knowledgebase.EmbeddingGeminiAPIKey 'yaml-kb-gemini-key', got %q",
			cfg.Knowledgebase.EmbeddingGeminiAPIKey)
	}
	if cfg.Knowledgebase.EmbeddingGeminiBaseURL != "https://gemini.example.com" {
		t.Errorf("expected Knowledgebase.EmbeddingGeminiBaseURL 'https://gemini.example.com', got %q",
			cfg.Knowledgebase.EmbeddingGeminiBaseURL)
	}
}

// TestDatabaseConfigLoadPassword exercises the password_file resolution
// path on DatabaseConfig.LoadPassword across its behaviors: an
// already-set password short-circuits the file read; a valid file is
// read and trimmed into the unexported resolved field (NOT Password); a
// missing or empty file surfaces an error; and an empty Password with an
// empty PasswordFile is a no-op. A file-sourced secret must never land
// in the marshalable Password field.
func TestDatabaseConfigLoadPassword(t *testing.T) {
	t.Run("PasswordAlreadySetIsNoOp", func(t *testing.T) {
		tmpDir := t.TempDir()
		passFile := filepath.Join(tmpDir, "password.txt")
		if err := os.WriteFile(passFile, []byte("from-file"), 0600); err != nil {
			t.Fatalf("failed to write password file: %v", err)
		}

		cfg := &DatabaseConfig{
			Password:     "explicit-password",
			PasswordFile: passFile,
		}

		if err := cfg.LoadPassword(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Password != "explicit-password" {
			t.Errorf("expected password to remain 'explicit-password', got %q", cfg.Password)
		}
		// The file read must be skipped entirely; nothing resolved.
		if cfg.resolvedPassword != "" {
			t.Errorf("expected resolvedPassword to stay empty, got %q", cfg.resolvedPassword)
		}
		if got := cfg.EffectivePassword(); got != "explicit-password" {
			t.Errorf("EffectivePassword() = %q, want 'explicit-password'", got)
		}
	})

	t.Run("ReadsAndTrimsTrailingNewlineFromValidFile", func(t *testing.T) {
		tmpDir := t.TempDir()
		passFile := filepath.Join(tmpDir, "password.txt")
		// Only the trailing newline is stripped; in-secret whitespace
		// (here a deliberate trailing space) is preserved.
		if err := os.WriteFile(passFile, []byte("secret pass \n"), 0600); err != nil {
			t.Fatalf("failed to write password file: %v", err)
		}

		cfg := &DatabaseConfig{PasswordFile: passFile}

		if err := cfg.LoadPassword(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// The marshalable Password field must remain empty so the
		// file-sourced secret cannot round-trip to disk.
		if cfg.Password != "" {
			t.Errorf("expected Password to remain empty, got %q", cfg.Password)
		}
		if cfg.resolvedPassword != "secret pass " {
			t.Errorf("expected resolvedPassword 'secret pass ', got %q", cfg.resolvedPassword)
		}
		if got := cfg.EffectivePassword(); got != "secret pass " {
			t.Errorf("EffectivePassword() = %q, want 'secret pass '", got)
		}
	})

	t.Run("EmptyFileReturnsError", func(t *testing.T) {
		tmpDir := t.TempDir()
		passFile := filepath.Join(tmpDir, "empty.txt")
		if err := os.WriteFile(passFile, []byte("\n"), 0600); err != nil {
			t.Fatalf("failed to write password file: %v", err)
		}

		cfg := &DatabaseConfig{PasswordFile: passFile}
		if err := cfg.LoadPassword(); err == nil {
			t.Fatal("expected an error for an empty password file, got nil")
		}
	})

	t.Run("MissingFileReturnsError", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := &DatabaseConfig{
			PasswordFile: filepath.Join(tmpDir, "does-not-exist.txt"),
		}

		err := cfg.LoadPassword()
		if err == nil {
			t.Fatal("expected an error for a missing password file, got nil")
		}
		if cfg.Password != "" {
			t.Errorf("expected password to remain empty on error, got %q", cfg.Password)
		}
		if cfg.resolvedPassword != "" {
			t.Errorf("expected resolvedPassword to remain empty on error, got %q", cfg.resolvedPassword)
		}
	})

	t.Run("BothEmptyIsNoOp", func(t *testing.T) {
		cfg := &DatabaseConfig{}

		if err := cfg.LoadPassword(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Password != "" {
			t.Errorf("expected password to remain empty, got %q", cfg.Password)
		}
		if got := cfg.EffectivePassword(); got != "" {
			t.Errorf("EffectivePassword() = %q, want empty", got)
		}
	})
}

// TestDatabaseConfigLoadPasswordClearsStaleSecret verifies that calling
// LoadPassword more than once on the SAME DatabaseConfig never leaves a
// previously resolved file-sourced secret behind. A second load that no
// longer has a readable password_file must clear the resolved value so
// EffectivePassword stops returning the stale secret. A nil receiver is
// also a safe no-op.
func TestDatabaseConfigLoadPasswordClearsStaleSecret(t *testing.T) {
	t.Run("ClearedWhenPasswordFileRemoved", func(t *testing.T) {
		tmpDir := t.TempDir()
		passFile := filepath.Join(tmpDir, "db.password")
		const secret = "first-secret"
		if err := os.WriteFile(passFile, []byte(secret+"\n"), 0600); err != nil {
			t.Fatalf("failed to write password file: %v", err)
		}

		cfg := &DatabaseConfig{PasswordFile: passFile}

		// First load resolves the secret from the file.
		if err := cfg.LoadPassword(); err != nil {
			t.Fatalf("first LoadPassword: %v", err)
		}
		if got := cfg.EffectivePassword(); got != secret {
			t.Fatalf("after first load EffectivePassword() = %q, want %q", got, secret)
		}

		// Operator removes password_file before a reload. The second
		// load must clear the previously resolved secret.
		cfg.PasswordFile = ""
		if err := cfg.LoadPassword(); err != nil {
			t.Fatalf("second LoadPassword: %v", err)
		}
		if cfg.resolvedPassword != "" {
			t.Errorf("resolvedPassword = %q, want empty after password_file removed", cfg.resolvedPassword)
		}
		if got := cfg.EffectivePassword(); got != "" {
			t.Errorf("EffectivePassword() = %q, want empty after password_file removed", got)
		}
	})

	t.Run("ClearedWhenSecondFileMissing", func(t *testing.T) {
		tmpDir := t.TempDir()
		passFile := filepath.Join(tmpDir, "db.password")
		const secret = "first-secret"
		if err := os.WriteFile(passFile, []byte(secret+"\n"), 0600); err != nil {
			t.Fatalf("failed to write password file: %v", err)
		}

		cfg := &DatabaseConfig{PasswordFile: passFile}
		if err := cfg.LoadPassword(); err != nil {
			t.Fatalf("first LoadPassword: %v", err)
		}
		if got := cfg.EffectivePassword(); got != secret {
			t.Fatalf("after first load EffectivePassword() = %q, want %q", got, secret)
		}

		// Point at a now-missing file. The second load must return an
		// error AND leave no stale secret behind.
		cfg.PasswordFile = filepath.Join(tmpDir, "does-not-exist.password")
		if err := cfg.LoadPassword(); err == nil {
			t.Fatal("second LoadPassword expected error for missing file, got nil")
		}
		if cfg.resolvedPassword != "" {
			t.Errorf("resolvedPassword = %q, want empty after failed read", cfg.resolvedPassword)
		}
		if got := cfg.EffectivePassword(); got != "" {
			t.Errorf("EffectivePassword() = %q, want empty after failed read", got)
		}
	})

	t.Run("ClearedWhenInlinePasswordSet", func(t *testing.T) {
		tmpDir := t.TempDir()
		passFile := filepath.Join(tmpDir, "db.password")
		const secret = "first-secret"
		if err := os.WriteFile(passFile, []byte(secret+"\n"), 0600); err != nil {
			t.Fatalf("failed to write password file: %v", err)
		}

		cfg := &DatabaseConfig{PasswordFile: passFile}
		if err := cfg.LoadPassword(); err != nil {
			t.Fatalf("first LoadPassword: %v", err)
		}
		if cfg.resolvedPassword != secret {
			t.Fatalf("after first load resolvedPassword = %q, want %q", cfg.resolvedPassword, secret)
		}

		// An inline Password now takes precedence. The reset must run
		// before the early return so the stale resolved value is gone.
		cfg.Password = "inline-now"
		if err := cfg.LoadPassword(); err != nil {
			t.Fatalf("second LoadPassword: %v", err)
		}
		if cfg.resolvedPassword != "" {
			t.Errorf("resolvedPassword = %q, want empty once inline Password is set", cfg.resolvedPassword)
		}
		if got := cfg.EffectivePassword(); got != "inline-now" {
			t.Errorf("EffectivePassword() = %q, want 'inline-now'", got)
		}
	})

	t.Run("NilReceiverIsNoOp", func(t *testing.T) {
		var cfg *DatabaseConfig
		if err := cfg.LoadPassword(); err != nil {
			t.Fatalf("nil-receiver LoadPassword: %v", err)
		}
	})
}

// TestDatabaseConfigEffectivePassword verifies the precedence rules of
// EffectivePassword independently of the file-resolution path: an inline
// Password always wins, otherwise the resolved (file-sourced) value is
// returned, and an unconfigured password yields the empty string.
func TestDatabaseConfigEffectivePassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		resolved string
		want     string
	}{
		{"inline wins over resolved", "inline-pw", "file-pw", "inline-pw"},
		{"inline only", "inline-pw", "", "inline-pw"},
		{"resolved only", "", "file-pw", "file-pw"},
		{"neither configured", "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &DatabaseConfig{
				Password:         tt.password,
				resolvedPassword: tt.resolved,
			}
			if got := cfg.EffectivePassword(); got != tt.want {
				t.Errorf("EffectivePassword() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestBuildConnectionStringUsesResolvedPassword confirms the connection
// string includes a file-sourced password resolved via LoadPassword,
// even though that secret lives only in the unexported resolved field.
func TestBuildConnectionStringUsesResolvedPassword(t *testing.T) {
	tmpDir := t.TempDir()
	passFile := filepath.Join(tmpDir, "db.password")
	const secret = "resolved-secret"
	if err := os.WriteFile(passFile, []byte(secret+"\n"), 0600); err != nil {
		t.Fatalf("failed to write password file: %v", err)
	}

	cfg := &DatabaseConfig{
		User:         "postgres",
		Host:         "localhost",
		Port:         5432,
		Database:     "testdb",
		PasswordFile: passFile,
	}

	if err := cfg.LoadPassword(); err != nil {
		t.Fatalf("LoadPassword: %v", err)
	}
	if cfg.Password != "" {
		t.Fatalf("expected Password to remain empty, got %q", cfg.Password)
	}

	got := cfg.BuildConnectionString()
	want := "postgres://postgres:" + secret + "@localhost:5432/testdb"
	if got != want {
		t.Errorf("BuildConnectionString() = %q, want %q", got, want)
	}
}

// TestMarshalDoesNotLeakResolvedPassword is the regression test for the
// CodeRabbit finding on config.go: a password sourced from password_file
// must not appear when the config is marshaled (the path SaveConfig
// uses). Because LoadPassword stores file-sourced secrets in the
// unexported, yaml:"-" resolvedPassword field, marshaling must emit an
// empty password and never the secret itself.
func TestMarshalDoesNotLeakResolvedPassword(t *testing.T) {
	tmpDir := t.TempDir()
	passFile := filepath.Join(tmpDir, "db.password")
	const secret = "must-not-leak-to-disk"
	if err := os.WriteFile(passFile, []byte(secret+"\n"), 0600); err != nil {
		t.Fatalf("failed to write password file: %v", err)
	}

	cfg := &Config{
		Database: &DatabaseConfig{
			User:         "postgres",
			Host:         "localhost",
			Port:         5432,
			Database:     "testdb",
			PasswordFile: passFile,
		},
	}

	if err := cfg.Database.LoadPassword(); err != nil {
		t.Fatalf("LoadPassword: %v", err)
	}
	// Sanity: the secret was resolved and is usable at runtime.
	if cfg.Database.EffectivePassword() != secret {
		t.Fatalf("EffectivePassword() = %q, want %q", cfg.Database.EffectivePassword(), secret)
	}

	// Marshal the live config exactly as SaveConfig does.
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	out := string(data)

	if strings.Contains(out, secret) {
		t.Fatalf("marshaled config leaked the file-sourced secret:\n%s", out)
	}
	// The password key must be present but empty for the file-sourced case.
	if !strings.Contains(out, "password: \"\"") {
		t.Errorf("expected an empty password field in marshaled output, got:\n%s", out)
	}
}

func TestLocalAuthDefaultsToEnabled(t *testing.T) {
	cfg := defaultConfig()
	if !cfg.HTTP.Auth.LocalEnabled() {
		t.Fatal("local authentication should default to enabled")
	}
}

func TestLocalAuthCanBeDisabledExplicitly(t *testing.T) {
	path := writeTempConfig(t, `
http:
  auth:
    local:
      enabled: false
    oidc:
      enabled: true
      issuer: https://idp.example.com
      client_id: workbench
      client_secret: s3cret
      redirect_url: https://workbench.example.com/api/v1/auth/oidc/callback
`)
	cfg, err := LoadConfig(path, CLIFlags{})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.HTTP.Auth.LocalEnabled() {
		t.Fatal("local authentication should be disabled")
	}
}

func TestOIDCDefaultClaimNames(t *testing.T) {
	cfg := defaultConfig()
	if cfg.HTTP.Auth.OIDC.UsernameClaim != "email" {
		t.Fatalf("username_claim = %q, want email", cfg.HTTP.Auth.OIDC.UsernameClaim)
	}
	if cfg.HTTP.Auth.OIDC.GroupsClaim != "groups" {
		t.Fatalf("groups_claim = %q, want groups", cfg.HTTP.Auth.OIDC.GroupsClaim)
	}
	want := []string{"openid", "email", "profile"}
	if !reflect.DeepEqual(cfg.HTTP.Auth.OIDC.Scopes, want) {
		t.Fatalf("scopes = %v, want %v", cfg.HTTP.Auth.OIDC.Scopes, want)
	}
}

// TestOIDCClientSecretReadFromFile deviates from the literal task brief,
// which checked cfg.HTTP.Auth.OIDC.ClientSecret directly. This
// implementation follows the DatabaseConfig.resolvedPassword pattern: a
// secret resolved from client_secret_file is stored in the unexported
// resolvedClientSecret field (never the exported, yaml-tagged
// ClientSecret field) so it cannot leak back out if the config is ever
// marshaled. The effective secret is read via EffectiveClientSecret.
// See TestMarshalDoesNotLeakOIDCClientSecret for the regression this
// protects against.
func TestOIDCClientSecretReadFromFile(t *testing.T) {
	secretPath := filepath.Join(t.TempDir(), "oidc-secret")
	if err := os.WriteFile(secretPath, []byte("file-secret\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	path := writeTempConfig(t, fmt.Sprintf(`
http:
  auth:
    oidc:
      enabled: true
      issuer: https://idp.example.com
      client_id: workbench
      client_secret_file: %s
      redirect_url: https://workbench.example.com/api/v1/auth/oidc/callback
`, secretPath))
	cfg, err := LoadConfig(path, CLIFlags{})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.HTTP.Auth.OIDC.EffectiveClientSecret() != "file-secret" {
		t.Fatalf("effective client secret = %q, want %q", cfg.HTTP.Auth.OIDC.EffectiveClientSecret(), "file-secret")
	}
	if cfg.HTTP.Auth.OIDC.ClientSecret != "" {
		t.Fatalf("ClientSecret should remain empty for a file-sourced secret, got %q", cfg.HTTP.Auth.OIDC.ClientSecret)
	}
}

// TestMarshalDoesNotLeakOIDCClientSecret is the OIDC counterpart of
// TestMarshalDoesNotLeakResolvedPassword: a client secret sourced from
// client_secret_file must never appear when the config is marshaled
// (the path SaveConfig would use), because resolvedClientSecret is
// unexported and tagged yaml:"-".
func TestMarshalDoesNotLeakOIDCClientSecret(t *testing.T) {
	secretPath := filepath.Join(t.TempDir(), "oidc-secret")
	const secret = "must-not-leak-to-disk"
	if err := os.WriteFile(secretPath, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	path := writeTempConfig(t, fmt.Sprintf(`
http:
  auth:
    oidc:
      enabled: true
      issuer: https://idp.example.com
      client_id: workbench
      client_secret_file: %s
      redirect_url: https://workbench.example.com/api/v1/auth/oidc/callback
`, secretPath))
	cfg, err := LoadConfig(path, CLIFlags{})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.HTTP.Auth.OIDC.EffectiveClientSecret() != secret {
		t.Fatalf("EffectiveClientSecret() = %q, want %q", cfg.HTTP.Auth.OIDC.EffectiveClientSecret(), secret)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	out := string(data)
	if strings.Contains(out, secret) {
		t.Fatalf("marshaled config leaked the file-sourced OIDC client secret:\n%s", out)
	}
}

// TestOIDCClientSecretFileMissingFails covers the read-failure return in
// loadAPIKeysFromFiles: a client_secret_file that does not exist must
// fail LoadConfig rather than silently leaving the client secret empty.
func TestOIDCClientSecretFileMissingFails(t *testing.T) {
	path := writeTempConfig(t, `
http:
  auth:
    oidc:
      enabled: true
      issuer: https://idp.example.com
      client_id: workbench
      client_secret_file: /nonexistent/oidc-secret
      redirect_url: https://workbench.example.com/api/v1/auth/oidc/callback
`)
	if _, err := LoadConfig(path, CLIFlags{}); err == nil {
		t.Fatal("expected LoadConfig to fail for a missing client_secret_file")
	}
}

// TestOIDCClientSecretFileEmptyFails covers the same return for a
// client_secret_file that exists but is empty; fileutil.ReadSecretFile
// itself rejects an empty/whitespace-only file.
func TestOIDCClientSecretFileEmptyFails(t *testing.T) {
	secretPath := filepath.Join(t.TempDir(), "oidc-secret")
	if err := os.WriteFile(secretPath, []byte(""), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	path := writeTempConfig(t, fmt.Sprintf(`
http:
  auth:
    oidc:
      enabled: true
      issuer: https://idp.example.com
      client_id: workbench
      client_secret_file: %s
      redirect_url: https://workbench.example.com/api/v1/auth/oidc/callback
`, secretPath))
	if _, err := LoadConfig(path, CLIFlags{}); err == nil {
		t.Fatal("expected LoadConfig to fail for an empty client_secret_file")
	}
}

func TestValidateConfigRejectsIncompleteOIDC(t *testing.T) {
	cases := map[string]OIDCConfig{
		"no issuer":         {Enabled: boolPtr(true), ClientID: "w", ClientSecret: "s", RedirectURL: "https://w.example.com/cb"},
		"no client id":      {Enabled: boolPtr(true), Issuer: "https://i.example.com", ClientSecret: "s", RedirectURL: "https://w.example.com/cb"},
		"no secret":         {Enabled: boolPtr(true), Issuer: "https://i.example.com", ClientID: "w", RedirectURL: "https://w.example.com/cb"},
		"no redirect":       {Enabled: boolPtr(true), Issuer: "https://i.example.com", ClientID: "w", ClientSecret: "s"},
		"plaintext issuer":  {Enabled: boolPtr(true), Issuer: "http://i.example.com", ClientID: "w", ClientSecret: "s", RedirectURL: "https://w.example.com/cb"},
		"relative redirect": {Enabled: boolPtr(true), Issuer: "https://i.example.com", ClientID: "w", ClientSecret: "s", RedirectURL: "/callback"},
	}
	for name, oidc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := defaultConfig()
			cfg.HTTP.Auth.OIDC = oidc
			if err := validateConfig(cfg); err == nil {
				t.Fatal("expected validation to fail")
			}
		})
	}
}

func TestValidateConfigAcceptsCompleteOIDC(t *testing.T) {
	cfg := defaultConfig()
	cfg.HTTP.Auth.OIDC = OIDCConfig{
		Enabled:      boolPtr(true),
		Issuer:       "https://idp.example.com",
		ClientID:     "workbench",
		ClientSecret: "s3cret",
		RedirectURL:  "https://workbench.example.com/api/v1/auth/oidc/callback",
	}
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("expected a complete OIDC configuration to validate, got: %v", err)
	}
}

func TestValidateConfigRejectsNoAuthenticationMethod(t *testing.T) {
	cfg := defaultConfig()
	disabled := false
	cfg.HTTP.Auth.Local.Enabled = &disabled
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected validation to refuse a configuration with no way to log in")
	}
}

// TestValidateConfigAllowsBareConfigWithNoAuthBlock guards the interaction
// between the "no login method available" check and the local-login
// default: a config file with no http.auth block at all must still
// validate, because local login defaults to enabled.
func TestValidateConfigAllowsBareConfigWithNoAuthBlock(t *testing.T) {
	cfg := defaultConfig()
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("expected the default configuration to validate, got: %v", err)
	}
}

// TestValidateConfigAllowsOIDCOnly guards the other side of the "no
// login method" interaction: disabling local login is fine as long as
// OIDC is fully configured and enabled.
func TestValidateConfigAllowsOIDCOnly(t *testing.T) {
	cfg := defaultConfig()
	disabled := false
	cfg.HTTP.Auth.Local.Enabled = &disabled
	cfg.HTTP.Auth.OIDC = OIDCConfig{
		Enabled:      boolPtr(true),
		Issuer:       "https://idp.example.com",
		ClientID:     "workbench",
		ClientSecret: "s3cret",
		RedirectURL:  "https://workbench.example.com/api/v1/auth/oidc/callback",
	}
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("expected local-disabled + OIDC-enabled to validate, got: %v", err)
	}
}

// TestMergeConfigOIDCFields verifies mergeConfig picks up every new OIDC
// field from a file-sourced config, matching the guarded-assignment idiom
// used throughout mergeConfig.
func TestMergeConfigOIDCFields(t *testing.T) {
	provisionUsers := true
	dest := defaultConfig()
	src := &Config{
		HTTP: HTTPConfig{
			Auth: AuthConfig{
				OIDC: OIDCConfig{
					Enabled:             boolPtr(true),
					Issuer:              "https://idp.example.com",
					ClientID:            "workbench",
					ClientSecret:        "s3cret",
					ClientSecretFile:    "/etc/secrets/oidc",
					RedirectURL:         "https://workbench.example.com/api/v1/auth/oidc/callback",
					Scopes:              []string{"openid"},
					UsernameClaim:       "preferred_username",
					DisplayNameClaim:    "display_name",
					GroupsClaim:         "roles",
					ButtonLabel:         "Sign in with Example IdP",
					ProvisionUsers:      &provisionUsers,
					AllowedEmailDomains: []string{"example.com"},
					SuperuserGroup:      "admins",
					GroupMap:            map[string]string{"admins": "superuser"},
				},
			},
		},
	}

	mergeConfig(dest, src)

	got := dest.HTTP.Auth.OIDC
	want := src.HTTP.Auth.OIDC
	if got.IsEnabled() != want.IsEnabled() ||
		got.Issuer != want.Issuer ||
		got.ClientID != want.ClientID ||
		got.ClientSecret != want.ClientSecret ||
		got.ClientSecretFile != want.ClientSecretFile ||
		got.RedirectURL != want.RedirectURL ||
		!reflect.DeepEqual(got.Scopes, want.Scopes) ||
		got.UsernameClaim != want.UsernameClaim ||
		got.DisplayNameClaim != want.DisplayNameClaim ||
		got.GroupsClaim != want.GroupsClaim ||
		got.ButtonLabel != want.ButtonLabel ||
		got.ProvisionUsersEnabled() != want.ProvisionUsersEnabled() ||
		!reflect.DeepEqual(got.AllowedEmailDomains, want.AllowedEmailDomains) ||
		got.SuperuserGroup != want.SuperuserGroup ||
		!reflect.DeepEqual(got.GroupMap, want.GroupMap) {
		t.Fatalf("mergeConfig did not merge all OIDC fields: got %+v, want %+v", got, want)
	}
}

// TestMergeConfigOIDCFieldsPreservesDefaultsWhenSourceEmpty is the other
// direction of TestMergeConfigOIDCFields: a source config with no OIDC
// block at all must leave the defaultConfig() OIDC defaults (scopes,
// username claim, button label) untouched, the same way
// TestLocalAuthEnabledMergesOnlyWhenSet checks for Local.Enabled.
func TestMergeConfigOIDCFieldsPreservesDefaultsWhenSourceEmpty(t *testing.T) {
	dest := defaultConfig()
	wantScopes := append([]string(nil), dest.HTTP.Auth.OIDC.Scopes...)
	wantUsernameClaim := dest.HTTP.Auth.OIDC.UsernameClaim
	wantButtonLabel := dest.HTTP.Auth.OIDC.ButtonLabel

	src := &Config{}
	mergeConfig(dest, src)

	if !reflect.DeepEqual(dest.HTTP.Auth.OIDC.Scopes, wantScopes) {
		t.Fatalf("Scopes = %v, want default %v", dest.HTTP.Auth.OIDC.Scopes, wantScopes)
	}
	if dest.HTTP.Auth.OIDC.UsernameClaim != wantUsernameClaim {
		t.Fatalf("UsernameClaim = %q, want default %q", dest.HTTP.Auth.OIDC.UsernameClaim, wantUsernameClaim)
	}
	if dest.HTTP.Auth.OIDC.ButtonLabel != wantButtonLabel {
		t.Fatalf("ButtonLabel = %q, want default %q", dest.HTTP.Auth.OIDC.ButtonLabel, wantButtonLabel)
	}
}

// TestLocalAuthEnabledMergesOnlyWhenSet verifies mergeConfig only
// overrides Local.Enabled when the source pointer is non-nil, and leaves
// dest untouched (still nil, i.e. defaulting to true) otherwise.
func TestLocalAuthEnabledMergesOnlyWhenSet(t *testing.T) {
	dest := defaultConfig()
	src := &Config{}

	mergeConfig(dest, src)

	if dest.HTTP.Auth.Local.Enabled != nil {
		t.Fatalf("expected Local.Enabled to remain nil when src has no override, got %v", *dest.HTTP.Auth.Local.Enabled)
	}
	if !dest.HTTP.Auth.LocalEnabled() {
		t.Fatal("local authentication should still default to enabled")
	}
}

// TestReloadWarnsOnOIDCChanges exercises logRestartRequiredSettings with
// every changed OIDC field, mirroring how it already covers http.tls, so
// a panic or an unguarded field access would be caught here.
func TestReloadWarnsOnOIDCChanges(t *testing.T) {
	provisionUsersReload := true
	oldCfg := defaultConfig()
	newCfg := defaultConfig()
	newCfg.HTTP.Auth.OIDC.Enabled = boolPtr(true)
	newCfg.HTTP.Auth.OIDC.Issuer = "https://idp.example.com"
	newCfg.HTTP.Auth.OIDC.ClientID = "workbench"
	newCfg.HTTP.Auth.OIDC.ClientSecret = "s3cret"
	newCfg.HTTP.Auth.OIDC.RedirectURL = "https://workbench.example.com/api/v1/auth/oidc/callback"
	newCfg.HTTP.Auth.OIDC.UsernameClaim = "preferred_username"
	newCfg.HTTP.Auth.OIDC.DisplayNameClaim = "display_name"
	newCfg.HTTP.Auth.OIDC.GroupsClaim = "roles"
	newCfg.HTTP.Auth.OIDC.Scopes = []string{"openid"}
	newCfg.HTTP.Auth.OIDC.ProvisionUsers = &provisionUsersReload
	newCfg.HTTP.Auth.OIDC.AllowedEmailDomains = []string{"example.com"}
	newCfg.HTTP.Auth.OIDC.SuperuserGroup = "admins"
	newCfg.HTTP.Auth.OIDC.GroupMap = map[string]string{"idp-admins": "admins"}
	localDisabled := false
	newCfg.HTTP.Auth.Local.Enabled = &localDisabled

	rc := &ReloadableConfig{config: oldCfg}

	out := captureStderr(t, func() {
		rc.logRestartRequiredSettings(newCfg)
	})

	// Every http.auth setting requires a restart, including the claim and
	// authorization mapping that a reload once reported as applied. The
	// handler keeps the copy of the OIDC configuration it was built with
	// and the capabilities payload is built once at startup, so a NOTE
	// saying the value "changed" would tell an operator containing an
	// incident that a narrowed allowed_email_domains or a pruned
	// group_map had taken effect when it had not.
	wantWarnings := []string{
		"WARNING: http.auth.oidc.enabled changed - requires restart",
		"WARNING: http.auth.oidc.issuer changed - requires restart",
		"WARNING: http.auth.oidc.client_id changed - requires restart",
		"WARNING: http.auth.oidc.client_secret changed - requires restart",
		"WARNING: http.auth.oidc.redirect_url changed - requires restart",
		"WARNING: http.auth.oidc.username_claim changed - requires restart",
		"WARNING: http.auth.oidc.display_name_claim changed - requires restart",
		"WARNING: http.auth.oidc.groups_claim changed - requires restart",
		"WARNING: http.auth.oidc.scopes changed - requires restart",
		"WARNING: http.auth.oidc.provision_users changed - requires restart",
		"WARNING: http.auth.oidc.allowed_email_domains changed - requires restart",
		"WARNING: http.auth.oidc.superuser_group changed - requires restart",
		"WARNING: http.auth.oidc.group_map changed - requires restart",
		"WARNING: http.auth.local.enabled changed - requires restart",
	}
	for _, want := range wantWarnings {
		if !strings.Contains(out, want) {
			t.Errorf("expected stderr to contain %q, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "NOTE: http.auth") {
		t.Errorf("no http.auth setting takes effect on reload, so none may be "+
			"reported as merely changed, got:\n%s", out)
	}
}

// captureStderr redirects os.Stderr for the duration of fn and returns
// everything written to it, so a test can assert on the exact log lines
// logRestartRequiredSettings produces rather than merely that it did not
// panic.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	original := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = original }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("closing pipe writer: %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("reading captured stderr: %v", err)
	}
	return string(data)
}

// TestOIDCEnabledMergesExplicitFalse is the reason OIDCConfig.Enabled is
// a pointer: a later configuration source saying "enabled: false" must
// switch federated login off, which a plain bool merged with the
// guarded-assignment idiom could never do, since false is its zero value
// and so reads as "not set".
func TestOIDCEnabledMergesExplicitFalse(t *testing.T) {
	dest := defaultConfig()
	dest.HTTP.Auth.OIDC.Enabled = boolPtr(true)

	mergeConfig(dest, &Config{})
	if !dest.HTTP.Auth.OIDC.IsEnabled() {
		t.Fatal("a source with no OIDC block switched federated login off")
	}

	src := &Config{}
	src.HTTP.Auth.OIDC.Enabled = boolPtr(false)
	mergeConfig(dest, src)
	if dest.HTTP.Auth.OIDC.IsEnabled() {
		t.Fatal("an explicit enabled: false in a later source did not switch federated login off")
	}

	if (OIDCConfig{}).IsEnabled() {
		t.Error("IsEnabled must default to false when the field is unset")
	}
	if got := BoolPtr(true); got == nil || !*got {
		t.Error("BoolPtr(true) must point at true")
	}
	if got := BoolPtr(false); got == nil || *got {
		t.Error("BoolPtr(false) must point at false")
	}
}

func TestAuditRetentionDaysDefault(t *testing.T) {
	cfg := defaultConfig()
	if got := cfg.HTTP.Auth.AuditRetentionDays(); got != 90 {
		t.Errorf("expected default audit retention of 90 days, got %d", got)
	}
}

func TestAuditRetentionDaysYAMLOverride(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
http:
    auth:
        audit_retention_days: 30
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadConfig(configPath, CLIFlags{ConfigFileSet: true, ConfigFile: configPath})
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if got := cfg.HTTP.Auth.AuditRetentionDays(); got != 30 {
		t.Errorf("expected audit retention of 30 days from YAML, got %d", got)
	}
}

func TestAuditRetentionDaysYAMLZeroKeepsForever(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
http:
    auth:
        audit_retention_days: 0
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadConfig(configPath, CLIFlags{ConfigFileSet: true, ConfigFile: configPath})
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if got := cfg.HTTP.Auth.AuditRetentionDays(); got != 0 {
		t.Errorf("expected explicit audit_retention_days: 0 to be kept as 0, got %d", got)
	}
}

func TestAuditRetentionDaysEnvVarOverride(t *testing.T) {
	origVal, origSet := os.LookupEnv("PGEDGE_AUDIT_RETENTION_DAYS")
	defer func() {
		if origSet {
			os.Setenv("PGEDGE_AUDIT_RETENTION_DAYS", origVal)
		} else {
			os.Unsetenv("PGEDGE_AUDIT_RETENTION_DAYS")
		}
	}()

	os.Setenv("PGEDGE_AUDIT_RETENTION_DAYS", "45")
	cfg, err := LoadConfig("", CLIFlags{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cfg.HTTP.Auth.AuditRetentionDays(); got != 45 {
		t.Errorf("expected audit retention of 45 days from env var, got %d", got)
	}

	// An explicit 0 via env var must also keep events forever.
	os.Setenv("PGEDGE_AUDIT_RETENTION_DAYS", "0")
	cfg, err = LoadConfig("", CLIFlags{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cfg.HTTP.Auth.AuditRetentionDays(); got != 0 {
		t.Errorf("expected audit retention of 0 from env var, got %d", got)
	}

	// Invalid values are ignored, leaving the default in place.
	os.Setenv("PGEDGE_AUDIT_RETENTION_DAYS", "not-a-number")
	cfg, err = LoadConfig("", CLIFlags{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cfg.HTTP.Auth.AuditRetentionDays(); got != 90 {
		t.Errorf("expected default audit retention when env var is invalid, got %d", got)
	}

	// A negative retention period is as invalid as a non-numeric one.
	os.Setenv("PGEDGE_AUDIT_RETENTION_DAYS", "-1")
	cfg, err = LoadConfig("", CLIFlags{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cfg.HTTP.Auth.AuditRetentionDays(); got != 90 {
		t.Errorf("expected default audit retention when env var is negative, got %d", got)
	}
}

func TestAuditRetentionDaysNegativeDefaultsTo90(t *testing.T) {
	a := AuthConfig{AuditRetentionDaysPtr: intPtr(-7)}
	if got := a.AuditRetentionDays(); got != 90 {
		t.Errorf("expected 90 when AuditRetentionDaysPtr is negative, got %d", got)
	}
}

func TestAuditRetentionDaysNilPointerDefaultsTo90(t *testing.T) {
	var a AuthConfig
	if got := a.AuditRetentionDays(); got != 90 {
		t.Errorf("expected 90 when AuditRetentionDaysPtr is nil, got %d", got)
	}
}

// TestMaxFailedAttemptsBeforeLockoutDefault checks that a config built
// from the defaults alone locks an account after ten failed attempts.
func TestMaxFailedAttemptsBeforeLockoutDefault(t *testing.T) {
	cfg := defaultConfig()
	if got := cfg.HTTP.Auth.MaxFailedAttemptsBeforeLockout(); got != 10 {
		t.Errorf("expected default lockout threshold of 10, got %d", got)
	}
}

// TestMaxFailedAttemptsBeforeLockoutOmittedKeepsDefault is the
// regression test for issue #473: a config file that omits the key
// must not overwrite the default of 10 with a zero value, which would
// silently disable account lockout.
func TestMaxFailedAttemptsBeforeLockoutOmittedKeepsDefault(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
http:
    address: ":9999"
    auth:
        rate_limit_max_attempts: 5
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadConfig(configPath, CLIFlags{ConfigFileSet: true, ConfigFile: configPath})
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if cfg.HTTP.Auth.MaxFailedAttemptsBeforeLockoutPtr == nil {
		t.Fatal("expected an omitted key to leave the default pointer in place")
	}
	if got := cfg.HTTP.Auth.MaxFailedAttemptsBeforeLockout(); got != 10 {
		t.Errorf("expected an omitted key to keep the default of 10, got %d", got)
	}
}

// TestMaxFailedAttemptsBeforeLockoutMergeDoesNotClobber checks that
// merging a source config that never named the key leaves the
// destination default in place.
func TestMaxFailedAttemptsBeforeLockoutMergeDoesNotClobber(t *testing.T) {
	dest := defaultConfig()
	src := &Config{}
	mergeConfig(dest, src)
	if got := dest.HTTP.Auth.MaxFailedAttemptsBeforeLockout(); got != 10 {
		t.Errorf("expected merging an empty config to keep 10, got %d", got)
	}

	src.HTTP.Auth.MaxFailedAttemptsBeforeLockoutPtr = intPtr(0)
	mergeConfig(dest, src)
	if got := dest.HTTP.Auth.MaxFailedAttemptsBeforeLockout(); got != 0 {
		t.Errorf("expected an explicit 0 to disable lockout, got %d", got)
	}
}

// TestMaxFailedAttemptsBeforeLockoutYAMLOverride checks that a value
// given in the config file replaces the default.
func TestMaxFailedAttemptsBeforeLockoutYAMLOverride(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
http:
    auth:
        max_failed_attempts_before_lockout: 3
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadConfig(configPath, CLIFlags{ConfigFileSet: true, ConfigFile: configPath})
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if got := cfg.HTTP.Auth.MaxFailedAttemptsBeforeLockout(); got != 3 {
		t.Errorf("expected a lockout threshold of 3 from YAML, got %d", got)
	}
}

// TestMaxFailedAttemptsBeforeLockoutYAMLZeroDisables checks that an
// explicit zero is taken as a deliberate request to disable account
// lockout rather than being treated as an omitted key.
func TestMaxFailedAttemptsBeforeLockoutYAMLZeroDisables(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
http:
    auth:
        max_failed_attempts_before_lockout: 0
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadConfig(configPath, CLIFlags{ConfigFileSet: true, ConfigFile: configPath})
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if got := cfg.HTTP.Auth.MaxFailedAttemptsBeforeLockout(); got != 0 {
		t.Errorf("expected an explicit 0 to disable lockout, got %d", got)
	}
}

// TestMaxFailedAttemptsBeforeLockoutNegativeDefaultsTo10 checks that a
// negative attempt count, which cannot mean anything sensible, falls
// back to the default.
func TestMaxFailedAttemptsBeforeLockoutNegativeDefaultsTo10(t *testing.T) {
	a := AuthConfig{MaxFailedAttemptsBeforeLockoutPtr: intPtr(-3)}
	if got := a.MaxFailedAttemptsBeforeLockout(); got != 10 {
		t.Errorf("expected 10 when the pointer is negative, got %d", got)
	}
}

// TestMaxFailedAttemptsBeforeLockoutNilPointerDefaultsTo10 checks that
// the accessor applies the default when the setting was never set.
func TestMaxFailedAttemptsBeforeLockoutNilPointerDefaultsTo10(t *testing.T) {
	var a AuthConfig
	if got := a.MaxFailedAttemptsBeforeLockout(); got != 10 {
		t.Errorf("expected 10 when the pointer is nil, got %d", got)
	}
}

// TestReloadWarnsOnLockoutThresholdChange covers the restart warning
// for the account lockout threshold. NewAuthStore reads the effective
// value once at start-up and the store keeps its own copy, so a SIGHUP
// that changes it applies nothing; without this warning an operator
// switching lockout on, or tightening it during an attack, is told the
// reload succeeded whilst every login goes on being counted against the
// value the server started with.
func TestReloadWarnsOnLockoutThresholdChange(t *testing.T) {
	const warning = "WARNING: http.auth.max_failed_attempts_before_lockout changed - requires restart"

	tests := map[string]struct {
		oldRaw   *int
		newRaw   *int
		wantWarn bool
	}{
		"enabling lockout warns": {
			oldRaw:   intPtr(0),
			newRaw:   intPtr(10),
			wantWarn: true,
		},
		"disabling lockout warns": {
			oldRaw:   intPtr(10),
			newRaw:   intPtr(0),
			wantWarn: true,
		},
		"tightening the threshold warns": {
			oldRaw:   intPtr(10),
			newRaw:   intPtr(3),
			wantWarn: true,
		},
		"an unchanged threshold is silent": {
			oldRaw:   intPtr(5),
			newRaw:   intPtr(5),
			wantWarn: false,
		},
		// The comparison is of effective values, so omitting the key
		// and spelling out the default are the same configuration and
		// must not produce a warning an operator would chase.
		"omitting the key matches the spelled-out default": {
			oldRaw:   nil,
			newRaw:   intPtr(10),
			wantWarn: false,
		},
		"a negative value matches the default it falls back to": {
			oldRaw:   intPtr(10),
			newRaw:   intPtr(-1),
			wantWarn: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			oldCfg := defaultConfig()
			oldCfg.HTTP.Auth.MaxFailedAttemptsBeforeLockoutPtr = tc.oldRaw
			newCfg := defaultConfig()
			newCfg.HTTP.Auth.MaxFailedAttemptsBeforeLockoutPtr = tc.newRaw

			rc := &ReloadableConfig{config: oldCfg}
			out := captureStderr(t, func() {
				rc.logRestartRequiredSettings(newCfg)
			})

			if got := strings.Contains(out, warning); got != tc.wantWarn {
				t.Errorf("warning present = %v, want %v; output:\n%s",
					got, tc.wantWarn, out)
			}
		})
	}
}

// TestMergeConfigOverridesEveryField populates every field mergeConfig
// knows about and checks that each one reaches the destination, so
// that the per-field guards are all exercised rather than only the
// handful a typical config file sets.
func TestMergeConfigOverridesEveryField(t *testing.T) {
	dest := defaultConfig()
	src := &Config{
		HTTP: HTTPConfig{
			Address: ":9443",
			TLS: TLSConfig{
				Enabled:   true,
				CertFile:  "/tmp/cert.pem",
				KeyFile:   "/tmp/key.pem",
				ChainFile: "/tmp/chain.pem",
			},
			TrustedProxies: []string{"192.0.2.0/24"},
			CORSOrigin:     "https://app.example.com",
			HSTSEnabled:    true,
			Auth: AuthConfig{
				MaxUserTokenDays:       30,
				RateLimitWindowMinutes: 5,
				RateLimitMaxAttempts:   3,
			},
		},
		Embedding: EmbeddingConfig{
			Enabled:          true,
			Provider:         "voyage",
			Model:            "voyage-3",
			VoyageAPIKey:     "vk",
			VoyageAPIKeyFile: "/tmp/vk",
			OpenAIAPIKey:     "ok",
			OpenAIAPIKeyFile: "/tmp/ok",
			VoyageBaseURL:    "https://voyage.example.com",
			OpenAIBaseURL:    "https://openai.example.com",
			GeminiAPIKey:     "gk",
			GeminiAPIKeyFile: "/tmp/gk",
			GeminiBaseURL:    "https://gemini.example.com",
			OllamaURL:        "https://ollama.example.com",
		},
		LLM: LLMConfig{
			Provider:                "anthropic",
			Model:                   "test-model",
			AnthropicAPIKey:         "ak",
			AnthropicAPIKeyFile:     "/tmp/ak",
			AnthropicBaseURL:        "https://anthropic.example.com",
			OpenAIAPIKey:            "ok",
			OpenAIAPIKeyFile:        "/tmp/ok",
			OpenAIBaseURL:           "https://openai.example.com",
			GeminiAPIKey:            "gk",
			GeminiAPIKeyFile:        "/tmp/gk",
			GeminiBaseURL:           "https://gemini.example.com",
			OllamaURL:               "https://ollama.example.com",
			MaxTokens:               1234,
			MaxIterations:           7,
			Temperature:             0.5,
			TimeoutSeconds:          99,
			CompactToolDescriptions: "always",
		},
		Knowledgebase: KnowledgebaseConfig{
			Enabled:                   true,
			DatabasePath:              "/tmp/kb.db",
			EmbeddingProvider:         "openai",
			EmbeddingModel:            "text-embedding-3-small",
			EmbeddingVoyageAPIKey:     "vk",
			EmbeddingVoyageAPIKeyFile: "/tmp/vk",
			EmbeddingOpenAIAPIKey:     "ok",
			EmbeddingOpenAIAPIKeyFile: "/tmp/ok",
			EmbeddingVoyageBaseURL:    "https://voyage.example.com",
			EmbeddingOpenAIBaseURL:    "https://openai.example.com",
			EmbeddingGeminiAPIKey:     "gk",
			EmbeddingGeminiAPIKeyFile: "/tmp/gk",
			EmbeddingGeminiBaseURL:    "https://gemini.example.com",
			EmbeddingOllamaURL:        "https://ollama.example.com",
		},
		Memory:                MemoryConfig{Enabled: BoolPtr(false)},
		SecretFile:            "/tmp/secret",
		CustomDefinitionsPath: "/tmp/defs",
		DataDir:               "/tmp/data",
		TraceFile:             "/tmp/trace.log",
		ConnectionSecurity: ConnectionSecurityConfig{
			AllowInternalNetworks: true,
			AllowedHosts:          []string{"db.example.com"},
			BlockedHosts:          []string{"169.254.169.254"},
		},
		Builtins: BuiltinsConfig{
			Tools: ToolsConfig{
				QueryDatabase:       BoolPtr(false),
				GetSchemaInfo:       BoolPtr(false),
				SimilaritySearch:    BoolPtr(false),
				ExecuteExplain:      BoolPtr(false),
				GenerateEmbedding:   BoolPtr(false),
				SearchKnowledgebase: BoolPtr(false),
				CountRows:           BoolPtr(false),
				ListProbes:          BoolPtr(false),
				DescribeProbe:       BoolPtr(false),
				QueryMetrics:        BoolPtr(false),
				StoreMemory:         BoolPtr(false),
				RecallMemories:      BoolPtr(false),
				DeleteMemory:        BoolPtr(false),
			},
			Resources: ResourcesConfig{
				SystemInfo:     BoolPtr(false),
				ConnectionInfo: BoolPtr(false),
			},
		},
	}

	mergeConfig(dest, src)

	checks := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"address", dest.HTTP.Address, ":9443"},
		{"tls enabled", dest.HTTP.TLS.Enabled, true},
		{"tls cert", dest.HTTP.TLS.CertFile, "/tmp/cert.pem"},
		{"tls key", dest.HTTP.TLS.KeyFile, "/tmp/key.pem"},
		{"tls chain", dest.HTTP.TLS.ChainFile, "/tmp/chain.pem"},
		{"cors origin", dest.HTTP.CORSOrigin, "https://app.example.com"},
		{"hsts", dest.HTTP.HSTSEnabled, true},
		{"max user token days", dest.HTTP.Auth.MaxUserTokenDays, 30},
		{"rate limit window", dest.HTTP.Auth.RateLimitWindowMinutes, 5},
		{"rate limit attempts", dest.HTTP.Auth.RateLimitMaxAttempts, 3},
		{"embedding provider", dest.Embedding.Provider, "voyage"},
		{"embedding model", dest.Embedding.Model, "voyage-3"},
		{"embedding voyage key", dest.Embedding.VoyageAPIKey, "vk"},
		{"embedding voyage key file", dest.Embedding.VoyageAPIKeyFile, "/tmp/vk"},
		{"embedding openai key", dest.Embedding.OpenAIAPIKey, "ok"},
		{"embedding openai key file", dest.Embedding.OpenAIAPIKeyFile, "/tmp/ok"},
		{"embedding voyage url", dest.Embedding.VoyageBaseURL, "https://voyage.example.com"},
		{"embedding openai url", dest.Embedding.OpenAIBaseURL, "https://openai.example.com"},
		{"embedding gemini key", dest.Embedding.GeminiAPIKey, "gk"},
		{"embedding gemini key file", dest.Embedding.GeminiAPIKeyFile, "/tmp/gk"},
		{"embedding gemini url", dest.Embedding.GeminiBaseURL, "https://gemini.example.com"},
		{"embedding ollama url", dest.Embedding.OllamaURL, "https://ollama.example.com"},
		{"llm provider", dest.LLM.Provider, "anthropic"},
		{"llm model", dest.LLM.Model, "test-model"},
		{"llm anthropic key", dest.LLM.AnthropicAPIKey, "ak"},
		{"llm anthropic key file", dest.LLM.AnthropicAPIKeyFile, "/tmp/ak"},
		{"llm anthropic url", dest.LLM.AnthropicBaseURL, "https://anthropic.example.com"},
		{"llm openai key", dest.LLM.OpenAIAPIKey, "ok"},
		{"llm openai key file", dest.LLM.OpenAIAPIKeyFile, "/tmp/ok"},
		{"llm openai url", dest.LLM.OpenAIBaseURL, "https://openai.example.com"},
		{"llm gemini key", dest.LLM.GeminiAPIKey, "gk"},
		{"llm gemini key file", dest.LLM.GeminiAPIKeyFile, "/tmp/gk"},
		{"llm gemini url", dest.LLM.GeminiBaseURL, "https://gemini.example.com"},
		{"llm ollama url", dest.LLM.OllamaURL, "https://ollama.example.com"},
		{"llm max tokens", dest.LLM.MaxTokens, 1234},
		{"llm max iterations", dest.LLM.MaxIterations, 7},
		{"llm timeout", dest.LLM.TimeoutSeconds, 99},
		{"llm compact tool descriptions", dest.LLM.CompactToolDescriptions, "always"},
		{"kb path", dest.Knowledgebase.DatabasePath, "/tmp/kb.db"},
		{"kb provider", dest.Knowledgebase.EmbeddingProvider, "openai"},
		{"kb model", dest.Knowledgebase.EmbeddingModel, "text-embedding-3-small"},
		{"kb voyage key", dest.Knowledgebase.EmbeddingVoyageAPIKey, "vk"},
		{"kb voyage key file", dest.Knowledgebase.EmbeddingVoyageAPIKeyFile, "/tmp/vk"},
		{"kb openai key", dest.Knowledgebase.EmbeddingOpenAIAPIKey, "ok"},
		{"kb openai key file", dest.Knowledgebase.EmbeddingOpenAIAPIKeyFile, "/tmp/ok"},
		{"kb voyage url", dest.Knowledgebase.EmbeddingVoyageBaseURL, "https://voyage.example.com"},
		{"kb openai url", dest.Knowledgebase.EmbeddingOpenAIBaseURL, "https://openai.example.com"},
		{"kb gemini key", dest.Knowledgebase.EmbeddingGeminiAPIKey, "gk"},
		{"kb gemini key file", dest.Knowledgebase.EmbeddingGeminiAPIKeyFile, "/tmp/gk"},
		{"kb gemini url", dest.Knowledgebase.EmbeddingGeminiBaseURL, "https://gemini.example.com"},
		{"kb ollama url", dest.Knowledgebase.EmbeddingOllamaURL, "https://ollama.example.com"},
		{"secret file", dest.SecretFile, "/tmp/secret"},
		{"custom definitions", dest.CustomDefinitionsPath, "/tmp/defs"},
		{"data dir", dest.DataDir, "/tmp/data"},
		{"trace file", dest.TraceFile, "/tmp/trace.log"},
		{"allow internal networks", dest.ConnectionSecurity.AllowInternalNetworks, true},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, c.got, c.want)
		}
	}

	if dest.LLM.Temperature != 0.5 {
		t.Errorf("llm temperature: got %v, want 0.5", dest.LLM.Temperature)
	}
	if len(dest.HTTP.TrustedProxies) != 1 || dest.HTTP.TrustedProxies[0] != "192.0.2.0/24" {
		t.Errorf("trusted proxies: got %v", dest.HTTP.TrustedProxies)
	}
	if len(dest.ConnectionSecurity.AllowedHosts) != 1 {
		t.Errorf("allowed hosts: got %v", dest.ConnectionSecurity.AllowedHosts)
	}
	if len(dest.ConnectionSecurity.BlockedHosts) != 1 {
		t.Errorf("blocked hosts: got %v", dest.ConnectionSecurity.BlockedHosts)
	}
	if dest.Memory.Enabled == nil || *dest.Memory.Enabled {
		t.Error("memory enabled: expected an explicit false to be carried over")
	}

	boolPtrs := map[string]*bool{
		"query_database":       dest.Builtins.Tools.QueryDatabase,
		"get_schema_info":      dest.Builtins.Tools.GetSchemaInfo,
		"similarity_search":    dest.Builtins.Tools.SimilaritySearch,
		"execute_explain":      dest.Builtins.Tools.ExecuteExplain,
		"generate_embedding":   dest.Builtins.Tools.GenerateEmbedding,
		"search_knowledgebase": dest.Builtins.Tools.SearchKnowledgebase,
		"count_rows":           dest.Builtins.Tools.CountRows,
		"list_probes":          dest.Builtins.Tools.ListProbes,
		"describe_probe":       dest.Builtins.Tools.DescribeProbe,
		"query_metrics":        dest.Builtins.Tools.QueryMetrics,
		"store_memory":         dest.Builtins.Tools.StoreMemory,
		"recall_memories":      dest.Builtins.Tools.RecallMemories,
		"delete_memory":        dest.Builtins.Tools.DeleteMemory,
		"system_info":          dest.Builtins.Resources.SystemInfo,
		"connection_info":      dest.Builtins.Resources.ConnectionInfo,
	}
	for name, ptr := range boolPtrs {
		if ptr == nil || *ptr {
			t.Errorf("builtin %s: expected an explicit false to be carried over", name)
		}
	}
}

// TestLoadConfigSecretFile covers the early, partial read of
// secret_file that the command line depends on, including the cases
// where there is nothing to read: each of those must come back as an
// empty path so the caller falls through to the default search order,
// and only an unreadable or malformed file is an error.
func TestLoadConfigSecretFile(t *testing.T) {
	tmpDir := t.TempDir()

	write := func(name, content string) string {
		path := filepath.Join(tmpDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}
		return path
	}

	tests := []struct {
		name        string
		path        string
		expected    string
		expectError bool
	}{
		{
			name: "config with secret_file",
			path: write("with_secret.yaml", `
http:
    address: ":8080"
secret_file: /etc/pgedge/custom.secret
`),
			expected: "/etc/pgedge/custom.secret",
		},
		{
			name: "config without secret_file",
			path: write("without_secret.yaml", `
http:
    address: ":8080"
`),
			expected: "",
		},
		{
			name:     "non-existent file",
			path:     filepath.Join(tmpDir, "nonexistent.yaml"),
			expected: "",
		},
		{
			name:     "empty path",
			path:     "",
			expected: "",
		},
		{
			name:     "empty file",
			path:     write("empty_secret.yaml", ""),
			expected: "",
		},
		{
			name:        "invalid YAML",
			path:        write("invalid_secret.yaml", "this: is: not: valid: yaml: {{{{\n"),
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := LoadConfigSecretFile(tc.path)
			if tc.expectError {
				if err == nil {
					t.Fatalf("expected an error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.expected {
				t.Errorf("LoadConfigSecretFile = %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestLoadConfigSecretFileUnreadable checks that a file that exists but
// cannot be read is reported rather than treated as absent: falling
// through to the default search order there would look for the secret
// somewhere the operator did not put it.
func TestLoadConfigSecretFileUnreadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Windows maps only the owner-write bit, so a mode of 0000
		// still leaves the file readable, and os.Geteuid returns
		// -1 there rather than 0, so the check below cannot skip.
		t.Skip("mode 0000 does not prevent a read on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can read a 0000 file")
	}

	path := filepath.Join(t.TempDir(), "unreadable.yaml")
	if err := os.WriteFile(path, []byte("secret_file: /x\n"), 0000); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	if _, err := LoadConfigSecretFile(path); err == nil {
		t.Error("expected an unreadable config file to be reported")
	}
}

// TestMergeConfigMergesEveryToolsField is the structural guard against a
// tool being added to ToolsConfig without a matching merge block in
// mergeConfig, which is how six tools (including query_datastore) came to
// be silently unconfigurable. It walks ToolsConfig by reflection, sets
// every *bool on the source to false, and requires mergeConfig to carry
// all of them across, naming any field that did not make the trip.
func TestMergeConfigMergesEveryToolsField(t *testing.T) {
	src := &Config{}
	srcTools := reflect.ValueOf(&src.Builtins.Tools).Elem()
	toolsType := srcTools.Type()

	boolPtrType := reflect.TypeOf((*bool)(nil))
	for i := 0; i < toolsType.NumField(); i++ {
		if toolsType.Field(i).Type != boolPtrType {
			t.Fatalf("ToolsConfig.%s is %s, expected *bool; extend this test to cover it",
				toolsType.Field(i).Name, toolsType.Field(i).Type)
		}
		srcTools.Field(i).Set(reflect.ValueOf(boolPtr(false)))
	}

	dest := &Config{}
	mergeConfig(dest, src)

	destTools := reflect.ValueOf(&dest.Builtins.Tools).Elem()
	var unmerged []string
	for i := 0; i < toolsType.NumField(); i++ {
		name := toolsType.Field(i).Name
		got, ok := destTools.Field(i).Interface().(*bool)
		if !ok || got == nil {
			unmerged = append(unmerged, name+" (nil)")
			continue
		}
		if *got {
			unmerged = append(unmerged, name+" (true)")
		}
	}
	if len(unmerged) > 0 {
		t.Fatalf("mergeConfig did not merge these ToolsConfig fields: %s; "+
			"add a merge block for each in mergeConfig", strings.Join(unmerged, ", "))
	}
}

// TestToolsConfig_IsToolEnabledHonoursEveryField is the companion guard to
// TestMergeConfigMergesEveryToolsField: every *bool on ToolsConfig must
// have an IsToolEnabled case keyed on its yaml tag, so that a newly added
// tool cannot fall through to the default-true branch and stay enabled
// however the administrator configures it.
func TestToolsConfig_IsToolEnabledHonoursEveryField(t *testing.T) {
	toolsType := reflect.TypeOf(ToolsConfig{})
	for i := 0; i < toolsType.NumField(); i++ {
		field := toolsType.Field(i)
		toolName := strings.Split(field.Tag.Get("yaml"), ",")[0]
		if toolName == "" {
			t.Fatalf("ToolsConfig.%s has no yaml tag", field.Name)
		}

		t.Run(toolName, func(t *testing.T) {
			var cfg ToolsConfig
			if !cfg.IsToolEnabled(toolName) {
				t.Fatalf("IsToolEnabled(%q) = false with no value set, want true", toolName)
			}

			value := reflect.ValueOf(&cfg).Elem().Field(i)
			value.Set(reflect.ValueOf(boolPtr(false)))
			if cfg.IsToolEnabled(toolName) {
				t.Fatalf("IsToolEnabled(%q) = true with %s set to false; "+
					"add a case for %q to IsToolEnabled", toolName, field.Name, toolName)
			}

			value.Set(reflect.ValueOf(boolPtr(true)))
			if !cfg.IsToolEnabled(toolName) {
				t.Fatalf("IsToolEnabled(%q) = false with %s set to true", toolName, field.Name)
			}
		})
	}
}

// TestToolsConfig_IsToolEnabledBlackoutsAndRequiredTestQuery pins the two
// tools the documentation and the example configuration advertised whilst
// IsToolEnabled had no case for either: get_blackouts is switchable and must
// respect a false value, whereas test_query is required and must stay enabled
// however it is configured, including through a configuration file.
func TestToolsConfig_IsToolEnabledBlackoutsAndRequiredTestQuery(t *testing.T) {
	cfg := ToolsConfig{GetBlackouts: boolPtr(false)}
	if cfg.IsToolEnabled("get_blackouts") {
		t.Error("IsToolEnabled(\"get_blackouts\") = true, want false")
	}

	if !cfg.IsToolEnabled("test_query") {
		t.Error("IsToolEnabled(\"test_query\") = false, want true: test_query is required")
	}

	// A YAML round-trip guards against someone reintroducing a
	// test_query field on ToolsConfig: an operator switching it off in the
	// configuration file must not be able to disable the tool.
	var fromFile Config
	yamlSrc := "builtins:\n  tools:\n    test_query: false\n    get_blackouts: false\n"
	if err := yaml.Unmarshal([]byte(yamlSrc), &fromFile); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}
	if !fromFile.Builtins.Tools.IsToolEnabled("test_query") {
		t.Error("IsToolEnabled(\"test_query\") = false after test_query: false in YAML, " +
			"want true: test_query must not be configurable")
	}
	if fromFile.Builtins.Tools.IsToolEnabled("get_blackouts") {
		t.Error("IsToolEnabled(\"get_blackouts\") = true after get_blackouts: false in YAML, want false")
	}
}
