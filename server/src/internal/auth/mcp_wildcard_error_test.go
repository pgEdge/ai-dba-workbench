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

import "testing"

// breakMCPWildcardCount replaces group_mcp_privileges with a view over
// the same rows whose group_id cannot be computed for the wildcard row.
// GetGroupEffectiveMCPPrivileges' identifier query never reaches that
// row, because it joins no mcp_privilege_identifiers entry, whilst the
// wildcard COUNT(*) selects it directly, so only the wildcard check
// fails (issue #471). GetUserMCPPrivileges and ListGroupMCPPrivileges
// filter on group_id first, so their identifier queries reach the row
// too and this fault cannot isolate their wildcard checks.
func breakMCPWildcardCount(t *testing.T, s *AuthStore) {
	t.Helper()

	mustExec(t, s, "ALTER TABLE group_mcp_privileges RENAME TO group_mcp_privileges_base")
	mustExec(t, s, `CREATE VIEW group_mcp_privileges AS
        SELECT id,
               CASE WHEN privilege_identifier_id = 0
                    THEN abs(-9223372036854775808)
                    ELSE group_id END AS group_id,
               privilege_identifier_id, created_at
        FROM group_mcp_privileges_base`)
}

// TestMCPWildcardCheckErrorsPropagate checks that a failed wildcard
// check in GetGroupEffectiveMCPPrivileges is returned as an error rather than read as "no wildcard",
// which would understate a group's reach when grants are bounded by a
// token's scope.
func TestMCPWildcardCheckErrorsPropagate(t *testing.T) {
	store, cleanup := createTestAuthStoreForTokenScope(t)
	defer cleanup()

	groupID, err := store.CreateGroup("wildcard-group", "")
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}
	privID, err := store.RegisterMCPPrivilege("query_database", "tool", "", false)
	if err != nil {
		t.Fatalf("RegisterMCPPrivilege failed: %v", err)
	}
	if err := store.GrantMCPPrivilege(groupID, privID); err != nil {
		t.Fatalf("GrantMCPPrivilege failed: %v", err)
	}
	if err := store.GrantMCPPrivilege(groupID, MCPPrivilegeIDWildcard); err != nil {
		t.Fatalf("GrantMCPPrivilege (wildcard) failed: %v", err)
	}

	breakMCPWildcardCount(t, store)

	if _, err := store.GetGroupEffectiveMCPPrivileges(groupID); err == nil {
		t.Error("GetGroupEffectiveMCPPrivileges: expected the wildcard check's error")
	}
}
