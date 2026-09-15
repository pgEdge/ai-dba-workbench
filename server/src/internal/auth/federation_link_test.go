/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package auth

import (
	"database/sql"
	"strings"
	"testing"
)

const (
	linkTestIssuer   = "https://idp.example.com"
	linkTestSubject  = "subject-0001"
	linkTestPassword = "Sup3r-Str0ng-Pass!"
)

// linkedAccountState reads the three columns linking decides, so assertions do
// not each have to spell the query out.
func linkedAccountState(t *testing.T, store *AuthStore, username string) (authSource, externalSubject, passwordHash string) {
	t.Helper()

	var subject sql.NullString
	if err := store.db.QueryRow(
		"SELECT auth_source, external_subject, password_hash FROM users WHERE username = ?",
		username).Scan(&authSource, &subject, &passwordHash); err != nil {
		t.Fatalf("reading account state for %s: %v", username, err)
	}
	return authSource, subject.String, passwordHash
}

func TestLinkFederatedIdentityStampsAccount(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("alice", linkTestPassword, "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	_, _, hashBefore := linkedAccountState(t, store, "alice")

	key, err := store.LinkFederatedIdentity("alice", linkTestIssuer, linkTestSubject, false)
	if err != nil {
		t.Fatalf("LinkFederatedIdentity: %v", err)
	}
	want := ExternalSubjectKey(linkTestIssuer, linkTestSubject)
	if key != want {
		t.Fatalf("returned key = %q, want %q", key, want)
	}

	authSource, subject, hashAfter := linkedAccountState(t, store, "alice")
	if authSource != AuthSourceOIDC {
		t.Fatalf("auth_source = %q, want %q", authSource, AuthSourceOIDC)
	}
	if subject != want {
		t.Fatalf("external_subject = %q, want %q", subject, want)
	}
	// Requirement: linking must not destroy the credential it supersedes.
	if hashAfter != hashBefore {
		t.Fatal("linking changed the stored password hash")
	}
}

// The whole point of the command: a federated login must now resolve with
// provisioning left at its default of disabled.
func TestLinkFederatedIdentityMakesLoginResolve(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("bob", linkTestPassword, "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	identity := FederatedIdentity{
		Issuer:   linkTestIssuer,
		Subject:  linkTestSubject,
		Username: "bob",
	}

	if _, err := store.ResolveFederatedUser(identity, FederationOptions{}); err == nil {
		t.Fatal("ResolveFederatedUser succeeded before the account was linked")
	}

	if _, err := store.LinkFederatedIdentity("bob", linkTestIssuer, linkTestSubject, false); err != nil {
		t.Fatalf("LinkFederatedIdentity: %v", err)
	}

	user, err := store.ResolveFederatedUser(identity, FederationOptions{})
	if err != nil {
		t.Fatalf("ResolveFederatedUser after linking: %v", err)
	}
	if user.Username != "bob" {
		t.Fatalf("resolved username = %q, want %q", user.Username, "bob")
	}
}

// Password login must stop working the moment the account is linked, without
// the link having had to clear the hash.
func TestLinkFederatedIdentityStopsPasswordLogin(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("carol", linkTestPassword, "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, _, err := store.AuthenticateUser("carol", linkTestPassword); err != nil {
		t.Fatalf("AuthenticateUser before linking: %v", err)
	}
	if _, err := store.LinkFederatedIdentity("carol", linkTestIssuer, linkTestSubject, false); err != nil {
		t.Fatalf("LinkFederatedIdentity: %v", err)
	}
	if _, _, err := store.AuthenticateUser("carol", linkTestPassword); err == nil {
		t.Fatal("password login succeeded for a linked account")
	}
}

func TestLinkFederatedIdentityIsIdempotent(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("dave", linkTestPassword, "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := store.LinkFederatedIdentity("dave", linkTestIssuer, linkTestSubject, false); err != nil {
			t.Fatalf("LinkFederatedIdentity pass %d: %v", i, err)
		}
	}
}

func TestLinkFederatedIdentityRefusesSecondIdentityWithoutRelink(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("erin", linkTestPassword, "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := store.LinkFederatedIdentity("erin", linkTestIssuer, linkTestSubject, false); err != nil {
		t.Fatalf("LinkFederatedIdentity: %v", err)
	}

	_, err := store.LinkFederatedIdentity("erin", linkTestIssuer, "subject-0002", false)
	if err == nil {
		t.Fatal("re-linking succeeded without -relink")
	}
	// The operator has to be able to see which identity is in the way.
	if !strings.Contains(err.Error(), linkTestSubject) || !strings.Contains(err.Error(), "-relink") {
		t.Fatalf("error does not name the existing subject and the flag: %v", err)
	}
	if _, subject, _ := linkedAccountState(t, store, "erin"); subject != ExternalSubjectKey(linkTestIssuer, linkTestSubject) {
		t.Fatalf("refused link changed external_subject to %q", subject)
	}
}

func TestLinkFederatedIdentityRelinkMovesIdentity(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("frank", linkTestPassword, "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := store.LinkFederatedIdentity("frank", linkTestIssuer, linkTestSubject, false); err != nil {
		t.Fatalf("LinkFederatedIdentity: %v", err)
	}
	if _, err := store.LinkFederatedIdentity("frank", linkTestIssuer, "subject-0002", true); err != nil {
		t.Fatalf("LinkFederatedIdentity with relink: %v", err)
	}

	_, subject, _ := linkedAccountState(t, store, "frank")
	if subject != ExternalSubjectKey(linkTestIssuer, "subject-0002") {
		t.Fatalf("external_subject = %q, want the second subject", subject)
	}
}

// The unique index decides collisions, and the message has to name the account
// already holding the subject rather than leak a constraint error.
func TestLinkFederatedIdentityReportsCollidingAccount(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("grace", linkTestPassword, "", "", ""); err != nil {
		t.Fatalf("CreateUser grace: %v", err)
	}
	if err := store.CreateUser("heidi", linkTestPassword, "", "", ""); err != nil {
		t.Fatalf("CreateUser heidi: %v", err)
	}
	if _, err := store.LinkFederatedIdentity("grace", linkTestIssuer, linkTestSubject, false); err != nil {
		t.Fatalf("LinkFederatedIdentity grace: %v", err)
	}

	_, err := store.LinkFederatedIdentity("heidi", linkTestIssuer, linkTestSubject, false)
	if err == nil {
		t.Fatal("two accounts were linked to one subject")
	}
	if !strings.Contains(err.Error(), "grace") {
		t.Fatalf("collision error does not name the holding account: %v", err)
	}
	if strings.Contains(err.Error(), "UNIQUE constraint") {
		t.Fatalf("collision error leaks the raw constraint failure: %v", err)
	}
	if authSource, subject, _ := linkedAccountState(t, store, "heidi"); authSource != AuthSourceLocal || subject != "" {
		t.Fatalf("refused link altered heidi: auth_source=%q external_subject=%q", authSource, subject)
	}
}

// Relinking an account onto a subject another account holds hits the same
// constraint, and must be refused with the same clarity.
func TestLinkFederatedIdentityRelinkStillRefusesCollision(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("ivan", linkTestPassword, "", "", ""); err != nil {
		t.Fatalf("CreateUser ivan: %v", err)
	}
	if err := store.CreateUser("judy", linkTestPassword, "", "", ""); err != nil {
		t.Fatalf("CreateUser judy: %v", err)
	}
	if _, err := store.LinkFederatedIdentity("ivan", linkTestIssuer, linkTestSubject, false); err != nil {
		t.Fatalf("LinkFederatedIdentity ivan: %v", err)
	}
	if _, err := store.LinkFederatedIdentity("judy", linkTestIssuer, "subject-0002", false); err != nil {
		t.Fatalf("LinkFederatedIdentity judy: %v", err)
	}

	_, err := store.LinkFederatedIdentity("judy", linkTestIssuer, linkTestSubject, true)
	if err == nil {
		t.Fatal("relink onto an occupied subject succeeded")
	}
	if !strings.Contains(err.Error(), "ivan") {
		t.Fatalf("collision error does not name the holding account: %v", err)
	}
}

func TestLinkFederatedIdentityRefusesServiceAccount(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateServiceAccount("robot", "", "", ""); err != nil {
		t.Fatalf("CreateServiceAccount: %v", err)
	}

	_, err := store.LinkFederatedIdentity("robot", linkTestIssuer, linkTestSubject, false)
	if err == nil {
		t.Fatal("a service account was linked to an identity provider")
	}
	if !strings.Contains(err.Error(), "service account") {
		t.Fatalf("unexpected error: %v", err)
	}
	if authSource, subject, _ := linkedAccountState(t, store, "robot"); authSource != AuthSourceLocal || subject != "" {
		t.Fatalf("refused link altered the service account: auth_source=%q external_subject=%q", authSource, subject)
	}
}

func TestLinkFederatedIdentityArgumentErrors(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("kate", linkTestPassword, "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	tests := []struct {
		name     string
		username string
		issuer   string
		subject  string
		want     string
	}{
		{"no username", "", linkTestIssuer, linkTestSubject, "username is required"},
		{"no issuer", "kate", "", linkTestSubject, "issuer and a subject"},
		{"no subject", "kate", linkTestIssuer, "", "issuer and a subject"},
		{"unknown user", "nobody", linkTestIssuer, linkTestSubject, "user not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.LinkFederatedIdentity(tt.username, tt.issuer, tt.subject, false)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestUnlinkFederatedIdentityRestoresLocalLoginOnRequest(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("leo", linkTestPassword, "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := store.LinkFederatedIdentity("leo", linkTestIssuer, linkTestSubject, false); err != nil {
		t.Fatalf("LinkFederatedIdentity: %v", err)
	}

	key, _, err := store.UnlinkFederatedIdentity("leo", true)
	if err != nil {
		t.Fatalf("UnlinkFederatedIdentity: %v", err)
	}
	if key != ExternalSubjectKey(linkTestIssuer, linkTestSubject) {
		t.Fatalf("returned key = %q, want the linked key", key)
	}

	authSource, subject, _ := linkedAccountState(t, store, "leo")
	if authSource != AuthSourceLocal {
		t.Fatalf("auth_source = %q, want %q", authSource, AuthSourceLocal)
	}
	if subject != "" {
		t.Fatalf("external_subject = %q, want it cleared", subject)
	}
	// With restorePassword the pre-link password is deliberately live
	// again; that is the flag's whole purpose, asserted so that nobody has
	// to guess what it does.
	if _, _, err := store.AuthenticateUser("leo", linkTestPassword); err != nil {
		t.Fatalf("the pre-link password did not work after unlinking: %v", err)
	}
	// And the identity no longer resolves.
	if _, err := store.ResolveFederatedUser(FederatedIdentity{
		Issuer:   linkTestIssuer,
		Subject:  linkTestSubject,
		Username: "leo",
	}, FederationOptions{}); err == nil {
		t.Fatal("the federated identity still resolved after unlinking")
	}
}

// An account the provider created holds an unusable hash, so unlinking it
// leaves nothing that can log in.
func TestUnlinkFederatedIdentityLeavesProvisionedAccountUnreachable(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	identity := FederatedIdentity{
		Issuer:   linkTestIssuer,
		Subject:  linkTestSubject,
		Username: "mallory",
	}
	if _, err := store.ResolveFederatedUser(identity, FederationOptions{ProvisionUsers: true}); err != nil {
		t.Fatalf("provisioning: %v", err)
	}
	if _, _, err := store.UnlinkFederatedIdentity("mallory", true); err != nil {
		t.Fatalf("UnlinkFederatedIdentity: %v", err)
	}
	if _, _, err := store.AuthenticateUser("mallory", linkTestPassword); err == nil {
		t.Fatal("a provisioned account became reachable by password after unlinking")
	}
}

func TestUnlinkFederatedIdentityArgumentErrors(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("nina", linkTestPassword, "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	tests := []struct {
		name     string
		username string
		want     string
	}{
		{"no username", "", "username is required"},
		{"unknown user", "nobody", "user not found"},
		{"not linked", "nina", "not linked"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := store.UnlinkFederatedIdentity(tt.username, false)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestParseExternalSubjectKey(t *testing.T) {
	tests := []struct {
		name        string
		key         string
		wantIssuer  string
		wantSubject string
		wantOK      bool
	}{
		{"round trip", ExternalSubjectKey(linkTestIssuer, linkTestSubject), linkTestIssuer, linkTestSubject, true},
		{"empty subject", ExternalSubjectKey(linkTestIssuer, ""), linkTestIssuer, "", true},
		{"separator in subject", ExternalSubjectKey("https://a", "b|c"), "https://a", "b|c", true},
		{"no separator", "nonsense", "", "", false},
		{"leading separator", "|x", "", "", false},
		{"length not a number", "x|y|z", "", "", false},
		{"length overruns", "99|short|s", "", "", false},
		{"no separator after issuer", "1|ab", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issuer, subject, ok := parseExternalSubjectKey(tt.key)
			if ok != tt.wantOK || issuer != tt.wantIssuer || subject != tt.wantSubject {
				t.Fatalf("parseExternalSubjectKey(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tt.key, issuer, subject, ok, tt.wantIssuer, tt.wantSubject, tt.wantOK)
			}
		})
	}
}

func TestDescribeSubjectKey(t *testing.T) {
	described := describeSubjectKey(ExternalSubjectKey(linkTestIssuer, linkTestSubject))
	if !strings.Contains(described, linkTestIssuer) || !strings.Contains(described, linkTestSubject) {
		t.Fatalf("describeSubjectKey = %q, want both halves named", described)
	}
	if fallback := describeSubjectKey("nonsense"); !strings.Contains(fallback, "nonsense") {
		t.Fatalf("describeSubjectKey fallback = %q, want the raw key", fallback)
	}
}

// The refusal an operator reads has to carry everything -link-oidc-user needs.
func TestResolveFederatedUserRefusalNamesTheLinkCommand(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	_, err := store.ResolveFederatedUser(FederatedIdentity{
		Issuer:   linkTestIssuer,
		Subject:  linkTestSubject,
		Username: "oscar",
	}, FederationOptions{})
	if err == nil {
		t.Fatal("an unknown subject resolved with provisioning disabled")
	}
	for _, want := range []string{
		"provisioning is disabled",
		"-link-oidc-user",
		`-issuer "` + linkTestIssuer + `"`,
		`-subject "` + linkTestSubject + `"`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal %q does not contain %q", err.Error(), want)
		}
	}
}

// accountRow is every column of the users row, so a test can assert that a
// command moved the columns it claims to and nothing else.
type accountRow struct {
	id              int64
	username        string
	passwordHash    string
	enabled         bool
	annotation      string
	displayName     string
	email           string
	failedAttempts  int
	isSuperuser     bool
	isService       bool
	authSource      string
	externalSubject string
	groups          []int64
}

func readAccountRow(t *testing.T, store *AuthStore, username string) accountRow {
	t.Helper()

	var row accountRow
	var displayName, email, subject sql.NullString
	err := store.db.QueryRow(
		`SELECT id, username, password_hash, enabled, annotation, display_name, email,
		        failed_attempts, is_superuser, is_service_account, auth_source, external_subject
		 FROM users WHERE username = ?`, username).
		Scan(&row.id, &row.username, &row.passwordHash, &row.enabled, &row.annotation,
			&displayName, &email, &row.failedAttempts, &row.isSuperuser, &row.isService,
			&row.authSource, &subject)
	if err != nil {
		t.Fatalf("reading the users row for %s: %v", username, err)
	}
	row.displayName = displayName.String
	row.email = email.String
	row.externalSubject = subject.String

	groups, err := store.GetUserGroups(row.id)
	if err != nil {
		t.Fatalf("reading group membership for %s: %v", username, err)
	}
	row.groups = groups
	return row
}

// seedFullyPopulatedAccount creates an account with every column and its group
// membership set to something distinctive, so that an unintended write to any
// of them shows up.
func seedFullyPopulatedAccount(t *testing.T, store *AuthStore, username string) accountRow {
	t.Helper()

	if err := store.CreateUser(username, linkTestPassword, "notes", "Full Name", "user@example.com"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.SetUserSuperuser(username, true); err != nil {
		t.Fatalf("SetUserSuperuser: %v", err)
	}
	groupID, err := store.CreateGroup("linked-group-"+username, "")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	before := readAccountRow(t, store, username)
	if err := store.AddUserToGroup(groupID, before.id); err != nil {
		t.Fatalf("AddUserToGroup: %v", err)
	}
	return readAccountRow(t, store, username)
}

// assertOnlyLinkColumnsChanged compares two snapshots, allowing only the
// columns the caller names to differ.
func assertOnlyLinkColumnsChanged(t *testing.T, before, after accountRow, allowHash bool) {
	t.Helper()

	if after.id != before.id || after.username != before.username {
		t.Fatalf("identity columns changed: %+v -> %+v", before, after)
	}
	if after.enabled != before.enabled {
		t.Fatalf("enabled changed from %v to %v", before.enabled, after.enabled)
	}
	if after.isSuperuser != before.isSuperuser {
		t.Fatalf("is_superuser changed from %v to %v", before.isSuperuser, after.isSuperuser)
	}
	if after.isService != before.isService {
		t.Fatalf("is_service_account changed from %v to %v", before.isService, after.isService)
	}
	if after.displayName != before.displayName || after.email != before.email {
		t.Fatalf("profile columns changed: display_name %q -> %q, email %q -> %q",
			before.displayName, after.displayName, before.email, after.email)
	}
	if after.annotation != before.annotation {
		t.Fatalf("annotation changed from %q to %q", before.annotation, after.annotation)
	}
	if after.failedAttempts != before.failedAttempts {
		t.Fatalf("failed_attempts changed from %d to %d", before.failedAttempts, after.failedAttempts)
	}
	if len(after.groups) != len(before.groups) {
		t.Fatalf("group membership changed from %v to %v", before.groups, after.groups)
	}
	for i := range after.groups {
		if after.groups[i] != before.groups[i] {
			t.Fatalf("group membership changed from %v to %v", before.groups, after.groups)
		}
	}
	if !allowHash && after.passwordHash != before.passwordHash {
		t.Fatal("password_hash changed when it should not have")
	}
}

func TestLinkFederatedIdentityTouchesOnlyTheTwoColumns(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	before := seedFullyPopulatedAccount(t, store, "priya")
	if _, err := store.LinkFederatedIdentity("priya", linkTestIssuer, linkTestSubject, false); err != nil {
		t.Fatalf("LinkFederatedIdentity: %v", err)
	}
	after := readAccountRow(t, store, "priya")

	assertOnlyLinkColumnsChanged(t, before, after, false)
	if after.authSource != AuthSourceOIDC ||
		after.externalSubject != ExternalSubjectKey(linkTestIssuer, linkTestSubject) {
		t.Fatalf("the two intended columns are wrong: auth_source=%q external_subject=%q",
			after.authSource, after.externalSubject)
	}
}

func TestUnlinkFederatedIdentityTouchesOnlyTheIntendedColumns(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	seedFullyPopulatedAccount(t, store, "quinn")
	if _, err := store.LinkFederatedIdentity("quinn", linkTestIssuer, linkTestSubject, false); err != nil {
		t.Fatalf("LinkFederatedIdentity: %v", err)
	}
	before := readAccountRow(t, store, "quinn")

	if _, _, err := store.UnlinkFederatedIdentity("quinn", true); err != nil {
		t.Fatalf("UnlinkFederatedIdentity: %v", err)
	}
	after := readAccountRow(t, store, "quinn")

	assertOnlyLinkColumnsChanged(t, before, after, false)
	if after.authSource != AuthSourceLocal || after.externalSubject != "" {
		t.Fatalf("the two intended columns are wrong: auth_source=%q external_subject=%q",
			after.authSource, after.externalSubject)
	}
}

// Without restorePassword the hash is the one further column unlinking is
// allowed to move, and it must become unusable rather than merely different.
func TestUnlinkFederatedIdentityMakesThePasswordUnusable(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	seedFullyPopulatedAccount(t, store, "rafael")
	if _, err := store.LinkFederatedIdentity("rafael", linkTestIssuer, linkTestSubject, false); err != nil {
		t.Fatalf("LinkFederatedIdentity: %v", err)
	}
	before := readAccountRow(t, store, "rafael")

	if _, _, err := store.UnlinkFederatedIdentity("rafael", false); err != nil {
		t.Fatalf("UnlinkFederatedIdentity: %v", err)
	}
	after := readAccountRow(t, store, "rafael")

	assertOnlyLinkColumnsChanged(t, before, after, true)
	if after.passwordHash == before.passwordHash {
		t.Fatal("the password hash survived an unlink without restorePassword")
	}
	if after.passwordHash == "" {
		t.Fatal("the password hash was emptied rather than replaced")
	}
	if _, _, err := store.AuthenticateUser("rafael", linkTestPassword); err == nil {
		t.Fatal("the pre-link password still worked after an unlink without restorePassword")
	}
	// The account is recoverable: an operator sets a new password and it
	// works, which is the state the command promises to leave behind.
	if err := store.UpdateUser("rafael", "An0ther-Str0ng-Pass!", "", "", ""); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if _, _, err := store.AuthenticateUser("rafael", "An0ther-Str0ng-Pass!"); err != nil {
		t.Fatalf("a freshly set password did not work: %v", err)
	}
}

// A session minted before the account's authentication source changed must not
// outlive the change: ValidateSessionToken re-reads only enabled, so nothing
// else would stop it.
func TestLinkFederatedIdentityInvalidatesSessions(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("sam", linkTestPassword, "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	token, _, err := store.CreateSessionForUser("sam")
	if err != nil {
		t.Fatalf("CreateSessionForUser: %v", err)
	}
	if _, err := store.ValidateSessionToken(token); err != nil {
		t.Fatalf("the session was not valid to begin with: %v", err)
	}

	if _, err := store.LinkFederatedIdentity("sam", linkTestIssuer, linkTestSubject, false); err != nil {
		t.Fatalf("LinkFederatedIdentity: %v", err)
	}
	if _, err := store.ValidateSessionToken(token); err == nil {
		t.Fatal("a session survived the account being linked")
	}
}

func TestUnlinkFederatedIdentityInvalidatesSessions(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("tara", linkTestPassword, "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := store.LinkFederatedIdentity("tara", linkTestIssuer, linkTestSubject, false); err != nil {
		t.Fatalf("LinkFederatedIdentity: %v", err)
	}
	// A federated login mints its session through the same function a
	// password login uses, so this is the session an unlink has to kill.
	token, _, err := store.CreateSessionForUser("tara")
	if err != nil {
		t.Fatalf("CreateSessionForUser: %v", err)
	}
	if _, err := store.ValidateSessionToken(token); err != nil {
		t.Fatalf("the session was not valid to begin with: %v", err)
	}

	if _, _, err := store.UnlinkFederatedIdentity("tara", true); err != nil {
		t.Fatalf("UnlinkFederatedIdentity: %v", err)
	}
	if _, err := store.ValidateSessionToken(token); err == nil {
		t.Fatal("a session survived the account being unlinked")
	}
}

// The guard rides on the UPDATE, so an account whose identity some other
// source owns is refused rather than quietly converted.
func TestLinkFederatedIdentityRefusesForeignAuthSource(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("ulric", linkTestPassword, "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := store.db.Exec(
		"UPDATE users SET auth_source = 'saml' WHERE username = ?", "ulric"); err != nil {
		t.Fatalf("seeding a third auth source: %v", err)
	}

	_, err := store.LinkFederatedIdentity("ulric", linkTestIssuer, linkTestSubject, false)
	if err == nil {
		t.Fatal("an account managed by another source was linked")
	}
	if !strings.Contains(err.Error(), "saml") {
		t.Fatalf("error does not name the offending auth_source: %v", err)
	}
	if _, subject, _ := linkedAccountState(t, store, "ulric"); subject != "" {
		t.Fatalf("the refused link still wrote external_subject = %q", subject)
	}
}

// The empty string is neither a subject nor the absence of one, and the schema
// is what keeps it out of the column.
func TestExternalSubjectRejectsTheEmptyString(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("vera", linkTestPassword, "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := store.db.Exec(
		"UPDATE users SET external_subject = '' WHERE username = ?", "vera"); err == nil {
		t.Fatal("the schema accepted an empty external_subject")
	}
}

// An offboarding unlink has to take the account's API tokens with it:
// ValidateToken checks only expiry and the owner's enabled flag, so a token
// minted whilst the account was federated would otherwise outlive the unlink,
// for ever when it was minted without an expiry.
func TestUnlinkFederatedIdentityRevokesTokens(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("wendy", linkTestPassword, "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := store.LinkFederatedIdentity("wendy", linkTestIssuer, linkTestSubject, false); err != nil {
		t.Fatalf("LinkFederatedIdentity: %v", err)
	}
	// No expiry, which is the token that would otherwise live for ever.
	rawToken, _, err := store.CreateToken("wendy", "kept after federation", nil)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	if _, err := store.ValidateToken(rawToken); err != nil {
		t.Fatalf("the token was not valid to begin with: %v", err)
	}

	_, revoked, err := store.UnlinkFederatedIdentity("wendy", false)
	if err != nil {
		t.Fatalf("UnlinkFederatedIdentity: %v", err)
	}
	if revoked != 1 {
		t.Fatalf("revoked = %d, want 1", revoked)
	}
	if _, err := store.ValidateToken(rawToken); err == nil {
		t.Fatal("an API token survived an offboarding unlink")
	}
	tokens, err := store.ListUserTokens("wendy")
	if err != nil {
		t.Fatalf("ListUserTokens: %v", err)
	}
	if len(tokens) != 0 {
		t.Fatalf("%d token(s) left in the table", len(tokens))
	}
}

// -restore-password is the convert-to-local-login branch, where the account
// keeps working: its tokens must survive.
func TestUnlinkFederatedIdentityKeepsTokensWithRestorePassword(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("xavier", linkTestPassword, "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := store.LinkFederatedIdentity("xavier", linkTestIssuer, linkTestSubject, false); err != nil {
		t.Fatalf("LinkFederatedIdentity: %v", err)
	}
	rawToken, _, err := store.CreateToken("xavier", "still wanted", nil)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	_, revoked, err := store.UnlinkFederatedIdentity("xavier", true)
	if err != nil {
		t.Fatalf("UnlinkFederatedIdentity: %v", err)
	}
	if revoked != 0 {
		t.Fatalf("revoked = %d, want 0", revoked)
	}
	if _, err := store.ValidateToken(rawToken); err != nil {
		t.Fatalf("a token was revoked on the restore-password branch: %v", err)
	}
}

// An account with no tokens is the ordinary case, and must not be mistaken for
// a failure by the shared token-deletion path, which treats an empty match set
// as an error when deleting one named token.
func TestUnlinkFederatedIdentityWithNoTokens(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("yolanda", linkTestPassword, "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := store.LinkFederatedIdentity("yolanda", linkTestIssuer, linkTestSubject, false); err != nil {
		t.Fatalf("LinkFederatedIdentity: %v", err)
	}
	_, revoked, err := store.UnlinkFederatedIdentity("yolanda", false)
	if err != nil {
		t.Fatalf("UnlinkFederatedIdentity: %v", err)
	}
	if revoked != 0 {
		t.Fatalf("revoked = %d, want 0", revoked)
	}
}

// Re-linking to the subject the account already holds must change nothing at
// all. SQLite's changes() counts matched rows rather than modified ones, so a
// configuration-management run that reasserts links every pass would otherwise
// log every federated user out every pass.
func TestLinkFederatedIdentityIdempotentRelinkKeepsSessions(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("zach", linkTestPassword, "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := store.LinkFederatedIdentity("zach", linkTestIssuer, linkTestSubject, false); err != nil {
		t.Fatalf("LinkFederatedIdentity: %v", err)
	}

	token, _, err := store.CreateSessionForUser("zach")
	if err != nil {
		t.Fatalf("CreateSessionForUser: %v", err)
	}
	rawToken, _, err := store.CreateToken("zach", "unaffected", nil)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	before := readAccountRow(t, store, "zach")

	// Both spellings of the reassertion: with and without -relink.
	for _, relink := range []bool{false, true} {
		key, err := store.LinkFederatedIdentity("zach", linkTestIssuer, linkTestSubject, relink)
		if err != nil {
			t.Fatalf("LinkFederatedIdentity(relink=%v): %v", relink, err)
		}
		if key != ExternalSubjectKey(linkTestIssuer, linkTestSubject) {
			t.Fatalf("returned key = %q, want the linked key", key)
		}
	}

	if _, err := store.ValidateSessionToken(token); err != nil {
		t.Fatalf("re-linking to the same subject logged the user out: %v", err)
	}
	if _, err := store.ValidateToken(rawToken); err != nil {
		t.Fatalf("re-linking to the same subject disturbed the account's tokens: %v", err)
	}
	assertOnlyLinkColumnsChanged(t, before, readAccountRow(t, store, "zach"), false)
}

// countRowsFor is a small helper for the per-token rows that have no accessor
// of their own.
func countRowsFor(t *testing.T, store *AuthStore, query string, arg any) int {
	t.Helper()

	var count int
	if err := store.db.QueryRow(query, arg).Scan(&count); err != nil {
		t.Fatalf("counting rows: %v", err)
	}
	return count
}

// Revocation is scoped to the account being unlinked, and takes the rows that
// hang off each token with it. Nothing else observes either property, and the
// scoping is the first thing anyone auditing this method will look for.
func TestUnlinkFederatedIdentityRevocationIsScopedAndComplete(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	for _, name := range []string{"leaver", "bystander"} {
		if err := store.CreateUser(name, linkTestPassword, "", "", ""); err != nil {
			t.Fatalf("CreateUser %s: %v", name, err)
		}
	}
	if _, err := store.LinkFederatedIdentity("leaver", linkTestIssuer, linkTestSubject, false); err != nil {
		t.Fatalf("LinkFederatedIdentity: %v", err)
	}

	leaverToken, leaverStored, err := store.CreateToken("leaver", "to be revoked", nil)
	if err != nil {
		t.Fatalf("CreateToken leaver: %v", err)
	}
	bystanderToken, bystanderStored, err := store.CreateToken("bystander", "not involved", nil)
	if err != nil {
		t.Fatalf("CreateToken bystander: %v", err)
	}

	// Give both tokens the full set of rows that hang off a token, so the
	// doc comment's claim that they go with it is observed rather than
	// merely asserted in prose.
	for _, token := range []*StoredToken{leaverStored, bystanderStored} {
		if err := store.SetTokenConnectionScope(token.ID,
			[]ScopedConnection{{ConnectionID: 1, AccessLevel: "read"}}); err != nil {
			t.Fatalf("SetTokenConnectionScope: %v", err)
		}
		if err := store.SetTokenAdminScope(token.ID, []string{"users"}); err != nil {
			t.Fatalf("SetTokenAdminScope: %v", err)
		}
	}
	if err := store.SetConnectionSession(GetTokenHashByRawToken(leaverToken), 1, nil); err != nil {
		t.Fatalf("SetConnectionSession leaver: %v", err)
	}
	if err := store.SetConnectionSession(GetTokenHashByRawToken(bystanderToken), 1, nil); err != nil {
		t.Fatalf("SetConnectionSession bystander: %v", err)
	}

	if _, _, err := store.UnlinkFederatedIdentity("leaver", false); err != nil {
		t.Fatalf("UnlinkFederatedIdentity: %v", err)
	}

	// The leaver's token and everything hanging off it is gone.
	if _, err := store.ValidateToken(leaverToken); err == nil {
		t.Fatal("the unlinked account's token survived")
	}
	for _, table := range []string{"token_connection_scope", "token_admin_scope"} {
		//nolint:gosec // table name is from a static list in this test
		if n := countRowsFor(t, store,
			"SELECT COUNT(*) FROM "+table+" WHERE token_id = ?", leaverStored.ID); n != 0 {
			t.Fatalf("%d %s row(s) left behind for the revoked token", n, table)
		}
	}
	if n := countRowsFor(t, store,
		"SELECT COUNT(*) FROM connection_sessions WHERE token_hash = ?",
		GetTokenHashByRawToken(leaverToken)); n != 0 {
		t.Fatalf("%d connection_sessions row(s) left behind for the revoked token", n)
	}

	// The bystander is untouched: token, scope rows and connection session.
	if _, err := store.ValidateToken(bystanderToken); err != nil {
		t.Fatalf("another account's token was revoked: %v", err)
	}
	for _, table := range []string{"token_connection_scope", "token_admin_scope"} {
		//nolint:gosec // table name is from a static list in this test
		if n := countRowsFor(t, store,
			"SELECT COUNT(*) FROM "+table+" WHERE token_id = ?", bystanderStored.ID); n == 0 {
			t.Fatalf("another account's %s rows were deleted", table)
		}
	}
	if n := countRowsFor(t, store,
		"SELECT COUNT(*) FROM connection_sessions WHERE token_hash = ?",
		GetTokenHashByRawToken(bystanderToken)); n == 0 {
		t.Fatal("another account's connection_sessions row was deleted")
	}
	if after := readAccountRow(t, store, "bystander"); after.authSource != AuthSourceLocal ||
		after.externalSubject != "" {
		t.Fatalf("another account's row was altered: auth_source=%q external_subject=%q",
			after.authSource, after.externalSubject)
	}
}

// A service account that somehow carried this very subject must be refused,
// not reported as an idempotent success: the refusal sits above the
// short-circuit so that a future command converting an account type cannot
// turn the combination into a silent success.
func TestLinkFederatedIdentityRefusesServiceAccountHoldingTheSameSubject(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateServiceAccount("android", "", "", ""); err != nil {
		t.Fatalf("CreateServiceAccount: %v", err)
	}
	// Not reachable through the shipped commands, which is the point: this
	// is the state a future account-type conversion could create.
	if _, err := store.db.Exec(
		"UPDATE users SET auth_source = ?, external_subject = ? WHERE username = ?",
		AuthSourceOIDC, ExternalSubjectKey(linkTestIssuer, linkTestSubject), "android"); err != nil {
		t.Fatalf("seeding the state: %v", err)
	}

	_, err := store.LinkFederatedIdentity("android", linkTestIssuer, linkTestSubject, false)
	if err == nil {
		t.Fatal("a service account already carrying the subject was reported as linked")
	}
	if !strings.Contains(err.Error(), "service account") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// A row deleted between the read and the UPDATE is a missing user, not a
// changed identity, and the message has to say which.
func TestUnlinkFederatedIdentityReportsADeletedAccount(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("ghost", linkTestPassword, "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := store.LinkFederatedIdentity("ghost", linkTestIssuer, linkTestSubject, false); err != nil {
		t.Fatalf("LinkFederatedIdentity: %v", err)
	}
	if err := store.DeleteUser("ghost"); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	_, _, err := store.UnlinkFederatedIdentity("ghost", false)
	if err == nil {
		t.Fatal("unlinking a deleted account succeeded")
	}
	if !strings.Contains(err.Error(), "user not found") {
		t.Fatalf("error = %v, want it to report a missing user", err)
	}
}

// The refusal explanation is exercised directly, because the interleaving it
// exists for, a row deleted or relinked between the read and the UPDATE,
// cannot be produced from a single-process test.
func TestExplainUnlinkRefusalLocked(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("present", linkTestPassword, "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if err := store.explainUnlinkRefusalLocked("missing"); err == nil ||
		!strings.Contains(err.Error(), "user not found") {
		t.Fatalf("a deleted account gave %v, want a missing-user error", err)
	}
	if err := store.explainUnlinkRefusalLocked("present"); err == nil ||
		!strings.Contains(err.Error(), "changed while the unlink was being applied") {
		t.Fatalf("a live account gave %v, want a changed-identity error", err)
	}
}
