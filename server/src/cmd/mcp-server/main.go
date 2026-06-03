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

	// Load data_dir from config file early (before resolving data directory)
	// This allows the config file's data_dir to be used if no CLI flag is set
	var configDataDir string
	if config.ConfigFileExists(configPath) {
		var err error
		configDataDir, err = config.LoadConfigDataDir(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: failed to read data_dir from config: %v\n", err)
		}
	}

	// Resolve data directory with proper precedence:
	// 1. CLI --data-dir flag (highest)
	// 2. Config file data_dir setting
	// 3. Default relative to executable (lowest)
	dataDir := flags.ResolveDataDir(execPath, configDataDir)

	// Handle CLI commands (token, user, group, privilege management)
	if RunCLICommands(flags, dataDir) {
		return
	}

	// Load configuration. If --config was passed explicitly but
	// points at a missing file, fail loudly. Otherwise, an empty or
	// missing default just falls through to the compiled-in
	// defaults; LoadConfig is tolerant of an empty path.
	configPathForLoad := ""
	if configPath != "" && config.ConfigFileExists(configPath) {
		configPathForLoad = configPath
		fmt.Fprintf(os.Stderr, "Config: %s\n", configPath)
	} else if cliFlags.ConfigFileSet {
		fmt.Fprintf(os.Stderr, "ERROR: configuration file not found: %s\n", configPath)
		os.Exit(1)
	} else {
		fmt.Fprintf(os.Stderr,
			"Config: no config file found in default search paths "+
				"(per-user config dir, /etc/pgedge); using defaults\n")
	}
	fmt.Fprintf(os.Stderr, "Data directory: %s\n", dataDir)

	cfg, err := config.LoadConfig(configPathForLoad, cliFlags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	// Resolve the datastore password from a password_file when no
	// password was supplied via CLI flag, environment variable, or
	// inline YAML. LoadConfig has already applied those higher-priority
	// sources, so a non-empty Password here means one of them won and
	// LoadPassword is a no-op. This keeps the YAML password_file option
	// consistent with the collector and alerter components.
	if cfg.Database != nil {
		if err := cfg.Database.LoadPassword(); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			os.Exit(1)
		}
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
