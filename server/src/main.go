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
	"github.com/pgEdge/ai-workbench/server/src/database"
	"github.com/pgedge/ai-workbench/pkg/logger"
	"github.com/pgEdge/ai-workbench/server/src/mcp"
	"github.com/pgEdge/ai-workbench/server/src/privileges"
	"github.com/pgEdge/ai-workbench/server/src/server"
	"github.com/pgEdge/ai-workbench/server/src/usermgmt"
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

	// User account management flags
	createUser = flag.String("create-user", "", "Create a new user account")
	listUsers  = flag.Bool("list-users", false, "List all user accounts")
	deleteUser = flag.String("delete-user", "", "Delete a user account")

	// Service token management flags
	createToken = flag.String("create-token", "",
		"Create a new service token")
	listTokens = flag.Bool("list-tokens", false, "List all service tokens")
	deleteToken = flag.String("delete-token", "", "Delete a service token")
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

	// Check if any user management commands were specified
	if *createUser != "" || *listUsers || *deleteUser != "" ||
		*createToken != "" || *listTokens || *deleteToken != "" {
		handleUserManagement(cfg)
		return
	}

	logger.Infof("Configuration loaded successfully")
	if cfg.GetTLS() {
		logger.Infof("TLS enabled: cert=%s, key=%s", cfg.GetTLSCert(),
			cfg.GetTLSKey())
	}
	logger.Infof("Server will listen on port %d", cfg.GetPort())

	// Connect to database
	dbPool, err := database.Connect(cfg)
	if err != nil {
		logger.Fatalf("Failed to connect to database: %v", err)
	}
	defer dbPool.Close()
	logger.Info("Database connection established")

	// Seed MCP privilege identifiers
	ctx := context.Background()
	if err := privileges.SeedMCPPrivileges(ctx, dbPool); err != nil {
		logger.Errorf("Warning: Failed to seed MCP privilege identifiers: %v", err)
		// Continue anyway - this is not a fatal error
	} else {
		logger.Info("MCP privilege identifiers seeded successfully")
	}

	// Create MCP handler
	mcpHandler := mcp.NewHandler(serverName, serverVersion, dbPool, cfg)

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

// handleUserManagement handles user account and service token management commands
func handleUserManagement(cfg *config.Config) {
	// Connect to the database
	pool, err := database.Connect(cfg)
	if err != nil {
		logger.Fatalf("Failed to connect to database: %v", err)
	}
	defer pool.Close()

	// Handle user account commands
	if *createUser != "" {
		if err := usermgmt.CreateUser(pool, *createUser, true); err != nil {
			logger.Fatalf("Failed to create user: %v", err)
		}
		return
	}

	if *listUsers {
		if err := usermgmt.ListUsers(pool); err != nil {
			logger.Fatalf("Failed to list users: %v", err)
		}
		return
	}

	if *deleteUser != "" {
		if err := usermgmt.DeleteUser(pool, *deleteUser, true); err != nil {
			logger.Fatalf("Failed to delete user: %v", err)
		}
		return
	}

	// Handle service token commands
	if *createToken != "" {
		if err := usermgmt.CreateServiceToken(pool, *createToken, true); err != nil {
			logger.Fatalf("Failed to create service token: %v", err)
		}
		return
	}

	if *listTokens {
		if err := usermgmt.ListServiceTokens(pool); err != nil {
			logger.Fatalf("Failed to list service tokens: %v", err)
		}
		return
	}

	if *deleteToken != "" {
		if err := usermgmt.DeleteServiceToken(pool, *deleteToken, true); err != nil {
			logger.Fatalf("Failed to delete service token: %v", err)
		}
		return
	}
}
