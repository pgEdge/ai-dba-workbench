/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package main implements the pgEdge AI Workbench Collector.
package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds all configuration options for the collector
type Config struct {
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

	// Connection pool settings
	DatastorePoolMaxConnections int // Max connections in the datastore connection pool
	DatastorePoolMaxIdleSeconds int // Max idle time (seconds) before closing idle datastore connections
	DatastorePoolMaxWaitSeconds int // Max wait time (seconds) to acquire a datastore connection before timeout
	MonitoredPoolMaxConnections int // Max concurrent connections PER monitored database server
	MonitoredPoolMaxIdleSeconds int // Max idle time (seconds) before closing idle monitored connections
	MonitoredPoolMaxWaitSeconds int // Max wait time (seconds) to acquire a monitored connection before timeout

	// Server secret for encryption
	ServerSecret string
}

// NewConfig creates a new Config with default values
func NewConfig() *Config {
	return &Config{
		PgHost:                      "localhost",
		PgDatabase:                  "ai_workbench",
		PgUsername:                  "postgres",
		PgPort:                      5432,
		PgSSLMode:                   "prefer",
		DatastorePoolMaxConnections: 25,
		DatastorePoolMaxIdleSeconds: 300,
		DatastorePoolMaxWaitSeconds: 60,
		MonitoredPoolMaxConnections: 5,
		MonitoredPoolMaxIdleSeconds: 300,
		MonitoredPoolMaxWaitSeconds: 60,
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
	case "datastore_pool_max_connections":
		maxConn, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid datastore_pool_max_connections: %w", err)
		}
		c.DatastorePoolMaxConnections = maxConn
	case "datastore_pool_max_idle_seconds":
		maxIdle, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid datastore_pool_max_idle_seconds: %w", err)
		}
		c.DatastorePoolMaxIdleSeconds = maxIdle
	case "monitored_pool_max_connections":
		maxConn, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid monitored_pool_max_connections: %w", err)
		}
		c.MonitoredPoolMaxConnections = maxConn
	case "monitored_pool_max_idle_seconds":
		maxIdle, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid monitored_pool_max_idle_seconds: %w", err)
		}
		c.MonitoredPoolMaxIdleSeconds = maxIdle
	case "datastore_pool_max_wait_seconds":
		maxWait, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid datastore_pool_max_wait_seconds: %w", err)
		}
		c.DatastorePoolMaxWaitSeconds = maxWait
	case "monitored_pool_max_wait_seconds":
		maxWait, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid monitored_pool_max_wait_seconds: %w", err)
		}
		c.MonitoredPoolMaxWaitSeconds = maxWait
	case "server_secret":
		c.ServerSecret = value
	default:
		return fmt.Errorf("unknown configuration key: %s", key)
	}
	return nil
}

// ApplyFlags applies command line flags to override config file values
func (c *Config) ApplyFlags() {
	if *pgHost != "" {
		c.PgHost = *pgHost
	}
	if *pgHostAddr != "" {
		c.PgHostAddr = *pgHostAddr
	}
	if *pgDatabase != "" {
		c.PgDatabase = *pgDatabase
	}
	if *pgUsername != "" {
		c.PgUsername = *pgUsername
	}
	if *pgPasswordFile != "" {
		password, err := readPasswordFile(*pgPasswordFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to read password file: %v\n", err)
		} else {
			c.PgPassword = password
		}
	}
	if *pgPort != 5432 {
		c.PgPort = *pgPort
	}
	if *pgSSLMode != "prefer" {
		c.PgSSLMode = *pgSSLMode
	}
	if *pgSSLCert != "" {
		c.PgSSLCert = *pgSSLCert
	}
	if *pgSSLKey != "" {
		c.PgSSLKey = *pgSSLKey
	}
	if *pgSSLRootCert != "" {
		c.PgSSLRootCert = *pgSSLRootCert
	}
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
	if c.DatastorePoolMaxConnections <= 0 {
		return fmt.Errorf("datastore_pool_max_connections must be greater than 0")
	}
	if c.DatastorePoolMaxIdleSeconds < 0 {
		return fmt.Errorf("datastore_pool_max_idle_seconds must be non-negative")
	}
	if c.MonitoredPoolMaxConnections <= 0 {
		return fmt.Errorf("monitored_pool_max_connections must be greater than 0")
	}
	if c.MonitoredPoolMaxIdleSeconds < 0 {
		return fmt.Errorf("monitored_pool_max_idle_seconds must be non-negative")
	}
	if c.DatastorePoolMaxWaitSeconds <= 0 {
		return fmt.Errorf("datastore_pool_max_wait_seconds must be greater than 0")
	}
	if c.MonitoredPoolMaxWaitSeconds <= 0 {
		return fmt.Errorf("monitored_pool_max_wait_seconds must be greater than 0")
	}
	return nil
}

// Getter methods to implement database.Config and scheduler.Config interfaces
func (c *Config) GetPgHost() string                   { return c.PgHost }
func (c *Config) GetPgHostAddr() string               { return c.PgHostAddr }
func (c *Config) GetPgDatabase() string               { return c.PgDatabase }
func (c *Config) GetPgUsername() string               { return c.PgUsername }
func (c *Config) GetPgPassword() string               { return c.PgPassword }
func (c *Config) GetPgPort() int                      { return c.PgPort }
func (c *Config) GetPgSSLMode() string                { return c.PgSSLMode }
func (c *Config) GetPgSSLCert() string                { return c.PgSSLCert }
func (c *Config) GetPgSSLKey() string                 { return c.PgSSLKey }
func (c *Config) GetPgSSLRootCert() string            { return c.PgSSLRootCert }
func (c *Config) GetDatastorePoolMaxConnections() int { return c.DatastorePoolMaxConnections }
func (c *Config) GetDatastorePoolMaxIdleSeconds() int { return c.DatastorePoolMaxIdleSeconds }
func (c *Config) GetDatastorePoolMaxWaitSeconds() int { return c.DatastorePoolMaxWaitSeconds }
func (c *Config) GetMonitoredPoolMaxWaitSeconds() int { return c.MonitoredPoolMaxWaitSeconds }
