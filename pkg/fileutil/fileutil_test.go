/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package fileutil

import (
    "os"
    "path/filepath"
    "testing"
)

func TestExpandTildePath(t *testing.T) {
    homeDir, err := os.UserHomeDir()
    if err != nil {
        t.Fatalf("failed to get home directory: %v", err)
    }

    tests := []struct {
        name     string
        path     string
        expected string
        wantErr  bool
    }{
        {
            name:     "empty path",
            path:     "",
            expected: "",
            wantErr:  false,
        },
        {
            name:     "path without tilde",
            path:     "/etc/config.yaml",
            expected: "/etc/config.yaml",
            wantErr:  false,
        },
        {
            name:     "path with tilde",
            path:     "~/.config/app.yaml",
            expected: filepath.Join(homeDir, ".config/app.yaml"),
            wantErr:  false,
        },
        {
            name:     "tilde only",
            path:     "~",
            expected: homeDir,
            wantErr:  false,
        },
        {
            name:     "relative path",
            path:     "./config.yaml",
            expected: "./config.yaml",
            wantErr:  false,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := ExpandTildePath(tt.path)
            if (err != nil) != tt.wantErr {
                t.Errorf("ExpandTildePath() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if result != tt.expected {
                t.Errorf("ExpandTildePath() = %v, want %v", result, tt.expected)
            }
        })
    }
}

func TestReadTrimmedFile(t *testing.T) {
    tmpDir := t.TempDir()

    // Test reading valid file with whitespace
    filePath := filepath.Join(tmpDir, "test.txt")
    if err := os.WriteFile(filePath, []byte("  hello world  \n\n"), 0600); err != nil {
        t.Fatalf("failed to write test file: %v", err)
    }

    content, err := ReadTrimmedFile(filePath)
    if err != nil {
        t.Errorf("ReadTrimmedFile() unexpected error: %v", err)
    }
    if content != "hello world" {
        t.Errorf("ReadTrimmedFile() = %q, want %q", content, "hello world")
    }

    // Test reading non-existent file
    _, err = ReadTrimmedFile(filepath.Join(tmpDir, "nonexistent.txt"))
    if err == nil {
        t.Error("ReadTrimmedFile() expected error for non-existent file")
    }

    // Test reading empty file
    emptyFile := filepath.Join(tmpDir, "empty.txt")
    if err := os.WriteFile(emptyFile, []byte(""), 0600); err != nil {
        t.Fatalf("failed to write empty file: %v", err)
    }
    content, err = ReadTrimmedFile(emptyFile)
    if err != nil {
        t.Errorf("ReadTrimmedFile() unexpected error for empty file: %v", err)
    }
    if content != "" {
        t.Errorf("ReadTrimmedFile() = %q, want empty string", content)
    }
}

func TestReadTrimmedFileWithTilde(t *testing.T) {
    tmpDir := t.TempDir()

    // Test reading valid file
    filePath := filepath.Join(tmpDir, "secret.txt")
    if err := os.WriteFile(filePath, []byte("  secret-value  "), 0600); err != nil {
        t.Fatalf("failed to write test file: %v", err)
    }

    content, err := ReadTrimmedFileWithTilde(filePath)
    if err != nil {
        t.Errorf("ReadTrimmedFileWithTilde() unexpected error: %v", err)
    }
    if content != "secret-value" {
        t.Errorf("ReadTrimmedFileWithTilde() = %q, want %q", content, "secret-value")
    }
}

func TestReadOptionalTrimmedFile(t *testing.T) {
    tmpDir := t.TempDir()

    // Test reading valid file
    filePath := filepath.Join(tmpDir, "api_key.txt")
    if err := os.WriteFile(filePath, []byte("  test-api-key  \n"), 0600); err != nil {
        t.Fatalf("failed to write test file: %v", err)
    }

    content, err := ReadOptionalTrimmedFile(filePath)
    if err != nil {
        t.Errorf("ReadOptionalTrimmedFile() unexpected error: %v", err)
    }
    if content != "test-api-key" {
        t.Errorf("ReadOptionalTrimmedFile() = %q, want %q", content, "test-api-key")
    }

    // Test empty path
    content, err = ReadOptionalTrimmedFile("")
    if err != nil {
        t.Errorf("ReadOptionalTrimmedFile() unexpected error for empty path: %v", err)
    }
    if content != "" {
        t.Errorf("ReadOptionalTrimmedFile() = %q, want empty string", content)
    }

    // Test non-existent file (should return empty, not error)
    content, err = ReadOptionalTrimmedFile(filepath.Join(tmpDir, "nonexistent.txt"))
    if err != nil {
        t.Errorf("ReadOptionalTrimmedFile() unexpected error for non-existent file: %v", err)
    }
    if content != "" {
        t.Errorf("ReadOptionalTrimmedFile() = %q, want empty string", content)
    }
}

func TestLoadYAMLFile(t *testing.T) {
    tmpDir := t.TempDir()

    type TestConfig struct {
        Name  string `yaml:"name"`
        Value int    `yaml:"value"`
    }

    // Test loading valid YAML file
    yamlContent := "name: test\nvalue: 42\n"
    filePath := filepath.Join(tmpDir, "config.yaml")
    if err := os.WriteFile(filePath, []byte(yamlContent), 0600); err != nil {
        t.Fatalf("failed to write test file: %v", err)
    }

    var cfg TestConfig
    err := LoadYAMLFile(filePath, &cfg)
    if err != nil {
        t.Errorf("LoadYAMLFile() unexpected error: %v", err)
    }
    if cfg.Name != "test" || cfg.Value != 42 {
        t.Errorf("LoadYAMLFile() = %+v, want {Name:test Value:42}", cfg)
    }

    // Test loading non-existent file
    err = LoadYAMLFile(filepath.Join(tmpDir, "nonexistent.yaml"), &cfg)
    if err == nil {
        t.Error("LoadYAMLFile() expected error for non-existent file")
    }

    // Test loading invalid YAML
    invalidPath := filepath.Join(tmpDir, "invalid.yaml")
    if err := os.WriteFile(invalidPath, []byte("invalid: [yaml: content"), 0600); err != nil {
        t.Fatalf("failed to write invalid yaml file: %v", err)
    }
    err = LoadYAMLFile(invalidPath, &cfg)
    if err == nil {
        t.Error("LoadYAMLFile() expected error for invalid YAML")
    }
}

func TestLoadOptionalYAMLFile(t *testing.T) {
    tmpDir := t.TempDir()

    type TestConfig struct {
        Name  string `yaml:"name"`
        Value int    `yaml:"value"`
    }

    // Test loading valid YAML file
    yamlContent := "name: optional\nvalue: 99\n"
    filePath := filepath.Join(tmpDir, "config.yaml")
    if err := os.WriteFile(filePath, []byte(yamlContent), 0600); err != nil {
        t.Fatalf("failed to write test file: %v", err)
    }

    var cfg TestConfig
    err := LoadOptionalYAMLFile(filePath, &cfg)
    if err != nil {
        t.Errorf("LoadOptionalYAMLFile() unexpected error: %v", err)
    }
    if cfg.Name != "optional" || cfg.Value != 99 {
        t.Errorf("LoadOptionalYAMLFile() = %+v, want {Name:optional Value:99}", cfg)
    }

    // Test loading non-existent file (should succeed with no modification)
    var emptyCfg TestConfig
    err = LoadOptionalYAMLFile(filepath.Join(tmpDir, "nonexistent.yaml"), &emptyCfg)
    if err != nil {
        t.Errorf("LoadOptionalYAMLFile() unexpected error for non-existent file: %v", err)
    }
    if emptyCfg.Name != "" || emptyCfg.Value != 0 {
        t.Errorf("LoadOptionalYAMLFile() modified cfg for non-existent file: %+v", emptyCfg)
    }
}

func TestFileExists(t *testing.T) {
    tmpDir := t.TempDir()

    // Create a test file
    existingFile := filepath.Join(tmpDir, "exists.txt")
    if err := os.WriteFile(existingFile, []byte("test"), 0600); err != nil {
        t.Fatalf("failed to create test file: %v", err)
    }

    // Test existing file
    if !FileExists(existingFile) {
        t.Error("FileExists() returned false for existing file")
    }

    // Test non-existing file
    if FileExists(filepath.Join(tmpDir, "nonexistent.txt")) {
        t.Error("FileExists() returned true for non-existing file")
    }

    // Test directory (should return true since it exists)
    if !FileExists(tmpDir) {
        t.Error("FileExists() returned false for existing directory")
    }
}

func TestGetDefaultConfigPath(t *testing.T) {
    // Test when system path does not exist (common case)
    result := GetDefaultConfigPath("/usr/local/bin/myapp", "myapp.yaml")

    // Check if /etc/pgedge/myapp.yaml exists
    systemPath := "/etc/pgedge/myapp.yaml"
    fallbackPath := "/usr/local/bin/myapp.yaml"

    if FileExists(systemPath) {
        // System path exists, should prefer it
        if result != systemPath {
            t.Errorf("GetDefaultConfigPath() = %q, want system path %q",
                result, systemPath)
        }
    } else {
        // System path doesn't exist, should use fallback
        if result != fallbackPath {
            t.Errorf("GetDefaultConfigPath() = %q, want fallback path %q",
                result, fallbackPath)
        }
    }
}

func TestGetDefaultConfigPathWithDifferentBinaryPaths(t *testing.T) {
    tests := []struct {
        name           string
        binaryPath     string
        configFilename string
        fallbackPath   string
    }{
        {
            name:           "absolute path",
            binaryPath:     "/opt/app/bin/service",
            configFilename: "service.yaml",
            fallbackPath:   "/opt/app/bin/service.yaml",
        },
        {
            name:           "home directory",
            binaryPath:     "/home/user/bin/collector",
            configFilename: "collector.yaml",
            fallbackPath:   "/home/user/bin/collector.yaml",
        },
        {
            name:           "with dots in filename",
            binaryPath:     "/usr/bin/app",
            configFilename: "app.config.yaml",
            fallbackPath:   "/usr/bin/app.config.yaml",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := GetDefaultConfigPath(tt.binaryPath, tt.configFilename)
            systemPath := filepath.Join("/etc/pgedge", tt.configFilename)

            if FileExists(systemPath) {
                // System path exists, should prefer it
                if result != systemPath {
                    t.Errorf("GetDefaultConfigPath(%q, %q) = %q, want system path %q",
                        tt.binaryPath, tt.configFilename, result, systemPath)
                }
            } else {
                // System path doesn't exist, should use fallback
                if result != tt.fallbackPath {
                    t.Errorf("GetDefaultConfigPath(%q, %q) = %q, want fallback path %q",
                        tt.binaryPath, tt.configFilename, result, tt.fallbackPath)
                }
            }
        })
    }
}

func TestReadOptionalTrimmedFileError(t *testing.T) {
    // Test with a directory (should fail when trying to read)
    tmpDir := t.TempDir()

    // Create a subdirectory
    subDir := filepath.Join(tmpDir, "subdir")
    if err := os.Mkdir(subDir, 0755); err != nil {
        t.Fatalf("failed to create subdirectory: %v", err)
    }

    // Attempting to read a directory should fail
    _, err := ReadOptionalTrimmedFile(subDir)
    if err == nil {
        t.Error("ReadOptionalTrimmedFile() should fail when reading directory")
    }
}

func TestReadTrimmedFileWithWhitespaceVariants(t *testing.T) {
    tmpDir := t.TempDir()

    tests := []struct {
        name     string
        content  string
        expected string
    }{
        {"leading spaces", "   hello", "hello"},
        {"trailing spaces", "hello   ", "hello"},
        {"leading tabs", "\t\thello", "hello"},
        {"trailing tabs", "hello\t\t", "hello"},
        {"leading newlines", "\n\nhello", "hello"},
        {"trailing newlines", "hello\n\n", "hello"},
        {"mixed whitespace", " \t\n hello world \n\t ", "hello world"},
        {"only whitespace", "   \t\n  ", ""},
        {"carriage returns", "\r\nhello\r\n", "hello"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            filePath := filepath.Join(tmpDir, tt.name+".txt")
            if err := os.WriteFile(filePath, []byte(tt.content), 0600); err != nil {
                t.Fatalf("failed to write test file: %v", err)
            }

            result, err := ReadTrimmedFile(filePath)
            if err != nil {
                t.Fatalf("ReadTrimmedFile() error: %v", err)
            }
            if result != tt.expected {
                t.Errorf("ReadTrimmedFile() = %q, want %q", result, tt.expected)
            }
        })
    }
}

func TestLoadYAMLFileWithNestedStructure(t *testing.T) {
    tmpDir := t.TempDir()

    type NestedConfig struct {
        Server struct {
            Host string `yaml:"host"`
            Port int    `yaml:"port"`
        } `yaml:"server"`
        Database struct {
            Name     string `yaml:"name"`
            Username string `yaml:"username"`
        } `yaml:"database"`
    }

    yamlContent := `
server:
  host: localhost
  port: 8080
database:
  name: testdb
  username: admin
`

    filePath := filepath.Join(tmpDir, "nested.yaml")
    if err := os.WriteFile(filePath, []byte(yamlContent), 0600); err != nil {
        t.Fatalf("failed to write test file: %v", err)
    }

    var cfg NestedConfig
    if err := LoadYAMLFile(filePath, &cfg); err != nil {
        t.Fatalf("LoadYAMLFile() error: %v", err)
    }

    if cfg.Server.Host != "localhost" {
        t.Errorf("Server.Host = %q, want %q", cfg.Server.Host, "localhost")
    }
    if cfg.Server.Port != 8080 {
        t.Errorf("Server.Port = %d, want %d", cfg.Server.Port, 8080)
    }
    if cfg.Database.Name != "testdb" {
        t.Errorf("Database.Name = %q, want %q", cfg.Database.Name, "testdb")
    }
    if cfg.Database.Username != "admin" {
        t.Errorf("Database.Username = %q, want %q",
            cfg.Database.Username, "admin")
    }
}

func TestReadTrimmedFileWithTildeError(t *testing.T) {
    // Test when file doesn't exist after tilde expansion
    _, err := ReadTrimmedFileWithTilde("~/nonexistent_file_12345.txt")
    if err == nil {
        t.Error("ReadTrimmedFileWithTilde() expected error for non-existent file")
    }
}

func TestGetDefaultConfigPathSystemPath(t *testing.T) {
    tmpDir := t.TempDir()

    result := GetDefaultConfigPath(
        filepath.Join(tmpDir, "binary"),
        "test.yaml",
    )

    systemPath := "/etc/pgedge/test.yaml"
    fallbackPath := filepath.Join(tmpDir, "test.yaml")

    if FileExists(systemPath) {
        // System path exists, should prefer it
        if result != systemPath {
            t.Errorf("GetDefaultConfigPath() = %q, want system path %q",
                result, systemPath)
        }
    } else {
        // System path doesn't exist, should use fallback
        if result != fallbackPath {
            t.Errorf("GetDefaultConfigPath() = %q, want fallback path %q",
                result, fallbackPath)
        }
    }
}

func TestExpandTildePathWithSlash(t *testing.T) {
    homeDir, err := os.UserHomeDir()
    if err != nil {
        t.Fatalf("failed to get home directory: %v", err)
    }

    // Test with just tilde and slash
    result, err := ExpandTildePath("~/")
    if err != nil {
        t.Fatalf("ExpandTildePath() error: %v", err)
    }
    expected := filepath.Join(homeDir, "/")
    if result != expected {
        t.Errorf("ExpandTildePath(\"~/\") = %q, want %q", result, expected)
    }

    // Test with deeply nested path
    result, err = ExpandTildePath("~/.config/app/settings.yaml")
    if err != nil {
        t.Fatalf("ExpandTildePath() error: %v", err)
    }
    expected = filepath.Join(homeDir, ".config/app/settings.yaml")
    if result != expected {
        t.Errorf("ExpandTildePath() = %q, want %q", result, expected)
    }
}

func TestLoadOptionalYAMLFileInvalidYAML(t *testing.T) {
    tmpDir := t.TempDir()

    type TestConfig struct {
        Name string `yaml:"name"`
    }

    // Create an invalid YAML file
    invalidPath := filepath.Join(tmpDir, "invalid.yaml")
    if err := os.WriteFile(invalidPath, []byte("invalid: [yaml: content"), 0600); err != nil {
        t.Fatalf("failed to write invalid yaml file: %v", err)
    }

    var cfg TestConfig
    err := LoadOptionalYAMLFile(invalidPath, &cfg)
    if err == nil {
        t.Error("LoadOptionalYAMLFile() expected error for invalid YAML")
    }
}
