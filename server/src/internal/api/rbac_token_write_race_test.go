/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package api

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// =============================================================================
// Token writes racing the checks made before them (issue #522 review)
//
// A token write is checked before its transaction opens, so these tests
// change what the checks read between the check and the write, through
// tokenWriteChecked, as a concurrent request could. None runs in
// parallel, because tokenWriteChecked is package state.
// =============================================================================

// setTokenWriteChecked installs hook for the rest of the test.
func setTokenWriteChecked(t *testing.T, hook func()) {
	t.Helper()
	tokenWriteChecked = hook
	t.Cleanup(func() { tokenWriteChecked = nil })
}

// auditTotal returns the number of audit rows recorded so far.
func auditTotal(t *testing.T, store *auth.AuthStore) int {
	t.Helper()
	_, total, err := store.ListAuditEvents(auth.AuditFilter{Limit: 1})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	return total
}

// TestTokenScopeWriteConflictsWithConcurrentChange checks that a scope
// write by a token, approved by its ceiling against the target's scope
// as it stood, is answered 409 when that scope is narrowed before the
// write, rather than writing back what the narrowing removed; that the
// refusal leaves the narrowed scope and one denied audit row; and that
// a retry is judged afresh. A session has no ceiling, so nothing is
// judged and its write proceeds as before.
func TestTokenScopeWriteConflictsWithConcurrentChange(t *testing.T) {
	const keep = `{"admin_permissions":["manage_users","manage_token_scopes"]}`
	narrowed := []string{auth.PermManageUsers}

	f, cleanup := newOwnershipFixture(t)
	defer cleanup()
	narrow := func() {
		if err := f.store.AsActor(auth.SystemActor()).SetTokenAdminScope(
			f.own, narrowed, true); err != nil {
			t.Errorf("SetTokenAdminScope failed: %v", err)
		}
	}
	setTokenWriteChecked(t, narrow)
	before := auditTotal(t, f.store)

	rec := tokenRouteRequest(f.handler, http.MethodPut,
		tokenPathOf(f.own)+"/scope", keep, f.callers()["token"])
	assertStatus(t, rec, http.StatusConflict)
	assertErrorMessage(t, rec, refuseTokenScopeChanged)
	if got := adminScopeOf(t, f.store, f.own); !reflect.DeepEqual(got,
		narrowed) {
		t.Errorf("Expected the narrowed scope %v to stand, got %v",
			narrowed, got)
	}
	events, after, err := f.store.ListAuditEvents(auth.AuditFilter{Limit: 2})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	// One row for the narrowing, one for the refusal.
	if after-before != 2 || events[0].Outcome != auth.OutcomeDenied ||
		events[0].Action != "token.scope.set" {
		t.Errorf("Expected the narrowing and one denied token.scope.set row, "+
			"got %d rows, newest %s %q", after-before, events[0].Action,
			events[0].Outcome)
	}

	// Retried with nothing in between, the write is judged against the
	// narrowed scope, which carol's own token may widen back.
	tokenWriteChecked = nil
	assertStatus(t, tokenRouteRequest(f.handler, http.MethodPut,
		tokenPathOf(f.own)+"/scope", keep, f.callers()["token"]),
		http.StatusNoContent)

	// A session is not bounded by a ceiling, so it judges no snapshot
	// and its write is not refused by the narrowing.
	setTokenWriteChecked(t, narrow)
	assertStatus(t, tokenRouteRequest(f.handler, http.MethodPut,
		tokenPathOf(f.own)+"/scope", keep, f.callers()["session"]),
		http.StatusNoContent)
	if got := adminScopeOf(t, f.store, f.own); len(got) != 2 {
		t.Errorf("Expected the session's write to land, got %v", got)
	}
}

// TestTokenCreateRefusedWhenOwnerReplaced checks that a token minted for
// the caller's own name is refused, 403 as for another user, when the
// account is deleted and a new one created under that name between the
// handler's owner check and the insert: the store checks the owner id
// the insert's own transaction reads, and the new account is not the
// caller.
func TestTokenCreateRefusedWhenOwnerReplaced(t *testing.T) {
	f, cleanup := newOwnershipFixture(t)
	defer cleanup()
	setTokenWriteChecked(t, func() {
		if err := f.store.DeleteUser("carol"); err != nil {
			t.Errorf("DeleteUser failed: %v", err)
		}
		if err := f.store.CreateUser("carol", "Password1234", "", "",
			""); err != nil {
			t.Errorf("CreateUser failed: %v", err)
		}
	})

	assertRefusedWith(t, tokenRouteRequest(f.handler, http.MethodPost,
		"/api/v1/rbac/tokens/", `{"owner_username":"carol"}`,
		f.callers()["session"]), refuseTokenForOtherOwner)
	carol, err := f.store.GetUser("carol")
	if err != nil || carol == nil {
		t.Fatalf("GetUser failed: %v", err)
	}
	tokens, err := f.store.ListAllTokens()
	if err != nil {
		t.Fatalf("ListAllTokens failed: %v", err)
	}
	for _, tk := range tokens {
		if tk.OwnerID == carol.ID {
			t.Errorf("Expected no token for the new carol, found %d", tk.ID)
		}
	}
}
