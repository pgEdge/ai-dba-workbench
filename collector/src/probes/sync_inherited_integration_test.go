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
	"testing"
)

// TestSyncInheritedProbeConfigs exercises the reload-time sync (issue
// #810) against session temporary tables, which take precedence over
// the minimal shared tables other tests in this package create, so the
// fixture cannot leak into them. It checks that a copied server row
// follows its nearest parent (cluster before group before global), that
// an overridden row is left alone, and that a second run changes
// nothing.
func TestSyncInheritedProbeConfigs(t *testing.T) {
	pool := helperPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	if _, err := conn.Exec(ctx, `
		CREATE TEMP TABLE probe_configs (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			collection_interval_seconds INTEGER NOT NULL DEFAULT 300,
			retention_days INTEGER NOT NULL DEFAULT 30,
			is_enabled BOOLEAN NOT NULL DEFAULT TRUE,
			scope TEXT NOT NULL,
			connection_id INTEGER,
			cluster_id INTEGER,
			group_id INTEGER,
			user_modified BOOLEAN NOT NULL DEFAULT FALSE,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE TEMP TABLE connections (id INTEGER PRIMARY KEY, cluster_id INTEGER);
		CREATE TEMP TABLE clusters (id INTEGER PRIMARY KEY, group_id INTEGER);
		INSERT INTO clusters VALUES (10, 100), (11, 101);
		INSERT INTO connections VALUES (1, NULL), (2, 10), (3, 11), (4, NULL);
		INSERT INTO probe_configs (name, scope, is_enabled, collection_interval_seconds, retention_days)
			VALUES ('p', 'global', FALSE, 60, 7);
		INSERT INTO probe_configs (name, scope, cluster_id, collection_interval_seconds)
			VALUES ('p', 'cluster', 10, 120);
		INSERT INTO probe_configs (name, scope, group_id, collection_interval_seconds)
			VALUES ('p', 'group', 101, 240);
		INSERT INTO probe_configs (name, scope, connection_id, user_modified)
			VALUES ('p', 'server', 1, FALSE), ('p', 'server', 2, FALSE),
			       ('p', 'server', 3, FALSE), ('p', 'server', 4, TRUE);
	`); err != nil {
		t.Fatalf("seed temporary tables: %v", err)
	}
	t.Cleanup(func() {
		if _, err := conn.Exec(context.Background(),
			`DROP TABLE pg_temp.probe_configs, pg_temp.connections, pg_temp.clusters`); err != nil {
			t.Errorf("drop temporary tables: %v", err)
		}
	})

	changed, err := SyncInheritedProbeConfigs(ctx, conn)
	if err != nil {
		t.Fatalf("SyncInheritedProbeConfigs: %v", err)
	}
	if changed != 3 {
		t.Errorf("first sync changed %d rows, want 3", changed)
	}

	want := map[int]struct {
		enabled  bool
		interval int
		days     int
	}{
		1: {false, 60, 7},  // global
		2: {true, 120, 30}, // cluster
		3: {true, 240, 30}, // group
		4: {true, 300, 30}, // override, untouched
	}
	for connID, w := range want {
		var enabled bool
		var interval, days int
		if err := conn.QueryRow(ctx, `
			SELECT is_enabled, collection_interval_seconds, retention_days
			FROM probe_configs WHERE scope = 'server' AND connection_id = $1`,
			connID).Scan(&enabled, &interval, &days); err != nil {
			t.Fatalf("read connection %d: %v", connID, err)
		}
		if enabled != w.enabled || interval != w.interval || days != w.days {
			t.Errorf("connection %d = (%v, %d, %d), want (%v, %d, %d)",
				connID, enabled, interval, days, w.enabled, w.interval, w.days)
		}
	}

	changed, err = SyncInheritedProbeConfigs(ctx, conn)
	if err != nil {
		t.Fatalf("second SyncInheritedProbeConfigs: %v", err)
	}
	if changed != 0 {
		t.Errorf("second sync changed %d rows, want 0", changed)
	}
}
