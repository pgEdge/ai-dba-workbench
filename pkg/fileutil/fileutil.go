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

// MaxSecretFileSize is the maximum size, in bytes, that the secret and
// key readers in this package will read from a single file. Secrets,
// tokens, API keys, and encryption keys are all comfortably under a few
// kilobytes, so a 1 MiB ceiling is generous while still bounding the
// memory a single file read can allocate. Files larger than this limit
// are rejected outright rather than truncated, so an oversized or
// runaway file is treated as a configuration error.
const MaxSecretFileSize = 1 << 20 // 1 MiB

// maxSecretFileSize is the effective ceiling enforced by
// readRegularFileBounded. It defaults to MaxSecretFileSize and is a
// variable only so tests can lower it to exercise the size-rejection
// and post-read overflow guards deterministically without allocating
// multi-megabyte fixtures. Production code never reassigns it.
var maxSecretFileSize int64 = MaxSecretFileSize

// openFileForRead is the open primitive used by readRegularFileBounded.
// It is a variable wrapping openNonBlocking so tests can substitute an
// open that yields a descriptor whose Stat fails, exercising the
// otherwise-unreachable stat-error guard. Production code never
// reassigns it.
var openFileForRead = openNonBlocking

// readRegularFileBounded opens path, verifies on the open descriptor
// that the target is a regular file (rejecting FIFOs, devices,
// directories, and symlinks resolving to non-regular files), enforces
// the MaxSecretFileSize ceiling on the bytes actually read, and returns
// the contents read from that same descriptor. Performing the stat, the
// regular-file check, the optional fd-based check, and the read on a
// single descriptor closes the TOCTOU window that a separate os.Stat +
// os.ReadFile would leave open.
//
// The size ceiling is enforced with an io.LimitReader capped at the
// limit plus one byte, so a file that grows after the stat (or a
// pseudo-file that under-reports its size) is rejected rather than read
// without bound or silently truncated.
//
// The check closure, when non-nil, runs against the open descriptor
// after the regular-file check passes but before the read, so
// additional fd-based validation (such as a permission check) shares
// the same TOCTOU-safe descriptor. The file is closed before the bytes
// are returned to the caller.
func readRegularFileBounded(path string, check func(info os.FileInfo) error) ([]byte, error) {
	// Open non-blocking where the platform supports it. A plain
	// os.Open of a FIFO blocks until a writer appears, which would
	// hang startup on a misconfigured path; opening O_NONBLOCK lets
	// the regular-file check below reject the FIFO immediately.
	f, err := openFileForRead(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat file %s: %w", path, err)
	}

	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf(
			"file %s is not a regular file (mode %s); refusing to read",
			path, info.Mode())
	}

	if check != nil {
		if err := check(info); err != nil {
			return nil, err
		}
	}

	// Read with a hard cap of maxSecretFileSize+1 so the limit is
	// enforced authoritatively on the bytes actually read. Relying on
	// the bounded read rather than the advisory info.Size() means a
	// file that grows after the stat, or a pseudo-file that under-
	// reports its size, is still rejected rather than read unbounded or
	// silently truncated.
	limit := maxSecretFileSize
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", path, err)
	}

	if int64(len(data)) > limit {
		return nil, fmt.Errorf(
			"file %s is too large: exceeds the %d-byte limit",
			path, limit)
	}

	return data, nil
}

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
// whitespace), and returns an error if the resulting secret is empty or
// contains only whitespace.
//
// The read goes through readRegularFileBounded, so a path that points
// at a FIFO, device, directory, or other non-regular file is rejected
// (rather than hanging startup), and a file larger than
// MaxSecretFileSize is refused rather than read into unbounded memory.
//
// The trailing-newline trim uses strings.TrimRight(data, "\r\n") rather
// than TrimSpace so that secrets containing intentional leading,
// trailing, or interior spaces survive intact; only the line-ending a
// text editor appends is stripped. The emptiness check uses
// strings.TrimSpace so a file holding only whitespace (spaces, tabs, or
// newlines) is rejected rather than yielding a useless whitespace-only
// secret; the value RETURNED, however, is the TrimRight("\r\n") result,
// so meaningful leading, trailing, or interior whitespace in a real
// secret is preserved unchanged. Only the validation trims whitespace.
func ReadSecretFile(path string) (string, error) {
	expandedPath, err := ExpandTildePath(path)
	if err != nil {
		return "", err
	}

	// The permissive-mode warning runs as the check closure rather than
	// before the read, so it fires only after readRegularFileBounded has
	// confirmed the target is a regular file. A path pointing at a
	// directory or FIFO is rejected without emitting a misleading
	// "chmod 600" warning, and the warning reuses the descriptor's stat
	// info rather than performing a second os.Stat. The closure is
	// advisory only and never returns an error.
	data, err := readRegularFileBounded(expandedPath,
		func(info os.FileInfo) error {
			warnIfPermissiveInfo(expandedPath, info)
			return nil
		})
	if err != nil {
		return "", err
	}

	secret := strings.TrimRight(string(data), "\r\n")
	if strings.TrimSpace(secret) == "" {
		return "", fmt.Errorf(
			"secret file %s is empty or contains only whitespace",
			expandedPath)
	}

	return secret, nil
}

// WarnIfPermissive prints a stderr warning if the file is group- or
// world-readable (mode & 0o077 != 0). It never returns an error and is
// a no-op on Windows, where Unix mode bits do not map cleanly. It
// expands a leading tilde (reusing ExpandTildePath) before the stat so a
// "~"-relative path warns consistently with ReadSecretFile and
// ReadOwnerOnlyFile, then stats the expanded path and delegates to
// warnIfPermissiveInfo; callers that already hold a FileInfo (for
// example from an open descriptor) should call warnIfPermissiveInfo
// directly to avoid a redundant stat.
func WarnIfPermissive(path string) {
	if runtime.GOOS == "windows" {
		return
	}

	expandedPath, err := ExpandTildePath(path)
	if err != nil {
		// Tilde expansion failures (HOME unset) are surfaced by the
		// subsequent read; the warning is advisory only.
		return
	}

	info, err := os.Stat(expandedPath)
	if err != nil {
		// Stat failures are surfaced by the subsequent read; the
		// warning is best-effort only.
		return
	}

	warnIfPermissiveInfo(expandedPath, info)
}

// warnIfPermissiveInfo prints a stderr warning if the supplied FileInfo
// describes a group- or world-readable file (mode & 0o077 != 0). It is a
// no-op on Windows, where Unix mode bits do not map cleanly, and never
// returns an error: the warning is advisory only. Accepting an existing
// FileInfo lets callers that already hold one (such as the descriptor
// stat inside readRegularFileBounded) warn without a second os.Stat,
// which both avoids a redundant syscall and ensures the warning reflects
// the same TOCTOU-safe stat used for the read.
func warnIfPermissiveInfo(path string, info os.FileInfo) {
	if runtime.GOOS == "windows" {
		return
	}

	if info.Mode().Perm()&0o077 != 0 {
		fmt.Fprintf(os.Stderr,
			"WARNING: secret file %s is group/world-accessible (%04o); "+
				"restrict it with: chmod 600 %s\n",
			path, info.Mode().Perm(), path)
	}
}

// ReadOwnerOnlyFile reads path and returns its raw bytes after
// verifying, on the open file descriptor, that the target is a regular
// file no larger than MaxSecretFileSize. On non-Windows platforms it
// additionally requires that the file grant no group or world access
// (mode & 0o077 == 0). The regular-file check, size check, permission
// check, and read all happen on the same open descriptor, closing the
// TOCTOU window that a separate os.Stat + os.ReadFile would leave.
//
// On non-Windows platforms, owner-only modes such as 0400 and 0600
// pass while 0640, 0644, and friends are rejected. On Windows the
// Unix mode bits do not map cleanly, so the permission check is
// skipped; only the regular-file and size checks apply there.
//
// A leading tilde is expanded (reusing ExpandTildePath) before the
// file is opened, so a "~"-relative key path resolves consistently
// with ReadSecretFile. The expanded path is used in the permission-
// error and read-error messages so they show the resolved location.
//
// The raw bytes are returned without trimming.
func ReadOwnerOnlyFile(path string) ([]byte, error) {
	expandedPath, err := ExpandTildePath(path)
	if err != nil {
		return nil, err
	}

	check := func(info os.FileInfo) error {
		if runtime.GOOS == "windows" {
			return nil
		}

		mode := info.Mode().Perm()
		if mode&0o077 != 0 {
			return fmt.Errorf(
				"insecure permissions on key file %s: %04o (group/world access "+
					"not permitted). Please run: chmod 600 %s",
				expandedPath, mode, expandedPath)
		}
		return nil
	}

	data, err := readRegularFileBounded(expandedPath, check)
	if err != nil {
		return nil, fmt.Errorf("failed to read key file %s: %w",
			expandedPath, err)
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
