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
	"encoding/json"
	"testing"
)

// lastPrivilegeAuditEvent returns the most recent audit event, failing
// the test when the log is empty.
func lastPrivilegeAuditEvent(t *testing.T, s *AuthStore) AuditEvent {
	t.Helper()

	events, _, err := s.ListAuditEvents(AuditFilter{Limit: 1})
	if err != nil {
		t.Fatalf("Failed to list audit events: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("Expected an audit event, found none")
	}

	return events[0]
}

// privilegeAuditDetails decodes an event's details into a map.
func privilegeAuditDetails(t *testing.T, ev AuditEvent) map[string]any {
	t.Helper()

	if len(ev.Details) == 0 {
		t.Fatalf("Audit event %d carries no details", ev.ID)
	}

	var decoded map[string]any
	if err := json.Unmarshal(ev.Details, &decoded); err != nil {
		t.Fatalf("Failed to decode audit details %s: %v", ev.Details, err)
	}

	return decoded
}

// privilegeTestActor is the acting principal used by the ActorStore cases.
var privilegeTestActor = Actor{
	Type: ActorUser,
	ID:   int64Ptr(42),
	Name: "alice",
	IP:   "192.0.2.10",
}

// assertPrivilegeEvent checks the common fields of a successful event.
func assertPrivilegeEvent(t *testing.T, ev AuditEvent, action string,
	groupID int64, groupName string, want map[string]any) {

	t.Helper()

	if ev.Action != action {
		t.Errorf("Expected action %q, got %q", action, ev.Action)
	}
	if ev.Outcome != OutcomeSuccess {
		t.Errorf("Expected outcome success, got %q", ev.Outcome)
	}
	if ev.TargetType != "group" {
		t.Errorf("Expected target type group, got %q", ev.TargetType)
	}
	if ev.TargetID == nil || *ev.TargetID != groupID {
		t.Errorf("Expected target id %d, got %v", groupID, ev.TargetID)
	}
	if ev.TargetName != groupName {
		t.Errorf("Expected target name %q, got %q", groupName, ev.TargetName)
	}

	details := privilegeAuditDetails(t, ev)
	for key, wantValue := range want {
		got, ok := details[key]
		if !ok {
			t.Errorf("Details %s missing key %q", ev.Details, key)
			continue
		}
		if !jsonEqual(got, wantValue) {
			t.Errorf("Details key %q: expected %v, got %v", key, wantValue, got)
		}
	}
}

// jsonEqual compares two decoded JSON values by their encodings, so that
// numbers and slices compare without type gymnastics.
func jsonEqual(a, b any) bool {
	left, err := json.Marshal(a)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(left) == string(right)
}

// privilegeAuditCase describes one audited privilege mutation.
type privilegeAuditCase struct {
	name string
	// setup prepares state the mutation depends on and returns the
	// privilege id under test, where the case needs one.
	setup func(t *testing.T, s *AuthStore, groupID int64) int64
	// call performs the mutation through the plain store.
	call func(s *AuthStore, groupID, privilegeID int64) error
	// callAsActor performs the same mutation through an actor view.
	callAsActor func(a *ActorStore, groupID, privilegeID int64) error
	// action is the expected audit action name.
	action string
	// details builds the expected details for the privilege id used.
	details func(privilegeID int64) map[string]any
	// brokenTable is dropped to force the mutation to fail.
	brokenTable string
}

func privilegeAuditCases() []privilegeAuditCase {
	registerTool := func(t *testing.T, s *AuthStore, _ int64) int64 {
		t.Helper()
		id, err := s.RegisterMCPPrivilege("query_database",
			MCPPrivilegeTypeTool, "Run queries", false)
		if err != nil {
			t.Fatalf("Failed to register privilege: %v", err)
		}
		return id
	}

	return []privilegeAuditCase{
		{
			name:  "GrantMCPPrivilege",
			setup: registerTool,
			call: func(s *AuthStore, groupID, privilegeID int64) error {
				return s.GrantMCPPrivilege(groupID, privilegeID)
			},
			callAsActor: func(a *ActorStore, groupID, privilegeID int64) error {
				return a.GrantMCPPrivilege(groupID, privilegeID)
			},
			action: "privilege.mcp.grant",
			details: func(privilegeID int64) map[string]any {
				return map[string]any{
					"privilege_id":   privilegeID,
					"privilege_name": "query_database",
				}
			},
			brokenTable: "group_mcp_privileges",
		},
		{
			name:  "GrantMCPPrivilegeWildcard",
			setup: func(t *testing.T, s *AuthStore, _ int64) int64 { return MCPPrivilegeIDWildcard },
			call: func(s *AuthStore, groupID, privilegeID int64) error {
				return s.GrantMCPPrivilege(groupID, privilegeID)
			},
			callAsActor: func(a *ActorStore, groupID, privilegeID int64) error {
				return a.GrantMCPPrivilege(groupID, privilegeID)
			},
			action: "privilege.mcp.grant",
			details: func(privilegeID int64) map[string]any {
				return map[string]any{
					"privilege_id":   privilegeID,
					"privilege_name": "*",
				}
			},
			brokenTable: "group_mcp_privileges",
		},
		{
			name:  "GrantMCPPrivilegeByName",
			setup: registerTool,
			call: func(s *AuthStore, groupID, _ int64) error {
				return s.GrantMCPPrivilegeByName(groupID, "query_database")
			},
			callAsActor: func(a *ActorStore, groupID, _ int64) error {
				return a.GrantMCPPrivilegeByName(groupID, "query_database")
			},
			action: "privilege.mcp.grant",
			details: func(privilegeID int64) map[string]any {
				return map[string]any{
					"privilege_id":   privilegeID,
					"privilege_name": "query_database",
				}
			},
			brokenTable: "group_mcp_privileges",
		},
		{
			name:  "GrantMCPPrivilegeByNameWildcard",
			setup: registerTool,
			call: func(s *AuthStore, groupID, _ int64) error {
				return s.GrantMCPPrivilegeByName(groupID, "*")
			},
			callAsActor: func(a *ActorStore, groupID, _ int64) error {
				return a.GrantMCPPrivilegeByName(groupID, "*")
			},
			action: "privilege.mcp.grant",
			details: func(_ int64) map[string]any {
				return map[string]any{
					"privilege_id":   MCPPrivilegeIDWildcard,
					"privilege_name": "*",
				}
			},
			brokenTable: "group_mcp_privileges",
		},
		{
			name: "RevokeMCPPrivilege",
			setup: func(t *testing.T, s *AuthStore, groupID int64) int64 {
				id := registerTool(t, s, groupID)
				if err := s.GrantMCPPrivilege(groupID, id); err != nil {
					t.Fatalf("Failed to grant privilege: %v", err)
				}
				return id
			},
			call: func(s *AuthStore, groupID, privilegeID int64) error {
				return s.RevokeMCPPrivilege(groupID, privilegeID)
			},
			callAsActor: func(a *ActorStore, groupID, privilegeID int64) error {
				return a.RevokeMCPPrivilege(groupID, privilegeID)
			},
			action: "privilege.mcp.revoke",
			details: func(privilegeID int64) map[string]any {
				return map[string]any{
					"privilege_id":   privilegeID,
					"privilege_name": "query_database",
				}
			},
			brokenTable: "group_mcp_privileges",
		},
		{
			name: "RevokeMCPPrivilegeByName",
			setup: func(t *testing.T, s *AuthStore, groupID int64) int64 {
				id := registerTool(t, s, groupID)
				if err := s.GrantMCPPrivilege(groupID, id); err != nil {
					t.Fatalf("Failed to grant privilege: %v", err)
				}
				return id
			},
			call: func(s *AuthStore, groupID, _ int64) error {
				return s.RevokeMCPPrivilegeByName(groupID, "query_database")
			},
			callAsActor: func(a *ActorStore, groupID, _ int64) error {
				return a.RevokeMCPPrivilegeByName(groupID, "query_database")
			},
			action: "privilege.mcp.revoke",
			details: func(privilegeID int64) map[string]any {
				return map[string]any{
					"privilege_id":   privilegeID,
					"privilege_name": "query_database",
				}
			},
			brokenTable: "group_mcp_privileges",
		},
		{
			name: "RevokeMCPPrivilegeByNameWildcard",
			setup: func(t *testing.T, s *AuthStore, groupID int64) int64 {
				if err := s.GrantMCPPrivilegeByName(groupID, "*"); err != nil {
					t.Fatalf("Failed to grant wildcard: %v", err)
				}
				return MCPPrivilegeIDWildcard
			},
			call: func(s *AuthStore, groupID, _ int64) error {
				return s.RevokeMCPPrivilegeByName(groupID, "*")
			},
			callAsActor: func(a *ActorStore, groupID, _ int64) error {
				return a.RevokeMCPPrivilegeByName(groupID, "*")
			},
			action: "privilege.mcp.revoke",
			details: func(_ int64) map[string]any {
				return map[string]any{
					"privilege_id":   MCPPrivilegeIDWildcard,
					"privilege_name": "*",
				}
			},
			brokenTable: "group_mcp_privileges",
		},
		{
			name:  "GrantConnectionPrivilege",
			setup: func(t *testing.T, s *AuthStore, _ int64) int64 { return 7 },
			call: func(s *AuthStore, groupID, connectionID int64) error {
				return s.GrantConnectionPrivilege(groupID, int(connectionID),
					AccessLevelRead)
			},
			callAsActor: func(a *ActorStore, groupID, connectionID int64) error {
				return a.GrantConnectionPrivilege(groupID, int(connectionID),
					AccessLevelRead)
			},
			action: "privilege.connection.grant",
			details: func(connectionID int64) map[string]any {
				return map[string]any{
					"connection_id": connectionID,
					"before":        nil,
					"after":         map[string]any{"access_level": AccessLevelRead},
				}
			},
			brokenTable: "connection_privileges",
		},
		{
			name: "RevokeConnectionPrivilege",
			setup: func(t *testing.T, s *AuthStore, groupID int64) int64 {
				if err := s.GrantConnectionPrivilege(groupID, 7,
					AccessLevelRead); err != nil {
					t.Fatalf("Failed to grant connection privilege: %v", err)
				}
				return 7
			},
			call: func(s *AuthStore, groupID, connectionID int64) error {
				return s.RevokeConnectionPrivilege(groupID, int(connectionID))
			},
			callAsActor: func(a *ActorStore, groupID, connectionID int64) error {
				return a.RevokeConnectionPrivilege(groupID, int(connectionID))
			},
			action: "privilege.connection.revoke",
			details: func(connectionID int64) map[string]any {
				return map[string]any{
					"connection_id": connectionID,
					"before":        map[string]any{"access_level": AccessLevelRead},
				}
			},
			brokenTable: "connection_privileges",
		},
		{
			name:  "GrantAdminPermission",
			setup: func(t *testing.T, s *AuthStore, _ int64) int64 { return 0 },
			call: func(s *AuthStore, groupID, _ int64) error {
				return s.GrantAdminPermission(groupID, PermManageUsers)
			},
			callAsActor: func(a *ActorStore, groupID, _ int64) error {
				return a.GrantAdminPermission(groupID, PermManageUsers)
			},
			action: "permission.admin.grant",
			details: func(_ int64) map[string]any {
				return map[string]any{"permission": PermManageUsers}
			},
			brokenTable: "group_admin_permissions",
		},
		{
			name: "RevokeAdminPermission",
			setup: func(t *testing.T, s *AuthStore, groupID int64) int64 {
				if err := s.GrantAdminPermission(groupID,
					PermManageUsers); err != nil {
					t.Fatalf("Failed to grant permission: %v", err)
				}
				return 0
			},
			call: func(s *AuthStore, groupID, _ int64) error {
				return s.RevokeAdminPermission(groupID, PermManageUsers)
			},
			callAsActor: func(a *ActorStore, groupID, _ int64) error {
				return a.RevokeAdminPermission(groupID, PermManageUsers)
			},
			action: "permission.admin.revoke",
			details: func(_ int64) map[string]any {
				return map[string]any{"permission": PermManageUsers}
			},
			brokenTable: "group_admin_permissions",
		},
	}
}

// newAuditedPrivilegeGroup creates a store and a group for one case.
func newAuditedPrivilegeGroup(t *testing.T) (*AuthStore, func(), int64) {
	t.Helper()

	store, cleanup := createTestAuthStoreForPrivileges(t)
	groupID, err := store.CreateGroup("analysts", "Analyst group")
	if err != nil {
		cleanup()
		t.Fatalf("Failed to create group: %v", err)
	}

	return store, cleanup, groupID
}

func TestPrivilegeMutationsAreAuditedAsSystem(t *testing.T) {
	for _, tc := range privilegeAuditCases() {
		t.Run(tc.name, func(t *testing.T) {
			store, cleanup, groupID := newAuditedPrivilegeGroup(t)
			defer cleanup()

			privilegeID := tc.setup(t, store, groupID)
			if err := tc.call(store, groupID, privilegeID); err != nil {
				t.Fatalf("Mutation failed: %v", err)
			}

			ev := lastPrivilegeAuditEvent(t, store)
			assertPrivilegeEvent(t, ev, tc.action, groupID, "analysts",
				tc.details(privilegeID))
			if ev.ActorType != ActorSystem {
				t.Errorf("Expected system actor, got %q", ev.ActorType)
			}
			if ev.ActorName != "system" {
				t.Errorf("Expected actor name system, got %q", ev.ActorName)
			}
		})
	}
}

func TestPrivilegeMutationsAreAuditedAsActor(t *testing.T) {
	for _, tc := range privilegeAuditCases() {
		t.Run(tc.name, func(t *testing.T) {
			store, cleanup, groupID := newAuditedPrivilegeGroup(t)
			defer cleanup()

			privilegeID := tc.setup(t, store, groupID)
			if err := tc.callAsActor(store.AsActor(privilegeTestActor), groupID,
				privilegeID); err != nil {
				t.Fatalf("Mutation failed: %v", err)
			}

			ev := lastPrivilegeAuditEvent(t, store)
			assertPrivilegeEvent(t, ev, tc.action, groupID, "analysts",
				tc.details(privilegeID))
			if ev.ActorType != ActorUser {
				t.Errorf("Expected user actor, got %q", ev.ActorType)
			}
			if ev.ActorID == nil || *ev.ActorID != 42 {
				t.Errorf("Expected actor id 42, got %v", ev.ActorID)
			}
			if ev.ActorName != "alice" {
				t.Errorf("Expected actor name alice, got %q", ev.ActorName)
			}
			if ev.ActorIP != "192.0.2.10" {
				t.Errorf("Expected actor IP 192.0.2.10, got %q", ev.ActorIP)
			}
		})
	}
}

func TestPrivilegeMutationFailuresAreAudited(t *testing.T) {
	for _, tc := range privilegeAuditCases() {
		t.Run(tc.name, func(t *testing.T) {
			store, cleanup, groupID := newAuditedPrivilegeGroup(t)
			defer cleanup()

			privilegeID := tc.setup(t, store, groupID)
			if _, err := store.db.Exec("DROP TABLE " + tc.brokenTable); err != nil {
				t.Fatalf("Failed to drop table: %v", err)
			}

			if err := tc.callAsActor(store.AsActor(privilegeTestActor), groupID,
				privilegeID); err == nil {
				t.Fatal("Expected the mutation to fail")
			}

			ev := lastPrivilegeAuditEvent(t, store)
			if ev.Action != tc.action {
				t.Errorf("Expected action %q, got %q", tc.action, ev.Action)
			}
			if ev.Outcome != OutcomeFailure {
				t.Errorf("Expected outcome failure, got %q", ev.Outcome)
			}
			if ev.TargetID == nil || *ev.TargetID != groupID {
				t.Errorf("Expected target id %d, got %v", groupID, ev.TargetID)
			}
			if ev.TargetName != "analysts" {
				t.Errorf("Expected target name analysts, got %q", ev.TargetName)
			}
			if ev.Error == "" {
				t.Error("Expected the failure event to carry an error")
			}
			if ev.ActorName != "alice" {
				t.Errorf("Expected actor name alice, got %q", ev.ActorName)
			}
		})
	}
}

// TestPrivilegeMutationAuditFailureRollsBack checks that a mutation is
// abandoned when its audit event cannot be written.
func TestPrivilegeMutationAuditFailureRollsBack(t *testing.T) {
	store, cleanup, groupID := newAuditedPrivilegeGroup(t)
	defer cleanup()

	if _, err := store.db.Exec("DROP TABLE audit_events"); err != nil {
		t.Fatalf("Failed to drop audit table: %v", err)
	}

	if err := store.GrantAdminPermission(groupID,
		PermManageUsers); err == nil {
		t.Fatal("Expected the grant to fail when the audit write fails")
	}

	perms, err := store.ListGroupAdminPermissions(groupID)
	if err != nil {
		t.Fatalf("Failed to list permissions: %v", err)
	}
	if len(perms) != 0 {
		t.Errorf("Expected the grant to be rolled back, got %v", perms)
	}
}

// TestPrivilegeAuditUnknownGroupName records the group id, and an empty
// target name, when the group does not exist. The grant itself fails on
// the foreign key, so the event is the failure one.
func TestPrivilegeAuditUnknownGroupName(t *testing.T) {
	store, cleanup := createTestAuthStoreForPrivileges(t)
	defer cleanup()

	const missingGroup int64 = 4242
	if err := store.GrantAdminPermission(missingGroup,
		PermManageUsers); err == nil {
		t.Fatal("Expected the grant to fail for a missing group")
	}

	ev := lastPrivilegeAuditEvent(t, store)
	if ev.Outcome != OutcomeFailure {
		t.Errorf("Expected outcome failure, got %q", ev.Outcome)
	}
	if ev.TargetID == nil || *ev.TargetID != missingGroup {
		t.Errorf("Expected target id %d, got %v", missingGroup, ev.TargetID)
	}
	if ev.TargetName != "" {
		t.Errorf("Expected an empty target name, got %q", ev.TargetName)
	}
}

// TestGrantMCPPrivilegeByNameUnknownIdentifier keeps the existing error
// message and records a failure event.
func TestGrantMCPPrivilegeByNameUnknownIdentifier(t *testing.T) {
	store, cleanup, groupID := newAuditedPrivilegeGroup(t)
	defer cleanup()

	err := store.AsActor(privilegeTestActor).GrantMCPPrivilegeByName(groupID, "nope")
	if err == nil || err.Error() != "privilege not found: nope" {
		t.Fatalf("Expected the unchanged not-found error, got %v", err)
	}

	ev := lastPrivilegeAuditEvent(t, store)
	if ev.Outcome != OutcomeFailure || ev.Action != "privilege.mcp.grant" {
		t.Errorf("Expected a failed grant event, got %q/%q", ev.Action, ev.Outcome)
	}
}

// TestRevokeNotFoundErrorsUnchanged checks the revoke error messages and
// that each records a failure event.
func TestRevokeNotFoundErrorsUnchanged(t *testing.T) {
	tests := []struct {
		name    string
		call    func(s *AuthStore, groupID int64) error
		wantErr string
		action  string
	}{
		{
			name: "MCPPrivilege",
			call: func(s *AuthStore, groupID int64) error {
				return s.RevokeMCPPrivilege(groupID, 99)
			},
			wantErr: "privilege grant not found",
			action:  "privilege.mcp.revoke",
		},
		{
			name: "MCPPrivilegeByName",
			call: func(s *AuthStore, groupID int64) error {
				return s.RevokeMCPPrivilegeByName(groupID, "absent")
			},
			wantErr: "privilege grant not found",
			action:  "privilege.mcp.revoke",
		},
		{
			name: "ConnectionPrivilege",
			call: func(s *AuthStore, groupID int64) error {
				return s.RevokeConnectionPrivilege(groupID, 99)
			},
			wantErr: "connection privilege not found",
			action:  "privilege.connection.revoke",
		},
		{
			name: "AdminPermission",
			call: func(s *AuthStore, groupID int64) error {
				return s.RevokeAdminPermission(groupID, PermManageUsers)
			},
			wantErr: "admin permission grant not found",
			action:  "permission.admin.revoke",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store, cleanup, groupID := newAuditedPrivilegeGroup(t)
			defer cleanup()

			err := tc.call(store, groupID)
			if err == nil || err.Error() != tc.wantErr {
				t.Fatalf("Expected %q, got %v", tc.wantErr, err)
			}

			ev := lastPrivilegeAuditEvent(t, store)
			if ev.Action != tc.action || ev.Outcome != OutcomeFailure {
				t.Errorf("Expected a failed %s event, got %q/%q", tc.action,
					ev.Action, ev.Outcome)
			}
		})
	}
}

// TestGrantConnectionPrivilegeInvalidLevel keeps the validation error and
// records the attempt as a failure.
func TestGrantConnectionPrivilegeInvalidLevelIsAudited(t *testing.T) {
	store, cleanup, groupID := newAuditedPrivilegeGroup(t)
	defer cleanup()

	err := store.GrantConnectionPrivilege(groupID, 7, "admin")
	if err == nil ||
		err.Error() != "invalid access level: admin (must be 'read' or 'read_write')" {
		t.Fatalf("Expected the unchanged validation error, got %v", err)
	}

	ev := lastPrivilegeAuditEvent(t, store)
	if ev.Action != "privilege.connection.grant" || ev.Outcome != OutcomeFailure {
		t.Errorf("Expected a failed connection grant event, got %q/%q",
			ev.Action, ev.Outcome)
	}
}

// TestGrantAdminPermissionWildcardCleanup records the individual
// permissions removed by the wildcard grant.
func TestGrantAdminPermissionWildcardCleanup(t *testing.T) {
	store, cleanup, groupID := newAuditedPrivilegeGroup(t)
	defer cleanup()

	for _, perm := range []string{PermManageUsers,
		PermManageGroups} {
		if err := store.GrantAdminPermission(groupID, perm); err != nil {
			t.Fatalf("Failed to grant %s: %v", perm, err)
		}
	}

	if err := store.GrantAdminPermission(groupID,
		AdminPermissionWildcard); err != nil {
		t.Fatalf("Failed to grant the wildcard: %v", err)
	}

	ev := lastPrivilegeAuditEvent(t, store)
	details := privilegeAuditDetails(t, ev)
	if details["permission"] != AdminPermissionWildcard {
		t.Errorf("Expected the wildcard permission, got %v", details["permission"])
	}
	removed, ok := details["removed_individual"].([]any)
	if !ok {
		t.Fatalf("Expected removed_individual in %s", ev.Details)
	}
	if len(removed) != 2 {
		t.Errorf("Expected two removed permissions, got %v", removed)
	}

	perms, err := store.ListGroupAdminPermissions(groupID)
	if err != nil {
		t.Fatalf("Failed to list permissions: %v", err)
	}
	if len(perms) != 1 || perms[0] != AdminPermissionWildcard {
		t.Errorf("Expected only the wildcard to remain, got %v", perms)
	}
}

// TestGrantMCPPrivilegeByNameWildcardCleanup checks that the wildcard
// grant still removes individual grants.
func TestGrantMCPPrivilegeByNameWildcardCleanup(t *testing.T) {
	store, cleanup, groupID := newAuditedPrivilegeGroup(t)
	defer cleanup()

	privilegeID, err := store.RegisterMCPPrivilege("query_database",
		MCPPrivilegeTypeTool, "Run queries", false)
	if err != nil {
		t.Fatalf("Failed to register privilege: %v", err)
	}
	if err := store.GrantMCPPrivilege(groupID, privilegeID); err != nil {
		t.Fatalf("Failed to grant privilege: %v", err)
	}

	if err := store.GrantMCPPrivilegeByName(groupID, "*"); err != nil {
		t.Fatalf("Failed to grant the wildcard: %v", err)
	}

	privileges, err := store.ListGroupMCPPrivileges(groupID)
	if err != nil {
		t.Fatalf("Failed to list group privileges: %v", err)
	}
	if len(privileges) != 1 {
		t.Errorf("Expected only the wildcard grant, got %d rows", len(privileges))
	}
}

// TestPrivilegeAuditOnClosedStore covers the transaction-begin failure
// path shared by every audited privilege mutation.
func TestPrivilegeAuditOnClosedStore(t *testing.T) {
	store, cleanup, groupID := newAuditedPrivilegeGroup(t)
	defer cleanup()

	if err := store.db.Close(); err != nil {
		t.Fatalf("Failed to close the database: %v", err)
	}

	calls := []func() error{
		func() error { return store.GrantMCPPrivilege(groupID, 1) },
		func() error { return store.GrantMCPPrivilegeByName(groupID, "x") },
		func() error { return store.RevokeMCPPrivilege(groupID, 1) },
		func() error { return store.RevokeMCPPrivilegeByName(groupID, "x") },
		func() error { return store.GrantConnectionPrivilege(groupID, 1, AccessLevelRead) },
		func() error { return store.RevokeConnectionPrivilege(groupID, 1) },
		func() error { return store.GrantAdminPermission(groupID, PermManageUsers) },
		func() error { return store.RevokeAdminPermission(groupID, PermManageUsers) },
	}

	for i, call := range calls {
		if err := call(); err == nil {
			t.Errorf("Call %d: expected an error on a closed store", i)
		}
	}
}

// TestPrivilegeMutationsFailWhenAuditUnwritable checks that every
// audited mutation is abandoned when its event cannot be written.
func TestPrivilegeMutationsFailWhenAuditUnwritable(t *testing.T) {
	for _, tc := range privilegeAuditCases() {
		t.Run(tc.name, func(t *testing.T) {
			store, cleanup, groupID := newAuditedPrivilegeGroup(t)
			defer cleanup()

			privilegeID := tc.setup(t, store, groupID)
			if _, err := store.db.Exec("DROP TABLE audit_events"); err != nil {
				t.Fatalf("Failed to drop the audit table: %v", err)
			}

			if err := tc.call(store, groupID, privilegeID); err == nil {
				t.Fatal("Expected the mutation to fail")
			}
		})
	}
}

// TestGrantMCPPrivilegeByNameLookupFails covers the privilege lookup
// error path, as distinct from the privilege simply being absent.
func TestGrantMCPPrivilegeByNameLookupFails(t *testing.T) {
	store, cleanup, groupID := newAuditedPrivilegeGroup(t)
	defer cleanup()

	if _, err := store.db.Exec("DROP TABLE mcp_privilege_identifiers"); err != nil {
		t.Fatalf("Failed to drop the privilege table: %v", err)
	}

	if err := store.GrantMCPPrivilegeByName(groupID, "query_database"); err == nil {
		t.Fatal("Expected the grant to fail")
	}

	ev := lastPrivilegeAuditEvent(t, store)
	if ev.Outcome != OutcomeFailure {
		t.Errorf("Expected outcome failure, got %q", ev.Outcome)
	}
}

// TestGrantAdminPermissionWildcardListFails covers the failure to read
// the individual permissions that the wildcard grant would remove.
func TestGrantAdminPermissionWildcardListFails(t *testing.T) {
	store, cleanup, groupID := newAuditedPrivilegeGroup(t)
	defer cleanup()

	if _, err := store.db.Exec("DROP TABLE group_admin_permissions"); err != nil {
		t.Fatalf("Failed to drop the permission table: %v", err)
	}

	if err := store.GrantAdminPermission(groupID,
		AdminPermissionWildcard); err == nil {
		t.Fatal("Expected the wildcard grant to fail")
	}

	ev := lastPrivilegeAuditEvent(t, store)
	if ev.Outcome != OutcomeFailure || ev.Action != "permission.admin.grant" {
		t.Errorf("Expected a failed grant event, got %q/%q", ev.Action, ev.Outcome)
	}
}

// TestConnectionGrantRecordsAccessLevelChange checks that a grant
// records the level it moved from as well as the level it moved to, so
// that a silent upgrade from read to read_write is visible in the log.
func TestConnectionGrantRecordsAccessLevelChange(t *testing.T) {
	store, cleanup, groupID := newAuditedPrivilegeGroup(t)
	defer cleanup()

	if err := store.GrantConnectionPrivilege(groupID, 7, AccessLevelRead); err != nil {
		t.Fatalf("First grant failed: %v", err)
	}

	first := lastPrivilegeAuditEvent(t, store)
	details := privilegeAuditDetails(t, first)
	if details["before"] != nil {
		t.Errorf("Expected a nil before on a fresh grant, got %v", details["before"])
	}
	if !jsonEqual(details["after"],
		map[string]any{"access_level": AccessLevelRead}) {
		t.Errorf("Expected after read, got %v", details["after"])
	}

	if err := store.GrantConnectionPrivilege(groupID, 7,
		AccessLevelReadWrite); err != nil {
		t.Fatalf("Second grant failed: %v", err)
	}

	second := lastPrivilegeAuditEvent(t, store)
	details = privilegeAuditDetails(t, second)
	if !jsonEqual(details["before"],
		map[string]any{"access_level": AccessLevelRead}) {
		t.Errorf("Expected before read, got %v", details["before"])
	}
	if !jsonEqual(details["after"],
		map[string]any{"access_level": AccessLevelReadWrite}) {
		t.Errorf("Expected after read_write, got %v", details["after"])
	}

	if err := store.RevokeConnectionPrivilege(groupID, 7); err != nil {
		t.Fatalf("Revoke failed: %v", err)
	}

	revoked := lastPrivilegeAuditEvent(t, store)
	details = privilegeAuditDetails(t, revoked)
	if !jsonEqual(details["before"],
		map[string]any{"access_level": AccessLevelReadWrite}) {
		t.Errorf("Expected revoke before read_write, got %v", details["before"])
	}
	if _, ok := details["after"]; ok {
		t.Errorf("Expected no after on a revoke, got %v", details["after"])
	}
}

// TestConnectionAccessLevelTxMissingTable checks that a failed lookup
// degrades to "no grant" rather than failing the mutation, which is
// what keeps the audit enrichment off the critical path.
func TestConnectionAccessLevelTxMissingTable(t *testing.T) {
	store, cleanup, groupID := newAuditedPrivilegeGroup(t)
	defer cleanup()

	tx, err := store.db.Begin()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer tx.Rollback() //nolint:errcheck // test cleanup

	if level := connectionAccessLevelTx(tx, groupID, 12345); level != "" {
		t.Errorf("Expected an empty level for a missing grant, got %q", level)
	}
}
