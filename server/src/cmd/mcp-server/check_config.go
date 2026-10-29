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
	"io"

	"github.com/pgedge/ai-workbench/server/internal/config"
)

// resolveConfigLoadPath returns the configuration file start-up loads,
// or "" for the compiled-in defaults, and notes the choice on w. A file
// named with -config that does not exist is an error; a missing default
// is not, because LoadConfig falls back to the defaults for an empty
// path.
func resolveConfigLoadPath(configPath string, configFileSet bool, w io.Writer) (string, error) {
	if configPath != "" && config.ConfigFileExists(configPath) {
		fmt.Fprintf(w, "Config: %s\n", configPath)
		return configPath, nil
	}
	if configFileSet {
		return "", fmt.Errorf("configuration file not found: %s", configPath)
	}
	fmt.Fprintf(w,
		"Config: no config file found in default search paths "+
			"(per-user config dir, /etc/pgedge); using defaults\n")
	return "", nil
}

// loadStartupConfig loads and validates the configuration as start-up
// does, including the datastore password_file, which is read when no
// password was supplied on the command line or inline in the file;
// LoadConfig has already applied those higher-priority sources, so a
// non-empty password here means one of them won and LoadPassword is a
// no-op. This keeps the YAML password_file option consistent with the
// collector and alerter. It opens no database and no auth store.
func loadStartupConfig(configPath string, cliFlags config.CLIFlags) (*config.Config, error) {
	cfg, err := config.LoadConfig(configPath, cliFlags)
	if err != nil {
		return nil, err
	}
	if cfg.Database != nil {
		if err := cfg.Database.LoadPassword(); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

// runCheckConfig is -check-config: it runs the configuration steps of
// start-up and nothing else, so that a configuration this release would
// refuse is caught before the release is installed. The Debian and RPM
// packages run it from the copy of the new binary in the
// pgedge-ai-dba-upgrade-check package before any other package's files
// are replaced (issue #784). The error it returns is the one start-up
// would have logged.
//
// It deliberately does not read the server secret. The secret's
// presence is a property of the host, not of the configuration, so an
// upgrade cannot change it; and the collector and alerter packages
// depend on the server package, which ships the configuration file, so
// a host that runs only the collector has a server configuration but no
// server secret, and checking for one would refuse its upgrades.
func runCheckConfig(configPath string, cliFlags config.CLIFlags, w io.Writer) error {
	configPathForLoad, err := resolveConfigLoadPath(configPath, cliFlags.ConfigFileSet, w)
	if err != nil {
		return err
	}
	if _, err := loadStartupConfig(configPathForLoad, cliFlags); err != nil {
		return err
	}
	fmt.Fprintf(w, "Configuration OK\n")
	return nil
}
