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
	"io"
	"os"
	"path/filepath"
	"runtime"
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

// ReadSecretFile reads a secret (password, token, API key, or server
// secret) from an operator-supplied file path. It expands a leading
// tilde, warns (to stderr) if the file is group/world-readable, trims
// only a trailing newline sequence (preserving any in-secret
// whitespace), and returns an error if the resulting secret is empty.
//
// The trailing-newline trim uses strings.TrimRight(data, "\r\n") rather
// than TrimSpace so that secrets containing intentional leading,
// trailing, or interior spaces survive intact; only the line-ending a
// text editor appends is stripped.
func ReadSecretFile(path string) (string, error) {
	expandedPath, err := ExpandTildePath(path)
	if err != nil {
		return "", err
	}

	WarnIfPermissive(expandedPath)

	// #nosec G304 - File path is provided by administrator configuration
	data, err := os.ReadFile(expandedPath)
	if err != nil {
		return "", err
	}

	secret := strings.TrimRight(string(data), "\r\n")
	if secret == "" {
		return "", fmt.Errorf("secret file %s is empty", expandedPath)
	}

	return secret, nil
}

// WarnIfPermissive prints a stderr warning if the file is group- or
// world-readable (mode & 0o077 != 0). It never returns an error and is
// a no-op on Windows, where Unix mode bits do not map cleanly.
func WarnIfPermissive(path string) {
	if runtime.GOOS == "windows" {
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		// Stat failures are surfaced by the subsequent read; the
		// warning is best-effort only.
		return
	}

	if info.Mode().Perm()&0o077 != 0 {
		fmt.Fprintf(os.Stderr,
			"WARNING: secret file %s is group/world-accessible (%04o); "+
				"restrict it with: chmod 600 %s\n",
			path, info.Mode().Perm(), path)
	}
}

// ReadOwnerOnlyFile opens path, verifies on the open file descriptor
// that the file grants no group or world access (mode & 0o077 == 0),
// and returns its raw bytes. Checking permissions on the already-open
// descriptor and reading from that same descriptor closes the TOCTOU
// window that a separate os.Stat + os.ReadFile would leave. The
// permission check is skipped on Windows, where Unix mode bits do not
// map cleanly. Modes such as 0400 and 0600 pass; 0640, 0644, and
// friends are rejected.
func ReadOwnerOnlyFile(path string) ([]byte, error) {
	// #nosec G304 - path is administrator-supplied configuration
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open key file %s: %w", path, err)
	}
	defer f.Close()

	if runtime.GOOS != "windows" {
		info, err := f.Stat()
		if err != nil {
			return nil, fmt.Errorf("failed to stat key file %s: %w", path, err)
		}

		mode := info.Mode().Perm()
		if mode&0o077 != 0 {
			return nil, fmt.Errorf(
				"insecure permissions on key file %s: %04o (group/world access "+
					"not permitted). Please run: chmod 600 %s", path, mode, path)
		}
	}

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("failed to read key file %s: %w", path, err)
	}

	return data, nil
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
