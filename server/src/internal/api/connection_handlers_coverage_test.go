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
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/pkg/crypto"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// =============================================================================
// Handler-level coverage for connection_handlers.go
//
// These tests drive every ConnectionHandler endpoint through its success
// and error branches against a real datastore (the Postgres named by
// TEST_AI_WORKBENCH_SERVER) and a real auth store, so the RBAC gates,
// the datastore calls and the session store all run unmocked.
// =============================================================================

// connCoverageSchema carries every column and table the connection
// handlers touch: the full connections row read by GetConnection and
// written by CreateConnection/UpdateConnectionFull, the cluster columns
// read and written by the cluster endpoints, and the relationship table
// GetClusterRelationships joins.
const connCoverageSchema = `
DROP TABLE IF EXISTS cluster_node_relationships CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
CREATE TABLE clusters (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    replication_type VARCHAR(50),
    auto_cluster_key VARCHAR(255),
    dismissed BOOLEAN NOT NULL DEFAULT FALSE
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
`

const (
	covSecret   = "connection-coverage-test-secret-0123456789"
	covAllowed  = "db.example.test"
	covBlocked  = "blocked.example.test"
	covOwner    = "conncov_owner"
	covAdmin    = "conncov_admin"
	covOther    = "conncov_other"
	covConnID   = 8100
	covMissing  = 8999
	covPassword = "Password1234"
)

// connCoverageUser is an authenticated caller: its session token for
// getUserInfoCompat, and its ID and name for the RBAC context.
type connCoverageUser struct {
	id          int64
	name        string
	token       string
	isSuperuser bool
}

// connCoverageHarness wires the handler to a real datastore and auth
// store and seeds one connection (covConnID) owned by covOwner.
type connCoverageHarness struct {
	t       *testing.T
	handler *ConnectionHandler
	ds      *database.Datastore
	pool    *pgxpool.Pool
	store   *auth.AuthStore
	owner   connCoverageUser
	admin   connCoverageUser
	other   connCoverageUser
}

func newConnCoverageHarness(t *testing.T) *connCoverageHarness {
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
	if _, err := pool.Exec(ctx, connCoverageSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create connection coverage schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `
            DROP TABLE IF EXISTS cluster_node_relationships CASCADE;
            DROP TABLE IF EXISTS connections CASCADE;
            DROP TABLE IF EXISTS clusters CASCADE;
            DROP FUNCTION IF EXISTS conncov_vanish() CASCADE;`)
		pool.Close()
	})

	_, store, cleanupStore := createTestRBACHandler(t)
	t.Cleanup(cleanupStore)

	h := &connCoverageHarness{
		t:     t,
		ds:    database.NewTestDatastoreWithSecret(pool, covSecret),
		pool:  pool,
		store: store,
	}
	h.owner = h.newUser(covOwner, nil)
	h.admin = h.newUser(covAdmin, []string{auth.PermManageConnections})
	h.other = h.newUser(covOther, nil)

	// Ownership and sharing decide access to an ungrouped connection;
	// production wires this lookup to the datastore, and so do we.
	checker := auth.NewRBACChecker(store)
	checker.SetConnectionSharingLookup(func(ctx context.Context, id int) (bool, string, error) {
		var shared bool
		var owner *string
		err := pool.QueryRow(ctx,
			`SELECT is_shared, owner_username FROM connections WHERE id = $1`, id,
		).Scan(&shared, &owner)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, "", nil
		}
		if err != nil {
			return false, "", err
		}
		if owner == nil {
			return shared, "", nil
		}
		return shared, *owner, nil
	})
	h.handler = NewConnectionHandlerWithSecurity(h.ds, store, checker, true,
		[]string{covAllowed}, []string{covBlocked})

	seedIssue269Connection(t, pool, covConnID, covOwner, "coverage-target")
	return h
}

func (h *connCoverageHarness) newUser(name string, perms []string) connCoverageUser {
	h.t.Helper()
	id := setupUserWithPermissions(h.t, h.store, name, perms)
	token, _, err := h.store.AuthenticateUser(name, covPassword)
	if err != nil {
		h.t.Fatalf("AuthenticateUser(%s): %v", name, err)
	}
	return connCoverageUser{id: id, name: name, token: token}
}

// superuser returns a caller that passes every RBAC gate. It reuses the
// admin's session token so getUserInfoCompat still resolves a user.
func (h *connCoverageHarness) superuser() connCoverageUser {
	u := h.admin
	u.isSuperuser = true
	return u
}

// request builds a request as the given caller. A nil body sends none;
// a string is sent verbatim; anything else is JSON-encoded.
func (h *connCoverageHarness) request(method, path string, body any, u *connCoverageUser) *http.Request {
	h.t.Helper()
	var reader *bytes.Reader
	switch b := body.(type) {
	case nil:
		reader = bytes.NewReader(nil)
	case string:
		reader = bytes.NewReader([]byte(b))
	default:
		data, err := json.Marshal(b)
		if err != nil {
			h.t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if u == nil {
		return req
	}
	req = withBearer(req, u.token)
	if u.isSuperuser {
		return withUsername(withSuperuser(req), u.name)
	}
	return withUsername(withUser(req, u.id), u.name)
}

// serve dispatches through the handler's registered routes so the
// routing switches run as they do in production.
func (h *connCoverageHarness) serve(req *http.Request) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	h.handler.RegisterRoutes(mux, func(f http.HandlerFunc) http.HandlerFunc { return f })
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func (h *connCoverageHarness) exec(sql string, args ...any) {
	h.t.Helper()
	if _, err := h.pool.Exec(context.Background(), sql, args...); err != nil {
		h.t.Fatalf("exec %q: %v", sql, err)
	}
}

func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d. Body: %s", rec.Code, want, rec.Body.String())
	}
}

func assertErrorMessage(t *testing.T, rec *httptest.ResponseRecorder, want string) {
	t.Helper()
	var resp ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode error response: %v. Body: %s", err, rec.Body.String())
	}
	if resp.Error != want {
		t.Fatalf("error = %q, want %q", resp.Error, want)
	}
}

// TestConnectionCoverage_Routing covers RegisterRoutes with a configured
// datastore and the dispatch switches of handleConnections and
// handleConnectionSubpath.
func TestConnectionCoverage_Routing(t *testing.T) {
	h := newConnCoverageHarness(t)
	su := h.superuser()

	tests := []struct {
		name   string
		method string
		path   string
		want   int
		allow  string
	}{
		{"list connections", http.MethodGet, "/api/v1/connections", http.StatusOK, ""},
		{"collection bad method", http.MethodPatch, "/api/v1/connections", http.StatusMethodNotAllowed, "GET, POST"},
		{"empty subpath", http.MethodGet, "/api/v1/connections/", http.StatusNotFound, ""},
		{"invalid id", http.MethodGet, "/api/v1/connections/abc", http.StatusBadRequest, ""},
		{"get by id", http.MethodGet, "/api/v1/connections/8100", http.StatusOK, ""},
		{"id bad method", http.MethodPatch, "/api/v1/connections/8100", http.StatusMethodNotAllowed, "GET, PUT, DELETE"},
		{"databases bad method", http.MethodPost, "/api/v1/connections/8100/databases", http.StatusMethodNotAllowed, "GET"},
		{"query bad method", http.MethodGet, "/api/v1/connections/8100/query", http.StatusMethodNotAllowed, "POST"},
		{"context", http.MethodGet, "/api/v1/connections/8100/context", http.StatusOK, ""},
		{"context bad method", http.MethodPost, "/api/v1/connections/8100/context", http.StatusMethodNotAllowed, "GET"},
		{"cluster", http.MethodGet, "/api/v1/connections/8100/cluster", http.StatusOK, ""},
		{"cluster bad method", http.MethodDelete, "/api/v1/connections/8100/cluster", http.StatusMethodNotAllowed, "GET, PUT"},
		{"unknown suffix", http.MethodGet, "/api/v1/connections/8100/bogus", http.StatusNotFound, ""},
		{"current via mux", http.MethodGet, "/api/v1/connections/current", http.StatusNotFound, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := h.serve(h.request(tt.method, tt.path, nil, &su))
			assertStatus(t, rec, tt.want)
			if tt.allow != "" && rec.Header().Get("Allow") != tt.allow {
				t.Errorf("Allow = %q, want %q", rec.Header().Get("Allow"), tt.allow)
			}
		})
	}

	t.Run("current via subpath dispatcher", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.handler.handleConnectionSubpath(rec,
			h.request(http.MethodGet, "/api/v1/connections/current", nil, &su))
		assertStatus(t, rec, http.StatusNotFound)
		assertErrorMessage(t, rec, "No database connection selected")
	})

	t.Run("put and delete dispatch", func(t *testing.T) {
		name := "renamed-by-dispatch"
		rec := h.serve(h.request(http.MethodPut, "/api/v1/connections/8100",
			ConnectionFullUpdateRequest{Name: &name}, &su))
		assertStatus(t, rec, http.StatusOK)

		rec = h.serve(h.request(http.MethodPut, "/api/v1/connections/8100/cluster",
			ConnectionClusterUpdateRequest{}, &su))
		assertStatus(t, rec, http.StatusOK)

		rec = h.serve(h.request(http.MethodDelete, "/api/v1/connections/8100", nil, &su))
		assertStatus(t, rec, http.StatusNoContent)
	})
}

// validCoverageCreate returns a create request that passes every check
// in createConnection.
func validCoverageCreate() ConnectionCreateRequest {
	desc := "created by coverage test"
	return ConnectionCreateRequest{
		Name:         "coverage-created",
		Description:  &desc,
		Host:         covAllowed,
		Port:         5432,
		DatabaseName: "postgres",
		Username:     "app",
		Password:     "secret",
	}
}

func TestConnectionCoverage_CreateConnection(t *testing.T) {
	h := newConnCoverageHarness(t)
	long := strings.Repeat("x", maxFieldLength+1)

	validation := []struct {
		name   string
		mutate func(*ConnectionCreateRequest)
		want   string
	}{
		{"invalid name", func(r *ConnectionCreateRequest) { r.Name = "" }, ""},
		{"missing host", func(r *ConnectionCreateRequest) { r.Host = "" }, "Host is required"},
		{"zero port", func(r *ConnectionCreateRequest) { r.Port = 0 }, "Port is required and must be positive"},
		{"missing database", func(r *ConnectionCreateRequest) { r.DatabaseName = "" }, "Maintenance Database is required"},
		{"missing username", func(r *ConnectionCreateRequest) { r.Username = "" }, "Username is required"},
		{"missing password", func(r *ConnectionCreateRequest) { r.Password = "" }, "Password is required"},
		{"host too long", func(r *ConnectionCreateRequest) { r.Host = long }, ""},
		{"database too long", func(r *ConnectionCreateRequest) { r.DatabaseName = long }, ""},
		{"username too long", func(r *ConnectionCreateRequest) { r.Username = long }, ""},
		{"blocked host", func(r *ConnectionCreateRequest) { r.Host = covBlocked }, "Invalid host"},
		{"blocked port", func(r *ConnectionCreateRequest) { r.Port = 22 }, "Invalid port"},
	}
	for _, tt := range validation {
		t.Run(tt.name, func(t *testing.T) {
			body := validCoverageCreate()
			tt.mutate(&body)
			rec := h.serve(h.request(http.MethodPost, "/api/v1/connections", body, &h.admin))
			assertStatus(t, rec, http.StatusBadRequest)
			if tt.want != "" {
				assertErrorMessage(t, rec, tt.want)
			}
		})
	}

	t.Run("invalid JSON", func(t *testing.T) {
		rec := h.serve(h.request(http.MethodPost, "/api/v1/connections", "{not json", &h.admin))
		assertStatus(t, rec, http.StatusBadRequest)
	})

	t.Run("missing authentication", func(t *testing.T) {
		rec := h.serve(h.request(http.MethodPost, "/api/v1/connections", validCoverageCreate(), nil))
		assertStatus(t, rec, http.StatusUnauthorized)
	})

	t.Run("without manage_connections", func(t *testing.T) {
		rec := h.serve(h.request(http.MethodPost, "/api/v1/connections", validCoverageCreate(), &h.other))
		assertStatus(t, rec, http.StatusForbidden)
	})

	t.Run("success", func(t *testing.T) {
		rec := h.serve(h.request(http.MethodPost, "/api/v1/connections", validCoverageCreate(), &h.admin))
		assertStatus(t, rec, http.StatusCreated)
		var owner string
		var encrypted *string
		if err := h.pool.QueryRow(context.Background(), `
            SELECT owner_username, password_encrypted FROM connections
            WHERE name = 'coverage-created'`).Scan(&owner, &encrypted); err != nil {
			t.Fatalf("reading created row: %v", err)
		}
		if owner != covAdmin {
			t.Fatalf("owner_username = %q, want %q", owner, covAdmin)
		}
		if encrypted == nil || *encrypted == "" || *encrypted == "secret" {
			t.Fatalf("password not stored encrypted: %v", encrypted)
		}
	})

	t.Run("datastore failure", func(t *testing.T) {
		// Without a server secret the datastore cannot encrypt the
		// password and refuses the insert.
		h.handler.datastore = database.NewTestDatastore(h.pool)
		defer func() { h.handler.datastore = h.ds }()
		rec := h.serve(h.request(http.MethodPost, "/api/v1/connections", validCoverageCreate(), &h.admin))
		assertStatus(t, rec, http.StatusInternalServerError)
		assertErrorMessage(t, rec, "Failed to create connection")
	})
}

func TestConnectionCoverage_GetConnection(t *testing.T) {
	h := newConnCoverageHarness(t)

	t.Run("owner", func(t *testing.T) {
		rec := h.serve(h.request(http.MethodGet, "/api/v1/connections/8100", nil, &h.owner))
		assertStatus(t, rec, http.StatusOK)
	})
	t.Run("non-owner denied", func(t *testing.T) {
		rec := h.serve(h.request(http.MethodGet, "/api/v1/connections/8100", nil, &h.other))
		assertStatus(t, rec, http.StatusForbidden)
	})
	t.Run("not found", func(t *testing.T) {
		su := h.superuser()
		rec := h.serve(h.request(http.MethodGet, "/api/v1/connections/8999", nil, &su))
		assertStatus(t, rec, http.StatusNotFound)
	})
}

func TestConnectionCoverage_UpdateConnection(t *testing.T) {
	h := newConnCoverageHarness(t)
	const path = "/api/v1/connections/8100"
	str := func(s string) *string { return &s }
	num := func(n int) *int { return &n }
	yes := true

	tests := []struct {
		name string
		user *connCoverageUser
		path string
		body any
		want int
		msg  string
	}{
		{"missing authentication", nil, path, ConnectionFullUpdateRequest{}, http.StatusUnauthorized, ""},
		{"not found", &h.admin, "/api/v1/connections/8999", ConnectionFullUpdateRequest{}, http.StatusNotFound, "Connection not found"},
		{"non-owner forbidden", &h.other, path, ConnectionFullUpdateRequest{}, http.StatusForbidden, ""},
		{"invalid JSON", &h.owner, path, "{not json", http.StatusBadRequest, "Invalid request body"},
		{"invalid name", &h.owner, path, ConnectionFullUpdateRequest{Name: str("")}, http.StatusBadRequest, ""},
		{"owner cannot share", &h.owner, path, ConnectionFullUpdateRequest{IsShared: &yes}, http.StatusForbidden,
			"Permission denied: you do not have permission to make connections shared"},
		{"blocked host", &h.owner, path, ConnectionFullUpdateRequest{Host: str(covBlocked)}, http.StatusBadRequest, "Invalid host"},
		{"blocked port", &h.owner, path, ConnectionFullUpdateRequest{Port: num(25)}, http.StatusBadRequest, "Invalid port"},
		{"datastore failure", &h.owner, path,
			ConnectionFullUpdateRequest{Username: str(strings.Repeat("u", 300))},
			http.StatusInternalServerError, "Failed to update connection"},
		{"owner updates", &h.owner, path,
			ConnectionFullUpdateRequest{Name: str("owner-renamed"), Host: str(covAllowed), Port: num(5433)},
			http.StatusOK, ""},
		{"admin shares", &h.admin, path, ConnectionFullUpdateRequest{IsShared: &yes}, http.StatusOK, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := h.serve(h.request(http.MethodPut, tt.path, tt.body, tt.user))
			assertStatus(t, rec, tt.want)
			if tt.msg != "" {
				assertErrorMessage(t, rec, tt.msg)
			}
		})
	}

	var name string
	var port int
	var shared bool
	if err := h.pool.QueryRow(context.Background(),
		`SELECT name, port, is_shared FROM connections WHERE id = $1`, covConnID,
	).Scan(&name, &port, &shared); err != nil {
		t.Fatalf("reading updated row: %v", err)
	}
	if name != "owner-renamed" || port != 5433 || !shared {
		t.Fatalf("row not updated: name=%q port=%d shared=%v", name, port, shared)
	}
}

func TestConnectionCoverage_ListDatabases(t *testing.T) {
	h := newConnCoverageHarness(t)
	su := h.superuser()

	t.Run("denied", func(t *testing.T) {
		rec := h.serve(h.request(http.MethodGet, "/api/v1/connections/8100/databases", nil, &h.other))
		assertStatus(t, rec, http.StatusForbidden)
	})

	t.Run("datastore failure", func(t *testing.T) {
		rec := h.serve(h.request(http.MethodGet, "/api/v1/connections/8999/databases", nil, &su))
		assertStatus(t, rec, http.StatusInternalServerError)
		assertErrorMessage(t, rec, "Failed to list databases")
	})

	t.Run("success", func(t *testing.T) {
		// Point a connection at the test Postgres itself so the
		// handler can list its databases.
		cfg, err := pgx.ParseConfig(os.Getenv("TEST_AI_WORKBENCH_SERVER"))
		if err != nil {
			t.Fatalf("parse test DSN: %v", err)
		}
		// CI authenticates with a password; store it encrypted the
		// way CreateConnection would, so ListDatabases decrypts it.
		var encrypted *string
		if cfg.Password != "" {
			enc, err := crypto.EncryptPassword(cfg.Password, covSecret)
			if err != nil {
				t.Fatalf("EncryptPassword: %v", err)
			}
			encrypted = &enc
		}
		h.exec(`
            INSERT INTO connections (id, name, host, port, database_name,
                username, password_encrypted, owner_username, sslmode)
            VALUES (8101, 'self', $1, $2, $3, $4, $5, $6, 'disable')`,
			cfg.Host, int(cfg.Port), cfg.Database, cfg.User, encrypted, covOwner)

		rec := h.serve(h.request(http.MethodGet, "/api/v1/connections/8101/databases", nil, &su))
		assertStatus(t, rec, http.StatusOK)
		var dbs []database.DatabaseInfo
		if err := json.Unmarshal(rec.Body.Bytes(), &dbs); err != nil {
			t.Fatalf("decode: %v", err)
		}
		found := false
		for _, db := range dbs {
			if db.Name == cfg.Database {
				found = true
			}
		}
		if !found {
			t.Fatalf("database %q not listed in %+v", cfg.Database, dbs)
		}
	})
}

func TestConnectionCoverage_GetConnectionContext(t *testing.T) {
	h := newConnCoverageHarness(t)
	su := h.superuser()

	t.Run("denied", func(t *testing.T) {
		rec := h.serve(h.request(http.MethodGet, "/api/v1/connections/8100/context", nil, &h.other))
		assertStatus(t, rec, http.StatusForbidden)
	})
	t.Run("not found", func(t *testing.T) {
		rec := h.serve(h.request(http.MethodGet, "/api/v1/connections/8999/context", nil, &su))
		assertStatus(t, rec, http.StatusNotFound)
	})
	t.Run("success", func(t *testing.T) {
		rec := h.serve(h.request(http.MethodGet, "/api/v1/connections/8100/context", nil, &h.owner))
		assertStatus(t, rec, http.StatusOK)
		var got database.ConnectionContext
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.ConnectionID != covConnID || got.ServerName != "coverage-target" {
			t.Fatalf("unexpected context: %+v", got)
		}
	})
}

func TestConnectionCoverage_CurrentConnection(t *testing.T) {
	h := newConnCoverageHarness(t)
	const path = "/api/v1/connections/current"
	su := h.superuser()
	dbName := "postgres"

	t.Run("get with no selection", func(t *testing.T) {
		rec := h.serve(h.request(http.MethodGet, path, nil, &h.owner))
		assertStatus(t, rec, http.StatusNotFound)
	})

	t.Run("set rejections", func(t *testing.T) {
		tests := []struct {
			name string
			user *connCoverageUser
			body any
			want int
			msg  string
		}{
			{"invalid JSON", &h.owner, "{not json", http.StatusBadRequest, "Invalid request body"},
			{"missing id", &h.owner, CurrentConnectionRequest{}, http.StatusBadRequest, "connection_id is required"},
			{"denied", &h.other, CurrentConnectionRequest{ConnectionID: covConnID}, http.StatusForbidden, "Access denied"},
			{"not found", &su, CurrentConnectionRequest{ConnectionID: covMissing}, http.StatusBadRequest, "Connection not found"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				rec := h.serve(h.request(http.MethodPost, path, tt.body, tt.user))
				assertStatus(t, rec, tt.want)
				assertErrorMessage(t, rec, tt.msg)
			})
		}
	})

	t.Run("set then get", func(t *testing.T) {
		rec := h.serve(h.request(http.MethodPost, path,
			CurrentConnectionRequest{ConnectionID: covConnID, DatabaseName: &dbName}, &h.owner))
		assertStatus(t, rec, http.StatusOK)

		rec = h.serve(h.request(http.MethodGet, path, nil, &h.owner))
		assertStatus(t, rec, http.StatusOK)
		var got CurrentConnectionResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.ConnectionID != covConnID || got.Name != "coverage-target" ||
			got.DatabaseName == nil || *got.DatabaseName != dbName {
			t.Fatalf("unexpected current connection: %+v", got)
		}
	})

	t.Run("get after access revoked", func(t *testing.T) {
		// The other user's session points at a connection they cannot
		// access; the read must re-check RBAC rather than trust it.
		if err := h.store.SetConnectionSession(auth.GetTokenHashByRawToken(h.other.token),
			covConnID, nil); err != nil {
			t.Fatalf("SetConnectionSession: %v", err)
		}
		rec := h.serve(h.request(http.MethodGet, path, nil, &h.other))
		assertStatus(t, rec, http.StatusForbidden)
	})

	t.Run("get when connection gone", func(t *testing.T) {
		if err := h.store.SetConnectionSession(auth.GetTokenHashByRawToken(su.token),
			covMissing, nil); err != nil {
			t.Fatalf("SetConnectionSession: %v", err)
		}
		rec := h.serve(h.request(http.MethodGet, path, nil, &su))
		assertStatus(t, rec, http.StatusInternalServerError)
		assertErrorMessage(t, rec, "Failed to get connection details")
	})

	t.Run("clear", func(t *testing.T) {
		rec := h.serve(h.request(http.MethodDelete, path, nil, &h.owner))
		assertStatus(t, rec, http.StatusNoContent)
		rec = h.serve(h.request(http.MethodGet, path, nil, &h.owner))
		assertStatus(t, rec, http.StatusNotFound)
	})

	t.Run("session store failures", func(t *testing.T) {
		// A closed auth store fails every session call. The superuser
		// bypass never consults the store, so the RBAC gates still pass
		// and each handler reaches its session-store error branch.
		if err := h.store.Close(); err != nil {
			t.Fatalf("closing auth store: %v", err)
		}

		rec := h.serve(h.request(http.MethodGet, path, nil, &su))
		assertStatus(t, rec, http.StatusInternalServerError)
		assertErrorMessage(t, rec, "Failed to get current connection")

		rec = h.serve(h.request(http.MethodPost, path,
			CurrentConnectionRequest{ConnectionID: covConnID}, &su))
		assertStatus(t, rec, http.StatusInternalServerError)
		assertErrorMessage(t, rec, "Failed to save connection selection")

		rec = h.serve(h.request(http.MethodDelete, path, nil, &su))
		assertStatus(t, rec, http.StatusInternalServerError)
		assertErrorMessage(t, rec, "Failed to clear connection selection")
	})
}

func TestConnectionCoverage_GetConnectionCluster(t *testing.T) {
	h := newConnCoverageHarness(t)
	const path = "/api/v1/connections/8100/cluster"
	su := h.superuser()

	h.exec(`INSERT INTO clusters (id, name, replication_type) VALUES
        (500, 'alpha', 'spock'), (501, 'beta', 'binary')`)
	seedIssue269Connection(t, h.pool, 8102, covOwner, "peer")
	seedIssue269Connection(t, h.pool, 8103, covOwner, "unrelated")
	h.exec(`UPDATE connections SET cluster_id = 500, role = 'primary' WHERE id IN (8100, 8102, 8103)`)
	h.exec(`INSERT INTO cluster_node_relationships
        (cluster_id, source_connection_id, target_connection_id, relationship_type)
        VALUES (500, 8100, 8102, 'replicates_with'),
               (500, 8103, 8100, 'streams_from'),
               (500, 8102, 8103, 'replicates_with')`)

	t.Run("denied", func(t *testing.T) {
		rec := h.serve(h.request(http.MethodGet, path, nil, &h.other))
		assertStatus(t, rec, http.StatusForbidden)
	})

	t.Run("not found", func(t *testing.T) {
		rec := h.serve(h.request(http.MethodGet, "/api/v1/connections/8999/cluster", nil, &su))
		assertStatus(t, rec, http.StatusNotFound)
	})

	t.Run("assigned with relationships", func(t *testing.T) {
		rec := h.serve(h.request(http.MethodGet, path, nil, &h.owner))
		assertStatus(t, rec, http.StatusOK)
		var got connectionClusterResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.Info == nil || got.Info.ClusterID == nil || *got.Info.ClusterID != 500 {
			t.Fatalf("unexpected info: %+v", got.Info)
		}
		if len(got.Clusters) != 2 {
			t.Fatalf("clusters = %+v, want 2", got.Clusters)
		}
		// Only the two relationships involving 8100 are returned.
		if len(got.Relationships) != 2 {
			t.Fatalf("relationships = %+v, want 2", got.Relationships)
		}
	})

	t.Run("relationship lookup failure is tolerated", func(t *testing.T) {
		h.exec(`ALTER TABLE cluster_node_relationships RENAME TO cnr_hidden`)
		defer h.exec(`ALTER TABLE cnr_hidden RENAME TO cluster_node_relationships`)
		rec := h.serve(h.request(http.MethodGet, path, nil, &h.owner))
		assertStatus(t, rec, http.StatusOK)
		var got connectionClusterResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.Relationships == nil || len(got.Relationships) != 0 {
			t.Fatalf("relationships = %+v, want empty list", got.Relationships)
		}
	})

	t.Run("unassigned", func(t *testing.T) {
		rec := h.serve(h.request(http.MethodGet, "/api/v1/connections/8102/cluster", nil, &su))
		assertStatus(t, rec, http.StatusOK)
		h.exec(`UPDATE connections SET cluster_id = NULL WHERE id = 8102`)
		rec = h.serve(h.request(http.MethodGet, "/api/v1/connections/8102/cluster", nil, &su))
		assertStatus(t, rec, http.StatusOK)
		if !strings.Contains(rec.Body.String(), `"relationships":[]`) {
			t.Fatalf("expected empty relationships list, got %s", rec.Body.String())
		}
	})

	t.Run("cluster list failure", func(t *testing.T) {
		h.exec(`ALTER TABLE clusters RENAME COLUMN dismissed TO dismissed_hidden`)
		defer h.exec(`ALTER TABLE clusters RENAME COLUMN dismissed_hidden TO dismissed`)
		rec := h.serve(h.request(http.MethodGet, path, nil, &h.owner))
		assertStatus(t, rec, http.StatusInternalServerError)
		assertErrorMessage(t, rec, "Failed to list clusters")
	})
}

func TestConnectionCoverage_UpdateConnectionCluster(t *testing.T) {
	h := newConnCoverageHarness(t)
	const path = "/api/v1/connections/8100/cluster"
	const missingPath = "/api/v1/connections/8999/cluster"
	su := h.superuser()
	h.exec(`INSERT INTO clusters (id, name) VALUES (600, 'gamma')`)
	cluster := 600
	role := "replica"

	tests := []struct {
		name string
		user *connCoverageUser
		path string
		body any
		want int
		msg  string
	}{
		{"not visible", &h.other, path, ConnectionClusterUpdateRequest{}, http.StatusForbidden, "Access denied"},
		{"visible without manage_connections", &h.owner, path, ConnectionClusterUpdateRequest{}, http.StatusForbidden,
			"Permission denied: requires manage_connections permission"},
		{"invalid JSON", &su, path, "{not json", http.StatusBadRequest, "Invalid request body"},
		{"reset on missing connection", &su, missingPath, ConnectionClusterUpdateRequest{},
			http.StatusInternalServerError, "Failed to reset membership source"},
		{"assign on missing connection", &su, missingPath,
			ConnectionClusterUpdateRequest{ClusterID: &cluster},
			http.StatusInternalServerError, "Failed to assign connection to cluster"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := h.serve(h.request(http.MethodPut, tt.path, tt.body, tt.user))
			assertStatus(t, rec, tt.want)
			assertErrorMessage(t, rec, tt.msg)
		})
	}

	readInfo := func(t *testing.T, rec *httptest.ResponseRecorder) database.ConnectionClusterInfo {
		t.Helper()
		assertStatus(t, rec, http.StatusOK)
		var info database.ConnectionClusterInfo
		if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return info
	}

	t.Run("assign defaults to auto", func(t *testing.T) {
		info := readInfo(t, h.serve(h.request(http.MethodPut, path,
			ConnectionClusterUpdateRequest{ClusterID: &cluster, Role: &role}, &su)))
		if info.ClusterID == nil || *info.ClusterID != cluster || info.MembershipSource != "auto" ||
			info.Role == nil || *info.Role != role {
			t.Fatalf("unexpected info: %+v", info)
		}
	})

	t.Run("manual unassign", func(t *testing.T) {
		info := readInfo(t, h.serve(h.request(http.MethodPut, path,
			ConnectionClusterUpdateRequest{MembershipSource: "manual"}, &su)))
		if info.ClusterID != nil || info.MembershipSource != "manual" {
			t.Fatalf("unexpected info: %+v", info)
		}
	})

	t.Run("reset to auto-detection", func(t *testing.T) {
		info := readInfo(t, h.serve(h.request(http.MethodPut, path,
			ConnectionClusterUpdateRequest{}, &su)))
		if info.MembershipSource != "auto" {
			t.Fatalf("unexpected info: %+v", info)
		}
	})

	t.Run("re-read failure", func(t *testing.T) {
		// The row vanishes between the write and the re-read.
		h.exec(`
            CREATE FUNCTION conncov_vanish() RETURNS trigger
            LANGUAGE plpgsql AS $$
            BEGIN
                DELETE FROM connections WHERE id = NEW.id;
                RETURN NULL;
            END;
            $$;`)
		h.exec(`CREATE TRIGGER conncov_vanish AFTER UPDATE ON connections
            FOR EACH ROW EXECUTE FUNCTION conncov_vanish()`)
		rec := h.serve(h.request(http.MethodPut, path, ConnectionClusterUpdateRequest{}, &su))
		assertStatus(t, rec, http.StatusInternalServerError)
		assertErrorMessage(t, rec, "Failed to get updated cluster info")
	})
}
