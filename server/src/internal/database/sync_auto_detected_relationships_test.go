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
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// syncAutoDetectedTestSchema creates the minimal subset of tables needed
// to exercise SyncAutoDetectedRelationships. It mirrors the production
// shape used by the collector schema for cluster_groups, clusters,
// connections, and cluster_node_relationships, including the unique
// constraint that the ON CONFLICT clause depends on.
const syncAutoDetectedTestSchema = `
DROP TABLE IF EXISTS cluster_node_relationships CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
DROP TABLE IF EXISTS cluster_groups CASCADE;

CREATE TABLE cluster_groups (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    is_shared BOOLEAN NOT NULL DEFAULT FALSE,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE clusters (
    id SERIAL PRIMARY KEY,
    group_id INTEGER REFERENCES cluster_groups(id) ON DELETE CASCADE,
    auto_cluster_key VARCHAR(255) UNIQUE,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    replication_type VARCHAR(50),
    dismissed BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE SET NULL,
    membership_source VARCHAR(16) NOT NULL DEFAULT 'auto',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE cluster_node_relationships (
    id SERIAL PRIMARY KEY,
    cluster_id INTEGER NOT NULL
        REFERENCES clusters(id) ON DELETE CASCADE,
    source_connection_id INTEGER NOT NULL
        REFERENCES connections(id) ON DELETE CASCADE,
    target_connection_id INTEGER NOT NULL
        REFERENCES connections(id) ON DELETE CASCADE,
    relationship_type VARCHAR(50) NOT NULL,
    is_auto_detected BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_no_self_relationship
        CHECK (source_connection_id != target_connection_id),
    CONSTRAINT uq_relationship
        UNIQUE (cluster_id, source_connection_id,
                target_connection_id, relationship_type)
);
`

const syncAutoDetectedTestTeardown = `
DROP TABLE IF EXISTS cluster_node_relationships CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
DROP TABLE IF EXISTS cluster_groups CASCADE;
`

// newSyncAutoDetectedTestDatastore wires up a *Datastore against the
// TEST_AI_WORKBENCH_SERVER Postgres instance with only the tables
// required to exercise SyncAutoDetectedRelationships. It returns a
// cleanup function that drops the schema and closes the pool.
func newSyncAutoDetectedTestDatastore(t *testing.T) (*Datastore, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("Test database ping failed: %v", err)
	}

	if _, err := pool.Exec(ctx, syncAutoDetectedTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create sync auto-detected test schema: %v", err)
	}

	ds := NewTestDatastore(pool)

	cleanup := func() {
		if _, err := pool.Exec(context.Background(), syncAutoDetectedTestTeardown); err != nil {
			t.Logf("sync auto-detected teardown failed: %v", err)
		}
		pool.Close()
	}

	return ds, pool, cleanup
}

// insertSyncTestCluster inserts a cluster row and returns its id.
func insertSyncTestCluster(t *testing.T, pool *pgxpool.Pool, name string) int {
	t.Helper()
	var id int
	err := pool.QueryRow(context.Background(),
		`INSERT INTO clusters (name) VALUES ($1) RETURNING id`,
		name,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert cluster %q: %v", name, err)
	}
	return id
}

// insertSyncTestConnection inserts a connection row and returns its id.
func insertSyncTestConnection(t *testing.T, pool *pgxpool.Pool, name string) int {
	t.Helper()
	var id int
	err := pool.QueryRow(context.Background(),
		`INSERT INTO connections (name) VALUES ($1) RETURNING id`,
		name,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert connection %q: %v", name, err)
	}
	return id
}

// countRelationships returns the total relationship count for a cluster
// matching the given is_auto_detected filter.
func countRelationships(t *testing.T, pool *pgxpool.Pool, clusterID int, autoDetected bool) int {
	t.Helper()
	var count int
	err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM cluster_node_relationships
         WHERE cluster_id = $1 AND is_auto_detected = $2`,
		clusterID, autoDetected,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count relationships (cluster=%d, auto=%v): %v",
			clusterID, autoDetected, err)
	}
	return count
}

// hasRelationship reports whether the given relationship row exists for
// the cluster with the supplied is_auto_detected flag.
func hasRelationship(t *testing.T, pool *pgxpool.Pool, clusterID, sourceID, targetID int, relType string, autoDetected bool) bool {
	t.Helper()
	var exists bool
	err := pool.QueryRow(context.Background(),
		`SELECT EXISTS(
             SELECT 1 FROM cluster_node_relationships
             WHERE cluster_id = $1
               AND source_connection_id = $2
               AND target_connection_id = $3
               AND relationship_type = $4
               AND is_auto_detected = $5
         )`,
		clusterID, sourceID, targetID, relType, autoDetected,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("hasRelationship: %v", err)
	}
	return exists
}

// insertAutoRelationship seeds an existing auto-detected relationship
// directly via SQL so tests can verify it gets removed by sync.
func insertAutoRelationship(t *testing.T, pool *pgxpool.Pool, clusterID, sourceID, targetID int, relType string, auto bool) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO cluster_node_relationships
         (cluster_id, source_connection_id, target_connection_id,
          relationship_type, is_auto_detected)
         VALUES ($1, $2, $3, $4, $5)`,
		clusterID, sourceID, targetID, relType, auto,
	)
	if err != nil {
		t.Fatalf("insertAutoRelationship: %v", err)
	}
}

// TestSyncAutoDetectedRelationships_ReplacesObsoleteRows is the
// regression test for issue #152. When the topology changes (failover,
// subscriber removal, new parent in a binary chain), the old
// auto-detected rows must be removed and replaced with the new set in
// a single transaction.
func TestSyncAutoDetectedRelationships_ReplacesObsoleteRows(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	clusterID := insertSyncTestCluster(t, pool, "test-cluster")
	connA := insertSyncTestConnection(t, pool, "node-a")
	connB := insertSyncTestConnection(t, pool, "node-b")
	connC := insertSyncTestConnection(t, pool, "node-c")

	initial := []AutoRelationshipInput{
		{SourceConnectionID: connA, TargetConnectionID: connB, RelationshipType: "streams_from"},
		{SourceConnectionID: connB, TargetConnectionID: connA, RelationshipType: "replicates_with"},
	}
	if err := ds.SyncAutoDetectedRelationships(ctx, clusterID, initial); err != nil {
		t.Fatalf("initial sync failed: %v", err)
	}

	if got := countRelationships(t, pool, clusterID, true); got != 2 {
		t.Fatalf("expected 2 auto rows after initial sync, got %d", got)
	}

	replacement := []AutoRelationshipInput{
		{SourceConnectionID: connA, TargetConnectionID: connC, RelationshipType: "streams_from"},
	}
	if err := ds.SyncAutoDetectedRelationships(ctx, clusterID, replacement); err != nil {
		t.Fatalf("replacement sync failed: %v", err)
	}

	if got := countRelationships(t, pool, clusterID, true); got != 1 {
		t.Fatalf("expected 1 auto row after replacement, got %d", got)
	}
	if !hasRelationship(t, pool, clusterID, connA, connC, "streams_from", true) {
		t.Fatalf("expected new edge connA->connC streams_from to exist")
	}
	if hasRelationship(t, pool, clusterID, connA, connB, "streams_from", true) {
		t.Fatalf("obsolete edge connA->connB streams_from was not removed (issue #152)")
	}
	if hasRelationship(t, pool, clusterID, connB, connA, "replicates_with", true) {
		t.Fatalf("obsolete edge connB->connA replicates_with was not removed (issue #152)")
	}
}

// TestSyncAutoDetectedRelationships_PreservesManualRows verifies that
// rows where is_auto_detected = FALSE are not touched by sync.
func TestSyncAutoDetectedRelationships_PreservesManualRows(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	clusterID := insertSyncTestCluster(t, pool, "test-cluster")
	connA := insertSyncTestConnection(t, pool, "node-a")
	connB := insertSyncTestConnection(t, pool, "node-b")
	connC := insertSyncTestConnection(t, pool, "node-c")

	insertAutoRelationship(t, pool, clusterID, connA, connB, "streams_from", false)
	insertAutoRelationship(t, pool, clusterID, connB, connC, "replicates_with", true)

	if got := countRelationships(t, pool, clusterID, false); got != 1 {
		t.Fatalf("expected 1 manual row before sync, got %d", got)
	}

	replacement := []AutoRelationshipInput{
		{SourceConnectionID: connA, TargetConnectionID: connC, RelationshipType: "streams_from"},
	}
	if err := ds.SyncAutoDetectedRelationships(ctx, clusterID, replacement); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	if got := countRelationships(t, pool, clusterID, false); got != 1 {
		t.Fatalf("manual rows were modified by sync: expected 1, got %d", got)
	}
	if !hasRelationship(t, pool, clusterID, connA, connB, "streams_from", false) {
		t.Fatalf("manual relationship was removed by sync")
	}
	if got := countRelationships(t, pool, clusterID, true); got != 1 {
		t.Fatalf("expected 1 auto row after sync, got %d", got)
	}
	if !hasRelationship(t, pool, clusterID, connA, connC, "streams_from", true) {
		t.Fatalf("expected new auto edge to be inserted")
	}
}

// TestSyncAutoDetectedRelationships_PreservesOtherClusters verifies
// that auto-detected rows belonging to a different cluster are not
// touched when syncing a given cluster.
func TestSyncAutoDetectedRelationships_PreservesOtherClusters(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	clusterA := insertSyncTestCluster(t, pool, "cluster-a")
	clusterB := insertSyncTestCluster(t, pool, "cluster-b")
	conn1 := insertSyncTestConnection(t, pool, "node-1")
	conn2 := insertSyncTestConnection(t, pool, "node-2")

	insertAutoRelationship(t, pool, clusterA, conn1, conn2, "streams_from", true)
	insertAutoRelationship(t, pool, clusterB, conn2, conn1, "replicates_with", true)

	replacement := []AutoRelationshipInput{
		{SourceConnectionID: conn2, TargetConnectionID: conn1, RelationshipType: "streams_from"},
	}
	if err := ds.SyncAutoDetectedRelationships(ctx, clusterA, replacement); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	if got := countRelationships(t, pool, clusterB, true); got != 1 {
		t.Fatalf("auto rows for cluster B were modified: expected 1, got %d", got)
	}
	if !hasRelationship(t, pool, clusterB, conn2, conn1, "replicates_with", true) {
		t.Fatalf("auto edge for cluster B was removed by sync of cluster A")
	}

	if got := countRelationships(t, pool, clusterA, true); got != 1 {
		t.Fatalf("expected 1 auto row in cluster A after sync, got %d", got)
	}
	if !hasRelationship(t, pool, clusterA, conn2, conn1, "streams_from", true) {
		t.Fatalf("expected replacement edge in cluster A")
	}
	if hasRelationship(t, pool, clusterA, conn1, conn2, "streams_from", true) {
		t.Fatalf("obsolete cluster A edge was not removed")
	}
}

// TestSyncAutoDetectedRelationships_EmptyDetectedClearsAutoRows
// verifies that calling sync with an empty slice removes every existing
// auto-detected row for the target cluster (e.g. all replication links
// disappeared) while leaving manual rows intact.
func TestSyncAutoDetectedRelationships_EmptyDetectedClearsAutoRows(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	clusterID := insertSyncTestCluster(t, pool, "test-cluster")
	connA := insertSyncTestConnection(t, pool, "node-a")
	connB := insertSyncTestConnection(t, pool, "node-b")

	insertAutoRelationship(t, pool, clusterID, connA, connB, "streams_from", true)
	insertAutoRelationship(t, pool, clusterID, connB, connA, "replicates_with", true)
	insertAutoRelationship(t, pool, clusterID, connA, connB, "subscribes_to", false)

	if err := ds.SyncAutoDetectedRelationships(ctx, clusterID, nil); err != nil {
		t.Fatalf("sync with nil slice failed: %v", err)
	}

	if got := countRelationships(t, pool, clusterID, true); got != 0 {
		t.Fatalf("expected 0 auto rows after empty sync, got %d", got)
	}
	if got := countRelationships(t, pool, clusterID, false); got != 1 {
		t.Fatalf("manual rows were affected by empty sync: expected 1, got %d", got)
	}

	if err := ds.SyncAutoDetectedRelationships(ctx, clusterID, []AutoRelationshipInput{}); err != nil {
		t.Fatalf("sync with empty slice failed: %v", err)
	}
	if got := countRelationships(t, pool, clusterID, true); got != 0 {
		t.Fatalf("expected 0 auto rows after empty-slice sync, got %d", got)
	}
}

// TestSyncAutoDetectedRelationships_HandlesDuplicatesInInput verifies
// that a duplicate inside the detected slice does not error and that
// only one row is materialized per unique tuple.
func TestSyncAutoDetectedRelationships_HandlesDuplicatesInInput(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	clusterID := insertSyncTestCluster(t, pool, "test-cluster")
	connA := insertSyncTestConnection(t, pool, "node-a")
	connB := insertSyncTestConnection(t, pool, "node-b")

	dup := []AutoRelationshipInput{
		{SourceConnectionID: connA, TargetConnectionID: connB, RelationshipType: "streams_from"},
		{SourceConnectionID: connA, TargetConnectionID: connB, RelationshipType: "streams_from"},
		{SourceConnectionID: connA, TargetConnectionID: connB, RelationshipType: "streams_from"},
	}
	if err := ds.SyncAutoDetectedRelationships(ctx, clusterID, dup); err != nil {
		t.Fatalf("sync with duplicates failed: %v", err)
	}

	if got := countRelationships(t, pool, clusterID, true); got != 1 {
		t.Fatalf("expected 1 auto row from duplicates, got %d", got)
	}
	if !hasRelationship(t, pool, clusterID, connA, connB, "streams_from", true) {
		t.Fatalf("expected edge to be present")
	}
}

// TestSyncAutoDetectedRelationships_RollsBackOnInsertFailure verifies
// the transactional guarantee: if any insert fails (e.g. violates the
// CHECK constraint that source != target), the prior delete is rolled
// back and the previous auto-detected state survives intact.
func TestSyncAutoDetectedRelationships_RollsBackOnInsertFailure(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	clusterID := insertSyncTestCluster(t, pool, "test-cluster")
	connA := insertSyncTestConnection(t, pool, "node-a")
	connB := insertSyncTestConnection(t, pool, "node-b")

	initial := []AutoRelationshipInput{
		{SourceConnectionID: connA, TargetConnectionID: connB, RelationshipType: "streams_from"},
	}
	if err := ds.SyncAutoDetectedRelationships(ctx, clusterID, initial); err != nil {
		t.Fatalf("initial sync failed: %v", err)
	}
	if got := countRelationships(t, pool, clusterID, true); got != 1 {
		t.Fatalf("expected 1 auto row after initial sync, got %d", got)
	}

	bad := []AutoRelationshipInput{
		{SourceConnectionID: connB, TargetConnectionID: connA, RelationshipType: "streams_from"},
		{SourceConnectionID: connA, TargetConnectionID: connA, RelationshipType: "streams_from"},
	}
	if err := ds.SyncAutoDetectedRelationships(ctx, clusterID, bad); err == nil {
		t.Fatalf("expected error from self-relationship insert, got nil")
	}

	if got := countRelationships(t, pool, clusterID, true); got != 1 {
		t.Fatalf("transaction did not roll back: expected 1 auto row, got %d", got)
	}
	if !hasRelationship(t, pool, clusterID, connA, connB, "streams_from", true) {
		t.Fatalf("rollback lost the original auto edge")
	}
	if hasRelationship(t, pool, clusterID, connB, connA, "streams_from", true) {
		t.Fatalf("partial insert from failed transaction was committed")
	}
}

// TestSyncAutoDetectedRelationships_NoExistingRows verifies the empty
// initial-state path: syncing into a cluster with no prior auto-detected
// rows should simply insert the new set without error.
func TestSyncAutoDetectedRelationships_NoExistingRows(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	clusterID := insertSyncTestCluster(t, pool, "test-cluster")
	connA := insertSyncTestConnection(t, pool, "node-a")
	connB := insertSyncTestConnection(t, pool, "node-b")

	detected := []AutoRelationshipInput{
		{SourceConnectionID: connA, TargetConnectionID: connB, RelationshipType: "streams_from"},
		{SourceConnectionID: connB, TargetConnectionID: connA, RelationshipType: "replicates_with"},
	}
	if err := ds.SyncAutoDetectedRelationships(ctx, clusterID, detected); err != nil {
		t.Fatalf("sync into empty cluster failed: %v", err)
	}

	if got := countRelationships(t, pool, clusterID, true); got != 2 {
		t.Fatalf("expected 2 auto rows, got %d", got)
	}
}

// TestSyncAutoDetectedRelationships_BeginFailure verifies that a
// failure to begin the transaction is surfaced as an error and does
// not leak a half-applied state.
func TestSyncAutoDetectedRelationships_BeginFailure(t *testing.T) {
	ds, _, cleanup := newSyncAutoDetectedTestDatastore(t)
	cleanup()

	ctx := context.Background()
	err := ds.SyncAutoDetectedRelationships(ctx, 1, []AutoRelationshipInput{
		{SourceConnectionID: 1, TargetConnectionID: 2, RelationshipType: "streams_from"},
	})
	if err == nil {
		t.Fatalf("expected error from closed pool, got nil")
	}
	if !strings.Contains(err.Error(), "begin transaction") {
		t.Fatalf("expected begin-transaction error, got %v", err)
	}
}

// TestSyncAutoDetectedRelationships_DeleteFailure verifies that a
// DELETE failure (here triggered by dropping the table after the test
// schema is created) is surfaced as a wrapped error and the
// transaction is rolled back.
func TestSyncAutoDetectedRelationships_DeleteFailure(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	clusterID := insertSyncTestCluster(t, pool, "test-cluster")

	if _, err := pool.Exec(ctx, `DROP TABLE cluster_node_relationships CASCADE`); err != nil {
		t.Fatalf("dropping relationships table failed: %v", err)
	}

	err := ds.SyncAutoDetectedRelationships(ctx, clusterID, []AutoRelationshipInput{
		{SourceConnectionID: 1, TargetConnectionID: 2, RelationshipType: "streams_from"},
	})
	if err == nil {
		t.Fatalf("expected error when relationships table is missing, got nil")
	}
	if !strings.Contains(err.Error(), "delete existing auto-detected relationships") {
		t.Fatalf("expected delete-error wrap, got %v", err)
	}
}

// TestSyncAutoDetectedRelationships_CanceledContext verifies that a
// canceled context produces an error rather than a silent success or
// a partial commit.
func TestSyncAutoDetectedRelationships_CanceledContext(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	clusterID := insertSyncTestCluster(t, pool, "test-cluster")
	connA := insertSyncTestConnection(t, pool, "node-a")
	connB := insertSyncTestConnection(t, pool, "node-b")

	insertAutoRelationship(t, pool, clusterID, connA, connB, "streams_from", true)

	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	err := ds.SyncAutoDetectedRelationships(cancelCtx, clusterID,
		[]AutoRelationshipInput{
			{SourceConnectionID: connB, TargetConnectionID: connA, RelationshipType: "streams_from"},
		})
	if err == nil {
		t.Fatalf("expected error from canceled context, got nil")
	}

	if got := countRelationships(t, pool, clusterID, true); got != 1 {
		t.Fatalf("canceled sync left %d auto rows; expected the original 1", got)
	}
	if !hasRelationship(t, pool, clusterID, connA, connB, "streams_from", true) {
		t.Fatalf("original auto edge was lost despite canceled context")
	}
}

// TestSyncAutoDetectedRelationships_MissingClusterReturnsError verifies
// that calling sync with a cluster id that does not exist in the
// clusters table returns ErrClusterNotFound and makes no changes. The
// SELECT ... FOR UPDATE on the clusters row produces pgx.ErrNoRows
// which the function maps to the canonical ErrClusterNotFound sentinel.
func TestSyncAutoDetectedRelationships_MissingClusterReturnsError(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connA := insertSyncTestConnection(t, pool, "node-a")
	connB := insertSyncTestConnection(t, pool, "node-b")

	const missingClusterID = 999999
	err := ds.SyncAutoDetectedRelationships(ctx, missingClusterID,
		[]AutoRelationshipInput{
			{SourceConnectionID: connA, TargetConnectionID: connB, RelationshipType: "streams_from"},
		})
	if err == nil {
		t.Fatalf("expected error for missing cluster, got nil")
	}
	if !errors.Is(err, ErrClusterNotFound) {
		t.Fatalf("expected ErrClusterNotFound, got %v", err)
	}

	var rowCount int
	if scanErr := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM cluster_node_relationships WHERE cluster_id = $1`,
		missingClusterID,
	).Scan(&rowCount); scanErr != nil {
		t.Fatalf("count rows for missing cluster failed: %v", scanErr)
	}
	if rowCount != 0 {
		t.Fatalf("expected 0 rows for missing cluster id, got %d", rowCount)
	}
}

// TestSyncAutoDetectedRelationships_LockQueryFailure verifies that any
// non-ErrNoRows failure on the SELECT ... FOR UPDATE that locks the
// cluster row is surfaced as a wrapped "failed to lock cluster row"
// error and not silently swallowed.
func TestSyncAutoDetectedRelationships_LockQueryFailure(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	clusterID := insertSyncTestCluster(t, pool, "test-cluster")

	// Drop the clusters table after the cluster id is captured so the
	// SELECT ... FOR UPDATE inside SyncAutoDetectedRelationships fails
	// with a missing-table error rather than ErrNoRows.
	if _, err := pool.Exec(ctx, `DROP TABLE clusters CASCADE`); err != nil {
		t.Fatalf("dropping clusters table failed: %v", err)
	}

	err := ds.SyncAutoDetectedRelationships(ctx, clusterID,
		[]AutoRelationshipInput{
			{SourceConnectionID: 1, TargetConnectionID: 2, RelationshipType: "streams_from"},
		})
	if err == nil {
		t.Fatalf("expected error when clusters table is missing, got nil")
	}
	if errors.Is(err, ErrClusterNotFound) {
		t.Fatalf("expected wrapped lock error, got ErrClusterNotFound")
	}
	if !strings.Contains(err.Error(), "failed to lock cluster row") {
		t.Fatalf("expected lock-error wrap, got %v", err)
	}
}

// TestSyncAutoDetectedRelationships_SerializesConcurrentSyncsForSameCluster
// drives two goroutines that both call SyncAutoDetectedRelationships on
// the same cluster with disjoint inputs. The SELECT ... FOR UPDATE
// guarantees the second sync only runs after the first commits, so the
// final auto-detected set must equal exactly one of the two inputs and
// never the union of both.
func TestSyncAutoDetectedRelationships_SerializesConcurrentSyncsForSameCluster(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	clusterID := insertSyncTestCluster(t, pool, "test-cluster")
	connA := insertSyncTestConnection(t, pool, "node-a")
	connB := insertSyncTestConnection(t, pool, "node-b")
	connC := insertSyncTestConnection(t, pool, "node-c")
	connD := insertSyncTestConnection(t, pool, "node-d")

	inputOne := []AutoRelationshipInput{
		{SourceConnectionID: connA, TargetConnectionID: connB, RelationshipType: "streams_from"},
		{SourceConnectionID: connB, TargetConnectionID: connA, RelationshipType: "replicates_with"},
	}
	inputTwo := []AutoRelationshipInput{
		{SourceConnectionID: connC, TargetConnectionID: connD, RelationshipType: "streams_from"},
		{SourceConnectionID: connD, TargetConnectionID: connC, RelationshipType: "replicates_with"},
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, 2)

	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		errs[0] = ds.SyncAutoDetectedRelationships(ctx, clusterID, inputOne)
	}()
	go func() {
		defer wg.Done()
		<-start
		errs[1] = ds.SyncAutoDetectedRelationships(ctx, clusterID, inputTwo)
	}()

	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d sync failed: %v", i, err)
		}
	}

	got := countRelationships(t, pool, clusterID, true)
	if got != len(inputOne) {
		t.Fatalf("expected exactly %d auto rows (one input survives), got %d (union of both inputs would be %d)",
			len(inputOne), got, len(inputOne)+len(inputTwo))
	}

	matchesOne := hasRelationship(t, pool, clusterID, connA, connB, "streams_from", true) &&
		hasRelationship(t, pool, clusterID, connB, connA, "replicates_with", true)
	matchesTwo := hasRelationship(t, pool, clusterID, connC, connD, "streams_from", true) &&
		hasRelationship(t, pool, clusterID, connD, connC, "replicates_with", true)
	if !matchesOne && !matchesTwo {
		t.Fatalf("final state does not match either input exactly")
	}
	if matchesOne && matchesTwo {
		t.Fatalf("final state matched both inputs, indicating union (race not prevented)")
	}
}

// TestSyncAutoDetectedRelationships_DoesNotBlockDifferentClusters
// confirms that the cluster-row lock is row-scoped, not table-scoped.
// One goroutine holds an explicit FOR UPDATE lock on cluster A's row in
// a long-lived transaction; another goroutine syncs cluster B and must
// complete promptly without waiting for the first transaction.
func TestSyncAutoDetectedRelationships_DoesNotBlockDifferentClusters(t *testing.T) {
	ds, pool, cleanup := newSyncAutoDetectedTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	clusterA := insertSyncTestCluster(t, pool, "cluster-a")
	clusterB := insertSyncTestCluster(t, pool, "cluster-b")
	connOne := insertSyncTestConnection(t, pool, "node-1")
	connTwo := insertSyncTestConnection(t, pool, "node-2")

	// Hold cluster A's row lock in a separate goroutine so that the
	// blocker tx remains open while the test syncs cluster B. The
	// blocker signals readiness via `locked` and waits on `release`
	// before committing.
	locked := make(chan struct{})
	release := make(chan struct{})
	blockerDone := make(chan error, 1)
	go func() {
		tx, err := pool.Begin(ctx)
		if err != nil {
			blockerDone <- err
			return
		}
		defer tx.Rollback(ctx) //nolint:errcheck
		var id int
		if err := tx.QueryRow(ctx,
			`SELECT id FROM clusters WHERE id = $1 FOR UPDATE`,
			clusterA,
		).Scan(&id); err != nil {
			blockerDone <- err
			return
		}
		close(locked)
		<-release
		blockerDone <- tx.Commit(ctx)
	}()

	select {
	case <-locked:
	case <-time.After(5 * time.Second):
		t.Fatal("blocker goroutine never acquired cluster A lock")
	}

	// While cluster A's row is locked, cluster B sync must proceed
	// without waiting. Use a short timeout to detect any unintended
	// blocking behavior; row-scoped locks should let this finish in
	// well under a second on a healthy local Postgres.
	syncCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	syncDone := make(chan error, 1)
	go func() {
		syncDone <- ds.SyncAutoDetectedRelationships(syncCtx, clusterB,
			[]AutoRelationshipInput{
				{SourceConnectionID: connOne, TargetConnectionID: connTwo, RelationshipType: "streams_from"},
			})
	}()

	select {
	case err := <-syncDone:
		if err != nil {
			close(release)
			<-blockerDone
			t.Fatalf("cluster B sync returned error while cluster A was locked: %v", err)
		}
	case <-time.After(4 * time.Second):
		close(release)
		<-blockerDone
		t.Fatal("cluster B sync was blocked by cluster A row lock; row-scoped lock failed")
	}

	// Release the blocker and confirm it commits cleanly.
	close(release)
	if err := <-blockerDone; err != nil {
		t.Fatalf("blocker tx returned error: %v", err)
	}

	if got := countRelationships(t, pool, clusterB, true); got != 1 {
		t.Fatalf("expected 1 auto row in cluster B, got %d", got)
	}
}
