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

// TestAssignConnectionToCluster covers the sentinel errors
// AssignConnectionToCluster returns, which PUT
// /api/v1/connections/{id}/cluster maps to 404 so that a missing cluster
// cannot be told apart from a hidden one (issue #471).
func TestAssignConnectionToCluster(t *testing.T) {
	ds, pool, cleanup := newClusterDismissTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		"ALTER TABLE connections ADD COLUMN role VARCHAR(64)"); err != nil {
		t.Fatalf("Add role column: %v", err)
	}
	groupID := insertClusterDismissTestGroup(t, pool)
	var clusterID, connID int
	if err := pool.QueryRow(ctx,
		"INSERT INTO clusters (group_id, name) VALUES ($1, 'Target') RETURNING id",
		groupID).Scan(&clusterID); err != nil {
		t.Fatalf("Insert cluster: %v", err)
	}
	if err := pool.QueryRow(ctx,
		"INSERT INTO connections (name) VALUES ('conn') RETURNING id").Scan(&connID); err != nil {
		t.Fatalf("Insert connection: %v", err)
	}

	role := "primary"
	t.Run("assigns", func(t *testing.T) {
		if err := ds.AssignConnectionToCluster(ctx, connID, &clusterID, &role, "manual"); err != nil {
			t.Fatalf("AssignConnectionToCluster: %v", err)
		}
		var gotCluster int
		var gotRole, gotSource string
		if err := pool.QueryRow(ctx,
			"SELECT cluster_id, role, membership_source FROM connections WHERE id = $1",
			connID).Scan(&gotCluster, &gotRole, &gotSource); err != nil {
			t.Fatalf("Read back: %v", err)
		}
		if gotCluster != clusterID || gotRole != role || gotSource != "manual" {
			t.Errorf("Stored cluster=%d role=%q source=%q", gotCluster, gotRole, gotSource)
		}
	})

	missingCluster := clusterID + 1000
	t.Run("missing cluster", func(t *testing.T) {
		err := ds.AssignConnectionToCluster(ctx, connID, &missingCluster, nil, "manual")
		if !errors.Is(err, ErrClusterNotFound) {
			t.Fatalf("Expected ErrClusterNotFound, got %v", err)
		}
	})

	t.Run("missing connection", func(t *testing.T) {
		err := ds.AssignConnectionToCluster(ctx, connID+1000, &clusterID, nil, "manual")
		if !errors.Is(err, ErrConnectionNotFound) {
			t.Fatalf("Expected ErrConnectionNotFound, got %v", err)
		}
	})

	t.Run("other failure", func(t *testing.T) {
		if _, err := pool.Exec(ctx, "ALTER TABLE connections DROP COLUMN role"); err != nil {
			t.Fatalf("Drop role column: %v", err)
		}
		err := ds.AssignConnectionToCluster(ctx, connID, &clusterID, nil, "manual")
		if err == nil || errors.Is(err, ErrClusterNotFound) ||
			errors.Is(err, ErrConnectionNotFound) {
			t.Fatalf("Expected a wrapped datastore error, got %v", err)
		}
	})
}
