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
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pgedge/ai-workbench/server/internal/config"
)

func TestConnectToRejectsBadInput(t *testing.T) {
	tests := []struct {
		name     string
		connStr  string
		dbConfig *config.DatabaseConfig
		wantErr  string
	}{
		{
			name:    "unparseable URL",
			connStr: "postgres://%zz",
			wantErr: "unable to enhance connection string",
		},
		{
			name:    "invalid pgx option",
			connStr: "postgres://postgres@127.0.0.1:5432/x?sslmode=bogus",
			wantErr: "unable to parse connection string",
		},
		{
			name:     "invalid idle time",
			connStr:  "postgres://postgres@127.0.0.1:5432/x",
			dbConfig: &config.DatabaseConfig{PoolMaxConnIdleTime: "nope"},
			wantErr:  "invalid pool_max_conn_idle_time",
		},
		{
			name:    "unreachable server",
			connStr: "postgres://postgres@127.0.0.1:1/x?connect_timeout=2",
			wantErr: "unable to ping database",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewClient(tt.dbConfig)
			err := c.ConnectTo(tt.connStr)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want one containing %q", err, tt.wantErr)
			}
			if pool := c.GetPoolFor(tt.connStr); pool != nil {
				t.Error("a failed connection left a pool behind")
			}
		})
	}
}

// TestConnectToConfiguresPoolIntegration checks the settings ConnectTo
// applies, in particular that the pool always uses the extended
// protocol, which refuses a query holding several statements, even
// when the connection string asks for the simple protocol.
func TestConnectToConfiguresPoolIntegration(t *testing.T) {
	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	base := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if base == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping integration test")
	}
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	connStr := base + sep + "default_query_exec_mode=simple_protocol"

	prevLevel := GetLogLevel()
	SetLogLevel(LogLevelDebug)
	defer SetLogLevel(prevLevel)

	c := NewClient(&config.DatabaseConfig{
		PoolMaxConns:        3,
		PoolMinConns:        1,
		PoolMaxConnIdleTime: "90s",
	})
	defer c.Close()

	if err := c.ConnectTo(connStr); err != nil {
		t.Fatalf("ConnectTo: %v", err)
	}
	if err := c.ConnectTo(connStr); err != nil {
		t.Fatalf("second ConnectTo should be a no-op: %v", err)
	}

	pool := c.GetPoolFor(connStr)
	if pool == nil {
		t.Fatal("no pool after ConnectTo")
	}
	cfg := pool.Config()
	if got := cfg.ConnConfig.DefaultQueryExecMode; got != pgx.QueryExecModeCacheStatement {
		t.Errorf("DefaultQueryExecMode = %v, want QueryExecModeCacheStatement", got)
	}
	if got := cfg.ConnConfig.RuntimeParams["default_transaction_read_only"]; got != "on" {
		t.Errorf("default_transaction_read_only = %q, want on", got)
	}
	if cfg.MaxConns != 3 || cfg.MinConns != 1 || cfg.MaxConnIdleTime != 90*time.Second {
		t.Errorf("pool settings = max %d, min %d, idle %v",
			cfg.MaxConns, cfg.MinConns, cfg.MaxConnIdleTime)
	}
}
