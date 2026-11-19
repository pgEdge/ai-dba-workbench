/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package mcp

import "context"

// CallMeta carries optional details a client supplies alongside a tool
// call: the chat conversation the call belongs to and the model and
// provider that produced it. Tools that record provenance, such as
// propose_action, read it from the call context; every field may be
// empty.
type CallMeta struct {
	ConversationID string
	Model          string
	Provider       string
}

type callMetaKey struct{}

// WithCallMeta returns a context carrying meta.
func WithCallMeta(ctx context.Context, meta CallMeta) context.Context {
	return context.WithValue(ctx, callMetaKey{}, meta)
}

// CallMetaFromContext returns the metadata stored by WithCallMeta, or the
// zero value when there is none.
func CallMetaFromContext(ctx context.Context) CallMeta {
	if meta, ok := ctx.Value(callMetaKey{}).(CallMeta); ok {
		return meta
	}
	return CallMeta{}
}
