/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
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

// LoadYAMLFile reads a YAML file and unmarshals its contents into the
// provided value. The value must be a pointer to the target structure.
func LoadYAMLFile(path string, v interface{}) error {
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
func LoadOptionalYAMLFile(path string, v interface{}) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}

	return LoadYAMLFile(path, v)
}
