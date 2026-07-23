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

// clusterGroupShareTestSchema creates the minimum cluster_groups table
// exercised by CreateClusterGroupWithOwner and UpdateClusterGroup. It mirrors
// the production shape (collector/src/database/schema.go), limited to the
// columns those two functions read, write, and RETURN. The owner_username,
// owner_token, and is_shared columns are required because both functions
// reference them in their RETURNING clauses (issue #304).
const clusterGroupShareTestSchema = `
DROP TABLE IF EXISTS cluster_groups CASCADE;
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
    CONSTRAINT cluster_groups_share_name_unique UNIQUE (name)
);
`

const clusterGroupShareTestTeardown = `DROP TABLE IF EXISTS cluster_groups CASCADE;`

// newClusterGroupShareTestDatastore wires up a *Datastore against the
// TEST_AI_WORKBENCH_SERVER Postgres instance with only the cluster_groups
// table the share/owner path needs. The caller receives a cleanup that drops
// the schema and closes the pool.
func newClusterGroupShareTestDatastore(t *testing.T) (*Datastore, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping cluster group share integration test")
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

	if _, err := pool.Exec(ctx, clusterGroupShareTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create cluster group share test schema: %v", err)
	}

	ds := NewTestDatastore(pool)

	cleanup := func() {
		if _, err := pool.Exec(context.Background(), clusterGroupShareTestTeardown); err != nil {
			t.Logf("cluster group share teardown failed: %v", err)
		}
		pool.Close()
	}

	return ds, pool, cleanup
}

// TestCreateClusterGroupWithOwner_Integration confirms the owner and shared
// flag are persisted for both the shared and private cases.
func TestCreateClusterGroupWithOwner_Integration(t *testing.T) {
	ds, _, cleanup := newClusterGroupShareTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	owner := "alice"

	shared, err := ds.CreateClusterGroupWithOwner(ctx, "Shared", nil, &owner, true)
	if err != nil {
		t.Fatalf("CreateClusterGroupWithOwner (shared) failed: %v", err)
	}
	if !shared.IsShared {
		t.Errorf("shared.IsShared = false, want true")
	}
	if !shared.OwnerUsername.Valid || shared.OwnerUsername.String != owner {
		t.Errorf("shared.OwnerUsername = %v, want %q", shared.OwnerUsername, owner)
	}

	desc := "a private group"
	private, err := ds.CreateClusterGroupWithOwner(ctx, "Private", &desc, &owner, false)
	if err != nil {
		t.Fatalf("CreateClusterGroupWithOwner (private) failed: %v", err)
	}
	if private.IsShared {
		t.Errorf("private.IsShared = true, want false")
	}
	if private.Description == nil || *private.Description != desc {
		t.Errorf("private.Description = %v, want %q", private.Description, desc)
	}
}

// TestUpdateClusterGroup_Integration_PartialSemantics drives the partial
// update behavior of UpdateClusterGroup directly: is_shared and description
// change when a non-nil pointer is supplied and are preserved when nil.
func TestUpdateClusterGroup_Integration_PartialSemantics(t *testing.T) {
	ds, _, cleanup := newClusterGroupShareTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	owner := "bob"
	origDesc := "keep me"
	created, err := ds.CreateClusterGroupWithOwner(ctx, "Partial", &origDesc, &owner, true)
	if err != nil {
		t.Fatalf("seed create failed: %v", err)
	}

	// Rename only: nil description and nil is_shared must preserve both.
	got, err := ds.UpdateClusterGroup(ctx, created.ID, "Partial Renamed", nil, nil)
	if err != nil {
		t.Fatalf("UpdateClusterGroup (name only) failed: %v", err)
	}
	if got.Name != "Partial Renamed" {
		t.Errorf("Name = %q, want %q", got.Name, "Partial Renamed")
	}
	if got.Description == nil || *got.Description != origDesc {
		t.Errorf("Description = %v, want preserved %q", got.Description, origDesc)
	}
	if !got.IsShared {
		t.Errorf("IsShared = false, want preserved true")
	}

	// Flip is_shared to false and set a new description.
	newDesc := "changed"
	shared := false
	got, err = ds.UpdateClusterGroup(ctx, created.ID, "Partial Renamed", &newDesc, &shared)
	if err != nil {
		t.Fatalf("UpdateClusterGroup (full) failed: %v", err)
	}
	if got.IsShared {
		t.Errorf("IsShared = true, want false")
	}
	if got.Description == nil || *got.Description != newDesc {
		t.Errorf("Description = %v, want %q", got.Description, newDesc)
	}

	// Flip is_shared back to true, leaving description untouched (nil).
	shared = true
	got, err = ds.UpdateClusterGroup(ctx, created.ID, "Partial Renamed", nil, &shared)
	if err != nil {
		t.Fatalf("UpdateClusterGroup (share back) failed: %v", err)
	}
	if !got.IsShared {
		t.Errorf("IsShared = false, want true")
	}
	if got.Description == nil || *got.Description != newDesc {
		t.Errorf("Description = %v, want preserved %q", got.Description, newDesc)
	}
}

// TestUpdateClusterGroup_Integration_Error confirms the error path: renaming
// a group to a name already taken by another group violates the unique
// constraint and returns a wrapped error.
func TestUpdateClusterGroup_Integration_Error(t *testing.T) {
	ds, _, cleanup := newClusterGroupShareTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := ds.CreateClusterGroupWithOwner(ctx, "Taken", nil, nil, false); err != nil {
		t.Fatalf("seed create failed: %v", err)
	}
	target, err := ds.CreateClusterGroupWithOwner(ctx, "Mover", nil, nil, false)
	if err != nil {
		t.Fatalf("seed create failed: %v", err)
	}

	if _, err := ds.UpdateClusterGroup(ctx, target.ID, "Taken", nil, nil); err == nil {
		t.Fatalf("expected error renaming to a duplicate name, got nil")
	}
}

// TestCreateClusterGroupWithOwner_Integration_Error confirms the create error
// path: inserting a second group with a duplicate name violates the unique
// constraint and returns a wrapped error.
func TestCreateClusterGroupWithOwner_Integration_Error(t *testing.T) {
	ds, _, cleanup := newClusterGroupShareTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := ds.CreateClusterGroupWithOwner(ctx, "OnlyOne", nil, nil, false); err != nil {
		t.Fatalf("seed create failed: %v", err)
	}
	if _, err := ds.CreateClusterGroupWithOwner(ctx, "OnlyOne", nil, nil, false); err == nil {
		t.Fatalf("expected error on duplicate name, got nil")
	}
}
