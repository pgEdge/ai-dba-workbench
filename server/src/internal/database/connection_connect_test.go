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
	"time"

	"github.com/pgedge/ai-workbench/server/internal/config"
)

// TestConnectToErrors covers the ConnectTo failures that occur before
// or instead of a working connection; none needs a database.
func TestConnectToErrors(t *testing.T) {
	tests := []struct {
		name     string
		connStr  string
		dbConfig *config.DatabaseConfig
		wantErr  string
	}{
		{
			name:    "unparsable URL",
			connStr: "postgres://%zz",
			wantErr: "unable to enhance connection string",
		},
		{
			name:    "invalid pgx option",
			connStr: "postgres://localhost/db?sslmode=bogus",
			wantErr: "unable to parse connection string",
		},
		{
			name:     "invalid idle time",
			connStr:  "postgres://localhost/db",
			dbConfig: &config.DatabaseConfig{PoolMaxConnIdleTime: "soon"},
			wantErr:  "invalid pool_max_conn_idle_time",
		},
		{
			// Port 1 on loopback refuses the connection at once.
			name:    "unreachable server",
			connStr: "postgres://postgres@127.0.0.1:1/db?connect_timeout=2&sslmode=disable",
			wantErr: "unable to ping database",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.dbConfig)
			err := client.ConnectTo(tt.connStr)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ConnectTo(%q) error = %v, want one containing %q", tt.connStr, err, tt.wantErr)
			}
			if len(client.ListConnections()) != 0 {
				t.Error("a failed ConnectTo left a connection registered")
			}
		})
	}
}

// TestConnectToAppliesPoolConfig checks the pool settings and the
// session reset hook on a real connection, and that a second
// ConnectTo for the same string reuses the existing pool.
func TestConnectToAppliesPoolConfig(t *testing.T) {
	connStr := sessionResetTestDSN(t)

	previous := GetLogLevel()
	SetLogLevel(LogLevelDebug)
	t.Cleanup(func() { SetLogLevel(previous) })

	client := NewClient(&config.DatabaseConfig{
		PoolMaxConns:        3,
		PoolMinConns:        1,
		PoolMaxConnIdleTime: "90s",
	})
	if err := client.ConnectTo(connStr); err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	t.Cleanup(client.Close)

	pool := client.GetPoolFor(connStr)
	if pool == nil {
		t.Fatal("GetPoolFor returned nil after ConnectTo")
	}
	cfg := pool.Config()
	if cfg.MaxConns != 3 || cfg.MinConns != 1 || cfg.MaxConnIdleTime != 90*time.Second {
		t.Errorf("pool config = max %d, min %d, idle %s; want 3, 1, 1m30s",
			cfg.MaxConns, cfg.MinConns, cfg.MaxConnIdleTime)
	}
	// pgx's default lifetime bounds how long state the reset cannot
	// clear (LOAD, dblink connections) can last; see sessionResetSteps.
	if cfg.MaxConnLifetime != time.Hour {
		t.Errorf("MaxConnLifetime = %s, want pgx's default of 1h", cfg.MaxConnLifetime)
	}
	if cfg.AfterRelease == nil {
		t.Error("pool has no AfterRelease hook to reset session state")
	}
	if got := cfg.ConnConfig.RuntimeParams["default_transaction_read_only"]; got != "on" {
		t.Errorf("default_transaction_read_only = %q, want on", got)
	}

	if err := client.ConnectTo(connStr); err != nil {
		t.Fatalf("second ConnectTo failed: %v", err)
	}
	if client.GetPoolFor(connStr) != pool {
		t.Error("second ConnectTo replaced the existing pool")
	}
}
