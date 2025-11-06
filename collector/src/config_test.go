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
	"os"
	"path/filepath"
	"testing"
)

func TestNewConfig(t *testing.T) {
	config := NewConfig()

	if config.PgHost != "localhost" {
		t.Errorf("Expected default PgHost to be 'localhost', got '%s'", config.PgHost)
	}

	if config.PgPort != 5432 {
		t.Errorf("Expected default PgPort to be 5432, got %d", config.PgPort)
	}

	if config.PgDatabase != "ai_workbench" {
		t.Errorf("Expected default PgDatabase to be 'ai_workbench', got '%s'", config.PgDatabase)
	}

	if config.DatastorePoolMaxWaitSeconds != 60 {
		t.Errorf("Expected default DatastorePoolMaxWaitSeconds to be 60, got %d", config.DatastorePoolMaxWaitSeconds)
	}

	if config.MonitoredPoolMaxWaitSeconds != 60 {
		t.Errorf("Expected default MonitoredPoolMaxWaitSeconds to be 60, got %d", config.MonitoredPoolMaxWaitSeconds)
	}
}

func TestConfigLoadFromFile(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.conf")

	configContent := `# Test configuration
pg_host = testhost
pg_port = 5433
pg_database = testdb
pg_username = testuser
server_secret = "test-secret"
`

	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("Failed to write test config file: %v", err)
	}

	config := NewConfig()
	if err := config.LoadFromFile(configPath); err != nil {
		t.Fatalf("Failed to load config from file: %v", err)
	}

	if config.PgHost != "testhost" {
		t.Errorf("Expected PgHost to be 'testhost', got '%s'", config.PgHost)
	}

	if config.PgPort != 5433 {
		t.Errorf("Expected PgPort to be 5433, got %d", config.PgPort)
	}

	if config.PgDatabase != "testdb" {
		t.Errorf("Expected PgDatabase to be 'testdb', got '%s'", config.PgDatabase)
	}

	if config.PgUsername != "testuser" {
		t.Errorf("Expected PgUsername to be 'testuser', got '%s'", config.PgUsername)
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
				PgHost:                      "localhost",
				PgDatabase:                  "testdb",
				PgUsername:                  "testuser",
				PgPort:                      5432,
				DatastorePoolMaxConnections: 10,
				DatastorePoolMaxIdleSeconds: 60,
				DatastorePoolMaxWaitSeconds: 60,
				MonitoredPoolMaxConnections: 5,
				MonitoredPoolMaxWaitSeconds: 60,
			},
			wantErr: false,
		},
		{
			name: "missing host",
			config: &Config{
				PgHost:     "",
				PgDatabase: "testdb",
				PgUsername: "testuser",
				PgPort:     5432,
			},
			wantErr: true,
		},
		{
			name: "missing database",
			config: &Config{
				PgHost:     "localhost",
				PgDatabase: "",
				PgUsername: "testuser",
				PgPort:     5432,
			},
			wantErr: true,
		},
		{
			name: "invalid port",
			config: &Config{
				PgHost:                      "localhost",
				PgDatabase:                  "testdb",
				PgUsername:                  "testuser",
				PgPort:                      -1,
				DatastorePoolMaxConnections: 10,
			},
			wantErr: true,
		},
		{
			name: "invalid pool_max_connections",
			config: &Config{
				PgHost:                      "localhost",
				PgDatabase:                  "testdb",
				PgUsername:                  "testuser",
				PgPort:                      5432,
				DatastorePoolMaxConnections: 0,
			},
			wantErr: true,
		},
		{
			name: "invalid pool_max_idle_seconds",
			config: &Config{
				PgHost:                      "localhost",
				PgDatabase:                  "testdb",
				PgUsername:                  "testuser",
				PgPort:                      5432,
				DatastorePoolMaxConnections: 10,
				DatastorePoolMaxIdleSeconds: -1,
			},
			wantErr: true,
		},
		{
			name: "invalid datastore_pool_max_wait_seconds",
			config: &Config{
				PgHost:                      "localhost",
				PgDatabase:                  "testdb",
				PgUsername:                  "testuser",
				PgPort:                      5432,
				DatastorePoolMaxConnections: 10,
				DatastorePoolMaxIdleSeconds: 60,
				DatastorePoolMaxWaitSeconds: 0,
				MonitoredPoolMaxConnections: 5,
				MonitoredPoolMaxWaitSeconds: 60,
			},
			wantErr: true,
		},
		{
			name: "invalid monitored_pool_max_wait_seconds",
			config: &Config{
				PgHost:                      "localhost",
				PgDatabase:                  "testdb",
				PgUsername:                  "testuser",
				PgPort:                      5432,
				DatastorePoolMaxConnections: 10,
				DatastorePoolMaxIdleSeconds: 60,
				DatastorePoolMaxWaitSeconds: 60,
				MonitoredPoolMaxConnections: 5,
				MonitoredPoolMaxWaitSeconds: -1,
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
