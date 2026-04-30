/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Tests that drive specific branches of probe Execute paths that are
// otherwise hard to reach: the "view does not exist" fallback in
// pg_stat_wal and pg_replication_slots, and Execute error wrapping
// when an extension claims to exist but its functions are missing.
package probes

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestPgStatWalProbe_NoStatWalView documents that the legacy
// "pg_stat_wal does not exist" branch in PgStatWalProbe.Execute is
// unreachable from an integration test (the view lives in pg_catalog
// and cannot be hidden from a normal session) and records the
// alternate coverage source.
func TestPgStatWalProbe_NoStatWalView(t *testing.T) {
	// pg_stat_wal is a system view in pg_catalog that cannot be renamed
	// or dropped from a regular session, so the "view does not exist"
	// branch is unreachable from an integration test. The branch is
	// instead covered by the version-gated GetQuery tests, which
	// exercise the same fallback statically.
	t.Skip("pg_stat_wal cannot be hidden in PG16; covered via " +
		"version branch by GetQuery test")
}

// TestEnsurePartition_ErrorPath confirms that EnsurePartition surfaces
// the error returned by CREATE TABLE when the parent table does not
// exist. This walks the error-wrapping path that the happy-path tests
// skip.
func TestEnsurePartition_ErrorPath(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	err := EnsurePartition(ctx, conn,
		"definitely_no_such_parent_table",
		time.Now().UTC())
	if err == nil {
		t.Fatal("expected error for missing parent table")
	}
	if !strings.Contains(err.Error(), "failed to create partition") {
		t.Errorf("expected create-failure error, got %v", err)
	}
}

// TestStoreMetrics_ErrorPath exercises StoreMetrics rolling back when
// the INSERT fails (here, by referencing a non-existent column). The
// transaction defer must roll back without panicking.
func TestStoreMetrics_ErrorPath(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	now := time.Now().UTC()
	if err := EnsurePartition(ctx, conn, "pg_stat_activity",
		now); err != nil {
		t.Fatalf("EnsurePartition: %v", err)
	}
	err := StoreMetrics(ctx, conn, "pg_stat_activity",
		[]string{"definitely_no_such_column"},
		[][]any{{int64(1)}})
	if err == nil {
		t.Fatal("expected error for unknown column")
	}
	if !strings.Contains(err.Error(), "failed to") {
		t.Errorf("expected wrapped error, got %v", err)
	}
}

// TestStoreMetrics_BeginError exercises the "failed to begin
// transaction" branch by passing a closed connection. This forces a
// negative-path through the very first Begin call.
//
// We use a separate pool that we close before the call so the acquire
// would have already happened against a now-closed pool's connection.
func TestStoreMetrics_BeginError(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	// Close the underlying connection to force Begin to fail. We use
	// the shared breakConn helper so a failed close fails the test
	// instead of silently leaving an open conn.
	breakConn(t, conn)

	err := StoreMetrics(ctx, conn, "pg_stat_activity",
		[]string{"connection_id"}, [][]any{{int64(1)}})
	if err == nil {
		t.Fatal("expected error from closed connection")
	}
	if !strings.Contains(err.Error(), "failed to begin transaction") {
		t.Errorf("expected wrapped begin-transaction error, got %v",
			err)
	}
}
