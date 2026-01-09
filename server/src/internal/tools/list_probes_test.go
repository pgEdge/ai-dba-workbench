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

func TestListProbes_NilPool(t *testing.T) {
	tool := ListProbesTool(nil)

	if tool.Definition.Name != "list_probes" {
		t.Errorf("Expected tool name 'list_probes', got '%s'", tool.Definition.Name)
	}

	// Test with nil pool - should return error
	resp, err := tool.Handler(map[string]interface{}{})
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if !resp.IsError {
		t.Error("Expected error response when pool is nil")
	}

	// Check error message mentions datastore not configured
	if len(resp.Content) == 0 {
		t.Fatal("Expected content in response")
	}

	// Verify content contains expected error text
	found := false
	for _, item := range resp.Content {
		if item.Text != "" && contains(item.Text, "Datastore not configured") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected error message about datastore not configured")
	}
}

func TestDetermineProbeScope(t *testing.T) {
	tests := []struct {
		probeName string
		expected  string
	}{
		{"pg_stat_database", "database"},
		{"pg_stat_database_conflicts", "database"},
		{"pg_stat_user_tables", "database"},
		{"pg_stat_statements", "database"},
		{"pg_stat_activity", "server"},
		{"pg_stat_replication", "server"},
		{"pg_stat_bgwriter", "server"},
		{"pg_settings", "server"},
		{"pg_sys_cpu_info", "server"},
		{"unknown_probe", "server"},
	}

	for _, tt := range tests {
		t.Run(tt.probeName, func(t *testing.T) {
			result := determineProbeScope(tt.probeName)
			if result != tt.expected {
				t.Errorf("determineProbeScope(%s) = %s, want %s", tt.probeName, result, tt.expected)
			}
		})
	}
}

// contains checks if s contains substr
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
