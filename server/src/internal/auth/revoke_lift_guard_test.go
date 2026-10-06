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

// sharedReadOnlyGrant gives connection 7, which the fixture's token
// holds read-only, a grant in the fixture's group and in a second
// group, so that each grant alone is not the connection's last. It
// returns the second group's id.
func (f *grantScopeFixture) sharedReadOnlyGrant(t *testing.T) int64 {
	t.Helper()
	f.grant(t, 7, AccessLevelRead)
	return f.newOwner(t, []ScopedConnection{{ConnectionID: 7,
		AccessLevel: AccessLevelRead}}, nil).groupID
}

// assertStillRestricted fails unless connection 7 still has a grant.
func (f *grantScopeFixture) assertStillRestricted(t *testing.T) {
	t.Helper()
	assigned, err := f.store.IsConnectionAssignedToAnyGroup(7)
	if err != nil {
		t.Fatalf("IsConnectionAssignedToAnyGroup failed: %v", err)
	}
	if !assigned {
		t.Error("Connection 7 lost its last grant, lifting its restriction")
	}
}

// TestRevokeGuardRefusesNewlyLastGrant checks that the store decides
// whether a grant is the last inside the revoke, not from a guard
// computed when it was not: once one revoke has removed the other
// grant, the second revoke with the same guard is refused.
func TestRevokeGuardRefusesNewlyLastGrant(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()
	other := f.sharedReadOnlyGrant(t)
	guard := f.checker.ConnectionLiftGuard(f.tokenCtx(), 7)
	actor := f.store.AsActor(systemActor)

	if err := actor.RevokeConnectionPrivilegeGuarded(other, 7, guard); err != nil {
		t.Fatalf("Revoking a grant that is not the last failed: %v", err)
	}
	err := actor.RevokeConnectionPrivilegeGuarded(f.groupID, 7, guard)
	if !errors.Is(err, ErrRevokeLiftsRestriction) {
		t.Errorf("Expected ErrRevokeLiftsRestriction, got %v", err)
	}
	f.assertStillRestricted(t)

	// A guard that allows the lift, as a session's does, lets it through.
	if err := actor.RevokeConnectionPrivilegeGuarded(f.groupID, 7,
		AllowAllLifts()); err != nil {
		t.Errorf("An allowing guard should revoke the last grant: %v", err)
	}
}

// TestRevokeGuardConcurrent checks that two revokes racing to remove a
// connection's last two grants, each checked as not the last before
// either ran, cannot both succeed.
func TestRevokeGuardConcurrent(t *testing.T) {
	for i := 0; i < 20; i++ {
		f, cleanup := newGrantScopeFixture(t)
		other := f.sharedReadOnlyGrant(t)
		ctx := f.tokenCtx()
		groups := []int64{f.groupID, other}
		guards := []*LiftGuard{f.checker.ConnectionLiftGuard(ctx, 7),
			f.checker.ConnectionLiftGuard(ctx, 7)}
		actor := f.store.AsActor(systemActor)

		errs := make([]error, len(groups))
		var wg sync.WaitGroup
		for n := range groups {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				errs[n] = actor.RevokeConnectionPrivilegeGuarded(groups[n], 7,
					guards[n])
			}(n)
		}
		wg.Wait()

		var ok, refused int
		for _, err := range errs {
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrRevokeLiftsRestriction):
				refused++
			default:
				t.Fatalf("Unexpected error: %v", err)
			}
		}
		if ok != 1 || refused != 1 {
			t.Errorf("Expected one revoke and one refusal, got %d and %d",
				ok, refused)
		}
		f.assertStillRestricted(t)
		cleanup()
	}
}

// TestDeleteGroupGuardRefusesNewlyLastGrant checks that a group delete
// racing a revoke of the connection's other grant cannot leave it with
// no grant, and that a delete whose guard was computed before the
// group gained a grant fails closed on that grant.
func TestDeleteGroupGuardRefusesNewlyLastGrant(t *testing.T) {
	t.Run("concurrent revoke", func(t *testing.T) {
		for i := 0; i < 20; i++ {
			f, cleanup := newGrantScopeFixture(t)
			other := f.sharedReadOnlyGrant(t)
			ctx := f.tokenCtx()
			deleteGuard := f.checker.GroupDeleteLiftGuard(ctx, f.groupID)
			revokeGuard := f.checker.ConnectionLiftGuard(ctx, 7)
			actor := f.store.AsActor(systemActor)

			var deleteErr, revokeErr error
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				deleteErr = actor.DeleteGroupGuarded(f.groupID, deleteGuard)
			}()
			go func() {
				defer wg.Done()
				revokeErr = actor.RevokeConnectionPrivilegeGuarded(other, 7,
					revokeGuard)
			}()
			wg.Wait()

			if (deleteErr == nil) == (revokeErr == nil) {
				t.Errorf("Expected exactly one to succeed, got delete %v, "+
					"revoke %v", deleteErr, revokeErr)
			}
			for _, err := range []error{deleteErr, revokeErr} {
				if err != nil && !errors.Is(err, ErrRevokeLiftsRestriction) {
					t.Fatalf("Unexpected error: %v", err)
				}
			}
			f.assertStillRestricted(t)
			cleanup()
		}
	})

	t.Run("grant added after the guard", func(t *testing.T) {
		f, cleanup := newGrantScopeFixture(t)
		defer cleanup()
		guard := f.checker.GroupDeleteLiftGuard(f.tokenCtx(), f.groupID)
		f.grant(t, 5, AccessLevelRead)
		err := f.store.AsActor(systemActor).DeleteGroupGuarded(f.groupID, guard)
		if !errors.Is(err, ErrRevokeLiftsRestriction) {
			t.Errorf("Expected ErrRevokeLiftsRestriction, got %v", err)
		}
		if _, err := f.store.GetGroup(f.groupID); err != nil {
			t.Errorf("The group should survive the refusal: %v", err)
		}
	})
}

// TestConnectionLiftGuard covers the guard the checker builds.
func TestConnectionLiftGuard(t *testing.T) {
	f, cleanup := newGrantScopeFixture(t)
	defer cleanup()

	g := f.checker.ConnectionLiftGuard(f.tokenCtx(), 5, 7, 9, ConnectionIDAll)
	for conn, want := range map[int]bool{5: true, 7: false, 9: false,
		ConnectionIDAll: false} {
		if got := g.allows(conn); got != want {
			t.Errorf("allows(%d) = %v, want %v", conn, got, want)
		}
	}
	if !f.checker.ConnectionLiftGuard(sessionCtx(), 9).allows(9) {
		t.Error("A session's guard should allow every lift")
	}
	if f.checker.ConnectionLiftGuard(incompleteTokenCtx(), 5).allows(5) {
		t.Error("An unreadable ceiling's guard should allow no lift")
	}
	var none *LiftGuard
	if !none.allows(5) {
		t.Error("A nil guard should allow every lift")
	}
}
