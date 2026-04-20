/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package api

import (
	"context"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// =============================================================================
// Regression Test Coverage for GitHub Issue #35 - Cluster Endpoints
//
// Issue #35 was an access-control leak in which non-owner users could see
// unshared connections (and the clusters/groups that contained them)
// through cluster-list, cluster-detail, cluster-servers, cluster-group-
// list, cluster-group-detail, and clusters-in-group REST endpoints.
//
// The production fix added the following to ClusterHandler:
//
//   - resolveVisibleConnections builds the caller's visible connection-ID
//     set via rbacChecker.VisibleConnectionIDs. Superuser/wildcard
//     callers get allConnections=true so the handler skips filtering.
//   - clusterMembersVisible is the pure decision function used by every
//     gated endpoint: a cluster is visible iff at least one of its
//     member connections is in the caller's visible set. Empty clusters
//     are invisible (no ownership claim to satisfy).
//   - filterGroupsByVisibility, groupHasVisibleConnection, and
//     clusterHasVisibleConnection wrap clusterMembersVisible with the
//     datastore lookups.
//
// The tests in this file exercise clusterMembersVisible directly
// (pure function, no datastore) and the VisibleConnectionIDs pairing
// the handler uses to build the visible set. Full handler-level end-
// to-end tests against a real PostgreSQL are covered by the integration
// suite gated on TEST_AI_WORKBENCH_SERVER (see cluster_handlers_test.go
// for the pattern); the decision surface is what regresses under
// refactoring, and clusterMembersVisible is where any regression would
// land.
//
// This mirrors the approach TestFilterTopologyByVisibility_Issue35_*
// already takes for the cluster-topology endpoint: test the pure
// filter that drives the gate, and rely on the handler glue
// (resolveVisibleConnections + filterGroupsByVisibility) being thin.
// =============================================================================

// TestClusterMembersVisible_Issue35_NonOwnerZeroGrant verifies the
// regression case the fix targets: a non-owner zero-grant user's visible
// set is empty, so a cluster whose only members are unshared non-owned
// connections is not visible. This is the exact code path that makes
// GET /api/v1/clusters/list hide an unshared cluster owned by another
// user.
func TestClusterMembersVisible_Issue35_NonOwnerZeroGrant(t *testing.T) {
	// Non-owner zero-grant callers produce an empty visible set.
	visible := map[int]bool{}

	// A cluster holds two unshared non-owned connections (IDs 42, 43).
	members := []int{42, 43}

	if clusterMembersVisible(members, visible) {
		t.Fatal("Expected cluster with only unshared non-owned members " +
			"to be invisible to non-owner zero-grant caller")
	}
}

// TestClusterMembersVisible_Issue35_OwnerSeesOwnUnshared verifies the
// owner of an unshared connection can still see the cluster that
// contains it. This guards against an over-eager filter that would
// block the owner from their own cluster.
func TestClusterMembersVisible_Issue35_OwnerSeesOwnUnshared(t *testing.T) {
	// The owner's visible set contains the connection ID they own.
	visible := map[int]bool{42: true}

	members := []int{42, 43}

	if !clusterMembersVisible(members, visible) {
		t.Fatal("Expected cluster with an owner-visible member to be " +
			"visible; owner must see their own unshared cluster")
	}
}

// TestClusterMembersVisible_Issue35_OnlySharedVisible verifies a cluster
// that mixes a shared member (visible to the caller) and unshared non-
// owned members is still visible to the caller. The caller sees the
// cluster because their shared-member stake is enough; the server
// filter on listServersInCluster is what drops the unshared servers
// from the membership view.
func TestClusterMembersVisible_Issue35_OnlySharedVisible(t *testing.T) {
	// Caller can see connection 100 (shared) but not 42, 43.
	visible := map[int]bool{100: true}

	members := []int{42, 43, 100}

	if !clusterMembersVisible(members, visible) {
		t.Fatal("Expected cluster with at least one shared-member to be " +
			"visible to the caller")
	}
}

// TestClusterMembersVisible_Issue35_EmptyClusterInvisible verifies that
// a cluster containing zero connections is invisible. An empty cluster
// has no ownership or sharing claim to satisfy, so non-superuser
// callers must not see it. This closes the naive "cluster exists ->
// caller can see it" leak.
func TestClusterMembersVisible_Issue35_EmptyClusterInvisible(t *testing.T) {
	visible := map[int]bool{100: true}

	if clusterMembersVisible(nil, visible) {
		t.Fatal("Expected nil-members cluster to be invisible")
	}
	if clusterMembersVisible([]int{}, visible) {
		t.Fatal("Expected empty-members cluster to be invisible")
	}
}

// TestClusterMembersVisible_Issue35_MultipleSharedVisible verifies that
// when several connections are visible to the caller, any overlap with
// the cluster's members makes the cluster visible. This protects
// against the short-circuit ever depending on ordering.
func TestClusterMembersVisible_Issue35_MultipleSharedVisible(t *testing.T) {
	visible := map[int]bool{10: true, 20: true, 30: true}

	// Cluster members interleave visible and hidden IDs; any overlap
	// is enough for visibility.
	members := []int{42, 20, 43}

	if !clusterMembersVisible(members, visible) {
		t.Fatal("Expected cluster to be visible when its members overlap " +
			"the visible set at any position")
	}
}

// =============================================================================
// VisibleConnectionIDs integration for cluster endpoints (issue #35)
//
// The cluster-list, cluster-detail, and cluster-group endpoints all
// call resolveVisibleConnections, which in turn calls
// rbacChecker.VisibleConnectionIDs against a datastore-backed lister.
// These tests pair the checker with a stubVisibilityLister (from
// rbac_issue35_test.go) so we can assert the visible-set contract the
// cluster handlers depend on end-to-end, without requiring a running
// PostgreSQL.
// =============================================================================

// TestVisibleConnectionIDs_Issue35_Clusters_NonOwnerZeroGrant_HidesUnshared
// locks in the contract that drives listClusterGroups / handleListClusters
// for a non-owner zero-grant user: the visible set is empty, so both
// endpoints will filter every cluster whose only members are unshared
// non-owned connections.
func TestVisibleConnectionIDs_Issue35_Clusters_NonOwnerZeroGrant_HidesUnshared(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	bobID := newTestUser(t, store, "bob")

	checker := auth.NewRBACChecker(store)

	// A single unshared connection owned by alice: bob has no grant
	// and is not a superuser, so the visible set must be empty.
	lister := &stubVisibilityLister{
		connections: []auth.ConnectionVisibilityInfo{
			{ID: rbacUnsharedConnID, IsShared: false, OwnerUsername: "alice"},
		},
	}

	ctx := context.WithValue(context.Background(), auth.UserIDContextKey, bobID)
	ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, auth.UsernameContextKey, "bob")

	ids, all, err := checker.VisibleConnectionIDs(ctx, lister)
	if err != nil {
		t.Fatalf("VisibleConnectionIDs: %v", err)
	}
	if all {
		t.Fatal("Non-superuser zero-grant caller must not get allConnections")
	}
	if len(ids) != 0 {
		t.Errorf("Expected empty visible set for non-owner zero-grant, got %v", ids)
	}

	// Feeding that visible set into clusterMembersVisible with a
	// cluster composed entirely of the unshared connection ID must
	// return false: the cluster is invisible to bob. This is the
	// end-to-end decision the getCluster handler makes via
	// clusterHasVisibleConnection.
	visibleMap := make(map[int]bool, len(ids))
	for _, id := range ids {
		visibleMap[id] = true
	}
	if clusterMembersVisible([]int{rbacUnsharedConnID}, visibleMap) {
		t.Error("Expected cluster with only unshared member to be " +
			"invisible to non-owner zero-grant caller")
	}
}

// TestVisibleConnectionIDs_Issue35_Clusters_OwnerSeesOwnUnshared locks
// in the positive case: the owner of an unshared connection still sees
// any cluster that contains it through getCluster / listClustersInGroup.
func TestVisibleConnectionIDs_Issue35_Clusters_OwnerSeesOwnUnshared(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	aliceID := newTestUser(t, store, "alice")

	checker := auth.NewRBACChecker(store)

	lister := &stubVisibilityLister{
		connections: []auth.ConnectionVisibilityInfo{
			{ID: rbacUnsharedConnID, IsShared: false, OwnerUsername: "alice"},
		},
	}

	ctx := context.WithValue(context.Background(), auth.UserIDContextKey, aliceID)
	ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, auth.UsernameContextKey, "alice")

	ids, all, err := checker.VisibleConnectionIDs(ctx, lister)
	if err != nil {
		t.Fatalf("VisibleConnectionIDs: %v", err)
	}
	if all {
		t.Fatal("Owner should not be flagged as having allConnections")
	}
	if len(ids) != 1 || ids[0] != rbacUnsharedConnID {
		t.Fatalf("Expected owner to see [%d], got %v",
			rbacUnsharedConnID, ids)
	}

	// The pure filter confirms the cluster containing the owner's
	// connection is visible through the same code path the handler
	// uses after resolveVisibleConnections.
	visibleMap := map[int]bool{ids[0]: true}
	if !clusterMembersVisible([]int{rbacUnsharedConnID}, visibleMap) {
		t.Error("Owner must see the cluster containing their own unshared " +
			"connection")
	}
}

// TestVisibleConnectionIDs_Issue35_Clusters_Superuser_SkipsFilter locks
// in the superuser escape-hatch. Superusers get allConnections=true, so
// the cluster handlers must not filter and must return every cluster
// including those composed entirely of unshared non-owned connections.
func TestVisibleConnectionIDs_Issue35_Clusters_Superuser_SkipsFilter(t *testing.T) {
	_, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	checker := auth.NewRBACChecker(store)

	// Superuser identity has IsSuperuserContextKey=true; user ID/name
	// are irrelevant for VisibleConnectionIDs in the superuser path.
	ctx := context.WithValue(context.Background(), auth.IsSuperuserContextKey, true)
	ctx = context.WithValue(ctx, auth.UsernameContextKey, "admin")

	lister := &stubVisibilityLister{
		connections: []auth.ConnectionVisibilityInfo{
			{ID: rbacUnsharedConnID, IsShared: false, OwnerUsername: "alice"},
		},
	}

	_, all, err := checker.VisibleConnectionIDs(ctx, lister)
	if err != nil {
		t.Fatalf("VisibleConnectionIDs: %v", err)
	}
	if !all {
		t.Fatal("Superuser must receive allConnections=true so the cluster " +
			"handlers skip filtering entirely")
	}
}
