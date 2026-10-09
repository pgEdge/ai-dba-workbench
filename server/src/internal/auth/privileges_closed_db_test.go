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

// TestPrivilegeReadsFailOnClosedDatabase checks that each privilege
// lookup returns its query error rather than an empty result when the
// database cannot be read.
func TestPrivilegeReadsFailOnClosedDatabase(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()
	store.db.Close()

	cases := []struct {
		name string
		call func() error
	}{
		{"RegisterMCPPrivilege", func() error {
			_, err := store.RegisterMCPPrivilege("tool_x", MCPPrivilegeTypeTool, "", false)
			return err
		}},
		{"GetMCPPrivilege", func() error {
			_, err := store.GetMCPPrivilege("tool_x")
			return err
		}},
		{"GetMCPPrivilegeByID", func() error {
			_, err := store.GetMCPPrivilegeByID(1)
			return err
		}},
		{"ListMCPPrivileges", func() error {
			_, err := store.ListMCPPrivileges()
			return err
		}},
		{"ListMCPPrivilegesByType", func() error {
			_, err := store.ListMCPPrivilegesByType(MCPPrivilegeTypeTool)
			return err
		}},
		{"GetConnectionPrivilege", func() error {
			_, err := store.GetConnectionPrivilege(1, 1)
			return err
		}},
		{"ListGroupMCPPrivileges", func() error {
			_, err := store.ListGroupMCPPrivileges(1)
			return err
		}},
		{"ListGroupConnectionPrivileges", func() error {
			_, err := store.ListGroupConnectionPrivileges(1)
			return err
		}},
		{"GetGroupWithPrivileges", func() error {
			_, err := store.GetGroupWithPrivileges(1)
			return err
		}},
		{"IsPrivilegeAssignedToAnyGroup", func() error {
			_, err := store.IsPrivilegeAssignedToAnyGroup("tool_x")
			return err
		}},
		{"GetUserMCPPrivileges", func() error {
			_, err := store.GetUserMCPPrivileges(1)
			return err
		}},
		{"GetUserConnectionPrivileges", func() error {
			_, err := store.GetUserConnectionPrivileges(1)
			return err
		}},
		{"ListGroupAdminPermissions", func() error {
			_, err := store.ListGroupAdminPermissions(1)
			return err
		}},
		{"GetUserAdminPermissions", func() error {
			_, err := store.GetUserAdminPermissions(1)
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Errorf("%s: expected an error from a closed database", tc.name)
			}
		})
	}
}
