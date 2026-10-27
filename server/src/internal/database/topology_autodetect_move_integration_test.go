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
	"testing"
)

// clusterOf returns the stored cluster_id of a connection, or 0 when it
// is NULL.
func (f *moveFixture) clusterOf(t *testing.T, conn int) int {
	t.Helper()
	var id *int
	if err := f.pool.QueryRow(context.Background(),
		"SELECT cluster_id FROM connections WHERE id = $1", conn).Scan(&id); err != nil {
		t.Fatalf("Read cluster_id: %v", err)
	}
	if id == nil {
		return 0
	}
	return *id
}

// exec runs a setup statement on the fixture's pool.
func (f *moveFixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("Setup %q: %v", sql, err)
	}
}

// TestSyncAutoDetectedClusterAssignments_DropsOldClusterRelationships
// covers issue #719: when auto-detection moves a connection to another
// cluster, its relationship rows in the old cluster go with the move,
// as they do for AssignConnectionToCluster (issue #707).
func TestSyncAutoDetectedClusterAssignments_DropsOldClusterRelationships(t *testing.T) {
	ctx := context.Background()
	f := newMoveFixture(t)
	defer f.cleanup()

	var groupID int
	if err := f.pool.QueryRow(ctx,
		"INSERT INTO cluster_groups (name, is_default) VALUES ('default', TRUE) RETURNING id",
	).Scan(&groupID); err != nil {
		t.Fatalf("Insert group: %v", err)
	}
	f.exec(t, "UPDATE clusters SET auto_cluster_key = 'binary:new' WHERE id = $1", f.newCluster)

	ids := f.ds.syncAutoDetectedClusterAssignments(ctx, map[string]TopologyCluster{
		"binary:new": {
			Name:        "move-new",
			ClusterType: "binary",
			Servers: []TopologyServerInfo{
				{ID: f.moved, PrimaryRole: "binary_primary"},
			},
		},
	}, groupID)

	if ids["binary:new"] != f.newCluster {
		t.Fatalf("cluster id map = %v, want binary:new -> %d", ids, f.newCluster)
	}
	if got := f.clusterOf(t, f.moved); got != f.newCluster {
		t.Errorf("cluster_id = %d, want %d", got, f.newCluster)
	}
	f.assertOldRowsDropped(t, 1)
}

// TestSyncAutoDetectedClusterAssignments_Skips covers the paths that
// leave the connection where it is: a dismissed cluster, a failed
// cluster upsert, and failed connection updates, which are logged.
func TestSyncAutoDetectedClusterAssignments_Skips(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		setup   string
		wantIDs int
	}{
		{
			name:  "dismissed cluster",
			setup: "UPDATE clusters SET dismissed = TRUE WHERE auto_cluster_key = 'binary:new'",
		},
		{
			name:  "upsert fails",
			setup: "ALTER TABLE clusters RENAME COLUMN dismissed TO hidden",
		},
		{
			name:    "connection updates fail",
			setup:   "ALTER TABLE connections DROP COLUMN role",
			wantIDs: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newMoveFixture(t)
			defer f.cleanup()
			f.exec(t, "UPDATE clusters SET auto_cluster_key = 'binary:new' WHERE id = $1", f.newCluster)
			f.exec(t, tt.setup)

			ids := f.ds.syncAutoDetectedClusterAssignments(ctx, map[string]TopologyCluster{
				"binary:new": {
					Name:        "move-new",
					ClusterType: "binary",
					Servers: []TopologyServerInfo{
						{ID: f.moved, PrimaryRole: "binary_primary"},
					},
				},
			}, 0)

			if len(ids) != tt.wantIDs {
				t.Errorf("cluster id map = %v, want %d entries", ids, tt.wantIDs)
			}
			if got := f.clusterOf(t, f.moved); got != f.oldCluster {
				t.Errorf("cluster_id = %d, want %d", got, f.oldCluster)
			}
			f.assertUnchanged(t)
		})
	}
}

func TestAssignAutoDetectedConnection(t *testing.T) {
	ctx := context.Background()

	t.Run("auto member moves and drops old rows", func(t *testing.T) {
		f := newMoveFixture(t)
		defer f.cleanup()
		if err := f.ds.assignAutoDetectedConnection(ctx, f.newCluster, f.moved, "binary_standby"); err != nil {
			t.Fatalf("assignAutoDetectedConnection: %v", err)
		}
		if got := f.clusterOf(t, f.moved); got != f.newCluster {
			t.Errorf("cluster_id = %d, want %d", got, f.newCluster)
		}
		f.assertOldRowsDropped(t, 1)
	})

	t.Run("unassigned connection drops rows outside the new cluster", func(t *testing.T) {
		f := newMoveFixture(t)
		defer f.cleanup()
		f.exec(t, "UPDATE connections SET cluster_id = NULL WHERE id = $1", f.moved)
		if err := f.ds.assignAutoDetectedConnection(ctx, f.newCluster, f.moved, "binary_standby"); err != nil {
			t.Fatalf("assignAutoDetectedConnection: %v", err)
		}
		f.assertOldRowsDropped(t, 1)
	})

	// Rows left in another cluster by an earlier move are not swept up
	// when detection only reconfirms the current cluster.
	t.Run("reconfirmed member keeps its rows", func(t *testing.T) {
		f := newMoveFixture(t)
		defer f.cleanup()
		if err := f.ds.assignAutoDetectedConnection(ctx, f.oldCluster, f.moved, "binary_primary"); err != nil {
			t.Fatalf("assignAutoDetectedConnection: %v", err)
		}
		var role string
		if err := f.pool.QueryRow(ctx,
			"SELECT role FROM connections WHERE id = $1", f.moved).Scan(&role); err != nil {
			t.Fatalf("Read role: %v", err)
		}
		if role != "binary_primary" {
			t.Errorf("role = %q, want binary_primary", role)
		}
		f.assertUnchanged(t)
	})

	t.Run("manual member of another cluster is left alone", func(t *testing.T) {
		f := newMoveFixture(t)
		defer f.cleanup()
		f.exec(t, "UPDATE connections SET membership_source = 'manual' WHERE id = $1", f.moved)
		if err := f.ds.assignAutoDetectedConnection(ctx, f.newCluster, f.moved, "binary_standby"); err != nil {
			t.Fatalf("assignAutoDetectedConnection: %v", err)
		}
		if got := f.clusterOf(t, f.moved); got != f.oldCluster {
			t.Errorf("cluster_id = %d, want the manual %d", got, f.oldCluster)
		}
		f.assertUnchanged(t)
	})

	t.Run("deleted connection is left alone", func(t *testing.T) {
		f := newMoveFixture(t)
		defer f.cleanup()
		f.exec(t, "UPDATE connections SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1", f.moved)
		if err := f.ds.assignAutoDetectedConnection(ctx, f.newCluster, f.moved, "binary_standby"); err != nil {
			t.Fatalf("assignAutoDetectedConnection: %v", err)
		}
		if got := f.clusterOf(t, f.moved); got != f.oldCluster {
			t.Errorf("cluster_id = %d, want %d", got, f.oldCluster)
		}
		f.assertUnchanged(t)
	})
}

// TestAssignAutoDetectedConnection_Failures covers the error branches;
// each leaves the connection and its rows as they were.
func TestAssignAutoDetectedConnection_Failures(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name  string
		setup string
	}{
		{
			name:  "lock fails",
			setup: "ALTER TABLE connections RENAME COLUMN membership_source TO source",
		},
		{
			name:  "update fails",
			setup: "ALTER TABLE connections DROP COLUMN role",
		},
		{
			name: "relationship delete fails",
			setup: `ALTER TABLE cluster_node_relationships
			        RENAME COLUMN target_connection_id TO target_id`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newMoveFixture(t)
			defer f.cleanup()
			f.exec(t, tt.setup)
			if err := f.ds.assignAutoDetectedConnection(ctx, f.newCluster, f.moved, "binary_standby"); err == nil {
				t.Fatal("assignAutoDetectedConnection returned nil; want error")
			}
			if got := f.clusterOf(t, f.moved); got != f.oldCluster {
				t.Errorf("cluster_id = %d after a failure, want %d", got, f.oldCluster)
			}
		})
	}

	t.Run("commit fails", func(t *testing.T) {
		f := newMoveFixture(t)
		defer f.cleanup()
		f.exec(t, `
			CREATE FUNCTION fail_autodetect_move() RETURNS trigger
			LANGUAGE plpgsql AS $$
			BEGIN
				RAISE EXCEPTION 'injected commit failure';
			END $$;
			CREATE CONSTRAINT TRIGGER fail_autodetect_move
				AFTER UPDATE ON connections
				DEFERRABLE INITIALLY DEFERRED
				FOR EACH ROW EXECUTE FUNCTION fail_autodetect_move();
		`)
		defer func() {
			_, _ = f.pool.Exec(context.Background(),
				`DROP FUNCTION IF EXISTS fail_autodetect_move() CASCADE`)
		}()
		if err := f.ds.assignAutoDetectedConnection(ctx, f.newCluster, f.moved, "binary_standby"); err == nil {
			t.Fatal("assignAutoDetectedConnection with a failing commit returned nil; want error")
		}
		if got := f.clusterOf(t, f.moved); got != f.oldCluster {
			t.Errorf("cluster_id = %d after a failed commit, want %d", got, f.oldCluster)
		}
		f.assertUnchanged(t)
	})

	t.Run("begin fails", func(t *testing.T) {
		ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
		cleanup()
		_ = pool
		if err := ds.assignAutoDetectedConnection(ctx, 1, 1, "binary_primary"); err == nil {
			t.Fatal("assignAutoDetectedConnection against a closed pool returned nil; want error")
		}
	})
}
