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

// superuserTargetWrites are the two writes a caller must be a superuser
// to apply to a superuser account, called as a caller whose superuser
// status is callerIsSuperuser.
var superuserTargetWrites = []struct {
	name  string
	write func(s *ActorStore, username string, callerIsSuperuser bool) error
}{
	{"update", func(s *ActorStore, username string, callerIsSuperuser bool) error {
		note := "after"
		return s.UpdateUserAtomic(username, UserUpdate{Annotation: &note},
			callerIsSuperuser)
	}},
	{"delete", func(s *ActorStore, username string, callerIsSuperuser bool) error {
		return s.DeleteUser(username, callerIsSuperuser)
	}},
}

// newTargetStore returns a store holding a superuser "root", which keeps
// the last-superuser guard out of the way, and an ordinary account
// "target" annotated "before".
func newTargetStore(t *testing.T) (*AuthStore, func()) {
	t.Helper()
	store, cleanup := newSoleSuperuserStore(t)
	if err := store.CreateUser("target", "Password1234", "before", "", ""); err != nil {
		cleanup()
		t.Fatalf("CreateUser failed: %v", err)
	}
	return store, cleanup
}

// assertTargetUntouched checks that a refused write left "target" as it
// was: present, still a superuser and still annotated "before".
func assertTargetUntouched(t *testing.T, store *AuthStore) {
	t.Helper()
	user, err := store.GetUser("target")
	if err != nil || user == nil {
		t.Fatalf("The refused write removed the account: %v", err)
	}
	if !user.IsSuperuser || user.Annotation != "before" {
		t.Errorf("The refused write changed the account: superuser=%v annotation=%q",
			user.IsSuperuser, user.Annotation)
	}
}

// assertNoFailureRow checks that the store recorded no failure event for
// action. The handler records a refusal with ErrSuperuserTargetForbidden
// as a coalesced denial, so a failure row as well would let a caller grow
// the audit log by one row per refused request.
func assertNoFailureRow(t *testing.T, store *AuthStore, action string) {
	t.Helper()
	events, _, err := store.ListAuditEvents(AuditFilter{
		Action:  action,
		Outcome: string(OutcomeFailure),
	})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("Expected no failure event for %s, got %d", action, len(events))
	}
}

// TestSuperuserTargetGuard checks that a caller who is not a superuser
// may update or delete an ordinary account but not a superuser one, and
// that a superuser may do either.
func TestSuperuserTargetGuard(t *testing.T) {
	for _, w := range superuserTargetWrites {
		t.Run(w.name+" ordinary account", func(t *testing.T) {
			store, cleanup := newTargetStore(t)
			defer cleanup()

			if err := w.write(store.AsActor(testActor()), "target", false); err != nil {
				t.Errorf("Expected the write to pass, got %v", err)
			}
		})
		t.Run(w.name+" superuser account refused", func(t *testing.T) {
			store, cleanup := newTargetStore(t)
			defer cleanup()
			if err := store.SetUserSuperuser("target", true); err != nil {
				t.Fatalf("SetUserSuperuser failed: %v", err)
			}

			err := w.write(store.AsActor(testActor()), "target", false)
			if !errors.Is(err, ErrSuperuserTargetForbidden) {
				t.Fatalf("Expected ErrSuperuserTargetForbidden, got %v", err)
			}
			assertTargetUntouched(t, store)
			assertNoFailureRow(t, store, "user."+w.name)
		})
		t.Run(w.name+" superuser account by a superuser", func(t *testing.T) {
			store, cleanup := newTargetStore(t)
			defer cleanup()
			if err := store.SetUserSuperuser("target", true); err != nil {
				t.Fatalf("SetUserSuperuser failed: %v", err)
			}

			if err := w.write(store.AsActor(testActor()), "target", true); err != nil {
				t.Errorf("Expected the write to pass, got %v", err)
			}
		})
	}
}

// TestSuperuserTargetGuardRefusesBeforeLastSuperuser checks that a caller
// who is not a superuser, deleting or disabling the only superuser, is
// told it lacks the role rather than that the account is the last one.
func TestSuperuserTargetGuardRefusesBeforeLastSuperuser(t *testing.T) {
	no := false
	writes := map[string]func(s *ActorStore) error{
		"update": func(s *ActorStore) error {
			return s.UpdateUserAtomic("root", UserUpdate{Enabled: &no}, false)
		},
		"delete": func(s *ActorStore) error { return s.DeleteUser("root", false) },
	}
	for name, write := range writes {
		t.Run(name, func(t *testing.T) {
			store, cleanup := newSoleSuperuserStore(t)
			defer cleanup()

			if err := write(store.AsActor(testActor())); !errors.Is(err,
				ErrSuperuserTargetForbidden) {
				t.Errorf("Expected ErrSuperuserTargetForbidden, got %v", err)
			}
		})
	}
}

// TestSuperuserTargetPromotedAfterRead covers issue #588: a caller that
// read the target as an ordinary account, and so would have passed a
// check made before the write, is still refused when the target has been
// promoted by the time the write runs, because the store reads the flag
// inside the write's own transaction.
func TestSuperuserTargetPromotedAfterRead(t *testing.T) {
	for _, w := range superuserTargetWrites {
		t.Run(w.name, func(t *testing.T) {
			store, cleanup := newTargetStore(t)
			defer cleanup()

			// The read a handler makes before the write.
			seen, err := store.GetUser("target")
			if err != nil || seen == nil || seen.IsSuperuser {
				t.Fatalf("Expected an ordinary account, got %+v, %v", seen, err)
			}

			// Another request promotes the target in between.
			if err := store.SetUserSuperuser("target", true); err != nil {
				t.Fatalf("SetUserSuperuser failed: %v", err)
			}

			err = w.write(store.AsActor(testActor()), "target", false)
			if !errors.Is(err, ErrSuperuserTargetForbidden) {
				t.Fatalf("Expected ErrSuperuserTargetForbidden, got %v", err)
			}
			assertTargetUntouched(t, store)
		})
	}
}

// TestSuperuserTargetGuardConcurrentPromotion races a promotion of the
// target against a delete by a caller who is not a superuser. Exactly one
// may succeed: either the delete commits first and the promotion finds no
// account, or the promotion commits first and the delete is refused.
func TestSuperuserTargetGuardConcurrentPromotion(t *testing.T) {
	for i := 0; i < 20; i++ {
		store, cleanup := newTargetStore(t)

		var promoteErr, deleteErr error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			promoteErr = store.SetUserSuperuser("target", true)
		}()
		go func() {
			defer wg.Done()
			deleteErr = store.AsActor(testActor()).DeleteUser("target", false)
		}()
		wg.Wait()

		if (promoteErr == nil) == (deleteErr == nil) {
			t.Errorf("Expected exactly one of the promotion and the delete, got %v and %v",
				promoteErr, deleteErr)
		}
		if deleteErr != nil && !errors.Is(deleteErr, ErrSuperuserTargetForbidden) {
			t.Errorf("Expected the delete to be refused as forbidden, got %v",
				deleteErr)
		}
		cleanup()
	}
}
