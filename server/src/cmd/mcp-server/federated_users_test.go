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

const (
	cliTestIssuer   = "https://idp.example.com"
	cliTestSubject  = "subject-0001"
	cliTestPassword = "Sup3r-Str0ng-Pass!"
)

// seedLinkTestUser creates a local account and returns the data directory the
// CLI commands should be pointed at. The store is closed before returning, so
// the command can open the same SQLite database.
func seedLinkTestUser(t *testing.T, username string) string {
	t.Helper()

	dataDir, store := newCLITestStore(t)
	if err := store.CreateUser(username, cliTestPassword, "", "", ""); err != nil {
		store.Close()
		t.Fatalf("failed to create user: %v", err)
	}
	store.Close()
	return dataDir
}

// reopenStore reopens the auth store a command has finished with, so a test can
// assert on what the command wrote.
func reopenStore(t *testing.T, dataDir string) *auth.AuthStore {
	t.Helper()

	store, err := auth.NewAuthStore(dataDir, 0, 0, auth.AuditKeyForTesting())
	if err != nil {
		t.Fatalf("failed to reopen auth store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestLinkOIDCUserCommand(t *testing.T) {
	t.Run("missing arguments return errors", func(t *testing.T) {
		tests := []struct {
			name     string
			username string
			issuer   string
			subject  string
		}{
			{"no username", "", cliTestIssuer, cliTestSubject},
			{"no issuer", "alice", "", cliTestSubject},
			{"no subject", "alice", cliTestIssuer, ""},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if err := linkOIDCUserCommand(t.TempDir(), tt.username, tt.issuer, tt.subject, false); err == nil {
					t.Fatal("expected an error")
				}
			})
		}
	})

	t.Run("unopenable data dir returns error", func(t *testing.T) {
		err := linkOIDCUserCommand(blockingDataDir(t), "alice", cliTestIssuer, cliTestSubject, false)
		if err == nil {
			t.Fatal("expected an error when the auth store cannot be opened")
		}
	})

	t.Run("unknown user returns error", func(t *testing.T) {
		dataDir := seedLinkTestUser(t, "alice")
		err := linkOIDCUserCommand(dataDir, "nobody", cliTestIssuer, cliTestSubject, false)
		if err == nil {
			t.Fatal("expected an error for an unknown user")
		}
		if !strings.Contains(err.Error(), "user not found") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("links the account and reports it", func(t *testing.T) {
		dataDir := seedLinkTestUser(t, "alice")

		var err error
		output := captureStdout(t, func() {
			err = linkOIDCUserCommand(dataDir, "alice", cliTestIssuer, cliTestSubject, false)
		})
		if err != nil {
			t.Fatalf("linkOIDCUserCommand: %v", err)
		}
		// Both caveats matter here: linking changes how the account
		// signs in and leaves its sessions and its tokens working, and
		// an operator who reads the banner as a complete account of
		// what changed would be wrong on both counts.
		for _, want := range []string{
			"alice", cliTestIssuer, cliTestSubject, "oidc",
			"not cleared by this command", "-disable-user",
			"keeps working at its full privilege", "-remove-token",
		} {
			if !strings.Contains(output, want) {
				t.Fatalf("output %q does not mention %q", output, want)
			}
		}

		user, err := reopenStore(t, dataDir).GetUser("alice")
		if err != nil {
			t.Fatalf("GetUser: %v", err)
		}
		if user.AuthSource != auth.AuthSourceOIDC {
			t.Fatalf("auth_source = %q, want %q", user.AuthSource, auth.AuthSourceOIDC)
		}
		if user.ExternalSubject != auth.ExternalSubjectKey(cliTestIssuer, cliTestSubject) {
			t.Fatalf("external_subject = %q, want the subject key", user.ExternalSubject)
		}
	})

	t.Run("a second identity needs the relink flag", func(t *testing.T) {
		dataDir := seedLinkTestUser(t, "alice")
		captureStdout(t, func() {
			if err := linkOIDCUserCommand(dataDir, "alice", cliTestIssuer, cliTestSubject, false); err != nil {
				t.Errorf("first link: %v", err)
			}
		})

		if err := linkOIDCUserCommand(dataDir, "alice", cliTestIssuer, "subject-0002", false); err == nil {
			t.Fatal("expected an error when re-linking without -relink")
		}

		captureStdout(t, func() {
			if err := linkOIDCUserCommand(dataDir, "alice", cliTestIssuer, "subject-0002", true); err != nil {
				t.Errorf("relink: %v", err)
			}
		})

		user, err := reopenStore(t, dataDir).GetUser("alice")
		if err != nil {
			t.Fatalf("GetUser: %v", err)
		}
		if user.ExternalSubject != auth.ExternalSubjectKey(cliTestIssuer, "subject-0002") {
			t.Fatalf("external_subject = %q, want the second subject", user.ExternalSubject)
		}
	})
}

// RunCLICommands must route the two new flags, since a command nothing
// dispatches is a command that silently starts the server instead.
func TestRunCLICommandsDispatchesOIDCLinking(t *testing.T) {
	dataDir := seedLinkTestUser(t, "alice")

	link := &Flags{
		LinkOIDCUserCmd: true,
		Username:        "alice",
		OIDCIssuer:      cliTestIssuer,
		OIDCSubject:     cliTestSubject,
	}
	captureStdout(t, func() {
		if !RunCLICommands(link, dataDir, "") {
			t.Error("RunCLICommands did not handle -link-oidc-user")
		}
	})

	unlink := &Flags{UnlinkOIDCUserCmd: true, Username: "alice", RestorePassword: true}
	captureStdout(t, func() {
		if !RunCLICommands(unlink, dataDir, "") {
			t.Error("RunCLICommands did not handle -unlink-oidc-user")
		}
	})

	user, err := reopenStore(t, dataDir).GetUser("alice")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if user.AuthSource != auth.AuthSourceLocal || user.ExternalSubject != "" {
		t.Fatalf("account not returned to local: auth_source=%q external_subject=%q",
			user.AuthSource, user.ExternalSubject)
	}
}

func TestUnlinkOIDCUserCommand(t *testing.T) {
	t.Run("missing username returns error", func(t *testing.T) {
		if err := unlinkOIDCUserCommand(t.TempDir(), "", false); err == nil {
			t.Fatal("expected an error for an empty username")
		}
	})

	t.Run("unopenable data dir returns error", func(t *testing.T) {
		if err := unlinkOIDCUserCommand(blockingDataDir(t), "alice", false); err == nil {
			t.Fatal("expected an error when the auth store cannot be opened")
		}
	})

	t.Run("an unlinked account returns error", func(t *testing.T) {
		dataDir := seedLinkTestUser(t, "alice")

		var err error
		output := captureStdout(t, func() {
			err = unlinkOIDCUserCommand(dataDir, "alice", false)
		})
		if err == nil {
			t.Fatal("expected an error for an account that is not linked")
		}
		if !strings.Contains(err.Error(), "not linked") {
			t.Fatalf("unexpected error: %v", err)
		}
		if errors.Is(err, auth.ErrPartialUnlink) {
			t.Fatalf("a refusal that changed nothing was reported as a partial unlink: %v", err)
		}
		// A refusal that changed nothing says nothing about sessions;
		// the caveat belongs to outcomes that actually altered the
		// account.
		if strings.Contains(output, "not cleared by this command") {
			t.Fatalf("output %q carries the session caveat for a refusal that changed nothing", output)
		}
	})

	t.Run("by default the password is made unusable", func(t *testing.T) {
		dataDir := seedLinkTestUser(t, "alice")
		captureStdout(t, func() {
			if err := linkOIDCUserCommand(dataDir, "alice", cliTestIssuer, cliTestSubject, false); err != nil {
				t.Errorf("link: %v", err)
			}
		})

		var err error
		output := captureStdout(t, func() {
			err = unlinkOIDCUserCommand(dataDir, "alice", false)
		})
		if err != nil {
			t.Fatalf("unlinkOIDCUserCommand: %v", err)
		}
		// The output is read by an operator deciding whether access is
		// revoked, so it has to account for the password, the tokens and
		// the sessions, and it must not claim to have ended sessions
		// that live in another process's memory.
		for _, want := range []string{
			"unusable", "token(s) have been revoked", "-restore-password",
			"not cleared by this command", "-disable-user",
		} {
			if !strings.Contains(output, want) {
				t.Fatalf("output %q does not mention %q", output, want)
			}
		}
		if strings.Contains(output, "sessions have been invalidated") {
			t.Fatalf("output %q claims to have ended sessions this process cannot reach", output)
		}

		store := reopenStore(t, dataDir)
		if _, _, err := store.AuthenticateUser("alice", cliTestPassword); err == nil {
			t.Fatal("the pre-link password still worked after the default unlink")
		}
		user, err := store.GetUser("alice")
		if err != nil {
			t.Fatalf("GetUser: %v", err)
		}
		if user.AuthSource != auth.AuthSourceLocal || user.ExternalSubject != "" {
			t.Fatalf("account not returned to local: auth_source=%q external_subject=%q",
				user.AuthSource, user.ExternalSubject)
		}
	})

	t.Run("unlinks and warns about the restored password", func(t *testing.T) {
		dataDir := seedLinkTestUser(t, "alice")
		captureStdout(t, func() {
			if err := linkOIDCUserCommand(dataDir, "alice", cliTestIssuer, cliTestSubject, false); err != nil {
				t.Errorf("link: %v", err)
			}
		})

		var err error
		output := captureStdout(t, func() {
			err = unlinkOIDCUserCommand(dataDir, "alice", true)
		})
		if err != nil {
			t.Fatalf("unlinkOIDCUserCommand: %v", err)
		}
		for _, want := range []string{
			"works again", "tokens were", "left in place",
			"-update-user", "-disable-user", "not cleared by this command",
		} {
			if !strings.Contains(output, want) {
				t.Fatalf("output %q does not mention %q", output, want)
			}
		}
		if strings.Contains(output, "sessions have been invalidated") {
			t.Fatalf("output %q claims to have ended sessions this process cannot reach", output)
		}

		user, err := reopenStore(t, dataDir).GetUser("alice")
		if err != nil {
			t.Fatalf("GetUser: %v", err)
		}
		if user.AuthSource != auth.AuthSourceLocal || user.ExternalSubject != "" {
			t.Fatalf("account not returned to local: auth_source=%q external_subject=%q",
				user.AuthSource, user.ExternalSubject)
		}
	})
}
