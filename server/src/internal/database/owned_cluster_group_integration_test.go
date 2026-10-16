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
	"reflect"
	"testing"
)

// ownedClusterGroupTestSchema creates the cluster_groups, clusters and
// connections columns that GetOwnedClusterGroupConnectionIDs joins on.
const ownedClusterGroupTestSchema = `
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
DROP TABLE IF EXISTS cluster_groups CASCADE;
CREATE TABLE cluster_groups (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    owner_username VARCHAR(255)
);
CREATE TABLE clusters (
    id SERIAL PRIMARY KEY,
    group_id INTEGER REFERENCES cluster_groups(id)
);
CREATE TABLE connections (
    id INTEGER PRIMARY KEY,
    cluster_id INTEGER REFERENCES clusters(id)
);
`

const ownedClusterGroupTestTeardown = `
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
DROP TABLE IF EXISTS cluster_groups CASCADE;
`

// TestGetOwnedClusterGroupConnectionIDs_Integration checks that the
// lookup returns every member of every cluster group the user owns,
// once each and in order, and nothing from another user's groups.
func TestGetOwnedClusterGroupConnectionIDs_Integration(t *testing.T) {
	ds, pool, cleanup := newClusterGroupShareTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := pool.Exec(ctx, ownedClusterGroupTestSchema); err != nil {
		t.Fatalf("Failed to create schema: %v", err)
	}
	defer func() {
		if _, err := pool.Exec(context.Background(), ownedClusterGroupTestTeardown); err != nil {
			t.Logf("teardown failed: %v", err)
		}
	}()

	seed := `
        INSERT INTO cluster_groups (id, name, owner_username) VALUES
            (1, 'alice-a', 'alice'), (2, 'alice-b', 'alice'),
            (3, 'bob', 'bob'), (4, 'nobody', NULL);
        INSERT INTO clusters (id, group_id) VALUES
            (10, 1), (11, 1), (12, 2), (13, 3), (14, 4), (15, NULL);
        INSERT INTO connections (id, cluster_id) VALUES
            (9, 11), (5, 10), (6, 12), (7, 13), (8, 14), (4, 15), (3, NULL);
    `
	if _, err := pool.Exec(ctx, seed); err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	tests := []struct {
		user string
		want []int
	}{
		{"alice", []int{5, 6, 9}},
		{"bob", []int{7}},
		{"carol", nil},
	}
	for _, tt := range tests {
		got, err := ds.GetOwnedClusterGroupConnectionIDs(ctx, tt.user)
		if err != nil {
			t.Fatalf("GetOwnedClusterGroupConnectionIDs(%q) failed: %v", tt.user, err)
		}
		if len(got) == 0 && len(tt.want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("GetOwnedClusterGroupConnectionIDs(%q) = %v, want %v", tt.user, got, tt.want)
		}
	}

	// Through the visibility lister the datastore's lookup is used.
	lister := newVisibilityListerWithSource(ds)
	got, err := lister.GetOwnedClusterGroupConnectionIDs(ctx, "bob")
	if err != nil || !reflect.DeepEqual(got, []int{7}) {
		t.Errorf("lister lookup = %v, %v; want [7], nil", got, err)
	}

	// A failing query reports an error.
	if _, err := pool.Exec(ctx, ownedClusterGroupTestTeardown); err != nil {
		t.Fatalf("teardown failed: %v", err)
	}
	if _, err := ds.GetOwnedClusterGroupConnectionIDs(ctx, "alice"); err == nil {
		t.Error("Expected an error once the tables are gone")
	}
}
