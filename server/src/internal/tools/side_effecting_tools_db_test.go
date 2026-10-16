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

	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
)

// callTool runs a tool handler with ctx injected as the registry would,
// failing the test on a Go error.
func callTool(t *testing.T, tool Tool, ctx context.Context, args map[string]any) mcp.ToolResponse {
	t.Helper()
	args["__context"] = ctx
	resp, err := tool.Handler(args)
	if err != nil {
		t.Fatalf("Handler returned a Go error: %v", err)
	}
	return resp
}

// TestCountRows_Database runs count_rows against the resolver fixture
// table, covering the paths around the WHERE clause check.
func TestCountRows_Database(t *testing.T) {
	env := newResolverTestEnv(t)
	ctx := superuserContext()
	tool := CountRowsTool(nil, env.resolver)

	base := func() map[string]any {
		return map[string]any{
			"connection_id": float64(env.connID),
			"schema":        resolverTestSchemaName,
			"table":         "connections",
		}
	}

	t.Run("whole table", func(t *testing.T) {
		resp := callTool(t, tool, ctx, base())
		if resp.IsError || !strings.Contains(responseText(resp), "Count: 1") {
			t.Fatalf("got %+v, want a count of 1", resp)
		}
	})

	t.Run("with a WHERE clause", func(t *testing.T) {
		args := base()
		args["where"] = "name = 'nothing-matches'"
		resp := callTool(t, tool, ctx, args)
		if resp.IsError || !strings.Contains(responseText(resp), "Count: 0") {
			t.Fatalf("got %+v, want a count of 0", resp)
		}
	})

	t.Run("side-effecting WHERE clause", func(t *testing.T) {
		args := base()
		args["where"] = `"pg_reload_conf"() IS NOT NULL`
		resp := callTool(t, tool, ctx, args)
		if !resp.IsError || !strings.Contains(responseText(resp), "side effects") {
			t.Fatalf("got %+v, want the side-effects refusal", resp)
		}
	})

	t.Run("invalid WHERE clause", func(t *testing.T) {
		args := base()
		args["where"] = "no_such_column = 1"
		resp := callTool(t, tool, ctx, args)
		if !resp.IsError || !strings.Contains(responseText(resp), "no_such_column") {
			t.Fatalf("got %+v, want the database error", resp)
		}
	})

	t.Run("unknown connection", func(t *testing.T) {
		args := base()
		args["connection_id"] = float64(env.connID + 1000)
		resp := callTool(t, tool, ctx, args)
		if !resp.IsError {
			t.Fatalf("got %+v, want an error", resp)
		}
	})
}

// TestExecuteExplain_Database runs execute_explain against the test
// database, covering its option handling and CTE checks.
func TestExecuteExplain_Database(t *testing.T) {
	env := newResolverTestEnv(t)
	ctx := superuserContext()
	tool := ExecuteExplainTool(nil, env.resolver)

	t.Run("analyze in text with analysis", func(t *testing.T) {
		resp := callTool(t, tool, ctx, map[string]any{
			"connection_id": float64(env.connID),
			"query": "SELECT * FROM generate_series(1, 2000) a " +
				"JOIN generate_series(1, 2000) b ON a = b",
		})
		if resp.IsError || !strings.Contains(responseText(resp), "Execution Plan:") {
			t.Fatalf("got %+v, want a plan", resp)
		}
	})

	t.Run("json without buffers", func(t *testing.T) {
		resp := callTool(t, tool, ctx, map[string]any{
			"connection_id": float64(env.connID),
			"query":         "WITH t AS (SELECT 1 AS updated_at) SELECT * FROM t",
			"buffers":       false,
			"format":        "json",
		})
		if resp.IsError || !strings.Contains(responseText(resp), "\"Plan\"") {
			t.Fatalf("got %+v, want a JSON plan", resp)
		}
	})

	t.Run("CTE with DML refused", func(t *testing.T) {
		resp := callTool(t, tool, ctx, map[string]any{
			"connection_id": float64(env.connID),
			"query":         "WITH d AS (DELETE FROM t RETURNING *) SELECT * FROM d",
		})
		if !resp.IsError || !strings.Contains(responseText(resp), "DELETE") {
			t.Fatalf("got %+v, want the CTE refusal", resp)
		}
	})

	t.Run("invalid query", func(t *testing.T) {
		resp := callTool(t, tool, ctx, map[string]any{
			"connection_id": float64(env.connID),
			"query":         "SELECT * FROM no_such_table_issue646",
		})
		if !resp.IsError || !strings.Contains(responseText(resp), "Error executing EXPLAIN") {
			t.Fatalf("got %+v, want the EXPLAIN error", resp)
		}
	})

	t.Run("unknown connection", func(t *testing.T) {
		resp := callTool(t, tool, ctx, map[string]any{
			"connection_id": float64(env.connID + 1000),
			"query":         "SELECT 1",
		})
		if !resp.IsError {
			t.Fatalf("got %+v, want an error", resp)
		}
	})
}

// TestQueryDatabase_SelectedConnectionPath covers query_database
// without a connection_id, where it runs on the caller's selected
// connection. A connection string in the query text is passed to the
// server as SQL rather than connected to (issue #662), and the
// side-effect check sees the whole query text.
func TestQueryDatabase_SelectedConnectionPath(t *testing.T) {
	env := newResolverTestEnv(t)
	ctx := superuserContext()
	dsn := os.Getenv("TEST_AI_WORKBENCH_SERVER")

	resolved, errResp := env.resolver.Resolve(ctx, deniedActionConnectionQuery, map[string]any{
		"connection_id": float64(env.connID),
	}, nil)
	if errResp != nil {
		t.Fatalf("Resolve: %s", toolResponseText(errResp))
	}
	client := resolved.Client
	tool := QueryDatabaseTool(client, env.resolver)
	selected := client.GetDefaultConnection()
	connections := len(client.ListConnections())

	// assertUnchanged checks that a call neither opened a connection nor
	// changed the selected one.
	assertUnchanged := func(t *testing.T) {
		t.Helper()
		if got := len(client.ListConnections()); got != connections {
			t.Errorf("connections = %d, want %d", got, connections)
		}
		if got := client.GetDefaultConnection(); got != selected {
			t.Errorf("selected connection changed to %q",
				database.SanitizeConnStr(got))
		}
	}

	t.Run("non-string query", func(t *testing.T) {
		resp := callTool(t, tool, ctx, map[string]any{"query": 1})
		if !resp.IsError {
			t.Fatalf("got %+v, want an error", resp)
		}
	})

	t.Run("empty query", func(t *testing.T) {
		resp := callTool(t, tool, ctx, map[string]any{"query": ""})
		if !resp.IsError ||
			!strings.Contains(responseText(resp), "Missing or invalid 'query'") {
			t.Fatalf("got %+v, want the missing-query error", resp)
		}
	})

	t.Run("query at a connection string", func(t *testing.T) {
		resp := callTool(t, tool, ctx, map[string]any{
			"query": "SELECT 'issue646' AS marker at " + dsn,
		})
		if !resp.IsError || !strings.Contains(responseText(resp), "syntax error") {
			t.Fatalf("got %+v, want the server's syntax error", resp)
		}
		assertUnchanged(t)
	})

	t.Run("side-effecting query at a connection string", func(t *testing.T) {
		resp := callTool(t, tool, ctx, map[string]any{
			"query": `SELECT "pg_terminate_backend"(0) at ` + dsn,
		})
		if !resp.IsError || responseText(resp) != queryDatabaseReadOnlyError {
			t.Fatalf("got %+v, want the read-only refusal", resp)
		}
		assertUnchanged(t)
	})

	t.Run("set the default connection", func(t *testing.T) {
		resp := callTool(t, tool, ctx, map[string]any{
			"query": "set default database to " + dsn,
		})
		if !resp.IsError || responseText(resp) != queryDatabaseReadOnlyError {
			t.Fatalf("got %+v, want the read-only refusal", resp)
		}
		assertUnchanged(t)
	})

	// Nothing listens on loopback port 1, so a connection attempt would
	// fail at once; none is made.
	unreachable := "postgresql://nobody@127.0.0.1:1/unused"

	t.Run("query at an unreachable connection string", func(t *testing.T) {
		resp := callTool(t, tool, ctx, map[string]any{
			"query": "SELECT 1 at " + unreachable,
		})
		text := responseText(resp)
		if !resp.IsError || strings.Contains(text, "Failed to connect") ||
			!strings.Contains(text, "syntax error") {
			t.Fatalf("got %+v, want the server's syntax error", resp)
		}
		assertUnchanged(t)
	})

	t.Run("set an unreachable default connection", func(t *testing.T) {
		resp := callTool(t, tool, ctx, map[string]any{
			"query": "set default database to " + unreachable,
		})
		if !resp.IsError || responseText(resp) != queryDatabaseReadOnlyError {
			t.Fatalf("got %+v, want the read-only refusal", resp)
		}
		assertUnchanged(t)
	})

	t.Run("no context in the arguments", func(t *testing.T) {
		resp, err := tool.Handler(map[string]any{"query": "SELECT 1 AS one"})
		if err != nil || resp.IsError {
			t.Fatalf("got %+v, %v, want the query result", resp, err)
		}
	})

	t.Run("invalid query", func(t *testing.T) {
		resp := callTool(t, tool, ctx, map[string]any{
			"connection_id": float64(env.connID),
			"query":         "SELECT * FROM no_such_table_issue646",
		})
		if !resp.IsError {
			t.Fatalf("got %+v, want the database error", resp)
		}
	})
}
