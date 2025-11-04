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
	"testing"
)

func TestBuildConnectionString(t *testing.T) {
	config := &Config{
		PgHost:     "testhost",
		PgDatabase: "testdb",
		PgUsername: "testuser",
		PgPassword: "testpass",
		PgPort:     5432,
		PgSSLMode:  "require",
	}

	ds := &Datastore{
		config: config,
	}

	connStr := ds.buildConnectionString()

	// Check that all required parameters are present
	expectedParams := map[string]bool{
		"host":     false,
		"dbname":   false,
		"user":     false,
		"password": false,
		"port":     false,
		"sslmode":  false,
	}

	for key := range expectedParams {
		if !contains(connStr, key) {
			t.Errorf("Connection string missing parameter: %s", key)
		}
	}
}

func TestBuildConnectionStringWithHostAddr(t *testing.T) {
	config := &Config{
		PgHost:     "hostname.example.com",
		PgHostAddr: "192.168.1.100",
		PgDatabase: "testdb",
		PgUsername: "testuser",
		PgPort:     5432,
	}

	ds := &Datastore{
		config: config,
	}

	connStr := ds.buildConnectionString()

	// When hostaddr is provided, it should be used instead of host
	if !contains(connStr, "hostaddr") {
		t.Error("Connection string should contain hostaddr when provided")
	}

	// Should not contain host parameter when hostaddr is present
	if contains(connStr, "host=") {
		t.Error("Connection string should not contain host when hostaddr is provided")
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid minimal config",
			config: &Config{
				PgHost:             "localhost",
				PgDatabase:         "testdb",
				PgUsername:         "testuser",
				PgPort:             5432,
				PoolMaxConnections: 10,
				PoolMaxIdleSeconds: 60,
			},
			wantErr: false,
		},
		{
			name: "missing host",
			config: &Config{
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
				PgUsername: "testuser",
				PgPort:     5432,
			},
			wantErr: true,
		},
		{
			name: "invalid port",
			config: &Config{
				PgHost:     "localhost",
				PgDatabase: "testdb",
				PgUsername: "testuser",
				PgPort:     70000,
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

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(indexOf(s, substr) >= 0))
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
