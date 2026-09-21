/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package resources

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	conf "github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
	"github.com/pgedge/ai-workbench/server/internal/tracing"
	_ "modernc.org/sqlite"

	"golang.org/x/crypto/bcrypt"
)

// readTestAuthStore returns an auth store holding a single ordinary
// (non-superuser) user, together with that user's ID.
func readTestAuthStore(t *testing.T) (*auth.AuthStore, int64) {
	t.Helper()

	store, err := auth.NewAuthStore(t.TempDir(), 0, 0)
	if err != nil {
		t.Fatalf("NewAuthStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)

	if err := store.CreateUser("reader", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	// connection_info is a public resource in the shipped privilege
	// set, so register it that way; tests that need a refusal register
	// their own non-public privilege.
	if _, err := store.RegisterMCPPrivilege(URIConnectionInfo,
		auth.MCPPrivilegeTypeResource, "connection info", true); err != nil {
		t.Fatalf("RegisterMCPPrivilege: %v", err)
	}
	userID, err := store.GetUserID("reader")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}

	return store, userID
}

// readTestConfig enables both built-in resources.
func readTestConfig() *conf.Config {
	return &conf.Config{
		Builtins: conf.BuiltinsConfig{
			Resources: conf.ResourcesConfig{
				SystemInfo:     boolPtr(true),
				ConnectionInfo: boolPtr(true),
			},
		},
	}
}

// dropAuthTableForTest removes one table from an auth store's SQLite
// database, so that exactly one lookup fails whilst the store itself
// stays open and usable for everything else.
func dropAuthTableForTest(t *testing.T, dataDir, table string) {
	t.Helper()

	db, err := sql.Open("sqlite", filepath.Join(dataDir, "auth.db"))
	if err != nil {
		t.Fatalf("Failed to open the auth database: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec("DROP TABLE " + table); err != nil {
		t.Fatalf("Failed to drop %s: %v", table, err)
	}
}

// firstText returns the first text item of a resource response, failing
// the test when the response carries none.
func firstText(t *testing.T, content mcp.ResourceContent) string {
	t.Helper()

	if len(content.Contents) == 0 {
		t.Fatal("Expected the response to carry content")
	}
	return content.Contents[0].Text
}

// TestReadRefusesResourceOutsideRBAC verifies that a caller who holds no
// privilege for a resource is told so by Read, rather than being handed
// the resource or a database error.
func TestReadRefusesResourceOutsideRBAC(t *testing.T) {
	store, userID := readTestAuthStore(t)
	if _, err := store.RegisterMCPPrivilege(URISystemInfo,
		auth.MCPPrivilegeTypeResource, "system info", false); err != nil {
		t.Fatalf("RegisterMCPPrivilege: %v", err)
	}

	clientManager := database.NewClientManager(nil)
	defer clientManager.CloseAll()

	registry := NewContextAwareRegistry(clientManager, readTestConfig(), store, nil)

	ctx := context.WithValue(context.Background(), auth.UserIDContextKey, userID)
	content, err := registry.Read(ctx, URISystemInfo)
	if err != nil {
		t.Fatalf("Read returned an error: %v", err)
	}

	text := firstText(t, content)
	if !strings.Contains(text, "Access denied") {
		t.Errorf("Expected an access denial, got: %s", text)
	}
	if content.URI != URISystemInfo {
		t.Errorf("Expected the denial to name %q, got %q", URISystemInfo,
			content.URI)
	}
}

// TestReadCustomResourceReportsClientError verifies that a custom
// resource whose database client cannot be resolved reports the
// resolution error to the caller instead of invoking the handler.
func TestReadCustomResourceReportsClientError(t *testing.T) {
	// A nil client manager leaves the registry without a client
	// resolver, which is the shape a server started with no database
	// configured has.
	registry := NewContextAwareRegistry(nil, readTestConfig(), nil, nil)

	handlerCalled := false
	registry.customResources["pg://custom"] = customResource{
		definition: mcp.Resource{URI: "pg://custom", Name: "Custom"},
		handler: func(context.Context, *database.Client) (mcp.ResourceContent, error) {
			handlerCalled = true
			return mcp.ResourceContent{}, nil
		},
	}

	content, err := registry.Read(context.Background(), "pg://custom")
	if err != nil {
		t.Fatalf("Read returned an error: %v", err)
	}
	if handlerCalled {
		t.Error("Expected the handler not to run without a database client")
	}

	text := firstText(t, content)
	if !strings.Contains(text, "no database connection configured") {
		t.Errorf("Expected the client resolution error, got: %s", text)
	}
}

// TestReadTracesResourceReads verifies that an enabled trace records the
// read and its result, which is the only observable effect of the
// tracing branches in Read.
func TestReadTracesResourceReads(t *testing.T) {
	// tracing.Initialize is guarded by a sync.Once and tracing.Close
	// does not clear the enabled flag, so closing the trace here would
	// leave every later read in this package writing to a closed file
	// and warning about it. The trace file goes to the test's temporary
	// directory and is discarded with it instead.
	tracePath := filepath.Join(t.TempDir(), "trace.jsonl")
	if err := tracing.Initialize(tracePath); err != nil {
		t.Fatalf("tracing.Initialize: %v", err)
	}
	if !tracing.IsEnabled() {
		t.Fatal("Expected tracing to be enabled")
	}

	registry := NewContextAwareRegistry(nil, readTestConfig(), nil, nil)
	registry.customResources["pg://custom"] = customResource{
		definition: mcp.Resource{URI: "pg://custom", Name: "Custom"},
		handler: func(context.Context, *database.Client) (mcp.ResourceContent, error) {
			return mcp.ResourceContent{}, nil
		},
	}

	ctx := context.WithValue(context.Background(), auth.TokenHashContextKey,
		"trace-token-hash")

	// A single-text response, a multi-text response and a response with
	// no text at all each take a different branch when the trace entry
	// is assembled.
	if _, err := registry.Read(ctx, "pg://nonexistent"); err != nil {
		t.Fatalf("Read(unknown): %v", err)
	}
	registry.customResources["pg://multi"] = customResource{
		definition: mcp.Resource{URI: "pg://multi", Name: "Multi"},
		handler: func(context.Context, *database.Client) (mcp.ResourceContent, error) {
			return mcp.ResourceContent{
				URI: "pg://multi",
				Contents: []mcp.ContentItem{
					{Type: "text", Text: "one"},
					{Type: "text", Text: "two"},
				},
			}, nil
		},
	}
	registry.customResources["pg://empty"] = customResource{
		definition: mcp.Resource{URI: "pg://empty", Name: "Empty"},
		handler: func(context.Context, *database.Client) (mcp.ResourceContent, error) {
			return mcp.ResourceContent{
				URI:      "pg://empty",
				Contents: []mcp.ContentItem{{Type: "text"}},
			}, nil
		},
	}
	if _, err := registry.Read(ctx, "pg://multi"); err != nil {
		t.Fatalf("Read(multi): %v", err)
	}
	if _, err := registry.Read(ctx, "pg://empty"); err != nil {
		t.Fatalf("Read(empty): %v", err)
	}

	data, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	trace := string(data)
	if !strings.Contains(trace, "pg://nonexistent") {
		t.Errorf("Expected the trace to record the read, got: %s", trace)
	}
	if !strings.Contains(trace, "Resource not found") {
		t.Errorf("Expected the trace to record the result, got: %s", trace)
	}
}

// ---------------------------------------------------------------------------
// readConnectionInfo
// ---------------------------------------------------------------------------

// decodeConnectionInfo unmarshals a connection_info response.
func decodeConnectionInfo(t *testing.T, content mcp.ResourceContent) *ConnectionInfo {
	t.Helper()

	var info ConnectionInfo
	if err := json.Unmarshal([]byte(firstText(t, content)), &info); err != nil {
		t.Fatalf("Unmarshal connection info: %v", err)
	}
	return &info
}

// TestReadConnectionInfoWithoutToken verifies the response when the
// request carries no token at all, which is the CLI-over-stdio shape.
func TestReadConnectionInfoWithoutToken(t *testing.T) {
	registry := NewContextAwareRegistry(nil, readTestConfig(), nil, nil)

	content, err := registry.Read(context.Background(), URIConnectionInfo)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	info := decodeConnectionInfo(t, content)
	if info.Connected {
		t.Error("Expected connected to be false without a token")
	}
	if !strings.Contains(info.Message, "No authentication token found") {
		t.Errorf("Unexpected message: %s", info.Message)
	}
}

// TestReadConnectionInfoWithoutDatastore verifies the response when the
// server runs without connection management configured.
func TestReadConnectionInfoWithoutDatastore(t *testing.T) {
	store, _ := readTestAuthStore(t)
	registry := NewContextAwareRegistry(nil, readTestConfig(), store, nil)

	ctx := context.WithValue(context.Background(), auth.TokenHashContextKey,
		"some-token-hash")
	content, err := registry.Read(ctx, URIConnectionInfo)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	info := decodeConnectionInfo(t, content)
	if info.Connected {
		t.Error("Expected connected to be false without a datastore")
	}
	if !strings.Contains(info.Message, "Connection management not available") {
		t.Errorf("Unexpected message: %s", info.Message)
	}
}

// newConnectionInfoTestPool brings up the minimum connections table the
// connection_info resource reads, and returns a datastore over it.
func newConnectionInfoTestPool(t *testing.T) (*database.Datastore, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("Could not connect to the test database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("Test database ping failed: %v", err)
	}

	const schema = `
DROP TABLE IF EXISTS connections CASCADE;
CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    host VARCHAR(255) NOT NULL DEFAULT '',
    hostaddr VARCHAR(255),
    port INTEGER NOT NULL DEFAULT 5432,
    database_name VARCHAR(255) NOT NULL DEFAULT 'postgres',
    username VARCHAR(255) NOT NULL DEFAULT 'postgres',
    password_encrypted TEXT,
    sslmode VARCHAR(32),
    sslcert TEXT,
    sslkey TEXT,
    sslrootcert TEXT,
    owner_username VARCHAR(255),
    owner_token VARCHAR(255),
    is_monitored BOOLEAN NOT NULL DEFAULT TRUE,
    is_shared BOOLEAN NOT NULL DEFAULT TRUE,
    membership_source VARCHAR(16) NOT NULL DEFAULT 'auto'
);
`
	if _, err := pool.Exec(ctx, schema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create the connections table: %v", err)
	}

	cleanup := func() {
		if _, err := pool.Exec(context.Background(),
			"DROP TABLE IF EXISTS connections CASCADE"); err != nil {
			t.Logf("teardown failed: %v", err)
		}
		pool.Close()
	}

	return database.NewTestDatastore(pool), cleanup
}

// TestReadConnectionInfoSessionStates walks the three states a token can
// be in once connection management is configured: no session, a session
// naming a connection that no longer exists, and a live session.
func TestReadConnectionInfoSessionStates(t *testing.T) {
	datastore, cleanup := newConnectionInfoTestPool(t)
	defer cleanup()

	store, _ := readTestAuthStore(t)
	registry := NewContextAwareRegistry(nil, readTestConfig(), store, datastore)

	const tokenHash = "connection-info-token-hash"
	ctx := context.WithValue(context.Background(), auth.TokenHashContextKey,
		tokenHash)

	t.Run("no session selected", func(t *testing.T) {
		content, err := registry.Read(ctx, URIConnectionInfo)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		info := decodeConnectionInfo(t, content)
		if info.Connected {
			t.Error("Expected connected to be false with no session")
		}
		if !strings.Contains(info.Message, "No database connection selected") {
			t.Errorf("Unexpected message: %s", info.Message)
		}
	})

	t.Run("session names a missing connection", func(t *testing.T) {
		if err := store.SetConnectionSession(tokenHash, 999999, nil); err != nil {
			t.Fatalf("SetConnectionSession: %v", err)
		}
		content, err := registry.Read(ctx, URIConnectionInfo)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		info := decodeConnectionInfo(t, content)
		if info.Connected {
			t.Error("Expected connected to be false for a missing connection")
		}
		if !strings.Contains(info.Message, "Error retrieving connection details") {
			t.Errorf("Unexpected message: %s", info.Message)
		}
	})

	t.Run("live session reports the connection", func(t *testing.T) {
		connDescription := "integration fixture"
		conn, err := datastore.CreateConnection(context.Background(),
			database.ConnectionCreateParams{
				Name:          "primary",
				Description:   &connDescription,
				Host:          "192.0.2.10",
				Port:          5433,
				DatabaseName:  "appdb",
				Username:      "appuser",
				IsMonitored:   true,
				OwnerUsername: "reader",
			})
		if err != nil {
			t.Fatalf("CreateConnection: %v", err)
		}

		override := "otherdb"
		if err := store.SetConnectionSession(tokenHash, conn.ID,
			&override); err != nil {
			t.Fatalf("SetConnectionSession: %v", err)
		}

		content, err := registry.Read(ctx, URIConnectionInfo)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		info := decodeConnectionInfo(t, content)
		if !info.Connected {
			t.Fatalf("Expected a connected response, got: %s", info.Message)
		}
		if info.ConnectionID == nil || *info.ConnectionID != conn.ID {
			t.Errorf("Expected connection ID %d, got %v", conn.ID,
				info.ConnectionID)
		}
		if info.DatabaseName == nil || *info.DatabaseName != override {
			t.Errorf("Expected the session's database override %q, got %v",
				override, info.DatabaseName)
		}
		if info.Host == nil || *info.Host != "192.0.2.10" {
			t.Errorf("Expected the connection host, got %v", info.Host)
		}
		if info.IsMonitored == nil || !*info.IsMonitored {
			t.Error("Expected the monitored flag to be reported")
		}

		// Without the session override the connection's own database
		// name is reported instead.
		if err := store.SetConnectionSession(tokenHash, conn.ID, nil); err != nil {
			t.Fatalf("SetConnectionSession: %v", err)
		}
		content, err = registry.Read(ctx, URIConnectionInfo)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		info = decodeConnectionInfo(t, content)
		if info.DatabaseName == nil || *info.DatabaseName != "appdb" {
			t.Errorf("Expected the connection's own database, got %v",
				info.DatabaseName)
		}
	})
}

// TestReadConnectionInfoReportsSessionLookupFailure verifies that a
// broken session store is reported to the caller rather than being
// mistaken for "no connection selected".
func TestReadConnectionInfoReportsSessionLookupFailure(t *testing.T) {
	datastore, cleanup := newConnectionInfoTestPool(t)
	defer cleanup()

	dir := t.TempDir()
	store, err := auth.NewAuthStore(dir, 0, 0)
	if err != nil {
		t.Fatalf("NewAuthStore: %v", err)
	}
	defer store.Close()

	if _, err := store.RegisterMCPPrivilege(URIConnectionInfo,
		auth.MCPPrivilegeTypeResource, "connection info", true); err != nil {
		t.Fatalf("RegisterMCPPrivilege: %v", err)
	}
	dropAuthTableForTest(t, dir, "connection_sessions")

	registry := NewContextAwareRegistry(nil, readTestConfig(), store, datastore)
	ctx := context.WithValue(context.Background(), auth.TokenHashContextKey,
		"broken-store-token")

	content, err := registry.Read(ctx, URIConnectionInfo)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	info := decodeConnectionInfo(t, content)
	if info.Connected {
		t.Error("Expected connected to be false when the lookup fails")
	}
	if !strings.Contains(info.Message, "Error retrieving connection session") {
		t.Errorf("Unexpected message: %s", info.Message)
	}
}

// ---------------------------------------------------------------------------
// Reads that reach a real database
// ---------------------------------------------------------------------------

// testDatabaseConfig turns TEST_AI_WORKBENCH_SERVER into the config a
// ClientManager needs, skipping the test when it is unset.
func testDatabaseConfig(t *testing.T) *conf.DatabaseConfig {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping integration test")
	}

	parsed, err := url.Parse(connStr)
	if err != nil {
		t.Skipf("Could not parse TEST_AI_WORKBENCH_SERVER: %v", err)
	}
	port := 5432
	if p := parsed.Port(); p != "" {
		if converted, convErr := strconv.Atoi(p); convErr == nil {
			port = converted
		}
	}
	user := "postgres"
	if parsed.User != nil && parsed.User.Username() != "" {
		user = parsed.User.Username()
	}
	password, _ := parsed.User.Password()

	return &conf.DatabaseConfig{
		Host:     parsed.Hostname(),
		Port:     port,
		Database: strings.TrimPrefix(parsed.Path, "/"),
		User:     user,
		Password: password,
		SSLMode:  "disable",
	}
}

// TestReadServesResourcesFromRealClient covers the success path of both
// a built-in and a custom resource, which needs a live database client.
func TestReadServesResourcesFromRealClient(t *testing.T) {
	dbConfig := testDatabaseConfig(t)

	// Initialize is a no-op once tracing is on, so this leaves the
	// trace enabled whichever order the package's tests run in, and
	// exercises the result-logging branches alongside the reads.
	if err := tracing.Initialize(filepath.Join(t.TempDir(),
		"trace.jsonl")); err != nil {
		t.Fatalf("tracing.Initialize: %v", err)
	}

	clientManager := database.NewClientManager(dbConfig)
	defer clientManager.CloseAll()

	registry := NewContextAwareRegistry(clientManager, readTestConfig(), nil, nil)
	registry.customResources["pg://multi"] = customResource{
		definition: mcp.Resource{URI: "pg://multi", Name: "Multi"},
		handler: func(context.Context, *database.Client) (mcp.ResourceContent, error) {
			return mcp.ResourceContent{
				URI: "pg://multi",
				Contents: []mcp.ContentItem{
					{Type: "text", Text: "first"},
					{Type: "text", Text: "second"},
				},
			}, nil
		},
	}

	var handlerClient *database.Client
	registry.customResources["pg://custom"] = customResource{
		definition: mcp.Resource{URI: "pg://custom", Name: "Custom"},
		handler: func(_ context.Context, client *database.Client) (mcp.ResourceContent, error) {
			handlerClient = client
			return mcp.ResourceContent{
				URI:      "pg://custom",
				Contents: []mcp.ContentItem{{Type: "text", Text: "custom body"}},
			}, nil
		},
	}

	ctx := context.WithValue(context.Background(), auth.TokenHashContextKey,
		"real-client-token-hash")

	content, err := registry.Read(ctx, URISystemInfo)
	if err != nil {
		t.Fatalf("Read(system_info): %v", err)
	}
	var systemInfo map[string]any
	if err := json.Unmarshal([]byte(firstText(t, content)),
		&systemInfo); err != nil {
		t.Fatalf("system_info is not JSON: %v", err)
	}
	if _, ok := systemInfo["postgresql_version"]; !ok {
		t.Errorf("Expected the server version in system_info, got %v", systemInfo)
	}

	content, err = registry.Read(ctx, "pg://custom")
	if err != nil {
		t.Fatalf("Read(custom): %v", err)
	}
	if got := firstText(t, content); got != "custom body" {
		t.Errorf("Expected the handler's body, got %q", got)
	}
	if handlerClient == nil {
		t.Error("Expected the custom handler to receive a database client")
	}

	content, err = registry.Read(ctx, "pg://multi")
	if err != nil {
		t.Fatalf("Read(multi): %v", err)
	}
	if len(content.Contents) != 2 {
		t.Fatalf("Expected two content items, got %d", len(content.Contents))
	}
	if content.Contents[1].Text != "second" {
		t.Errorf("Expected the second item to survive, got %q",
			content.Contents[1].Text)
	}
}

// TestAuthSessionAdapter covers the adapter that lets the database
// package read connection sessions without importing auth: a missing
// session, a stored one and a failing lookup.
func TestAuthSessionAdapter(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.NewAuthStore(dir, 0, 0)
	if err != nil {
		t.Fatalf("NewAuthStore: %v", err)
	}
	defer store.Close()

	adapter := &authSessionAdapter{store: store}

	session, err := adapter.GetConnectionSession("no-such-token")
	if err != nil {
		t.Fatalf("GetConnectionSession: %v", err)
	}
	if session != nil {
		t.Errorf("Expected no session, got %+v", session)
	}

	database := "reporting"
	if err := store.SetConnectionSession("token-hash", 42, &database); err != nil {
		t.Fatalf("SetConnectionSession: %v", err)
	}
	session, err = adapter.GetConnectionSession("token-hash")
	if err != nil {
		t.Fatalf("GetConnectionSession: %v", err)
	}
	if session == nil || session.ConnectionID != 42 {
		t.Fatalf("Expected the stored session, got %+v", session)
	}
	if session.DatabaseName == nil || *session.DatabaseName != database {
		t.Errorf("Expected the database override to survive, got %v",
			session.DatabaseName)
	}

	if err := adapter.ClearConnectionSession("token-hash"); err != nil {
		t.Fatalf("ClearConnectionSession: %v", err)
	}
	session, err = adapter.GetConnectionSession("token-hash")
	if err != nil || session != nil {
		t.Fatalf("Expected the session to be cleared, got %+v, %v", session, err)
	}

	dropAuthTableForTest(t, dir, "connection_sessions")
	if _, err := adapter.GetConnectionSession("token-hash"); err == nil {
		t.Error("Expected a lookup failure to be reported")
	}
}

// TestListOmitsDisabledResources verifies that a resource turned off in
// the configuration is absent from the listing, which is the branch the
// enabled-only listing test leaves out.
func TestListOmitsDisabledResources(t *testing.T) {
	cfg := &conf.Config{
		Builtins: conf.BuiltinsConfig{
			Resources: conf.ResourcesConfig{
				SystemInfo:     boolPtr(false),
				ConnectionInfo: boolPtr(false),
			},
		},
	}

	registry := NewContextAwareRegistry(nil, cfg, nil, nil)
	registry.customResources["pg://custom"] = customResource{
		definition: mcp.Resource{URI: "pg://custom", Name: "Custom"},
	}

	listed := registry.List()
	if len(listed) != 1 || listed[0].URI != "pg://custom" {
		t.Fatalf("Expected only the custom resource, got %+v", listed)
	}
}
