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
	"testing"
)

func TestValidateStringParam(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]interface{}
		paramName string
		wantValue string
		wantError bool
	}{
		{
			name:      "valid string parameter",
			args:      map[string]interface{}{"test": "value"},
			paramName: "test",
			wantValue: "value",
			wantError: false,
		},
		{
			name:      "missing parameter",
			args:      map[string]interface{}{},
			paramName: "test",
			wantValue: "",
			wantError: true,
		},
		{
			name:      "empty string",
			args:      map[string]interface{}{"test": ""},
			paramName: "test",
			wantValue: "",
			wantError: true,
		},
		{
			name:      "wrong type (number)",
			args:      map[string]interface{}{"test": 123},
			paramName: "test",
			wantValue: "",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotValue, gotError := ValidateStringParam(tt.args, tt.paramName)

			if gotValue != tt.wantValue {
				t.Errorf("ValidateStringParam() value = %v, want %v", gotValue, tt.wantValue)
			}

			if (gotError != nil) != tt.wantError {
				t.Errorf("ValidateStringParam() error = %v, wantError %v", gotError != nil, tt.wantError)
			}

			if gotError != nil && !gotError.IsError {
				t.Error("ValidateStringParam() returned response should have IsError = true")
			}
		})
	}
}

func TestValidateOptionalStringParam(t *testing.T) {
	tests := []struct {
		name         string
		args         map[string]interface{}
		paramName    string
		defaultValue string
		want         string
	}{
		{
			name:         "present parameter",
			args:         map[string]interface{}{"test": "value"},
			paramName:    "test",
			defaultValue: "default",
			want:         "value",
		},
		{
			name:         "missing parameter",
			args:         map[string]interface{}{},
			paramName:    "test",
			defaultValue: "default",
			want:         "default",
		},
		{
			name:         "wrong type",
			args:         map[string]interface{}{"test": 123},
			paramName:    "test",
			defaultValue: "default",
			want:         "default",
		},
		{
			name:         "empty string returns empty",
			args:         map[string]interface{}{"test": ""},
			paramName:    "test",
			defaultValue: "default",
			want:         "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidateOptionalStringParam(tt.args, tt.paramName, tt.defaultValue)
			if got != tt.want {
				t.Errorf("ValidateOptionalStringParam() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateNumberParam(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]interface{}
		paramName string
		wantValue float64
		wantError bool
	}{
		{
			name:      "valid number",
			args:      map[string]interface{}{"test": 42.5},
			paramName: "test",
			wantValue: 42.5,
			wantError: false,
		},
		{
			name:      "missing parameter",
			args:      map[string]interface{}{},
			paramName: "test",
			wantValue: 0,
			wantError: true,
		},
		{
			name:      "wrong type (string)",
			args:      map[string]interface{}{"test": "123"},
			paramName: "test",
			wantValue: 0,
			wantError: true,
		},
		{
			name:      "zero is valid",
			args:      map[string]interface{}{"test": 0.0},
			paramName: "test",
			wantValue: 0,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotValue, gotError := ValidateNumberParam(tt.args, tt.paramName)

			if gotValue != tt.wantValue {
				t.Errorf("ValidateNumberParam() value = %v, want %v", gotValue, tt.wantValue)
			}

			if (gotError != nil) != tt.wantError {
				t.Errorf("ValidateNumberParam() error = %v, wantError %v", gotError != nil, tt.wantError)
			}
		})
	}
}

func TestValidateOptionalNumberParam(t *testing.T) {
	tests := []struct {
		name         string
		args         map[string]interface{}
		paramName    string
		defaultValue float64
		want         float64
	}{
		{
			name:         "present parameter",
			args:         map[string]interface{}{"test": 42.5},
			paramName:    "test",
			defaultValue: 10.0,
			want:         42.5,
		},
		{
			name:         "missing parameter",
			args:         map[string]interface{}{},
			paramName:    "test",
			defaultValue: 10.0,
			want:         10.0,
		},
		{
			name:         "wrong type",
			args:         map[string]interface{}{"test": "123"},
			paramName:    "test",
			defaultValue: 10.0,
			want:         10.0,
		},
		{
			name:         "zero returns zero",
			args:         map[string]interface{}{"test": 0.0},
			paramName:    "test",
			defaultValue: 10.0,
			want:         0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidateOptionalNumberParam(tt.args, tt.paramName, tt.defaultValue)
			if got != tt.want {
				t.Errorf("ValidateOptionalNumberParam() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateBoolParam(t *testing.T) {
	tests := []struct {
		name         string
		args         map[string]interface{}
		paramName    string
		defaultValue bool
		want         bool
	}{
		{
			name:         "present true",
			args:         map[string]interface{}{"test": true},
			paramName:    "test",
			defaultValue: false,
			want:         true,
		},
		{
			name:         "present false",
			args:         map[string]interface{}{"test": false},
			paramName:    "test",
			defaultValue: true,
			want:         false,
		},
		{
			name:         "missing parameter",
			args:         map[string]interface{}{},
			paramName:    "test",
			defaultValue: true,
			want:         true,
		},
		{
			name:         "wrong type",
			args:         map[string]interface{}{"test": "true"},
			paramName:    "test",
			defaultValue: false,
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidateBoolParam(tt.args, tt.paramName, tt.defaultValue)
			if got != tt.want {
				t.Errorf("ValidateBoolParam() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidatePositiveNumber(t *testing.T) {
	tests := []struct {
		name      string
		value     float64
		paramName string
		wantError bool
	}{
		{
			name:      "positive number",
			value:     42.5,
			paramName: "test",
			wantError: false,
		},
		{
			name:      "zero is invalid",
			value:     0,
			paramName: "test",
			wantError: true,
		},
		{
			name:      "negative number",
			value:     -5.0,
			paramName: "test",
			wantError: true,
		},
		{
			name:      "small positive",
			value:     0.001,
			paramName: "test",
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotError := ValidatePositiveNumber(tt.value, tt.paramName)

			if (gotError != nil) != tt.wantError {
				t.Errorf("ValidatePositiveNumber() error = %v, wantError %v", gotError != nil, tt.wantError)
			}

			if gotError != nil && !gotError.IsError {
				t.Error("ValidatePositiveNumber() returned response should have IsError = true")
			}
		})
	}
}

func TestValidateIdentifier(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantError bool
	}{
		{
			name:      "valid simple name",
			input:     "users",
			wantError: false,
		},
		{
			name:      "valid name with underscore",
			input:     "user_table",
			wantError: false,
		},
		{
			name:      "valid name starting with underscore",
			input:     "_private",
			wantError: false,
		},
		{
			name:      "valid name with numbers",
			input:     "table1",
			wantError: false,
		},
		{
			name:      "valid uppercase",
			input:     "MyTable",
			wantError: false,
		},
		{
			name:      "empty string",
			input:     "",
			wantError: true,
		},
		{
			name:      "starts with number",
			input:     "1table",
			wantError: true,
		},
		{
			name:      "contains space",
			input:     "user table",
			wantError: true,
		},
		{
			name:      "contains hyphen",
			input:     "user-table",
			wantError: true,
		},
		{
			name:      "contains dot",
			input:     "schema.table",
			wantError: true,
		},
		{
			name:      "SQL injection attempt",
			input:     "users; DROP TABLE users;--",
			wantError: true,
		},
		{
			name:      "SQL injection with quotes",
			input:     "users'--",
			wantError: true,
		},
		{
			name:      "maximum valid length (63 chars)",
			input:     "a23456789012345678901234567890123456789012345678901234567890123",
			wantError: false,
		},
		{
			name:      "too long (64 chars)",
			input:     "a234567890123456789012345678901234567890123456789012345678901234",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateIdentifier(tt.input)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateIdentifier(%q) error = %v, wantError %v", tt.input, err, tt.wantError)
			}
		})
	}
}

func TestValidateQualifiedTableName(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantError bool
	}{
		{
			name:      "simple table name",
			input:     "users",
			wantError: false,
		},
		{
			name:      "qualified name",
			input:     "public.users",
			wantError: false,
		},
		{
			name:      "qualified with underscores",
			input:     "my_schema.my_table",
			wantError: false,
		},
		{
			name:      "empty string",
			input:     "",
			wantError: true,
		},
		{
			name:      "just a dot",
			input:     ".",
			wantError: true,
		},
		{
			name:      "schema only with dot",
			input:     "schema.",
			wantError: true,
		},
		{
			name:      "table only with dot",
			input:     ".table",
			wantError: true,
		},
		{
			name:      "three parts",
			input:     "db.schema.table",
			wantError: true,
		},
		{
			name:      "SQL injection in schema",
			input:     "public;DROP TABLE users;--.table",
			wantError: true,
		},
		{
			name:      "SQL injection in table",
			input:     "public.users; DROP TABLE users;--",
			wantError: true,
		},
		{
			name:      "contains space",
			input:     "public .users",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateQualifiedTableName(tt.input)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateQualifiedTableName(%q) error = %v, wantError %v", tt.input, err, tt.wantError)
			}
		})
	}
}

func TestValidateColumnNames(t *testing.T) {
	tests := []struct {
		name      string
		input     []string
		wantError bool
	}{
		{
			name:      "valid single column",
			input:     []string{"name"},
			wantError: false,
		},
		{
			name:      "valid multiple columns",
			input:     []string{"id", "name", "created_at"},
			wantError: false,
		},
		{
			name:      "empty slice",
			input:     []string{},
			wantError: false,
		},
		{
			name:      "one invalid column",
			input:     []string{"id", "name; DROP TABLE users;--", "created_at"},
			wantError: true,
		},
		{
			name:      "all invalid columns",
			input:     []string{"1column", "has space", "has-dash"},
			wantError: true,
		},
		{
			name:      "contains empty string",
			input:     []string{"id", "", "name"},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateColumnNames(tt.input)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateColumnNames(%v) error = %v, wantError %v", tt.input, err, tt.wantError)
			}
		})
	}
}
