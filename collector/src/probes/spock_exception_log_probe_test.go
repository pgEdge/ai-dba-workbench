/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package probes

import (
	"context"
	"strings"
	"testing"
	"time"
)

// newSpockExceptionLogProbeForTest constructs a probe with a minimal,
// deterministic configuration suitable for unit and integration tests.
// The returned probe is database-scoped, matching production behaviour.
func newSpockExceptionLogProbeForTest() *SpockExceptionLogProbe {
	return NewSpockExceptionLogProbe(&ProbeConfig{
		Name:                      ProbeNameSpockExceptionLog,
		CollectionIntervalSeconds: 60,
		RetentionDays:             7,
		IsEnabled:                 true,
	})
}

// TestSpockExceptionLogProbe_Surface verifies the probe's static
// metadata: name, table name, scope, and that GetQuery returns SQL
// that targets spock.exception_log over the rolling 15-minute window
// described in the design spec.
func TestSpockExceptionLogProbe_Surface(t *testing.T) {
	p := newSpockExceptionLogProbeForTest()

	if got := p.GetName(); got != ProbeNameSpockExceptionLog {
		t.Errorf("GetName() = %q, want %q",
			got, ProbeNameSpockExceptionLog)
	}
	if got := p.GetTableName(); got != ProbeNameSpockExceptionLog {
		t.Errorf("GetTableName() = %q, want %q",
			got, ProbeNameSpockExceptionLog)
	}
	if !p.IsDatabaseScoped() {
		t.Error("IsDatabaseScoped() = false, want true " +
			"(spock_exception_log is a per-database catalog)")
	}

	q := p.GetQuery()
	for _, s := range []string{
		"spock.exception_log",
		"retry_errored_at",
		"remote_origin",
		"error_message",
		"interval '15 minutes'",
	} {
		if !strings.Contains(q, s) {
			t.Errorf("GetQuery missing substring %q; query was:\n%s",
				s, q)
		}
	}
}

// TestSpockExceptionLogProbe_StoreEmpty asserts that Store returns
// nil with a nil/empty metrics slice and never touches the
// datastore connection. This is the no-op fast path that lets the
// scheduler call Store unconditionally even when Execute returned
// nothing (e.g. Spock not installed, or no exceptions in the window).
func TestSpockExceptionLogProbe_StoreEmpty(t *testing.T) {
	p := newSpockExceptionLogProbeForTest()
	if err := p.Store(context.Background(), nil, 1, time.Now(),
		nil); err != nil {
		t.Errorf("Store(nil) = %v, want nil", err)
	}
	// Empty slice should also be a no-op.
	if err := p.Store(context.Background(), nil, 1, time.Now(),
		[]map[string]any{}); err != nil {
		t.Errorf("Store([]) = %v, want nil", err)
	}
}

// TestSpockExceptionLogProbe_ExecuteWhenSpockAbsent verifies the
// graceful-skip behaviour: when the Spock extension is not installed
// in the connected database, Execute returns (nil, nil) rather than
// raising an error or attempting to query a missing catalog. This
// branch is hit on every collection cycle for non-Spock databases
// and must not log noise or churn the connection pool.
func TestSpockExceptionLogProbe_ExecuteWhenSpockAbsent(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	pgVersion := detectPgVersion(t, conn)

	p := newSpockExceptionLogProbeForTest()
	ctx := context.Background()

	metrics, err := p.Execute(ctx, "no-spock", conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
	if metrics != nil {
		t.Errorf("Execute() returned %d metrics; want nil "+
			"(integration database has no Spock extension)",
			len(metrics))
	}
}
