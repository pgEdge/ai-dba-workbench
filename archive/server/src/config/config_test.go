/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNewConfig tests that NewConfig creates a config with expected defaults
func TestNewConfig(t *testing.T) {
	cfg := NewConfig()

	if cfg.TLS != false {
		t.Errorf("Expected TLS default to be false, got %v", cfg.TLS)
	}
	if cfg.Port != 8080 {
		t.Errorf("Expected Port default to be 8080, got %d", cfg.Port)
	}
	if cfg.PgHost != "localhost" {
		t.Errorf("Expected PgHost default to be localhost, got %s", cfg.PgHost)
	}
	if cfg.PgDatabase != "ai_workbench" {
		t.Errorf("Expected PgDatabase default to be ai_workbench, got %s",
			cfg.PgDatabase)
	}
	if cfg.PgUsername != "postgres" {
		t.Errorf("Expected PgUsername default to be postgres, got %s",
			cfg.PgUsername)
	}
	if cfg.PgPort != 5432 {
		t.Errorf("Expected PgPort default to be 5432, got %d", cfg.PgPort)
	}
	if cfg.PgSSLMode != "prefer" {
		t.Errorf("Expected PgSSLMode default to be prefer, got %s",
			cfg.PgSSLMode)
	}
}

// TestLoadFromFile tests loading configuration from a file
func TestLoadFromFile(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.conf")

	configContent := `# Test configuration file
tls = true
tls_cert = /path/to/cert.pem
tls_key = /path/to/key.pem
tls_chain = /path/to/chain.pem
port = 9443

pg_host = testhost
pg_database = testdb
pg_username = testuser
pg_port = 5433
pg_sslmode = require
server_secret = test_secret_12345
`

	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	cfg := NewConfig()
	if err := cfg.LoadFromFile(configPath); err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	// Verify all values were loaded correctly
	if cfg.TLS != true {
		t.Errorf("Expected TLS to be true, got %v", cfg.TLS)
	}
	if cfg.TLSCert != "/path/to/cert.pem" {
		t.Errorf("Expected TLSCert to be /path/to/cert.pem, got %s",
			cfg.TLSCert)
	}
	if cfg.TLSKey != "/path/to/key.pem" {
		t.Errorf("Expected TLSKey to be /path/to/key.pem, got %s", cfg.TLSKey)
	}
	if cfg.TLSChain != "/path/to/chain.pem" {
		t.Errorf("Expected TLSChain to be /path/to/chain.pem, got %s",
			cfg.TLSChain)
	}
	if cfg.Port != 9443 {
		t.Errorf("Expected Port to be 9443, got %d", cfg.Port)
	}
	if cfg.PgHost != "testhost" {
		t.Errorf("Expected PgHost to be testhost, got %s", cfg.PgHost)
	}
	if cfg.PgDatabase != "testdb" {
		t.Errorf("Expected PgDatabase to be testdb, got %s", cfg.PgDatabase)
	}
	if cfg.PgUsername != "testuser" {
		t.Errorf("Expected PgUsername to be testuser, got %s", cfg.PgUsername)
	}
	if cfg.PgPort != 5433 {
		t.Errorf("Expected PgPort to be 5433, got %d", cfg.PgPort)
	}
	if cfg.PgSSLMode != "require" {
		t.Errorf("Expected PgSSLMode to be require, got %s", cfg.PgSSLMode)
	}
	if cfg.ServerSecret != "test_secret_12345" {
		t.Errorf("Expected ServerSecret to be test_secret_12345, got %s",
			cfg.ServerSecret)
	}
}

// TestLoadFromFileWithQuotes tests loading configuration with quoted values
func TestLoadFromFileWithQuotes(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_quotes.conf")

	configContent := `pg_host = "quoted.host.com"
server_secret = "secret with spaces"
`

	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	cfg := NewConfig()
	if err := cfg.LoadFromFile(configPath); err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	if cfg.PgHost != "quoted.host.com" {
		t.Errorf("Expected PgHost to be quoted.host.com, got %s", cfg.PgHost)
	}
	if cfg.ServerSecret != "secret with spaces" {
		t.Errorf("Expected ServerSecret to be 'secret with spaces', got %s",
			cfg.ServerSecret)
	}
}

// TestLoadFromFileInvalidLines tests error handling for invalid config lines
func TestLoadFromFileInvalidLines(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_invalid.conf")

	configContent := `pg_host = testhost
invalid line without equals sign
pg_database = testdb
`

	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	cfg := NewConfig()
	err := cfg.LoadFromFile(configPath)
	if err == nil {
		t.Error("Expected error for invalid config line, got nil")
	}
}

// TestLoadFromFilePasswordFile tests loading password from a file
func TestLoadFromFilePasswordFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create password file
	passwordPath := filepath.Join(tmpDir, "pgpass")
	if err := os.WriteFile(passwordPath, []byte("secret_password\n"), 0600); err != nil {
		t.Fatalf("Failed to create password file: %v", err)
	}

	// Create config file
	configPath := filepath.Join(tmpDir, "test_password.conf")
	configContent := `pg_host = testhost
pg_password_file = ` + passwordPath + `
`

	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	cfg := NewConfig()
	if err := cfg.LoadFromFile(configPath); err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	if cfg.PgPassword != "secret_password" {
		t.Errorf("Expected PgPassword to be secret_password, got %s",
			cfg.PgPassword)
	}
}

// TestValidate tests configuration validation
func TestValidate(t *testing.T) {
	tests := []struct {
		name        string
		modifyFunc  func(*Config)
		expectError bool
		errorSubstr string
	}{
		{
			name:        "Valid default config",
			modifyFunc:  func(c *Config) {},
			expectError: false,
		},
		{
			name: "TLS enabled without cert",
			modifyFunc: func(c *Config) {
				c.TLS = true
				c.TLSKey = "/path/to/key"
			},
			expectError: true,
			errorSubstr: "tls_cert is required",
		},
		{
			name: "TLS enabled without key",
			modifyFunc: func(c *Config) {
				c.TLS = true
				c.TLSCert = "/path/to/cert"
			},
			expectError: true,
			errorSubstr: "tls_key is required",
		},
		{
			name: "Invalid port (too low)",
			modifyFunc: func(c *Config) {
				c.Port = 0
			},
			expectError: true,
			errorSubstr: "port must be between",
		},
		{
			name: "Invalid port (too high)",
			modifyFunc: func(c *Config) {
				c.Port = 70000
			},
			expectError: true,
			errorSubstr: "port must be between",
		},
		{
			name: "Empty pg_host",
			modifyFunc: func(c *Config) {
				c.PgHost = ""
			},
			expectError: true,
			errorSubstr: "pg_host is required",
		},
		{
			name: "Empty pg_database",
			modifyFunc: func(c *Config) {
				c.PgDatabase = ""
			},
			expectError: true,
			errorSubstr: "pg_database is required",
		},
		{
			name: "Empty pg_username",
			modifyFunc: func(c *Config) {
				c.PgUsername = ""
			},
			expectError: true,
			errorSubstr: "pg_username is required",
		},
		{
			name: "Invalid pg_port",
			modifyFunc: func(c *Config) {
				c.PgPort = 0
			},
			expectError: true,
			errorSubstr: "pg_port must be between",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewConfig()
			tt.modifyFunc(cfg)

			err := cfg.Validate()
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected validation error, got nil")
				} else if tt.errorSubstr != "" && !contains(err.Error(),
					tt.errorSubstr) {
					t.Errorf("Expected error containing '%s', got '%s'",
						tt.errorSubstr, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no validation error, got: %v", err)
				}
			}
		})
	}
}

// TestGetterMethods tests all getter methods
func TestGetterMethods(t *testing.T) {
	cfg := &Config{
		TLS:           true,
		TLSCert:       "/cert",
		TLSKey:        "/key",
		TLSChain:      "/chain",
		Port:          8443,
		PgHost:        "host",
		PgHostAddr:    "1.2.3.4",
		PgDatabase:    "db",
		PgUsername:    "user",
		PgPassword:    "pass",
		PgPort:        5432,
		PgSSLMode:     "require",
		PgSSLCert:     "/pg_cert",
		PgSSLKey:      "/pg_key",
		PgSSLRootCert: "/pg_root",
		ServerSecret:  "secret",
	}

	if cfg.GetTLS() != true {
		t.Errorf("GetTLS() failed")
	}
	if cfg.GetTLSCert() != "/cert" {
		t.Errorf("GetTLSCert() failed")
	}
	if cfg.GetTLSKey() != "/key" {
		t.Errorf("GetTLSKey() failed")
	}
	if cfg.GetTLSChain() != "/chain" {
		t.Errorf("GetTLSChain() failed")
	}
	if cfg.GetPort() != 8443 {
		t.Errorf("GetPort() failed")
	}
	if cfg.GetPgHost() != "host" {
		t.Errorf("GetPgHost() failed")
	}
	if cfg.GetPgHostAddr() != "1.2.3.4" {
		t.Errorf("GetPgHostAddr() failed")
	}
	if cfg.GetPgDatabase() != "db" {
		t.Errorf("GetPgDatabase() failed")
	}
	if cfg.GetPgUsername() != "user" {
		t.Errorf("GetPgUsername() failed")
	}
	if cfg.GetPgPassword() != "pass" {
		t.Errorf("GetPgPassword() failed")
	}
	if cfg.GetPgPort() != 5432 {
		t.Errorf("GetPgPort() failed")
	}
	if cfg.GetPgSSLMode() != "require" {
		t.Errorf("GetPgSSLMode() failed")
	}
	if cfg.GetPgSSLCert() != "/pg_cert" {
		t.Errorf("GetPgSSLCert() failed")
	}
	if cfg.GetPgSSLKey() != "/pg_key" {
		t.Errorf("GetPgSSLKey() failed")
	}
	if cfg.GetPgSSLRootCert() != "/pg_root" {
		t.Errorf("GetPgSSLRootCert() failed")
	}
	if cfg.GetServerSecret() != "secret" {
		t.Errorf("GetServerSecret() failed")
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		len(s) > len(substr) && (s[:len(substr)] == substr ||
			s[len(s)-len(substr):] == substr ||
			containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
