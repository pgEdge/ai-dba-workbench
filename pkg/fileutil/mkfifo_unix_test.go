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

import "syscall"

// mkfifoForTest creates a named pipe (FIFO) at path. It is only built on
// non-Windows platforms, where syscall.Mkfifo is available.
func mkfifoForTest(path string) error {
	return syscall.Mkfifo(path, 0600)
}
