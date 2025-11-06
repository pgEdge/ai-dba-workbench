/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package config provides configuration handling for the MCP server
package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds all configuration options for the MCP server
type Config struct {
	// MCP Server settings
	TLS      bool   // Enable HTTPS mode
	TLSCert  string // Path to TLS certificate file
	TLSKey   string // Path to TLS key file
	TLSChain string // Path to TLS certificate chain file
	Port     int    // Server listening port

	// Datastore connection settings
	PgHost        string
	PgHostAddr    string
	PgDatabase    string
	PgUsername    string
	PgPassword    string
	PgPort        int
	PgSSLMode     string
	PgSSLCert     string
	PgSSLKey      string
	PgSSLRootCert string

	// Server secret for encryption
	ServerSecret string
}

// NewConfig creates a new Config with default values
func NewConfig() *Config {
	return &Config{
		TLS:        false,
		Port:       8080,
		PgHost:     "localhost",
		PgDatabase: "ai_workbench",
		PgUsername: "postgres",
		PgPort:     5432,
		PgSSLMode:  "prefer",
	}
}

// LoadFromFile loads configuration from a file
func (c *Config) LoadFromFile(filename string) error {
	file, err := os.Open(filename) // #nosec G304 - Config file path is provided by administrator
	if err != nil {
		return fmt.Errorf("failed to open config file: %w", err)
	}
	defer func() {
		if cerr := file.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("failed to close config file: %w", cerr)
		}
	}()

	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse key = value
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid config line %d: %s", lineNum, line)
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// Remove quotes if present
		if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
			value = value[1 : len(value)-1]
		}

		if err := c.setConfigValue(key, value); err != nil {
			return fmt.Errorf("error on line %d: %w", lineNum, err)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading config file: %w", err)
	}

	return nil
}

// setConfigValue sets a configuration value by key
func (c *Config) setConfigValue(key, value string) error {
	switch key {
	// MCP Server options
	case "tls":
		tls, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid tls value: %w", err)
		}
		c.TLS = tls
	case "tls_cert":
		c.TLSCert = value
	case "tls_key":
		c.TLSKey = value
	case "tls_chain":
		c.TLSChain = value
	case "port":
		port, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid port number: %w", err)
		}
		c.Port = port
	// Datastore options
	case "pg_host":
		c.PgHost = value
	case "pg_hostaddr":
		c.PgHostAddr = value
	case "pg_database":
		c.PgDatabase = value
	case "pg_username":
		c.PgUsername = value
	case "pg_password_file":
		password, err := readPasswordFile(value)
		if err != nil {
			return fmt.Errorf("failed to read password file: %w", err)
		}
		c.PgPassword = password
	case "pg_port":
		port, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid port number: %w", err)
		}
		c.PgPort = port
	case "pg_sslmode":
		c.PgSSLMode = value
	case "pg_sslcert":
		c.PgSSLCert = value
	case "pg_sslkey":
		c.PgSSLKey = value
	case "pg_sslrootcert":
		c.PgSSLRootCert = value
	case "server_secret":
		c.ServerSecret = value
	default:
		// Ignore unknown keys (allows sharing config with collector)
		// No error - just skip keys we don't recognize
	}
	return nil
}

// readPasswordFile reads a password from a file
func readPasswordFile(filename string) (string, error) {
	content, err := os.ReadFile(filename) // #nosec G304 - Password file path is provided by administrator
	if err != nil {
		return "", err
	}
	// Trim whitespace and newlines
	return strings.TrimSpace(string(content)), nil
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	// Validate MCP server options
	if c.TLS {
		if c.TLSCert == "" {
			return fmt.Errorf("tls_cert is required when TLS is enabled")
		}
		if c.TLSKey == "" {
			return fmt.Errorf("tls_key is required when TLS is enabled")
		}
	}
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}

	// Validate datastore options
	if c.PgHost == "" {
		return fmt.Errorf("pg_host is required")
	}
	if c.PgDatabase == "" {
		return fmt.Errorf("pg_database is required")
	}
	if c.PgUsername == "" {
		return fmt.Errorf("pg_username is required")
	}
	if c.PgPort <= 0 || c.PgPort > 65535 {
		return fmt.Errorf("pg_port must be between 1 and 65535")
	}

	return nil
}

// GetTLS returns whether TLS is enabled
func (c *Config) GetTLS() bool { return c.TLS }

// GetTLSCert returns the TLS certificate path
func (c *Config) GetTLSCert() string { return c.TLSCert }

// GetTLSKey returns the TLS key path
func (c *Config) GetTLSKey() string { return c.TLSKey }

// GetTLSChain returns the TLS chain path
func (c *Config) GetTLSChain() string { return c.TLSChain }

// GetPort returns the server port
func (c *Config) GetPort() int { return c.Port }

// GetPgHost returns the PostgreSQL host
func (c *Config) GetPgHost() string { return c.PgHost }

// GetPgHostAddr returns the PostgreSQL host address
func (c *Config) GetPgHostAddr() string { return c.PgHostAddr }

// GetPgDatabase returns the PostgreSQL database name
func (c *Config) GetPgDatabase() string { return c.PgDatabase }

// GetPgUsername returns the PostgreSQL username
func (c *Config) GetPgUsername() string { return c.PgUsername }

// GetPgPassword returns the PostgreSQL password
func (c *Config) GetPgPassword() string { return c.PgPassword }

// GetPgPort returns the PostgreSQL port
func (c *Config) GetPgPort() int { return c.PgPort }

// GetPgSSLMode returns the PostgreSQL SSL mode
func (c *Config) GetPgSSLMode() string { return c.PgSSLMode }

// GetPgSSLCert returns the PostgreSQL SSL certificate path
func (c *Config) GetPgSSLCert() string { return c.PgSSLCert }

// GetPgSSLKey returns the PostgreSQL SSL key path
func (c *Config) GetPgSSLKey() string { return c.PgSSLKey }

// GetPgSSLRootCert returns the PostgreSQL SSL root certificate path
func (c *Config) GetPgSSLRootCert() string { return c.PgSSLRootCert }

// GetServerSecret returns the server secret
func (c *Config) GetServerSecret() string { return c.ServerSecret }
