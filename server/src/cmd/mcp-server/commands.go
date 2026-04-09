/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package main

import (
	"fmt"
	"os"
	"time"
)

// RunCLICommands executes any CLI commands specified in flags.
// Returns true if a command was executed and the program should exit.
func RunCLICommands(f *Flags, dataDir string) bool {
	// Handle token management commands
	if f.HasTokenCommand() {
		if f.AddTokenCmd {
			if err := runAddTokenCommand(f, dataDir); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return true
		}

		if f.RemoveTokenCmd != "" {
			if err := removeTokenCommand(dataDir, f.RemoveTokenCmd); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return true
		}

		if f.ListTokensCmd {
			if err := listTokensCommand(dataDir); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return true
		}
	}

	// Handle user management commands
	if f.HasUserCommand() {
		if f.AddUserCmd {
			if err := addUserCommand(dataDir, f.Username, f.UserPassword, f.UserNote, f.FullName, f.Email); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return true
		}

		if f.UpdateUserCmd {
			if err := updateUserCommand(dataDir, f.Username, f.UserPassword, f.UserNote, f.FullName, f.Email); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return true
		}

		if f.DeleteUserCmd {
			if err := deleteUserCommand(dataDir, f.Username); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return true
		}

		if f.ListUsersCmd {
			if err := listUsersCommand(dataDir); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return true
		}

		if f.EnableUserCmd {
			if err := enableUserCommand(dataDir, f.Username); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return true
		}

		if f.DisableUserCmd {
			if err := disableUserCommand(dataDir, f.Username); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return true
		}

		if f.AddServiceAccountCmd {
			if err := addServiceAccountCommand(dataDir, f.Username, f.UserNote, f.FullName, f.Email); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return true
		}
	}

	// Handle group management commands
	if f.HasGroupCommand() {
		if f.AddGroupCmd {
			if err := addGroupCommand(dataDir, f.GroupName, ""); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return true
		}

		if f.DeleteGroupCmd {
			if err := deleteGroupCommand(dataDir, f.GroupName); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return true
		}

		if f.ListGroupsCmd {
			if err := listGroupsCommand(dataDir); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return true
		}

		if f.AddMemberCmd {
			if err := addMemberCommand(dataDir, f.GroupName, f.Username, f.MemberGroup); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return true
		}

		if f.RemoveMemberCmd {
			if err := removeMemberCommand(dataDir, f.GroupName, f.Username, f.MemberGroup); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return true
		}

		if f.SetSuperuserCmd {
			if err := setSuperuserCommand(dataDir, f.Username, true); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return true
		}

		if f.UnsetSuperuserCmd {
			if err := setSuperuserCommand(dataDir, f.Username, false); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return true
		}
	}

	// Handle privilege management commands
	if f.HasPrivilegeCommand() {
		if f.GrantPrivilegeCmd {
			if err := grantMCPPrivilegeCommand(dataDir, f.GroupName, f.PrivilegeIdentifier); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return true
		}

		if f.RevokePrivilegeCmd {
			if err := revokeMCPPrivilegeCommand(dataDir, f.GroupName, f.PrivilegeIdentifier); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return true
		}

		if f.GrantConnectionCmd {
			if err := grantConnectionPrivilegeCommand(dataDir, f.GroupName, f.ConnectionID, f.AccessLevel); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return true
		}

		if f.RevokeConnectionCmd {
			if err := revokeConnectionPrivilegeCommand(dataDir, f.GroupName, f.ConnectionID); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return true
		}

		if f.ListPrivilegesCmd {
			if err := listPrivilegesCommand(dataDir); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return true
		}

		if f.ShowGroupPrivilegesCmd {
			if err := showGroupPrivilegesCommand(dataDir, f.GroupName); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return true
		}

		if f.RegisterPrivilegeCmd {
			if err := registerPrivilegeCommand(dataDir, f.PrivilegeIdentifier, f.PrivilegeType, f.PrivilegeDescription, f.PrivilegeIsPublic); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return true
		}
	}

	// Handle token scope commands
	if f.HasTokenScopeCommand() {
		if f.ScopeTokenConnCmd {
			if err := scopeTokenConnectionsCommand(dataDir, f.TokenID, f.ScopeConnections); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return true
		}

		if f.ScopeTokenToolsCmd {
			if err := scopeTokenToolsCommand(dataDir, f.TokenID, f.ScopeTools); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return true
		}

		if f.ClearTokenScopeCmd {
			if err := clearTokenScopeCommand(dataDir, f.TokenID); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return true
		}

		if f.ShowTokenScopeCmd {
			if err := showTokenScopeCommand(dataDir, f.TokenID); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return true
		}
	}

	return false
}

// runAddTokenCommand handles the add-token command with expiry parsing
func runAddTokenCommand(f *Flags, dataDir string) error {
	var expiry time.Duration
	switch {
	case f.TokenExpiry != "" && f.TokenExpiry != "never":
		var err error
		expiry, err = parseDuration(f.TokenExpiry)
		if err != nil {
			return fmt.Errorf("invalid expiry duration: %w", err)
		}
	case f.TokenExpiry == "":
		expiry = 0 // Will prompt user
	default:
		expiry = -1 // Never expires
	}

	return addTokenCommand(dataDir, f.TokenUser, f.TokenNote, expiry)
}
