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
	"strings"
	"testing"
)

// newFederatedTestUser creates a local account and links it to an identity
// provider, which is the state the password-write guard exists to protect.
func newFederatedTestUser(t *testing.T, store *AuthStore, username string) {
	t.Helper()

	if err := store.CreateUser(username, "Sup3r-Str0ng-Pass!", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := store.LinkFederatedIdentity(username, "https://idp.example.com", "subject-1", false); err != nil {
		t.Fatalf("LinkFederatedIdentity: %v", err)
	}
}

// storedPasswordHash reads the hash directly, so a test can prove the guard
// left it untouched rather than inferring it from a login attempt, which a
// federated account refuses either way.
func storedPasswordHash(t *testing.T, store *AuthStore, username string) string {
	t.Helper()

	var hash string
	if err := store.db.QueryRow("SELECT password_hash FROM users WHERE username = ?", username).Scan(&hash); err != nil {
		t.Fatalf("reading password hash: %v", err)
	}
	return hash
}

// TestUpdateUserRefusesPasswordOnFederatedAccount covers the revival path:
// a password written onto a federated account lies dormant until an unlink
// with -restore-password puts auth_source back to local, at which point it
// becomes live. Refusing the write is what keeps the restored password the
// one the account last had as a local account.
func TestUpdateUserRefusesPasswordOnFederatedAccount(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	newFederatedTestUser(t, store, "federated")
	before := storedPasswordHash(t, store, "federated")

	err := store.UpdateUser("federated", "An0ther-Str0ng-Pass!", "note", "Fed User", "fed@example.com")
	if err == nil {
		t.Fatal("expected a password write to a federated account to be refused")
	}
	if !strings.Contains(err.Error(), "identity is managed by oidc") {
		t.Errorf("expected the error to name the managing identity source, got: %v", err)
	}
	if after := storedPasswordHash(t, store, "federated"); after != before {
		t.Error("the password hash was changed despite the write being refused")
	}
}

// TestUpdateUserAtomicRefusesPasswordOnFederatedAccount covers the same
// invariant on the transactional path the RBAC user handler uses, and
// additionally checks the transaction left nothing else behind.
func TestUpdateUserAtomicRefusesPasswordOnFederatedAccount(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	newFederatedTestUser(t, store, "federated-atomic")
	before := storedPasswordHash(t, store, "federated-atomic")

	password := "An0ther-Str0ng-Pass!"
	annotation := "should not be written"
	err := store.UpdateUserAtomic("federated-atomic", UserUpdate{
		Password:   &password,
		Annotation: &annotation,
	})
	if err == nil {
		t.Fatal("expected a password write to a federated account to be refused")
	}
	if !strings.Contains(err.Error(), "identity is managed by oidc") {
		t.Errorf("expected the error to name the managing identity source, got: %v", err)
	}
	if after := storedPasswordHash(t, store, "federated-atomic"); after != before {
		t.Error("the password hash was changed despite the write being refused")
	}

	user, getErr := store.GetUser("federated-atomic")
	if getErr != nil {
		t.Fatalf("GetUser: %v", getErr)
	}
	if user.Annotation == annotation {
		t.Error("the refused transaction still wrote the annotation")
	}
}

// TestUpdateUserAllowsPasswordOnLocalAccount checks the guard did not shut
// the ordinary path, which is the recovery route an operator needs in order
// to turn local login back on.
func TestUpdateUserAllowsPasswordOnLocalAccount(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("localuser", "Sup3r-Str0ng-Pass!", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	before := storedPasswordHash(t, store, "localuser")

	if err := store.UpdateUser("localuser", "An0ther-Str0ng-Pass!", "", "", ""); err != nil {
		t.Fatalf("UpdateUser on a local account: %v", err)
	}
	if after := storedPasswordHash(t, store, "localuser"); after == before {
		t.Error("expected the password hash to change")
	}

	password := "Third-Str0ng-Pass!"
	if err := store.UpdateUserAtomic("localuser", UserUpdate{Password: &password}); err != nil {
		t.Fatalf("UpdateUserAtomic on a local account: %v", err)
	}
}

// TestUpdateUserPasswordForMissingUserIsNotAnError preserves the existing
// behavior of both update paths, where a username that matches no row
// simply updates nothing: the guard must not turn that into a failure.
func TestUpdateUserPasswordForMissingUserIsNotAnError(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.UpdateUser("nobody", "An0ther-Str0ng-Pass!", "", "", ""); err != nil {
		t.Errorf("UpdateUser for a missing user: %v", err)
	}

	password := "An0ther-Str0ng-Pass!"
	err := store.UpdateUserAtomic("nobody", UserUpdate{Password: &password})
	if err != nil {
		t.Errorf("UpdateUserAtomic for a missing user: %v", err)
	}
}

// TestPasswordWriteGuardReportsQueryFailure covers the path where the
// auth_source lookup itself fails. The guard must refuse the write in
// that case rather than falling through to it, since a password written
// on the strength of a failed check is exactly what the guard exists to
// prevent.
func TestPasswordWriteGuardReportsQueryFailure(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("doomed", "Sup3r-Str0ng-Pass!", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := store.db.Exec("DROP TABLE users"); err != nil {
		t.Fatalf("dropping the users table: %v", err)
	}

	err := store.UpdateUser("doomed", "An0ther-Str0ng-Pass!", "", "", "")
	if err == nil {
		t.Fatal("expected the password write to fail when the lookup does")
	}
	if !strings.Contains(err.Error(), "failed to check authentication source") {
		t.Errorf("expected the lookup failure to be reported, got: %v", err)
	}
}
