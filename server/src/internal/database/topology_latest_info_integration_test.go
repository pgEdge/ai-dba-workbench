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

// seedStaleServerInfo gives connectionID current connectivity and role
// samples, and server info, OS info and Spock version rows older than
// the windows the topology readers used to apply: change-tracked probes
// write a row only when a value changes, and the OS probe runs every ten
// minutes. Each table also gets an older row with superseded values, so
// a reader that picks any row other than the newest fails.
func seedStaleServerInfo(t *testing.T, ds *Datastore, connectionID int) {
	t.Helper()
	for _, sql := range []string{
		`INSERT INTO metrics.pg_connectivity (connection_id) VALUES ($1)`,
		`INSERT INTO metrics.pg_node_role (connection_id, primary_role, has_spock, spock_node_name)
            VALUES ($1, 'standalone', TRUE, 'n1')`,
		`INSERT INTO metrics.pg_server_info (connection_id, server_version, system_identifier, collected_at)
            VALUES ($1, '16.4', 111, NOW() - INTERVAL '30 days'),
                   ($1, '17.2', 7400000000000000001, NOW() - INTERVAL '3 hours')`,
		`INSERT INTO metrics.pg_sys_os_info (connection_id, name, collected_at)
            VALUES ($1, 'Old OS', NOW() - INTERVAL '20 minutes'),
                   ($1, 'Linux', NOW() - INTERVAL '9 minutes')`,
		`INSERT INTO metrics.pg_extension (connection_id, extname, extversion, collected_at)
            VALUES ($1, 'spock', '4.0.0', NOW() - INTERVAL '10 days'),
                   ($1, 'spock', '5.0.1', NOW() - INTERVAL '2 days')`,
	} {
		if _, err := ds.pool.Exec(context.Background(), sql, connectionID); err != nil {
			t.Fatalf("seed metrics: %v\n%s", err, sql)
		}
	}
}

// TestTopologyReadersUseLatestServerInfoIntegration checks that the
// three topology readers (all connections, persisted members of
// auto-detected clusters and members of manual clusters) return the
// newest stored PostgreSQL version, system identifier, OS and Spock
// version however old that row is, rather than only a row written in
// the last few minutes (issue #805).
func TestTopologyReadersUseLatestServerInfoIntegration(t *testing.T) {
	ds, _, fx, cleanup := newConfigQueriesTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	var clusterID, manualClusterID, standaloneID, memberID, manualID int
	if err := ds.pool.QueryRow(ctx, `INSERT INTO clusters (group_id, name, auto_cluster_key)
        VALUES ($1, 'auto-cls', 'spock:latest-info') RETURNING id`, fx.GroupID).Scan(&clusterID); err != nil {
		t.Fatalf("seed auto cluster: %v", err)
	}
	if err := ds.pool.QueryRow(ctx, `INSERT INTO connections (name)
        VALUES ('stale-info-standalone') RETURNING id`).Scan(&standaloneID); err != nil {
		t.Fatalf("seed standalone connection: %v", err)
	}
	if err := ds.pool.QueryRow(ctx, `INSERT INTO connections (name, cluster_id)
        VALUES ('stale-info-member', $1) RETURNING id`, clusterID).Scan(&memberID); err != nil {
		t.Fatalf("seed member connection: %v", err)
	}
	if err := ds.pool.QueryRow(ctx, `INSERT INTO clusters (group_id, name)
        VALUES ($1, 'manual-cls') RETURNING id`, fx.GroupID).Scan(&manualClusterID); err != nil {
		t.Fatalf("seed manual cluster: %v", err)
	}
	if err := ds.pool.QueryRow(ctx, `INSERT INTO connections (name, cluster_id)
        VALUES ('stale-info-manual', $1) RETURNING id`, manualClusterID).Scan(&manualID); err != nil {
		t.Fatalf("seed manual member connection: %v", err)
	}
	for _, id := range []int{standaloneID, memberID, manualID} {
		seedStaleServerInfo(t, ds, id)
	}

	conns, err := ds.getAllConnectionsWithRoles(ctx)
	if err != nil {
		t.Fatalf("getAllConnectionsWithRoles: %v", err)
	}
	found := false
	for _, c := range conns {
		if c.ID != standaloneID {
			continue
		}
		found = true
		if c.Version.String != "17.2" {
			t.Errorf("Version = %+v, want 17.2", c.Version)
		}
		if c.SystemIdentifier.Int64 != 7400000000000000001 {
			t.Errorf("SystemIdentifier = %+v, want 7400000000000000001", c.SystemIdentifier)
		}
		if c.OS.String != "Linux" {
			t.Errorf("OS = %+v, want Linux", c.OS)
		}
		if c.SpockVersion.String != "5.0.1" {
			t.Errorf("SpockVersion = %+v, want 5.0.1", c.SpockVersion)
		}
	}
	if !found {
		t.Fatalf("getAllConnectionsWithRoles did not return connection %d", standaloneID)
	}

	members, err := ds.getPersistedClusterMembers(ctx, nil)
	if err != nil {
		t.Fatalf("getPersistedClusterMembers: %v", err)
	}
	got := members["spock:latest-info"]
	if len(got) != 1 || got[0].ID != memberID {
		t.Fatalf("getPersistedClusterMembers = %+v, want only connection %d", members, memberID)
	}
	assertLatestServerInfo(t, "getPersistedClusterMembers", got[0])

	manual, err := ds.getServersInClusterWithRolesInternal(ctx, manualClusterID)
	if err != nil {
		t.Fatalf("getServersInClusterWithRolesInternal: %v", err)
	}
	if len(manual) != 1 || manual[0].ID != manualID {
		t.Fatalf("getServersInClusterWithRolesInternal = %+v, want only connection %d", manual, manualID)
	}
	assertLatestServerInfo(t, "getServersInClusterWithRolesInternal", manual[0])
}

// assertLatestServerInfo checks that a topology entry carries the newest
// values seedStaleServerInfo wrote.
func assertLatestServerInfo(t *testing.T, reader string, s TopologyServerInfo) {
	t.Helper()
	if s.Version != "17.2" || s.OS != "Linux" || s.SpockVersion != "5.0.1" {
		t.Errorf("%s Version = %q, OS = %q, SpockVersion = %q, want 17.2, Linux and 5.0.1",
			reader, s.Version, s.OS, s.SpockVersion)
	}
}
