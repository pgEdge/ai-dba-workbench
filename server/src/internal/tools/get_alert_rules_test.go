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
// get_alert_rules tool definition tests
// ---------------------------------------------------------------------------

func TestGetAlertRulesToolDefinition(t *testing.T) {
	tool := GetAlertRulesTool(nil, nil)

	if tool.Definition.Name != "get_alert_rules" {
		t.Errorf("expected tool name 'get_alert_rules', got %q", tool.Definition.Name)
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
	for _, prop := range []string{"connection_id", "category", "enabled_only"} {
		if _, ok := tool.Definition.InputSchema.Properties[prop]; !ok {
			t.Errorf("expected property %q in input schema", prop)
		}
	}
}

// ---------------------------------------------------------------------------
// get_alert_rules nil pool test
// ---------------------------------------------------------------------------

func TestGetAlertRulesNilPool(t *testing.T) {
	tool := GetAlertRulesTool(nil, nil)

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
// get_alert_rules parameter validation tests
// ---------------------------------------------------------------------------

func TestGetAlertRulesInvalidConnectionID(t *testing.T) {
	tool := GetAlertRulesTool(nil, nil)

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
		{
			name: "connection_id is boolean",
			args: map[string]any{
				"connection_id": true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The tool will fail on nil pool first, so we just verify
			// the tool can be created with various arg types
			response, err := tool.Handler(tt.args)
			if err != nil {
				t.Fatalf("handler returned unexpected error: %v", err)
			}
			// Should fail on nil pool, not on parameter validation
			if !response.IsError {
				t.Error("expected error response")
			}
		})
	}
}

func TestGetAlertRulesInvalidCategory(t *testing.T) {
	tool := GetAlertRulesTool(nil, nil)

	// Since pool is nil, we cannot test the category validation path
	// fully. This test verifies the tool handles the nil pool case.
	args := map[string]any{
		"category": "invalid_category",
	}

	response, err := tool.Handler(args)
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if !response.IsError {
		t.Error("expected error response")
	}
	// Should fail on nil pool first
	if !strings.Contains(response.Content[0].Text, "Datastore not configured") {
		t.Errorf("expected 'Datastore not configured' error, got: %s",
			response.Content[0].Text)
	}
}

func TestGetAlertRulesEnabledOnlyDefault(t *testing.T) {
	// Verify the schema indicates default is true
	tool := GetAlertRulesTool(nil, nil)

	enabledOnlyProp, ok := tool.Definition.InputSchema.Properties["enabled_only"].(map[string]any)
	if !ok {
		t.Fatal("expected 'enabled_only' property to be a map")
	}

	defaultVal, hasDefault := enabledOnlyProp["default"]
	if !hasDefault {
		t.Error("expected 'enabled_only' to have a default value")
	}
	if defaultVal != true {
		t.Errorf("expected 'enabled_only' default to be true, got %v", defaultVal)
	}
}
