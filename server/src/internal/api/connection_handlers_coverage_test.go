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
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// =============================================================================
// Tests for the ConnectionHandler endpoints in
// connection_handlers.go: create, get, update, delete, database listing,
// current-connection selection, connection context and cluster assignment.
//
// Each test runs the handler against a real datastore on the Postgres named
// by TEST_AI_WORKBENCH_SERVER and a real SQLite auth store, and asserts on
// status codes, response bodies and the rows left behind.
// =============================================================================

// connCovSchema is a self-contained schema carrying every table and column
// the connection handlers read or write. conn_cov_delete_blocker holds a
// foreign key without ON DELETE so a test can force DeleteConnection to fail
// after the handler has passed its permission checks.
const connCovSchema = `
DROP TABLE IF EXISTS conn_cov_delete_blocker CASCADE;
DROP TABLE IF EXISTS cluster_node_relationships CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
CREATE TABLE clusters (
    id SERIAL PRIMARY KEY,
    group_id INTEGER,
    auto_cluster_key VARCHAR(255) UNIQUE,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    replication_type VARCHAR(50),
    dismissed BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    host VARCHAR(255) NOT NULL,
    hostaddr VARCHAR(255),
    port INTEGER NOT NULL DEFAULT 5432,
    database_name VARCHAR(255) NOT NULL,
    username VARCHAR(255),
    password_encrypted TEXT,
    sslmode VARCHAR(32),
    sslcert TEXT,
    sslkey TEXT,
    sslrootcert TEXT,
    owner_username VARCHAR(255),
    owner_token VARCHAR(255),
    is_monitored BOOLEAN NOT NULL DEFAULT FALSE,
    is_shared BOOLEAN NOT NULL DEFAULT FALSE,
    membership_source VARCHAR(16) NOT NULL DEFAULT 'auto',
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE SET NULL,
    role VARCHAR(64),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE cluster_node_relationships (
    id SERIAL PRIMARY KEY,
    cluster_id INTEGER NOT NULL,
    source_connection_id INTEGER NOT NULL,
    target_connection_id INTEGER NOT NULL,
    relationship_type VARCHAR(50) NOT NULL,
    is_auto_detected BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE TABLE conn_cov_delete_blocker (
    connection_id INTEGER REFERENCES connections(id)
);
`

// connCovAllowedHost is placed on the handler's host allowlist so create
// and update tests can pass SSRF validation without a DNS lookup.
const connCovAllowedHost = "db.example.com"

// connCovSecret is the server secret given to fixtures that need to
// encrypt connection passwords.
const connCovSecret = "connection-handler-coverage-secret"

// connCovFixture bundles the datastore, auth store and handler for one test.
type connCovFixture struct {
	ds      *database.Datastore
	pool    *pgxpool.Pool
	connStr string
	store   *auth.AuthStore
	handler *ConnectionHandler
}

// newConnCovFixture installs connCovSchema in the database named by
// TEST_AI_WORKBENCH_SERVER and builds a ConnectionHandler wired as in
// production: the RBAC checker uses the datastore for its sharing lookup.
// An empty secret yields a datastore that cannot encrypt passwords.
func newConnCovFixture(t *testing.T, secret string) *connCovFixture {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping connection handler test")
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
	if _, err := pool.Exec(ctx, connCovSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create connection handler schema: %v", err)
	}

	var ds *database.Datastore
	if secret == "" {
		ds = database.NewTestDatastore(pool)
	} else {
		ds = database.NewTestDatastoreWithSecret(pool, secret)
	}

	_, store, cleanupStore := createTestRBACHandler(t)
	checker := auth.NewRBACCheckerForDatastore(store, ds)
	handler := NewConnectionHandlerWithSecurity(ds, store, checker, false,
		[]string{connCovAllowedHost}, nil)

	t.Cleanup(func() {
		cleanupStore()
		_, _ = pool.Exec(context.Background(), `
            DROP TABLE IF EXISTS conn_cov_delete_blocker CASCADE;
            DROP TABLE IF EXISTS cluster_node_relationships CASCADE;
            DROP TABLE IF EXISTS connections CASCADE;
            DROP TABLE IF EXISTS clusters CASCADE;
        `)
		pool.Close()
	})

	return &connCovFixture{
		ds:      ds,
		pool:    pool,
		connStr: connStr,
		store:   store,
		handler: handler,
	}
}

// connCovCaller is an authenticated session user.
type connCovCaller struct {
	id       int64
	username string
	token    string
}

// newCaller creates a user holding the given admin permissions (none when
// the slice is empty) and signs them in to obtain a session token.
func (f *connCovFixture) newCaller(t *testing.T, username string, permissions ...string) connCovCaller {
	t.Helper()
	var id int64
	if len(permissions) == 0 {
		id = newIssue207UnprivilegedUser(t, f.store, username)
	} else {
		id = setupUserWithPermissions(t, f.store, username, permissions)
	}
	token, _, err := f.store.AuthenticateUser(username, "Password1234")
	if err != nil {
		t.Fatalf("AuthenticateUser(%s): %v", username, err)
	}
	return connCovCaller{id: id, username: username, token: token}
}

// seed inserts a connection and returns its ID.
func (f *connCovFixture) seed(t *testing.T, name, owner string, shared bool) int {
	t.Helper()
	var id int
	err := f.pool.QueryRow(context.Background(), `
        INSERT INTO connections (name, description, host, port,
            database_name, username, owner_username, is_shared,
            membership_source)
        VALUES ($1, 'seeded', 'db.example.com', 5432, 'postgres',
            'app', $2, $3, 'auto')
        RETURNING id
    `, name, owner, shared).Scan(&id)
	if err != nil {
		t.Fatalf("Seed connection %q: %v", name, err)
	}
	return id
}

// seedCluster inserts a cluster and returns its ID.
func (f *connCovFixture) seedCluster(t *testing.T, name string, dismissed bool) int {
	t.Helper()
	var id int
	err := f.pool.QueryRow(context.Background(), `
        INSERT INTO clusters (name, replication_type, dismissed)
        VALUES ($1, 'spock', $2)
        RETURNING id
    `, name, dismissed).Scan(&id)
	if err != nil {
		t.Fatalf("Seed cluster %q: %v", name, err)
	}
	return id
}

// exec runs a statement against the test database.
func (f *connCovFixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("Exec %q: %v", sql, err)
	}
}

// rowExists reports whether a connection row with the given ID exists.
func (f *connCovFixture) rowExists(t *testing.T, id int) bool {
	t.Helper()
	var exists bool
	if err := f.pool.QueryRow(context.Background(),
		"SELECT EXISTS (SELECT 1 FROM connections WHERE id = $1)", id).Scan(&exists); err != nil {
		t.Fatalf("Check connection %d: %v", id, err)
	}
	return exists
}

// serve dispatches a request through handleConnectionSubpath, so the path
// routing is exercised alongside the handler it selects.
func (f *connCovFixture) serve(req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	f.handler.handleConnectionSubpath(rec, req)
	return rec
}

// connCovRequest builds a request with an optional JSON body. A string
// body is sent verbatim, which lets tests send malformed JSON.
func connCovRequest(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()
	var reader *bytes.Reader
	switch b := body.(type) {
	case nil:
		reader = bytes.NewReader(nil)
	case string:
		reader = bytes.NewReader([]byte(b))
	default:
		data, err := json.Marshal(b)
		if err != nil {
			t.Fatalf("Marshal request body: %v", err)
		}
		reader = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	return req
}

// asCaller presents the request as the given session user, setting both the
// bearer header read by getUserInfoCompat and the context values the
// authentication middleware would otherwise populate.
func asCaller(req *http.Request, c connCovCaller) *http.Request {
	req = withBearer(req, c.token)
	req = withUser(req, c.id)
	return withUsername(req, c.username)
}

// asSuperuserCaller presents the request as the given session user with the
// superuser flag set, as the middleware does for a superuser session.
func asSuperuserCaller(req *http.Request, c connCovCaller) *http.Request {
	req = asCaller(req, c)
	return withSuperuser(req)
}

// withIncompleteToken marks the request as API-token authenticated without a
// token ID. Every RBAC check fails closed on such a context, which gives
// the tests a deterministic out-of-scope caller.
func withIncompleteToken(req *http.Request) *http.Request {
	ctx := context.WithValue(req.Context(), auth.IsAPITokenContextKey, true)
	return req.WithContext(ctx)
}

// connCovPath builds /api/v1/connections/{id}[/suffix].
func connCovPath(id int, suffix string) string {
	p := fmt.Sprintf("/api/v1/connections/%d", id)
	if suffix != "" {
		p += "/" + suffix
	}
	return p
}

// assertStatus fails the test when the response code is not want.
func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("Expected status %d, got %d. Body: %s", want, rec.Code, rec.Body.String())
	}
}

// assertError fails the test unless the response has the given status and
// an ErrorResponse body whose message equals wantMsg.
func assertError(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantMsg string) {
	t.Helper()
	assertStatus(t, rec, wantStatus)
	var resp ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Decode error body %q: %v", rec.Body.String(), err)
	}
	if resp.Error != wantMsg {
		t.Errorf("Error message = %q, want %q", resp.Error, wantMsg)
	}
}

// decodeInto decodes the response body into dest.
func decodeInto(t *testing.T, rec *httptest.ResponseRecorder, dest any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), dest); err != nil {
		t.Fatalf("Decode body %q: %v", rec.Body.String(), err)
	}
}

// connCovJSONConn mirrors the MonitoredConnection JSON shape.
type connCovJSONConn struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	DatabaseName string `json:"database_name"`
	Username     string `json:"username"`
	IsShared     bool   `json:"is_shared"`
	IsMonitored  bool   `json:"is_monitored"`
}

// =============================================================================
// RegisterRoutes and top-level routing
// =============================================================================

// TestConnCov_RegisterRoutes_Configured confirms that a handler with a
// datastore registers the real endpoints and wraps each in the supplied
// auth wrapper.
func TestConnCov_RegisterRoutes_Configured(t *testing.T) {
	f := newConnCovFixture(t, connCovSecret)
	admin := f.newCaller(t, "cov_routes_admin")
	id := f.seed(t, "routed", admin.username, false)

	wrapped := 0
	wrapper := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			wrapped++
			next(w, r)
		}
	}
	mux := http.NewServeMux()
	f.handler.RegisterRoutes(mux, wrapper)

	// GET /api/v1/connections/{id} reaches getConnection.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, asSuperuserCaller(connCovRequest(t, http.MethodGet, connCovPath(id, ""), nil), admin))
	assertStatus(t, rec, http.StatusOK)
	var got connCovJSONConn
	decodeInto(t, rec, &got)
	if got.ID != id || got.Name != "routed" {
		t.Errorf("Got connection %+v, want id=%d name=routed", got, id)
	}

	// /api/v1/connections/current reaches handleCurrentConnection, which
	// rejects a request without a bearer token.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, connCovRequest(t, http.MethodGet, "/api/v1/connections/current", nil))
	assertError(t, rec, http.StatusUnauthorized, "Invalid or missing authentication token")

	// /api/v1/connections reaches handleConnections, which refuses PATCH.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, connCovRequest(t, http.MethodPatch, "/api/v1/connections", nil))
	assertStatus(t, rec, http.StatusMethodNotAllowed)
	if allow := rec.Header().Get("Allow"); allow != "GET, POST" {
		t.Errorf("Allow = %q, want %q", allow, "GET, POST")
	}

	if wrapped != 3 {
		t.Errorf("Auth wrapper ran %d times, want 3", wrapped)
	}
}

// TestConnCov_HandleConnections_PostCreates confirms that POST on the
// collection endpoint is routed to createConnection.
func TestConnCov_HandleConnections_PostCreates(t *testing.T) {
	f := newConnCovFixture(t, connCovSecret)
	admin := f.newCaller(t, "cov_post_admin", auth.PermManageConnections)

	// A description is supplied because CreateConnection cannot scan a
	// NULL description back out of the row it has just inserted.
	desc := ""
	req := asCaller(connCovRequest(t, http.MethodPost, "/api/v1/connections", ConnectionCreateRequest{
		Name:         "posted",
		Description:  &desc,
		Host:         connCovAllowedHost,
		Port:         5432,
		DatabaseName: "postgres",
		Username:     "app",
		Password:     "secret",
	}), admin)
	rec := httptest.NewRecorder()
	f.handler.handleConnections(rec, req)

	assertStatus(t, rec, http.StatusCreated)
	var got connCovJSONConn
	decodeInto(t, rec, &got)
	if !f.rowExists(t, got.ID) {
		t.Errorf("Connection %d was not persisted", got.ID)
	}
}

// TestConnCov_HandleConnectionSubpath_Routing covers the subpath dispatch
// branches that do not depend on handler-specific setup.
func TestConnCov_HandleConnectionSubpath_Routing(t *testing.T) {
	f := newConnCovFixture(t, connCovSecret)

	tests := []struct {
		name      string
		method    string
		path      string
		wantCode  int
		wantAllow string
	}{
		{"current delegates", http.MethodGet, "/api/v1/connections/current", http.StatusUnauthorized, ""},
		{"context wrong method", http.MethodPost, connCovPath(1, "context"), http.StatusMethodNotAllowed, "GET"},
		{"cluster wrong method", http.MethodDelete, connCovPath(1, "cluster"), http.StatusMethodNotAllowed, "GET, PUT"},
		{"query wrong method", http.MethodGet, connCovPath(1, "query"), http.StatusMethodNotAllowed, "POST"},
		{"unknown suffix", http.MethodGet, connCovPath(1, "unknown"), http.StatusNotFound, ""},
		{"too many segments", http.MethodGet, connCovPath(1, "cluster/extra"), http.StatusNotFound, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := f.serve(connCovRequest(t, tt.method, tt.path, nil))
			assertStatus(t, rec, tt.wantCode)
			if tt.wantAllow != "" && rec.Header().Get("Allow") != tt.wantAllow {
				t.Errorf("Allow = %q, want %q", rec.Header().Get("Allow"), tt.wantAllow)
			}
		})
	}
}

// =============================================================================
// createConnection
// =============================================================================

// TestConnCov_CreateConnection_Validation walks the request validation that
// runs after the permission gate, confirming each rejection's status and
// message and that nothing is written.
func TestConnCov_CreateConnection_Validation(t *testing.T) {
	f := newConnCovFixture(t, connCovSecret)
	admin := f.newCaller(t, "cov_create_validation", auth.PermManageConnections)

	valid := func() ConnectionCreateRequest {
		return ConnectionCreateRequest{
			Name:         "new-conn",
			Host:         connCovAllowedHost,
			Port:         5432,
			DatabaseName: "postgres",
			Username:     "app",
			Password:     "secret",
		}
	}

	tests := []struct {
		name    string
		body    any
		wantMsg string
	}{
		{"malformed JSON", "{not json", "Invalid request body"},
		{"missing host", func() any { r := valid(); r.Host = ""; return r }(), "Host is required"},
		{"zero port", func() any { r := valid(); r.Port = 0; return r }(), "Port is required and must be positive"},
		{"missing database", func() any { r := valid(); r.DatabaseName = ""; return r }(), "Maintenance Database is required"},
		{"missing username", func() any { r := valid(); r.Username = ""; return r }(), "Username is required"},
		{"missing password", func() any { r := valid(); r.Password = ""; return r }(), "Password is required"},
		{"host too long", func() any { r := valid(); r.Host = strings.Repeat("h", 256); return r }(), "Host must be 255 characters or less"},
		{"database too long", func() any { r := valid(); r.DatabaseName = strings.Repeat("d", 256); return r }(), "Maintenance Database must be 255 characters or less"},
		{"username too long", func() any { r := valid(); r.Username = strings.Repeat("u", 256); return r }(), "Username must be 255 characters or less"},
		{"internal host", func() any { r := valid(); r.Host = "10.0.0.1"; return r }(), "Invalid host"},
		{"blocked port", func() any { r := valid(); r.Port = 22; return r }(), "Invalid port"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := asCaller(connCovRequest(t, http.MethodPost, "/api/v1/connections", tt.body), admin)
			rec := httptest.NewRecorder()
			f.handler.createConnection(rec, req)

			assertStatus(t, rec, http.StatusBadRequest)
			if !strings.Contains(rec.Body.String(), tt.wantMsg) {
				t.Errorf("Body %q does not contain %q", rec.Body.String(), tt.wantMsg)
			}
		})
	}

	var count int
	if err := f.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM connections").Scan(&count); err != nil {
		t.Fatalf("Count connections: %v", err)
	}
	if count != 0 {
		t.Errorf("Rejected requests left %d connection rows, want 0", count)
	}
}

// TestConnCov_CreateConnection_Success confirms that a permitted caller's
// connection is stored with the caller as owner and an encrypted password,
// and that the response omits the password.
func TestConnCov_CreateConnection_Success(t *testing.T) {
	f := newConnCovFixture(t, connCovSecret)
	admin := f.newCaller(t, "cov_create_ok", auth.PermManageConnections)

	desc := "primary reporting server"
	req := asCaller(connCovRequest(t, http.MethodPost, "/api/v1/connections", ConnectionCreateRequest{
		Name:         "reporting",
		Description:  &desc,
		Host:         connCovAllowedHost,
		Port:         5433,
		DatabaseName: "reports",
		Username:     "reporter",
		Password:     "s3cret-value",
		IsShared:     true,
		IsMonitored:  true,
	}), admin)
	rec := httptest.NewRecorder()
	f.handler.createConnection(rec, req)

	assertStatus(t, rec, http.StatusCreated)
	if strings.Contains(rec.Body.String(), "s3cret-value") {
		t.Errorf("Response leaks the password: %s", rec.Body.String())
	}
	var got connCovJSONConn
	decodeInto(t, rec, &got)
	if got.Name != "reporting" || got.Description != desc || got.Port != 5433 ||
		got.DatabaseName != "reports" || !got.IsShared || !got.IsMonitored {
		t.Errorf("Unexpected response body: %+v", got)
	}

	var owner, encrypted string
	if err := f.pool.QueryRow(context.Background(),
		"SELECT owner_username, password_encrypted FROM connections WHERE id = $1",
		got.ID).Scan(&owner, &encrypted); err != nil {
		t.Fatalf("Read back connection: %v", err)
	}
	if owner != admin.username {
		t.Errorf("owner_username = %q, want %q", owner, admin.username)
	}
	if encrypted == "" || encrypted == "s3cret-value" {
		t.Errorf("password_encrypted = %q, want a non-empty ciphertext", encrypted)
	}
}

// TestConnCov_CreateConnection_DatastoreError confirms that a datastore
// failure (here, no server secret to encrypt the password with) returns a
// generic 500 without detail.
func TestConnCov_CreateConnection_DatastoreError(t *testing.T) {
	f := newConnCovFixture(t, "")
	admin := f.newCaller(t, "cov_create_err", auth.PermManageConnections)

	req := asCaller(connCovRequest(t, http.MethodPost, "/api/v1/connections", ConnectionCreateRequest{
		Name:         "no-secret",
		Host:         connCovAllowedHost,
		Port:         5432,
		DatabaseName: "postgres",
		Username:     "app",
		Password:     "secret",
	}), admin)
	rec := httptest.NewRecorder()
	f.handler.createConnection(rec, req)

	assertError(t, rec, http.StatusInternalServerError, "Failed to create connection")
}

// =============================================================================
// getConnection
// =============================================================================

// TestConnCov_GetConnection covers the access check, the not-found path and
// a successful read.
func TestConnCov_GetConnection(t *testing.T) {
	f := newConnCovFixture(t, connCovSecret)
	owner := f.newCaller(t, "cov_get_owner")
	other := f.newCaller(t, "cov_get_other")
	id := f.seed(t, "private", owner.username, false)

	t.Run("owner reads", func(t *testing.T) {
		rec := f.serve(asCaller(connCovRequest(t, http.MethodGet, connCovPath(id, ""), nil), owner))
		assertStatus(t, rec, http.StatusOK)
		var got connCovJSONConn
		decodeInto(t, rec, &got)
		if got.ID != id || got.Name != "private" || got.Host != "db.example.com" {
			t.Errorf("Unexpected body: %+v", got)
		}
	})

	t.Run("non-owner of unshared connection denied", func(t *testing.T) {
		rec := f.serve(asCaller(connCovRequest(t, http.MethodGet, connCovPath(id, ""), nil), other))
		assertError(t, rec, http.StatusForbidden, "Access denied")
	})

	t.Run("missing connection", func(t *testing.T) {
		rec := f.serve(asSuperuserCaller(connCovRequest(t, http.MethodGet, connCovPath(id+1000, ""), nil), owner))
		assertError(t, rec, http.StatusNotFound, "Connection not found")
	})
}

// =============================================================================
// updateConnection
// =============================================================================

// TestConnCov_UpdateConnection_Rejections covers every refusal in
// updateConnection and confirms the row is left unchanged.
func TestConnCov_UpdateConnection_Rejections(t *testing.T) {
	f := newConnCovFixture(t, "")
	owner := f.newCaller(t, "cov_upd_owner")
	other := f.newCaller(t, "cov_upd_other")
	id := f.seed(t, "original", owner.username, false)

	shared := true
	badHost := "10.0.0.1"
	badPort := 25
	password := "new-password"

	tests := []struct {
		name       string
		caller     *connCovCaller
		id         int
		body       any
		scopeFail  bool
		wantStatus int
		wantMsg    string
	}{
		{"no bearer", nil, id, ConnectionFullUpdateRequest{}, false,
			http.StatusUnauthorized, "Invalid or missing authentication token"},
		{"out of token scope", &owner, id, ConnectionFullUpdateRequest{}, true,
			http.StatusForbidden, connectionOutOfTokenScope},
		{"missing connection", &owner, id + 1000, ConnectionFullUpdateRequest{}, false,
			http.StatusNotFound, "Connection not found"},
		{"non-owner without permission", &other, id, ConnectionFullUpdateRequest{}, false,
			http.StatusForbidden, "Permission denied: you must be the owner or have the manage_connections permission to update this connection"},
		{"malformed JSON", &owner, id, "{broken", false,
			http.StatusBadRequest, "Invalid request body"},
		{"owner cannot share", &owner, id, ConnectionFullUpdateRequest{IsShared: &shared}, false,
			http.StatusForbidden, "Permission denied: you do not have permission to make connections shared"},
		{"internal host", &owner, id, ConnectionFullUpdateRequest{Host: &badHost}, false,
			http.StatusBadRequest, "Invalid host"},
		{"blocked port", &owner, id, ConnectionFullUpdateRequest{Port: &badPort}, false,
			http.StatusBadRequest, "Invalid port"},
		{"datastore error", &owner, id, ConnectionFullUpdateRequest{Password: &password}, false,
			http.StatusInternalServerError, "Failed to update connection"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := connCovRequest(t, http.MethodPut, connCovPath(tt.id, ""), tt.body)
			if tt.caller != nil {
				req = asCaller(req, *tt.caller)
			}
			if tt.scopeFail {
				req = withIncompleteToken(req)
			}
			assertError(t, f.serve(req), tt.wantStatus, tt.wantMsg)
		})
	}

	var name string
	var isShared bool
	var host string
	var pw *string
	if err := f.pool.QueryRow(context.Background(),
		"SELECT name, is_shared, host, password_encrypted FROM connections WHERE id = $1",
		id).Scan(&name, &isShared, &host, &pw); err != nil {
		t.Fatalf("Read back connection: %v", err)
	}
	if name != "original" || isShared || host != "db.example.com" || pw != nil {
		t.Errorf("Rejected updates changed the row: name=%q shared=%v host=%q password set=%v",
			name, isShared, host, pw != nil)
	}
}

// TestConnCov_UpdateConnection_OwnerUpdates confirms that an owner without
// manage_connections may change their own connection.
func TestConnCov_UpdateConnection_OwnerUpdates(t *testing.T) {
	f := newConnCovFixture(t, connCovSecret)
	owner := f.newCaller(t, "cov_upd_ok_owner")
	id := f.seed(t, "before", owner.username, false)

	desc := "after description"
	port := 6432
	host := connCovAllowedHost
	notShared := false
	rec := f.serve(asCaller(connCovRequest(t, http.MethodPut, connCovPath(id, ""),
		ConnectionFullUpdateRequest{Description: &desc, Port: &port, Host: &host, IsShared: &notShared}), owner))

	assertStatus(t, rec, http.StatusOK)
	var got connCovJSONConn
	decodeInto(t, rec, &got)
	if got.Description != desc || got.Port != port {
		t.Errorf("Unexpected response: %+v", got)
	}
	var storedDesc string
	var storedPort int
	if err := f.pool.QueryRow(context.Background(),
		"SELECT description, port FROM connections WHERE id = $1", id).Scan(&storedDesc, &storedPort); err != nil {
		t.Fatalf("Read back: %v", err)
	}
	if storedDesc != desc || storedPort != port {
		t.Errorf("Stored description=%q port=%d, want %q %d", storedDesc, storedPort, desc, port)
	}
}

// TestConnCov_UpdateConnection_AdminSharesOthersConnection confirms that a
// caller holding manage_connections may update and share a connection they
// do not own.
func TestConnCov_UpdateConnection_AdminSharesOthersConnection(t *testing.T) {
	f := newConnCovFixture(t, connCovSecret)
	admin := f.newCaller(t, "cov_upd_admin", auth.PermManageConnections)
	id := f.seed(t, "someone-elses", "cov_upd_absent_owner", false)

	shared := true
	rec := f.serve(asCaller(connCovRequest(t, http.MethodPut, connCovPath(id, ""),
		ConnectionFullUpdateRequest{IsShared: &shared}), admin))

	assertStatus(t, rec, http.StatusOK)
	var isShared bool
	if err := f.pool.QueryRow(context.Background(),
		"SELECT is_shared FROM connections WHERE id = $1", id).Scan(&isShared); err != nil {
		t.Fatalf("Read back: %v", err)
	}
	if !isShared {
		t.Error("Connection was not shared")
	}
}

// =============================================================================
// deleteConnection
// =============================================================================

// TestConnCov_DeleteConnection_Rejections covers every refusal and failure
// in deleteConnection and confirms the row survives each.
func TestConnCov_DeleteConnection_Rejections(t *testing.T) {
	f := newConnCovFixture(t, connCovSecret)
	owner := f.newCaller(t, "cov_del_owner")
	other := f.newCaller(t, "cov_del_other")
	id := f.seed(t, "keep-me", owner.username, false)
	blocked := f.seed(t, "blocked", owner.username, false)
	f.exec(t, "INSERT INTO conn_cov_delete_blocker (connection_id) VALUES ($1)", blocked)

	tests := []struct {
		name       string
		caller     *connCovCaller
		id         int
		scopeFail  bool
		wantStatus int
		wantMsg    string
	}{
		{"no bearer", nil, id, false, http.StatusUnauthorized, "Invalid or missing authentication token"},
		{"out of token scope", &owner, id, true, http.StatusForbidden, connectionOutOfTokenScope},
		{"missing connection", &owner, id + 1000, false, http.StatusNotFound, "Connection not found"},
		{"non-owner without permission", &other, id, false, http.StatusForbidden,
			"Permission denied: you must be the owner or have the manage_connections permission to delete this connection"},
		{"datastore error", &owner, blocked, false, http.StatusInternalServerError, "Failed to delete connection"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := connCovRequest(t, http.MethodDelete, connCovPath(tt.id, ""), nil)
			if tt.caller != nil {
				req = asCaller(req, *tt.caller)
			}
			if tt.scopeFail {
				req = withIncompleteToken(req)
			}
			assertError(t, f.serve(req), tt.wantStatus, tt.wantMsg)
		})
	}

	if !f.rowExists(t, id) || !f.rowExists(t, blocked) {
		t.Error("A refused delete removed a connection row")
	}
}

// TestConnCov_DeleteConnection_Success confirms that both the owner and a
// caller holding manage_connections may delete a connection.
func TestConnCov_DeleteConnection_Success(t *testing.T) {
	f := newConnCovFixture(t, connCovSecret)
	owner := f.newCaller(t, "cov_del_ok_owner")
	admin := f.newCaller(t, "cov_del_ok_admin", auth.PermManageConnections)
	own := f.seed(t, "owned", owner.username, false)
	foreign := f.seed(t, "foreign", "cov_del_absent_owner", false)

	rec := f.serve(asCaller(connCovRequest(t, http.MethodDelete, connCovPath(own, ""), nil), owner))
	assertStatus(t, rec, http.StatusNoContent)
	rec = f.serve(asCaller(connCovRequest(t, http.MethodDelete, connCovPath(foreign, ""), nil), admin))
	assertStatus(t, rec, http.StatusNoContent)

	if f.rowExists(t, own) || f.rowExists(t, foreign) {
		t.Error("Deleted connection rows still exist")
	}
}

// =============================================================================
// listDatabases
// =============================================================================

// TestConnCov_ListDatabases covers the access check, a lookup failure and a
// real listing against the local test server.
func TestConnCov_ListDatabases(t *testing.T) {
	f := newConnCovFixture(t, connCovSecret)
	owner := f.newCaller(t, "cov_listdb_owner")
	other := f.newCaller(t, "cov_listdb_other")

	cfg, err := pgx.ParseConfig(f.connStr)
	if err != nil {
		t.Fatalf("Parse test connection string: %v", err)
	}
	// Only ever point the seeded connection at a loopback server.
	if ip := net.ParseIP(cfg.Host); cfg.Host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		t.Skipf("Test database host %q is not loopback; skipping live listing", cfg.Host)
	}
	var id int
	if err := f.pool.QueryRow(context.Background(), `
        INSERT INTO connections (name, description, host, port,
            database_name, username, owner_username, sslmode)
        VALUES ('local', '', $1, $2, $3, $4, $5, 'disable')
        RETURNING id
    `, cfg.Host, int(cfg.Port), cfg.Database, cfg.User, owner.username).Scan(&id); err != nil {
		t.Fatalf("Seed local connection: %v", err)
	}

	t.Run("non-owner denied", func(t *testing.T) {
		rec := f.serve(asCaller(connCovRequest(t, http.MethodGet, connCovPath(id, "databases"), nil), other))
		assertError(t, rec, http.StatusForbidden, "Access denied")
	})

	t.Run("missing connection", func(t *testing.T) {
		rec := f.serve(asSuperuserCaller(connCovRequest(t, http.MethodGet, connCovPath(id+1000, "databases"), nil), owner))
		assertError(t, rec, http.StatusInternalServerError, "Failed to list databases")
	})

	t.Run("owner lists databases", func(t *testing.T) {
		rec := f.serve(asCaller(connCovRequest(t, http.MethodGet, connCovPath(id, "databases"), nil), owner))
		assertStatus(t, rec, http.StatusOK)
		var dbs []database.DatabaseInfo
		decodeInto(t, rec, &dbs)
		found := false
		for _, d := range dbs {
			if d.Name == "template1" || d.Name == "template0" {
				t.Errorf("Listing includes template database %q", d.Name)
			}
			if d.Name == cfg.Database {
				found = true
			}
		}
		if !found {
			t.Errorf("Listing %+v does not include the test database %q", dbs, cfg.Database)
		}
	})
}

// =============================================================================
// Current connection selection
// =============================================================================

// TestConnCov_CurrentConnection_Lifecycle sets, reads and clears the
// current connection for a token, checking the stored selection at each
// step.
func TestConnCov_CurrentConnection_Lifecycle(t *testing.T) {
	f := newConnCovFixture(t, connCovSecret)
	owner := f.newCaller(t, "cov_current_owner")
	id := f.seed(t, "selected", owner.username, false)

	call := func(method string, body any) *httptest.ResponseRecorder {
		req := asCaller(connCovRequest(t, method, "/api/v1/connections/current", body), owner)
		rec := httptest.NewRecorder()
		f.handler.handleCurrentConnection(rec, req)
		return rec
	}

	assertError(t, call(http.MethodGet, nil), http.StatusNotFound, "No database connection selected")

	dbName := "analytics"
	rec := call(http.MethodPost, CurrentConnectionRequest{ConnectionID: id, DatabaseName: &dbName})
	assertStatus(t, rec, http.StatusOK)
	var set CurrentConnectionResponse
	decodeInto(t, rec, &set)
	if set.ConnectionID != id || set.Name != "selected" || set.Host != "db.example.com" ||
		set.Port != 5432 || set.DatabaseName == nil || *set.DatabaseName != dbName {
		t.Errorf("Unexpected set response: %+v", set)
	}

	rec = call(http.MethodGet, nil)
	assertStatus(t, rec, http.StatusOK)
	var got CurrentConnectionResponse
	decodeInto(t, rec, &got)
	if got.ConnectionID != id || got.Name != "selected" || got.DatabaseName == nil || *got.DatabaseName != dbName {
		t.Errorf("Unexpected get response: %+v", got)
	}

	assertStatus(t, call(http.MethodDelete, nil), http.StatusNoContent)
	session, err := f.store.GetConnectionSession(auth.GetTokenHashByRawToken(owner.token))
	if err != nil {
		t.Fatalf("GetConnectionSession: %v", err)
	}
	if session != nil {
		t.Errorf("Session still set after DELETE: %+v", session)
	}
	assertError(t, call(http.MethodGet, nil), http.StatusNotFound, "No database connection selected")
}

// TestConnCov_SetCurrentConnection_Rejections covers the refusals when
// selecting a connection, and confirms none of them stores a selection.
func TestConnCov_SetCurrentConnection_Rejections(t *testing.T) {
	f := newConnCovFixture(t, connCovSecret)
	owner := f.newCaller(t, "cov_setcur_owner")
	other := f.newCaller(t, "cov_setcur_other")
	id := f.seed(t, "not-yours", owner.username, false)

	tests := []struct {
		name       string
		caller     connCovCaller
		superuser  bool
		body       any
		wantStatus int
		wantMsg    string
	}{
		{"malformed JSON", other, false, "{nope", http.StatusBadRequest, "Invalid request body"},
		{"missing connection_id", other, false, CurrentConnectionRequest{}, http.StatusBadRequest, "connection_id is required"},
		{"inaccessible connection", other, false, CurrentConnectionRequest{ConnectionID: id}, http.StatusForbidden, "Access denied"},
		{"missing connection", other, true, CurrentConnectionRequest{ConnectionID: id + 1000}, http.StatusBadRequest, "Connection not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := connCovRequest(t, http.MethodPost, "/api/v1/connections/current", tt.body)
			if tt.superuser {
				req = asSuperuserCaller(req, tt.caller)
			} else {
				req = asCaller(req, tt.caller)
			}
			rec := httptest.NewRecorder()
			f.handler.handleCurrentConnection(rec, req)
			assertError(t, rec, tt.wantStatus, tt.wantMsg)
		})
	}

	session, err := f.store.GetConnectionSession(auth.GetTokenHashByRawToken(other.token))
	if err != nil {
		t.Fatalf("GetConnectionSession: %v", err)
	}
	if session != nil {
		t.Errorf("A refused selection was stored: %+v", session)
	}
}

// TestConnCov_GetCurrentConnection_RechecksAccess confirms that a stored
// selection is re-checked on read: a caller who can no longer reach the
// connection is refused, and a selection whose connection has gone
// reports a server error.
func TestConnCov_GetCurrentConnection_RechecksAccess(t *testing.T) {
	f := newConnCovFixture(t, connCovSecret)
	owner := f.newCaller(t, "cov_getcur_owner")
	other := f.newCaller(t, "cov_getcur_other")
	id := f.seed(t, "owners-only", owner.username, false)

	get := func(req *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		f.handler.handleCurrentConnection(rec, req)
		return rec
	}

	otherHash := auth.GetTokenHashByRawToken(other.token)
	if err := f.store.SetConnectionSession(otherHash, id, nil); err != nil {
		t.Fatalf("SetConnectionSession: %v", err)
	}
	rec := get(asCaller(connCovRequest(t, http.MethodGet, "/api/v1/connections/current", nil), other))
	assertError(t, rec, http.StatusForbidden, "Access denied")

	ownerHash := auth.GetTokenHashByRawToken(owner.token)
	if err := f.store.SetConnectionSession(ownerHash, id+1000, nil); err != nil {
		t.Fatalf("SetConnectionSession: %v", err)
	}
	rec = get(asSuperuserCaller(connCovRequest(t, http.MethodGet, "/api/v1/connections/current", nil), owner))
	assertError(t, rec, http.StatusInternalServerError, "Failed to get connection details")
}

// TestConnCov_CurrentConnection_StoreErrors confirms that a failing auth
// store yields a generic 500 from each current-connection method.
func TestConnCov_CurrentConnection_StoreErrors(t *testing.T) {
	f := newConnCovFixture(t, connCovSecret)
	admin := f.newCaller(t, "cov_curerr_admin")
	id := f.seed(t, "exists", admin.username, false)

	// Closing the store makes every subsequent SQLite call fail. The
	// fixture's cleanup closes it again, which is harmless.
	if err := f.store.Close(); err != nil {
		t.Fatalf("Close auth store: %v", err)
	}

	tests := []struct {
		method  string
		body    any
		wantMsg string
	}{
		{http.MethodGet, nil, "Failed to get current connection"},
		{http.MethodPost, CurrentConnectionRequest{ConnectionID: id}, "Failed to save connection selection"},
		{http.MethodDelete, nil, "Failed to clear connection selection"},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			req := asSuperuserCaller(connCovRequest(t, tt.method, "/api/v1/connections/current", tt.body), admin)
			rec := httptest.NewRecorder()
			f.handler.handleCurrentConnection(rec, req)
			assertError(t, rec, http.StatusInternalServerError, tt.wantMsg)
		})
	}
}

// =============================================================================
// getConnectionContext
// =============================================================================

// TestConnCov_GetConnectionContext covers the access check, the not-found
// path and a successful read with no collected metrics.
func TestConnCov_GetConnectionContext(t *testing.T) {
	f := newConnCovFixture(t, connCovSecret)
	owner := f.newCaller(t, "cov_ctx_owner")
	other := f.newCaller(t, "cov_ctx_other")
	id := f.seed(t, "context-server", owner.username, false)

	t.Run("non-owner denied", func(t *testing.T) {
		rec := f.serve(asCaller(connCovRequest(t, http.MethodGet, connCovPath(id, "context"), nil), other))
		assertError(t, rec, http.StatusForbidden, "Access denied")
	})

	t.Run("missing connection", func(t *testing.T) {
		rec := f.serve(asSuperuserCaller(connCovRequest(t, http.MethodGet, connCovPath(id+1000, "context"), nil), owner))
		assertError(t, rec, http.StatusNotFound, "Connection not found")
	})

	t.Run("owner reads context", func(t *testing.T) {
		rec := f.serve(asCaller(connCovRequest(t, http.MethodGet, connCovPath(id, "context"), nil), owner))
		assertStatus(t, rec, http.StatusOK)
		var got database.ConnectionContext
		decodeInto(t, rec, &got)
		if got.ConnectionID != id || got.ServerName != "context-server" {
			t.Errorf("Unexpected context: %+v", got)
		}
	})
}

// =============================================================================
// Connection cluster endpoints
// =============================================================================

// connCovClusterResponse mirrors connectionClusterResponse for decoding.
type connCovClusterResponse struct {
	Info          *database.ConnectionClusterInfo `json:"info"`
	Clusters      []database.ClusterSummary       `json:"clusters"`
	Relationships []database.NodeRelationship     `json:"relationships"`
}

// TestConnCov_GetConnectionCluster covers the access check, the not-found
// path, an unassigned connection and an assigned connection whose
// relationships are filtered to those touching it.
func TestConnCov_GetConnectionCluster(t *testing.T) {
	f := newConnCovFixture(t, connCovSecret)
	owner := f.newCaller(t, "cov_getcl_owner")
	other := f.newCaller(t, "cov_getcl_other")

	clusterID := f.seedCluster(t, "alpha", false)
	f.seedCluster(t, "retired", true)
	a := f.seed(t, "node-a", owner.username, false)
	b := f.seed(t, "node-b", owner.username, false)
	c := f.seed(t, "node-c", owner.username, false)
	unassigned := f.seed(t, "loner", owner.username, false)
	f.exec(t, "UPDATE connections SET cluster_id = $1, role = 'primary' WHERE id IN ($2, $3, $4)",
		clusterID, a, b, c)
	f.exec(t, `INSERT INTO cluster_node_relationships
        (cluster_id, source_connection_id, target_connection_id, relationship_type)
        VALUES ($1, $2, $3, 'streams_from'), ($1, $3, $4, 'streams_from')`,
		clusterID, a, b, c)

	get := func(caller connCovCaller, id int, superuser bool) *httptest.ResponseRecorder {
		req := connCovRequest(t, http.MethodGet, connCovPath(id, "cluster"), nil)
		if superuser {
			return f.serve(asSuperuserCaller(req, caller))
		}
		return f.serve(asCaller(req, caller))
	}

	t.Run("non-owner denied", func(t *testing.T) {
		assertError(t, get(other, a, false), http.StatusForbidden, "Access denied")
	})

	t.Run("missing connection", func(t *testing.T) {
		assertError(t, get(owner, a+1000, true), http.StatusNotFound, "Connection not found")
	})

	t.Run("unassigned connection", func(t *testing.T) {
		rec := get(owner, unassigned, false)
		assertStatus(t, rec, http.StatusOK)
		var got connCovClusterResponse
		decodeInto(t, rec, &got)
		if got.Info == nil || got.Info.ClusterID != nil {
			t.Errorf("Info = %+v, want no cluster", got.Info)
		}
		if got.Relationships == nil || len(got.Relationships) != 0 {
			t.Errorf("Relationships = %v, want an empty list", got.Relationships)
		}
		if len(got.Clusters) != 1 || got.Clusters[0].Name != "alpha" {
			t.Errorf("Clusters = %+v, want only the undismissed cluster", got.Clusters)
		}
	})

	t.Run("assigned connection", func(t *testing.T) {
		rec := get(owner, a, false)
		assertStatus(t, rec, http.StatusOK)
		var got connCovClusterResponse
		decodeInto(t, rec, &got)
		if got.Info == nil || got.Info.ClusterID == nil || *got.Info.ClusterID != clusterID ||
			got.Info.ClusterName == nil || *got.Info.ClusterName != "alpha" {
			t.Errorf("Info = %+v, want cluster alpha", got.Info)
		}
		if len(got.Relationships) != 1 || got.Relationships[0].SourceConnectionID != a ||
			got.Relationships[0].TargetConnectionID != b {
			t.Errorf("Relationships = %+v, want only node-a -> node-b", got.Relationships)
		}
	})

	t.Run("relationship lookup failure is tolerated", func(t *testing.T) {
		f.exec(t, "DROP TABLE cluster_node_relationships")
		rec := get(owner, a, false)
		assertStatus(t, rec, http.StatusOK)
		var got connCovClusterResponse
		decodeInto(t, rec, &got)
		if got.Relationships == nil || len(got.Relationships) != 0 {
			t.Errorf("Relationships = %v, want an empty list", got.Relationships)
		}
	})

	t.Run("cluster list failure", func(t *testing.T) {
		f.exec(t, "ALTER TABLE clusters DROP COLUMN dismissed")
		assertError(t, get(owner, a, false), http.StatusInternalServerError, "Failed to list clusters")
	})
}

// TestConnCov_UpdateConnectionCluster_Gates covers the visibility check,
// the manage_connections gate and body decoding, confirming none of them
// changes the row.
func TestConnCov_UpdateConnectionCluster_Gates(t *testing.T) {
	f := newConnCovFixture(t, connCovSecret)
	owner := f.newCaller(t, "cov_updcl_owner")
	admin := f.newCaller(t, "cov_updcl_admin", auth.PermManageConnections)
	clusterID := f.seedCluster(t, "target", false)
	id := f.seed(t, "movable", owner.username, true)

	body := ConnectionClusterUpdateRequest{ClusterID: &clusterID, MembershipSource: "manual"}

	t.Run("out of token scope", func(t *testing.T) {
		req := withIncompleteToken(asCaller(connCovRequest(t, http.MethodPut, connCovPath(id, "cluster"), body), admin))
		assertError(t, f.serve(req), http.StatusForbidden, "Access denied")
	})

	t.Run("visible but lacking manage_connections", func(t *testing.T) {
		req := asCaller(connCovRequest(t, http.MethodPut, connCovPath(id, "cluster"), body), owner)
		assertError(t, f.serve(req), http.StatusForbidden,
			"Permission denied: requires manage_connections permission")
	})

	t.Run("malformed JSON", func(t *testing.T) {
		req := asCaller(connCovRequest(t, http.MethodPut, connCovPath(id, "cluster"), "{bad"), admin)
		assertStatus(t, f.serve(req), http.StatusBadRequest)
	})

	var cluster *int
	if err := f.pool.QueryRow(context.Background(),
		"SELECT cluster_id FROM connections WHERE id = $1", id).Scan(&cluster); err != nil {
		t.Fatalf("Read back: %v", err)
	}
	if cluster != nil {
		t.Errorf("Refused requests assigned the connection to cluster %d", *cluster)
	}
}

// TestConnCov_UpdateConnectionCluster_Changes covers the assign and reset
// branches and checks the stored membership after each.
func TestConnCov_UpdateConnectionCluster_Changes(t *testing.T) {
	f := newConnCovFixture(t, connCovSecret)
	admin := f.newCaller(t, "cov_updcl_changes", auth.PermManageConnections)
	clusterID := f.seedCluster(t, "home", false)
	id := f.seed(t, "member", admin.username, true)

	put := func(body any) *httptest.ResponseRecorder {
		return f.serve(asCaller(connCovRequest(t, http.MethodPut, connCovPath(id, "cluster"), body), admin))
	}
	stored := func() (cluster *int, role *string, source string) {
		t.Helper()
		if err := f.pool.QueryRow(context.Background(),
			"SELECT cluster_id, role, membership_source FROM connections WHERE id = $1",
			id).Scan(&cluster, &role, &source); err != nil {
			t.Fatalf("Read back: %v", err)
		}
		return cluster, role, source
	}

	role := "primary"
	rec := put(ConnectionClusterUpdateRequest{ClusterID: &clusterID, Role: &role, MembershipSource: "manual"})
	assertStatus(t, rec, http.StatusOK)
	var info database.ConnectionClusterInfo
	decodeInto(t, rec, &info)
	if info.ClusterID == nil || *info.ClusterID != clusterID || info.MembershipSource != "manual" ||
		info.Role == nil || *info.Role != role {
		t.Errorf("Assign response = %+v", info)
	}
	if c, r, s := stored(); c == nil || *c != clusterID || r == nil || *r != role || s != "manual" {
		t.Errorf("Stored after assign: cluster=%v role=%v source=%q", c, r, s)
	}

	// A cluster with no membership source defaults to auto.
	rec = put(ConnectionClusterUpdateRequest{ClusterID: &clusterID})
	assertStatus(t, rec, http.StatusOK)
	if _, _, s := stored(); s != "auto" {
		t.Errorf("membership_source = %q, want auto", s)
	}

	// No cluster and a non-manual source resets to auto-detection,
	// leaving the cluster assignment for the collector to revisit.
	f.exec(t, "UPDATE connections SET membership_source = 'manual' WHERE id = $1", id)
	rec = put(ConnectionClusterUpdateRequest{})
	assertStatus(t, rec, http.StatusOK)
	decodeInto(t, rec, &info)
	if info.MembershipSource != "auto" {
		t.Errorf("Reset response membership_source = %q, want auto", info.MembershipSource)
	}
	if c, _, s := stored(); s != "auto" || c == nil || *c != clusterID {
		t.Errorf("Stored after reset: cluster=%v source=%q", c, s)
	}

	// No cluster with a manual source pins the connection outside any
	// cluster.
	rec = put(ConnectionClusterUpdateRequest{MembershipSource: "manual"})
	assertStatus(t, rec, http.StatusOK)
	if c, _, s := stored(); c != nil || s != "manual" {
		t.Errorf("Stored after manual unassign: cluster=%v source=%q", c, s)
	}
}

// TestConnCov_UpdateConnectionCluster_Failures covers the datastore
// failures in each branch.
func TestConnCov_UpdateConnectionCluster_Failures(t *testing.T) {
	f := newConnCovFixture(t, connCovSecret)
	admin := f.newCaller(t, "cov_updcl_fail")
	clusterID := f.seedCluster(t, "anywhere", false)
	id := f.seed(t, "exists", admin.username, false)
	missing := id + 1000

	put := func(id int, body any) *httptest.ResponseRecorder {
		return f.serve(asSuperuserCaller(connCovRequest(t, http.MethodPut, connCovPath(id, "cluster"), body), admin))
	}

	assertError(t, put(missing, ConnectionClusterUpdateRequest{}),
		http.StatusInternalServerError, "Failed to reset membership source")
	assertError(t, put(missing, ConnectionClusterUpdateRequest{ClusterID: &clusterID}),
		http.StatusInternalServerError, "Failed to assign connection to cluster")

	// With the clusters table gone the reset still succeeds, but reading
	// back the joined cluster info fails.
	f.exec(t, "DROP TABLE clusters CASCADE")
	assertError(t, put(id, ConnectionClusterUpdateRequest{}),
		http.StatusInternalServerError, "Failed to get updated cluster info")
}
