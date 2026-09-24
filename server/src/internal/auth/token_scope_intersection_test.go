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
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

// Connection A is the one the token is scoped to; connection B is the one
// it must never reach, whichever route it tries.
const (
	scopedConnA = 101
	scopedConnB = 202
)

// scopeFixture is the shared setup for the token-scope intersection
// tests: one non-superuser user, one API token scoped to connection A at
// read, and an RBACChecker over the same store.
type scopeFixture struct {
	store   *AuthStore
	checker *RBACChecker
	dir     string
	userID  int64
	tokenID int64
}

// newScopeFixture builds the fixture. scopeLevel is the access level the
// token is granted over connection A.
func newScopeFixture(t *testing.T, scopeLevel string) *scopeFixture {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "auth-scope-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	store, err := NewAuthStore(tmpDir, 0, 0, AuditKeyForTesting())
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create auth store: %v", err)
	}
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)
	t.Cleanup(func() {
		store.Close()
		os.RemoveAll(tmpDir)
	})

	if err := store.CreateUser("scopeuser", "Testpass1234", "", "", ""); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	user, err := store.GetUser("scopeuser")
	if err != nil {
		t.Fatalf("failed to fetch user: %v", err)
	}
	_, stored, err := store.CreateToken("scopeuser", "scoped token", nil)
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}
	if err := store.SetTokenConnectionScope(stored.ID, []ScopedConnection{
		{ConnectionID: scopedConnA, AccessLevel: scopeLevel},
	}); err != nil {
		t.Fatalf("failed to set token connection scope: %v", err)
	}

	return &scopeFixture{
		store:   store,
		checker: NewRBACChecker(store),
		dir:     tmpDir,
		userID:  user.ID,
		tokenID: stored.ID,
	}
}

// tokenContext builds the request context AuthenticateRequest would
// produce for the fixture's API token, including the username that the
// convergence made available on every surface.
func (f *scopeFixture) tokenContext() context.Context {
	ctx := context.WithValue(context.Background(), IsAPITokenContextKey, true)
	ctx = context.WithValue(ctx, TokenIDContextKey, f.tokenID)
	ctx = context.WithValue(ctx, UserIDContextKey, f.userID)
	ctx = context.WithValue(ctx, IsSuperuserContextKey, false)
	return context.WithValue(ctx, UsernameContextKey, "scopeuser")
}

// ownedBy makes every connection look owned by the fixture's user and
// unshared, which is the arrangement that previously let a scoped token
// reach an out-of-scope connection.
func ownedBy(username string) ConnectionSharingLookupFunc {
	return func(_ context.Context, _ int) (bool, string, error) {
		return false, username, nil
	}
}

// TestTokenScopeDeniesGroupGrantedConnection covers the route through
// the group-restricted branch: the user's group grants connection B, but
// the token was not issued for it.
func TestTokenScopeDeniesGroupGrantedConnection(t *testing.T) {
	f := newScopeFixture(t, AccessLevelRead)

	groupID, err := f.store.CreateGroup("scope-group", "group")
	if err != nil {
		t.Fatalf("failed to create group: %v", err)
	}
	if err := f.store.AddUserToGroup(groupID, f.userID); err != nil {
		t.Fatalf("failed to add user to group: %v", err)
	}
	for _, connID := range []int{scopedConnA, scopedConnB} {
		if err := f.store.GrantConnectionPrivilege(groupID, connID, AccessLevelReadWrite); err != nil {
			t.Fatalf("failed to grant connection privilege: %v", err)
		}
	}

	ctx := f.tokenContext()

	if canAccess, level := f.checker.CanAccessConnection(ctx, scopedConnB); canAccess {
		t.Errorf("out-of-scope connection %d granted at %q, want denial", scopedConnB, level)
	}

	// In-scope, but the token's read ceiling must survive the group's
	// read_write grant.
	canAccess, level := f.checker.CanAccessConnection(ctx, scopedConnA)
	if !canAccess {
		t.Fatalf("in-scope connection %d denied", scopedConnA)
	}
	if level != AccessLevelRead {
		t.Errorf("access level = %q, want %q (the token's ceiling, not the group's grant)",
			level, AccessLevelRead)
	}
	if f.checker.HasWriteAccess(ctx, scopedConnA) {
		t.Error("HasWriteAccess = true for a read-scoped token")
	}
}

// TestTokenScopeDeniesSharedConnection covers the route through the
// ungrouped branch by way of the is_shared flag.
func TestTokenScopeDeniesSharedConnection(t *testing.T) {
	f := newScopeFixture(t, AccessLevelRead)
	f.checker.SetConnectionSharingLookup(
		func(_ context.Context, _ int) (bool, string, error) {
			return true, "someone-else", nil
		})

	ctx := f.tokenContext()

	if canAccess, level := f.checker.CanAccessConnection(ctx, scopedConnB); canAccess {
		t.Errorf("out-of-scope shared connection %d granted at %q, want denial", scopedConnB, level)
	}
	canAccess, level := f.checker.CanAccessConnection(ctx, scopedConnA)
	if !canAccess || level != AccessLevelRead {
		t.Errorf("in-scope shared connection = (%v, %q), want (true, %q)",
			canAccess, level, AccessLevelRead)
	}
}

// TestTokenScopeDeniesOwnedUngroupedConnection is the regression test for
// the escalation itself: connection B belongs to the token's own owner
// and is in no group, so ownership alone used to return read_write.
func TestTokenScopeDeniesOwnedUngroupedConnection(t *testing.T) {
	f := newScopeFixture(t, AccessLevelRead)
	f.checker.SetConnectionSharingLookup(ownedBy("scopeuser"))

	ctx := f.tokenContext()

	if canAccess, level := f.checker.CanAccessConnection(ctx, scopedConnB); canAccess {
		t.Errorf("out-of-scope owned connection %d granted at %q, want denial", scopedConnB, level)
	}

	canAccess, level := f.checker.CanAccessConnection(ctx, scopedConnA)
	if !canAccess {
		t.Fatalf("in-scope owned connection %d denied", scopedConnA)
	}
	if level != AccessLevelRead {
		t.Errorf("access level = %q, want %q: ownership must not lift the token ceiling",
			level, AccessLevelRead)
	}
}

// TestTokenScopeReadWriteCeilingOnOwnedConnection checks the ceiling is a
// ceiling and not a fixed value: a read_write-scoped token keeps
// read_write on its own ungrouped connection.
func TestTokenScopeReadWriteCeilingOnOwnedConnection(t *testing.T) {
	f := newScopeFixture(t, AccessLevelReadWrite)
	f.checker.SetConnectionSharingLookup(ownedBy("scopeuser"))

	ctx := f.tokenContext()

	canAccess, level := f.checker.CanAccessConnection(ctx, scopedConnA)
	if !canAccess || level != AccessLevelReadWrite {
		t.Errorf("in-scope connection = (%v, %q), want (true, %q)",
			canAccess, level, AccessLevelReadWrite)
	}
}

// TestUnscopedTokenKeepsOwnedConnection guards against over-correction:
// a token with no connection scope at all must still reach its owner's
// ungrouped connections exactly as before.
func TestUnscopedTokenKeepsOwnedConnection(t *testing.T) {
	f := newScopeFixture(t, AccessLevelRead)
	if err := f.store.SetTokenConnectionScope(f.tokenID, nil); err != nil {
		t.Fatalf("failed to clear token connection scope: %v", err)
	}
	f.checker.SetConnectionSharingLookup(ownedBy("scopeuser"))

	ctx := f.tokenContext()

	canAccess, level := f.checker.CanAccessConnection(ctx, scopedConnB)
	if !canAccess || level != AccessLevelReadWrite {
		t.Errorf("unscoped token = (%v, %q), want (true, %q)",
			canAccess, level, AccessLevelReadWrite)
	}
}

// TestSessionCallerKeepsOwnedConnection guards the other half: a session
// caller has no token ID, and must be unaffected by the intersection.
func TestSessionCallerKeepsOwnedConnection(t *testing.T) {
	f := newScopeFixture(t, AccessLevelRead)
	f.checker.SetConnectionSharingLookup(ownedBy("scopeuser"))

	ctx := context.WithValue(context.Background(), IsAPITokenContextKey, false)
	ctx = context.WithValue(ctx, UserIDContextKey, f.userID)
	ctx = context.WithValue(ctx, UsernameContextKey, "scopeuser")

	canAccess, level := f.checker.CanAccessConnection(ctx, scopedConnB)
	if !canAccess || level != AccessLevelReadWrite {
		t.Errorf("session caller = (%v, %q), want (true, %q)",
			canAccess, level, AccessLevelReadWrite)
	}
}

// stubLister enumerates a fixed set of connections for
// VisibleConnectionIDs.
type stubLister struct {
	conns []ConnectionVisibilityInfo
	err   error
}

func (s *stubLister) GetAllConnections(context.Context) ([]ConnectionVisibilityInfo, error) {
	return s.conns, s.err
}

// TestVisibleConnectionIDsIntersectsTokenScope covers finding 2: the
// owner and shared branches must not enumerate connections outside the
// token's scope.
func TestVisibleConnectionIDsIntersectsTokenScope(t *testing.T) {
	f := newScopeFixture(t, AccessLevelRead)

	lister := &stubLister{conns: []ConnectionVisibilityInfo{
		{ID: scopedConnA, OwnerUsername: "scopeuser"},
		{ID: scopedConnB, OwnerUsername: "scopeuser"},
		{ID: 303, IsShared: true, OwnerUsername: "someone-else"},
	}}

	ids, all, err := f.checker.VisibleConnectionIDs(f.tokenContext(), lister)
	if err != nil {
		t.Fatalf("VisibleConnectionIDs: %v", err)
	}
	if all {
		t.Fatal("allConnections = true for a scoped token")
	}
	sort.Ints(ids)
	if len(ids) != 1 || ids[0] != scopedConnA {
		t.Errorf("visible ids = %v, want [%d]", ids, scopedConnA)
	}
}

// TestVisibleConnectionIDsUnscopedTokenSeesOwned is the matching
// over-correction guard for VisibleConnectionIDs.
func TestVisibleConnectionIDsUnscopedTokenSeesOwned(t *testing.T) {
	f := newScopeFixture(t, AccessLevelRead)
	if err := f.store.SetTokenConnectionScope(f.tokenID, nil); err != nil {
		t.Fatalf("failed to clear token connection scope: %v", err)
	}

	lister := &stubLister{conns: []ConnectionVisibilityInfo{
		{ID: scopedConnA, OwnerUsername: "scopeuser"},
		{ID: scopedConnB, OwnerUsername: "scopeuser"},
	}}

	ids, all, err := f.checker.VisibleConnectionIDs(f.tokenContext(), lister)
	if err != nil {
		t.Fatalf("VisibleConnectionIDs: %v", err)
	}
	if all {
		t.Fatal("allConnections = true, want an explicit list")
	}
	sort.Ints(ids)
	if len(ids) != 2 || ids[0] != scopedConnA || ids[1] != scopedConnB {
		t.Errorf("visible ids = %v, want [%d %d]", ids, scopedConnA, scopedConnB)
	}
}

// TestVisibleConnectionIDsListerError checks the error path still
// surfaces rather than silently returning a partial list.
func TestVisibleConnectionIDsListerError(t *testing.T) {
	f := newScopeFixture(t, AccessLevelRead)

	wantErr := errors.New("lister exploded")
	_, _, err := f.checker.VisibleConnectionIDs(f.tokenContext(), &stubLister{err: wantErr})
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}

// TestIncompleteTokenContextFailsClosed covers finding 4: a context that
// claims API-token authentication but carries no token ID cannot have its
// scope evaluated, so every gate must deny rather than fall through to
// the permissive "no token, no scope" default.
func TestIncompleteTokenContextFailsClosed(t *testing.T) {
	f := newScopeFixture(t, AccessLevelRead)
	f.checker.SetConnectionSharingLookup(ownedBy("scopeuser"))

	if _, err := f.store.RegisterMCPPrivilege(
		"public_tool", MCPPrivilegeTypeTool, "Public", true); err != nil {
		t.Fatalf("failed to register privilege: %v", err)
	}

	// Superuser is deliberately true: an incomplete token context must
	// fail closed even then, because the claim cannot be trusted when
	// the credential it came from cannot be identified.
	ctx := context.WithValue(context.Background(), IsAPITokenContextKey, true)
	ctx = context.WithValue(ctx, UserIDContextKey, f.userID)
	ctx = context.WithValue(ctx, IsSuperuserContextKey, true)
	ctx = context.WithValue(ctx, UsernameContextKey, "scopeuser")

	if canAccess, _ := f.checker.CanAccessConnection(ctx, scopedConnA); canAccess {
		t.Error("CanAccessConnection granted access for a token context with no token ID")
	}
	if f.checker.CanAccessMCPItem(ctx, "public_tool") {
		t.Error("CanAccessMCPItem granted access for a token context with no token ID")
	}
	if f.checker.HasAdminPermission(ctx, PermManageUsers) {
		t.Error("HasAdminPermission granted permission for a token context with no token ID")
	}
	ids, all, err := f.checker.VisibleConnectionIDs(ctx, &stubLister{
		conns: []ConnectionVisibilityInfo{{ID: scopedConnA, OwnerUsername: "scopeuser"}},
	})
	if err != nil {
		t.Fatalf("VisibleConnectionIDs: %v", err)
	}
	if all || len(ids) != 0 {
		t.Errorf("VisibleConnectionIDs = (%v, %v), want (nil, false)", ids, all)
	}
}

// TestTokenScopeLookupErrorDeniesAccess locks in the fail-closed
// behavior when the scope lookup itself fails. The table is dropped
// through a second connection to the store's SQLite file, because a
// scope query that errors whilst the surrounding queries succeed cannot
// be produced through the store's own API.
func TestTokenScopeLookupErrorDeniesAccess(t *testing.T) {
	f := newScopeFixture(t, AccessLevelRead)
	f.checker.SetConnectionSharingLookup(ownedBy("scopeuser"))

	db, err := sql.Open("sqlite", filepath.Join(f.dir, "auth.db"))
	if err != nil {
		t.Fatalf("failed to open auth database: %v", err)
	}
	defer db.Close()
	// Couples to the token_connection_scope table name. RBACChecker holds
	// a concrete *AuthStore with no seam to stub, so this is the only way
	// in; if the schema moves, give the checker a narrow scope-lookup
	// interface and stub it here rather than deleting the test.
	if _, err := db.Exec("DROP TABLE token_connection_scope"); err != nil {
		t.Fatalf("failed to drop the scope table: %v", err)
	}

	ctx := f.tokenContext()

	if canAccess, _ := f.checker.CanAccessConnection(ctx, scopedConnA); canAccess {
		t.Error("CanAccessConnection granted access despite a failed scope lookup")
	}
	if _, _, err := f.checker.VisibleConnectionIDs(ctx, &stubLister{
		conns: []ConnectionVisibilityInfo{{ID: scopedConnA, OwnerUsername: "scopeuser"}},
	}); err == nil {
		t.Error("VisibleConnectionIDs returned no error despite a failed scope lookup")
	}
}
