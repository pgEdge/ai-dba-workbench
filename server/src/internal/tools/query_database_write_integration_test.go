/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package tools

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// newMonitoredStylePool returns a pool built by database.Client, the
// same way query_database obtains pools for monitored databases, so it
// carries the default_transaction_read_only=on session setting. It also
// returns a plain pool for setup and teardown, since the monitored-style
// pool cannot run DDL outside BeginTx.
func newMonitoredStylePool(t *testing.T) (monitored, admin *pgxpool.Pool, cleanup func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping query_database write integration test")
	}

	ctx := context.Background()
	admin, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	if err := admin.Ping(ctx); err != nil {
		admin.Close()
		t.Skipf("Test database ping failed: %v", err)
	}

	client := database.NewClient(nil)
	if err := client.ConnectTo(connStr); err != nil {
		admin.Close()
		t.Fatalf("database.Client.ConnectTo failed: %v", err)
	}
	monitored = client.GetPoolFor(connStr)
	if monitored == nil {
		client.Close()
		admin.Close()
		t.Fatal("database.Client returned no pool for the test connection")
	}

	return monitored, admin, func() {
		client.Close()
		admin.Close()
	}
}

// TestMonitoredPoolDefaultsToReadOnly pins the precondition that issue
// #663 depends on: a plain BEGIN on a monitored-database pool opens a
// read-only transaction, so the write path must override it.
func TestMonitoredPoolDefaultsToReadOnly(t *testing.T) {
	monitored, _, cleanup := newMonitoredStylePool(t)
	defer cleanup()

	var readOnly string
	if err := monitored.QueryRow(context.Background(),
		"SHOW transaction_read_only").Scan(&readOnly); err != nil {
		t.Fatalf("SHOW transaction_read_only failed: %v", err)
	}
	if readOnly != "on" {
		t.Fatalf("monitored pool transaction_read_only = %q, want \"on\"", readOnly)
	}
}

// TestBeginTxOverridesReadOnlySessionDefault verifies that BeginTx opens
// a read-write transaction even when the session default is read-only,
// so that writes and nextval succeed (issue #663).
func TestBeginTxOverridesReadOnlySessionDefault(t *testing.T) {
	monitored, _, cleanup := newMonitoredStylePool(t)
	defer cleanup()

	ctx := context.Background()
	mt, errResp, txCleanup := BeginTx(ctx, monitored)
	if errResp != nil {
		t.Fatalf("BeginTx returned error response: %v", errResp)
	}
	defer txCleanup()

	var readOnly string
	if err := mt.Tx.QueryRow(ctx, "SHOW transaction_read_only").Scan(&readOnly); err != nil {
		t.Fatalf("SHOW transaction_read_only failed: %v", err)
	}
	if readOnly != "off" {
		t.Errorf("BeginTx transaction_read_only = %q, want \"off\"", readOnly)
	}

	if _, err := mt.Tx.Exec(ctx, "CREATE TEMP SEQUENCE issue663_seq"); err != nil {
		t.Fatalf("CREATE TEMP SEQUENCE in BeginTx failed: %v", err)
	}
	var next int64
	if err := mt.Tx.QueryRow(ctx, "SELECT nextval('issue663_seq')").Scan(&next); err != nil {
		t.Fatalf("nextval in BeginTx failed: %v", err)
	}
	if next != 1 {
		t.Errorf("nextval = %d, want 1", next)
	}
}

// TestBeginReadOnlyTxStillRejectsWrites guards the other side of the
// change: the read-only constructor must keep refusing writes on the
// same pool.
func TestBeginReadOnlyTxStillRejectsWrites(t *testing.T) {
	monitored, _, cleanup := newMonitoredStylePool(t)
	defer cleanup()

	ctx := context.Background()
	rot, errResp, txCleanup := BeginReadOnlyTx(ctx, monitored)
	if errResp != nil {
		t.Fatalf("BeginReadOnlyTx returned error response: %v", errResp)
	}
	defer txCleanup()

	_, err := rot.Tx.Exec(ctx, "CREATE TEMP SEQUENCE issue663_ro_seq")
	if err == nil {
		t.Fatal("expected a write in a read-only transaction to fail")
	}
	if !strings.Contains(err.Error(), "read-only transaction") {
		t.Errorf("unexpected error for write in read-only transaction: %v", err)
	}
}

// TestExecuteQueryWritePathCommitsOnMonitoredPool drives the exact call
// query_database makes for a write (executeQuery with readOnly=false)
// against a monitored-style pool, and checks that INSERT, UPDATE and
// DELETE all run and commit, and that nextval works on the write path.
func TestExecuteQueryWritePathCommitsOnMonitoredPool(t *testing.T) {
	monitored, admin, cleanup := newMonitoredStylePool(t)
	defer cleanup()

	ctx := context.Background()
	const setup = `DROP TABLE IF EXISTS issue663_write_test;
CREATE TABLE issue663_write_test (id serial PRIMARY KEY, val text NOT NULL)`
	if _, err := admin.Exec(ctx, setup); err != nil {
		t.Fatalf("failed to create test table: %v", err)
	}
	defer func() {
		if _, err := admin.Exec(context.Background(),
			"DROP TABLE IF EXISTS issue663_write_test"); err != nil {
			t.Logf("failed to drop test table: %v", err)
		}
	}()

	statements := []string{
		"INSERT INTO issue663_write_test (val) VALUES ('a'), ('b') RETURNING id",
		"UPDATE issue663_write_test SET val = 'c' WHERE val = 'a'",
		"DELETE FROM issue663_write_test WHERE val = 'b'",
		"SELECT nextval('issue663_write_test_id_seq')",
	}
	for _, stmt := range statements {
		if _, errResp := executeQuery(ctx, monitored, stmt, 100, false, "", false); errResp != nil {
			t.Fatalf("executeQuery(%q) returned error response: %+v", stmt, errResp)
		}
	}

	var got string
	if err := admin.QueryRow(ctx,
		"SELECT string_agg(val, ',' ORDER BY id) FROM issue663_write_test").Scan(&got); err != nil {
		t.Fatalf("failed to read back test table: %v", err)
	}
	if got != "c" {
		t.Errorf("table contents after writes = %q, want \"c\"", got)
	}
}

// queryDatabaseWriteEnv drives the query_database handler end to end:
// a resolver on a real auth store and a connection row pointing back at
// the test database, whose pools carry the read-only session default.
type queryDatabaseWriteEnv struct {
	tool      Tool
	admin     *pgxpool.Pool
	authStore *auth.AuthStore
	connID    int
	connStr   string
}

func newQueryDatabaseWriteEnv(t *testing.T) *queryDatabaseWriteEnv {
	t.Helper()

	env := newResolverTestEnv(t)
	authStore, closeAuthStore := newRBACTestStore(t)
	t.Cleanup(closeAuthStore)

	ctx := context.Background()
	const setup = `DROP TABLE IF EXISTS public.issue663_handler_test;
CREATE TABLE public.issue663_handler_test (id serial PRIMARY KEY, val text NOT NULL);
INSERT INTO public.issue663_handler_test (val) VALUES ('seed')`
	if _, err := env.pool.Exec(ctx, setup); err != nil {
		t.Fatalf("failed to create handler test table: %v", err)
	}
	t.Cleanup(func() {
		if _, err := env.pool.Exec(context.Background(),
			"DROP TABLE IF EXISTS public.issue663_handler_test"); err != nil {
			t.Logf("failed to drop handler test table: %v", err)
		}
	})

	resolver := NewConnectionResolver(env.clients,
		database.NewTestDatastoreWithSecret(env.pool, resolverTestSecret),
		auth.NewRBACChecker(authStore))

	return &queryDatabaseWriteEnv{
		tool:      QueryDatabaseTool(nil, resolver),
		admin:     env.pool,
		authStore: authStore,
		connID:    env.connID,
		connStr:   os.Getenv("TEST_AI_WORKBENCH_SERVER"),
	}
}

// call runs the handler with the given context and arguments.
func (e *queryDatabaseWriteEnv) call(t *testing.T, ctx context.Context, args map[string]any) (bool, string) {
	t.Helper()
	args["__context"] = ctx
	resp, err := e.tool.Handler(args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	return resp.IsError, toolResponseText(&resp)
}

// rows returns the table contents in id order.
func (e *queryDatabaseWriteEnv) rows(t *testing.T) string {
	t.Helper()
	var got string
	if err := e.admin.QueryRow(context.Background(),
		"SELECT coalesce(string_agg(val, ',' ORDER BY id), '') FROM public.issue663_handler_test").Scan(&got); err != nil {
		t.Fatalf("failed to read handler test table: %v", err)
	}
	return got
}

// readOnlyUserContext creates a non-superuser holding only read access
// to the connection, which also marks the connection as restricted.
func (e *queryDatabaseWriteEnv) readOnlyUserContext(t *testing.T) context.Context {
	t.Helper()
	if err := e.authStore.CreateUser("issue663reader", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, err := e.authStore.GetUserID("issue663reader")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}
	groupID, err := e.authStore.CreateGroup("issue663-readers", "")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := e.authStore.AddUserToGroup(groupID, userID); err != nil {
		t.Fatalf("AddUserToGroup: %v", err)
	}
	if err := e.authStore.GrantConnectionPrivilege(groupID, e.connID, auth.AccessLevelRead); err != nil {
		t.Fatalf("GrantConnectionPrivilege: %v", err)
	}
	return nonSuperuserContextInt(userID, "issue663reader")
}

// TestQueryDatabaseWriteGateIntegration exercises every branch of the
// query_database write gate now that writes which pass it really run
// (issue #663).
func TestQueryDatabaseWriteGateIntegration(t *testing.T) {
	env := newQueryDatabaseWriteEnv(t)
	reader := env.readOnlyUserContext(t)

	t.Run("read_write user can write", func(t *testing.T) {
		isErr, text := env.call(t, superuserContext(), map[string]any{
			"connection_id": float64(env.connID),
			"query":         "INSERT INTO public.issue663_handler_test (val) VALUES ('written')",
		})
		if isErr {
			t.Fatalf("write by a read_write user failed: %s", text)
		}
		if got := env.rows(t); got != "seed,written" {
			t.Errorf("table contents = %q, want \"seed,written\"", got)
		}
	})

	t.Run("read user can read", func(t *testing.T) {
		isErr, text := env.call(t, reader, map[string]any{
			"connection_id": float64(env.connID),
			"query":         "SELECT val FROM public.issue663_handler_test ORDER BY id",
		})
		if isErr {
			t.Fatalf("read by a read user failed: %s", text)
		}
		if !strings.Contains(text, "seed") {
			t.Errorf("read result missing the seed row: %s", text)
		}
	})

	t.Run("read user cannot write", func(t *testing.T) {
		isErr, text := env.call(t, reader, map[string]any{
			"connection_id": float64(env.connID),
			"query":         "DELETE FROM public.issue663_handler_test",
		})
		if !isErr || !strings.Contains(text, "require read_write access") {
			t.Errorf("expected the read_write refusal, got error=%v %q", isErr, text)
		}
	})

	t.Run("user without a grant cannot write", func(t *testing.T) {
		if err := env.authStore.CreateUser("issue663nobody", "Password1234", "", "", ""); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		userID, err := env.authStore.GetUserID("issue663nobody")
		if err != nil {
			t.Fatalf("GetUserID: %v", err)
		}
		isErr, text := env.call(t, nonSuperuserContextInt(userID, "issue663nobody"), map[string]any{
			"connection_id": float64(env.connID),
			"query":         "DELETE FROM public.issue663_handler_test",
		})
		if !isErr || !strings.Contains(text, "not found or not accessible") {
			t.Errorf("expected an access refusal, got error=%v %q", isErr, text)
		}
	})

	// Statements a read user might use to smuggle a write past the gate
	// must fail whichever way they are classified: as a read they run in
	// a read-only transaction, as a write they meet the gate.
	smuggled := map[string]string{
		"data-modifying CTE":  "WITH d AS (DELETE FROM public.issue663_handler_test RETURNING *) SELECT * FROM d",
		"EXPLAIN ANALYZE":     "EXPLAIN ANALYZE DELETE FROM public.issue663_handler_test",
		"multiple statements": "SELECT 1; DELETE FROM public.issue663_handler_test",
	}
	for name, query := range smuggled {
		t.Run("read user "+name, func(t *testing.T) {
			isErr, text := env.call(t, reader, map[string]any{
				"connection_id": float64(env.connID),
				"query":         query,
			})
			if !isErr {
				t.Errorf("expected %s to fail for a read user, got %q", name, text)
			}
		})
	}
	if got := env.rows(t); got != "seed,written" {
		t.Errorf("table contents after refused writes = %q, want \"seed,written\"", got)
	}
}

// TestQueryDatabaseWriteNeedsConnectionIDIntegration covers the legacy
// path with no connection_id, which must refuse writes outright.
func TestQueryDatabaseWriteNeedsConnectionIDIntegration(t *testing.T) {
	env := newQueryDatabaseWriteEnv(t)

	client := database.NewClient(nil)
	t.Cleanup(client.Close)
	if err := client.SetDefaultConnection(env.connStr); err != nil {
		t.Fatalf("SetDefaultConnection: %v", err)
	}
	tool := QueryDatabaseTool(client, nil)

	resp, err := tool.Handler(map[string]any{
		"__context": superuserContext(),
		"query":     "DELETE FROM public.issue663_handler_test",
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if text := toolResponseText(&resp); !resp.IsError ||
		!strings.Contains(text, "explicit connection_id") {
		t.Errorf("expected the connection_id refusal, got error=%v %q", resp.IsError, text)
	}
	if got := env.rows(t); got != "seed" {
		t.Errorf("table contents = %q, want \"seed\"", got)
	}
}
