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
)

// TestNewConfig tests that NewConfig creates a configuration with sensible defaults
func TestNewConfig(t *testing.T) {
	cfg := NewConfig()

	if cfg == nil {
		t.Fatal("NewConfig returned nil")
	}

	// Verify datastore defaults
	tests := []struct {
		name     string
		got      interface{}
		expected interface{}
	}{
		{"datastore host", cfg.Datastore.Host, "localhost"},
		{"datastore database", cfg.Datastore.Database, "ai_workbench"},
		{"datastore username", cfg.Datastore.Username, "postgres"},
		{"datastore port", cfg.Datastore.Port, 5432},
		{"datastore sslmode", cfg.Datastore.SSLMode, "prefer"},
		{"pool max connections", cfg.Pool.MaxConnections, 10},
		{"pool max idle seconds", cfg.Pool.MaxIdleSeconds, 300},
		{"threshold evaluation interval", cfg.Threshold.EvaluationIntervalSeconds, 60},
		{"anomaly enabled", cfg.Anomaly.Enabled, true},
		{"anomaly tier1 enabled", cfg.Anomaly.Tier1.Enabled, true},
		{"anomaly tier1 sensitivity", cfg.Anomaly.Tier1.DefaultSensitivity, 3.0},
		{"anomaly tier2 enabled", cfg.Anomaly.Tier2.Enabled, true},
		{"anomaly tier3 enabled", cfg.Anomaly.Tier3.Enabled, true},
		{"baselines refresh interval", cfg.Baselines.RefreshIntervalSeconds, 3600},
		{"correlation window", cfg.Correlation.WindowSeconds, 120},
		{"llm embedding provider", cfg.LLM.EmbeddingProvider, "ollama"},
		{"llm reasoning provider", cfg.LLM.ReasoningProvider, "ollama"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s = %v, expected %v", tt.name, tt.got, tt.expected)
			}
		})
	}
}

// TestConfigValidate tests the Validate method
func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name        string
		modifyFunc  func(*Config)
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid default config",
			modifyFunc:  func(c *Config) {},
			expectError: false,
		},
		{
			name: "missing host",
			modifyFunc: func(c *Config) {
				c.Datastore.Host = ""
			},
			expectError: true,
			errorMsg:    "datastore.host is required",
		},
		{
			name: "missing database",
			modifyFunc: func(c *Config) {
				c.Datastore.Database = ""
			},
			expectError: true,
			errorMsg:    "datastore.database is required",
		},
		{
			name: "missing username",
			modifyFunc: func(c *Config) {
				c.Datastore.Username = ""
			},
			expectError: true,
			errorMsg:    "datastore.username is required",
		},
		{
			name: "port zero",
			modifyFunc: func(c *Config) {
				c.Datastore.Port = 0
			},
			expectError: true,
			errorMsg:    "datastore.port must be between 1 and 65535",
		},
		{
			name: "port negative",
			modifyFunc: func(c *Config) {
				c.Datastore.Port = -1
			},
			expectError: true,
			errorMsg:    "datastore.port must be between 1 and 65535",
		},
		{
			name: "port too high",
			modifyFunc: func(c *Config) {
				c.Datastore.Port = 65536
			},
			expectError: true,
			errorMsg:    "datastore.port must be between 1 and 65535",
		},
		{
			name: "valid max port",
			modifyFunc: func(c *Config) {
				c.Datastore.Port = 65535
			},
			expectError: false,
		},
		{
			name: "valid min port",
			modifyFunc: func(c *Config) {
				c.Datastore.Port = 1
			},
			expectError: false,
		},
		{
			name: "pool max connections zero",
			modifyFunc: func(c *Config) {
				c.Pool.MaxConnections = 0
			},
			expectError: true,
			errorMsg:    "pool.max_connections must be greater than 0",
		},
		{
			name: "pool max connections negative",
			modifyFunc: func(c *Config) {
				c.Pool.MaxConnections = -5
			},
			expectError: true,
			errorMsg:    "pool.max_connections must be greater than 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewConfig()
			tt.modifyFunc(cfg)

			err := cfg.Validate()

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errorMsg)
				} else if err.Error() != tt.errorMsg {
					t.Errorf("expected error %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestLoadFromEnv tests the LoadFromEnv method
func TestLoadFromEnv(t *testing.T) {
	// Save original environment
	originalEnv := map[string]string{
		"AI_DBA_PG_HOST":        os.Getenv("AI_DBA_PG_HOST"),
		"AI_DBA_PG_HOSTADDR":    os.Getenv("AI_DBA_PG_HOSTADDR"),
		"AI_DBA_PG_DATABASE":    os.Getenv("AI_DBA_PG_DATABASE"),
		"AI_DBA_PG_USERNAME":    os.Getenv("AI_DBA_PG_USERNAME"),
		"AI_DBA_PG_PASSWORD":    os.Getenv("AI_DBA_PG_PASSWORD"),
		"AI_DBA_PG_SSLMODE":     os.Getenv("AI_DBA_PG_SSLMODE"),
		"AI_DBA_PG_SSLCERT":     os.Getenv("AI_DBA_PG_SSLCERT"),
		"AI_DBA_PG_SSLKEY":      os.Getenv("AI_DBA_PG_SSLKEY"),
		"AI_DBA_PG_SSLROOTCERT": os.Getenv("AI_DBA_PG_SSLROOTCERT"),
	}

	// Restore environment after test
	defer func() {
		for k, v := range originalEnv {
			if v == "" {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, v)
			}
		}
	}()

	tests := []struct {
		name    string
		envVars map[string]string
		checkFn func(*Config) bool
		desc    string
	}{
		{
			name: "override host",
			envVars: map[string]string{
				"AI_DBA_PG_HOST": "myhost.example.com",
			},
			checkFn: func(c *Config) bool {
				return c.Datastore.Host == "myhost.example.com"
			},
			desc: "host should be myhost.example.com",
		},
		{
			name: "override hostaddr",
			envVars: map[string]string{
				"AI_DBA_PG_HOSTADDR": "192.168.1.100",
			},
			checkFn: func(c *Config) bool {
				return c.Datastore.HostAddr == "192.168.1.100"
			},
			desc: "hostaddr should be 192.168.1.100",
		},
		{
			name: "override database",
			envVars: map[string]string{
				"AI_DBA_PG_DATABASE": "test_db",
			},
			checkFn: func(c *Config) bool {
				return c.Datastore.Database == "test_db"
			},
			desc: "database should be test_db",
		},
		{
			name: "override username",
			envVars: map[string]string{
				"AI_DBA_PG_USERNAME": "test_user",
			},
			checkFn: func(c *Config) bool {
				return c.Datastore.Username == "test_user"
			},
			desc: "username should be test_user",
		},
		{
			name: "override password",
			envVars: map[string]string{
				"AI_DBA_PG_PASSWORD": "secret123",
			},
			checkFn: func(c *Config) bool {
				return c.Datastore.Password == "secret123"
			},
			desc: "password should be secret123",
		},
		{
			name: "override sslmode",
			envVars: map[string]string{
				"AI_DBA_PG_SSLMODE": "require",
			},
			checkFn: func(c *Config) bool {
				return c.Datastore.SSLMode == "require"
			},
			desc: "sslmode should be require",
		},
		{
			name: "override ssl cert paths",
			envVars: map[string]string{
				"AI_DBA_PG_SSLCERT":     "/path/to/cert.pem",
				"AI_DBA_PG_SSLKEY":      "/path/to/key.pem",
				"AI_DBA_PG_SSLROOTCERT": "/path/to/root.pem",
			},
			checkFn: func(c *Config) bool {
				return c.Datastore.SSLCert == "/path/to/cert.pem" &&
					c.Datastore.SSLKey == "/path/to/key.pem" &&
					c.Datastore.SSLRootCert == "/path/to/root.pem"
			},
			desc: "ssl certificate paths should be set",
		},
		{
			name: "multiple overrides",
			envVars: map[string]string{
				"AI_DBA_PG_HOST":     "production.example.com",
				"AI_DBA_PG_DATABASE": "prod_db",
				"AI_DBA_PG_USERNAME": "prod_user",
				"AI_DBA_PG_SSLMODE":  "verify-full",
			},
			checkFn: func(c *Config) bool {
				return c.Datastore.Host == "production.example.com" &&
					c.Datastore.Database == "prod_db" &&
					c.Datastore.Username == "prod_user" &&
					c.Datastore.SSLMode == "verify-full"
			},
			desc: "multiple values should be overridden",
		},
		{
			name:    "empty env vars don't override",
			envVars: map[string]string{},
			checkFn: func(c *Config) bool {
				return c.Datastore.Host == "localhost" &&
					c.Datastore.Database == "ai_workbench"
			},
			desc: "defaults should remain when env is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear relevant env vars
			for k := range originalEnv {
				os.Unsetenv(k)
			}

			// Set test env vars
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			cfg := NewConfig()
			cfg.LoadFromEnv()

			if !tt.checkFn(cfg) {
				t.Errorf("check failed: %s", tt.desc)
			}
		})
	}
}

// TestLoadPassword tests the LoadPassword method
func TestLoadPassword(t *testing.T) {
	t.Run("password already set", func(t *testing.T) {
		cfg := NewConfig()
		cfg.Datastore.Password = "existing_password"

		err := cfg.LoadPassword()
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if cfg.Datastore.Password != "existing_password" {
			t.Errorf("password changed unexpectedly")
		}
	})

	t.Run("load from file", func(t *testing.T) {
		// Create a temporary password file
		tmpDir := t.TempDir()
		pwFile := filepath.Join(tmpDir, "password.txt")
		err := os.WriteFile(pwFile, []byte("file_password\n"), 0600)
		if err != nil {
			t.Fatalf("failed to create password file: %v", err)
		}

		cfg := NewConfig()
		cfg.Datastore.PasswordFile = pwFile

		err = cfg.LoadPassword()
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if cfg.Datastore.Password != "file_password" {
			t.Errorf("password = %q, expected %q", cfg.Datastore.Password, "file_password")
		}
	})

	t.Run("file not found", func(t *testing.T) {
		cfg := NewConfig()
		cfg.Datastore.PasswordFile = "/nonexistent/path/password.txt"

		err := cfg.LoadPassword()
		if err == nil {
			t.Error("expected error for nonexistent file, got nil")
		}
	})

	t.Run("no password file specified", func(t *testing.T) {
		cfg := NewConfig()

		err := cfg.LoadPassword()
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if cfg.Datastore.Password != "" {
			t.Errorf("password = %q, expected empty string", cfg.Datastore.Password)
		}
	})
}

// TestLoadAPIKeys tests the LoadAPIKeys method
func TestLoadAPIKeys(t *testing.T) {
	t.Run("load openai key", func(t *testing.T) {
		tmpDir := t.TempDir()
		keyFile := filepath.Join(tmpDir, "openai.key")
		err := os.WriteFile(keyFile, []byte("sk-test-openai-key\n"), 0600)
		if err != nil {
			t.Fatalf("failed to create key file: %v", err)
		}

		cfg := NewConfig()
		cfg.LLM.OpenAI.APIKeyFile = keyFile

		err = cfg.LoadAPIKeys()
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if cfg.GetOpenAIAPIKey() != "sk-test-openai-key" {
			t.Errorf("OpenAI API key = %q, expected %q", cfg.GetOpenAIAPIKey(), "sk-test-openai-key")
		}
	})

	t.Run("load anthropic key", func(t *testing.T) {
		tmpDir := t.TempDir()
		keyFile := filepath.Join(tmpDir, "anthropic.key")
		err := os.WriteFile(keyFile, []byte("sk-ant-test-key\n"), 0600)
		if err != nil {
			t.Fatalf("failed to create key file: %v", err)
		}

		cfg := NewConfig()
		cfg.LLM.Anthropic.APIKeyFile = keyFile

		err = cfg.LoadAPIKeys()
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if cfg.GetAnthropicAPIKey() != "sk-ant-test-key" {
			t.Errorf("Anthropic API key = %q, expected %q", cfg.GetAnthropicAPIKey(), "sk-ant-test-key")
		}
	})

	t.Run("load voyage key", func(t *testing.T) {
		tmpDir := t.TempDir()
		keyFile := filepath.Join(tmpDir, "voyage.key")
		err := os.WriteFile(keyFile, []byte("voyage-test-key\n"), 0600)
		if err != nil {
			t.Fatalf("failed to create key file: %v", err)
		}

		cfg := NewConfig()
		cfg.LLM.Voyage.APIKeyFile = keyFile

		err = cfg.LoadAPIKeys()
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if cfg.GetVoyageAPIKey() != "voyage-test-key" {
			t.Errorf("Voyage API key = %q, expected %q", cfg.GetVoyageAPIKey(), "voyage-test-key")
		}
	})

	t.Run("missing key file", func(t *testing.T) {
		cfg := NewConfig()
		cfg.LLM.OpenAI.APIKeyFile = "/nonexistent/openai.key"

		err := cfg.LoadAPIKeys()
		if err == nil {
			t.Error("expected error for missing key file, got nil")
		}
	})

	t.Run("no key files specified", func(t *testing.T) {
		cfg := NewConfig()

		err := cfg.LoadAPIKeys()
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

// TestLoadFromFile tests the LoadFromFile method
func TestLoadFromFile(t *testing.T) {
	t.Run("valid yaml file", func(t *testing.T) {
		tmpDir := t.TempDir()
		configFile := filepath.Join(tmpDir, "config.yaml")
		yamlContent := `
datastore:
  host: testhost
  port: 5433
  database: testdb
  username: testuser
  sslmode: require
pool:
  max_connections: 20
threshold:
  evaluation_interval_seconds: 120
`
		err := os.WriteFile(configFile, []byte(yamlContent), 0644)
		if err != nil {
			t.Fatalf("failed to create config file: %v", err)
		}

		cfg := NewConfig()
		err = cfg.LoadFromFile(configFile)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if cfg.Datastore.Host != "testhost" {
			t.Errorf("host = %q, expected %q", cfg.Datastore.Host, "testhost")
		}
		if cfg.Datastore.Port != 5433 {
			t.Errorf("port = %d, expected %d", cfg.Datastore.Port, 5433)
		}
		if cfg.Pool.MaxConnections != 20 {
			t.Errorf("max_connections = %d, expected %d", cfg.Pool.MaxConnections, 20)
		}
		if cfg.Threshold.EvaluationIntervalSeconds != 120 {
			t.Errorf("evaluation_interval = %d, expected %d",
				cfg.Threshold.EvaluationIntervalSeconds, 120)
		}
	})

	t.Run("file not found", func(t *testing.T) {
		cfg := NewConfig()
		err := cfg.LoadFromFile("/nonexistent/config.yaml")
		if err == nil {
			t.Error("expected error for nonexistent file, got nil")
		}
	})

	t.Run("invalid yaml", func(t *testing.T) {
		tmpDir := t.TempDir()
		configFile := filepath.Join(tmpDir, "invalid.yaml")
		err := os.WriteFile(configFile, []byte("invalid: yaml: content: ["), 0644)
		if err != nil {
			t.Fatalf("failed to create config file: %v", err)
		}

		cfg := NewConfig()
		err = cfg.LoadFromFile(configFile)
		if err == nil {
			t.Error("expected error for invalid yaml, got nil")
		}
	})
}

// TestConfigFileExists tests the ConfigFileExists function
func TestConfigFileExists(t *testing.T) {
	t.Run("file exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		configFile := filepath.Join(tmpDir, "config.yaml")
		err := os.WriteFile(configFile, []byte("test: true"), 0644)
		if err != nil {
			t.Fatalf("failed to create config file: %v", err)
		}

		if !ConfigFileExists(configFile) {
			t.Error("ConfigFileExists returned false for existing file")
		}
	})

	t.Run("file does not exist", func(t *testing.T) {
		if ConfigFileExists("/nonexistent/config.yaml") {
			t.Error("ConfigFileExists returned true for nonexistent file")
		}
	})
}

// TestGetDefaultConfigPath tests the GetDefaultConfigPath function
func TestGetDefaultConfigPath(t *testing.T) {
	// When system config doesn't exist, should return path relative to binary
	path := GetDefaultConfigPath("/usr/local/bin/ai-dba-alerter")
	expected := "/usr/local/bin/ai-dba-alerter.yaml"

	if path != expected {
		t.Errorf("GetDefaultConfigPath = %q, expected %q", path, expected)
	}
}

// TestAPIKeyGetters tests the API key getter methods
func TestAPIKeyGetters(t *testing.T) {
	cfg := NewConfig()

	// Initially, keys should be empty
	if cfg.GetOpenAIAPIKey() != "" {
		t.Errorf("GetOpenAIAPIKey = %q, expected empty string", cfg.GetOpenAIAPIKey())
	}
	if cfg.GetAnthropicAPIKey() != "" {
		t.Errorf("GetAnthropicAPIKey = %q, expected empty string", cfg.GetAnthropicAPIKey())
	}
	if cfg.GetVoyageAPIKey() != "" {
		t.Errorf("GetVoyageAPIKey = %q, expected empty string", cfg.GetVoyageAPIKey())
	}
}
