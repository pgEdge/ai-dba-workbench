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
	"runtime"
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

func TestReadSecretFile(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"trailing newline trimmed", "secret-value\n", "secret-value"},
		{"trailing CRLF trimmed", "secret-value\r\n", "secret-value"},
		{"multiple trailing newlines trimmed", "secret-value\n\n\n", "secret-value"},
		{"no trailing newline", "secret-value", "secret-value"},
		{"interior spaces preserved", "a secret with spaces\n", "a secret with spaces"},
		{"leading spaces preserved", "   leading\n", "   leading"},
		{"trailing spaces preserved", "trailing   \n", "trailing   "},
		{"tabs preserved", "a\tb\tc\n", "a\tb\tc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filePath := filepath.Join(tmpDir, tt.name+".txt")
			if err := os.WriteFile(filePath, []byte(tt.content), 0600); err != nil {
				t.Fatalf("failed to write test file: %v", err)
			}

			got, err := ReadSecretFile(filePath)
			if err != nil {
				t.Fatalf("ReadSecretFile() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("ReadSecretFile() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReadSecretFileEmpty(t *testing.T) {
	tmpDir := t.TempDir()

	// A file that is empty or contains only a trailing newline is an error.
	for _, content := range []string{"", "\n", "\r\n", "\n\n"} {
		filePath := filepath.Join(tmpDir, "empty.txt")
		if err := os.WriteFile(filePath, []byte(content), 0600); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		_, err := ReadSecretFile(filePath)
		if err == nil {
			t.Errorf("ReadSecretFile(%q) expected empty-file error, got nil", content)
		}
	}
}

func TestReadSecretFileMissing(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := ReadSecretFile(filepath.Join(tmpDir, "nonexistent.txt"))
	if err == nil {
		t.Error("ReadSecretFile() expected error for non-existent file")
	}
}

func TestReadSecretFileTildeExpansion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// os.UserHomeDir reads %USERPROFILE% on Windows, so set it too to
	// keep the tilde resolving inside the temp dir on every platform.
	t.Setenv("USERPROFILE", home)

	filePath := filepath.Join(home, "secret.txt")
	if err := os.WriteFile(filePath, []byte("tilde-secret\n"), 0600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	got, err := ReadSecretFile("~/secret.txt")
	if err != nil {
		t.Fatalf("ReadSecretFile() unexpected error: %v", err)
	}
	if got != "tilde-secret" {
		t.Errorf("ReadSecretFile() = %q, want %q", got, "tilde-secret")
	}
}

func TestReadSecretFileTildeExpansionError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME-unset behaviour differs on Windows")
	}

	// With HOME unset, os.UserHomeDir() fails, so ExpandTildePath
	// returns an error and ReadSecretFile must propagate it.
	t.Setenv("HOME", "")
	_, err := ReadSecretFile("~/secret.txt")
	if err == nil {
		t.Error("ReadSecretFile() expected error when HOME is unset")
	}
}

func TestReadSecretFilePermissiveWarning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits do not map on Windows")
	}

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "secret.txt")
	if err := os.WriteFile(filePath, []byte("secret\n"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// The warning goes to stderr; this exercises the warn branch and
	// confirms the read still succeeds despite the permissive mode.
	got, err := ReadSecretFile(filePath)
	if err != nil {
		t.Fatalf("ReadSecretFile() unexpected error: %v", err)
	}
	if got != "secret" {
		t.Errorf("ReadSecretFile() = %q, want %q", got, "secret")
	}
}

func TestWarnIfPermissive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits do not map on Windows")
	}

	tmpDir := t.TempDir()

	// Owner-only file: no warning path, must not panic.
	ownerOnly := filepath.Join(tmpDir, "owner.txt")
	if err := os.WriteFile(ownerOnly, []byte("x"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	WarnIfPermissive(ownerOnly)

	// Group/world readable: exercises the warning branch.
	permissive := filepath.Join(tmpDir, "perm.txt")
	if err := os.WriteFile(permissive, []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	WarnIfPermissive(permissive)

	// Missing file: best-effort, returns without panic.
	WarnIfPermissive(filepath.Join(tmpDir, "missing.txt"))
}

func TestReadOwnerOnlyFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits do not map on Windows")
	}

	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		mode    os.FileMode
		wantErr bool
	}{
		{"0400 accepted", 0400, false},
		{"0600 accepted", 0600, false},
		{"0640 rejected", 0640, true},
		{"0644 rejected", 0644, true},
		{"0604 rejected", 0604, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filePath := filepath.Join(tmpDir, tt.name)
			if err := os.WriteFile(filePath, []byte("key"), 0600); err != nil {
				t.Fatalf("write: %v", err)
			}
			if err := os.Chmod(filePath, tt.mode); err != nil {
				t.Fatalf("chmod: %v", err)
			}

			data, err := ReadOwnerOnlyFile(filePath)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ReadOwnerOnlyFile(%04o) = nil error, want error",
						tt.mode)
				}
				return
			}
			if err != nil {
				t.Errorf("ReadOwnerOnlyFile(%04o) = %v, want nil", tt.mode, err)
				return
			}
			if string(data) != "key" {
				t.Errorf("ReadOwnerOnlyFile() = %q, want %q", data, "key")
			}
		})
	}
}

// TestReadOwnerOnlyFileReturnsRawBytes confirms the helper returns the
// file contents verbatim, performing no trimming of surrounding or
// interior whitespace.
func TestReadOwnerOnlyFileReturnsRawBytes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits do not map on Windows")
	}

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "raw.txt")
	content := "  raw\tcontent\n\n"
	if err := os.WriteFile(filePath, []byte(content), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, err := ReadOwnerOnlyFile(filePath)
	if err != nil {
		t.Fatalf("ReadOwnerOnlyFile() unexpected error: %v", err)
	}
	if string(data) != content {
		t.Errorf("ReadOwnerOnlyFile() = %q, want verbatim %q", data, content)
	}
}

func TestReadOwnerOnlyFileMissing(t *testing.T) {
	tmpDir := t.TempDir()
	_, err := ReadOwnerOnlyFile(filepath.Join(tmpDir, "nonexistent.txt"))
	if err == nil {
		t.Error("ReadOwnerOnlyFile() expected error for missing file")
	}
}

// TestReadOwnerOnlyFileReadError exercises the io.ReadAll failure
// branch. Opening a directory succeeds and its 0700 mode passes the
// owner-only check, but reading its bytes fails, so the helper must
// surface a read error rather than returning content.
func TestReadOwnerOnlyFileReadError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory read semantics differ on Windows")
	}

	dir := filepath.Join(t.TempDir(), "ownerdir")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	_, err := ReadOwnerOnlyFile(dir)
	if err == nil {
		t.Error("ReadOwnerOnlyFile() expected error reading a directory")
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

// redirectUserConfigDir points os.UserConfigDir() at a temporary
// directory by setting the platform-appropriate environment
// variable. The returned path is the directory the helper now
// resolves to; the test should create files under that location to
// simulate per-user configs.
func redirectUserConfigDir(t *testing.T) string {
	t.Helper()
	base := t.TempDir()

	// os.UserConfigDir consults different env vars per platform.
	// Setting all of them keeps the test portable and isolated from
	// whatever the developer happens to have in their environment.
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("HOME", base)
	t.Setenv("AppData", base)

	return base
}

// redirectSystemConfigDir overrides the package-level systemConfigDir
// pointer so the helper consults a writable temporary directory
// instead of the real /etc/pgedge. The override is reverted via
// t.Cleanup so test ordering is irrelevant.
func redirectSystemConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	prev := systemConfigDir
	systemConfigDir = dir
	t.Cleanup(func() { systemConfigDir = prev })
	return dir
}

// TestGetDefaultConfigPath_NothingExists verifies that the helper
// returns an empty string when no candidate config file is present.
// This is the fall-through-to-defaults branch.
func TestGetDefaultConfigPath_NothingExists(t *testing.T) {
	redirectUserConfigDir(t)
	redirectSystemConfigDir(t)

	name := "fileutil-test-nothing-exists.yaml"
	result := GetDefaultConfigPath("/usr/local/bin/myapp", name)

	if result != "" {
		t.Errorf("GetDefaultConfigPath() = %q, want empty string", result)
	}
}

// TestGetDefaultConfigPath_UserDirPreferred verifies that a file in
// the per-user config directory is returned even when the
// system-wide directory holds the same filename. The user path
// must win.
func TestGetDefaultConfigPath_UserDirPreferred(t *testing.T) {
	base := redirectUserConfigDir(t)
	sysDir := redirectSystemConfigDir(t)

	name := "fileutil-test-user-pref.yaml"

	userDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("os.UserConfigDir() error: %v", err)
	}
	pgedgeDir := filepath.Join(userDir, "pgedge")
	if err := os.MkdirAll(pgedgeDir, 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	userPath := filepath.Join(pgedgeDir, name)
	if err := os.WriteFile(userPath, []byte("user"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Place a competing file in the system dir; the user path
	// must still win.
	systemPath := filepath.Join(sysDir, name)
	if err := os.WriteFile(systemPath, []byte("system"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	result := GetDefaultConfigPath("/ignored/binary", name)
	if result != userPath {
		t.Errorf("GetDefaultConfigPath() = %q, want user path %q (base=%q)",
			result, userPath, base)
	}
}

// TestGetDefaultConfigPath_SystemPath verifies that the system
// config directory is consulted when no per-user config exists.
// The system directory is redirected to a temporary location so the
// branch is exercised regardless of whether /etc/pgedge is
// populated on the host.
func TestGetDefaultConfigPath_SystemPath(t *testing.T) {
	redirectUserConfigDir(t)
	sysDir := redirectSystemConfigDir(t)

	name := "fileutil-test-system-path.yaml"
	systemPath := filepath.Join(sysDir, name)
	if err := os.WriteFile(systemPath, []byte("system"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	result := GetDefaultConfigPath("/ignored/binary", name)
	if result != systemPath {
		t.Errorf("GetDefaultConfigPath() = %q, want system path %q",
			result, systemPath)
	}
}

// TestGetDefaultConfigPath_BinaryPathIgnored confirms the formerly
// load-bearing binary directory is no longer scanned. A file
// sitting next to the (fictional) binary must NOT be returned.
func TestGetDefaultConfigPath_BinaryPathIgnored(t *testing.T) {
	redirectUserConfigDir(t)
	redirectSystemConfigDir(t)

	binDir := t.TempDir()
	name := "fileutil-test-binary-ignored.yaml"
	next := filepath.Join(binDir, name)
	if err := os.WriteFile(next, []byte("legacy"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	binary := filepath.Join(binDir, "service")
	result := GetDefaultConfigPath(binary, name)
	if result == next {
		t.Errorf("GetDefaultConfigPath() returned binary-dir path %q; "+
			"binary-directory fallback was supposed to be removed",
			result)
	}
	if result != "" {
		t.Errorf("GetDefaultConfigPath() = %q, want empty string", result)
	}
}

// TestGetDefaultConfigPath_EmptyUserConfigDir exercises the rare
// branch where os.UserConfigDir() returns an error (HOME unset on
// Unix). The helper must skip the user-dir step and fall through to
// the system path without panicking. The system dir is redirected
// so the test does not depend on host state.
func TestGetDefaultConfigPath_EmptyUserConfigDir(t *testing.T) {
	// Clear every env var os.UserConfigDir() consults so it
	// returns an error on every platform.
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv("AppData", "")
	sysDir := redirectSystemConfigDir(t)

	name := "fileutil-test-no-userdir.yaml"

	// First, with no system file present: helper must return "".
	if got := GetDefaultConfigPath("/ignored/binary", name); got != "" {
		t.Errorf("GetDefaultConfigPath() = %q, want empty string", got)
	}

	// Then drop a file into the system dir: helper must return it.
	systemPath := filepath.Join(sysDir, name)
	if err := os.WriteFile(systemPath, []byte("system"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got := GetDefaultConfigPath("/ignored/binary", name); got != systemPath {
		t.Errorf("GetDefaultConfigPath() = %q, want system path %q",
			got, systemPath)
	}
}

func TestReadSecretFileDirectory(t *testing.T) {
	// Reading a directory should fail.
	tmpDir := t.TempDir()

	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdirectory: %v", err)
	}

	_, err := ReadSecretFile(subDir)
	if err == nil {
		t.Error("ReadSecretFile() should fail when reading a directory")
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

// TestSetSystemConfigDirForTest_RedirectsAndRestores verifies that
// SetSystemConfigDirForTest both redirects the system fallback for
// the lifetime of the surrounding test and restores the previous
// value via t.Cleanup so later tests are not affected.
func TestSetSystemConfigDirForTest_RedirectsAndRestores(t *testing.T) {
	original := systemConfigDir

	// Drop a candidate file at a temp dir and point the system
	// fallback at it. The per-user dir is also redirected at an
	// empty location so the helper falls through to the system
	// path deterministically.
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("HOME", base)
	t.Setenv("AppData", base)

	systemDir := filepath.Join(base, "fake-etc-pgedge")
	pgedgeSub := filepath.Join(systemDir, "pgedge")
	if err := os.MkdirAll(pgedgeSub, 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	t.Run("redirected", func(t *testing.T) {
		// systemConfigDir = systemDir, expect lookup to find the
		// dropped candidate file under it.
		SetSystemConfigDirForTest(t, systemDir)
		expected := filepath.Join(systemDir, "fake.yaml")
		if err := os.WriteFile(expected, []byte("k: v\n"), 0600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		got := GetDefaultConfigPath("", "fake.yaml")
		if got != expected {
			t.Errorf("GetDefaultConfigPath = %q, want %q", got, expected)
		}
	})

	// After the inner test (and its t.Cleanup) ran, the package
	// variable must have been restored to whatever it was before
	// we called SetSystemConfigDirForTest above (i.e. the original
	// value at the top of this test).
	if systemConfigDir != original {
		t.Errorf("systemConfigDir = %q, want restored value %q",
			systemConfigDir, original)
	}
}

// TestSetSystemConfigDirForTest_EmptyDirYieldsNoMatch verifies the
// "fully isolated" use case: pointing the system fallback at a
// directory that does not exist makes GetDefaultConfigPath return
// "" deterministically when neither the per-user dir nor the
// system dir holds a candidate file.
func TestSetSystemConfigDirForTest_EmptyDirYieldsNoMatch(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("HOME", base)
	t.Setenv("AppData", base)
	SetSystemConfigDirForTest(t, filepath.Join(base, "absent-etc-pgedge"))

	if got := GetDefaultConfigPath("", "ai-dba-server.yaml"); got != "" {
		t.Errorf("GetDefaultConfigPath = %q, want empty string", got)
	}
}
