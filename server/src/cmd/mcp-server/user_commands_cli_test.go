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
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// fakePasswords makes readPassword return answers in turn, then an error
// once they run out, restoring the real reader when the test ends. An
// answer of nil stands for a failed read.
func fakePasswords(t *testing.T, answers ...[]byte) {
	t.Helper()

	old := readPassword
	t.Cleanup(func() { readPassword = old })
	readPassword = func() ([]byte, error) {
		if len(answers) == 0 || answers[0] == nil {
			return nil, errors.New("no terminal")
		}
		answer := answers[0]
		answers = answers[1:]
		return answer, nil
	}
}

// newUserCommandStore returns a data directory holding an ordinary
// account "alice" with the given annotation.
func newUserCommandStore(t *testing.T) string {
	t.Helper()

	dataDir, store := newCLITestStore(t)
	if err := store.CreateUser("alice", cliTestPassword, "old notes",
		"Alice Old", "alice.old@example.com"); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("failed to close store: %v", err)
	}
	return dataDir
}

// storedUser reads the named user from the store at dataDir, or nil.
func storedUser(t *testing.T, dataDir, username string) *auth.StoredUser {
	t.Helper()

	store, err := openAuthStoreCLI(dataDir)
	if err != nil {
		t.Fatalf("failed to reopen auth store: %v", err)
	}
	defer store.Close()
	user, err := store.GetUser(username)
	if err != nil {
		return nil
	}
	return user
}

// runCLI runs fn with stdin reading input, returning its output and error.
func runCLI(t *testing.T, input string, fn func() error) (string, error) {
	t.Helper()

	var err error
	out := captureStdout(t, func() {
		withStdin(t, input, func() { err = fn() })
	})
	return out, err
}

// assertErrContains fails unless err is non-nil and contains want.
func assertErrContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("expected an error containing %q, got %v", want, err)
	}
}

// TestAddUserCommand covers the add-user command with and without flags.
func TestAddUserCommand(t *testing.T) {
	t.Run("everything from flags", func(t *testing.T) {
		dataDir, store := newCLITestStore(t)
		store.Close()

		out, err := runCLI(t, "", func() error {
			return addUserCommand(dataDir, "bob", cliTestPassword, "notes",
				"Bob Example", "bob@example.com")
		})
		if err != nil {
			t.Fatalf("addUserCommand failed: %v", err)
		}
		for _, want := range []string{"User created successfully!",
			"Username:  bob", "Full Name: Bob Example",
			"Email:    bob@example.com", "Notes:    notes",
			"Status:   Enabled"} {
			if !strings.Contains(out, want) {
				t.Errorf("output lacks %q: %q", want, out)
			}
		}
		user := storedUser(t, dataDir, "bob")
		if user == nil || user.Email != "bob@example.com" {
			t.Errorf("stored user = %+v", user)
		}
	})

	t.Run("everything from prompts", func(t *testing.T) {
		dataDir, store := newCLITestStore(t)
		store.Close()
		fakePasswords(t, []byte(cliTestPassword), []byte(cliTestPassword))

		out, err := runCLI(t, "carol\nCarol Example\ncarol@example.com\nsome notes\n",
			func() error { return addUserCommand(dataDir, "", "", "", "", "") })
		if err != nil {
			t.Fatalf("addUserCommand failed: %v", err)
		}
		for _, prompt := range []string{"Enter username: ",
			"Enter full name (optional): ", "Enter email address (optional): ",
			"Enter notes for this user (optional): "} {
			if !strings.Contains(out, prompt) {
				t.Errorf("output lacks prompt %q: %q", prompt, out)
			}
		}
		user := storedUser(t, dataDir, "carol")
		if user == nil || user.DisplayName != "Carol Example" ||
			user.Email != "carol@example.com" || user.Annotation != "some notes" {
			t.Errorf("stored user = %+v", user)
		}
		reopened, err := openAuthStoreCLI(dataDir)
		if err != nil {
			t.Fatalf("failed to reopen auth store: %v", err)
		}
		defer reopened.Close()
		if _, _, err := reopened.AuthenticateUser("carol", cliTestPassword); err != nil {
			t.Errorf("the prompted password does not work: %v", err)
		}
	})

	t.Run("optional prompts left empty", func(t *testing.T) {
		dataDir, store := newCLITestStore(t)
		store.Close()

		out, err := runCLI(t, "\n\n\n", func() error {
			return addUserCommand(dataDir, "bob", cliTestPassword, "", "", "")
		})
		if err != nil {
			t.Fatalf("addUserCommand failed: %v", err)
		}
		for _, absent := range []string{"Full Name:", "Email:", "Notes:"} {
			if strings.Contains(out, absent) {
				t.Errorf("output should not contain %q: %q", absent, out)
			}
		}
		if storedUser(t, dataDir, "bob") == nil {
			t.Fatal("user was not created")
		}
	})

	failures := []struct {
		name      string
		username  string
		passwords [][]byte
		wantErr   string
	}{
		{"no username", "", nil, "username is required"},
		{"password unreadable", "carol", [][]byte{nil}, "failed to read password"},
		{"empty password", "carol", [][]byte{{}}, "password is required"},
		{"confirmation unreadable", "carol", [][]byte{[]byte(cliTestPassword), nil},
			"failed to read password confirmation"},
		{"passwords differ", "carol",
			[][]byte{[]byte(cliTestPassword), []byte("Something-Else-42")},
			"passwords do not match"},
		{"password too short", "carol", [][]byte{[]byte("short"), []byte("short")},
			"failed to add user"},
	}
	for _, tt := range failures {
		t.Run(tt.name, func(t *testing.T) {
			dataDir, store := newCLITestStore(t)
			store.Close()
			fakePasswords(t, tt.passwords...)

			_, err := runCLI(t, "\n\n\n\n", func() error {
				return addUserCommand(dataDir, tt.username, "", "", "", "")
			})
			assertErrContains(t, err, tt.wantErr)
		})
	}

	t.Run("store open fails", func(t *testing.T) {
		err := addUserCommand(blockingDataDir(t), "bob", cliTestPassword, "", "", "")
		assertErrContains(t, err, "failed to open auth store")
	})
}

// TestUpdateUserCommandFromFlags covers the update-user command when the
// changes are given as flags.
func TestUpdateUserCommandFromFlags(t *testing.T) {
	dataDir := newUserCommandStore(t)

	out, err := runCLI(t, "", func() error {
		return updateUserCommand(dataDir, "alice", "", "new notes",
			"Alice New", "alice.new@example.com")
	})
	if err != nil {
		t.Fatalf("updateUserCommand failed: %v", err)
	}
	if !strings.Contains(out, "updated successfully") {
		t.Errorf("output = %q", out)
	}
	user := storedUser(t, dataDir, "alice")
	if user.Annotation != "new notes" || user.DisplayName != "Alice New" ||
		user.Email != "alice.new@example.com" {
		t.Errorf("stored user = %+v", user)
	}
}

// TestUpdateUserCommandInteractive covers the update-user command's
// prompts, including a username read from the same stdin as the answers
// that follow it.
func TestUpdateUserCommandInteractive(t *testing.T) {
	dataDir := newUserCommandStore(t)
	fakePasswords(t, []byte(cliTestPassword+"x"), []byte(cliTestPassword+"x"))

	input := "alice\n" + // username
		"y\n" + // update password
		"y\nAlice New\n" + // full name
		"yes\nalice.new@example.com\n" + // email
		"y\nnew notes\n" // notes
	out, err := runCLI(t, input, func() error {
		return updateUserCommand(dataDir, "", "", "", "", "")
	})
	if err != nil {
		t.Fatalf("updateUserCommand failed: %v", err)
	}
	if !strings.Contains(out, "What would you like to update?") {
		t.Errorf("expected the interactive menu, got %q", out)
	}
	user := storedUser(t, dataDir, "alice")
	if user.Annotation != "new notes" || user.DisplayName != "Alice New" ||
		user.Email != "alice.new@example.com" {
		t.Errorf("stored user = %+v", user)
	}
}

// TestUpdateUserCommandFailures covers the ways the update-user command
// refuses or fails.
func TestUpdateUserCommandFailures(t *testing.T) {
	tests := []struct {
		name      string
		username  string
		password  string
		stdin     string
		passwords [][]byte
		wantErr   string
	}{
		{name: "no username", stdin: "\n", wantErr: "username is required"},
		{name: "unknown user", username: "nobody", wantErr: "user 'nobody' not found"},
		{name: "nothing chosen", username: "alice", stdin: "n\nn\nn\nn\n",
			wantErr: "no updates specified"},
		{name: "input ends before any answer", username: "alice",
			wantErr: "no updates specified"},
		{name: "empty new password", username: "alice", stdin: "y\nn\nn\nn\n",
			passwords: [][]byte{{}}, wantErr: "no updates specified"},
		{name: "password unreadable", username: "alice", stdin: "y\n",
			passwords: [][]byte{nil}, wantErr: "failed to read password"},
		{name: "confirmation unreadable", username: "alice", stdin: "y\n",
			passwords: [][]byte{[]byte(cliTestPassword), nil},
			wantErr:   "failed to read password confirmation"},
		{name: "passwords differ", username: "alice", stdin: "y\n",
			passwords: [][]byte{[]byte(cliTestPassword), []byte("Something-Else-42")},
			wantErr:   "passwords do not match"},
		{name: "password too short", username: "alice", password: "short",
			wantErr: "failed to update user"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataDir := newUserCommandStore(t)
			fakePasswords(t, tt.passwords...)

			_, err := runCLI(t, tt.stdin, func() error {
				return updateUserCommand(dataDir, tt.username, tt.password, "", "", "")
			})
			assertErrContains(t, err, tt.wantErr)
			if user := storedUser(t, dataDir, "alice"); user.Annotation != "old notes" {
				t.Errorf("the failed update changed the account: %+v", user)
			}
		})
	}

	t.Run("store open fails", func(t *testing.T) {
		err := updateUserCommand(blockingDataDir(t), "alice", "", "x", "", "")
		assertErrContains(t, err, "failed to open auth store")
	})
}

// TestEnableDisableUserCommands covers the enable-user and disable-user
// commands, which share their shape.
func TestEnableDisableUserCommands(t *testing.T) {
	commands := []struct {
		name        string
		run         func(dataDir, username string) error
		wantEnabled bool
		wantOutput  string
	}{
		{"disable", disableUserCommand, false,
			"User 'alice' disabled successfully"},
		{"enable", enableUserCommand, true,
			"User 'alice' enabled successfully (failed attempts reset)"},
	}
	for _, c := range commands {
		t.Run(c.name+" named", func(t *testing.T) {
			dataDir := newUserCommandStore(t)
			out, err := runCLI(t, "", func() error { return c.run(dataDir, "alice") })
			if err != nil {
				t.Fatalf("%s failed: %v", c.name, err)
			}
			if !strings.Contains(out, c.wantOutput) {
				t.Errorf("output = %q", out)
			}
			if user := storedUser(t, dataDir, "alice"); user.Enabled != c.wantEnabled {
				t.Errorf("enabled = %v, want %v", user.Enabled, c.wantEnabled)
			}
		})
		t.Run(c.name+" prompted", func(t *testing.T) {
			dataDir := newUserCommandStore(t)
			out, err := runCLI(t, "alice\n", func() error { return c.run(dataDir, "") })
			if err != nil {
				t.Fatalf("%s failed: %v", c.name, err)
			}
			if prompt := "Enter username to " + c.name + ": "; !strings.Contains(out, prompt) {
				t.Errorf("output lacks prompt %q: %q", prompt, out)
			}
			if user := storedUser(t, dataDir, "alice"); user.Enabled != c.wantEnabled {
				t.Errorf("enabled = %v, want %v", user.Enabled, c.wantEnabled)
			}
		})
		t.Run(c.name+" no username", func(t *testing.T) {
			dataDir := newUserCommandStore(t)
			_, err := runCLI(t, "\n", func() error { return c.run(dataDir, "") })
			assertErrContains(t, err, "username is required")
		})
		t.Run(c.name+" unknown user", func(t *testing.T) {
			dataDir := newUserCommandStore(t)
			_, err := runCLI(t, "", func() error { return c.run(dataDir, "nobody") })
			assertErrContains(t, err, "failed to "+c.name+" user")
		})
		t.Run(c.name+" store open fails", func(t *testing.T) {
			err := c.run(blockingDataDir(t), "alice")
			assertErrContains(t, err, "failed to open auth store")
		})
	}
}

// TestAddServiceAccountCommand covers the add-service-account command.
func TestAddServiceAccountCommand(t *testing.T) {
	t.Run("everything from flags", func(t *testing.T) {
		dataDir, store := newCLITestStore(t)
		store.Close()

		out, err := runCLI(t, "", func() error {
			return addServiceAccountCommand(dataDir, "svc", "notes",
				"Service Example", "svc@example.com")
		})
		if err != nil {
			t.Fatalf("addServiceAccountCommand failed: %v", err)
		}
		for _, want := range []string{"Service account created successfully!",
			"Username:  svc", "Type:     Service Account (no password login)",
			"Full Name: Service Example", "Email:    svc@example.com",
			"Notes:    notes", "Use -add-token -user svc"} {
			if !strings.Contains(out, want) {
				t.Errorf("output lacks %q: %q", want, out)
			}
		}
		if user := storedUser(t, dataDir, "svc"); user == nil || !user.IsServiceAccount {
			t.Errorf("stored user = %+v", user)
		}
	})

	t.Run("everything from prompts", func(t *testing.T) {
		dataDir, store := newCLITestStore(t)
		store.Close()

		out, err := runCLI(t, "svc\nService Example\nsvc@example.com\nsome notes\n",
			func() error { return addServiceAccountCommand(dataDir, "", "", "", "") })
		if err != nil {
			t.Fatalf("addServiceAccountCommand failed: %v", err)
		}
		if !strings.Contains(out, "Enter service account username: ") {
			t.Errorf("expected the username prompt, got %q", out)
		}
		user := storedUser(t, dataDir, "svc")
		if user == nil || user.DisplayName != "Service Example" ||
			user.Email != "svc@example.com" || user.Annotation != "some notes" {
			t.Errorf("stored user = %+v", user)
		}
	})

	t.Run("optional prompts left empty", func(t *testing.T) {
		dataDir, store := newCLITestStore(t)
		store.Close()

		out, err := runCLI(t, "", func() error {
			return addServiceAccountCommand(dataDir, "svc", "", "", "")
		})
		if err != nil {
			t.Fatalf("addServiceAccountCommand failed: %v", err)
		}
		for _, absent := range []string{"Full Name:", "Email:", "Notes:"} {
			if strings.Contains(out, absent) {
				t.Errorf("output should not contain %q: %q", absent, out)
			}
		}
	})

	t.Run("no username", func(t *testing.T) {
		dataDir, store := newCLITestStore(t)
		store.Close()
		_, err := runCLI(t, "\n", func() error {
			return addServiceAccountCommand(dataDir, "", "", "", "")
		})
		assertErrContains(t, err, "username is required")
	})

	t.Run("duplicate name", func(t *testing.T) {
		dataDir := newUserCommandStore(t)
		_, err := runCLI(t, "\n\n\n", func() error {
			return addServiceAccountCommand(dataDir, "alice", "", "", "")
		})
		assertErrContains(t, err, "failed to create service account")
	})

	t.Run("store open fails", func(t *testing.T) {
		err := addServiceAccountCommand(blockingDataDir(t), "svc", "", "", "")
		assertErrContains(t, err, "failed to open auth store")
	})
}
