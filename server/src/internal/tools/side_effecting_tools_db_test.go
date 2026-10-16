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

// TestQueryDatabase_LegacyConnectionPath covers query_database without a
// connection_id, where the query text may name a connection string, and
// shows that the side-effect check sees the query left once that is
// removed.
func TestQueryDatabase_LegacyConnectionPath(t *testing.T) {
	env := newResolverTestEnv(t)
	ctx := superuserContext()
	dsn := os.Getenv("TEST_AI_WORKBENCH_SERVER")

	resolved, errResp := env.resolver.Resolve(ctx, map[string]any{
		"connection_id": float64(env.connID),
	}, nil)
	if errResp != nil {
		t.Fatalf("Resolve: %s", toolResponseText(errResp))
	}
	tool := QueryDatabaseTool(resolved.Client, env.resolver)

	t.Run("non-string query", func(t *testing.T) {
		resp := callTool(t, tool, ctx, map[string]any{"query": 1})
		if !resp.IsError {
			t.Fatalf("got %+v, want an error", resp)
		}
	})

	t.Run("empty query", func(t *testing.T) {
		resp := callTool(t, tool, ctx, map[string]any{"query": ""})
		if resp.IsError || !strings.Contains(responseText(resp), "No query to run") {
			t.Fatalf("got %+v, want the no-query answer", resp)
		}
	})

	t.Run("query at a connection string", func(t *testing.T) {
		resp := callTool(t, tool, ctx, map[string]any{
			"query": "SELECT 'issue646' AS marker at " + dsn,
		})
		if resp.IsError || !strings.Contains(responseText(resp), "issue646") {
			t.Fatalf("got %+v, want the query result", resp)
		}
	})

	t.Run("side-effecting query at a connection string", func(t *testing.T) {
		resp := callTool(t, tool, ctx, map[string]any{
			"query": `SELECT "pg_terminate_backend"(0) at ` + dsn,
		})
		if !resp.IsError || responseText(resp) !=
			"Write operations require an explicit connection_id parameter" {
			t.Fatalf("got %+v, want the explicit connection_id refusal", resp)
		}
	})

	t.Run("set the default connection", func(t *testing.T) {
		resp := callTool(t, tool, ctx, map[string]any{
			"query": "set default database to " + dsn,
		})
		if resp.IsError || !strings.Contains(responseText(resp),
			"Successfully set default database connection") {
			t.Fatalf("got %+v, want the default set", resp)
		}
	})

	// Nothing listens on loopback port 1, so both commands fail at once.
	unreachable := "postgresql://nobody@127.0.0.1:1/unused"

	t.Run("query at an unreachable connection string", func(t *testing.T) {
		resp := callTool(t, tool, ctx, map[string]any{
			"query": "SELECT 1 at " + unreachable,
		})
		if !resp.IsError || !strings.Contains(responseText(resp), "Failed to connect") {
			t.Fatalf("got %+v, want the connection failure", resp)
		}
	})

	t.Run("set an unreachable default connection", func(t *testing.T) {
		resp := callTool(t, tool, ctx, map[string]any{
			"query": "set default database to " + unreachable,
		})
		if !resp.IsError || !strings.Contains(responseText(resp), "Failed to set default") {
			t.Fatalf("got %+v, want the failure", resp)
		}
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
