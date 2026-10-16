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
	"errors"
	"os"
	"strings"
	"testing"
)

// quietCLI points the process's standard streams at the null device
// for the test's scope, so that the commands neither block on a prompt
// nor fill the test output, and replaces cliExit with a recorder. It
// returns the exit codes recorded.
func quietCLI(t *testing.T) *[]int {
	t.Helper()

	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("Failed to open %s: %v", os.DevNull, err)
	}
	savedIn, savedOut, savedErr := os.Stdin, os.Stdout, os.Stderr
	savedExit := cliExit
	os.Stdin, os.Stdout, os.Stderr = null, null, null

	codes := &[]int{}
	cliExit = func(code int) { *codes = append(*codes, code) }

	t.Cleanup(func() {
		os.Stdin, os.Stdout, os.Stderr = savedIn, savedOut, savedErr
		cliExit = savedExit
		null.Close()
	})

	return codes
}

// TestRunCLICommandGroupsDispatchEveryCommand selects each command in
// turn and checks that its group runs it, rather than falling through
// to the server, and that a command that fails ends the process with
// status 1. The commands run against an empty data directory, where
// most of them fail for want of the user, group or token they name;
// what is under test is the dispatch, not the commands, which have
// tests of their own.
func TestRunCLICommandGroupsDispatchEveryCommand(t *testing.T) {
	type group func(*Flags, string) bool

	tests := []struct {
		name  string
		group group
		set   func(*Flags)
	}{
		{"add token", runTokenCommands, func(f *Flags) {
			f.AddTokenCmd, f.TokenExpiry = true, "bogus"
		}},
		{"remove token", runTokenCommands, func(f *Flags) {
			f.RemoveTokenCmd = "no-such-token"
		}},
		{"list tokens", runTokenCommands, func(f *Flags) {
			f.ListTokensCmd = true
		}},
		{"add user", runUserCommands, func(f *Flags) { f.AddUserCmd = true }},
		{"update user", runUserCommands, func(f *Flags) {
			f.UpdateUserCmd, f.Username = true, "nobody"
		}},
		{"delete user", runUserCommands, func(f *Flags) {
			f.DeleteUserCmd, f.Username = true, "nobody"
		}},
		{"list users", runUserCommands, func(f *Flags) {
			f.ListUsersCmd = true
		}},
		{"enable user", runUserCommands, func(f *Flags) {
			f.EnableUserCmd, f.Username = true, "nobody"
		}},
		{"disable user", runUserCommands, func(f *Flags) {
			f.DisableUserCmd, f.Username = true, "nobody"
		}},
		{"link OIDC user", runUserCommands, func(f *Flags) {
			f.LinkOIDCUserCmd, f.Username = true, "nobody"
		}},
		{"unlink OIDC user", runUserCommands, func(f *Flags) {
			f.UnlinkOIDCUserCmd, f.Username = true, "nobody"
		}},
		{"add service account", runUserCommands, func(f *Flags) {
			f.AddServiceAccountCmd = true
		}},
		{"add group", runGroupCommands, func(f *Flags) {
			f.AddGroupCmd = true
		}},
		{"delete group", runGroupCommands, func(f *Flags) {
			f.DeleteGroupCmd, f.GroupName = true, "nogroup"
		}},
		{"list groups", runGroupCommands, func(f *Flags) {
			f.ListGroupsCmd = true
		}},
		{"add member", runGroupCommands, func(f *Flags) {
			f.AddMemberCmd, f.GroupName = true, "nogroup"
		}},
		{"remove member", runGroupCommands, func(f *Flags) {
			f.RemoveMemberCmd, f.GroupName = true, "nogroup"
		}},
		{"list members", runGroupCommands, func(f *Flags) {
			f.ListMembersCmd, f.GroupName = true, "nogroup"
		}},
		{"set superuser", runGroupCommands, func(f *Flags) {
			f.SetSuperuserCmd, f.Username = true, "nobody"
		}},
		{"unset superuser", runGroupCommands, func(f *Flags) {
			f.UnsetSuperuserCmd, f.Username = true, "nobody"
		}},
		{"grant privilege", runPrivilegeCommands, func(f *Flags) {
			f.GrantPrivilegeCmd, f.GroupName = true, "nogroup"
		}},
		{"revoke privilege", runPrivilegeCommands, func(f *Flags) {
			f.RevokePrivilegeCmd, f.GroupName = true, "nogroup"
		}},
		{"grant connection", runPrivilegeCommands, func(f *Flags) {
			f.GrantConnectionCmd, f.GroupName = true, "nogroup"
		}},
		{"revoke connection", runPrivilegeCommands, func(f *Flags) {
			f.RevokeConnectionCmd, f.GroupName = true, "nogroup"
		}},
		{"list privileges", runPrivilegeCommands, func(f *Flags) {
			f.ListPrivilegesCmd = true
		}},
		{"show group privileges", runPrivilegeCommands, func(f *Flags) {
			f.ShowGroupPrivilegesCmd, f.GroupName = true, "nogroup"
		}},
		{"register privilege", runPrivilegeCommands, func(f *Flags) {
			f.RegisterPrivilegeCmd = true
		}},
		{"scope token connections", runTokenScopeCommands, func(f *Flags) {
			f.ScopeTokenConnCmd = true
		}},
		{"scope token tools", runTokenScopeCommands, func(f *Flags) {
			f.ScopeTokenToolsCmd = true
		}},
		{"clear token scope", runTokenScopeCommands, func(f *Flags) {
			f.ClearTokenScopeCmd = true
		}},
		{"show token scope", runTokenScopeCommands, func(f *Flags) {
			f.ShowTokenScopeCmd = true
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codes := quietCLI(t)

			f := &Flags{}
			tt.set(f)
			if !tt.group(f, t.TempDir()) {
				t.Fatal("Expected the command to run")
			}
			for _, code := range *codes {
				if code != 1 {
					t.Errorf("Expected a failure to exit with status 1, "+
						"got %d", code)
				}
			}
		})
	}
}

// TestRunCLICommandGroupsIgnoreOtherCommands checks that each group
// reports that it ran nothing when none of its commands is selected,
// so that RunCLICommands moves on to the next group.
func TestRunCLICommandGroupsIgnoreOtherCommands(t *testing.T) {
	codes := quietCLI(t)

	for name, group := range map[string]func(*Flags, string) bool{
		"token":       runTokenCommands,
		"user":        runUserCommands,
		"group":       runGroupCommands,
		"privilege":   runPrivilegeCommands,
		"token scope": runTokenScopeCommands,
		"audit":       runAuditCommands,
	} {
		if group(&Flags{}, t.TempDir()) {
			t.Errorf("Expected the %s group to run nothing", name)
		}
	}
	if len(*codes) != 0 {
		t.Errorf("Expected no exit, got %v", *codes)
	}
}

// TestRunFirstSelectedExitCodes checks that a failing command ends the
// process with status 1 by default and with the status its exitCode
// gives where it has one, and that only the first selected command
// runs.
func TestRunFirstSelectedExitCodes(t *testing.T) {
	codes := quietCLI(t)
	fail := func() error { return errors.New("boom") }

	ranSecond := false
	if !runFirstSelected([]cliCommand{
		{selected: false, run: fail},
		{selected: true, run: fail},
		{selected: true, run: func() error {
			ranSecond = true
			return nil
		}},
	}) {
		t.Fatal("Expected a command to run")
	}
	if ranSecond {
		t.Error("Expected only the first selected command to run")
	}

	if !runFirstSelected([]cliCommand{{selected: true, run: fail,
		exitCode: func(error) int { return 3 }}}) {
		t.Fatal("Expected a command to run")
	}

	if len(*codes) != 2 || (*codes)[0] != 1 || (*codes)[1] != 3 {
		t.Errorf("Expected exit statuses [1 3], got %v", *codes)
	}
}

// TestRunAddTokenCommandExpiry checks how -token-expiry is read: an
// unparseable duration is refused before anything is opened, and the
// other forms reach the command, which then fails for want of the user
// in an empty data directory.
func TestRunAddTokenCommandExpiry(t *testing.T) {
	quietCLI(t)

	err := runAddTokenCommand(&Flags{TokenExpiry: "bogus"}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(),
		"invalid expiry duration") {
		t.Errorf("Expected an unparseable expiry to be refused, got %v",
			err)
	}

	for _, expiry := range []string{"30d", "never", ""} {
		err := runAddTokenCommand(&Flags{TokenExpiry: expiry,
			TokenUser: "nobody"}, t.TempDir())
		if err == nil {
			t.Errorf("Expected expiry %q to reach the command and fail "+
				"for a user that does not exist", expiry)
		} else if strings.Contains(err.Error(), "invalid expiry duration") {
			t.Errorf("Expected expiry %q to parse, got %v", expiry, err)
		}
	}
}
