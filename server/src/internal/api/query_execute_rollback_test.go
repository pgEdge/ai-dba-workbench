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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// queryExecTestSchema creates the connections table executeQuery reads to
// find the monitored server it should run the statements against. The
// column list matches the production schema for the columns
// GetConnectionWithPassword scans.
const queryExecTestSchema = `
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

const queryExecTestTeardown = `
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS query_exec_write_target CASCADE;
`

// queryExecTestTarget holds the parsed pieces of the test connection
// string, so the seeded connection row points back at the same local
// instance the test itself is using.
type queryExecTestTarget struct {
	host     string
	port     int
	database string
	username string
}

// newQueryExecTestHandler wires a ConnectionHandler to the
// TEST_AI_WORKBENCH_SERVER Postgres instance. A nil auth store grants
// both read and write access, so the tests exercise the handler body
// rather than the RBAC gate.
func newQueryExecTestHandler(
	t *testing.T,
) (*ConnectionHandler, *pgxpool.Pool, queryExecTestTarget, func()) {
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
		t.Skipf("Could not parse test database connection string: %v", err)
	}
	target := queryExecTestTarget{
		host:     cfg.ConnConfig.Host,
		port:     int(cfg.ConnConfig.Port),
		database: cfg.ConnConfig.Database,
		username: cfg.ConnConfig.User,
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("Test database ping failed: %v", err)
	}
	if _, err := pool.Exec(ctx, queryExecTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create query execution test schema: %v", err)
	}

	handler := NewConnectionHandler(
		database.NewTestDatastore(pool), nil, auth.NewRBACChecker(nil))

	cleanup := func() {
		_, _ = pool.Exec(context.Background(), queryExecTestTeardown)
		pool.Close()
	}
	return handler, pool, target, cleanup
}

// seedQueryExecConnection inserts a connection row pointing at the given
// host and port and returns its ID. The password column stays empty, so
// no server secret is needed and loopback trust authentication applies.
func seedQueryExecConnection(
	t *testing.T,
	pool *pgxpool.Pool,
	target queryExecTestTarget,
	host string,
	port int,
) int {
	t.Helper()

	var id int
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO connections
			(name, host, port, database_name, username, sslmode)
		VALUES ('query-exec-test', $1, $2, $3, $4, 'disable')
		RETURNING id
	`, host, port, target.database, target.username).Scan(&id); err != nil {
		t.Fatalf("Failed to insert connection: %v", err)
	}
	return id
}

// postQuery drives executeQuery with the given request body.
func postQuery(
	t *testing.T,
	h *ConnectionHandler,
	connID int,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/connections/1/query", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.executeQuery(rec, req, connID)
	return rec
}

// decodeMultiQuery decodes an executeQuery response body.
func decodeMultiQuery(t *testing.T, rec *httptest.ResponseRecorder) multiQueryResponse {
	t.Helper()
	var resp multiQueryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode query response: %v (body %q)",
			err, rec.Body.String())
	}
	return resp
}

// TestExecuteQuery_ReadOnlyPathCommitsAndReleases covers the read-only
// branch, where the deferred rollback converted for issue #381 lives. The
// statements commit, so the rollback is the no-op case; the assertions
// confirm the handler still answers with the rows and leaves nothing
// behind.
func TestExecuteQuery_ReadOnlyPathCommitsAndReleases(t *testing.T) {
	h, pool, target, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	connID := seedQueryExecConnection(t, pool, target, target.host, target.port)

	rec := postQuery(t, h, connID,
		`{"query":"SELECT 1 AS one; SELECT 2 AS two;"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %q)",
			rec.Code, http.StatusOK, rec.Body.String())
	}

	resp := decodeMultiQuery(t, rec)
	if resp.TotalStatements != 2 {
		t.Errorf("total statements = %d, want 2", resp.TotalStatements)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("results = %d, want 2", len(resp.Results))
	}
	if resp.Results[0].Error != "" {
		t.Errorf("first statement error = %q, want none", resp.Results[0].Error)
	}
	if len(resp.Results[0].Rows) != 1 || resp.Results[0].Rows[0][0] != "1" {
		t.Errorf("first statement rows = %v, want [[1]]", resp.Results[0].Rows)
	}
	if resp.RequiresConfirmation {
		t.Error("a read-only request must not require confirmation")
	}
}

// TestExecuteQuery_ReadOnlyFailureRollsBack covers the aborted read-only
// transaction: the first statement fails, so the handler skips the commit
// and the deferred rollback unwinds the transaction. The pooled
// datastore connection must be unaffected, and a subsequent query on the
// same handler must still succeed.
func TestExecuteQuery_ReadOnlyFailureRollsBack(t *testing.T) {
	h, pool, target, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	connID := seedQueryExecConnection(t, pool, target, target.host, target.port)

	rec := postQuery(t, h, connID,
		`{"query":"SELECT * FROM no_such_table_for_issue_381; SELECT 1;"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %q)",
			rec.Code, http.StatusOK, rec.Body.String())
	}

	resp := decodeMultiQuery(t, rec)
	if len(resp.Results) != 1 {
		t.Fatalf("results = %d, want 1 (execution stops at the first error)",
			len(resp.Results))
	}
	if resp.Results[0].Error == "" {
		t.Error("expected an error for the missing relation")
	}

	// The rollback must leave everything usable: run a clean query after.
	rec = postQuery(t, h, connID, `{"query":"SELECT 1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("follow-up status = %d, want %d (body %q)",
			rec.Code, http.StatusOK, rec.Body.String())
	}
	if acquired := pool.Stat().AcquiredConns(); acquired != 0 {
		t.Errorf("acquired datastore connections = %d, want 0", acquired)
	}
}

// TestExecuteQuery_WriteRequiresConfirmation covers the confirmation
// prompt returned for unconfirmed write statements, before any database
// connection is opened.
func TestExecuteQuery_WriteRequiresConfirmation(t *testing.T) {
	h, pool, target, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	connID := seedQueryExecConnection(t, pool, target, target.host, target.port)

	rec := postQuery(t, h, connID,
		`{"query":"CREATE TABLE query_exec_write_target (id int)"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	resp := decodeMultiQuery(t, rec)
	if !resp.RequiresConfirmation {
		t.Fatal("expected requires_confirmation for an unconfirmed write")
	}
	if len(resp.WriteStatements) != 1 {
		t.Errorf("write statements = %v, want 1", resp.WriteStatements)
	}
	if !strings.Contains(resp.ConfirmationMessage, "1 write statement") {
		t.Errorf("confirmation message = %q", resp.ConfirmationMessage)
	}
}

// TestExecuteQuery_ConfirmedWritePath covers the write branch, which runs
// each statement outside a transaction and reports a success row for the
// ones that are not read-only.
func TestExecuteQuery_ConfirmedWritePath(t *testing.T) {
	h, pool, target, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	connID := seedQueryExecConnection(t, pool, target, target.host, target.port)

	rec := postQuery(t, h, connID, `{"query":"CREATE TABLE `+
		`query_exec_write_target (id int); SELECT count(*) FROM `+
		`query_exec_write_target;","confirmed":true}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %q)",
			rec.Code, http.StatusOK, rec.Body.String())
	}
	resp := decodeMultiQuery(t, rec)
	if len(resp.Results) != 2 {
		t.Fatalf("results = %d, want 2", len(resp.Results))
	}
	if resp.Results[0].RowCount != 1 ||
		resp.Results[0].Rows[0][0] != "Statement executed successfully" {
		t.Errorf("write result = %+v, want the success row", resp.Results[0])
	}
	if resp.Results[1].Error != "" {
		t.Errorf("read-back error = %q, want none", resp.Results[1].Error)
	}
}

// TestExecuteQuery_ConfirmedWriteFailureReported covers the write branch's
// error path, where a statement fails and execution stops.
func TestExecuteQuery_ConfirmedWriteFailureReported(t *testing.T) {
	h, pool, target, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	connID := seedQueryExecConnection(t, pool, target, target.host, target.port)

	rec := postQuery(t, h, connID,
		`{"query":"DROP TABLE no_such_table_for_issue_381; SELECT 1;",`+
			`"confirmed":true}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	resp := decodeMultiQuery(t, rec)
	if len(resp.Results) != 1 {
		t.Fatalf("results = %d, want 1 (execution stops at the first error)",
			len(resp.Results))
	}
	if resp.Results[0].Error == "" {
		t.Error("expected an error for dropping a missing table")
	}
}

// TestExecuteQuery_ExplainWithParametersUsesSimpleProtocol covers the
// simple-protocol branch taken by EXPLAIN statements that contain $N
// placeholders, which bypasses the transaction path entirely.
func TestExecuteQuery_ExplainWithParametersUsesSimpleProtocol(t *testing.T) {
	h, pool, target, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	connID := seedQueryExecConnection(t, pool, target, target.host, target.port)

	rec := postQuery(t, h, connID,
		`{"query":"EXPLAIN SELECT * FROM connections WHERE id = $1"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %q)",
			rec.Code, http.StatusOK, rec.Body.String())
	}
	resp := decodeMultiQuery(t, rec)
	if len(resp.Results) != 1 {
		t.Fatalf("results = %d, want 1", len(resp.Results))
	}
	if resp.Results[0].Error == "" && len(resp.Results[0].Rows) == 0 {
		t.Error("expected either plan rows or an error from the EXPLAIN")
	}
}

// TestExecuteQuery_UnreachableServerReportsError covers the
// begin-transaction failure branch, reached when the monitored server
// cannot be contacted at all. No transaction exists, so the deferred
// rollback is never registered.
func TestExecuteQuery_UnreachableServerReportsError(t *testing.T) {
	h, pool, target, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	// Port 1 is privileged and unused, so the connection attempt fails
	// promptly rather than hanging.
	connID := seedQueryExecConnection(t, pool, target, "127.0.0.1", 1)

	rec := postQuery(t, h, connID, `{"query":"SELECT 1"}`)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d (body %q)",
			rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
}

// TestExecuteQuery_InvalidConnectionStringReportsError covers the
// connection-string parse failure branch. A host containing a character
// that is illegal in a URL authority makes pgxpool.ParseConfig fail
// before any network activity.
func TestExecuteQuery_InvalidConnectionStringReportsError(t *testing.T) {
	h, pool, target, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	connID := seedQueryExecConnection(t, pool, target, "bad host[", 5432)

	rec := postQuery(t, h, connID, `{"query":"SELECT 1"}`)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d (body %q)",
			rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
}

// TestExecuteQuery_ExplainAcquireFailureReportsError covers the
// connection-acquisition failure inside the simple-protocol EXPLAIN
// branch, which is a separate error path from the transactional one.
func TestExecuteQuery_ExplainAcquireFailureReportsError(t *testing.T) {
	h, pool, target, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	connID := seedQueryExecConnection(t, pool, target, "127.0.0.1", 1)

	rec := postQuery(t, h, connID,
		`{"query":"EXPLAIN SELECT * FROM connections WHERE id = $1"}`)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d (body %q)",
			rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
}

// TestExecuteQuery_ConfirmedWriteStopsOnReadFailure covers the write
// path's read-only statement error branch, where a SELECT following a
// successful write fails and execution stops.
func TestExecuteQuery_ConfirmedWriteStopsOnReadFailure(t *testing.T) {
	h, pool, target, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	connID := seedQueryExecConnection(t, pool, target, target.host, target.port)

	rec := postQuery(t, h, connID, `{"query":"CREATE TABLE `+
		`query_exec_write_target (id int); SELECT * FROM `+
		`no_such_table_for_issue_381; SELECT 1;","confirmed":true}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %q)",
			rec.Code, http.StatusOK, rec.Body.String())
	}
	resp := decodeMultiQuery(t, rec)
	if len(resp.Results) != 2 {
		t.Fatalf("results = %d, want 2 (execution stops after the failure)",
			len(resp.Results))
	}
	if resp.Results[1].Error == "" {
		t.Error("expected an error for the missing relation")
	}
}

// TestExecuteQuery_DeniesUnauthorisedCallers covers the two RBAC gates:
// the read gate that rejects a caller with no access to the connection,
// and the write gate that rejects a read-only caller who confirms a write
// statement. Both must refuse before any database connection is opened.
func TestExecuteQuery_DeniesUnauthorisedCallers(t *testing.T) {
	_, pool, target, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	connID := seedQueryExecConnection(t, pool, target, target.host, target.port)
	ds := database.NewTestDatastore(pool)

	t.Run("no read access", func(t *testing.T) {
		store, storeCleanup := createTestAuthStoreForAlertOverrides(t)
		defer storeCleanup()

		// An unshared connection owned by somebody else denies an
		// anonymous caller outright.
		checker := auth.NewRBACCheckerWithSharing(store,
			func(context.Context, int) (bool, string, error) {
				return false, "someone-else", nil
			})
		h := NewConnectionHandler(ds, store, checker)

		rec := postQuery(t, h, connID, `{"query":"SELECT 1"}`)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d (body %q)",
				rec.Code, http.StatusForbidden, rec.Body.String())
		}
	})

	t.Run("no write access", func(t *testing.T) {
		store, storeCleanup := createTestAuthStoreForAlertOverrides(t)
		defer storeCleanup()

		userID := newGroupGrantedUser(t, store, "readonly_user", connID,
			auth.AccessLevelRead)
		h := NewConnectionHandler(ds, store, auth.NewRBACChecker(store))

		ctx := context.WithValue(context.Background(),
			auth.UserIDContextKey, userID)
		ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, false)
		ctx = context.WithValue(ctx, auth.UsernameContextKey, "readonly_user")

		req := httptest.NewRequest(http.MethodPost,
			"/api/v1/connections/1/query",
			strings.NewReader(`{"query":"CREATE TABLE nope (id int)",`+
				`"confirmed":true}`)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		h.executeQuery(rec, req, connID)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d (body %q)",
				rec.Code, http.StatusForbidden, rec.Body.String())
		}
		want := "Permission denied: you do not have write access to this connection"
		if got := decodeError(t, rec).Error; got != want {
			t.Errorf("error = %q, want %q", got, want)
		}
	})
}

// TestExecuteQuery_RejectsInvalidRequests covers the method guard, the
// body decoding failure, the empty-query guards, and the missing
// connection lookup.
func TestExecuteQuery_RejectsInvalidRequests(t *testing.T) {
	h, pool, target, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	connID := seedQueryExecConnection(t, pool, target, target.host, target.port)

	t.Run("method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/connections/1/query", nil)
		rec := httptest.NewRecorder()

		h.executeQuery(rec, req, connID)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
		}
		if got := rec.Header().Get("Allow"); got != http.MethodPost {
			t.Errorf("Allow header = %q, want %q", got, http.MethodPost)
		}
	})

	t.Run("malformed body", func(t *testing.T) {
		rec := postQuery(t, h, connID, `{"query":`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("empty query", func(t *testing.T) {
		rec := postQuery(t, h, connID, `{"query":"   "}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
		if got := decodeError(t, rec).Error; got != "Query is required" {
			t.Errorf("error = %q, want \"Query is required\"", got)
		}
	})

	t.Run("comments only", func(t *testing.T) {
		rec := postQuery(t, h, connID, `{"query":"-- nothing to see here"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d (body %q)",
				rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	})

	t.Run("connection not found", func(t *testing.T) {
		rec := postQuery(t, h, 999999, `{"query":"SELECT 1"}`)
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})
}
