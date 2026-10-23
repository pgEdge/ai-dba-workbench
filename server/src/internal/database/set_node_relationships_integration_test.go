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
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestSetNodeRelationships_ReplacesManualRowsOnly covers the
// SetNodeRelationships happy path: manual rows for the source are
// replaced wholesale, whilst auto-detected rows survive.
func TestSetNodeRelationships_ReplacesManualRowsOnly(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	clusterID := insertSyncTestCluster(t, pool, "set-node-rels")
	source := insertSyncTestConnection(t, pool, "source")
	targetA := insertSyncTestConnection(t, pool, "target-a")
	targetB := insertSyncTestConnection(t, pool, "target-b")
	joinSyncTestCluster(t, pool, clusterID, source, targetA, targetB)

	// Seed one auto-detected row that must survive, and one manual row
	// that must be replaced.
	if _, err := pool.Exec(ctx, `
		INSERT INTO cluster_node_relationships
			(cluster_id, source_connection_id, target_connection_id,
			 relationship_type, is_auto_detected)
		VALUES ($1, $2, $3, 'replication', TRUE),
		       ($1, $2, $4, 'standby', FALSE)
	`, clusterID, source, targetA, targetB); err != nil {
		t.Fatalf("Failed to seed relationships: %v", err)
	}

	err := ds.SetNodeRelationships(ctx, clusterID, source, []RelationshipInput{
		{TargetConnectionID: targetB, RelationshipType: "replication"},
		// A duplicate of the same pair exercises the ON CONFLICT clause.
		{TargetConnectionID: targetB, RelationshipType: "replication"},
	}, nil)
	if err != nil {
		t.Fatalf("SetNodeRelationships returned error: %v", err)
	}

	if got := countRelationships(t, pool, clusterID, true); got != 1 {
		t.Errorf("auto-detected rows = %d, want 1 (they must be untouched)", got)
	}
	if got := countRelationships(t, pool, clusterID, false); got != 1 {
		t.Errorf("manual rows = %d, want 1", got)
	}

	var target int
	var relType string
	if err := pool.QueryRow(ctx, `
		SELECT target_connection_id, relationship_type
		FROM cluster_node_relationships
		WHERE cluster_id = $1 AND source_connection_id = $2
		  AND is_auto_detected = FALSE
	`, clusterID, source).Scan(&target, &relType); err != nil {
		t.Fatalf("Failed to read manual relationship: %v", err)
	}
	if target != targetB || relType != "replication" {
		t.Errorf("manual relationship = (%d, %q), want (%d, \"replication\")",
			target, relType, targetB)
	}
}

// TestSetNodeRelationships_EmptyInputClearsManualRows covers the
// zero-iteration insert loop: passing no relationships removes the
// source's manual rows and commits.
func TestSetNodeRelationships_EmptyInputClearsManualRows(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	clusterID := insertSyncTestCluster(t, pool, "set-node-rels-empty")
	source := insertSyncTestConnection(t, pool, "source-empty")
	target := insertSyncTestConnection(t, pool, "target-empty")
	joinSyncTestCluster(t, pool, clusterID, source, target)

	if _, err := pool.Exec(ctx, `
		INSERT INTO cluster_node_relationships
			(cluster_id, source_connection_id, target_connection_id,
			 relationship_type, is_auto_detected)
		VALUES ($1, $2, $3, 'replication', FALSE)
	`, clusterID, source, target); err != nil {
		t.Fatalf("Failed to seed relationship: %v", err)
	}

	if err := ds.SetNodeRelationships(ctx, clusterID, source, nil, nil); err != nil {
		t.Fatalf("SetNodeRelationships returned error: %v", err)
	}

	if got := countRelationships(t, pool, clusterID, false); got != 0 {
		t.Errorf("manual rows = %d, want 0", got)
	}
}

// TestSetNodeRelationships_InsertFailureRollsBack covers the failed
// INSERT branch. A self-relationship violates the table's check
// constraint, so the function must return an error and leave the
// pre-existing manual rows intact.
func TestSetNodeRelationships_InsertFailureRollsBack(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	clusterID := insertSyncTestCluster(t, pool, "set-node-rels-bad-insert")
	source := insertSyncTestConnection(t, pool, "source-bad")
	target := insertSyncTestConnection(t, pool, "target-bad")
	joinSyncTestCluster(t, pool, clusterID, source, target)

	if _, err := pool.Exec(ctx, `
		INSERT INTO cluster_node_relationships
			(cluster_id, source_connection_id, target_connection_id,
			 relationship_type, is_auto_detected)
		VALUES ($1, $2, $3, 'replication', FALSE)
	`, clusterID, source, target); err != nil {
		t.Fatalf("Failed to seed relationship: %v", err)
	}

	err := ds.SetNodeRelationships(ctx, clusterID, source, []RelationshipInput{
		{TargetConnectionID: source, RelationshipType: "replication"},
	}, nil)
	if err == nil {
		t.Fatal("SetNodeRelationships with a self-relationship returned nil; want error")
	}

	if got := countRelationships(t, pool, clusterID, false); got != 1 {
		t.Errorf("manual rows after rollback = %d, want the original 1", got)
	}
}

// TestSetNodeRelationships_DeleteFailureReturnsError covers the failed
// DELETE branch, reached when the relationships table is absent.
func TestSetNodeRelationships_DeleteFailureReturnsError(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`DROP TABLE cluster_node_relationships CASCADE`); err != nil {
		t.Fatalf("Failed to drop relationships table: %v", err)
	}

	err := ds.SetNodeRelationships(ctx, 1, 1, nil, nil)
	if err == nil {
		t.Fatal("SetNodeRelationships without the relationships table returned nil; want error")
	}
}

// TestSetNodeRelationships_ClosedPoolReturnsError covers the
// begin-transaction failure branch.
func TestSetNodeRelationships_ClosedPoolReturnsError(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	if _, err := pool.Exec(context.Background(), syncAutoDetectedTestTeardown); err != nil {
		t.Fatalf("teardown: %v", err)
	}
	pool.Close()
	_ = cleanup

	if err := ds.SetNodeRelationships(context.Background(), 1, 1, nil, nil); err == nil {
		t.Fatal("SetNodeRelationships against a closed pool returned nil; want error")
	}
}

// TestSetNodeRelationships_PermittedSet covers the permitted set: a
// manual row to a target outside it rolls the rewrite back with
// ErrHiddenRelationship, whilst an auto-detected row to such a target
// is not deleted and so does not block the rewrite.
func TestSetNodeRelationships_PermittedSet(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	clusterID := insertSyncTestCluster(t, pool, "set-node-rels-permitted")
	source := insertSyncTestConnection(t, pool, "permitted-source")
	visible := insertSyncTestConnection(t, pool, "permitted-visible")
	hiddenManual := insertSyncTestConnection(t, pool, "permitted-hidden-manual")
	hiddenAuto := insertSyncTestConnection(t, pool, "permitted-hidden-auto")
	joinSyncTestCluster(t, pool, clusterID, source, visible, hiddenManual,
		hiddenAuto)

	if _, err := pool.Exec(ctx, `
		INSERT INTO cluster_node_relationships
			(cluster_id, source_connection_id, target_connection_id,
			 relationship_type, is_auto_detected)
		VALUES ($1, $2, $3, 'streams_from', FALSE),
		       ($1, $2, $4, 'replication', TRUE)
	`, clusterID, source, hiddenManual, hiddenAuto); err != nil {
		t.Fatalf("Failed to seed relationships: %v", err)
	}
	permitted := map[int]bool{source: true, visible: true}
	rels := []RelationshipInput{
		{TargetConnectionID: visible, RelationshipType: "streams_from"},
	}

	err := ds.SetNodeRelationships(ctx, clusterID, source, rels, permitted)
	if !errors.Is(err, ErrHiddenRelationship) {
		t.Fatalf("SetNodeRelationships = %v, want ErrHiddenRelationship", err)
	}
	if got := countRelationships(t, pool, clusterID, false); got != 1 {
		t.Errorf("manual rows after refusal = %d, want the original 1", got)
	}

	// With the manual row's target permitted, only the auto-detected
	// row's target is hidden, and the rewrite goes ahead.
	permitted[hiddenManual] = true
	if err := ds.SetNodeRelationships(ctx, clusterID, source, rels, permitted); err != nil {
		t.Fatalf("SetNodeRelationships returned error: %v", err)
	}
	if got := countRelationships(t, pool, clusterID, false); got != 1 {
		t.Errorf("manual rows = %d, want 1 (the new row)", got)
	}
	if got := countRelationships(t, pool, clusterID, true); got != 1 {
		t.Errorf("auto-detected rows = %d, want 1 (untouched)", got)
	}
}

// TestClearNodeRelationships covers ClearNodeRelationships with and
// without a permitted set.
func TestClearNodeRelationships(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	clusterID := insertSyncTestCluster(t, pool, "clear-node-rels")
	source := insertSyncTestConnection(t, pool, "clear-source")
	visible := insertSyncTestConnection(t, pool, "clear-visible")
	hidden := insertSyncTestConnection(t, pool, "clear-hidden")

	if _, err := pool.Exec(ctx, `
		INSERT INTO cluster_node_relationships
			(cluster_id, source_connection_id, target_connection_id,
			 relationship_type, is_auto_detected)
		VALUES ($1, $2, $3, 'streams_from', FALSE),
		       ($1, $2, $4, 'streams_from', FALSE),
		       ($1, $2, $4, 'replication', TRUE)
	`, clusterID, source, visible, hidden); err != nil {
		t.Fatalf("Failed to seed relationships: %v", err)
	}

	err := ds.ClearNodeRelationships(ctx, clusterID, source,
		map[int]bool{source: true, visible: true})
	if !errors.Is(err, ErrHiddenRelationship) {
		t.Fatalf("ClearNodeRelationships = %v, want ErrHiddenRelationship", err)
	}
	if got := countRelationships(t, pool, clusterID, false); got != 2 {
		t.Errorf("manual rows after refusal = %d, want the original 2", got)
	}

	if err := ds.ClearNodeRelationships(ctx, clusterID, source, nil); err != nil {
		t.Fatalf("ClearNodeRelationships returned error: %v", err)
	}
	if got := countRelationships(t, pool, clusterID, false); got != 0 {
		t.Errorf("manual rows = %d, want 0", got)
	}
	if got := countRelationships(t, pool, clusterID, true); got != 1 {
		t.Errorf("auto-detected rows = %d, want 1 (untouched)", got)
	}
}

// TestClearNodeRelationships_DeleteFailureReturnsError covers the failed
// DELETE branch, reached when the relationships table is absent.
func TestClearNodeRelationships_DeleteFailureReturnsError(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	if _, err := pool.Exec(context.Background(),
		`DROP TABLE cluster_node_relationships CASCADE`); err != nil {
		t.Fatalf("Failed to drop relationships table: %v", err)
	}
	if err := ds.ClearNodeRelationships(context.Background(), 1, 1, nil); err == nil {
		t.Fatal("ClearNodeRelationships without the relationships table returned nil; want error")
	}
}

// TestNodeRelationships_CommitFailureReturnsError covers the failed
// COMMIT branch of SetNodeRelationships and ClearNodeRelationships: a
// deferred constraint trigger that raises on DELETE fails the commit,
// and the deleted row survives.
func TestNodeRelationships_CommitFailureReturnsError(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	clusterID := insertSyncTestCluster(t, pool, "node-rels-commit")
	source := insertSyncTestConnection(t, pool, "commit-source")
	target := insertSyncTestConnection(t, pool, "commit-target")
	joinSyncTestCluster(t, pool, clusterID, source, target)

	if _, err := pool.Exec(ctx, `
		INSERT INTO cluster_node_relationships
			(cluster_id, source_connection_id, target_connection_id,
			 relationship_type, is_auto_detected)
		VALUES ($1, $2, $3, 'streams_from', FALSE)
	`, clusterID, source, target); err != nil {
		t.Fatalf("Failed to seed relationship: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		CREATE FUNCTION fail_relationship_delete() RETURNS trigger
		LANGUAGE plpgsql AS $$
		BEGIN
			RAISE EXCEPTION 'injected commit failure';
		END $$;
		CREATE CONSTRAINT TRIGGER fail_relationship_delete
			AFTER DELETE ON cluster_node_relationships
			DEFERRABLE INITIALLY DEFERRED
			FOR EACH ROW EXECUTE FUNCTION fail_relationship_delete();
	`); err != nil {
		t.Fatalf("Failed to create the failing trigger: %v", err)
	}
	// A deferred drop runs before the earlier deferred cleanup closes the
	// pool; t.Cleanup would run after it and leave the function behind.
	defer func() {
		_, _ = pool.Exec(context.Background(),
			`DROP FUNCTION IF EXISTS fail_relationship_delete() CASCADE`)
	}()

	if err := ds.SetNodeRelationships(ctx, clusterID, source, nil, nil); err == nil {
		t.Error("SetNodeRelationships with a failing commit returned nil; want error")
	}
	if err := ds.ClearNodeRelationships(ctx, clusterID, source, nil); err == nil {
		t.Error("ClearNodeRelationships with a failing commit returned nil; want error")
	}
	if got := countRelationships(t, pool, clusterID, false); got != 1 {
		t.Errorf("manual rows = %d, want the original 1", got)
	}
}

// TestClearNodeRelationships_ClosedPoolReturnsError covers the
// begin-transaction failure branch.
func TestClearNodeRelationships_ClosedPoolReturnsError(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	if _, err := pool.Exec(context.Background(), syncAutoDetectedTestTeardown); err != nil {
		t.Fatalf("teardown: %v", err)
	}
	pool.Close()
	_ = cleanup

	if err := ds.ClearNodeRelationships(context.Background(), 1, 1, nil); err == nil {
		t.Fatal("ClearNodeRelationships against a closed pool returned nil; want error")
	}
}

// TestAddNodeRelationship_KeepsOtherManualRows covers the insert-only
// path the replicates_with reverse row uses: the target's existing
// manual rows survive, and a duplicate insert is a no-op.
func TestAddNodeRelationship_KeepsOtherManualRows(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	clusterID := insertSyncTestCluster(t, pool, "add-node-rel")
	source := insertSyncTestConnection(t, pool, "add-source")
	targetA := insertSyncTestConnection(t, pool, "add-target-a")
	targetB := insertSyncTestConnection(t, pool, "add-target-b")
	joinSyncTestCluster(t, pool, clusterID, source, targetA, targetB)

	if _, err := pool.Exec(ctx, `
		INSERT INTO cluster_node_relationships
			(cluster_id, source_connection_id, target_connection_id,
			 relationship_type, is_auto_detected)
		VALUES ($1, $2, $3, 'streams_from', FALSE)
	`, clusterID, source, targetA); err != nil {
		t.Fatalf("Failed to seed relationship: %v", err)
	}

	rel := RelationshipInput{TargetConnectionID: targetB,
		RelationshipType: "replicates_with"}
	for i := 0; i < 2; i++ {
		if err := ds.AddNodeRelationship(ctx, clusterID, source, rel); err != nil {
			t.Fatalf("AddNodeRelationship returned error: %v", err)
		}
	}

	if got := countRelationships(t, pool, clusterID, false); got != 2 {
		t.Errorf("manual rows = %d, want 2 (the seeded row and one added row)", got)
	}
}

// TestAddNodeRelationship_InsertFailureReturnsError covers the failed
// INSERT branch: a self-relationship violates the check constraint.
func TestAddNodeRelationship_InsertFailureReturnsError(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	clusterID := insertSyncTestCluster(t, pool, "add-node-rel-bad")
	source := insertSyncTestConnection(t, pool, "add-source-bad")
	joinSyncTestCluster(t, pool, clusterID, source)

	err := ds.AddNodeRelationship(context.Background(), clusterID, source,
		RelationshipInput{TargetConnectionID: source,
			RelationshipType: "replicates_with"})
	if err == nil {
		t.Fatal("AddNodeRelationship with a self-relationship returned nil; want error")
	}
}

// joinSyncTestCluster makes the given connections members of the
// cluster, as the relationship writes require.
func joinSyncTestCluster(t *testing.T, pool *pgxpool.Pool, clusterID int, ids ...int) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE connections SET cluster_id = $1 WHERE id = ANY($2)`,
		clusterID, ids); err != nil {
		t.Fatalf("Failed to join connections to the cluster: %v", err)
	}
}

// TestNodeRelationships_RefuseNonMembers covers the membership recheck
// both writes make under a lock (issue #707): a source or target that is
// not a member of the cluster is refused with the matching sentinel, and
// nothing is written or deleted.
func TestNodeRelationships_RefuseNonMembers(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	clusterID := insertSyncTestCluster(t, pool, "node-rels-members")
	member := insertSyncTestConnection(t, pool, "members-member")
	other := insertSyncTestConnection(t, pool, "members-other")
	outsider := insertSyncTestConnection(t, pool, "members-outsider")
	joinSyncTestCluster(t, pool, clusterID, member, other)

	if _, err := pool.Exec(ctx, `
		INSERT INTO cluster_node_relationships
			(cluster_id, source_connection_id, target_connection_id,
			 relationship_type, is_auto_detected)
		VALUES ($1, $2, $3, 'streams_from', FALSE)
	`, clusterID, member, other); err != nil {
		t.Fatalf("Failed to seed relationship: %v", err)
	}

	toOutsider := RelationshipInput{TargetConnectionID: outsider,
		RelationshipType: "streams_from"}
	toMember := RelationshipInput{TargetConnectionID: member,
		RelationshipType: "streams_from"}
	tests := []struct {
		name string
		call func() error
		want error
	}{
		{"set, source outside", func() error {
			return ds.SetNodeRelationships(ctx, clusterID, outsider,
				[]RelationshipInput{toMember}, nil)
		}, ErrSourceNotInCluster},
		{"set, target outside", func() error {
			return ds.SetNodeRelationships(ctx, clusterID, member,
				[]RelationshipInput{toOutsider}, nil)
		}, ErrTargetNotInCluster},
		{"add, source outside", func() error {
			return ds.AddNodeRelationship(ctx, clusterID, outsider, toMember)
		}, ErrSourceNotInCluster},
		{"add, target outside", func() error {
			return ds.AddNodeRelationship(ctx, clusterID, member, toOutsider)
		}, ErrTargetNotInCluster},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
			if got := countRelationships(t, pool, clusterID, false); got != 1 {
				t.Errorf("manual rows = %d, want the original 1", got)
			}
		})
	}
}

// TestNodeRelationships_LockFailureReturnsError covers the failed
// membership lock, reached when the connections table is absent.
func TestNodeRelationships_LockFailureReturnsError(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP TABLE connections CASCADE`); err != nil {
		t.Fatalf("Failed to drop connections table: %v", err)
	}
	rel := RelationshipInput{TargetConnectionID: 2,
		RelationshipType: "streams_from"}
	if err := ds.SetNodeRelationships(ctx, 1, 1,
		[]RelationshipInput{rel}, nil); err == nil {
		t.Error("SetNodeRelationships without the connections table returned nil; want error")
	}
	if err := ds.AddNodeRelationship(ctx, 1, 1, rel); err == nil {
		t.Error("AddNodeRelationship without the connections table returned nil; want error")
	}
}

// TestAddNodeRelationship_ClosedPoolReturnsError covers the
// begin-transaction failure branch.
func TestAddNodeRelationship_ClosedPoolReturnsError(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	if _, err := pool.Exec(context.Background(), syncAutoDetectedTestTeardown); err != nil {
		t.Fatalf("teardown: %v", err)
	}
	pool.Close()
	_ = cleanup

	if err := ds.AddNodeRelationship(context.Background(), 1, 1,
		RelationshipInput{TargetConnectionID: 2,
			RelationshipType: "streams_from"}); err == nil {
		t.Fatal("AddNodeRelationship against a closed pool returned nil; want error")
	}
}

// TestAddNodeRelationship_CommitFailureReturnsError covers the failed
// COMMIT branch: a deferred constraint trigger that raises on INSERT
// fails the commit, and no row is written.
func TestAddNodeRelationship_CommitFailureReturnsError(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	clusterID := insertSyncTestCluster(t, pool, "add-node-rel-commit")
	source := insertSyncTestConnection(t, pool, "add-commit-source")
	target := insertSyncTestConnection(t, pool, "add-commit-target")
	joinSyncTestCluster(t, pool, clusterID, source, target)

	if _, err := pool.Exec(ctx, `
		CREATE FUNCTION fail_relationship_insert() RETURNS trigger
		LANGUAGE plpgsql AS $$
		BEGIN
			RAISE EXCEPTION 'injected commit failure';
		END $$;
		CREATE CONSTRAINT TRIGGER fail_relationship_insert
			AFTER INSERT ON cluster_node_relationships
			DEFERRABLE INITIALLY DEFERRED
			FOR EACH ROW EXECUTE FUNCTION fail_relationship_insert();
	`); err != nil {
		t.Fatalf("Failed to create the failing trigger: %v", err)
	}
	// A deferred drop runs before the earlier deferred cleanup closes the
	// pool; t.Cleanup would run after it and leave the function behind.
	defer func() {
		_, _ = pool.Exec(context.Background(),
			`DROP FUNCTION IF EXISTS fail_relationship_insert() CASCADE`)
	}()

	if err := ds.AddNodeRelationship(ctx, clusterID, source,
		RelationshipInput{TargetConnectionID: target,
			RelationshipType: "replicates_with"}); err == nil {
		t.Error("AddNodeRelationship with a failing commit returned nil; want error")
	}
	if got := countRelationships(t, pool, clusterID, false); got != 0 {
		t.Errorf("manual rows = %d, want 0", got)
	}
}

// waitForLockWait waits until a backend in the test database whose
// query contains fragment is waiting on a lock, or until done is
// closed, and reports whether the lock wait was seen.
func waitForLockWait(t *testing.T, pool *pgxpool.Pool, fragment string, done <-chan error) bool {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			return false
		default:
		}
		var n int
		if err := pool.QueryRow(context.Background(), `
			SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database()
			  AND wait_event_type = 'Lock'
			  AND position($1 IN query) > 0`, fragment).Scan(&n); err != nil {
			t.Fatalf("Failed to read pg_stat_activity: %v", err)
		}
		if n > 0 {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no backend running %q waited on a lock", fragment)
	return false
}

// TestNodeRelationships_WaitForUncommittedMove pins the FOR SHARE lock
// both writes take on the member connection rows (issue #707). A move's
// UPDATE is left uncommitted whilst the write starts; the write must
// wait for it and, once it commits, refuse the departed target rather
// than insert a row naming a connection outside its cluster.
func TestNodeRelationships_WaitForUncommittedMove(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	clusterID := insertSyncTestCluster(t, pool, "node-rels-move")
	otherCluster := insertSyncTestCluster(t, pool, "node-rels-move-dest")
	source := insertSyncTestConnection(t, pool, "move-source")
	target := insertSyncTestConnection(t, pool, "move-target")

	rel := RelationshipInput{TargetConnectionID: target,
		RelationshipType: "streams_from"}
	tests := []struct {
		name string
		call func() error
	}{
		{"set", func() error {
			return ds.SetNodeRelationships(ctx, clusterID, source,
				[]RelationshipInput{rel}, nil)
		}},
		{"add", func() error {
			return ds.AddNodeRelationship(ctx, clusterID, source, rel)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			joinSyncTestCluster(t, pool, clusterID, source, target)

			move, err := pool.Begin(ctx)
			if err != nil {
				t.Fatalf("Failed to begin the move: %v", err)
			}
			defer move.Rollback(ctx) //nolint:errcheck // no-op after commit
			if _, err := move.Exec(ctx,
				`UPDATE connections SET cluster_id = $1 WHERE id = $2`,
				otherCluster, target); err != nil {
				t.Fatalf("Failed to move the target: %v", err)
			}

			done := make(chan error, 1)
			go func() { done <- tt.call() }()
			if !waitForLockWait(t, pool, "FOR SHARE", done) {
				t.Fatal("the write finished without waiting for the uncommitted move")
			}
			if err := move.Commit(ctx); err != nil {
				t.Fatalf("Failed to commit the move: %v", err)
			}
			if err := <-done; !errors.Is(err, ErrTargetNotInCluster) {
				t.Fatalf("error = %v, want %v", err, ErrTargetNotInCluster)
			}
			if got := countRelationships(t, pool, clusterID, false); got != 0 {
				t.Errorf("manual rows = %d, want 0", got)
			}
		})
	}
}

// TestNodeRelationships_DeleteClusterNoDeadlock pins the lock order in
// lockClusterMembers. A write holds its locks whilst a hard delete of
// the cluster starts; the write then inserts and commits. Locking the
// member connections before the cluster row deadlocked here, since the
// delete holds the cluster row and its ON DELETE SET NULL cascade waits
// on the connection rows whilst the INSERT waits on the cluster row.
func TestNodeRelationships_DeleteClusterNoDeadlock(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	clusterID := insertSyncTestCluster(t, pool, "node-rels-delete")
	source := insertSyncTestConnection(t, pool, "delete-source")
	target := insertSyncTestConnection(t, pool, "delete-target")
	joinSyncTestCluster(t, pool, clusterID, source, target)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Failed to begin the write: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	if err := lockClusterMembers(ctx, tx, clusterID, source,
		[]int{target}); err != nil {
		t.Fatalf("lockClusterMembers: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- ds.DeleteCluster(ctx, clusterID) }()
	if !waitForLockWait(t, pool, "DELETE FROM clusters", done) {
		t.Fatal("DeleteCluster finished without waiting for the write")
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO cluster_node_relationships
			(cluster_id, source_connection_id, target_connection_id,
			 relationship_type, is_auto_detected)
		VALUES ($1, $2, $3, 'streams_from', FALSE)`,
		clusterID, source, target); err != nil {
		t.Fatalf("Failed to insert the relationship: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Failed to commit the write: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("DeleteCluster: %v", err)
	}
	if got := countRelationships(t, pool, clusterID, false); got != 0 {
		t.Errorf("manual rows = %d, want 0 after the cascade", got)
	}
}

// TestNodeRelationships_RemoveServerNoDeadlock pins the statement order
// in RemoveServerFromCluster. A write holds its member locks whilst the
// remove starts and waits on the connection row; the write then
// deletes its manual rows and commits. Deleting the relationship rows
// before updating the connection row deadlocked here, since the remove
// held the row the write's delete needed.
func TestNodeRelationships_RemoveServerNoDeadlock(t *testing.T) {
	for _, removeSource := range []bool{false, true} {
		name := "remove target"
		if removeSource {
			name = "remove source"
		}
		t.Run(name, func(t *testing.T) {
			ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
			defer cleanup()

			ctx := context.Background()
			// RemoveServerFromCluster also clears the role.
			if _, err := pool.Exec(ctx,
				`ALTER TABLE connections ADD COLUMN role VARCHAR(50)`); err != nil {
				t.Fatalf("Failed to add the role column: %v", err)
			}
			clusterID := insertSyncTestCluster(t, pool, "node-rels-remove")
			source := insertSyncTestConnection(t, pool, "remove-source")
			target := insertSyncTestConnection(t, pool, "remove-target")
			joinSyncTestCluster(t, pool, clusterID, source, target)
			if _, err := pool.Exec(ctx, `
				INSERT INTO cluster_node_relationships
					(cluster_id, source_connection_id, target_connection_id,
					 relationship_type, is_auto_detected)
				VALUES ($1, $2, $3, 'streams_from', FALSE)`,
				clusterID, source, target); err != nil {
				t.Fatalf("Failed to seed relationship: %v", err)
			}
			removed := target
			if removeSource {
				removed = source
			}

			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatalf("Failed to begin the write: %v", err)
			}
			defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
			if err := lockClusterMembers(ctx, tx, clusterID, source,
				[]int{target}); err != nil {
				t.Fatalf("lockClusterMembers: %v", err)
			}

			done := make(chan error, 1)
			go func() { done <- ds.RemoveServerFromCluster(ctx, clusterID, removed) }()
			if !waitForLockWait(t, pool, "UPDATE connections", done) {
				t.Fatal("RemoveServerFromCluster finished without waiting for the write")
			}
			if err := deleteManualRelationships(ctx, tx, clusterID, source,
				nil); err != nil {
				t.Fatalf("deleteManualRelationships: %v", err)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatalf("Failed to commit the write: %v", err)
			}
			if err := <-done; err != nil {
				t.Fatalf("RemoveServerFromCluster: %v", err)
			}
		})
	}
}

// TestNodeRelationships_DismissClusterNoDeadlock pins the id-order lock
// in detachClusterMembersSQL. The members are stored in the heap as cc,
// b, a whilst their ids run a < b < cc. A third transaction holds b, so
// the write (holding a, waiting on b) and the dismiss both queue on it
// before it is released. Detaching in heap order made the dismiss take
// cc first and then need a, whilst the write needed cc, and the two
// deadlocked.
func TestNodeRelationships_DismissClusterNoDeadlock(t *testing.T) {
	tests := []struct {
		name    string
		dismiss func(ds *Datastore, clusterID int, key string) error
	}{
		{"DeleteCluster", func(ds *Datastore, clusterID int, _ string) error {
			return ds.DeleteCluster(context.Background(), clusterID)
		}},
		{"DeleteAutoDetectedCluster", func(ds *Datastore, _ int, key string) error {
			return ds.DeleteAutoDetectedCluster(context.Background(), key)
		}},
		{"DismissAutoDetectedClusterKeys", func(ds *Datastore, _ int, key string) error {
			return ds.DismissAutoDetectedClusterKeys(context.Background(),
				[]string{key})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
			defer cleanup()

			ctx := context.Background()
			const key = "spock:dismiss-deadlock"
			var clusterID int
			if err := pool.QueryRow(ctx, `
				INSERT INTO clusters (name, auto_cluster_key)
				VALUES ('dismiss-deadlock', $1) RETURNING id`,
				key).Scan(&clusterID); err != nil {
				t.Fatalf("Failed to insert cluster: %v", err)
			}
			a := insertSyncTestConnection(t, pool, "dismiss-a")
			b := insertSyncTestConnection(t, pool, "dismiss-b")
			cc := insertSyncTestConnection(t, pool, "dismiss-cc")
			joinSyncTestCluster(t, pool, clusterID, cc)
			// Updating b and then a moves their live tuples after cc.
			joinSyncTestCluster(t, pool, clusterID, b)
			joinSyncTestCluster(t, pool, clusterID, a)

			holder, err := pool.Begin(ctx)
			if err != nil {
				t.Fatalf("Failed to begin the holder: %v", err)
			}
			defer holder.Rollback(ctx) //nolint:errcheck // no-op after rollback
			if _, err := holder.Exec(ctx,
				`SELECT 1 FROM connections WHERE id = $1 FOR NO KEY UPDATE`,
				b); err != nil {
				t.Fatalf("Failed to lock b: %v", err)
			}

			write := make(chan error, 1)
			go func() {
				write <- ds.SetNodeRelationships(ctx, clusterID, a,
					[]RelationshipInput{
						{TargetConnectionID: b, RelationshipType: "streams_from"},
						{TargetConnectionID: cc, RelationshipType: "streams_from"},
					}, nil)
			}()
			if !waitForLockWait(t, pool, "FOR SHARE", write) {
				t.Fatal("the write finished without waiting for b")
			}
			dismiss := make(chan error, 1)
			go func() { dismiss <- tt.dismiss(ds, clusterID, key) }()
			if !waitForLockWait(t, pool, "UPDATE connections", dismiss) {
				t.Fatal("the dismiss finished without waiting")
			}
			if err := holder.Rollback(ctx); err != nil {
				t.Fatalf("Failed to release b: %v", err)
			}
			if err := <-write; err != nil {
				t.Errorf("SetNodeRelationships: %v", err)
			}
			if err := <-dismiss; err != nil {
				t.Errorf("dismiss: %v", err)
			}
		})
	}
}
