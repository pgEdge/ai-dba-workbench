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
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// captureStdout runs fn while capturing everything written to os.Stdout and
// returns the captured output. Stderr is left untouched so the auth-store path
// message emitted by openAuthStoreCLI does not pollute the captured stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w

	defer func() {
		os.Stdout = oldStdout
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("failed to close pipe writer: %v", err)
	}

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("failed to read captured stdout: %v", err)
	}

	return buf.String()
}

// newCLITestStore creates an auth store in a fresh temp directory, returning the
// data directory (for use by the CLI commands, which open the store by path) and
// the open store for direct population. The caller must close the store before
// invoking a CLI command so the two do not contend for the SQLite database.
func newCLITestStore(t *testing.T) (string, *auth.AuthStore) {
	t.Helper()

	dataDir := t.TempDir()
	store, err := auth.NewAuthStore(dataDir, 0, 0)
	if err != nil {
		t.Fatalf("failed to create auth store: %v", err)
	}
	return dataDir, store
}

// blockingDataDir returns a data-dir path that cannot be created because a
// regular file sits where a parent directory would need to be, forcing
// openAuthStoreCLI (via NewAuthStore) to fail.
func blockingDataDir(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	blockingFile := tmpDir + "/blocking"
	if err := os.WriteFile(blockingFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create blocking file: %v", err)
	}
	return blockingFile + "/subdir"
}

func TestListGroupMembersCommand(t *testing.T) {
	t.Run("empty group name returns error", func(t *testing.T) {
		if err := listGroupMembersCommand(t.TempDir(), ""); err == nil {
			t.Fatal("expected error for empty group name")
		}
	})

	t.Run("unopenable data dir returns error", func(t *testing.T) {
		if err := listGroupMembersCommand(blockingDataDir(t), "any"); err == nil {
			t.Fatal("expected error when auth store cannot be opened")
		}
	})

	t.Run("non-existent group returns error", func(t *testing.T) {
		dataDir, store := newCLITestStore(t)
		store.Close()

		err := listGroupMembersCommand(dataDir, "does-not-exist")
		if err == nil {
			t.Fatal("expected error for non-existent group")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("expected 'not found' error, got %v", err)
		}
	})

	t.Run("lists user and group members", func(t *testing.T) {
		dataDir, store := newCLITestStore(t)

		parentID, err := store.CreateGroup("parent", "Parent group")
		if err != nil {
			t.Fatalf("failed to create parent group: %v", err)
		}
		childID, err := store.CreateGroup("child", "Child group")
		if err != nil {
			t.Fatalf("failed to create child group: %v", err)
		}
		if err := store.CreateUser("alice", "Password1234", "", "", ""); err != nil {
			t.Fatalf("failed to create user: %v", err)
		}
		user, err := store.GetUser("alice")
		if err != nil {
			t.Fatalf("failed to get user: %v", err)
		}
		if err := store.AddUserToGroup(parentID, user.ID); err != nil {
			t.Fatalf("failed to add user to group: %v", err)
		}
		if err := store.AddGroupToGroup(parentID, childID); err != nil {
			t.Fatalf("failed to add child group: %v", err)
		}
		store.Close()

		var cmdErr error
		out := captureStdout(t, func() {
			cmdErr = listGroupMembersCommand(dataDir, "parent")
		})
		if cmdErr != nil {
			t.Fatalf("listGroupMembersCommand returned error: %v", cmdErr)
		}

		if !strings.Contains(out, "Members of group 'parent'") {
			t.Errorf("expected header in output, got:\n%s", out)
		}
		if !strings.Contains(out, "- alice") {
			t.Errorf("expected user member 'alice' in output, got:\n%s", out)
		}
		if !strings.Contains(out, "- child") {
			t.Errorf("expected group member 'child' in output, got:\n%s", out)
		}
	})

	t.Run("empty members show None", func(t *testing.T) {
		dataDir, store := newCLITestStore(t)
		if _, err := store.CreateGroup("empty", "Empty group"); err != nil {
			t.Fatalf("failed to create group: %v", err)
		}
		store.Close()

		var cmdErr error
		out := captureStdout(t, func() {
			cmdErr = listGroupMembersCommand(dataDir, "empty")
		})
		if cmdErr != nil {
			t.Fatalf("listGroupMembersCommand returned error: %v", cmdErr)
		}

		if !strings.Contains(out, "Users: None") {
			t.Errorf("expected 'Users: None' in output, got:\n%s", out)
		}
		if !strings.Contains(out, "Groups: None") {
			t.Errorf("expected 'Groups: None' in output, got:\n%s", out)
		}
	})
}

func TestShowGroupPrivilegesCommandAdminPermissions(t *testing.T) {
	t.Run("shows granted admin permissions", func(t *testing.T) {
		dataDir, store := newCLITestStore(t)

		groupID, err := store.CreateGroup("admins", "Admin group")
		if err != nil {
			t.Fatalf("failed to create group: %v", err)
		}
		if err := store.GrantAdminPermission(groupID, "manage_users"); err != nil {
			t.Fatalf("failed to grant admin permission: %v", err)
		}
		// Also grant an MCP privilege and a connection privilege so the
		// populated (non-None) branches of the command are exercised.
		if _, err := store.RegisterMCPPrivilege("query_database", "tool", "Run queries", false); err != nil {
			t.Fatalf("failed to register MCP privilege: %v", err)
		}
		if err := store.GrantMCPPrivilegeByName(groupID, "query_database"); err != nil {
			t.Fatalf("failed to grant MCP privilege: %v", err)
		}
		if err := store.GrantConnectionPrivilege(groupID, 7, "read_write"); err != nil {
			t.Fatalf("failed to grant connection privilege: %v", err)
		}
		store.Close()

		var cmdErr error
		out := captureStdout(t, func() {
			cmdErr = showGroupPrivilegesCommand(dataDir, "admins")
		})
		if cmdErr != nil {
			t.Fatalf("showGroupPrivilegesCommand returned error: %v", cmdErr)
		}

		if !strings.Contains(out, "Admin Permissions:") {
			t.Errorf("expected 'Admin Permissions:' header in output, got:\n%s", out)
		}
		if !strings.Contains(out, "- manage_users") {
			t.Errorf("expected 'manage_users' in output, got:\n%s", out)
		}
		if !strings.Contains(out, "query_database") {
			t.Errorf("expected MCP privilege 'query_database' in output, got:\n%s", out)
		}
		if !strings.Contains(out, "Connection 7") {
			t.Errorf("expected 'Connection 7' in output, got:\n%s", out)
		}
	})

	t.Run("shows None when no admin permissions", func(t *testing.T) {
		dataDir, store := newCLITestStore(t)
		if _, err := store.CreateGroup("plain", "Plain group"); err != nil {
			t.Fatalf("failed to create group: %v", err)
		}
		store.Close()

		var cmdErr error
		out := captureStdout(t, func() {
			cmdErr = showGroupPrivilegesCommand(dataDir, "plain")
		})
		if cmdErr != nil {
			t.Fatalf("showGroupPrivilegesCommand returned error: %v", cmdErr)
		}

		if !strings.Contains(out, "Admin Permissions: None") {
			t.Errorf("expected 'Admin Permissions: None' in output, got:\n%s", out)
		}
	})

	t.Run("empty group name returns error", func(t *testing.T) {
		if err := showGroupPrivilegesCommand(t.TempDir(), ""); err == nil {
			t.Fatal("expected error for empty group name")
		}
	})

	t.Run("unopenable data dir returns error", func(t *testing.T) {
		if err := showGroupPrivilegesCommand(blockingDataDir(t), "any"); err == nil {
			t.Fatal("expected error when auth store cannot be opened")
		}
	})

	t.Run("non-existent group returns error", func(t *testing.T) {
		dataDir, store := newCLITestStore(t)
		store.Close()

		err := showGroupPrivilegesCommand(dataDir, "missing")
		if err == nil {
			t.Fatal("expected error for non-existent group")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("expected 'not found' error, got %v", err)
		}
	})
}
