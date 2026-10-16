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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// postValidate drives validateQuery with the given request body.
func postValidate(
	t *testing.T,
	h *ConnectionHandler,
	connID int,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/connections/1/query/validate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.validateQuery(rec, req, connID)
	return rec
}

// decodeValidate decodes a validateQuery response body, insisting on a
// 200 first so a failure reports the server's message rather than a
// decode error against an error body.
func decodeValidate(t *testing.T, rec *httptest.ResponseRecorder) queryValidateResponse {
	t.Helper()

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %q)",
			rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp queryValidateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode validation response: %v (body %q)",
			err, rec.Body.String())
	}
	return resp
}

// testServerSupportsGenericPlan reports whether the test instance is
// PostgreSQL 16 or later, so the parameterised cases can assert the
// right outcome on either side of that boundary.
func testServerSupportsGenericPlan(t *testing.T, pool *pgxpool.Pool) bool {
	t.Helper()

	var versionNum int
	if err := pool.QueryRow(context.Background(),
		"SELECT current_setting('server_version_num')::int").
		Scan(&versionNum); err != nil {
		t.Fatalf("failed to read the server version: %v", err)
	}
	return versionNum >= genericPlanMinVersionNum
}

// assertStatement checks one statement result against the status and
// the substring its error is expected to carry.
func assertStatement(
	t *testing.T,
	got statementValidation,
	wantStatus, wantError string,
) {
	t.Helper()

	if got.Status != wantStatus {
		t.Errorf("status = %q, want %q (error %q)",
			got.Status, wantStatus, got.Error)
	}
	if wantError == "" {
		if got.Error != "" {
			t.Errorf("error = %q, want none", got.Error)
		}
		return
	}
	if !strings.Contains(got.Error, wantError) {
		t.Errorf("error = %q, want it to mention %q", got.Error, wantError)
	}
}

// TestValidateQuery_ValidStatements covers the happy path, including
// the SQL-aware split: the semicolon inside the string literal must
// not divide the second statement.
func TestValidateQuery_ValidStatements(t *testing.T) {
	h, pool, target, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	connID := seedQueryExecConnection(t, pool, target, target.host, target.port)

	rec := postValidate(t, h, connID,
		`{"query":"SELECT 1; SELECT 'a;b' AS text"}`)
	resp := decodeValidate(t, rec)

	if !resp.Valid {
		t.Errorf("valid = false, want true (statements %+v)", resp.Statements)
	}
	if resp.TotalStatements != 2 {
		t.Fatalf("total statements = %d, want 2", resp.TotalStatements)
	}
	if len(resp.Statements) != 2 {
		t.Fatalf("statements = %d, want 2", len(resp.Statements))
	}
	for _, stmt := range resp.Statements {
		assertStatement(t, stmt, validationValid, "")
	}
	if resp.Statements[1].Query != "SELECT 'a;b' AS text" {
		t.Errorf("second statement = %q, want the whole quoted literal",
			resp.Statements[1].Query)
	}
}

// TestValidateQuery_InvalidStatementDoesNotStopTheRest covers the
// savepoint: a statement PostgreSQL rejects must be reported as
// invalid without aborting validation of the statements after it.
func TestValidateQuery_InvalidStatementDoesNotStopTheRest(t *testing.T) {
	h, pool, target, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	connID := seedQueryExecConnection(t, pool, target, target.host, target.port)

	rec := postValidate(t, h, connID,
		`{"query":"SELECT no_such_column_for_issue_532 FROM pg_class; SELECT 1"}`)
	resp := decodeValidate(t, rec)

	if resp.Valid {
		t.Error("valid = true, want false when a statement is rejected")
	}
	if len(resp.Statements) != 2 {
		t.Fatalf("statements = %d, want 2 (validation must not stop early)",
			len(resp.Statements))
	}
	assertStatement(t, resp.Statements[0], validationInvalid,
		"no_such_column_for_issue_532")
	assertStatement(t, resp.Statements[1], validationValid, "")
}

// TestValidateQuery_MissingRelation covers the hallucinated-table case
// that motivated issue #532.
func TestValidateQuery_MissingRelation(t *testing.T) {
	h, pool, target, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	connID := seedQueryExecConnection(t, pool, target, target.host, target.port)

	rec := postValidate(t, h, connID,
		`{"query":"SELECT * FROM no_such_table_for_issue_532"}`)
	resp := decodeValidate(t, rec)

	if resp.Valid {
		t.Error("valid = true, want false for a missing relation")
	}
	assertStatement(t, resp.Statements[0], validationInvalid, "Validation error")
}

// TestValidateQuery_UnsupportedStatements covers the statement kinds
// EXPLAIN cannot plan. None of them makes the request invalid.
func TestValidateQuery_UnsupportedStatements(t *testing.T) {
	h, pool, target, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	connID := seedQueryExecConnection(t, pool, target, target.host, target.port)

	rec := postValidate(t, h, connID,
		`{"query":"VACUUM pg_class; SET work_mem = '4MB'; SHOW work_mem; `+
			`ALTER SYSTEM SET work_mem = '4MB'; EXPLAIN ANALYZE SELECT 1"}`)
	resp := decodeValidate(t, rec)

	if !resp.Valid {
		t.Error("valid = false, want true: nothing was found wrong")
	}
	if len(resp.Statements) != 5 {
		t.Fatalf("statements = %d, want 5", len(resp.Statements))
	}
	for _, stmt := range resp.Statements {
		assertStatement(t, stmt, validationUnsupported, "not validated")
	}
}

// TestValidateQuery_ParameterisedStatement covers the GENERIC_PLAN
// path, and the pre-16 fallback on an older server.
func TestValidateQuery_ParameterisedStatement(t *testing.T) {
	h, pool, target, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	connID := seedQueryExecConnection(t, pool, target, target.host, target.port)

	rec := postValidate(t, h, connID,
		`{"query":"SELECT relname FROM pg_class WHERE relname = $1"}`)
	resp := decodeValidate(t, rec)

	if testServerSupportsGenericPlan(t, pool) {
		if !resp.Valid {
			t.Errorf("valid = false, want true (statements %+v)", resp.Statements)
		}
		assertStatement(t, resp.Statements[0], validationValid, "")
		return
	}
	if !resp.Valid {
		t.Error("valid = false, want true: an unvalidatable statement is not invalid")
	}
	assertStatement(t, resp.Statements[0], validationUnsupported, "PostgreSQL 16")
}

// TestValidateQuery_ParameterisedStatementIsStillChecked confirms the
// GENERIC_PLAN path reports a genuinely wrong parameterised statement
// as invalid rather than waving it through.
func TestValidateQuery_ParameterisedStatementIsStillChecked(t *testing.T) {
	h, pool, target, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	if !testServerSupportsGenericPlan(t, pool) {
		t.Skip("server predates EXPLAIN (GENERIC_PLAN)")
	}
	connID := seedQueryExecConnection(t, pool, target, target.host, target.port)

	rec := postValidate(t, h, connID,
		`{"query":"SELECT * FROM no_such_table_for_issue_532 WHERE a = $1"}`)
	resp := decodeValidate(t, rec)

	if resp.Valid {
		t.Error("valid = true, want false for a missing relation")
	}
	assertStatement(t, resp.Statements[0], validationInvalid, "Validation error")
}

// TestValidateQuery_NothingIsExecuted plans a write against a real
// table and confirms the table is untouched afterwards.
func TestValidateQuery_NothingIsExecuted(t *testing.T) {
	h, pool, target, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`CREATE TABLE IF NOT EXISTS query_exec_write_target (a int)`); err != nil {
		t.Fatalf("failed to create the write target: %v", err)
	}

	connID := seedQueryExecConnection(t, pool, target, target.host, target.port)

	rec := postValidate(t, h, connID,
		`{"query":"INSERT INTO query_exec_write_target VALUES (1)"}`)
	resp := decodeValidate(t, rec)

	if !resp.Valid {
		t.Errorf("valid = false, want true (statements %+v)", resp.Statements)
	}
	assertStatement(t, resp.Statements[0], validationValid, "")

	var rows int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM query_exec_write_target`).Scan(&rows); err != nil {
		t.Fatalf("failed to count the write target rows: %v", err)
	}
	if rows != 0 {
		t.Errorf("write target holds %d rows, want 0: validation executed the statement",
			rows)
	}
}

// TestValidateQuery_DatabaseNameOverride covers the optional
// database_name field, which selects the database the statements are
// planned against.
func TestValidateQuery_DatabaseNameOverride(t *testing.T) {
	h, pool, target, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	connID := seedQueryExecConnection(t, pool, target, target.host, target.port)

	rec := postValidate(t, h, connID,
		`{"query":"SELECT current_database()","database_name":"`+
			target.database+`"}`)
	resp := decodeValidate(t, rec)

	if !resp.Valid {
		t.Errorf("valid = false, want true (statements %+v)", resp.Statements)
	}
}

// TestValidateQuery_UnknownConnection covers the 404 path.
func TestValidateQuery_UnknownConnection(t *testing.T) {
	h, _, _, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	rec := postValidate(t, h, 999999, `{"query":"SELECT 1"}`)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (body %q)",
			rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

// TestValidateQuery_UnreachableServer covers the 500 path, where the
// connection row points at a port nothing is listening on.
func TestValidateQuery_UnreachableServer(t *testing.T) {
	h, pool, target, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	connID := seedQueryExecConnection(t, pool, target, target.host, 1)

	rec := postValidate(t, h, connID, `{"query":"SELECT 1"}`)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d (body %q)",
			rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
}

// TestValidateQuery_RouteDispatch confirms the subpath router reaches
// the validation handler rather than the execution handler or a 404.
func TestValidateQuery_RouteDispatch(t *testing.T) {
	h, pool, target, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	connID := seedQueryExecConnection(t, pool, target, target.host, target.port)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/connections/"+itoa(int64(connID))+"/query/validate",
		strings.NewReader(`{"query":"SELECT 1"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.handleConnectionSubpath(rec, req)

	resp := decodeValidate(t, rec)
	if !resp.Valid || len(resp.Statements) != 1 {
		t.Errorf("response = %+v, want one valid statement", resp)
	}
}
