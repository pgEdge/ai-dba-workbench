/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Tests for the Workbench-internal SQL marker on the scheduler's own
// query against monitored servers. See GitHub issue #364.
package scheduler

import (
	"context"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/pkg/sqlmarker"
)

// TestDatabaseListQueryIsTagged asserts the database enumeration query
// carries the marker in a position pg_stat_statements preserves.
func TestDatabaseListQueryIsTagged(t *testing.T) {
	if !strings.Contains(databaseListQuery, sqlmarker.Marker) {
		t.Fatalf("database list query is untagged: %s", databaseListQuery)
	}
	trimmed := strings.TrimLeft(databaseListQuery, " \t\r\n")
	if strings.HasPrefix(trimmed, sqlmarker.Comment) {
		t.Errorf("marker precedes the leading keyword, where "+
			"PostgreSQL strips it: %s", databaseListQuery)
	}
	if !strings.HasPrefix(trimmed, "SELECT "+sqlmarker.Comment) {
		t.Errorf("marker is not immediately after the keyword: %s",
			databaseListQuery)
	}
}

// TestSchedulerGetDatabaseList_QueryError covers the query-failure
// branch of getDatabaseList by closing the underlying connection before
// the query is issued.
func TestSchedulerGetDatabaseList_QueryError(t *testing.T) {
	f := setupIntegration(t)
	ps := NewProbeScheduler(
		f.ds, f.pool, integrationTestConfig(), testServerSecret)
	defer ps.Stop()

	mc := makeMonitoredConn(f)
	ctx := context.Background()
	conn, err := ps.poolManager.GetConnection(ctx, mc, testServerSecret)
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	defer ps.poolManager.ReturnConnection(mc.ID, conn)

	// Closing the underlying pgx connection makes the next Query fail
	// outright rather than failing mid-iteration.
	if err := conn.Conn().Close(ctx); err != nil {
		t.Fatalf("close underlying conn: %v", err)
	}

	if _, err := ps.getDatabaseList(ctx, conn); err == nil {
		t.Fatal("expected an error from a closed connection, got nil")
	} else if !strings.Contains(err.Error(), "failed to query pg_database") {
		t.Errorf("error = %v, want a pg_database query failure", err)
	}
}
