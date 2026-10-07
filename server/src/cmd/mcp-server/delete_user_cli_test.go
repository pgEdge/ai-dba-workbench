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
)

// withStdin runs fn with os.Stdin reading input, restoring it afterwards.
func withStdin(t *testing.T, input string, fn func()) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(path, []byte(input), 0600); err != nil {
		t.Fatalf("failed to write stdin file: %v", err)
	}
	f, err := os.Open(path) // #nosec G304 -- path is under t.TempDir()
	if err != nil {
		t.Fatalf("failed to open stdin file: %v", err)
	}
	defer f.Close()

	oldStdin := os.Stdin
	os.Stdin = f
	defer func() { os.Stdin = oldStdin }()

	fn()
}

// newDeleteUserStore returns a data directory holding two superusers,
// "root" and "spare", so that either may be deleted without leaving none.
func newDeleteUserStore(t *testing.T) string {
	t.Helper()

	dataDir, store := newCLITestStore(t)
	for _, name := range []string{"root", "spare"} {
		if err := store.CreateUser(name, "Password1234", "", "", ""); err != nil {
			t.Fatalf("failed to create user: %v", err)
		}
		if err := store.SetUserSuperuser(name, true); err != nil {
			t.Fatalf("failed to set superuser: %v", err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("failed to close store: %v", err)
	}
	return dataDir
}

// userExists reports whether the named user is in the store at dataDir.
func userExists(t *testing.T, dataDir, username string) bool {
	t.Helper()

	store, err := openAuthStoreCLI(dataDir)
	if err != nil {
		t.Fatalf("failed to reopen auth store: %v", err)
	}
	defer store.Close()
	user, err := store.GetUser(username)
	return err == nil && user != nil
}

// TestDeleteUserCommand covers the delete-user command, including that
// the CLI may delete a superuser account: it acts with the server's own
// authority over the auth database (issue #588).
func TestDeleteUserCommand(t *testing.T) {
	tests := []struct {
		name        string
		username    string
		stdin       string
		wantErr     string
		wantOutput  string
		wantDeleted bool
	}{
		{name: "confirmed delete of a superuser", username: "root",
			stdin: "y\n", wantOutput: "deleted successfully", wantDeleted: true},
		{name: "username read from the prompt", stdin: "root\ny\n",
			wantOutput: "deleted successfully", wantDeleted: true},
		{name: "declined", username: "root", stdin: "n\n",
			wantOutput: "Deletion canceled"},
		{name: "no username given", stdin: "\n",
			wantErr: "username is required"},
		{name: "unknown user", username: "nobody", stdin: "yes\n",
			wantErr: "failed to delete user"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataDir := newDeleteUserStore(t)

			var err error
			out := captureStdout(t, func() {
				withStdin(t, tt.stdin, func() {
					err = deleteUserCommand(dataDir, tt.username)
				})
			})

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected an error containing %q, got %v",
						tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("deleteUserCommand failed: %v", err)
			}
			if !strings.Contains(out, tt.wantOutput) {
				t.Errorf("expected output to contain %q, got %q",
					tt.wantOutput, out)
			}
			if deleted := !userExists(t, dataDir, "root"); deleted != tt.wantDeleted {
				t.Errorf("root deleted = %v, want %v", deleted, tt.wantDeleted)
			}
		})
	}
}

// TestDeleteUserCommandStoreOpenFails checks that a data directory the
// store cannot be opened in is reported.
func TestDeleteUserCommandStoreOpenFails(t *testing.T) {
	err := deleteUserCommand(blockingDataDir(t), "root")
	if err == nil || !strings.Contains(err.Error(), "failed to open auth store") {
		t.Errorf("expected an open failure, got %v", err)
	}
}
