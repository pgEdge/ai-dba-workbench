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
	"testing"
	"time"
)

// TestTimeline_PurgedConnectionOnViews is the regression test for the
// timeline total on a pgEdge ColdFront datastore (issue #755), where
// metrics.pg_settings is a view whose rows the purge leaves in place.
// Once a deleted connection's row is purged its configuration changes
// are still in the view; the event query drops them by joining live
// connections, so the count must drop them too, rather than only those
// of a connection still marked.
func TestTimeline_PurgedConnectionOnViews(t *testing.T) {
	e := newCSEnv(t)

	// The view's store keeps pg_settings' columns but no foreign key, so
	// nothing cascades from the connection, as with ColdFront's Iceberg
	// storage.
	e.exec(`CREATE TABLE pg_settings_cf_store (LIKE metrics.pg_settings)`)
	e.exec(`DROP TABLE metrics.pg_settings CASCADE`)
	e.exec(`CREATE VIEW metrics.pg_settings AS SELECT * FROM pg_settings_cf_store`)

	kept := e.conn("kept", "192.0.2.21", "", nil, nil)
	purged := e.conn("purged", "192.0.2.22", "", nil, nil)
	for _, id := range []int{kept, purged} {
		for i, ago := range []string{"2 hours", "1 hour"} {
			e.exec(`INSERT INTO pg_settings_cf_store (connection_id, name, setting, collected_at)
                    VALUES ($1, 'work_mem', $2, now() - $3::text::interval)`,
				id, []string{"4MB", "8MB"}[i], ago)
		}
	}

	if err := e.ds.DeleteConnection(e.ctx, purged); err != nil {
		t.Fatalf("DeleteConnection: %v", err)
	}
	if err := e.ds.PurgeConnection(e.ctx, purged, 100); err != nil {
		t.Fatalf("PurgeConnection: %v", err)
	}
	if n := e.queryInt(`SELECT count(*) FROM connections WHERE id = $1`, purged); n != 0 {
		t.Fatalf("the purged connection still has %d rows", n)
	}
	if n := e.queryInt(`SELECT count(*) FROM metrics.pg_settings WHERE connection_id = $1`, purged); n != 2 {
		t.Fatalf("the view holds %d rows of the purged connection, want 2 left in place", n)
	}

	result, err := e.ds.GetTimelineEvents(e.ctx, TimelineFilter{
		StartTime:  time.Now().Add(-24 * time.Hour),
		EndTime:    time.Now(),
		EventTypes: []string{EventTypeConfigChange},
		Limit:      100,
	})
	if err != nil {
		t.Fatalf("GetTimelineEvents: %v", err)
	}
	if len(result.Events) != 1 || result.Events[0].ConnectionID != kept {
		t.Fatalf("events = %+v, want the kept connection's one change", result.Events)
	}
	if result.TotalCount != len(result.Events) {
		t.Errorf("TotalCount = %d, events = %d; the purged connection's change was counted",
			result.TotalCount, len(result.Events))
	}
}
