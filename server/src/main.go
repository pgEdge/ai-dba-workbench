/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package main is the entry point for the pgEdge AI Workbench MCP server
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/pgEdge/ai-workbench/server/src/config"
	"github.com/pgEdge/ai-workbench/server/src/logger"
	"github.com/pgEdge/ai-workbench/server/src/mcp"
	"github.com/pgEdge/ai-workbench/server/src/server"
)

const (
	serverName    = "pgEdge AI Workbench MCP Server"
	serverVersion = "0.1.0"
)

var (
	// Configuration file flag
	configFile = flag.String("config", "", "Path to configuration file")

	// Verbose logging flag
	verbose = flag.Bool("v", false, "Enable verbose logging")

	// MCP Server flags
	tls      = flag.Bool("tls", false, "Enable HTTPS mode")
	tlsCert  = flag.String("tls-cert", "", "Path to TLS certificate")
	tlsKey   = flag.String("tls-key", "", "Path to TLS key")
	tlsChain = flag.String("tls-chain", "", "Path to TLS certificate chain")
	port     = flag.Int("port", 8080, "Server listening port")

	// Datastore flags
	pgHost         = flag.String("pg-host", "", "PostgreSQL host")
	pgHostAddr     = flag.String("pg-hostaddr", "", "PostgreSQL host address")
	pgDatabase     = flag.String("pg-database", "", "PostgreSQL database")
	pgUsername     = flag.String("pg-username", "", "PostgreSQL username")
	pgPasswordFile = flag.String("pg-password-file", "",
		"Path to file containing PostgreSQL password")
	pgPort        = flag.Int("pg-port", 5432, "PostgreSQL port")
	pgSSLMode     = flag.String("pg-sslmode", "prefer", "PostgreSQL SSL mode")
	pgSSLCert     = flag.String("pg-sslcert", "", "PostgreSQL SSL certificate")
	pgSSLKey      = flag.String("pg-sslkey", "", "PostgreSQL SSL key")
	pgSSLRootCert = flag.String("pg-sslrootcert", "",
		"PostgreSQL SSL root certificate")
)

func main() {
	flag.Parse()

	// Initialize logger
	logger.Init()
	logger.SetVerbose(*verbose)

	logger.Startup(serverName + " v" + serverVersion)

	// Load configuration
	cfg, err := loadConfiguration()
	if err != nil {
		logger.Fatalf("Configuration error: %v", err)
	}

	// Apply command-line flags (they override config file)
	applyFlags(cfg)

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		logger.Fatalf("Invalid configuration: %v", err)
	}

	logger.Infof("Configuration loaded successfully")
	if cfg.GetTLS() {
		logger.Infof("TLS enabled: cert=%s, key=%s", cfg.GetTLSCert(),
			cfg.GetTLSKey())
	}
	logger.Infof("Server will listen on port %d", cfg.GetPort())

	// Create MCP handler
	mcpHandler := mcp.NewHandler(serverName, serverVersion)

	// Create server
	srv := server.New(cfg, mcpHandler)

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Start server in a goroutine
	go func() {
		if err := srv.Start(); err != nil {
			logger.Errorf("Server error: %v", err)
			sigChan <- os.Interrupt
		}
	}()

	logger.Startup("Server started successfully")

	// Wait for shutdown signal
	sig := <-sigChan
	logger.Infof("Received signal: %v", sig)

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Errorf("Error during shutdown: %v", err)
		os.Exit(1)
	}

	logger.Startup("Server stopped gracefully")
}

// loadConfiguration loads configuration from file or creates default
func loadConfiguration() (*config.Config, error) {
	cfg := config.NewConfig()

	// If config file is specified, load it
	if *configFile != "" {
		logger.Infof("Loading configuration from: %s", *configFile)
		if err := cfg.LoadFromFile(*configFile); err != nil {
			return nil, fmt.Errorf("failed to load config file: %w", err)
		}
	} else {
		// Try to load from default location (same directory as binary)
		execPath, err := os.Executable()
		if err == nil {
			defaultConfigPath := filepath.Join(filepath.Dir(execPath),
				"server.conf")
			if _, err := os.Stat(defaultConfigPath); err == nil {
				logger.Infof("Loading configuration from: %s",
					defaultConfigPath)
				if err := cfg.LoadFromFile(defaultConfigPath); err != nil {
					logger.Errorf("Warning: failed to load default config: %v",
						err)
				}
			}
		}
	}

	return cfg, nil
}

// applyFlags applies command-line flags to override config file values
func applyFlags(cfg *config.Config) {
	// MCP Server flags
	if *tls {
		cfg.TLS = *tls
	}
	if *tlsCert != "" {
		cfg.TLSCert = *tlsCert
	}
	if *tlsKey != "" {
		cfg.TLSKey = *tlsKey
	}
	if *tlsChain != "" {
		cfg.TLSChain = *tlsChain
	}
	if *port != 8080 {
		cfg.Port = *port
	}

	// Datastore flags
	if *pgHost != "" {
		cfg.PgHost = *pgHost
	}
	if *pgHostAddr != "" {
		cfg.PgHostAddr = *pgHostAddr
	}
	if *pgDatabase != "" {
		cfg.PgDatabase = *pgDatabase
	}
	if *pgUsername != "" {
		cfg.PgUsername = *pgUsername
	}
	if *pgPasswordFile != "" {
		// Read password from file
		content, err := os.ReadFile(*pgPasswordFile)
		if err != nil {
			logger.Errorf("Warning: failed to read password file: %v", err)
		} else {
			cfg.PgPassword = string(content)
		}
	}
	if *pgPort != 5432 {
		cfg.PgPort = *pgPort
	}
	if *pgSSLMode != "prefer" {
		cfg.PgSSLMode = *pgSSLMode
	}
	if *pgSSLCert != "" {
		cfg.PgSSLCert = *pgSSLCert
	}
	if *pgSSLKey != "" {
		cfg.PgSSLKey = *pgSSLKey
	}
	if *pgSSLRootCert != "" {
		cfg.PgSSLRootCert = *pgSSLRootCert
	}
}
