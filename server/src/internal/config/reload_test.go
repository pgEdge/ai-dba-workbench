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
	"os"
	"path/filepath"
	"testing"
)

// writeReloadConfig writes a minimal valid server config to a temp file
// and returns its path. The database user is always set so the config
// passes validateConfig. The passwordFile argument, when non-empty, is
// written into the database block as password_file.
func writeReloadConfig(t *testing.T, dir, passwordFile string) string {
	t.Helper()

	pwLine := ""
	if passwordFile != "" {
		pwLine = fmt.Sprintf("  password_file: %q\n", passwordFile)
	}

	body := fmt.Sprintf(`database:
  host: 127.0.0.1
  port: 5432
  database: ai_workbench
  user: postgres
%s`, pwLine)

	path := filepath.Join(dir, "ai-dba-server.yaml")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// TestReloadResolvesPasswordFile confirms that a SIGHUP-style reload
// re-resolves a YAML password_file, so the reloaded DatabaseConfig has
// its Password populated from the file contents (matching startup).
func TestReloadResolvesPasswordFile(t *testing.T) {
	dir := t.TempDir()

	const secret = "s3cr3t-reload-pw"
	pwFile := filepath.Join(dir, "db.password")
	if err := os.WriteFile(pwFile, []byte(secret+"\n"), 0600); err != nil {
		t.Fatalf("write password file: %v", err)
	}

	cfgPath := writeReloadConfig(t, dir, pwFile)

	// Build an initial config from the same file; it should already have
	// the password resolved if we run LoadPassword, but the reloadable
	// wrapper starts from whatever we hand it.
	initial, err := LoadConfig(cfgPath, CLIFlags{ConfigFileSet: true, ConfigFile: cfgPath})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	rc := NewReloadableConfig(initial, cfgPath, CLIFlags{ConfigFileSet: true, ConfigFile: cfgPath})

	var seenByCallback string
	rc.OnReload(func(newCfg *Config) {
		if newCfg.Database != nil {
			seenByCallback = newCfg.Database.EffectivePassword()
		}
	})

	if err := rc.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	got := rc.Get()
	if got.Database == nil {
		t.Fatal("reloaded config has nil Database")
	}
	// A file-sourced secret must not populate the marshalable Password
	// field; it is resolved into the unexported field and surfaced only
	// through EffectivePassword.
	if got.Database.Password != "" {
		t.Errorf("reloaded Password = %q, want empty (file-sourced secret must not leak)", got.Database.Password)
	}
	if got.Database.EffectivePassword() != secret {
		t.Errorf("reloaded EffectivePassword() = %q, want %q", got.Database.EffectivePassword(), secret)
	}
	if seenByCallback != secret {
		t.Errorf("onReload callback saw EffectivePassword %q, want %q", seenByCallback, secret)
	}
}

// TestReloadMissingPasswordFileAbortsReload confirms that a reload whose
// password_file cannot be read returns an error and does NOT swap the
// active config.
func TestReloadMissingPasswordFileAbortsReload(t *testing.T) {
	dir := t.TempDir()

	// Start from a valid config with no password_file.
	startPath := writeReloadConfig(t, dir, "")
	initial, err := LoadConfig(startPath, CLIFlags{ConfigFileSet: true, ConfigFile: startPath})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	rc := NewReloadableConfig(initial, startPath, CLIFlags{ConfigFileSet: true, ConfigFile: startPath})

	callbackRan := false
	rc.OnReload(func(_ *Config) { callbackRan = true })

	// Rewrite the same config path to point at a missing password file.
	missing := filepath.Join(dir, "does-not-exist.password")
	if err := os.WriteFile(startPath, []byte(fmt.Sprintf(`database:
  host: 127.0.0.1
  port: 5432
  database: ai_workbench
  user: postgres
  password_file: %q
`, missing)), 0600); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}

	err = rc.Reload()
	if err == nil {
		t.Fatal("Reload() expected error for missing password file, got nil")
	}

	// Config must not have been swapped: it is still the initial pointer.
	if rc.Get() != initial {
		t.Error("Reload() swapped config despite password resolution failure")
	}
	if callbackRan {
		t.Error("onReload callback ran despite failed reload")
	}
}
