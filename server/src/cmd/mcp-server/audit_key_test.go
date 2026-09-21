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

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
)

// useRealCLIAuditKey restores the production resolver for the duration
// of one test, undoing the fixed key TestMain installs for the rest of
// the binary, and points the search at the given configured path.
func useRealCLIAuditKey(t *testing.T, configuredPath string) {
	t.Helper()

	previousFn := cliAuditKey
	previousSource := cliSecretSource
	cliAuditKey = resolveCLIAuditKey
	cliSecretSource.configuredPath = configuredPath

	t.Cleanup(func() {
		cliAuditKey = previousFn
		cliSecretSource = previousSource
	})
}

// TestCLIRefusesToOpenTheStoreWithoutASecret checks that a command-line
// invocation with no readable server secret fails, and says why, rather
// than opening a store that would write rows under no key at all.
func TestCLIRefusesToOpenTheStoreWithoutASecret(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.secret")
	useRealCLIAuditKey(t, missing)

	store, err := openAuthStoreCLI(t.TempDir())
	if err == nil {
		store.Close()
		t.Fatal("Expected the command line to refuse to open the store")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("Expected the error to name the secret file, got %v", err)
	}
	if !strings.Contains(err.Error(), "audit log") {
		t.Errorf("Expected the error to explain why the secret is needed, got %v", err)
	}
}

// TestCLIReportsAnEmptyDefaultSearch checks the message an operator sees
// when no secret file exists in any of the default locations, since that
// message is the only thing telling them where to put one.
func TestCLIReportsAnEmptyDefaultSearch(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if config.GetDefaultSecretPath() != "" {
		t.Skip("This host has a server secret in a default location")
	}
	useRealCLIAuditKey(t, "")

	_, err := cliAuditKey()
	if err == nil {
		t.Fatal("Expected the resolution to fail with no secret anywhere")
	}
	for _, want := range []string{"default search path", "/etc/pgedge"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Expected the error to mention %q, got %v", want, err)
		}
	}
}

// TestCLIDerivesTheSameKeyAsTheServer checks that a command opening the
// store arrives at the key the server would from the same secret. If the
// two ever diverged, every row a command wrote would be unverifiable by
// the server, which is the failure this plumbing exists to prevent.
func TestCLIDerivesTheSameKeyAsTheServer(t *testing.T) {
	secretPath := filepath.Join(t.TempDir(), "ai-dba-server.secret")
	if err := os.WriteFile(secretPath, []byte("a shared server secret\n"), 0600); err != nil {
		t.Fatalf("Failed to write the secret file: %v", err)
	}
	useRealCLIAuditKey(t, secretPath)

	key, err := cliAuditKey()
	if err != nil {
		t.Fatalf("Expected the key to resolve, got %v", err)
	}
	if !bytes.Equal(key, auth.DeriveAuditKey("a shared server secret")) {
		t.Error("The command line derived a different key from the same secret")
	}

	// The store must open with it, and the row a command writes must
	// verify afterwards.
	dataDir := t.TempDir()
	store, err := openAuthStoreCLI(dataDir)
	if err != nil {
		t.Fatalf("Failed to open the store: %v", err)
	}
	defer store.Close()

	if _, err := store.CreateGroup("testers", "for the test"); err != nil {
		t.Fatalf("Failed to create a group: %v", err)
	}
	if _, _, err := store.VerifyAuditChain(); err != nil {
		t.Errorf("Expected the chain written by the command line to verify, got %v", err)
	}
}

// TestCLIRejectsAnEmptySecretFile checks that a secret file that exists
// but holds nothing is refused: a key derived from an empty secret is no
// key at all.
func TestCLIRejectsAnEmptySecretFile(t *testing.T) {
	secretPath := filepath.Join(t.TempDir(), "empty.secret")
	if err := os.WriteFile(secretPath, []byte("\n"), 0600); err != nil {
		t.Fatalf("Failed to write the secret file: %v", err)
	}
	useRealCLIAuditKey(t, secretPath)

	if _, err := cliAuditKey(); err == nil {
		t.Fatal("Expected an empty secret to be refused")
	}
}

// TestRunCLICommandsRecordsTheSecretSource checks that the values main
// resolved reach the resolver, since a command that ignored secret_file
// would look for the secret somewhere the server does not.
func TestRunCLICommandsRecordsTheSecretSource(t *testing.T) {
	previous := cliSecretSource
	t.Cleanup(func() { cliSecretSource = previous })

	if RunCLICommands(&Flags{}, t.TempDir(), "/etc/pgedge/custom.secret") {
		t.Fatal("Expected no command to be dispatched for empty flags")
	}
	if cliSecretSource.configuredPath != "/etc/pgedge/custom.secret" {
		t.Errorf("configuredPath = %q, want the configured secret file",
			cliSecretSource.configuredPath)
	}
}

// TestInitAuthStoreRefusesAnEmptySecret checks that the server does not
// open its auth store without a secret to key the audit chain with. The
// store would refuse the first mutation anyway; failing here says why.
func TestInitAuthStoreRefusesAnEmptySecret(t *testing.T) {
	s := &Server{dataDir: t.TempDir(), cfg: &config.Config{}}

	err := s.initAuthStore("")
	if err == nil {
		t.Fatal("Expected an empty server secret to be refused")
	}
	if !strings.Contains(err.Error(), "audit chain key") {
		t.Errorf("Expected the error to name the audit chain key, got %v", err)
	}
	if s.authStore != nil {
		t.Error("Expected no auth store to have been opened")
	}
}

// TestInitAuthStoreOpensWithASecret is the matching success case, and
// checks the store it opens writes rows the same secret verifies.
func TestInitAuthStoreOpensWithASecret(t *testing.T) {
	s := &Server{dataDir: t.TempDir(), cfg: &config.Config{}}

	if err := s.initAuthStore("a server secret"); err != nil {
		t.Fatalf("initAuthStore: %v", err)
	}
	defer s.authStore.Close()

	if _, err := s.authStore.CreateGroup("testers", "for the test"); err != nil {
		t.Fatalf("Failed to create a group: %v", err)
	}
	if _, _, err := s.authStore.VerifyAuditChain(); err != nil {
		t.Errorf("Expected the chain to verify, got %v", err)
	}
}
