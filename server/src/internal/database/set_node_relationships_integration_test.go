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

	err := ds.AddNodeRelationship(context.Background(), clusterID, source,
		RelationshipInput{TargetConnectionID: source,
			RelationshipType: "replicates_with"})
	if err == nil {
		t.Fatal("AddNodeRelationship with a self-relationship returned nil; want error")
	}
}
