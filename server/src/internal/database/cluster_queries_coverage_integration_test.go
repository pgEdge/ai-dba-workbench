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
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// clusterQueriesTestSchema creates the cluster hierarchy tables and the
// two metrics tables that cluster_queries.go reads. It mirrors the
// production shape (collector/src/database/schema.go), limited to the
// columns those queries reference.
const clusterQueriesTestSchema = `
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
DROP TABLE IF EXISTS cluster_groups CASCADE;
DROP SCHEMA IF EXISTS metrics CASCADE;

CREATE TABLE cluster_groups (
    id SERIAL PRIMARY KEY,
    owner_username VARCHAR(255),
    owner_token VARCHAR(255),
    is_shared BOOLEAN NOT NULL DEFAULT FALSE,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    auto_group_key VARCHAR(255) UNIQUE,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT cluster_groups_name_unique UNIQUE (name)
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
    host VARCHAR(255),
    port INTEGER NOT NULL DEFAULT 5432,
    database_name VARCHAR(255) NOT NULL DEFAULT 'postgres',
    role VARCHAR(64),
    is_monitored BOOLEAN NOT NULL DEFAULT FALSE,
    connection_error TEXT,
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE SET NULL,
    membership_source VARCHAR(16) NOT NULL DEFAULT 'auto',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE SCHEMA metrics;

CREATE TABLE metrics.pg_stat_activity (
    connection_id INTEGER NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE metrics.pg_connectivity (
    connection_id INTEGER NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL
);
`

const clusterQueriesTestTeardown = `
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
DROP TABLE IF EXISTS cluster_groups CASCADE;
DROP SCHEMA IF EXISTS metrics CASCADE;
`

// newClusterQueriesTestDatastore wires up a *Datastore against the
// TEST_AI_WORKBENCH_SERVER Postgres instance with the tables
// cluster_queries.go needs. The caller receives a cleanup that drops the
// schema and closes the pool.
func newClusterQueriesTestDatastore(t *testing.T) (*Datastore, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping cluster queries integration test")
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

	if _, err := pool.Exec(ctx, clusterQueriesTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create cluster queries test schema: %v", err)
	}

	ds := NewTestDatastore(pool)

	cleanup := func() {
		if _, err := pool.Exec(context.Background(), clusterQueriesTestTeardown); err != nil {
			t.Logf("cluster queries teardown failed: %v", err)
		}
		pool.Close()
	}

	return ds, pool, cleanup
}

// mustExec runs a statement on the pool and fails the test on error.
func mustExecClusterQueries(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("Exec %q: %v", sql, err)
	}
}

// insertClusterQueriesConnection inserts a connection row and returns
// its id.
func insertClusterQueriesConnection(t *testing.T, pool *pgxpool.Pool, name string, clusterID *int, role *string) int {
	t.Helper()
	var id int
	if err := pool.QueryRow(context.Background(), `
        INSERT INTO connections (name, host, cluster_id, role)
        VALUES ($1, 'db.example.com', $2, $3)
        RETURNING id
    `, name, clusterID, role).Scan(&id); err != nil {
		t.Fatalf("Insert connection %q: %v", name, err)
	}
	return id
}

// TestClusterGroupCRUD_Integration drives the cluster group read, create,
// update and delete paths, including the not-found and duplicate cases.
func TestClusterGroupCRUD_Integration(t *testing.T) {
	ds, _, cleanup := newClusterQueriesTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	groups, err := ds.GetClusterGroups(ctx)
	if err != nil {
		t.Fatalf("GetClusterGroups (empty): %v", err)
	}
	if len(groups) != 0 {
		t.Fatalf("Expected no groups, got %d", len(groups))
	}

	beta, err := ds.CreateClusterGroup(ctx, "Beta", strPtr("second"))
	if err != nil {
		t.Fatalf("CreateClusterGroup: %v", err)
	}
	if !beta.IsShared {
		t.Errorf("CreateClusterGroup should create a shared group")
	}
	if _, err := ds.CreateClusterGroup(ctx, "Alpha", nil); err != nil {
		t.Fatalf("CreateClusterGroup (Alpha): %v", err)
	}
	if _, err := ds.CreateClusterGroup(ctx, "Alpha", nil); err == nil {
		t.Errorf("Expected duplicate group name to fail")
	}

	groups, err = ds.GetClusterGroups(ctx)
	if err != nil {
		t.Fatalf("GetClusterGroups: %v", err)
	}
	if len(groups) != 2 || groups[0].Name != "Alpha" || groups[1].Name != "Beta" {
		t.Fatalf("Unexpected groups: %+v", groups)
	}

	got, err := ds.GetClusterGroup(ctx, beta.ID)
	if err != nil {
		t.Fatalf("GetClusterGroup: %v", err)
	}
	if got.Name != "Beta" || got.Description == nil || *got.Description != "second" {
		t.Errorf("Unexpected group: %+v", got)
	}
	if _, err := ds.GetClusterGroup(ctx, beta.ID+1000); err == nil {
		t.Errorf("Expected GetClusterGroup on a missing id to fail")
	}

	if _, err := ds.UpdateClusterGroup(ctx, beta.ID+1000, "Nope", nil, nil); err == nil {
		t.Errorf("Expected UpdateClusterGroup on a missing id to fail")
	}

	if err := ds.DeleteClusterGroup(ctx, beta.ID); err != nil {
		t.Fatalf("DeleteClusterGroup: %v", err)
	}
	if err := ds.DeleteClusterGroup(ctx, beta.ID); !errors.Is(err, ErrClusterGroupNotFound) {
		t.Errorf("Expected ErrClusterGroupNotFound, got %v", err)
	}
}

// TestClusterCRUD_Integration drives the cluster read, create, update,
// partial update and delete paths.
func TestClusterCRUD_Integration(t *testing.T) {
	ds, pool, cleanup := newClusterQueriesTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	group, err := ds.CreateClusterGroup(ctx, "Group", nil)
	if err != nil {
		t.Fatalf("CreateClusterGroup: %v", err)
	}
	other, err := ds.CreateClusterGroup(ctx, "Other", nil)
	if err != nil {
		t.Fatalf("CreateClusterGroup (Other): %v", err)
	}

	zeta, err := ds.CreateCluster(ctx, group.ID, "Zeta", strPtr("z"))
	if err != nil {
		t.Fatalf("CreateCluster: %v", err)
	}
	if _, err := ds.CreateCluster(ctx, group.ID, "Alpha", nil); err != nil {
		t.Fatalf("CreateCluster (Alpha): %v", err)
	}
	if _, err := ds.CreateCluster(ctx, group.ID, "Alpha", nil); err == nil {
		t.Errorf("Expected duplicate cluster name in a group to fail")
	}

	clusters, err := ds.GetClustersInGroup(ctx, group.ID)
	if err != nil {
		t.Fatalf("GetClustersInGroup: %v", err)
	}
	if len(clusters) != 2 || clusters[0].Name != "Alpha" || clusters[1].Name != "Zeta" {
		t.Fatalf("Unexpected clusters: %+v", clusters)
	}

	got, err := ds.GetCluster(ctx, zeta.ID)
	if err != nil {
		t.Fatalf("GetCluster: %v", err)
	}
	if got.Name != "Zeta" || !got.GroupID.Valid || int(got.GroupID.Int32) != group.ID {
		t.Errorf("Unexpected cluster: %+v", got)
	}
	if _, err := ds.GetCluster(ctx, zeta.ID+1000); err == nil {
		t.Errorf("Expected GetCluster on a missing id to fail")
	}

	updated, err := ds.UpdateCluster(ctx, zeta.ID, other.ID, "Zeta2", nil)
	if err != nil {
		t.Fatalf("UpdateCluster: %v", err)
	}
	if updated.Name != "Zeta2" || int(updated.GroupID.Int32) != other.ID || updated.Description != nil {
		t.Errorf("Unexpected updated cluster: %+v", updated)
	}
	if _, err := ds.UpdateCluster(ctx, zeta.ID+1000, other.ID, "x", nil); err == nil {
		t.Errorf("Expected UpdateCluster on a missing id to fail")
	}

	t.Run("partial with every field", func(t *testing.T) {
		c, err := ds.UpdateClusterPartial(ctx, zeta.ID, &group.ID, "Zeta3",
			strPtr("desc"), strPtr("spock"))
		if err != nil {
			t.Fatalf("UpdateClusterPartial: %v", err)
		}
		if c.Name != "Zeta3" || int(c.GroupID.Int32) != group.ID ||
			c.Description == nil || *c.Description != "desc" ||
			c.ReplicationType == nil || *c.ReplicationType != "spock" {
			t.Errorf("Unexpected cluster: %+v", c)
		}
	})

	t.Run("partial with no fields", func(t *testing.T) {
		c, err := ds.UpdateClusterPartial(ctx, zeta.ID, nil, "", nil, nil)
		if err != nil {
			t.Fatalf("UpdateClusterPartial: %v", err)
		}
		if c.Name != "Zeta3" {
			t.Errorf("Name changed unexpectedly: %q", c.Name)
		}
	})

	t.Run("partial on a missing id", func(t *testing.T) {
		if _, err := ds.UpdateClusterPartial(ctx, zeta.ID+1000, nil, "x", nil, nil); err == nil {
			t.Errorf("Expected UpdateClusterPartial on a missing id to fail")
		}
	})

	t.Run("delete user cluster", func(t *testing.T) {
		if err := ds.DeleteCluster(ctx, zeta.ID); err != nil {
			t.Fatalf("DeleteCluster: %v", err)
		}
		var n int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM clusters WHERE id = $1",
			zeta.ID).Scan(&n); err != nil {
			t.Fatalf("Count: %v", err)
		}
		if n != 0 {
			t.Errorf("User-created cluster was not hard-deleted")
		}
		if err := ds.DeleteCluster(ctx, zeta.ID); !errors.Is(err, ErrClusterNotFound) {
			t.Errorf("Expected ErrClusterNotFound, got %v", err)
		}
	})

	t.Run("delete auto-detected cluster detaches connections", func(t *testing.T) {
		auto, err := ds.UpsertClusterByAutoKey(ctx, "binary:crud", "auto")
		if err != nil {
			t.Fatalf("UpsertClusterByAutoKey: %v", err)
		}
		connID := insertClusterQueriesConnection(t, pool, "member", &auto.ID, nil)
		if err := ds.DeleteCluster(ctx, auto.ID); err != nil {
			t.Fatalf("DeleteCluster: %v", err)
		}
		var clusterID *int
		if err := pool.QueryRow(ctx, "SELECT cluster_id FROM connections WHERE id = $1",
			connID).Scan(&clusterID); err != nil {
			t.Fatalf("Read back: %v", err)
		}
		if clusterID != nil {
			t.Errorf("Connection still attached to dismissed cluster %d", *clusterID)
		}
	})
}

// TestDeriveClusterNameFromKey covers every prefix the helper recognizes.
func TestDeriveClusterNameFromKey_AllPrefixes(t *testing.T) {
	cases := map[string]string{
		"spock:prod":      "prod Spock",
		"binary:42":       "binary-42",
		"standalone:7":    "standalone-7",
		"logical:pub":     "logical-pub",
		"other:thing":     "other:thing",
		"no-separator":    "no-separator",
		"spock:with:more": "with:more Spock",
	}
	for in, want := range cases {
		if got := deriveClusterNameFromKey(in); got != want {
			t.Errorf("deriveClusterNameFromKey(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestDeleteAutoDetectedCluster_Integration covers both the placeholder
// path and the dismiss-existing path, plus a failing insert.
func TestDeleteAutoDetectedCluster_Integration(t *testing.T) {
	ds, pool, cleanup := newClusterQueriesTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("placeholder for unknown key", func(t *testing.T) {
		if err := ds.DeleteAutoDetectedCluster(ctx, "spock:ghost"); err != nil {
			t.Fatalf("DeleteAutoDetectedCluster: %v", err)
		}
		var name string
		var dismissed bool
		if err := pool.QueryRow(ctx,
			"SELECT name, dismissed FROM clusters WHERE auto_cluster_key = 'spock:ghost'",
		).Scan(&name, &dismissed); err != nil {
			t.Fatalf("Read placeholder: %v", err)
		}
		if name != "ghost Spock" || !dismissed {
			t.Errorf("Unexpected placeholder name=%q dismissed=%v", name, dismissed)
		}
	})

	t.Run("dismisses existing cluster", func(t *testing.T) {
		auto, err := ds.UpsertClusterByAutoKey(ctx, "logical:real", "real")
		if err != nil {
			t.Fatalf("UpsertClusterByAutoKey: %v", err)
		}
		connID := insertClusterQueriesConnection(t, pool, "member", &auto.ID, nil)
		if err := ds.DeleteAutoDetectedCluster(ctx, "logical:real"); err != nil {
			t.Fatalf("DeleteAutoDetectedCluster: %v", err)
		}
		var dismissed bool
		if err := pool.QueryRow(ctx, "SELECT dismissed FROM clusters WHERE id = $1",
			auto.ID).Scan(&dismissed); err != nil {
			t.Fatalf("Read dismissed: %v", err)
		}
		var clusterID *int
		if err := pool.QueryRow(ctx, "SELECT cluster_id FROM connections WHERE id = $1",
			connID).Scan(&clusterID); err != nil {
			t.Fatalf("Read connection: %v", err)
		}
		if !dismissed || clusterID != nil {
			t.Errorf("dismissed=%v cluster_id=%v", dismissed, clusterID)
		}
	})

	t.Run("insert failure", func(t *testing.T) {
		longKey := "binary:" + strings.Repeat("x", 300)
		if err := ds.DeleteAutoDetectedCluster(ctx, longKey); err == nil {
			t.Errorf("Expected an over-long key to fail the placeholder insert")
		}
	})
}

// TestDismissAutoDetectedClusterKeys_Integration covers a batch holding a
// known key and an unknown key, plus a failing insert.
func TestDismissAutoDetectedClusterKeys_Integration(t *testing.T) {
	ds, pool, cleanup := newClusterQueriesTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	auto, err := ds.UpsertClusterByAutoKey(ctx, "spock:known", "known")
	if err != nil {
		t.Fatalf("UpsertClusterByAutoKey: %v", err)
	}
	connID := insertClusterQueriesConnection(t, pool, "member", &auto.ID, nil)

	if err := ds.DismissAutoDetectedClusterKeys(ctx,
		[]string{"spock:known", "standalone:new"}); err != nil {
		t.Fatalf("DismissAutoDetectedClusterKeys: %v", err)
	}

	rows, err := pool.Query(ctx,
		"SELECT auto_cluster_key, name, dismissed FROM clusters ORDER BY auto_cluster_key")
	if err != nil {
		t.Fatalf("Query clusters: %v", err)
	}
	defer rows.Close()
	seen := map[string]string{}
	for rows.Next() {
		var key, name string
		var dismissed bool
		if err := rows.Scan(&key, &name, &dismissed); err != nil {
			t.Fatalf("Scan: %v", err)
		}
		if !dismissed {
			t.Errorf("Cluster %q not dismissed", key)
		}
		seen[key] = name
	}
	if seen["spock:known"] != "known" || seen["standalone:new"] != "standalone-new" {
		t.Errorf("Unexpected clusters: %v", seen)
	}

	var clusterID *int
	if err := pool.QueryRow(ctx, "SELECT cluster_id FROM connections WHERE id = $1",
		connID).Scan(&clusterID); err != nil {
		t.Fatalf("Read connection: %v", err)
	}
	if clusterID != nil {
		t.Errorf("Connection still attached to dismissed cluster")
	}

	if err := ds.DismissAutoDetectedClusterKeys(ctx, nil); err != nil {
		t.Errorf("Empty batch should succeed, got %v", err)
	}

	longKey := "binary:" + strings.Repeat("x", 300)
	if err := ds.DismissAutoDetectedClusterKeys(ctx, []string{longKey}); err == nil {
		t.Errorf("Expected an over-long key to fail the placeholder insert")
	}
}

// TestClusterOverridesAndUpserts_Integration covers the auto-key upserts
// and the override maps built from them.
func TestClusterOverridesAndUpserts_Integration(t *testing.T) {
	ds, pool, cleanup := newClusterQueriesTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	group, err := ds.CreateClusterGroup(ctx, "Group", nil)
	if err != nil {
		t.Fatalf("CreateClusterGroup: %v", err)
	}

	t.Run("UpsertClusterByAutoKey inserts then renames and restores", func(t *testing.T) {
		c, err := ds.UpsertClusterByAutoKey(ctx, "binary:1", "first")
		if err != nil {
			t.Fatalf("UpsertClusterByAutoKey: %v", err)
		}
		mustExecClusterQueries(t, pool, "UPDATE clusters SET dismissed = TRUE WHERE id = $1", c.ID)
		renamed, err := ds.UpsertClusterByAutoKey(ctx, "binary:1", "renamed")
		if err != nil {
			t.Fatalf("UpsertClusterByAutoKey (rename): %v", err)
		}
		if renamed.ID != c.ID || renamed.Name != "renamed" {
			t.Errorf("Unexpected rename result: %+v", renamed)
		}
		var dismissed bool
		if err := pool.QueryRow(ctx, "SELECT dismissed FROM clusters WHERE id = $1",
			c.ID).Scan(&dismissed); err != nil {
			t.Fatalf("Read dismissed: %v", err)
		}
		if dismissed {
			t.Errorf("Explicit rename should clear dismissed")
		}
		if _, err := ds.UpsertClusterByAutoKey(ctx, "binary:2",
			strings.Repeat("n", 300)); err == nil {
			t.Errorf("Expected an over-long name to fail")
		}
	})

	t.Run("UpsertAutoDetectedCluster", func(t *testing.T) {
		if _, err := ds.UpsertAutoDetectedCluster(ctx, "spock:new", "", nil, nil); err == nil ||
			!strings.Contains(err.Error(), "name is required") {
			t.Errorf("Expected name-required error, got %v", err)
		}
		created, err := ds.UpsertAutoDetectedCluster(ctx, "spock:new", "New",
			strPtr("described"), &group.ID)
		if err != nil {
			t.Fatalf("UpsertAutoDetectedCluster (create): %v", err)
		}
		if created.Name != "New" || int(created.GroupID.Int32) != group.ID {
			t.Errorf("Unexpected created cluster: %+v", created)
		}
		updated, err := ds.UpsertAutoDetectedCluster(ctx, "spock:new", "Newer",
			strPtr("changed"), &group.ID)
		if err != nil {
			t.Fatalf("UpsertAutoDetectedCluster (update): %v", err)
		}
		if updated.ID != created.ID || updated.Name != "Newer" ||
			updated.Description == nil || *updated.Description != "changed" {
			t.Errorf("Unexpected updated cluster: %+v", updated)
		}
		untouched, err := ds.UpsertAutoDetectedCluster(ctx, "spock:new", "", nil, nil)
		if err != nil {
			t.Fatalf("UpsertAutoDetectedCluster (no fields): %v", err)
		}
		if untouched.Name != "Newer" {
			t.Errorf("Name changed unexpectedly: %q", untouched.Name)
		}
		if _, err := ds.UpsertAutoDetectedCluster(ctx, "spock:new",
			strings.Repeat("n", 300), nil, nil); err == nil {
			t.Errorf("Expected an over-long name to fail the update")
		}
		missingGroup := group.ID + 1000
		if _, err := ds.UpsertAutoDetectedCluster(ctx, "spock:other", "Other",
			nil, &missingGroup); err == nil {
			t.Errorf("Expected a missing group to fail the insert")
		}
	})

	t.Run("GetClusterOverrides skips dismissed rows", func(t *testing.T) {
		if err := ds.DeleteAutoDetectedCluster(ctx, "logical:hidden"); err != nil {
			t.Fatalf("DeleteAutoDetectedCluster: %v", err)
		}
		overrides, err := ds.GetClusterOverrides(ctx)
		if err != nil {
			t.Fatalf("GetClusterOverrides: %v", err)
		}
		if got := overrides["spock:new"]; got.Name != "Newer" || got.Description != "changed" {
			t.Errorf("Unexpected override for spock:new: %+v", got)
		}
		if got := overrides["binary:1"]; got.Name != "renamed" || got.Description != "" {
			t.Errorf("Unexpected override for binary:1: %+v", got)
		}
		if _, ok := overrides["logical:hidden"]; ok {
			t.Errorf("Dismissed cluster appeared in overrides")
		}
	})

	t.Run("group auto keys", func(t *testing.T) {
		g, err := ds.UpsertGroupByAutoKey(ctx, "default", "Servers")
		if err != nil {
			t.Fatalf("UpsertGroupByAutoKey: %v", err)
		}
		again, err := ds.UpsertGroupByAutoKey(ctx, "default", "Renamed Servers")
		if err != nil {
			t.Fatalf("UpsertGroupByAutoKey (rename): %v", err)
		}
		if again.ID != g.ID || again.Name != "Renamed Servers" || !again.IsShared ||
			!again.AutoGroupKey.Valid || again.AutoGroupKey.String != "default" {
			t.Errorf("Unexpected group: %+v", again)
		}
		if _, err := ds.UpsertGroupByAutoKey(ctx, "dupe", "Group"); err == nil {
			t.Errorf("Expected a duplicate group name to fail")
		}
		overrides, err := ds.GetGroupOverrides(ctx)
		if err != nil {
			t.Fatalf("GetGroupOverrides: %v", err)
		}
		if len(overrides) != 1 || overrides["default"] != "Renamed Servers" {
			t.Errorf("Unexpected group overrides: %v", overrides)
		}
	})

	t.Run("GetDefaultGroupID", func(t *testing.T) {
		mustExecClusterQueries(t, pool, "UPDATE cluster_groups SET is_default = TRUE WHERE id = $1", group.ID)
		id, err := ds.GetDefaultGroupID(ctx)
		if err != nil {
			t.Fatalf("GetDefaultGroupID: %v", err)
		}
		if id != group.ID {
			t.Errorf("GetDefaultGroupID = %d, want %d", id, group.ID)
		}
	})
}

// TestClusterServersAndHierarchy_Integration covers the server listings
// and the hierarchy the web client renders.
func TestClusterServersAndHierarchy_Integration(t *testing.T) {
	ds, pool, cleanup := newClusterQueriesTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	group, err := ds.CreateClusterGroup(ctx, "Group", nil)
	if err != nil {
		t.Fatalf("CreateClusterGroup: %v", err)
	}
	if _, err := ds.CreateClusterGroup(ctx, "Empty", nil); err != nil {
		t.Fatalf("CreateClusterGroup (Empty): %v", err)
	}
	cluster, err := ds.CreateCluster(ctx, group.ID, "Cluster", nil)
	if err != nil {
		t.Fatalf("CreateCluster: %v", err)
	}

	primary := insertClusterQueriesConnection(t, pool, "b-primary", &cluster.ID, strPtr("primary"))
	warn := insertClusterQueriesConnection(t, pool, "a-replica", &cluster.ID, strPtr("replica"))
	stale := insertClusterQueriesConnection(t, pool, "c-norole", &cluster.ID, nil)
	ungrouped := insertClusterQueriesConnection(t, pool, "loner", nil, nil)
	mustExecClusterQueries(t, pool, "UPDATE connections SET connection_error = 'refused' WHERE id = $1", ungrouped)

	mustExecClusterQueries(t, pool, `
        INSERT INTO metrics.pg_stat_activity (connection_id, collected_at) VALUES
            ($1, NOW() - INTERVAL '10 seconds'),
            ($2, NOW() - INTERVAL '3 minutes'),
            ($3, NOW() - INTERVAL '1 hour')
    `, primary, warn, stale)
	mustExecClusterQueries(t, pool, `
        INSERT INTO metrics.pg_connectivity (connection_id, collected_at) VALUES
            ($1, NOW() - INTERVAL '10 seconds'),
            ($2, NOW() - INTERVAL '100 seconds')
    `, primary, warn)

	t.Run("GetServersInCluster", func(t *testing.T) {
		servers, err := ds.GetServersInCluster(ctx, cluster.ID)
		if err != nil {
			t.Fatalf("GetServersInCluster: %v", err)
		}
		if len(servers) != 3 {
			t.Fatalf("Expected 3 servers, got %d", len(servers))
		}
		want := []struct {
			name, status string
		}{
			{"b-primary", "online"},
			{"a-replica", "warning"},
			{"c-norole", "offline"},
		}
		for i, w := range want {
			if servers[i].Name != w.name || servers[i].Status != w.status {
				t.Errorf("servers[%d] = %s/%s, want %s/%s", i,
					servers[i].Name, servers[i].Status, w.name, w.status)
			}
		}
		if servers[0].Role == nil || *servers[0].Role != "primary" {
			t.Errorf("Primary role not populated: %v", servers[0].Role)
		}
		if servers[2].Role != nil {
			t.Errorf("Null role should stay nil, got %q", *servers[2].Role)
		}
	})

	t.Run("GetClusterHierarchy", func(t *testing.T) {
		hierarchy, err := ds.GetClusterHierarchy(ctx)
		if err != nil {
			t.Fatalf("GetClusterHierarchy: %v", err)
		}
		if len(hierarchy) != 3 {
			t.Fatalf("Expected Empty, Group and Ungrouped, got %+v", hierarchy)
		}
		if hierarchy[0].Name != "Empty" || len(hierarchy[0].Clusters) != 0 {
			t.Errorf("Unexpected first group: %+v", hierarchy[0])
		}
		g := hierarchy[1]
		if g.Name != "Group" || len(g.Clusters) != 1 || len(g.Clusters[0].Servers) != 3 {
			t.Fatalf("Unexpected second group: %+v", g)
		}
		servers := g.Clusters[0].Servers
		if servers[0].Name != "b-primary" || servers[0].Status != "online" {
			t.Errorf("Unexpected first server: %+v", servers[0])
		}
		if servers[1].Status != "warning" || servers[2].Status != "unknown" {
			t.Errorf("Unexpected statuses: %s, %s", servers[1].Status, servers[2].Status)
		}
		u := hierarchy[2]
		if u.ID != "group-ungrouped" || len(u.Clusters) != 1 {
			t.Fatalf("Unexpected ungrouped group: %+v", u)
		}
		loner := u.Clusters[0].Servers[0]
		if loner.Name != "loner" || loner.ConnectionError == nil ||
			*loner.ConnectionError != "refused" || loner.Role == nil || *loner.Role != "primary" {
			t.Errorf("Unexpected ungrouped server: %+v", loner)
		}
	})

	t.Run("hierarchy without ungrouped servers", func(t *testing.T) {
		mustExecClusterQueries(t, pool, "UPDATE connections SET connection_error = 'down' WHERE id = $1", stale)
		mustExecClusterQueries(t, pool, "DELETE FROM connections WHERE id = $1", ungrouped)
		hierarchy, err := ds.GetClusterHierarchy(ctx)
		if err != nil {
			t.Fatalf("GetClusterHierarchy: %v", err)
		}
		if len(hierarchy) != 2 {
			t.Fatalf("Expected no Ungrouped group, got %+v", hierarchy)
		}
		servers := hierarchy[1].Clusters[0].Servers
		if servers[2].ConnectionError == nil || *servers[2].ConnectionError != "down" {
			t.Errorf("Connection error not populated: %+v", servers[2])
		}
	})

	t.Run("scan failures", func(t *testing.T) {
		mustExecClusterQueries(t, pool, "UPDATE connections SET host = NULL")
		if _, err := ds.GetServersInCluster(ctx, cluster.ID); err == nil {
			t.Errorf("Expected a NULL host to fail GetServersInCluster")
		}
		if _, err := ds.GetClusterHierarchy(ctx); err == nil {
			t.Errorf("Expected a NULL host to fail the cluster server listing")
		}
		mustExecClusterQueries(t, pool, "UPDATE connections SET cluster_id = NULL")
		if _, err := ds.GetClusterHierarchy(ctx); err == nil {
			t.Errorf("Expected a NULL host to fail the ungrouped server listing")
		}
	})
}

// TestClusterQueries_DatastoreErrors drops the schema out from under the
// datastore so that every query reports a wrapped error.
func TestClusterQueries_DatastoreErrors(t *testing.T) {
	ds, pool, cleanup := newClusterQueriesTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("missing metrics tables", func(t *testing.T) {
		group, err := ds.CreateClusterGroup(ctx, "Group", nil)
		if err != nil {
			t.Fatalf("CreateClusterGroup: %v", err)
		}
		if _, err := ds.CreateCluster(ctx, group.ID, "Cluster", nil); err != nil {
			t.Fatalf("CreateCluster: %v", err)
		}
		mustExecClusterQueries(t, pool, "DROP SCHEMA metrics CASCADE")
		if _, err := ds.GetServersInCluster(ctx, 1); err == nil {
			t.Errorf("GetServersInCluster: expected an error")
		}
		if _, err := ds.GetClusterHierarchy(ctx); err == nil {
			t.Errorf("GetClusterHierarchy (cluster servers): expected an error")
		}
		mustExecClusterQueries(t, pool, "DELETE FROM clusters")
		if _, err := ds.GetClusterHierarchy(ctx); err == nil {
			t.Errorf("GetClusterHierarchy (ungrouped servers): expected an error")
		}
	})

	t.Run("missing clusters table", func(t *testing.T) {
		mustExecClusterQueries(t, pool, "DROP TABLE connections CASCADE")
		if err := ds.DeleteCluster(ctx, 1); err == nil {
			t.Errorf("DeleteCluster: expected an error")
		}
		mustExecClusterQueries(t, pool, "DROP TABLE clusters CASCADE")
		checks := map[string]func() error{
			"GetClustersInGroup": func() error {
				_, err := ds.GetClustersInGroup(ctx, 1)
				return err
			},
			"GetClusterOverrides": func() error {
				_, err := ds.GetClusterOverrides(ctx)
				return err
			},
			"GetClusterHierarchy": func() error {
				_, err := ds.GetClusterHierarchy(ctx)
				return err
			},
			"DeleteAutoDetectedCluster": func() error {
				return ds.DeleteAutoDetectedCluster(ctx, "spock:x")
			},
			"DismissAutoDetectedClusterKeys": func() error {
				return ds.DismissAutoDetectedClusterKeys(ctx, []string{"spock:x"})
			},
		}
		for name, check := range checks {
			if err := check(); err == nil {
				t.Errorf("%s: expected an error", name)
			}
		}
	})

	t.Run("missing cluster_groups table", func(t *testing.T) {
		mustExecClusterQueries(t, pool, "DROP TABLE cluster_groups CASCADE")
		checks := map[string]func() error{
			"GetClusterGroups": func() error {
				_, err := ds.GetClusterGroups(ctx)
				return err
			},
			"GetGroupOverrides": func() error {
				_, err := ds.GetGroupOverrides(ctx)
				return err
			},
			"GetDefaultGroupID": func() error {
				_, err := ds.GetDefaultGroupID(ctx)
				return err
			},
			"GetClusterHierarchy": func() error {
				_, err := ds.GetClusterHierarchy(ctx)
				return err
			},
			"DeleteClusterGroup": func() error {
				return ds.DeleteClusterGroup(ctx, 1)
			},
		}
		for name, check := range checks {
			if err := check(); err == nil {
				t.Errorf("%s: expected an error", name)
			}
		}
	})
}

// TestClusterQueries_ExistingClusterErrorPaths drops the connections
// table after creating an auto-detected cluster, so that the detach step
// of each dismiss path fails.
func TestClusterQueries_ExistingClusterErrorPaths(t *testing.T) {
	ds, pool, cleanup := newClusterQueriesTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := ds.UpsertClusterByAutoKey(ctx, "spock:a", "a"); err != nil {
		t.Fatalf("UpsertClusterByAutoKey: %v", err)
	}
	auto, err := ds.UpsertClusterByAutoKey(ctx, "spock:b", "b")
	if err != nil {
		t.Fatalf("UpsertClusterByAutoKey: %v", err)
	}
	mustExecClusterQueries(t, pool, "DROP TABLE connections CASCADE")

	if err := ds.DeleteCluster(ctx, auto.ID); err == nil {
		t.Errorf("DeleteCluster: expected the detach to fail")
	}
	if err := ds.DeleteAutoDetectedCluster(ctx, "spock:a"); err == nil {
		t.Errorf("DeleteAutoDetectedCluster: expected the detach to fail")
	}
	if err := ds.DismissAutoDetectedClusterKeys(ctx, []string{"spock:a"}); err == nil {
		t.Errorf("DismissAutoDetectedClusterKeys: expected the detach to fail")
	}
}
