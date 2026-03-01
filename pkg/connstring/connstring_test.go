/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package connstring

import (
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/pkg/datastoreconfig"
)

func TestEscapeValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty string", "", ""},
		{"no special chars", "hello", "hello"},
		{"single quote", "it's", "it''s"},
		{"backslash", `back\slash`, `back\\slash`},
		{"both", `it\'s`, `it\\''s`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EscapeValue(tt.input)
			if result != tt.expected {
				t.Errorf("EscapeValue(%q) = %q, want %q",
					tt.input, result, tt.expected)
			}
		})
	}
}

func TestBuild(t *testing.T) {
	params := map[string]string{
		"host":   "localhost",
		"dbname": "testdb",
	}

	result := Build(params)
	if !strings.Contains(result, "host='localhost'") {
		t.Errorf("Build() = %q, want it to contain host='localhost'", result)
	}
	if !strings.Contains(result, "dbname='testdb'") {
		t.Errorf("Build() = %q, want it to contain dbname='testdb'", result)
	}
}

func TestBuildFromConfig(t *testing.T) {
	cfg := datastoreconfig.DatastoreConfig{
		Host:     "db.example.com",
		Database: "mydb",
		Username: "admin",
		Password: "secret",
		Port:     5432,
		SSLMode:  "require",
	}

	result := BuildFromConfig(cfg, "test-app")
	for _, want := range []string{
		"host='db.example.com'",
		"dbname='mydb'",
		"user='admin'",
		"password='secret'",
		"port='5432'",
		"sslmode='require'",
		"application_name='test-app'",
	} {
		if !strings.Contains(result, want) {
			t.Errorf("BuildFromConfig() = %q, want it to contain %q",
				result, want)
		}
	}
}
