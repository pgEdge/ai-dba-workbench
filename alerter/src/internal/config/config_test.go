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
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/pkg/fileutil"
	"gopkg.in/yaml.v3"
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
		got      any
		expected any
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
		{"llm max tokens", cfg.LLM.MaxTokens, DefaultLLMMaxTokens},
		{"gemini embedding model", cfg.LLM.Gemini.EmbeddingModel, "gemini-embedding-001"},
		{"gemini reasoning model", cfg.LLM.Gemini.ReasoningModel, "gemini-2.5-flash"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s = %v, expected %v", tt.name, tt.got, tt.expected)
			}
		})
	}
}

// TestReasoningMaxTokens verifies that the reasoning budget comes from
// llm.max_tokens and falls back to DefaultLLMMaxTokens whenever the
// setting is absent or non-positive. A nil receiver is covered because
// callers may hold a nil *LLMConfig.
func TestReasoningMaxTokens(t *testing.T) {
	tests := []struct {
		name string
		cfg  *LLMConfig
		want int
	}{
		{name: "nil config", cfg: nil, want: DefaultLLMMaxTokens},
		{name: "unset", cfg: &LLMConfig{}, want: DefaultLLMMaxTokens},
		{name: "zero", cfg: &LLMConfig{MaxTokens: 0}, want: DefaultLLMMaxTokens},
		{name: "negative", cfg: &LLMConfig{MaxTokens: -5}, want: DefaultLLMMaxTokens},
		{name: "configured", cfg: &LLMConfig{MaxTokens: 8192}, want: 8192},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.ReasoningMaxTokens(); got != tt.want {
				t.Errorf("ReasoningMaxTokens() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestNewConfigAnomalyDefaults verifies the defaults for the new
// anomaly knobs introduced for the tier-1 warmup gate and the
// hybrid variance floor. Every newly added field is asserted so
// the test naturally covers the new types in full.
func TestNewConfigAnomalyDefaults(t *testing.T) {
	cfg := NewConfig()

	tests := []struct {
		name     string
		got      any
		expected any
	}{
		// Existing default - sanity check we did not regress.
		{"tier1 default_sensitivity",
			cfg.Anomaly.Tier1.DefaultSensitivity, 3.0},

		// New: z-score cap.
		{"tier1 max_z_score",
			cfg.Anomaly.Tier1.MaxZScore, 100.0},

		// New: hybrid variance floor.
		{"tier1 variance_floor.relative_pct",
			cfg.Anomaly.Tier1.VarianceFloor.RelativePct, 0.05},
		{"tier1 variance_floor.absolute_floor",
			cfg.Anomaly.Tier1.VarianceFloor.AbsoluteFloor, 0.001},

		// New: warmup, indexed per period_type.
		{"tier1 warmup.all.min_samples",
			cfg.Anomaly.Tier1.Warmup.All.MinSamples, 100},
		{"tier1 warmup.all.min_span_hours",
			cfg.Anomaly.Tier1.Warmup.All.MinSpanHours, 24},
		{"tier1 warmup.hourly.min_samples",
			cfg.Anomaly.Tier1.Warmup.Hourly.MinSamples, 5},
		{"tier1 warmup.hourly.min_span_hours",
			cfg.Anomaly.Tier1.Warmup.Hourly.MinSpanHours, 120},
		{"tier1 warmup.daily.min_samples",
			cfg.Anomaly.Tier1.Warmup.Daily.MinSamples, 3},
		{"tier1 warmup.daily.min_span_hours",
			cfg.Anomaly.Tier1.Warmup.Daily.MinSpanHours, 336},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s = %v, expected %v",
					tt.name, tt.got, tt.expected)
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

// TestValidateRejectsBadAnomalyConfig covers the new Tier-1 range
// checks added to Validate(): MaxZScore, VarianceFloor.RelativePct,
// VarianceFloor.AbsoluteFloor, and Warmup.All.MinSamples. One sub-
// case per error path is sufficient since each branch is a simple
// numeric guard.
func TestValidateRejectsBadAnomalyConfig(t *testing.T) {
	tests := []struct {
		name       string
		modifyFunc func(*Config)
		errorMsg   string
	}{
		{
			name: "negative max_z_score rejected",
			modifyFunc: func(c *Config) {
				c.Anomaly.Tier1.MaxZScore = -1.0
			},
			errorMsg: "anomaly.tier1.max_z_score must be a finite non-negative number",
		},
		{
			name: "positive Inf max_z_score rejected",
			modifyFunc: func(c *Config) {
				c.Anomaly.Tier1.MaxZScore = math.Inf(1)
			},
			errorMsg: "anomaly.tier1.max_z_score must be a finite non-negative number",
		},
		{
			name: "NaN relative_pct rejected",
			modifyFunc: func(c *Config) {
				c.Anomaly.Tier1.VarianceFloor.RelativePct = math.NaN()
			},
			errorMsg: "anomaly.tier1.variance_floor.relative_pct must be a finite non-negative number",
		},
		{
			name: "negative Inf relative_pct rejected",
			modifyFunc: func(c *Config) {
				c.Anomaly.Tier1.VarianceFloor.RelativePct = math.Inf(-1)
			},
			errorMsg: "anomaly.tier1.variance_floor.relative_pct must be a finite non-negative number",
		},
		{
			name: "negative absolute_floor rejected",
			modifyFunc: func(c *Config) {
				c.Anomaly.Tier1.VarianceFloor.AbsoluteFloor = -0.5
			},
			errorMsg: "anomaly.tier1.variance_floor.absolute_floor must be a finite non-negative number",
		},
		{
			name: "positive Inf absolute_floor rejected",
			modifyFunc: func(c *Config) {
				c.Anomaly.Tier1.VarianceFloor.AbsoluteFloor = math.Inf(1)
			},
			errorMsg: "anomaly.tier1.variance_floor.absolute_floor must be a finite non-negative number",
		},
		{
			name: "negative warmup all.min_samples rejected",
			modifyFunc: func(c *Config) {
				c.Anomaly.Tier1.Warmup.All.MinSamples = -1
			},
			errorMsg: "anomaly.tier1.warmup thresholds must be >= 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewConfig()
			tt.modifyFunc(cfg)

			err := cfg.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil",
					tt.errorMsg)
			}
			if err.Error() != tt.errorMsg {
				t.Errorf("expected error %q, got %q",
					tt.errorMsg, err.Error())
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

	t.Run("empty file is an error", func(t *testing.T) {
		tmpDir := t.TempDir()
		pwFile := filepath.Join(tmpDir, "empty.txt")
		if err := os.WriteFile(pwFile, []byte("\n"), 0600); err != nil {
			t.Fatalf("failed to create password file: %v", err)
		}

		cfg := NewConfig()
		cfg.Datastore.PasswordFile = pwFile
		if err := cfg.LoadPassword(); err == nil {
			t.Error("expected error for empty password file, got nil")
		}
	})

	t.Run("preserves interior whitespace", func(t *testing.T) {
		tmpDir := t.TempDir()
		pwFile := filepath.Join(tmpDir, "pw.txt")
		if err := os.WriteFile(pwFile, []byte("file password \n"), 0600); err != nil {
			t.Fatalf("failed to create password file: %v", err)
		}

		cfg := NewConfig()
		cfg.Datastore.PasswordFile = pwFile
		if err := cfg.LoadPassword(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Datastore.Password != "file password " {
			t.Errorf("password = %q, expected %q", cfg.Datastore.Password, "file password ")
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

	t.Run("load gemini key", func(t *testing.T) {
		tmpDir := t.TempDir()
		keyFile := filepath.Join(tmpDir, "gemini.key")
		err := os.WriteFile(keyFile, []byte("gemini-test-key\n"), 0600)
		if err != nil {
			t.Fatalf("failed to create key file: %v", err)
		}

		cfg := NewConfig()
		cfg.LLM.Gemini.APIKeyFile = keyFile

		err = cfg.LoadAPIKeys()
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if cfg.GetGeminiAPIKey() != "gemini-test-key" {
			t.Errorf("Gemini API key = %q, expected %q",
				cfg.GetGeminiAPIKey(), "gemini-test-key")
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

	t.Run("empty key file is an error", func(t *testing.T) {
		tmpDir := t.TempDir()
		emptyFile := filepath.Join(tmpDir, "empty.key")
		if err := os.WriteFile(emptyFile, []byte("\n"), 0600); err != nil {
			t.Fatalf("failed to create key file: %v", err)
		}

		// Exercise each provider's error branch.
		setters := []func(*Config){
			func(c *Config) { c.LLM.OpenAI.APIKeyFile = emptyFile },
			func(c *Config) { c.LLM.Anthropic.APIKeyFile = emptyFile },
			func(c *Config) { c.LLM.Voyage.APIKeyFile = emptyFile },
			func(c *Config) { c.LLM.Gemini.APIKeyFile = emptyFile },
		}
		for i, set := range setters {
			cfg := NewConfig()
			set(cfg)
			if err := cfg.LoadAPIKeys(); err == nil {
				t.Errorf("provider %d: expected error for empty key file, got nil", i)
			}
		}
	})

	t.Run("missing anthropic voyage gemini key files", func(t *testing.T) {
		setters := []func(*Config){
			func(c *Config) { c.LLM.Anthropic.APIKeyFile = "/nonexistent/anthropic.key" },
			func(c *Config) { c.LLM.Voyage.APIKeyFile = "/nonexistent/voyage.key" },
			func(c *Config) { c.LLM.Gemini.APIKeyFile = "/nonexistent/gemini.key" },
		}
		for i, set := range setters {
			cfg := NewConfig()
			set(cfg)
			if err := cfg.LoadAPIKeys(); err == nil {
				t.Errorf("provider %d: expected error for missing key file, got nil", i)
			}
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

	t.Run("gemini embedding model round-trip", func(t *testing.T) {
		tmpDir := t.TempDir()
		configFile := filepath.Join(tmpDir, "gemini.yaml")
		yamlContent := `
llm:
  embedding_provider: gemini
  gemini:
    embedding_model: gemini-embedding-2
    reasoning_model: gemini-2.5-pro
`
		if err := os.WriteFile(configFile, []byte(yamlContent), 0644); err != nil {
			t.Fatalf("failed to create config file: %v", err)
		}

		cfg := NewConfig()
		if err := cfg.LoadFromFile(configFile); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if cfg.LLM.Gemini.EmbeddingModel != "gemini-embedding-2" {
			t.Errorf("gemini embedding_model = %q, expected %q",
				cfg.LLM.Gemini.EmbeddingModel, "gemini-embedding-2")
		}
		if cfg.LLM.Gemini.ReasoningModel != "gemini-2.5-pro" {
			t.Errorf("gemini reasoning_model = %q, expected %q",
				cfg.LLM.Gemini.ReasoningModel, "gemini-2.5-pro")
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

// TestExampleConfigsParse loads each of the shipped example
// alerter YAML files and confirms that the tier-1 anomaly knobs
// added for the warmup gate, variance floor, and z-score cap
// round-trip correctly through LoadFromFile. It also verifies
// that each of the new keys is explicitly present in the raw
// YAML, not merely supplied by NewConfig defaults during the
// merge; this catches drift between the live config schema and
// the annotated examples that operators copy from.
func TestExampleConfigsParse(t *testing.T) {
	paths := []string{
		"../../../../examples/ai-dba-alerter.yaml",
		"../../../../examples/walkthrough/config/ai-dba-alerter.yaml",
		"../../../../docker/config/ai-dba-alerter.yaml",
	}

	requiredKeys := []string{
		"anomaly.tier1.max_z_score",
		"anomaly.tier1.variance_floor.relative_pct",
		"anomaly.tier1.variance_floor.absolute_floor",
		"anomaly.tier1.warmup.all.min_samples",
		"anomaly.tier1.warmup.all.min_span_hours",
		"anomaly.tier1.warmup.hourly.min_samples",
		"anomaly.tier1.warmup.hourly.min_span_hours",
		"anomaly.tier1.warmup.daily.min_samples",
		"anomaly.tier1.warmup.daily.min_span_hours",
	}

	for _, p := range paths {
		p := p
		t.Run(p, func(t *testing.T) {
			// First check the raw YAML for explicit key presence.
			// Without this, a missing key would still yield the
			// correct merged value because NewConfig supplies a
			// non-zero default, defeating the drift check.
			raw, err := os.ReadFile(p) // #nosec G304 - test fixture path
			if err != nil {
				t.Fatalf("read %s: %v", p, err)
			}
			var m map[string]any
			if err := yaml.Unmarshal(raw, &m); err != nil {
				t.Fatalf("yaml unmarshal %s: %v", p, err)
			}
			for _, key := range requiredKeys {
				if !hasNestedKey(m, strings.Split(key, ".")) {
					t.Errorf("%s: missing key %s", p, key)
				}
			}

			// Then confirm that the merged values match the
			// documented defaults, catching example/schema drift
			// in values as well as in key presence.
			cfg := NewConfig()
			if err := cfg.LoadFromFile(p); err != nil {
				t.Fatalf("failed to parse %s: %v", p, err)
			}

			if cfg.Anomaly.Tier1.MaxZScore != 100.0 {
				t.Errorf("%s: MaxZScore = %v, want 100.0",
					p, cfg.Anomaly.Tier1.MaxZScore)
			}
			if cfg.Anomaly.Tier1.VarianceFloor.RelativePct != 0.05 {
				t.Errorf("%s: VarianceFloor.RelativePct = %v, want 0.05",
					p, cfg.Anomaly.Tier1.VarianceFloor.RelativePct)
			}
			if cfg.Anomaly.Tier1.VarianceFloor.AbsoluteFloor != 0.001 {
				t.Errorf("%s: VarianceFloor.AbsoluteFloor = %v, want 0.001",
					p, cfg.Anomaly.Tier1.VarianceFloor.AbsoluteFloor)
			}
			if cfg.Anomaly.Tier1.Warmup.All.MinSamples != 100 {
				t.Errorf("%s: Warmup.All.MinSamples = %d, want 100",
					p, cfg.Anomaly.Tier1.Warmup.All.MinSamples)
			}
			if cfg.Anomaly.Tier1.Warmup.All.MinSpanHours != 24 {
				t.Errorf("%s: Warmup.All.MinSpanHours = %d, want 24",
					p, cfg.Anomaly.Tier1.Warmup.All.MinSpanHours)
			}
			if cfg.Anomaly.Tier1.Warmup.Hourly.MinSamples != 5 {
				t.Errorf("%s: Warmup.Hourly.MinSamples = %d, want 5",
					p, cfg.Anomaly.Tier1.Warmup.Hourly.MinSamples)
			}
			if cfg.Anomaly.Tier1.Warmup.Hourly.MinSpanHours != 120 {
				t.Errorf("%s: Warmup.Hourly.MinSpanHours = %d, want 120",
					p, cfg.Anomaly.Tier1.Warmup.Hourly.MinSpanHours)
			}
			if cfg.Anomaly.Tier1.Warmup.Daily.MinSamples != 3 {
				t.Errorf("%s: Warmup.Daily.MinSamples = %d, want 3",
					p, cfg.Anomaly.Tier1.Warmup.Daily.MinSamples)
			}
			if cfg.Anomaly.Tier1.Warmup.Daily.MinSpanHours != 336 {
				t.Errorf("%s: Warmup.Daily.MinSpanHours = %d, want 336",
					p, cfg.Anomaly.Tier1.Warmup.Daily.MinSpanHours)
			}
		})
	}
}

// hasNestedKey walks a nested map with the given path segments
// and reports whether the leaf is explicitly present (non-nil).
// Used by TestExampleConfigsParse to confirm example files set
// the new anomaly keys rather than relying on NewConfig defaults.
func hasNestedKey(m map[string]any, path []string) bool {
	if len(path) == 0 {
		return false
	}
	head, rest := path[0], path[1:]
	v, ok := m[head]
	if !ok {
		return false
	}
	if len(rest) == 0 {
		return v != nil
	}
	inner, ok := v.(map[string]any)
	if !ok {
		return false
	}
	return hasNestedKey(inner, rest)
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

// TestGetDefaultConfigPath verifies the alerter wrapper returns
// "" when no candidate file is present. The binaryPath argument
// is no longer consulted. The system-wide fallback is redirected
// at a non-existent path so the assertion is exact and not
// dependent on whether the test host has /etc/pgedge populated.
func TestGetDefaultConfigPath(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("HOME", base)
	t.Setenv("AppData", base)
	fileutil.SetSystemConfigDirForTest(t, filepath.Join(base, "absent-etc-pgedge"))

	path := GetDefaultConfigPath("/usr/local/bin/ai-dba-alerter")
	if path != "" {
		t.Errorf("GetDefaultConfigPath = %q, want empty path", path)
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
	expected := filepath.Join(pgedgeDir, "ai-dba-alerter.yaml")
	if err := os.WriteFile(expected, []byte("datastore:\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if got := GetDefaultConfigPath(""); got != expected {
		t.Errorf("GetDefaultConfigPath = %q, want %q", got, expected)
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

// isolateSecretDirs redirects both default secret-search locations to
// fresh temporary directories so ResolveServerSecret never touches the
// host's real per-user config dir or /etc/pgedge. It returns the
// resolved per-user pgedge directory and the system-wide directory so
// callers can place candidate secret files.
func isolateSecretDirs(t *testing.T) (userPgedgeDir, systemDir string) {
	t.Helper()

	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("HOME", base)
	t.Setenv("AppData", base)

	systemDir = filepath.Join(base, "absent-etc-pgedge")
	fileutil.SetSystemConfigDirForTest(t, systemDir)

	userDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("os.UserConfigDir: %v", err)
	}
	userPgedgeDir = filepath.Join(userDir, "pgedge")

	return userPgedgeDir, systemDir
}

// TestResolveServerSecret covers the alerter's server-secret search
// order: an explicit SecretFile, fallback to the per-user config dir,
// fallback to /etc/pgedge, and the not-found error path.
func TestResolveServerSecret(t *testing.T) {
	t.Run("explicit secret file honored", func(t *testing.T) {
		// Isolate defaults so a stray host file cannot satisfy the
		// lookup; the explicit path must take precedence regardless.
		isolateSecretDirs(t)

		secretPath := filepath.Join(t.TempDir(), "explicit.secret")
		if err := os.WriteFile(secretPath, []byte("explicit-secret\n"), 0600); err != nil {
			t.Fatalf("failed to write secret file: %v", err)
		}

		cfg := &NotificationsConfig{SecretFile: secretPath}
		secret, err := cfg.ResolveServerSecret()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if secret != "explicit-secret" {
			t.Errorf("secret = %q, expected %q", secret, "explicit-secret")
		}
	})

	t.Run("explicit secret file read error", func(t *testing.T) {
		isolateSecretDirs(t)

		cfg := &NotificationsConfig{SecretFile: "/nonexistent/path/alerter.secret"}
		_, err := cfg.ResolveServerSecret()
		if err == nil {
			t.Fatal("expected error for nonexistent explicit secret file")
		}
		if !strings.Contains(err.Error(), "failed to read secret file") {
			t.Errorf("error = %q, expected it to mention reading the secret file", err)
		}
	})

	t.Run("fallback to per-user config dir", func(t *testing.T) {
		userPgedgeDir, _ := isolateSecretDirs(t)

		if err := os.MkdirAll(userPgedgeDir, 0700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		candidate := filepath.Join(userPgedgeDir, "ai-dba-alerter.secret")
		if err := os.WriteFile(candidate, []byte("user-dir-secret\n"), 0600); err != nil {
			t.Fatalf("failed to write secret file: %v", err)
		}

		cfg := &NotificationsConfig{}
		secret, err := cfg.ResolveServerSecret()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if secret != "user-dir-secret" {
			t.Errorf("secret = %q, expected %q", secret, "user-dir-secret")
		}
	})

	t.Run("fallback to /etc/pgedge", func(t *testing.T) {
		_, systemDir := isolateSecretDirs(t)

		if err := os.MkdirAll(systemDir, 0700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		candidate := filepath.Join(systemDir, "ai-dba-alerter.secret")
		if err := os.WriteFile(candidate, []byte("system-dir-secret\n"), 0600); err != nil {
			t.Fatalf("failed to write secret file: %v", err)
		}

		cfg := &NotificationsConfig{}
		secret, err := cfg.ResolveServerSecret()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if secret != "system-dir-secret" {
			t.Errorf("secret = %q, expected %q", secret, "system-dir-secret")
		}
	})

	t.Run("error names both locations when none found", func(t *testing.T) {
		isolateSecretDirs(t)

		cfg := &NotificationsConfig{}
		_, err := cfg.ResolveServerSecret()
		if err == nil {
			t.Fatal("expected error when no secret file exists on default paths")
		}
		msg := err.Error()
		if !strings.Contains(msg, "per-user config dir") {
			t.Errorf("error = %q, expected it to mention the per-user config dir", msg)
		}
		if !strings.Contains(msg, "/etc/pgedge/ai-dba-alerter.secret") {
			t.Errorf("error = %q, expected it to mention /etc/pgedge/ai-dba-alerter.secret", msg)
		}
	})
}
