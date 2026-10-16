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

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
)

// queryDatabaseText runs the query_database handler and returns the
// first content item's text along with the error flag.
func queryDatabaseText(t *testing.T, client *database.Client, args map[string]any) (string, bool) {
	t.Helper()
	resp, err := QueryDatabaseTool(client, nil).Handler(args)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if len(resp.Content) == 0 {
		t.Fatal("handler returned no content")
	}
	return resp.Content[0].Text, resp.IsError
}

// TestQueryDatabaseIgnoresConnectionStringInQuery verifies that a
// connection string written in the query text never selects the target
// connection, so the tool cannot be pointed at an arbitrary host
// outside connection RBAC (issue #662). 192.0.2.1 is a TEST-NET-1
// address; any attempt to reach it would show up as a pool for it.
func TestQueryDatabaseIgnoresConnectionStringInQuery(t *testing.T) {
	const target = "postgres://probe@192.0.2.1:5432/other?connect_timeout=1"

	queries := []string{
		"SELECT 1 at " + target,
		"SELECT 1 FROM " + target,
		"database " + target + " SELECT 1",
		"set default database to " + target,
		"use database " + target,
		"switch to " + target,
	}

	for _, q := range queries {
		t.Run(q, func(t *testing.T) {
			client := database.NewClient(nil)

			text, isErr := queryDatabaseText(t, client, map[string]any{"query": q})
			if !isErr {
				t.Fatalf("expected an error without a selected connection, got %q", text)
			}
			if text != mcp.DatabaseNotReadyError {
				t.Errorf("expected the not-ready error, got %q", text)
			}
			if client.GetPoolFor(target) != nil {
				t.Error("the tool opened a pool for the connection string in the query")
			}
			if got := client.GetDefaultConnection(); got != "" {
				t.Errorf("the default connection changed to %q", got)
			}
		})
	}
}

// TestQueryDatabaseNilClientWithoutConnectionID verifies that a call
// with no connection_id and no selected client is refused plainly.
func TestQueryDatabaseNilClientWithoutConnectionID(t *testing.T) {
	text, isErr := queryDatabaseText(t, nil, map[string]any{"query": "SELECT 1"})
	if !isErr {
		t.Fatalf("expected an error, got %q", text)
	}
	if !strings.Contains(text, "No database connection available") {
		t.Errorf("unexpected error text %q", text)
	}
}

// TestQueryDatabaseConnectionStringIsPlainSQLIntegration verifies that,
// on the selected connection, text that looks like a connection command
// is executed as ordinary SQL and leaves the default connection alone.
func TestQueryDatabaseConnectionStringIsPlainSQLIntegration(t *testing.T) {
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping integration test")
	}

	client := database.NewClientWithConnectionString(connStr, nil)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(client.Close)
	if err := client.LoadMetadataFor(connStr); err != nil {
		t.Fatalf("LoadMetadataFor: %v", err)
	}

	const target = "postgres://probe@192.0.2.1:5432/other"
	text, isErr := queryDatabaseText(t, client, map[string]any{
		"query": "SELECT 'switch to " + target + "' AS u",
	})
	if isErr {
		t.Fatalf("query failed: %q", text)
	}
	if !strings.Contains(text, "switch to "+target) {
		t.Errorf("expected the literal back as a row, got %q", text)
	}
	if !strings.HasPrefix(text, "Database: ") {
		t.Errorf("expected the database context header, got %q", text)
	}
	if got := client.GetDefaultConnection(); got != connStr {
		t.Errorf("default connection changed to %q", got)
	}
	if client.GetPoolFor(target) != nil {
		t.Error("the tool opened a pool for the connection string in the query")
	}
}

// TestQueryDatabaseMissingQuery verifies the query argument is required.
func TestQueryDatabaseMissingQuery(t *testing.T) {
	text, isErr := queryDatabaseText(t, database.NewClient(nil), map[string]any{})
	if !isErr || !strings.Contains(text, "'query'") {
		t.Errorf("expected a missing-query error, got %q (error=%v)", text, isErr)
	}
}

// TestQueryDatabaseSelectedConnectionIntegration covers the selected
// connection path: writes are refused without a connection_id, and a
// failing statement is reported as a tool error.
func TestQueryDatabaseSelectedConnectionIntegration(t *testing.T) {
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping integration test")
	}

	client := database.NewClientWithConnectionString(connStr, nil)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(client.Close)
	if err := client.LoadMetadataFor(connStr); err != nil {
		t.Fatalf("LoadMetadataFor: %v", err)
	}

	text, isErr := queryDatabaseText(t, client, map[string]any{
		"query": "CREATE TABLE ai_wb_issue662_never (id int)",
	})
	if !isErr || !strings.Contains(text, "explicit connection_id") {
		t.Errorf("expected the write to be refused, got %q (error=%v)", text, isErr)
	}

	text, isErr = queryDatabaseText(t, client, map[string]any{
		"query": "SELECT * FROM ai_wb_issue662_no_such_table",
	})
	if !isErr {
		t.Errorf("expected a query error, got %q", text)
	}
}

// TestQueryDatabaseExplicitConnectionIntegration covers the
// connection_id path, which resolves the target through the RBAC-checked
// resolver rather than anything in the query text.
func TestQueryDatabaseExplicitConnectionIntegration(t *testing.T) {
	env := newResolverTestEnv(t)
	ctx := context.WithValue(context.Background(),
		auth.IsSuperuserContextKey, true)
	tool := QueryDatabaseTool(nil, env.resolver)

	run := func(connID int, query string) (string, bool) {
		t.Helper()
		resp, err := tool.Handler(map[string]any{
			"__context":     ctx,
			"connection_id": float64(connID),
			"query":         query,
		})
		if err != nil {
			t.Fatalf("handler returned error: %v", err)
		}
		if len(resp.Content) == 0 {
			t.Fatal("handler returned no content")
		}
		return resp.Content[0].Text, resp.IsError
	}

	text, isErr := run(env.connID, "SELECT current_database() AS db")
	if isErr {
		t.Fatalf("query failed: %q", text)
	}
	if !strings.Contains(text, env.database) {
		t.Errorf("expected the connection's database in %q", text)
	}

	// A superuser holds read_write, so the write passes the access
	// check; whatever the database then says is outside this test. An
	// empty DO block counts as a write but leaves nothing behind.
	text, _ = run(env.connID, "DO $$ BEGIN END $$")
	if strings.Contains(text, "Write operations require") ||
		strings.Contains(text, "Access denied") {
		t.Errorf("expected the write to pass the access check, got %q", text)
	}

	// An unknown connection is refused by the resolver.
	if text, isErr = run(env.connID+1000, "SELECT 1"); !isErr {
		t.Errorf("expected a refusal for an unknown connection, got %q", text)
	}
}
