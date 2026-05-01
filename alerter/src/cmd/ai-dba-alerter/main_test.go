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
)

// TestResolveConfigPath_ExplicitFlag verifies that an explicit
// --config value is returned verbatim and is flagged as explicit.
// Whether the file actually exists is not the helper's concern;
// validating that is the caller's job.
func TestResolveConfigPath_ExplicitFlag(t *testing.T) {
	got := resolveConfigPath("/some/explicit/path.yaml")
	if got.Path != "/some/explicit/path.yaml" {
		t.Errorf("Path = %q, want %q", got.Path, "/some/explicit/path.yaml")
	}
	if !got.Explicit {
		t.Error("Explicit = false, want true")
	}
}

// TestResolveConfigPath_NoFlagNoFile verifies that an empty flag
// plus no candidate file resolves to the empty string with
// Explicit=false (the "fall through to defaults" signal).
func TestResolveConfigPath_NoFlagNoFile(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("HOME", base)
	t.Setenv("AppData", base)

	got := resolveConfigPath("")
	if got.Path != "" && got.Path != "/etc/pgedge/ai-dba-alerter.yaml" {
		t.Errorf("Path = %q, want empty string or /etc/pgedge fallback", got.Path)
	}
	if got.Explicit {
		t.Error("Explicit = true, want false")
	}
}

// TestResolveConfigPath_NoFlagUserDirHit verifies that an empty
// flag picks up a config file dropped into the per-user config
// directory and reports it as a non-explicit (auto-discovered)
// match.
func TestResolveConfigPath_NoFlagUserDirHit(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("HOME", base)
	t.Setenv("AppData", base)

	userDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("os.UserConfigDir(): %v", err)
	}
	pgedgeDir := filepath.Join(userDir, "pgedge")
	if err := os.MkdirAll(pgedgeDir, 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	expected := filepath.Join(pgedgeDir, "ai-dba-alerter.yaml")
	if err := os.WriteFile(expected, []byte("datastore:\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got := resolveConfigPath("")
	if got.Path != expected {
		t.Errorf("Path = %q, want %q", got.Path, expected)
	}
	if got.Explicit {
		t.Error("Explicit = true, want false")
	}
}
