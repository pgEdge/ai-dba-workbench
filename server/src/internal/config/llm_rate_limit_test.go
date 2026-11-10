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

func TestOverviewRateLimitAccessors(t *testing.T) {
	tests := []struct {
		name        string
		cfg         OverviewRateLimitConfig
		wantPerHour int
		wantBurst   int
		wantRefresh int
	}{
		{
			name:        "unset uses defaults",
			cfg:         OverviewRateLimitConfig{},
			wantPerHour: DefaultOverviewGenerationsPerHour,
			wantBurst:   DefaultOverviewGenerationBurst,
			wantRefresh: DefaultOverviewRefreshIntervalSeconds,
		},
		{
			name: "explicit values are kept",
			cfg: OverviewRateLimitConfig{
				GenerationsPerHourPtr:     intPtr(120),
				BurstPtr:                  intPtr(5),
				RefreshIntervalSecondsPtr: intPtr(90),
			},
			wantPerHour: 120,
			wantBurst:   5,
			wantRefresh: 90,
		},
		{
			name: "explicit zero disables the limits but not the burst",
			cfg: OverviewRateLimitConfig{
				GenerationsPerHourPtr:     intPtr(0),
				BurstPtr:                  intPtr(0),
				RefreshIntervalSecondsPtr: intPtr(0),
			},
			wantPerHour: 0,
			wantBurst:   DefaultOverviewGenerationBurst,
			wantRefresh: 0,
		},
		{
			name: "negative values fall back to defaults",
			cfg: OverviewRateLimitConfig{
				GenerationsPerHourPtr:     intPtr(-1),
				BurstPtr:                  intPtr(-1),
				RefreshIntervalSecondsPtr: intPtr(-1),
			},
			wantPerHour: DefaultOverviewGenerationsPerHour,
			wantBurst:   DefaultOverviewGenerationBurst,
			wantRefresh: DefaultOverviewRefreshIntervalSeconds,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GenerationsPerHour(); got != tt.wantPerHour {
				t.Errorf("GenerationsPerHour() = %d, want %d", got, tt.wantPerHour)
			}
			if got := tt.cfg.Burst(); got != tt.wantBurst {
				t.Errorf("Burst() = %d, want %d", got, tt.wantBurst)
			}
			if got := tt.cfg.RefreshIntervalSeconds(); got != tt.wantRefresh {
				t.Errorf("RefreshIntervalSeconds() = %d, want %d", got, tt.wantRefresh)
			}
		})
	}
}

func TestOverviewRateLimitYAML(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	configContent := `
llm:
    rate_limit:
        overview:
            generations_per_hour: 0
            burst: 4
            refresh_interval_seconds: 120
`
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadConfig(configPath, CLIFlags{ConfigFileSet: true, ConfigFile: configPath})
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	rl := cfg.LLM.RateLimit.Overview
	if got := rl.GenerationsPerHour(); got != 0 {
		t.Errorf("GenerationsPerHour() = %d, want explicit 0", got)
	}
	if got := rl.Burst(); got != 4 {
		t.Errorf("Burst() = %d, want 4", got)
	}
	if got := rl.RefreshIntervalSeconds(); got != 120 {
		t.Errorf("RefreshIntervalSeconds() = %d, want 120", got)
	}
}

func TestOverviewRateLimitYAMLOmittedUsesDefaults(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("llm:\n    max_tokens: 1024\n"), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadConfig(configPath, CLIFlags{ConfigFileSet: true, ConfigFile: configPath})
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	rl := cfg.LLM.RateLimit.Overview
	if got := rl.GenerationsPerHour(); got != DefaultOverviewGenerationsPerHour {
		t.Errorf("GenerationsPerHour() = %d, want default %d", got, DefaultOverviewGenerationsPerHour)
	}
	if got := rl.Burst(); got != DefaultOverviewGenerationBurst {
		t.Errorf("Burst() = %d, want default %d", got, DefaultOverviewGenerationBurst)
	}
	if got := rl.RefreshIntervalSeconds(); got != DefaultOverviewRefreshIntervalSeconds {
		t.Errorf("RefreshIntervalSeconds() = %d, want default %d", got, DefaultOverviewRefreshIntervalSeconds)
	}
}
