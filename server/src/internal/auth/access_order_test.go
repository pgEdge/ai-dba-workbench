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
	"context"
	"errors"
	"testing"
)

// TestCanAccessConnectionReadsPrivilegesBeforeSharingLookup locks in the
// order of the queries CanAccessConnection runs for a non-superuser
// (issue #677). The sharing lookup fails for a connection that does not
// exist, so if it ran before the group privileges read, a missing
// connection would be refused one query sooner than a connection the
// caller cannot see, and the response time would reveal which IDs exist.
// The handler-level tests compare only status and body, so they cannot
// catch a revert of that order; this test does.
func TestCanAccessConnectionReadsPrivilegesBeforeSharingLookup(t *testing.T) {
	const connectionID = 42

	// newChecker returns a checker whose sharing lookup counts its calls
	// and fails as it does for a missing connection, and a context for a
	// user with no groups.
	newChecker := func(t *testing.T) (*AuthStore, *RBACChecker, *int, context.Context) {
		t.Helper()
		store, cleanup := createTestAuthStoreForAccess(t)
		t.Cleanup(cleanup)

		mustCreateUser(t, store, "order677")
		userID, err := store.GetUserID("order677")
		if err != nil {
			t.Fatalf("GetUserID: %v", err)
		}

		calls := 0
		checker := NewRBACCheckerWithSharing(store,
			func(context.Context, int) (bool, string, error) {
				calls++
				return false, "", errors.New("connection not found")
			})
		ctx := context.WithValue(context.Background(), UserIDContextKey, userID)
		ctx = context.WithValue(ctx, UsernameContextKey, "order677")
		return store, checker, &calls, ctx
	}

	t.Run("a failed privileges read stops before the sharing lookup", func(t *testing.T) {
		store, checker, calls, ctx := newChecker(t)
		mustExec(t, store, "DROP TABLE group_memberships")

		ok, level := checker.CanAccessConnection(ctx, connectionID)
		if ok || level != AccessLevelNone {
			t.Errorf("CanAccessConnection = (%v, %q), want (false, %q)",
				ok, level, AccessLevelNone)
		}
		if *calls != 0 {
			t.Errorf("Sharing lookup ran %d times before the privileges read failed, want 0", *calls)
		}
	})

	// The control: with the privileges read intact, the same checker does
	// reach the lookup, so the zero above is not an unwired counter.
	t.Run("a readable caller reaches the sharing lookup", func(t *testing.T) {
		_, checker, calls, ctx := newChecker(t)

		ok, level := checker.CanAccessConnection(ctx, connectionID)
		if ok || level != AccessLevelNone {
			t.Errorf("CanAccessConnection = (%v, %q), want (false, %q)",
				ok, level, AccessLevelNone)
		}
		if *calls != 1 {
			t.Errorf("Sharing lookup ran %d times, want 1", *calls)
		}
	})
}
