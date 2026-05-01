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
	"github.com/pgedge/ai-workbench/collector/src/database"
	"github.com/pgedge/ai-workbench/collector/src/scheduler"
	"github.com/pgedge/ai-workbench/pkg/logger"

	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

var (
	// Version information
	Version = "1.0.0-beta1"

	// Command line flags
	configFile = flag.String("config", "", "Path to configuration file")
	verbose    = flag.Bool("v", false, "Enable verbose logging")

	// Datastore connection flags
	pgHost         = flag.String("pg-host", "", "PostgreSQL server hostname or IP address")
	pgHostAddr     = flag.String("pg-hostaddr", "", "PostgreSQL server IP address")
	pgDatabase     = flag.String("pg-database", "", "PostgreSQL database name")
	pgUsername     = flag.String("pg-username", "", "PostgreSQL username")
	pgPasswordFile = flag.String("pg-password-file", "", "Path to file containing PostgreSQL password")
	pgPort         = flag.Int("pg-port", 5432, "PostgreSQL server port")
	pgSSLMode      = flag.String("pg-sslmode", "prefer", "PostgreSQL SSL mode")
	pgSSLCert      = flag.String("pg-sslcert", "", "Path to PostgreSQL client SSL certificate")
	pgSSLKey       = flag.String("pg-sslkey", "", "Path to PostgreSQL client SSL key")
	pgSSLRootCert  = flag.String("pg-sslrootcert", "", "Path to PostgreSQL root SSL certificate")
)

func main() {
	flag.Parse()

	// Initialize logger
	logger.Init()
	logger.SetVerbose(*verbose)

	logger.Startupf("pgEdge AI DBA Workbench Collector v%s starting...", Version)

	// Load configuration
	config, err := loadConfiguration()
	if err != nil {
		logger.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize datastore connection
	ds, err := database.NewDatastore(config)
	if err != nil {
		logger.Fatalf("Failed to initialize datastore: %v", err)
	}

	logger.Startup("Datastore connection established")

	// Create context for operations
	ctx := context.Background()

	// Initialize monitored connection pool manager
	logger.Infof("Creating monitored pool manager with %d max connections per server, idle timeout %ds",
		config.Pool.MaxConnectionsPerServer, config.Pool.MonitoredMaxIdleSeconds)
	poolManager := database.NewMonitoredConnectionPoolManager(config.Pool.MaxConnectionsPerServer, config.Pool.MonitoredMaxIdleSeconds)

	// Initialize probe scheduler
	probeScheduler := scheduler.NewProbeScheduler(ds, poolManager, config, config.GetServerSecret())
	if err := probeScheduler.Start(ctx); err != nil {
		logger.Fatalf("Failed to start probe scheduler: %v", err)
	}

	// Initialize garbage collector
	gc := NewGarbageCollector(ds)
	if err := gc.Start(ctx); err != nil {
		logger.Fatalf("Failed to start garbage collector: %v", err)
	}

	logger.Startup("Collector is running. Press Ctrl+C to stop.")

	// Wait for shutdown signal
	waitForShutdown()

	logger.Startup("Shutdown signal received, stopping...")

	// Shutdown in proper order to ensure clean connection closure
	// 1. Stop probe scheduler (no new probe queries)
	probeScheduler.Stop()

	// 2. Stop garbage collector (no new cleanup queries)
	gc.Stop()

	// 3. Close monitored connection pools (all probe connections)
	logger.Info("Closing monitored connection pools...")
	if err := poolManager.Close(); err != nil {
		logger.Errorf("Error closing pool manager: %v", err)
	} else {
		logger.Info("Monitored connection pools closed")
	}

	// 4. Close datastore connection pool (last to close)
	logger.Info("Closing datastore connection pool...")
	ds.Close()
	logger.Info("Datastore connection pool closed")

	logger.Startup("Collector stopped")
}

// loadConfiguration loads configuration from file, environment, and command line.
// Priority (highest to lowest): CLI flags > environment variables > config file > defaults.
//
// When --config is not given, the function consults the shared
// helper which searches the per-user config directory first and
// /etc/pgedge second. When neither exists the function logs an
// informational message and proceeds with compiled-in defaults
// rather than failing.
func loadConfiguration() (*Config, error) {
	config := NewConfig()

	// Determine config file path. The empty string from the helper
	// is meaningful here ("no default file found"), so we track the
	// explicit-vs-default distinction separately.
	configPath := *configFile
	explicitConfigPath := (configPath != "")

	if !explicitConfigPath {
		configPath = GetDefaultConfigPath("")
	}

	if configPath == "" {
		// No --config flag and no default file present. Use
		// compiled-in defaults silently (info log only).
		logger.Info("No configuration file found in default search " +
			"paths (per-user config dir, /etc/pgedge); using defaults")
	} else if _, err := os.Stat(configPath); err == nil {
		if err := config.LoadFromFile(configPath); err != nil {
			return nil, fmt.Errorf("failed to load config file: %w", err)
		}
		logger.Startupf("Configuration loaded from: %s", configPath)
	} else if explicitConfigPath {
		// The user explicitly asked for a file that does not
		// exist; this is fatal.
		return nil, fmt.Errorf("specified config file not found: %s", configPath)
	} else {
		// Defensive branch: between the helper's stat and ours
		// the file disappeared. Fall back to defaults rather
		// than crashing.
		logger.Infof("Configuration file %s no longer exists; using defaults", configPath)
	}

	// Override with command line flags (highest priority)
	config.ApplyFlags()

	// Load password from file if specified
	if err := config.LoadPassword(); err != nil {
		return nil, err
	}

	// Load server secret from file. The helper inside LoadSecret no
	// longer needs the binary path, but we keep the call signature
	// stable for callers that might still pass it.
	if err := config.LoadSecret(""); err != nil {
		return nil, err
	}

	return config, nil
}

// waitForShutdown waits for an interrupt signal
func waitForShutdown() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan
}
