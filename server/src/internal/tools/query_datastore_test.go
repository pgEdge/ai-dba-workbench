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
// query_datastore tool definition tests
// ---------------------------------------------------------------------------

func TestQueryDatastoreToolDefinition(t *testing.T) {
	tool := QueryDatastoreTool(nil)

	if tool.Definition.Name != "query_datastore" {
		t.Errorf("expected tool name 'query_datastore', got %q", tool.Definition.Name)
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
	if required[0] != "query" {
		t.Errorf("expected required parameter 'query', got %q", required[0])
	}

	// Verify all expected properties exist
	for _, prop := range []string{"query", "limit", "offset"} {
		if _, ok := tool.Definition.InputSchema.Properties[prop]; !ok {
			t.Errorf("expected property %q in input schema", prop)
		}
	}
}

// ---------------------------------------------------------------------------
// query_datastore nil pool test
// ---------------------------------------------------------------------------

func TestQueryDatastoreNilPool(t *testing.T) {
	tool := QueryDatastoreTool(nil)

	args := map[string]any{
		"query": "SELECT 1",
	}

	response, err := tool.Handler(args)
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
// query_datastore parameter validation tests
// ---------------------------------------------------------------------------

func TestQueryDatastoreMissingQuery(t *testing.T) {
	tool := QueryDatastoreTool(nil)

	tests := []struct {
		name string
		args map[string]any
	}{
		{
			name: "query key absent",
			args: map[string]any{},
		},
		{
			name: "query is empty string",
			args: map[string]any{
				"query": "",
			},
		},
		{
			name: "query is whitespace only",
			args: map[string]any{
				"query": "   \t\n   ",
			},
		},
		{
			name: "query is wrong type",
			args: map[string]any{
				"query": 123,
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
				t.Error("expected an error response for missing/invalid query")
			}
			if len(response.Content) == 0 {
				t.Fatal("expected error message in response content")
			}
			if !strings.Contains(response.Content[0].Text, "query") {
				t.Errorf("error message should mention 'query', got: %s",
					response.Content[0].Text)
			}
		})
	}
}

func TestQueryDatastoreDefaultValues(t *testing.T) {
	tool := QueryDatastoreTool(nil)

	// Verify limit default
	limitProp, ok := tool.Definition.InputSchema.Properties["limit"].(map[string]any)
	if !ok {
		t.Fatal("expected 'limit' property to be a map")
	}
	if limitProp["default"] != 100 {
		t.Errorf("expected 'limit' default to be 100, got %v", limitProp["default"])
	}
	if limitProp["minimum"] != 1 {
		t.Errorf("expected 'limit' minimum to be 1, got %v", limitProp["minimum"])
	}
	if limitProp["maximum"] != 1000 {
		t.Errorf("expected 'limit' maximum to be 1000, got %v", limitProp["maximum"])
	}

	// Verify offset default
	offsetProp, ok := tool.Definition.InputSchema.Properties["offset"].(map[string]any)
	if !ok {
		t.Fatal("expected 'offset' property to be a map")
	}
	if offsetProp["default"] != 0 {
		t.Errorf("expected 'offset' default to be 0, got %v", offsetProp["default"])
	}
	if offsetProp["minimum"] != 0 {
		t.Errorf("expected 'offset' minimum to be 0, got %v", offsetProp["minimum"])
	}
}

func TestQueryDatastoreWithLimitAndOffset(t *testing.T) {
	tool := QueryDatastoreTool(nil)

	args := map[string]any{
		"query":  "SELECT * FROM connections",
		"limit":  50,
		"offset": 10,
	}

	response, err := tool.Handler(args)
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	// Should fail on nil pool, not on parameter validation
	if !response.IsError {
		t.Error("expected error response")
	}
	if len(response.Content) == 0 {
		t.Fatal("expected error message in response content")
	}
	if !strings.Contains(response.Content[0].Text, "Datastore not configured") {
		t.Errorf("expected 'Datastore not configured' error, got: %s",
			response.Content[0].Text)
	}
}
