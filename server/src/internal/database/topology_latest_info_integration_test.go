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
	"time"
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

// spockSnapshotRow is one metrics.pg_extension row seeded by
// TestTopologyReadersSpockVersionSnapshotIntegration. Rows sharing an
// age form one snapshot, as the collector writes them.
type spockSnapshotRow struct {
	age      string
	database string
	extname  string
	version  string
}

// TestTopologyReadersSpockVersionSnapshotIntegration checks that the
// three topology readers take the Spock version from the newest
// pg_extension snapshot only, preferring the connection's configured
// database and then the highest version (issue #805).
func TestTopologyReadersSpockVersionSnapshotIntegration(t *testing.T) {
	tests := []struct {
		name       string
		configured string
		rows       []spockSnapshotRow
		want       string
	}{
		{
			name:       "spock removed leaves the version blank",
			configured: "postgres",
			rows: []spockSnapshotRow{
				{"2 days", "postgres", "spock", "5.0.1"},
				{"2 days", "postgres", "plpgsql", "1.0"},
				{"1 hour", "postgres", "plpgsql", "1.0"},
			},
			want: "",
		},
		{
			name:       "partial snapshot without the spock database leaves the version blank",
			configured: "postgres",
			rows: []spockSnapshotRow{
				{"2 days", "postgres", "plpgsql", "1.0"},
				{"2 days", "appdb", "spock", "5.0.1"},
				{"1 hour", "postgres", "plpgsql", "1.0"},
			},
			want: "",
		},
		{
			name:       "full snapshot after a partial one restores the version",
			configured: "postgres",
			rows: []spockSnapshotRow{
				{"2 days", "appdb", "spock", "5.0.1"},
				{"1 hour", "postgres", "plpgsql", "1.0"},
				{"5 minutes", "postgres", "plpgsql", "1.0"},
				{"5 minutes", "appdb", "spock", "5.0.1"},
			},
			want: "5.0.1",
		},
		{
			name:       "configured database wins over a higher version elsewhere",
			configured: "appdb",
			rows: []spockSnapshotRow{
				{"2 days", "appdb", "spock", "6.0.0"},
				{"1 hour", "postgres", "spock", "5.0.10"},
				{"1 hour", "appdb", "spock", "4.0.1"},
			},
			want: "4.0.1",
		},
		{
			name:       "highest version wins when the configured database has none",
			configured: "postgres",
			rows: []spockSnapshotRow{
				{"1 hour", "postgres", "plpgsql", "1.0"},
				{"1 hour", "db_a", "spock", "5.0.9"},
				{"1 hour", "db_b", "spock", "5.0.10"},
				{"1 hour", "db_c", "spock", "5.0.2-devel"},
			},
			want: "5.0.10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := readSpockVersions(t, tt.configured, tt.rows)
			for reader, version := range got {
				if version != tt.want {
					t.Errorf("%s SpockVersion = %q, want %q", reader, version, tt.want)
				}
			}
		})
	}
}

// readSpockVersions seeds a standalone connection, a persisted member
// of an auto-detected cluster and a member of a manual cluster, each
// configured for database configured and given rows, and returns the
// Spock version each topology reader reports, keyed by reader.
func readSpockVersions(t *testing.T, configured string, rows []spockSnapshotRow) map[string]string {
	t.Helper()
	ds, _, fx, cleanup := newConfigQueriesTestDatastore(t)
	t.Cleanup(cleanup)
	ctx := context.Background()
	// One base time, so rows sharing an age share a collected_at exactly.
	base := time.Now().UTC().Truncate(time.Second)

	var autoClusterID, manualClusterID, standaloneID, memberID, manualID int
	if err := ds.pool.QueryRow(ctx, `INSERT INTO clusters (group_id, name, auto_cluster_key)
        VALUES ($1, 'auto-cls', 'spock:snapshot') RETURNING id`, fx.GroupID).Scan(&autoClusterID); err != nil {
		t.Fatalf("seed auto cluster: %v", err)
	}
	if err := ds.pool.QueryRow(ctx, `INSERT INTO clusters (group_id, name)
        VALUES ($1, 'manual-cls') RETURNING id`, fx.GroupID).Scan(&manualClusterID); err != nil {
		t.Fatalf("seed manual cluster: %v", err)
	}
	for _, c := range []struct {
		name    string
		cluster any
		id      *int
	}{
		{"spock-standalone", nil, &standaloneID},
		{"spock-member", autoClusterID, &memberID},
		{"spock-manual", manualClusterID, &manualID},
	} {
		if err := ds.pool.QueryRow(ctx, `INSERT INTO connections (name, database_name, cluster_id)
            VALUES ($1, $2, $3) RETURNING id`, c.name, configured, c.cluster).Scan(c.id); err != nil {
			t.Fatalf("seed connection %s: %v", c.name, err)
		}
		for _, r := range rows {
			if _, err := ds.pool.Exec(ctx, `INSERT INTO metrics.pg_extension
                (connection_id, database_name, extname, extversion, collected_at)
                VALUES ($1, $2, $3, $4, $5::timestamptz - $6::text::interval)`,
				*c.id, r.database, r.extname, r.version, base, r.age); err != nil {
				t.Fatalf("seed pg_extension: %v", err)
			}
		}
	}

	got := make(map[string]string)
	conns, err := ds.getAllConnectionsWithRoles(ctx)
	if err != nil {
		t.Fatalf("getAllConnectionsWithRoles: %v", err)
	}
	for _, c := range conns {
		if c.ID == standaloneID {
			got["getAllConnectionsWithRoles"] = c.SpockVersion.String
		}
	}
	members, err := ds.getPersistedClusterMembers(ctx, nil)
	if err != nil {
		t.Fatalf("getPersistedClusterMembers: %v", err)
	}
	for _, m := range members["spock:snapshot"] {
		if m.ID == memberID {
			got["getPersistedClusterMembers"] = m.SpockVersion
		}
	}
	manual, err := ds.getServersInClusterWithRolesInternal(ctx, manualClusterID)
	if err != nil {
		t.Fatalf("getServersInClusterWithRolesInternal: %v", err)
	}
	for _, m := range manual {
		if m.ID == manualID {
			got["getServersInClusterWithRolesInternal"] = m.SpockVersion
		}
	}
	if len(got) != 3 {
		t.Fatalf("readers returned %v, want an entry from each of the three", got)
	}
	return got
}
