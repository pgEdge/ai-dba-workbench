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
	"strings"
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
// re-resolves a YAML password_file. The reload does NOT populate the
// marshalable DatabaseConfig.Password field; the file-sourced secret is
// kept out of Password and surfaced only through EffectivePassword
// (matching startup behavior).
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

// TestReloadRefusesWithoutAUsableFile covers the two refusals that come
// before validation: no file to read, and a file that will not parse.
// Each keeps the previous configuration.
func TestReloadRefusesWithoutAUsableFile(t *testing.T) {
	initial := defaultConfig()

	rc := NewReloadableConfig(initial, "", CLIFlags{})
	if err := rc.Reload(); err == nil || !strings.Contains(err.Error(), "no configuration file path set") {
		t.Errorf("Reload() with no path = %v, want a refusal", err)
	}

	path := writeTempConfig(t, "http: [unterminated\n")
	rc = NewReloadableConfig(initial, path, CLIFlags{ConfigFileSet: true, ConfigFile: path})
	if err := rc.Reload(); err == nil || !strings.Contains(err.Error(), "failed to load configuration") {
		t.Errorf("Reload() of malformed YAML = %v, want a load failure", err)
	}
	if rc.Get() != initial {
		t.Error("a refused reload swapped the configuration")
	}
}

// TestReloadReportsADatastoreThatWasRemoved checks the reload log when
// the new file drops the database block the server started with.
func TestReloadReportsADatastoreThatWasRemoved(t *testing.T) {
	initial := defaultConfig()
	initial.Database = &DatabaseConfig{User: "workbench", Host: "localhost", Port: 5432, Database: "workbench"}
	path := writeTempConfig(t, "http:\n  address: \":8080\"\n")
	rc := NewReloadableConfig(initial, path, CLIFlags{ConfigFileSet: true, ConfigFile: path})

	var err error
	out := captureStderr(t, func() { err = rc.Reload() })
	if err != nil {
		t.Fatalf("Reload() = %v", err)
	}
	for _, want := range []string{"Database: not configured", "Database configuration changed"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected stderr to contain %q, got:\n%s", want, out)
		}
	}
}

// TestReloadReportsNonAuthChanges covers the HTTP, TLS and provider
// changes logRestartRequiredSettings reports alongside the OIDC ones.
func TestReloadReportsNonAuthChanges(t *testing.T) {
	old := defaultConfig()
	cur := defaultConfig()
	cur.HTTP.Address = ":9443"
	cur.HTTP.TLS.Enabled = !old.HTTP.TLS.Enabled
	cur.HTTP.TLS.CertFile = "/etc/workbench/new-cert.pem"
	cur.HTTP.TLS.KeyFile = "/etc/workbench/new-key.pem"
	cur.LLM.Provider = "ollama"
	cur.LLM.Model = "a-different-model"
	cur.Embedding.Provider = "voyage"

	rc := &ReloadableConfig{config: old, startup: old}
	out := captureStderr(t, func() { rc.logRestartRequiredSettings(cur) })
	for _, want := range []string{
		"WARNING: http.address changed - requires restart",
		"WARNING: http.tls.enabled changed - requires restart",
		"WARNING: http.tls.cert_file changed - requires restart",
		"WARNING: http.tls.key_file changed - requires restart",
		"NOTE: llm.provider changed to ollama",
		"NOTE: llm.model changed to a-different-model",
		"NOTE: embedding.provider changed to voyage",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected stderr to contain %q, got:\n%s", want, out)
		}
	}
}
