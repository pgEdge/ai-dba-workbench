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
	"strconv"
	"sync"
	"testing"
)

// ownedTokenWrites are the token writes a caller must be a superuser to
// apply when the token's owner, "owner", is a superuser, called with the
// caller's authority as allowed.
var ownedTokenWrites = []struct {
	name  string
	write func(s *ActorStore, tokenID int64, allowed bool) error
}{
	{"create", func(s *ActorStore, _ int64, allowed bool) error {
		_, _, err := s.CreateToken("owner", "another", nil, allowed)
		return err
	}},
	{"delete", func(s *ActorStore, tokenID int64, allowed bool) error {
		return s.DeleteToken(strconv.FormatInt(tokenID, 10), allowed)
	}},
	{"set scope", func(s *ActorStore, tokenID int64, allowed bool) error {
		return s.SetTokenScope(tokenID, TokenScopeChange{
			AdminPermissions: []string{"*"},
		}, allowed)
	}},
	{"clear scope", func(s *ActorStore, tokenID int64, allowed bool) error {
		return s.ClearTokenScope(tokenID, allowed)
	}},
	{"set connection scope", func(s *ActorStore, tokenID int64, allowed bool) error {
		return s.SetTokenConnectionScope(tokenID, []ScopedConnection{
			{ConnectionID: 1, AccessLevel: AccessLevelRead},
		}, allowed)
	}},
	{"set MCP scope", func(s *ActorStore, tokenID int64, allowed bool) error {
		return s.SetTokenMCPScope(tokenID, nil, allowed)
	}},
	{"set MCP scope by names", func(s *ActorStore, tokenID int64, allowed bool) error {
		return s.SetTokenMCPScopeByNames(tokenID, nil, allowed)
	}},
	{"set admin scope", func(s *ActorStore, tokenID int64, allowed bool) error {
		return s.SetTokenAdminScope(tokenID, []string{"*"}, allowed)
	}},
}

// newOwnedTokenStore returns a store holding a superuser "root", an
// ordinary account "owner" and one token of owner's whose admin scope is
// manage_users.
func newOwnedTokenStore(t *testing.T) (*AuthStore, int64, func()) {
	t.Helper()
	store, cleanup := newSoleSuperuserStore(t)
	if err := store.CreateUser("owner", "Password1234", "", "", ""); err != nil {
		cleanup()
		t.Fatalf("CreateUser failed: %v", err)
	}
	_, token, err := store.CreateToken("owner", "scoped", nil)
	if err != nil {
		cleanup()
		t.Fatalf("CreateToken failed: %v", err)
	}
	if err := store.SetTokenAdminScope(token.ID,
		[]string{PermManageUsers}); err != nil {
		cleanup()
		t.Fatalf("SetTokenAdminScope failed: %v", err)
	}
	return store, token.ID, cleanup
}

// assertOwnedTokenUntouched checks that a refused write left owner with
// its one token, still scoped to manage_users alone.
func assertOwnedTokenUntouched(t *testing.T, store *AuthStore, tokenID int64) {
	t.Helper()
	tokens, err := store.ListUserTokens("owner")
	if err != nil {
		t.Fatalf("ListUserTokens failed: %v", err)
	}
	if len(tokens) != 1 || tokens[0].ID != tokenID {
		t.Fatalf("The refused write changed owner's tokens: %+v", tokens)
	}
	scope, err := store.GetTokenScope(tokenID)
	if err != nil {
		t.Fatalf("GetTokenScope failed: %v", err)
	}
	if scope == nil || len(scope.Connections) != 0 ||
		len(scope.MCPPrivileges) != 0 || len(scope.AdminPermissions) != 1 ||
		scope.AdminPermissions[0] != PermManageUsers {
		t.Errorf("The refused write changed the token's scope: %+v", scope)
	}
}

// auditEventID returns the ID of the latest successful event recorded
// under action, failing the test when there is none.
func auditEventID(t *testing.T, store *AuthStore, action string) int64 {
	t.Helper()
	events, _, err := store.ListAuditEvents(AuditFilter{Action: action})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	for _, ev := range events {
		if ev.Outcome == OutcomeSuccess {
			return ev.ID
		}
	}
	t.Fatalf("No successful %s event recorded", action)
	return 0
}

// TestSuperuserOwnedTokenGuard checks that a caller who is not a
// superuser may write to an ordinary account's token but not to a
// superuser's, and that a superuser may write to either.
func TestSuperuserOwnedTokenGuard(t *testing.T) {
	for _, w := range ownedTokenWrites {
		t.Run(w.name+" ordinary owner", func(t *testing.T) {
			store, tokenID, cleanup := newOwnedTokenStore(t)
			defer cleanup()

			if err := w.write(store.AsActor(testActor()), tokenID, false); err != nil {
				t.Errorf("Expected the write to pass, got %v", err)
			}
		})
		t.Run(w.name+" superuser owner refused", func(t *testing.T) {
			store, tokenID, cleanup := newOwnedTokenStore(t)
			defer cleanup()
			if err := store.SetUserSuperuser("owner", true); err != nil {
				t.Fatalf("SetUserSuperuser failed: %v", err)
			}

			err := w.write(store.AsActor(testActor()), tokenID, false)
			if !errors.Is(err, ErrSuperuserTargetForbidden) {
				t.Fatalf("Expected ErrSuperuserTargetForbidden, got %v", err)
			}
			assertOwnedTokenUntouched(t, store, tokenID)
			if ev := lastAuditEvent(t, store); ev.Outcome != OutcomeFailure {
				t.Errorf("Expected a failed audit event, got %q", ev.Outcome)
			}
		})
		t.Run(w.name+" superuser owner allowed", func(t *testing.T) {
			store, tokenID, cleanup := newOwnedTokenStore(t)
			defer cleanup()
			if err := store.SetUserSuperuser("owner", true); err != nil {
				t.Fatalf("SetUserSuperuser failed: %v", err)
			}

			if err := w.write(store.AsActor(testActor()), tokenID, true); err != nil {
				t.Errorf("Expected the write to pass, got %v", err)
			}
		})
	}
}

// TestSuperuserOwnedTokenPromotedAfterRead covers issue #607: a caller
// that read the token's owner as an ordinary account, and so would have
// passed a check made before the write, is still refused when the owner
// has been promoted by the time the write runs, because the store reads
// the owner inside the write's own transaction.
func TestSuperuserOwnedTokenPromotedAfterRead(t *testing.T) {
	for _, w := range ownedTokenWrites {
		t.Run(w.name, func(t *testing.T) {
			store, tokenID, cleanup := newOwnedTokenStore(t)
			defer cleanup()

			// The read a handler made before the write.
			seen, err := store.GetUser("owner")
			if err != nil || seen == nil || seen.IsSuperuser {
				t.Fatalf("Expected an ordinary account, got %+v, %v", seen, err)
			}

			// Another request promotes the owner in between.
			if err := store.SetUserSuperuser("owner", true); err != nil {
				t.Fatalf("SetUserSuperuser failed: %v", err)
			}

			err = w.write(store.AsActor(testActor()), tokenID, false)
			if !errors.Is(err, ErrSuperuserTargetForbidden) {
				t.Fatalf("Expected ErrSuperuserTargetForbidden, got %v", err)
			}
			assertOwnedTokenUntouched(t, store, tokenID)
		})
	}
}

// TestSuperuserOwnedTokenConcurrentPromotion races a promotion of the
// owner against a widening of its token's scope by a caller who is not a
// superuser. Either the widening commits first, while the owner is
// still ordinary, or the promotion does and the widening is refused; in
// no order may the superuser's token end up widened by that caller.
func TestSuperuserOwnedTokenConcurrentPromotion(t *testing.T) {
	for i := 0; i < 20; i++ {
		store, tokenID, cleanup := newOwnedTokenStore(t)

		var promoteErr, widenErr error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			promoteErr = store.SetUserSuperuser("owner", true)
		}()
		go func() {
			defer wg.Done()
			widenErr = store.AsActor(testActor()).SetTokenScope(tokenID,
				TokenScopeChange{AdminPermissions: []string{"*"}}, false)
		}()
		wg.Wait()

		if promoteErr != nil {
			t.Fatalf("SetUserSuperuser failed: %v", promoteErr)
		}
		if widenErr != nil && !errors.Is(widenErr, ErrSuperuserTargetForbidden) {
			t.Errorf("Expected the widening to be refused as forbidden, got %v",
				widenErr)
		}
		if widenErr != nil {
			assertOwnedTokenUntouched(t, store, tokenID)
		} else if widen, promote := auditEventID(t, store, actionSetAdminScope),
			auditEventID(t, store, "user.set_superuser"); widen > promote {
			// A widening that succeeded must have committed while the
			// owner was still ordinary, and so before the promotion.
			t.Errorf("The widening (event %d) committed after the promotion (event %d)",
				widen, promote)
		}
		cleanup()
	}
}

// TestSuperuserOwnedTokenSetScopeIsAtomic checks that SetTokenScope
// writes every kind or none: an unknown MCP privilege, or an admin
// scope refused by the superuser guard, leaves the connection and admin
// scopes as they were.
func TestSuperuserOwnedTokenSetScopeIsAtomic(t *testing.T) {
	store, tokenID, cleanup := newOwnedTokenStore(t)
	defer cleanup()
	actor := store.AsActor(testActor())

	err := actor.SetTokenScope(tokenID, TokenScopeChange{
		MCPPrivileges:    []string{"no_such_tool"},
		Connections:      []ScopedConnection{{ConnectionID: 1, AccessLevel: AccessLevelRead}},
		AdminPermissions: []string{"*"},
	}, false)
	if !errors.Is(err, ErrUnknownMCPPrivilege) {
		t.Fatalf("Expected ErrUnknownMCPPrivilege, got %v", err)
	}
	assertOwnedTokenUntouched(t, store, tokenID)
	ev := lastAuditEvent(t, store)
	if ev.Outcome != OutcomeFailure || ev.Action != actionSetMCPScope {
		t.Errorf("Expected a failed %s event, got %s %q", actionSetMCPScope,
			ev.Action, ev.Outcome)
	}
}

// TestSetTokenScopeWritesEveryKind checks that SetTokenScope writes
// each kind it is given and records one event per kind, and that a
// change supplying no kind does nothing.
func TestSetTokenScopeWritesEveryKind(t *testing.T) {
	store, tokenID, cleanup := newOwnedTokenStore(t)
	defer cleanup()
	if _, err := store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool,
		"a", false); err != nil {
		t.Fatalf("RegisterMCPPrivilege failed: %v", err)
	}
	actor := store.AsActor(testActor())

	before := lastAuditEvent(t, store).ID
	if err := actor.SetTokenScope(tokenID, TokenScopeChange{}, false); err != nil {
		t.Fatalf("Expected an empty change to pass, got %v", err)
	}
	if got := lastAuditEvent(t, store).ID; got != before {
		t.Errorf("Expected an empty change to record nothing, got event %d", got)
	}

	if err := actor.SetTokenScope(tokenID, TokenScopeChange{
		MCPPrivileges:    []string{"tool_a"},
		Connections:      []ScopedConnection{{ConnectionID: 3, AccessLevel: AccessLevelRead}},
		AdminPermissions: []string{PermManageGroups},
	}, false); err != nil {
		t.Fatalf("SetTokenScope failed: %v", err)
	}
	scope, err := store.GetTokenScope(tokenID)
	if err != nil || scope == nil {
		t.Fatalf("GetTokenScope failed: %+v, %v", scope, err)
	}
	if len(scope.MCPPrivileges) != 1 || len(scope.Connections) != 1 ||
		scope.Connections[0].ConnectionID != 3 ||
		len(scope.AdminPermissions) != 1 ||
		scope.AdminPermissions[0] != PermManageGroups {
		t.Errorf("Unexpected scope after the change: %+v", scope)
	}

	events, _, err := store.ListAuditEvents(AuditFilter{Limit: 3})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	seen := map[string]bool{}
	for _, ev := range events {
		if ev.Outcome == OutcomeSuccess {
			seen[ev.Action] = true
		}
	}
	for _, action := range []string{actionSetMCPScope,
		actionSetConnectionScope, actionSetAdminScope} {
		if !seen[action] {
			t.Errorf("Expected a %s event, got %+v", action, events)
		}
	}
}

// TestSetTokenScopeRejectsInvalidInput checks that a malformed
// connection scope or an unknown admin permission is refused before the
// transaction starts.
func TestSetTokenScopeRejectsInvalidInput(t *testing.T) {
	store, tokenID, cleanup := newOwnedTokenStore(t)
	defer cleanup()
	actor := store.AsActor(testActor())

	for name, change := range map[string]TokenScopeChange{
		"connections": {Connections: []ScopedConnection{
			{ConnectionID: 5, AccessLevel: AccessLevelRead},
			{ConnectionID: 5, AccessLevel: AccessLevelReadWrite},
		}},
		"admin": {AdminPermissions: []string{"manage_everything"}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := actor.SetTokenScope(tokenID, change, false); err == nil {
				t.Errorf("Expected the change to be refused")
			}
			assertOwnedTokenUntouched(t, store, tokenID)
		})
	}
}

// TestSuperuserOwnedTokenOwnerReadFails checks that the guard refuses
// the change when it cannot read the token's owner, rather than taking
// the failure to mean the owner is not a superuser, and that a caller
// allowed a superuser's token skips the read.
func TestSuperuserOwnedTokenOwnerReadFails(t *testing.T) {
	store, tokenID, cleanup := newOwnedTokenStore(t)
	defer cleanup()

	tx, err := store.db.Begin()
	if err != nil {
		t.Fatalf("Begin failed: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec("ALTER TABLE users RENAME TO users_gone"); err != nil {
		t.Fatalf("Failed to rename the users table: %v", err)
	}

	err = guardSuperuserOwnedTokenTx(tx, tokenID, false)
	if err == nil || errors.Is(err, ErrSuperuserTargetForbidden) {
		t.Errorf("Expected a read failure, got %v", err)
	}
	if err := guardSuperuserOwnedTokenTx(tx, tokenID, true); err != nil {
		t.Errorf("Expected an allowed caller to skip the read, got %v", err)
	}
}

// TestSuperuserOwnedTokenGuardMissingToken checks that the guard lets a
// token that does not exist through, for the change to report.
func TestSuperuserOwnedTokenGuardMissingToken(t *testing.T) {
	store, _, cleanup := newOwnedTokenStore(t)
	defer cleanup()

	err := store.AsActor(testActor()).ClearTokenScope(99999, false)
	if errors.Is(err, ErrSuperuserTargetForbidden) {
		t.Errorf("Expected a missing token not to be refused as forbidden")
	}
}

// TestSuperuserOwnedTokenHashPrefixDelete checks that a hash-prefix
// delete matching a superuser's token is refused as a whole.
func TestSuperuserOwnedTokenHashPrefixDelete(t *testing.T) {
	store, tokenID, cleanup := newOwnedTokenStore(t)
	defer cleanup()
	if err := store.SetUserSuperuser("owner", true); err != nil {
		t.Fatalf("SetUserSuperuser failed: %v", err)
	}
	token, err := store.GetTokenByID(tokenID)
	if err != nil || token == nil {
		t.Fatalf("GetTokenByID failed: %+v, %v", token, err)
	}

	err = store.AsActor(testActor()).DeleteToken(token.TokenHash[:16], false)
	if !errors.Is(err, ErrSuperuserTargetForbidden) {
		t.Fatalf("Expected ErrSuperuserTargetForbidden, got %v", err)
	}
	assertOwnedTokenUntouched(t, store, tokenID)
}
