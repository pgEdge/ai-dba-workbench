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
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
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
// guarded write itself fails. The guard is a condition on the UPDATE
// rather than a lookup that runs beforehand, so a database failure is
// reported from the write and nothing falls through to an unguarded one.
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
		t.Fatal("expected the password write to fail when the database does")
	}
	if !strings.Contains(err.Error(), "failed to update password") {
		t.Errorf("expected the write failure to be reported, got: %v", err)
	}
}

// TestPasswordWriteGuardIsAConditionOnTheUpdate is the regression test
// for the check-then-write race. The account is local when the hash is
// computed and is linked to an identity provider before the UPDATE runs,
// which is what the CLI in another process can do during the bcrypt
// computation; the hash must not land. The interleaving is reproduced
// directly: compute the hash for a local account, link the account, then
// call the guarded write with that hash.
func TestPasswordWriteGuardIsAConditionOnTheUpdate(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("racer", "Sup3r-Str0ng-Pass!", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	before := storedPasswordHash(t, store, "racer")

	hash, err := bcrypt.GenerateFromPassword([]byte("An0ther-Str0ng-Pass!"), store.bcryptCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	if _, err := store.db.Exec(
		"UPDATE users SET auth_source = ?, external_subject = ? WHERE username = ?",
		AuthSourceOIDC, "23|https://idp.example.com|subject-9", "racer"); err != nil {
		t.Fatalf("linking the account: %v", err)
	}

	store.mu.Lock()
	err = store.writePasswordHashLocked(store.db, "racer", hash)
	store.mu.Unlock()
	if err == nil {
		t.Fatal("expected the write to be refused once the account was linked")
	}
	if !strings.Contains(err.Error(), "identity is managed by oidc") {
		t.Errorf("expected the refusal to name the managing identity source, got: %v", err)
	}
	if after := storedPasswordHash(t, store, "racer"); after != before {
		t.Error("the password hash was written onto a federated account")
	}
}

// failingExecer wraps a real database handle and fails whichever of the
// two calls a case asks it to, so that the two failure branches of
// writePasswordHashLocked that no schema change can reach, a failed
// RowsAffected and a failed auth_source lookup after a write that matched
// nothing, are still exercised.
type failingExecer struct {
	db         *sql.DB
	failRows   bool
	failLookup bool
}

type failingResult struct{}

func (failingResult) LastInsertId() (int64, error) { return 0, nil }
func (failingResult) RowsAffected() (int64, error) { return 0, errors.New("rows affected unavailable") }

func (f *failingExecer) Exec(query string, args ...any) (sql.Result, error) {
	result, err := f.db.Exec(query, args...)
	if err == nil && f.failRows {
		return failingResult{}, nil
	}
	return result, err
}

func (f *failingExecer) QueryRow(query string, args ...any) *sql.Row {
	if f.failLookup {
		return f.db.QueryRow("SELECT auth_source FROM no_such_table")
	}
	return f.db.QueryRow(query, args...)
}

func TestWritePasswordHashLockedReportsConfirmationFailures(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()
	hash := []byte("$2a$04$notarealhashbutlongenoughtostorexxxxxxxxxxxxxxxxxxxxxx")

	store.mu.Lock()
	defer store.mu.Unlock()

	err := store.writePasswordHashLocked(&failingExecer{db: store.db, failRows: true}, "nobody", hash)
	if err == nil || !strings.Contains(err.Error(), "failed to confirm the password update") {
		t.Errorf("RowsAffected failure: err = %v, want 'failed to confirm the password update'", err)
	}

	err = store.writePasswordHashLocked(&failingExecer{db: store.db, failLookup: true}, "nobody", hash)
	if err == nil || !strings.Contains(err.Error(), "failed to check authentication source") {
		t.Errorf("lookup failure: err = %v, want 'failed to check authentication source'", err)
	}
}
