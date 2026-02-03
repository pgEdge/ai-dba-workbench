//go:build linux

/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package chat

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// stdinHasData uses poll (Linux) to check if stdin has data available.
// This approach is more efficient than busy-waiting and works reliably on Linux.
func stdinHasData(timeout time.Duration) bool {
	fds := []unix.PollFd{
		{
			Fd:     int32(os.Stdin.Fd()),
			Events: unix.POLLIN,
		},
	}

	// Convert timeout to milliseconds for poll
	timeoutMs := int(timeout.Milliseconds())
	if timeoutMs < 1 {
		timeoutMs = 1
	}

	n, err := unix.Poll(fds, timeoutMs)
	if err != nil {
		return false
	}

	return n > 0 && (fds[0].Revents&unix.POLLIN) != 0
}
