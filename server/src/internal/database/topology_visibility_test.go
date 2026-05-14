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
	"encoding/json"
	"strings"
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

// TestPruneTopologyServers_FiltersRelationshipsToHiddenPeers verifies
// that Relationships entries targeting hidden peers are dropped during
// pruning so TargetServerID and TargetServerName never leak across the
// visibility boundary. Relationships targeting visible peers must
// survive, and IsExpandable must reflect the remaining child count.
func TestPruneTopologyServers_FiltersRelationshipsToHiddenPeers(t *testing.T) {
	visible := map[int]bool{1: true, 2: true}
	servers := []TopologyServerInfo{
		{
			ID:           1,
			Name:         "visible-1",
			IsExpandable: true,
			Relationships: []TopologyRelationship{
				{
					TargetServerID:   2,
					TargetServerName: "visible-peer",
					RelationshipType: "spock_subscriber",
				},
				{
					TargetServerID:   99,
					TargetServerName: "hidden-peer",
					RelationshipType: "spock_subscriber",
				},
			},
		},
		{
			ID:   2,
			Name: "visible-2",
			Relationships: []TopologyRelationship{
				{
					TargetServerID:   77,
					TargetServerName: "another-hidden",
					RelationshipType: "spock_provider",
				},
			},
		},
	}

	out := pruneTopologyServers(servers, visible)

	if len(out) != 2 {
		t.Fatalf("Expected 2 servers, got %d", len(out))
	}

	// Server 1 keeps only the relationship targeting visible peer 2.
	if len(out[0].Relationships) != 1 {
		t.Fatalf("Expected 1 relationship on server 1, got %d",
			len(out[0].Relationships))
	}
	if out[0].Relationships[0].TargetServerID != 2 {
		t.Errorf("Expected relationship target 2, got %d",
			out[0].Relationships[0].TargetServerID)
	}
	if out[0].Relationships[0].TargetServerName != "visible-peer" {
		t.Errorf("Expected visible-peer target name, got %q",
			out[0].Relationships[0].TargetServerName)
	}

	// Server 2's sole relationship targeted a hidden peer; it should be
	// stripped to an empty slice so no TargetServerID/Name leaks.
	if len(out[1].Relationships) != 0 {
		t.Errorf("Expected all relationships on server 2 to be dropped, got %d",
			len(out[1].Relationships))
	}

	// IsExpandable must reflect the remaining visible child count.
	if out[0].IsExpandable {
		t.Errorf("Expected IsExpandable=false for server 1 with no visible children")
	}
}

// TestPruneTopologyServers_PreservesRelationshipsBetweenVisiblePeers
// verifies that when every relationship targets a visible peer the
// full Relationships slice is preserved intact.
func TestPruneTopologyServers_PreservesRelationshipsBetweenVisiblePeers(t *testing.T) {
	visible := map[int]bool{1: true, 2: true, 3: true}
	servers := []TopologyServerInfo{
		{
			ID:   1,
			Name: "visible-1",
			Relationships: []TopologyRelationship{
				{TargetServerID: 2, TargetServerName: "peer-2"},
				{TargetServerID: 3, TargetServerName: "peer-3"},
			},
		},
	}

	out := pruneTopologyServers(servers, visible)

	if len(out) != 1 {
		t.Fatalf("Expected 1 server, got %d", len(out))
	}
	if len(out[0].Relationships) != 2 {
		t.Fatalf("Expected both relationships preserved, got %d",
			len(out[0].Relationships))
	}
	if out[0].Relationships[0].TargetServerID != 2 ||
		out[0].Relationships[1].TargetServerID != 3 {
		t.Errorf("Relationship order/targets changed: got %+v",
			out[0].Relationships)
	}
}

// TestPruneTopologyServers_IsExpandableReflectsVisibleChildren verifies
// that IsExpandable is recomputed from the pruned Children slice: a
// server whose only children are hidden must become non-expandable, and
// a server with a remaining visible child stays expandable.
func TestPruneTopologyServers_IsExpandableReflectsVisibleChildren(t *testing.T) {
	visible := map[int]bool{1: true, 2: true, 3: true}
	servers := []TopologyServerInfo{
		{
			ID:           1,
			Name:         "parent-all-hidden-children",
			IsExpandable: true,
			Children: []TopologyServerInfo{
				{ID: 98, Name: "hidden-child-a"},
				{ID: 99, Name: "hidden-child-b"},
			},
		},
		{
			ID:           2,
			Name:         "parent-mixed",
			IsExpandable: true,
			Children: []TopologyServerInfo{
				{ID: 3, Name: "visible-child"},
				{ID: 97, Name: "hidden-child"},
			},
		},
	}

	out := pruneTopologyServers(servers, visible)

	if len(out) != 2 {
		t.Fatalf("Expected 2 top-level servers, got %d", len(out))
	}
	if out[0].IsExpandable {
		t.Errorf("Expected IsExpandable=false when all children hidden")
	}
	if !out[1].IsExpandable {
		t.Errorf("Expected IsExpandable=true when one child is visible")
	}
	if len(out[1].Children) != 1 || out[1].Children[0].ID != 3 {
		t.Errorf("Expected single visible child ID=3, got %+v", out[1].Children)
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

// TestGetClusterTopology_EmptyFilterReturnsNonNilSlice is the regression
// test for issue #242. The empty-filter short-circuit must return a
// non-nil []TopologyGroup so that the JSON encoding is "[]" rather than
// "null"; the web client crashes when the topology comes back as null.
func TestGetClusterTopology_EmptyFilterReturnsNonNilSlice(t *testing.T) {
	d := &Datastore{}
	result, err := d.GetClusterTopology(context.Background(), []int{})
	if err != nil {
		t.Fatalf("GetClusterTopology: %v", err)
	}
	if result == nil {
		t.Fatal("Expected non-nil slice for empty filter; nil marshals to JSON null and crashes the client")
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if string(encoded) != "[]" {
		t.Errorf("Expected JSON \"[]\", got %q", string(encoded))
	}
}

// TestNormalizeTopologyGroups_ReplacesNilClustersWithEmpty is the unit
// regression for issue #242. A TopologyGroup whose Clusters is nil must
// be normalized to a non-nil empty slice so JSON encoding emits
// "clusters": [] rather than "clusters": null. This is the exact bug the
// web client crashed on after the only cluster in a group was deleted.
func TestNormalizeTopologyGroups_ReplacesNilClustersWithEmpty(t *testing.T) {
	groups := []TopologyGroup{
		{
			ID:       "g-empty",
			Name:     "Default",
			Clusters: nil, // The bug shape: nil after deletion.
		},
	}

	normalizeTopologyGroups(groups)

	if groups[0].Clusters == nil {
		t.Fatal("Expected non-nil Clusters slice after normalization")
	}
	if len(groups[0].Clusters) != 0 {
		t.Errorf("Expected empty Clusters slice, got %d", len(groups[0].Clusters))
	}

	encoded, err := json.Marshal(groups[0])
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !strings.Contains(string(encoded), `"clusters":[]`) {
		t.Errorf("Expected JSON to contain \"clusters\":[], got %s", string(encoded))
	}
	if strings.Contains(string(encoded), `"clusters":null`) {
		t.Errorf("Expected JSON not to contain \"clusters\":null, got %s", string(encoded))
	}
}

// TestNormalizeTopologyGroups_ReplacesNilServersWithEmpty verifies the
// same nil-slice contract applies to TopologyCluster.Servers. A cluster
// whose Servers field is nil must marshal to "servers": [] rather than
// "servers": null.
func TestNormalizeTopologyGroups_ReplacesNilServersWithEmpty(t *testing.T) {
	groups := []TopologyGroup{
		{
			ID:   "g1",
			Name: "Default",
			Clusters: []TopologyCluster{
				{
					ID:          "c-empty",
					Name:        "Empty Cluster",
					ClusterType: "manual",
					Servers:     nil,
				},
			},
		},
	}

	normalizeTopologyGroups(groups)

	if groups[0].Clusters[0].Servers == nil {
		t.Fatal("Expected non-nil Servers slice after normalization")
	}
	if len(groups[0].Clusters[0].Servers) != 0 {
		t.Errorf("Expected empty Servers slice, got %d", len(groups[0].Clusters[0].Servers))
	}

	encoded, err := json.Marshal(groups[0].Clusters[0])
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !strings.Contains(string(encoded), `"servers":[]`) {
		t.Errorf("Expected JSON to contain \"servers\":[], got %s", string(encoded))
	}
	if strings.Contains(string(encoded), `"servers":null`) {
		t.Errorf("Expected JSON not to contain \"servers\":null, got %s", string(encoded))
	}
}

// TestNormalizeTopologyGroups_PreservesNonNilSlices verifies that
// normalization leaves already-initialized slices untouched. The function
// must only replace nil slices, never overwrite existing data.
func TestNormalizeTopologyGroups_PreservesNonNilSlices(t *testing.T) {
	groups := []TopologyGroup{
		{
			ID:   "g1",
			Name: "Group",
			Clusters: []TopologyCluster{
				{
					ID:   "c1",
					Name: "Cluster",
					Servers: []TopologyServerInfo{
						{ID: 42, Name: "node"},
					},
				},
			},
		},
	}

	normalizeTopologyGroups(groups)

	if len(groups[0].Clusters) != 1 {
		t.Fatalf("Expected 1 cluster preserved, got %d", len(groups[0].Clusters))
	}
	if len(groups[0].Clusters[0].Servers) != 1 {
		t.Fatalf("Expected 1 server preserved, got %d", len(groups[0].Clusters[0].Servers))
	}
	if groups[0].Clusters[0].Servers[0].ID != 42 {
		t.Errorf("Expected server ID 42, got %d", groups[0].Clusters[0].Servers[0].ID)
	}
}

// TestNormalizeTopologyGroups_HandlesEmptyAndNilInput verifies the helper
// is safe to call on edge inputs: a nil slice and an empty slice. Both
// must return without panicking; the function has no return value, so
// the assertion is simply that the call completes without panicking and
// that the empty slice remains empty.
func TestNormalizeTopologyGroups_HandlesEmptyAndNilInput(t *testing.T) {
	// nil input: must not panic. There is nothing to assert on the
	// returned slice because the function takes the slice by value;
	// callers that need the nil-preserving guarantee can rely on the
	// fact that the empty for-loop body never runs.
	var nilGroups []TopologyGroup
	normalizeTopologyGroups(nilGroups)

	// empty input
	emptyGroups := []TopologyGroup{}
	normalizeTopologyGroups(emptyGroups)
	if len(emptyGroups) != 0 {
		t.Errorf("Expected empty input preserved, got %d groups", len(emptyGroups))
	}
}

// TestBuildTopologyHierarchy_EmptyConnections_ReturnsNonNilClusters is
// the producer-side regression for issue #242. When the connection list
// is empty, buildTopologyHierarchy must still return a default group
// whose Clusters slice is non-nil so that the topology marshals to JSON
// with "clusters": [] rather than "clusters": null. This covers the
// "after the only cluster is deleted" scenario that caused the bug.
func TestBuildTopologyHierarchy_EmptyConnections_ReturnsNonNilClusters(t *testing.T) {
	ds := &Datastore{}
	defaultGroup := &defaultGroupInfo{ID: 1, Name: "Servers/Clusters"}

	groups := ds.buildTopologyHierarchy(
		nil, // no connections at all
		make(map[string]clusterOverride),
		make(map[string]bool),
		make(map[string]bool),
		defaultGroup,
	)

	if len(groups) != 1 {
		t.Fatalf("Expected 1 default group, got %d", len(groups))
	}
	if groups[0].Clusters == nil {
		t.Fatal("Expected non-nil Clusters slice; nil marshals to JSON null and crashes the client")
	}
	if len(groups[0].Clusters) != 0 {
		t.Errorf("Expected empty Clusters slice for no-connections case, got %d",
			len(groups[0].Clusters))
	}

	// Confirm the JSON wire shape.
	encoded, err := json.Marshal(groups[0])
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !strings.Contains(string(encoded), `"clusters":[]`) {
		t.Errorf("Expected JSON to contain \"clusters\":[], got %s", string(encoded))
	}
	if strings.Contains(string(encoded), `"clusters":null`) {
		t.Errorf("Expected JSON not to contain \"clusters\":null, got %s", string(encoded))
	}
}

// TestNormalizeTopologyGroups_MixedNilAndNonNilSlices is a comprehensive
// regression test that mixes the nil-clusters case (the issue #242 bug
// shape) with adjacent groups whose Clusters slice is already populated.
// Normalization must touch only the nil ones and leave the rest intact.
func TestNormalizeTopologyGroups_MixedNilAndNonNilSlices(t *testing.T) {
	groups := []TopologyGroup{
		{
			ID:       "g-empty",
			Name:     "Empty Group",
			Clusters: nil,
		},
		{
			ID:   "g-populated",
			Name: "Populated Group",
			Clusters: []TopologyCluster{
				{
					ID:          "c1",
					Name:        "With Servers",
					ClusterType: "manual",
					Servers: []TopologyServerInfo{
						{ID: 1, Name: "n1"},
					},
				},
				{
					ID:          "c2",
					Name:        "Empty Servers",
					ClusterType: "manual",
					Servers:     nil,
				},
			},
		},
	}

	normalizeTopologyGroups(groups)

	if groups[0].Clusters == nil {
		t.Fatal("Expected first group's Clusters to be non-nil after normalization")
	}
	if len(groups[0].Clusters) != 0 {
		t.Errorf("Expected first group's Clusters to be empty, got %d", len(groups[0].Clusters))
	}

	if len(groups[1].Clusters) != 2 {
		t.Fatalf("Expected second group to have 2 clusters preserved, got %d", len(groups[1].Clusters))
	}
	if len(groups[1].Clusters[0].Servers) != 1 {
		t.Errorf("Expected populated cluster's Servers preserved, got %d",
			len(groups[1].Clusters[0].Servers))
	}
	if groups[1].Clusters[1].Servers == nil {
		t.Fatal("Expected empty-servers cluster to be normalized to non-nil")
	}
	if len(groups[1].Clusters[1].Servers) != 0 {
		t.Errorf("Expected empty-servers cluster Servers to be empty, got %d",
			len(groups[1].Clusters[1].Servers))
	}

	// Encode the whole tree and confirm no "clusters":null / "servers":null leaks.
	encoded, err := json.Marshal(groups)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if strings.Contains(string(encoded), `"clusters":null`) {
		t.Errorf("Expected no \"clusters\":null in JSON, got %s", string(encoded))
	}
	if strings.Contains(string(encoded), `"servers":null`) {
		t.Errorf("Expected no \"servers\":null in JSON, got %s", string(encoded))
	}
}
