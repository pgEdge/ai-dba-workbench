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
	"unicode/utf8"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"golang.org/x/crypto/bcrypt"
)

// TestDescribeUserAuth covers the label the authentication column carries for
// every shape of stored row, including the ones an operator is most likely to
// misread: a row with no auth_source at all, which login refuses and so must
// not read as local, and a federated row whose stored identity does not parse.
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
			name: "empty auth source does not read as local",
			user: auth.StoredUser{},
			want: "Unknown",
		},
		{
			name: "an unexpected source is shown as stored",
			user: auth.StoredUser{AuthSource: "saml"},
			want: "saml",
		},
		{
			name: "an unexpected source with an identity shows the issuer",
			user: auth.StoredUser{
				AuthSource:      "saml",
				ExternalSubject: auth.ExternalSubjectKey(cliTestIssuer, cliTestSubject),
			},
			want: cliTestIssuer,
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
		{"width no wider than the ellipsis cuts without one", "abcdefgh", 2, "ab"},
		{"zero width yields nothing", "abcdefgh", 0, ""},
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
// constants the rules are measured from. The rules span the header row, whose
// last column is the authentication heading.
func TestListUsersRowFormatMatchesColumnWidths(t *testing.T) {
	row := fmt.Sprintf(listUsersRowFormat,
		strings.Repeat("u", listUsersUsernameWidth),
		strings.Repeat("c", listUsersCreatedWidth),
		strings.Repeat("l", listUsersLastLoginWidth),
		strings.Repeat("s", listUsersStatusWidth),
		strings.Repeat("n", listUsersNotesWidth),
		listUsersAuthHeader)

	if got := len(strings.TrimRight(row, "\n")); got != listUsersRuleWidth {
		t.Fatalf("a full header row is %d characters wide, but the rules are %d",
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

	t.Run("a long issuer is printed in full as the last column", func(t *testing.T) {
		// Two issuers from one provider that differ only at the end, as
		// Entra tenants and Keycloak realms do: truncating them would
		// make the two accounts look as though they share a provider.
		prefix := "https://login.example.com/" + strings.Repeat("a", 40)
		issuerOne := prefix + "/realm-one"
		issuerTwo := prefix + "/realm-two"

		dataDir, store := newCLITestStore(t)
		for _, account := range []struct{ name, issuer string }{
			{"carol", issuerOne},
			{"chris", issuerTwo},
		} {
			if err := store.CreateUser(account.name, cliTestPassword,
				strings.Repeat("n", 60), "", ""); err != nil {
				store.Close()
				t.Fatalf("CreateUser %s: %v", account.name, err)
			}
			if _, err := store.LinkFederatedIdentity(account.name, account.issuer,
				cliTestSubject+"-"+account.name, false); err != nil {
				store.Close()
				t.Fatalf("LinkFederatedIdentity %s: %v", account.name, err)
			}
		}
		store.Close()

		var err error
		output := captureStdout(t, func() {
			err = listUsersCommand(dataDir)
		})
		if err != nil {
			t.Fatalf("listUsersCommand: %v", err)
		}

		for name, issuer := range map[string]string{"carol": issuerOne, "chris": issuerTwo} {
			row := rowFor(t, output, name)
			if !strings.HasSuffix(row, " "+issuer) {
				t.Fatalf("row %q does not end with the full issuer %q", row, issuer)
			}
			// The long annotation is still cut to the notes column, which
			// ends exactly where the authentication column begins.
			notesEnd := listUsersRuleWidth - len(listUsersAuthHeader)
			if got := row[:notesEnd]; !strings.HasSuffix(got, "... ") {
				t.Fatalf("row %q does not elide the long annotation before the issuer", row)
			}
			if row[notesEnd:] != issuer {
				t.Fatalf("row %q does not start the issuer at column %d", row, notesEnd)
			}
		}
	})

	t.Run("control characters are stripped before printing", func(t *testing.T) {
		// An ESC sequence that would clear the screen and a newline that
		// would forge a row, in the annotation. LinkFederatedIdentity now
		// refuses an issuer holding an ASCII control character, because
		// url.Parse does, so the issuer carries the single-character C1
		// form of the same sequence (CSI, U+009B), which it accepts and
		// which a row already stored, or provisioned from an ID token,
		// could equally hold.
		const escape = "\x1b[2J"
		issuer := "https://idp.example.com/\u009b2Jrealm"

		dataDir, store := newCLITestStore(t)
		if err := store.CreateUser("dora", cliTestPassword,
			"note"+escape+"\nforged", "", ""); err != nil {
			store.Close()
			t.Fatalf("CreateUser dora: %v", err)
		}
		if _, err := store.LinkFederatedIdentity("dora", issuer, cliTestSubject, false); err != nil {
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

		if strings.ContainsRune(output, '\x1b') || strings.ContainsRune(output, '\u009b') {
			t.Fatalf("the listing printed a raw escape character: %q", output)
		}
		if strings.Contains(output, "\nforged") {
			t.Fatalf("the annotation's newline reached the terminal: %q", output)
		}
		row := rowFor(t, output, "dora")
		if !strings.Contains(row, "note?[2J?forged") {
			t.Fatalf("row %q does not show the annotation with its controls replaced", row)
		}
		if !strings.HasSuffix(row, "https://idp.example.com/?2Jrealm") {
			t.Fatalf("row %q does not show the issuer with its controls replaced", row)
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
	store, err := auth.NewAuthStore(dataDir, 0, 1, auth.AuditKeyForTesting())
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

// TestTruncateColumnMultiByte checks that a value is measured and cut in runes.
// An annotation is free text an administrator typed, so it can hold multi-byte
// characters: a byte-wise slice would leave a partial rune behind and print a
// replacement character, and a byte-wise measure would cut a value the
// rune-counting padding would have fitted.
func TestTruncateColumnMultiByte(t *testing.T) {
	tests := []struct {
		name  string
		value string
		width int
		want  string
	}{
		{
			// Thirty two-byte runes, so the value is truncated; seven
			// runes are left once the ellipsis is allowed for.
			name:  "two-byte runes are not split",
			value: strings.Repeat("\u00e6\u00f8\u00e5", 10),
			width: 10,
			want:  "\u00e6\u00f8\u00e5\u00e6\u00f8\u00e5\u00e6...",
		},
		{
			// Four-byte runes count as one column each, as the padding
			// counts them, so three survive beside the ellipsis.
			name:  "four-byte runes count once each",
			value: strings.Repeat("\U0001F600", 8),
			width: 6,
			want:  strings.Repeat("\U0001F600", 3) + "...",
		},
		{
			name:  "a value that fits is untouched whatever its encoding",
			value: "caf\u00e9",
			width: 5,
			want:  "caf\u00e9",
		},
		{
			// Ten runes but twenty bytes: it fits the column exactly, as
			// the rune-counting %-10s padding sees it, so it must not be
			// cut however many bytes it takes.
			name:  "a value that fits in runes but not bytes is untouched",
			value: strings.Repeat("\u00e6\u00f8", 5),
			width: 10,
			want:  strings.Repeat("\u00e6\u00f8", 5),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateColumn(tt.value, tt.width)
			if got != tt.want {
				t.Fatalf("truncateColumn(%q, %d) = %q, want %q",
					tt.value, tt.width, got, tt.want)
			}
			if utf8.RuneCountInString(got) > tt.width {
				t.Fatalf("truncateColumn(%q, %d) = %q, which overruns the column",
					tt.value, tt.width, got)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("truncateColumn(%q, %d) = %q, which is not valid UTF-8",
					tt.value, tt.width, got)
			}
			if strings.ContainsRune(got, utf8.RuneError) {
				t.Fatalf("truncateColumn(%q, %d) = %q, which split a rune",
					tt.value, tt.width, got)
			}
		})
	}
}

// TestListUsersRowFormatIsStable pins the format string the width constants
// generate, so that a change to the layout has to be made deliberately rather
// than by accident.
func TestListUsersRowFormatIsStable(t *testing.T) {
	const want = "%-20s %-17s %-17s %-20s %-20s %s\n"
	if listUsersRowFormat != want {
		t.Fatalf("listUsersRowFormat = %q, want %q", listUsersRowFormat, want)
	}
}

func TestStripControlCharacters(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"plain text is untouched", "https://idp.example.com", "https://idp.example.com"},
		{"non-ASCII text is untouched", "caf\u00e9 \u00e6\u00f8", "caf\u00e9 \u00e6\u00f8"},
		{"ESC sequence", "a\x1b[31mb", "a?[31mb"},
		{"C0 controls", "a\x00b\x07c\td\re\nf", "a?b?c?d?e?f"},
		{"DEL", "a\x7fb", "a?b"},
		{"C1 single-byte CSI", "a\u009b31mb", "a?31mb"},
		{"right-to-left override", "https://idp.example.com/\u202emoc.live", "https://idp.example.com/?moc.live"},
		{"zero-width characters", "a\u200bb\u200dc\ufeffd", "a?b?c?d"},
		{"empty value", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripControlCharacters(tt.value); got != tt.want {
				t.Fatalf("stripControlCharacters(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}
