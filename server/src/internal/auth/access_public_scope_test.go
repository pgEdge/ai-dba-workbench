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
	"strings"
	"testing"
)

// =============================================================================
// Regression coverage for GitHub issue #482
//
// A public MCP privilege needs no group membership, and CanAccessMCPItem
// used to return true for one before the token's MCP scope was
// consulted, so a token issued for a single tool could call every
// public tool in the installation. The scope is now applied to the
// public path as well.
// =============================================================================

// publicScopeFixture holds a plain, non-superuser user with a token and
// two public tools, so that the public branch of CanAccessMCPItem is
// the one under test.
type publicScopeFixture struct {
	store   *AuthStore
	checker *RBACChecker
	userID  int64
	tokenID int64
}

func newPublicScopeFixture(t *testing.T) (*publicScopeFixture, func()) {
	t.Helper()

	store, cleanup := createTestAuthStoreForAccess(t)

	if err := store.CreateUser("plain", "Password1234", "Plain", "", ""); err != nil {
		cleanup()
		t.Fatalf("Failed to create user: %v", err)
	}
	userID, err := store.GetUserID("plain")
	if err != nil {
		cleanup()
		t.Fatalf("Failed to read user id: %v", err)
	}
	for _, name := range []string{"public_a", "public_b"} {
		if _, err := store.RegisterMCPPrivilege(name, MCPPrivilegeTypeTool,
			"Public tool", true); err != nil {
			cleanup()
			t.Fatalf("Failed to register %s: %v", name, err)
		}
	}
	_, token, err := store.CreateToken("plain", "public scope token", nil)
	if err != nil {
		cleanup()
		t.Fatalf("Failed to create token: %v", err)
	}

	return &publicScopeFixture{
		store:   store,
		checker: NewRBACChecker(store),
		userID:  userID,
		tokenID: token.ID,
	}, cleanup
}

// tokenCtx presents the fixture's token, which carries no superuser
// status, so every check runs the ordinary path.
func (f *publicScopeFixture) tokenCtx() context.Context {
	ctx := context.WithValue(context.Background(), UserIDContextKey, f.userID)
	ctx = context.WithValue(ctx, IsAPITokenContextKey, true)
	return context.WithValue(ctx, TokenIDContextKey, f.tokenID)
}

// sessionCtx presents the same user without a token.
func (f *publicScopeFixture) sessionCtx() context.Context {
	return context.WithValue(context.Background(), UserIDContextKey, f.userID)
}

// TestCanAccessPublicMCPItemHonoursTokenScope is the issue #482 case: a
// token scoped to one public tool must not reach the other.
func TestCanAccessPublicMCPItemHonoursTokenScope(t *testing.T) {
	f, cleanup := newPublicScopeFixture(t)
	defer cleanup()

	if err := f.store.SetTokenMCPScopeByNames(f.tokenID,
		[]string{"public_a"}); err != nil {
		t.Fatalf("Failed to set MCP scope: %v", err)
	}

	if !f.checker.CanAccessMCPItem(f.tokenCtx(), "public_a") {
		t.Error("Expected the public tool named by the scope to be allowed")
	}
	if f.checker.CanAccessMCPItem(f.tokenCtx(), "public_b") {
		t.Error("Expected a public tool outside the scope to be denied")
	}
}

// TestCanAccessPublicMCPItemUnscopedAndSession covers the callers the
// scope does not narrow: a token with no MCP scope, a wildcard scope
// and a session.
func TestCanAccessPublicMCPItemUnscopedAndSession(t *testing.T) {
	t.Run("no scope", func(t *testing.T) {
		f, cleanup := newPublicScopeFixture(t)
		defer cleanup()

		for _, name := range []string{"public_a", "public_b"} {
			if !f.checker.CanAccessMCPItem(f.tokenCtx(), name) {
				t.Errorf("Expected %s to be allowed for an unscoped token", name)
			}
		}
	})

	t.Run("wildcard scope", func(t *testing.T) {
		f, cleanup := newPublicScopeFixture(t)
		defer cleanup()

		if err := f.store.SetTokenMCPScopeByNames(f.tokenID,
			[]string{mcpWildcardIdentifier}); err != nil {
			t.Fatalf("Failed to set MCP scope: %v", err)
		}

		for _, name := range []string{"public_a", "public_b"} {
			if !f.checker.CanAccessMCPItem(f.tokenCtx(), name) {
				t.Errorf("Expected %s to be allowed for a wildcard scope", name)
			}
		}
	})

	t.Run("session", func(t *testing.T) {
		f, cleanup := newPublicScopeFixture(t)
		defer cleanup()

		if !f.checker.CanAccessMCPItem(f.sessionCtx(), "public_b") {
			t.Error("Expected a session to reach every public tool")
		}
	})
}

// TestCanAccessPublicMCPItemDeniesOnScopeLookupError checks that the
// public path fails closed as the other branches do.
func TestCanAccessPublicMCPItemDeniesOnScopeLookupError(t *testing.T) {
	f, cleanup := newPublicScopeFixture(t)
	defer cleanup()

	if _, err := f.store.db.Exec("DROP TABLE token_mcp_scope"); err != nil {
		t.Fatalf("Failed to drop token_mcp_scope: %v", err)
	}

	if f.checker.CanAccessMCPItem(f.tokenCtx(), "public_a") {
		t.Error("Expected an unreadable MCP scope to deny a public tool")
	}
}

// =============================================================================
// Fail-closed gaps found in the security review of the #471 change
// =============================================================================

// TestGetEffectivePrivilegesDeniesIncompleteTokenContext checks that an
// API-token context carrying no token id reports nothing, rather than
// its owner's full group privileges with no scope applied.
func TestGetEffectivePrivilegesDeniesIncompleteTokenContext(t *testing.T) {
	f, cleanup := newPublicScopeFixture(t)
	defer cleanup()

	ctx := context.WithValue(context.Background(), UserIDContextKey, f.userID)
	ctx = context.WithValue(ctx, IsAPITokenContextKey, true)

	privs := f.checker.GetEffectivePrivileges(ctx)
	if privs.IsSuperuser || len(privs.MCPPrivileges) != 0 ||
		len(privs.ConnectionPrivileges) != 0 || len(privs.AdminPermissions) != 0 {
		t.Errorf("Expected no privileges for an unidentified token, got %+v",
			privs)
	}
}

// TestApplyTokenCeilingRejectsUnknownLevel checks that a level this
// code does not understand cannot widen access.
func TestApplyTokenCeilingRejectsUnknownLevel(t *testing.T) {
	tests := []struct {
		name       string
		tokenLevel string
		userLevel  string
		want       string
	}{
		{"both read_write", AccessLevelReadWrite, AccessLevelReadWrite,
			AccessLevelReadWrite},
		{"token read", AccessLevelRead, AccessLevelReadWrite, AccessLevelRead},
		{"user read", AccessLevelReadWrite, AccessLevelRead, AccessLevelRead},
		{"user none", AccessLevelReadWrite, AccessLevelNone, AccessLevelNone},
		{"unknown token level", "admin", AccessLevelReadWrite, AccessLevelRead},
		{"unknown user level", AccessLevelReadWrite, "admin", AccessLevelRead},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := applyTokenCeiling(tc.tokenLevel, tc.userLevel); got != tc.want {
				t.Errorf("applyTokenCeiling(%q, %q) = %q, want %q", tc.tokenLevel,
					tc.userLevel, got, tc.want)
			}
		})
	}
}

// TestSetTokenConnectionScopeRejectsUnknownLevel checks that a bad
// access level is reported as such rather than as an opaque insert
// failure from the database.
func TestSetTokenConnectionScopeRejectsUnknownLevel(t *testing.T) {
	f, cleanup := newPublicScopeFixture(t)
	defer cleanup()

	err := f.store.SetTokenConnectionScope(f.tokenID, []ScopedConnection{
		{ConnectionID: 1, AccessLevel: AccessLevelRead},
		{ConnectionID: 2, AccessLevel: "admin"},
	})
	if err == nil {
		t.Fatal("Expected an invalid access level to be refused")
	}
	if !strings.Contains(err.Error(), "invalid access level") {
		t.Errorf("Expected the error to name the problem, got %v", err)
	}

	// The whole call is refused, so the first, valid row is not written
	// either.
	scope, scopeErr := f.store.GetTokenScope(f.tokenID)
	if scopeErr != nil {
		t.Fatalf("GetTokenScope failed: %v", scopeErr)
	}
	if scope != nil && len(scope.Connections) != 0 {
		t.Errorf("Expected no connection scope to be written, got %+v",
			scope.Connections)
	}
}
