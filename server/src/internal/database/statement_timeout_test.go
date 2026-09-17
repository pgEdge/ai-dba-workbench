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
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/config"
)

// TestResolveStatementTimeout covers the translation from the
// configured statement_timeout string to the startup runtime parameter
// value, which is always a bare millisecond count.
func TestResolveStatementTimeout(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    string
		wantErr string
	}{
		{
			name:  "empty selects the default",
			value: "",
			want:  "30000",
		},
		{
			name:  "seconds are converted to milliseconds",
			value: "45s",
			want:  "45000",
		},
		{
			name:  "sub-second durations survive the conversion",
			value: "1500ms",
			want:  "1500",
		},
		{
			name:  "compound durations are accepted",
			value: "1m30s",
			want:  "90000",
		},
		{
			name:  "zero disables the timeout",
			value: "0",
			want:  "0",
		},
		{
			name:    "an unparseable duration is rejected",
			value:   "30 seconds",
			wantErr: "invalid statement_timeout",
		},
		{
			name:    "a bare number is rejected rather than guessed at",
			value:   "30",
			wantErr: "invalid statement_timeout",
		},
		{
			name:    "a negative duration is rejected",
			value:   "-5s",
			wantErr: "is negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveStatementTimeout(tt.value)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("resolveStatementTimeout(%q) = %q, want error containing %q",
						tt.value, got, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("resolveStatementTimeout(%q) error = %v, want it to contain %q",
						tt.value, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveStatementTimeout(%q) returned unexpected error: %v", tt.value, err)
			}
			if got != tt.want {
				t.Fatalf("resolveStatementTimeout(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

// TestDefaultDatastoreStatementTimeoutMatchesHandlerBudget pins the
// default to the longest context deadline any handler grants a
// datastore query, so that raising one without the other is a visible
// change rather than a silent one.
func TestDefaultDatastoreStatementTimeoutMatchesHandlerBudget(t *testing.T) {
	if DefaultDatastoreStatementTimeout != 30*time.Second {
		t.Fatalf("DefaultDatastoreStatementTimeout = %v, want 30s to match the "+
			"performance summary handlers' context deadline", DefaultDatastoreStatementTimeout)
	}
}

// TestNewDatastoreRejectsInvalidStatementTimeout checks that a bad
// value fails before any connection is attempted, so the operator sees
// the configuration key rather than a connection error.
func TestNewDatastoreRejectsInvalidStatementTimeout(t *testing.T) {
	ds, err := NewDatastore(&config.DatabaseConfig{
		Host:             "127.0.0.1",
		Port:             5432,
		Database:         "postgres",
		User:             "postgres",
		StatementTimeout: "never",
	}, "test-secret")
	if err == nil {
		ds.Close()
		t.Fatal("NewDatastore accepted an invalid statement_timeout")
	}
	if !strings.Contains(err.Error(), "invalid statement_timeout") {
		t.Fatalf("NewDatastore error = %v, want it to mention the statement_timeout key", err)
	}
}

// testDatastoreConfig builds a DatabaseConfig pointing at the local
// integration test database named by TEST_AI_WORKBENCH_SERVER.
func testDatastoreConfig(t *testing.T) *config.DatabaseConfig {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping statement timeout integration test")
	}

	parsed, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		t.Skipf("Could not parse test database connection string: %v", err)
	}

	return &config.DatabaseConfig{
		Host:     parsed.ConnConfig.Host,
		Port:     int(parsed.ConnConfig.Port),
		Database: parsed.ConnConfig.Database,
		User:     parsed.ConnConfig.User,
		Password: parsed.ConnConfig.Password,
	}
}

// showStatementTimeout reads the setting as the server sees it on a
// connection handed out by the pool, which is the only assertion that
// proves the parameter actually reached Postgres.
func showStatementTimeout(t *testing.T, ds *Datastore) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var value string
	if err := ds.GetPool().QueryRow(ctx, "SHOW statement_timeout").Scan(&value); err != nil {
		t.Fatalf("SHOW statement_timeout failed: %v", err)
	}
	return value
}

// TestDatastorePoolAppliesDefaultStatementTimeout connects a real pool
// and asks Postgres what statement_timeout it is running with.
func TestDatastorePoolAppliesDefaultStatementTimeout(t *testing.T) {
	cfg := testDatastoreConfig(t)

	ds, err := NewDatastore(cfg, "test-secret")
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	defer ds.Close()

	if got := showStatementTimeout(t, ds); got != "30s" {
		t.Fatalf("statement_timeout = %q, want \"30s\"", got)
	}
}

// TestDatastorePoolAppliesConfiguredStatementTimeout checks that an
// operator-supplied value wins over the default, and that it reaches
// every connection the pool hands out rather than only the first.
func TestDatastorePoolAppliesConfiguredStatementTimeout(t *testing.T) {
	cfg := testDatastoreConfig(t)
	cfg.StatementTimeout = "7s"
	cfg.PoolMaxConns = 2
	cfg.PoolMinConns = 2

	ds, err := NewDatastore(cfg, "test-secret")
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	defer ds.Close()

	for i := 0; i < 4; i++ {
		if got := showStatementTimeout(t, ds); got != "7s" {
			t.Fatalf("statement_timeout on query %d = %q, want \"7s\"", i, got)
		}
	}
}

// TestDatastorePoolStatementTimeoutCancelsRunawayQuery is the test that
// matters: a query that outlives the timeout is killed by the server,
// not merely abandoned by the client, so the pool slot comes back.
func TestDatastorePoolStatementTimeoutCancelsRunawayQuery(t *testing.T) {
	cfg := testDatastoreConfig(t)
	cfg.StatementTimeout = "250ms"
	cfg.PoolMaxConns = 1

	ds, err := NewDatastore(cfg, "test-secret")
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	defer ds.Close()

	// The context deadline is deliberately far longer than the
	// statement timeout, so only a server-side cancellation can end
	// this query.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	_, err = ds.GetPool().Exec(ctx, "SELECT pg_sleep(10)")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("pg_sleep(10) completed despite a 250ms statement_timeout")
	}
	if !strings.Contains(err.Error(), "canceling statement due to statement timeout") {
		t.Fatalf("error = %v, want a statement timeout cancellation", err)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("query ran for %v, want it killed well before pg_sleep(10) finished", elapsed)
	}

	// The slot must be usable again straight away.
	if got := showStatementTimeout(t, ds); got != "250ms" {
		t.Fatalf("statement_timeout after the cancellation = %q, want \"250ms\"", got)
	}
}

// TestDatastorePoolStatementTimeoutCanBeDisabled documents the escape
// hatch: an explicit zero leaves Postgres with no limit.
func TestDatastorePoolStatementTimeoutCanBeDisabled(t *testing.T) {
	cfg := testDatastoreConfig(t)
	cfg.StatementTimeout = "0"

	ds, err := NewDatastore(cfg, "test-secret")
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	defer ds.Close()

	if got := showStatementTimeout(t, ds); got != "0" {
		t.Fatalf("statement_timeout = %q, want \"0\"", got)
	}
}
