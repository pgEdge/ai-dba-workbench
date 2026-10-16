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
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
)

// sideEffectingSpellings are SELECTs that call a side-effecting function
// in the spellings a caller might use to slip one past a read-only
// check (issue #646). Each call is harmless if it ever ran: PID 0 is
// never a backend, so pg_terminate_backend(0) only warns.
var sideEffectingSpellings = []struct {
	name  string
	query string
}{
	{"plain", "SELECT pg_terminate_backend(0)"},
	{"upper case", "SELECT PG_TERMINATE_BACKEND(0)"},
	{"schema qualified", "SELECT pg_catalog.pg_terminate_backend(0)"},
	{"quoted name", `SELECT "pg_terminate_backend"(0)`},
	{"quoted schema and name", `SELECT "pg_catalog"."pg_terminate_backend"(0)`},
	{"unicode escaped name", `SELECT U&"pg\005fterminate\005fbackend"(0)`},
	{"comment before paren", "SELECT pg_terminate_backend/* x */(0)"},
	{"newline before paren", "SELECT pg_terminate_backend\n(0)"},
	{"leading comment", "-- note\nSELECT pg_terminate_backend(0)"},
	{"in a FROM clause", "SELECT * FROM pg_terminate_backend(0) AS r"},
	{"in a CTE", "WITH t AS (SELECT pg_terminate_backend(0)) SELECT * FROM t"},
	{"values", "VALUES (pg_terminate_backend(0))"},
	{"explain analyze", "EXPLAIN ANALYZE SELECT pg_terminate_backend(0)"},
	{"hidden under the escape reading",
		`SELECT '\'' AS c, pg_terminate_backend(0) --'`},
	{"hidden under the standard reading",
		`SELECT 'a\' AS c, pg_terminate_backend(0) --'`},
	{"uescape fails closed", `SELECT U&"pg!005fterminate!005fbackend" UESCAPE '!'(0)`},
	{"set_config", "SELECT set_config('work_mem', '1MB', false)"},
	{"nextval", "SELECT nextval('s')"},
	{"pg_reload_conf", "SELECT pg_reload_conf()"},
	// Field selection: PostgreSQL reads arg.func as func(arg).
	{"field selection", "SELECT (0).pg_terminate_backend"},
	{"field selection comment after dot", `SELECT (0)./**/"pg_terminate_backend"`},
	{"field selection newline after dot", "SELECT (0).\npg_terminate_backend"},
	{"field selection unicode", `SELECT (0).U&"pg\005fterminate\005fbackend"`},
	{"field selection FROM alias", "SELECT p.pg_terminate_backend FROM unnest(ARRAY[0]) p"},
	{"field selection in a CTE",
		"WITH t AS (SELECT (0).pg_terminate_backend) SELECT * FROM t"},
	{"call after a parenthesised main query",
		"WITH a AS (SELECT 1) (SELECT 1) ORDER BY 1, pg_try_advisory_lock(0)"},
	// WITH as a column label does not open a list of CTEs.
	{"call after a WITH column label",
		"SELECT 1 AS with FROM pg_class, pg_terminate_backend(0)"},
	{"call after a WITH column label, chained",
		"SELECT 1 AS with FROM pg_class, pg_class c2, pg_terminate_backend(0)"},
	{"call after a WITH column label, unicode",
		`SELECT 1 AS with FROM pg_class, U&"pg\005fterminate\005fbackend"(0)`},
	{"call after WITH TIME ZONE",
		"SELECT now()::timestamp WITH TIME ZONE, pg_terminate_backend(0)"},
}

// harmlessSelects name side-effecting functions only as text, and stay
// read-only.
var harmlessSelects = []struct {
	name  string
	query string
}{
	{"name in a string literal", "SELECT 'pg_terminate_backend(0)'"},
	{"name in a dollar quote", "SELECT $$pg_terminate_backend(0)$$"},
	{"name in a block comment", "SELECT 1 /* pg_terminate_backend(0) */"},
	{"name in a line comment", "SELECT 1 -- pg_terminate_backend(0)"},
	{"name as a column", "SELECT pg_terminate_backend FROM t"},
	{"ordinary functions", "SELECT now(), count(*), current_setting('work_mem')"},
	{"qualified columns", "SELECT s.sub_name, s.sub_enabled FROM spock.subscription s"},
	{"decimal literal", "SELECT 1.5"},
	{"CTE named like a function", "WITH schedule(id) AS (SELECT 1) SELECT * FROM schedule"},
	{"alias column list", "SELECT * FROM generate_series(1, 2) AS dblink_rows(x)"},
}

// TestIsSelectQuery_SideEffectingFunctions pins issue #646 for the MCP
// tools: a SELECT-like statement that calls a side-effecting function is
// not read-only, however the call is spelled.
func TestIsSelectQuery_SideEffectingFunctions(t *testing.T) {
	for _, tt := range sideEffectingSpellings {
		t.Run(tt.name, func(t *testing.T) {
			if isSelectQuery(tt.query) {
				t.Errorf("isSelectQuery(%q) = true, want false", tt.query)
			}
		})
	}
	for _, tt := range harmlessSelects {
		t.Run(tt.name, func(t *testing.T) {
			if !isSelectQuery(tt.query) {
				t.Errorf("isSelectQuery(%q) = false, want true", tt.query)
			}
		})
	}
}

// TestInjectLimitOffset_SideEffectingSelect verifies that a
// side-effecting SELECT gets no injected LIMIT, which would silently cap
// how many rows the function acts on.
func TestInjectLimitOffset_SideEffectingSelect(t *testing.T) {
	query := "SELECT pg_terminate_backend(pid) FROM pg_stat_activity"
	got, hadLimit, hadOffset := injectLimitOffset(query, 100, 10)
	if got != query || hadLimit || hadOffset {
		t.Errorf("injectLimitOffset(%q) = %q, %v, %v; want it unchanged",
			query, got, hadLimit, hadOffset)
	}
}

// TestExplainOnlyPlans covers the statement starts that keep "EXPLAIN "
// plus the statement from running it.
func TestExplainOnlyPlans(t *testing.T) {
	tests := []struct {
		stmt  string
		plans bool
	}{
		{"SELECT 1", true},
		{"  -- c\n/* d */ select 1", true},
		{"WITH t AS (SELECT 1) SELECT * FROM t", true},
		{"INSERT INTO t VALUES (1)", true},
		{"UPDATE t SET a = 1", true},
		{"DELETE FROM t", true},
		{"MERGE INTO t USING s ON true WHEN MATCHED THEN DELETE", true},
		{"VALUES (1)", true},
		{"TABLE t", true},
		{"ANALYZE SELECT 1", false},
		{"ANALYSE SELECT 1", false}, //nolint:misspell // PostgreSQL accepts ANALYSE
		{"VERBOSE SELECT 1", false},
		{"(ANALYZE) SELECT 1", false},
		{"-- c\nANALYZE SELECT 1", false},
		{"-- x\rANALYZE\nSELECT 1", false},
		{"-- x\r\nSELECT 1", true},
		{"EXPLAIN ANALYZE SELECT 1", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := explainOnlyPlans(tt.stmt); got != tt.plans {
			t.Errorf("explainOnlyPlans(%q) = %v, want %v", tt.stmt, got, tt.plans)
		}
	}
}

// TestMayRunSideEffects covers the test_query guard: a query is refused
// only when it calls a side-effecting function and validating it could
// run it.
func TestMayRunSideEffects(t *testing.T) {
	tests := []struct {
		name  string
		query string
		runs  bool
	}{
		{"plain select is only planned", "SELECT pg_terminate_backend(0)", false},
		{"analyze prefix", "ANALYZE SELECT pg_terminate_backend(0)", true},
		{"alternative spelling prefix", "ANALYSE SELECT pg_reload_conf()", true}, //nolint:misspell // PostgreSQL accepts ANALYSE
		{"option list prefix", "(ANALYZE) SELECT pg_terminate_backend(0)", true},
		{"verbose analyze prefix", "VERBOSE ANALYZE SELECT pg_reload_conf()", true},
		{"comment then analyze", "/* x */ ANALYZE SELECT pg_reload_conf()", true},
		{"carriage return ends a comment", "-- x\rANALYZE\nSELECT pg_terminate_backend(1)", true},
		{"carriage return in a later statement", "SELECT 1; -- x\rANALYZE SELECT pg_reload_conf()", true},
		{"second statement analyzes", "SELECT 1; ANALYZE SELECT nextval('s')", true},
		{"quoted name with analyze", `ANALYZE SELECT "pg_reload_conf"()`, true},
		{"analyze without side effects", "ANALYZE SELECT 1", false},
		{"both statements only planned", "SELECT 1; SELECT pg_reload_conf()", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mayRunSideEffects(tt.query); got != tt.runs {
				t.Errorf("mayRunSideEffects(%q) = %v, want %v", tt.query, got, tt.runs)
			}
		})
	}
}

// TestTestQuery_RefusesExecutingSideEffects verifies that test_query,
// which any caller may use, cannot be turned into an EXPLAIN ANALYZE of a
// side-effecting function. The refusal comes before any connection is
// resolved.
func TestTestQuery_RefusesExecutingSideEffects(t *testing.T) {
	tool := TestQueryTool(nil, nil)
	queries := []string{
		"ANALYZE SELECT pg_terminate_backend(0)",
		"(ANALYZE) SELECT pg_terminate_backend(0)",
		"(ANALYZE TRUE, FORMAT JSON) SELECT pg_catalog.pg_terminate_backend(0)",
		"VERBOSE ANALYZE SELECT \"pg_terminate_backend\"(0)",
		"-- c\nANALYZE SELECT U&\"pg\\005fterminate\\005fbackend\"(0)",
		"SELECT 1; ANALYZE SELECT set_config('work_mem', '1MB', false)",
		"-- x\rANALYZE\nSELECT pg_terminate_backend(1)",
	}
	for _, q := range queries {
		resp, err := tool.Handler(map[string]any{"query": q})
		if err != nil {
			t.Fatalf("Handler(%q) returned a Go error: %v", q, err)
		}
		if !resp.IsError || !strings.Contains(responseText(resp), "side effects") {
			t.Errorf("Handler(%q) = %+v, want the side-effects refusal", q, resp)
		}
	}
}

// TestExecuteExplain_RefusesAnalyzeOfSideEffects verifies that
// execute_explain will not EXPLAIN ANALYZE a side-effecting SELECT,
// which would run it, in any spelling. The refusal comes before any
// connection is resolved.
func TestExecuteExplain_RefusesAnalyzeOfSideEffects(t *testing.T) {
	tool := ExecuteExplainTool(nil, nil)
	for _, tt := range sideEffectingSpellings {
		upper := strings.ToUpper(strings.TrimSpace(tt.query))
		if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "WITH") {
			// The tool already refuses anything else for everyone.
			continue
		}
		t.Run(tt.name, func(t *testing.T) {
			for _, args := range []map[string]any{
				{"query": tt.query},
				{"query": tt.query, "analyze": true},
			} {
				resp, err := tool.Handler(args)
				if err != nil {
					t.Fatalf("Handler returned a Go error: %v", err)
				}
				if !resp.IsError || !strings.Contains(responseText(resp), "side effects") {
					t.Errorf("Handler(%v) = %+v, want the side-effects refusal", args, resp)
				}
			}
		})
	}
}

// TestValidateWhereClause_SideEffectingFunctions verifies that count_rows
// refuses a WHERE clause calling a side-effecting function in the
// spellings its substring list misses.
func TestValidateWhereClause_SideEffectingFunctions(t *testing.T) {
	refused := []string{
		`"pg_reload_conf"() IS NOT NULL`,
		`set_config('work_mem', '1MB', false) IS NOT NULL`,
		`U&"pg\005fterminate\005fbackend"(0)`,
		`pg_catalog.pg_reload_conf ()`,
		`nextval('s') > 0`,
		`pg_try_advisory_lock(1)`,
		`(0).pg_reload_conf`,
		`(0).U&"pg\005fterminate\005fbackend"`,
		`id . "pg_reload_conf" IS NOT NULL`,
	}
	for _, clause := range refused {
		err := validateWhereClause(clause)
		if err == nil || !strings.Contains(err.Error(), "side effects") {
			t.Errorf("validateWhereClause(%q) = %v, want the side-effects refusal",
				clause, err)
		}
	}

	allowed := []string{
		"status = 'pg_reload_conf()'",
		"name = 'set_config(1)' AND id > 3",
		"pg_reload_conf IS NULL",
	}
	for _, clause := range allowed {
		if err := validateWhereClause(clause); err != nil {
			t.Errorf("validateWhereClause(%q) = %v, want nil", clause, err)
		}
	}
}

// TestQueryDatabase_SideEffectingSelects runs query_database against the
// test database and pins what each kind of caller sees for a SELECT that
// calls a side-effecting function (issue #646): query_database is
// read-only (issue #663), so every caller gets the read-only refusal.
func TestQueryDatabase_SideEffectingSelects(t *testing.T) {
	env := newResolverTestEnv(t)
	ctx := context.Background()

	// A caller with a read grant on the connection, and nothing more.
	store, closeStore := newRBACTestStore(t)
	defer closeStore()
	if err := store.CreateUser("reader", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	readerID, err := store.GetUserID("reader")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}
	groupID, err := store.CreateGroup("readers", "")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := store.AddUserToGroup(groupID, readerID); err != nil {
		t.Fatalf("AddUserToGroup: %v", err)
	}
	if err := store.GrantConnectionPrivilege(groupID, env.connID,
		auth.AccessLevelRead); err != nil {
		t.Fatalf("GrantConnectionPrivilege: %v", err)
	}
	readerResolver := NewConnectionResolver(env.clients,
		database.NewTestDatastoreWithSecret(env.pool, resolverTestSecret),
		auth.NewRBACChecker(store))

	run := func(t *testing.T, tool Tool, callCtx context.Context, args map[string]any) mcp.ToolResponse {
		t.Helper()
		args["__context"] = callCtx
		resp, err := tool.Handler(args)
		if err != nil {
			t.Fatalf("Handler returned a Go error: %v", err)
		}
		return resp
	}

	readerTool := QueryDatabaseTool(nil, readerResolver)
	readerCtx := nonSuperuserContextInt(readerID, "reader")

	t.Run("reader can read", func(t *testing.T) {
		resp := run(t, readerTool, readerCtx, map[string]any{
			"connection_id": float64(env.connID),
			"query":         "SELECT 'pg_terminate_backend(0)' AS text_only",
		})
		if resp.IsError {
			t.Fatalf("plain SELECT refused: %s", responseText(resp))
		}
	})

	for _, tt := range sideEffectingSpellings {
		t.Run("reader refused: "+tt.name, func(t *testing.T) {
			resp := run(t, readerTool, readerCtx, map[string]any{
				"connection_id": float64(env.connID),
				"query":         tt.query,
			})
			if !resp.IsError || responseText(resp) != queryDatabaseReadOnlyError {
				t.Errorf("query %q: got %+v, want the read-only refusal", tt.query, resp)
			}
		})
	}

	superCtx := context.WithValue(ctx, auth.IsSuperuserContextKey, true)
	superTool := QueryDatabaseTool(nil, env.resolver)

	t.Run("superuser refused too", func(t *testing.T) {
		// query_database is read-only for every caller (issue #663), so
		// a superuser, who holds read_write on every connection, gets
		// the same refusal as a reader.
		for _, tt := range sideEffectingSpellings {
			resp := run(t, superTool, superCtx, map[string]any{
				"connection_id": float64(env.connID),
				"query":         tt.query,
			})
			if !resp.IsError || responseText(resp) != queryDatabaseReadOnlyError {
				t.Errorf("query %q: got %+v, want the read-only refusal", tt.query, resp)
			}
		}
	})

	t.Run("selected connection refused too", func(t *testing.T) {
		resolved, errResp := env.resolver.Resolve(superCtx, map[string]any{
			"connection_id": float64(env.connID),
		}, nil)
		if errResp != nil {
			t.Fatalf("Resolve: %s", toolResponseText(errResp))
		}
		selectedTool := QueryDatabaseTool(resolved.Client, env.resolver)
		resp := run(t, selectedTool, superCtx, map[string]any{
			"query": `SELECT "pg_terminate_backend"(0)`,
		})
		if !resp.IsError || responseText(resp) != queryDatabaseReadOnlyError {
			t.Errorf("got %+v, want the read-only refusal", resp)
		}
	})

	t.Run("execute_explain plans without analyze", func(t *testing.T) {
		tool := ExecuteExplainTool(nil, readerResolver)
		resp := run(t, tool, readerCtx, map[string]any{
			"connection_id": float64(env.connID),
			"query":         "SELECT pg_terminate_backend(0)",
			"analyze":       false,
		})
		if resp.IsError {
			t.Fatalf("plain EXPLAIN refused: %s", responseText(resp))
		}
	})

	t.Run("test_query plans a side-effecting select", func(t *testing.T) {
		tool := TestQueryTool(nil, readerResolver)
		resp := run(t, tool, readerCtx, map[string]any{
			"connection_id": float64(env.connID),
			"query":         "SELECT pg_terminate_backend(0)",
		})
		if resp.IsError || responseText(resp) != "Query is valid." {
			t.Fatalf("got %+v, want the query reported valid", resp)
		}
	})
}

// TestQueryDatabase_SideEffectRefusedBeforeResolving verifies that
// query_database refuses a side-effecting SELECT before it resolves a
// connection: the client here has no connection selected, and the
// read-only refusal still comes first.
func TestQueryDatabase_SideEffectRefusedBeforeResolving(t *testing.T) {
	tool := QueryDatabaseTool(database.NewClient(nil), nil)
	resp, err := tool.Handler(map[string]any{
		"query": "SELECT (0).pg_terminate_backend",
	})
	if err != nil {
		t.Fatalf("Handler returned a Go error: %v", err)
	}
	if !resp.IsError || responseText(resp) != queryDatabaseReadOnlyError {
		t.Errorf("got %+v, want the read-only refusal", resp)
	}
}
