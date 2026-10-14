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
)

// TestDecisionConfigDefaults pins the defaults of the decision provider
// settings added for GitHub issue #626: off by default, so existing
// configurations behave exactly as before.
func TestDecisionConfigDefaults(t *testing.T) {
	cfg := NewConfig()
	if cfg.LLM.DecisionProvider != "" {
		t.Errorf("DecisionProvider = %q, want empty", cfg.LLM.DecisionProvider)
	}
	if cfg.LLM.DecisionProviderEnabled() {
		t.Error("DecisionProviderEnabled() = true by default, want false")
	}
	if got := cfg.Anomaly.Tier3.DecisionSuppressThreshold; got != DefaultDecisionSuppressThreshold {
		t.Errorf("DecisionSuppressThreshold = %v, want %v", got, DefaultDecisionSuppressThreshold)
	}
	if cfg.LLM.TypeSafe.DecisionModel != "jev-latest" {
		t.Errorf("TypeSafe.DecisionModel = %q, want jev-latest", cfg.LLM.TypeSafe.DecisionModel)
	}
	if cfg.LLM.OpenAI.DecisionModel != "gpt-6-luna" {
		t.Errorf("OpenAI.DecisionModel = %q, want gpt-6-luna", cfg.LLM.OpenAI.DecisionModel)
	}
	if cfg.GetTypeSafeAPIKey() != "" {
		t.Errorf("GetTypeSafeAPIKey() = %q before LoadAPIKeys, want empty", cfg.GetTypeSafeAPIKey())
	}
}

// TestDecisionProviderEnabled covers each value of llm.decision_provider.
func TestDecisionProviderEnabled(t *testing.T) {
	tests := []struct {
		provider string
		want     bool
	}{
		{"", false},
		{"none", false},
		{"disabled", false},
		{"typesafe", true},
		{"openai", true},
		{"bogus", true},
	}
	for _, tt := range tests {
		l := LLMConfig{DecisionProvider: tt.provider}
		if got := l.DecisionProviderEnabled(); got != tt.want {
			t.Errorf("DecisionProviderEnabled(%q) = %v, want %v", tt.provider, got, tt.want)
		}
	}
}

// TestValidateDecisionProvider covers the provider name and the suppress
// threshold, which is checked only whilst a decision provider is selected.
func TestValidateDecisionProvider(t *testing.T) {
	tests := []struct {
		name      string
		provider  string
		threshold float64
		wantErr   string
	}{
		{"unset ignores threshold", "", 7, ""},
		{"none ignores threshold", "none", -1, ""},
		{"disabled ignores threshold", "disabled", math.NaN(), ""},
		{"typesafe default", "typesafe", 0.5, ""},
		{"openai near bounds", "openai", 0.999, ""},
		{"typesafe low", "typesafe", 0.001, ""},
		{"unknown provider", "anthropic", 0.5, "llm.decision_provider"},
		{"zero threshold", "typesafe", 0, "decision_suppress_threshold"},
		{"negative threshold", "typesafe", -0.1, "decision_suppress_threshold"},
		{"one threshold", "openai", 1, "decision_suppress_threshold"},
		{"above one threshold", "openai", 1.5, "decision_suppress_threshold"},
		{"NaN threshold", "typesafe", math.NaN(), "decision_suppress_threshold"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewConfig()
			cfg.Datastore.Database = "db"
			cfg.Datastore.Username = "user"
			cfg.LLM.DecisionProvider = tt.provider
			cfg.Anomaly.Tier3.DecisionSuppressThreshold = tt.threshold
			err := cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

// TestLoadTypeSafeAPIKey covers reading, and failing to read, the
// TypeSafe API key file.
func TestLoadTypeSafeAPIKey(t *testing.T) {
	dir := t.TempDir()

	keyFile := filepath.Join(dir, "typesafe.key")
	if err := os.WriteFile(keyFile, []byte("ts-test-key\n"), 0600); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	cfg := NewConfig()
	cfg.LLM.TypeSafe.APIKeyFile = keyFile
	if err := cfg.LoadAPIKeys(); err != nil {
		t.Fatalf("LoadAPIKeys() = %v", err)
	}
	if got := cfg.GetTypeSafeAPIKey(); got != "ts-test-key" {
		t.Errorf("GetTypeSafeAPIKey() = %q, want ts-test-key", got)
	}

	emptyFile := filepath.Join(dir, "empty.key")
	if err := os.WriteFile(emptyFile, []byte("\n"), 0600); err != nil {
		t.Fatalf("write empty key file: %v", err)
	}
	for _, path := range []string{emptyFile, filepath.Join(dir, "missing.key")} {
		cfg := NewConfig()
		cfg.LLM.TypeSafe.APIKeyFile = path
		err := cfg.LoadAPIKeys()
		if err == nil || !strings.Contains(err.Error(), "TypeSafe") {
			t.Errorf("LoadAPIKeys(%s) = %v, want a TypeSafe key error", filepath.Base(path), err)
		}
	}
}

// TestLoadDecisionConfigFromFile checks the YAML key names.
func TestLoadDecisionConfigFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decision.yaml")
	content := `
llm:
  decision_provider: typesafe
  typesafe:
    api_key_file: /tmp/does-not-matter.key
    base_url: http://127.0.0.1:9999/v1
    decision_model: jev-test
  openai:
    decision_model: decide-test
anomaly:
  tier3:
    decision_suppress_threshold: 0.7
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg := NewConfig()
	if err := cfg.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile() = %v", err)
	}
	if cfg.LLM.DecisionProvider != "typesafe" {
		t.Errorf("DecisionProvider = %q", cfg.LLM.DecisionProvider)
	}
	if cfg.LLM.TypeSafe.APIKeyFile != "/tmp/does-not-matter.key" ||
		cfg.LLM.TypeSafe.BaseURL != "http://127.0.0.1:9999/v1" ||
		cfg.LLM.TypeSafe.DecisionModel != "jev-test" {
		t.Errorf("TypeSafe = %+v", cfg.LLM.TypeSafe)
	}
	if cfg.LLM.OpenAI.DecisionModel != "decide-test" {
		t.Errorf("OpenAI.DecisionModel = %q", cfg.LLM.OpenAI.DecisionModel)
	}
	if cfg.Anomaly.Tier3.DecisionSuppressThreshold != 0.7 {
		t.Errorf("DecisionSuppressThreshold = %v", cfg.Anomaly.Tier3.DecisionSuppressThreshold)
	}
	// Tier 3's other defaults survive a partial tier3 block.
	if !cfg.Anomaly.Tier3.Enabled || cfg.Anomaly.Tier3.TimeoutSeconds != 30 {
		t.Errorf("Tier3 = %+v, want defaults kept", cfg.Anomaly.Tier3)
	}
}
