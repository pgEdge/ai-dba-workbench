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
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/pkg/connstring"
	"github.com/pgedge/ai-workbench/pkg/datastoreconfig"
)

// TestBuildConnectionString tests that connstring.BuildFromConfig produces
// the expected connection string from a DatastoreConfig.
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
				Datastore: datastoreconfig.DatastoreConfig{
					Host:     "localhost",
					Port:     5432,
					Database: "testdb",
					Username: "testuser",
					SSLMode:  "disable",
				},
			},
			contains: []string{
				"host='localhost'",
				"port='5432'",
				"dbname='testdb'",
				"user='testuser'",
				"sslmode='disable'",
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
				Datastore: datastoreconfig.DatastoreConfig{
					Host:     "db.example.com",
					Port:     5433,
					Database: "production",
					Username: "admin",
					Password: "secret123",
					SSLMode:  "require",
				},
			},
			contains: []string{
				"host='db.example.com'",
				"port='5433'",
				"dbname='production'",
				"user='admin'",
				"password='secret123'",
				"sslmode='require'",
			},
		},
		{
			name: "connection string with hostaddr",
			config: &config.Config{
				Datastore: datastoreconfig.DatastoreConfig{
					Host:     "db.example.com",
					HostAddr: "192.168.1.100",
					Port:     5432,
					Database: "testdb",
					Username: "testuser",
					SSLMode:  "prefer",
				},
			},
			contains: []string{
				"host='db.example.com'",
				"hostaddr='192.168.1.100'",
				"port='5432'",
			},
		},
		{
			name: "connection string with SSL certificates",
			config: &config.Config{
				Datastore: datastoreconfig.DatastoreConfig{
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
				"host='secure.example.com'",
				"sslmode='verify-full'",
				"sslcert='/path/to/client.crt'",
				"sslkey='/path/to/client.key'",
				"sslrootcert='/path/to/ca.crt'",
			},
		},
		{
			name: "connection string with all options",
			config: &config.Config{
				Datastore: datastoreconfig.DatastoreConfig{
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
				"host='db.example.com'",
				"hostaddr='10.0.0.5'",
				"port='5432'",
				"dbname='mydb'",
				"user='myuser'",
				"password='mypass'",
				"sslmode='verify-ca'",
				"sslcert='/ssl/client.crt'",
				"sslkey='/ssl/client.key'",
				"sslrootcert='/ssl/root.crt'",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			connStr := connstring.BuildFromConfig(tt.config.Datastore, "")

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

// TestBuildConnectionStringNoEmptyValues tests that empty values are not included
func TestBuildConnectionStringNoEmptyValues(t *testing.T) {
	cfg := datastoreconfig.DatastoreConfig{
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
	}

	connStr := connstring.BuildFromConfig(cfg, "")

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

// TestNewDatastoreSuccess exercises the happy path through NewDatastore
// against the integration test database. This is required to lift the
// datastore.go line coverage above 90%; the closed-pool error path
// short-circuits at Close() so it cannot reach the pool-creation code
// inside NewDatastore.
func TestNewDatastoreSuccess(t *testing.T) {
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping NewDatastore success test")
	}

	// Parse the connection string into a DatastoreConfig.
	host, port, db, user, pw := parseTestConnString(connStr)
	cfg := &config.Config{
		Datastore: datastoreconfig.DatastoreConfig{
			Host:     host,
			Port:     port,
			Database: db,
			Username: user,
			Password: pw,
			SSLMode:  "disable",
		},
		Pool: config.PoolConfig{
			MaxConnections: 5,
			MaxIdleSeconds: 30,
		},
	}
	ds, err := NewDatastore(cfg)
	if err != nil {
		t.Fatalf("NewDatastore: %v", err)
	}
	defer ds.Close()
	if ds.Pool() == nil {
		t.Errorf("expected non-nil pool from NewDatastore")
	}
}

// TestNewDatastoreInvalidConfig exercises the parse-failure branch of
// NewDatastore. An invalid SSL mode produces a parse error from the
// underlying pgxpool.ParseConfig call.
func TestNewDatastoreInvalidConfig(t *testing.T) {
	cfg := &config.Config{
		Datastore: datastoreconfig.DatastoreConfig{
			Host:     "localhost",
			Port:     -1,
			Database: "x",
			Username: "x",
			SSLMode:  "not-a-real-mode",
		},
	}
	if _, err := NewDatastore(cfg); err == nil {
		t.Errorf("expected error for invalid config")
	}
}

// TestNewDatastorePingFailure exercises the failed-Ping branch of
// NewDatastore by pointing it at an unreachable port. The connection
// pool can be constructed (lazy), but the Ping() at the end of
// NewDatastore must fail and trigger pool.Close + error return.
func TestNewDatastorePingFailure(t *testing.T) {
	cfg := &config.Config{
		Datastore: datastoreconfig.DatastoreConfig{
			Host:     "127.0.0.1",
			Port:     1, // unlikely to host PostgreSQL
			Database: "no_such_db",
			Username: "nobody",
			SSLMode:  "disable",
		},
		Pool: config.PoolConfig{
			MaxConnections: 1,
			MaxIdleSeconds: 1,
		},
	}
	if _, err := NewDatastore(cfg); err == nil {
		t.Errorf("expected ping failure on unreachable port")
	}
}

// parseTestConnString extracts host, port, db, user, password from a
// libpq-style URI. Only the limited form used by the integration tests
// is supported.
func parseTestConnString(connStr string) (host string, port int, db, user, pw string) {
	// Default values
	host = "localhost"
	port = 5432
	// Strip protocol.
	rest := strings.TrimPrefix(connStr, "postgres://")
	rest = strings.TrimPrefix(rest, "postgresql://")
	// Strip query.
	if i := strings.Index(rest, "?"); i >= 0 {
		rest = rest[:i]
	}
	// user[:pw]@host[:port]/db
	if i := strings.Index(rest, "@"); i >= 0 {
		userPart := rest[:i]
		rest = rest[i+1:]
		if j := strings.Index(userPart, ":"); j >= 0 {
			user = userPart[:j]
			pw = userPart[j+1:]
		} else {
			user = userPart
		}
	}
	if i := strings.Index(rest, "/"); i >= 0 {
		db = rest[i+1:]
		hostPart := rest[:i]
		if j := strings.Index(hostPart, ":"); j >= 0 {
			host = hostPart[:j]
			if p, err := strconv.Atoi(hostPart[j+1:]); err == nil {
				port = p
			}
		} else {
			host = hostPart
		}
	}
	return
}
