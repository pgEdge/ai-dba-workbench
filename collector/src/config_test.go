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
	"os"
	"path/filepath"
	"testing"
)

func TestNewConfig(t *testing.T) {
	config := NewConfig()

	if config.Datastore.Host != "localhost" {
		t.Errorf("Expected default Datastore.Host to be 'localhost', got '%s'", config.Datastore.Host)
	}

	if config.Datastore.Port != 5432 {
		t.Errorf("Expected default Datastore.Port to be 5432, got %d", config.Datastore.Port)
	}

	if config.Datastore.Database != "ai_workbench" {
		t.Errorf("Expected default Datastore.Database to be 'ai_workbench', got '%s'", config.Datastore.Database)
	}

	if config.Pool.DatastoreMaxWaitSeconds != 60 {
		t.Errorf("Expected default Pool.DatastoreMaxWaitSeconds to be 60, got %d", config.Pool.DatastoreMaxWaitSeconds)
	}

	if config.Pool.MonitoredMaxWaitSeconds != 60 {
		t.Errorf("Expected default Pool.MonitoredMaxWaitSeconds to be 60, got %d", config.Pool.MonitoredMaxWaitSeconds)
	}
}

func TestConfigLoadFromFile(t *testing.T) {
	// Create a temporary config file in YAML format
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.yaml")

	configContent := `# Test configuration
datastore:
  host: testhost
  port: 5433
  database: testdb
  username: testuser
server_secret: "test-secret"
`

	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("Failed to write test config file: %v", err)
	}

	config := NewConfig()
	if err := config.LoadFromFile(configPath); err != nil {
		t.Fatalf("Failed to load config from file: %v", err)
	}

	if config.Datastore.Host != "testhost" {
		t.Errorf("Expected Datastore.Host to be 'testhost', got '%s'", config.Datastore.Host)
	}

	if config.Datastore.Port != 5433 {
		t.Errorf("Expected Datastore.Port to be 5433, got %d", config.Datastore.Port)
	}

	if config.Datastore.Database != "testdb" {
		t.Errorf("Expected Datastore.Database to be 'testdb', got '%s'", config.Datastore.Database)
	}

	if config.Datastore.Username != "testuser" {
		t.Errorf("Expected Datastore.Username to be 'testuser', got '%s'", config.Datastore.Username)
	}

	if config.ServerSecret != "test-secret" {
		t.Errorf("Expected ServerSecret to be 'test-secret', got '%s'", config.ServerSecret)
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: &Config{
				Datastore: DatastoreConfig{
					Host:     "localhost",
					Database: "testdb",
					Username: "testuser",
					Port:     5432,
				},
				Pool: PoolConfig{
					DatastoreMaxConnections: 10,
					DatastoreMaxIdleSeconds: 60,
					DatastoreMaxWaitSeconds: 60,
					MonitoredMaxConnections: 5,
					MonitoredMaxWaitSeconds: 60,
				},
			},
			wantErr: false,
		},
		{
			name: "missing host",
			config: &Config{
				Datastore: DatastoreConfig{
					Host:     "",
					Database: "testdb",
					Username: "testuser",
					Port:     5432,
				},
			},
			wantErr: true,
		},
		{
			name: "missing database",
			config: &Config{
				Datastore: DatastoreConfig{
					Host:     "localhost",
					Database: "",
					Username: "testuser",
					Port:     5432,
				},
			},
			wantErr: true,
		},
		{
			name: "invalid port",
			config: &Config{
				Datastore: DatastoreConfig{
					Host:     "localhost",
					Database: "testdb",
					Username: "testuser",
					Port:     -1,
				},
				Pool: PoolConfig{
					DatastoreMaxConnections: 10,
				},
			},
			wantErr: true,
		},
		{
			name: "invalid pool_max_connections",
			config: &Config{
				Datastore: DatastoreConfig{
					Host:     "localhost",
					Database: "testdb",
					Username: "testuser",
					Port:     5432,
				},
				Pool: PoolConfig{
					DatastoreMaxConnections: 0,
				},
			},
			wantErr: true,
		},
		{
			name: "invalid pool_max_idle_seconds",
			config: &Config{
				Datastore: DatastoreConfig{
					Host:     "localhost",
					Database: "testdb",
					Username: "testuser",
					Port:     5432,
				},
				Pool: PoolConfig{
					DatastoreMaxConnections: 10,
					DatastoreMaxIdleSeconds: -1,
				},
			},
			wantErr: true,
		},
		{
			name: "invalid datastore_pool_max_wait_seconds",
			config: &Config{
				Datastore: DatastoreConfig{
					Host:     "localhost",
					Database: "testdb",
					Username: "testuser",
					Port:     5432,
				},
				Pool: PoolConfig{
					DatastoreMaxConnections: 10,
					DatastoreMaxIdleSeconds: 60,
					DatastoreMaxWaitSeconds: 0,
					MonitoredMaxConnections: 5,
					MonitoredMaxWaitSeconds: 60,
				},
			},
			wantErr: true,
		},
		{
			name: "invalid monitored_pool_max_wait_seconds",
			config: &Config{
				Datastore: DatastoreConfig{
					Host:     "localhost",
					Database: "testdb",
					Username: "testuser",
					Port:     5432,
				},
				Pool: PoolConfig{
					DatastoreMaxConnections: 10,
					DatastoreMaxIdleSeconds: 60,
					DatastoreMaxWaitSeconds: 60,
					MonitoredMaxConnections: 5,
					MonitoredMaxWaitSeconds: -1,
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestReadPasswordFile(t *testing.T) {
	tmpDir := t.TempDir()
	passwordFile := filepath.Join(tmpDir, "password.txt")

	testPassword := "test-password-123"
	if err := os.WriteFile(passwordFile, []byte(testPassword+"\n"), 0600); err != nil {
		t.Fatalf("Failed to write test password file: %v", err)
	}

	password, err := readPasswordFile(passwordFile)
	if err != nil {
		t.Fatalf("readPasswordFile() error = %v", err)
	}

	if password != testPassword {
		t.Errorf("Expected password to be '%s', got '%s'", testPassword, password)
	}
}

func TestGetDefaultConfigPath(t *testing.T) {
	// Test that it returns a path ending in ai-dba-collector.yaml
	binaryPath := "/usr/local/bin/ai-dba-collector"
	configPath := GetDefaultConfigPath(binaryPath)

	// Since /etc/pgedge/ai-dba-collector.yaml likely doesn't exist,
	// it should fall back to the binary directory
	expected := "/usr/local/bin/ai-dba-collector.yaml"
	if configPath != expected {
		t.Errorf("Expected config path '%s', got '%s'", expected, configPath)
	}
}

func TestConfigGetters(t *testing.T) {
	config := &Config{
		Datastore: DatastoreConfig{
			Host:        "testhost",
			HostAddr:    "192.168.1.1",
			Database:    "testdb",
			Username:    "testuser",
			Password:    "testpass",
			Port:        5433,
			SSLMode:     "require",
			SSLCert:     "/path/to/cert",
			SSLKey:      "/path/to/key",
			SSLRootCert: "/path/to/root",
		},
		Pool: PoolConfig{
			DatastoreMaxConnections: 20,
			DatastoreMaxIdleSeconds: 120,
			DatastoreMaxWaitSeconds: 30,
			MonitoredMaxWaitSeconds: 45,
		},
	}

	if config.GetPgHost() != "testhost" {
		t.Errorf("GetPgHost() = %s, want testhost", config.GetPgHost())
	}
	if config.GetPgHostAddr() != "192.168.1.1" {
		t.Errorf("GetPgHostAddr() = %s, want 192.168.1.1", config.GetPgHostAddr())
	}
	if config.GetPgDatabase() != "testdb" {
		t.Errorf("GetPgDatabase() = %s, want testdb", config.GetPgDatabase())
	}
	if config.GetPgUsername() != "testuser" {
		t.Errorf("GetPgUsername() = %s, want testuser", config.GetPgUsername())
	}
	if config.GetPgPassword() != "testpass" {
		t.Errorf("GetPgPassword() = %s, want testpass", config.GetPgPassword())
	}
	if config.GetPgPort() != 5433 {
		t.Errorf("GetPgPort() = %d, want 5433", config.GetPgPort())
	}
	if config.GetPgSSLMode() != "require" {
		t.Errorf("GetPgSSLMode() = %s, want require", config.GetPgSSLMode())
	}
	if config.GetPgSSLCert() != "/path/to/cert" {
		t.Errorf("GetPgSSLCert() = %s, want /path/to/cert", config.GetPgSSLCert())
	}
	if config.GetPgSSLKey() != "/path/to/key" {
		t.Errorf("GetPgSSLKey() = %s, want /path/to/key", config.GetPgSSLKey())
	}
	if config.GetPgSSLRootCert() != "/path/to/root" {
		t.Errorf("GetPgSSLRootCert() = %s, want /path/to/root", config.GetPgSSLRootCert())
	}
	if config.GetDatastorePoolMaxConnections() != 20 {
		t.Errorf("GetDatastorePoolMaxConnections() = %d, want 20", config.GetDatastorePoolMaxConnections())
	}
	if config.GetDatastorePoolMaxIdleSeconds() != 120 {
		t.Errorf("GetDatastorePoolMaxIdleSeconds() = %d, want 120", config.GetDatastorePoolMaxIdleSeconds())
	}
	if config.GetDatastorePoolMaxWaitSeconds() != 30 {
		t.Errorf("GetDatastorePoolMaxWaitSeconds() = %d, want 30", config.GetDatastorePoolMaxWaitSeconds())
	}
	if config.GetMonitoredPoolMaxWaitSeconds() != 45 {
		t.Errorf("GetMonitoredPoolMaxWaitSeconds() = %d, want 45", config.GetMonitoredPoolMaxWaitSeconds())
	}
}
