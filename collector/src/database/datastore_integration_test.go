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
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// integrationConfig is a fully populated Config implementation used by the
// integration tests in this file. The fields mirror collector main Config
// but live in this package so we don't introduce a test-only dependency on
// the main package.
type integrationConfig struct {
	host           string
	hostAddr       string
	database       string
	username       string
	password       string
	port           int
	sslMode        string
	sslCert        string
	sslKey         string
	sslRootCert    string
	maxConnections int
	maxIdleSeconds int
	validateErr    error
}

func (c *integrationConfig) Validate() error                     { return c.validateErr }
func (c *integrationConfig) GetPgHost() string                   { return c.host }
func (c *integrationConfig) GetPgHostAddr() string               { return c.hostAddr }
func (c *integrationConfig) GetPgDatabase() string               { return c.database }
func (c *integrationConfig) GetPgUsername() string               { return c.username }
func (c *integrationConfig) GetPgPassword() string               { return c.password }
func (c *integrationConfig) GetPgPort() int                      { return c.port }
func (c *integrationConfig) GetPgSSLMode() string                { return c.sslMode }
func (c *integrationConfig) GetPgSSLCert() string                { return c.sslCert }
func (c *integrationConfig) GetPgSSLKey() string                 { return c.sslKey }
func (c *integrationConfig) GetPgSSLRootCert() string            { return c.sslRootCert }
func (c *integrationConfig) GetDatastorePoolMaxConnections() int { return c.maxConnections }
func (c *integrationConfig) GetDatastorePoolMaxIdleSeconds() int { return c.maxIdleSeconds }

// parseTestServerURL extracts host/port/user/password from the
// TEST_AI_WORKBENCH_SERVER URL or the connstring fallbacks. The returned
// values are suitable for filling an integrationConfig.
func parseTestServerURL(t *testing.T) *integrationConfig {
	t.Helper()

	// Default values match the schema_test.go fallback.
	cfg := &integrationConfig{
		host:           "localhost",
		port:           5432,
		username:       "postgres",
		sslMode:        "disable",
		maxConnections: 5,
		maxIdleSeconds: 60,
	}

	// We accept the same URL formats getAdminConnectionString accepts.
	url := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if url == "" {
		url = os.Getenv("TEST_DB_CONN")
	}

	if url == "" {
		return cfg
	}

	if strings.HasPrefix(url, "postgres://") || strings.HasPrefix(url, "postgresql://") {
		// Strip scheme and any query params.
		stripped := strings.TrimPrefix(url, "postgres://")
		stripped = strings.TrimPrefix(stripped, "postgresql://")
		if idx := strings.Index(stripped, "?"); idx != -1 {
			stripped = stripped[:idx]
		}

		// Split userinfo@hostport/dbname.
		var userinfo, hostpart string
		if at := strings.Index(stripped, "@"); at != -1 {
			userinfo = stripped[:at]
			hostpart = stripped[at+1:]
		} else {
			hostpart = stripped
		}

		// userinfo can be "user" or "user:pass".
		if userinfo != "" {
			if c := strings.Index(userinfo, ":"); c != -1 {
				cfg.username = userinfo[:c]
				cfg.password = userinfo[c+1:]
			} else {
				cfg.username = userinfo
			}
		}

		// hostpart can include /dbname; strip it.
		if slash := strings.Index(hostpart, "/"); slash != -1 {
			hostpart = hostpart[:slash]
		}
		if c := strings.LastIndex(hostpart, ":"); c != -1 {
			cfg.host = hostpart[:c]
			fmt.Sscanf(hostpart[c+1:], "%d", &cfg.port)
		} else {
			cfg.host = hostpart
		}
		return cfg
	}

	// key=value form.
	for _, part := range strings.Fields(url) {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "host":
			cfg.host = kv[1]
		case "port":
			fmt.Sscanf(kv[1], "%d", &cfg.port)
		case "user":
			cfg.username = kv[1]
		case "password":
			cfg.password = kv[1]
		case "sslmode":
			cfg.sslMode = kv[1]
		}
	}
	return cfg
}

// newDatastoreForTest builds a Datastore wired to the test database
// created by TestMain. It also runs migrations so the connection table
// exists.
func newDatastoreForTest(t *testing.T) *Datastore {
	t.Helper()

	if testDBName == "" {
		t.Skip("test database not provisioned")
	}

	cfg := parseTestServerURL(t)
	cfg.database = testDBName
	ds, err := NewDatastore(cfg)
	if err != nil {
		t.Fatalf("NewDatastore() error = %v", err)
	}
	t.Cleanup(ds.Close)

	return ds
}

func TestNewDatastore_ValidationError(t *testing.T) {
	cfg := &integrationConfig{
		validateErr: errors.New("invalid"),
	}
	if _, err := NewDatastore(cfg); err == nil {
		t.Fatal("expected validation error, got nil")
	} else if !strings.Contains(err.Error(), "invalid configuration") {
		t.Errorf("error = %v, want it to contain 'invalid configuration'", err)
	}
}

func TestNewDatastore_BadConnect(t *testing.T) {
	// Use a port unlikely to be open to force connect/ping failure quickly.
	cfg := &integrationConfig{
		host:           "127.0.0.1",
		database:       "no_such_db",
		username:       "postgres",
		port:           1, // reserved
		sslMode:        "disable",
		maxConnections: 1,
		maxIdleSeconds: 1,
	}
	if _, err := NewDatastore(cfg); err == nil {
		t.Fatal("expected connect error, got nil")
	}
}

func TestConnect_BadConnString(t *testing.T) {
	// Force ParseConfig to fail by giving a host that yields an invalid
	// port; we must use a path that fails very fast.
	ds := &Datastore{
		config: &integrationConfig{
			// "host" containing invalid characters trips ParseConfig.
			host: "::::not-a-valid-host::::",
		},
	}
	// Set max conns above MaxInt32 to also exercise the bounds clamp.
	ds.config.(*integrationConfig).maxConnections = int(int64(1<<31) + 100)
	ds.config.(*integrationConfig).maxIdleSeconds = 1
	ds.config.(*integrationConfig).port = -1
	ds.config.(*integrationConfig).database = "x"
	ds.config.(*integrationConfig).username = "x"
	ds.config.(*integrationConfig).sslMode = "disable"

	if err := ds.connect(); err == nil {
		t.Fatal("expected connect failure")
	}
}

func TestNewDatastore_HappyPath(t *testing.T) {
	ds := newDatastoreForTest(t)

	if ds.pool == nil {
		t.Fatal("pool should be initialized")
	}

	// Running again should be idempotent (Migrate no-ops).
	if err := ds.initializeSchema(); err != nil {
		t.Errorf("initializeSchema() second call = %v", err)
	}
}

func TestGetConnection_AndReturn(t *testing.T) {
	ds := newDatastoreForTest(t)

	conn, err := ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection() error = %v", err)
	}
	if conn == nil {
		t.Fatal("GetConnection() returned nil conn")
	}

	// Using a no-op nil release path.
	ds.ReturnConnection(nil)

	// Real release path.
	ds.ReturnConnection(conn)
}

func TestGetConnectionWithContext(t *testing.T) {
	ds := newDatastoreForTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := ds.GetConnectionWithContext(ctx)
	if err != nil {
		t.Fatalf("GetConnectionWithContext() error = %v", err)
	}
	defer ds.ReturnConnection(conn)

	// Cancelled context should yield an error.
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	if _, err := ds.GetConnectionWithContext(ctx2); err == nil {
		t.Error("expected error from canceled context")
	}
}

func TestClose_WithNilPool(t *testing.T) {
	ds := &Datastore{}
	// Must not panic.
	ds.Close()
}

func TestGetMonitoredConnections_Empty(t *testing.T) {
	ds := newDatastoreForTest(t)

	// Wipe any leftover rows so the test is deterministic.
	conn, err := ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection() error = %v", err)
	}
	defer ds.ReturnConnection(conn)
	if _, err := conn.Exec(context.Background(), "DELETE FROM connections"); err != nil {
		t.Fatalf("cleanup DELETE error = %v", err)
	}

	got, err := ds.GetMonitoredConnections()
	if err != nil {
		t.Fatalf("GetMonitoredConnections() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 monitored connections, got %d", len(got))
	}
}

func TestGetMonitoredConnections_WithRow(t *testing.T) {
	ds := newDatastoreForTest(t)

	conn, err := ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection() error = %v", err)
	}
	defer ds.ReturnConnection(conn)

	if _, err := conn.Exec(context.Background(), "DELETE FROM connections"); err != nil {
		t.Fatalf("cleanup error = %v", err)
	}

	// Insert two rows: one monitored, one not. Only the monitored row
	// should be returned.
	_, err = conn.Exec(context.Background(), `
		INSERT INTO connections
			(name, host, hostaddr, port, database_name, username,
			 password_encrypted, sslmode, sslcert, sslkey, sslrootcert,
			 owner_username, is_monitored, connection_error)
		VALUES
			('monitored-1', 'host1', '10.0.0.1', 5432, 'db1', 'u1',
			 'encrypted-pw', 'require', 'cert', 'key', 'rootcert',
			 'owner1', TRUE, 'an error'),
			('not-monitored', 'host2', NULL, 5433, 'db2', 'u2',
			 NULL, NULL, NULL, NULL, NULL,
			 'owner2', FALSE, NULL);
	`)
	if err != nil {
		t.Fatalf("seed error = %v", err)
	}

	got, err := ds.GetMonitoredConnections()
	if err != nil {
		t.Fatalf("GetMonitoredConnections() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}

	row := got[0]
	if row.Name != "monitored-1" {
		t.Errorf("Name = %q, want monitored-1", row.Name)
	}
	if !row.HostAddr.Valid || row.HostAddr.String != "10.0.0.1" {
		t.Errorf("HostAddr = %+v, want 10.0.0.1", row.HostAddr)
	}
	if row.ConnectionError == nil || *row.ConnectionError != "an error" {
		t.Errorf("ConnectionError = %v, want 'an error'", row.ConnectionError)
	}
}

func TestGetMonitoredConnectionByID(t *testing.T) {
	ds := newDatastoreForTest(t)

	conn, err := ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection() error = %v", err)
	}
	defer ds.ReturnConnection(conn)

	if _, err := conn.Exec(context.Background(), "DELETE FROM connections"); err != nil {
		t.Fatalf("cleanup error = %v", err)
	}

	var id int
	err = conn.QueryRow(context.Background(), `
		INSERT INTO connections
			(name, host, port, database_name, username, owner_username, is_monitored)
		VALUES
			('mon-by-id', 'localhost', 5432, 'db', 'u', 'o', TRUE)
		RETURNING id
	`).Scan(&id)
	if err != nil {
		t.Fatalf("seed error = %v", err)
	}

	got, err := ds.GetMonitoredConnectionByID(id)
	if err != nil {
		t.Fatalf("GetMonitoredConnectionByID() error = %v", err)
	}
	if got.ID != id || got.Name != "mon-by-id" {
		t.Errorf("got = %+v, want id=%d name=mon-by-id", got, id)
	}

	// Missing row -> error.
	if _, err := ds.GetMonitoredConnectionByID(999_999); err == nil {
		t.Error("expected error for missing connection ID")
	}

	// Same ID but is_monitored = FALSE: should not be returned either.
	if _, err := conn.Exec(context.Background(),
		"UPDATE connections SET is_monitored = FALSE WHERE id = $1", id); err != nil {
		t.Fatalf("flip is_monitored failed: %v", err)
	}
	if _, err := ds.GetMonitoredConnectionByID(id); err == nil {
		t.Error("expected error when row is not monitored")
	}
}

func TestSetConnectionError(t *testing.T) {
	ds := newDatastoreForTest(t)

	conn, err := ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection() error = %v", err)
	}
	defer ds.ReturnConnection(conn)

	if _, err := conn.Exec(context.Background(), "DELETE FROM connections"); err != nil {
		t.Fatalf("cleanup error = %v", err)
	}

	var id int
	err = conn.QueryRow(context.Background(), `
		INSERT INTO connections
			(name, host, port, database_name, username, owner_username, is_monitored)
		VALUES
			('set-err', 'h', 5432, 'd', 'u', 'o', TRUE)
		RETURNING id
	`).Scan(&id)
	if err != nil {
		t.Fatalf("seed error = %v", err)
	}

	ctx := context.Background()
	msg := "kapow"
	if err := SetConnectionError(ctx, conn, id, &msg); err != nil {
		t.Fatalf("SetConnectionError(set) error = %v", err)
	}

	var got sql.NullString
	if err := conn.QueryRow(ctx,
		"SELECT connection_error FROM connections WHERE id=$1", id).Scan(&got); err != nil {
		t.Fatalf("readback error = %v", err)
	}
	if !got.Valid || got.String != "kapow" {
		t.Errorf("got %v, want 'kapow'", got)
	}

	// Clear it.
	if err := SetConnectionError(ctx, conn, id, nil); err != nil {
		t.Fatalf("SetConnectionError(clear) error = %v", err)
	}
	if err := conn.QueryRow(ctx,
		"SELECT connection_error FROM connections WHERE id=$1", id).Scan(&got); err != nil {
		t.Fatalf("readback error = %v", err)
	}
	if got.Valid {
		t.Errorf("expected NULL connection_error, got %q", got.String)
	}
}

func TestNewDatastore_BadSchemaInit(t *testing.T) {
	// Drop ourselves into the test DB, then point Datastore at a host
	// that connects fine but where Migrate will fail because we run
	// inside a TX that we forcibly poison. We simulate this by using
	// a config pointing to a non-existent database name to force the
	// underlying ping to fail; we already cover the Validate path
	// above, and an error from connect() bypasses initializeSchema.
	cfg := parseTestServerURL(t)
	cfg.database = "definitely_does_not_exist_42"
	cfg.maxConnections = 1
	cfg.maxIdleSeconds = 1
	if _, err := NewDatastore(cfg); err == nil {
		t.Fatal("expected error connecting to nonexistent database")
	}
}

// TestConnect_ClampsMaxConns exercises the math.MaxInt32 clamp branch in
// connect(). We pass a maxConnections value larger than int32 can hold
// and verify the pool is created successfully (the value gets clamped).
func TestConnect_ClampsMaxConns(t *testing.T) {
	requireSchema(t)

	cfg := parseTestServerURL(t)
	cfg.database = testDBName
	cfg.maxConnections = int(int64(1<<31) + 100)
	cfg.maxIdleSeconds = 1

	ds := &Datastore{config: cfg}
	if err := ds.connect(); err != nil {
		t.Fatalf("connect with huge maxConnections: %v", err)
	}
	defer ds.Close()
}

// TestGetMonitoredConnections_AfterClose exercises the failure paths in
// GetMonitoredConnections / GetMonitoredConnectionByID by closing the
// pool first. Acquire returns an error which surfaces as a wrapped
// "failed to get connection" message.
//
// This test sets a custom small maxConnections so the underlying pool
// exhausts quickly when closed, but the immediate Close path returns
// pgxpool's "closed pool" error with no timeout wait.
func TestGetMonitoredConnections_AfterClose(t *testing.T) {
	ds := newDatastoreForTest(t)
	ds.pool.Close()

	if _, err := ds.GetMonitoredConnections(); err == nil {
		t.Error("expected error on GetMonitoredConnections after pool Close")
	}
	if _, err := ds.GetMonitoredConnectionByID(1); err == nil {
		t.Error("expected error on GetMonitoredConnectionByID after pool Close")
	}
}

// TestInitializeSchema_AfterClose exercises the error path inside
// initializeSchema where GetConnection itself fails.
func TestInitializeSchema_AfterClose(t *testing.T) {
	ds := newDatastoreForTest(t)
	ds.pool.Close()

	if err := ds.initializeSchema(); err == nil {
		t.Error("expected error from initializeSchema on closed pool")
	}
}

// TestGetMonitoredConnections_BadQuery causes the rows.Next() Scan path
// to fail by dropping a column the query relies on. The query itself
// succeeds (the SELECT names exist in the schema after migration), so to
// trigger Scan failure we use a separate poisoned table approach: drop
// the schema entirely so the query fails on the connections table.
func TestGetMonitoredConnections_QueryFailure(t *testing.T) {
	ds := newDatastoreForTest(t)

	conn, err := ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	// Drop the connections table to force the SELECT to fail.
	if _, err := conn.Exec(context.Background(),
		"DROP TABLE IF EXISTS connections CASCADE"); err != nil {
		t.Fatalf("DROP error: %v", err)
	}
	ds.ReturnConnection(conn)

	if _, err := ds.GetMonitoredConnections(); err == nil {
		t.Error("expected query failure when connections table is gone")
	}

	// Reset for downstream tests.
	conn2, err := ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection 2: %v", err)
	}
	defer ds.ReturnConnection(conn2)
	if err := NewSchemaManager().Migrate(conn2); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
}
