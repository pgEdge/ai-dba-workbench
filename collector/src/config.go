/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package main implements the pgEdge AI DBA Workbench Collector.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds all configuration options for the collector
type Config struct {
	// Datastore connection settings
	Datastore DatastoreConfig `yaml:"datastore"`

	// Connection pool settings
	Pool PoolConfig `yaml:"pool"`

	// Server secret for encryption
	ServerSecret string `yaml:"server_secret"`
}

// DatastoreConfig holds PostgreSQL connection settings for the datastore
type DatastoreConfig struct {
	Host         string `yaml:"host"`          // PostgreSQL server hostname or IP address
	HostAddr     string `yaml:"hostaddr"`      // PostgreSQL server IP address (bypasses DNS)
	Database     string `yaml:"database"`      // Database name
	Username     string `yaml:"username"`      // Username for connection
	Password     string `yaml:"password"`      // Password (discouraged - use password_file or env var)
	PasswordFile string `yaml:"password_file"` // Path to file containing password
	Port         int    `yaml:"port"`          // PostgreSQL server port
	SSLMode      string `yaml:"sslmode"`       // SSL mode (disable, allow, prefer, require, verify-ca, verify-full)
	SSLCert      string `yaml:"sslcert"`       // Path to client SSL certificate
	SSLKey       string `yaml:"sslkey"`        // Path to client SSL private key
	SSLRootCert  string `yaml:"sslrootcert"`   // Path to root CA certificate
}

// PoolConfig holds connection pool settings
type PoolConfig struct {
	// Datastore pool settings
	DatastoreMaxConnections int `yaml:"datastore_max_connections"` // Max connections in the datastore pool
	DatastoreMaxIdleSeconds int `yaml:"datastore_max_idle_seconds"` // Max idle time before closing connections
	DatastoreMaxWaitSeconds int `yaml:"datastore_max_wait_seconds"` // Max wait time to acquire a connection

	// Monitored server pool settings (per-server)
	MonitoredMaxConnections int `yaml:"monitored_max_connections"` // Max connections PER monitored server
	MonitoredMaxIdleSeconds int `yaml:"monitored_max_idle_seconds"` // Max idle time before closing connections
	MonitoredMaxWaitSeconds int `yaml:"monitored_max_wait_seconds"` // Max wait time to acquire a connection
}

// NewConfig creates a new Config with default values
func NewConfig() *Config {
	return &Config{
		Datastore: DatastoreConfig{
			Host:     "localhost",
			Database: "ai_workbench",
			Username: "postgres",
			Port:     5432,
			SSLMode:  "prefer",
		},
		Pool: PoolConfig{
			DatastoreMaxConnections: 25,
			DatastoreMaxIdleSeconds: 300,
			DatastoreMaxWaitSeconds: 60,
			MonitoredMaxConnections: 5,
			MonitoredMaxIdleSeconds: 300,
			MonitoredMaxWaitSeconds: 60,
		},
	}
}

// LoadFromFile loads configuration from a YAML file
func (c *Config) LoadFromFile(filename string) error {
	data, err := os.ReadFile(filename) // #nosec G304 - Config file path is provided by administrator
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, c); err != nil {
		return fmt.Errorf("failed to parse YAML config: %w", err)
	}

	return nil
}

// ApplyEnvironment applies environment variables to override config values
func (c *Config) ApplyEnvironment() {
	// Datastore settings
	setStringFromEnv(&c.Datastore.Host, "PGEDGE_DB_HOST", "PGHOST")
	setStringFromEnv(&c.Datastore.HostAddr, "PGEDGE_DB_HOSTADDR")
	setStringFromEnv(&c.Datastore.Database, "PGEDGE_DB_NAME", "PGDATABASE")
	setStringFromEnv(&c.Datastore.Username, "PGEDGE_DB_USER", "PGUSER")
	setStringFromEnv(&c.Datastore.Password, "PGEDGE_DB_PASSWORD", "PGPASSWORD")
	setStringFromEnv(&c.Datastore.PasswordFile, "PGEDGE_DB_PASSWORD_FILE")
	setIntFromEnv(&c.Datastore.Port, "PGEDGE_DB_PORT", "PGPORT")
	setStringFromEnv(&c.Datastore.SSLMode, "PGEDGE_DB_SSLMODE", "PGSSLMODE")
	setStringFromEnv(&c.Datastore.SSLCert, "PGEDGE_DB_SSLCERT", "PGSSLCERT")
	setStringFromEnv(&c.Datastore.SSLKey, "PGEDGE_DB_SSLKEY", "PGSSLKEY")
	setStringFromEnv(&c.Datastore.SSLRootCert, "PGEDGE_DB_SSLROOTCERT", "PGSSLROOTCERT")

	// Pool settings
	setIntFromEnv(&c.Pool.DatastoreMaxConnections, "PGEDGE_POOL_DATASTORE_MAX_CONNECTIONS")
	setIntFromEnv(&c.Pool.DatastoreMaxIdleSeconds, "PGEDGE_POOL_DATASTORE_MAX_IDLE_SECONDS")
	setIntFromEnv(&c.Pool.DatastoreMaxWaitSeconds, "PGEDGE_POOL_DATASTORE_MAX_WAIT_SECONDS")
	setIntFromEnv(&c.Pool.MonitoredMaxConnections, "PGEDGE_POOL_MONITORED_MAX_CONNECTIONS")
	setIntFromEnv(&c.Pool.MonitoredMaxIdleSeconds, "PGEDGE_POOL_MONITORED_MAX_IDLE_SECONDS")
	setIntFromEnv(&c.Pool.MonitoredMaxWaitSeconds, "PGEDGE_POOL_MONITORED_MAX_WAIT_SECONDS")

	// Server secret
	setStringFromEnv(&c.ServerSecret, "PGEDGE_SERVER_SECRET")
}

// ApplyFlags applies command line flags to override config values
func (c *Config) ApplyFlags() {
	if *pgHost != "" {
		c.Datastore.Host = *pgHost
	}
	if *pgHostAddr != "" {
		c.Datastore.HostAddr = *pgHostAddr
	}
	if *pgDatabase != "" {
		c.Datastore.Database = *pgDatabase
	}
	if *pgUsername != "" {
		c.Datastore.Username = *pgUsername
	}
	if *pgPasswordFile != "" {
		c.Datastore.PasswordFile = *pgPasswordFile
	}
	if *pgPort != 5432 {
		c.Datastore.Port = *pgPort
	}
	if *pgSSLMode != "prefer" {
		c.Datastore.SSLMode = *pgSSLMode
	}
	if *pgSSLCert != "" {
		c.Datastore.SSLCert = *pgSSLCert
	}
	if *pgSSLKey != "" {
		c.Datastore.SSLKey = *pgSSLKey
	}
	if *pgSSLRootCert != "" {
		c.Datastore.SSLRootCert = *pgSSLRootCert
	}
}

// LoadPassword loads the password from password file if specified
func (c *Config) LoadPassword() error {
	// Password priority: direct password > password file > .pgpass (handled by driver)
	if c.Datastore.Password != "" {
		return nil // Already have a password
	}

	if c.Datastore.PasswordFile != "" {
		password, err := readPasswordFile(c.Datastore.PasswordFile)
		if err != nil {
			return fmt.Errorf("failed to read password file: %w", err)
		}
		c.Datastore.Password = password
	}

	return nil
}

// readPasswordFile reads a password from a file
func readPasswordFile(filename string) (string, error) {
	// Expand tilde to home directory
	if filename != "" && filename[0] == '~' {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		filename = filepath.Join(homeDir, filename[1:])
	}

	content, err := os.ReadFile(filename) // #nosec G304 - Password file path is provided by administrator
	if err != nil {
		return "", err
	}
	// Trim whitespace and newlines
	return strings.TrimSpace(string(content)), nil
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.Datastore.Host == "" {
		return fmt.Errorf("datastore.host is required")
	}
	if c.Datastore.Database == "" {
		return fmt.Errorf("datastore.database is required")
	}
	if c.Datastore.Username == "" {
		return fmt.Errorf("datastore.username is required")
	}
	if c.Datastore.Port <= 0 || c.Datastore.Port > 65535 {
		return fmt.Errorf("datastore.port must be between 1 and 65535")
	}
	if c.Pool.DatastoreMaxConnections <= 0 {
		return fmt.Errorf("pool.datastore_max_connections must be greater than 0")
	}
	if c.Pool.DatastoreMaxIdleSeconds < 0 {
		return fmt.Errorf("pool.datastore_max_idle_seconds must be non-negative")
	}
	if c.Pool.MonitoredMaxConnections <= 0 {
		return fmt.Errorf("pool.monitored_max_connections must be greater than 0")
	}
	if c.Pool.MonitoredMaxIdleSeconds < 0 {
		return fmt.Errorf("pool.monitored_max_idle_seconds must be non-negative")
	}
	if c.Pool.DatastoreMaxWaitSeconds <= 0 {
		return fmt.Errorf("pool.datastore_max_wait_seconds must be greater than 0")
	}
	if c.Pool.MonitoredMaxWaitSeconds <= 0 {
		return fmt.Errorf("pool.monitored_max_wait_seconds must be greater than 0")
	}
	return nil
}

// Getter methods to implement database.Config and scheduler.Config interfaces
func (c *Config) GetPgHost() string                   { return c.Datastore.Host }
func (c *Config) GetPgHostAddr() string               { return c.Datastore.HostAddr }
func (c *Config) GetPgDatabase() string               { return c.Datastore.Database }
func (c *Config) GetPgUsername() string               { return c.Datastore.Username }
func (c *Config) GetPgPassword() string               { return c.Datastore.Password }
func (c *Config) GetPgPort() int                      { return c.Datastore.Port }
func (c *Config) GetPgSSLMode() string                { return c.Datastore.SSLMode }
func (c *Config) GetPgSSLCert() string                { return c.Datastore.SSLCert }
func (c *Config) GetPgSSLKey() string                 { return c.Datastore.SSLKey }
func (c *Config) GetPgSSLRootCert() string            { return c.Datastore.SSLRootCert }
func (c *Config) GetDatastorePoolMaxConnections() int { return c.Pool.DatastoreMaxConnections }
func (c *Config) GetDatastorePoolMaxIdleSeconds() int { return c.Pool.DatastoreMaxIdleSeconds }
func (c *Config) GetDatastorePoolMaxWaitSeconds() int { return c.Pool.DatastoreMaxWaitSeconds }
func (c *Config) GetMonitoredPoolMaxWaitSeconds() int { return c.Pool.MonitoredMaxWaitSeconds }

// GetDefaultConfigPath returns the default config file path
// Searches /etc/pgedge/ first, then binary directory
func GetDefaultConfigPath(binaryPath string) string {
	systemPath := "/etc/pgedge/ai-dba-collector.yaml"
	if _, err := os.Stat(systemPath); err == nil {
		return systemPath
	}

	dir := filepath.Dir(binaryPath)
	return filepath.Join(dir, "ai-dba-collector.yaml")
}

// Helper functions for environment variable loading

// setStringFromEnv sets a string from environment variables (checks multiple keys in order)
func setStringFromEnv(dest *string, keys ...string) {
	for _, key := range keys {
		if val := os.Getenv(key); val != "" {
			*dest = val
			return
		}
	}
}

// setIntFromEnv sets an integer from environment variables (checks multiple keys in order)
func setIntFromEnv(dest *int, keys ...string) {
	for _, key := range keys {
		if val := os.Getenv(key); val != "" {
			var intVal int
			_, err := fmt.Sscanf(val, "%d", &intVal)
			if err == nil {
				*dest = intVal
				return
			}
		}
	}
}
