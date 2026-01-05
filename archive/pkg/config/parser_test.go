/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package config

import (
    "os"
    "path/filepath"
    "testing"
)

func TestParser(t *testing.T) {
    // Create temporary config file
    tmpDir := t.TempDir()
    configPath := filepath.Join(tmpDir, "test.conf")

    content := `# Test config
key1 = value1
key2 = "value2"
key3 = 123
# Comment
key4 = true
`
    if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
        t.Fatalf("Failed to create test config: %v", err)
    }

    // Parse with handlers
    values := make(map[string]string)
    parser := NewParser()

    for _, key := range []string{"key1", "key2", "key3", "key4"} {
        k := key // capture for closure
        parser.RegisterHandler(k, func(key, value string) error {
            values[k] = value
            return nil
        })
    }

    if err := parser.ParseFile(configPath); err != nil {
        t.Fatalf("ParseFile failed: %v", err)
    }

    // Verify values
    expected := map[string]string{
        "key1": "value1",
        "key2": "value2",
        "key3": "123",
        "key4": "true",
    }

    for k, v := range expected {
        if values[k] != v {
            t.Errorf("Expected %s=%s, got %s", k, v, values[k])
        }
    }
}

func TestParseInt(t *testing.T) {
    tests := []struct {
        input    string
        expected int
        wantErr  bool
    }{
        {"123", 123, false},
        {"0", 0, false},
        {"-42", -42, false},
        {"invalid", 0, true},
        {"", 0, true},
    }

    for _, tt := range tests {
        result, err := ParseInt(tt.input)
        if tt.wantErr {
            if err == nil {
                t.Errorf("ParseInt(%q) expected error, got nil", tt.input)
            }
        } else {
            if err != nil {
                t.Errorf("ParseInt(%q) unexpected error: %v", tt.input, err)
            }
            if result != tt.expected {
                t.Errorf("ParseInt(%q) = %d, expected %d", tt.input, result, tt.expected)
            }
        }
    }
}

func TestParseBool(t *testing.T) {
    tests := []struct {
        input    string
        expected bool
        wantErr  bool
    }{
        {"true", true, false},
        {"false", false, false},
        {"1", true, false},
        {"0", false, false},
        {"invalid", false, true},
    }

    for _, tt := range tests {
        result, err := ParseBool(tt.input)
        if tt.wantErr {
            if err == nil {
                t.Errorf("ParseBool(%q) expected error, got nil", tt.input)
            }
        } else {
            if err != nil {
                t.Errorf("ParseBool(%q) unexpected error: %v", tt.input, err)
            }
            if result != tt.expected {
                t.Errorf("ParseBool(%q) = %v, expected %v", tt.input, result, tt.expected)
            }
        }
    }
}

func TestReadPasswordFile(t *testing.T) {
    tmpDir := t.TempDir()

    // Test valid password file
    pwdPath := filepath.Join(tmpDir, "password.txt")
    if err := os.WriteFile(pwdPath, []byte("secret123\n"), 0600); err != nil {
        t.Fatalf("Failed to create password file: %v", err)
    }

    password, err := ReadPasswordFile(pwdPath)
    if err != nil {
        t.Errorf("ReadPasswordFile failed: %v", err)
    }
    if password != "secret123" {
        t.Errorf("Expected 'secret123', got '%s'", password)
    }

    // Test empty password file
    emptyPath := filepath.Join(tmpDir, "empty.txt")
    if err := os.WriteFile(emptyPath, []byte(""), 0600); err != nil {
        t.Fatalf("Failed to create empty file: %v", err)
    }

    _, err = ReadPasswordFile(emptyPath)
    if err == nil {
        t.Error("Expected error for empty password file")
    }

    // Test non-existent file
    _, err = ReadPasswordFile("/nonexistent/file.txt")
    if err == nil {
        t.Error("Expected error for non-existent file")
    }
}
