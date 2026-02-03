//go:build unix

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
	"context"
	"os"
	"time"

	"golang.org/x/term"
)

// ListenForEscape monitors stdin for the Escape key and calls cancel when detected.
// This allows users to cancel LLM requests by pressing Escape.
// Unix implementation uses raw terminal mode to detect keypresses immediately.
func ListenForEscape(ctx context.Context, done chan struct{}, cancel context.CancelFunc) {
	// Save the current terminal state
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		// Can't enter raw mode (not a TTY), just wait for done
		select {
		case <-done:
		case <-ctx.Done():
		}
		return
	}

	// Restore terminal on exit
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	buf := make([]byte, 1)
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		default:
			// Use platform-specific poll with short timeout
			if stdinHasData(50 * time.Millisecond) {
				n, err := os.Stdin.Read(buf)
				if err != nil || n == 0 {
					continue
				}
				if buf[0] == KeyEscape {
					cancel()
					return
				}
			}
		}
	}
}
