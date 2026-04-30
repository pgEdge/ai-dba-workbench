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

// TestSyncRelationshipsFromTopology_EmptyDetectedClearsAutoRows is the
// regression test for issue #152 follow-up: when a cluster's topology no
// longer yields any edges (e.g. its last subscriber was removed or all
// replication was torn down), the obsolete auto-detected rows for that
// cluster must be cleared. Auto-detected rows for OTHER clusters and
// manual rows on the same cluster must be left untouched.
func TestSyncRelationshipsFromTopology_EmptyDetectedClearsAutoRows(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	ctx := context.Background()

	clusterAID := insertSyncTestCluster(t, pool, "cluster-a")
	clusterBID := insertSyncTestCluster(t, pool, "cluster-b")
	connA1 := insertSyncTestConnection(t, pool, "a-node-1")
	connA2 := insertSyncTestConnection(t, pool, "a-node-2")
	connB1 := insertSyncTestConnection(t, pool, "b-node-1")
	connB2 := insertSyncTestConnection(t, pool, "b-node-2")

	// Pre-populate cluster A with two auto-detected rows and one manual
	// row. After the empty-topology sync the auto rows must be gone but
	// the manual row must remain.
	insertAutoRelationship(t, pool, clusterAID, connA1, connA2, "streams_from", true)
	insertAutoRelationship(t, pool, clusterAID, connA2, connA1, "replicates_with", true)
	insertAutoRelationship(t, pool, clusterAID, connA1, connA2, "subscribes_to", false)

	// Cluster B is NOT in the topology map passed to the function and
	// must be untouched.
	insertAutoRelationship(t, pool, clusterBID, connB1, connB2, "streams_from", true)
	insertAutoRelationship(t, pool, clusterBID, connB2, connB1, "replicates_with", true)

	// Build a topology that yields zero edges for cluster A: a logical
	// cluster whose publisher has no Children. extractLogicalRelationships
	// returns an empty slice for this shape, so SyncAutoDetectedRelationships
	// is invoked with an empty detected slice.
	autoKey := "auto-key-cluster-a"
	autoDetectedClusters := map[string]TopologyCluster{
		autoKey: {
			ID:          "auto-cluster-a",
			Name:        "cluster-a",
			ClusterType: "logical",
			Servers: []TopologyServerInfo{
				{ID: connA1, Name: "a-node-1"},
			},
		},
	}
	clusterIDMap := map[string]int{autoKey: clusterAID}

	ds.syncRelationshipsFromTopology(ctx, autoDetectedClusters, clusterIDMap)

	if got := countRelationships(t, pool, clusterAID, true); got != 0 {
		t.Fatalf("cluster A still has %d auto-detected rows after empty-topology sync; expected 0", got)
	}
	if got := countRelationships(t, pool, clusterAID, false); got != 1 {
		t.Fatalf("cluster A manual row count changed: expected 1, got %d", got)
	}
	if !hasRelationship(t, pool, clusterAID, connA1, connA2, "subscribes_to", false) {
		t.Fatalf("cluster A manual relationship was removed by empty-topology sync")
	}

	if got := countRelationships(t, pool, clusterBID, true); got != 2 {
		t.Fatalf("cluster B auto rows were modified: expected 2, got %d", got)
	}
	if !hasRelationship(t, pool, clusterBID, connB1, connB2, "streams_from", true) {
		t.Fatalf("cluster B auto edge B1->B2 streams_from was removed")
	}
	if !hasRelationship(t, pool, clusterBID, connB2, connB1, "replicates_with", true) {
		t.Fatalf("cluster B auto edge B2->B1 replicates_with was removed")
	}
}

// TestSyncRelationshipsFromTopology_SkipsUnmappedClusters verifies the
// early-continue path: a topology entry whose auto-key is not present in
// the cluster ID map (standalone server, failed upsert) does not trigger
// a sync call and does not affect existing rows.
func TestSyncRelationshipsFromTopology_SkipsUnmappedClusters(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	clusterID := insertSyncTestCluster(t, pool, "test-cluster")
	connA := insertSyncTestConnection(t, pool, "node-a")
	connB := insertSyncTestConnection(t, pool, "node-b")

	insertAutoRelationship(t, pool, clusterID, connA, connB, "streams_from", true)

	autoDetectedClusters := map[string]TopologyCluster{
		"unmapped-key": {
			ID:          "unmapped",
			Name:        "unmapped",
			ClusterType: "binary",
			Servers: []TopologyServerInfo{
				{ID: connA, Name: "a"},
			},
		},
	}
	// Empty map: no auto-key has a db cluster ID, so the function must
	// skip every entry without touching the database.
	clusterIDMap := map[string]int{}

	ds.syncRelationshipsFromTopology(ctx, autoDetectedClusters, clusterIDMap)

	if got := countRelationships(t, pool, clusterID, true); got != 1 {
		t.Fatalf("unmapped cluster sync touched existing rows: got %d auto rows, expected 1", got)
	}
	if !hasRelationship(t, pool, clusterID, connA, connB, "streams_from", true) {
		t.Fatalf("existing auto edge was removed despite unmapped topology")
	}
}

// TestSyncRelationshipsFromTopology_BinaryClusterPopulatesEdges verifies
// the happy path for a binary cluster: a primary with two standbys
// produces two streams_from auto-detected rows in the database.
func TestSyncRelationshipsFromTopology_BinaryClusterPopulatesEdges(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	clusterID := insertSyncTestCluster(t, pool, "binary-cluster")
	primary := insertSyncTestConnection(t, pool, "primary")
	standby1 := insertSyncTestConnection(t, pool, "standby-1")
	standby2 := insertSyncTestConnection(t, pool, "standby-2")

	autoKey := "binary-auto"
	autoDetectedClusters := map[string]TopologyCluster{
		autoKey: {
			ID:          "binary-auto",
			Name:        "binary-cluster",
			ClusterType: "binary",
			Servers: []TopologyServerInfo{
				{
					ID:   primary,
					Name: "primary",
					Children: []TopologyServerInfo{
						{ID: standby1, Name: "standby-1"},
						{ID: standby2, Name: "standby-2"},
					},
				},
			},
		},
	}
	clusterIDMap := map[string]int{autoKey: clusterID}

	ds.syncRelationshipsFromTopology(ctx, autoDetectedClusters, clusterIDMap)

	if got := countRelationships(t, pool, clusterID, true); got != 2 {
		t.Fatalf("expected 2 auto-detected rows from binary topology, got %d", got)
	}
	if !hasRelationship(t, pool, clusterID, standby1, primary, "streams_from", true) {
		t.Fatalf("expected standby1->primary streams_from edge")
	}
	if !hasRelationship(t, pool, clusterID, standby2, primary, "streams_from", true) {
		t.Fatalf("expected standby2->primary streams_from edge")
	}
}

// TestSyncRelationshipsFromTopology_SpockCluster verifies the spock
// cluster path produces bidirectional replicates_with rows for every
// pair of nodes.
func TestSyncRelationshipsFromTopology_SpockCluster(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	clusterID := insertSyncTestCluster(t, pool, "spock-cluster")
	n1 := insertSyncTestConnection(t, pool, "spock-1")
	n2 := insertSyncTestConnection(t, pool, "spock-2")

	autoKey := "spock-auto"
	autoDetectedClusters := map[string]TopologyCluster{
		autoKey: {
			ID:          "spock-auto",
			Name:        "spock-cluster",
			ClusterType: "spock",
			Servers: []TopologyServerInfo{
				{ID: n1, Name: "spock-1"},
				{ID: n2, Name: "spock-2"},
			},
		},
	}
	clusterIDMap := map[string]int{autoKey: clusterID}

	ds.syncRelationshipsFromTopology(ctx, autoDetectedClusters, clusterIDMap)

	if got := countRelationships(t, pool, clusterID, true); got != 2 {
		t.Fatalf("expected 2 replicates_with rows for spock pair, got %d", got)
	}
	if !hasRelationship(t, pool, clusterID, n1, n2, "replicates_with", true) {
		t.Fatalf("expected n1->n2 replicates_with edge")
	}
	if !hasRelationship(t, pool, clusterID, n2, n1, "replicates_with", true) {
		t.Fatalf("expected n2->n1 replicates_with edge")
	}
}

// TestSyncRelationshipsFromTopology_ServerClusterTypeProducesNoEdges
// verifies that a non-replicating cluster type ("server") falls through
// the switch with no edges and that any prior auto-detected rows for
// the cluster are cleared (defensive: the case is unlikely in practice
// but the function must not leak stale rows for it).
func TestSyncRelationshipsFromTopology_ServerClusterTypeProducesNoEdges(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	clusterID := insertSyncTestCluster(t, pool, "lone-server")
	connA := insertSyncTestConnection(t, pool, "a")
	connB := insertSyncTestConnection(t, pool, "b")

	insertAutoRelationship(t, pool, clusterID, connA, connB, "streams_from", true)

	autoKey := "server-auto"
	autoDetectedClusters := map[string]TopologyCluster{
		autoKey: {
			ID:          "server-auto",
			Name:        "lone-server",
			ClusterType: "server",
			Servers: []TopologyServerInfo{
				{ID: connA, Name: "a"},
			},
		},
	}
	clusterIDMap := map[string]int{autoKey: clusterID}

	ds.syncRelationshipsFromTopology(ctx, autoDetectedClusters, clusterIDMap)

	if got := countRelationships(t, pool, clusterID, true); got != 0 {
		t.Fatalf("expected stale auto rows to be cleared for server cluster, got %d", got)
	}
}
