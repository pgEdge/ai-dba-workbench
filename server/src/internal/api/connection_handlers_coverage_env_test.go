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
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// connHandlerCoverageSchema is a trimmed copy of the collector's
// connections, clusters and cluster_node_relationships tables, holding
// every column the connection handlers' datastore calls select or write
// (GetConnection, UpdateConnectionFull, DeleteConnection,
// GetConnectionClusterInfo, ListClustersForAutocomplete,
// GetClusterRelationships, AssignConnectionToCluster and
// ResetMembershipSource). The trigger lets a test force DeleteConnection
// to fail for one named row so the handler's 500 path can be reached.
// It is created inside connHandlerCoverageSchemaName, selected through
// search_path, so it never drops or alters the public-schema tables
// that other packages' integration tests build in the same database.
const connHandlerCoverageSchema = `
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
    description TEXT DEFAULT '',
    host VARCHAR(255) NOT NULL,
    hostaddr VARCHAR(255),
    port INTEGER NOT NULL DEFAULT 5432,
    database_name VARCHAR(255) NOT NULL,
    username VARCHAR(255),
    password_encrypted TEXT,
    sslmode VARCHAR(50),
    sslcert TEXT,
    sslkey TEXT,
    sslrootcert TEXT,
    owner_username VARCHAR(255),
    owner_token VARCHAR(255),
    is_monitored BOOLEAN NOT NULL DEFAULT FALSE,
    is_shared BOOLEAN NOT NULL DEFAULT FALSE,
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE SET NULL,
    role VARCHAR(50),
    membership_source VARCHAR(20) NOT NULL DEFAULT 'auto',
    connection_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE cluster_node_relationships (
    id SERIAL PRIMARY KEY,
    cluster_id INTEGER NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    source_connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    target_connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    relationship_type VARCHAR(50) NOT NULL,
    is_auto_detected BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE FUNCTION conn_handler_cov_block_delete() RETURNS trigger AS $$
BEGIN
    IF OLD.name = 'undeletable' THEN
        RAISE EXCEPTION 'delete blocked for test';
    END IF;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER conn_handler_cov_block_delete
    BEFORE DELETE ON connections
    FOR EACH ROW EXECUTE FUNCTION conn_handler_cov_block_delete();
`

// connHandlerCoverageSchemaName is the schema the fixture above is built
// in; it is dropped before and after each test. The process ID suffix
// stops two test processes sharing a database from dropping each
// other's schema.
var connHandlerCoverageSchemaName = "api_conn_handler_cov_" + strconv.Itoa(os.Getpid())

// Fixture connection IDs. ownedConnID is unshared and owned by the
// unprivileged owner; foreignConnID is unshared and owned by someone
// else; undeletableConnID is owned by the owner but refuses deletion;
// unreachableConnID points at a port nothing listens on; missingConnID
// never exists.
const (
	ownedConnID       = 31
	foreignConnID     = 32
	undeletableConnID = 33
	unreachableConnID = 34
	missingConnID     = 999
	fixtureClusterID  = 7
)

// connHandlerEnv bundles the handler under test with the identities the
// tests drive it as.
type connHandlerEnv struct {
	t          *testing.T
	ds         *database.Datastore
	pool       *pgxpool.Pool
	store      *auth.AuthStore
	handler    *ConnectionHandler
	ownerID    int64
	ownerToken string
	adminID    int64
	adminToken string
}

// newConnHandlerEnv installs the schema and fixture rows, creates an
// unprivileged owner and an admin holding manage_connections, and
// builds a handler whose RBAC checker reads sharing from the datastore.
// The handler allows internal hosts (so a connection can point at the
// test database) but blocks blocked.example.com, which gives the update
// and create paths a host that fails validation without DNS.
func newConnHandlerEnv(t *testing.T) *connHandlerEnv {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping connection handler integration test")
	}

	ctx := context.Background()
	dropSchema := "DROP SCHEMA IF EXISTS " + connHandlerCoverageSchemaName + " CASCADE"
	admin := covPoolForSchema(t, connStr, "public")
	// Registered first so that it runs last, after the pool below is
	// closed, and still runs if building that pool skips the test.
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), dropSchema)
		admin.Close()
	})
	if _, err := admin.Exec(ctx, dropSchema); err != nil {
		t.Fatalf("Failed to drop stale connection handler schema: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+connHandlerCoverageSchemaName); err != nil {
		t.Fatalf("Failed to create connection handler schema: %v", err)
	}
	pool := covPoolForSchema(t, connStr, connHandlerCoverageSchemaName)
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, connHandlerCoverageSchema); err != nil {
		t.Fatalf("Failed to create connection handler tables: %v", err)
	}

	_, err := pool.Exec(ctx, `
        INSERT INTO clusters (id, name, replication_type)
        VALUES ($1, 'fixture-cluster', 'spock');
        `, fixtureClusterID)
	if err != nil {
		t.Fatalf("Seed cluster: %v", err)
	}
	_, err = pool.Exec(ctx, `
        INSERT INTO connections (id, name, host, port, database_name,
            username, is_shared, owner_username)
        VALUES
            (31, 'owned', 'db1.example.com', 5432, 'postgres', 'app', FALSE, 'cov_owner'),
            (32, 'foreign', 'db2.example.com', 5432, 'postgres', 'app', FALSE, 'someone_else'),
            (33, 'undeletable', 'db3.example.com', 5432, 'postgres', 'app', FALSE, 'cov_owner'),
            (34, 'unreachable', '127.0.0.1', 1, 'postgres', 'app', FALSE, 'cov_owner')
    `)
	if err != nil {
		t.Fatalf("Seed connections: %v", err)
	}

	ds := database.NewTestDatastoreWithSecret(pool, "connection-handler-test-secret")

	_, store, cleanupStore := createTestRBACHandler(t)
	t.Cleanup(cleanupStore)

	ownerID := newIssue207UnprivilegedUser(t, store, "cov_owner")
	ownerToken, _, err := store.AuthenticateUser("cov_owner", "Password1234")
	if err != nil {
		t.Fatalf("Authenticate owner: %v", err)
	}
	adminID := setupUserWithPermissions(t, store, "cov_admin",
		[]string{auth.PermManageConnections})
	adminToken, _, err := store.AuthenticateUser("cov_admin", "Password1234")
	if err != nil {
		t.Fatalf("Authenticate admin: %v", err)
	}

	checker := auth.NewRBACChecker(store)
	checker.SetConnectionSharingLookup(
		func(ctx context.Context, id int) (bool, string, error) {
			return ds.GetConnectionSharingInfo(ctx, id)
		},
	)
	handler := NewConnectionHandlerWithSecurity(ds, store, checker, true,
		nil, []string{"blocked.example.com"})

	return &connHandlerEnv{
		t: t, ds: ds, pool: pool, store: store, handler: handler,
		ownerID: ownerID, ownerToken: ownerToken,
		adminID: adminID, adminToken: adminToken,
	}
}

// asOwner, asAdmin and asSuperuser build requests carrying the bearer
// token and the context values the auth middleware would set.
func (e *connHandlerEnv) asOwner(method, path string, body any) *http.Request {
	return e.request(method, path, body, e.ownerToken, e.ownerID, "cov_owner", false)
}

func (e *connHandlerEnv) asAdmin(method, path string, body any) *http.Request {
	return e.request(method, path, body, e.adminToken, e.adminID, "cov_admin", false)
}

func (e *connHandlerEnv) asSuperuser(method, path string, body any) *http.Request {
	return e.request(method, path, body, e.adminToken, e.adminID, "cov_admin", true)
}

func (e *connHandlerEnv) request(method, path string, body any, token string,
	userID int64, username string, superuser bool) *http.Request {
	e.t.Helper()
	var reader *bytes.Reader
	switch b := body.(type) {
	case nil:
		reader = bytes.NewReader(nil)
	case string:
		reader = bytes.NewReader([]byte(b))
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			e.t.Fatalf("Marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	req = withBearer(req, token)
	ctx := context.WithValue(req.Context(), auth.IsSuperuserContextKey, superuser)
	ctx = context.WithValue(ctx, auth.UserIDContextKey, userID)
	ctx = context.WithValue(ctx, auth.UsernameContextKey, username)
	return req.WithContext(ctx)
}

// serve routes a request through handleConnectionSubpath, as the mux
// does in production, and returns the recorder.
func (e *connHandlerEnv) serve(req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.handler.handleConnectionSubpath(rec, req)
	return rec
}

// expectConnHandlerStatus fails the test when the response status is not want.
func expectConnHandlerStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("expected status %d, got %d: %s", want, rec.Code, rec.Body.String())
	}
}

func TestConnectionHandlerCoverage_GetConnection(t *testing.T) {
	e := newConnHandlerEnv(t)

	rec := e.serve(e.asOwner(http.MethodGet, "/api/v1/connections/31", nil))
	expectConnHandlerStatus(t, rec, http.StatusOK)
	if !strings.Contains(rec.Body.String(), "owned") {
		t.Errorf("expected connection 31 in the body, got %s", rec.Body.String())
	}

	expectConnHandlerStatus(t, e.serve(e.asOwner(http.MethodGet, "/api/v1/connections/32", nil)),
		http.StatusForbidden)
	expectConnHandlerStatus(t, e.serve(e.asSuperuser(http.MethodGet, "/api/v1/connections/999", nil)),
		http.StatusNotFound)
}

func TestConnectionHandlerCoverage_UpdateConnection(t *testing.T) {
	e := newConnHandlerEnv(t)
	path := "/api/v1/connections/31"

	tests := []struct {
		name string
		req  *http.Request
		want int
	}{
		{"owner renames", e.asOwner(http.MethodPut, path,
			ConnectionFullUpdateRequest{Name: strPtr("renamed")}), http.StatusOK},
		{"malformed body", e.asOwner(http.MethodPut, path, "{"), http.StatusBadRequest},
		{"invalid name", e.asOwner(http.MethodPut, path,
			ConnectionFullUpdateRequest{Name: strPtr("bad<name>")}), http.StatusBadRequest},
		{"non-admin cannot share", e.asOwner(http.MethodPut, path,
			map[string]any{"is_shared": true}), http.StatusForbidden},
		{"blocked host", e.asOwner(http.MethodPut, path,
			map[string]any{"host": "blocked.example.com"}), http.StatusBadRequest},
		{"blocked port", e.asOwner(http.MethodPut, path,
			map[string]any{"port": 22}), http.StatusBadRequest},
		{"datastore rejects over-long value", e.asAdmin(http.MethodPut, path,
			map[string]any{"database_name": strings.Repeat("d", 300)}),
			http.StatusInternalServerError},
		{"non-owner denied", e.asOwner(http.MethodPut, "/api/v1/connections/32",
			map[string]any{"name": "x"}), http.StatusForbidden},
		{"missing connection", e.asAdmin(http.MethodPut, "/api/v1/connections/999",
			map[string]any{"name": "x"}), http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expectConnHandlerStatus(t, e.serve(tt.req), tt.want)
		})
	}

	var name string
	if err := e.pool.QueryRow(context.Background(),
		"SELECT name FROM connections WHERE id = 31").Scan(&name); err != nil {
		t.Fatalf("Read back: %v", err)
	}
	if name != "renamed" {
		t.Errorf("expected name 'renamed', got %q", name)
	}
}

func TestConnectionHandlerCoverage_DeleteConnection(t *testing.T) {
	e := newConnHandlerEnv(t)

	unauth := httptest.NewRequest(http.MethodDelete, "/api/v1/connections/31", nil)
	expectConnHandlerStatus(t, e.serve(unauth), http.StatusUnauthorized)

	expectConnHandlerStatus(t, e.serve(e.asOwner(http.MethodDelete, "/api/v1/connections/32", nil)),
		http.StatusForbidden)
	expectConnHandlerStatus(t, e.serve(e.asAdmin(http.MethodDelete, "/api/v1/connections/999", nil)),
		http.StatusNotFound)
	expectConnHandlerStatus(t, e.serve(e.asOwner(http.MethodDelete, "/api/v1/connections/33", nil)),
		http.StatusInternalServerError)
	expectConnHandlerStatus(t, e.serve(e.asOwner(http.MethodDelete, "/api/v1/connections/31", nil)),
		http.StatusNoContent)

	var n int
	if err := e.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM connections WHERE id = 31").Scan(&n); err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 0 {
		t.Errorf("connection 31 should have been deleted")
	}
}

func TestConnectionHandlerCoverage_ListDatabases(t *testing.T) {
	e := newConnHandlerEnv(t)

	// Point a connection at the test database itself so the listing
	// succeeds; the credentials come from the test DSN, never from any
	// runtime configuration.
	cfg, err := pgx.ParseConfig(os.Getenv("TEST_AI_WORKBENCH_SERVER"))
	if err != nil {
		t.Fatalf("Parse test DSN: %v", err)
	}
	conn, err := e.ds.CreateConnection(context.Background(), database.ConnectionCreateParams{
		Name:          "self",
		Description:   strPtr(""),
		Host:          cfg.Host,
		Port:          int(cfg.Port),
		DatabaseName:  cfg.Database,
		Username:      cfg.User,
		Password:      cfg.Password,
		SSLMode:       strPtr("disable"),
		OwnerUsername: "cov_owner",
	})
	if err != nil {
		t.Fatalf("Create self connection: %v", err)
	}

	rec := e.serve(e.asOwner(http.MethodGet,
		"/api/v1/connections/"+strconv.Itoa(conn.ID)+"/databases", nil))
	expectConnHandlerStatus(t, rec, http.StatusOK)
	var dbs []database.DatabaseInfo
	if err := json.NewDecoder(rec.Body).Decode(&dbs); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(dbs) == 0 {
		t.Error("expected at least one database in the listing")
	}

	expectConnHandlerStatus(t, e.serve(e.asOwner(http.MethodGet, "/api/v1/connections/32/databases", nil)),
		http.StatusForbidden)
	expectConnHandlerStatus(t, e.serve(e.asOwner(http.MethodGet, "/api/v1/connections/34/databases", nil)),
		http.StatusInternalServerError)
}

func TestConnectionHandlerCoverage_ConnectionContext(t *testing.T) {
	e := newConnHandlerEnv(t)

	rec := e.serve(e.asOwner(http.MethodGet, "/api/v1/connections/31/context", nil))
	expectConnHandlerStatus(t, rec, http.StatusOK)
	if !strings.Contains(rec.Body.String(), "owned") {
		t.Errorf("expected server name in context, got %s", rec.Body.String())
	}
	expectConnHandlerStatus(t, e.serve(e.asOwner(http.MethodGet, "/api/v1/connections/32/context", nil)),
		http.StatusForbidden)
	expectConnHandlerStatus(t, e.serve(e.asSuperuser(http.MethodGet, "/api/v1/connections/999/context", nil)),
		http.StatusNotFound)
	expectConnHandlerStatus(t, e.serve(e.asOwner(http.MethodPost, "/api/v1/connections/31/context", nil)),
		http.StatusMethodNotAllowed)
}

func TestConnectionHandlerCoverage_CurrentConnection(t *testing.T) {
	e := newConnHandlerEnv(t)
	path := "/api/v1/connections/current"

	// Nothing selected yet.
	expectConnHandlerStatus(t, e.serve(e.asOwner(http.MethodGet, path, nil)), http.StatusNotFound)

	rec := e.serve(e.asOwner(http.MethodPost, path,
		CurrentConnectionRequest{ConnectionID: ownedConnID, DatabaseName: strPtr("postgres")}))
	expectConnHandlerStatus(t, rec, http.StatusOK)

	rec = e.serve(e.asOwner(http.MethodGet, path, nil))
	expectConnHandlerStatus(t, rec, http.StatusOK)
	var got CurrentConnectionResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.ConnectionID != ownedConnID || got.Name != "owned" ||
		got.DatabaseName == nil || *got.DatabaseName != "postgres" {
		t.Errorf("unexpected current connection %+v", got)
	}

	expectConnHandlerStatus(t, e.serve(e.asOwner(http.MethodPost, path, "{")), http.StatusBadRequest)
	expectConnHandlerStatus(t, e.serve(e.asOwner(http.MethodPost, path,
		CurrentConnectionRequest{ConnectionID: foreignConnID})), http.StatusForbidden)
	expectConnHandlerStatus(t, e.serve(e.asSuperuser(http.MethodPost, path,
		CurrentConnectionRequest{ConnectionID: missingConnID})), http.StatusBadRequest)

	expectConnHandlerStatus(t, e.serve(e.asOwner(http.MethodDelete, path, nil)), http.StatusNoContent)
	expectConnHandlerStatus(t, e.serve(e.asOwner(http.MethodGet, path, nil)), http.StatusNotFound)

	// A stored selection is re-checked on every read, so a session
	// pinned to a connection the caller cannot see is refused.
	ownerHash := auth.GetTokenHashByRawToken(e.ownerToken)
	if err := e.store.SetConnectionSession(ownerHash, foreignConnID, nil); err != nil {
		t.Fatalf("SetConnectionSession: %v", err)
	}
	expectConnHandlerStatus(t, e.serve(e.asOwner(http.MethodGet, path, nil)), http.StatusForbidden)

	// A selection whose connection has since disappeared is a 500.
	adminHash := auth.GetTokenHashByRawToken(e.adminToken)
	if err := e.store.SetConnectionSession(adminHash, missingConnID, nil); err != nil {
		t.Fatalf("SetConnectionSession: %v", err)
	}
	expectConnHandlerStatus(t, e.serve(e.asSuperuser(http.MethodGet, path, nil)),
		http.StatusInternalServerError)
}

func TestConnectionHandlerCoverage_GetCluster(t *testing.T) {
	e := newConnHandlerEnv(t)
	ctx := context.Background()

	// Unassigned: no cluster info, an empty relationship list.
	rec := e.serve(e.asOwner(http.MethodGet, "/api/v1/connections/31/cluster", nil))
	expectConnHandlerStatus(t, rec, http.StatusOK)
	var resp connectionClusterResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if resp.Info == nil || resp.Info.ClusterID != nil {
		t.Errorf("expected an unassigned connection, got %+v", resp.Info)
	}
	if resp.Relationships == nil || len(resp.Relationships) != 0 {
		t.Errorf("expected an empty relationship list, got %+v", resp.Relationships)
	}
	if len(resp.Clusters) != 1 {
		t.Errorf("expected the fixture cluster in the list, got %+v", resp.Clusters)
	}

	// Assigned with relationships: only those touching 31 come back.
	if _, err := e.pool.Exec(ctx, `
        UPDATE connections SET cluster_id = $1 WHERE id IN (31, 32, 33);
        `, fixtureClusterID); err != nil {
		t.Fatalf("Assign cluster: %v", err)
	}
	if _, err := e.pool.Exec(ctx, `
        INSERT INTO cluster_node_relationships
            (cluster_id, source_connection_id, target_connection_id, relationship_type)
        VALUES ($1, 31, 32, 'replicates_with'), ($1, 32, 33, 'replicates_with');
        `, fixtureClusterID); err != nil {
		t.Fatalf("Seed relationships: %v", err)
	}
	rec = e.serve(e.asOwner(http.MethodGet, "/api/v1/connections/31/cluster", nil))
	expectConnHandlerStatus(t, rec, http.StatusOK)
	resp = connectionClusterResponse{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(resp.Relationships) != 1 || resp.Relationships[0].TargetConnectionID != foreignConnID {
		t.Errorf("expected only the 31 -> 32 relationship, got %+v", resp.Relationships)
	}

	expectConnHandlerStatus(t, e.serve(e.asOwner(http.MethodGet, "/api/v1/connections/32/cluster", nil)),
		http.StatusForbidden)
	expectConnHandlerStatus(t, e.serve(e.asSuperuser(http.MethodGet, "/api/v1/connections/999/cluster", nil)),
		http.StatusNotFound)
	expectConnHandlerStatus(t, e.serve(e.asOwner(http.MethodPatch, "/api/v1/connections/31/cluster", nil)),
		http.StatusMethodNotAllowed)

	// A failed relationship lookup is logged and degrades to an empty
	// list rather than failing the request.
	if _, err := e.pool.Exec(ctx, "DROP TABLE cluster_node_relationships"); err != nil {
		t.Fatalf("Drop relationships: %v", err)
	}
	rec = e.serve(e.asOwner(http.MethodGet, "/api/v1/connections/31/cluster", nil))
	expectConnHandlerStatus(t, rec, http.StatusOK)
	resp = connectionClusterResponse{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if resp.Relationships == nil || len(resp.Relationships) != 0 {
		t.Errorf("expected an empty relationship list, got %+v", resp.Relationships)
	}

	// The autocomplete listing filters on clusters.dismissed, which the
	// cluster info lookup does not read, so dropping that column fails
	// the listing alone.
	if _, err := e.pool.Exec(ctx, "ALTER TABLE clusters DROP COLUMN dismissed"); err != nil {
		t.Fatalf("Drop dismissed: %v", err)
	}
	expectConnHandlerStatus(t, e.serve(e.asOwner(http.MethodGet, "/api/v1/connections/31/cluster", nil)),
		http.StatusInternalServerError)
}

func TestConnectionHandlerCoverage_UpdateCluster(t *testing.T) {
	e := newConnHandlerEnv(t)
	path := "/api/v1/connections/31/cluster"
	clusterID := fixtureClusterID
	missingCluster := 12345

	tests := []struct {
		name string
		req  *http.Request
		want int
	}{
		{"owner lacks manage_connections", e.asOwner(http.MethodPut, path,
			ConnectionClusterUpdateRequest{ClusterID: &clusterID}), http.StatusForbidden},
		{"not visible", e.asOwner(http.MethodPut, "/api/v1/connections/32/cluster",
			ConnectionClusterUpdateRequest{ClusterID: &clusterID}), http.StatusForbidden},
		{"malformed body", e.asSuperuser(http.MethodPut, path, "{"), http.StatusBadRequest},
		{"manual assignment", e.asSuperuser(http.MethodPut, path,
			ConnectionClusterUpdateRequest{ClusterID: &clusterID, Role: strPtr("primary"),
				MembershipSource: "manual"}), http.StatusOK},
		{"default source is auto", e.asSuperuser(http.MethodPut, path,
			ConnectionClusterUpdateRequest{ClusterID: &clusterID}), http.StatusOK},
		{"reset to auto-detection", e.asSuperuser(http.MethodPut, path,
			ConnectionClusterUpdateRequest{}), http.StatusOK},
		{"unknown cluster", e.asSuperuser(http.MethodPut, path,
			ConnectionClusterUpdateRequest{ClusterID: &missingCluster}),
			http.StatusInternalServerError},
		{"reset missing connection", e.asSuperuser(http.MethodPut,
			"/api/v1/connections/999/cluster", ConnectionClusterUpdateRequest{}),
			http.StatusInternalServerError},
		{"assign missing connection", e.asSuperuser(http.MethodPut,
			"/api/v1/connections/999/cluster",
			ConnectionClusterUpdateRequest{ClusterID: &clusterID}),
			http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expectConnHandlerStatus(t, e.serve(tt.req), tt.want)
		})
	}

	var source string
	if err := e.pool.QueryRow(context.Background(),
		"SELECT membership_source FROM connections WHERE id = 31").Scan(&source); err != nil {
		t.Fatalf("Read back: %v", err)
	}
	if source != "auto" {
		t.Errorf("expected membership_source 'auto' after reset, got %q", source)
	}
}

func TestConnectionHandlerCoverage_CreateConnection(t *testing.T) {
	e := newConnHandlerEnv(t)

	valid := func() map[string]any {
		return map[string]any{
			"name": "created", "description": "", "host": "127.0.0.1", "port": 5432,
			"database_name": "postgres", "username": "app", "password": "secret",
		}
	}
	with := func(k string, v any) map[string]any {
		m := valid()
		m[k] = v
		return m
	}

	tests := []struct {
		name string
		body any
		want int
	}{
		{"created", valid(), http.StatusCreated},
		{"malformed body", "{", http.StatusBadRequest},
		{"missing host", with("host", ""), http.StatusBadRequest},
		{"missing port", with("port", 0), http.StatusBadRequest},
		{"missing database", with("database_name", ""), http.StatusBadRequest},
		{"missing username", with("username", ""), http.StatusBadRequest},
		{"missing password", with("password", ""), http.StatusBadRequest},
		{"blocked host", with("host", "blocked.example.com"), http.StatusBadRequest},
		{"blocked port", with("port", 22), http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			e.handler.handleConnections(rec,
				e.asAdmin(http.MethodPost, "/api/v1/connections", tt.body))
			expectConnHandlerStatus(t, rec, tt.want)
		})
	}

	// Without a server secret the password cannot be encrypted, so the
	// datastore call fails and the handler reports it.
	noSecret := NewConnectionHandlerWithSecurity(database.NewTestDatastore(e.pool),
		e.store, e.handler.rbacChecker, true, nil, nil)
	rec := httptest.NewRecorder()
	noSecret.handleConnections(rec,
		e.asAdmin(http.MethodPost, "/api/v1/connections", valid()))
	expectConnHandlerStatus(t, rec, http.StatusInternalServerError)
}

func TestConnectionHandlerCoverage_RegisterRoutes(t *testing.T) {
	e := newConnHandlerEnv(t)

	mux := http.NewServeMux()
	passthrough := func(h http.HandlerFunc) http.HandlerFunc { return h }
	e.handler.RegisterRoutes(mux, passthrough)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, e.asOwner(http.MethodGet, "/api/v1/connections/31", nil))
	expectConnHandlerStatus(t, rec, http.StatusOK)

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, e.asOwner(http.MethodGet, "/api/v1/connections/current", nil))
	expectConnHandlerStatus(t, rec, http.StatusNotFound)

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, e.asOwner(http.MethodDelete, "/api/v1/connections", nil))
	expectConnHandlerStatus(t, rec, http.StatusMethodNotAllowed)
}
