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
	"fmt"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"golang.org/x/crypto/bcrypt"
)

// TestDescribeUserAuth covers the label the authentication column carries for
// every shape of stored row, including the two an operator is most likely to
// misread: a row written before federation existed, and a federated row whose
// stored identity does not parse.
func TestDescribeUserAuth(t *testing.T) {
	tests := []struct {
		name string
		user auth.StoredUser
		want string
	}{
		{
			name: "local account",
			user: auth.StoredUser{AuthSource: auth.AuthSourceLocal},
			want: "Local",
		},
		{
			name: "empty auth source reads as local",
			user: auth.StoredUser{},
			want: "Local",
		},
		{
			name: "federated account shows its issuer",
			user: auth.StoredUser{
				AuthSource:      auth.AuthSourceOIDC,
				ExternalSubject: auth.ExternalSubjectKey(cliTestIssuer, cliTestSubject),
			},
			want: cliTestIssuer,
		},
		{
			name: "unparsable identity falls back to the generic label",
			user: auth.StoredUser{
				AuthSource:      auth.AuthSourceOIDC,
				ExternalSubject: "nonsense",
			},
			want: "OIDC",
		},
		{
			name: "missing identity falls back to the generic label",
			user: auth.StoredUser{AuthSource: auth.AuthSourceOIDC},
			want: "OIDC",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := tt.user
			if got := describeUserAuth(&user); got != tt.want {
				t.Fatalf("describeUserAuth = %q, want %q", got, tt.want)
			}
			if strings.Contains(describeUserAuth(&user), cliTestSubject) {
				t.Fatal("the authentication column leaked the provider subject")
			}
		})
	}
}

func TestTruncateColumn(t *testing.T) {
	tests := []struct {
		name  string
		value string
		width int
		want  string
	}{
		{"short value is untouched", "Local", 10, "Local"},
		{"exact fit is untouched", "abcde", 5, "abcde"},
		{"long value is elided", "abcdefgh", 5, "ab..."},
		{"empty value", "", 5, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateColumn(tt.value, tt.width)
			if got != tt.want {
				t.Fatalf("truncateColumn(%q, %d) = %q, want %q", tt.value, tt.width, got, tt.want)
			}
			if len(got) > tt.width {
				t.Fatalf("truncateColumn(%q, %d) = %q, which overruns the column",
					tt.value, tt.width, got)
			}
		})
	}
}

// TestListUsersRowFormatMatchesColumnWidths guards the one thing that silently
// breaks the table: the widths in listUsersRowFormat drifting away from the
// constants the rules are measured from.
func TestListUsersRowFormatMatchesColumnWidths(t *testing.T) {
	row := fmt.Sprintf(listUsersRowFormat,
		strings.Repeat("u", listUsersUsernameWidth),
		strings.Repeat("c", listUsersCreatedWidth),
		strings.Repeat("l", listUsersLastLoginWidth),
		strings.Repeat("s", listUsersStatusWidth),
		strings.Repeat("a", listUsersAuthWidth),
		strings.Repeat("n", listUsersNotesWidth))

	if got := len(strings.TrimRight(row, "\n")); got != listUsersRuleWidth {
		t.Fatalf("a full row is %d characters wide, but the rules are %d",
			got, listUsersRuleWidth)
	}
}

func TestListUsersCommand(t *testing.T) {
	t.Run("unopenable data dir returns error", func(t *testing.T) {
		if err := listUsersCommand(blockingDataDir(t)); err == nil {
			t.Fatal("expected an error when the auth store cannot be opened")
		}
	})

	t.Run("an empty store says so", func(t *testing.T) {
		dataDir, store := newCLITestStore(t)
		store.Close()

		var err error
		output := captureStdout(t, func() {
			err = listUsersCommand(dataDir)
		})
		if err != nil {
			t.Fatalf("listUsersCommand: %v", err)
		}
		if !strings.Contains(output, "No users found.") {
			t.Fatalf("output %q does not report an empty store", output)
		}
	})

	t.Run("reports how each account signs in", func(t *testing.T) {
		dataDir, store := newCLITestStore(t)
		if err := store.CreateUser("alice", cliTestPassword, "", "", ""); err != nil {
			store.Close()
			t.Fatalf("CreateUser alice: %v", err)
		}
		if err := store.CreateUser("bob", cliTestPassword, "", "", ""); err != nil {
			store.Close()
			t.Fatalf("CreateUser bob: %v", err)
		}
		if _, err := store.LinkFederatedIdentity("bob", cliTestIssuer, cliTestSubject, false); err != nil {
			store.Close()
			t.Fatalf("LinkFederatedIdentity: %v", err)
		}
		store.Close()

		var err error
		output := captureStdout(t, func() {
			err = listUsersCommand(dataDir)
		})
		if err != nil {
			t.Fatalf("listUsersCommand: %v", err)
		}

		if !strings.Contains(output, "Authentication") {
			t.Fatalf("output %q has no authentication column", output)
		}
		alice := rowFor(t, output, "alice")
		if !strings.Contains(alice, "Local") {
			t.Fatalf("the local account reads as %q, want a Local row", alice)
		}
		bob := rowFor(t, output, "bob")
		if !strings.Contains(bob, cliTestIssuer) {
			t.Fatalf("the federated account reads as %q, want the issuer", bob)
		}

		// The subject is an identifier there is no reason to print, and a
		// long one at that.
		if strings.Contains(output, cliTestSubject) {
			t.Fatalf("the listing leaked the provider subject: %s", output)
		}
	})

	t.Run("a long issuer is elided rather than breaking the table", func(t *testing.T) {
		longIssuer := "https://" + strings.Repeat("a", 60) + ".example.com"

		dataDir, store := newCLITestStore(t)
		if err := store.CreateUser("carol", cliTestPassword,
			strings.Repeat("n", 60), "", ""); err != nil {
			store.Close()
			t.Fatalf("CreateUser carol: %v", err)
		}
		if _, err := store.LinkFederatedIdentity("carol", longIssuer, cliTestSubject, false); err != nil {
			store.Close()
			t.Fatalf("LinkFederatedIdentity: %v", err)
		}
		store.Close()

		var err error
		output := captureStdout(t, func() {
			err = listUsersCommand(dataDir)
		})
		if err != nil {
			t.Fatalf("listUsersCommand: %v", err)
		}

		row := rowFor(t, output, "carol")
		if !strings.Contains(row, "...") {
			t.Fatalf("row %q does not elide the long issuer", row)
		}
		if strings.Contains(row, longIssuer) {
			t.Fatalf("row %q prints the long issuer in full", row)
		}
		if len(row) > listUsersRuleWidth {
			t.Fatalf("row %q is %d characters wide, past the %d-character rule",
				row, len(row), listUsersRuleWidth)
		}
	})
}

// rowFor returns the single line of table output describing username.
func rowFor(t *testing.T, output, username string) string {
	t.Helper()

	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, username+" ") {
			return line
		}
	}
	t.Fatalf("no row for %q in output:\n%s", username, output)
	return ""
}

// TestListUsersCommandStatusColumns covers the two row variants the
// authentication rework left untouched but which share the same fixed-width
// row: an account that has signed in before, and one locked out after failed
// attempts.
func TestListUsersCommandStatusColumns(t *testing.T) {
	dataDir := t.TempDir()
	store, err := auth.NewAuthStore(dataDir, 0, 1)
	if err != nil {
		t.Fatalf("NewAuthStore: %v", err)
	}
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)

	if err := store.CreateUser("dave", cliTestPassword, "", "", ""); err != nil {
		store.Close()
		t.Fatalf("CreateUser dave: %v", err)
	}
	if err := store.CreateUser("erin", cliTestPassword, "", "", ""); err != nil {
		store.Close()
		t.Fatalf("CreateUser erin: %v", err)
	}
	if _, _, err := store.AuthenticateUser("dave", cliTestPassword); err != nil {
		store.Close()
		t.Fatalf("AuthenticateUser dave: %v", err)
	}
	// One wrong password is enough to trip the single-attempt lockout this
	// store was built with.
	if _, _, err := store.AuthenticateUser("erin", "WrongPassword1234"); err == nil {
		store.Close()
		t.Fatal("expected the wrong password to be rejected")
	}
	store.Close()

	output := captureStdout(t, func() {
		if err := listUsersCommand(dataDir); err != nil {
			t.Errorf("listUsersCommand: %v", err)
		}
	})

	dave := rowFor(t, output, "dave")
	if strings.Contains(dave, "Never") {
		t.Fatalf("row %q still reports no last login", dave)
	}
	erin := rowFor(t, output, "erin")
	if !strings.Contains(erin, "DISABLED (1 fails)") {
		t.Fatalf("row %q does not report the lockout", erin)
	}
}
