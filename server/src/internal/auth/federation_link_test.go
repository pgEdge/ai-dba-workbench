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

func TestUnlinkFederatedIdentityRestoresLocalLogin(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("leo", linkTestPassword, "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := store.LinkFederatedIdentity("leo", linkTestIssuer, linkTestSubject, false); err != nil {
		t.Fatalf("LinkFederatedIdentity: %v", err)
	}

	key, err := store.UnlinkFederatedIdentity("leo")
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
	// This is the documented consequence of restoring auth_source, asserted
	// so that nobody has to guess whether the old credential is live.
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
	if _, err := store.UnlinkFederatedIdentity("mallory"); err != nil {
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
			_, err := store.UnlinkFederatedIdentity(tt.username)
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
