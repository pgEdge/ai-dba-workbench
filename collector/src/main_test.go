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
	"syscall"
	"testing"
	"time"
)

// saveAndClearFlags resets the package-level flag pointers and returns a
// restore function. Tests that mutate global flags use this to isolate
// themselves from each other.
func saveAndClearFlags(t *testing.T) func() {
	t.Helper()
	origConfig := *configFile
	origHost, origHostAddr := *pgHost, *pgHostAddr
	origDB, origUser := *pgDatabase, *pgUsername
	origPwdFile, origPort := *pgPasswordFile, *pgPort
	origSSLMode, origSSLCert := *pgSSLMode, *pgSSLCert
	origSSLKey, origSSLRootCert := *pgSSLKey, *pgSSLRootCert

	*configFile = ""
	*pgHost = ""
	*pgHostAddr = ""
	*pgDatabase = ""
	*pgUsername = ""
	*pgPasswordFile = ""
	*pgPort = 5432
	*pgSSLMode = "prefer"
	*pgSSLCert = ""
	*pgSSLKey = ""
	*pgSSLRootCert = ""

	return func() {
		*configFile = origConfig
		*pgHost = origHost
		*pgHostAddr = origHostAddr
		*pgDatabase = origDB
		*pgUsername = origUser
		*pgPasswordFile = origPwdFile
		*pgPort = origPort
		*pgSSLMode = origSSLMode
		*pgSSLCert = origSSLCert
		*pgSSLKey = origSSLKey
		*pgSSLRootCert = origSSLRootCert
	}
}

func TestLoadConfiguration_ExplicitConfigMissing(t *testing.T) {
	defer saveAndClearFlags(t)()

	*configFile = "/nonexistent/path/to/config.yaml"

	cfg, err := loadConfiguration()
	if err == nil {
		t.Fatal("expected error when explicit config file does not exist")
	}
	if cfg != nil {
		t.Errorf("expected nil config on error, got %+v", cfg)
	}
}

func TestLoadConfiguration_ExplicitConfigMalformed(t *testing.T) {
	defer saveAndClearFlags(t)()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "bad.yaml")
	if err := os.WriteFile(configPath, []byte("datastore:\n  host: [broken"), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	*configFile = configPath
	_, err := loadConfiguration()
	if err == nil {
		t.Fatal("expected error for malformed YAML")
	}
}

func TestLoadConfiguration_PasswordFileMissing(t *testing.T) {
	defer saveAndClearFlags(t)()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "cfg.yaml")
	if err := os.WriteFile(configPath, []byte("datastore:\n  host: h\n  database: d\n  username: u\n"), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	*configFile = configPath
	*pgPasswordFile = "/definitely/missing/password-file"

	_, err := loadConfiguration()
	if err == nil {
		t.Fatal("expected error because password file is missing")
	}
}

func TestLoadConfiguration_SecretFileMissing(t *testing.T) {
	defer saveAndClearFlags(t)()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "cfg.yaml")
	// Point secret_file to a known-missing path within our temp directory
	// to avoid relying on ambient secret discovery.
	missingSecret := filepath.Join(tmpDir, "missing.secret")
	cfgYAML := "datastore:\n" +
		"  host: h\n" +
		"  database: d\n" +
		"  username: u\n" +
		"secret_file: \"" + missingSecret + "\"\n"
	if err := os.WriteFile(configPath, []byte(cfgYAML), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	*configFile = configPath
	_, err := loadConfiguration()
	if err == nil {
		t.Fatal("expected error because server secret file is missing")
	}
}

// TestLoadConfiguration_Success exercises the full happy path of
// loadConfiguration(): it writes a config file, a password file, and a
// secret file all within the temp directory and explicitly references
// them in the config YAML.
func TestLoadConfiguration_Success(t *testing.T) {
	defer saveAndClearFlags(t)()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "cfg.yaml")
	pwPath := filepath.Join(tmpDir, "pw.txt")
	secretPath := filepath.Join(tmpDir, "test.secret")

	if err := os.WriteFile(secretPath, []byte("main-test-secret"), 0600); err != nil {
		t.Fatalf("write secret: %v", err)
	}

	cfgYAML := "datastore:\n" +
		"  host: testhost\n" +
		"  database: testdb\n" +
		"  username: testuser\n" +
		"  password_file: " + pwPath + "\n" +
		"secret_file: \"" + secretPath + "\"\n"
	if err := os.WriteFile(configPath, []byte(cfgYAML), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(pwPath, []byte("test-pw\n"), 0600); err != nil {
		t.Fatalf("write pw: %v", err)
	}

	*configFile = configPath

	cfg, err := loadConfiguration()
	if err != nil {
		t.Fatalf("loadConfiguration error: %v", err)
	}
	if cfg.Datastore.Host != "testhost" {
		t.Errorf("Host: got %q, want testhost", cfg.Datastore.Host)
	}
	if cfg.Datastore.Password != "test-pw" {
		t.Errorf("Password: got %q, want test-pw", cfg.Datastore.Password)
	}
	if cfg.GetServerSecret() != "main-test-secret" {
		t.Errorf("Secret: got %q, want main-test-secret", cfg.GetServerSecret())
	}
}

// TestLoadConfiguration_NoConfigFileUsesDefaults exercises the
// "no explicit config, no default config present" branch: the
// function should log and continue with defaults (then fail only
// at secret load because no secret file is present).
//
// We redirect os.UserConfigDir() at a temp directory so the test
// is deterministic regardless of the developer's environment, and
// we tolerate the rare case where /etc/pgedge/ai-dba-collector.yaml
// exists on the host (the helper will pick it up and the
// "configuration loaded from" branch will run instead).
func TestLoadConfiguration_NoConfigFileUsesDefaults(t *testing.T) {
	defer saveAndClearFlags(t)()

	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("HOME", base)
	t.Setenv("AppData", base)

	// loadConfiguration must surface an error from LoadSecret since
	// there is no secret file available in either default location.
	_, err := loadConfiguration()
	if err == nil {
		t.Fatal("expected error because no secret file exists on default paths")
	}
}

// TestLoadConfiguration_DefaultConfigFromUserDir verifies that the
// auto-discovery path correctly loads a config file dropped into
// the per-user pgedge directory.
func TestLoadConfiguration_DefaultConfigFromUserDir(t *testing.T) {
	defer saveAndClearFlags(t)()

	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("HOME", base)
	t.Setenv("AppData", base)

	userDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("os.UserConfigDir: %v", err)
	}
	pgedgeDir := filepath.Join(userDir, "pgedge")
	if err := os.MkdirAll(pgedgeDir, 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Drop a real config (with secret_file) and a matching secret
	// so loadConfiguration runs end-to-end without any --config
	// flag.
	secretPath := filepath.Join(pgedgeDir, "ai-dba-collector.secret")
	if err := os.WriteFile(secretPath, []byte("auto-discovered-secret"), 0600); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	cfgPath := filepath.Join(pgedgeDir, "ai-dba-collector.yaml")
	cfgYAML := "datastore:\n" +
		"  host: discovered-host\n" +
		"  database: discovered-db\n" +
		"  username: discovered-user\n"
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0600); err != nil {
		t.Fatalf("write cfg: %v", err)
	}

	cfg, err := loadConfiguration()
	if err != nil {
		t.Fatalf("loadConfiguration: %v", err)
	}
	if cfg.Datastore.Host != "discovered-host" {
		t.Errorf("Host: got %q, want discovered-host", cfg.Datastore.Host)
	}
	if cfg.GetServerSecret() != "auto-discovered-secret" {
		t.Errorf("Secret: got %q, want auto-discovered-secret",
			cfg.GetServerSecret())
	}
}

func TestWaitForShutdown(t *testing.T) {
	done := make(chan struct{})

	go func() {
		waitForShutdown()
		close(done)
	}()

	// Give the goroutine time to install its signal handler before we
	// deliver the signal.
	time.Sleep(50 * time.Millisecond)

	proc, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("FindProcess: %v", err)
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("Signal: %v", err)
	}

	select {
	case <-done:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatal("waitForShutdown did not return after SIGTERM")
	}
}
