/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package database

import (
	"strings"
	"testing"
)

func TestEscapeConnStringValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no special characters",
			input:    "simplepassword",
			expected: "simplepassword",
		},
		{
			name:     "single quote",
			input:    "test'pass",
			expected: "test''pass",
		},
		{
			name:     "multiple single quotes",
			input:    "it's a test's password",
			expected: "it''s a test''s password",
		},
		{
			name:     "backslash",
			input:    `test\pass`,
			expected: `test\\pass`,
		},
		{
			name:     "multiple backslashes",
			input:    `c:\path\to\file`,
			expected: `c:\\path\\to\\file`,
		},
		{
			name:     "single quote and backslash",
			input:    `test\'pass`,
			expected: `test\\''pass`,
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only single quote",
			input:    "'",
			expected: "''",
		},
		{
			name:     "only backslash",
			input:    `\`,
			expected: `\\`,
		},
		{
			name:     "complex password with symbols",
			input:    "P@ss'w0rd\\123!",
			expected: "P@ss''w0rd\\\\123!",
		},
		{
			name:     "consecutive single quotes",
			input:    "test''pass",
			expected: "test''''pass",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeConnStringValue(tt.input)
			if result != tt.expected {
				t.Errorf("escapeConnStringValue(%q) = %q, want %q",
					tt.input, result, tt.expected)
			}
		})
	}
}

func TestBuildPostgresConnectionStringEscaping(t *testing.T) {
	tests := []struct {
		name           string
		params         map[string]string
		checkContains  []string
		checkNotExists []string
	}{
		{
			name: "password with single quote",
			params: map[string]string{
				"host":     "localhost",
				"password": "test'pass",
			},
			checkContains: []string{
				"password='test''pass'",
				"host='localhost'",
			},
		},
		{
			name: "password with backslash",
			params: map[string]string{
				"host":     "localhost",
				"password": `test\pass`,
			},
			checkContains: []string{
				`password='test\\pass'`,
			},
		},
		{
			name: "password with single quote and backslash",
			params: map[string]string{
				"host":     "localhost",
				"password": `O'Brien\secret`,
			},
			checkContains: []string{
				`password='O''Brien\\secret'`,
			},
		},
		{
			name: "all params with special characters",
			params: map[string]string{
				"host":     "db.example.com",
				"dbname":   "my'database",
				"user":     "admin'user",
				"password": `P@ss'w0rd\123`,
			},
			checkContains: []string{
				"dbname='my''database'",
				"user='admin''user'",
				`password='P@ss''w0rd\\123'`,
			},
		},
		{
			name: "empty password",
			params: map[string]string{
				"host":     "localhost",
				"password": "",
			},
			checkContains: []string{
				"password=''",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildPostgresConnectionString(tt.params)

			for _, expected := range tt.checkContains {
				if !strings.Contains(result, expected) {
					t.Errorf("buildPostgresConnectionString() = %q, want it to contain %q",
						result, expected)
				}
			}

			for _, notExpected := range tt.checkNotExists {
				if strings.Contains(result, notExpected) {
					t.Errorf("buildPostgresConnectionString() = %q, should not contain %q",
						result, notExpected)
				}
			}
		})
	}
}

// TestConnectionStringInjectionPrevention verifies that the escaping prevents
// connection string injection attacks
func TestConnectionStringInjectionPrevention(t *testing.T) {
	// An attacker might try to inject additional parameters via the password
	maliciousPassword := "secret' host='evil.com"

	params := map[string]string{
		"host":     "legitimate.com",
		"password": maliciousPassword,
	}

	result := buildPostgresConnectionString(params)

	// The injected host should be escaped, not treated as a separate parameter.
	// The result should contain the escaped version where single quotes are doubled.
	escapedPassword := "password='secret'' host=''evil.com'"
	if !strings.Contains(result, escapedPassword) {
		t.Errorf("Connection string injection not prevented: %s", result)
	}

	// Verify the legitimate host parameter is present and properly formatted.
	if !strings.Contains(result, "host='legitimate.com'") {
		t.Errorf("Legitimate host parameter not found in: %s", result)
	}

	// Verify the malicious host is NOT present as a standalone parameter.
	// If injection worked, we would see "host='evil.com'" as a separate param.
	if strings.Contains(result, "host='evil.com'") {
		t.Errorf("Injection attack succeeded - evil.com appeared as host param: %s", result)
	}
}
