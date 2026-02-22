/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package database

import (
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
)

// TestBuildConnectionString tests the buildConnectionString function
func TestBuildConnectionString(t *testing.T) {
	tests := []struct {
		name     string
		config   *config.Config
		contains []string
		excludes []string
	}{
		{
			name: "basic connection string",
			config: &config.Config{
				Datastore: config.DatastoreConfig{
					Host:     "localhost",
					Port:     5432,
					Database: "testdb",
					Username: "testuser",
					SSLMode:  "disable",
				},
			},
			contains: []string{
				"host=localhost",
				"port=5432",
				"dbname=testdb",
				"user=testuser",
				"sslmode=disable",
			},
			excludes: []string{
				"hostaddr=",
				"password=",
				"sslcert=",
				"sslkey=",
				"sslrootcert=",
			},
		},
		{
			name: "connection string with password",
			config: &config.Config{
				Datastore: config.DatastoreConfig{
					Host:     "db.example.com",
					Port:     5433,
					Database: "production",
					Username: "admin",
					Password: "secret123",
					SSLMode:  "require",
				},
			},
			contains: []string{
				"host=db.example.com",
				"port=5433",
				"dbname=production",
				"user=admin",
				"password='secret123'",
				"sslmode=require",
			},
		},
		{
			name: "connection string with hostaddr",
			config: &config.Config{
				Datastore: config.DatastoreConfig{
					Host:     "db.example.com",
					HostAddr: "192.168.1.100",
					Port:     5432,
					Database: "testdb",
					Username: "testuser",
					SSLMode:  "prefer",
				},
			},
			contains: []string{
				"host=db.example.com",
				"hostaddr=192.168.1.100",
				"port=5432",
			},
		},
		{
			name: "connection string with SSL certificates",
			config: &config.Config{
				Datastore: config.DatastoreConfig{
					Host:        "secure.example.com",
					Port:        5432,
					Database:    "testdb",
					Username:    "testuser",
					SSLMode:     "verify-full",
					SSLCert:     "/path/to/client.crt",
					SSLKey:      "/path/to/client.key",
					SSLRootCert: "/path/to/ca.crt",
				},
			},
			contains: []string{
				"host=secure.example.com",
				"sslmode=verify-full",
				"sslcert=/path/to/client.crt",
				"sslkey=/path/to/client.key",
				"sslrootcert=/path/to/ca.crt",
			},
		},
		{
			name: "connection string with all options",
			config: &config.Config{
				Datastore: config.DatastoreConfig{
					Host:        "db.example.com",
					HostAddr:    "10.0.0.5",
					Port:        5432,
					Database:    "mydb",
					Username:    "myuser",
					Password:    "mypass",
					SSLMode:     "verify-ca",
					SSLCert:     "/ssl/client.crt",
					SSLKey:      "/ssl/client.key",
					SSLRootCert: "/ssl/root.crt",
				},
			},
			contains: []string{
				"host=db.example.com",
				"hostaddr=10.0.0.5",
				"port=5432",
				"dbname=mydb",
				"user=myuser",
				"password='mypass'",
				"sslmode=verify-ca",
				"sslcert=/ssl/client.crt",
				"sslkey=/ssl/client.key",
				"sslrootcert=/ssl/root.crt",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			connStr := buildConnectionString(tt.config)

			for _, expected := range tt.contains {
				if !strings.Contains(connStr, expected) {
					t.Errorf("connection string should contain %q, got %q", expected, connStr)
				}
			}

			for _, excluded := range tt.excludes {
				if strings.Contains(connStr, excluded) {
					t.Errorf("connection string should not contain %q, got %q", excluded, connStr)
				}
			}
		})
	}
}

// TestBuildConnectionStringOrder tests that required fields come first
func TestBuildConnectionStringOrder(t *testing.T) {
	cfg := &config.Config{
		Datastore: config.DatastoreConfig{
			Host:     "localhost",
			Port:     5432,
			Database: "testdb",
			Username: "testuser",
			SSLMode:  "disable",
		},
	}

	connStr := buildConnectionString(cfg)

	// The connection string should start with "host="
	if !strings.HasPrefix(connStr, "host=") {
		t.Errorf("connection string should start with 'host=', got %q", connStr)
	}
}

// TestBuildConnectionStringNoEmptyValues tests that empty values are not included
func TestBuildConnectionStringNoEmptyValues(t *testing.T) {
	cfg := &config.Config{
		Datastore: config.DatastoreConfig{
			Host:        "localhost",
			Port:        5432,
			Database:    "testdb",
			Username:    "testuser",
			SSLMode:     "disable",
			Password:    "",
			HostAddr:    "",
			SSLCert:     "",
			SSLKey:      "",
			SSLRootCert: "",
		},
	}

	connStr := buildConnectionString(cfg)

	// Empty values should not be included
	if strings.Contains(connStr, "password=") {
		t.Errorf("connection string should not contain empty password, got %q", connStr)
	}
	if strings.Contains(connStr, "hostaddr=") {
		t.Errorf("connection string should not contain empty hostaddr, got %q", connStr)
	}
	if strings.Contains(connStr, "sslcert=") {
		t.Errorf("connection string should not contain empty sslcert, got %q", connStr)
	}
}

// TestDatastoreCloseNilPool tests that Close handles nil pool gracefully
func TestDatastoreCloseNilPool(t *testing.T) {
	ds := &Datastore{
		pool:   nil,
		config: nil,
	}

	// Should not panic
	ds.Close()
}

// TestDatastorePoolAccessor tests the Pool accessor method
func TestDatastorePoolAccessor(t *testing.T) {
	ds := &Datastore{
		pool:   nil,
		config: nil,
	}

	// Pool should return nil when no pool is set
	if ds.Pool() != nil {
		t.Errorf("Pool() should return nil for uninitialized datastore")
	}
}
