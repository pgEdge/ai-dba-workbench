//go:build darwin

/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
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

// stdinHasData uses kqueue (macOS) to check if stdin has data available.
// This approach is more efficient than polling and works reliably on macOS.
func stdinHasData(timeout time.Duration) bool {
	kq, err := unix.Kqueue()
	if err != nil {
		return false
	}
	defer unix.Close(kq)

	// Set up a kevent to watch stdin for read availability
	changes := []unix.Kevent_t{
		{
			Ident:  uint64(os.Stdin.Fd()),
			Filter: unix.EVFILT_READ,
			Flags:  unix.EV_ADD | unix.EV_ONESHOT,
		},
	}

	events := make([]unix.Kevent_t, 1)

	// Convert timeout to timespec
	timeoutSpec := unix.NsecToTimespec(timeout.Nanoseconds())

	n, err := unix.Kevent(kq, changes, events, &timeoutSpec)
	if err != nil {
		return false
	}

	return n > 0 && events[0].Filter == unix.EVFILT_READ
}
