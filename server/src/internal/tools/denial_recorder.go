/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package tools

import (
	"context"
	"log"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
)

// DenialRecorder writes a tool call refused for a missing permission to
// the audit log, as the REST handlers' recorder does for a refused
// request (issue #674). Action and target must come from a fixed
// vocabulary, never from tool arguments, so that a caller cannot
// inflate the coalescing key or the audit row. A nil recorder audits
// nothing, which is the case when the server runs without an auth
// store.
type DenialRecorder func(ctx context.Context, action, target, reason string)

// NewDenialRecorder returns a DenialRecorder that records through the
// auth store's denial coalescer, attributing the refusal to the
// principal in the tool call's context, or nil for a nil store.
func NewDenialRecorder(store *auth.AuthStore) DenialRecorder {
	if store == nil {
		return nil
	}
	return func(ctx context.Context, action, target, reason string) {
		if err := store.RecordCoalescedDenial(auth.ActorFromContext(ctx),
			action, target, reason); err != nil {
			log.Printf("[ERROR] Failed to record tool denial for %s: %v",
				action, err)
		}
	}
}

// refuse audits the refusal, when a recorder is set, and returns reason
// as the tool error.
func (record DenialRecorder) refuse(ctx context.Context, action, target,
	reason string) (mcp.ToolResponse, error) {

	if record != nil {
		record(ctx, action, target, reason)
	}
	return mcp.NewToolError(reason)
}
