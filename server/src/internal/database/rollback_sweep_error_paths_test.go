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
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// removeServerTestSchema covers the tables RemoveServerFromCluster
// touches. Unlike the sync-relationships schema it includes the role
// column, which the cluster-assignment UPDATE clears.
const removeServerTestSchema = `
DROP TABLE IF EXISTS cluster_node_relationships CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;

CREATE TABLE clusters (
    id SERIAL PRIMARY KEY,
    auto_cluster_key VARCHAR(255) UNIQUE,
    name VARCHAR(255) NOT NULL,
    dismissed BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE SET NULL,
    role VARCHAR(50),
    membership_source VARCHAR(16) NOT NULL DEFAULT 'auto',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE cluster_node_relationships (
    id SERIAL PRIMARY KEY,
    cluster_id INTEGER NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    source_connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    target_connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    relationship_type VARCHAR(50) NOT NULL,
    is_auto_detected BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

const removeServerTestTeardown = `
DROP TABLE IF EXISTS cluster_node_relationships CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
`

// newRemoveServerTestDatastore wires up a *Datastore against the
// TEST_AI_WORKBENCH_SERVER Postgres instance with the tables
// RemoveServerFromCluster needs.
func newRemoveServerTestDatastore(t *testing.T) (*Datastore, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping remove server integration test")
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
	if _, err := pool.Exec(ctx, removeServerTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create remove server test schema: %v", err)
	}

	cleanup := func() {
		if _, err := pool.Exec(context.Background(), removeServerTestTeardown); err != nil {
			t.Logf("remove server teardown failed: %v", err)
		}
		pool.Close()
	}

	return NewTestDatastore(pool), pool, cleanup
}

// newRemoveServerTracedDatastore is newRemoveServerTestDatastore with a
// query tracer installed, so a test can cancel the request context at a
// chosen point in the statement sequence.
func newRemoveServerTracedDatastore(
	t *testing.T,
	tracer *rollbackCtxTracer,
) (*Datastore, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping remove server integration test")
	}

	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		t.Skipf("Could not parse test database connection string: %v", err)
	}
	cfg.MaxConns = 1
	cfg.MinConns = 0
	cfg.ConnConfig.Tracer = tracer

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("Test database ping failed: %v", err)
	}
	if _, err := pool.Exec(ctx, removeServerTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create remove server test schema: %v", err)
	}

	cleanup := func() {
		if _, err := pool.Exec(context.Background(), removeServerTestTeardown); err != nil {
			t.Logf("remove server teardown failed: %v", err)
		}
		pool.Close()
	}

	return NewTestDatastore(pool), pool, cleanup
}

// seedClusteredConnection inserts a cluster with one member connection
// and returns both IDs.
func seedClusteredConnection(t *testing.T, pool *pgxpool.Pool, name string) (clusterID, connID int) {
	t.Helper()

	ctx := context.Background()
	if err := pool.QueryRow(ctx,
		`INSERT INTO clusters (name) VALUES ($1) RETURNING id`, name).
		Scan(&clusterID); err != nil {
		t.Fatalf("Failed to insert cluster: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO connections (name, cluster_id, role, membership_source)
		 VALUES ($1, $2, 'primary', 'manual') RETURNING id`,
		name+"-node", clusterID).Scan(&connID); err != nil {
		t.Fatalf("Failed to insert connection: %v", err)
	}
	return clusterID, connID
}

// TestRemoveServerFromCluster_DetachesAndClearsRelationships covers the
// happy path: relationships involving the connection are removed and the
// connection is detached from the cluster.
func TestRemoveServerFromCluster_DetachesAndClearsRelationships(t *testing.T) {
	ds, pool, cleanup := newRemoveServerTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	clusterID, connID := seedClusteredConnection(t, pool, "remove-happy")

	var peerID int
	if err := pool.QueryRow(ctx,
		`INSERT INTO connections (name, cluster_id) VALUES ('peer', $1) RETURNING id`,
		clusterID).Scan(&peerID); err != nil {
		t.Fatalf("Failed to insert peer connection: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO cluster_node_relationships
			(cluster_id, source_connection_id, target_connection_id, relationship_type)
		VALUES ($1, $2, $3, 'replication')
	`, clusterID, connID, peerID); err != nil {
		t.Fatalf("Failed to seed relationship: %v", err)
	}

	if err := ds.RemoveServerFromCluster(ctx, clusterID, connID); err != nil {
		t.Fatalf("RemoveServerFromCluster returned error: %v", err)
	}

	var relCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM cluster_node_relationships WHERE cluster_id = $1`,
		clusterID).Scan(&relCount); err != nil {
		t.Fatalf("Failed to count relationships: %v", err)
	}
	if relCount != 0 {
		t.Errorf("relationships after removal = %d, want 0", relCount)
	}

	var clusterIDAfter *int
	var role *string
	var membership string
	if err := pool.QueryRow(ctx,
		`SELECT cluster_id, role, membership_source FROM connections WHERE id = $1`,
		connID).Scan(&clusterIDAfter, &role, &membership); err != nil {
		t.Fatalf("Failed to read connection: %v", err)
	}
	if clusterIDAfter != nil || role != nil || membership != "auto" {
		t.Errorf("connection after removal = (%v, %v, %q), want (nil, nil, \"auto\")",
			clusterIDAfter, role, membership)
	}
}

// TestRemoveServerFromCluster_NonMemberReturnsNotFound covers the
// membership guard, which must reject connections that do not belong to
// the cluster before opening a transaction.
func TestRemoveServerFromCluster_NonMemberReturnsNotFound(t *testing.T) {
	ds, pool, cleanup := newRemoveServerTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	clusterID, _ := seedClusteredConnection(t, pool, "remove-non-member")

	var strayID int
	if err := pool.QueryRow(ctx,
		`INSERT INTO connections (name) VALUES ('stray') RETURNING id`).
		Scan(&strayID); err != nil {
		t.Fatalf("Failed to insert stray connection: %v", err)
	}

	err := ds.RemoveServerFromCluster(ctx, clusterID, strayID)
	if !errors.Is(err, ErrConnectionNotFound) {
		t.Errorf("RemoveServerFromCluster error = %v, want ErrConnectionNotFound", err)
	}
}

// TestRemoveServerFromCluster_MembershipCheckFailure covers the failure
// of the membership lookup itself, reached when the connections table is
// missing.
func TestRemoveServerFromCluster_MembershipCheckFailure(t *testing.T) {
	ds, pool, cleanup := newRemoveServerTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP TABLE connections CASCADE`); err != nil {
		t.Fatalf("Failed to drop connections table: %v", err)
	}

	if err := ds.RemoveServerFromCluster(ctx, 1, 1); err == nil {
		t.Fatal("RemoveServerFromCluster without a connections table returned nil; want error")
	}
}

// TestRemoveServerFromCluster_RelationshipDeleteFailure covers the
// failed-DELETE branch inside the transaction, so the deferred rollback
// runs on a real error path.
func TestRemoveServerFromCluster_RelationshipDeleteFailure(t *testing.T) {
	ds, pool, cleanup := newRemoveServerTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	clusterID, connID := seedClusteredConnection(t, pool, "remove-del-fail")

	if _, err := pool.Exec(ctx,
		`DROP TABLE cluster_node_relationships CASCADE`); err != nil {
		t.Fatalf("Failed to drop relationships table: %v", err)
	}

	if err := ds.RemoveServerFromCluster(ctx, clusterID, connID); err == nil {
		t.Fatal("RemoveServerFromCluster without a relationships table returned nil; want error")
	}

	// The rollback must leave the membership untouched.
	var clusterIDAfter *int
	if err := pool.QueryRow(ctx,
		`SELECT cluster_id FROM connections WHERE id = $1`, connID).
		Scan(&clusterIDAfter); err != nil {
		t.Fatalf("Failed to read connection: %v", err)
	}
	if clusterIDAfter == nil || *clusterIDAfter != clusterID {
		t.Errorf("cluster_id after failed removal = %v, want %d", clusterIDAfter, clusterID)
	}
}

// TestRemoveServerFromCluster_BeginFailureAfterMembershipCheck covers the
// begin-transaction failure branch. Canceling the request context once
// the membership check has completed is the realistic way to reach it: a
// client that disconnects at that instant leaves Begin with a canceled
// context.
func TestRemoveServerFromCluster_BeginFailureAfterMembershipCheck(t *testing.T) {
	tracer := &rollbackCtxTracer{cancelAfter: "select exists"}
	ds, pool, cleanup := newRemoveServerTracedDatastore(t, tracer)
	defer cleanup()

	clusterID, connID := seedClusteredConnection(t, pool, "remove-begin-fail")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tracer.mu.Lock()
	tracer.cancel = cancel
	tracer.mu.Unlock()

	err := ds.RemoveServerFromCluster(ctx, clusterID, connID)
	if err == nil {
		t.Fatal("RemoveServerFromCluster with a canceled context returned nil; want error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want it to wrap context.Canceled", err)
	}

	// No transaction was ever opened, so the membership must be intact.
	var clusterIDAfter *int
	if err := pool.QueryRow(context.Background(),
		`SELECT cluster_id FROM connections WHERE id = $1`, connID).
		Scan(&clusterIDAfter); err != nil {
		t.Fatalf("Failed to read connection: %v", err)
	}
	if clusterIDAfter == nil || *clusterIDAfter != clusterID {
		t.Errorf("cluster_id = %v, want %d", clusterIDAfter, clusterID)
	}
}

// TestRemoveServerFromCluster_ClusterAssignmentUpdateFailure covers the
// failed-UPDATE branch, where the relationship delete succeeds but
// clearing the cluster assignment does not. Dropping the role column the
// UPDATE writes reproduces that ordering.
func TestRemoveServerFromCluster_ClusterAssignmentUpdateFailure(t *testing.T) {
	ds, pool, cleanup := newRemoveServerTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	clusterID, connID := seedClusteredConnection(t, pool, "remove-update-fail")

	if _, err := pool.Exec(ctx,
		`ALTER TABLE connections DROP COLUMN role`); err != nil {
		t.Fatalf("Failed to drop role column: %v", err)
	}

	if err := ds.RemoveServerFromCluster(ctx, clusterID, connID); err == nil {
		t.Fatal("RemoveServerFromCluster without a role column returned nil; want error")
	}

	// The rollback must leave the membership untouched.
	var clusterIDAfter *int
	if err := pool.QueryRow(ctx,
		`SELECT cluster_id FROM connections WHERE id = $1`, connID).
		Scan(&clusterIDAfter); err != nil {
		t.Fatalf("Failed to read connection: %v", err)
	}
	if clusterIDAfter == nil || *clusterIDAfter != clusterID {
		t.Errorf("cluster_id after failed removal = %v, want %d",
			clusterIDAfter, clusterID)
	}
}

// TestDeleteAutoDetectedCluster_DismissUpdateFailureRollsBack covers the
// failed dismiss-UPDATE branch, reached when the cluster row is found but
// the dismissed column it must set is absent.
func TestDeleteAutoDetectedCluster_DismissUpdateFailureRollsBack(t *testing.T) {
	ds, pool, cleanup := newClusterDismissTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	groupID := insertClusterDismissTestGroup(t, pool)
	if _, err := pool.Exec(ctx, `
		INSERT INTO clusters (group_id, name, auto_cluster_key)
		VALUES ($1, 'dismiss-update-fail', 'key-dismiss-update-fail')
	`, groupID); err != nil {
		t.Fatalf("Failed to insert cluster: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`ALTER TABLE clusters DROP COLUMN dismissed`); err != nil {
		t.Fatalf("Failed to drop dismissed column: %v", err)
	}

	err := ds.DeleteAutoDetectedCluster(ctx, "key-dismiss-update-fail")
	if err == nil {
		t.Fatal("DeleteAutoDetectedCluster without a dismissed column returned nil; want error")
	}
}

// TestDeleteAutoDetectedCluster_DetachFailureRollsBack covers the
// connection-detach failure branch, where the cluster row is dismissed
// but the connections update cannot run.
func TestDeleteAutoDetectedCluster_DetachFailureRollsBack(t *testing.T) {
	ds, pool, cleanup := newClusterDismissTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	groupID := insertClusterDismissTestGroup(t, pool)
	if _, err := pool.Exec(ctx, `
		INSERT INTO clusters (group_id, name, auto_cluster_key)
		VALUES ($1, 'detach-fail', 'key-detach-fail')
	`, groupID); err != nil {
		t.Fatalf("Failed to insert cluster: %v", err)
	}
	if _, err := pool.Exec(ctx, `DROP TABLE connections CASCADE`); err != nil {
		t.Fatalf("Failed to drop connections table: %v", err)
	}

	if err := ds.DeleteAutoDetectedCluster(ctx, "key-detach-fail"); err == nil {
		t.Fatal("DeleteAutoDetectedCluster without a connections table returned nil; want error")
	}

	// The rollback must leave the cluster un-dismissed.
	var dismissed bool
	if err := pool.QueryRow(ctx,
		`SELECT dismissed FROM clusters WHERE auto_cluster_key = $1`,
		"key-detach-fail").Scan(&dismissed); err != nil {
		t.Fatalf("Failed to read cluster: %v", err)
	}
	if dismissed {
		t.Error("cluster was left dismissed after a failed detach; the rollback did not apply")
	}
}

// TestDeleteAutoDetectedCluster_PlaceholderInsertFailure covers the
// branch that creates a dismissed placeholder for an unknown key, in the
// case where the insert itself fails.
func TestDeleteAutoDetectedCluster_PlaceholderInsertFailure(t *testing.T) {
	ds, pool, cleanup := newClusterDismissTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP TABLE clusters CASCADE`); err != nil {
		t.Fatalf("Failed to drop clusters table: %v", err)
	}

	if err := ds.DeleteAutoDetectedCluster(ctx, "no-such-key"); err == nil {
		t.Fatal("DeleteAutoDetectedCluster without a clusters table returned nil; want error")
	}
}

// TestDeleteAutoDetectedCluster_ClosedPoolReturnsError covers the
// begin-transaction failure branch.
func TestDeleteAutoDetectedCluster_ClosedPoolReturnsError(t *testing.T) {
	ds, pool, cleanup := newClusterDismissTestDatastore(t)
	if _, err := pool.Exec(context.Background(), clusterDismissTestTeardown); err != nil {
		t.Fatalf("teardown: %v", err)
	}
	pool.Close()
	_ = cleanup

	if err := ds.DeleteAutoDetectedCluster(context.Background(), "any"); err == nil {
		t.Fatal("DeleteAutoDetectedCluster against a closed pool returned nil; want error")
	}
}

// TestDismissAutoDetectedClusterKeys_DetachFailureRollsBack covers the
// detach-failure branch of the bulk dismiss path.
func TestDismissAutoDetectedClusterKeys_DetachFailureRollsBack(t *testing.T) {
	ds, pool, cleanup := newClusterDismissTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	groupID := insertClusterDismissTestGroup(t, pool)
	if _, err := pool.Exec(ctx, `
		INSERT INTO clusters (group_id, name, auto_cluster_key)
		VALUES ($1, 'bulk-detach-fail', 'key-bulk-detach-fail')
	`, groupID); err != nil {
		t.Fatalf("Failed to insert cluster: %v", err)
	}
	if _, err := pool.Exec(ctx, `DROP TABLE connections CASCADE`); err != nil {
		t.Fatalf("Failed to drop connections table: %v", err)
	}

	err := ds.DismissAutoDetectedClusterKeys(ctx, []string{"key-bulk-detach-fail"})
	if err == nil {
		t.Fatal("DismissAutoDetectedClusterKeys without a connections table returned nil; want error")
	}

	var dismissed bool
	if err := pool.QueryRow(ctx,
		`SELECT dismissed FROM clusters WHERE auto_cluster_key = $1`,
		"key-bulk-detach-fail").Scan(&dismissed); err != nil {
		t.Fatalf("Failed to read cluster: %v", err)
	}
	if dismissed {
		t.Error("cluster was left dismissed after a failed detach; the rollback did not apply")
	}
}

// TestDismissAutoDetectedClusterKeys_PlaceholderInsertFailure covers the
// placeholder-insert failure branch of the bulk dismiss path.
func TestDismissAutoDetectedClusterKeys_PlaceholderInsertFailure(t *testing.T) {
	ds, pool, cleanup := newClusterDismissTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP TABLE clusters CASCADE`); err != nil {
		t.Fatalf("Failed to drop clusters table: %v", err)
	}

	err := ds.DismissAutoDetectedClusterKeys(ctx, []string{"no-such-key"})
	if err == nil {
		t.Fatal("DismissAutoDetectedClusterKeys without a clusters table returned nil; want error")
	}
}

// TestDismissAutoDetectedClusterKeys_ClosedPoolReturnsError covers the
// begin-transaction failure branch of the bulk dismiss path.
func TestDismissAutoDetectedClusterKeys_ClosedPoolReturnsError(t *testing.T) {
	ds, pool, cleanup := newClusterDismissTestDatastore(t)
	if _, err := pool.Exec(context.Background(), clusterDismissTestTeardown); err != nil {
		t.Fatalf("teardown: %v", err)
	}
	pool.Close()
	_ = cleanup

	err := ds.DismissAutoDetectedClusterKeys(context.Background(), []string{"any"})
	if err == nil {
		t.Fatal("DismissAutoDetectedClusterKeys against a closed pool returned nil; want error")
	}
}
