//go:build !windows

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
	"syscall"
)

// openNonBlocking opens path read-only with O_NONBLOCK so that a path
// resolving to a FIFO does not block waiting for a writer. The caller is
// expected to verify on the returned descriptor that the target is a
// regular file before reading. Clearing the file's status flags after
// the open is unnecessary for regular files, which ignore O_NONBLOCK.
func openNonBlocking(path string) (*os.File, error) {
	// #nosec G304 - File path is provided by administrator configuration
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
