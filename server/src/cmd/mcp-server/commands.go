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

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
)

// cliSecretSource carries what the command line needs in order to find
// the server secret by the same rules the server uses. RunCLICommands
// sets it once, from values main resolved, before it dispatches
// anything; nothing else writes it. It is package state rather than a
// parameter because the alternative is threading it through every one
// of the thirty-odd command functions that open the store, which would
// obscure far more than it documents.
var cliSecretSource struct {
	// configuredPath is secret_file from the configuration file, or ""
	// when the file sets none or does not exist.
	configuredPath string
}

// cliAuditKey resolves the audit chain key for a command-line
// invocation. It is a variable so that the test binary can substitute a
// fixed key in TestMain: tests have no server secret, and a test whose
// outcome depended on whether the host happens to have
// /etc/pgedge/ai-dba-server.secret would be worse than useless.
var cliAuditKey = resolveCLIAuditKey

// resolveCLIAuditKey reads the server secret and derives the audit
// chain key from it.
//
// A missing or unreadable secret is a hard failure, not a fallback to
// an unkeyed row: the command line writes audit events for everything
// it does, and a row written under no key, or not written at all, would
// leave the log quietly less trustworthy than it claims to be. The
// server treats the same condition as fatal, so the two agree.
func resolveCLIAuditKey() ([]byte, error) {
	serverSecret, err := loadServerSecretFile(cliSecretSource.configuredPath)
	if err != nil {
		return nil, fmt.Errorf("%w\n"+
			"       Command-line changes are recorded in the audit log, "+
			"whose hash chain is keyed by this secret, so they cannot be "+
			"made without it", err)
	}

	key := auth.DeriveAuditKey(serverSecret)
	if len(key) == 0 {
		return nil, fmt.Errorf("the server secret is empty, so the audit " +
			"chain key cannot be derived")
	}

	return key, nil
}

// loadCLISecretFile reads secret_file from the configuration file for
// the command line, which runs before the configuration proper is
// loaded.
//
// A configuration file that cannot be read or parsed is fatal rather
// than a warning. The two are not close: on a warning, configuredPath
// stays empty, the default search order takes over, and the command
// then keys its audit rows from whatever secret it finds there, which
// may not be the one the running server uses. A command writing rows
// the server cannot verify is precisely the divergence this plumbing
// exists to prevent, and a malformed config is no reason to guess.
func loadCLISecretFile(configPath string) (string, error) {
	if !config.ConfigFileExists(configPath) {
		return "", nil
	}

	secretFile, err := config.LoadConfigSecretFile(configPath)
	if err != nil {
		return "", fmt.Errorf("failed to read secret_file from the "+
			"configuration file %s: %w\n"+
			"       The audit log's hash chain is keyed by the server "+
			"secret that setting names, so a command line that guessed at "+
			"it would write rows the server cannot verify; fix the "+
			"configuration file before retrying", configPath, err)
	}

	return secretFile, nil
}

// openAuthStoreCLI opens the auth store and prints its path to stderr.
// This helps users diagnose issues where CLI commands and the server
// use different auth.db paths.
func openAuthStoreCLI(dataDir string) (*auth.AuthStore, error) {
	auditKey, err := cliAuditKey()
	if err != nil {
		return nil, err
	}

	store, err := auth.NewAuthStore(dataDir, 0, 0, auditKey)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(os.Stderr, "Auth store: %s\n", store.Path())
	return store, nil
}

// RunCLICommands executes any CLI commands specified in flags.
// Returns true if a command was executed and the program should exit.
//
// secretFile is secret_file as the configuration file gives it, which
// main reads ahead of the full configuration load, so that a command
// finds the same server secret the server itself would.
func RunCLICommands(f *Flags, dataDir, secretFile string) bool {
	cliSecretSource.configuredPath = secretFile

	for _, run := range []func(*Flags, string) bool{
		runTokenCommands,
		runUserCommands,
		runGroupCommands,
		runPrivilegeCommands,
		runTokenScopeCommands,
		runAuditCommands,
	} {
		if run(f, dataDir) {
			return true
		}
	}

	return false
}

// cliCommand pairs the flag that selects a command with the call that
// runs it. exitCode maps a failure to a process exit status; a nil
// exitCode means the usual 1.
type cliCommand struct {
	selected bool
	run      func() error
	exitCode func(error) int
}

// runFirstSelected runs the first command its flag selects and reports
// whether one ran. A command that fails reports the error and ends the
// process, so no caller here sees one.
func runFirstSelected(commands []cliCommand) bool {
	for _, cmd := range commands {
		if !cmd.selected {
			continue
		}
		if err := cmd.run(); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			code := 1
			if cmd.exitCode != nil {
				code = cmd.exitCode(err)
			}
			os.Exit(code)
		}
		return true
	}

	return false
}

// runTokenCommands runs the token management command named by the flags,
// reporting whether one ran.
func runTokenCommands(f *Flags, dataDir string) bool {
	if !f.HasTokenCommand() {
		return false
	}

	return runFirstSelected([]cliCommand{
		{
			selected: f.AddTokenCmd,
			run: func() error {
				return runAddTokenCommand(f, dataDir)
			},
		},
		{
			selected: f.RemoveTokenCmd != "",
			run: func() error {
				return removeTokenCommand(dataDir, f.RemoveTokenCmd)
			},
		},
		{
			selected: f.ListTokensCmd,
			run: func() error {
				return listTokensCommand(dataDir)
			},
		},
	})
}

// runUserCommands runs the user management command named by the flags,
// reporting whether one ran.
func runUserCommands(f *Flags, dataDir string) bool {
	if !f.HasUserCommand() {
		return false
	}

	return runFirstSelected([]cliCommand{
		{
			selected: f.AddUserCmd,
			run: func() error {
				return addUserCommand(dataDir, f.Username, f.UserPassword, f.UserNote, f.FullName, f.Email)
			},
		},
		{
			selected: f.UpdateUserCmd,
			run: func() error {
				return updateUserCommand(dataDir, f.Username, f.UserPassword, f.UserNote, f.FullName, f.Email)
			},
		},
		{
			selected: f.DeleteUserCmd,
			run: func() error {
				return deleteUserCommand(dataDir, f.Username)
			},
		},
		{
			selected: f.ListUsersCmd,
			run: func() error {
				return listUsersCommand(dataDir)
			},
		},
		{
			selected: f.EnableUserCmd,
			run: func() error {
				return enableUserCommand(dataDir, f.Username)
			},
		},
		{
			selected: f.DisableUserCmd,
			run: func() error {
				return disableUserCommand(dataDir, f.Username)
			},
		},
		{
			selected: f.LinkOIDCUserCmd,
			run: func() error {
				return linkOIDCUserCommand(dataDir, f.Username, f.OIDCIssuer, f.OIDCSubject, f.Relink)
			},
		},
		{
			selected: f.UnlinkOIDCUserCmd,
			run: func() error {
				return unlinkOIDCUserCommand(dataDir, f.Username, f.RestorePassword)
			},
		},
		{
			selected: f.AddServiceAccountCmd,
			run: func() error {
				return addServiceAccountCommand(dataDir, f.Username, f.UserNote, f.FullName, f.Email)
			},
		},
	})
}

// runGroupCommands runs the group management command named by the flags,
// reporting whether one ran.
func runGroupCommands(f *Flags, dataDir string) bool {
	if !f.HasGroupCommand() {
		return false
	}

	return runFirstSelected([]cliCommand{
		{
			selected: f.AddGroupCmd,
			run: func() error {
				return addGroupCommand(dataDir, f.GroupName, f.GroupDescription)
			},
		},
		{
			selected: f.DeleteGroupCmd,
			run: func() error {
				return deleteGroupCommand(dataDir, f.GroupName)
			},
		},
		{
			selected: f.ListGroupsCmd,
			run: func() error {
				return listGroupsCommand(dataDir)
			},
		},
		{
			selected: f.AddMemberCmd,
			run: func() error {
				return addMemberCommand(dataDir, f.GroupName, f.Username, f.MemberGroup)
			},
		},
		{
			selected: f.RemoveMemberCmd,
			run: func() error {
				return removeMemberCommand(dataDir, f.GroupName, f.Username, f.MemberGroup)
			},
		},
		{
			selected: f.ListMembersCmd,
			run: func() error {
				return listGroupMembersCommand(dataDir, f.GroupName)
			},
		},
		{
			selected: f.SetSuperuserCmd,
			run: func() error {
				return setSuperuserCommand(dataDir, f.Username, true)
			},
		},
		{
			selected: f.UnsetSuperuserCmd,
			run: func() error {
				return setSuperuserCommand(dataDir, f.Username, false)
			},
		},
	})
}

// runPrivilegeCommands runs the privilege management command named by the flags,
// reporting whether one ran.
func runPrivilegeCommands(f *Flags, dataDir string) bool {
	if !f.HasPrivilegeCommand() {
		return false
	}

	return runFirstSelected([]cliCommand{
		{
			selected: f.GrantPrivilegeCmd,
			run: func() error {
				return grantMCPPrivilegeCommand(dataDir, f.GroupName, f.PrivilegeIdentifier)
			},
		},
		{
			selected: f.RevokePrivilegeCmd,
			run: func() error {
				return revokeMCPPrivilegeCommand(dataDir, f.GroupName, f.PrivilegeIdentifier)
			},
		},
		{
			selected: f.GrantConnectionCmd,
			run: func() error {
				return grantConnectionPrivilegeCommand(dataDir, f.GroupName, f.ConnectionID, f.AccessLevel)
			},
		},
		{
			selected: f.RevokeConnectionCmd,
			run: func() error {
				return revokeConnectionPrivilegeCommand(dataDir, f.GroupName, f.ConnectionID)
			},
		},
		{
			selected: f.ListPrivilegesCmd,
			run: func() error {
				return listPrivilegesCommand(dataDir)
			},
		},
		{
			selected: f.ShowGroupPrivilegesCmd,
			run: func() error {
				return showGroupPrivilegesCommand(dataDir, f.GroupName)
			},
		},
		{
			selected: f.RegisterPrivilegeCmd,
			run: func() error {
				return registerPrivilegeCommand(dataDir, f.PrivilegeIdentifier, f.PrivilegeType, f.PrivilegeDescription, f.PrivilegeIsPublic)
			},
		},
	})
}

// runTokenScopeCommands runs the token scope command named by the flags,
// reporting whether one ran.
func runTokenScopeCommands(f *Flags, dataDir string) bool {
	if !f.HasTokenScopeCommand() {
		return false
	}

	return runFirstSelected([]cliCommand{
		{
			selected: f.ScopeTokenConnCmd,
			run: func() error {
				return scopeTokenConnectionsCommand(dataDir, f.TokenID, f.ScopeConnections)
			},
		},
		{
			selected: f.ScopeTokenToolsCmd,
			run: func() error {
				return scopeTokenToolsCommand(dataDir, f.TokenID, f.ScopeTools)
			},
		},
		{
			selected: f.ClearTokenScopeCmd,
			run: func() error {
				return clearTokenScopeCommand(dataDir, f.TokenID)
			},
		},
		{
			selected: f.ShowTokenScopeCmd,
			run: func() error {
				return showTokenScopeCommand(dataDir, f.TokenID)
			},
		},
	})
}

// runAuditCommands runs the audit log command named by the flags,
// reporting whether one ran.
func runAuditCommands(f *Flags, dataDir string) bool {
	if !f.HasAuditCommand() {
		return false
	}

	return runFirstSelected([]cliCommand{
		{
			selected: f.ListAuditCmd,
			run: func() error {
				return listAuditCommand(dataDir, f)
			},
		},
		{
			selected: f.VerifyAuditCmd,
			run: func() error {
				return verifyAuditLogCommand(dataDir)
			},
			// Unlike the other commands, this one distinguishes a
			// log that contradicts its own chain from one the key
			// cannot open at all; see auditVerifyExitCode.
			exitCode: auditVerifyExitCode,
		},
		{
			selected: f.RechainAuditCmd,
			run: func() error {
				return rechainAuditLogCommand(dataDir, f.ConfirmRechain,
					os.Stdin, os.Stdout)
			},
		},
	})
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
