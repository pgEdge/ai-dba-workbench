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
// get_blackouts tool definition tests
// ---------------------------------------------------------------------------

func TestGetBlackoutsToolDefinition(t *testing.T) {
	tool := GetBlackoutsTool(nil, nil, nil)

	if tool.Definition.Name != "get_blackouts" {
		t.Errorf("expected tool name 'get_blackouts', got %q", tool.Definition.Name)
	}

	if tool.Definition.Description == "" {
		t.Error("expected non-empty description")
	}

	if tool.Definition.CompactDescription == "" {
		t.Error("expected non-empty compact description")
	}

	// Verify no required parameters
	if len(tool.Definition.InputSchema.Required) != 0 {
		t.Errorf("expected 0 required parameters, got %d",
			len(tool.Definition.InputSchema.Required))
	}

	// Verify all expected properties exist
	for _, prop := range []string{"connection_id", "active_only", "include_schedules", "limit"} {
		if _, ok := tool.Definition.InputSchema.Properties[prop]; !ok {
			t.Errorf("expected property %q in input schema", prop)
		}
	}
}

// ---------------------------------------------------------------------------
// get_blackouts nil pool test
// ---------------------------------------------------------------------------

func TestGetBlackoutsNilPool(t *testing.T) {
	tool := GetBlackoutsTool(nil, nil, nil)

	response, err := tool.Handler(map[string]any{})
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if !response.IsError {
		t.Error("expected error response when pool is nil")
	}
	if len(response.Content) == 0 {
		t.Fatal("expected error message in response content")
	}
	if !strings.Contains(response.Content[0].Text, "Datastore not configured") {
		t.Errorf("expected 'Datastore not configured' error, got: %s",
			response.Content[0].Text)
	}
}

// ---------------------------------------------------------------------------
// get_blackouts parameter validation tests
// ---------------------------------------------------------------------------

func TestGetBlackoutsInvalidConnectionID(t *testing.T) {
	tool := GetBlackoutsTool(nil, nil, nil)

	tests := []struct {
		name string
		args map[string]any
	}{
		{
			name: "connection_id is string",
			args: map[string]any{
				"connection_id": "abc",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := tool.Handler(tt.args)
			if err != nil {
				t.Fatalf("handler returned unexpected error: %v", err)
			}
			// Should fail on nil pool first
			if !response.IsError {
				t.Error("expected error response")
			}
		})
	}
}

func TestGetBlackoutsDefaultValues(t *testing.T) {
	tool := GetBlackoutsTool(nil, nil, nil)

	// Verify active_only default
	activeOnlyProp, ok := tool.Definition.InputSchema.Properties["active_only"].(map[string]any)
	if !ok {
		t.Fatal("expected 'active_only' property to be a map")
	}
	if activeOnlyProp["default"] != false {
		t.Errorf("expected 'active_only' default to be false, got %v",
			activeOnlyProp["default"])
	}

	// Verify include_schedules default
	includeSchedProp, ok := tool.Definition.InputSchema.Properties["include_schedules"].(map[string]any)
	if !ok {
		t.Fatal("expected 'include_schedules' property to be a map")
	}
	if includeSchedProp["default"] != false {
		t.Errorf("expected 'include_schedules' default to be false, got %v",
			includeSchedProp["default"])
	}

	// Verify limit default
	limitProp, ok := tool.Definition.InputSchema.Properties["limit"].(map[string]any)
	if !ok {
		t.Fatal("expected 'limit' property to be a map")
	}
	if limitProp["default"] != 20 {
		t.Errorf("expected 'limit' default to be 20, got %v", limitProp["default"])
	}
}

// ---------------------------------------------------------------------------
// replaceColumnInFilter helper tests
// ---------------------------------------------------------------------------

func TestReplaceColumnInFilter(t *testing.T) {
	tests := []struct {
		name      string
		filter    string
		oldColumn string
		newColumn string
		want      string
	}{
		{
			name:      "simple replacement",
			filter:    "b.connection_id IN (1, 2, 3)",
			oldColumn: "b.connection_id",
			newColumn: "connections.id",
			want:      "connections.id IN (1, 2, 3)",
		},
		{
			name:      "replacement with equals",
			filter:    "b.connection_id = $1",
			oldColumn: "b.connection_id",
			newColumn: "cn.id",
			want:      "cn.id = $1",
		},
		{
			name:      "only replaces first occurrence",
			filter:    "b.connection_id IN (SELECT b.connection_id FROM t)",
			oldColumn: "b.connection_id",
			newColumn: "c.id",
			want:      "c.id IN (SELECT b.connection_id FROM t)",
		},
		{
			name:      "no match",
			filter:    "a.other_column = 1",
			oldColumn: "b.connection_id",
			newColumn: "c.id",
			want:      "a.other_column = 1",
		},
		{
			name:      "empty filter",
			filter:    "",
			oldColumn: "b.connection_id",
			newColumn: "c.id",
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := replaceColumnInFilter(tt.filter, tt.oldColumn, tt.newColumn)
			if got != tt.want {
				t.Errorf("replaceColumnInFilter() = %q, want %q", got, tt.want)
			}
		})
	}
}
