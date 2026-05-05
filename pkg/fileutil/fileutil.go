/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package fileutil provides common file operations for reading configuration
// files, secrets, and other file-based data with support for tilde expansion
// and YAML parsing.
package fileutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ExpandTildePath expands a leading tilde (~) in a file path to the user's
// home directory. Returns the path unchanged if it does not start with tilde.
func ExpandTildePath(path string) (string, error) {
	if path == "" || path[0] != '~' {
		return path, nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	return filepath.Join(homeDir, path[1:]), nil
}

// ReadTrimmedFile reads a file and returns its contents with leading and
// trailing whitespace removed.
func ReadTrimmedFile(path string) (string, error) {
	// #nosec G304 - File path is provided by administrator configuration
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(content)), nil
}

// ReadTrimmedFileWithTilde reads a file after expanding any leading tilde
// in the path, then returns the contents with whitespace trimmed.
func ReadTrimmedFileWithTilde(path string) (string, error) {
	expandedPath, err := ExpandTildePath(path)
	if err != nil {
		return "", err
	}

	return ReadTrimmedFile(expandedPath)
}

// ReadOptionalTrimmedFile reads a file and returns its contents with
// whitespace trimmed. If the file does not exist, it returns an empty
// string without an error. The path is expanded if it starts with tilde.
func ReadOptionalTrimmedFile(path string) (string, error) {
	if path == "" {
		return "", nil
	}

	expandedPath, err := ExpandTildePath(path)
	if err != nil {
		return "", err
	}

	// Check if file exists
	if _, err := os.Stat(expandedPath); os.IsNotExist(err) {
		return "", nil
	}

	// #nosec G304 - File path is provided by administrator configuration
	content, err := os.ReadFile(expandedPath)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", expandedPath, err)
	}

	return strings.TrimSpace(string(content)), nil
}

// FileExists checks if a file exists at the given path.
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// systemConfigDir is the directory consulted as the system-wide
// fallback in GetDefaultConfigPath. It is a variable rather than a
// constant so tests can redirect it onto a temp directory and
// exercise the system-path branch on a host that does not have a
// real /etc/pgedge populated.
var systemConfigDir = "/etc/pgedge"

// SetSystemConfigDirForTest temporarily redirects the system-wide
// config directory consulted by GetDefaultConfigPath. It is intended
// for use by test code in this and other packages: the production
// default of /etc/pgedge can leak host state into tests on
// developer machines and CI runners that have a real config
// installed, making tests for the "no file found" branch
// non-deterministic. The helper installs a t.Cleanup that restores
// the previous value when the test (or its subtests) finishes.
//
// Pass an empty string to point the system fallback at a directory
// that is guaranteed not to exist, fully isolating tests from any
// real /etc/pgedge content. Pass a non-empty path (typically a
// t.TempDir()) to drive the system-path branch deterministically.
func SetSystemConfigDirForTest(t TestingT, dir string) {
	t.Helper()
	prev := systemConfigDir
	systemConfigDir = dir
	t.Cleanup(func() {
		systemConfigDir = prev
	})
}

// TestingT is the subset of *testing.T used by SetSystemConfigDirForTest.
// Defining it as an interface keeps fileutil free of a hard dependency
// on the testing package at production build time, while still making
// the helper callable from any *testing.T (and from *testing.B if a
// future caller needs it).
type TestingT interface {
	Helper()
	Cleanup(func())
}

// GetDefaultConfigPath returns the default config file path for a
// service. The function searches for an existing file in the
// following order, returning the first match:
//
//  1. The user config directory reported by os.UserConfigDir(),
//     under a "pgedge" subdirectory. On Linux this resolves to
//     ~/.config/pgedge/<configFilename>; on macOS it resolves to
//     ~/Library/Application Support/pgedge/<configFilename>; and
//     on Windows it resolves to %AppData%\pgedge\<configFilename>.
//
//  2. The system-wide path /etc/pgedge/<configFilename>.
//
// If neither candidate exists, the function returns "" so the
// caller can fall through to compiled-in defaults. Callers that
// need to require a config file must check the empty return value
// and act accordingly.
//
// The binaryPath parameter is no longer consulted; in earlier
// revisions the helper would silently pick up a file sitting next
// to the binary, which made it easy to load a development config
// in production. The parameter is retained for now so the three
// service callers continue to compile without churn; remove it
// when the call sites are next refactored.
//
// The configFilename parameter is the base name of the config file
// (e.g. "ai-dba-alerter.yaml" or "ai-dba-server.secret"). The
// helper applies the same precedence rules to secret-file lookups
// because the issue that motivated this change ("avoid silent
// prod-vs-dev confusion") applies equally to secrets.
func GetDefaultConfigPath(binaryPath, configFilename string) string {
	_ = binaryPath // intentionally unused; retained for caller stability

	// 1. Per-user XDG-style config directory. os.UserConfigDir only
	// fails when HOME (or its platform equivalent) is unset, which
	// is rare; in that case skip straight to the system path.
	if userDir, err := os.UserConfigDir(); err == nil {
		userPath := filepath.Join(userDir, "pgedge", configFilename)
		if _, statErr := os.Stat(userPath); statErr == nil {
			return userPath
		}
	}

	// 2. System-wide path under /etc/pgedge (overridable for tests).
	systemPath := filepath.Join(systemConfigDir, configFilename)
	if _, err := os.Stat(systemPath); err == nil {
		return systemPath
	}

	// 3. Nothing matched. Signal "fall through to defaults" with
	// an empty string rather than guessing at a synthetic path.
	return ""
}

// LoadYAMLFile reads a YAML file and unmarshals its contents into the
// provided value. The value must be a pointer to the target structure.
func LoadYAMLFile(path string, v any) error {
	// #nosec G304 - Config file path is provided by administrator
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	return yaml.Unmarshal(data, v)
}

// LoadOptionalYAMLFile reads a YAML file and unmarshals its contents into
// the provided value. If the file does not exist, it returns nil without
// modifying the value.
func LoadOptionalYAMLFile(path string, v any) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}

	return LoadYAMLFile(path, v)
}
