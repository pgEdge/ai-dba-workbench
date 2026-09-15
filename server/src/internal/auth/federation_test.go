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
	"testing"
	"time"
)

// sessionFarFuture is used by malformed-session-entry tests so the entries
// never look expired regardless of when the test runs.
var sessionFarFuture = time.Now().Add(24 * time.Hour)

func TestCreateSessionForUserEnforcesSessionCap(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("capped", "Sup3r-Str0ng-Pass!", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	tokens := make([]string, 0, maxSessionsPerUser+1)
	for i := 0; i < maxSessionsPerUser+1; i++ {
		token, _, err := store.CreateSessionForUser("capped")
		if err != nil {
			t.Fatalf("CreateSessionForUser %d: %v", i, err)
		}
		tokens = append(tokens, token)
	}

	live := 0
	for _, token := range tokens {
		if _, err := store.ValidateSessionToken(token); err == nil {
			live++
		}
	}
	if live != maxSessionsPerUser {
		t.Fatalf("live sessions = %d, want %d", live, maxSessionsPerUser)
	}
}

func TestCreateSessionForUserUpdatesLastLogin(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("stamped", "Sup3r-Str0ng-Pass!", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	// Give the account a nonzero failed_attempts count first, so the
	// assertion below proves CreateSessionForUser actually resets it
	// rather than merely observing a column that was already zero.
	if _, err := store.db.Exec(
		"UPDATE users SET failed_attempts = 3 WHERE username = ?", "stamped"); err != nil {
		t.Fatalf("seed failed_attempts: %v", err)
	}
	if _, _, err := store.CreateSessionForUser("stamped"); err != nil {
		t.Fatalf("CreateSessionForUser: %v", err)
	}

	var lastLogin sql.NullTime
	var failedAttempts int
	if err := store.db.QueryRow(
		"SELECT last_login, failed_attempts FROM users WHERE username = ?", "stamped").
		Scan(&lastLogin, &failedAttempts); err != nil {
		t.Fatalf("last_login: %v", err)
	}
	if !lastLogin.Valid {
		t.Fatal("last_login was not set")
	}
	if failedAttempts != 0 {
		t.Fatalf("failed_attempts = %d, want 0", failedAttempts)
	}
}

func TestCreateSessionForUserRejectsServiceAccount(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateServiceAccount("svc", "", "", ""); err != nil {
		t.Fatalf("CreateServiceAccount: %v", err)
	}
	if _, _, err := store.CreateSessionForUser("svc"); err == nil {
		t.Fatal("expected a service account to be refused a session")
	}
}

func TestCreateSessionForUserRejectsUnknownUser(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if _, _, err := store.CreateSessionForUser("nobody"); err == nil {
		t.Fatal("expected an error for an unknown user")
	}
}

func TestCreateSessionForUserLookupError(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("broken", "Sup3r-Str0ng-Pass!", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Close the underlying database directly (bypassing AuthStore.Close, which
	// also stops the session-cleanup goroutine) so the lookup query in
	// CreateSessionForUser fails with something other than sql.ErrNoRows.
	if err := store.db.Close(); err != nil {
		t.Fatalf("db.Close: %v", err)
	}

	if _, _, err := store.CreateSessionForUser("broken"); err == nil {
		t.Fatal("expected a database error to surface")
	}
}

// TestCreateSessionForUserForLockedSkipsMalformedSessionEntries exercises the
// defensive type-assertion branches in createSessionForUserLocked's eviction
// scan. Nothing in the public API can put a malformed entry into s.sessions,
// so this test does it directly, in-package, to prove those branches are
// skipped safely rather than panicking.
func TestCreateSessionForUserForLockedSkipsMalformedSessionEntries(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("malformed", "Sup3r-Str0ng-Pass!", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// A value that isn't a *SessionInfo.
	store.sessions.Store("not-a-session-info", "garbage")
	// A key that isn't a string.
	store.sessions.Store(42, &SessionInfo{Username: "malformed", ExpiresAt: sessionFarFuture})

	token, _, err := store.CreateSessionForUser("malformed")
	if err != nil {
		t.Fatalf("CreateSessionForUser: %v", err)
	}

	gotUsername, err := store.ValidateSessionToken(token)
	if err != nil {
		t.Fatalf("ValidateSessionToken: %v", err)
	}
	if gotUsername != "malformed" {
		t.Fatalf("ValidateSessionToken username = %q, want %q", gotUsername, "malformed")
	}

	if _, ok := store.sessions.Load("not-a-session-info"); !ok {
		t.Fatal("malformed-value entry was evicted, want it left alone")
	}
	if _, ok := store.sessions.Load(42); !ok {
		t.Fatal("malformed-key entry was evicted, want it left alone")
	}
}

func TestCreateSessionForUserRejectsDisabledUser(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("off", "Sup3r-Str0ng-Pass!", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.DisableUser("off"); err != nil {
		t.Fatalf("DisableUser: %v", err)
	}
	if _, _, err := store.CreateSessionForUser("off"); err == nil {
		t.Fatal("expected a disabled account to be refused a session")
	}
}

// =============================================================================
// Federated Identity Resolution Tests
// =============================================================================

// testIdentity is the identity the federation tests start from; each test
// copies it and changes only the field under test.
func testIdentity() FederatedIdentity {
	return FederatedIdentity{
		Issuer:   "https://idp.example.com",
		Subject:  "subject-1",
		Username: "jane.doe@example.com",
	}
}

// assertInGroup asserts direct-or-inherited membership of a group.
func assertInGroup(t *testing.T, store *AuthStore, userID, groupID int64, want bool) {
	t.Helper()

	got, err := store.IsUserInGroup(userID, groupID)
	if err != nil {
		t.Fatalf("IsUserInGroup(%d, %d): %v", userID, groupID, err)
	}
	if got != want {
		t.Fatalf("IsUserInGroup(%d, %d) = %v, want %v", userID, groupID, got, want)
	}
}

// resolveForTest provisions the standard test identity and returns the user.
func resolveForTest(t *testing.T, store *AuthStore) *StoredUser {
	t.Helper()

	user, err := store.ResolveFederatedUser(testIdentity(), FederationOptions{ProvisionUsers: true})
	if err != nil {
		t.Fatalf("ResolveFederatedUser: %v", err)
	}
	return user
}

func TestExternalSubjectKeyCombinesIssuerAndSubject(t *testing.T) {
	first := ExternalSubjectKey("https://idp-a.example.com", "shared-subject")
	second := ExternalSubjectKey("https://idp-b.example.com", "shared-subject")
	if first == second {
		t.Fatalf("two issuers with the same subject collided on key %q", first)
	}
	if want := "https://idp-a.example.com|shared-subject"; first != want {
		t.Fatalf("key = %q, want %q", first, want)
	}
}

func TestResolveFederatedUserMatchesOnSubjectNotUsername(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	identity := testIdentity()
	identity.Username = "old.name@example.com"
	opts := FederationOptions{ProvisionUsers: true}

	first, err := store.ResolveFederatedUser(identity, opts)
	if err != nil {
		t.Fatalf("first resolve: %v", err)
	}

	// The person changed their name at the provider; the subject did not move.
	identity.Username = "new.name@example.com"
	second, err := store.ResolveFederatedUser(identity, opts)
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("resolved a different user (%d) after a username change (%d)",
			second.ID, first.ID)
	}
	if second.Username != "old.name@example.com" {
		t.Fatalf("username = %q, want the account matched on subject to be reused",
			second.Username)
	}
}

// TestResolveFederatedUserIgnoresAMatchingLocalUsername is the direct test of
// the authentication-bypass this design exists to prevent: a provider
// asserting the username of a privileged local account must not reach it.
func TestResolveFederatedUserIgnoresAMatchingLocalUsername(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("admin", "Sup3r-Str0ng-Pass!", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.SetUserSuperuser("admin", true); err != nil {
		t.Fatalf("SetUserSuperuser: %v", err)
	}

	identity := testIdentity()
	identity.Username = "admin"
	if _, err := store.ResolveFederatedUser(identity, FederationOptions{ProvisionUsers: true}); err == nil {
		t.Fatal("expected the federated identity to be refused the local admin account")
	}

	local, err := store.GetUser("admin")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if local.AuthSource != AuthSourceLocal || local.ExternalSubject != "" {
		t.Fatalf("local admin was altered: auth_source=%q external_subject=%q",
			local.AuthSource, local.ExternalSubject)
	}
}

func TestResolveFederatedUserSeparatesIssuersSharingASubject(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	opts := FederationOptions{ProvisionUsers: true}
	first, err := store.ResolveFederatedUser(FederatedIdentity{
		Issuer: "https://idp-a.example.com", Subject: "shared-subject",
		Username: "a.user@example.com"}, opts)
	if err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	second, err := store.ResolveFederatedUser(FederatedIdentity{
		Issuer: "https://idp-b.example.com", Subject: "shared-subject",
		Username: "b.user@example.com"}, opts)
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if first.ID == second.ID {
		t.Fatal("two providers issuing the same subject resolved to one account")
	}
}

func TestResolveFederatedUserRefusesUnknownSubjectWithoutProvisioning(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	_, err := store.ResolveFederatedUser(testIdentity(), FederationOptions{ProvisionUsers: false})
	if err == nil {
		t.Fatal("expected an unknown subject to be refused when provisioning is off")
	}
	users, err := store.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 0 {
		t.Fatalf("refusal created %d user(s)", len(users))
	}
}

func TestResolveFederatedUserRefusesToHijackALocalAccount(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("jane.doe@example.com", "Sup3r-Str0ng-Pass!", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	_, err := store.ResolveFederatedUser(testIdentity(), FederationOptions{ProvisionUsers: true})
	if err == nil {
		t.Fatal("expected provisioning to refuse to take over an existing local account")
	}
}

// TestResolveFederatedUserRefusesALocalAccountDifferingOnlyInCase locks in the
// case-insensitive collision check. The users table is case-sensitive, so
// "Alice" and "alice" would otherwise coexist as two accounts an administrator
// has to remember to revoke separately.
func TestResolveFederatedUserRefusesALocalAccountDifferingOnlyInCase(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("alice", "Sup3r-Str0ng-Pass!", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	identity := testIdentity()
	identity.Username = "Alice"
	if _, err := store.ResolveFederatedUser(identity, FederationOptions{ProvisionUsers: true}); err == nil {
		t.Fatal("expected a case-different local account to block provisioning")
	}

	users, err := store.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("user count = %d, want the single local account", len(users))
	}
}

func TestResolveFederatedUserRefusesDisabledAccount(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	user := resolveForTest(t, store)
	if err := store.DisableUser(user.Username); err != nil {
		t.Fatalf("DisableUser: %v", err)
	}

	if _, err := store.ResolveFederatedUser(testIdentity(), FederationOptions{ProvisionUsers: true}); err == nil {
		t.Fatal("expected a disabled federated account to be refused")
	}
}

func TestResolveFederatedUserRequiresIssuerAndSubject(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	cases := []struct {
		name     string
		identity FederatedIdentity
	}{
		{"no issuer", FederatedIdentity{Subject: "subject-1", Username: "jane.doe@example.com"}},
		{"no subject", FederatedIdentity{Issuer: "https://idp.example.com", Username: "jane.doe@example.com"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := store.ResolveFederatedUser(tc.identity,
				FederationOptions{ProvisionUsers: true}); err == nil {
				t.Fatal("expected an incomplete identity to be refused")
			}
		})
	}
}

func TestResolveFederatedUserRefusesAnUnusableUsername(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	identity := testIdentity()
	identity.Username = ""
	if _, err := store.ResolveFederatedUser(identity,
		FederationOptions{ProvisionUsers: true}); err == nil {
		t.Fatal("expected an empty username to be refused")
	}
}

func TestResolveFederatedUserStoresTheProviderProfile(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	identity := testIdentity()
	identity.DisplayName = "Jane Doe"
	identity.Email = "jane.doe@example.com"

	user, err := store.ResolveFederatedUser(identity, FederationOptions{ProvisionUsers: true})
	if err != nil {
		t.Fatalf("ResolveFederatedUser: %v", err)
	}
	if user.DisplayName != "Jane Doe" || user.Email != "jane.doe@example.com" {
		t.Fatalf("profile = %q/%q, want the asserted values", user.DisplayName, user.Email)
	}
	if want := ExternalSubjectKey(identity.Issuer, identity.Subject); user.ExternalSubject != want {
		t.Fatalf("external_subject = %q, want %q", user.ExternalSubject, want)
	}
	if !user.Enabled {
		t.Fatal("provisioned account is not enabled")
	}
	if user.IsSuperuser {
		t.Fatal("provisioned account is a superuser")
	}
}

func TestProvisionedUserCannotLogInWithAPassword(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	user := resolveForTest(t, store)
	if user.AuthSource != AuthSourceOIDC {
		t.Fatalf("auth_source = %q, want %q", user.AuthSource, AuthSourceOIDC)
	}
	if user.PasswordHash == "" {
		t.Fatal("provisioned account has an empty password hash")
	}

	for _, password := range []string{"", " ", user.Username, user.ExternalSubject} {
		if _, _, err := store.AuthenticateUser(user.Username, password); err == nil {
			t.Fatalf("password %q authenticated a provisioned account", password)
		}
	}
}

// TestProvisionedPasswordHashesDiffer proves the unusable hash is derived from
// fresh random bytes each time rather than a shared constant.
func TestProvisionedPasswordHashesDiffer(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	first := resolveForTest(t, store)
	identity := testIdentity()
	identity.Subject = "subject-2"
	identity.Username = "john.doe@example.com"
	second, err := store.ResolveFederatedUser(identity, FederationOptions{ProvisionUsers: true})
	if err != nil {
		t.Fatalf("ResolveFederatedUser: %v", err)
	}
	if first.PasswordHash == second.PasswordHash {
		t.Fatal("two provisioned accounts share a password hash")
	}
}

func TestResolveFederatedUserRefusesAServiceAccountSubject(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateServiceAccount("svc-federated", "", "", ""); err != nil {
		t.Fatalf("CreateServiceAccount: %v", err)
	}
	key := ExternalSubjectKey("https://idp.example.com", "subject-1")
	if _, err := store.db.Exec(
		"UPDATE users SET auth_source = ?, external_subject = ? WHERE username = ?",
		AuthSourceOIDC, key, "svc-federated"); err != nil {
		t.Fatalf("seed service account: %v", err)
	}

	if _, err := store.ResolveFederatedUser(testIdentity(),
		FederationOptions{ProvisionUsers: true}); err == nil {
		t.Fatal("expected a service account to be refused an interactive federated login")
	}
}

// TestResolveFederatedUserRefusesANonFederatedSubjectRow covers the defensive
// auth_source check on a matched row: a local account that somehow carries an
// external subject must not be adopted by a federated login.
func TestResolveFederatedUserRefusesANonFederatedSubjectRow(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("local.user", "Sup3r-Str0ng-Pass!", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	key := ExternalSubjectKey("https://idp.example.com", "subject-1")
	if _, err := store.db.Exec(
		"UPDATE users SET external_subject = ? WHERE username = ?", key, "local.user"); err != nil {
		t.Fatalf("seed external subject: %v", err)
	}

	if _, err := store.ResolveFederatedUser(testIdentity(),
		FederationOptions{ProvisionUsers: true}); err == nil {
		t.Fatal("expected a local account carrying an external subject to be refused")
	}
}

func TestResolveFederatedUserSurfacesDatabaseErrors(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.db.Close(); err != nil {
		t.Fatalf("db.Close: %v", err)
	}
	if _, err := store.ResolveFederatedUser(testIdentity(),
		FederationOptions{ProvisionUsers: true}); err == nil {
		t.Fatal("expected a database error to surface")
	}
}

// =============================================================================
// Federated Group Reconciliation Tests
// =============================================================================

func TestReconcileFederatedGroupsOnlyTouchesMappedGroups(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	mappedID, err := store.CreateGroup("workbench-admins", "")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	localID, err := store.CreateGroup("locally-managed", "")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	user := resolveForTest(t, store)
	if err := store.AddUserToGroup(localID, user.ID); err != nil {
		t.Fatalf("AddUserToGroup: %v", err)
	}

	opts := FederationOptions{GroupMap: map[string]string{
		"idp-database-admins": "workbench-admins",
	}}

	// The provider asserts the mapped group: the user joins it.
	if err := store.ReconcileFederatedGroups(user.ID, FederatedIdentity{
		Groups: []string{"idp-database-admins"}}, opts); err != nil {
		t.Fatalf("reconcile (present): %v", err)
	}
	assertInGroup(t, store, user.ID, mappedID, true)
	assertInGroup(t, store, user.ID, localID, true)

	// The provider stops asserting it: the user leaves the mapped group but
	// keeps the locally managed one.
	if err := store.ReconcileFederatedGroups(user.ID, FederatedIdentity{
		Groups: nil}, opts); err != nil {
		t.Fatalf("reconcile (absent): %v", err)
	}
	assertInGroup(t, store, user.ID, mappedID, false)
	assertInGroup(t, store, user.ID, localID, true)
}

// TestReconcileFederatedGroupsIsIdempotent proves a repeated assertion neither
// fails on the membership uniqueness constraint nor duplicates the row.
func TestReconcileFederatedGroupsIsIdempotent(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	mappedID, err := store.CreateGroup("workbench-admins", "")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	user := resolveForTest(t, store)
	opts := FederationOptions{GroupMap: map[string]string{"idp-admins": "workbench-admins"}}
	identity := FederatedIdentity{Groups: []string{"idp-admins"}}

	for i := 0; i < 3; i++ {
		if err := store.ReconcileFederatedGroups(user.ID, identity, opts); err != nil {
			t.Fatalf("reconcile %d: %v", i, err)
		}
	}
	assertInGroup(t, store, user.ID, mappedID, true)

	var rows int
	if err := store.db.QueryRow(
		"SELECT COUNT(*) FROM group_memberships WHERE parent_group_id = ? AND member_user_id = ?",
		mappedID, user.ID).Scan(&rows); err != nil {
		t.Fatalf("count memberships: %v", err)
	}
	if rows != 1 {
		t.Fatalf("membership rows = %d, want 1", rows)
	}

	// Removing twice is equally harmless.
	for i := 0; i < 2; i++ {
		if err := store.ReconcileFederatedGroups(user.ID, FederatedIdentity{}, opts); err != nil {
			t.Fatalf("reconcile (absent) %d: %v", i, err)
		}
	}
	assertInGroup(t, store, user.ID, mappedID, false)
}

func TestReconcileFederatedGroupsGrantsAndRevokesSuperuser(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	user := resolveForTest(t, store)
	opts := FederationOptions{SuperuserGroup: "idp-workbench-superusers"}

	if err := store.ReconcileFederatedGroups(user.ID, FederatedIdentity{
		Groups: []string{"idp-workbench-superusers"}}, opts); err != nil {
		t.Fatalf("reconcile (granting): %v", err)
	}
	assertSuperuser(t, store, user.Username, true)

	if err := store.ReconcileFederatedGroups(user.ID, FederatedIdentity{
		Groups: []string{"idp-other-group"}}, opts); err != nil {
		t.Fatalf("reconcile (revoking): %v", err)
	}
	assertSuperuser(t, store, user.Username, false)
}

// TestReconcileFederatedGroupsNeverInfersSuperuser proves no claim other than
// SuperuserGroup grants the flag: a mapped Workbench group whose name looks
// administrative confers no superuser of its own.
func TestReconcileFederatedGroupsNeverInfersSuperuser(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if _, err := store.CreateGroup("workbench-superusers", ""); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	user := resolveForTest(t, store)

	if err := store.ReconcileFederatedGroups(user.ID, FederatedIdentity{
		Groups: []string{"idp-superusers", "superuser", "admin"}},
		FederationOptions{GroupMap: map[string]string{
			"idp-superusers": "workbench-superusers",
		}}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	assertSuperuser(t, store, user.Username, false)
}

// TestReconcileFederatedGroupsLeavesSuperuserAloneWhenUnconfigured covers the
// operator who keeps superuser under local control: with no SuperuserGroup
// configured, a locally granted flag survives reconciliation.
func TestReconcileFederatedGroupsLeavesSuperuserAloneWhenUnconfigured(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	user := resolveForTest(t, store)
	if err := store.SetUserSuperuser(user.Username, true); err != nil {
		t.Fatalf("SetUserSuperuser: %v", err)
	}

	if err := store.ReconcileFederatedGroups(user.ID, FederatedIdentity{},
		FederationOptions{}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	assertSuperuser(t, store, user.Username, true)
}

func TestReconcileFederatedGroupsIgnoresUnmappedProviderGroups(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	// A Workbench group whose name matches a provider group exactly: name
	// equality must not be a route into it.
	sameNameID, err := store.CreateGroup("idp-database-admins", "")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	user := resolveForTest(t, store)

	before, err := store.ListGroups()
	if err != nil {
		t.Fatalf("ListGroups: %v", err)
	}

	if err := store.ReconcileFederatedGroups(user.ID, FederatedIdentity{
		Groups: []string{"idp-database-admins", "never-heard-of-it"}},
		FederationOptions{}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	after, err := store.ListGroups()
	if err != nil {
		t.Fatalf("ListGroups: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("group count = %d, want %d: reconciliation created a group",
			len(after), len(before))
	}
	assertInGroup(t, store, user.ID, sameNameID, false)
}

func TestReconcileFederatedGroupsToleratesAMissingTargetGroup(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	user := resolveForTest(t, store)

	if err := store.ReconcileFederatedGroups(user.ID, FederatedIdentity{
		Groups: []string{"idp-database-admins"}},
		FederationOptions{GroupMap: map[string]string{
			"idp-database-admins": "typo-in-the-operator-config",
		}}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	groups, err := store.ListGroups()
	if err != nil {
		t.Fatalf("ListGroups: %v", err)
	}
	if len(groups) != 0 {
		t.Fatalf("group count = %d, want 0: a missing target was created", len(groups))
	}
}

// TestReconcileFederatedGroupsUnionsDuplicateMappings covers two provider
// groups mapped onto one Workbench group: asserting either must grant it,
// whatever order the map iterates in.
func TestReconcileFederatedGroupsUnionsDuplicateMappings(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	mappedID, err := store.CreateGroup("workbench-admins", "")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	user := resolveForTest(t, store)
	opts := FederationOptions{GroupMap: map[string]string{
		"idp-dbas":    "workbench-admins",
		"idp-oncall":  "workbench-admins",
		"idp-ignored": "",
	}}

	if err := store.ReconcileFederatedGroups(user.ID, FederatedIdentity{
		Groups: []string{"idp-oncall", "idp-ignored"}}, opts); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	assertInGroup(t, store, user.ID, mappedID, true)

	if err := store.ReconcileFederatedGroups(user.ID, FederatedIdentity{}, opts); err != nil {
		t.Fatalf("reconcile (absent): %v", err)
	}
	assertInGroup(t, store, user.ID, mappedID, false)
}

func TestReconcileFederatedGroupsRefusesNonFederatedAccounts(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	groupID, err := store.CreateGroup("workbench-admins", "")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := store.CreateUser("local.admin", "Sup3r-Str0ng-Pass!", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, err := store.GetUserID("local.admin")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}

	if err := store.ReconcileFederatedGroups(userID, FederatedIdentity{
		Groups: []string{"idp-admins", "idp-supers"}},
		FederationOptions{
			GroupMap:       map[string]string{"idp-admins": "workbench-admins"},
			SuperuserGroup: "idp-supers",
		}); err == nil {
		t.Fatal("expected reconciliation to refuse a local account")
	}
	assertInGroup(t, store, userID, groupID, false)
	assertSuperuser(t, store, "local.admin", false)
}

func TestReconcileFederatedGroupsRejectsUnknownUser(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.ReconcileFederatedGroups(4242, FederatedIdentity{},
		FederationOptions{}); err == nil {
		t.Fatal("expected an unknown user ID to be refused")
	}
}

func TestReconcileFederatedGroupsSurfacesDatabaseErrors(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	user := resolveForTest(t, store)
	if err := store.db.Close(); err != nil {
		t.Fatalf("db.Close: %v", err)
	}
	if err := store.ReconcileFederatedGroups(user.ID, FederatedIdentity{},
		FederationOptions{}); err == nil {
		t.Fatal("expected a database error to surface")
	}
}

// TestProvisioningSurfacesAnInsertFailure drives the INSERT error path with a
// trigger that refuses the write, since nothing a caller can supply reaches it
// once the username collision check has passed.
func TestProvisioningSurfacesAnInsertFailure(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if _, err := store.db.Exec(`CREATE TRIGGER refuse_insert BEFORE INSERT ON users
        BEGIN SELECT RAISE(ABORT, 'refused'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	if _, err := store.ResolveFederatedUser(testIdentity(),
		FederationOptions{ProvisionUsers: true}); err == nil {
		t.Fatal("expected the insert failure to surface")
	}
}

// TestProvisioningSurfacesAReadBackFailure covers the read-back of the row
// just inserted, using a trigger that removes it again.
func TestProvisioningSurfacesAReadBackFailure(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if _, err := store.db.Exec(`CREATE TRIGGER vanish AFTER INSERT ON users
        BEGIN DELETE FROM users WHERE id = NEW.id; END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	if _, err := store.ResolveFederatedUser(testIdentity(),
		FederationOptions{ProvisionUsers: true}); err == nil {
		t.Fatal("expected the read-back failure to surface")
	}
}

// TestProvisioningSurfacesAHashingFailure covers the unusable-password hash
// failing, forced with a bcrypt cost outside the permitted range.
func TestProvisioningSurfacesAHashingFailure(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	store.bcryptCost = 99

	if _, err := store.ResolveFederatedUser(testIdentity(),
		FederationOptions{ProvisionUsers: true}); err == nil {
		t.Fatal("expected the hashing failure to surface")
	}
}

// TestUsernameTakenSurfacesDatabaseErrors exercises the helper directly,
// because a closed database fails the subject lookup in ResolveFederatedUser
// long before the username scan is reached.
func TestUsernameTakenSurfacesDatabaseErrors(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.db.Close(); err != nil {
		t.Fatalf("db.Close: %v", err)
	}
	if _, err := store.usernameTakenLocked("jane.doe@example.com"); err == nil {
		t.Fatal("expected a database error to surface")
	}
}

func TestReconcileFederatedGroupsSurfacesGroupLookupErrors(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	user := resolveForTest(t, store)
	if _, err := store.db.Exec("DROP TABLE group_memberships"); err != nil {
		t.Fatalf("drop group_memberships: %v", err)
	}
	if _, err := store.db.Exec("DROP TABLE user_groups"); err != nil {
		t.Fatalf("drop user_groups: %v", err)
	}

	if err := store.ReconcileFederatedGroups(user.ID, FederatedIdentity{},
		FederationOptions{GroupMap: map[string]string{"idp-admins": "workbench-admins"}}); err == nil {
		t.Fatal("expected the group lookup failure to surface")
	}
}

// TestReconcileFederatedGroupsSurfacesMembershipWriteErrors covers both
// membership writes, using triggers that refuse them.
func TestReconcileFederatedGroupsSurfacesMembershipWriteErrors(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if _, err := store.CreateGroup("workbench-admins", ""); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	user := resolveForTest(t, store)
	opts := FederationOptions{GroupMap: map[string]string{"idp-admins": "workbench-admins"}}

	// The DELETE first: a BEFORE DELETE trigger only fires when there is a
	// row to remove, so seed the membership before arming it.
	if _, err := store.db.Exec(
		"INSERT INTO group_memberships (parent_group_id, member_user_id) SELECT id, ? FROM user_groups",
		user.ID); err != nil {
		t.Fatalf("seed membership: %v", err)
	}
	if _, err := store.db.Exec(`CREATE TRIGGER refuse_membership_delete
        BEFORE DELETE ON group_memberships
        BEGIN SELECT RAISE(ABORT, 'refused'); END`); err != nil {
		t.Fatalf("create delete trigger: %v", err)
	}
	if err := store.ReconcileFederatedGroups(user.ID, FederatedIdentity{}, opts); err == nil {
		t.Fatal("expected the membership delete failure to surface")
	}

	// Now the INSERT, with the seeded row cleared again so the write is a
	// genuine insert rather than a no-op.
	if _, err := store.db.Exec("DROP TRIGGER refuse_membership_delete"); err != nil {
		t.Fatalf("drop delete trigger: %v", err)
	}
	if _, err := store.db.Exec(
		"DELETE FROM group_memberships WHERE member_user_id = ?", user.ID); err != nil {
		t.Fatalf("clear membership: %v", err)
	}
	if _, err := store.db.Exec(`CREATE TRIGGER refuse_membership_insert
        BEFORE INSERT ON group_memberships
        BEGIN SELECT RAISE(ABORT, 'refused'); END`); err != nil {
		t.Fatalf("create insert trigger: %v", err)
	}
	if err := store.ReconcileFederatedGroups(user.ID, FederatedIdentity{
		Groups: []string{"idp-admins"}}, opts); err == nil {
		t.Fatal("expected the membership insert failure to surface")
	}
}

func TestReconcileFederatedGroupsSurfacesSuperuserWriteErrors(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	user := resolveForTest(t, store)
	if _, err := store.db.Exec(`CREATE TRIGGER refuse_user_update BEFORE UPDATE ON users
        BEGIN SELECT RAISE(ABORT, 'refused'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	if err := store.ReconcileFederatedGroups(user.ID, FederatedIdentity{
		Groups: []string{"idp-supers"}},
		FederationOptions{SuperuserGroup: "idp-supers"}); err == nil {
		t.Fatal("expected the superuser update failure to surface")
	}
}

// assertSuperuser reads the flag back through the public API.
func assertSuperuser(t *testing.T, store *AuthStore, username string, want bool) {
	t.Helper()

	user, err := store.GetUser(username)
	if err != nil {
		t.Fatalf("GetUser(%s): %v", username, err)
	}
	if user == nil {
		t.Fatalf("GetUser(%s) returned no user", username)
	}
	if user.IsSuperuser != want {
		t.Fatalf("IsSuperuser = %v, want %v", user.IsSuperuser, want)
	}
}
