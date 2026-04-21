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
	"time"
)

// ---------------------------------------------------------------------------
// get_alert_history tool definition tests
// ---------------------------------------------------------------------------

func TestGetAlertHistoryToolDefinition(t *testing.T) {
	tool := GetAlertHistoryTool(nil, nil, nil)

	if tool.Definition.Name != "get_alert_history" {
		t.Errorf("expected tool name 'get_alert_history', got %q", tool.Definition.Name)
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
	for _, prop := range []string{"connection_id", "status", "rule_id", "metric_name", "time_start", "limit", "offset"} {
		if _, ok := tool.Definition.InputSchema.Properties[prop]; !ok {
			t.Errorf("expected property %q in input schema", prop)
		}
	}
}

// ---------------------------------------------------------------------------
// get_alert_history nil pool test
// ---------------------------------------------------------------------------

func TestGetAlertHistoryNilPool(t *testing.T) {
	tool := GetAlertHistoryTool(nil, nil, nil)

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
// get_alert_history parameter validation tests
// ---------------------------------------------------------------------------

func TestGetAlertHistoryInvalidStatus(t *testing.T) {
	tool := GetAlertHistoryTool(nil, nil, nil)

	args := map[string]any{
		"status": "invalid_status",
	}

	response, err := tool.Handler(args)
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	// Should fail on nil pool first
	if !response.IsError {
		t.Error("expected error response")
	}
}

func TestGetAlertHistoryValidStatusValues(t *testing.T) {
	// Verify the schema has the correct enum values
	tool := GetAlertHistoryTool(nil, nil, nil)

	statusProp, ok := tool.Definition.InputSchema.Properties["status"].(map[string]any)
	if !ok {
		t.Fatal("expected 'status' property to be a map")
	}

	enumValues, hasEnum := statusProp["enum"]
	if !hasEnum {
		t.Error("expected 'status' to have enum values")
	}

	expectedValues := []string{"active", "cleared", "acknowledged", "all"}
	enumSlice, ok := enumValues.([]string)
	if !ok {
		t.Fatal("expected enum values to be a string slice")
	}

	if len(enumSlice) != len(expectedValues) {
		t.Errorf("expected %d enum values, got %d", len(expectedValues), len(enumSlice))
	}

	for _, expected := range expectedValues {
		found := false
		for _, actual := range enumSlice {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected enum value %q not found", expected)
		}
	}
}

// ---------------------------------------------------------------------------
// helper function tests
// ---------------------------------------------------------------------------

func TestFormatOptionalFloat(t *testing.T) {
	tests := []struct {
		name  string
		input *float64
		want  string
	}{
		{
			name:  "nil",
			input: nil,
			want:  "",
		},
		{
			name:  "zero",
			input: ptrFloat(0),
			want:  "0",
		},
		{
			name:  "positive",
			input: ptrFloat(42.5),
			want:  "42.5",
		},
		{
			name:  "negative",
			input: ptrFloat(-10.25),
			want:  "-10.25",
		},
		{
			name:  "integer value",
			input: ptrFloat(100),
			want:  "100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatOptionalFloat(tt.input)
			if got != tt.want {
				t.Errorf("formatOptionalFloat() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatOptionalTime(t *testing.T) {
	fixedTime := time.Date(2024, 6, 15, 14, 30, 45, 0, time.UTC)

	tests := []struct {
		name  string
		input *time.Time
		want  string
	}{
		{
			name:  "nil",
			input: nil,
			want:  "",
		},
		{
			name:  "valid time",
			input: &fixedTime,
			want:  "2024-06-15T14:30:45Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatOptionalTime(tt.input)
			if got != tt.want {
				t.Errorf("formatOptionalTime() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatOptionalBool(t *testing.T) {
	tests := []struct {
		name  string
		input *bool
		want  string
	}{
		{
			name:  "nil",
			input: nil,
			want:  "",
		},
		{
			name:  "true",
			input: ptrBool(true),
			want:  "true",
		},
		{
			name:  "false",
			input: ptrBool(false),
			want:  "false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatOptionalBool(tt.input)
			if got != tt.want {
				t.Errorf("formatOptionalBool() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatOptionalString(t *testing.T) {
	tests := []struct {
		name  string
		input *string
		want  string
	}{
		{
			name:  "nil",
			input: nil,
			want:  "",
		},
		{
			name:  "empty string",
			input: ptrString(""),
			want:  "",
		},
		{
			name:  "normal string",
			input: ptrString("hello"),
			want:  "hello",
		},
		{
			name:  "string with spaces",
			input: ptrString("hello world"),
			want:  "hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatOptionalString(tt.input)
			if got != tt.want {
				t.Errorf("formatOptionalString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatOptionalStringEscaped(t *testing.T) {
	tests := []struct {
		name  string
		input *string
		want  string
	}{
		{
			name:  "nil",
			input: nil,
			want:  "",
		},
		{
			name:  "normal string",
			input: ptrString("hello"),
			want:  "hello",
		},
		{
			name:  "string with tab",
			input: ptrString("hello\tworld"),
			want:  "hello\\tworld", // TSV format escapes tabs as literal \t
		},
		{
			name:  "string with newline",
			input: ptrString("hello\nworld"),
			want:  "hello\\nworld", // TSV format escapes newlines as literal \n
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatOptionalStringEscaped(tt.input)
			if got != tt.want {
				t.Errorf("formatOptionalStringEscaped() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// helper functions for tests
// ---------------------------------------------------------------------------

func ptrFloat(f float64) *float64 {
	return &f
}

func ptrBool(b bool) *bool {
	return &b
}

func ptrString(s string) *string {
	return &s
}
