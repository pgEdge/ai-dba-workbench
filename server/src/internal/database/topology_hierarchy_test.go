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
	"database/sql"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// findServerByID returns the first descendant in the tree (DFS) whose ID
// matches target, or nil if not found. The recursion makes it easy for
// hierarchy assertions to confirm grandchildren survived materialization
// without depending on insertion order at any level.
func findServerByID(servers []TopologyServerInfo, target int) *TopologyServerInfo {
	for i := range servers {
		if servers[i].ID == target {
			return &servers[i]
		}
		if found := findServerByID(servers[i].Children, target); found != nil {
			return found
		}
	}
	return nil
}

// permuteByID returns the input slice reordered to put the element with
// the given ID first. The underlying bug in nestPersistedMembers depended
// on the order in which children appeared in the input slice, so test
// fixtures use this helper to drive every relevant ordering through the
// nesting code.
func permuteByID(members []TopologyServerInfo, firstID int) []TopologyServerInfo {
	out := make([]TopologyServerInfo, 0, len(members))
	for i := range members {
		if members[i].ID == firstID {
			out = append(out, members[i])
		}
	}
	for i := range members {
		if members[i].ID != firstID {
			out = append(out, members[i])
		}
	}
	return out
}

// TestNestPersistedMembers_PreservesGrandchildInChain is the regression
// test for issue #153 against nestPersistedMembers. A primary -> standby
// -> cascading-standby chain must round-trip with the cascading standby
// nested under the standby regardless of input slice ordering. Prior to
// the fix the function appended struct copies before the intermediate
// node had its own children attached, so the grandchild silently
// disappeared whenever the standby was visited before the cascading
// child.
func TestNestPersistedMembers_PreservesGrandchildInChain(t *testing.T) {
	primary := TopologyServerInfo{ID: 1, Name: "primary", PrimaryRole: "binary_primary"}
	standby := TopologyServerInfo{ID: 2, Name: "standby", PrimaryRole: "binary_standby"}
	cascading := TopologyServerInfo{ID: 3, Name: "cascading", PrimaryRole: "binary_cascading"}

	parentMap := map[int]int{
		2: 1, // standby's parent is primary
		3: 2, // cascading's parent is standby
	}

	// Drive every input ordering: primary-first, standby-first, and
	// cascading-first. The pre-fix algorithm survived primary-first but
	// dropped the grandchild for the other two orderings.
	for _, firstID := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("first_id_%d", firstID), func(t *testing.T) {
			members := permuteByID(
				[]TopologyServerInfo{primary, standby, cascading},
				firstID,
			)
			result := nestPersistedMembers(members, parentMap)

			if len(result) != 1 {
				t.Fatalf("expected 1 top-level member, got %d", len(result))
			}
			top := result[0]
			if top.ID != 1 {
				t.Fatalf("expected primary (1) at top, got %d", top.ID)
			}
			if !top.IsExpandable {
				t.Errorf("expected primary to be expandable")
			}
			if len(top.Children) != 1 {
				t.Fatalf("expected primary to have 1 child, got %d", len(top.Children))
			}
			child := top.Children[0]
			if child.ID != 2 {
				t.Fatalf("expected standby (2) under primary, got %d", child.ID)
			}
			if !child.IsExpandable {
				t.Errorf("expected standby to be expandable")
			}
			if len(child.Children) != 1 {
				t.Fatalf("standby lost its grandchild (issue #153): got %d children, want 1",
					len(child.Children))
			}
			grand := child.Children[0]
			if grand.ID != 3 {
				t.Fatalf("expected cascading (3) under standby, got %d", grand.ID)
			}
			if grand.IsExpandable {
				t.Errorf("expected cascading leaf to be non-expandable")
			}
		})
	}
}

// TestNestPersistedMembers_NoParentMapReturnsInput verifies the
// short-circuit: when no parent map is supplied the input list is
// returned unchanged.
func TestNestPersistedMembers_NoParentMapReturnsInput(t *testing.T) {
	members := []TopologyServerInfo{
		{ID: 1, Name: "a"},
		{ID: 2, Name: "b"},
	}
	out := nestPersistedMembers(members, map[int]int{})
	if len(out) != 2 {
		t.Fatalf("expected 2 members, got %d", len(out))
	}
}

// TestNestPersistedMembers_ParentNotInSet keeps members with an unknown
// parent at the top level and does not promote them to children of the
// missing node.
func TestNestPersistedMembers_ParentNotInSet(t *testing.T) {
	members := []TopologyServerInfo{
		{ID: 1, Name: "a"},
		{ID: 2, Name: "b"},
	}
	// Parent 99 does not exist in the member set; member 2 must remain
	// at the top level.
	parentMap := map[int]int{2: 99}
	out := nestPersistedMembers(members, parentMap)
	if len(out) != 2 {
		t.Fatalf("expected 2 top-level members, got %d", len(out))
	}
	for _, m := range out {
		if m.IsExpandable {
			t.Errorf("expected no expandable members, %d was expandable", m.ID)
		}
	}
}

// TestNestPersistedMembers_DeepCopyIsolatesGrandchildren ensures the
// returned tree is a deep copy: mutating a returned grandchild must not
// bleed back into another root reference of the same ID. The pre-fix
// shallow-copy bug meant the same struct could be observed from two
// places.
func TestNestPersistedMembers_DeepCopyIsolatesGrandchildren(t *testing.T) {
	members := []TopologyServerInfo{
		{ID: 1, Name: "primary"},
		{ID: 2, Name: "standby"},
		{ID: 3, Name: "cascading"},
	}
	parentMap := map[int]int{2: 1, 3: 2}
	result := nestPersistedMembers(members, parentMap)
	if len(result) != 1 {
		t.Fatalf("expected 1 root, got %d", len(result))
	}
	// Mutate the grandchild in the result tree; the original input
	// slice (members[2]) must not change.
	result[0].Children[0].Children[0].Name = "mutated"
	if members[2].Name == "mutated" {
		t.Errorf("nestPersistedMembers leaked mutations into input slice (deep-copy expected)")
	}
}

// buildManualClusterHierarchyTestSchema builds the minimum subset of the
// cluster hierarchy tables exercised by buildManualClusterHierarchy. It
// only needs cluster_node_relationships rows; the function does not
// inspect connections or clusters tables.
const buildManualClusterHierarchyTestSchema = `
DROP TABLE IF EXISTS cluster_node_relationships CASCADE;

CREATE TABLE cluster_node_relationships (
    id SERIAL PRIMARY KEY,
    cluster_id INTEGER NOT NULL,
    source_connection_id INTEGER NOT NULL,
    target_connection_id INTEGER NOT NULL,
    relationship_type VARCHAR(50) NOT NULL,
    is_auto_detected BOOLEAN NOT NULL DEFAULT FALSE
);
`

const buildManualClusterHierarchyTestTeardown = `
DROP TABLE IF EXISTS cluster_node_relationships CASCADE;
`

// newBuildManualClusterHierarchyDatastore wires up a *Datastore with the
// minimum schema buildManualClusterHierarchy needs. It is structurally
// identical to newClusterDismissTestDatastore so the test follows the
// existing integration-test idiom.
func newBuildManualClusterHierarchyDatastore(t *testing.T) (*Datastore, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping buildManualClusterHierarchy integration test")
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

	if _, err := pool.Exec(ctx, buildManualClusterHierarchyTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create buildManualClusterHierarchy test schema: %v", err)
	}

	ds := NewTestDatastore(pool)

	cleanup := func() {
		if _, err := pool.Exec(context.Background(), buildManualClusterHierarchyTestTeardown); err != nil {
			t.Logf("buildManualClusterHierarchy teardown failed: %v", err)
		}
		pool.Close()
	}
	return ds, pool, cleanup
}

// insertHierarchyRelationship inserts a single child -> parent edge into
// cluster_node_relationships using the same relationship_type values
// buildManualClusterHierarchy filters on.
func insertHierarchyRelationship(t *testing.T, pool *pgxpool.Pool, clusterID, sourceID, targetID int, relType string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
        INSERT INTO cluster_node_relationships
            (cluster_id, source_connection_id, target_connection_id, relationship_type)
        VALUES ($1, $2, $3, $4)
    `, clusterID, sourceID, targetID, relType)
	if err != nil {
		t.Fatalf("insert relationship (%d -> %d) failed: %v", sourceID, targetID, err)
	}
}

// TestBuildManualClusterHierarchy_PreservesDeepChain is the integration
// regression for issue #153 against buildManualClusterHierarchy. The
// previous implementation iterated parentMap (a Go map) and appended
// shallow struct copies, so map iteration order determined whether
// grandchildren survived. The test loops the call enough times to make
// the nondeterministic bug effectively impossible to miss.
func TestBuildManualClusterHierarchy_PreservesDeepChain(t *testing.T) {
	ds, pool, cleanup := newBuildManualClusterHierarchyDatastore(t)
	defer cleanup()

	const clusterID = 1
	// Five-deep chain: 1 <- 2 <- 3 <- 4 <- 5 (child streams_from
	// parent). The depth ensures any single map ordering flip would
	// orphan something.
	insertHierarchyRelationship(t, pool, clusterID, 2, 1, "streams_from")
	insertHierarchyRelationship(t, pool, clusterID, 3, 2, "streams_from")
	insertHierarchyRelationship(t, pool, clusterID, 4, 3, "subscribes_to")
	insertHierarchyRelationship(t, pool, clusterID, 5, 4, "streams_from")

	servers := []TopologyServerInfo{
		{ID: 1, Name: "n1"},
		{ID: 2, Name: "n2"},
		{ID: 3, Name: "n3"},
		{ID: 4, Name: "n4"},
		{ID: 5, Name: "n5"},
	}

	const iterations = 100
	for i := 0; i < iterations; i++ {
		result := ds.buildManualClusterHierarchy(context.Background(), clusterID, append([]TopologyServerInfo(nil), servers...))

		if len(result) != 1 {
			t.Fatalf("iteration %d: expected 1 top-level server, got %d", i, len(result))
		}
		top := result[0]
		if top.ID != 1 {
			t.Fatalf("iteration %d: expected root ID 1, got %d", i, top.ID)
		}
		// Every descendant must still be reachable from the root.
		for _, id := range []int{2, 3, 4, 5} {
			if findServerByID(result, id) == nil {
				t.Fatalf("iteration %d: descendant %d missing from materialized tree (issue #153)",
					i, id)
			}
		}
		// Verify the actual chain depth, not just presence.
		n2 := top.Children
		if len(n2) != 1 || n2[0].ID != 2 {
			t.Fatalf("iteration %d: expected n1 -> [n2], got %+v", i, n2)
		}
		n3 := n2[0].Children
		if len(n3) != 1 || n3[0].ID != 3 {
			t.Fatalf("iteration %d: expected n2 -> [n3], got %+v", i, n3)
		}
		n4 := n3[0].Children
		if len(n4) != 1 || n4[0].ID != 4 {
			t.Fatalf("iteration %d: expected n3 -> [n4], got %+v", i, n4)
		}
		n5 := n4[0].Children
		if len(n5) != 1 || n5[0].ID != 5 {
			t.Fatalf("iteration %d: expected n4 -> [n5], got %+v", i, n5)
		}
		// Intermediate nodes must remain expandable; the leaf must not.
		if !top.IsExpandable || !n2[0].IsExpandable || !n3[0].IsExpandable || !n4[0].IsExpandable {
			t.Fatalf("iteration %d: expected every internal node expandable", i)
		}
		if n5[0].IsExpandable {
			t.Fatalf("iteration %d: leaf must not be expandable", i)
		}
	}
}

// TestBuildManualClusterHierarchy_NoRelationshipsReturnsServersAsIs
// covers the early-out path when no rows match the cluster.
func TestBuildManualClusterHierarchy_NoRelationshipsReturnsServersAsIs(t *testing.T) {
	ds, _, cleanup := newBuildManualClusterHierarchyDatastore(t)
	defer cleanup()

	servers := []TopologyServerInfo{{ID: 1, Name: "lone"}}
	result := ds.buildManualClusterHierarchy(context.Background(), 42, servers)
	if len(result) != 1 || result[0].ID != 1 {
		t.Fatalf("expected single server passthrough, got %+v", result)
	}
}

// TestBuildManualClusterHierarchy_EmptyServersShortCircuits exercises
// the len(servers)==0 branch, which must not even open a connection.
func TestBuildManualClusterHierarchy_EmptyServersShortCircuits(t *testing.T) {
	d := &Datastore{}
	out := d.buildManualClusterHierarchy(context.Background(), 1, nil)
	if len(out) != 0 {
		t.Fatalf("expected empty result, got %+v", out)
	}
}

// TestBuildManualClusterHierarchy_QueryErrorReturnsServers covers the
// query-error path: when pool.Query fails the function logs and returns
// the unmodified servers slice. The pool is closed before the call so
// pgx returns an error immediately.
func TestBuildManualClusterHierarchy_QueryErrorReturnsServers(t *testing.T) {
	ds, pool, cleanup := newBuildManualClusterHierarchyDatastore(t)
	cleanup() // closes the pool; subsequent Query calls will error
	_ = pool

	servers := []TopologyServerInfo{{ID: 1, Name: "n1"}, {ID: 2, Name: "n2"}}
	result := ds.buildManualClusterHierarchy(context.Background(), 1, servers)
	if len(result) != 2 {
		t.Fatalf("expected 2 servers (passthrough on query error), got %d", len(result))
	}
	if result[0].ID != 1 || result[1].ID != 2 {
		t.Errorf("expected original ordering preserved, got %+v", result)
	}
}

// TestBuildServerWithChildren_ManualVisibility is the regression test
// for the IsExpandable bug fixed alongside issue #153. When allowManual
// is false and every linked child is filtered out, IsExpandable must
// reflect the post-filter Children slice rather than the unfiltered
// childrenMap. Pre-fix code derived IsExpandable from the map and so
// reported expandable=true on a node whose visible children list was
// empty.
func TestBuildServerWithChildren_ManualVisibility(t *testing.T) {
	ds := &Datastore{}

	primary := &connectionWithRole{
		ID:               10,
		Name:             "primary",
		Host:             "10.0.0.1",
		Port:             5432,
		DatabaseName:     "app",
		Username:         "postgres",
		PrimaryRole:      "binary_primary",
		MembershipSource: "auto",
		Status:           "online",
	}
	manualStandby := &connectionWithRole{
		ID:               11,
		Name:             "manual-standby",
		Host:             "10.0.0.2",
		Port:             5432,
		DatabaseName:     "app",
		Username:         "postgres",
		PrimaryRole:      "binary_standby",
		MembershipSource: "manual",
		Status:           "online",
	}
	autoStandby := &connectionWithRole{
		ID:               12,
		Name:             "auto-standby",
		Host:             "10.0.0.3",
		Port:             5432,
		DatabaseName:     "app",
		Username:         "postgres",
		PrimaryRole:      "binary_standby",
		MembershipSource: "auto",
		Status:           "online",
	}

	cases := []struct {
		name                  string
		children              []*connectionWithRole
		allowManual           bool
		expectChildIDs        []int
		expectIsExpandable    bool
		expectChildrenAreCopy bool
	}{
		{
			name:               "manual-only_disallowed_filters_and_clears_isExpandable",
			children:           []*connectionWithRole{manualStandby},
			allowManual:        false,
			expectChildIDs:     []int{},
			expectIsExpandable: false,
		},
		{
			name:               "manual-only_allowed_keeps_child_and_marks_expandable",
			children:           []*connectionWithRole{manualStandby},
			allowManual:        true,
			expectChildIDs:     []int{11},
			expectIsExpandable: true,
		},
		{
			name:               "auto_child_stays_expandable_regardless_of_allowManual_false",
			children:           []*connectionWithRole{autoStandby},
			allowManual:        false,
			expectChildIDs:     []int{12},
			expectIsExpandable: true,
		},
		{
			name:               "auto_child_stays_expandable_regardless_of_allowManual_true",
			children:           []*connectionWithRole{autoStandby},
			allowManual:        true,
			expectChildIDs:     []int{12},
			expectIsExpandable: true,
		},
		{
			name:               "mixed_children_disallowed_drops_manual_keeps_auto",
			children:           []*connectionWithRole{manualStandby, autoStandby},
			allowManual:        false,
			expectChildIDs:     []int{12},
			expectIsExpandable: true,
		},
		{
			name:               "no_children_means_not_expandable",
			children:           nil,
			allowManual:        false,
			expectChildIDs:     []int{},
			expectIsExpandable: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			connByID := map[int]*connectionWithRole{primary.ID: primary}
			childIDs := make([]int, 0, len(tc.children))
			for _, c := range tc.children {
				connByID[c.ID] = c
				childIDs = append(childIDs, c.ID)
			}
			childrenMap := map[int][]int{primary.ID: childIDs}
			assigned := make(map[int]bool)

			result := ds.buildServerWithChildren(primary, childrenMap, connByID, assigned, tc.allowManual)

			if len(result.Children) != len(tc.expectChildIDs) {
				t.Fatalf("expected %d children, got %d (%+v)",
					len(tc.expectChildIDs), len(result.Children), result.Children)
			}
			for i, id := range tc.expectChildIDs {
				if result.Children[i].ID != id {
					t.Errorf("child[%d]: expected ID %d, got %d",
						i, id, result.Children[i].ID)
				}
			}
			if result.IsExpandable != tc.expectIsExpandable {
				t.Errorf("IsExpandable: expected %v, got %v (issue #153)",
					tc.expectIsExpandable, result.IsExpandable)
			}
		})
	}
}

// TestBuildServerWithChildren_DoesNotRevisitAssignedConnection guards the
// recursion-cycle protection: when the child has already been assigned
// elsewhere (e.g. picked up by another cluster), it is skipped and does
// not contribute to IsExpandable.
func TestBuildServerWithChildren_DoesNotRevisitAssignedConnection(t *testing.T) {
	ds := &Datastore{}
	primary := &connectionWithRole{
		ID: 20, Name: "primary", PrimaryRole: "binary_primary",
		MembershipSource: "auto", Status: "online",
	}
	child := &connectionWithRole{
		ID: 21, Name: "child", PrimaryRole: "binary_standby",
		MembershipSource: "auto", Status: "online",
	}
	connByID := map[int]*connectionWithRole{primary.ID: primary, child.ID: child}
	childrenMap := map[int][]int{primary.ID: {child.ID}}
	assigned := map[int]bool{child.ID: true}

	result := ds.buildServerWithChildren(primary, childrenMap, connByID, assigned, true)
	if len(result.Children) != 0 {
		t.Fatalf("expected already-assigned child to be skipped; got %+v", result.Children)
	}
	if result.IsExpandable {
		t.Errorf("expected IsExpandable=false when no children survive assignment filter")
	}
}

// TestBuildServerWithChildren_PopulatesNullStringFields covers the
// Valid-branch assignments for every sql.NullString conn field plus the
// "child not present in connByID" guard. Together they exercise the
// remaining branches in buildServerWithChildren so the function reaches
// the project's 90% coverage floor.
func TestBuildServerWithChildren_PopulatesNullStringFields(t *testing.T) {
	ds := &Datastore{}
	conn := &connectionWithRole{
		ID:               30,
		Name:             "primary",
		Host:             "10.1.1.1",
		Port:             5432,
		DatabaseName:     "app",
		Username:         "postgres",
		PrimaryRole:      "binary_primary",
		MembershipSource: "auto",
		Status:           "online",
		OwnerUsername:    sql.NullString{String: "alice", Valid: true},
		Version:          sql.NullString{String: "17.0", Valid: true},
		OS:               sql.NullString{String: "linux", Valid: true},
		SpockNodeName:    sql.NullString{String: "node-a", Valid: true},
		SpockVersion:     sql.NullString{String: "5.0", Valid: true},
		Description:      sql.NullString{String: "primary db", Valid: true},
		ConnectionError:  sql.NullString{String: "boom", Valid: true},
	}
	// childrenMap references an ID that is not in connByID so the
	// `!exists` continue branch runs.
	childrenMap := map[int][]int{conn.ID: {999}}
	connByID := map[int]*connectionWithRole{conn.ID: conn}
	assigned := make(map[int]bool)

	result := ds.buildServerWithChildren(conn, childrenMap, connByID, assigned, true)

	if result.OwnerUsername != "alice" {
		t.Errorf("OwnerUsername: got %q", result.OwnerUsername)
	}
	if result.Version != "17.0" {
		t.Errorf("Version: got %q", result.Version)
	}
	if result.OS != "linux" {
		t.Errorf("OS: got %q", result.OS)
	}
	if result.SpockNodeName != "node-a" {
		t.Errorf("SpockNodeName: got %q", result.SpockNodeName)
	}
	if result.SpockVersion != "5.0" {
		t.Errorf("SpockVersion: got %q", result.SpockVersion)
	}
	if result.Description != "primary db" {
		t.Errorf("Description: got %q", result.Description)
	}
	if result.ConnectionError != "boom" {
		t.Errorf("ConnectionError: got %q", result.ConnectionError)
	}
	if len(result.Children) != 0 {
		t.Errorf("expected missing-child reference to be skipped; got %+v", result.Children)
	}
	if result.IsExpandable {
		t.Errorf("expected IsExpandable=false when child reference is unresolved")
	}
}
