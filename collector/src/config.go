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

	"github.com/pgedge/ai-workbench/pkg/datastoreconfig"
	"github.com/pgedge/ai-workbench/pkg/fileutil"
	"gopkg.in/yaml.v3"
)

// Config holds all configuration options for the collector
type Config struct {
	// Datastore connection settings
	Datastore datastoreconfig.DatastoreConfig `yaml:"datastore"`

	// Connection pool settings
	Pool PoolConfig `yaml:"pool"`

	// Path to file containing server secret for encryption
	// Default search paths: /etc/pgedge/ai-dba-collector.secret, ./ai-dba-collector.secret
	SecretFile string `yaml:"secret_file"`

	// Loaded server secret (not from YAML, loaded from SecretFile)
	serverSecret string
}

// PoolConfig holds connection pool settings
type PoolConfig struct {
	// Datastore pool settings
	DatastoreMaxConnections int `yaml:"datastore_max_connections"`  // Max connections in the datastore pool
	DatastoreMaxIdleSeconds int `yaml:"datastore_max_idle_seconds"` // Max idle time before closing connections
	DatastoreMaxWaitSeconds int `yaml:"datastore_max_wait_seconds"` // Max wait time to acquire a connection

	// Monitored server pool settings (per-server)
	MaxConnectionsPerServer int `yaml:"max_connections_per_server"` // Max connections per monitored server
	MonitoredMaxIdleSeconds int `yaml:"monitored_max_idle_seconds"` // Max idle time before closing connections
	MonitoredMaxWaitSeconds int `yaml:"monitored_max_wait_seconds"` // Max wait time to acquire a connection
}

// NewConfig creates a new Config with default values
func NewConfig() *Config {
	return &Config{
		Datastore: datastoreconfig.DatastoreConfig{
			Host:     "localhost",
			Database: "ai_workbench",
			Username: "postgres",
			Port:     5432,
			// The default SSLMode of "prefer" follows PostgreSQL's standard
			// libpq connection behavior for maximum compatibility with diverse
			// deployment configurations, including development environments
			// and Docker setups where SSL may not be configured.
			SSLMode: "prefer",
		},
		Pool: PoolConfig{
			DatastoreMaxConnections: 25,
			DatastoreMaxIdleSeconds: 300,
			DatastoreMaxWaitSeconds: 60,
			MaxConnectionsPerServer: 3,
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
		password, err := fileutil.ReadTrimmedFileWithTilde(c.Datastore.PasswordFile)
		if err != nil {
			return fmt.Errorf("failed to read password file: %w", err)
		}
		c.Datastore.Password = password
	}

	return nil
}

// LoadSecret loads the server secret from the secret file
// Search order: explicit SecretFile config > /etc/pgedge/ > binary directory
func (c *Config) LoadSecret(binaryPath string) error {
	// If secret file is explicitly specified, use it
	if c.SecretFile != "" {
		secret, err := fileutil.ReadTrimmedFileWithTilde(c.SecretFile)
		if err != nil {
			return fmt.Errorf("failed to read secret file: %w", err)
		}
		c.serverSecret = secret
		return nil
	}

	// Search default paths
	searchPaths := []string{
		"/etc/pgedge/ai-dba-collector.secret",
	}

	// Add binary directory path
	if binaryPath != "" {
		dir := filepath.Dir(binaryPath)
		searchPaths = append(searchPaths, filepath.Join(dir, "ai-dba-collector.secret"))
	}

	// Also check current directory
	searchPaths = append(searchPaths, "./ai-dba-collector.secret")

	for _, path := range searchPaths {
		if _, err := os.Stat(path); err == nil {
			secret, err := fileutil.ReadTrimmedFileWithTilde(path)
			if err != nil {
				return fmt.Errorf("failed to read secret file %s: %w", path, err)
			}
			c.serverSecret = secret
			return nil
		}
	}

	return fmt.Errorf("server secret file not found. Create a secret file at one of: %v", searchPaths)
}

// GetServerSecret returns the loaded server secret
func (c *Config) GetServerSecret() string {
	return c.serverSecret
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
	if c.Pool.MonitoredMaxIdleSeconds < 0 {
		return fmt.Errorf("pool.monitored_max_idle_seconds must be non-negative")
	}
	if c.Pool.MaxConnectionsPerServer <= 0 {
		return fmt.Errorf("pool.max_connections_per_server must be greater than 0")
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
	return fileutil.GetDefaultConfigPath(binaryPath, "ai-dba-collector.yaml")
}
