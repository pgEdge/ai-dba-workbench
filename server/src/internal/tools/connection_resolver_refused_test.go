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
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// TestResolveExplicit_RefusesUnsafeStoredTarget checks that a stored
// hostaddr which would add a second target to the connection string is
// refused before any connection attempt, with the generic message.
func TestResolveExplicit_RefusesUnsafeStoredTarget(t *testing.T) {
	env := newResolverTestEnv(t)
	ctx := context.WithValue(context.Background(),
		auth.IsSuperuserContextKey, true)

	const hostile = "127.0.0.1]:6379,[127.0.0.1"
	if _, err := env.pool.Exec(ctx,
		"UPDATE connections SET hostaddr = $1 WHERE id = $2",
		hostile, env.connID); err != nil {
		t.Fatalf("set hostaddr: %v", err)
	}

	resolved, resp := env.resolver.Resolve(ctx, deniedActionConnectionQuery,
		map[string]any{"connection_id": float64(env.connID)}, nil)
	if resolved != nil || resp == nil || !resp.IsError {
		t.Fatalf("expected an error response, got %v, %v", resolved, resp)
	}
	got := toolResponseText(resp)
	if got != "Failed to establish database connection" {
		t.Errorf("unexpected error text %q", got)
	}
	if strings.Contains(got, hostile) {
		t.Errorf("response %q echoes the stored hostaddr", got)
	}
}
