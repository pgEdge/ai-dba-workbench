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
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
)

// testRBACChecker returns a real RBAC checker backed by a temporary auth
// store. A nil checker denies everything (issue #561), so tests that
// exercise a tool's behavior past its access check use this together
// with asSuperuser rather than passing nil.
func testRBACChecker(t *testing.T) *auth.RBACChecker {
	t.Helper()
	store, cleanup := newRBACRegressionTestStore(t)
	t.Cleanup(cleanup)
	return auth.NewRBACChecker(store)
}

// superuserContext returns a context that the auth middleware would build
// for a signed-in superuser.
func superuserContext() context.Context {
	ctx := context.WithValue(context.Background(), auth.IsSuperuserContextKey, true)
	ctx = context.WithValue(ctx, auth.UserIDContextKey, int64(1))
	return context.WithValue(ctx, auth.UsernameContextKey, "admin")
}

// asSuperuser wraps tool so that every call without an explicit
// "__context" argument runs as a superuser, which a real checker admits
// to every connection.
func asSuperuser(tool Tool) Tool {
	next := tool.Handler
	tool.Handler = func(args map[string]any) (mcp.ToolResponse, error) {
		if _, ok := args["__context"]; !ok {
			args["__context"] = superuserContext()
		}
		return next(args)
	}
	return tool
}
