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
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// moveFixture is a connection that is a member of cluster "old", with
// relationship rows in old (manual and auto-detected, as source and as
// target), a row in "new" that already names it, and a row in old
// between two other nodes that a move must leave alone.
type moveFixture struct {
	ds          *Datastore
	pool        *pgxpool.Pool
	oldCluster  int
	newCluster  int
	moved       int
	peer        int
	other       int
	cleanupFunc func()
}

func newMoveFixture(t *testing.T) *moveFixture {
	t.Helper()
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		"ALTER TABLE connections ADD COLUMN role VARCHAR(64)"); err != nil {
		cleanup()
		t.Fatalf("Add role column: %v", err)
	}

	f := &moveFixture{ds: ds, pool: pool, cleanupFunc: cleanup}
	f.oldCluster = insertSyncTestCluster(t, pool, "move-old")
	f.newCluster = insertSyncTestCluster(t, pool, "move-new")
	f.moved = insertSyncTestConnection(t, pool, "moved")
	f.peer = insertSyncTestConnection(t, pool, "peer")
	f.other = insertSyncTestConnection(t, pool, "other")

	if _, err := pool.Exec(ctx,
		"UPDATE connections SET cluster_id = $1 WHERE id = ANY($2)",
		f.oldCluster, []int{f.moved, f.peer, f.other}); err != nil {
		cleanup()
		t.Fatalf("Place connections in the old cluster: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO cluster_node_relationships
			(cluster_id, source_connection_id, target_connection_id,
			 relationship_type, is_auto_detected)
		VALUES ($1, $3, $4, 'streams_from', FALSE),
		       ($1, $4, $3, 'replicates_with', TRUE),
		       ($1, $5, $3, 'subscribes_to', FALSE),
		       ($1, $4, $5, 'streams_from', FALSE),
		       ($2, $3, $4, 'replicates_with', FALSE)
	`, f.oldCluster, f.newCluster, f.moved, f.peer, f.other); err != nil {
		cleanup()
		t.Fatalf("Seed relationships: %v", err)
	}
	return f
}

func (f *moveFixture) cleanup() { f.cleanupFunc() }

// rowsInvolving counts the rows in a cluster where conn is the source
// or the target.
func (f *moveFixture) rowsInvolving(t *testing.T, clusterID, conn int) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM cluster_node_relationships
		WHERE cluster_id = $1
		  AND (source_connection_id = $2 OR target_connection_id = $2)
	`, clusterID, conn).Scan(&n); err != nil {
		t.Fatalf("Count relationships: %v", err)
	}
	return n
}

// assertOldRowsDropped checks the outcome of a move out of the old
// cluster: the moved node's rows there are gone, the peers' own row
// survives, and the row in the new cluster is kept.
func (f *moveFixture) assertOldRowsDropped(t *testing.T, keptInNew int) {
	t.Helper()
	if got := f.rowsInvolving(t, f.oldCluster, f.moved); got != 0 {
		t.Errorf("rows naming the moved node in the old cluster = %d, want 0", got)
	}
	if !hasRelationship(t, f.pool, f.oldCluster, f.peer, f.other, "streams_from", false) {
		t.Error("the row between the remaining nodes of the old cluster was deleted")
	}
	if got := f.rowsInvolving(t, f.newCluster, f.moved); got != keptInNew {
		t.Errorf("rows naming the moved node in the new cluster = %d, want %d", got, keptInNew)
	}
}

// assertUnchanged checks that a failed or no-op call left every row.
func (f *moveFixture) assertUnchanged(t *testing.T) {
	t.Helper()
	if got := f.rowsInvolving(t, f.oldCluster, f.moved); got != 3 {
		t.Errorf("rows naming the moved node in the old cluster = %d, want 3", got)
	}
	if got := f.rowsInvolving(t, f.newCluster, f.moved); got != 1 {
		t.Errorf("rows naming the moved node in the new cluster = %d, want 1", got)
	}
}

// TestAssignConnectionToCluster_DropsOldClusterRelationships covers
// issue #707: moving a connection removes its relationship rows,
// manual and auto-detected, as source and as target, from every other
// cluster, and keeps its rows in the destination.
func TestAssignConnectionToCluster_DropsOldClusterRelationships(t *testing.T) {
	ctx := context.Background()

	t.Run("move to another cluster", func(t *testing.T) {
		f := newMoveFixture(t)
		defer f.cleanup()
		if err := f.ds.AssignConnectionToCluster(ctx, f.moved, &f.newCluster, nil, "manual"); err != nil {
			t.Fatalf("AssignConnectionToCluster: %v", err)
		}
		f.assertOldRowsDropped(t, 1)
	})

	t.Run("unassign", func(t *testing.T) {
		f := newMoveFixture(t)
		defer f.cleanup()
		if err := f.ds.AssignConnectionToCluster(ctx, f.moved, nil, nil, "auto"); err != nil {
			t.Fatalf("AssignConnectionToCluster: %v", err)
		}
		f.assertOldRowsDropped(t, 0)
	})

	t.Run("same cluster keeps rows", func(t *testing.T) {
		f := newMoveFixture(t)
		defer f.cleanup()
		if err := f.ds.AssignConnectionToCluster(ctx, f.moved, &f.oldCluster, nil, "manual"); err != nil {
			t.Fatalf("AssignConnectionToCluster: %v", err)
		}
		if got := f.rowsInvolving(t, f.oldCluster, f.moved); got != 3 {
			t.Errorf("rows in the unchanged cluster = %d, want 3", got)
		}
	})

	t.Run("missing cluster keeps rows", func(t *testing.T) {
		f := newMoveFixture(t)
		defer f.cleanup()
		missing := f.newCluster + 1000
		err := f.ds.AssignConnectionToCluster(ctx, f.moved, &missing, nil, "manual")
		if !errors.Is(err, ErrClusterNotFound) {
			t.Fatalf("Expected ErrClusterNotFound, got %v", err)
		}
		f.assertUnchanged(t)
	})
}

// TestAddServerToCluster_DropsOldClusterRelationships covers the same
// rule for POST /api/v1/clusters/{id}/servers, which moves a member of
// another cluster.
func TestAddServerToCluster_DropsOldClusterRelationships(t *testing.T) {
	ctx := context.Background()

	t.Run("move to another cluster", func(t *testing.T) {
		f := newMoveFixture(t)
		defer f.cleanup()
		if err := f.ds.AddServerToCluster(ctx, f.newCluster, f.moved, nil); err != nil {
			t.Fatalf("AddServerToCluster: %v", err)
		}
		f.assertOldRowsDropped(t, 1)
		var cluster int
		var source string
		if err := f.pool.QueryRow(ctx,
			"SELECT cluster_id, membership_source FROM connections WHERE id = $1",
			f.moved).Scan(&cluster, &source); err != nil {
			t.Fatalf("Read back: %v", err)
		}
		if cluster != f.newCluster || source != "manual" {
			t.Errorf("Stored cluster=%d source=%q", cluster, source)
		}
	})

	t.Run("same cluster keeps rows", func(t *testing.T) {
		f := newMoveFixture(t)
		defer f.cleanup()
		if err := f.ds.AddServerToCluster(ctx, f.oldCluster, f.moved, nil); err != nil {
			t.Fatalf("AddServerToCluster: %v", err)
		}
		if got := f.rowsInvolving(t, f.oldCluster, f.moved); got != 3 {
			t.Errorf("rows in the unchanged cluster = %d, want 3", got)
		}
	})

	t.Run("missing cluster", func(t *testing.T) {
		f := newMoveFixture(t)
		defer f.cleanup()
		err := f.ds.AddServerToCluster(ctx, f.newCluster+1000, f.moved, nil)
		if !errors.Is(err, ErrClusterNotFound) {
			t.Fatalf("Expected ErrClusterNotFound, got %v", err)
		}
		f.assertUnchanged(t)
	})

	t.Run("missing connection", func(t *testing.T) {
		f := newMoveFixture(t)
		defer f.cleanup()
		err := f.ds.AddServerToCluster(ctx, f.newCluster, f.other+1000, nil)
		if !errors.Is(err, ErrConnectionNotFound) {
			t.Fatalf("Expected ErrConnectionNotFound, got %v", err)
		}
	})

	t.Run("lookup failures", func(t *testing.T) {
		f := newMoveFixture(t)
		defer f.cleanup()
		if _, err := f.pool.Exec(ctx, "ALTER TABLE connections RENAME TO connections_gone"); err != nil {
			t.Fatalf("Rename connections: %v", err)
		}
		err := f.ds.AddServerToCluster(ctx, f.newCluster, f.moved, nil)
		if err == nil || errors.Is(err, ErrConnectionNotFound) {
			t.Errorf("Expected a wrapped connection lookup error, got %v", err)
		}
		if _, err := f.pool.Exec(ctx, "ALTER TABLE connections_gone RENAME TO connections"); err != nil {
			t.Fatalf("Restore connections: %v", err)
		}
		if _, err := f.pool.Exec(ctx, "ALTER TABLE clusters RENAME TO clusters_gone"); err != nil {
			t.Fatalf("Rename clusters: %v", err)
		}
		err = f.ds.AddServerToCluster(ctx, f.newCluster, f.moved, nil)
		if err == nil || errors.Is(err, ErrClusterNotFound) {
			t.Errorf("Expected a wrapped cluster lookup error, got %v", err)
		}
		if _, err := f.pool.Exec(ctx, "ALTER TABLE clusters_gone RENAME TO clusters"); err != nil {
			t.Fatalf("Restore clusters: %v", err)
		}
	})

	t.Run("update failure keeps rows", func(t *testing.T) {
		f := newMoveFixture(t)
		defer f.cleanup()
		if _, err := f.pool.Exec(ctx, "ALTER TABLE connections DROP COLUMN role"); err != nil {
			t.Fatalf("Drop role column: %v", err)
		}
		if err := f.ds.AddServerToCluster(ctx, f.newCluster, f.moved, nil); err == nil {
			t.Fatal("AddServerToCluster without the role column returned nil; want error")
		}
		f.assertUnchanged(t)
	})
}

// TestMoveConnection_RelationshipDeleteFailure covers the delete
// failure branch: the move rolls back with it.
func TestMoveConnection_RelationshipDeleteFailure(t *testing.T) {
	ctx := context.Background()
	f := newMoveFixture(t)
	defer f.cleanup()
	if _, err := f.pool.Exec(ctx, `
		ALTER TABLE cluster_node_relationships
			RENAME COLUMN target_connection_id TO target_id
	`); err != nil {
		t.Fatalf("Rename column: %v", err)
	}

	if err := f.ds.AssignConnectionToCluster(ctx, f.moved, &f.newCluster, nil, "manual"); err == nil {
		t.Error("AssignConnectionToCluster with a failing delete returned nil; want error")
	}
	if err := f.ds.AddServerToCluster(ctx, f.newCluster, f.moved, nil); err == nil {
		t.Error("AddServerToCluster with a failing delete returned nil; want error")
	}
	var cluster int
	if err := f.pool.QueryRow(ctx,
		"SELECT cluster_id FROM connections WHERE id = $1", f.moved).Scan(&cluster); err != nil {
		t.Fatalf("Read back: %v", err)
	}
	if cluster != f.oldCluster {
		t.Errorf("cluster_id = %d after a failed move, want the old %d", cluster, f.oldCluster)
	}
}

// TestMoveConnection_CommitFailure covers the commit failure branch
// through a deferred constraint trigger on the connections UPDATE that
// raises at commit time, so it fires whether or not the move deletes
// any relationship rows.
func TestMoveConnection_CommitFailure(t *testing.T) {
	ctx := context.Background()
	f := newMoveFixture(t)
	defer f.cleanup()
	if _, err := f.pool.Exec(ctx, `
		CREATE FUNCTION fail_move_connection_update() RETURNS trigger
		LANGUAGE plpgsql AS $$
		BEGIN
			RAISE EXCEPTION 'injected commit failure';
		END $$;
		CREATE CONSTRAINT TRIGGER fail_move_connection_update
			AFTER UPDATE ON connections
			DEFERRABLE INITIALLY DEFERRED
			FOR EACH ROW EXECUTE FUNCTION fail_move_connection_update();
	`); err != nil {
		t.Fatalf("Create the failing trigger: %v", err)
	}
	// A deferred drop runs before the earlier deferred cleanup closes the
	// pool; t.Cleanup would run after it and leave the function behind.
	defer func() {
		_, _ = f.pool.Exec(context.Background(),
			`DROP FUNCTION IF EXISTS fail_move_connection_update() CASCADE`)
	}()

	if err := f.ds.AssignConnectionToCluster(ctx, f.moved, &f.newCluster, nil, "manual"); err == nil {
		t.Error("AssignConnectionToCluster with a failing commit returned nil; want error")
	}
	if err := f.ds.AddServerToCluster(ctx, f.newCluster, f.moved, nil); err == nil {
		t.Error("AddServerToCluster with a failing commit returned nil; want error")
	}
	f.assertUnchanged(t)
}

// TestAssignConnectionToCluster_ClosedPool covers the begin failure
// branch.
func TestAssignConnectionToCluster_ClosedPool(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	if _, err := pool.Exec(context.Background(), syncAutoDetectedTestTeardown); err != nil {
		t.Fatalf("teardown: %v", err)
	}
	pool.Close()
	_ = cleanup

	if err := ds.AssignConnectionToCluster(context.Background(), 1, nil, nil, "auto"); err == nil {
		t.Fatal("AssignConnectionToCluster against a closed pool returned nil; want error")
	}
}
