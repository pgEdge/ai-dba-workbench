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
	"reflect"
	"testing"
)

// ownerTokenCtx presents the fixture's token acting on its own behalf,
// as an API token owned by "owner", who is not a superuser.
func (f *ownerGuardFixture) ownerTokenCtx() context.Context {
	ctx := context.WithValue(context.Background(), IsAPITokenContextKey, true)
	ctx = context.WithValue(ctx, TokenIDContextKey, f.tokenID)
	ctx = context.WithValue(ctx, UserIDContextKey, f.ownerID)
	return context.WithValue(ctx, UsernameContextKey, "owner")
}

// judge runs the ceiling check a handler runs before a scope write, as
// the fixture's token acting on itself, and returns the snapshot the
// write is to be given. clear judges clearing the scope instead of
// change.
func (f *ownerGuardFixture) judge(t *testing.T, change TokenScopeChange,
	clear bool) *TokenScopeSnapshot {

	t.Helper()
	checker := NewRBACChecker(f.store)
	var allowed bool
	var snap *TokenScopeSnapshot
	var err error
	if clear {
		allowed, snap, err = checker.JudgeTokenScopeClear(f.ownerTokenCtx(),
			f.tokenID, &stubVisibilityLister{})
	} else {
		allowed, snap, err = checker.JudgeTokenScopeChange(f.ownerTokenCtx(),
			f.tokenID, change)
	}
	if err != nil || !allowed || snap == nil {
		t.Fatalf("judge: allowed=%v snapshot=%v err=%v; want an allowed "+
			"change with a snapshot", allowed, snap, err)
	}
	return snap
}

// TestTokenScopeWriteRefusedWhenScopeChanged checks the race the
// snapshot closes (VULN-001 in the #522 review): a change the ceiling
// approved against the scope as it stood is refused, with
// ErrTokenScopeChanged, once the scope has changed underneath it, so
// that a token cannot write back what an administrator removed between
// the check and the write. The refusal changes nothing and leaves no
// failure row, the handler's denial being the only record of it.
func TestTokenScopeWriteRefusedWhenScopeChanged(t *testing.T) {
	// Each interleaving changes one part of the stored rows after the
	// check and before the write.
	interleavings := []struct {
		name  string
		apply func(t *testing.T, f *ownerGuardFixture)
	}{
		{"connection dropped", func(t *testing.T, f *ownerGuardFixture) {
			f.write(t, TokenScopeChange{Connections: []ScopedConnection{
				{ConnectionID: 5, AccessLevel: AccessLevelReadWrite}}})
		}},
		{"connection level lowered", func(t *testing.T, f *ownerGuardFixture) {
			f.write(t, TokenScopeChange{Connections: []ScopedConnection{
				{ConnectionID: 5, AccessLevel: AccessLevelRead},
				{ConnectionID: 7, AccessLevel: AccessLevelRead}}})
		}},
		{"MCP item dropped", func(t *testing.T, f *ownerGuardFixture) {
			f.write(t, TokenScopeChange{MCPPrivileges: []string{"tool_a"}})
		}},
		{"orphaned MCP row added", func(t *testing.T, f *ownerGuardFixture) {
			// A row naming no identifier is invisible to the joins that
			// yield names, but it is still a change to the stored rows.
			if _, err := f.store.db.Exec(`INSERT INTO token_mcp_scope
                (token_id, privilege_identifier_id) VALUES (?, ?)`,
				f.tokenID, 987654); err != nil {
				t.Fatalf("insert orphaned MCP row failed: %v", err)
			}
		}},
		{"admin permission dropped", func(t *testing.T, f *ownerGuardFixture) {
			f.write(t, TokenScopeChange{
				AdminPermissions: []string{PermManageUsers}})
		}},
		{"scope cleared", func(t *testing.T, f *ownerGuardFixture) {
			if err := f.store.AsActor(systemActor).ClearTokenScope(f.tokenID,
				true, AnyTokenOwner(), nil); err != nil {
				t.Fatalf("ClearTokenScope failed: %v", err)
			}
		}},
	}
	for _, il := range interleavings {
		t.Run(il.name, func(t *testing.T) {
			f, cleanup := newOwnerGuardFixture(t)
			defer cleanup()
			// Rewriting the scope it holds is a change the token may make
			// on itself, so the ceiling approves it; the write that
			// follows would restore whatever the interleaving removed.
			snap := f.judge(t, restrictedChange(), false)
			il.apply(t, f)
			before := f.scope(t)
			audits := f.auditCount(t)

			err := f.store.AsActor(testActor()).SetTokenScope(f.tokenID,
				restrictedChange(), true, TokensOwnedBy(f.ownerID), snap)
			if !errors.Is(err, ErrTokenScopeChanged) {
				t.Fatalf("Expected ErrTokenScopeChanged, got %v", err)
			}
			if after := f.scope(t); !reflect.DeepEqual(before, after) {
				t.Errorf("Expected the scope to be untouched:\n"+
					" before %+v\n after  %+v", before, after)
			}
			if got := f.auditCount(t); got != audits {
				t.Errorf("Expected no audit row for the refusal, got %d new",
					got-audits)
			}
		})
	}
}

// write applies change to the fixture's token as the system, which no
// guard restricts.
func (f *ownerGuardFixture) write(t *testing.T, change TokenScopeChange) {
	t.Helper()
	if err := f.store.AsActor(systemActor).SetTokenScope(f.tokenID, change,
		true, AnyTokenOwner(), nil); err != nil {
		t.Fatalf("SetTokenScope failed: %v", err)
	}
}

// TestTokenScopeClearRefusedWhenScopeChanged checks that a clear judged
// against one scope is refused once the scope has changed, and allowed
// while it has not.
func TestTokenScopeClearRefusedWhenScopeChanged(t *testing.T) {
	f, cleanup := newOwnerGuardFixture(t)
	defer cleanup()
	// The owner reaches exactly the connections her token is scoped to,
	// at the same levels, and nothing else, so clearing her token's
	// scope is within its ceiling.
	groupID, err := f.store.CreateGroup("owner-conns", "")
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}
	if err := f.store.AddUserToGroup(groupID, f.ownerID); err != nil {
		t.Fatalf("AddUserToGroup failed: %v", err)
	}
	for id, level := range map[int]string{5: AccessLevelReadWrite,
		7: AccessLevelRead} {
		if err := f.store.GrantConnectionPrivilege(groupID, id,
			level); err != nil {
			t.Fatalf("GrantConnectionPrivilege failed: %v", err)
		}
	}
	snap := f.judge(t, TokenScopeChange{}, true)
	f.write(t, TokenScopeChange{AdminPermissions: []string{PermManageUsers}})
	before := f.scope(t)
	err = f.store.AsActor(testActor()).ClearTokenScope(f.tokenID, true,
		TokensOwnedBy(f.ownerID), snap)
	if !errors.Is(err, ErrTokenScopeChanged) {
		t.Fatalf("Expected ErrTokenScopeChanged, got %v", err)
	}
	if after := f.scope(t); !reflect.DeepEqual(before, after) {
		t.Errorf("Expected the scope to be untouched, got %+v", after)
	}

	snap = f.judge(t, TokenScopeChange{}, true)
	if err := f.store.AsActor(testActor()).ClearTokenScope(f.tokenID, true,
		TokensOwnedBy(f.ownerID), snap); err != nil {
		t.Fatalf("Expected the clear to pass on an unchanged scope, got %v",
			err)
	}
	if scope := f.scope(t); scope != nil {
		t.Errorf("Expected the scope to be cleared, got %+v", scope)
	}
}

// TestTokenScopeWriteProceedsOnUnchangedScope checks that a snapshot
// still matching the stored scope lets the write through, as nil does
// for a write no ceiling judged, and that a write the store itself
// makes in between (the token's own last_used_at, say) is not a change.
func TestTokenScopeWriteProceedsOnUnchangedScope(t *testing.T) {
	narrowed := TokenScopeChange{AdminPermissions: []string{PermManageUsers}}
	for name, snapOf := range map[string]func(*ownerGuardFixture,
		*testing.T) *TokenScopeSnapshot{
		"judged": func(f *ownerGuardFixture, t *testing.T) *TokenScopeSnapshot {
			return f.judge(t, narrowed, false)
		},
		"unjudged": func(*ownerGuardFixture, *testing.T) *TokenScopeSnapshot {
			return nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			f, cleanup := newOwnerGuardFixture(t)
			defer cleanup()
			snap := snapOf(f, t)
			// Rewriting the same scope leaves the rows as they were.
			f.write(t, restrictedChange())
			if err := f.store.AsActor(testActor()).SetTokenScope(f.tokenID,
				narrowed, true, TokensOwnedBy(f.ownerID), snap); err != nil {
				t.Fatalf("SetTokenScope failed: %v", err)
			}
			if got := f.scope(t).AdminPermissions; !reflect.DeepEqual(got,
				[]string{PermManageUsers}) {
				t.Errorf("Expected admin scope [%s], got %v",
					PermManageUsers, got)
			}
		})
	}
}

// TestJudgeTokenScopeSnapshotOnlyWhenJudged checks that the judging
// checks return a snapshot only for an allowed token change: a session
// has no ceiling, so nothing to compare, and a refused change is never
// written.
func TestJudgeTokenScopeSnapshotOnlyWhenJudged(t *testing.T) {
	f, cleanup := newOwnerGuardFixture(t)
	defer cleanup()
	checker := NewRBACChecker(f.store)
	session := context.WithValue(context.Background(), UserIDContextKey,
		f.ownerID)
	widen := TokenScopeChange{AdminPermissions: []string{
		AdminPermissionWildcard}}

	cases := []struct {
		name        string
		judge       func() (bool, *TokenScopeSnapshot, error)
		wantAllowed bool
	}{
		{"session change", func() (bool, *TokenScopeSnapshot, error) {
			return checker.JudgeTokenScopeChange(session, f.tokenID, widen)
		}, true},
		{"session clear", func() (bool, *TokenScopeSnapshot, error) {
			return checker.JudgeTokenScopeClear(session, f.tokenID, nil)
		}, true},
		{"token widening itself", func() (bool, *TokenScopeSnapshot, error) {
			return checker.JudgeTokenScopeChange(f.ownerTokenCtx(), f.tokenID,
				widen)
		}, false},
		{"incomplete token", func() (bool, *TokenScopeSnapshot, error) {
			return checker.JudgeTokenScopeChange(incompleteTokenCtx(),
				f.tokenID, widen)
		}, false},
		{"incomplete token clear", func() (bool, *TokenScopeSnapshot, error) {
			return checker.JudgeTokenScopeClear(incompleteTokenCtx(),
				f.tokenID, nil)
		}, false},
		{"unknown token", func() (bool, *TokenScopeSnapshot, error) {
			return checker.JudgeTokenScopeClear(f.ownerTokenCtx(), 99999, nil)
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			allowed, snap, err := tc.judge()
			if err != nil {
				t.Fatalf("judge failed: %v", err)
			}
			if allowed != tc.wantAllowed || snap != nil {
				t.Errorf("Expected allowed=%v with no snapshot, got %v, %v",
					tc.wantAllowed, allowed, snap)
			}
		})
	}
}

// TestJudgeTokenScopeClearRefusedBeyondCeiling checks that a clear the
// ceiling refuses returns no snapshot: a token whose owner reaches more
// than the token may not clear itself.
func TestJudgeTokenScopeClearRefusedBeyondCeiling(t *testing.T) {
	f, cleanup := newOwnerGuardFixture(t)
	defer cleanup()
	groupID, err := f.store.CreateGroup("owner-admins", "")
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}
	if err := f.store.AddUserToGroup(groupID, f.ownerID); err != nil {
		t.Fatalf("AddUserToGroup failed: %v", err)
	}
	if err := f.store.GrantAdminPermission(groupID,
		PermManageConnections); err != nil {
		t.Fatalf("GrantAdminPermission failed: %v", err)
	}
	allowed, snap, err := NewRBACChecker(f.store).JudgeTokenScopeClear(
		f.ownerTokenCtx(), f.tokenID, nil)
	if err != nil || allowed || snap != nil {
		t.Errorf("Expected a refused clear with no snapshot, got %v, %v, %v",
			allowed, snap, err)
	}
}

// TestGuardJudgedTokenScopeTxReadFailures checks that a stored scope
// the write's transaction cannot read refuses the write with that error
// rather than letting it through or calling it a change.
func TestGuardJudgedTokenScopeTxReadFailures(t *testing.T) {
	// Each statement is a literal, so that no SQL is built from a
	// variable.
	drops := map[string]string{
		"token_connection_scope": "DROP TABLE token_connection_scope",
		"token_mcp_scope":        "DROP TABLE token_mcp_scope",
		"token_admin_scope":      "DROP TABLE token_admin_scope",
	}
	for table, drop := range drops {
		t.Run(table, func(t *testing.T) {
			f, cleanup := newOwnerGuardFixture(t)
			defer cleanup()
			snap := f.judge(t, restrictedChange(), false)
			tx, err := f.store.db.Begin()
			if err != nil {
				t.Fatalf("Begin failed: %v", err)
			}
			//nolint:errcheck // The test only reads inside the transaction.
			defer tx.Rollback()
			if _, err := tx.Exec(drop); err != nil {
				t.Fatalf("DROP TABLE failed: %v", err)
			}
			err = guardJudgedTokenScopeTx(tx, f.tokenID, snap)
			if err == nil || errors.Is(err, ErrTokenScopeChanged) {
				t.Errorf("Expected a read error, got %v", err)
			}
		})
	}
}

// TestCreateTokenForChecksOwnerInTransaction checks VULN-002 in the
// #522 review: the owner of a new token is checked against the user the
// caller acts as on the owner row the insert's own transaction reads,
// so that a user recreated under the name after a handler looked it up
// does not receive the token. A refusal inserts nothing and leaves no
// failure row.
func TestCreateTokenForChecksOwnerInTransaction(t *testing.T) {
	cases := []struct {
		name    string
		guard   func(f *ownerGuardFixture) TokenOwnerGuard
		refused bool
	}{
		{"own user", func(f *ownerGuardFixture) TokenOwnerGuard {
			return TokensOwnedBy(f.ownerID)
		}, false},
		{"any owner", func(*ownerGuardFixture) TokenOwnerGuard {
			return AnyTokenOwner()
		}, false},
		{"another user", func(f *ownerGuardFixture) TokenOwnerGuard {
			return TokensOwnedBy(f.otherID)
		}, true},
		{"zero guard", func(*ownerGuardFixture) TokenOwnerGuard {
			return TokenOwnerGuard{}
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, cleanup := newOwnerGuardFixture(t)
			defer cleanup()
			tokens := func() int {
				all, err := f.store.ListAllTokens()
				if err != nil {
					t.Fatalf("ListAllTokens failed: %v", err)
				}
				return len(all)
			}
			beforeTokens, beforeAudit := tokens(), f.auditCount(t)

			raw, stored, err := f.store.AsActor(testActor()).CreateTokenFor(
				"owner", "owner check", nil, false, tc.guard(f))
			if !tc.refused {
				if err != nil || raw == "" || stored == nil {
					t.Fatalf("Expected a token, got %v", err)
				}
				return
			}
			if !errors.Is(err, ErrTokenNotOwned) || raw != "" || stored != nil {
				t.Fatalf("Expected ErrTokenNotOwned and no token, got %q, %v, %v",
					raw, stored, err)
			}
			if got := tokens(); got != beforeTokens {
				t.Errorf("Expected no token inserted, %d before and %d after",
					beforeTokens, got)
			}
			if got := f.auditCount(t); got != beforeAudit {
				t.Errorf("Expected no audit row for the refusal, got %d new",
					got-beforeAudit)
			}
		})
	}
}
