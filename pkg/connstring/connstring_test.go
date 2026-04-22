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
    if !strings.Contains(result, "password='p@ss''w\\\\ord'") {
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
        {"multiple quotes", "a''b''c", "a''''b''''c"},
        {"alternating", `a\'b\'c`, `a\\''b\\''c`},
        {"complex password", `P@ss\'word"123`, `P@ss\\''word"123`},
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
