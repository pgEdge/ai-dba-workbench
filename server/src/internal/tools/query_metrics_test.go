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
	"math/big"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestQueryMetrics_NilPool(t *testing.T) {
	tool := QueryMetricsTool(nil, nil)

	if tool.Definition.Name != "query_metrics" {
		t.Errorf("Expected tool name 'query_metrics', got '%s'", tool.Definition.Name)
	}

	// Test with nil pool - should return error
	resp, err := tool.Handler(map[string]any{
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
	tool := QueryMetricsTool(nil, nil)

	tests := []struct {
		name string
		args map[string]any
	}{
		{
			name: "missing probe_name",
			args: map[string]any{
				"connection_id": float64(1),
			},
		},
		{
			name: "missing connection_id",
			args: map[string]any{
				"probe_name": "pg_stat_database",
			},
		},
		{
			name: "empty probe_name",
			args: map[string]any{
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
		args    map[string]any
		wantErr bool
	}{
		{
			name:    "default values",
			args:    map[string]any{},
			wantErr: false,
		},
		{
			name: "relative start",
			args: map[string]any{
				"time_start": "24h",
			},
			wantErr: false,
		},
		{
			name: "absolute start",
			args: map[string]any{
				"time_start": "2024-01-15T10:00:00Z",
			},
			wantErr: false,
		},
		{
			name: "both times",
			args: map[string]any{
				"time_start": "2024-01-15T10:00:00Z",
				"time_end":   "2024-01-15T12:00:00Z",
			},
			wantErr: false,
		},
		{
			name: "start after end",
			args: map[string]any{
				"time_start": "2024-01-15T12:00:00Z",
				"time_end":   "2024-01-15T10:00:00Z",
			},
			wantErr: true,
		},
		{
			name: "invalid start",
			args: map[string]any{
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
		args    map[string]any
		key     string
		want    int
		wantErr bool
	}{
		{
			name:    "float64 value",
			args:    map[string]any{"num": float64(42)},
			key:     "num",
			want:    42,
			wantErr: false,
		},
		{
			name:    "int value",
			args:    map[string]any{"num": 42},
			key:     "num",
			want:    42,
			wantErr: false,
		},
		{
			name:    "int64 value",
			args:    map[string]any{"num": int64(42)},
			key:     "num",
			want:    42,
			wantErr: false,
		},
		{
			name:    "missing key",
			args:    map[string]any{},
			key:     "num",
			want:    0,
			wantErr: true,
		},
		{
			name:    "wrong type",
			args:    map[string]any{"num": "42"},
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
		input    any
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

// Note: formatNumeric is now tested in the tsv package (tsv/format_test.go)

func TestFormatMetricValue_Numeric(t *testing.T) {
	// Test that pgtype.Numeric values are properly handled by formatMetricValue
	numeric := pgtype.Numeric{Valid: true, Int: big.NewInt(42), Exp: 0}
	result := formatMetricValue(numeric)
	if result != "42" {
		t.Errorf("formatMetricValue(pgtype.Numeric) = %q, want %q", result, "42")
	}
}

func TestFormatMetricValue_PgTypes(t *testing.T) {
	testTime := time.Date(2024, 1, 15, 10, 30, 45, 0, time.UTC)

	tests := []struct {
		name     string
		input    any
		expected string
	}{
		// Integer types
		{
			name:     "pgtype.Int2 valid",
			input:    pgtype.Int2{Valid: true, Int16: 123},
			expected: "123",
		},
		{
			name:     "pgtype.Int2 invalid",
			input:    pgtype.Int2{Valid: false},
			expected: "",
		},
		{
			name:     "pgtype.Int4 valid",
			input:    pgtype.Int4{Valid: true, Int32: 456789},
			expected: "456789",
		},
		{
			name:     "pgtype.Int4 invalid",
			input:    pgtype.Int4{Valid: false},
			expected: "",
		},
		{
			name:     "pgtype.Int8 valid",
			input:    pgtype.Int8{Valid: true, Int64: 9223372036854775807},
			expected: "9223372036854775807",
		},
		{
			name:     "pgtype.Int8 invalid",
			input:    pgtype.Int8{Valid: false},
			expected: "",
		},
		// Float types
		{
			name:     "pgtype.Float4 valid",
			input:    pgtype.Float4{Valid: true, Float32: 3.14},
			expected: "3.14",
		},
		{
			name:     "pgtype.Float4 invalid",
			input:    pgtype.Float4{Valid: false},
			expected: "",
		},
		{
			name:     "pgtype.Float8 valid",
			input:    pgtype.Float8{Valid: true, Float64: 3.14159265359},
			expected: "3.14159265359",
		},
		{
			name:     "pgtype.Float8 invalid",
			input:    pgtype.Float8{Valid: false},
			expected: "",
		},
		// Text type
		{
			name:     "pgtype.Text valid",
			input:    pgtype.Text{Valid: true, String: "hello world"},
			expected: "hello world",
		},
		{
			name:     "pgtype.Text invalid",
			input:    pgtype.Text{Valid: false},
			expected: "",
		},
		// Bool type
		{
			name:     "pgtype.Bool true",
			input:    pgtype.Bool{Valid: true, Bool: true},
			expected: "true",
		},
		{
			name:     "pgtype.Bool false",
			input:    pgtype.Bool{Valid: true, Bool: false},
			expected: "false",
		},
		{
			name:     "pgtype.Bool invalid",
			input:    pgtype.Bool{Valid: false},
			expected: "",
		},
		// Timestamp types
		{
			name:     "pgtype.Timestamp valid",
			input:    pgtype.Timestamp{Valid: true, Time: testTime},
			expected: "2024-01-15T10:30:45Z",
		},
		{
			name:     "pgtype.Timestamp invalid",
			input:    pgtype.Timestamp{Valid: false},
			expected: "",
		},
		{
			name:     "pgtype.Timestamptz valid",
			input:    pgtype.Timestamptz{Valid: true, Time: testTime},
			expected: "2024-01-15T10:30:45Z",
		},
		{
			name:     "pgtype.Timestamptz invalid",
			input:    pgtype.Timestamptz{Valid: false},
			expected: "",
		},
		{
			name:     "pgtype.Date valid",
			input:    pgtype.Date{Valid: true, Time: testTime},
			expected: "2024-01-15",
		},
		{
			name:     "pgtype.Date invalid",
			input:    pgtype.Date{Valid: false},
			expected: "",
		},
		// Interval type
		{
			name:     "pgtype.Interval valid",
			input:    pgtype.Interval{Valid: true, Days: 5, Microseconds: 3600000000},
			expected: "5 days 01:00:00",
		},
		{
			name:     "pgtype.Interval invalid",
			input:    pgtype.Interval{Valid: false},
			expected: "",
		},
		// UUID type
		{
			name:     "pgtype.UUID valid",
			input:    pgtype.UUID{Valid: true, Bytes: [16]byte{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0, 0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0}},
			expected: "12345678-9abc-def0-1234-56789abcdef0",
		},
		{
			name:     "pgtype.UUID invalid",
			input:    pgtype.UUID{Valid: false},
			expected: "",
		},
		{
			name:     "raw UUID bytes",
			input:    [16]byte{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0, 0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0},
			expected: "12345678-9abc-def0-1234-56789abcdef0",
		},
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

// Note: formatInterval and formatUUID are now tested in the tsv package (tsv/format_test.go)
