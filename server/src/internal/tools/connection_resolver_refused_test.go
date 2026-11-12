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
//
// The hostaddr is a comma-separated list naming the test database's own
// host twice. Without the stored-target check that list reaches pgx as a
// valid multi-host URI and connects to the test database, so the test
// fails; only the check, applied when the connection string is built,
// refuses it. (A value with brackets in it, such as
// "127.0.0.1]:6379,[127.0.0.1", is no use here: the client manager
// refuses the malformed URI itself, so it is refused with the same
// message whether or not the check runs.)
func TestResolveExplicit_RefusesUnsafeStoredTarget(t *testing.T) {
	env := newResolverTestEnv(t)
	if env.host == "" || strings.HasPrefix(env.host, "/") {
		t.Skipf("test database host %q is not a TCP host", env.host)
	}
	ctx := context.WithValue(context.Background(),
		auth.IsSuperuserContextKey, true)

	hostile := env.host + "," + env.host
	if _, err := env.pool.Exec(ctx,
		"UPDATE connections SET hostaddr = $1 WHERE id = $2",
		hostile, env.connID); err != nil {
		t.Fatalf("set hostaddr: %v", err)
	}

	resolved, resp := env.resolver.Resolve(ctx, deniedActionConnectionQuery,
		map[string]any{"connection_id": float64(env.connID)}, nil)
	if resolved != nil {
		t.Fatalf("resolved a connection through the hostaddr list %q", hostile)
	}
	if resp == nil || !resp.IsError {
		t.Fatalf("expected an error response, got %v", resp)
	}
	got := toolResponseText(resp)
	if got != "Failed to establish database connection" {
		t.Errorf("unexpected error text %q", got)
	}
	if strings.Contains(got, hostile) {
		t.Errorf("response %q echoes the stored hostaddr", got)
	}
}
