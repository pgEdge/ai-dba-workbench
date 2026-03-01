/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package tsv

import (
	"math/big"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestFormatValue(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{"nil value", nil, ""},
		{"empty string", "", ""},
		{"simple string", "hello", "hello"},
		{"string with tab", "hello\tworld", "hello\\tworld"},
		{"string with newline", "hello\nworld", "hello\\nworld"},
		{"string with carriage return", "hello\rworld", "hello\\rworld"},
		{"integer", 42, "42"},
		{"negative integer", -17, "-17"},
		{"int64", int64(9223372036854775807), "9223372036854775807"},
		{"float64", 3.14159, "3.14159"},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
		{"byte slice", []byte("bytes"), "bytes"},
		{"array", []any{"a", "b"}, `["a","b"]`},
		{"map", map[string]any{"key": "value"}, `{"key":"value"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatValue(tt.input)
			if result != tt.expected {
				t.Errorf("FormatValue(%v) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatValue_Time(t *testing.T) {
	testTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	result := FormatValue(testTime)
	expected := "2024-01-15T10:30:00Z"

	if result != expected {
		t.Errorf("FormatValue(time) = %q, want %q", result, expected)
	}
}

func TestFormatResults(t *testing.T) {
	columnNames := []string{"id", "name", "active"}
	results := [][]any{
		{1, "Alice", true},
		{2, "Bob", false},
	}

	result := FormatResults(columnNames, results)
	expected := "id\tname\tactive\n1\tAlice\ttrue\n2\tBob\tfalse"

	if result != expected {
		t.Errorf("FormatResults() = %q, want %q", result, expected)
	}
}

func TestFormatResults_Empty(t *testing.T) {
	result := FormatResults([]string{}, nil)
	if result != "" {
		t.Errorf("FormatResults(empty) = %q, want empty string", result)
	}
}

func TestBuildRow(t *testing.T) {
	result := BuildRow("a", "b\tc", "d")
	expected := "a\tb\\tc\td"

	if result != expected {
		t.Errorf("BuildRow() = %q, want %q", result, expected)
	}
}

func TestFormatValue_PgTypes(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		// Numeric type
		{
			name:     "pgtype.Numeric valid",
			input:    pgtype.Numeric{Valid: true, Int: big.NewInt(42), Exp: 0},
			expected: "42",
		},
		{
			name:     "pgtype.Numeric invalid",
			input:    pgtype.Numeric{Valid: false},
			expected: "",
		},
		{
			name:     "pgtype.Numeric decimal",
			input:    pgtype.Numeric{Valid: true, Int: big.NewInt(314), Exp: -2},
			expected: "3.14",
		},
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
		{
			name:     "pgtype.Text with tab",
			input:    pgtype.Text{Valid: true, String: "hello\tworld"},
			expected: "hello\\tworld",
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
			result := FormatValue(tt.input)
			if result != tt.expected {
				t.Errorf("FormatValue(%v) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatValue_PgTimestamps(t *testing.T) {
	testTime := time.Date(2024, 1, 15, 10, 30, 45, 0, time.UTC)

	tests := []struct {
		name     string
		input    any
		expected string
	}{
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatValue(tt.input)
			if result != tt.expected {
				t.Errorf("FormatValue(%v) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatValue_PgInterval(t *testing.T) {
	tests := []struct {
		name     string
		input    pgtype.Interval
		expected string
	}{
		{
			name:     "invalid interval",
			input:    pgtype.Interval{Valid: false},
			expected: "",
		},
		{
			name:     "zero interval",
			input:    pgtype.Interval{Valid: true, Months: 0, Days: 0, Microseconds: 0},
			expected: "00:00:00",
		},
		{
			name:     "1 hour",
			input:    pgtype.Interval{Valid: true, Microseconds: 3600000000},
			expected: "01:00:00",
		},
		{
			name:     "1 day",
			input:    pgtype.Interval{Valid: true, Days: 1},
			expected: "1 day",
		},
		{
			name:     "5 days",
			input:    pgtype.Interval{Valid: true, Days: 5},
			expected: "5 days",
		},
		{
			name:     "1 month",
			input:    pgtype.Interval{Valid: true, Months: 1},
			expected: "1 mon",
		},
		{
			name:     "3 months",
			input:    pgtype.Interval{Valid: true, Months: 3},
			expected: "3 mons",
		},
		{
			name:     "1 year",
			input:    pgtype.Interval{Valid: true, Months: 12},
			expected: "1 year",
		},
		{
			name:     "2 years",
			input:    pgtype.Interval{Valid: true, Months: 24},
			expected: "2 years",
		},
		{
			name:     "1 year 6 months",
			input:    pgtype.Interval{Valid: true, Months: 18},
			expected: "1 year 6 mons",
		},
		{
			name:     "complex interval",
			input:    pgtype.Interval{Valid: true, Months: 14, Days: 3, Microseconds: 3661000000},
			expected: "1 year 2 mons 3 days 01:01:01",
		},
		{
			name:     "with fractional seconds",
			input:    pgtype.Interval{Valid: true, Microseconds: 1500000},
			expected: "00:00:01.500000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatValue(tt.input)
			if result != tt.expected {
				t.Errorf("FormatValue(%v) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatNumeric(t *testing.T) {
	tests := []struct {
		name     string
		input    pgtype.Numeric
		expected string
	}{
		{
			name:     "invalid numeric",
			input:    pgtype.Numeric{Valid: false},
			expected: "",
		},
		{
			name:     "NaN",
			input:    pgtype.Numeric{Valid: true, NaN: true},
			expected: "NaN",
		},
		{
			name:     "positive infinity",
			input:    pgtype.Numeric{Valid: true, InfinityModifier: pgtype.Infinity},
			expected: "Infinity",
		},
		{
			name:     "negative infinity",
			input:    pgtype.Numeric{Valid: true, InfinityModifier: pgtype.NegativeInfinity},
			expected: "-Infinity",
		},
		{
			name:     "nil Int",
			input:    pgtype.Numeric{Valid: true, Int: nil, Exp: 0},
			expected: "0",
		},
		{
			name:     "whole number 6",
			input:    pgtype.Numeric{Valid: true, Int: big.NewInt(6), Exp: 0},
			expected: "6",
		},
		{
			name:     "decimal 3.14 (Int=314, Exp=-2)",
			input:    pgtype.Numeric{Valid: true, Int: big.NewInt(314), Exp: -2},
			expected: "3.14",
		},
		{
			name:     "large number with positive exp",
			input:    pgtype.Numeric{Valid: true, Int: big.NewInt(5), Exp: 3},
			expected: "5000",
		},
		{
			name:     "numeric from trace (60000000000000000, -16) = 6",
			input:    pgtype.Numeric{Valid: true, Int: big.NewInt(60000000000000000), Exp: -16},
			expected: "6",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatNumeric(tt.input)
			if result != tt.expected {
				t.Errorf("formatNumeric() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestFormatInterval(t *testing.T) {
	tests := []struct {
		name     string
		input    pgtype.Interval
		expected string
	}{
		{
			name:     "zero",
			input:    pgtype.Interval{Valid: true},
			expected: "00:00:00",
		},
		{
			name:     "hours minutes seconds",
			input:    pgtype.Interval{Valid: true, Microseconds: 7384000000}, // 2:03:04
			expected: "02:03:04",
		},
		{
			name:     "negative months",
			input:    pgtype.Interval{Valid: true, Months: -3},
			expected: "-3 mons", // Negative intervals are supported
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatInterval(tt.input)
			if result != tt.expected {
				t.Errorf("formatInterval() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestFormatUUID(t *testing.T) {
	tests := []struct {
		name     string
		input    [16]byte
		expected string
	}{
		{
			name:     "standard UUID",
			input:    [16]byte{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0, 0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0},
			expected: "12345678-9abc-def0-1234-56789abcdef0",
		},
		{
			name:     "all zeros",
			input:    [16]byte{},
			expected: "00000000-0000-0000-0000-000000000000",
		},
		{
			name:     "all ones",
			input:    [16]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
			expected: "ffffffff-ffff-ffff-ffff-ffffffffffff",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatUUID(tt.input)
			if result != tt.expected {
				t.Errorf("formatUUID() = %q, want %q", result, tt.expected)
			}
		})
	}
}
