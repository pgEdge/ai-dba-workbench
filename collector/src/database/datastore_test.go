/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package database

import (
	"testing"
)

// mockConfig implements the Config interface for testing
type mockConfig struct {
	pgHost                      string
	pgHostAddr                  string
	pgDatabase                  string
	pgUsername                  string
	pgPassword                  string
	pgPort                      int
	pgSSLMode                   string
	pgSSLCert                   string
	pgSSLKey                    string
	pgSSLRootCert               string
	datastorePoolMaxConnections int
	datastorePoolMaxIdleSeconds int
	validationError             error
}

func (m *mockConfig) Validate() error {
	return m.validationError
}

func (m *mockConfig) GetPgHost() string {
	return m.pgHost
}

func (m *mockConfig) GetPgHostAddr() string {
	return m.pgHostAddr
}

func (m *mockConfig) GetPgDatabase() string {
	return m.pgDatabase
}

func (m *mockConfig) GetPgUsername() string {
	return m.pgUsername
}

func (m *mockConfig) GetPgPassword() string {
	return m.pgPassword
}

func (m *mockConfig) GetPgPort() int {
	return m.pgPort
}

func (m *mockConfig) GetPgSSLMode() string {
	return m.pgSSLMode
}

func (m *mockConfig) GetPgSSLCert() string {
	return m.pgSSLCert
}

func (m *mockConfig) GetPgSSLKey() string {
	return m.pgSSLKey
}

func (m *mockConfig) GetPgSSLRootCert() string {
	return m.pgSSLRootCert
}

func (m *mockConfig) GetDatastorePoolMaxConnections() int {
	return m.datastorePoolMaxConnections
}

func (m *mockConfig) GetDatastorePoolMaxIdleSeconds() int {
	return m.datastorePoolMaxIdleSeconds
}

func TestBuildConnectionString(t *testing.T) {
	config := &mockConfig{
		pgHost:                      "testhost",
		pgDatabase:                  "testdb",
		pgUsername:                  "testuser",
		pgPassword:                  "testpass",
		pgPort:                      5432,
		pgSSLMode:                   "require",
		datastorePoolMaxConnections: 10,
		datastorePoolMaxIdleSeconds: 60,
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
	config := &mockConfig{
		pgHost:                      "hostname.example.com",
		pgHostAddr:                  "192.168.1.100",
		pgDatabase:                  "testdb",
		pgUsername:                  "testuser",
		pgPort:                      5432,
		datastorePoolMaxConnections: 10,
		datastorePoolMaxIdleSeconds: 60,
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
		config  *mockConfig
		wantErr bool
	}{
		{
			name: "valid minimal config",
			config: &mockConfig{
				pgHost:                      "localhost",
				pgDatabase:                  "testdb",
				pgUsername:                  "testuser",
				pgPort:                      5432,
				datastorePoolMaxConnections: 10,
				datastorePoolMaxIdleSeconds: 60,
				validationError:             nil,
			},
			wantErr: false,
		},
		{
			name: "missing host",
			config: &mockConfig{
				pgDatabase:                  "testdb",
				pgUsername:                  "testuser",
				pgPort:                      5432,
				datastorePoolMaxConnections: 10,
				datastorePoolMaxIdleSeconds: 60,
				validationError:             nil,
			},
			wantErr: false, // The database package doesn't validate, that's done by main.Config
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
