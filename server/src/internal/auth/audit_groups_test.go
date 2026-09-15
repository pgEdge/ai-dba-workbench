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
	"database/sql"
	"errors"
	"strings"
	"testing"
)

// assertGroupSuccess checks the common success-event columns for an
// event whose target is a group.
func assertGroupSuccess(t *testing.T, ev AuditEvent, action, targetName string) {
	t.Helper()

	assertUserActor(t, ev)
	if ev.Action != action {
		t.Errorf("Expected action %q, got %q", action, ev.Action)
	}
	if ev.TargetType != "group" {
		t.Errorf("Expected target type group, got %q", ev.TargetType)
	}
	if ev.TargetName != targetName {
		t.Errorf("Expected target name %q, got %q", targetName, ev.TargetName)
	}
	if ev.TargetID == nil {
		t.Error("Expected a target id, got nil")
	}
	if ev.Outcome != OutcomeSuccess {
		t.Errorf("Expected outcome success, got %q", ev.Outcome)
	}
	if ev.Error != "" {
		t.Errorf("Expected no error text, got %q", ev.Error)
	}
}

// assertGroupFailure checks the common failure-event columns.
func assertGroupFailure(t *testing.T, ev AuditEvent, action, targetName string) {
	t.Helper()

	if ev.Action != action {
		t.Errorf("Expected action %q, got %q", action, ev.Action)
	}
	if ev.TargetType != "group" {
		t.Errorf("Expected target type group, got %q", ev.TargetType)
	}
	if ev.TargetName != targetName {
		t.Errorf("Expected target name %q, got %q", targetName, ev.TargetName)
	}
	if ev.Outcome != OutcomeFailure {
		t.Errorf("Expected outcome failure, got %q", ev.Outcome)
	}
	if ev.Error == "" {
		t.Error("Expected error text on a failure event")
	}
}

// mustCreateAuditGroup creates a group through the system actor and
// returns its id.
func mustCreateAuditGroup(t *testing.T, s *AuthStore, name, description string) int64 {
	t.Helper()

	id, err := s.CreateGroup(name, description)
	if err != nil {
		t.Fatalf("CreateGroup(%q) failed: %v", name, err)
	}

	return id
}

// groupDetailString reads a string field out of a details map.
func groupDetailString(t *testing.T, details map[string]any, key string) string {
	t.Helper()

	value, ok := details[key].(string)
	if !ok {
		t.Fatalf("Details %v has no string field %q", details, key)
	}

	return value
}

// groupDetailObject reads a nested object out of a details map.
func groupDetailObject(t *testing.T, details map[string]any, key string) map[string]any {
	t.Helper()

	value, ok := details[key].(map[string]any)
	if !ok {
		t.Fatalf("Details %v has no object field %q", details, key)
	}

	return value
}

// groupDetailStrings reads a list of strings out of a details map.
func groupDetailStrings(t *testing.T, details map[string]any, key string) []string {
	t.Helper()

	raw, ok := details[key].([]any)
	if !ok {
		t.Fatalf("Details %v has no list field %q", details, key)
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("Field %q holds a non-string entry %v", key, item)
		}
		out = append(out, text)
	}

	return out
}

// =============================================================================
// Create
// =============================================================================

func TestAuditGroupsCreateGroup(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	as := store.AsActor(testActor())
	id, err := as.CreateGroup("dbas", "Database administrators")
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}

	ev := lastAuditEvent(t, store)
	assertGroupSuccess(t, ev, "group.create", "dbas")
	if ev.TargetID == nil || *ev.TargetID != id {
		t.Errorf("Expected target id %d, got %v", id, ev.TargetID)
	}

	after := groupDetailObject(t, auditDetails(t, ev), "after")
	if got := groupDetailString(t, after, "name"); got != "dbas" {
		t.Errorf("Expected after name dbas, got %q", got)
	}
	if got := groupDetailString(t, after, "description"); got != "Database administrators" {
		t.Errorf("Expected after description, got %q", got)
	}
}

func TestAuditGroupsCreateGroupDuplicateRecordsFailure(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	as := store.AsActor(testActor())
	if _, err := as.CreateGroup("dbas", ""); err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}
	if _, err := as.CreateGroup("dbas", ""); err == nil {
		t.Fatal("Expected a duplicate group name to be rejected")
	}

	ev := lastAuditEvent(t, store)
	assertGroupFailure(t, ev, "group.create", "dbas")
	if ev.TargetID != nil {
		t.Errorf("Expected no target id on a failed create, got %d", *ev.TargetID)
	}
}

// =============================================================================
// Update
// =============================================================================

func TestAuditGroupsUpdateGroup(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	id := mustCreateAuditGroup(t, store, "dbas", "old")

	as := store.AsActor(testActor())
	if err := as.UpdateGroup(id, "admins", "new"); err != nil {
		t.Fatalf("UpdateGroup failed: %v", err)
	}

	ev := lastAuditEvent(t, store)
	assertGroupSuccess(t, ev, "group.update", "dbas")

	details := auditDetails(t, ev)
	before := groupDetailObject(t, details, "before")
	after := groupDetailObject(t, details, "after")
	if got := groupDetailString(t, before, "name"); got != "dbas" {
		t.Errorf("Expected before name dbas, got %q", got)
	}
	if got := groupDetailString(t, before, "description"); got != "old" {
		t.Errorf("Expected before description old, got %q", got)
	}
	if got := groupDetailString(t, after, "name"); got != "admins" {
		t.Errorf("Expected after name admins, got %q", got)
	}
	if got := groupDetailString(t, after, "description"); got != "new" {
		t.Errorf("Expected after description new, got %q", got)
	}
}

func TestAuditGroupsUpdateGroupDescriptionOnly(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	id := mustCreateAuditGroup(t, store, "dbas", "old")

	as := store.AsActor(testActor())
	if err := as.UpdateGroup(id, "", "new"); err != nil {
		t.Fatalf("UpdateGroup failed: %v", err)
	}

	after := groupDetailObject(t, auditDetails(t, lastAuditEvent(t, store)), "after")
	if got := groupDetailString(t, after, "name"); got != "dbas" {
		t.Errorf("Expected the name to be unchanged, got %q", got)
	}
	if got := groupDetailString(t, after, "description"); got != "new" {
		t.Errorf("Expected after description new, got %q", got)
	}
}

func TestAuditGroupsUpdateGroupNotFound(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	as := store.AsActor(testActor())
	err := as.UpdateGroup(4242, "admins", "new")
	if err == nil {
		t.Fatal("Expected UpdateGroup to fail for a missing group")
	}
	if !strings.Contains(err.Error(), "group not found: 4242") {
		t.Errorf("Expected the existing not-found message, got %v", err)
	}

	ev := lastAuditEvent(t, store)
	assertGroupFailure(t, ev, "group.update", "")
	if ev.TargetID == nil || *ev.TargetID != 4242 {
		t.Errorf("Expected target id 4242, got %v", ev.TargetID)
	}
}

func TestAuditGroupsUpdateGroupDuplicateNameRecordsFailure(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustCreateAuditGroup(t, store, "dbas", "")
	id := mustCreateAuditGroup(t, store, "devs", "")

	as := store.AsActor(testActor())
	if err := as.UpdateGroup(id, "dbas", ""); err == nil {
		t.Fatal("Expected a colliding name to be rejected")
	}

	ev := lastAuditEvent(t, store)
	assertGroupFailure(t, ev, "group.update", "devs")
	if ev.TargetID == nil || *ev.TargetID != id {
		t.Errorf("Expected target id %d, got %v", id, ev.TargetID)
	}
}

// =============================================================================
// Delete
// =============================================================================

func TestAuditGroupsDeleteGroup(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	id := mustCreateAuditGroup(t, store, "dbas", "Database administrators")
	childID := mustCreateAuditGroup(t, store, "juniors", "")
	mustCreateUser(t, store, "bob")

	userID, err := store.GetUserID("bob")
	if err != nil {
		t.Fatalf("GetUserID failed: %v", err)
	}
	if err := store.AddUserToGroup(id, userID); err != nil {
		t.Fatalf("AddUserToGroup failed: %v", err)
	}
	if err := store.AddGroupToGroup(id, childID); err != nil {
		t.Fatalf("AddGroupToGroup failed: %v", err)
	}
	if err := store.GrantMCPPrivilegeByName(id, "*"); err != nil {
		t.Fatalf("GrantMCPPrivilegeByName failed: %v", err)
	}
	if err := store.GrantConnectionPrivilege(id, 7, "read"); err != nil {
		t.Fatalf("GrantConnectionPrivilege failed: %v", err)
	}
	if err := store.GrantAdminPermission(id, "manage_users"); err != nil {
		t.Fatalf("GrantAdminPermission failed: %v", err)
	}

	as := store.AsActor(testActor())
	if err := as.DeleteGroup(id); err != nil {
		t.Fatalf("DeleteGroup failed: %v", err)
	}

	ev := lastAuditEvent(t, store)
	assertGroupSuccess(t, ev, "group.delete", "dbas")

	details := auditDetails(t, ev)
	before := groupDetailObject(t, details, "before")
	if got := groupDetailString(t, before, "description"); got != "Database administrators" {
		t.Errorf("Expected the before description, got %q", got)
	}

	members := groupDetailObject(t, details, "members")
	if users := groupDetailStrings(t, members, "users"); len(users) != 1 ||
		users[0] != "bob" {
		t.Errorf("Expected member user bob, got %v", users)
	}
	if groups := groupDetailStrings(t, members, "groups"); len(groups) != 1 ||
		groups[0] != "juniors" {
		t.Errorf("Expected nested group juniors, got %v", groups)
	}

	if privs := groupDetailStrings(t, details, "mcp_privileges"); len(privs) != 1 ||
		privs[0] != "*" {
		t.Errorf("Expected the wildcard MCP privilege, got %v", privs)
	}
	if perms := groupDetailStrings(t, details, "admin_permissions"); len(perms) != 1 ||
		perms[0] != "manage_users" {
		t.Errorf("Expected admin permission manage_users, got %v", perms)
	}

	conns, ok := details["connection_privileges"].([]any)
	if !ok || len(conns) != 1 {
		t.Fatalf("Expected one connection privilege, got %v",
			details["connection_privileges"])
	}
	conn, ok := conns[0].(map[string]any)
	if !ok {
		t.Fatalf("Expected a connection privilege object, got %v", conns[0])
	}
	if got := groupDetailString(t, conn, "access_level"); got != "read" {
		t.Errorf("Expected access level read, got %q", got)
	}
	if got, ok := conn["connection_id"].(float64); !ok || int(got) != 7 {
		t.Errorf("Expected connection id 7, got %v", conn["connection_id"])
	}
}

func TestAuditGroupsDeleteGroupWithoutMembers(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	id := mustCreateAuditGroup(t, store, "dbas", "")

	as := store.AsActor(testActor())
	if err := as.DeleteGroup(id); err != nil {
		t.Fatalf("DeleteGroup failed: %v", err)
	}

	details := auditDetails(t, lastAuditEvent(t, store))
	members := groupDetailObject(t, details, "members")
	if users := groupDetailStrings(t, members, "users"); len(users) != 0 {
		t.Errorf("Expected no member users, got %v", users)
	}
	if privs := groupDetailStrings(t, details, "mcp_privileges"); len(privs) != 0 {
		t.Errorf("Expected no MCP privileges, got %v", privs)
	}
}

func TestAuditGroupsDeleteGroupNotFound(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	as := store.AsActor(testActor())
	err := as.DeleteGroup(4242)
	if err == nil {
		t.Fatal("Expected DeleteGroup to fail for a missing group")
	}
	if !strings.Contains(err.Error(), "group not found: 4242") {
		t.Errorf("Expected the existing not-found message, got %v", err)
	}

	ev := lastAuditEvent(t, store)
	assertGroupFailure(t, ev, "group.delete", "")
	if ev.TargetID == nil || *ev.TargetID != 4242 {
		t.Errorf("Expected target id 4242, got %v", ev.TargetID)
	}
}

// =============================================================================
// Membership
// =============================================================================

func TestAuditGroupsAddUserToGroup(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	id := mustCreateAuditGroup(t, store, "dbas", "")
	mustCreateUser(t, store, "bob")
	userID, err := store.GetUserID("bob")
	if err != nil {
		t.Fatalf("GetUserID failed: %v", err)
	}

	as := store.AsActor(testActor())
	if err := as.AddUserToGroup(id, userID); err != nil {
		t.Fatalf("AddUserToGroup failed: %v", err)
	}

	ev := lastAuditEvent(t, store)
	assertGroupSuccess(t, ev, "group.member.add", "dbas")

	details := auditDetails(t, ev)
	if got := groupDetailString(t, details, "member_type"); got != "user" {
		t.Errorf("Expected member type user, got %q", got)
	}
	if got := groupDetailString(t, details, "member_name"); got != "bob" {
		t.Errorf("Expected member name bob, got %q", got)
	}
	if got, ok := details["member_id"].(float64); !ok || int64(got) != userID {
		t.Errorf("Expected member id %d, got %v", userID, details["member_id"])
	}
}

func TestAuditGroupsAddGroupToGroup(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	parentID := mustCreateAuditGroup(t, store, "dbas", "")
	childID := mustCreateAuditGroup(t, store, "juniors", "")

	as := store.AsActor(testActor())
	if err := as.AddGroupToGroup(parentID, childID); err != nil {
		t.Fatalf("AddGroupToGroup failed: %v", err)
	}

	ev := lastAuditEvent(t, store)
	assertGroupSuccess(t, ev, "group.member.add", "dbas")

	details := auditDetails(t, ev)
	if got := groupDetailString(t, details, "member_type"); got != "group" {
		t.Errorf("Expected member type group, got %q", got)
	}
	if got := groupDetailString(t, details, "member_name"); got != "juniors" {
		t.Errorf("Expected member name juniors, got %q", got)
	}
	if got, ok := details["member_id"].(float64); !ok || int64(got) != childID {
		t.Errorf("Expected member id %d, got %v", childID, details["member_id"])
	}
}

func TestAuditGroupsRemoveUserFromGroup(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	id := mustCreateAuditGroup(t, store, "dbas", "")
	mustCreateUser(t, store, "bob")
	userID, err := store.GetUserID("bob")
	if err != nil {
		t.Fatalf("GetUserID failed: %v", err)
	}
	if err := store.AddUserToGroup(id, userID); err != nil {
		t.Fatalf("AddUserToGroup failed: %v", err)
	}

	as := store.AsActor(testActor())
	if err := as.RemoveUserFromGroup(id, userID); err != nil {
		t.Fatalf("RemoveUserFromGroup failed: %v", err)
	}

	ev := lastAuditEvent(t, store)
	assertGroupSuccess(t, ev, "group.member.remove", "dbas")

	details := auditDetails(t, ev)
	if got := groupDetailString(t, details, "member_type"); got != "user" {
		t.Errorf("Expected member type user, got %q", got)
	}
	if got := groupDetailString(t, details, "member_name"); got != "bob" {
		t.Errorf("Expected member name bob, got %q", got)
	}
}

func TestAuditGroupsRemoveGroupFromGroup(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	parentID := mustCreateAuditGroup(t, store, "dbas", "")
	childID := mustCreateAuditGroup(t, store, "juniors", "")
	if err := store.AddGroupToGroup(parentID, childID); err != nil {
		t.Fatalf("AddGroupToGroup failed: %v", err)
	}

	as := store.AsActor(testActor())
	if err := as.RemoveGroupFromGroup(parentID, childID); err != nil {
		t.Fatalf("RemoveGroupFromGroup failed: %v", err)
	}

	ev := lastAuditEvent(t, store)
	assertGroupSuccess(t, ev, "group.member.remove", "dbas")

	details := auditDetails(t, ev)
	if got := groupDetailString(t, details, "member_type"); got != "group" {
		t.Errorf("Expected member type group, got %q", got)
	}
	if got := groupDetailString(t, details, "member_name"); got != "juniors" {
		t.Errorf("Expected member name juniors, got %q", got)
	}
}

func TestAuditGroupsRemoveNonMemberRecordsFailure(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	id := mustCreateAuditGroup(t, store, "dbas", "")
	other := mustCreateAuditGroup(t, store, "juniors", "")
	mustCreateUser(t, store, "bob")
	userID, err := store.GetUserID("bob")
	if err != nil {
		t.Fatalf("GetUserID failed: %v", err)
	}

	as := store.AsActor(testActor())

	err = as.RemoveUserFromGroup(id, userID)
	if err == nil || !strings.Contains(err.Error(),
		"user is not a member of this group") {
		t.Fatalf("Expected the existing not-a-member error, got %v", err)
	}
	assertGroupFailure(t, lastAuditEvent(t, store), "group.member.remove", "dbas")

	err = as.RemoveGroupFromGroup(id, other)
	if err == nil || !strings.Contains(err.Error(),
		"group is not a member of this group") {
		t.Fatalf("Expected the existing not-a-member error, got %v", err)
	}
	assertGroupFailure(t, lastAuditEvent(t, store), "group.member.remove", "dbas")
}

func TestAuditGroupsAddGroupToGroupCircularRecordsFailure(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	parentID := mustCreateAuditGroup(t, store, "dbas", "")
	childID := mustCreateAuditGroup(t, store, "juniors", "")
	if err := store.AddGroupToGroup(parentID, childID); err != nil {
		t.Fatalf("AddGroupToGroup failed: %v", err)
	}

	as := store.AsActor(testActor())
	err := as.AddGroupToGroup(childID, parentID)
	if err == nil ||
		!strings.Contains(err.Error(), "would create a circular reference") {
		t.Fatalf("Expected a circular-reference error, got %v", err)
	}
	assertGroupFailure(t, lastAuditEvent(t, store), "group.member.add", "juniors")

	if err := as.AddGroupToGroup(parentID, parentID); err == nil ||
		!strings.Contains(err.Error(), "cannot add group to itself") {
		t.Fatalf("Expected a self-membership error, got %v", err)
	}
	assertGroupFailure(t, lastAuditEvent(t, store), "group.member.add", "dbas")
}

func TestAuditGroupsMembershipOnMissingGroup(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustCreateUser(t, store, "bob")
	userID, err := store.GetUserID("bob")
	if err != nil {
		t.Fatalf("GetUserID failed: %v", err)
	}

	as := store.AsActor(testActor())
	if err := as.AddUserToGroup(4242, userID); err == nil {
		t.Fatal("Expected adding to a missing group to fail")
	}

	ev := lastAuditEvent(t, store)
	assertGroupFailure(t, ev, "group.member.add", "")
	if ev.TargetID == nil || *ev.TargetID != 4242 {
		t.Errorf("Expected target id 4242, got %v", ev.TargetID)
	}
}

// =============================================================================
// Actor attribution and chain integrity
// =============================================================================

func TestAuditGroupsSystemActor(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustCreateAuditGroup(t, store, "dbas", "")

	ev := lastAuditEvent(t, store)
	if ev.ActorType != ActorSystem {
		t.Errorf("Expected actor type system, got %q", ev.ActorType)
	}
	if ev.ActorName != "system" {
		t.Errorf("Expected actor name system, got %q", ev.ActorName)
	}
	if ev.Action != "group.create" {
		t.Errorf("Expected action group.create, got %q", ev.Action)
	}
}

func TestAuditGroupsChainStaysIntact(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	as := store.AsActor(testActor())
	id, err := as.CreateGroup("dbas", "")
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}
	childID, err := as.CreateGroup("juniors", "")
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}
	if err := as.UpdateGroup(id, "admins", "note"); err != nil {
		t.Fatalf("UpdateGroup failed: %v", err)
	}
	if err := as.AddGroupToGroup(id, childID); err != nil {
		t.Fatalf("AddGroupToGroup failed: %v", err)
	}
	if err := as.RemoveGroupFromGroup(id, childID); err != nil {
		t.Fatalf("RemoveGroupFromGroup failed: %v", err)
	}
	if err := as.DeleteGroup(id); err != nil {
		t.Fatalf("DeleteGroup failed: %v", err)
	}

	count, firstBad, err := store.VerifyAuditChain()
	if err != nil {
		t.Fatalf("VerifyAuditChain failed at row %d: %v", firstBad, err)
	}
	if count < 6 {
		t.Errorf("Expected at least 6 audit rows, got %d", count)
	}
}

// =============================================================================
// Error paths
// =============================================================================

func TestAuditGroupsRecordAuditFailurePropagates(t *testing.T) {
	as := func(s *AuthStore) *ActorStore { return s.AsActor(testActor()) }

	cases := []struct {
		name string
		call func(*AuthStore, int64, int64) error
	}{
		{"CreateGroup", func(s *AuthStore, _, _ int64) error {
			_, err := as(s).CreateGroup("new", "")
			return err
		}},
		{"UpdateGroup", func(s *AuthStore, id, _ int64) error {
			return as(s).UpdateGroup(id, "renamed", "")
		}},
		{"DeleteGroup", func(s *AuthStore, id, _ int64) error {
			return as(s).DeleteGroup(id)
		}},
		{"AddGroupToGroup", func(s *AuthStore, id, child int64) error {
			return as(s).AddGroupToGroup(id, child)
		}},
		{"RemoveGroupFromGroup", func(s *AuthStore, id, child int64) error {
			if err := s.AddGroupToGroup(id, child); err != nil {
				return err
			}
			return as(s).RemoveGroupFromGroup(id, child)
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, cleanup := createTestAuthStoreForAudit(t)
			defer cleanup()

			id := mustCreateAuditGroup(t, store, "dbas", "")
			childID := mustCreateAuditGroup(t, store, "juniors", "")
			mustExec(t, store, "DROP TRIGGER audit_events_no_update")
			mustExec(t, store, "DROP TABLE audit_events")

			if err := tc.call(store, id, childID); err == nil {
				t.Errorf("Expected %s to fail without an audit_events table", tc.name)
			}
		})
	}
}

func TestAuditGroupsClosedStoreFailsToBegin(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	id := mustCreateAuditGroup(t, store, "dbas", "")
	if err := store.db.Close(); err != nil {
		t.Fatalf("Failed to close the store database: %v", err)
	}

	as := store.AsActor(testActor())
	if _, err := as.CreateGroup("new", ""); err == nil {
		t.Error("Expected CreateGroup to fail on a closed store")
	}
	if err := as.UpdateGroup(id, "x", ""); err == nil {
		t.Error("Expected UpdateGroup to fail on a closed store")
	}
	if err := as.DeleteGroup(id); err == nil {
		t.Error("Expected DeleteGroup to fail on a closed store")
	}
	if err := as.AddUserToGroup(id, 1); err == nil {
		t.Error("Expected AddUserToGroup to fail on a closed store")
	}
	if err := as.AddGroupToGroup(id, 2); err == nil {
		t.Error("Expected AddGroupToGroup to fail on a closed store")
	}
	if err := as.RemoveUserFromGroup(id, 1); err == nil {
		t.Error("Expected RemoveUserFromGroup to fail on a closed store")
	}
	if err := as.RemoveGroupFromGroup(id, 2); err == nil {
		t.Error("Expected RemoveGroupFromGroup to fail on a closed store")
	}
}

func TestAuditGroupsDeleteSnapshotFailuresAreWrapped(t *testing.T) {
	cases := []struct {
		name    string
		drop    string
		message string
	}{
		{"members", "DROP TABLE group_memberships",
			"failed to list group members"},
		{"mcp", "DROP TABLE group_mcp_privileges",
			"failed to list group MCP privileges"},
		{"connections", "DROP TABLE connection_privileges",
			"failed to list group connection privileges"},
		{"admin", "DROP TABLE group_admin_permissions",
			"failed to list group admin permissions"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, cleanup := createTestAuthStoreForAudit(t)
			defer cleanup()

			id := mustCreateAuditGroup(t, store, "dbas", "")
			mustExec(t, store, tc.drop)

			err := store.AsActor(testActor()).DeleteGroup(id)
			if err == nil {
				t.Fatalf("Expected DeleteGroup to fail after %q", tc.drop)
			}
			if !strings.Contains(err.Error(), tc.message) {
				t.Errorf("Expected %q, got %v", tc.message, err)
			}
		})
	}
}

func TestAuditGroupsLookupFailureIsWrapped(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustExec(t, store, "ALTER TABLE user_groups RENAME TO user_groups_moved")

	as := store.AsActor(testActor())
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"UpdateGroup", func() error { return as.UpdateGroup(1, "x", "") }},
		{"DeleteGroup", func() error { return as.DeleteGroup(1) }},
		{"AddUserToGroup", func() error { return as.AddUserToGroup(1, 1) }},
	} {
		err := tc.call()
		if err == nil {
			t.Fatalf("Expected %s to fail without a user_groups table", tc.name)
		}
		if !strings.Contains(err.Error(), "failed to look up group") {
			t.Errorf("Expected a wrapped group lookup error from %s, got %v",
				tc.name, err)
		}
	}
}

func TestAuditGroupsCreateGroupInsertFailureRecordsFailure(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustExec(t, store, "ALTER TABLE user_groups RENAME TO user_groups_moved")

	_, err := store.AsActor(testActor()).CreateGroup("dbas", "")
	if err == nil {
		t.Fatal("Expected CreateGroup to fail without a user_groups table")
	}
	if !strings.Contains(err.Error(), "failed to create group") {
		t.Errorf("Expected a wrapped create error, got %v", err)
	}

	assertGroupFailure(t, lastAuditEvent(t, store), "group.create", "dbas")
}

// blockGroupStatement installs a BEFORE trigger on user_groups that
// makes the named statement fail or, with RAISE(IGNORE), silently affect
// no rows. Both outcomes are otherwise unreachable, because the audited
// mutations read the group inside the same transaction first.
func blockGroupStatement(t *testing.T, s *AuthStore, event, action string) {
	t.Helper()

	// RAISE(IGNORE) takes no message, unlike RAISE(ABORT, ...).
	raise := "RAISE(IGNORE)"
	if action == "ABORT" {
		raise = "RAISE(ABORT, 'blocked')"
	}

	mustExec(t, s, "CREATE TRIGGER block_group_"+strings.ToLower(event)+
		" BEFORE "+event+" ON user_groups BEGIN SELECT "+raise+"; END")
}

func TestAuditGroupsUpdateStatementFailureRecordsFailure(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	id := mustCreateAuditGroup(t, store, "dbas", "")
	blockGroupStatement(t, store, "UPDATE", "ABORT")

	err := store.AsActor(testActor()).UpdateGroup(id, "admins", "")
	if err == nil {
		t.Fatal("Expected UpdateGroup to fail while the trigger blocks it")
	}
	if !strings.Contains(err.Error(), "failed to update group") {
		t.Errorf("Expected a wrapped update error, got %v", err)
	}

	assertGroupFailure(t, lastAuditEvent(t, store), "group.update", "dbas")
}

func TestAuditGroupsUpdateAffectingNoRowsRecordsFailure(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	id := mustCreateAuditGroup(t, store, "dbas", "")
	blockGroupStatement(t, store, "UPDATE", "IGNORE")

	err := store.AsActor(testActor()).UpdateGroup(id, "admins", "")
	if !errors.Is(err, ErrGroupNotFound) {
		t.Fatalf("Expected ErrGroupNotFound, got %v", err)
	}

	assertGroupFailure(t, lastAuditEvent(t, store), "group.update", "dbas")
}

func TestAuditGroupsDeleteStatementFailureRecordsFailure(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	id := mustCreateAuditGroup(t, store, "dbas", "")
	blockGroupStatement(t, store, "DELETE", "ABORT")

	err := store.AsActor(testActor()).DeleteGroup(id)
	if err == nil {
		t.Fatal("Expected DeleteGroup to fail while the trigger blocks it")
	}
	if !strings.Contains(err.Error(), "failed to delete group") {
		t.Errorf("Expected a wrapped delete error, got %v", err)
	}

	assertGroupFailure(t, lastAuditEvent(t, store), "group.delete", "dbas")
}

func TestAuditGroupsDeleteAffectingNoRowsRecordsFailure(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	id := mustCreateAuditGroup(t, store, "dbas", "")
	blockGroupStatement(t, store, "DELETE", "IGNORE")

	err := store.AsActor(testActor()).DeleteGroup(id)
	if err == nil || !strings.Contains(err.Error(), "group not found") {
		t.Fatalf("Expected a not-found error, got %v", err)
	}

	assertGroupFailure(t, lastAuditEvent(t, store), "group.delete", "dbas")
}

// mustPopulateGroup gives the group one member, one MCP privilege, one
// connection privilege and one admin permission, so that every
// dependent delete inside DeleteGroup has a row to act on.
func mustPopulateGroup(t *testing.T, s *AuthStore, groupID int64) {
	t.Helper()

	mustCreateUser(t, s, "bob")
	userID, err := s.GetUserID("bob")
	if err != nil {
		t.Fatalf("GetUserID failed: %v", err)
	}
	if err := s.AddUserToGroup(groupID, userID); err != nil {
		t.Fatalf("AddUserToGroup failed: %v", err)
	}
	if err := s.GrantMCPPrivilegeByName(groupID, "*"); err != nil {
		t.Fatalf("GrantMCPPrivilegeByName failed: %v", err)
	}
	if err := s.GrantConnectionPrivilege(groupID, 7, "read"); err != nil {
		t.Fatalf("GrantConnectionPrivilege failed: %v", err)
	}
	if err := s.GrantAdminPermission(groupID, "manage_users"); err != nil {
		t.Fatalf("GrantAdminPermission failed: %v", err)
	}
}

func TestAuditGroupsDependentDeleteFailuresRecordFailure(t *testing.T) {
	cases := []struct {
		name    string
		table   string
		message string
	}{
		{"memberships", "group_memberships",
			"failed to delete group memberships"},
		{"mcp", "group_mcp_privileges",
			"failed to delete group MCP privileges"},
		{"connections", "connection_privileges",
			"failed to delete group connection privileges"},
		{"admin", "group_admin_permissions",
			"failed to delete group admin permissions"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, cleanup := createTestAuthStoreForAudit(t)
			defer cleanup()

			id := mustCreateAuditGroup(t, store, "dbas", "")
			mustPopulateGroup(t, store, id)
			mustExec(t, store, "CREATE TRIGGER block_dependent BEFORE DELETE ON "+
				tc.table+" BEGIN SELECT RAISE(ABORT, 'blocked'); END")

			err := store.AsActor(testActor()).DeleteGroup(id)
			if err == nil {
				t.Fatalf("Expected DeleteGroup to fail while %s is blocked", tc.table)
			}
			if !strings.Contains(err.Error(), tc.message) {
				t.Errorf("Expected %q, got %v", tc.message, err)
			}

			assertGroupFailure(t, lastAuditEvent(t, store), "group.delete", "dbas")
		})
	}
}

func TestAuditGroupsAddGroupToGroupInsertFailureRecordsFailure(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	id := mustCreateAuditGroup(t, store, "dbas", "")

	err := store.AsActor(testActor()).AddGroupToGroup(id, 4242)
	if err == nil {
		t.Fatal("Expected adding a missing child group to fail")
	}
	if !strings.Contains(err.Error(), "failed to add group to group") {
		t.Errorf("Expected a wrapped insert error, got %v", err)
	}

	assertGroupFailure(t, lastAuditEvent(t, store), "group.member.add", "dbas")
}

func TestAuditGroupsMembershipDeleteFailuresRecordFailure(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	id := mustCreateAuditGroup(t, store, "dbas", "")
	childID := mustCreateAuditGroup(t, store, "juniors", "")
	mustPopulateGroup(t, store, id)
	if err := store.AddGroupToGroup(id, childID); err != nil {
		t.Fatalf("AddGroupToGroup failed: %v", err)
	}
	userID, err := store.GetUserID("bob")
	if err != nil {
		t.Fatalf("GetUserID failed: %v", err)
	}
	mustExec(t, store, `CREATE TRIGGER block_membership_delete
        BEFORE DELETE ON group_memberships
        BEGIN SELECT RAISE(ABORT, 'blocked'); END`)

	as := store.AsActor(testActor())
	if err := as.RemoveUserFromGroup(id, userID); err == nil ||
		!strings.Contains(err.Error(), "failed to remove user from group") {
		t.Fatalf("Expected a wrapped removal error, got %v", err)
	}
	assertGroupFailure(t, lastAuditEvent(t, store), "group.member.remove", "dbas")

	if err := store.RemoveGroupFromGroup(id, childID); err == nil ||
		!strings.Contains(err.Error(), "failed to remove group from group") {
		t.Fatalf("Expected a wrapped removal error, got %v", err)
	}
	ev := lastAuditEvent(t, store)
	if ev.ActorType != ActorSystem {
		t.Errorf("Expected the system actor, got %q", ev.ActorType)
	}
	if ev.Outcome != OutcomeFailure {
		t.Errorf("Expected outcome failure, got %q", ev.Outcome)
	}
}

func TestAuditGroupsCreateGroupSnapshotFailureRecordsFailure(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	blockGroupStatement(t, store, "INSERT", "IGNORE")

	_, err := store.AsActor(testActor()).CreateGroup("dbas", "")
	if err == nil {
		t.Fatal("Expected CreateGroup to fail when the insert is ignored")
	}
	if !strings.Contains(err.Error(), "failed to read created group") {
		t.Errorf("Expected a read-back failure, got %v", err)
	}

	assertGroupFailure(t, lastAuditEvent(t, store), "group.create", "dbas")
}

func TestAuditGroupsMemberNameLookupFailure(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	id := mustCreateAuditGroup(t, store, "dbas", "")
	mustPopulateGroup(t, store, id)
	userID, err := store.GetUserID("bob")
	if err != nil {
		t.Fatalf("GetUserID failed: %v", err)
	}
	mustExec(t, store, "ALTER TABLE users RENAME TO users_moved")

	err = store.AsActor(testActor()).RemoveUserFromGroup(id, userID)
	if err == nil {
		t.Fatal("Expected the member lookup to fail without a users table")
	}
	if !strings.Contains(err.Error(), "failed to look up group member") {
		t.Errorf("Expected a wrapped member lookup error, got %v", err)
	}

	assertGroupFailure(t, lastAuditEvent(t, store), "group.member.remove", "dbas")
}

func TestAuditGroupsUnsupportedMemberType(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	id := mustCreateAuditGroup(t, store, "dbas", "")

	err := store.membershipChange(systemActor, "group.member.add", "robot",
		id, 1, func(*sql.Tx) error { return nil })
	if err == nil {
		t.Fatal("Expected an unsupported member type to be rejected")
	}
	if !strings.Contains(err.Error(), "unsupported member type") {
		t.Errorf("Expected an unsupported-member-type error, got %v", err)
	}
}
