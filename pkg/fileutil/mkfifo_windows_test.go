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

import "errors"

// mkfifoForTest is unsupported on Windows; callers skip the FIFO tests
// when this stub returns an error.
func mkfifoForTest(_ string) error {
	return errors.New("mkfifo not supported on windows")
}
