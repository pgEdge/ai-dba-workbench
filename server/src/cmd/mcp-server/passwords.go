/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package main

import (
	"fmt"
	"os"

	"github.com/pgedge/ai-workbench/pkg/fileutil"
)

// PasswordSource indicates how a password was resolved.
type PasswordSource int

const (
	// PasswordSourceNone means no password was provided.
	PasswordSourceNone PasswordSource = iota
	// PasswordSourceFlag means the password came from a CLI flag.
	PasswordSourceFlag
	// PasswordSourceFile means the password was read from a file.
	PasswordSourceFile
)

// PasswordResult holds a resolved password and its source.
type PasswordResult struct {
	Value  string
	Source PasswordSource
}

// ResolvePassword determines a password value using the following
// precedence: CLI flag (highest), file, none.
// When the CLI flag is used, a deprecation warning is printed to
// stderr because command-line arguments are visible in process
// listings.
//
// When a file path is supplied, the read is delegated to
// fileutil.ReadSecretFile, which expands a leading tilde, warns on
// group/world-readable permissions, trims only the trailing newline,
// and reports an empty file as an error. There is no directory-traversal
// guard: the path is operator-supplied configuration, and legitimate
// relative paths (including those containing "..") must be honored.
func ResolvePassword(flagValue string, flagSet bool, filePath string) (PasswordResult, error) {
	// 1. CLI flag (highest precedence, but deprecated)
	if flagSet && flagValue != "" {
		fmt.Fprintf(os.Stderr,
			"WARNING: Passing passwords via command-line flags is insecure "+
				"and deprecated. Use the corresponding -password-file flag instead.\n")
		return PasswordResult{Value: flagValue, Source: PasswordSourceFlag}, nil
	}

	// 2. Password file
	if filePath != "" {
		password, err := fileutil.ReadSecretFile(filePath)
		if err != nil {
			return PasswordResult{}, fmt.Errorf("reading password file %s: %w", filePath, err)
		}
		return PasswordResult{Value: password, Source: PasswordSourceFile}, nil
	}

	return PasswordResult{}, nil
}
