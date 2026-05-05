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
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/pkg/fileutil"
)

// minimalValidYAML is a config payload that satisfies
// config.Validate so reloadConfigOnSignal returns a config
// instead of a validation error.
const minimalValidYAML = "datastore:\n" +
	"  host: testhost\n" +
	"  port: 5432\n" +
	"  database: testdb\n" +
	"  username: testuser\n" +
	"pool:\n" +
	"  max_connections: 5\n"

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
// Explicit=false (the "fall through to defaults" signal). The
// system-wide fallback is redirected so the assertion is exact
// regardless of the test host's /etc/pgedge state.
func TestResolveConfigPath_NoFlagNoFile(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("HOME", base)
	t.Setenv("AppData", base)
	fileutil.SetSystemConfigDirForTest(t, filepath.Join(base, "absent-etc-pgedge"))

	got := resolveConfigPath("")
	if got.Path != "" {
		t.Errorf("Path = %q, want empty string", got.Path)
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

// TestReloadConfigOnSignal_ExplicitMissing verifies the headline
// fix from issue #195's CodeRabbit thread: when the operator
// started the alerter with --config <path> and the file later
// vanishes, a SIGHUP reload must NOT replace the running config
// with compiled-in defaults. The helper returns (nil, nil) and
// logs a descriptive error so the caller can keep the existing
// config.
func TestReloadConfigOnSignal_ExplicitMissing(t *testing.T) {
	var buf bytes.Buffer
	got, err := reloadConfigOnSignal(
		&buf,
		"/definitely/not/a/real/path.yaml",
		true, // explicit
		reloadFlagOverrides{},
	)
	if err != nil {
		t.Fatalf("reloadConfigOnSignal: unexpected error %v", err)
	}
	if got != nil {
		t.Errorf("expected nil config (keep current), got %+v", got)
	}
	if !strings.Contains(buf.String(), "not found during SIGHUP reload") {
		t.Errorf("log = %q, want it to mention the missing-file branch", buf.String())
	}
}

// TestReloadConfigOnSignal_NoCandidateFile covers the case where
// the alerter started without --config, no default file was
// discovered, and SIGHUP arrives with still no file present.
// The helper returns (nil, nil) and logs the "no candidate"
// branch.
func TestReloadConfigOnSignal_NoCandidateFile(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("HOME", base)
	t.Setenv("AppData", base)
	fileutil.SetSystemConfigDirForTest(t, filepath.Join(base, "absent-etc-pgedge"))

	var buf bytes.Buffer
	got, err := reloadConfigOnSignal(
		&buf,
		"", // no previously-resolved path either
		false,
		reloadFlagOverrides{},
	)
	if err != nil {
		t.Fatalf("reloadConfigOnSignal: unexpected error %v", err)
	}
	if got != nil {
		t.Errorf("expected nil config (keep current), got %+v", got)
	}
	if !strings.Contains(buf.String(), "No configuration file found in default search") {
		t.Errorf("log = %q, want it to mention the no-candidate branch", buf.String())
	}
}

// TestReloadConfigOnSignal_HappyPath verifies the helper returns
// a fully populated config when a valid file is on disk.
func TestReloadConfigOnSignal_HappyPath(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "alerter.yaml")
	if err := os.WriteFile(cfgPath, []byte(minimalValidYAML), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var buf bytes.Buffer
	got, err := reloadConfigOnSignal(
		&buf,
		cfgPath,
		true,
		reloadFlagOverrides{},
	)
	if err != nil {
		t.Fatalf("reloadConfigOnSignal: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil config, got nil")
	}
	if got.Datastore.Host != "testhost" {
		t.Errorf("Host = %q, want testhost", got.Datastore.Host)
	}
	if !strings.Contains(buf.String(), "Configuration reloaded from") {
		t.Errorf("log = %q, want it to confirm the reload", buf.String())
	}
}

// TestReloadConfigOnSignal_FlagOverridesApplied verifies that the
// CLI flag overrides bundled in reloadFlagOverrides are applied
// to the freshly loaded config so SIGHUP does not silently drop
// command-line settings.
func TestReloadConfigOnSignal_FlagOverridesApplied(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "alerter.yaml")
	if err := os.WriteFile(cfgPath, []byte(minimalValidYAML), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var buf bytes.Buffer
	got, err := reloadConfigOnSignal(
		&buf,
		cfgPath,
		true,
		reloadFlagOverrides{
			DBHost:    "override-host",
			DBPort:    9999,
			DBSSLMode: "require",
		},
	)
	if err != nil {
		t.Fatalf("reloadConfigOnSignal: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil config, got nil")
	}
	if got.Datastore.Host != "override-host" {
		t.Errorf("Host = %q, want override-host", got.Datastore.Host)
	}
	if got.Datastore.Port != 9999 {
		t.Errorf("Port = %d, want 9999", got.Datastore.Port)
	}
	if got.Datastore.SSLMode != "require" {
		t.Errorf("SSLMode = %q, want require", got.Datastore.SSLMode)
	}
}

// TestReloadConfigOnSignal_InvalidConfig covers the validation
// failure branch. NewConfig() seeds datastore.host with
// "localhost" by default, so we have to clear it explicitly to
// trigger Validate's first check; an out-of-range port also
// works and is documented here for the next reader.
func TestReloadConfigOnSignal_InvalidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "alerter.yaml")
	// Empty host fails Validate's "datastore.host is required" rule.
	body := "datastore:\n  host: \"\"\n  port: 5432\n  database: d\n  username: u\n" +
		"pool:\n  max_connections: 5\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var buf bytes.Buffer
	got, err := reloadConfigOnSignal(
		&buf,
		cfgPath,
		true,
		reloadFlagOverrides{},
	)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if got != nil {
		t.Errorf("expected nil config on validation error, got %+v", got)
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Errorf("err = %v, want it to mention 'invalid'", err)
	}
}

// TestReloadConfigOnSignal_MalformedYAML covers the LoadFromFile
// failure branch.
func TestReloadConfigOnSignal_MalformedYAML(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "alerter.yaml")
	if err := os.WriteFile(cfgPath, []byte("datastore: [broken"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var buf bytes.Buffer
	got, err := reloadConfigOnSignal(
		&buf,
		cfgPath,
		true,
		reloadFlagOverrides{},
	)
	if err == nil {
		t.Fatal("expected YAML parse error, got nil")
	}
	if got != nil {
		t.Errorf("expected nil config on parse error, got %+v", got)
	}
}

// TestReloadConfigOnSignal_APIKeyWarning covers the non-fatal
// LoadAPIKeys branch: a missing API-key file logs a warning but
// must not block the reload, so the helper still returns a valid
// config.
func TestReloadConfigOnSignal_APIKeyWarning(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "alerter.yaml")
	body := minimalValidYAML +
		"llm:\n  openai:\n    api_key_file: /definitely/not/here\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var buf bytes.Buffer
	got, err := reloadConfigOnSignal(
		&buf,
		cfgPath,
		true,
		reloadFlagOverrides{},
	)
	if err != nil {
		t.Fatalf("reloadConfigOnSignal: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil config even when api-key file missing")
	}
	if !strings.Contains(buf.String(), "WARNING") {
		t.Errorf("log = %q, want it to contain WARNING", buf.String())
	}
}

// TestReloadConfigOnSignal_PasswordFileMissing covers the
// LoadPassword failure branch.
func TestReloadConfigOnSignal_PasswordFileMissing(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "alerter.yaml")
	body := "datastore:\n" +
		"  host: testhost\n" +
		"  port: 5432\n" +
		"  database: testdb\n" +
		"  username: testuser\n" +
		"  password_file: /definitely/not/here\n" +
		"pool:\n" +
		"  max_connections: 5\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var buf bytes.Buffer
	got, err := reloadConfigOnSignal(
		&buf,
		cfgPath,
		true,
		reloadFlagOverrides{},
	)
	if err == nil {
		t.Fatal("expected password-load error, got nil")
	}
	if got != nil {
		t.Errorf("expected nil config on password-load error, got %+v", got)
	}
	if !strings.Contains(err.Error(), "password") {
		t.Errorf("err = %v, want it to mention 'password'", err)
	}
}
