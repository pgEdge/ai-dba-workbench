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

// resolveHierarchyTestSchema mirrors the columns referenced by
// resolveConnectionHierarchy -> getAllConnectionsWithRoles /
// buildAutoDetectedClusters. It is intentionally minimal: just enough
// for auto-detection to produce a spock cluster keyed "spock:pg17" out
// of a single connection whose name starts with "pg17-".
const resolveHierarchyTestSchema = `
DROP SCHEMA IF EXISTS metrics CASCADE;
DROP TABLE IF EXISTS alerts CASCADE;
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
    owner_username VARCHAR(255),
    owner_token VARCHAR(255),
    is_monitored BOOLEAN NOT NULL DEFAULT FALSE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    name VARCHAR(255) NOT NULL,
    description TEXT DEFAULT '',
    host VARCHAR(255) NOT NULL,
    port INTEGER NOT NULL DEFAULT 5432,
    database_name VARCHAR(255) NOT NULL,
    username VARCHAR(255) NOT NULL,
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE SET NULL,
    role VARCHAR(50) DEFAULT 'primary',
    connection_error TEXT,
    membership_source VARCHAR(20) NOT NULL DEFAULT 'auto',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE alerts (
    id SERIAL PRIMARY KEY,
    connection_id INTEGER,
    status VARCHAR(50) NOT NULL
);

CREATE SCHEMA metrics;

CREATE TABLE metrics.pg_connectivity (
    connection_id INTEGER NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE metrics.pg_node_role (
    connection_id INTEGER NOT NULL,
    primary_role TEXT,
    upstream_host TEXT,
    upstream_port INTEGER,
    has_spock BOOLEAN,
    spock_node_name TEXT,
    binary_standby_count INTEGER,
    is_streaming_standby BOOLEAN,
    publisher_host TEXT,
    publisher_port INTEGER,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE metrics.pg_server_info (
    connection_id INTEGER NOT NULL,
    server_version TEXT,
    system_identifier BIGINT,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE metrics.pg_sys_os_info (
    connection_id INTEGER NOT NULL,
    name TEXT,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE metrics.pg_extension (
    connection_id INTEGER NOT NULL,
    extname TEXT NOT NULL,
    extversion TEXT,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

const resolveHierarchyTestTeardown = `
DROP SCHEMA IF EXISTS metrics CASCADE;
DROP TABLE IF EXISTS alerts CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
DROP TABLE IF EXISTS cluster_groups CASCADE;
`

// newResolveHierarchyTestDatastore wires up a *Datastore against the
// TEST_AI_WORKBENCH_SERVER Postgres instance with the minimal schema
// needed by resolveConnectionHierarchy.
func newResolveHierarchyTestDatastore(t *testing.T) (*Datastore, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping resolve hierarchy integration test")
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

	if _, err := pool.Exec(ctx, resolveHierarchyTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create resolve hierarchy test schema: %v", err)
	}

	ds := NewTestDatastore(pool)

	cleanup := func() {
		if _, err := pool.Exec(context.Background(), resolveHierarchyTestTeardown); err != nil {
			t.Logf("resolve hierarchy teardown failed: %v", err)
		}
		pool.Close()
	}

	return ds, pool, cleanup
}

// TestResolveConnectionHierarchy_DoesNotResurrectDismissedCluster is the
// regression test for issue #36. After a user dismisses an auto-detected
// cluster, any call path that triggers resolveConnectionHierarchy for a
// connection that still keys into the same auto_cluster_key must not:
//   - return the dismissed cluster as the connection's hierarchy, nor
//   - flip the dismissed flag back to FALSE, nor
//   - insert a fresh dismissed=FALSE row alongside the dismissed one.
//
// Before the fix, the SELECT that looked up the cluster by auto_cluster_key
// did not read the dismissed flag, and the fallback INSERT ... ON CONFLICT
// DO UPDATE SET updated_at = ... silently surfaced the cluster in
// ListClustersForAutocomplete again.
func TestResolveConnectionHierarchy_DoesNotResurrectDismissedCluster(t *testing.T) {
	ds, pool, cleanup := newResolveHierarchyTestDatastore(t)
	defer cleanup()

	ctx := context.Background()

	// 1) Default group + a connection that auto-detects as a spock cluster.
	var groupID int
	if err := pool.QueryRow(ctx, `
        INSERT INTO cluster_groups (name, description, is_shared, is_default)
        VALUES ('Servers/Clusters', 'default', TRUE, TRUE)
        RETURNING id
    `).Scan(&groupID); err != nil {
		t.Fatalf("insert default group: %v", err)
	}

	var connID int
	if err := pool.QueryRow(ctx, `
        INSERT INTO connections (
            name, host, port, database_name, username,
            owner_username, is_monitored, membership_source
        )
        VALUES ('pg17-node1', '10.0.0.1', 5432, 'postgres', 'postgres',
                'alice', TRUE, 'auto')
        RETURNING id
    `).Scan(&connID); err != nil {
		t.Fatalf("insert connection: %v", err)
	}

	// Metrics rows make the connection look like a live Spock primary.
	// buildAutoDetectedClusters keys this as auto_cluster_key = "spock:pg17"
	// (prefix extracted from the connection name).
	if _, err := pool.Exec(ctx, `
        INSERT INTO metrics.pg_connectivity (connection_id, collected_at)
        VALUES ($1, NOW())
    `, connID); err != nil {
		t.Fatalf("insert pg_connectivity: %v", err)
	}
	if _, err := pool.Exec(ctx, `
        INSERT INTO metrics.pg_node_role (
            connection_id, primary_role, has_spock,
            binary_standby_count, is_streaming_standby, collected_at
        )
        VALUES ($1, 'primary', TRUE, 0, FALSE, NOW())
    `, connID); err != nil {
		t.Fatalf("insert pg_node_role: %v", err)
	}

	// 2) Seed the clusters table with the row auto-detection would normally
	//    produce on the first hierarchy resolve. This gives us a stable
	//    cluster id to DeleteCluster in step 3.
	var clusterID int
	if err := pool.QueryRow(ctx, `
        INSERT INTO clusters (name, auto_cluster_key, group_id)
        VALUES ('pg17 Spock', 'spock:pg17', $1)
        RETURNING id
    `, groupID).Scan(&clusterID); err != nil {
		t.Fatalf("seed auto cluster: %v", err)
	}

	// Sanity: resolver returns the cluster while it is live.
	cID, _, gID, _, err := ds.resolveConnectionHierarchy(ctx, connID)
	if err != nil {
		t.Fatalf("resolveConnectionHierarchy (live): %v", err)
	}
	if cID == nil || *cID != clusterID {
		t.Fatalf("resolver returned unexpected cluster id: got %v, want %d",
			cID, clusterID)
	}
	if gID == nil || *gID != groupID {
		t.Fatalf("resolver returned unexpected group id: got %v, want %d",
			gID, groupID)
	}

	// 3) User dismisses the auto-detected cluster.
	if err := ds.DeleteCluster(ctx, clusterID); err != nil {
		t.Fatalf("DeleteCluster: %v", err)
	}

	// Confirm the row was soft-deleted, not removed.
	var dismissed bool
	if err := pool.QueryRow(ctx,
		`SELECT dismissed FROM clusters WHERE id = $1`, clusterID,
	).Scan(&dismissed); err != nil {
		t.Fatalf("read dismissed flag: %v", err)
	}
	if !dismissed {
		t.Fatalf("DeleteCluster did not set dismissed = TRUE on auto cluster")
	}

	// 4) The resolver path is the regression surface. Before the fix, this
	//    either handed the dismissed cluster back to callers, or (if the
	//    row was missing) created a fresh dismissed=FALSE row on the
	//    INSERT ... ON CONFLICT DO UPDATE fallback.
	cID, cName, gID, gName, err := ds.resolveConnectionHierarchy(ctx, connID)
	if err != nil {
		t.Fatalf("resolveConnectionHierarchy (post-dismiss): %v", err)
	}
	if cID != nil || cName != nil || gID != nil || gName != nil {
		t.Fatalf("resolver resurrected a dismissed cluster: cID=%v cName=%v gID=%v gName=%v (issue #36)",
			cID, cName, gID, gName)
	}

	// The dismissed row must still be marked dismissed, and there must be
	// no second (live) row carrying the same auto_cluster_key.
	var (
		totalRows     int
		dismissedRows int
		liveRows      int
	)
	if err := pool.QueryRow(ctx, `
        SELECT
            COUNT(*),
            COUNT(*) FILTER (WHERE dismissed = TRUE),
            COUNT(*) FILTER (WHERE dismissed = FALSE)
        FROM clusters
        WHERE auto_cluster_key = 'spock:pg17'
    `).Scan(&totalRows, &dismissedRows, &liveRows); err != nil {
		t.Fatalf("count clusters by auto key: %v", err)
	}
	if totalRows != 1 || dismissedRows != 1 || liveRows != 0 {
		t.Fatalf("unexpected clusters rows after resolver call: total=%d dismissed=%d live=%d (issue #36)",
			totalRows, dismissedRows, liveRows)
	}

	// 5) ListClustersForAutocomplete must still hide the dismissed cluster.
	summaries, err := ds.ListClustersForAutocomplete(ctx)
	if err != nil {
		t.Fatalf("ListClustersForAutocomplete: %v", err)
	}
	for _, s := range summaries {
		if s.AutoClusterKey != nil && *s.AutoClusterKey == "spock:pg17" {
			t.Fatalf("dropdown re-surfaced dismissed auto cluster: %+v (issue #36)", s)
		}
		if s.ID == clusterID {
			t.Fatalf("dropdown re-surfaced dismissed cluster id %d (issue #36)", clusterID)
		}
	}
}

// TestResolveConnectionHierarchy_InsertBranchDoesNotResurrect exercises
// the INSERT fallback inside resolveConnectionHierarchy when a dismissed
// cluster row already occupies the auto_cluster_key. The row is pre-seeded
// directly via SQL (bypassing DeleteCluster) so the test isolates the
// INSERT … ON CONFLICT DO NOTHING + re-SELECT WHERE dismissed = FALSE
// path. Before the fix, the ON CONFLICT … DO UPDATE SET updated_at
// clause would silently surface the dismissed row by returning it via
// RETURNING.
//
// With the fixed initial SELECT that reads cl.dismissed, the presence of
// the dismissed row causes the function to return nil before reaching the
// INSERT branch. This test therefore also validates the SELECT-path
// dismissed check from a different angle: the row was never part of the
// normal upsert-then-dismiss lifecycle. It was dismissed from birth, as
// would happen if a previous process created and dismissed it and only
// the tombstone remains.
func TestResolveConnectionHierarchy_InsertBranchDoesNotResurrect(t *testing.T) {
	ds, pool, cleanup := newResolveHierarchyTestDatastore(t)
	defer cleanup()

	ctx := context.Background()

	// 1) Default group.
	var groupID int
	if err := pool.QueryRow(ctx, `
        INSERT INTO cluster_groups (name, description, is_shared, is_default)
        VALUES ('Servers/Clusters', 'default', TRUE, TRUE)
        RETURNING id
    `).Scan(&groupID); err != nil {
		t.Fatalf("insert default group: %v", err)
	}

	// 2) Pre-seed a dismissed cluster row directly. We intentionally
	//    bypass DeleteCluster so the row starts as dismissed. This
	//    simulates a cluster that was dismissed in a previous session
	//    and only exists as a tombstone in the database.
	autoKey := "spock:pg17"
	var dismissedClusterID int
	if err := pool.QueryRow(ctx, `
        INSERT INTO clusters (name, auto_cluster_key, group_id, dismissed)
        VALUES ('pg17 Spock', $1, $2, TRUE)
        RETURNING id`, autoKey, groupID).Scan(&dismissedClusterID); err != nil {
		t.Fatalf("pre-seed dismissed cluster: %v", err)
	}

	// Sanity: the row is dismissed.
	var dismissed bool
	if err := pool.QueryRow(ctx,
		`SELECT dismissed FROM clusters WHERE id = $1`,
		dismissedClusterID).Scan(&dismissed); err != nil {
		t.Fatalf("read pre-seeded dismissed flag: %v", err)
	}
	if !dismissed {
		t.Fatalf("pre-seeded cluster is not dismissed")
	}

	// 3) Two Spock connections that form a cluster keyed 'spock:pg17'.
	//    Two nodes are needed so the cluster type is "spock" rather than
	//    "server" (standalone servers are skipped by the resolver).
	var connID int
	for _, name := range []string{"pg17-node1", "pg17-node2"} {
		var id int
		if err := pool.QueryRow(ctx, `
            INSERT INTO connections (
                name, host, port, database_name, username,
                owner_username, is_monitored, membership_source
            )
            VALUES ($1, '10.0.0.1', 5432, 'postgres', 'postgres',
                    'alice', TRUE, 'auto')
            RETURNING id`, name).Scan(&id); err != nil {
			t.Fatalf("insert connection %s: %v", name, err)
		}
		if connID == 0 {
			connID = id
		}
		if _, err := pool.Exec(ctx, `
            INSERT INTO metrics.pg_connectivity (connection_id, collected_at)
            VALUES ($1, NOW())`, id); err != nil {
			t.Fatalf("insert pg_connectivity for %s: %v", name, err)
		}
		if _, err := pool.Exec(ctx, `
            INSERT INTO metrics.pg_node_role (
                connection_id, primary_role, has_spock,
                binary_standby_count, is_streaming_standby, collected_at
            )
            VALUES ($1, 'primary', TRUE, 0, FALSE, NOW())`, id); err != nil {
			t.Fatalf("insert pg_node_role for %s: %v", name, err)
		}
	}

	// 4) resolveConnectionHierarchy must return nil hierarchy because
	//    the matching cluster is dismissed. Before the fix, this call
	//    would return the dismissed cluster's id and name.
	cID, cName, gID, gName, err := ds.resolveConnectionHierarchy(ctx, connID)
	if err != nil {
		t.Fatalf("resolveConnectionHierarchy: %v", err)
	}
	if cID != nil || cName != nil || gID != nil || gName != nil {
		t.Fatalf("resolver resurrected a pre-seeded dismissed cluster: "+
			"cID=%v cName=%v gID=%v gName=%v (issue #36)",
			cID, cName, gID, gName)
	}

	// 5) The dismissed row must be unchanged: still dismissed, and no
	//    second (live) row inserted alongside it.
	if err := pool.QueryRow(ctx,
		`SELECT dismissed FROM clusters WHERE id = $1`,
		dismissedClusterID).Scan(&dismissed); err != nil {
		t.Fatalf("read dismissed flag after resolve: %v", err)
	}
	if !dismissed {
		t.Fatalf("resolver cleared dismissed flag on pre-seeded row (issue #36)")
	}

	var totalRows, liveRows int
	if err := pool.QueryRow(ctx, `
        SELECT
            COUNT(*),
            COUNT(*) FILTER (WHERE dismissed = FALSE)
        FROM clusters
        WHERE auto_cluster_key = $1`, autoKey).Scan(&totalRows, &liveRows); err != nil {
		t.Fatalf("count clusters by auto key: %v", err)
	}
	if totalRows != 1 || liveRows != 0 {
		t.Fatalf("unexpected cluster rows: total=%d live=%d (issue #36)",
			totalRows, liveRows)
	}

	// 6) ListClustersForAutocomplete must not return the dismissed cluster.
	summaries, err := ds.ListClustersForAutocomplete(ctx)
	if err != nil {
		t.Fatalf("ListClustersForAutocomplete: %v", err)
	}
	if containsClusterID(summaries, dismissedClusterID) {
		t.Fatalf("dropdown returned dismissed cluster %d after resolve (issue #36)",
			dismissedClusterID)
	}
}
