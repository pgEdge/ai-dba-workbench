/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

var (
	// Version information
	Version = "0.1.0"

	// Command line flags
	configFile = flag.String("config", "", "Path to configuration file")

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

	log.Printf("pgEdge AI Workbench Collector v%s starting...", Version)

	// Load configuration
	config, err := loadConfiguration()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize datastore connection
	ds, err := initDatastore(config)
	if err != nil {
		log.Fatalf("Failed to initialize datastore: %v", err)
	}
	defer func() {
		if cerr := ds.Close(); cerr != nil {
			log.Printf("Error closing datastore: %v", cerr)
		}
	}()

	log.Println("Datastore connection established")

	// Create context for operations
	ctx := context.Background()

	// Initialize monitored connection pool manager
	poolManager := NewMonitoredConnectionPoolManager()
	defer func() {
		if cerr := poolManager.Close(); cerr != nil {
			log.Printf("Error closing pool manager: %v", cerr)
		}
	}()

	// Initialize probe scheduler
	scheduler := NewProbeScheduler(ds, poolManager, config.ServerSecret)
	if err := scheduler.Start(ctx); err != nil {
		log.Fatalf("Failed to start probe scheduler: %v", err)
	}

	// Initialize garbage collector
	gc := NewGarbageCollector(ds)
	if err := gc.Start(ctx); err != nil {
		log.Fatalf("Failed to start garbage collector: %v", err)
	}

	log.Println("Collector is running. Press Ctrl+C to stop.")

	// Wait for shutdown signal
	waitForShutdown()

	log.Println("Shutdown signal received, stopping...")

	// Stop scheduler and garbage collector
	scheduler.Stop()
	gc.Stop()

	log.Println("Collector stopped")
}

// loadConfiguration loads configuration from file and command line
func loadConfiguration() (*Config, error) {
	config := NewConfig()

	// Determine config file path
	configPath := *configFile
	explicitConfigPath := (configPath != "")

	if configPath == "" {
		// Default to executable directory
		exe, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("failed to get executable path: %w", err)
		}
		configPath = filepath.Join(filepath.Dir(exe), "ai-workbench.conf")
	}

	// Load config file if it exists
	if _, err := os.Stat(configPath); err == nil {
		if err := config.LoadFromFile(configPath); err != nil {
			return nil, fmt.Errorf("failed to load config file: %w", err)
		}
		log.Printf("Configuration loaded from: %s", configPath)
	} else {
		// If user explicitly specified a config file that doesn't exist, fail
		if explicitConfigPath {
			return nil, fmt.Errorf("specified config file not found: %s", configPath)
		}
		// Otherwise, just log and continue with defaults
		log.Printf("Configuration file not found: %s, using defaults", configPath)
	}

	// Override with command line flags
	config.ApplyFlags()

	return config, nil
}

// waitForShutdown waits for an interrupt signal
func waitForShutdown() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan
}
