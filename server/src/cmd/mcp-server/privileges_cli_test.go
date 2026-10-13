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
	"strings"
	"testing"
)

// newPrivilegeCLIStore returns a data directory holding a group called
// "ops" and a registered describe_datastore tool privilege, with the
// store closed so the CLI commands can open it.
func newPrivilegeCLIStore(t *testing.T) string {
	t.Helper()
	dataDir, store := newCLITestStore(t)
	if _, err := store.CreateGroup("ops", "Operations"); err != nil {
		t.Fatalf("failed to create group: %v", err)
	}
	if _, err := store.RegisterMCPPrivilege("describe_datastore", "tool", "Describe the datastore", false); err != nil {
		t.Fatalf("failed to register privilege: %v", err)
	}
	store.Close()
	return dataDir
}

// expectCLIError runs fn and fails unless it returns an error containing
// want.
func expectCLIError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("err = %v, want one containing %q", err, want)
	}
}

func TestMCPPrivilegeGrantRevokeCommands(t *testing.T) {
	dataDir := newPrivilegeCLIStore(t)

	var err error
	out := captureStdout(t, func() { err = grantMCPPrivilegeCommand(dataDir, "ops", "describe_datastore") })
	if err != nil || !strings.Contains(out, "Granted privilege 'describe_datastore' to group 'ops'") {
		t.Fatalf("grant: err=%v out=%q", err, out)
	}
	out = captureStdout(t, func() { err = showGroupPrivilegesCommand(dataDir, "ops") })
	if err != nil || !strings.Contains(out, "[tool] describe_datastore") {
		t.Fatalf("show after grant: err=%v out=%q", err, out)
	}
	out = captureStdout(t, func() { err = revokeMCPPrivilegeCommand(dataDir, "ops", "describe_datastore") })
	if err != nil || !strings.Contains(out, "Revoked privilege 'describe_datastore' from group 'ops'") {
		t.Fatalf("revoke: err=%v out=%q", err, out)
	}

	for name, cmd := range map[string]func(string, string, string) error{
		"grant":  grantMCPPrivilegeCommand,
		"revoke": revokeMCPPrivilegeCommand,
	} {
		t.Run(name, func(t *testing.T) {
			expectCLIError(t, cmd(dataDir, "", "describe_datastore"), "group name is required")
			expectCLIError(t, cmd(dataDir, "ops", ""), "privilege identifier is required")
			expectCLIError(t, cmd(blockingDataDir(t), "ops", "describe_datastore"), "failed to open auth store")
			expectCLIError(t, cmd(dataDir, "missing", "describe_datastore"), "group 'missing' not found")
			expectCLIError(t, cmd(dataDir, "ops", "no_such_tool"), "privilege 'no_such_tool' not found")
		})
	}
}

func TestConnectionPrivilegeGrantRevokeCommands(t *testing.T) {
	dataDir := newPrivilegeCLIStore(t)

	var err error
	out := captureStdout(t, func() { err = grantConnectionPrivilegeCommand(dataDir, "ops", 7, "") })
	if err != nil || !strings.Contains(out, "Granted read access to connection 7 for group 'ops'") {
		t.Fatalf("grant with default level: err=%v out=%q", err, out)
	}
	out = captureStdout(t, func() { err = grantConnectionPrivilegeCommand(dataDir, "ops", 8, "read_write") })
	if err != nil || !strings.Contains(out, "Granted read_write access to connection 8") {
		t.Fatalf("grant read_write: err=%v out=%q", err, out)
	}
	out = captureStdout(t, func() { err = revokeConnectionPrivilegeCommand(dataDir, "ops", 7) })
	if err != nil || !strings.Contains(out, "Revoked access to connection 7 from group 'ops'") {
		t.Fatalf("revoke: err=%v out=%q", err, out)
	}

	expectCLIError(t, grantConnectionPrivilegeCommand(dataDir, "", 7, "read"), "group name is required")
	expectCLIError(t, grantConnectionPrivilegeCommand(dataDir, "ops", 0, "read"), "valid connection ID is required")
	expectCLIError(t, grantConnectionPrivilegeCommand(dataDir, "ops", 7, "admin"), "access level must be")
	expectCLIError(t, grantConnectionPrivilegeCommand(blockingDataDir(t), "ops", 7, "read"), "failed to open auth store")
	expectCLIError(t, grantConnectionPrivilegeCommand(dataDir, "missing", 7, "read"), "group 'missing' not found")

	expectCLIError(t, revokeConnectionPrivilegeCommand(dataDir, "", 7), "group name is required")
	expectCLIError(t, revokeConnectionPrivilegeCommand(dataDir, "ops", -1), "valid connection ID is required")
	expectCLIError(t, revokeConnectionPrivilegeCommand(blockingDataDir(t), "ops", 7), "failed to open auth store")
	expectCLIError(t, revokeConnectionPrivilegeCommand(dataDir, "missing", 7), "group 'missing' not found")
}

func TestListAndRegisterPrivilegeCommands(t *testing.T) {
	dataDir, store := newCLITestStore(t)
	store.Close()

	var err error
	out := captureStdout(t, func() { err = listPrivilegesCommand(dataDir) })
	if err != nil || !strings.Contains(out, "No MCP privileges registered.") {
		t.Fatalf("empty list: err=%v out=%q", err, out)
	}

	out = captureStdout(t, func() {
		err = registerPrivilegeCommand(dataDir, "describe_datastore", "tool",
			"List and describe the datastore sources query_datastore can read", true)
	})
	if err != nil || !strings.Contains(out, "Privilege 'describe_datastore' registered") ||
		!strings.Contains(out, "public: true") {
		t.Fatalf("register: err=%v out=%q", err, out)
	}

	out = captureStdout(t, func() { err = listPrivilegesCommand(dataDir) })
	if err != nil || !strings.Contains(out, "describe_datastore") {
		t.Fatalf("list: err=%v out=%q", err, out)
	}
	// Long descriptions are cut to fit the table.
	if !strings.Contains(out, "List and describe the datastore ...") {
		t.Errorf("long description not truncated:\n%s", out)
	}

	expectCLIError(t, listPrivilegesCommand(blockingDataDir(t)), "failed to open auth store")
	expectCLIError(t, registerPrivilegeCommand(dataDir, "", "tool", "", false), "privilege identifier is required")
	expectCLIError(t, registerPrivilegeCommand(dataDir, "x", "", "", false), "item type is required")
	expectCLIError(t, registerPrivilegeCommand(blockingDataDir(t), "x", "tool", "", false), "failed to open auth store")
	expectCLIError(t, registerPrivilegeCommand(dataDir, "x", "not_a_type", "", false), "failed to register privilege")
}

func TestRegisterMCPPrivilegesIncludesDatastoreTools(t *testing.T) {
	_, store := newCLITestStore(t)
	defer store.Close()

	registerMCPPrivileges(store)

	for _, name := range []string{"describe_datastore", "query_datastore"} {
		priv, err := store.GetMCPPrivilege(name)
		if err != nil || priv == nil {
			t.Fatalf("%s not registered: %v", name, err)
		}
		if priv.ItemType != "tool" {
			t.Errorf("%s item type = %q, want tool", name, priv.ItemType)
		}
	}
}
