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

	"github.com/pgedge/ai-workbench/server/internal/config"
)

func main() {
	// Get default paths based on executable location
	execPath, defaultConfigPath, _, err := GetDefaultPaths()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to get executable path: %v\n", err)
		os.Exit(1)
	}

	// Parse command-line flags
	flags := ParseFlags(defaultConfigPath)

	// Resolve passwords from flags, environment variables, or files
	if err := flags.ResolvePasswords(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	// Resolve data directory
	dataDir := flags.ResolveDataDir(execPath)

	// Handle CLI commands (token, user, group, privilege management)
	if RunCLICommands(flags, dataDir) {
		return
	}

	// Build CLIFlags for config loading
	cliFlags := flags.ToCLIFlags()

	// Determine config file path
	configPath := flags.ConfigFile
	if !cliFlags.ConfigFileSet {
		configPath = defaultConfigPath
	}

	// Load configuration
	configPathForLoad := ""
	if config.ConfigFileExists(configPath) {
		configPathForLoad = configPath
	}

	cfg, err := config.LoadConfig(configPathForLoad, cliFlags)
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

	// Run the server (blocks until shutdown)
	if err := server.Run(flags, configPath); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
}
