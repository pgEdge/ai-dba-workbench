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
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// TestExplainCommand covers the statement classifier that decides
// whether a statement can be planned at all, and with which EXPLAIN.
func TestExplainCommand(t *testing.T) {
	tests := []struct {
		name        string
		stmt        string
		genericPlan bool
		wantSQL     string
		wantReason  string
	}{
		{
			name:    "select",
			stmt:    "SELECT 1",
			wantSQL: "EXPLAIN SELECT 1",
		},
		{
			name:    "leading line comment is stripped before EXPLAIN",
			stmt:    "-- find the rows\nSELECT 1",
			wantSQL: "EXPLAIN SELECT 1",
		},
		{
			name:    "leading block comment is stripped before EXPLAIN",
			stmt:    "/* note */ SELECT 1",
			wantSQL: "EXPLAIN SELECT 1",
		},
		{
			name:    "common table expression",
			stmt:    "WITH x AS (SELECT 1) SELECT * FROM x",
			wantSQL: "EXPLAIN WITH x AS (SELECT 1) SELECT * FROM x",
		},
		{
			name:    "insert",
			stmt:    "INSERT INTO t VALUES (1)",
			wantSQL: "EXPLAIN INSERT INTO t VALUES (1)",
		},
		{
			name:    "update",
			stmt:    "UPDATE t SET a = 1",
			wantSQL: "EXPLAIN UPDATE t SET a = 1",
		},
		{
			name:    "delete",
			stmt:    "DELETE FROM t",
			wantSQL: "EXPLAIN DELETE FROM t",
		},
		{
			name:    "values",
			stmt:    "VALUES (1)",
			wantSQL: "EXPLAIN VALUES (1)",
		},
		{
			name:    "table",
			stmt:    "TABLE pg_class",
			wantSQL: "EXPLAIN TABLE pg_class",
		},
		{
			name:    "merge",
			stmt:    "MERGE INTO t USING s ON t.a = s.a WHEN MATCHED THEN DO NOTHING",
			wantSQL: "EXPLAIN MERGE INTO t USING s ON t.a = s.a WHEN MATCHED THEN DO NOTHING",
		},
		{
			name:       "create table is not explainable",
			stmt:       "CREATE TABLE t (a int)",
			wantReason: "CREATE",
		},
		{
			name:       "alter system is not explainable",
			stmt:       "ALTER SYSTEM SET work_mem = '4MB'",
			wantReason: "ALTER",
		},
		{
			name:       "vacuum is not explainable",
			stmt:       "VACUUM ANALYZE t",
			wantReason: "VACUUM",
		},
		{
			name:       "set is not explainable",
			stmt:       "SET work_mem = '4MB'",
			wantReason: "SET",
		},
		{
			name:       "show is not explainable",
			stmt:       "SHOW work_mem",
			wantReason: "SHOW",
		},
		{
			name:       "grant is not explainable",
			stmt:       "GRANT SELECT ON t TO reporting",
			wantReason: "GRANT",
		},
		{
			name:       "reindex is not explainable",
			stmt:       "REINDEX INDEX t_pkey",
			wantReason: "REINDEX",
		},
		{
			name:       "cluster is not explainable",
			stmt:       "CLUSTER t USING t_pkey",
			wantReason: "CLUSTER",
		},
		{
			name:       "analyze is not explainable",
			stmt:       "ANALYZE t",
			wantReason: "ANALYZE",
		},
		{
			name:       "statement starting with a parenthesis",
			stmt:       "(oops)",
			wantReason: "unrecognized",
		},
		{
			name:       "comment only",
			stmt:       "-- nothing here",
			wantReason: "no SQL",
		},
		{
			name:    "caller supplied EXPLAIN is used as written",
			stmt:    "EXPLAIN SELECT 1",
			wantSQL: "EXPLAIN SELECT 1",
		},
		{
			name:       "EXPLAIN ANALYZE would execute",
			stmt:       "EXPLAIN ANALYZE SELECT 1",
			wantReason: "runs the statement",
		},
		{
			name:       "EXPLAIN ANALYSE would execute",       //nolint:misspell // ANALYSE is a PostgreSQL keyword
			stmt:       "EXPLAIN (ANALYSE, BUFFERS) SELECT 1", //nolint:misspell // ANALYSE is a PostgreSQL keyword
			wantReason: "runs the statement",
		},
		{
			name:       "caller supplied EXPLAIN with a placeholder",
			stmt:       "EXPLAIN SELECT * FROM t WHERE a = $1",
			wantReason: "EXPLAIN carrying parameter",
		},
		{
			name:        "placeholder on a server with GENERIC_PLAN",
			stmt:        "SELECT * FROM t WHERE a = $1",
			genericPlan: true,
			wantSQL:     "EXPLAIN (GENERIC_PLAN) SELECT * FROM t WHERE a = $1",
		},
		{
			name:       "placeholder on a server without GENERIC_PLAN",
			stmt:       "SELECT * FROM t WHERE a = $1",
			wantReason: "PostgreSQL 16",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotReason := explainCommand(tt.stmt, tt.genericPlan)

			if gotSQL != tt.wantSQL {
				t.Errorf("explain command = %q, want %q", gotSQL, tt.wantSQL)
			}
			if tt.wantReason == "" {
				if gotReason != "" {
					t.Errorf("reason = %q, want none", gotReason)
				}
				return
			}
			if !strings.Contains(gotReason, tt.wantReason) {
				t.Errorf("reason = %q, want it to mention %q",
					gotReason, tt.wantReason)
			}
		})
	}
}

// TestFirstSQLWord covers the keyword extraction the unsupported
// message is built from.
func TestFirstSQLWord(t *testing.T) {
	tests := []struct {
		upper string
		want  string
	}{
		{"SELECT 1", "SELECT"},
		{"ALTER SYSTEM", "ALTER"},
		{"", "unrecognized"},
		{"(SELECT 1)", "unrecognized"},
	}
	for _, tt := range tests {
		if got := firstSQLWord(tt.upper); got != tt.want {
			t.Errorf("firstSQLWord(%q) = %q, want %q", tt.upper, got, tt.want)
		}
	}
}

func TestValidateQuery_MethodNotAllowed(t *testing.T) {
	handler := newTestConnectionHandlerWithRBAC()

	for _, method := range []string{
		http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodPatch,
	} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method,
				"/api/v1/connections/1/query/validate", nil)
			rec := httptest.NewRecorder()

			handler.validateQuery(rec, req, 1)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("status = %d, want %d",
					rec.Code, http.StatusMethodNotAllowed)
			}
			if allowed := rec.Header().Get("Allow"); allowed != "POST" {
				t.Errorf("Allow header = %q, want POST", allowed)
			}
		})
	}
}

// TestValidateQuery_BadRequests covers the 400 paths, all of which are
// reached before any database connection is opened; the handler has a
// nil datastore, so reaching one would panic.
func TestValidateQuery_BadRequests(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "empty query", body: `{"query": ""}`},
		{name: "whitespace only", body: `{"query": "   "}`},
		{name: "comment only", body: `{"query": "-- nothing to run"}`},
		{name: "invalid JSON", body: `{invalid json}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newTestConnectionHandlerWithRBAC()
			req := httptest.NewRequest(http.MethodPost,
				"/api/v1/connections/1/query/validate",
				bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			handler.validateQuery(rec, req, 1)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d (body %q)",
					rec.Code, http.StatusBadRequest, rec.Body.String())
			}
		})
	}
}

// TestValidateQuery_ReadCheckDenied covers the RBAC gate. The caller
// holds a token scoped to a different connection, so the read check
// fails; the nil datastore proves the gate runs before any datastore
// call, and the invalid body proves it runs before the body is decoded.
func TestValidateQuery_ReadCheckDenied(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	userID := newIssue207UnprivilegedUser(t, store, "issue532_validate_denied")
	tokenID, _ := createTokenForUser(t, store, userID, "issue532_validate")
	if err := store.SetTokenConnectionScope(tokenID, []auth.ScopedConnection{
		{ConnectionID: 1, AccessLevel: auth.AccessLevelRead},
	}); err != nil {
		t.Fatalf("Failed to scope the test token: %v", err)
	}

	handler := NewConnectionHandlerWithSecurity(
		nil, store, auth.NewRBACChecker(store), false, nil, nil)

	for _, body := range []string{`{"query": "SELECT 1"}`, `{invalid json}`} {
		req := httptest.NewRequest(http.MethodPost,
			"/api/v1/connections/4242/query/validate",
			bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		ctx := context.WithValue(req.Context(), auth.UserIDContextKey, userID)
		ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, false)
		ctx = context.WithValue(ctx, auth.TokenIDContextKey, tokenID)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		handler.validateQuery(rec, req, 4242)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d (body %q)",
				rec.Code, http.StatusForbidden, rec.Body.String())
		}
		var resp ErrorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if !strings.Contains(resp.Error, "Permission denied") {
			t.Errorf("error = %q, want it to mention Permission denied",
				resp.Error)
		}
	}
}
