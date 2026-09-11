/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package database

import (
	"context"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/pkg/sqlmarker"
)

// seedProbeAvailabilityConnection clears the connections table and
// inserts a single monitored connection, returning its ID.
func seedProbeAvailabilityConnection(t *testing.T, ds *Datastore) int {
	t.Helper()

	conn, err := ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection() error = %v", err)
	}
	defer ds.ReturnConnection(conn)

	ctx := context.Background()
	if _, err := conn.Exec(ctx, "DELETE FROM connections"); err != nil {
		t.Fatalf("cleanup error = %v", err)
	}

	var id int
	err = conn.QueryRow(ctx, `
		INSERT INTO connections
			(name, host, port, database_name, username, owner_username,
			 is_monitored)
		VALUES ('probe-avail', 'h', 5432, 'd', 'u', 'o', TRUE)
		RETURNING id
	`).Scan(&id)
	if err != nil {
		t.Fatalf("seed error = %v", err)
	}
	return id
}

// TestUpsertProbeAvailability covers the insert path, the ON CONFLICT
// update path, and the nil-databaseName coalescing that makes the
// unique constraint match for server-level probes.
func TestUpsertProbeAvailability(t *testing.T) {
	ds := newDatastoreForTest(t)
	connectionID := seedProbeAvailabilityConnection(t, ds)

	conn, err := ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection() error = %v", err)
	}
	defer ds.ReturnConnection(conn)

	ctx := context.Background()
	dbName := "appdb"
	extName := "pg_stat_statements"
	reason := "extension missing"

	// Insert: available, with a database name.
	if err := UpsertProbeAvailability(ctx, conn, connectionID, &dbName,
		"pg_stat_statements", &extName, true, nil); err != nil {
		t.Fatalf("UpsertProbeAvailability(insert) error = %v", err)
	}

	var rows int
	var isAvailable bool
	var lastCollected *string
	err = conn.QueryRow(ctx, `
		SELECT count(*), bool_and(is_available),
		       max(last_collected)::text
		FROM probe_availability
		WHERE connection_id = $1 AND probe_name = 'pg_stat_statements'
	`, connectionID).Scan(&rows, &isAvailable, &lastCollected)
	if err != nil {
		t.Fatalf("readback error = %v", err)
	}
	if rows != 1 || !isAvailable {
		t.Fatalf("rows = %d, isAvailable = %v; want 1, true",
			rows, isAvailable)
	}
	if lastCollected == nil {
		t.Error("expected last_collected to be set for an available probe")
	}

	// Upsert the same key as unavailable: the row must be updated in
	// place and last_collected preserved.
	if err := UpsertProbeAvailability(ctx, conn, connectionID, &dbName,
		"pg_stat_statements", &extName, false, &reason); err != nil {
		t.Fatalf("UpsertProbeAvailability(update) error = %v", err)
	}

	var gotReason *string
	var stillCollected *string
	err = conn.QueryRow(ctx, `
		SELECT count(*), bool_and(is_available), max(unavailable_reason),
		       max(last_collected)::text
		FROM probe_availability
		WHERE connection_id = $1 AND probe_name = 'pg_stat_statements'
	`, connectionID).Scan(&rows, &isAvailable, &gotReason, &stillCollected)
	if err != nil {
		t.Fatalf("readback error = %v", err)
	}
	if rows != 1 {
		t.Errorf("rows = %d, want 1 after the upsert", rows)
	}
	if isAvailable {
		t.Error("expected is_available to be false after the upsert")
	}
	if gotReason == nil || *gotReason != reason {
		t.Errorf("unavailable_reason = %v, want %q", gotReason, reason)
	}
	if stillCollected == nil {
		t.Error("expected last_collected to be preserved")
	}

	// A nil database name must be coalesced to an empty string so the
	// unique constraint still matches on a second call.
	for i := 0; i < 2; i++ {
		if err := UpsertProbeAvailability(ctx, conn, connectionID, nil,
			"pg_server_info", nil, true, nil); err != nil {
			t.Fatalf("UpsertProbeAvailability(server-level) error = %v", err)
		}
	}
	err = conn.QueryRow(ctx, `
		SELECT count(*) FROM probe_availability
		WHERE connection_id = $1 AND probe_name = 'pg_server_info'
	`, connectionID).Scan(&rows)
	if err != nil {
		t.Fatalf("readback error = %v", err)
	}
	if rows != 1 {
		t.Errorf("rows = %d, want 1 for the server-level probe", rows)
	}
}

// TestUpsertProbeAvailability_Error covers the error return by pointing
// the upsert at a connection ID that violates the foreign key.
func TestUpsertProbeAvailability_Error(t *testing.T) {
	ds := newDatastoreForTest(t)
	seedProbeAvailabilityConnection(t, ds)

	conn, err := ds.GetConnection()
	if err != nil {
		t.Fatalf("GetConnection() error = %v", err)
	}
	defer ds.ReturnConnection(conn)

	err = UpsertProbeAvailability(context.Background(), conn, 999_999,
		nil, "pg_server_info", nil, true, nil)
	if err == nil {
		t.Fatal("expected a foreign key violation, got nil")
	}
}

// TestProbeAvailabilityUpsertIsTagged asserts the upsert statement
// carries the Workbench-internal marker, in the position that
// pg_stat_statements preserves. See GitHub issue #364.
func TestProbeAvailabilityUpsertIsTagged(t *testing.T) {
	if !strings.Contains(probeAvailabilityUpsert, sqlmarker.Marker) {
		t.Fatalf("probe availability upsert is untagged: %s",
			probeAvailabilityUpsert)
	}
	trimmed := strings.TrimLeft(probeAvailabilityUpsert, " \t\r\n")
	if strings.HasPrefix(trimmed, sqlmarker.Comment) {
		t.Errorf("marker precedes the leading keyword, where "+
			"PostgreSQL strips it: %s", probeAvailabilityUpsert)
	}
	if !strings.HasPrefix(trimmed, "INSERT "+sqlmarker.Comment) {
		t.Errorf("marker is not immediately after the keyword: %s",
			probeAvailabilityUpsert)
	}
}

// TestConnectionQueriesAreTagged asserts the collector's recurring reads
// and writes against its own connections table carry the marker.
func TestConnectionQueriesAreTagged(t *testing.T) {
	cases := map[string]string{
		"monitoredConnectionsQuery":    monitoredConnectionsQuery,
		"monitoredConnectionByIDQuery": monitoredConnectionByIDQuery,
		"setConnectionErrorStatement":  setConnectionErrorStatement,
	}

	for name, sql := range cases {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(sql, sqlmarker.Marker) {
				t.Fatalf("%s is untagged: %s", name, sql)
			}
			trimmed := strings.TrimLeft(sql, " \t\r\n")
			if strings.HasPrefix(trimmed, sqlmarker.Comment) {
				t.Errorf("%s carries the marker before the leading "+
					"keyword: %s", name, sql)
			}
		})
	}
}
