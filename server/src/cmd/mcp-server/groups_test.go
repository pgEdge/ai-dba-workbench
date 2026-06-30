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
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// TestAddGroupCommand verifies that addGroupCommand creates a group and that
// the supplied description is persisted and round-trips through the auth store.
func TestAddGroupCommand(t *testing.T) {
	t.Run("persists non-empty description", func(t *testing.T) {
		tmpDir := t.TempDir()

		const (
			groupName = "platform-team"
			groupDesc = "Owners of the platform connections"
		)

		if err := addGroupCommand(tmpDir, groupName, groupDesc); err != nil {
			t.Fatalf("addGroupCommand returned error: %v", err)
		}

		store, err := auth.NewAuthStore(tmpDir, 0, 0)
		if err != nil {
			t.Fatalf("failed to open auth store: %v", err)
		}
		defer store.Close()

		group, err := store.GetGroupByName(groupName)
		if err != nil {
			t.Fatalf("GetGroupByName returned error: %v", err)
		}
		if group == nil {
			t.Fatalf("expected group %q to exist, got nil", groupName)
		}
		if group.Name != groupName {
			t.Errorf("expected name %q, got %q", groupName, group.Name)
		}
		if group.Description != groupDesc {
			t.Errorf("expected description %q, got %q", groupDesc, group.Description)
		}
	})

	t.Run("persists empty description", func(t *testing.T) {
		tmpDir := t.TempDir()

		const groupName = "no-description-team"

		if err := addGroupCommand(tmpDir, groupName, ""); err != nil {
			t.Fatalf("addGroupCommand returned error: %v", err)
		}

		store, err := auth.NewAuthStore(tmpDir, 0, 0)
		if err != nil {
			t.Fatalf("failed to open auth store: %v", err)
		}
		defer store.Close()

		group, err := store.GetGroupByName(groupName)
		if err != nil {
			t.Fatalf("GetGroupByName returned error: %v", err)
		}
		if group == nil {
			t.Fatalf("expected group %q to exist, got nil", groupName)
		}
		if group.Description != "" {
			t.Errorf("expected empty description, got %q", group.Description)
		}
	})

	t.Run("requires group name", func(t *testing.T) {
		tmpDir := t.TempDir()

		err := addGroupCommand(tmpDir, "", "some description")
		if err == nil {
			t.Fatal("expected error for empty group name, got nil")
		}
	})

	t.Run("returns error when auth store cannot be opened", func(t *testing.T) {
		tmpDir := t.TempDir()
		blockingFile := tmpDir + "/blocking"
		if err := os.WriteFile(blockingFile, []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create blocking file: %v", err)
		}

		err := addGroupCommand(blockingFile+"/subdir", "any-group", "desc")
		if err == nil {
			t.Fatal("expected error when auth store path is invalid, got nil")
		}
	})
}
