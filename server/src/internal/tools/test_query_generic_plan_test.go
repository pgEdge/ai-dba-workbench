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

// TestContainsParamPlaceholder covers the $N detector that decides
// whether a query needs EXPLAIN (GENERIC_PLAN).
func TestContainsParamPlaceholder(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want bool
	}{
		{name: "no placeholder", sql: "SELECT 1", want: false},
		{name: "first placeholder", sql: "SELECT * FROM t WHERE a = $1", want: true},
		{name: "later placeholder", sql: "SELECT * FROM t WHERE a = $2", want: true},
		{name: "dollar quote is not a placeholder", sql: "SELECT $$a$$", want: false},
		{name: "trailing dollar", sql: "SELECT 1 $", want: false},
		{name: "empty", sql: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsParamPlaceholder(tt.sql); got != tt.want {
				t.Errorf("containsParamPlaceholder(%q) = %v, want %v",
					tt.sql, got, tt.want)
			}
		})
	}
}

// newTestQueryToolClient wires a database.Client to the
// TEST_AI_WORKBENCH_SERVER instance and returns the test_query tool
// bound to it, so the tool's own handler runs end to end.
func newTestQueryToolClient(t *testing.T) (Tool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping test_query integration test")
	}

	client := database.NewClientWithConnectionString(connStr, nil)
	if err := client.Connect(); err != nil {
		t.Skipf("Could not connect to the test database: %v", err)
	}

	return TestQueryTool(client, nil), client.Close
}

// runTestQuery invokes the tool handler with the given SQL.
func runTestQuery(t *testing.T, tool Tool, query string) mcp.ToolResponse {
	t.Helper()

	resp, err := tool.Handler(map[string]any{
		"query":     query,
		"__context": context.Background(),
	})
	if err != nil {
		t.Fatalf("test_query handler returned an error: %v", err)
	}
	return resp
}

// responseText flattens a tool response's content for assertions.
func responseText(resp mcp.ToolResponse) string {
	var sb strings.Builder
	for _, item := range resp.Content {
		sb.WriteString(item.Text)
	}
	return sb.String()
}

// serverSupportsGenericPlan reports whether the test instance is
// PostgreSQL 16 or later.
func serverSupportsGenericPlan(t *testing.T) bool {
	t.Helper()

	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping test_query integration test")
	}
	pool, ds, cleanup := newToolsTestPool(t)
	_ = ds
	defer cleanup()

	var versionNum int
	if err := pool.QueryRow(context.Background(),
		"SELECT current_setting('server_version_num')::int").
		Scan(&versionNum); err != nil {
		t.Fatalf("failed to read the server version: %v", err)
	}
	return versionNum >= genericPlanMinVersionNum
}

// TestQueryTool_ParameterisedQueryIsValidated is the regression guard
// for issue #532: a query carrying $1 used to be rejected with "could
// not determine data type of parameter $1", which sent the model
// rewriting perfectly good SQL.
func TestQueryTool_ParameterisedQueryIsValidated(t *testing.T) {
	if !serverSupportsGenericPlan(t) {
		t.Skip("server predates EXPLAIN (GENERIC_PLAN)")
	}

	tool, cleanup := newTestQueryToolClient(t)
	defer cleanup()

	resp := runTestQuery(t,
		tool, "SELECT relname FROM pg_class WHERE relname = $1")

	if resp.IsError {
		t.Fatalf("parameterised query was rejected: %s", responseText(resp))
	}
	if !strings.Contains(responseText(resp), "valid") {
		t.Errorf("response = %q, want it to report the query as valid",
			responseText(resp))
	}
}

// TestQueryTool_ParameterisedMultiStatement covers the split inside
// the GENERIC_PLAN path, which plans one statement at a time.
func TestQueryTool_ParameterisedMultiStatement(t *testing.T) {
	if !serverSupportsGenericPlan(t) {
		t.Skip("server predates EXPLAIN (GENERIC_PLAN)")
	}

	tool, cleanup := newTestQueryToolClient(t)
	defer cleanup()

	resp := runTestQuery(t, tool,
		"SELECT relname FROM pg_class WHERE relname = $1; SELECT 1;")

	if resp.IsError {
		t.Fatalf("multi-statement parameterised query was rejected: %s",
			responseText(resp))
	}
}

// TestQueryTool_ParameterisedSmuggledCommandsAreNeverRun sends a
// parameterised query the splitter cannot divide, carrying a COMMIT
// and a DROP TABLE. Each statement is planned over the extended query
// protocol, which refuses more than one command, so the table must
// survive and the query must be rejected.
func TestQueryTool_ParameterisedSmuggledCommandsAreNeverRun(t *testing.T) {
	if !serverSupportsGenericPlan(t) {
		t.Skip("server predates EXPLAIN (GENERIC_PLAN)")
	}

	pool, _, poolCleanup := newToolsTestPool(t)
	defer poolCleanup()

	ctx := context.Background()
	const victim = "test_query_smuggle_victim"
	if _, err := pool.Exec(ctx,
		"CREATE TABLE IF NOT EXISTS "+victim+" (a int)"); err != nil {
		t.Fatalf("failed to create the victim table: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), "DROP TABLE IF EXISTS "+victim)
	}()

	tool, cleanup := newTestQueryToolClient(t)
	defer cleanup()

	resp := runTestQuery(t, tool,
		`SELECT E'\'', $1; COMMIT; DROP TABLE `+victim)

	if !resp.IsError {
		t.Errorf("a smuggled COMMIT was accepted: %s", responseText(resp))
	}

	var exists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL",
		victim).Scan(&exists); err != nil {
		t.Fatalf("failed to look up the victim table: %v", err)
	}
	if !exists {
		t.Fatal("the victim table was dropped: test_query ran a smuggled command")
	}
}

// TestQueryTool_ParameterisedQueryStillReportsErrors confirms the
// GENERIC_PLAN path rejects a genuinely wrong parameterised query, and
// names the offending statement.
func TestQueryTool_ParameterisedQueryStillReportsErrors(t *testing.T) {
	if !serverSupportsGenericPlan(t) {
		t.Skip("server predates EXPLAIN (GENERIC_PLAN)")
	}

	tool, cleanup := newTestQueryToolClient(t)
	defer cleanup()

	resp := runTestQuery(t, tool,
		"SELECT 1; SELECT * FROM no_such_table_for_issue_532 WHERE a = $1")

	if !resp.IsError {
		t.Fatalf("a missing relation was accepted: %s", responseText(resp))
	}
	text := responseText(resp)
	if !strings.Contains(text, "Statement 2") ||
		!strings.Contains(text, "no_such_table_for_issue_532") {
		t.Errorf("response = %q, want it to name the failing statement", text)
	}
}

// TestValidateGenericPlan_NoStatements covers the guard for SQL that
// splits to nothing; the guard runs before the transaction is touched,
// so no database is needed.
func TestValidateGenericPlan_NoStatements(t *testing.T) {
	resp, err := validateGenericPlan(context.Background(), nil, " ; ")
	if err != nil {
		t.Fatalf("validateGenericPlan returned an error: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("an empty query was accepted: %s", responseText(resp))
	}
	if !strings.Contains(responseText(resp), "no valid SQL statements") {
		t.Errorf("response = %q, want the empty-query message",
			responseText(resp))
	}
}

// TestQueryTool_UnparameterisedQueriesStillWork confirms the existing
// EXPLAIN path, including the multiple-statement fallback, is intact.
func TestQueryTool_UnparameterisedQueriesStillWork(t *testing.T) {
	tool, cleanup := newTestQueryToolClient(t)
	defer cleanup()

	t.Run("single statement", func(t *testing.T) {
		resp := runTestQuery(t, tool, "SELECT 1")
		if resp.IsError {
			t.Errorf("valid query was rejected: %s", responseText(resp))
		}
	})

	t.Run("multiple statements", func(t *testing.T) {
		resp := runTestQuery(t, tool, "SELECT 1; SELECT 2")
		if resp.IsError {
			t.Errorf("valid multi-statement query was rejected: %s",
				responseText(resp))
		}
	})

	t.Run("invalid statement", func(t *testing.T) {
		resp := runTestQuery(t, tool,
			"SELECT * FROM no_such_table_for_issue_532")
		if !resp.IsError {
			t.Errorf("a missing relation was accepted: %s", responseText(resp))
		}
	})
}
