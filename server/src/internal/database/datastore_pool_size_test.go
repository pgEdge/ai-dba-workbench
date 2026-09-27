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

// TestNewDatastorePoolMaxConns checks the datastore pool size: an unset
// pool_max_conns selects config.DefaultPoolMaxConns (issue #478) rather
// than pgxpool's own max(4, NumCPU), and an explicit value wins.
func TestNewDatastorePoolMaxConns(t *testing.T) {
	tests := []struct {
		name       string
		configured int
		want       int32
	}{
		{"unset selects the default", 0, config.DefaultPoolMaxConns},
		{"negative selects the default", -1, config.DefaultPoolMaxConns},
		{"explicit value wins", 3, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testDatastoreConfig(t)
			cfg.PoolMaxConns = tt.configured

			ds, err := NewDatastore(cfg, "test-secret")
			if err != nil {
				t.Skipf("Could not connect to test database: %v", err)
			}
			defer ds.Close()

			if got := ds.GetPool().Config().MaxConns; got != tt.want {
				t.Fatalf("MaxConns = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestNewDatastorePoolSettings covers the remaining pool settings and
// the constructor's error paths, which share the pool-size code above.
func TestNewDatastorePoolSettings(t *testing.T) {
	t.Run("nil configuration is rejected", func(t *testing.T) {
		if _, err := NewDatastore(nil, "test-secret"); err == nil {
			t.Fatal("NewDatastore(nil) returned no error")
		}
	})

	t.Run("min conns and idle time are applied", func(t *testing.T) {
		cfg := testDatastoreConfig(t)
		cfg.PoolMinConns = 1
		cfg.PoolMaxConnIdleTime = "90s"

		ds, err := NewDatastore(cfg, "test-secret")
		if err != nil {
			t.Fatalf("NewDatastore: %v", err)
		}
		defer ds.Close()

		pc := ds.GetPool().Config()
		if pc.MinConns != 1 || pc.MaxConnIdleTime != 90*time.Second {
			t.Fatalf("MinConns = %d, MaxConnIdleTime = %v; want 1 and 90s",
				pc.MinConns, pc.MaxConnIdleTime)
		}
	})

	errorCases := []struct {
		name   string
		modify func(c *config.DatabaseConfig)
		want   string
	}{
		{"invalid idle time", func(c *config.DatabaseConfig) {
			c.PoolMaxConnIdleTime = "soon"
		}, "invalid pool_max_conn_idle_time"},
		{"unparseable connection string", func(c *config.DatabaseConfig) {
			c.SSLMode = "not-a-mode"
		}, "failed to parse connection string"},
		{"unreachable server", func(c *config.DatabaseConfig) {
			c.Host = "127.0.0.1"
			c.Port = 1
		}, "failed to connect to datastore"},
	}
	for _, tt := range errorCases {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testDatastoreConfig(t)
			tt.modify(cfg)
			ds, err := NewDatastore(cfg, "test-secret")
			if err == nil {
				ds.Close()
				t.Fatal("NewDatastore returned no error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

// TestConnectionDeleteTimeoutBounds guards the ordering the delete
// relies on. Its budget must exceed the pool-wide statement timeout it
// overrides, or the override would shorten it, and it must stay below
// the HTTP server's 300 second WriteTimeout so that the response can
// still be written once the delete finishes.
func TestConnectionDeleteTimeoutBounds(t *testing.T) {
	if ConnectionDeleteTimeout <= DefaultDatastoreStatementTimeout {
		t.Fatalf("ConnectionDeleteTimeout = %v, want more than %v",
			ConnectionDeleteTimeout, DefaultDatastoreStatementTimeout)
	}
	if ConnectionDeleteTimeout >= 300*time.Second {
		t.Fatalf("ConnectionDeleteTimeout = %v, want less than the 300s HTTP WriteTimeout",
			ConnectionDeleteTimeout)
	}
}
