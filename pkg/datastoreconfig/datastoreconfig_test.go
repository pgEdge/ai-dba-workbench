/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package datastoreconfig

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDatastoreConfigDefaults(t *testing.T) {
	// Test that a zero-value config has expected defaults
	var cfg DatastoreConfig

	if cfg.Host != "" {
		t.Errorf("expected empty Host, got %q", cfg.Host)
	}
	if cfg.HostAddr != "" {
		t.Errorf("expected empty HostAddr, got %q", cfg.HostAddr)
	}
	if cfg.Database != "" {
		t.Errorf("expected empty Database, got %q", cfg.Database)
	}
	if cfg.Username != "" {
		t.Errorf("expected empty Username, got %q", cfg.Username)
	}
	if cfg.Password != "" {
		t.Errorf("expected empty Password, got %q", cfg.Password)
	}
	if cfg.PasswordFile != "" {
		t.Errorf("expected empty PasswordFile, got %q", cfg.PasswordFile)
	}
	if cfg.Port != 0 {
		t.Errorf("expected zero Port, got %d", cfg.Port)
	}
	if cfg.SSLMode != "" {
		t.Errorf("expected empty SSLMode, got %q", cfg.SSLMode)
	}
	if cfg.SSLCert != "" {
		t.Errorf("expected empty SSLCert, got %q", cfg.SSLCert)
	}
	if cfg.SSLKey != "" {
		t.Errorf("expected empty SSLKey, got %q", cfg.SSLKey)
	}
	if cfg.SSLRootCert != "" {
		t.Errorf("expected empty SSLRootCert, got %q", cfg.SSLRootCert)
	}
}

func TestDatastoreConfigYAMLTags(t *testing.T) {
	yamlData := `
host: db.example.com
hostaddr: 192.168.1.100
database: mydb
username: admin
password: secret123
password_file: /etc/secrets/db_password
port: 5432
sslmode: require
sslcert: /etc/ssl/client.crt
sslkey: /etc/ssl/client.key
sslrootcert: /etc/ssl/ca.crt
`

	var cfg DatastoreConfig
	if err := yaml.Unmarshal([]byte(yamlData), &cfg); err != nil {
		t.Fatalf("failed to unmarshal YAML: %v", err)
	}

	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{"Host", cfg.Host, "db.example.com"},
		{"HostAddr", cfg.HostAddr, "192.168.1.100"},
		{"Database", cfg.Database, "mydb"},
		{"Username", cfg.Username, "admin"},
		{"Password", cfg.Password, "secret123"},
		{"PasswordFile", cfg.PasswordFile, "/etc/secrets/db_password"},
		{"SSLMode", cfg.SSLMode, "require"},
		{"SSLCert", cfg.SSLCert, "/etc/ssl/client.crt"},
		{"SSLKey", cfg.SSLKey, "/etc/ssl/client.key"},
		{"SSLRootCert", cfg.SSLRootCert, "/etc/ssl/ca.crt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.expected)
			}
		})
	}

	if cfg.Port != 5432 {
		t.Errorf("Port = %d, want 5432", cfg.Port)
	}
}

func TestDatastoreConfigYAMLMarshal(t *testing.T) {
	cfg := DatastoreConfig{
		Host:         "localhost",
		HostAddr:     "127.0.0.1",
		Database:     "testdb",
		Username:     "testuser",
		Password:     "testpass",
		PasswordFile: "/path/to/password",
		Port:         5433,
		SSLMode:      "verify-full",
		SSLCert:      "/path/to/cert",
		SSLKey:       "/path/to/key",
		SSLRootCert:  "/path/to/root",
	}

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}

	// Unmarshal back to verify round-trip
	var cfg2 DatastoreConfig
	if err := yaml.Unmarshal(data, &cfg2); err != nil {
		t.Fatalf("failed to unmarshal YAML: %v", err)
	}

	if cfg2.Host != cfg.Host {
		t.Errorf("Host round-trip failed: got %q, want %q", cfg2.Host, cfg.Host)
	}
	if cfg2.HostAddr != cfg.HostAddr {
		t.Errorf("HostAddr round-trip failed: got %q, want %q",
			cfg2.HostAddr, cfg.HostAddr)
	}
	if cfg2.Database != cfg.Database {
		t.Errorf("Database round-trip failed: got %q, want %q",
			cfg2.Database, cfg.Database)
	}
	if cfg2.Username != cfg.Username {
		t.Errorf("Username round-trip failed: got %q, want %q",
			cfg2.Username, cfg.Username)
	}
	if cfg2.Password != cfg.Password {
		t.Errorf("Password round-trip failed: got %q, want %q",
			cfg2.Password, cfg.Password)
	}
	if cfg2.PasswordFile != cfg.PasswordFile {
		t.Errorf("PasswordFile round-trip failed: got %q, want %q",
			cfg2.PasswordFile, cfg.PasswordFile)
	}
	if cfg2.Port != cfg.Port {
		t.Errorf("Port round-trip failed: got %d, want %d", cfg2.Port, cfg.Port)
	}
	if cfg2.SSLMode != cfg.SSLMode {
		t.Errorf("SSLMode round-trip failed: got %q, want %q",
			cfg2.SSLMode, cfg.SSLMode)
	}
	if cfg2.SSLCert != cfg.SSLCert {
		t.Errorf("SSLCert round-trip failed: got %q, want %q",
			cfg2.SSLCert, cfg.SSLCert)
	}
	if cfg2.SSLKey != cfg.SSLKey {
		t.Errorf("SSLKey round-trip failed: got %q, want %q",
			cfg2.SSLKey, cfg.SSLKey)
	}
	if cfg2.SSLRootCert != cfg.SSLRootCert {
		t.Errorf("SSLRootCert round-trip failed: got %q, want %q",
			cfg2.SSLRootCert, cfg.SSLRootCert)
	}
}

func TestDatastoreConfigPartialYAML(t *testing.T) {
	// Test that partial YAML only sets specified fields
	yamlData := `
host: db.example.com
database: mydb
username: admin
port: 5432
`

	var cfg DatastoreConfig
	if err := yaml.Unmarshal([]byte(yamlData), &cfg); err != nil {
		t.Fatalf("failed to unmarshal YAML: %v", err)
	}

	// Specified fields
	if cfg.Host != "db.example.com" {
		t.Errorf("Host = %q, want %q", cfg.Host, "db.example.com")
	}
	if cfg.Database != "mydb" {
		t.Errorf("Database = %q, want %q", cfg.Database, "mydb")
	}
	if cfg.Username != "admin" {
		t.Errorf("Username = %q, want %q", cfg.Username, "admin")
	}
	if cfg.Port != 5432 {
		t.Errorf("Port = %d, want 5432", cfg.Port)
	}

	// Unspecified fields should be zero values
	if cfg.HostAddr != "" {
		t.Errorf("HostAddr = %q, want empty string", cfg.HostAddr)
	}
	if cfg.Password != "" {
		t.Errorf("Password = %q, want empty string", cfg.Password)
	}
	if cfg.PasswordFile != "" {
		t.Errorf("PasswordFile = %q, want empty string", cfg.PasswordFile)
	}
	if cfg.SSLMode != "" {
		t.Errorf("SSLMode = %q, want empty string", cfg.SSLMode)
	}
	if cfg.SSLCert != "" {
		t.Errorf("SSLCert = %q, want empty string", cfg.SSLCert)
	}
	if cfg.SSLKey != "" {
		t.Errorf("SSLKey = %q, want empty string", cfg.SSLKey)
	}
	if cfg.SSLRootCert != "" {
		t.Errorf("SSLRootCert = %q, want empty string", cfg.SSLRootCert)
	}
}

func TestDatastoreConfigSSLModeValues(t *testing.T) {
	sslModes := []string{
		"disable",
		"allow",
		"prefer",
		"require",
		"verify-ca",
		"verify-full",
	}

	for _, mode := range sslModes {
		t.Run(mode, func(t *testing.T) {
			cfg := DatastoreConfig{SSLMode: mode}
			if cfg.SSLMode != mode {
				t.Errorf("SSLMode = %q, want %q", cfg.SSLMode, mode)
			}
		})
	}
}

func TestDatastoreConfigEmptyYAML(t *testing.T) {
	var cfg DatastoreConfig
	if err := yaml.Unmarshal([]byte("{}"), &cfg); err != nil {
		t.Fatalf("failed to unmarshal empty YAML: %v", err)
	}

	// All fields should be zero values
	if cfg.Host != "" {
		t.Errorf("Host = %q, want empty string", cfg.Host)
	}
	if cfg.Port != 0 {
		t.Errorf("Port = %d, want 0", cfg.Port)
	}
}

func TestDatastoreConfigSpecialCharacters(t *testing.T) {
	// Test with special characters in password and other fields
	yamlData := `
host: "db.example.com"
database: "my-db_test"
username: "admin@domain.com"
password: "p@ss'w\"ord!#$%^&*()"
port: 5432
`

	var cfg DatastoreConfig
	if err := yaml.Unmarshal([]byte(yamlData), &cfg); err != nil {
		t.Fatalf("failed to unmarshal YAML: %v", err)
	}

	if cfg.Username != "admin@domain.com" {
		t.Errorf("Username = %q, want %q", cfg.Username, "admin@domain.com")
	}
	if cfg.Password != "p@ss'w\"ord!#$%^&*()" {
		t.Errorf("Password = %q, want %q", cfg.Password, "p@ss'w\"ord!#$%^&*()")
	}
	if cfg.Database != "my-db_test" {
		t.Errorf("Database = %q, want %q", cfg.Database, "my-db_test")
	}
}
