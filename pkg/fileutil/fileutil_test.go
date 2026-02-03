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
