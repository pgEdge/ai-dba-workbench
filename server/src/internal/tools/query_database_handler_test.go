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
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
)

// runQueryDatabase invokes the query_database handler with args and
// returns the text of the first content item.
func runQueryDatabase(t *testing.T, client *database.Client, args map[string]any) (mcp.ToolResponse, string) {
	t.Helper()

	tool := QueryDatabaseTool(client, nil)
	resp, err := tool.Handler(args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	text := ""
	if len(resp.Content) > 0 {
		text = resp.Content[0].Text
	}
	return resp, text
}

// TestQueryDatabase_RejectsMissingQuery checks that a missing, non-string
// or blank query is refused before anything else happens.
func TestQueryDatabase_RejectsMissingQuery(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
	}{
		{name: "missing", args: map[string]any{}},
		{name: "not a string", args: map[string]any{"query": 42}},
		{name: "blank", args: map[string]any{"query": "  \n\t "}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, text := runQueryDatabase(t, nil, tt.args)
			if !resp.IsError {
				t.Fatal("expected an error response")
			}
			if !strings.Contains(text, "Missing or invalid 'query' parameter") {
				t.Errorf("unexpected message: %q", text)
			}
		})
	}
}

// TestQueryDatabase_RefusesWrites checks that query_database refuses any
// statement that is not a read (issue #663), and does so before it
// resolves a connection: with a connection_id and no resolver, reaching
// resolution would answer "connection resolution is not available"
// instead. A connection command written into the query (issue #662) is
// not a read either, so it is refused the same way.
func TestQueryDatabase_RefusesWrites(t *testing.T) {
	queries := []string{
		"INSERT INTO t VALUES (1)",
		"UPDATE t SET a = 1",
		"DELETE FROM t",
		"MERGE INTO t USING s ON true WHEN MATCHED THEN DELETE",
		"CREATE TABLE t (a int)",
		"DROP TABLE t",
		"TRUNCATE t",
		"ALTER SYSTEM SET work_mem = '1GB'",
		"SET search_path = evil",
		"COPY t FROM PROGRAM 'id'",
		"/* comment */ -- line\n delete from t",
		"set default database to postgres://192.0.2.1:5432/probe",
		"use database postgres://192.0.2.1/probe",
		"database postgres://192.0.2.1/probe SELECT 1",
	}

	for _, q := range queries {
		for _, withConnID := range []bool{false, true} {
			args := map[string]any{"query": q}
			if withConnID {
				args["connection_id"] = float64(7)
			}
			resp, text := runQueryDatabase(t, nil, args)
			if !resp.IsError {
				t.Errorf("%q (connection_id=%v): expected a refusal", q, withConnID)
				continue
			}
			if text != queryDatabaseReadOnlyError {
				t.Errorf("%q (connection_id=%v): got %q, want the read-only refusal",
					q, withConnID, text)
			}
		}
	}
}

// TestQueryDatabase_NoConnectionAvailable checks that a read with no
// connection_id and no selected connection reports that plainly.
func TestQueryDatabase_NoConnectionAvailable(t *testing.T) {
	resp, text := runQueryDatabase(t, nil, map[string]any{"query": "SELECT 1"})
	if !resp.IsError {
		t.Fatal("expected an error response")
	}
	if !strings.Contains(text, "No database connection available") {
		t.Errorf("unexpected message: %q", text)
	}
}

// TestQueryDatabase_ConnectionIDNeedsResolver checks that a read naming
// a connection_id goes through the resolver rather than any fallback.
func TestQueryDatabase_ConnectionIDNeedsResolver(t *testing.T) {
	client := database.NewTestClient("postgres://192.0.2.1/test", nil)
	resp, text := runQueryDatabase(t, client, map[string]any{
		"query":         "SELECT 1",
		"connection_id": float64(3),
	})
	if !resp.IsError {
		t.Fatal("expected an error response")
	}
	if !strings.Contains(text, "connection resolution is not available") {
		t.Errorf("unexpected message: %q", text)
	}
}

// TestQueryDatabase_SelectedConnectionWithoutPool checks that the
// fallback path reports a selected connection with no pool rather than
// connecting anywhere itself.
func TestQueryDatabase_SelectedConnectionWithoutPool(t *testing.T) {
	client := database.NewTestClient("postgres://192.0.2.1/test", nil)
	resp, text := runQueryDatabase(t, client, map[string]any{"query": "SELECT 1"})
	if !resp.IsError {
		t.Fatal("expected an error response")
	}
	if !strings.Contains(text, "Connection pool not found") {
		t.Errorf("unexpected message: %q", text)
	}
	if got := client.ListConnections(); len(got) != 1 {
		t.Errorf("connections = %v, want only the selected one", got)
	}
}

// newQueryDatabaseTestClient connects a client to the test database as
// the caller's selected connection, skipping when no database is set.
func newQueryDatabaseTestClient(t *testing.T, loadMetadata bool) (*database.Client, string) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping integration test")
	}

	client := database.NewClientWithConnectionString(connStr, nil)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(client.Close)
	if loadMetadata {
		if err := client.LoadMetadata(); err != nil {
			t.Fatalf("LoadMetadata: %v", err)
		}
	}
	return client, connStr
}

// TestQueryDatabase_SelectedConnectionIntegration checks the read path
// on the caller's selected connection: the query runs read-only, a
// connection string in the query text is never connected to (issue
// #662), and the selected connection is left unchanged.
func TestQueryDatabase_SelectedConnectionIntegration(t *testing.T) {
	client, connStr := newQueryDatabaseTestClient(t, true)

	t.Run("runs in a read-only transaction", func(t *testing.T) {
		resp, text := runQueryDatabase(t, client, map[string]any{
			"query":     "SELECT current_setting('transaction_read_only') AS ro",
			"__context": context.Background(),
		})
		if resp.IsError {
			t.Fatalf("unexpected error: %q", text)
		}
		if !strings.HasPrefix(text, "Database: ") {
			t.Errorf("expected the database header, got %q", text)
		}
		if !strings.Contains(text, "ro\non") {
			t.Errorf("expected transaction_read_only = on, got %q", text)
		}
	})

	t.Run("ignores a connection string in the query", func(t *testing.T) {
		resp, text := runQueryDatabase(t, client, map[string]any{
			"query": "SELECT 1 AS one -- from postgres://192.0.2.1:5432/probe",
		})
		if resp.IsError {
			t.Fatalf("unexpected error: %q", text)
		}
		if !strings.Contains(text, "one\n1") {
			t.Errorf("expected the query to run on the selected connection, got %q", text)
		}
		wantHeader := "Database: " + database.SanitizeConnStr(connStr) + "\n"
		if !strings.HasPrefix(text, wantHeader) {
			t.Errorf("expected the selected connection's header, got %q", text)
		}
		if got := client.ListConnections(); len(got) != 1 {
			t.Errorf("connections = %v, want only the selected one", got)
		}
		if got := client.GetDefaultConnection(); got != connStr {
			t.Errorf("selected connection changed to %q", database.SanitizeConnStr(got))
		}
	})
}

// TestQueryDatabase_MetadataNotLoadedIntegration checks that a selected
// connection whose metadata has not loaded yet reports that it is not
// ready rather than running the query.
func TestQueryDatabase_MetadataNotLoadedIntegration(t *testing.T) {
	client, _ := newQueryDatabaseTestClient(t, false)

	resp, text := runQueryDatabase(t, client, map[string]any{"query": "SELECT 1"})
	if !resp.IsError {
		t.Fatal("expected an error response")
	}
	if text != mcp.DatabaseNotReadyError {
		t.Errorf("got %q, want the not-ready error", text)
	}
}

// TestQueryDatabase_RefusesMultipleStatementsIntegration checks that a
// read followed by a second statement is refused by the extended
// protocol rather than run, even when the connection string asks for
// the simple protocol, so neither a write nor a COMMIT can follow the
// read inside the read-only transaction.
func TestQueryDatabase_RefusesMultipleStatementsIntegration(t *testing.T) {
	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	baseConnStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if baseConnStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping integration test")
	}

	ctx := context.Background()
	setup, err := pgxpool.New(ctx, baseConnStr)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer setup.Close()
	for _, stmt := range []string{
		"CREATE TABLE IF NOT EXISTS qd_multi_stmt_probe (a int)",
		"TRUNCATE qd_multi_stmt_probe",
		"INSERT INTO qd_multi_stmt_probe VALUES (1)",
	} {
		if _, err := setup.Exec(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	defer func() {
		if _, err := setup.Exec(context.Background(), "DROP TABLE IF EXISTS qd_multi_stmt_probe"); err != nil {
			t.Logf("drop probe table: %v", err)
		}
	}()

	u, err := url.Parse(baseConnStr)
	if err != nil {
		t.Fatalf("parse connection string: %v", err)
	}
	q := u.Query()
	q.Set("default_query_exec_mode", "simple_protocol")
	u.RawQuery = q.Encode()

	for _, connStr := range []string{baseConnStr, u.String()} {
		client := database.NewClientWithConnectionString(connStr, nil)
		if err := client.Connect(); err != nil {
			t.Fatalf("Connect: %v", err)
		}
		if err := client.LoadMetadata(); err != nil {
			client.Close()
			t.Fatalf("LoadMetadata: %v", err)
		}

		for _, query := range []string{
			// The queries carry their own LIMIT, so that no LIMIT is
			// appended to the final statement.
			"SELECT 1 LIMIT 1; DELETE FROM qd_multi_stmt_probe",
			"SELECT 1 LIMIT 1; COMMIT",
			// Under the simple protocol this would end the read-only
			// transaction and delete the probe row in a new one.
			"SELECT 1 LIMIT 1; COMMIT; BEGIN READ WRITE; " +
				"DELETE FROM qd_multi_stmt_probe; COMMIT",
		} {
			resp, text := runQueryDatabase(t, client, map[string]any{"query": query})
			if !resp.IsError {
				t.Errorf("%q on %q: expected a refusal, got %q",
					query, database.SanitizeConnStr(connStr), text)
			}
		}
		client.Close()

		var n int
		if err := setup.QueryRow(ctx, "SELECT count(*) FROM qd_multi_stmt_probe").Scan(&n); err != nil {
			t.Fatalf("count probe rows: %v", err)
		}
		if n != 1 {
			t.Errorf("probe table has %d rows after queries on %q, want 1",
				n, database.SanitizeConnStr(connStr))
		}
	}
}
