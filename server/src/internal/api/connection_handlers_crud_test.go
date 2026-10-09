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

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/pkg/crypto"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// connCRUDTestSchema carries every column and table the connection
// handlers touch through the datastore: the connection row itself, the
// cluster it may belong to and the cluster's node relationships.
const connCRUDTestSchema = `
DROP TABLE IF EXISTS cluster_node_relationships CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
CREATE TABLE clusters (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    replication_type VARCHAR(50),
    auto_cluster_key VARCHAR(255),
    dismissed BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
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
    role VARCHAR(50),
    connection_error TEXT,
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

const connCRUDTestTeardown = `
DROP TABLE IF EXISTS cluster_node_relationships CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
`

// connCRUDTestSecret is a fixed server secret so stored passwords
// round-trip through the real encryption.
const connCRUDTestSecret = "test-server-secret-32-bytes-long!"

// connCRUDEnv is a connection handler on a live datastore and a real
// auth store, with a caller who holds no admin permissions.
type connCRUDEnv struct {
	handler *ConnectionHandler
	ds      *database.Datastore
	pool    *pgxpool.Pool
	store   *auth.AuthStore
	user    string
	userID  int64
	token   string
}

// newConnCRUDEnv builds a connCRUDEnv, skipping when no test database
// is configured. "blocked.example.com" is on the host blocklist and the
// documentation address 192.0.2.10 is explicitly allowed.
func newConnCRUDEnv(t *testing.T) *connCRUDEnv {
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
		t.Fatalf("Could not connect to test database: %v", err)
	}
	if _, err := pool.Exec(ctx, connCRUDTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create connection handler schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), connCRUDTestTeardown)
		pool.Close()
	})

	store := newTestAuthStore(t)
	const user = "conn_crud_user"
	userID := setupUserWithPermissions(t, store, user, nil)
	token, _, err := store.AuthenticateUser(user, "Password1234")
	if err != nil {
		t.Fatalf("AuthenticateUser: %v", err)
	}

	ds := database.NewTestDatastoreWithSecret(pool, connCRUDTestSecret)
	return &connCRUDEnv{
		handler: NewConnectionHandlerWithSecurity(ds, store,
			auth.NewRBACChecker(store), false, []string{"192.0.2.10"},
			[]string{"blocked.example.com"}),
		ds:     ds,
		pool:   pool,
		store:  store,
		user:   user,
		userID: userID,
		token:  token,
	}
}

// seed inserts a connection owned by owner and returns its ID.
func (e *connCRUDEnv) seed(t *testing.T, owner, host string, port int,
	dbName, user string, encrypted *string) int {
	t.Helper()
	var id int
	if err := e.pool.QueryRow(context.Background(), `
        INSERT INTO connections (name, host, port, database_name, username,
            password_encrypted, sslmode, owner_username)
        VALUES ('crud-test', $1, $2, $3, $4, $5, 'disable', $6)
        RETURNING id
    `, host, port, dbName, user, encrypted, owner).Scan(&id); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	return id
}

// request builds an authenticated request. superuser marks the context
// as a superuser; otherwise the caller is the env's ordinary user.
func (e *connCRUDEnv) request(method, path string, body any,
	superuser bool) *http.Request {
	var reader *bytes.Reader
	switch b := body.(type) {
	case nil:
		reader = bytes.NewReader(nil)
	case string:
		reader = bytes.NewReader([]byte(b))
	default:
		data, _ := json.Marshal(b)
		reader = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	req = withBearer(req, e.token)
	if superuser {
		return withSuperuser(req)
	}
	return withUser(req, e.userID)
}

// serve routes a request the way RegisterRoutes does and returns the
// recorder.
func (e *connCRUDEnv) serve(req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	if req.URL.Path == "/api/v1/connections" {
		e.handler.handleConnections(rec, req)
	} else {
		e.handler.handleConnectionSubpath(rec, req)
	}
	return rec
}

func connPath(id int, suffix string) string {
	return "/api/v1/connections/" + strconv.Itoa(id) + suffix
}

func expectStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, want,
			rec.Body.String())
	}
}

// TestConnectionHandler_RegisterRoutes_Configured checks the configured
// routes reach the handlers.
func TestConnectionHandler_RegisterRoutes_Configured(t *testing.T) {
	h := NewConnectionHandlerWithSecurity(&database.Datastore{}, nil,
		newTestRBACChecker(t), false, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, func(f http.HandlerFunc) http.HandlerFunc { return f })

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch,
		"/api/v1/connections", nil))
	expectStatus(t, rec, http.StatusMethodNotAllowed)

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/v1/connections/current", nil))
	expectStatus(t, rec, http.StatusUnauthorized)

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/v1/connections/1/unknown", nil))
	expectStatus(t, rec, http.StatusNotFound)
}

// TestConnectionHandler_CreateConnection_Live covers the permission gate,
// each validation branch, a successful create and a datastore failure.
func TestConnectionHandler_CreateConnection_Live(t *testing.T) {
	env := newConnCRUDEnv(t)

	t.Run("requires manage_connections", func(t *testing.T) {
		rec := env.serve(env.request(http.MethodPost, "/api/v1/connections",
			validConnectionCreateRequest(), false))
		expectStatus(t, rec, http.StatusForbidden)
	})

	cases := []struct {
		name   string
		mutate func(*ConnectionCreateRequest)
		want   string
	}{
		{"missing host", func(r *ConnectionCreateRequest) { r.Host = "" },
			"Host is required"},
		{"missing port", func(r *ConnectionCreateRequest) { r.Port = 0 },
			"Port is required"},
		{"missing database", func(r *ConnectionCreateRequest) { r.DatabaseName = "" },
			"Maintenance Database is required"},
		{"missing username", func(r *ConnectionCreateRequest) { r.Username = "" },
			"Username is required"},
		{"missing password", func(r *ConnectionCreateRequest) { r.Password = "" },
			"Password is required"},
		{"username too long", func(r *ConnectionCreateRequest) {
			r.Username = strings.Repeat("u", maxFieldLength+1)
		}, "Username"},
		{"blocked host", func(r *ConnectionCreateRequest) {
			r.Host = "blocked.example.com"
		}, "Invalid host"},
		{"blocked port", func(r *ConnectionCreateRequest) { r.Port = 22 },
			"Invalid port"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := validConnectionCreateRequest()
			body.Host = "192.0.2.10"
			tc.mutate(&body)
			rec := env.serve(env.request(http.MethodPost,
				"/api/v1/connections", body, true))
			expectStatus(t, rec, http.StatusBadRequest)
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Errorf("body %q does not mention %q", rec.Body.String(),
					tc.want)
			}
		})
	}

	t.Run("creates the connection", func(t *testing.T) {
		body := validConnectionCreateRequest()
		body.Host = "192.0.2.10"
		rec := env.serve(env.request(http.MethodPost, "/api/v1/connections",
			body, true))
		expectStatus(t, rec, http.StatusCreated)
		var owner string
		if err := env.pool.QueryRow(context.Background(),
			"SELECT owner_username FROM connections WHERE host = '192.0.2.10'").
			Scan(&owner); err != nil || owner != env.user {
			t.Errorf("owner = %q, err = %v, want %q", owner, err, env.user)
		}
	})

	t.Run("datastore failure", func(t *testing.T) {
		// Without a server secret the password cannot be encrypted.
		h := NewConnectionHandlerWithSecurity(
			database.NewTestDatastore(env.pool), env.store,
			auth.NewRBACChecker(env.store), false, []string{"192.0.2.10"},
			nil)
		body := validConnectionCreateRequest()
		body.Host = "192.0.2.10"
		rec := httptest.NewRecorder()
		h.handleConnections(rec, env.request(http.MethodPost,
			"/api/v1/connections", body, true))
		expectStatus(t, rec, http.StatusInternalServerError)
	})
}

// TestConnectionHandler_GetUpdateDelete_Live covers the single-connection
// endpoints, including ownership and permission checks.
func TestConnectionHandler_GetUpdateDelete_Live(t *testing.T) {
	env := newConnCRUDEnv(t)
	ownID := env.seed(t, env.user, "192.0.2.10", 5432, "app", "test_user", nil)
	otherID := env.seed(t, "someone_else", "192.0.2.11", 5432, "app",
		"test_user", nil)
	missing := otherID + 1000

	t.Run("get", func(t *testing.T) {
		expectStatus(t, env.serve(env.request(http.MethodGet,
			connPath(ownID, ""), nil, true)), http.StatusOK)
		expectStatus(t, env.serve(env.request(http.MethodGet,
			connPath(missing, ""), nil, true)), http.StatusNotFound)
	})

	t.Run("update", func(t *testing.T) {
		name := "renamed"
		shared := true
		host := "blocked.example.com"
		port := 22
		password := "new-secret"

		cases := []struct {
			name      string
			id        int
			body      any
			superuser bool
			want      int
		}{
			{"missing connection", missing, ConnectionFullUpdateRequest{}, true,
				http.StatusNotFound},
			{"not the owner", otherID, ConnectionFullUpdateRequest{}, false,
				http.StatusForbidden},
			{"invalid body", ownID, "{", false, http.StatusBadRequest},
			{"sharing needs manage_connections", ownID,
				ConnectionFullUpdateRequest{IsShared: &shared}, false,
				http.StatusForbidden},
			{"blocked host", ownID, ConnectionFullUpdateRequest{Host: &host},
				false, http.StatusBadRequest},
			{"blocked port", ownID, ConnectionFullUpdateRequest{Port: &port},
				false, http.StatusBadRequest},
			{"owner renames", ownID, ConnectionFullUpdateRequest{Name: &name},
				false, http.StatusOK},
			{"password change", ownID,
				ConnectionFullUpdateRequest{Password: &password}, false,
				http.StatusOK},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				expectStatus(t, env.serve(env.request(http.MethodPut,
					connPath(tc.id, ""), tc.body, tc.superuser)), tc.want)
			})
		}

		t.Run("datastore failure", func(t *testing.T) {
			h := NewConnectionHandlerWithSecurity(
				database.NewTestDatastore(env.pool), env.store,
				auth.NewRBACChecker(env.store), false, nil, nil)
			rec := httptest.NewRecorder()
			h.handleConnectionSubpath(rec, env.request(http.MethodPut,
				connPath(ownID, ""),
				ConnectionFullUpdateRequest{Password: &password}, false))
			expectStatus(t, rec, http.StatusInternalServerError)
		})
	})

	t.Run("delete", func(t *testing.T) {
		noAuth := httptest.NewRequest(http.MethodDelete, connPath(ownID, ""),
			nil)
		expectStatus(t, env.serve(noAuth), http.StatusUnauthorized)
		expectStatus(t, env.serve(env.request(http.MethodDelete,
			connPath(missing, ""), nil, true)), http.StatusNotFound)
		expectStatus(t, env.serve(env.request(http.MethodDelete,
			connPath(otherID, ""), nil, false)), http.StatusForbidden)
		expectStatus(t, env.serve(env.request(http.MethodDelete,
			connPath(ownID, ""), nil, false)), http.StatusNoContent)
	})
}

// TestConnectionHandler_ListDatabases_Live lists databases through a
// connection that points back at the test server.
func TestConnectionHandler_ListDatabases_Live(t *testing.T) {
	env := newConnCRUDEnv(t)

	cfg := env.pool.Config().ConnConfig
	var encrypted *string
	if cfg.Password != "" {
		value, err := crypto.EncryptPassword(cfg.Password, connCRUDTestSecret)
		if err != nil {
			t.Fatalf("EncryptPassword: %v", err)
		}
		encrypted = &value
	}
	id := env.seed(t, env.user, cfg.Host, int(cfg.Port), cfg.Database,
		cfg.User, encrypted)

	rec := env.serve(env.request(http.MethodGet, connPath(id, "/databases"),
		nil, true))
	expectStatus(t, rec, http.StatusOK)
	if !strings.Contains(rec.Body.String(), `"`+cfg.Database+`"`) {
		t.Errorf("database %q missing from %s", cfg.Database,
			rec.Body.String())
	}

	expectStatus(t, env.serve(env.request(http.MethodGet,
		connPath(id+1000, "/databases"), nil, true)),
		http.StatusInternalServerError)
}

// TestConnectionHandler_CurrentConnection_Live covers selecting, reading
// and clearing the session's current connection.
func TestConnectionHandler_CurrentConnection_Live(t *testing.T) {
	env := newConnCRUDEnv(t)
	id := env.seed(t, env.user, "192.0.2.10", 5432, "app", "test_user", nil)
	const path = "/api/v1/connections/current"

	expectStatus(t, env.serve(env.request(http.MethodGet, path, nil, true)),
		http.StatusNotFound)
	expectStatus(t, env.serve(env.request(http.MethodPost, path, "{", true)),
		http.StatusBadRequest)

	dbName := "reports"
	rec := env.serve(env.request(http.MethodPost, path,
		CurrentConnectionRequest{ConnectionID: id, DatabaseName: &dbName},
		true))
	expectStatus(t, rec, http.StatusOK)

	rec = env.serve(env.request(http.MethodGet, path, nil, true))
	expectStatus(t, rec, http.StatusOK)
	var got CurrentConnectionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ConnectionID != id || got.DatabaseName == nil ||
		*got.DatabaseName != "reports" || got.Host != "192.0.2.10" {
		t.Errorf("unexpected current connection: %+v", got)
	}

	// A session whose connection has since gone fails to load it.
	if _, err := env.pool.Exec(context.Background(),
		"DELETE FROM connections WHERE id = $1", id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	expectStatus(t, env.serve(env.request(http.MethodGet, path, nil, true)),
		http.StatusInternalServerError)

	expectStatus(t, env.serve(env.request(http.MethodDelete, path, nil,
		true)), http.StatusNoContent)
	expectStatus(t, env.serve(env.request(http.MethodGet, path, nil, true)),
		http.StatusNotFound)
}

// TestConnectionHandler_CurrentConnection_StoreErrors covers the session
// store failing on each of the three operations.
func TestConnectionHandler_CurrentConnection_StoreErrors(t *testing.T) {
	env := newConnCRUDEnv(t)
	id := env.seed(t, env.user, "192.0.2.10", 5432, "app", "test_user", nil)

	closed, err := auth.NewAuthStore(t.TempDir(), 0, 0,
		auth.AuditKeyForTesting())
	if err != nil {
		t.Fatalf("NewAuthStore: %v", err)
	}
	closed.Close()
	h := NewConnectionHandlerWithSecurity(env.ds, closed,
		auth.NewRBACChecker(env.store), false, nil, nil)
	const path = "/api/v1/connections/current"

	for _, tc := range []struct {
		method string
		body   any
	}{
		{http.MethodGet, nil},
		{http.MethodPost, CurrentConnectionRequest{ConnectionID: id}},
		{http.MethodDelete, nil},
	} {
		rec := httptest.NewRecorder()
		h.handleCurrentConnection(rec, env.request(tc.method, path, tc.body,
			true))
		expectStatus(t, rec, http.StatusInternalServerError)
	}
}

// TestConnectionHandler_ContextAndCluster_Live covers the context and
// cluster sub-resources.
func TestConnectionHandler_ContextAndCluster_Live(t *testing.T) {
	env := newConnCRUDEnv(t)
	ctx := context.Background()

	var clusterID int
	if err := env.pool.QueryRow(ctx, `
        INSERT INTO clusters (name, replication_type)
        VALUES ('crud-cluster', 'binary') RETURNING id
    `).Scan(&clusterID); err != nil {
		t.Fatalf("insert cluster: %v", err)
	}
	primary := env.seed(t, env.user, "192.0.2.10", 5432, "app", "u", nil)
	standby := env.seed(t, env.user, "192.0.2.11", 5432, "app", "u", nil)
	unrelated := env.seed(t, env.user, "192.0.2.12", 5432, "app", "u", nil)
	if _, err := env.pool.Exec(ctx, `
        UPDATE connections SET cluster_id = $1 WHERE id IN ($2, $3, $4);
    `, clusterID, primary, standby, unrelated); err != nil {
		t.Fatalf("assign cluster: %v", err)
	}
	if _, err := env.pool.Exec(ctx, `
        INSERT INTO cluster_node_relationships
            (cluster_id, source_connection_id, target_connection_id,
             relationship_type)
        VALUES ($1, $2, $3, 'streams_from'), ($1, $3, $4, 'streams_from')
    `, clusterID, standby, primary, unrelated); err != nil {
		t.Fatalf("insert relationships: %v", err)
	}
	missing := unrelated + 1000

	t.Run("context method and missing connection", func(t *testing.T) {
		expectStatus(t, env.serve(env.request(http.MethodPost,
			connPath(primary, "/context"), nil, true)),
			http.StatusMethodNotAllowed)
		expectStatus(t, env.serve(env.request(http.MethodGet,
			connPath(missing, "/context"), nil, true)), http.StatusNotFound)
	})

	t.Run("get cluster", func(t *testing.T) {
		rec := env.serve(env.request(http.MethodGet,
			connPath(standby, "/cluster"), nil, true))
		expectStatus(t, rec, http.StatusOK)
		var got connectionClusterResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		// standby takes part in one relationship, primary in both.
		if got.Info == nil || got.Info.ClusterID == nil ||
			*got.Info.ClusterID != clusterID || len(got.Clusters) != 1 ||
			len(got.Relationships) != 1 {
			t.Errorf("unexpected cluster response: %+v", got)
		}

		rec = env.serve(env.request(http.MethodGet,
			connPath(primary, "/cluster"), nil, true))
		expectStatus(t, rec, http.StatusOK)
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(got.Relationships) != 2 {
			t.Errorf("primary relationships = %+v, want two",
				got.Relationships)
		}

		expectStatus(t, env.serve(env.request(http.MethodGet,
			connPath(missing, "/cluster"), nil, true)), http.StatusNotFound)
		expectStatus(t, env.serve(env.request(http.MethodPatch,
			connPath(primary, "/cluster"), nil, true)),
			http.StatusMethodNotAllowed)
	})

	t.Run("get cluster without relationships table", func(t *testing.T) {
		// The relationships lookup failing is logged, not fatal.
		if _, err := env.pool.Exec(ctx,
			"ALTER TABLE cluster_node_relationships RENAME TO cnr_hidden"); err != nil {
			t.Fatalf("rename: %v", err)
		}
		defer func() {
			_, _ = env.pool.Exec(ctx,
				"ALTER TABLE cnr_hidden RENAME TO cluster_node_relationships")
		}()
		rec := env.serve(env.request(http.MethodGet,
			connPath(primary, "/cluster"), nil, true))
		expectStatus(t, rec, http.StatusOK)
		if !strings.Contains(rec.Body.String(), `"relationships":[]`) {
			t.Errorf("want empty relationships, got %s", rec.Body.String())
		}
	})

	t.Run("update cluster", func(t *testing.T) {
		role := "primary"
		cases := []struct {
			name string
			id   int
			body any
			want int
		}{
			{"invalid body", primary, "{", http.StatusBadRequest},
			{"reset to auto", primary, ConnectionClusterUpdateRequest{},
				http.StatusOK},
			{"reset missing connection", missing,
				ConnectionClusterUpdateRequest{},
				http.StatusInternalServerError},
			{"assign with default source", primary,
				ConnectionClusterUpdateRequest{ClusterID: &clusterID,
					Role: &role}, http.StatusOK},
			{"assign missing connection", missing,
				ConnectionClusterUpdateRequest{ClusterID: &clusterID,
					MembershipSource: "manual"},
				http.StatusNotFound},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				expectStatus(t, env.serve(env.request(http.MethodPut,
					connPath(tc.id, "/cluster"), tc.body, true)), tc.want)
			})
		}

		info, err := env.ds.GetConnectionClusterInfo(ctx, primary)
		if err != nil {
			t.Fatalf("GetConnectionClusterInfo: %v", err)
		}
		if info.MembershipSource != "auto" || info.Role == nil ||
			*info.Role != "primary" {
			t.Errorf("unexpected cluster info after assign: %+v", info)
		}
	})

	t.Run("update cluster info reload fails", func(t *testing.T) {
		// Dropping the clusters table leaves the UPDATE working but the
		// reload's join failing.
		if _, err := env.pool.Exec(ctx,
			"ALTER TABLE clusters RENAME TO clusters_hidden"); err != nil {
			t.Fatalf("rename: %v", err)
		}
		defer func() {
			_, _ = env.pool.Exec(ctx,
				"ALTER TABLE clusters_hidden RENAME TO clusters")
		}()
		expectStatus(t, env.serve(env.request(http.MethodPut,
			connPath(primary, "/cluster"),
			ConnectionClusterUpdateRequest{}, true)),
			http.StatusInternalServerError)
		expectStatus(t, env.serve(env.request(http.MethodGet,
			connPath(primary, "/cluster"), nil, true)),
			http.StatusNotFound)
	})
}
