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
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/api"
	"github.com/pgedge/ai-workbench/server/internal/config"
)

// schemaHealthCheckTimeout bounds the startup datastore health probe
// so a hung or unreachable datastore does not block startup
// indefinitely. The probe issues only short metadata queries, so a
// generous timeout still surfaces real failures quickly.
const schemaHealthCheckTimeout = 30 * time.Second

func main() {
	// Get default paths based on executable location
	execPath, defaultConfigPath, _, err := GetDefaultPaths()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to get executable path: %v\n", err)
		os.Exit(1)
	}

	// Parse command-line flags
	flags := ParseFlags(defaultConfigPath)

	// Handle -openapi flag: write spec to file and exit
	if flags.OpenAPI != "" {
		spec := api.BuildOpenAPISpec()
		data, err := json.MarshalIndent(spec, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: marshaling OpenAPI spec: %v\n", err)
			os.Exit(1)
		}
		data = append(data, '\n')
		//nolint:gosec // G306: OpenAPI spec is a public documentation file; world-readable permissions are intentional.
		if err := os.WriteFile(flags.OpenAPI, data, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: writing OpenAPI spec to %s: %v\n", flags.OpenAPI, err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Resolve passwords from flags, environment variables, or files
	if err := flags.ResolvePasswords(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	// Build CLIFlags for config loading
	cliFlags := flags.ToCLIFlags()

	// Determine config file path
	configPath := flags.ConfigFile
	if !cliFlags.ConfigFileSet {
		configPath = defaultConfigPath
	}

	// Load data_dir from the config file early, before resolving the
	// data directory, so that the file's setting applies when no CLI
	// flag was given.
	//
	// A config file that cannot be read is fatal rather than a warning,
	// for the same reason as loadCLISecretFile below: on a warning,
	// configDataDir stays empty, the default search order takes over,
	// and every command then operates on a different auth.db from the
	// one the server uses. A user added to the wrong database, or an
	// audit log verified in the wrong place, is a worse outcome than a
	// refusal to start, and a malformed config is no reason to guess.
	var configDataDir string
	if config.ConfigFileExists(configPath) {
		var err error
		configDataDir, err = config.LoadConfigDataDir(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: failed to read data_dir from "+
				"the configuration file %s: %v\n", configPath, err)
			os.Exit(1)
		}
	}

	// Resolve data directory with proper precedence:
	// 1. CLI --data-dir flag (highest)
	// 2. Config file data_dir setting
	// 3. Default relative to executable (lowest)
	dataDir := flags.ResolveDataDir(execPath, configDataDir)

	// Read secret_file the same way data_dir was read above, and for
	// the same reason: the commands below run before the full
	// configuration is loaded, and each of them writes an audit row
	// whose hash chain is keyed by the server secret.
	configSecretFile, err := loadCLISecretFile(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
	if err := loadCLITokenPolicy(configPath); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	// Handle -check-config: load and validate the configuration file as
	// start-up does, without opening the auth store or any database, and
	// exit with the error start-up would have logged. Start-up's checks
	// of the host (TLS certificate files, the database section, the
	// server secret) are not part of it; see runCheckConfig.
	if flags.CheckConfig {
		if err := runCheckConfig(configPath, cliFlags, os.Stderr); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Handle CLI commands (token, user, group, privilege management)
	if RunCLICommands(flags, dataDir, configSecretFile) {
		return
	}

	// Load and validate the configuration. The same two calls serve
	// -check-config above, so that the check cannot drift from what
	// start-up refuses.
	configPathForLoad, err := resolveConfigLoadPath(configPath, cliFlags.ConfigFileSet, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Data directory: %s\n", dataDir)

	cfg, err := loadStartupConfig(configPathForLoad, cliFlags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	// Create and initialize the server
	server, err := NewServer(&ServerConfig{
		Config:   cfg,
		DataDir:  dataDir,
		ExecPath: execPath,
		Debug:    flags.Debug,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
	defer server.Close()

	// Verify the collector-owned datastore schema before wiring any
	// handlers. A missing or partial schema would otherwise let the
	// server come up and 500 on every dashboard endpoint, which is
	// strictly worse than failing fast here with an actionable
	// message that names the affected datastore.
	healthCtx, healthCancel := context.WithTimeout(context.Background(), schemaHealthCheckTimeout)
	if err := server.VerifySchemaHealth(healthCtx); err != nil {
		healthCancel()
		fmt.Fprintf(os.Stderr, "[ERROR] datastore schema health check failed: %v\n", err)
		os.Exit(1)
	}
	healthCancel()

	// Run the server (blocks until shutdown)
	if err := server.Run(flags, configPath); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
}
