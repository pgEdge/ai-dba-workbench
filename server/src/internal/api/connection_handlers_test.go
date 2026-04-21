/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

func TestNewConnectionHandler(t *testing.T) {
	handler := NewConnectionHandler(nil, nil, nil)
	if handler == nil {
		t.Fatal("NewConnectionHandler returned nil")
	}
	if handler.datastore != nil {
		t.Error("Expected nil datastore")
	}
	if handler.authStore != nil {
		t.Error("Expected nil authStore")
	}
	if handler.hostValidator == nil {
		t.Error("Expected non-nil hostValidator (should use default)")
	}
}

func TestNewConnectionHandlerWithSecurity(t *testing.T) {
	handler := NewConnectionHandlerWithSecurity(nil, nil, nil, true,
		[]string{"allowed.example.com"},
		[]string{"blocked.example.com"})

	if handler == nil {
		t.Fatal("NewConnectionHandlerWithSecurity returned nil")
	}
	if handler.hostValidator == nil {
		t.Error("Expected non-nil hostValidator")
	}
	if !handler.hostValidator.AllowInternalNetworks {
		t.Error("Expected AllowInternalNetworks to be true")
	}
}

func TestConnectionHandler_HandleNotConfigured(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections", nil)
	rec := httptest.NewRecorder()

	HandleNotConfigured("Database connection management")(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	expected := "Database connection management is not available. The datastore is not configured."
	if response.Error != expected {
		t.Errorf("Expected error %q, got %q", expected, response.Error)
	}
}

func TestConnectionHandler_HandleConnections_MethodNotAllowed(t *testing.T) {
	handler := NewConnectionHandler(nil, nil, nil)

	tests := []struct {
		name   string
		method string
	}{
		{"DELETE not allowed", http.MethodDelete},
		{"PUT not allowed", http.MethodPut},
		{"PATCH not allowed", http.MethodPatch},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/v1/connections", nil)
			rec := httptest.NewRecorder()

			handler.handleConnections(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("Expected status %d, got %d",
					http.StatusMethodNotAllowed, rec.Code)
			}

			allowed := rec.Header().Get("Allow")
			if allowed != "GET, POST" {
				t.Errorf("Expected Allow header 'GET, POST', got %q", allowed)
			}
		})
	}
}

func TestConnectionHandler_HandleConnectionSubpath_InvalidID(t *testing.T) {
	handler := NewConnectionHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections/abc", nil)
	rec := httptest.NewRecorder()

	handler.handleConnectionSubpath(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "Invalid connection ID" {
		t.Errorf("Expected error 'Invalid connection ID', got %q", response.Error)
	}
}

func TestConnectionHandler_HandleConnectionSubpath_MethodNotAllowed(t *testing.T) {
	handler := NewConnectionHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/connections/1", nil)
	rec := httptest.NewRecorder()

	handler.handleConnectionSubpath(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	allowed := rec.Header().Get("Allow")
	if allowed != "GET, PUT, DELETE" {
		t.Errorf("Expected Allow header 'GET, PUT, DELETE', got %q", allowed)
	}
}

func TestConnectionHandler_HandleConnectionSubpath_DatabasesMethodNotAllowed(t *testing.T) {
	handler := NewConnectionHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/connections/1/databases", nil)
	rec := httptest.NewRecorder()

	handler.handleConnectionSubpath(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	allowed := rec.Header().Get("Allow")
	if allowed != "GET" {
		t.Errorf("Expected Allow header 'GET', got %q", allowed)
	}
}

func TestConnectionHandler_HandleCurrentConnection_MethodNotAllowed(t *testing.T) {
	handler := NewConnectionHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/connections/current", nil)
	req.Header.Set("Authorization", "Bearer testtoken")
	rec := httptest.NewRecorder()

	handler.handleCurrentConnection(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}

	allowed := rec.Header().Get("Allow")
	if allowed != "GET, POST, DELETE" {
		t.Errorf("Expected Allow header 'GET, POST, DELETE', got %q", allowed)
	}
}

func TestConnectionHandler_HandleCurrentConnection_MissingAuth(t *testing.T) {
	handler := NewConnectionHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections/current", nil)
	rec := httptest.NewRecorder()

	handler.handleCurrentConnection(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "Invalid or missing authentication token" {
		t.Errorf("Expected auth error, got %q", response.Error)
	}
}

func TestConnectionHandler_RegisterRoutes_NotConfigured(t *testing.T) {
	handler := NewConnectionHandler(nil, nil, nil)
	mux := http.NewServeMux()
	noopWrapper := func(h http.HandlerFunc) http.HandlerFunc { return h }

	handler.RegisterRoutes(mux, noopWrapper)

	paths := []string{
		"/api/v1/connections",
		"/api/v1/connections/1",
		"/api/v1/connections/current",
	}

	for _, path := range paths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("Path %s: expected status %d, got %d",
				path, http.StatusServiceUnavailable, rec.Code)
		}
	}
}

func TestConnectionHandler_CreateConnection_NoAuth(t *testing.T) {
	// Test that createConnection requires authentication
	rbac := auth.NewRBACChecker(nil)
	handler := NewConnectionHandler(nil, nil, rbac)

	body, _ := json.Marshal(ConnectionCreateRequest{
		Name:         "test",
		Host:         "example.com",
		Port:         5432,
		DatabaseName: "testdb",
		Username:     "user",
		Password:     "pass",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connections",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// No Authorization header
	rec := httptest.NewRecorder()

	handler.createConnection(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusUnauthorized, rec.Code, rec.Body.String())
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "Invalid or missing authentication token" {
		t.Errorf("Expected auth error, got %q", response.Error)
	}
}

func TestConnectionHandler_UpdateConnection_NoAuth(t *testing.T) {
	// Test that updateConnection requires authentication
	rbac := auth.NewRBACChecker(nil)
	handler := NewConnectionHandler(nil, nil, rbac)

	body, _ := json.Marshal(ConnectionFullUpdateRequest{})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/connections/1",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// No Authorization header
	rec := httptest.NewRecorder()

	handler.updateConnection(rec, req, 1)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "Invalid or missing authentication token" {
		t.Errorf("Expected auth error, got %q", response.Error)
	}
}

func TestConnectionHandler_SetCurrentConnection_InvalidConnectionID(t *testing.T) {
	handler := NewConnectionHandler(nil, nil, nil)

	body, _ := json.Marshal(CurrentConnectionRequest{ConnectionID: 0})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connections/current",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.setCurrentConnection(rec, req, "test-token-hash")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.Error != "connection_id is required" {
		t.Errorf("Expected error 'connection_id is required', got %q", response.Error)
	}
}

func TestConnectionCreateRequest_JSON(t *testing.T) {
	sslMode := "require"
	req := ConnectionCreateRequest{
		Name:         "Production DB",
		Host:         "db.example.com",
		Port:         5432,
		DatabaseName: "mydb",
		Username:     "admin",
		Password:     "secret",
		SSLMode:      &sslMode,
		IsShared:     true,
		IsMonitored:  true,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var decoded ConnectionCreateRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if decoded.Name != req.Name {
		t.Errorf("Name = %q, want %q", decoded.Name, req.Name)
	}
	if decoded.Host != req.Host {
		t.Errorf("Host = %q, want %q", decoded.Host, req.Host)
	}
	if decoded.Port != req.Port {
		t.Errorf("Port = %d, want %d", decoded.Port, req.Port)
	}
	if decoded.SSLMode == nil || *decoded.SSLMode != sslMode {
		t.Error("SSLMode mismatch")
	}
	if !decoded.IsShared {
		t.Error("Expected IsShared to be true")
	}
	if !decoded.IsMonitored {
		t.Error("Expected IsMonitored to be true")
	}
}

func TestConnectionFullUpdateRequest_JSON(t *testing.T) {
	name := "Updated Name"
	port := 5433
	req := ConnectionFullUpdateRequest{
		Name: &name,
		Port: &port,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var decoded ConnectionFullUpdateRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if decoded.Name == nil || *decoded.Name != name {
		t.Error("Name mismatch")
	}
	if decoded.Port == nil || *decoded.Port != port {
		t.Error("Port mismatch")
	}
}

func TestCurrentConnectionRequest_JSON(t *testing.T) {
	dbName := "testdb"
	req := CurrentConnectionRequest{
		ConnectionID: 42,
		DatabaseName: &dbName,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var decoded CurrentConnectionRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if decoded.ConnectionID != 42 {
		t.Errorf("ConnectionID = %d, want 42", decoded.ConnectionID)
	}
	if decoded.DatabaseName == nil || *decoded.DatabaseName != dbName {
		t.Error("DatabaseName mismatch")
	}
}

func TestCurrentConnectionResponse_JSON(t *testing.T) {
	dbName := "testdb"
	resp := CurrentConnectionResponse{
		ConnectionID: 42,
		DatabaseName: &dbName,
		Host:         "db.example.com",
		Port:         5432,
		Name:         "Production",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var decoded CurrentConnectionResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if decoded.ConnectionID != resp.ConnectionID {
		t.Errorf("ConnectionID = %d, want %d", decoded.ConnectionID, resp.ConnectionID)
	}
	if decoded.Host != resp.Host {
		t.Errorf("Host = %q, want %q", decoded.Host, resp.Host)
	}
	if decoded.Port != resp.Port {
		t.Errorf("Port = %d, want %d", decoded.Port, resp.Port)
	}
	if decoded.Name != resp.Name {
		t.Errorf("Name = %q, want %q", decoded.Name, resp.Name)
	}
}

func TestConnectionHandler_HandleSubpath_NotFound(t *testing.T) {
	handler := NewConnectionHandler(nil, nil, nil)

	// Test unknown subpath
	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections/1/unknown", nil)
	rec := httptest.NewRecorder()

	handler.handleConnectionSubpath(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
}

func TestConnectionHandler_HandleSubpath_EmptyPath(t *testing.T) {
	handler := NewConnectionHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections/", nil)
	rec := httptest.NewRecorder()

	handler.handleConnectionSubpath(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
}

// =============================================================================
// Regression Test for GitHub Issue #83
//
// Issue #83: GET /api/v1/connections returned an empty array for a
// scoped token when the token owner's access to the scoped connection
// came through the ConnectionIDAll group wildcard instead of a specific
// group grant. GetEffectivePrivileges intersected the scoped connection
// IDs against the user's explicit privileges map and silently dropped
// the entry because only ConnectionIDAll (0) was present in the map.
//
// This test exercises the full HTTP handler path with a real datastore
// and auth store: it seeds a connection owned by "alice", gives "bob" a
// wildcard group grant, creates a scoped token for bob whose scope
// names that specific connection, and asserts the listing contains the
// scoped connection.
// =============================================================================

// listConnectionsIssue83TestSchema creates the minimum columns listed in
// database.ConnectionListItem. The schema is intentionally trimmed to
// only what GetAllConnections selects.
//
// NOTE: This is a trimmed snapshot of the production connections table
// DDL. It must track the columns selected by GetAllConnections in
// database/datastore.go. If the production schema adds NOT NULL columns
// or renames fields, update this constant to match.
const listConnectionsIssue83TestSchema = `
DROP TABLE IF EXISTS connections CASCADE;
CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    host VARCHAR(255) NOT NULL,
    hostaddr VARCHAR(255),
    port INTEGER NOT NULL DEFAULT 5432,
    database_name VARCHAR(255) NOT NULL,
    username VARCHAR(255),
    is_monitored BOOLEAN NOT NULL DEFAULT FALSE,
    is_shared BOOLEAN NOT NULL DEFAULT FALSE,
    owner_username VARCHAR(255),
    cluster_id INTEGER,
    membership_source VARCHAR(16) NOT NULL DEFAULT 'auto',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

// newListConnectionsIssue83Datastore wires a *database.Datastore to the
// Postgres instance named by TEST_AI_WORKBENCH_SERVER and installs the
// trimmed schema above. The test is skipped when the environment is not
// configured to run database-backed tests.
func newListConnectionsIssue83Datastore(t *testing.T) (*database.Datastore, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping issue #83 integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("Test database ping failed: %v", err)
	}

	if _, err := pool.Exec(ctx, listConnectionsIssue83TestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create issue #83 test schema: %v", err)
	}

	ds := database.NewTestDatastore(pool)
	cleanup := func() {
		_, _ = pool.Exec(context.Background(),
			"DROP TABLE IF EXISTS connections CASCADE")
		pool.Close()
	}
	return ds, pool, cleanup
}

// TestListConnectionsScopedTokenReturnsScopedConnection is the handler-
// level regression test for issue #83. Before the fix, the body would
// be "[]" because GetEffectivePrivileges dropped the scoped connection
// during intersection with the user's wildcard grant. After the fix,
// connection 11 appears in the response.
func TestListConnectionsScopedTokenReturnsScopedConnection(t *testing.T) {
	ds, pool, cleanupDS := newListConnectionsIssue83Datastore(t)
	defer cleanupDS()

	// Auth store with a user "bob" whose ONLY connection access comes
	// through a group-wide ConnectionIDAll read_write grant. The bug
	// reproducer requires that the group grant not name connection 11
	// explicitly.
	_, store, cleanupStore := createTestRBACHandler(t)
	defer cleanupStore()

	if err := store.CreateUser("bob", "Password1", "Bob", "", ""); err != nil {
		t.Fatalf("CreateUser bob: %v", err)
	}
	bobID, err := store.GetUserID("bob")
	if err != nil {
		t.Fatalf("GetUserID bob: %v", err)
	}
	groupID, err := store.CreateGroup("bob-group", "Bob's group")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := store.AddUserToGroup(groupID, bobID); err != nil {
		t.Fatalf("AddUserToGroup: %v", err)
	}
	if err := store.GrantConnectionPrivilege(groupID, auth.ConnectionIDAll,
		auth.AccessLevelReadWrite); err != nil {
		t.Fatalf("GrantConnectionPrivilege: %v", err)
	}

	// Seed connection 11 (unshared) owned by someone other than bob so
	// the listConnections "owner" branch does not rescue it.
	const scopedConnID = 11
	_, err = pool.Exec(context.Background(), `
        INSERT INTO connections (id, name, description, host, port,
            database_name, username, is_shared, owner_username,
            membership_source)
        VALUES ($1, 'scoped-conn', '', 'db.example.com', 5432, 'postgres',
            'alice', FALSE, 'alice', 'manual')
    `, scopedConnID)
	if err != nil {
		t.Fatalf("Seed connection: %v", err)
	}
	// Also seed a second unshared connection owned by alice that bob's
	// token is NOT scoped to. Without the fix this could mask the bug by
	// accidentally being present; after the fix the scoped token must
	// exclude it.
	_, err = pool.Exec(context.Background(), `
        INSERT INTO connections (id, name, description, host, port,
            database_name, username, is_shared, owner_username,
            membership_source)
        VALUES (12, 'other-conn', '', 'db2.example.com', 5432, 'postgres',
            'alice', FALSE, 'alice', 'manual')
    `)
	if err != nil {
		t.Fatalf("Seed second connection: %v", err)
	}

	// Create a scoped token for bob naming connection 11 only.
	_, token, err := store.CreateToken("bob", "Scoped token", nil)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	if err := store.SetTokenConnectionScope(token.ID, []auth.ScopedConnection{
		{ConnectionID: scopedConnID, AccessLevel: auth.AccessLevelRead},
	}); err != nil {
		t.Fatalf("SetTokenConnectionScope: %v", err)
	}

	// Build the handler with a real checker wired to the datastore's
	// sharing lookup. The listConnections handler reads the context
	// directly, so we populate the same values the middleware would set
	// in production: user ID, username, token ID, and the
	// not-a-superuser flag.
	checker := auth.NewRBACChecker(store)
	checker.SetConnectionSharingLookup(
		func(ctx context.Context, id int) (bool, string, error) {
			return ds.GetConnectionSharingInfo(ctx, id)
		},
	)
	handler := NewConnectionHandler(ds, store, checker)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections", nil)
	ctx := req.Context()
	ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, auth.UserIDContextKey, bobID)
	ctx = context.WithValue(ctx, auth.UsernameContextKey, "bob")
	ctx = context.WithValue(ctx, auth.TokenIDContextKey, token.ID)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.handleConnections(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d. Body: %s",
			rec.Code, rec.Body.String())
	}

	var got []database.ConnectionListItem
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("Decode response: %v", err)
	}

	// The scoped connection must be present. Before the fix this was []
	// and the test would fail.
	var foundScoped, foundOther bool
	for _, c := range got {
		switch c.ID {
		case scopedConnID:
			foundScoped = true
		case 12:
			foundOther = true
		}
	}
	if !foundScoped {
		t.Errorf("Expected connection %d in response, got %+v (issue #83 regression)",
			scopedConnID, got)
	}
	if foundOther {
		t.Errorf("Expected connection 12 NOT in response (token not scoped to it), got %+v",
			got)
	}
}
