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
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// count_rows tool definition tests
// ---------------------------------------------------------------------------

func TestCountRowsToolDefinition(t *testing.T) {
	tool := CountRowsTool(nil, nil)

	if tool.Definition.Name != "count_rows" {
		t.Errorf("expected tool name 'count_rows', got %q", tool.Definition.Name)
	}

	if tool.Definition.Description == "" {
		t.Error("expected non-empty description")
	}

	if tool.Definition.CompactDescription == "" {
		t.Error("expected non-empty compact description")
	}

	// Verify required parameters
	required := tool.Definition.InputSchema.Required
	if len(required) != 1 {
		t.Fatalf("expected 1 required parameter, got %d", len(required))
	}
	if required[0] != "table" {
		t.Errorf("expected required parameter 'table', got %q", required[0])
	}

	// Verify all expected properties exist
	for _, prop := range []string{"connection_id", "database_name", "table", "schema", "where"} {
		if _, ok := tool.Definition.InputSchema.Properties[prop]; !ok {
			t.Errorf("expected property %q in input schema", prop)
		}
	}
}

// ---------------------------------------------------------------------------
// count_rows parameter validation tests
// ---------------------------------------------------------------------------

func TestCountRowsMissingTable(t *testing.T) {
	tool := CountRowsTool(nil, nil)

	tests := []struct {
		name string
		args map[string]any
	}{
		{
			name: "table key absent",
			args: map[string]any{},
		},
		{
			name: "table is empty string",
			args: map[string]any{
				"table": "",
			},
		},
		{
			name: "table is wrong type",
			args: map[string]any{
				"table": 123,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := tool.Handler(tt.args)
			if err != nil {
				t.Fatalf("handler returned unexpected error: %v", err)
			}
			if !response.IsError {
				t.Error("expected an error response for missing/invalid table")
			}
			if len(response.Content) == 0 {
				t.Fatal("expected error message in response content")
			}
			if !strings.Contains(response.Content[0].Text, "table") {
				t.Errorf("error message should mention 'table', got: %s",
					response.Content[0].Text)
			}
		})
	}
}

func TestCountRowsInvalidTableName(t *testing.T) {
	tool := CountRowsTool(nil, nil)

	tests := []struct {
		name  string
		table string
	}{
		{"SQL injection", "users; DROP TABLE users;--"},
		{"starts with number", "1table"},
		{"contains space", "user table"},
		{"too long", strings.Repeat("a", 64)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]any{
				"table": tt.table,
			}
			response, err := tool.Handler(args)
			if err != nil {
				t.Fatalf("handler returned unexpected error: %v", err)
			}
			if !response.IsError {
				t.Error("expected error response for invalid table name")
			}
			if len(response.Content) == 0 {
				t.Fatal("expected error message in response content")
			}
			if !strings.Contains(response.Content[0].Text, "Invalid table name") {
				t.Errorf("expected 'Invalid table name' error, got: %s",
					response.Content[0].Text)
			}
		})
	}
}

func TestCountRowsInvalidSchemaName(t *testing.T) {
	tool := CountRowsTool(nil, nil)

	tests := []struct {
		name   string
		schema string
	}{
		{"SQL injection", "public; DROP TABLE users;--"},
		{"starts with number", "1schema"},
		{"contains space", "my schema"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]any{
				"table":  "users",
				"schema": tt.schema,
			}
			response, err := tool.Handler(args)
			if err != nil {
				t.Fatalf("handler returned unexpected error: %v", err)
			}
			if !response.IsError {
				t.Error("expected error response for invalid schema name")
			}
			if len(response.Content) == 0 {
				t.Fatal("expected error message in response content")
			}
			if !strings.Contains(response.Content[0].Text, "Invalid schema name") {
				t.Errorf("expected 'Invalid schema name' error, got: %s",
					response.Content[0].Text)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// validateWhereClause tests
// ---------------------------------------------------------------------------

func TestValidateWhereClause(t *testing.T) {
	tests := []struct {
		name      string
		clause    string
		wantError bool
		errorText string
	}{
		// Valid clauses
		{
			name:      "simple equality",
			clause:    "status = 'active'",
			wantError: false,
		},
		{
			name:      "multiple conditions",
			clause:    "status = 'active' AND created_at > '2024-01-01'",
			wantError: false,
		},
		{
			name:      "numeric comparison",
			clause:    "count > 100 AND count < 1000",
			wantError: false,
		},
		{
			name:      "LIKE clause",
			clause:    "name LIKE '%test%'",
			wantError: false,
		},
		{
			name:      "IN clause",
			clause:    "id IN (1, 2, 3)",
			wantError: false,
		},
		{
			name:      "IS NULL",
			clause:    "deleted_at IS NULL",
			wantError: false,
		},
		{
			name:      "BETWEEN",
			clause:    "age BETWEEN 18 AND 65",
			wantError: false,
		},

		// Dangerous patterns - denial of service
		{
			name:      "pg_sleep",
			clause:    "1=1 AND pg_sleep(10)=''",
			wantError: true,
			errorText: "pg_sleep",
		},
		{
			name:      "pg_sleep uppercase",
			clause:    "1=1 AND PG_SLEEP(10)=''",
			wantError: true,
			errorText: "pg_sleep",
		},
		{
			name:      "generate_series",
			clause:    "id IN (SELECT * FROM generate_series(1, 1000000000))",
			wantError: true,
			errorText: "generate_series",
		},

		// Dangerous patterns - backend control
		{
			name:      "pg_cancel_backend",
			clause:    "pg_cancel_backend(123)",
			wantError: true,
			errorText: "pg_cancel_backend",
		},
		{
			name:      "pg_terminate_backend",
			clause:    "pg_terminate_backend(123)",
			wantError: true,
			errorText: "pg_terminate_backend",
		},

		// Dangerous patterns - external access
		{
			name:      "dblink",
			clause:    "dblink('host=evil.com', 'SELECT')",
			wantError: true,
			errorText: "dblink",
		},

		// Dangerous patterns - file system
		{
			name:      "copy with space",
			clause:    "copy test FROM '/etc/passwd'",
			wantError: true,
			errorText: "copy ",
		},
		{
			name:      "copy with paren",
			clause:    "copy(SELECT * FROM users)",
			wantError: true,
			errorText: "copy(",
		},
		{
			name:      "pg_read_file",
			clause:    "pg_read_file('/etc/passwd')",
			wantError: true,
			errorText: "pg_read_file",
		},
		{
			name:      "pg_read_binary_file",
			clause:    "pg_read_binary_file('/etc/passwd')",
			wantError: true,
			errorText: "pg_read_binary_file",
		},
		{
			name:      "pg_ls_dir",
			clause:    "pg_ls_dir('/')",
			wantError: true,
			errorText: "pg_ls_dir",
		},
		{
			name:      "pg_stat_file",
			clause:    "pg_stat_file('/etc/passwd')",
			wantError: true,
			errorText: "pg_stat_file",
		},

		// Dangerous patterns - large objects
		{
			name:      "lo_import",
			clause:    "lo_import('/etc/passwd')",
			wantError: true,
			errorText: "lo_import",
		},
		{
			name:      "lo_export",
			clause:    "lo_export(12345, '/tmp/out')",
			wantError: true,
			errorText: "lo_export",
		},

		// Dangerous patterns - character obfuscation
		{
			name:      "chr function",
			clause:    "chr(59)||chr(68)",
			wantError: true,
			errorText: "chr(",
		},
		{
			name:      "convert_from",
			clause:    "convert_from(decode('test', 'base64'), 'UTF8')",
			wantError: true,
			errorText: "convert_from(",
		},

		// Dangerous patterns - lock contention
		{
			name:      "pg_advisory_lock",
			clause:    "pg_advisory_lock(1)",
			wantError: true,
			errorText: "pg_advisory_lock",
		},

		// Dangerous patterns - configuration disclosure
		{
			name:      "current_setting",
			clause:    "current_setting('config_file')",
			wantError: true,
			errorText: "current_setting",
		},

		// Injection patterns
		{
			name:      "semicolon",
			clause:    "id = 1; DROP TABLE users",
			wantError: true,
			errorText: ";",
		},
		{
			name:      "double dash comment",
			clause:    "id = 1 -- comment",
			wantError: true,
			errorText: "--",
		},
		{
			name:      "block comment",
			clause:    "id = 1 /* comment */",
			wantError: true,
			errorText: "/*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWhereClause(tt.clause)
			if (err != nil) != tt.wantError {
				t.Errorf("validateWhereClause(%q) error = %v, wantError %v",
					tt.clause, err, tt.wantError)
			}
			if err != nil && tt.errorText != "" {
				if !strings.Contains(err.Error(), tt.errorText) {
					t.Errorf("error message should contain %q, got: %s",
						tt.errorText, err.Error())
				}
			}
		})
	}
}

func TestCountRowsInvalidWhereClause(t *testing.T) {
	tool := CountRowsTool(nil, nil)

	tests := []struct {
		name  string
		where string
	}{
		{"semicolon injection", "id = 1; DROP TABLE users"},
		{"pg_sleep attack", "1=1 AND pg_sleep(10)=''"},
		{"comment injection", "id = 1 -- ignore rest"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]any{
				"table": "users",
				"where": tt.where,
			}
			response, err := tool.Handler(args)
			if err != nil {
				t.Fatalf("handler returned unexpected error: %v", err)
			}
			if !response.IsError {
				t.Error("expected error response for invalid WHERE clause")
			}
			if len(response.Content) == 0 {
				t.Fatal("expected error message in response content")
			}
			if !strings.Contains(response.Content[0].Text, "Invalid WHERE clause") {
				t.Errorf("expected 'Invalid WHERE clause' error, got: %s",
					response.Content[0].Text)
			}
		})
	}
}

func TestCountRowsValidTableAndSchemaPassValidation(t *testing.T) {
	// Even without a resolver, valid table/schema names should pass
	// initial validation. The resolver error will come later.
	tool := CountRowsTool(nil, nil)

	args := map[string]any{
		"table":  "users",
		"schema": "public",
	}

	response, err := tool.Handler(args)
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}

	// Without a resolver, we expect a different error (not about table/schema)
	if response.IsError {
		if len(response.Content) == 0 {
			t.Fatal("expected error message in response content")
		}
		text := response.Content[0].Text
		if strings.Contains(text, "Invalid table name") ||
			strings.Contains(text, "Invalid schema name") {
			t.Errorf("valid table/schema should pass validation: %s", text)
		}
	}
}
