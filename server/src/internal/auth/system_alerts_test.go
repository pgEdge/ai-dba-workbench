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
	"testing"
)

// systemAlertCtx builds a request context with the given identity
// fields; a zero userID or tokenID leaves that key unset.
func systemAlertCtx(superuser, apiToken bool, userID, tokenID int64) context.Context {
	ctx := context.WithValue(context.Background(), IsSuperuserContextKey, superuser)
	if apiToken {
		ctx = context.WithValue(ctx, IsAPITokenContextKey, true)
	}
	if userID != 0 {
		ctx = context.WithValue(ctx, UserIDContextKey, userID)
	}
	if tokenID != 0 {
		ctx = context.WithValue(ctx, TokenIDContextKey, tokenID)
	}
	return ctx
}

// TestCanSeeSystemAlerts covers every branch of the gate, including the
// fail-closed ones.
func TestCanSeeSystemAlerts(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	if err := store.CreateUser("sysalertuser", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	_, token, err := store.AsActor(systemActor).CreateToken("sysalertuser", "system alert token", nil, true)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	checker := NewRBACChecker(store)

	tests := []struct {
		name    string
		checker *RBACChecker
		ctx     context.Context
		want    bool
	}{
		{"nil checker denies", nil, systemAlertCtx(true, false, 1, 0), false},
		{"no auth store denies", NewRBACChecker(nil), systemAlertCtx(true, false, 1, 0), false},
		{"token context without token ID denies", checker, systemAlertCtx(true, true, 1, 0), false},
		{"superuser allows", checker, systemAlertCtx(true, false, 1, 0), true},
		{"no user ID denies", checker, systemAlertCtx(false, false, 0, 0), false},
		{"session user allows", checker, systemAlertCtx(false, false, 1, 0), true},
		{"token with readable scope allows", checker, systemAlertCtx(false, true, 1, token.ID), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.checker.CanSeeSystemAlerts(tt.ctx); got != tt.want {
				t.Errorf("CanSeeSystemAlerts = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestCanSeeSystemAlerts_TokenScopeErrorDenies proves a token whose scope
// cannot be read is refused rather than let through.
func TestCanSeeSystemAlerts_TokenScopeErrorDenies(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)
	// Closing the store makes every scope read fail.
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if checker.CanSeeSystemAlerts(systemAlertCtx(false, true, 1, 7)) {
		t.Error("CanSeeSystemAlerts allowed a token whose scope read failed")
	}
}

// TestCanManageSystemAlerts covers who may acknowledge a system alert:
// a superuser or a manage_alert_rules holder, with a token bounded by
// its own admin scope, and nobody else.
func TestCanManageSystemAlerts(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	for _, name := range []string{"rulesadmin", "plainuser"} {
		if err := store.CreateUser(name, "Password1234", "", "", ""); err != nil {
			t.Fatalf("CreateUser(%s): %v", name, err)
		}
	}
	holderID, err := store.GetUserID("rulesadmin")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}
	plainID, err := store.GetUserID("plainuser")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}
	groupID, err := store.CreateGroup("alert-rule-admins", "")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := store.AddUserToGroup(groupID, holderID); err != nil {
		t.Fatalf("AddUserToGroup: %v", err)
	}
	if err := store.GrantAdminPermission(groupID, PermManageAlertRules); err != nil {
		t.Fatalf("GrantAdminPermission: %v", err)
	}

	newToken := func(owner string, adminScope []string) int64 {
		t.Helper()
		_, token, tokErr := store.AsActor(systemActor).CreateToken(owner, owner+" token", nil, true)
		if tokErr != nil {
			t.Fatalf("CreateToken: %v", tokErr)
		}
		if adminScope != nil {
			if scopeErr := store.AsActor(systemActor).SetTokenAdminScope(token.ID, adminScope, true); scopeErr != nil {
				t.Fatalf("SetTokenAdminScope: %v", scopeErr)
			}
		}
		return token.ID
	}
	grantedToken := newToken("rulesadmin", []string{PermManageAlertRules})
	withoutGrantToken := newToken("rulesadmin", []string{PermManageUsers})
	plainToken := newToken("plainuser", nil)

	checker := NewRBACChecker(store)
	tests := []struct {
		name    string
		checker *RBACChecker
		ctx     context.Context
		want    bool
	}{
		{"nil checker denies", nil, systemAlertCtx(true, false, 1, 0), false},
		{"no auth store denies", NewRBACChecker(nil), systemAlertCtx(true, false, 1, 0), false},
		{"token context without token ID denies", checker, systemAlertCtx(true, true, holderID, 0), false},
		{"superuser allows", checker, systemAlertCtx(true, false, 1, 0), true},
		{"no user ID denies", checker, systemAlertCtx(false, false, 0, 0), false},
		{"manage_alert_rules holder allows", checker, systemAlertCtx(false, false, holderID, 0), true},
		{"plain user denies", checker, systemAlertCtx(false, false, plainID, 0), false},
		{"token scoped to the grant allows", checker, systemAlertCtx(false, true, holderID, grantedToken), true},
		{"token scoped without the grant denies", checker, systemAlertCtx(false, true, holderID, withoutGrantToken), false},
		{"plain user token denies", checker, systemAlertCtx(false, true, plainID, plainToken), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.checker.CanManageSystemAlerts(tt.ctx); got != tt.want {
				t.Errorf("CanManageSystemAlerts = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestCanManageSystemAlerts_LookupErrorDenies proves a permission lookup
// that fails is refused rather than let through.
func TestCanManageSystemAlerts_LookupErrorDenies(t *testing.T) {
	store, cleanup := createTestAuthStoreForAccess(t)
	defer cleanup()

	checker := NewRBACChecker(store)
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if checker.CanManageSystemAlerts(systemAlertCtx(false, false, 1, 0)) {
		t.Error("CanManageSystemAlerts allowed a caller whose permissions could not be read")
	}
}
