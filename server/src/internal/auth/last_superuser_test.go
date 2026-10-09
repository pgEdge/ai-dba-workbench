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
	"errors"
	"sync"
	"testing"
)

// addSpareSuperuser creates a second enabled superuser, so that a test
// that demotes, disables or deletes its own superuser does not trip the
// last-superuser guard.
func addSpareSuperuser(t *testing.T, store *AuthStore) {
	t.Helper()
	if err := store.CreateUser("spare-admin", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if err := store.SetUserSuperuser("spare-admin", true); err != nil {
		t.Fatalf("SetUserSuperuser failed: %v", err)
	}
}

// newSoleSuperuserStore returns a store whose only superuser is "root".
func newSoleSuperuserStore(t *testing.T) (*AuthStore, func()) {
	t.Helper()
	store, cleanup := createTestAuthStoreForAudit(t)
	if err := store.CreateUser("root", "Password1234", "", "", ""); err != nil {
		cleanup()
		t.Fatalf("CreateUser failed: %v", err)
	}
	if err := store.SetUserSuperuser("root", true); err != nil {
		cleanup()
		t.Fatalf("SetUserSuperuser failed: %v", err)
	}
	return store, cleanup
}

// TestLastSuperuserGuard checks that every way of taking away the last
// enabled superuser is refused with ErrLastSuperuser and leaves the
// account as it was, and that each is allowed once another enabled
// superuser exists.
func TestLastSuperuserGuard(t *testing.T) {
	no := false
	tests := []struct {
		name   string
		change func(s *ActorStore) error
	}{
		{"demote", func(s *ActorStore) error {
			return s.SetUserSuperuser("root", false)
		}},
		{"disable", func(s *ActorStore) error {
			return s.DisableUser("root")
		}},
		{"delete", func(s *ActorStore) error {
			return s.DeleteUser("root", true)
		}},
		{"update demote", func(s *ActorStore) error {
			return s.UpdateUserAtomic("root", UserUpdate{IsSuperuser: &no}, true)
		}},
		{"update disable", func(s *ActorStore) error {
			return s.UpdateUserAtomic("root", UserUpdate{Enabled: &no}, true)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, cleanup := newSoleSuperuserStore(t)
			defer cleanup()
			as := store.AsActor(testActor())

			if err := tt.change(as); !errors.Is(err, ErrLastSuperuser) {
				t.Fatalf("Expected ErrLastSuperuser, got %v", err)
			}
			user, err := store.GetUser("root")
			if err != nil || user == nil || !user.IsSuperuser || !user.Enabled {
				t.Fatalf("The refused change altered the account: %+v, %v",
					user, err)
			}
			if ev := lastAuditEvent(t, store); ev.Outcome != OutcomeFailure {
				t.Errorf("Expected a failed audit event, got %q", ev.Outcome)
			}

			addSpareSuperuser(t, store)
			if err := tt.change(as); err != nil {
				t.Errorf("Expected the change to pass with a spare superuser: %v",
					err)
			}
		})
	}
}

// TestLastSuperuserGuardIgnoresOthers checks that the guard leaves
// alone changes that keep the last superuser, and changes to accounts
// that are not enabled superusers.
func TestLastSuperuserGuardIgnoresOthers(t *testing.T) {
	store, cleanup := newSoleSuperuserStore(t)
	defer cleanup()
	yes := true
	note := "note"

	if err := store.UpdateUserAtomic("root", UserUpdate{Annotation: &note,
		IsSuperuser: &yes, Enabled: &yes}); err != nil {
		t.Errorf("Keeping the last superuser should pass: %v", err)
	}
	if err := store.CreateUser("plain", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if err := store.DisableUser("plain"); err != nil {
		t.Errorf("Disabling a non-superuser should pass: %v", err)
	}
	if err := store.DeleteUser("plain"); err != nil {
		t.Errorf("Deleting a non-superuser should pass: %v", err)
	}

	// A disabled superuser is not one the guard protects.
	addSpareSuperuser(t, store)
	if err := store.DisableUser("spare-admin"); err != nil {
		t.Fatalf("DisableUser failed: %v", err)
	}
	if err := store.DeleteUser("spare-admin"); err != nil {
		t.Errorf("Deleting a disabled superuser should pass: %v", err)
	}
}

// TestLastSuperuserGuardConcurrent checks that two superusers demoting
// each other at once cannot both succeed: the count runs in each
// demotion's own transaction.
func TestLastSuperuserGuardConcurrent(t *testing.T) {
	for i := 0; i < 20; i++ {
		store, cleanup := newSoleSuperuserStore(t)
		addSpareSuperuser(t, store)

		names := []string{"root", "spare-admin"}
		errs := make([]error, len(names))
		var wg sync.WaitGroup
		for n := range names {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				errs[n] = store.SetUserSuperuser(names[n], false)
			}(n)
		}
		wg.Wait()

		if (errs[0] == nil) == (errs[1] == nil) {
			t.Errorf("Expected exactly one demotion, got %v and %v",
				errs[0], errs[1])
		}
		cleanup()
	}
}

// TestLastSuperuserGuardCountFails checks that a failing count refuses
// the change.
func TestLastSuperuserGuardCountFails(t *testing.T) {
	store, cleanup := newSoleSuperuserStore(t)
	defer cleanup()
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatalf("Begin failed: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec("ALTER TABLE users RENAME TO users_gone"); err != nil {
		t.Fatalf("rename failed: %v", err)
	}
	before := userSnapshot{ID: 1, IsSuperuser: true, Enabled: true}
	err = guardLastSuperuserTx(tx, before, before, true)
	if err == nil || errors.Is(err, ErrLastSuperuser) {
		t.Errorf("Expected a count failure, got %v", err)
	}
}
