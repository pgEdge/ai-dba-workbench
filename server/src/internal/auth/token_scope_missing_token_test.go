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
	"strconv"
	"testing"
)

// scopeReads calls every token scope read in the store for tokenID and
// returns each one's error, by name.
func scopeReads(s *AuthStore, tokenID int64) map[string]error {
	errs := map[string]error{}
	_, errs["GetTokenScope"] = s.GetTokenScope(tokenID)
	_, _, errs["IsConnectionInTokenScope"] = s.IsConnectionInTokenScope(tokenID, 5)
	_, errs["IsMCPItemInTokenScope"] = s.IsMCPItemInTokenScope(tokenID, "list_connections")
	_, errs["GetTokenConnectionScope"] = s.GetTokenConnectionScope(tokenID)
	_, errs["HasTokenMCPScope"] = s.HasTokenMCPScope(tokenID)
	_, errs["GetTokenMCPScope"] = s.GetTokenMCPScope(tokenID)
	_, errs["HasTokenScope"] = s.HasTokenScope(tokenID)
	_, errs["GetTokenAdminScope"] = s.GetTokenAdminScope(tokenID)
	_, errs["IsAdminPermissionInTokenScope"] = s.IsAdminPermissionInTokenScope(
		tokenID, PermManageUsers)
	return errs
}

// TestTokenScopeReadsRefuseMissingToken checks that every scope read
// reports ErrTokenNotFound for a token that has been deleted, or never
// existed, rather than the empty scope that reads as unrestricted, and
// still reads an existing token's scope.
func TestTokenScopeReadsRefuseMissingToken(t *testing.T) {
	s, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()
	if err := s.CreateUser("owner", "Password1234!x", "", "", ""); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	_, live, err := s.AsActor(systemActor).CreateToken("owner", "live", nil, true)
	if err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}
	_, gone, err := s.AsActor(systemActor).CreateToken("owner", "gone", nil, true)
	if err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}
	if err := s.AsActor(systemActor).SetTokenAdminScope(gone.ID, []string{PermManageUsers}, true); err != nil {
		t.Fatalf("SetTokenAdminScope failed: %v", err)
	}
	if err := s.AsActor(systemActor).DeleteToken(strconv.FormatInt(gone.ID, 10), true); err != nil {
		t.Fatalf("DeleteToken failed: %v", err)
	}

	for name, err := range scopeReads(s, live.ID) {
		if err != nil {
			t.Errorf("%s on an existing token: unexpected error %v", name, err)
		}
	}
	for _, tokenID := range []int64{gone.ID, 999999} {
		for name, err := range scopeReads(s, tokenID) {
			if !errors.Is(err, ErrTokenNotFound) {
				t.Errorf("%s on missing token %d: expected ErrTokenNotFound, got %v",
					name, tokenID, err)
			}
		}
	}
}

// TestTokenScopeReadsFailWhenExistenceUnreadable checks that a failed
// existence check is reported as an error that is not ErrTokenNotFound,
// so that it still fails closed without claiming the token is gone.
func TestTokenScopeReadsFailWhenExistenceUnreadable(t *testing.T) {
	s, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()
	if err := s.db.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	err := s.requireTokenLocked(1)
	if err == nil || errors.Is(err, ErrTokenNotFound) {
		t.Fatalf("Expected a lookup error, got %v", err)
	}
}

// TestCheckerDeniesDeletedActingToken checks the access checks a handler
// makes for a token that was deleted after its request authenticated:
// each denies, as for a token whose scope cannot be read, where an empty
// scope would otherwise have read as unrestricted.
func TestCheckerDeniesDeletedActingToken(t *testing.T) {
	s, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()
	if err := s.CreateUser("root", "Password1234!x", "", "", ""); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if err := s.SetUserSuperuser("root", true); err != nil {
		t.Fatalf("SetUserSuperuser failed: %v", err)
	}
	userID, err := s.GetUserID("root")
	if err != nil {
		t.Fatalf("GetUserID failed: %v", err)
	}
	if _, err := s.RegisterMCPPrivilege("list_connections",
		MCPPrivilegeTypeTool, "", false); err != nil {
		t.Fatalf("RegisterMCPPrivilege failed: %v", err)
	}
	_, token, err := s.AsActor(systemActor).CreateToken("root", "self-deleting", nil, true)
	if err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}
	ctx := context.WithValue(context.Background(), UserIDContextKey, userID)
	ctx = context.WithValue(ctx, IsSuperuserContextKey, true)
	ctx = context.WithValue(ctx, IsAPITokenContextKey, true)
	ctx = context.WithValue(ctx, TokenIDContextKey, token.ID)
	rc := NewRBACChecker(s)

	if !rc.IsSuperuser(ctx) || !rc.TokenHoldsEverything(ctx) {
		t.Fatal("Expected the unscoped superuser token to hold everything")
	}
	if err := s.AsActor(systemActor).DeleteToken(strconv.FormatInt(token.ID, 10), true); err != nil {
		t.Fatalf("DeleteToken failed: %v", err)
	}

	checks := map[string]bool{
		"IsSuperuser":          rc.IsSuperuser(ctx),
		"TokenHoldsEverything": rc.TokenHoldsEverything(ctx),
		"HasAdminPermission":   rc.HasAdminPermission(ctx, PermManageUsers),
		"CanAccessMCPItem":     rc.CanAccessMCPItem(ctx, "list_connections"),
		"CanGrantMCPItem":      rc.CanGrantMCPItem(ctx, "list_connections"),
		"CanGrantAdminPermission": rc.CanGrantAdminPermission(ctx,
			PermManageBlackouts),
		"CanGrantConnection": rc.CanGrantConnection(ctx, 5,
			AccessLevelRead),
		"ConnectionInTokenScope": rc.ConnectionInTokenScope(ctx, 5),
	}
	if ok, _ := rc.CanAccessConnection(ctx, 5); ok {
		checks["CanAccessConnection"] = true
	}
	for name, allowed := range checks {
		if allowed {
			t.Errorf("%s allowed a deleted token", name)
		}
	}
}
