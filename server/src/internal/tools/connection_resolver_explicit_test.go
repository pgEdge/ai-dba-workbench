/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/pkg/crypto"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
)

// resolverTestSchema creates the connections table that
// GetConnectionWithPassword reads, with the columns it scans.
const resolverTestSchema = `
DROP TABLE IF EXISTS connections CASCADE;

CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    host VARCHAR(255) NOT NULL DEFAULT '',
    hostaddr VARCHAR(255),
    port INTEGER NOT NULL DEFAULT 5432,
    database_name VARCHAR(255) NOT NULL DEFAULT '',
    username VARCHAR(255) NOT NULL DEFAULT '',
    password_encrypted TEXT,
    sslmode VARCHAR(32),
    sslcert TEXT,
    sslkey TEXT,
    sslrootcert TEXT,
    owner_username VARCHAR(255),
    owner_token VARCHAR(255),
    is_monitored BOOLEAN NOT NULL DEFAULT TRUE,
    is_shared BOOLEAN NOT NULL DEFAULT FALSE,
    membership_source VARCHAR(16) NOT NULL DEFAULT 'auto',
    cluster_id INTEGER,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

// resolverTestSecret is a fixed 32-byte server secret, so the stored
// password round-trips through the decrypt step the resolver runs.
const resolverTestSecret = "test-server-secret-32-bytes-long!"

// hostileDatabaseName is a real database name carrying the characters
// that, pasted into a URL, would end the path and add libpq parameters
// pointing the connection at another host (192.0.2.1 is TEST-NET-1).
const hostileDatabaseName = "ai_wb_resolver?host=192.0.2.1&sslmode=disable"

// createHostileDatabase and dropHostileDatabase create and drop the
// database named hostileDatabaseName. They are written out in full,
// because CREATE DATABASE cannot run inside a DO block and no SQL is
// assembled at run time; TestHostileDatabaseStatements checks that they
// name the same database.
const (
	createHostileDatabase = `CREATE DATABASE "ai_wb_resolver?host=192.0.2.1&sslmode=disable"`
	dropHostileDatabase   = `DROP DATABASE IF EXISTS "ai_wb_resolver?host=192.0.2.1&sslmode=disable" WITH (FORCE)`
)

// TestHostileDatabaseStatements keeps the literal statements above in
// step with hostileDatabaseName.
func TestHostileDatabaseStatements(t *testing.T) {
	quoted := pgx.Identifier{hostileDatabaseName}.Sanitize()
	for _, stmt := range []string{createHostileDatabase, dropHostileDatabase} {
		if !strings.Contains(stmt, " "+quoted) {
			t.Errorf("%q does not name %s", stmt, quoted)
		}
	}
}

// toolResponseText returns the text of the first content item.
func toolResponseText(resp *mcp.ToolResponse) string {
	if resp == nil || len(resp.Content) == 0 {
		return ""
	}
	return resp.Content[0].Text
}

// TestResolveExplicit_InvalidDatabaseNameFirst checks that an invalid
// override is refused before the RBAC check or any credential lookup:
// the resolver has no datastore or checker, so reaching either would
// panic.
func TestResolveExplicit_InvalidDatabaseNameFirst(t *testing.T) {
	r := NewConnectionResolver(nil, nil, nil)
	tests := []struct {
		name   string
		dbName string
	}{
		{"NUL byte", "postgres\x00"},
		{"newline", "postgres\nhost=192.0.2.1"},
		{"over 63 bytes", strings.Repeat("a", 64)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved, resp := r.Resolve(context.Background(), map[string]any{
				"connection_id": float64(1),
				"database_name": tt.dbName,
			}, nil)
			if resolved != nil || resp == nil || !resp.IsError {
				t.Fatalf("expected an error response, got %v, %v",
					resolved, resp)
			}
			if !strings.Contains(toolResponseText(resp), "invalid database_name") {
				t.Errorf("unexpected error text %q", toolResponseText(resp))
			}
		})
	}
}

// TestResolveExplicit_RBACDenied checks that a denied caller gets the
// generic not-found message and the datastore is never consulted. An API
// token context without a token ID is denied by the checker before it
// touches its store.
func TestResolveExplicit_RBACDenied(t *testing.T) {
	r := NewConnectionResolver(nil, nil, auth.NewRBACChecker(&auth.AuthStore{}))
	ctx := context.WithValue(context.Background(), auth.IsAPITokenContextKey, true)

	resolved, resp := r.Resolve(ctx, map[string]any{
		"connection_id": float64(1),
	}, nil)
	if resolved != nil || resp == nil || !resp.IsError {
		t.Fatalf("expected an error response, got %v, %v", resolved, resp)
	}
	if got := toolResponseText(resp); got != "connection not found or not accessible" {
		t.Errorf("unexpected error text %q", got)
	}
}

// resolverTestEnv is a resolver wired to the TEST_AI_WORKBENCH_SERVER
// instance, with one connection row pointing back at that instance.
type resolverTestEnv struct {
	resolver *ConnectionResolver
	pool     *pgxpool.Pool
	clients  *database.ClientManager
	connID   int
	host     string
	database string
}

// newResolverTestEnv builds a resolverTestEnv, skipping when no test
// database is configured. A nil auth store grants full access, so the
// tests exercise the resolver rather than the RBAC gate.
func newResolverTestEnv(t *testing.T) *resolverTestEnv {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping database test")
	}

	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		t.Fatalf("Could not parse test database connection string: %v", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("Could not connect to test database: %v", err)
	}
	if _, err := pool.Exec(ctx, resolverTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create resolver test schema: %v", err)
	}

	var encrypted *string
	if cfg.ConnConfig.Password != "" {
		value, err := crypto.EncryptPassword(cfg.ConnConfig.Password,
			resolverTestSecret)
		if err != nil {
			pool.Close()
			t.Fatalf("Failed to encrypt the test password: %v", err)
		}
		encrypted = &value
	}

	var connID int
	if err := pool.QueryRow(ctx, `
        INSERT INTO connections
            (name, host, port, database_name, username, password_encrypted,
             sslmode)
        VALUES ('resolver-test', $1, $2, $3, $4, $5, 'disable')
        RETURNING id
    `, cfg.ConnConfig.Host, int(cfg.ConnConfig.Port), cfg.ConnConfig.Database,
		cfg.ConnConfig.User, encrypted).Scan(&connID); err != nil {
		pool.Close()
		t.Fatalf("Failed to insert connection: %v", err)
	}

	cm := database.NewClientManager(nil)
	t.Cleanup(func() {
		_ = cm.CloseAll()
		_, _ = pool.Exec(context.Background(),
			"DROP TABLE IF EXISTS connections CASCADE")
		pool.Close()
	})

	return &resolverTestEnv{
		resolver: NewConnectionResolver(cm,
			database.NewTestDatastoreWithSecret(pool, resolverTestSecret),
			auth.NewRBACChecker(nil)),
		pool:     pool,
		clients:  cm,
		connID:   connID,
		host:     cfg.ConnConfig.Host,
		database: cfg.ConnConfig.Database,
	}
}

// currentDatabase returns the database the resolved pool is connected
// to, which is what an override must never be able to change beyond the
// database name itself.
func currentDatabase(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var name string
	if err := pool.QueryRow(context.Background(),
		"SELECT current_database()").Scan(&name); err != nil {
		t.Fatalf("current_database() failed: %v", err)
	}
	return name
}

// TestResolveExplicit_Database covers the credential lookup, the
// connection and the session key against a live database.
func TestResolveExplicit_Database(t *testing.T) {
	env := newResolverTestEnv(t)
	ctx := context.Background()

	t.Run("unknown connection", func(t *testing.T) {
		resolved, resp := env.resolver.Resolve(ctx, map[string]any{
			"connection_id": float64(env.connID + 1000),
		}, nil)
		if resolved != nil || resp == nil || !resp.IsError {
			t.Fatalf("expected an error response, got %v, %v", resolved, resp)
		}
		if got := toolResponseText(resp); got != "connection not found or not accessible" {
			t.Errorf("unexpected error text %q", got)
		}
	})

	t.Run("stored database", func(t *testing.T) {
		resolved, resp := env.resolver.Resolve(ctx, map[string]any{
			"connection_id": float64(env.connID),
		}, nil)
		if resp != nil {
			t.Fatalf("unexpected error: %s", toolResponseText(resp))
		}
		if resolved.ConnID != env.connID || resolved.DBName != env.database {
			t.Errorf("resolved %d/%q, want %d/%q", resolved.ConnID,
				resolved.DBName, env.connID, env.database)
		}
		if got := currentDatabase(t, resolved.Pool); got != env.database {
			t.Errorf("connected to %q, want %q", got, env.database)
		}
	})

	t.Run("cached client without a pool", func(t *testing.T) {
		args := map[string]any{"connection_id": float64(env.connID)}
		resolved, resp := env.resolver.Resolve(ctx, args, nil)
		if resp != nil {
			t.Fatalf("unexpected error: %s", toolResponseText(resp))
		}

		// Replace the cached session client with one that was never
		// connected, under the key GetClientForSession builds for a
		// caller with no token hash and no override, so the resolver
		// finds neither metadata nor a pool for the connection string.
		key := fmt.Sprintf(":conn:%d", env.connID)
		if err := env.clients.SetClient(key,
			database.NewClientWithConnectionString(resolved.ConnStr, nil)); err != nil {
			t.Fatalf("SetClient failed: %v", err)
		}

		resolved, resp = env.resolver.Resolve(ctx, args, nil)
		if resolved != nil || resp == nil || !resp.IsError {
			t.Fatalf("expected an error response, got %v, %v", resolved, resp)
		}
		if got := toolResponseText(resp); got != "Failed to establish database connection" {
			t.Errorf("unexpected error text %q", got)
		}
		if err := env.clients.RemoveClient(key); err != nil {
			t.Fatalf("RemoveClient failed: %v", err)
		}
	})

	t.Run("missing override database", func(t *testing.T) {
		resolved, resp := env.resolver.Resolve(ctx, map[string]any{
			"connection_id": float64(env.connID),
			"database_name": "ai_workbench_resolver_no_such_db",
		}, nil)
		if resolved != nil || resp == nil || !resp.IsError {
			t.Fatalf("expected an error response, got %v, %v", resolved, resp)
		}
		if got := toolResponseText(resp); got != "Failed to establish database connection" {
			t.Errorf("unexpected error text %q", got)
		}
	})

	t.Run("hostile override stays on the stored host", func(t *testing.T) {
		if _, err := env.pool.Exec(ctx, createHostileDatabase); err != nil {
			t.Fatalf("CREATE DATABASE failed: %v", err)
		}
		t.Cleanup(func() {
			_, _ = env.pool.Exec(context.Background(), dropHostileDatabase)
		})

		resolved, resp := env.resolver.Resolve(ctx, map[string]any{
			"connection_id": float64(env.connID),
			"database_name": hostileDatabaseName,
		}, nil)
		if resp != nil {
			t.Fatalf("unexpected error: %s", toolResponseText(resp))
		}
		if resolved.DBName != hostileDatabaseName {
			t.Errorf("DBName = %q, want %q", resolved.DBName,
				hostileDatabaseName)
		}
		if got := currentDatabase(t, resolved.Pool); got != hostileDatabaseName {
			t.Errorf("connected to %q, want %q", got, hostileDatabaseName)
		}
		parsed, err := pgconn.ParseConfig(resolved.ConnStr)
		if err != nil {
			t.Fatalf("ParseConfig(%q) failed: %v", resolved.ConnStr, err)
		}
		if parsed.Host != env.host || parsed.Database != hostileDatabaseName {
			t.Errorf("connection string %q parses to host %q, database %q",
				resolved.ConnStr, parsed.Host, parsed.Database)
		}
		if _, ok := parsed.RuntimeParams["host"]; ok || len(parsed.Fallbacks) > 0 {
			t.Errorf("connection string %q gained a host parameter",
				resolved.ConnStr)
		}
	})
}
