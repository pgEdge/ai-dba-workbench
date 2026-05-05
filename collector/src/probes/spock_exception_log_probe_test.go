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
// The returned probe is database-scoped, matching production behavior.
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
// graceful-skip behavior: when the Spock extension is not installed
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

// TestSpockExceptionLogProbe_GetExtensionName documents the
// extension dependency advertised to the scheduler so operator-
// facing reasons ("Spock not installed") stay in sync with the
// runtime guard inside Execute.
func TestSpockExceptionLogProbe_GetExtensionName(t *testing.T) {
	p := newSpockExceptionLogProbeForTest()
	if got := p.GetExtensionName(); got != "spock" {
		t.Errorf("GetExtensionName() = %q, want %q", got, "spock")
	}
}

// TestSpockExceptionLogProbe_ExecuteWithStubSpock drives the Spock-
// installed branch of Execute by registering a stub spock extension
// and creating a minimal spock.exception_log table that matches the
// columns the probe selects. With the schema in place Execute should
// succeed and return an empty (non-nil) slice when no rows fall
// inside the rolling 15-minute window. We then insert a single row
// and verify it round-trips through Execute and Store.
func TestSpockExceptionLogProbe_ExecuteWithStubSpock(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	if spockAlreadyInstalled(t, ctx, conn) {
		t.Skip("spock extension already installed; this test " +
			"creates a stub spock and would otherwise need to " +
			"destroy a real one to clean up")
	}

	stmts := []string{
		`CREATE SCHEMA IF NOT EXISTS spock`,
		`CREATE TABLE IF NOT EXISTS spock.exception_log (
			remote_origin OID NOT NULL,
			remote_commit_ts TIMESTAMPTZ NOT NULL,
			command_counter INTEGER NOT NULL,
			retry_errored_at TIMESTAMPTZ NOT NULL,
			remote_xid BIGINT NOT NULL,
			local_origin OID,
			local_commit_ts TIMESTAMPTZ,
			table_schema TEXT,
			table_name TEXT,
			operation TEXT,
			local_tup JSONB,
			remote_old_tup JSONB,
			remote_new_tup JSONB,
			ddl_statement TEXT,
			ddl_user TEXT,
			error_message TEXT
		)`,
		`INSERT INTO pg_extension (oid, extname, extowner,
			extnamespace, extrelocatable, extversion,
			extconfig, extcondition)
			SELECT (SELECT MAX(oid::oid::int)
				FROM pg_extension) + 1, 'spock', 10,
				(SELECT oid FROM pg_namespace
					WHERE nspname='spock'),
				TRUE, '1.0', NULL, NULL
			WHERE NOT EXISTS (SELECT 1 FROM pg_extension
				WHERE extname='spock')`,
	}
	for _, s := range stmts {
		if _, err := conn.Exec(ctx, s); err != nil {
			t.Fatalf("setup spock stub: %v\n%s", err, s)
		}
	}
	t.Cleanup(func() {
		if _, err := conn.Exec(ctx,
			"DELETE FROM pg_extension "+
				"WHERE extname='spock'"); err != nil {
			t.Logf("cleanup pg_extension spock row: %v", err)
		}
		if _, err := conn.Exec(ctx,
			"DROP SCHEMA spock CASCADE"); err != nil {
			t.Logf("cleanup spock schema: %v", err)
		}
	})

	p := newSpockExceptionLogProbeForTest()

	// Empty-window case: no rows yet, but the query and scan path
	// must succeed and return a non-error result.
	metrics, err := p.Execute(ctx, "with-spock", conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute (empty window): %v", err)
	}
	if len(metrics) != 0 {
		t.Errorf("Execute (empty window) returned %d metrics, "+
			"want 0", len(metrics))
	}

	// Insert a synthetic row that falls inside the 15-minute window.
	if _, err := conn.Exec(ctx, `
		INSERT INTO spock.exception_log (
			remote_origin, remote_commit_ts, command_counter,
			retry_errored_at, remote_xid, local_origin,
			local_commit_ts, table_schema, table_name, operation,
			local_tup, remote_old_tup, remote_new_tup,
			ddl_statement, ddl_user, error_message)
		VALUES (
			16384, now(), 1, now(), 12345, 16385,
			now(), 'public', 't1', 'INSERT',
			'{"a":1}'::jsonb, NULL, '{"a":2}'::jsonb,
			NULL, NULL, 'apply failed: dup key')
	`); err != nil {
		t.Fatalf("insert exception_log row: %v", err)
	}

	metrics, err = p.Execute(ctx, "with-spock", conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute (one row): %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("Execute returned %d rows, want 1", len(metrics))
	}
	if got := metrics[0]["error_message"]; got !=
		"apply failed: dup key" {
		t.Errorf("error_message = %v, want %q",
			got, "apply failed: dup key")
	}

	// The scheduler injects _database_name on every row before
	// Store is called. Mirror that here so Store can populate the
	// destination database_name column without errors.
	for i := range metrics {
		metrics[i]["_database_name"] = "stub-db"
	}

	// Store the row to metrics.spock_exception_log to exercise the
	// happy path of the COPY/INSERT pipeline. The integration
	// helper schema includes this table so partition creation and
	// the column ordering above are both verified end-to-end.
	if err := p.Store(ctx, conn, 1, time.Now().UTC(),
		metrics); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Confirm at least one row landed in the destination table.
	var stored int
	if err := conn.QueryRow(ctx, `
		SELECT COUNT(*) FROM metrics.spock_exception_log
		 WHERE connection_id = 1
	`).Scan(&stored); err != nil {
		t.Fatalf("count stored rows: %v", err)
	}
	if stored != 1 {
		t.Errorf("metrics.spock_exception_log count = %d, want 1",
			stored)
	}
}

// TestSpockExceptionLogProbe_ExecuteCheckExtensionError exercises the
// CheckExtensionExists error branch by passing a connection that has
// already been released. pgxpool returns an error for any query
// attempted on a released connection; the probe must wrap and
// return that error rather than swallowing it.
func TestSpockExceptionLogProbe_ExecuteCheckExtensionError(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	// Force the underlying *pgx.Conn into a state that fails the
	// next query. Canceling the context is the cheapest way to
	// guarantee QueryRow returns an error without disturbing the
	// shared pool.
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()

	p := newSpockExceptionLogProbeForTest()
	if _, err := p.Execute(cancelCtx, "canceled", conn,
		pgVersion); err == nil {
		t.Fatal("Execute with canceled context: expected error, " +
			"got nil")
	}
}

// TestSpockExceptionLogProbe_StoreEnsurePartitionError drives the
// EnsurePartition error branch of Store by passing a canceled
// context with a real datastore connection. EnsurePartition issues
// SQL through the connection, which fails immediately when the
// context is already canceled, causing Store to surface the wrapped
// "failed to ensure partition" error.
func TestSpockExceptionLogProbe_StoreEnsurePartitionError(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()

	metrics := []map[string]any{
		{
			"remote_origin":    uint32(1),
			"remote_commit_ts": time.Now(),
			"command_counter":  1,
			"retry_errored_at": time.Now(),
			"remote_xid":       int64(1),
			"local_origin":     nil,
			"local_commit_ts":  nil,
			"table_schema":     "public",
			"table_name":       "t",
			"operation":        "INSERT",
			"local_tup":        nil,
			"remote_old_tup":   nil,
			"remote_new_tup":   nil,
			"ddl_statement":    nil,
			"ddl_user":         nil,
			"error_message":    "boom",
		},
	}

	p := newSpockExceptionLogProbeForTest()
	err := p.Store(cancelCtx, conn, 1, time.Now(), metrics)
	if err == nil {
		t.Fatal("Store(canceled ctx) expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to ensure partition") {
		t.Errorf("Store error = %q; want it to mention "+
			"'failed to ensure partition'", err.Error())
	}
}
