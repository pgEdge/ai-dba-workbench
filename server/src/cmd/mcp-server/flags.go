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
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pgedge/ai-workbench/server/internal/config"
)

// Flags holds all command-line flag values
type Flags struct {
	// Configuration
	ConfigFile string
	HTTPAddr   string
	DataDir    string
	TraceFile  string
	OpenAPI    string
	Debug      bool

	// TLS settings
	TLSMode   bool
	CertFile  string
	KeyFile   string
	ChainFile string

	// Database connection
	DBHost         string
	DBPort         int
	DBName         string
	DBUser         string
	DBPassword     string
	DBPasswordFile string
	DBSSLMode      string

	// Token management commands
	AddTokenCmd    bool
	RemoveTokenCmd string
	ListTokensCmd  bool
	TokenNote      string
	TokenExpiry    string
	TokenUser      string

	// User management commands
	AddUserCmd           bool
	UpdateUserCmd        bool
	DeleteUserCmd        bool
	ListUsersCmd         bool
	EnableUserCmd        bool
	DisableUserCmd       bool
	AddServiceAccountCmd bool
	Username             string
	UserPassword         string
	UserPasswordFile     string
	UserNote             string
	FullName             string
	Email                string

	// Group management commands
	AddGroupCmd       bool
	DeleteGroupCmd    bool
	ListGroupsCmd     bool
	AddMemberCmd      bool
	RemoveMemberCmd   bool
	GroupName         string
	MemberGroup       string
	SetSuperuserCmd   bool
	UnsetSuperuserCmd bool

	// Privilege management commands
	GrantPrivilegeCmd      bool
	RevokePrivilegeCmd     bool
	GrantConnectionCmd     bool
	RevokeConnectionCmd    bool
	ListPrivilegesCmd      bool
	ShowGroupPrivilegesCmd bool
	RegisterPrivilegeCmd   bool
	PrivilegeIdentifier    string
	PrivilegeType          string
	PrivilegeDescription   string
	ConnectionID           int
	AccessLevel            string

	// Token scope commands
	ScopeTokenConnCmd  bool
	ScopeTokenToolsCmd bool
	ClearTokenScopeCmd bool
	ShowTokenScopeCmd  bool
	TokenID            int64
	ScopeConnections   string
	ScopeTools         string
}

// ParseFlags parses command-line flags and returns a Flags struct
func ParseFlags(defaultConfigPath string) *Flags {
	f := &Flags{}

	// Configuration flags
	flag.StringVar(&f.ConfigFile, "config", defaultConfigPath, "Path to configuration file")
	flag.StringVar(&f.HTTPAddr, "addr", "", "HTTP server address")
	flag.BoolVar(&f.TLSMode, "tls", false, "Enable TLS/HTTPS")
	flag.StringVar(&f.CertFile, "cert", "", "Path to TLS certificate file")
	flag.StringVar(&f.KeyFile, "key", "", "Path to TLS key file")
	flag.StringVar(&f.ChainFile, "chain", "", "Path to TLS certificate chain file (optional)")
	flag.BoolVar(&f.Debug, "debug", false, "Enable debug logging (logs HTTP requests/responses)")
	flag.StringVar(&f.DataDir, "data-dir", "", "Data directory for auth database and conversations")
	flag.StringVar(&f.TraceFile, "trace-file", "", "Path to trace file for logging MCP requests/responses")
	flag.StringVar(&f.OpenAPI, "openapi", "", "Write OpenAPI specification to file and exit")

	// Database connection flags
	flag.StringVar(&f.DBHost, "db-host", "", "Database host")
	flag.IntVar(&f.DBPort, "db-port", 0, "Database port")
	flag.StringVar(&f.DBName, "db-name", "", "Database name")
	flag.StringVar(&f.DBUser, "db-user", "", "Database user")
	flag.StringVar(&f.DBPassword, "db-password", "", "Database password (prefer -db-password-file for production use)")
	flag.StringVar(&f.DBPasswordFile, "db-password-file", "", "Path to file containing the database password")
	flag.StringVar(&f.DBSSLMode, "db-sslmode", "", "Database SSL mode (disable, require, verify-ca, verify-full)")

	// Token management commands
	flag.BoolVar(&f.AddTokenCmd, "add-token", false, "Add a new service token")
	flag.StringVar(&f.RemoveTokenCmd, "remove-token", "", "Remove a service token by ID or hash prefix")
	flag.BoolVar(&f.ListTokensCmd, "list-tokens", false, "List all service tokens")
	flag.StringVar(&f.TokenNote, "token-note", "", "Annotation for the new token (used with -add-token)")
	flag.StringVar(&f.TokenExpiry, "token-expiry", "", "Token expiry duration: '30d', '1y', '2w', '12h', 'never' (used with -add-token)")
	flag.StringVar(&f.TokenUser, "user", "", "Owner username for the new token (used with -add-token)")

	// User management commands
	flag.BoolVar(&f.AddUserCmd, "add-user", false, "Add a new user")
	flag.BoolVar(&f.UpdateUserCmd, "update-user", false, "Update an existing user")
	flag.BoolVar(&f.DeleteUserCmd, "delete-user", false, "Delete a user")
	flag.BoolVar(&f.ListUsersCmd, "list-users", false, "List all users")
	flag.BoolVar(&f.EnableUserCmd, "enable-user", false, "Enable a user account")
	flag.BoolVar(&f.DisableUserCmd, "disable-user", false, "Disable a user account")
	flag.BoolVar(&f.AddServiceAccountCmd, "add-service-account", false, "Add a new service account")
	flag.StringVar(&f.Username, "username", "", "Username for user management commands")
	flag.StringVar(&f.UserPassword, "password", "", "Password for user management commands (prefer -password-file for production use)")
	flag.StringVar(&f.UserPasswordFile, "password-file", "", "Path to file containing the user password")
	flag.StringVar(&f.UserNote, "user-note", "", "Notes for the user (used with -add-user, -update-user)")
	flag.StringVar(&f.FullName, "full-name", "", "Full name for user management commands")
	flag.StringVar(&f.Email, "email", "", "Email address for user management commands")

	// Group management commands
	flag.BoolVar(&f.AddGroupCmd, "add-group", false, "Add a new RBAC group")
	flag.BoolVar(&f.DeleteGroupCmd, "delete-group", false, "Delete an RBAC group")
	flag.BoolVar(&f.ListGroupsCmd, "list-groups", false, "List all RBAC groups")
	flag.BoolVar(&f.AddMemberCmd, "add-member", false, "Add a user or group to a group")
	flag.BoolVar(&f.RemoveMemberCmd, "remove-member", false, "Remove a user or group from a group")
	flag.StringVar(&f.GroupName, "group", "", "Group name for group management commands")
	flag.StringVar(&f.MemberGroup, "member-group", "", "Member group name (for nested group membership)")
	flag.BoolVar(&f.SetSuperuserCmd, "set-superuser", false, "Set superuser status for a user")
	flag.BoolVar(&f.UnsetSuperuserCmd, "unset-superuser", false, "Remove superuser status from a user")

	// Privilege management commands
	flag.BoolVar(&f.GrantPrivilegeCmd, "grant-privilege", false, "Grant an MCP privilege to a group")
	flag.BoolVar(&f.RevokePrivilegeCmd, "revoke-privilege", false, "Revoke an MCP privilege from a group")
	flag.BoolVar(&f.GrantConnectionCmd, "grant-connection", false, "Grant connection access to a group")
	flag.BoolVar(&f.RevokeConnectionCmd, "revoke-connection", false, "Revoke connection access from a group")
	flag.BoolVar(&f.ListPrivilegesCmd, "list-privileges", false, "List all registered MCP privileges")
	flag.BoolVar(&f.ShowGroupPrivilegesCmd, "show-group-privileges", false, "Show privileges for a specific group")
	flag.BoolVar(&f.RegisterPrivilegeCmd, "register-privilege", false, "Register a new MCP privilege identifier")
	flag.StringVar(&f.PrivilegeIdentifier, "privilege", "", "MCP privilege identifier")
	flag.StringVar(&f.PrivilegeType, "privilege-type", "", "MCP privilege type (tool, resource, prompt)")
	flag.StringVar(&f.PrivilegeDescription, "privilege-description", "", "Description for the privilege")
	flag.IntVar(&f.ConnectionID, "connection", 0, "Connection ID for connection privileges")
	flag.StringVar(&f.AccessLevel, "access-level", "read", "Access level for connection privileges (read, read_write)")

	// Token scope commands
	flag.BoolVar(&f.ScopeTokenConnCmd, "scope-token-connections", false, "Set connection scope for a token")
	flag.BoolVar(&f.ScopeTokenToolsCmd, "scope-token-tools", false, "Set MCP tool scope for a token")
	flag.BoolVar(&f.ClearTokenScopeCmd, "clear-token-scope", false, "Clear all scope restrictions from a token")
	flag.BoolVar(&f.ShowTokenScopeCmd, "show-token-scope", false, "Show current scope for a token")
	flag.Int64Var(&f.TokenID, "token-id", 0, "Token ID for token scope commands")
	flag.StringVar(&f.ScopeConnections, "scope-connections", "", "Comma-separated list of connection IDs")
	flag.StringVar(&f.ScopeTools, "scope-tools", "", "Comma-separated list of tool names")

	flag.Parse()
	return f
}

// ToCLIFlags converts Flags to config.CLIFlags, tracking which flags were explicitly set
func (f *Flags) ToCLIFlags() config.CLIFlags {
	cliFlags := config.CLIFlags{}

	flag.Visit(func(fl *flag.Flag) {
		switch fl.Name {
		case "config":
			cliFlags.ConfigFileSet = true
			cliFlags.ConfigFile = f.ConfigFile
		case "addr":
			cliFlags.HTTPAddrSet = true
			cliFlags.HTTPAddr = f.HTTPAddr
		case "tls":
			cliFlags.TLSEnabledSet = true
			cliFlags.TLSEnabled = f.TLSMode
		case "cert":
			cliFlags.TLSCertSet = true
			cliFlags.TLSCertFile = f.CertFile
		case "key":
			cliFlags.TLSKeySet = true
			cliFlags.TLSKeyFile = f.KeyFile
		case "chain":
			cliFlags.TLSChainSet = true
			cliFlags.TLSChainFile = f.ChainFile
		case "db-host":
			cliFlags.DBHostSet = true
			cliFlags.DBHost = f.DBHost
		case "db-port":
			cliFlags.DBPortSet = true
			cliFlags.DBPort = f.DBPort
		case "db-name":
			cliFlags.DBNameSet = true
			cliFlags.DBName = f.DBName
		case "db-user":
			cliFlags.DBUserSet = true
			cliFlags.DBUser = f.DBUser
		case "db-password":
			cliFlags.DBPassSet = true
			cliFlags.DBPassword = f.DBPassword
		case "db-sslmode":
			cliFlags.DBSSLSet = true
			cliFlags.DBSSLMode = f.DBSSLMode
		case "trace-file":
			cliFlags.TraceFileSet = true
			cliFlags.TraceFile = f.TraceFile
		}
	})

	return cliFlags
}

// ToReloadCLIFlags returns CLIFlags suitable for config reload operations
func (f *Flags) ToReloadCLIFlags() config.CLIFlags {
	return config.CLIFlags{
		DBHost:     f.DBHost,
		DBPort:     f.DBPort,
		DBName:     f.DBName,
		DBUser:     f.DBUser,
		DBPassword: f.DBPassword,
		DBSSLMode:  f.DBSSLMode,
	}
}

// ResolveDataDir returns the resolved data directory path
func (f *Flags) ResolveDataDir(execPath string) string {
	if f.DataDir != "" {
		return f.DataDir
	}
	return filepath.Join(filepath.Dir(execPath), "data")
}

// HasTokenCommand returns true if any token management command was specified
func (f *Flags) HasTokenCommand() bool {
	return f.AddTokenCmd || f.RemoveTokenCmd != "" || f.ListTokensCmd
}

// HasUserCommand returns true if any user management command was specified
func (f *Flags) HasUserCommand() bool {
	return f.AddUserCmd || f.UpdateUserCmd || f.DeleteUserCmd ||
		f.ListUsersCmd || f.EnableUserCmd || f.DisableUserCmd ||
		f.AddServiceAccountCmd
}

// HasGroupCommand returns true if any group management command was specified
func (f *Flags) HasGroupCommand() bool {
	return f.AddGroupCmd || f.DeleteGroupCmd || f.ListGroupsCmd ||
		f.AddMemberCmd || f.RemoveMemberCmd ||
		f.SetSuperuserCmd || f.UnsetSuperuserCmd
}

// HasPrivilegeCommand returns true if any privilege management command was specified
func (f *Flags) HasPrivilegeCommand() bool {
	return f.GrantPrivilegeCmd || f.RevokePrivilegeCmd ||
		f.GrantConnectionCmd || f.RevokeConnectionCmd ||
		f.ListPrivilegesCmd || f.ShowGroupPrivilegesCmd ||
		f.RegisterPrivilegeCmd
}

// HasTokenScopeCommand returns true if any token scope command was specified
func (f *Flags) HasTokenScopeCommand() bool {
	return f.ScopeTokenConnCmd || f.ScopeTokenToolsCmd ||
		f.ClearTokenScopeCmd || f.ShowTokenScopeCmd
}

// HasCLICommand returns true if any CLI command (not server mode) was specified
func (f *Flags) HasCLICommand() bool {
	return f.HasTokenCommand() || f.HasUserCommand() ||
		f.HasGroupCommand() || f.HasPrivilegeCommand() ||
		f.HasTokenScopeCommand()
}

// isFlagSet returns true if the named flag was explicitly set on the command line.
func isFlagSet(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

// ResolvePasswords resolves database and user passwords from flags,
// environment variables, or files and updates the Flags struct in place.
func (f *Flags) ResolvePasswords() error {
	dbResult, err := ResolvePassword(
		f.DBPassword, isFlagSet("db-password"),
		f.DBPasswordFile,
	)
	if err != nil {
		return fmt.Errorf("resolving database password: %w", err)
	}
	if dbResult.Source != PasswordSourceNone {
		f.DBPassword = dbResult.Value
	}

	userResult, err := ResolvePassword(
		f.UserPassword, isFlagSet("password"),
		f.UserPasswordFile,
	)
	if err != nil {
		return fmt.Errorf("resolving user password: %w", err)
	}
	if userResult.Source != PasswordSourceNone {
		f.UserPassword = userResult.Value
	}

	return nil
}

// GetDefaultPaths returns default config and secret paths based on the executable path
func GetDefaultPaths() (execPath, configPath, secretPath string, err error) {
	execPath, err = os.Executable()
	if err != nil {
		return "", "", "", err
	}
	configPath = config.GetDefaultConfigPath(execPath)
	secretPath = config.GetDefaultSecretPath(execPath)
	return execPath, configPath, secretPath, nil
}
