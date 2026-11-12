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
		{"single quote", "it's", `it\'s`},
		{"backslash", `back\slash`, `back\\slash`},
		{"both", `it\'s`, `it\\\'s`},
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
	if want := "dbname='testdb' host='localhost'"; result != want {
		t.Errorf("Build() = %q, want %q", result, want)
	}
}

// TestBuildSortedKeys checks that Build emits keys in sorted order on
// every call, so its output never depends on map iteration order.
func TestBuildSortedKeys(t *testing.T) {
	params := map[string]string{
		"user":             "u",
		"application_name": "a",
		"sslmode":          "require",
		"host":             "h",
		"dbname":           "d",
		"port":             "5432",
		"password":         "p",
		"hostaddr":         "192.0.2.1",
	}
	want := "application_name='a' dbname='d' host='h' hostaddr='192.0.2.1' " +
		"password='p' port='5432' sslmode='require' user='u'"
	for range 50 {
		if got := Build(params); got != want {
			t.Fatalf("Build() = %q, want %q", got, want)
		}
	}
}

// parseKeywordValue parses a keyword/value connection string with the
// libpq-faithful scanner used by RedactPasswords, failing the test on a
// duplicate keyword, since a duplicate is how an injected value would
// override a real one.
func parseKeywordValue(t *testing.T, s string) map[string]string {
	t.Helper()
	got := make(map[string]string)
	i, n := 0, len(s)
	for {
		for i < n && isConnSpace(s[i]) {
			i++
		}
		if i >= n {
			return got
		}
		keyStart := i
		for i < n && s[i] != '=' && !isConnSpace(s[i]) {
			i++
		}
		key := s[keyStart:i]
		if i >= n || s[i] != '=' {
			t.Fatalf("parse %q: keyword %q has no '='", s, key)
		}
		i++
		val, next, err := scanKeywordValue(s, i)
		if err != nil {
			t.Fatalf("parse %q: %v", s, err)
		}
		if _, dup := got[key]; dup {
			t.Fatalf("parse %q: duplicate keyword %q", s, key)
		}
		got[key] = val.decoded
		i = next
	}
}

// hostileValues are values that would break out of a quoted value if
// quotes or backslashes were escaped incorrectly.
var hostileValues = []string{
	`x' host='evil.example.com`,
	`x\' host=evil.example.com`,
	`x'' host='evil.example.com`,
	`trailing\`,
	`'`,
	`\`,
	`''`,
	`\'`,
	"a b\tc\nd",
	"a=b c=d",
	` sslcert=/tmp/evil.crt `,
	`plain`,
	"",
}

// TestBuildRoundTrip builds connection strings with hostile values in
// every field and parses them back, checking that each value survives
// unchanged and that no keyword is added or overridden.
func TestBuildRoundTrip(t *testing.T) {
	keys := []string{"dbname", "user", "password", "host", "sslcert", "application_name"}
	for _, v := range hostileValues {
		for _, target := range keys {
			params := map[string]string{
				"dbname":           "db",
				"user":             "u",
				"password":         "p",
				"host":             "db.example.com",
				"sslcert":          "/certs/client.crt",
				"application_name": "app",
			}
			params[target] = v
			s := Build(params)
			got := parseKeywordValue(t, s)
			if len(got) != len(params) {
				t.Errorf("%s=%q: Build() = %q parsed to %d keywords, want %d",
					target, v, s, len(got), len(params))
			}
			for k, want := range params {
				if got[k] != want {
					t.Errorf("%s=%q: Build() = %q parsed %s as %q, want %q",
						target, v, s, k, got[k], want)
				}
			}
		}
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

func TestBuildFromConfigWithHostAddr(t *testing.T) {
	cfg := datastoreconfig.DatastoreConfig{
		Host:     "db.example.com",
		HostAddr: "192.168.1.100",
		Database: "mydb",
		Username: "admin",
	}

	result := BuildFromConfig(cfg, "")
	if !strings.Contains(result, "hostaddr='192.168.1.100'") {
		t.Errorf("BuildFromConfig() = %q, want it to contain hostaddr",
			result)
	}
	if !strings.Contains(result, "host='db.example.com'") {
		t.Errorf("BuildFromConfig() = %q, want it to contain host", result)
	}
	// application_name should not be present when empty
	if strings.Contains(result, "application_name") {
		t.Errorf("BuildFromConfig() = %q, should not contain application_name",
			result)
	}
}

func TestBuildFromConfigWithSSLOptions(t *testing.T) {
	cfg := datastoreconfig.DatastoreConfig{
		Host:        "db.example.com",
		Database:    "mydb",
		Username:    "admin",
		SSLMode:     "verify-full",
		SSLCert:     "/path/to/cert.pem",
		SSLKey:      "/path/to/key.pem",
		SSLRootCert: "/path/to/ca.pem",
	}

	result := BuildFromConfig(cfg, "ssl-test")
	for _, want := range []string{
		"sslmode='verify-full'",
		"sslcert='/path/to/cert.pem'",
		"sslkey='/path/to/key.pem'",
		"sslrootcert='/path/to/ca.pem'",
	} {
		if !strings.Contains(result, want) {
			t.Errorf("BuildFromConfig() = %q, want it to contain %q",
				result, want)
		}
	}
}

func TestBuildFromConfigMinimal(t *testing.T) {
	// Test with only required fields
	cfg := datastoreconfig.DatastoreConfig{
		Database: "testdb",
		Username: "testuser",
	}

	result := BuildFromConfig(cfg, "")
	if !strings.Contains(result, "dbname='testdb'") {
		t.Errorf("BuildFromConfig() = %q, want it to contain dbname", result)
	}
	if !strings.Contains(result, "user='testuser'") {
		t.Errorf("BuildFromConfig() = %q, want it to contain user", result)
	}
	// Optional fields should not be present
	if strings.Contains(result, "port=") {
		t.Errorf("BuildFromConfig() = %q, should not contain port", result)
	}
	if strings.Contains(result, "password=") {
		t.Errorf("BuildFromConfig() = %q, should not contain password", result)
	}
}

func TestBuildFromConfigZeroPort(t *testing.T) {
	cfg := datastoreconfig.DatastoreConfig{
		Host:     "localhost",
		Database: "mydb",
		Username: "admin",
		Port:     0, // Zero port should be omitted
	}

	result := BuildFromConfig(cfg, "")
	if strings.Contains(result, "port=") {
		t.Errorf("BuildFromConfig() = %q, should not contain port when zero",
			result)
	}
}

func TestBuildFromConfigSpecialCharsInPassword(t *testing.T) {
	cfg := datastoreconfig.DatastoreConfig{
		Host:     "localhost",
		Database: "mydb",
		Username: "admin",
		Password: "p@ss'w\\ord",
	}

	result := BuildFromConfig(cfg, "")
	// Password should be escaped
	if !strings.Contains(result, `password='p@ss\'w\\ord'`) {
		t.Errorf("BuildFromConfig() = %q, want escaped password", result)
	}
}

func TestBuildEmptyParams(t *testing.T) {
	params := map[string]string{}
	result := Build(params)
	if result != "" {
		t.Errorf("Build(empty) = %q, want empty string", result)
	}
}

func TestEscapeValueMultipleSpecialChars(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"multiple backslashes", `a\\b\\c`, `a\\\\b\\\\c`},
		{"multiple quotes", "a''b''c", `a\'\'b\'\'c`},
		{"alternating", `a\'b\'c`, `a\\\'b\\\'c`},
		{"complex password", `P@ss\'word"123`, `P@ss\\\'word"123`},
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
