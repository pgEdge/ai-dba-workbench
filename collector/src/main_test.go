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
	cfgYAML := "datastore:\n  host: h\n  database: d\n  username: u\n"
	if err := os.WriteFile(configPath, []byte(cfgYAML), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Do not create any secret file anywhere discoverable.
	*configFile = configPath
	_, err := loadConfiguration()
	if err == nil {
		t.Fatal("expected error because server secret file is missing")
	}
}

// TestLoadConfiguration_Success exercises the full happy path of
// loadConfiguration(): it writes a config file, a password file, and a
// secret file next to the test binary so LoadSecret's default search
// finds it.
func TestLoadConfiguration_Success(t *testing.T) {
	defer saveAndClearFlags(t)()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "cfg.yaml")
	pwPath := filepath.Join(tmpDir, "pw.txt")

	// Secret file must live next to the running test binary so that
	// LoadSecret's default search can find it via os.Executable().
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	secretPath := filepath.Join(filepath.Dir(exe), "ai-dba-collector.secret")
	if err := os.WriteFile(secretPath, []byte("main-test-secret"), 0600); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(secretPath) })

	cfgYAML := "datastore:\n" +
		"  host: testhost\n" +
		"  database: testdb\n" +
		"  username: testuser\n" +
		"  password_file: " + pwPath + "\n"
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
// "no explicit config, no default config present" branch: the function
// should log and continue with defaults (then fail only at secret load
// because no secret file is present).
func TestLoadConfiguration_NoConfigFileUsesDefaults(t *testing.T) {
	defer saveAndClearFlags(t)()

	// *configFile is empty; default search path is resolved via
	// os.Executable(). The binary directory typically has no
	// ai-dba-collector.yaml so the function should fall through to the
	// secret-loading step.
	// Make sure the secret file does not exist either.
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	secretPath := filepath.Join(filepath.Dir(exe), "ai-dba-collector.secret")
	_ = os.Remove(secretPath)

	// Pre-flight: also ensure the default config path doesn't exist. If
	// it somehow does (CI cache, etc.) the test still passes so long as
	// the function returns *some* result; we just skip stronger
	// assertions in that case.
	defaultYAML := filepath.Join(filepath.Dir(exe), "ai-dba-collector.yaml")
	_, cfgStatErr := os.Stat(defaultYAML)
	if cfgStatErr == nil {
		t.Skipf("default config file unexpectedly exists at %s; skipping", defaultYAML)
	}

	_, err = loadConfiguration()
	if err == nil {
		t.Fatal("expected error because no secret file exists on default paths")
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
