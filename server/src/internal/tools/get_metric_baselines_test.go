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
// get_metric_baselines tool definition tests
// ---------------------------------------------------------------------------

func TestGetMetricBaselinesToolDefinition(t *testing.T) {
	tool := GetMetricBaselinesTool(nil, nil, nil)

	if tool.Definition.Name != "get_metric_baselines" {
		t.Errorf("expected tool name 'get_metric_baselines', got %q", tool.Definition.Name)
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
	for _, prop := range []string{"connection_id", "metric_name"} {
		if _, ok := tool.Definition.InputSchema.Properties[prop]; !ok {
			t.Errorf("expected property %q in input schema", prop)
		}
	}
}

// ---------------------------------------------------------------------------
// get_metric_baselines nil pool test
// ---------------------------------------------------------------------------

func TestGetMetricBaselinesNilPool(t *testing.T) {
	tool := GetMetricBaselinesTool(nil, nil, nil)

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
// get_metric_baselines parameter validation tests
// ---------------------------------------------------------------------------

func TestGetMetricBaselinesInvalidConnectionID(t *testing.T) {
	tool := GetMetricBaselinesTool(nil, nil, nil)

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

func TestGetMetricBaselinesWithMetricName(t *testing.T) {
	tool := GetMetricBaselinesTool(nil, nil, nil)

	args := map[string]any{
		"metric_name": "cpu_usage",
	}

	response, err := tool.Handler(args)
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	// Should fail on nil pool
	if !response.IsError {
		t.Error("expected error response")
	}
	if !strings.Contains(response.Content[0].Text, "Datastore not configured") {
		t.Errorf("expected 'Datastore not configured' error, got: %s",
			response.Content[0].Text)
	}
}

// ---------------------------------------------------------------------------
// formatOptionalInt helper tests
// ---------------------------------------------------------------------------

func TestFormatOptionalInt(t *testing.T) {
	tests := []struct {
		name  string
		input *int
		want  string
	}{
		{
			name:  "nil",
			input: nil,
			want:  "",
		},
		{
			name:  "zero",
			input: ptrInt(0),
			want:  "0",
		},
		{
			name:  "positive",
			input: ptrInt(42),
			want:  "42",
		},
		{
			name:  "negative",
			input: ptrInt(-10),
			want:  "-10",
		},
		{
			name:  "day of week",
			input: ptrInt(3),
			want:  "3",
		},
		{
			name:  "hour of day",
			input: ptrInt(14),
			want:  "14",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatOptionalInt(tt.input)
			if got != tt.want {
				t.Errorf("formatOptionalInt() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// helper functions for tests
// ---------------------------------------------------------------------------

func ptrInt(i int) *int {
	return &i
}
