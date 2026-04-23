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
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// clusterDismissTestSchema creates the minimum subset of the cluster
// hierarchy tables exercised by the dismiss-then-rediscover path. It
// mirrors the shape used in production (collector/src/database/schema.go),
// limited to columns referenced by UpsertAutoDetectedCluster, DeleteCluster,
// GetCluster, and ListClustersForAutocomplete.
const clusterDismissTestSchema = `
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
DROP TABLE IF EXISTS cluster_groups CASCADE;

CREATE TABLE cluster_groups (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    is_shared BOOLEAN NOT NULL DEFAULT FALSE,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    auto_group_key VARCHAR(255),
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
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT clusters_group_name_unique UNIQUE (group_id, name)
);

CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE SET NULL,
    membership_source VARCHAR(16) NOT NULL DEFAULT 'auto',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

const clusterDismissTestTeardown = `
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
DROP TABLE IF EXISTS cluster_groups CASCADE;
`

// newClusterDismissTestDatastore wires up a *Datastore against the
// TEST_AI_WORKBENCH_SERVER Postgres instance with only the tables the
// cluster dismiss path needs. The caller receives a cleanup that drops the
// schema and closes the pool.
func newClusterDismissTestDatastore(t *testing.T) (*Datastore, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping cluster dismiss integration test")
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

	if _, err := pool.Exec(ctx, clusterDismissTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create cluster dismiss test schema: %v", err)
	}

	ds := NewTestDatastore(pool)

	cleanup := func() {
		if _, err := pool.Exec(context.Background(), clusterDismissTestTeardown); err != nil {
			t.Logf("cluster dismiss teardown failed: %v", err)
		}
		pool.Close()
	}

	return ds, pool, cleanup
}

// insertClusterDismissTestGroup inserts a cluster_groups row marked as
// default and returns its id.
func insertClusterDismissTestGroup(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var id int
	err := pool.QueryRow(context.Background(), `
        INSERT INTO cluster_groups (name, description, is_shared, is_default)
        VALUES ('Servers/Clusters', 'default', TRUE, TRUE)
        RETURNING id
    `).Scan(&id)
	if err != nil {
		t.Fatalf("Failed to insert default cluster group: %v", err)
	}
	return id
}

// TestUpsertAutoDetectedCluster_PreservesDismissed verifies the fix for
// issue #36. When an auto-detected cluster is dismissed (soft-deleted via
// DeleteCluster) and then rediscovered through UpsertAutoDetectedCluster
// (either via the user PUT endpoint or any re-run of the path that keys
// off auto_cluster_key), the dismissed flag must not be cleared. Prior
// to the fix, the UPDATE branch unconditionally set dismissed = FALSE,
// which resurfaced the cluster in ListClustersForAutocomplete the next
// time auto-detection ran.
func TestUpsertAutoDetectedCluster_PreservesDismissed(t *testing.T) {
	ds, pool, cleanup := newClusterDismissTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	groupID := insertClusterDismissTestGroup(t, pool)
	autoKey := "spock:issue36-prefix"

	// 1) Auto-detection inserts the cluster for the first time.
	created, err := ds.UpsertAutoDetectedCluster(
		ctx, autoKey, "auto-cluster-1", nil, &groupID,
	)
	if err != nil {
		t.Fatalf("initial UpsertAutoDetectedCluster failed: %v", err)
	}
	if created == nil || created.ID == 0 {
		t.Fatalf("initial upsert returned empty cluster: %+v", created)
	}

	// Sanity: the dropdown sees it.
	summaries, err := ds.ListClustersForAutocomplete(ctx)
	if err != nil {
		t.Fatalf("ListClustersForAutocomplete failed: %v", err)
	}
	if !containsClusterID(summaries, created.ID) {
		t.Fatalf("dropdown did not include freshly-created cluster %d", created.ID)
	}

	// 2) User dismisses the cluster via DeleteCluster.
	if err := ds.DeleteCluster(ctx, created.ID); err != nil {
		t.Fatalf("DeleteCluster failed: %v", err)
	}

	// The row must be soft-deleted, not hard-deleted.
	var dismissed bool
	if err := pool.QueryRow(ctx,
		`SELECT dismissed FROM clusters WHERE id = $1`, created.ID,
	).Scan(&dismissed); err != nil {
		t.Fatalf("reading dismissed flag after DeleteCluster failed: %v", err)
	}
	if !dismissed {
		t.Fatalf("DeleteCluster did not set dismissed = TRUE for auto-detected cluster")
	}

	// Dropdown must hide it.
	summaries, err = ds.ListClustersForAutocomplete(ctx)
	if err != nil {
		t.Fatalf("ListClustersForAutocomplete (after dismiss) failed: %v", err)
	}
	if containsClusterID(summaries, created.ID) {
		t.Fatalf("dropdown still shows dismissed cluster %d", created.ID)
	}

	// 3) Auto-detection rediscovers the same cluster. With the bug this
	//    un-dismissed the row; the fix keeps dismissed = TRUE.
	redetected, err := ds.UpsertAutoDetectedCluster(
		ctx, autoKey, "auto-cluster-1", nil, &groupID,
	)
	if err != nil {
		t.Fatalf("rediscovery UpsertAutoDetectedCluster failed: %v", err)
	}
	if redetected.ID != created.ID {
		t.Fatalf("rediscovery produced a different cluster id: got %d, want %d (auto_cluster_key is UNIQUE)",
			redetected.ID, created.ID)
	}

	if err := pool.QueryRow(ctx,
		`SELECT dismissed FROM clusters WHERE id = $1`, created.ID,
	).Scan(&dismissed); err != nil {
		t.Fatalf("reading dismissed flag after rediscovery failed: %v", err)
	}
	if !dismissed {
		t.Fatalf("UpsertAutoDetectedCluster resurrected a dismissed cluster (issue #36)")
	}

	// Dropdown must still hide it.
	summaries, err = ds.ListClustersForAutocomplete(ctx)
	if err != nil {
		t.Fatalf("ListClustersForAutocomplete (after rediscovery) failed: %v", err)
	}
	if containsClusterID(summaries, created.ID) {
		t.Fatalf("dropdown re-surfaced dismissed cluster %d after rediscovery (issue #36)",
			created.ID)
	}

	// GetCluster should also hide dismissed rows now.
	if _, err := ds.GetCluster(ctx, created.ID); err == nil {
		t.Fatalf("GetCluster returned a dismissed cluster; expected error")
	}
}

// containsClusterID reports whether any entry in the slice has the
// given cluster id.
func containsClusterID(summaries []ClusterSummary, id int) bool {
	for _, s := range summaries {
		if s.ID == id {
			return true
		}
	}
	return false
}

func TestGetDismissedAutoClusterKeys(t *testing.T) {
	ds, pool, cleanup := newClusterDismissTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	groupID := insertClusterDismissTestGroup(t, pool)

	_, err := pool.Exec(ctx, `
        INSERT INTO clusters (name, auto_cluster_key, group_id, dismissed)
        VALUES ('active-cluster', 'spock:active', $1, FALSE),
               ('dismissed-cluster', 'spock:dismissed', $1, TRUE)
    `, groupID)
	if err != nil {
		t.Fatalf("failed to seed clusters: %v", err)
	}

	_, err = pool.Exec(ctx, `
        INSERT INTO clusters (name, group_id, dismissed)
        VALUES ('manual-cluster', $1, FALSE)
    `, groupID)
	if err != nil {
		t.Fatalf("failed to seed manual cluster: %v", err)
	}

	dismissed, err := ds.getDismissedAutoClusterKeys(ctx)
	if err != nil {
		t.Fatalf("getDismissedAutoClusterKeys failed: %v", err)
	}

	if len(dismissed) != 1 {
		t.Fatalf("expected 1 dismissed key, got %d: %v", len(dismissed), dismissed)
	}
	if !dismissed["spock:dismissed"] {
		t.Fatalf("expected spock:dismissed in set, got %v", dismissed)
	}
}
