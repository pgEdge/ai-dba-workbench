//go:build windows

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
	"time"
)

// ListenForEscape is a stub for Windows.
// Escape key detection is not currently supported on Windows.
// Users can use Ctrl+C instead.
func ListenForEscape(ctx context.Context, done chan struct{}, cancel context.CancelFunc) {
	// On Windows, just wait for done or context cancellation
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// stdinHasData is a stub for Windows.
// Returns false immediately as non-blocking stdin check is complex on Windows.
func stdinHasData(timeout time.Duration) bool {
	return false
}
