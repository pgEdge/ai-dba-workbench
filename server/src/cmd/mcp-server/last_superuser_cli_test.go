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
	"errors"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// TestCLIKeepsLastSuperuser checks that the CLI, like the API, refuses
// to demote or disable the last enabled superuser, and that enabling a
// disabled superuser, the recovery path, is not refused.
func TestCLIKeepsLastSuperuser(t *testing.T) {
	dataDir := t.TempDir()
	store, err := openAuthStoreCLI(dataDir)
	if err != nil {
		t.Fatalf("openAuthStoreCLI failed: %v", err)
	}
	if err := store.CreateUser("root", "Password1234", "", "", ""); err != nil {
		store.Close()
		t.Fatalf("CreateUser failed: %v", err)
	}
	store.Close()

	if err := setSuperuserCommand(dataDir, "root", true); err != nil {
		t.Fatalf("Promoting failed: %v", err)
	}
	if err := setSuperuserCommand(dataDir, "root", false); !errors.Is(err,
		auth.ErrLastSuperuser) {
		t.Errorf("Demoting: expected ErrLastSuperuser, got %v", err)
	}
	if err := disableUserCommand(dataDir, "root"); !errors.Is(err,
		auth.ErrLastSuperuser) {
		t.Errorf("Disabling: expected ErrLastSuperuser, got %v", err)
	}
	if err := enableUserCommand(dataDir, "root"); err != nil {
		t.Errorf("Enabling should pass: %v", err)
	}
}
