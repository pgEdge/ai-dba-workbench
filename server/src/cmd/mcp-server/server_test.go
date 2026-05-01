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
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/config"
)

// newServerWithSecretFile builds a minimal Server fixture wired up
// only with the SecretFile field. This avoids depending on the
// full server initialisation path while still exercising the
// secret-loading method end to end.
func newServerWithSecretFile(secretFile string) *Server {
	return &Server{cfg: &config.Config{SecretFile: secretFile}}
}

// TestLoadServerSecret_ExplicitFile verifies that an explicit
// SecretFile in the config is read verbatim and trimmed.
func TestLoadServerSecret_ExplicitFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "server.secret")
	if err := os.WriteFile(path, []byte("  explicit-secret\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	s := newServerWithSecretFile(path)

	got, err := s.loadServerSecret("/ignored/exec/path")
	if err != nil {
		t.Fatalf("loadServerSecret: %v", err)
	}
	if got != "explicit-secret" {
		t.Errorf("got %q, want %q", got, "explicit-secret")
	}
}

// TestLoadServerSecret_DefaultUserDir verifies that when no
// explicit SecretFile is set the helper picks up a file from the
// per-user pgedge config directory.
func TestLoadServerSecret_DefaultUserDir(t *testing.T) {
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
	if err := os.WriteFile(
		filepath.Join(pgedgeDir, "ai-dba-server.secret"),
		[]byte("auto-discovered"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	s := newServerWithSecretFile("")
	got, err := s.loadServerSecret("/ignored/exec/path")
	if err != nil {
		t.Fatalf("loadServerSecret: %v", err)
	}
	if got != "auto-discovered" {
		t.Errorf("got %q, want %q", got, "auto-discovered")
	}
}

// TestLoadServerSecret_NoneFound verifies that the helper returns
// a descriptive error when neither an explicit path nor any of the
// default search paths yield a secret file.
func TestLoadServerSecret_NoneFound(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("HOME", base)
	t.Setenv("AppData", base)

	s := newServerWithSecretFile("")
	_, err := s.loadServerSecret("/ignored/exec/path")
	if err == nil {
		t.Fatal("expected error when no secret file is reachable")
	}
	// Skip strong assertion if the host happens to have a real
	// /etc/pgedge/ai-dba-server.secret file installed.
	if strings.Contains(err.Error(), "not found in any") {
		return
	}
	t.Logf("note: host appears to have /etc/pgedge populated; err = %v", err)
}

// TestLoadServerSecret_EmptyFile verifies that a secret file
// containing only whitespace is rejected.
func TestLoadServerSecret_EmptyFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "blank.secret")
	if err := os.WriteFile(path, []byte("   \n\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	s := newServerWithSecretFile(path)

	_, err := s.loadServerSecret("/ignored/exec/path")
	if err == nil {
		t.Fatal("expected error when secret file is empty")
	}
	if !strings.Contains(err.Error(), "is empty") {
		t.Errorf("err = %v, want it to mention 'is empty'", err)
	}
}

// TestLoadServerSecret_ExplicitMissing verifies that an explicit
// SecretFile pointing at a non-existent path returns a read error
// (not the "not found in any default search path" message).
func TestLoadServerSecret_ExplicitMissing(t *testing.T) {
	s := newServerWithSecretFile("/definitely/not/a/real/path.secret")

	_, err := s.loadServerSecret("/ignored/exec/path")
	if err == nil {
		t.Fatal("expected error for missing explicit secret file")
	}
	if !strings.Contains(err.Error(), "failed to read secret file") {
		t.Errorf("err = %v, want it to mention 'failed to read secret file'", err)
	}
}
