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

		if f.LinkOIDCUserCmd {
			if err := linkOIDCUserCommand(dataDir, f.Username, f.OIDCIssuer, f.OIDCSubject, f.Relink); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return true
		}

		if f.UnlinkOIDCUserCmd {
			if err := unlinkOIDCUserCommand(dataDir, f.Username, f.RestorePassword); err != nil {
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
			if err := addGroupCommand(dataDir, f.GroupName, f.GroupDescription); err != nil {
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

		if f.ListMembersCmd {
			if err := listGroupMembersCommand(dataDir, f.GroupName); err != nil {
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

	// Handle audit log commands
	if f.HasAuditCommand() {
		if f.ListAuditCmd {
			if err := listAuditCommand(dataDir, f); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				os.Exit(1)
			}
			return true
		}

		if f.VerifyAuditCmd {
			if err := verifyAuditLogCommand(dataDir); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				// Unlike the other commands, this one distinguishes a
				// log that contradicts its own chain from one the key
				// cannot open at all; see auditVerifyExitCode.
				os.Exit(auditVerifyExitCode(err))
			}
			return true
		}

		if f.RechainAuditCmd {
			if err := rechainAuditLogCommand(dataDir, f.ConfirmRechain,
				os.Stdin, os.Stdout); err != nil {
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
