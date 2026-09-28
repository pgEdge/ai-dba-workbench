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
	_, token, err := store.CreateToken("sysalertuser", "system alert token", nil)
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
		{"no auth store allows", NewRBACChecker(nil), context.Background(), true},
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
