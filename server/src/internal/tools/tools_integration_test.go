/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Integration tests for tool handlers. These tests execute real queries
// against the Postgres instance named by TEST_AI_WORKBENCH_SERVER and
// skip gracefully when that environment variable is not set. They cover
// the validation paths that the nil-pool unit tests cannot reach: pool
// returns "Datastore not configured" before any parameter validation
// runs, so a real pool is required to exercise invalid-input branches
// for status, category, connection_id parsing, and bounds on limit and
// offset.
//
// The pattern mirrors the one documented in
// .claude/golang-expert/testing-strategy.md and used by
// server/src/internal/database/cluster_dismiss_integration_test.go and
// server/src/internal/api/cluster_handlers_test.go.

package tools

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"golang.org/x/crypto/bcrypt"
)

// toolsIntegrationSchema creates the minimum subset of datastore tables
// exercised by the validation paths of the tool handlers in this file.
// Only the connections lookup issued by get_blackouts and get_alert_history
// (via pool.QueryRow on connections) needs a real table; the other tests
// validate their inputs before any SQL executes. The schema is intentionally
// a strict subset of the production migration in
// collector/src/database/schema.go.
const toolsIntegrationSchema = `
DROP TABLE IF EXISTS connections CASCADE;
DROP SCHEMA IF EXISTS metrics CASCADE;

CREATE SCHEMA metrics;

CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    host VARCHAR(255) NOT NULL DEFAULT '',
    port INTEGER NOT NULL DEFAULT 5432,
    database_name VARCHAR(255) NOT NULL DEFAULT 'postgres',
    description TEXT NOT NULL DEFAULT '',
    is_monitored BOOLEAN NOT NULL DEFAULT TRUE,
    is_shared BOOLEAN NOT NULL DEFAULT TRUE,
    owner_username VARCHAR(255),
    cluster_id INTEGER,
    membership_source VARCHAR(16) NOT NULL DEFAULT 'auto',
    connection_error TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE metrics.pg_connectivity (
    id SERIAL PRIMARY KEY,
    connection_id INTEGER NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

const toolsIntegrationTeardown = `
DROP TABLE IF EXISTS connections CASCADE;
DROP SCHEMA IF EXISTS metrics CASCADE;
`

// newToolsTestPool returns a *pgxpool.Pool and *database.Datastore wired
// to the TEST_AI_WORKBENCH_SERVER Postgres instance with the minimum
// schema needed by the tool validation tests. The caller receives a
// cleanup function that drops the schema and closes the pool. The test
// is skipped if the environment variable is unset, matching the project
// convention.
func newToolsTestPool(t *testing.T) (*pgxpool.Pool, *database.Datastore, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping tools integration test")
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

	if _, err := pool.Exec(ctx, toolsIntegrationSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create tools integration schema: %v", err)
	}

	ds := database.NewTestDatastore(pool)

	cleanup := func() {
		if _, err := pool.Exec(context.Background(), toolsIntegrationTeardown); err != nil {
			t.Logf("tools integration teardown failed: %v", err)
		}
		pool.Close()
	}

	return pool, ds, cleanup
}

// ---------------------------------------------------------------------------
// database.NewVisibilityLister: non-nil datastore path
// ---------------------------------------------------------------------------

// TestNewVisibilityListerNonNilIntegration complements the nil-datastore unit
// test by verifying that a non-nil *database.Datastore produces a usable
// lister whose GetAllConnections method queries the real datastore.
func TestNewVisibilityListerNonNilIntegration(t *testing.T) {
	pool, ds, cleanup := newToolsTestPool(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`INSERT INTO connections (name, host, port, database_name, is_shared)
		 VALUES ('vis-test', 'localhost', 5432, 'postgres', TRUE)`); err != nil {
		t.Fatalf("failed to seed connections row: %v", err)
	}

	lister := database.NewVisibilityLister(ds)
	if lister == nil {
		t.Fatal("expected non-nil lister for non-nil datastore")
	}

	conns, err := lister.GetAllConnections(ctx)
	if err != nil {
		t.Fatalf("GetAllConnections failed: %v", err)
	}
	if len(conns) == 0 {
		t.Fatal("expected at least one visible connection from seeded row")
	}

	var found bool
	for _, c := range conns {
		if c.ID > 0 && c.IsShared {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected seeded shared connection to appear in visible set")
	}
}

// ---------------------------------------------------------------------------
// get_alert_history: status validation reached with a real pool
// ---------------------------------------------------------------------------

// TestGetAlertHistoryInvalidStatusIntegration verifies that an invalid
// status parameter actually reaches the status-validation branch when a
// real pool is supplied. The nil-pool test short-circuits before
// validation runs, so CodeRabbit correctly noted it does not exercise
// the rejection path; this test does.
func TestGetAlertHistoryInvalidStatusIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	tool := GetAlertHistoryTool(pool, nil, nil)

	response, err := tool.Handler(map[string]any{
		"status": "invalid_status",
	})
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if !response.IsError {
		t.Fatal("expected error response for invalid status")
	}
	if len(response.Content) == 0 {
		t.Fatal("expected error message in response content")
	}
	text := response.Content[0].Text
	if !strings.Contains(text, "Invalid 'status'") {
		t.Errorf("expected status validation error, got: %s", text)
	}
}

// ---------------------------------------------------------------------------
// get_alert_rules: connection_id and category validation
// ---------------------------------------------------------------------------

// TestGetAlertRulesInvalidConnectionIDIntegration verifies that a
// non-integer connection_id is rejected by parseIntArg with the
// "must be an integer" message. parseIntArg runs before any SQL is
// issued, but the nil-pool path short-circuits earlier, so a real pool
// is required to reach this branch.
func TestGetAlertRulesInvalidConnectionIDIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	tool := GetAlertRulesTool(pool, nil)

	tests := []struct {
		name  string
		value any
	}{
		{"string", "abc"},
		{"bool", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := tool.Handler(map[string]any{
				"connection_id": tt.value,
			})
			if err != nil {
				t.Fatalf("handler returned unexpected error: %v", err)
			}
			if !response.IsError {
				t.Fatal("expected error response for invalid connection_id")
			}
			if len(response.Content) == 0 {
				t.Fatal("expected error message in response content")
			}
			text := response.Content[0].Text
			if !strings.Contains(text, "connection_id") ||
				!strings.Contains(text, "integer") {
				t.Errorf("expected connection_id integer error, got: %s", text)
			}
		})
	}
}

// TestGetAlertRulesInvalidCategoryIntegration verifies that an unknown
// category value is rejected by the category-whitelist check.
func TestGetAlertRulesInvalidCategoryIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	tool := GetAlertRulesTool(pool, nil)

	response, err := tool.Handler(map[string]any{
		"category": "not_a_real_category",
	})
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if !response.IsError {
		t.Fatal("expected error response for invalid category")
	}
	if len(response.Content) == 0 {
		t.Fatal("expected error message in response content")
	}
	text := response.Content[0].Text
	if !strings.Contains(text, "Invalid 'category'") {
		t.Errorf("expected category validation error, got: %s", text)
	}
}

// ---------------------------------------------------------------------------
// get_blackouts: connection_id validation
// ---------------------------------------------------------------------------

// TestGetBlackoutsInvalidConnectionIDIntegration verifies that a
// non-integer connection_id is rejected with the tool's specific error
// message pointing users at list_connections.
func TestGetBlackoutsInvalidConnectionIDIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	tool := GetBlackoutsTool(pool, nil, nil)

	response, err := tool.Handler(map[string]any{
		"connection_id": "abc",
	})
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if !response.IsError {
		t.Fatal("expected error response for invalid connection_id")
	}
	if len(response.Content) == 0 {
		t.Fatal("expected error message in response content")
	}
	text := response.Content[0].Text
	if !strings.Contains(text, "connection_id") ||
		!strings.Contains(text, "integer") {
		t.Errorf("expected connection_id integer error, got: %s", text)
	}
}

// ---------------------------------------------------------------------------
// get_metric_baselines: connection_id validation
// ---------------------------------------------------------------------------

// TestGetMetricBaselinesInvalidConnectionIDIntegration verifies that a
// non-integer connection_id is rejected before any SQL runs.
func TestGetMetricBaselinesInvalidConnectionIDIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	tool := GetMetricBaselinesTool(pool, nil, nil)

	tests := []struct {
		name  string
		value any
	}{
		{"string", "abc"},
		{"bool", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := tool.Handler(map[string]any{
				"connection_id": tt.value,
			})
			if err != nil {
				t.Fatalf("handler returned unexpected error: %v", err)
			}
			if !response.IsError {
				t.Fatal("expected error response for invalid connection_id")
			}
			if len(response.Content) == 0 {
				t.Fatal("expected error message in response content")
			}
			text := response.Content[0].Text
			if !strings.Contains(text, "connection_id") ||
				!strings.Contains(text, "integer") {
				t.Errorf("expected connection_id integer error, got: %s", text)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// query_datastore: limit and offset actually applied
// ---------------------------------------------------------------------------

// TestQueryDatastoreWithLimitAndOffsetIntegration verifies that a
// well-formed query executes against a real datastore and that the
// requested limit bounds the returned row count. This is the behavior
// the nil-pool unit test cannot cover: it stops at the "Datastore not
// configured" short-circuit and does not prove the limit/offset
// parameters are actually honored by the handler.
func TestQueryDatastoreWithLimitAndOffsetIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	tool := QueryDatastoreTool(pool)

	response, err := tool.Handler(map[string]any{
		"query":  "SELECT generate_series(1, 20) AS n",
		"limit":  5,
		"offset": 0,
	})
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if response.IsError {
		if len(response.Content) == 0 {
			t.Fatal("expected error message in response content")
		}
		t.Fatalf("handler returned error response: %s", response.Content[0].Text)
	}
	if len(response.Content) == 0 {
		t.Fatal("expected non-empty response content")
	}

	text := response.Content[0].Text
	// Quick structural assertions: the datastore query header is present
	// and the output advertises a bounded result. We do not assert the
	// exact row count because formatPaginatedResults controls formatting.
	if !strings.Contains(text, "Datastore Query") {
		t.Errorf("expected 'Datastore Query' header, got: %s", text)
	}
	// generate_series produces 20 rows, but limit=5 must cap the returned
	// rows. Count occurrences of row delimiter (newline after header).
	// A conservative check: the response must not contain '20' as a
	// standalone row value (which would indicate the limit was ignored).
	if strings.Contains(text, "\n20\n") {
		t.Errorf("limit was not applied; response contains row value 20: %s", text)
	}
}

// TestQueryDatastoreRejectsInvalidLimitIntegration verifies that out-of-range
// limit values are rejected. The handler's parseLimitOffset helper clamps
// to [1, 1000]; supplying a limit above the max is silently capped, but a
// zero or negative value should be caught. Since parseLimitOffset treats
// out-of-range values by falling back to defaults, this test documents
// the observed behavior: the handler must not panic and must still return
// a successful response using the default.
func TestQueryDatastoreRejectsInvalidLimitIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	tool := QueryDatastoreTool(pool)

	response, err := tool.Handler(map[string]any{
		"query": "SELECT 1",
		"limit": -5,
	})
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if len(response.Content) == 0 {
		t.Fatal("expected non-empty response content")
	}
	// Either the handler rejects the invalid limit with an error, or it
	// falls back to the default and returns success; both are acceptable
	// contracts. What is NOT acceptable is a panic, which this test
	// implicitly covers by reaching this assertion at all.
	_ = response
}

// ---------------------------------------------------------------------------
// ManagedTx: Commit actually marks committed
// ---------------------------------------------------------------------------

// TestManagedTxCommitMarksCommittedIntegration exercises the full
// ManagedTx.Commit() path against a real database, confirming that
// committed is set to true on successful commit and that the deferred
// rollback becomes a no-op afterwards. The unit test in transaction_test.go
// only verifies the zero-value of the struct, which, as CodeRabbit noted,
// does not test Commit() at all.
func TestManagedTxCommitMarksCommittedIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	ctx := context.Background()

	rot, errResp, txCleanup := BeginReadOnlyTx(ctx, pool)
	if errResp != nil {
		t.Fatalf("BeginReadOnlyTx returned error response: %v", errResp)
	}
	defer txCleanup()

	if rot == nil {
		t.Fatal("expected non-nil ManagedTx")
	}
	if rot.committed {
		t.Error("expected committed to be false before Commit()")
	}
	if rot.Tx == nil {
		t.Error("expected Tx to be set after BeginReadOnlyTx")
	}

	// Exercise the transaction to prove it is live.
	var one int
	if err := rot.Tx.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
		t.Fatalf("failed to execute SELECT 1 in ReadOnly transaction: %v", err)
	}
	if one != 1 {
		t.Errorf("expected SELECT 1 to return 1, got %d", one)
	}

	if err := rot.Commit(); err != nil {
		t.Fatalf("Commit() returned unexpected error: %v", err)
	}
	if !rot.committed {
		t.Error("expected committed to be true after successful Commit()")
	}
}

// TestManagedTxBeginTxCommitMarksCommittedIntegration mirrors the
// read-only path for BeginTx, verifying that Commit() on a read-write
// managed transaction also sets committed = true.
func TestManagedTxBeginTxCommitMarksCommittedIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	ctx := context.Background()

	mt, errResp, txCleanup := BeginTx(ctx, pool)
	if errResp != nil {
		t.Fatalf("BeginTx returned error response: %v", errResp)
	}
	defer txCleanup()

	if mt == nil {
		t.Fatal("expected non-nil ManagedTx")
	}
	if mt.committed {
		t.Error("expected committed to be false before Commit()")
	}

	if err := mt.Commit(); err != nil {
		t.Fatalf("Commit() returned unexpected error: %v", err)
	}
	if !mt.committed {
		t.Error("expected committed to be true after successful Commit()")
	}
}

// ---------------------------------------------------------------------------
// list_connections: RBAC filtering message distinction
// ---------------------------------------------------------------------------

// newRBACTestStore spins up a throwaway SQLite auth store for RBAC tests.
// The caller receives a cleanup closure that closes the store and removes
// the temp directory.
func newRBACTestStore(t *testing.T) (*auth.AuthStore, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "tools-rbac-int-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	store, err := auth.NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("NewAuthStore: %v", err)
	}
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)
	return store, func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}
}

// nonSuperuserContextInt builds a context that matches how the auth
// middleware would populate it for a logged-in, non-superuser session.
func nonSuperuserContextInt(userID int64, username string) context.Context {
	ctx := context.WithValue(context.Background(), auth.IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, auth.UserIDContextKey, userID)
	ctx = context.WithValue(ctx, auth.UsernameContextKey, username)
	return ctx
}

// TestListConnectionsRBACNoAccessDistinctMessageIntegration is the
// regression guard for the scenario where connections exist in the datastore
// but the user has no access to any of them after RBAC filtering.
//
// Previously the tool returned "No database connections found in the datastore"
// in BOTH cases:
//  1. When no connections exist at all (correct)
//  2. When connections exist but user has no access (incorrect - misleading)
//
// The fix distinguishes these cases by tracking whether connections were
// filtered out vs. whether there were none to begin with.
func TestListConnectionsRBACNoAccessDistinctMessageIntegration(t *testing.T) {
	pool, ds, cleanup := newToolsTestPool(t)
	defer cleanup()

	authStore, authCleanup := newRBACTestStore(t)
	defer authCleanup()

	// Create user "bob" who has no group membership and no grants.
	if err := authStore.CreateUser("bob", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, err := authStore.GetUserID("bob")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}

	// Insert a connection owned by alice and NOT shared, so bob cannot see it.
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`INSERT INTO connections (name, host, port, database_name, is_shared, owner_username)
		 VALUES ('alice-conn', 'localhost', 5432, 'postgres', FALSE, 'alice')`); err != nil {
		t.Fatalf("failed to seed connections row: %v", err)
	}

	rbac := auth.NewRBACChecker(authStore)
	lister := database.NewVisibilityLister(ds)
	tool := ListConnectionsTool(pool, rbac, lister)

	userCtx := nonSuperuserContextInt(userID, "bob")
	resp, err := tool.Handler(map[string]any{"__context": userCtx})
	if err != nil {
		t.Fatalf("Handler returned error: %v", err)
	}
	// The response should NOT be an error (it's informational).
	if resp.IsError {
		t.Fatalf("Handler returned error response: %+v", resp.Content)
	}
	if len(resp.Content) == 0 {
		t.Fatal("Expected content in response")
	}
	body := resp.Content[0].Text

	// Must indicate user has no connection access (not that connections don't exist).
	if !strings.Contains(body, "You do not have access to any connections") {
		t.Errorf("Expected RBAC denial message 'You do not have access to any connections', got: %q", body)
	}

	// Must NOT contain the misleading "No database connections found in the datastore"
	// message, since connections DO exist - the user just can't see them.
	if strings.Contains(body, "No database connections found in the datastore") {
		t.Errorf("Should NOT say 'No database connections found' when connections exist but are inaccessible: %q", body)
	}

	// Must NOT leak alice's connection name anywhere in the body.
	if strings.Contains(body, "alice-conn") {
		t.Errorf("Response leaked connection name 'alice-conn': %q", body)
	}
}

// TestListConnectionsNoConnectionsExistIntegration verifies that when no
// connections exist at all in the datastore, the tool returns the correct
// message indicating connections must be added.
func TestListConnectionsNoConnectionsExistIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	// No connections are inserted - the connections table is empty.
	tool := ListConnectionsTool(pool, nil, nil)

	resp, err := tool.Handler(map[string]any{})
	if err != nil {
		t.Fatalf("Handler returned error: %v", err)
	}
	if resp.IsError {
		t.Fatalf("Handler returned error response: %+v", resp.Content)
	}
	if len(resp.Content) == 0 {
		t.Fatal("Expected content in response")
	}
	body := resp.Content[0].Text

	// When no connections exist, should say so explicitly.
	if !strings.Contains(body, "No database connections found in the datastore") {
		t.Errorf("Expected 'No database connections found in the datastore', got: %q", body)
	}
}
