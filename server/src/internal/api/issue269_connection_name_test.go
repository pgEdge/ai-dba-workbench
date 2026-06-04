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
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// =============================================================================
// Regression tests for GitHub issue #269: server connection name character
// validation. createConnection and updateConnection previously rejected only
// empty names; they now funnel through ValidateDisplayName and reject names
// containing disallowed characters with HTTP 400 and a user-facing message.
//
// createConnection validates before any datastore access, so its test uses
// the nil-datastore admin harness from the issue #233 suite. updateConnection
// loads the existing row first, so its tests seed a real connection in the
// Postgres named by TEST_AI_WORKBENCH_SERVER.
// =============================================================================

// issue269ConnectionSchema is a self-contained connections table carrying
// every column GetConnection and UpdateConnectionFull read or write, so the
// updateConnection tests exercise the real datastore path end to end.
const issue269ConnectionSchema = `
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
    cluster_id INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

// newIssue269ConnectionDatastore wires a *database.Datastore to the Postgres
// named by TEST_AI_WORKBENCH_SERVER and installs issue269ConnectionSchema.
// The test is skipped when the database environment is not configured.
func newIssue269ConnectionDatastore(t *testing.T) (*database.Datastore, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping issue #269 connection test")
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
	if _, err := pool.Exec(ctx, issue269ConnectionSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create issue #269 connection schema: %v", err)
	}

	ds := database.NewTestDatastore(pool)
	cleanup := func() {
		_, _ = pool.Exec(context.Background(),
			"DROP TABLE IF EXISTS connections CASCADE")
		pool.Close()
	}
	return ds, pool, cleanup
}

// TestConnectionHandler_CreateConnection_Issue269_InvalidChars confirms that
// an admin caller supplying a name with disallowed characters is rejected
// with 400 before the connection is written.
func TestConnectionHandler_CreateConnection_Issue269_InvalidChars(t *testing.T) {
	handler, userID, token, cleanup := setupIssue233CreateConnection(
		t, "issue269_create_invalid",
		[]string{auth.PermManageConnections})
	defer cleanup()

	body, _ := json.Marshal(ConnectionCreateRequest{
		Name:         issue269InvalidName,
		Host:         "db.example.com",
		Port:         5432,
		DatabaseName: "postgres",
		Username:     "alice",
		Password:     "secret",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connections",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withBearer(req, token)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.createConnection(rec, req)

	assertInvalidNameRejected(t, rec)
}

// TestConnectionHandler_CreateConnection_Issue269_PaddedNameRawTooLong covers
// the whitespace-padding edge case at the handler level: a name whose trimmed
// length is within the 255-character limit but whose raw length exceeds it
// (250 content characters plus 10 trailing spaces) must be rejected with a
// 400 before any datastore write. Because the handler persists the raw value
// to the VARCHAR(255) column, ValidateDisplayName now rejects on the raw
// length and the over-length value never reaches the column.
func TestConnectionHandler_CreateConnection_Issue269_PaddedNameRawTooLong(t *testing.T) {
	handler, userID, token, cleanup := setupIssue233CreateConnection(
		t, "issue269_create_padded_toolong",
		[]string{auth.PermManageConnections})
	defer cleanup()

	paddedName := strings.Repeat("a", 250) + strings.Repeat(" ", 10)
	body, _ := json.Marshal(ConnectionCreateRequest{
		Name:         paddedName,
		Host:         "db.example.com",
		Port:         5432,
		DatabaseName: "postgres",
		Username:     "alice",
		Password:     "secret",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connections",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withBearer(req, token)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.createConnection(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusBadRequest, rec.Code, rec.Body.String())
	}
	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	const wantMsg = "Name must be 255 characters or fewer"
	if response.Error != wantMsg {
		t.Errorf("Expected %q, got %q", wantMsg, response.Error)
	}
}

// seedIssue269Connection inserts a connection owned by the named user and
// returns its ID. The caller supplies a distinct ID per test to avoid
// collisions within a shared schema.
func seedIssue269Connection(t *testing.T, pool *pgxpool.Pool, id int, owner, name string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
        INSERT INTO connections (id, name, description, host, port,
            database_name, username, is_shared, owner_username,
            membership_source)
        VALUES ($1, $2, '', 'db.example.com', 5432, 'postgres',
            $3, FALSE, $3, 'manual')
    `, id, name, owner)
	if err != nil {
		t.Fatalf("Seed connection: %v", err)
	}
}

// TestConnectionHandler_UpdateConnection_Issue269_InvalidChars seeds a real
// connection owned by the caller, then confirms that a PUT supplying a name
// with disallowed characters is rejected with 400.
func TestConnectionHandler_UpdateConnection_Issue269_InvalidChars(t *testing.T) {
	ds, pool, cleanupDS := newIssue269ConnectionDatastore(t)
	defer cleanupDS()

	_, store, cleanupStore := createTestRBACHandler(t)
	defer cleanupStore()

	const owner = "issue269_update_invalid"
	userID := setupUserWithPermissions(t, store, owner,
		[]string{auth.PermManageConnections})
	token, _, err := store.AuthenticateUser(owner, "Password1234")
	if err != nil {
		t.Fatalf("AuthenticateUser: %v", err)
	}

	const connID = 7269
	seedIssue269Connection(t, pool, connID, owner, "valid-name")

	checker := auth.NewRBACChecker(store)
	handler := NewConnectionHandler(ds, store, checker)

	invalid := issue269InvalidName
	body, _ := json.Marshal(ConnectionFullUpdateRequest{Name: &invalid})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/connections/7269",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withBearer(req, token)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.updateConnection(rec, req, connID)

	assertInvalidNameRejected(t, rec)
}

// TestConnectionHandler_UpdateConnection_Issue269_ValidNameSucceeds confirms
// that a valid name still updates the connection successfully, so the new
// validation does not over-reject legitimate input.
func TestConnectionHandler_UpdateConnection_Issue269_ValidNameSucceeds(t *testing.T) {
	ds, pool, cleanupDS := newIssue269ConnectionDatastore(t)
	defer cleanupDS()

	_, store, cleanupStore := createTestRBACHandler(t)
	defer cleanupStore()

	const owner = "issue269_update_valid"
	userID := setupUserWithPermissions(t, store, owner,
		[]string{auth.PermManageConnections})
	token, _, err := store.AuthenticateUser(owner, "Password1234")
	if err != nil {
		t.Fatalf("AuthenticateUser: %v", err)
	}

	const connID = 7270
	seedIssue269Connection(t, pool, connID, owner, "old-name")

	checker := auth.NewRBACChecker(store)
	handler := NewConnectionHandler(ds, store, checker)

	valid := "New Cluster (primary) - east_1.db"
	body, _ := json.Marshal(ConnectionFullUpdateRequest{Name: &valid})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/connections/7270",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withBearer(req, token)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()

	handler.updateConnection(rec, req, connID)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rec.Code, rec.Body.String())
	}

	var stored string
	if err := pool.QueryRow(context.Background(),
		"SELECT name FROM connections WHERE id = $1", connID).Scan(&stored); err != nil {
		t.Fatalf("Read back name: %v", err)
	}
	if stored != valid {
		t.Errorf("Stored name = %q, want %q", stored, valid)
	}
}
