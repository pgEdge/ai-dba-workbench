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
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/pkg/redact"
)

// topologyConnectionError is a driver error that quotes a connection
// URI and its password, as connections.connection_error can.
const topologyConnectionError = `failed to connect to postgresql://app:example-secret@192.0.2.1/db`

// seedTopologyConnectionErrorMembers adds an auto-detected cluster with
// two members to the config schema: "masked-member", which has a
// connection error, a description, an owner, no role and recent
// metrics, and "detected-member", which has a role and nothing else.
// It returns their IDs.
func seedTopologyConnectionErrorMembers(t *testing.T, ds *Datastore, groupID int) (maskedID, detectedID int) {
	t.Helper()
	ctx := context.Background()
	pool := ds.pool
	var clusterID int
	if err := pool.QueryRow(ctx, `INSERT INTO clusters (group_id, name, auto_cluster_key)
        VALUES ($1, 'auto-cls', 'spock:masking') RETURNING id`, groupID).Scan(&clusterID); err != nil {
		t.Fatalf("seed auto cluster: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO connections
        (name, description, owner_username, role, cluster_id, connection_error)
        VALUES ('masked-member', 'described', 'admin', NULL, $1, $2) RETURNING id`,
		clusterID, topologyConnectionError).Scan(&maskedID); err != nil {
		t.Fatalf("seed masked member: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO connections (name, role, cluster_id)
        VALUES ('detected-member', 'primary', $1) RETURNING id`, clusterID).Scan(&detectedID); err != nil {
		t.Fatalf("seed detected member: %v", err)
	}
	for _, sql := range []string{
		`INSERT INTO metrics.pg_connectivity (connection_id) VALUES ($1)`,
		`INSERT INTO metrics.pg_server_info (connection_id, server_version) VALUES ($1, '17.0')`,
		`INSERT INTO metrics.pg_sys_os_info (connection_id, name) VALUES ($1, 'Linux')`,
		`INSERT INTO metrics.pg_node_role (connection_id, primary_role, spock_node_name)
            VALUES ($1, 'standalone', 'n1')`,
	} {
		if _, err := pool.Exec(ctx, sql, maskedID); err != nil {
			t.Fatalf("seed metrics: %v\n%s", err, sql)
		}
	}
	return maskedID, detectedID
}

// TestTopologyQueriesMaskConnectionErrorIntegration checks that both
// topology readers of connections.connection_error return it masked
// (issue #683), and that the persisted-member reader still skips the
// connections auto-detection found and fills each optional field.
func TestTopologyQueriesMaskConnectionErrorIntegration(t *testing.T) {
	ds, _, fx, cleanup := newConfigQueriesTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	maskedID, detectedID := seedTopologyConnectionErrorMembers(t, ds, fx.GroupID)
	want := redact.Text(topologyConnectionError)
	if strings.Contains(want, "example-secret") {
		t.Fatalf("redact.Text left the password in %q", want)
	}

	conns, err := ds.getAllConnectionsWithRoles(ctx)
	if err != nil {
		t.Fatalf("getAllConnectionsWithRoles: %v", err)
	}
	found := false
	for _, c := range conns {
		if c.ID != maskedID {
			continue
		}
		found = true
		if !c.ConnectionError.Valid || c.ConnectionError.String != want {
			t.Errorf("getAllConnectionsWithRoles connection_error = %+v, want %q", c.ConnectionError, want)
		}
	}
	if !found {
		t.Fatalf("getAllConnectionsWithRoles did not return connection %d", maskedID)
	}

	members, err := ds.getPersistedClusterMembers(ctx, map[int]bool{detectedID: true})
	if err != nil {
		t.Fatalf("getPersistedClusterMembers: %v", err)
	}
	got := members["spock:masking"]
	if len(got) != 1 || got[0].ID != maskedID {
		t.Fatalf("getPersistedClusterMembers = %+v, want only connection %d", members, maskedID)
	}
	s := got[0]
	if s.ConnectionError != want {
		t.Errorf("getPersistedClusterMembers connection_error = %q, want %q", s.ConnectionError, want)
	}
	if s.Description != "described" || s.OwnerUsername != "admin" || s.Version != "17.0" ||
		s.OS != "Linux" || s.SpockNodeName != "n1" || s.Role == "" {
		t.Errorf("getPersistedClusterMembers did not fill the optional fields: %+v", s)
	}
}

// TestTopologyConnectionReadersErrorPathsIntegration checks that both
// readers report a row they cannot scan and a query that fails.
func TestTopologyConnectionReadersErrorPathsIntegration(t *testing.T) {
	ds, pool, fx, cleanup := newConfigQueriesTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	seedTopologyConnectionErrorMembers(t, ds, fx.GroupID)

	// A port that is not a number cannot be scanned into an int.
	mustExecClusterQueries(t, pool, `ALTER TABLE connections ALTER COLUMN port TYPE text`)
	mustExecClusterQueries(t, pool, `UPDATE connections SET port = 'not-a-port'`)
	if _, err := ds.getAllConnectionsWithRoles(ctx); err == nil || !strings.Contains(err.Error(), "failed to scan") {
		t.Errorf("getAllConnectionsWithRoles scan error = %v", err)
	}
	if _, err := ds.getPersistedClusterMembers(ctx, nil); err == nil || !strings.Contains(err.Error(), "failed to scan") {
		t.Errorf("getPersistedClusterMembers scan error = %v", err)
	}

	mustExecClusterQueries(t, pool, `DROP TABLE alerts CASCADE`)
	if _, err := ds.getAllConnectionsWithRoles(ctx); err == nil || !strings.Contains(err.Error(), "failed to query") {
		t.Errorf("getAllConnectionsWithRoles query error = %v", err)
	}
	if _, err := ds.getPersistedClusterMembers(ctx, nil); err == nil || !strings.Contains(err.Error(), "failed to query") {
		t.Errorf("getPersistedClusterMembers query error = %v", err)
	}
}
