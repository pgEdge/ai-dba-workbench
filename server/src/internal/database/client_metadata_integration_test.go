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
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/server/internal/config"
)

// metadataTestSchemaName is the schema metadataTestSchema creates. The
// statements below spell it literally, so that no SQL is assembled at
// run time; the tests using it do not run in parallel.
const metadataTestSchemaName = "ai_wb_client_meta_fixture"

// metadataTestSchemaSQL builds the fixture, first dropping any copy a
// crashed earlier run left behind.
const metadataTestSchemaSQL = `
DROP SCHEMA IF EXISTS ai_wb_client_meta_fixture CASCADE;
CREATE SCHEMA ai_wb_client_meta_fixture;
CREATE TABLE ai_wb_client_meta_fixture.widgets (id integer PRIMARY KEY, name text NOT NULL);
COMMENT ON TABLE ai_wb_client_meta_fixture.widgets IS 'Test widgets';
COMMENT ON COLUMN ai_wb_client_meta_fixture.widgets.name IS 'Widget name';
CREATE VIEW ai_wb_client_meta_fixture.widget_names AS
    SELECT name FROM ai_wb_client_meta_fixture.widgets;
CREATE MATERIALIZED VIEW ai_wb_client_meta_fixture.widget_ids AS
    SELECT id FROM ai_wb_client_meta_fixture.widgets;
`

// metadataTestSchemaDropSQL removes the fixture.
const metadataTestSchemaDropSQL = `DROP SCHEMA IF EXISTS ai_wb_client_meta_fixture CASCADE`

// metadataTestSchema creates a schema holding a commented table, a plain view
// and a MATERIALIZED VIEW on the TEST_AI_WORKBENCH_SERVER instance, so
// that a client's metadata load has every relation kind to read. It
// returns the connection string and the schema name, and drops the
// schema when the test ends.
func metadataTestSchema(t *testing.T) (string, string) {
	t.Helper()
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping client metadata integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(ctx, metadataTestSchemaSQL); err != nil {
		t.Fatalf("create metadata fixture: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), metadataTestSchemaDropSQL); err != nil {
			t.Errorf("drop schema %s: %v", metadataTestSchemaName, err)
		}
	})
	return connStr, metadataTestSchemaName
}

// testDatabaseConfig builds a DatabaseConfig equivalent to connStr.
func testDatabaseConfig(t *testing.T, connStr string) *config.DatabaseConfig {
	t.Helper()
	u, err := url.Parse(connStr)
	if err != nil {
		t.Fatalf("parse %s: %v", SanitizeConnStr(connStr), err)
	}
	port := 5432
	if p := u.Port(); p != "" {
		if port, err = strconv.Atoi(p); err != nil {
			t.Fatalf("port: %v", err)
		}
	}
	cfg := &config.DatabaseConfig{
		Host:     u.Hostname(),
		Port:     port,
		Database: strings.TrimPrefix(u.Path, "/"),
		User:     u.User.Username(),
		SSLMode:  "disable",
	}
	if pw, ok := u.User.Password(); ok {
		cfg.Password = pw
	}
	return cfg
}

// assertMetadata checks that client loaded every relation in schema.
func assertMetadata(t *testing.T, client *Client, schema string) {
	t.Helper()
	if !client.IsMetadataLoaded() {
		t.Fatal("metadata not loaded")
	}
	md := client.GetMetadata()
	widgets, ok := md[schema+".widgets"]
	if !ok {
		t.Fatalf("metadata has no %s.widgets", schema)
	}
	if widgets.TableType != "TABLE" || widgets.Description != "Test widgets" || len(widgets.Columns) != 2 {
		t.Errorf("widgets = %+v", widgets)
	}
	if v := md[schema+".widget_names"]; v.TableType != "VIEW" {
		t.Errorf("widget_names type = %q, want VIEW", v.TableType)
	}
	if v := md[schema+".widget_ids"]; v.TableType != "MATERIALIZED VIEW" {
		t.Errorf("widget_ids type = %q, want MATERIALIZED VIEW", v.TableType)
	}
}

// TestClientManager_MonitoredClientLoadsMetadata checks the monitored
// client path end to end: the dial guard permits a loopback target,
// metadata loads, an identical request reuses the client and a changed
// connection string replaces it.
func TestClientManager_MonitoredClientLoadsMetadata(t *testing.T) {
	connStr, schema := metadataTestSchema(t)

	prev := GetLogLevel()
	SetLogLevel(LogLevelTrace)
	defer SetLogLevel(prev)

	cm := NewClientManager(nil)
	t.Cleanup(func() { _ = cm.CloseAll() })

	dbName := "x"
	session := &SessionInfo{TokenHash: "tok", ConnectionID: 7, DatabaseName: &dbName}
	client, err := cm.GetClientForSession(session, connStr)
	if err != nil {
		t.Fatalf("GetClientForSession: %v", err)
	}
	assertMetadata(t, client, schema)

	again, err := cm.GetClientForSession(session, connStr)
	if err != nil || again != client {
		t.Fatalf("second request: client %p, %v; want the cached %p", again, err, client)
	}

	changed := connStr + "?connect_timeout=5"
	if strings.Contains(connStr, "?") {
		changed = connStr + "&connect_timeout=5"
	}
	replaced, err := cm.GetClientForSession(session, changed)
	if err != nil {
		t.Fatalf("changed connection string: %v", err)
	}
	if replaced == client || replaced.GetDefaultConnection() != changed {
		t.Error("a changed connection string did not replace the client")
	}

	if _, err := cm.GetClientForSession(nil, connStr); err == nil {
		t.Error("nil session: want an error")
	}
	if _, err := cm.GetClientForSession(session, ""); err == nil {
		t.Error("empty connection string: want an error")
	}
	if _, err := cm.GetOrCreateClientWithConnString("", connStr); err == nil {
		t.Error("empty key: want an error")
	}
}

// TestClientManager_DatastoreClients checks the token and keyed clients
// built from the configured datastore, which are not dial-guarded.
func TestClientManager_DatastoreClients(t *testing.T) {
	connStr, schema := metadataTestSchema(t)
	cm := NewClientManager(testDatabaseConfig(t, connStr))
	t.Cleanup(func() { _ = cm.CloseAll() })

	client, err := cm.GetClient("token-hash")
	if err != nil {
		t.Fatalf("GetClient: %v", err)
	}
	assertMetadata(t, client, schema)
	if again, err := cm.GetClient("token-hash"); err != nil || again != client {
		t.Errorf("GetClient reuse: %p, %v; want %p", again, err, client)
	}
	if client.dialGuard != nil {
		t.Error("a datastore client must not be dial-guarded")
	}

	keyed, err := cm.GetOrCreateClient("key", true)
	if err != nil {
		t.Fatalf("GetOrCreateClient: %v", err)
	}
	assertMetadata(t, keyed, schema)
	if again, err := cm.GetOrCreateClient("key", false); err != nil || again != keyed {
		t.Errorf("GetOrCreateClient reuse: %p, %v; want %p", again, err, keyed)
	}

	for name, call := range map[string]func() error{
		"GetClient empty hash":           func() error { _, err := cm.GetClient(""); return err },
		"GetOrCreateClient empty key":    func() error { _, err := cm.GetOrCreateClient("", true); return err },
		"GetOrCreateClient no autoconn":  func() error { _, err := cm.GetOrCreateClient("other", false); return err },
		"GetClient no config":            func() error { _, err := NewClientManager(nil).GetClient("h"); return err },
		"GetOrCreateClient no config":    func() error { _, err := NewClientManager(nil).GetOrCreateClient("k", true); return err },
		"GetClient unreachable database": func() error { return unreachableClientErr(t, connStr) },
	} {
		if err := call(); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

// unreachableClientErr asks a manager configured for a database that
// does not exist for a client, which must fail to connect.
func unreachableClientErr(t *testing.T, connStr string) error {
	t.Helper()
	cfg := testDatabaseConfig(t, connStr)
	cfg.Database = "no_such_database_for_client_test"
	cm := NewClientManager(cfg)
	t.Cleanup(func() { _ = cm.CloseAll() })
	_, err := cm.GetClient("h")
	if err != nil {
		_, err2 := cm.GetOrCreateClient("k", true)
		if err2 == nil {
			t.Error("GetOrCreateClient for a missing database: want an error")
		}
	}
	return err
}

// TestClient_ConnectFromEnvironment checks that a client with no
// configuration connects with PGEDGE_POSTGRES_CONNECTION_STRING.
func TestClient_ConnectFromEnvironment(t *testing.T) {
	connStr, schema := metadataTestSchema(t)
	t.Setenv("PGEDGE_POSTGRES_CONNECTION_STRING", connStr)

	c := NewClient(nil)
	defer c.Close()
	if err := c.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if c.GetDefaultConnection() != connStr {
		t.Errorf("default connection = %s, want the environment's", SanitizeConnStr(c.GetDefaultConnection()))
	}
	if err := c.LoadMetadata(); err != nil {
		t.Fatalf("LoadMetadata: %v", err)
	}
	assertMetadata(t, c, schema)
}

// TestLogger_LevelMethods checks that each level method writes only at
// or above its level.
func TestLogger_LevelMethods(t *testing.T) {
	var buf strings.Builder
	l := &Logger{level: LogLevelDebug}
	l.logger = log.New(&buf, "", 0)

	l.Info("i")
	l.Debug("d")
	l.Trace("t")
	if got := buf.String(); !strings.Contains(got, "[INFO] i") || !strings.Contains(got, "[DEBUG] d") || strings.Contains(got, "[TRACE]") {
		t.Errorf("debug level output = %q", got)
	}

	buf.Reset()
	l.level = LogLevelTrace
	l.Trace("t")
	if !strings.Contains(buf.String(), "[TRACE] t") {
		t.Errorf("trace level output = %q", buf.String())
	}
}
