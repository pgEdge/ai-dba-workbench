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
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestSplitStatements(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want []string
	}{
		{
			name: "single statement without semicolon",
			sql:  "SELECT 1",
			want: []string{"SELECT 1"},
		},
		{
			name: "single statement with trailing semicolon",
			sql:  "SELECT 1;",
			want: []string{"SELECT 1"},
		},
		{
			name: "multiple simple statements",
			sql:  "SELECT 1; SELECT 2; SELECT 3",
			want: []string{"SELECT 1", "SELECT 2", "SELECT 3"},
		},
		{
			name: "semicolon inside single-quoted string",
			sql:  "SELECT 'a;b'",
			want: []string{"SELECT 'a;b'"},
		},
		{
			name: "escaped single quotes with semicolon",
			sql:  "SELECT 'it''s;here'",
			want: []string{"SELECT 'it''s;here'"},
		},
		{
			name: "semicolon inside double-quoted identifier",
			sql:  `SELECT "my;col" FROM t`,
			want: []string{`SELECT "my;col" FROM t`},
		},
		{
			name: "semicolon inside dollar-quoted string",
			sql:  "SELECT $$a;b$$",
			want: []string{"SELECT $$a;b$$"},
		},
		{
			name: "semicolon inside named dollar-quoted string",
			sql:  "SELECT $tag$a;b$tag$",
			want: []string{"SELECT $tag$a;b$tag$"},
		},
		{
			name: "semicolon inside line comment",
			sql:  "SELECT 1 -- a;b\n; SELECT 2",
			want: []string{"SELECT 1 -- a;b", "SELECT 2"},
		},
		{
			name: "semicolon inside block comment",
			sql:  "SELECT /* a;b */ 1",
			want: []string{"SELECT /* a;b */ 1"},
		},
		{
			name: "nested block comments with semicolons",
			sql:  "SELECT /* /* a;b */ */ 1",
			want: []string{"SELECT /* /* a;b */ */ 1"},
		},
		{
			name: "empty input",
			sql:  "",
			want: nil,
		},
		{
			name: "whitespace-only input",
			sql:  "   \t\n  ",
			want: nil,
		},
		{
			name: "multiple semicolons producing empty statements",
			sql:  ";;; SELECT 1 ;;; SELECT 2 ;;;",
			want: []string{"SELECT 1", "SELECT 2"},
		},
		{
			name: "mixed quoting styles",
			sql:  `SELECT 'a;b'; SELECT "c;d"; SELECT $$e;f$$`,
			want: []string{"SELECT 'a;b'", `SELECT "c;d"`, "SELECT $$e;f$$"},
		},
		{
			name: "dollar quote with body containing semicolons and newlines",
			sql:  "SELECT $fn$\nBEGIN\n  x := 1;\n  RETURN x;\nEND;\n$fn$",
			want: []string{"SELECT $fn$\nBEGIN\n  x := 1;\n  RETURN x;\nEND;\n$fn$"},
		},
		{
			name: "line comment at end without newline",
			sql:  "SELECT 1 -- comment;no newline",
			want: []string{"SELECT 1 -- comment;no newline"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitStatements(tt.sql)

			if len(got) != len(tt.want) {
				t.Fatalf("splitStatements() returned %d statements, want %d\ngot:  %q\nwant: %q",
					len(got), len(tt.want), got, tt.want)
			}

			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("statement[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseDollarTag(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		pos  int
		want string
	}{
		{
			name: "valid $$ tag",
			sql:  "$$hello$$",
			pos:  0,
			want: "$$",
		},
		{
			name: "valid named tag",
			sql:  "$tag$hello$tag$",
			pos:  0,
			want: "$tag$",
		},
		{
			name: "invalid not starting with dollar",
			sql:  "abc",
			pos:  0,
			want: "",
		},
		{
			name: "tag with digits",
			sql:  "$t1$hello$t1$",
			pos:  0,
			want: "$t1$",
		},
		{
			name: "tag starting with digit is invalid",
			sql:  "$1tag$hello$1tag$",
			pos:  0,
			want: "",
		},
		{
			name: "tag with underscore",
			sql:  "$_my_tag$content$_my_tag$",
			pos:  0,
			want: "$_my_tag$",
		},
		{
			name: "position in middle of string",
			sql:  "SELECT $fn$body$fn$",
			pos:  7,
			want: "$fn$",
		},
		{
			name: "position beyond string length",
			sql:  "ab",
			pos:  5,
			want: "",
		},
		{
			name: "dollar at end of string",
			sql:  "a$",
			pos:  1,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDollarTag(tt.sql, tt.pos)
			if got != tt.want {
				t.Errorf("parseDollarTag(%q, %d) = %q, want %q", tt.sql, tt.pos, got, tt.want)
			}
		})
	}
}

func TestIsMultipleStatementError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "pgconn.PgError with code 42601 and multiple commands message",
			err: &pgconn.PgError{
				Code:    "42601",
				Message: "cannot insert multiple commands into a prepared statement",
			},
			want: true,
		},
		{
			name: "generic error with multiple commands text",
			err:  errors.New("cannot insert multiple commands into a prepared statement"),
			want: true,
		},
		{
			name: "generic error mentioning multiple commands",
			err:  errors.New("multiple commands detected"),
			want: true,
		},
		{
			name: "unrelated error",
			err:  errors.New("connection refused"),
			want: false,
		},
		{
			name: "pgconn.PgError with code 42601 but different message",
			err: &pgconn.PgError{
				Code:    "42601",
				Message: "syntax error at or near SELECT",
			},
			want: false,
		},
		{
			name: "pgconn.PgError with different code",
			err: &pgconn.PgError{
				Code:    "42P01",
				Message: "relation does not exist",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isMultipleStatementError(tt.err)
			if got != tt.want {
				t.Errorf("isMultipleStatementError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTestQueryToolDefinition(t *testing.T) {
	tool := TestQueryTool(nil, nil)

	if tool.Definition.Name != "test_query" {
		t.Errorf("Tool name = %v, want test_query", tool.Definition.Name)
	}

	if tool.Definition.Description == "" {
		t.Error("Tool description is empty")
	}

	schema := tool.Definition.InputSchema
	if schema.Type != "object" {
		t.Errorf("InputSchema.Type = %v, want object", schema.Type)
	}

	if len(schema.Required) != 1 || schema.Required[0] != "query" {
		t.Errorf("Required parameters = %v, want [query]", schema.Required)
	}

	if _, exists := schema.Properties["query"]; !exists {
		t.Error("Missing property: query")
	}

	if tool.Handler == nil {
		t.Error("Handler is nil")
	}
}

func TestTestQueryValidation(t *testing.T) {
	tool := TestQueryTool(nil, nil)

	tests := []struct {
		name     string
		args     map[string]any
		errorMsg string
	}{
		{
			name:     "Missing query parameter",
			args:     map[string]any{},
			errorMsg: "query",
		},
		{
			name: "Empty query parameter",
			args: map[string]any{
				"query": "",
			},
			errorMsg: "non-empty string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := tool.Handler(tt.args)

			if err != nil {
				t.Fatalf("Handler returned unexpected Go error: %v", err)
			}

			if !response.IsError {
				t.Error("Expected error response, but got success")
			}

			if len(response.Content) == 0 {
				t.Fatal("Response has no content items")
			}

			if !strings.Contains(response.Content[0].Text, tt.errorMsg) {
				t.Errorf("Error message %q does not contain %q",
					response.Content[0].Text, tt.errorMsg)
			}
		})
	}
}
