/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/alerter/internal/database"
	"github.com/pgedge/ai-workbench/alerter/internal/engine"
)

func main() {
	// Get executable path for default config location
	execPath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to get executable path: %v\n", err)
		os.Exit(1)
	}
	defaultConfigPath := config.GetDefaultConfigPath(execPath)

	// Command line flags
	configFile := flag.String("config", defaultConfigPath, "Path to configuration file")
	debug := flag.Bool("debug", false, "Enable debug logging")

	// Database connection flags
	dbHost := flag.String("db-host", "", "Database host (overrides config)")
	dbPort := flag.Int("db-port", 0, "Database port (overrides config)")
	dbName := flag.String("db-name", "", "Database name (overrides config)")
	dbUser := flag.String("db-user", "", "Database user (overrides config)")
	dbPassword := flag.String("db-password", "", "Database password (overrides config)")
	dbSSLMode := flag.String("db-sslmode", "", "Database SSL mode (overrides config)")

	flag.Parse()

	// Load configuration
	cfg := config.NewConfig()

	// Load from file if exists
	if config.ConfigFileExists(*configFile) {
		if err := cfg.LoadFromFile(*configFile); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: Failed to load config: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Configuration loaded from %s\n", *configFile)
	}

	// Apply environment variables
	cfg.LoadFromEnv()

	// Apply command line overrides
	if *dbHost != "" {
		cfg.Datastore.Host = *dbHost
	}
	if *dbPort != 0 {
		cfg.Datastore.Port = *dbPort
	}
	if *dbName != "" {
		cfg.Datastore.Database = *dbName
	}
	if *dbUser != "" {
		cfg.Datastore.Username = *dbUser
	}
	if *dbPassword != "" {
		cfg.Datastore.Password = *dbPassword
	}
	if *dbSSLMode != "" {
		cfg.Datastore.SSLMode = *dbSSLMode
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
				// Reload configuration
				newCfg := config.NewConfig()
				if config.ConfigFileExists(*configFile) {
					if err := newCfg.LoadFromFile(*configFile); err != nil {
						fmt.Fprintf(os.Stderr, "ERROR: Failed to reload config: %v\n", err)
						continue
					}
				}
				newCfg.LoadFromEnv()
				// Apply reloadable settings to the engine
				alerterEngine.ReloadConfig(newCfg)
				fmt.Fprintf(os.Stderr, "Configuration reloaded\n")
			case syscall.SIGINT, syscall.SIGTERM:
				fmt.Fprintf(os.Stderr, "\nShutting down...\n")
				cancel()
				return
			}
		}
	}()

	fmt.Fprintf(os.Stderr, "Starting alerter engine...\n")

	// Run the engine (blocks until context is cancelled)
	if err := alerterEngine.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Engine error: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Alerter stopped.\n")
}
