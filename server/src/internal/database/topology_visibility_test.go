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

// TestPruneTopologyByVisibility_KeepsVisibleServers verifies that only
// servers whose connection IDs appear in the visible set survive the
// prune, and that clusters and groups left without any visible servers
// are dropped.
func TestPruneTopologyByVisibility_KeepsVisibleServers(t *testing.T) {
	topology := []TopologyGroup{
		{
			ID:   "g1",
			Name: "Mixed Group",
			Clusters: []TopologyCluster{
				{
					ID:   "c1",
					Name: "Mixed Cluster",
					Servers: []TopologyServerInfo{
						{ID: 1, Name: "visible-1"},
						{ID: 2, Name: "hidden-2"},
					},
				},
				{
					ID:   "c2",
					Name: "All-Hidden Cluster",
					Servers: []TopologyServerInfo{
						{ID: 3, Name: "hidden-3"},
					},
				},
			},
		},
		{
			ID:   "g2",
			Name: "All-Hidden Group",
			Clusters: []TopologyCluster{
				{
					ID:   "c3",
					Name: "Hidden Only",
					Servers: []TopologyServerInfo{
						{ID: 4, Name: "hidden-4"},
					},
				},
			},
		},
	}

	filtered := pruneTopologyByVisibility(topology, []int{1})

	if len(filtered) != 1 {
		t.Fatalf("Expected 1 group, got %d", len(filtered))
	}
	if filtered[0].ID != "g1" {
		t.Errorf("Expected g1, got %s", filtered[0].ID)
	}
	if len(filtered[0].Clusters) != 1 {
		t.Fatalf("Expected 1 cluster, got %d", len(filtered[0].Clusters))
	}
	if filtered[0].Clusters[0].ID != "c1" {
		t.Errorf("Expected c1, got %s", filtered[0].Clusters[0].ID)
	}
	if len(filtered[0].Clusters[0].Servers) != 1 {
		t.Fatalf("Expected 1 server, got %d",
			len(filtered[0].Clusters[0].Servers))
	}
	if filtered[0].Clusters[0].Servers[0].ID != 1 {
		t.Errorf("Expected server 1, got %d",
			filtered[0].Clusters[0].Servers[0].ID)
	}
}

// TestPruneTopologyByVisibility_EmptyVisibleSetDropsEverything verifies
// that an empty visible set results in an empty topology.
func TestPruneTopologyByVisibility_EmptyVisibleSetDropsEverything(t *testing.T) {
	topology := []TopologyGroup{
		{
			ID:   "g1",
			Name: "Any Group",
			Clusters: []TopologyCluster{
				{
					ID:   "c1",
					Name: "Any Cluster",
					Servers: []TopologyServerInfo{
						{ID: 10, Name: "hidden-10"},
					},
				},
			},
		},
	}

	filtered := pruneTopologyByVisibility(topology, []int{})

	if len(filtered) != 0 {
		t.Errorf("Expected empty result, got %d groups", len(filtered))
	}
}

// TestPruneTopologyServers_RecursesIntoChildren verifies that child
// servers are filtered recursively so a visible parent with hidden
// children retains only the visible descendants.
func TestPruneTopologyServers_RecursesIntoChildren(t *testing.T) {
	visible := map[int]bool{1: true, 3: true}
	servers := []TopologyServerInfo{
		{
			ID:   1,
			Name: "parent-1",
			Children: []TopologyServerInfo{
				{ID: 2, Name: "hidden-child"},
				{ID: 3, Name: "visible-child"},
			},
		},
		{ID: 4, Name: "hidden-peer"},
	}

	out := pruneTopologyServers(servers, visible)

	if len(out) != 1 {
		t.Fatalf("Expected 1 top-level server, got %d", len(out))
	}
	if out[0].ID != 1 {
		t.Errorf("Expected top-level server 1, got %d", out[0].ID)
	}
	if len(out[0].Children) != 1 {
		t.Fatalf("Expected 1 visible child, got %d", len(out[0].Children))
	}
	if out[0].Children[0].ID != 3 {
		t.Errorf("Expected visible child 3, got %d", out[0].Children[0].ID)
	}
}

// TestPruneTopologyServers_HiddenParentDropsVisibleChildren verifies the
// documented contract: a hidden parent is dropped along with its
// children, even if some of those children would otherwise be visible.
func TestPruneTopologyServers_HiddenParentDropsVisibleChildren(t *testing.T) {
	visible := map[int]bool{2: true}
	servers := []TopologyServerInfo{
		{
			ID:   1,
			Name: "hidden-parent",
			Children: []TopologyServerInfo{
				{ID: 2, Name: "visible-child"},
			},
		},
	}

	out := pruneTopologyServers(servers, visible)

	if len(out) != 0 {
		t.Errorf("Expected hidden parent to drop the whole branch, got %d", len(out))
	}
}

// TestGetAlertCounts_EmptyFilterShortCircuits verifies that an explicit
// empty non-nil filter returns a zero result without opening a database
// connection. The test uses a zero-valued Datastore (nil pool) because
// the short-circuit must run before any pool access.
func TestGetAlertCounts_EmptyFilterShortCircuits(t *testing.T) {
	d := &Datastore{}
	result, err := d.GetAlertCounts(context.Background(), []int{})
	if err != nil {
		t.Fatalf("GetAlertCounts: %v", err)
	}
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result.Total != 0 {
		t.Errorf("Expected Total=0, got %d", result.Total)
	}
	if len(result.ByServer) != 0 {
		t.Errorf("Expected empty ByServer, got %+v", result.ByServer)
	}
}

// TestGetClusterTopology_EmptyFilterShortCircuits verifies that an
// explicit empty non-nil filter returns an empty topology without
// opening a database connection.
func TestGetClusterTopology_EmptyFilterShortCircuits(t *testing.T) {
	d := &Datastore{}
	result, err := d.GetClusterTopology(context.Background(), []int{})
	if err != nil {
		t.Fatalf("GetClusterTopology: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("Expected empty topology, got %d groups", len(result))
	}
}
