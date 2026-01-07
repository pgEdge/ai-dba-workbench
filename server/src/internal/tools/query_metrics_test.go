/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package tools

import (
	"testing"
	"time"
)

func TestQueryMetrics_NilPool(t *testing.T) {
	tool := QueryMetricsTool(nil)

	if tool.Definition.Name != "query_metrics" {
		t.Errorf("Expected tool name 'query_metrics', got '%s'", tool.Definition.Name)
	}

	// Test with nil pool - should return error
	resp, err := tool.Handler(map[string]interface{}{
		"probe_name":    "pg_stat_database",
		"connection_id": float64(1),
	})
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if !resp.IsError {
		t.Error("Expected error response when pool is nil")
	}
}

func TestQueryMetrics_MissingParameters(t *testing.T) {
	tool := QueryMetricsTool(nil)

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "missing probe_name",
			args: map[string]interface{}{
				"connection_id": float64(1),
			},
		},
		{
			name: "missing connection_id",
			args: map[string]interface{}{
				"probe_name": "pg_stat_database",
			},
		},
		{
			name: "empty probe_name",
			args: map[string]interface{}{
				"probe_name":    "",
				"connection_id": float64(1),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := tool.Handler(tt.args)
			if err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}
			if !resp.IsError {
				t.Error("Expected error response for missing/invalid parameters")
			}
		})
	}
}

func TestParseRelativeDuration(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
		wantErr  bool
	}{
		{"1h", 1 * time.Hour, false},
		{"24h", 24 * time.Hour, false},
		{"30m", 30 * time.Minute, false},
		{"1d", 24 * time.Hour, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"30d", 30 * 24 * time.Hour, false},
		{"", 0, true},
		{"invalid", 0, true},
		{"0d", 0, true},
		{"-1d", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseRelativeDuration(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseRelativeDuration(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("parseRelativeDuration(%q) unexpected error: %v", tt.input, err)
				return
			}
			if result != tt.expected {
				t.Errorf("parseRelativeDuration(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseTimeArg(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"2024-01-15T10:00:00Z", false},
		{"2024-01-15T10:00:00", false},
		{"2024-01-15 10:00:00", false},
		{"2024-01-15", false},
		{"invalid", true},
		{"", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, err := parseTimeArg(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseTimeArg(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("parseTimeArg(%q) unexpected error: %v", tt.input, err)
			}
		})
	}
}

func TestParseTimeRange(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]interface{}
		wantErr bool
	}{
		{
			name:    "default values",
			args:    map[string]interface{}{},
			wantErr: false,
		},
		{
			name: "relative start",
			args: map[string]interface{}{
				"time_start": "24h",
			},
			wantErr: false,
		},
		{
			name: "absolute start",
			args: map[string]interface{}{
				"time_start": "2024-01-15T10:00:00Z",
			},
			wantErr: false,
		},
		{
			name: "both times",
			args: map[string]interface{}{
				"time_start": "2024-01-15T10:00:00Z",
				"time_end":   "2024-01-15T12:00:00Z",
			},
			wantErr: false,
		},
		{
			name: "start after end",
			args: map[string]interface{}{
				"time_start": "2024-01-15T12:00:00Z",
				"time_end":   "2024-01-15T10:00:00Z",
			},
			wantErr: true,
		},
		{
			name: "invalid start",
			args: map[string]interface{}{
				"time_start": "invalid",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, err := parseTimeRange(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseTimeRange() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("parseTimeRange() unexpected error: %v", err)
				return
			}
			if start.After(end) {
				t.Errorf("parseTimeRange() start %v is after end %v", start, end)
			}
		})
	}
}

func TestParseIntArg(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]interface{}
		key     string
		want    int
		wantErr bool
	}{
		{
			name:    "float64 value",
			args:    map[string]interface{}{"num": float64(42)},
			key:     "num",
			want:    42,
			wantErr: false,
		},
		{
			name:    "int value",
			args:    map[string]interface{}{"num": 42},
			key:     "num",
			want:    42,
			wantErr: false,
		},
		{
			name:    "int64 value",
			args:    map[string]interface{}{"num": int64(42)},
			key:     "num",
			want:    42,
			wantErr: false,
		},
		{
			name:    "missing key",
			args:    map[string]interface{}{},
			key:     "num",
			want:    0,
			wantErr: true,
		},
		{
			name:    "wrong type",
			args:    map[string]interface{}{"num": "42"},
			key:     "num",
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseIntArg(tt.args, tt.key)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseIntArg() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("parseIntArg() unexpected error: %v", err)
				return
			}
			if result != tt.want {
				t.Errorf("parseIntArg() = %v, want %v", result, tt.want)
			}
		})
	}
}

func TestFormatMetricValue(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected string
	}{
		{"nil value", nil, ""},
		{"integer", int64(42), "42"},
		{"float whole", float64(42.0), "42"},
		{"float decimal", float64(3.14159), "3.14159"},
		{"int", 100, "100"},
		{"string", "test", "test"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatMetricValue(tt.input)
			if result != tt.expected {
				t.Errorf("formatMetricValue(%v) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
