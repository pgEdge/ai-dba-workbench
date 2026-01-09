/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package tools

import (
	"testing"
)

func TestDescribeProbe_NilPool(t *testing.T) {
	tool := DescribeProbeTool(nil)

	if tool.Definition.Name != "describe_probe" {
		t.Errorf("Expected tool name 'describe_probe', got '%s'", tool.Definition.Name)
	}

	// Test with nil pool - should return error
	resp, err := tool.Handler(map[string]interface{}{
		"probe_name": "pg_stat_database",
	})
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if !resp.IsError {
		t.Error("Expected error response when pool is nil")
	}
}

func TestDescribeProbe_MissingProbeName(t *testing.T) {
	tool := DescribeProbeTool(nil)

	// Test without probe_name parameter
	resp, err := tool.Handler(map[string]interface{}{})
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if !resp.IsError {
		t.Error("Expected error response when probe_name is missing")
	}
}

func TestDescribeProbe_InvalidProbeName(t *testing.T) {
	tool := DescribeProbeTool(nil)

	// Test with invalid probe name (SQL injection attempt)
	// Note: With nil pool, the pool check happens first, so we get
	// "Datastore not configured" instead of "Invalid probe name".
	// The isValidIdentifier function is tested separately below.
	resp, err := tool.Handler(map[string]interface{}{
		"probe_name": "pg_stat_database; DROP TABLE users;--",
	})
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// Should return error (either datastore not configured or invalid probe name)
	if !resp.IsError {
		t.Error("Expected error response for invalid probe name or nil pool")
	}
}

func TestIsValidIdentifier(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"pg_stat_database", true},
		{"PG_STAT_DATABASE", true},
		{"table1", true},
		{"_private", true},
		{"a", true},
		{"", false},
		{"123table", false},
		{"table-name", false},
		{"table name", false},
		{"table;drop", false},
		{"table'injection", false},
		{"select*from", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := isValidIdentifier(tt.input)
			if result != tt.expected {
				t.Errorf("isValidIdentifier(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsMetricColumn(t *testing.T) {
	tests := []struct {
		name     string
		dataType string
		expected bool
	}{
		// Dimension columns
		{"connection_id", "integer", false},
		{"collected_at", "timestamp with time zone", false},
		{"datname", "name", false},
		{"query", "text", false},
		{"client_addr", "inet", false},
		// Metric columns
		{"numbackends", "integer", true},
		{"xact_commit", "bigint", true},
		{"blks_hit", "bigint", true},
		{"temp_bytes", "numeric", true},
		{"active_time", "double precision", true},
		// Edge cases
		{"custom_column", "bigint", true},
		{"custom_text", "text", false},
	}

	for _, tt := range tests {
		t.Run(tt.name+"_"+tt.dataType, func(t *testing.T) {
			result := isMetricColumn(tt.name, tt.dataType)
			if result != tt.expected {
				t.Errorf("isMetricColumn(%q, %q) = %v, want %v", tt.name, tt.dataType, result, tt.expected)
			}
		})
	}
}
