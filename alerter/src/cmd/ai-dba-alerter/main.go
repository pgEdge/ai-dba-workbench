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
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/alerter/internal/database"
	"github.com/pgedge/ai-workbench/alerter/internal/engine"
	"github.com/pgedge/ai-workbench/pkg/fileutil"
)

// Version information
const Version = "1.0.0-beta1"

// resolveConfigPathResult describes the outcome of a config-path
// lookup. Exposed so main() and its unit tests share the same shape.
type resolveConfigPathResult struct {
	// Path is the resolved path. It is empty if no config file was
	// found in any default location and no explicit path was given.
	Path string
	// Explicit is true if the user passed --config on the command
	// line. When Explicit is true and Path is non-empty but the
	// file is missing, callers must error out rather than fall back
	// to defaults.
	Explicit bool
}

// resolveConfigPath returns the config path to use, given the value
// of the --config flag. When --config is empty, the shared
// discovery helper consults the per-user config directory first
// and /etc/pgedge second. The returned struct tells callers
// whether the path came from an explicit flag (in which case a
// missing file is fatal) or from auto-discovery (in which case a
// missing file means "use defaults").
//
// The helper is its own function so we can exercise both branches
// in tests without driving the alerter's main() entry point.
func resolveConfigPath(flagValue string) resolveConfigPathResult {
	if flagValue != "" {
		return resolveConfigPathResult{Path: flagValue, Explicit: true}
	}
	return resolveConfigPathResult{
		Path:     config.GetDefaultConfigPath(""),
		Explicit: false,
	}
}

// reloadFlagOverrides bundles the CLI flag values that survive
// across a SIGHUP reload and must be reapplied to the freshly
// loaded config so operators do not lose their command-line
// overrides on every reload.
type reloadFlagOverrides struct {
	DBHost         string
	DBPort         int
	DBName         string
	DBUser         string
	DBPasswordFile string
	DBSSLMode      string
}

// reloadConfigOnSignal builds a fresh *config.Config from the same
// discovery rules used at startup, applies the original flag
// overrides, validates the result, and returns it. The function
// is intentionally side-effect free apart from logging to the
// supplied io.Writer: callers (typically the SIGHUP handler in
// main) decide whether to install the new config and notify the
// running engine.
//
// The "no candidate file found" and "candidate file vanished"
// cases both return (nil, nil) so the caller can keep the
// running config rather than silently downgrade to compiled-in
// defaults. A non-nil error indicates a hard failure (read error,
// invalid YAML, validation failure, etc.) - again the caller
// keeps the current config.
//
// Splitting the reload logic out of main() keeps the goroutine
// that owns the signal channel small and lets us cover the
// branches in unit tests without driving the binary entry point.
func reloadConfigOnSignal(
	logOut io.Writer,
	prevPath string,
	explicit bool,
	overrides reloadFlagOverrides,
) (*config.Config, error) {
	reloadPath := prevPath
	if !explicit {
		reloadPath = config.GetDefaultConfigPath("")
	}
	if reloadPath == "" {
		fmt.Fprintf(logOut,
			"ERROR: No configuration file found in default search "+
				"paths during SIGHUP reload; keeping current config\n")
		return nil, nil
	}
	if !config.ConfigFileExists(reloadPath) {
		fmt.Fprintf(logOut,
			"ERROR: Configuration file %s not found during SIGHUP "+
				"reload; keeping current config\n", reloadPath)
		return nil, nil
	}

	newCfg := config.NewConfig()
	if err := newCfg.LoadFromFile(reloadPath); err != nil {
		return nil, fmt.Errorf("failed to reload config: %w", err)
	}

	if err := applyFlagOverrides(newCfg,
		overrides.DBHost, overrides.DBPort, overrides.DBName,
		overrides.DBUser, overrides.DBPasswordFile, overrides.DBSSLMode,
	); err != nil {
		return nil, fmt.Errorf("failed to apply overrides on reload: %w", err)
	}

	if err := newCfg.Validate(); err != nil {
		return nil, fmt.Errorf("reloaded config is invalid: %w", err)
	}

	if err := newCfg.LoadPassword(); err != nil {
		return nil, fmt.Errorf("failed to load password on reload: %w", err)
	}

	// API keys are non-critical: an error here is logged but does
	// not block the reload.
	if err := newCfg.LoadAPIKeys(); err != nil {
		fmt.Fprintf(logOut,
			"WARNING: Failed to load API keys on reload: %v\n", err)
	}

	fmt.Fprintf(logOut, "Configuration reloaded from %s\n", reloadPath)
	return newCfg, nil
}

func main() {
	fmt.Fprintf(os.Stderr, "pgEdge AI DBA Workbench Alerter v%s starting...\n", Version)

	// Command line flags. The --config default is left empty so we
	// can distinguish "user passed an explicit path" from "user
	// relied on default discovery"; the actual default lookup
	// happens after flag.Parse below.
	configFile := flag.String("config", "", "Path to configuration file (default: per-user pgedge config dir, then /etc/pgedge)")
	debug := flag.Bool("debug", false, "Enable debug logging")

	// Database connection flags
	dbHost := flag.String("db-host", "", "Database host (overrides config)")
	dbPort := flag.Int("db-port", 0, "Database port (overrides config)")
	dbName := flag.String("db-name", "", "Database name (overrides config)")
	dbUser := flag.String("db-user", "", "Database user (overrides config)")
	dbPasswordFile := flag.String("db-password-file", "", "Path to file containing the database password")
	dbSSLMode := flag.String("db-sslmode", "", "Database SSL mode (overrides config)")

	flag.Parse()

	// Resolve the config path: an explicit flag wins, otherwise the
	// shared discovery helper picks the highest-priority path that
	// exists. If neither exists, the resolved path is "" and the
	// alerter proceeds with compiled-in defaults.
	resolved := resolveConfigPath(*configFile)
	explicitConfigPath := resolved.Explicit
	resolvedConfigPath := resolved.Path

	// Load configuration
	cfg := config.NewConfig()

	// Load from file if it was explicitly requested or auto-discovered.
	if resolvedConfigPath != "" {
		if !config.ConfigFileExists(resolvedConfigPath) {
			if explicitConfigPath {
				fmt.Fprintf(os.Stderr, "ERROR: configuration file not found: %s\n", resolvedConfigPath)
				os.Exit(1)
			}
			// The helper said the file was there but it has since
			// vanished; fall through to defaults rather than
			// crashing.
			resolvedConfigPath = ""
		}
	}
	if resolvedConfigPath != "" {
		if err := cfg.LoadFromFile(resolvedConfigPath); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: Failed to load config: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Configuration loaded from %s\n", resolvedConfigPath)
	} else {
		fmt.Fprintf(os.Stderr,
			"No configuration file found in default search paths "+
				"(per-user config dir, /etc/pgedge); using defaults\n")
	}

	// Apply command line overrides
	if err := applyFlagOverrides(cfg, *dbHost, *dbPort, *dbName, *dbUser, *dbPasswordFile, *dbSSLMode); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Invalid configuration: %v\n", err)
		os.Exit(1)
	}

	// Load password from file if needed
	if err := cfg.LoadPassword(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	// Load API keys for LLM providers
	if err := cfg.LoadAPIKeys(); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: %v\n", err)
		// Continue without API keys - they may not be needed
	}

	// Initialize datastore connection
	datastore, err := database.NewDatastore(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to connect to datastore: %v\n", err)
		os.Exit(1)
	}
	defer datastore.Close()
	fmt.Fprintf(os.Stderr, "Datastore: connected to %s@%s:%d/%s\n",
		cfg.Datastore.Username, cfg.Datastore.Host, cfg.Datastore.Port, cfg.Datastore.Database)

	// Create cancellable context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create and start the alerter engine
	alerterEngine := engine.NewEngine(cfg, datastore, *debug)

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	go func() {
		for sig := range sigChan {
			switch sig {
			case syscall.SIGHUP:
				fmt.Fprintf(os.Stderr, "Received SIGHUP, reloading configuration...\n")
				// Delegate the reload work to a side-effect-free
				// helper so the SIGHUP handler stays small and
				// the reload logic stays testable. A nil config
				// (with a nil error) means "the helper logged
				// the reason and we should keep the current
				// running config" - never replace the live
				// config with compiled-in defaults.
				newCfg, err := reloadConfigOnSignal(
					os.Stderr,
					resolvedConfigPath,
					explicitConfigPath,
					reloadFlagOverrides{
						DBHost:         *dbHost,
						DBPort:         *dbPort,
						DBName:         *dbName,
						DBUser:         *dbUser,
						DBPasswordFile: *dbPasswordFile,
						DBSSLMode:      *dbSSLMode,
					},
				)
				if err != nil {
					fmt.Fprintf(os.Stderr,
						"ERROR: %v; keeping current config\n", err)
					continue
				}
				if newCfg == nil {
					// Helper already logged the reason. Skip
					// applying anything; the running engine
					// continues with its existing config.
					continue
				}

				alerterEngine.ReloadConfig(newCfg)
			case syscall.SIGINT, syscall.SIGTERM:
				fmt.Fprintf(os.Stderr, "\nShutting down...\n")
				cancel()
				return
			}
		}
	}()

	fmt.Fprintf(os.Stderr, "Starting alerter engine...\n")

	// Run the engine (blocks until context is canceled)
	if err := alerterEngine.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Engine error: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Alerter stopped.\n")
}

// applyFlagOverrides applies CLI flag values to the configuration, allowing
// command-line arguments to take precedence over the configuration file.
func applyFlagOverrides(cfg *config.Config, dbHost string, dbPort int, dbName, dbUser, dbPasswordFile, dbSSLMode string) error {
	if dbHost != "" {
		cfg.Datastore.Host = dbHost
	}
	if dbPort != 0 {
		cfg.Datastore.Port = dbPort
	}
	if dbName != "" {
		cfg.Datastore.Database = dbName
	}
	if dbUser != "" {
		cfg.Datastore.Username = dbUser
	}
	if dbPasswordFile != "" {
		password, err := fileutil.ReadTrimmedFileWithTilde(dbPasswordFile)
		if err != nil {
			return fmt.Errorf("failed to read password file: %w", err)
		}
		cfg.Datastore.Password = password
	}
	if dbSSLMode != "" {
		cfg.Datastore.SSLMode = dbSSLMode
	}
	return nil
}
