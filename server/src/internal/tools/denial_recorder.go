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
	"strconv"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
)

// DenialRecorder writes a tool call refused for a missing permission to
// the audit log, as the REST handlers' recorder does for a refused
// request (issue #674). Action, target and reason must come from the
// fixed vocabulary below, never from tool arguments, so that a caller
// cannot inflate the coalescing key or the audit row; a target may
// carry a connection ID only as an integer the tool has parsed. A nil recorder audits
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

// The audit actions recorded for refused tool calls. A refusal is
// recorded under the action the call would have used had it been
// allowed, using the names the REST routes record for the same refusal
// (issue #706), so that a denial reads the same whichever interface it
// came through. Reads below a connection, such as its schema, alert
// thresholds, alert history, blackouts or timeline, are recorded as
// reads of the connection, as REST records them.
const (
	deniedActionToolCall                = "mcp.tool.call"
	deniedActionMemoryCreate            = "memory.create"
	deniedActionConnectionRead          = "connection.read"
	deniedActionConnectionQuery         = "connection.query"
	deniedActionConnectionQueryValidate = "connection.query.validate"
	deniedActionConnectionQueryWrite    = "connection.query.write"
	deniedActionMetricsRead             = "metrics.read"
)

// The reasons recorded for refused tool calls. The reason is part of the
// denial coalescer's key, so it never names the tool or the connection:
// a caller probing many tools or connection IDs would otherwise open a
// fresh key, and write a fresh audit row, for each one. The object goes
// in the row's target instead. The connection reasons are those the
// REST routes record (issue #675), so that both interfaces coalesce
// alike.
const (
	toolAccessDenied       = "Access denied: you do not have permission to use this tool"
	connectionAccessDenied = "Permission denied: you do not have access to this connection"
	connectionWriteDenied  = "Permission denied: you do not have write access to this connection"
)

// The denial targets that name no single object. unmatchedToolTarget is
// recorded for a refused call to a name that is not a registered tool,
// as REST records an unmatched route, so that the caller's text never
// reaches the log.
const (
	memoriesTarget      = "memories"
	unmatchedToolTarget = "unmatched"
)

// connectionDenialTarget names a connection as a denial target, in the
// connections/<id> form the REST routes record. The ID is an integer
// the tool has already parsed, so no text from the call reaches the
// log; an ID that is not positive names the bare collection, as the
// REST denialResourceTarget does.
func connectionDenialTarget(connectionID int) string {
	if connectionID <= 0 {
		return "connections"
	}
	return "connections/" + strconv.Itoa(connectionID)
}

// deny audits a refusal, when a recorder is set, for a caller that
// answers with its own message rather than with the reason, such as a
// connection refusal, whose message must not tell a denied connection
// from a missing one.
func (record DenialRecorder) deny(ctx context.Context, action, target,
	reason string) {

	if record != nil {
		record(ctx, action, target, reason)
	}
}

// refuse audits the refusal, when a recorder is set, and returns reason
// as the tool error.
func (record DenialRecorder) refuse(ctx context.Context, action, target,
	reason string) (mcp.ToolResponse, error) {

	record.deny(ctx, action, target, reason)
	return mcp.NewToolError(reason)
}
