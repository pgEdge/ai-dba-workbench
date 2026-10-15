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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestIsReadOnlyStatement_SideEffectingFunctions pins issue #646 for
// the query endpoint: a statement that calls a side-effecting function
// is a write wherever the classifier reads a SELECT body, whilst the
// names appearing as text are not calls. The spellings and bypass
// attempts the call finder handles are pinned in package sqllex.
func TestIsReadOnlyStatement_SideEffectingFunctions(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		readOnly bool
	}{
		{"select", "SELECT pg_terminate_backend(1234)", false},
		{"schema qualified", "SELECT pg_catalog.pg_terminate_backend(1)", false},
		{"quoted name", `SELECT "pg_terminate_backend"(1)`, false},
		{"unicode escaped name", `SELECT U&"pg\005fterminate\005fbackend"(1)`, false},
		{"comment before paren", "SELECT pg_reload_conf/* x */()", false},
		{"in a FROM clause", "SELECT * FROM pg_terminate_backend(1) AS r", false},
		{"in a CTE", "WITH t AS (SELECT pg_reload_conf()) SELECT * FROM t", false},
		{"in a CTE body after the CTEs", "WITH t AS (SELECT 1) SELECT pg_reload_conf() FROM t", false},
		{"table then a call", "TABLE t; SELECT nextval('s')", false},
		{"explain analyze runs the call", "EXPLAIN ANALYZE SELECT pg_reload_conf()", false},
		{"explain options analyze runs the call", "EXPLAIN (ANALYZE, COSTS OFF) SELECT pg_reload_conf()", false},
		{"second statement", "SELECT 1; SELECT pg_reload_conf()", false},
		{"hidden under the escape reading",
			`SELECT '\'' AS c, pg_reload_conf() --'`, false},
		{"hidden under the standard reading",
			`SELECT 'a\' AS c, pg_reload_conf() --'`, false},
		{"uescape fails closed", `SELECT U&"pg!005freload_conf" UESCAPE '!'()`, false},
		{"spock schema", "SELECT spock.sub_drop('s')", false},

		{"name in a string literal", "SELECT 'pg_terminate_backend(1)'", true},
		{"name in a dollar quote", "SELECT $$pg_reload_conf()$$", true},
		{"name in a comment", "SELECT 1 /* pg_reload_conf() */", true},
		{"ordinary functions", "SELECT now(), count(*), current_setting('work_mem') FROM t", true},
		{"explain without analyze plans only", "EXPLAIN SELECT pg_reload_conf()", true},
		{"show", "SHOW work_mem", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isReadOnlyStatement(tt.input); got != tt.readOnly {
				t.Errorf("isReadOnlyStatement(%q) = %v, want %v",
					tt.input, got, tt.readOnly)
			}
		})
	}
}

// TestSideEffectingFunction_RequiresConfirmation proves that the query
// endpoint asks for confirmation before running a SELECT that calls a
// side-effecting function, and lists that statement alone as the write.
// The handler answers before it reaches the nil datastore.
func TestSideEffectingFunction_RequiresConfirmation(t *testing.T) {
	handler := newTestConnectionHandlerWithRBAC(t)

	query := "SELECT 1; SELECT pg_terminate_backend(pid) FROM pg_stat_activity"
	body, err := json.Marshal(queryRequest{Query: query})
	if err != nil {
		t.Fatalf("failed to encode the request: %v", err)
	}
	req := withSuperuser(httptest.NewRequest(http.MethodPost,
		"/api/v1/connections/1/query", bytes.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.executeQuery(rec, req, 1)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %q)",
			rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp multiQueryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !resp.RequiresConfirmation {
		t.Fatal("expected requires_confirmation for pg_terminate_backend")
	}
	if len(resp.WriteStatements) != 1 ||
		!strings.Contains(resp.WriteStatements[0], "pg_terminate_backend") {
		t.Errorf("write statements = %q, want the pg_terminate_backend statement",
			resp.WriteStatements)
	}
}
