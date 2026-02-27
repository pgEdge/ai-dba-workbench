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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// newTestConnectionHandlerWithRBAC creates a handler with auth disabled so
// RBAC checks pass without requiring a database.
func newTestConnectionHandlerWithRBAC() *ConnectionHandler {
	rbac := auth.NewRBACChecker(nil)
	return NewConnectionHandler(nil, nil, rbac)
}

func TestExecuteQuery_MethodNotAllowed(t *testing.T) {
	handler := newTestConnectionHandlerWithRBAC()

	methods := []string{http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodPatch}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/v1/connections/1/query", nil)
			rec := httptest.NewRecorder()

			handler.executeQuery(rec, req, 1)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("Expected status %d, got %d",
					http.StatusMethodNotAllowed, rec.Code)
			}

			allowed := rec.Header().Get("Allow")
			if allowed != "POST" {
				t.Errorf("Expected Allow header 'POST', got %q", allowed)
			}
		})
	}
}

func TestExecuteQuery_EmptyQuery(t *testing.T) {
	handler := newTestConnectionHandlerWithRBAC()

	body := `{"query": ""}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connections/1/query",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.executeQuery(rec, req, 1)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if response.Error != "Query is required" {
		t.Errorf("Expected error 'Query is required', got %q", response.Error)
	}
}

func TestExecuteQuery_WhitespaceOnlyQuery(t *testing.T) {
	handler := newTestConnectionHandlerWithRBAC()

	body := `{"query": "   "}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connections/1/query",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.executeQuery(rec, req, 1)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestExecuteQuery_InvalidJSON(t *testing.T) {
	handler := newTestConnectionHandlerWithRBAC()

	body := `{invalid json}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connections/1/query",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.executeQuery(rec, req, 1)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestExecuteQuery_NoDatastore(t *testing.T) {
	handler := newTestConnectionHandlerWithRBAC()

	body := `{"query": "SELECT 1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connections/1/query",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	// With nil datastore, GetConnectionWithPassword will panic.
	defer func() {
		if r := recover(); r != nil {
			t.Log("Got expected panic with nil datastore")
		}
	}()

	handler.executeQuery(rec, req, 1)

	// If no panic, the handler should return an error status
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected error status, got %d", rec.Code)
	}
}

func TestFormatValueForJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected string
	}{
		{"nil returns NULL", nil, "NULL"},
		{"string value", "hello", "hello"},
		{"integer value", 42, "42"},
		{"float value", 3.14, "3.14"},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
		{"empty string", "", ""},
		{"time value", time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC), "2025-01-15T10:30:00Z"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatValueForJSON(tt.input)
			if result != tt.expected {
				t.Errorf("formatValueForJSON(%v) = %q, want %q",
					tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatValueForJSON_PgTypes(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected string
	}{
		{
			"pgtype.Text valid",
			pgtype.Text{String: "test", Valid: true},
			"test",
		},
		{
			"pgtype.Text null",
			pgtype.Text{Valid: false},
			"",
		},
		{
			"pgtype.Bool true",
			pgtype.Bool{Bool: true, Valid: true},
			"true",
		},
		{
			"pgtype.Bool false",
			pgtype.Bool{Bool: false, Valid: true},
			"false",
		},
		{
			"pgtype.Int4 valid",
			pgtype.Int4{Int32: 42, Valid: true},
			"42",
		},
		{
			"pgtype.Int4 null",
			pgtype.Int4{Valid: false},
			"",
		},
		{
			"pgtype.Int8 valid",
			pgtype.Int8{Int64: 123456789, Valid: true},
			"123456789",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatValueForJSON(tt.input)
			if result != tt.expected {
				t.Errorf("formatValueForJSON(%v) = %q, want %q",
					tt.input, result, tt.expected)
			}
		})
	}
}

func TestSplitStatements(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			"single statement no semicolon",
			"SELECT 1",
			[]string{"SELECT 1"},
		},
		{
			"single statement with semicolon",
			"SELECT 1;",
			[]string{"SELECT 1"},
		},
		{
			"two statements",
			"SELECT 1; SELECT 2",
			[]string{"SELECT 1", "SELECT 2"},
		},
		{
			"two statements with trailing semicolon",
			"SELECT 1; SELECT 2;",
			[]string{"SELECT 1", "SELECT 2"},
		},
		{
			"statements with extra whitespace",
			"  SELECT 1 ;  SELECT 2  ;  ",
			[]string{"SELECT 1", "SELECT 2"},
		},
		{
			"empty input",
			"",
			nil,
		},
		{
			"only semicolons",
			";;;",
			nil,
		},
		{
			"only whitespace and semicolons",
			"  ;  ;  ",
			nil,
		},
		{
			"statement with SQL comment",
			"-- this is a comment\nSELECT 1",
			[]string{"-- this is a comment\nSELECT 1"},
		},
		{
			"only comments filtered out",
			"-- just a comment",
			nil,
		},
		{
			"line comment consumes rest of line",
			"SELECT 1; -- just a comment; SELECT 2",
			[]string{"SELECT 1"},
		},
		{
			"comments between semicolons filtered out",
			"SELECT 1; -- just a comment\n; SELECT 2",
			[]string{"SELECT 1", "SELECT 2"},
		},
		{
			"multiline with comments",
			"-- Query 1\nSELECT 1;\n-- Query 2\nSELECT 2;",
			[]string{"-- Query 1\nSELECT 1", "-- Query 2\nSELECT 2"},
		},
		{
			"comment-only block between statements",
			"SELECT 1;\n-- middle comment\n;\nSELECT 2",
			[]string{"SELECT 1", "SELECT 2"},
		},
		{
			"single_quoted_semicolon",
			"SELECT 'a;b'; SELECT 2",
			[]string{"SELECT 'a;b'", "SELECT 2"},
		},
		{
			"escaped_quotes",
			"SELECT 'it''s;here'; SELECT 2",
			[]string{"SELECT 'it''s;here'", "SELECT 2"},
		},
		{
			"dollar_quoted",
			"SELECT $$a;b$$; SELECT 2",
			[]string{"SELECT $$a;b$$", "SELECT 2"},
		},
		{
			"tagged_dollar",
			"SELECT $tag$a;b$tag$; SELECT 2",
			[]string{"SELECT $tag$a;b$tag$", "SELECT 2"},
		},
		{
			"line_comment_semicolon",
			"SELECT 1 -- comment; here\n; SELECT 2",
			[]string{"SELECT 1 -- comment; here", "SELECT 2"},
		},
		{
			"block_comment",
			"SELECT /* ; */ 1; SELECT 2",
			[]string{"SELECT /* ; */ 1", "SELECT 2"},
		},
		{
			"nested_block_comment",
			"SELECT /* outer /* inner ; */ ; */ 1; SELECT 2",
			[]string{"SELECT /* outer /* inner ; */ ; */ 1", "SELECT 2"},
		},
		{
			"trailing_semicolons",
			"SELECT 1;",
			[]string{"SELECT 1"},
		},
		{
			"whitespace_only_segments",
			"   ;  ;  ",
			nil,
		},
		{
			"block_comment_only",
			"/* just a comment */",
			nil,
		},
		{
			"plpgsql_function_body",
			"CREATE FUNCTION f() RETURNS void AS $$ BEGIN PERFORM 1; END; $$ LANGUAGE plpgsql; SELECT 1",
			[]string{"CREATE FUNCTION f() RETURNS void AS $$ BEGIN PERFORM 1; END; $$ LANGUAGE plpgsql", "SELECT 1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitStatements(tt.input)
			if len(result) != len(tt.expected) {
				t.Fatalf("splitStatements(%q) returned %d statements, want %d: %v",
					tt.input, len(result), len(tt.expected), result)
			}
			for i, stmt := range result {
				if stmt != tt.expected[i] {
					t.Errorf("splitStatements(%q)[%d] = %q, want %q",
						tt.input, i, stmt, tt.expected[i])
				}
			}
		})
	}
}

func TestMultiQueryResponse_JSONSerialization(t *testing.T) {
	resp := multiQueryResponse{
		Results: []statementResult{
			{
				Columns:   []string{"id", "name"},
				Rows:      [][]string{{"1", "Alice"}, {"2", "Bob"}},
				RowCount:  2,
				Truncated: false,
				Query:     "SELECT id, name FROM users",
			},
		},
		TotalStatements: 1,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal multiQueryResponse: %v", err)
	}

	var decoded multiQueryResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal multiQueryResponse: %v", err)
	}

	if len(decoded.Results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(decoded.Results))
	}
	if decoded.TotalStatements != 1 {
		t.Errorf("Expected total_statements 1, got %d", decoded.TotalStatements)
	}

	r := decoded.Results[0]
	if len(r.Columns) != 2 {
		t.Errorf("Expected 2 columns, got %d", len(r.Columns))
	}
	if r.Columns[0] != "id" || r.Columns[1] != "name" {
		t.Errorf("Unexpected columns: %v", r.Columns)
	}
	if len(r.Rows) != 2 {
		t.Errorf("Expected 2 rows, got %d", len(r.Rows))
	}
	if r.RowCount != 2 {
		t.Errorf("Expected row_count 2, got %d", r.RowCount)
	}
	if r.Truncated {
		t.Error("Expected truncated to be false")
	}
	if r.Query != "SELECT id, name FROM users" {
		t.Errorf("Expected query string, got %q", r.Query)
	}
	if r.Error != "" {
		t.Errorf("Expected no error, got %q", r.Error)
	}
}

func TestMultiQueryResponse_MultipleResults(t *testing.T) {
	resp := multiQueryResponse{
		Results: []statementResult{
			{
				Columns:  []string{"count"},
				Rows:     [][]string{{"42"}},
				RowCount: 1,
				Query:    "SELECT count(*) FROM users",
			},
			{
				Columns:  []string{"version"},
				Rows:     [][]string{{"PostgreSQL 16.1"}},
				RowCount: 1,
				Query:    "SELECT version()",
			},
		},
		TotalStatements: 2,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal multiQueryResponse: %v", err)
	}

	var decoded multiQueryResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal multiQueryResponse: %v", err)
	}

	if len(decoded.Results) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(decoded.Results))
	}
	if decoded.TotalStatements != 2 {
		t.Errorf("Expected total_statements 2, got %d", decoded.TotalStatements)
	}
	if decoded.Results[0].Query != "SELECT count(*) FROM users" {
		t.Errorf("Unexpected first query: %q", decoded.Results[0].Query)
	}
	if decoded.Results[1].Query != "SELECT version()" {
		t.Errorf("Unexpected second query: %q", decoded.Results[1].Query)
	}
}

func TestMultiQueryResponse_WithError(t *testing.T) {
	resp := multiQueryResponse{
		Results: []statementResult{
			{
				Columns:  []string{"id"},
				Rows:     [][]string{{"1"}},
				RowCount: 1,
				Query:    "SELECT 1 AS id",
			},
			{
				Query: "SELECT * FROM nonexistent",
				Error: "Query error: relation \"nonexistent\" does not exist",
			},
		},
		TotalStatements: 3,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal multiQueryResponse: %v", err)
	}

	var decoded multiQueryResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal multiQueryResponse: %v", err)
	}

	if len(decoded.Results) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(decoded.Results))
	}
	// First result should have data
	if decoded.Results[0].Error != "" {
		t.Errorf("Expected no error on first result, got %q", decoded.Results[0].Error)
	}
	if decoded.Results[0].RowCount != 1 {
		t.Errorf("Expected row_count 1 on first result, got %d", decoded.Results[0].RowCount)
	}
	// Second result should have error
	if decoded.Results[1].Error == "" {
		t.Error("Expected error on second result")
	}
	// Total statements reflects the original count, not results returned
	if decoded.TotalStatements != 3 {
		t.Errorf("Expected total_statements 3, got %d", decoded.TotalStatements)
	}
}

func TestMultiQueryResponse_EmptyRows(t *testing.T) {
	resp := multiQueryResponse{
		Results: []statementResult{
			{
				Columns:  []string{"id"},
				Rows:     [][]string{},
				RowCount: 0,
				Query:    "SELECT id FROM empty_table",
			},
		},
		TotalStatements: 1,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal multiQueryResponse: %v", err)
	}

	// Verify the rows field is present as an empty array
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Failed to unmarshal raw: %v", err)
	}

	results, ok := raw["results"].([]interface{})
	if !ok {
		t.Fatal("Expected results to be an array")
	}
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	firstResult, ok := results[0].(map[string]interface{})
	if !ok {
		t.Fatal("Expected first result to be an object")
	}

	rows, ok := firstResult["rows"].([]interface{})
	if !ok {
		t.Fatal("Expected rows to be an array")
	}
	if len(rows) != 0 {
		t.Errorf("Expected empty rows array, got %d elements", len(rows))
	}
}

func TestStatementResult_ErrorOmitsErrorField(t *testing.T) {
	// Successful result should omit the error field
	successResult := statementResult{
		Columns:  []string{"id"},
		Rows:     [][]string{{"1"}},
		RowCount: 1,
		Query:    "SELECT 1 AS id",
	}

	data, err := json.Marshal(successResult)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Error should be omitted for success results
	if _, ok := raw["error"]; ok {
		t.Error("Expected error to be omitted for success result")
	}

	// Error result should include the error field
	errResult := statementResult{
		Query: "SELECT bad",
		Error: "some error",
	}

	data, err = json.Marshal(errResult)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	raw = map[string]interface{}{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if _, ok := raw["error"]; !ok {
		t.Error("Expected error to be present for error result")
	}
	if raw["error"] != "some error" {
		t.Errorf("Expected error 'some error', got %q", raw["error"])
	}
}

func TestConnectionSubpath_QueryRoute(t *testing.T) {
	handler := newTestConnectionHandlerWithRBAC()

	// Verify that /api/v1/connections/1/query routes to executeQuery
	// by checking it does not return 404
	body := `{"query": "SELECT 1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connections/1/query",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	// With nil datastore, we expect the handler to reach the query execution
	// path and fail there, not return 404 from routing
	defer func() {
		if r := recover(); r != nil {
			// Expected: nil datastore causes a panic after routing succeeds
			t.Log("Got expected panic after successful routing")
		}
	}()

	handler.handleConnectionSubpath(rec, req)

	// If no panic, verify we did not get a 404 (which would mean routing failed)
	if rec.Code == http.StatusNotFound {
		t.Error("Expected query route to be handled, got 404")
	}
}

func TestStripLeadingComments(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			"no comments",
			"SELECT 1",
			"SELECT 1",
		},
		{
			"single comment line",
			"-- comment\nSELECT 1",
			"SELECT 1",
		},
		{
			"multiple comment lines",
			"-- comment 1\n-- comment 2\nSELECT 1",
			"SELECT 1",
		},
		{
			"blank lines and comments",
			"\n  \n-- comment\n\nSELECT 1",
			"SELECT 1",
		},
		{
			"only comments",
			"-- just a comment\n-- another",
			"",
		},
		{
			"empty string",
			"",
			"",
		},
		{
			"whitespace-only lines before SQL",
			"  \n\t\nSHOW shared_buffers",
			"SHOW shared_buffers",
		},
		{
			"comment with indentation",
			"  -- indented comment\nEXPLAIN SELECT 1",
			"EXPLAIN SELECT 1",
		},
		{
			"preserves remaining lines",
			"-- header\nSELECT 1\nFROM foo",
			"SELECT 1\nFROM foo",
		},
		{
			"block comment",
			"/* comment */ SELECT 1",
			"SELECT 1",
		},
		{
			"nested block comment",
			"/* outer /* inner */ */ SELECT 1",
			"SELECT 1",
		},
		{
			"mixed line and block comments",
			"-- line\n/* block */ SELECT 1",
			"SELECT 1",
		},
		{
			"multiple line comments",
			"-- one\n-- two\nSELECT 1",
			"SELECT 1",
		},
		{
			"block comment with newlines",
			"/* multi\n   line\n   comment */\nSELECT 1",
			"SELECT 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripLeadingComments(tt.input)
			if result != tt.expected {
				t.Errorf("stripLeadingComments(%q) = %q, want %q",
					tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsReadOnlyStatement(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		readOnly bool
	}{
		{"SELECT", "SELECT 1", true},
		{"select lowercase", "select * from users", true},
		{"SELECT with leading comment", "-- get data\nSELECT 1", true},
		{"WITH CTE", "WITH cte AS (SELECT 1) SELECT * FROM cte", true},
		{"with lowercase", "with x as (select 1) select * from x", true},
		{"SHOW", "SHOW shared_buffers", true},
		{"show lowercase", "show work_mem", true},
		{"EXPLAIN", "EXPLAIN SELECT 1", true},
		{"explain lowercase", "explain select 1", true},
		{"TABLE shorthand", "TABLE users", true},
		{"table lowercase", "table users", true},
		{"ALTER", "ALTER SYSTEM SET work_mem = '16MB'", false},
		{"CREATE", "CREATE TABLE foo (id int)", false},
		{"DROP", "DROP TABLE foo", false},
		{"INSERT", "INSERT INTO foo VALUES (1)", false},
		{"UPDATE", "UPDATE foo SET bar = 1", false},
		{"DELETE", "DELETE FROM foo", false},
		{"TRUNCATE", "TRUNCATE foo", false},
		{"GRANT", "GRANT SELECT ON foo TO bar", false},
		{"REVOKE", "REVOKE SELECT ON foo FROM bar", false},
		{"VACUUM", "VACUUM ANALYZE foo", false},
		{"REINDEX", "REINDEX TABLE foo", false},
		{"CLUSTER", "CLUSTER foo", false},
		{"REFRESH", "REFRESH MATERIALIZED VIEW mv", false},
		{"COMMENT", "COMMENT ON TABLE foo IS 'bar'", false},
		{"SET", "SET work_mem = '256MB'", false},
		{"ALTER with comment", "-- tune memory\nALTER SYSTEM SET work_mem = '16MB'", false},
		{"empty after stripping", "-- just a comment", false},
		{"writable CTE DELETE", "WITH deleted AS (DELETE FROM foo RETURNING *) SELECT * FROM deleted", false},
		{"writable CTE INSERT", "WITH ins AS (INSERT INTO foo VALUES (1) RETURNING *) SELECT * FROM ins", false},
		{"writable CTE UPDATE", "WITH upd AS (UPDATE foo SET bar = 1 RETURNING *) SELECT * FROM upd", false},
		{"CTE with updated_at column", "WITH cte AS (SELECT updated_at FROM foo) SELECT * FROM cte", true},
		{"CTE with delete_flag column", "WITH cte AS (SELECT delete_flag FROM foo) SELECT * FROM cte", true},
		{"CTE pure SELECT", "WITH cte AS (SELECT 1) SELECT * FROM cte", true},
		{"SELECT with block comment", "/* get data */ SELECT 1", true},
		{"ALTER with block comment", "/* tune */ ALTER SYSTEM SET work_mem = '16MB'", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isReadOnlyStatement(tt.input)
			if result != tt.readOnly {
				t.Errorf("isReadOnlyStatement(%q) = %v, want %v",
					tt.input, result, tt.readOnly)
			}
		})
	}
}

func TestWriteStatements_RequireConfirmation(t *testing.T) {
	handler := newTestConnectionHandlerWithRBAC()

	// Send an ALTER SYSTEM without confirmed flag; the handler should
	// return a confirmation response before touching the datastore.
	body := `{"query": "ALTER SYSTEM SET work_mem = '16MB'"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connections/1/query",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.executeQuery(rec, req, 1)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}

	var resp multiQueryResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !resp.RequiresConfirmation {
		t.Error("Expected requires_confirmation to be true")
	}
	if len(resp.WriteStatements) != 1 {
		t.Errorf("Expected 1 write statement, got %d", len(resp.WriteStatements))
	}
	if resp.ConfirmationMessage == "" {
		t.Error("Expected a non-empty confirmation message")
	}
}

func TestMixedStatements_RequireConfirmation(t *testing.T) {
	handler := newTestConnectionHandlerWithRBAC()

	body := `{"query": "SELECT 1; ALTER SYSTEM SET work_mem = '16MB'; SELECT 2"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connections/1/query",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.executeQuery(rec, req, 1)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}

	var resp multiQueryResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !resp.RequiresConfirmation {
		t.Error("Expected requires_confirmation to be true for mixed statements")
	}
	if len(resp.WriteStatements) != 1 {
		t.Errorf("Expected 1 write statement, got %d", len(resp.WriteStatements))
	}
	if resp.WriteStatements[0] != "ALTER SYSTEM SET work_mem = '16MB'" {
		t.Errorf("Expected ALTER SYSTEM statement, got %q", resp.WriteStatements[0])
	}
}

func TestReadOnlyStatements_NoConfirmation(t *testing.T) {
	handler := newTestConnectionHandlerWithRBAC()

	// Pure read-only query should NOT trigger confirmation; it should
	// proceed to the datastore path (which panics with nil datastore).
	body := `{"query": "SELECT 1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connections/1/query",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	panicked := false
	defer func() {
		if r := recover(); r != nil {
			panicked = true
		}
		if !panicked && rec.Code == http.StatusOK {
			// If no panic and we got a 200, check that there is no
			// requires_confirmation in the response.
			var resp multiQueryResponse
			if err := json.NewDecoder(rec.Body).Decode(&resp); err == nil {
				if resp.RequiresConfirmation {
					t.Error("Read-only query should not require confirmation")
				}
			}
		}
	}()

	handler.executeQuery(rec, req, 1)
}

func TestConfirmationResponse_JSONSerialization(t *testing.T) {
	resp := multiQueryResponse{
		RequiresConfirmation: true,
		WriteStatements:      []string{"ALTER SYSTEM SET work_mem = '16MB'"},
		ConfirmationMessage:  "This request contains 1 write statement(s) that will modify the database. Please confirm to proceed.",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal confirmation response: %v", err)
	}

	var decoded multiQueryResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal confirmation response: %v", err)
	}

	if !decoded.RequiresConfirmation {
		t.Error("Expected requires_confirmation to be true")
	}
	if len(decoded.WriteStatements) != 1 {
		t.Fatalf("Expected 1 write statement, got %d", len(decoded.WriteStatements))
	}
	if decoded.WriteStatements[0] != "ALTER SYSTEM SET work_mem = '16MB'" {
		t.Errorf("Unexpected write statement: %q", decoded.WriteStatements[0])
	}
	if decoded.ConfirmationMessage == "" {
		t.Error("Expected non-empty confirmation message")
	}
	// Results and TotalStatements should be zero-value / omitted
	if len(decoded.Results) != 0 {
		t.Errorf("Expected no results in confirmation response, got %d", len(decoded.Results))
	}
	if decoded.TotalStatements != 0 {
		t.Errorf("Expected total_statements 0 in confirmation response, got %d", decoded.TotalStatements)
	}
}

func TestLimitInjectionOnlyForSelectQueries(t *testing.T) {
	// Verify that non-SELECT statements do not get LIMIT injected.
	// We test this indirectly by checking that splitStatements +
	// stripLeadingComments correctly identifies non-SELECT queries.
	tests := []struct {
		name     string
		query    string
		isSelect bool
	}{
		{"plain SELECT", "SELECT 1", true},
		{"SELECT with comment", "-- comment\nSELECT 1", true},
		{"WITH CTE", "WITH cte AS (SELECT 1) SELECT * FROM cte", true},
		{"SHOW statement", "SHOW shared_buffers", false},
		{"EXPLAIN statement", "EXPLAIN SELECT 1", false},
		{"SET statement", "SET work_mem = '256MB'", false},
		{"SHOW with comment", "-- config check\nSHOW work_mem", false},
		{"select lowercase", "select 1", true},
		{"with lowercase", "with x as (select 1) select * from x", true},
		{"explain lowercase", "explain select 1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmtBody := stripLeadingComments(tt.query)
			upperBody := strings.ToUpper(stmtBody)
			isSelect := strings.HasPrefix(upperBody, "SELECT") ||
				strings.HasPrefix(upperBody, "WITH")
			if isSelect != tt.isSelect {
				t.Errorf("query %q: isSelect = %v, want %v",
					tt.query, isSelect, tt.isSelect)
			}
		})
	}
}

func TestQueryConstants(t *testing.T) {
	if defaultRowLimit != 500 {
		t.Errorf("Expected defaultRowLimit=500, got %d", defaultRowLimit)
	}
	if maxRowLimit != 1000 {
		t.Errorf("Expected maxRowLimit=1000, got %d", maxRowLimit)
	}
	if queryTimeout != 30*time.Second {
		t.Errorf("Expected queryTimeout=30s, got %v", queryTimeout)
	}
}

func TestScanDollarTag(t *testing.T) {
	tests := []struct {
		name     string
		sql      string
		i        int
		expected string
	}{
		{"empty_tag", "$$body$$", 0, "$$"},
		{"named_tag", "$tag$body$tag$", 0, "$tag$"},
		{"underscore_tag", "$_t1$body$_t1$", 0, "$_t1$"},
		{"not_a_tag_digit_start", "$1abc$", 0, ""},
		{"not_a_tag_no_close", "$abc", 0, ""},
		{"mid_string", "SELECT $fn$code$fn$", 7, "$fn$"},
		{"single_dollar", "$", 0, ""},
		{"out_of_bounds", "abc", 5, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scanDollarTag(tt.sql, tt.i)
			if got != tt.expected {
				t.Errorf("scanDollarTag(%q, %d) = %q, want %q",
					tt.sql, tt.i, got, tt.expected)
			}
		})
	}
}

func TestHasOnlyComments(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"empty", "", true},
		{"whitespace", "   \n\t  ", true},
		{"line comment", "-- hello", true},
		{"block comment", "/* hello */", true},
		{"nested block", "/* outer /* inner */ */", true},
		{"mixed", "-- line\n/* block */\n  ", true},
		{"has sql", "-- comment\nSELECT 1", false},
		{"just sql", "SELECT 1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasOnlyComments(tt.input)
			if got != tt.expected {
				t.Errorf("hasOnlyComments(%q) = %v, want %v",
					tt.input, got, tt.expected)
			}
		})
	}
}

func TestSafeQueryError(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		err      error
		expected string
	}{
		{
			"PgError with detail",
			"Query error",
			&pgconn.PgError{
				Message: "relation does not exist",
				Detail:  "table \"foo\" not found",
			},
			"Query error: relation does not exist (table \"foo\" not found)",
		},
		{
			"PgError without detail",
			"Query error",
			&pgconn.PgError{
				Message: "syntax error at or near SELECT",
			},
			"Query error: syntax error at or near SELECT",
		},
		{
			"context.DeadlineExceeded",
			"Query error",
			context.DeadlineExceeded,
			"Query error: query timed out",
		},
		{
			"wrapped DeadlineExceeded",
			"Query error",
			fmt.Errorf("wrapped: %w", context.DeadlineExceeded),
			"Query error: query timed out",
		},
		{
			"context.Canceled",
			"Query error",
			context.Canceled,
			"Query error: query was canceled",
		},
		{
			"wrapped Canceled",
			"Query error",
			fmt.Errorf("wrapped: %w", context.Canceled),
			"Query error: query was canceled",
		},
		{
			"parameter placeholder error",
			"Query error",
			fmt.Errorf("expected 1 arguments, got 0"),
			"Query error: query contains parameter placeholders ($1, $2, ...) " +
				"that require values; these cannot be executed directly",
		},
		{
			"connection error",
			"Query error",
			fmt.Errorf("dial tcp 127.0.0.1:5432: connection refused"),
			"Query error: connection error; the database may be unreachable",
		},
		{
			"generic unknown error",
			"Query error",
			fmt.Errorf("something unexpected happened"),
			"Query error: an internal error occurred",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := safeQueryError(tt.prefix, tt.err)
			if got != tt.expected {
				t.Errorf("safeQueryError(%q, %v) = %q, want %q",
					tt.prefix, tt.err, got, tt.expected)
			}
		})
	}
}

func TestIsConnectionError(t *testing.T) {
	tests := []struct {
		name     string
		msg      string
		expected bool
	}{
		{"connection refused", "connection refused", true},
		{"connection reset by peer", "connection reset by peer", true},
		{"no such host", "no such host", true},
		{"i/o timeout", "i/o timeout", true},
		{"broken pipe", "broken pipe", true},
		{"case insensitive", "Connection Refused", true},
		{"random error", "some random error", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isConnectionError(tt.msg)
			if got != tt.expected {
				t.Errorf("isConnectionError(%q) = %v, want %v",
					tt.msg, got, tt.expected)
			}
		})
	}
}

func TestIsParameterPlaceholderError(t *testing.T) {
	tests := []struct {
		name     string
		msg      string
		expected bool
	}{
		{"expected 1 arguments got 0", "expected 1 arguments, got 0", true},
		{"expected 3 arguments got 0", "expected 3 arguments, got 0", true},
		{"case insensitive", "EXPECTED 1 ARGUMENTS", true},
		{"arguments got substring", "some error with arguments, got nothing", true},
		{"random error", "some random error", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isParameterPlaceholderError(tt.msg)
			if got != tt.expected {
				t.Errorf("isParameterPlaceholderError(%q) = %v, want %v",
					tt.msg, got, tt.expected)
			}
		})
	}
}
