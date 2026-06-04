//go:build windows

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

import "os"

// openNonBlocking opens path read-only. Windows has no O_NONBLOCK and
// the named-pipe semantics that motivate it on Unix do not apply to the
// regular config and secret files this package reads, so a plain open is
// sufficient here.
func openNonBlocking(path string) (*os.File, error) {
	// #nosec G304 - File path is provided by administrator configuration
	return os.Open(path)
}
