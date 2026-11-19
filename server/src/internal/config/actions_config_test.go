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
	"strings"
	"testing"
	"time"
)

func TestActionsDefaults(t *testing.T) {
	t.Parallel()
	cfg := defaultConfig()
	a := cfg.Actions
	if a.LockTimeout != "5s" || a.StatementTimeout != "5m" ||
		a.MaxAttempts != 3 || a.ContextSnapshotLimit != 16384 {
		t.Fatalf("unexpected defaults: %+v", a)
	}
	if got := a.LockTimeoutDuration(); got != 5*time.Second {
		t.Errorf("lock timeout: got %v", got)
	}
	if got := a.StatementTimeoutDuration(); got != 5*time.Minute {
		t.Errorf("statement timeout: got %v", got)
	}
	if !cfg.Builtins.Tools.IsToolEnabled("propose_action") {
		t.Error("propose_action should be enabled by default")
	}
}

func TestActionsAccessorsZeroValue(t *testing.T) {
	t.Parallel()
	var a ActionsConfig
	if a.LockTimeoutDuration() != DefaultActionsLockTimeout {
		t.Error("zero lock timeout should fall back to the default")
	}
	if a.StatementTimeoutDuration() != DefaultActionsStatementTimeout {
		t.Error("zero statement timeout should fall back to the default")
	}
	bad := ActionsConfig{LockTimeout: "bogus", StatementTimeout: "-1s"}
	if bad.LockTimeoutDuration() != DefaultActionsLockTimeout ||
		bad.StatementTimeoutDuration() != DefaultActionsStatementTimeout {
		t.Error("unparsable or non-positive durations should fall back")
	}
}

func TestActionsYAMLAndMerge(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	yaml := "actions:\n  lock_timeout: 2s\n  statement_timeout: 90s\n" +
		"  max_attempts: 7\n  context_snapshot_limit: 4096\n" +
		"builtins:\n  tools:\n    propose_action: false\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path, CLIFlags{ConfigFileSet: true})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	a := cfg.Actions
	if a.LockTimeoutDuration() != 2*time.Second ||
		a.StatementTimeoutDuration() != 90*time.Second ||
		a.MaxAttempts != 7 || a.ContextSnapshotLimit != 4096 {
		t.Errorf("file values not applied: %+v", a)
	}
	if cfg.Builtins.Tools.IsToolEnabled("propose_action") {
		t.Error("propose_action should be disabled by the file")
	}
}

func TestActionsPartialMergeKeepsDefaults(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	if err := os.WriteFile(path, []byte("actions:\n  max_attempts: 5\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path, CLIFlags{ConfigFileSet: true})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	a := cfg.Actions
	if a.MaxAttempts != 5 || a.LockTimeout != "5s" ||
		a.StatementTimeout != "5m" || a.ContextSnapshotLimit != 16384 {
		t.Errorf("partial merge lost defaults: %+v", a)
	}
}

func TestActionsValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(*ActionsConfig)
		wantErr string
	}{
		{"valid", func(a *ActionsConfig) {}, ""},
		{"bad lock timeout", func(a *ActionsConfig) { a.LockTimeout = "soon" }, "actions.lock_timeout"},
		{"zero lock timeout", func(a *ActionsConfig) { a.LockTimeout = "0s" }, "actions.lock_timeout"},
		{"negative lock timeout", func(a *ActionsConfig) { a.LockTimeout = "-5s" }, "actions.lock_timeout"},
		{"sub-millisecond lock timeout", func(a *ActionsConfig) { a.LockTimeout = "500us" }, "actions.lock_timeout must be at least 1ms"},
		{"one millisecond lock timeout", func(a *ActionsConfig) { a.LockTimeout = "1ms" }, ""},
		{"bad statement timeout", func(a *ActionsConfig) { a.StatementTimeout = "x" }, "actions.statement_timeout"},
		{"sub-millisecond statement timeout", func(a *ActionsConfig) { a.StatementTimeout = "999999ns" }, "actions.statement_timeout must be at least 1ms"},
		{"one millisecond statement timeout", func(a *ActionsConfig) { a.StatementTimeout = "1ms" }, ""},
		{"zero statement timeout", func(a *ActionsConfig) { a.StatementTimeout = "0" }, "actions.statement_timeout"},
		{"negative attempts", func(a *ActionsConfig) { a.MaxAttempts = -1 }, "actions.max_attempts"},
		{"negative snapshot", func(a *ActionsConfig) { a.ContextSnapshotLimit = -1 }, "actions.context_snapshot_limit"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := defaultConfig()
			tc.mutate(&cfg.Actions)
			err := validateConfig(cfg)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("got %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestActionsInvalidFileRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	if err := os.WriteFile(path, []byte("actions:\n  max_attempts: -2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path, CLIFlags{ConfigFileSet: true}); err == nil {
		t.Fatal("expected a validation error for a negative max_attempts")
	}
}
