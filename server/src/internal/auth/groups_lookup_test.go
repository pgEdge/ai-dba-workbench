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
	"testing"
)

// groupLookupFixture creates a user in "alpha", and "alpha" inside
// "beta", so that the user belongs to both groups, plus an empty group
// "gamma".
type groupLookupFixture struct {
	store  *AuthStore
	userID int64
	alpha  int64
	beta   int64
	gamma  int64
}

func newGroupLookupFixture(t *testing.T) (*groupLookupFixture, func()) {
	t.Helper()
	store, cleanup := createTestAuthStoreForGroups(t)
	f := &groupLookupFixture{store: store}
	if err := store.CreateUser("lookup-user", "Password1234", "", "",
		""); err != nil {
		cleanup()
		t.Fatalf("CreateUser failed: %v", err)
	}
	var err error
	if f.userID, err = store.GetUserID("lookup-user"); err != nil {
		cleanup()
		t.Fatalf("GetUserID failed: %v", err)
	}
	for _, g := range []struct {
		name string
		id   *int64
	}{{"alpha", &f.alpha}, {"beta", &f.beta}, {"gamma", &f.gamma}} {
		if *g.id, err = store.CreateGroup(g.name, g.name+" group"); err != nil {
			cleanup()
			t.Fatalf("CreateGroup(%s) failed: %v", g.name, err)
		}
	}
	if err := store.AddUserToGroup(f.alpha, f.userID); err != nil {
		cleanup()
		t.Fatalf("AddUserToGroup failed: %v", err)
	}
	if err := store.AddGroupToGroup(f.beta, f.alpha); err != nil {
		cleanup()
		t.Fatalf("AddGroupToGroup failed: %v", err)
	}
	return f, cleanup
}

// TestListGroupsWithMemberCount checks that each group carries its count
// of direct members, users and groups alike, in name order.
func TestListGroupsWithMemberCount(t *testing.T) {
	f, cleanup := newGroupLookupFixture(t)
	defer cleanup()

	groups, err := f.store.ListGroupsWithMemberCount()
	if err != nil {
		t.Fatalf("ListGroupsWithMemberCount failed: %v", err)
	}
	want := []struct {
		name  string
		count int
	}{{"alpha", 1}, {"beta", 1}, {"gamma", 0}}
	if len(groups) != len(want) {
		t.Fatalf("Expected %d groups, got %d", len(want), len(groups))
	}
	for i, w := range want {
		if groups[i].Name != w.name || groups[i].MemberCount != w.count {
			t.Errorf("Group %d: got %s with %d members, want %s with %d",
				i, groups[i].Name, groups[i].MemberCount, w.name, w.count)
		}
	}

	mustExec(t, f.store, "ALTER TABLE group_memberships RENAME TO gm_gone")
	if _, err := f.store.ListGroupsWithMemberCount(); err == nil {
		t.Error("Expected an error once the memberships table is gone")
	}
}

// TestGetUserGroupsByUsername checks that a user's groups include those
// reached through nested membership, and that an unknown user or an
// unreadable users table is an error.
func TestGetUserGroupsByUsername(t *testing.T) {
	f, cleanup := newGroupLookupFixture(t)
	defer cleanup()

	ids, err := f.store.GetUserGroupsByUsername("lookup-user")
	if err != nil {
		t.Fatalf("GetUserGroupsByUsername failed: %v", err)
	}
	got := map[int64]bool{}
	for _, id := range ids {
		got[id] = true
	}
	if len(ids) != 2 || !got[f.alpha] || !got[f.beta] {
		t.Errorf("Expected alpha and beta, got %v", ids)
	}

	if _, err := f.store.GetUserGroupsByUsername("nobody"); err == nil {
		t.Error("Expected an error for an unknown user")
	}

	mustExec(t, f.store, "ALTER TABLE users RENAME TO users_gone")
	if _, err := f.store.GetUserGroupsByUsername("lookup-user"); err == nil {
		t.Error("Expected an error once the users table is gone")
	}
}

// TestValidateNoCircularReference checks the standalone cycle check.
func TestValidateNoCircularReference(t *testing.T) {
	f, cleanup := newGroupLookupFixture(t)
	defer cleanup()

	tests := []struct {
		name          string
		parent, child int64
		wantErr       bool
	}{
		{"group into itself", f.alpha, f.alpha, true},
		{"parent into its own member", f.alpha, f.beta, true},
		{"unrelated groups", f.gamma, f.beta, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := f.store.ValidateNoCircularReference(tt.parent, tt.child)
			if (err != nil) != tt.wantErr {
				t.Errorf("got %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestGetGroupsForUser checks the detailed group list for a member, an
// empty list for a user with no groups, and the error paths.
func TestGetGroupsForUser(t *testing.T) {
	f, cleanup := newGroupLookupFixture(t)
	defer cleanup()

	groups, err := f.store.GetGroupsForUser(f.userID)
	if err != nil {
		t.Fatalf("GetGroupsForUser failed: %v", err)
	}
	names := map[string]bool{}
	for _, g := range groups {
		names[g.Name] = true
	}
	if len(groups) != 2 || !names["alpha"] || !names["beta"] {
		t.Errorf("Expected alpha and beta, got %v", names)
	}

	if err := f.store.CreateUser("loner", "Password1234", "", "",
		""); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	lonerID, err := f.store.GetUserID("loner")
	if err != nil {
		t.Fatalf("GetUserID failed: %v", err)
	}
	groups, err = f.store.GetGroupsForUser(lonerID)
	if err != nil || groups != nil {
		t.Errorf("Expected no groups and no error, got %v, %v", groups, err)
	}

	mustExec(t, f.store, "ALTER TABLE user_groups RENAME TO ug_gone")
	if _, err := f.store.GetGroupsForUser(f.userID); err == nil {
		t.Error("Expected an error once the groups table is gone")
	}

	mustExec(t, f.store, "ALTER TABLE group_memberships RENAME TO gm_gone")
	if _, err := f.store.GetGroupsForUser(f.userID); err == nil {
		t.Error("Expected an error once the memberships table is gone")
	}
}
