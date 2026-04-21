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
	"database/sql"
	"testing"
)

// TestTopologyExcludesManualMembersFromAutoClusters is the regression test
// for issue #74. When a user deletes an auto-detected (Spock) cluster and
// its servers, re-adds those servers, and assigns them to a manually
// created cluster (which sets connections.membership_source = 'manual'),
// the next topology refresh must NOT regroup those servers under a
// re-created auto-detected Spock cluster.
//
// The two topology builders exercised here are pure functions that
// operate on in-memory []connectionWithRole slices, so the test runs
// without a database.
func TestTopologyExcludesManualMembersFromAutoClusters(t *testing.T) {
	ds := &Datastore{}

	// Three Spock nodes that share a naming prefix ("pg17-") would
	// normally form an auto-detected Spock cluster. Here all three are
	// pinned to a manually created cluster (membership_source='manual'),
	// so they must not appear under any auto-detected cluster.
	manualSpockConns := []connectionWithRole{
		{
			ID:               101,
			Name:             "pg17-node1",
			Host:             "10.0.0.1",
			Port:             5432,
			DatabaseName:     "app",
			Username:         "postgres",
			PrimaryRole:      "spock_node",
			HasSpock:         true,
			MembershipSource: "manual",
			Status:           "online",
			ClusterID:        sql.NullInt64{Int64: 42, Valid: true},
		},
		{
			ID:               102,
			Name:             "pg17-node2",
			Host:             "10.0.0.2",
			Port:             5432,
			DatabaseName:     "app",
			Username:         "postgres",
			PrimaryRole:      "spock_node",
			HasSpock:         true,
			MembershipSource: "manual",
			Status:           "online",
			ClusterID:        sql.NullInt64{Int64: 42, Valid: true},
		},
		{
			ID:               103,
			Name:             "pg17-node3",
			Host:             "10.0.0.3",
			Port:             5432,
			DatabaseName:     "app",
			Username:         "postgres",
			PrimaryRole:      "spock_node",
			HasSpock:         true,
			MembershipSource: "manual",
			Status:           "online",
			ClusterID:        sql.NullInt64{Int64: 42, Valid: true},
		},
	}

	overrides := map[string]clusterOverride{}

	// buildAutoDetectedClusters must not produce a Spock cluster for the
	// "pg17-" prefix when every candidate is pinned to a manual cluster.
	autoClusters := ds.buildAutoDetectedClusters(manualSpockConns, overrides)
	if cluster, ok := autoClusters["spock:pg17"]; ok {
		t.Fatalf("manual Spock members were regrouped into auto cluster %q with %d servers (issue #74)",
			cluster.AutoClusterKey, len(cluster.Servers))
	}
	for key, cluster := range autoClusters {
		if cluster.ClusterType == "spock" || cluster.ClusterType == "spock_ha" {
			t.Fatalf("unexpected auto Spock cluster %q (%s) containing %d manual servers",
				key, cluster.ClusterType, len(cluster.Servers))
		}
	}

	// buildTopologyHierarchy must not expose the same connections as an
	// auto-detected Spock cluster inside the default group either. Any
	// clusters produced here should be "server" (standalone) entries
	// carrying the manual servers individually at most; however, because
	// each connection has cluster_id set, they are also skipped from the
	// standalone branch. Either way, no cluster with type "spock" or
	// "spock_ha" should be returned.
	defaultGroup := &defaultGroupInfo{ID: 1, Name: "Servers/Clusters"}
	claimed := map[string]bool{}
	groups := ds.buildTopologyHierarchy(manualSpockConns, overrides, claimed, defaultGroup)
	if len(groups) != 1 {
		t.Fatalf("expected 1 topology group, got %d", len(groups))
	}
	for _, cluster := range groups[0].Clusters {
		if cluster.ClusterType == "spock" || cluster.ClusterType == "spock_ha" {
			t.Fatalf("buildTopologyHierarchy produced auto Spock cluster %q for manual members (issue #74)",
				cluster.AutoClusterKey)
		}
		// The manual servers also must not leak out as auto-detected
		// binary or logical clusters.
		if cluster.ClusterType == "binary" || cluster.ClusterType == "logical" {
			for _, s := range cluster.Servers {
				if s.MembershipSource == "manual" {
					t.Fatalf("manual server %d (%s) surfaced in auto %s cluster %q (issue #74)",
						s.ID, s.Name, cluster.ClusterType, cluster.AutoClusterKey)
				}
			}
		}
	}
}

// TestTopologyExcludesManualBinaryPrimaryFromAutoClusters verifies that
// a manually pinned binary primary (with a streaming standby child) is
// not regrouped under an auto-detected binary cluster.
func TestTopologyExcludesManualBinaryPrimaryFromAutoClusters(t *testing.T) {
	ds := &Datastore{}

	conns := []connectionWithRole{
		{
			ID:               201,
			Name:             "prod-primary",
			Host:             "10.0.1.1",
			Port:             5432,
			DatabaseName:     "app",
			Username:         "postgres",
			PrimaryRole:      "binary_primary",
			MembershipSource: "manual",
			Status:           "online",
			ClusterID:        sql.NullInt64{Int64: 99, Valid: true},
		},
		{
			ID:                 202,
			Name:               "prod-standby",
			Host:               "10.0.1.2",
			Port:               5432,
			DatabaseName:       "app",
			Username:           "postgres",
			PrimaryRole:        "binary_standby",
			MembershipSource:   "manual",
			Status:             "online",
			IsStreamingStandby: true,
			UpstreamHost:       sql.NullString{String: "10.0.1.1", Valid: true},
			UpstreamPort:       sql.NullInt32{Int32: 5432, Valid: true},
			ClusterID:          sql.NullInt64{Int64: 99, Valid: true},
		},
	}

	overrides := map[string]clusterOverride{}

	autoClusters := ds.buildAutoDetectedClusters(conns, overrides)
	if cluster, ok := autoClusters["binary:201"]; ok {
		t.Fatalf("manual binary primary regrouped into auto cluster %q with %d servers (issue #74)",
			cluster.AutoClusterKey, len(cluster.Servers))
	}

	defaultGroup := &defaultGroupInfo{ID: 1, Name: "Servers/Clusters"}
	groups := ds.buildTopologyHierarchy(conns, overrides, map[string]bool{}, defaultGroup)
	if len(groups) != 1 {
		t.Fatalf("expected 1 topology group, got %d", len(groups))
	}
	for _, cluster := range groups[0].Clusters {
		if cluster.ClusterType == "binary" {
			t.Fatalf("buildTopologyHierarchy produced auto binary cluster %q for manual members (issue #74)",
				cluster.AutoClusterKey)
		}
	}
}

// TestTopologyExcludesManualLogicalPublisherFromAutoClusters verifies
// that a manually pinned logical publisher/subscriber pair does not
// regroup into an auto-detected logical-replication cluster.
func TestTopologyExcludesManualLogicalPublisherFromAutoClusters(t *testing.T) {
	ds := &Datastore{}

	conns := []connectionWithRole{
		{
			ID:               301,
			Name:             "pub",
			Host:             "10.0.2.1",
			Port:             5432,
			DatabaseName:     "app",
			Username:         "postgres",
			PrimaryRole:      "logical_publisher",
			MembershipSource: "manual",
			Status:           "online",
			ClusterID:        sql.NullInt64{Int64: 7, Valid: true},
		},
		{
			ID:               302,
			Name:             "sub",
			Host:             "10.0.2.2",
			Port:             5432,
			DatabaseName:     "app",
			Username:         "postgres",
			PrimaryRole:      "logical_subscriber",
			MembershipSource: "manual",
			Status:           "online",
			PublisherHost:    sql.NullString{String: "10.0.2.1", Valid: true},
			PublisherPort:    sql.NullInt32{Int32: 5432, Valid: true},
			ClusterID:        sql.NullInt64{Int64: 7, Valid: true},
		},
	}

	overrides := map[string]clusterOverride{}

	autoClusters := ds.buildAutoDetectedClusters(conns, overrides)
	if cluster, ok := autoClusters["logical:301"]; ok {
		t.Fatalf("manual logical publisher regrouped into auto cluster %q with %d servers (issue #74)",
			cluster.AutoClusterKey, len(cluster.Servers))
	}

	defaultGroup := &defaultGroupInfo{ID: 1, Name: "Servers/Clusters"}
	groups := ds.buildTopologyHierarchy(conns, overrides, map[string]bool{}, defaultGroup)
	if len(groups) != 1 {
		t.Fatalf("expected 1 topology group, got %d", len(groups))
	}
	for _, cluster := range groups[0].Clusters {
		if cluster.ClusterType == "logical" {
			t.Fatalf("buildTopologyHierarchy produced auto logical cluster %q for manual members (issue #74)",
				cluster.AutoClusterKey)
		}
	}
}

// TestTopologyIncludesAutoMembersInAutoClusters is a sanity check that
// the manual-membership filter does not accidentally suppress genuine
// auto-detected clusters. Three Spock nodes with membership_source =
// 'auto' must still form a spock cluster.
func TestTopologyIncludesAutoMembersInAutoClusters(t *testing.T) {
	ds := &Datastore{}

	conns := []connectionWithRole{
		{
			ID:               401,
			Name:             "pg17-node1",
			Host:             "10.0.3.1",
			Port:             5432,
			DatabaseName:     "app",
			Username:         "postgres",
			PrimaryRole:      "spock_node",
			HasSpock:         true,
			MembershipSource: "auto",
			Status:           "online",
		},
		{
			ID:               402,
			Name:             "pg17-node2",
			Host:             "10.0.3.2",
			Port:             5432,
			DatabaseName:     "app",
			Username:         "postgres",
			PrimaryRole:      "spock_node",
			HasSpock:         true,
			MembershipSource: "auto",
			Status:           "online",
		},
		{
			ID:               403,
			Name:             "pg17-node3",
			Host:             "10.0.3.3",
			Port:             5432,
			DatabaseName:     "app",
			Username:         "postgres",
			PrimaryRole:      "spock_node",
			HasSpock:         true,
			MembershipSource: "auto",
			Status:           "online",
		},
	}

	overrides := map[string]clusterOverride{}
	autoClusters := ds.buildAutoDetectedClusters(conns, overrides)
	cluster, ok := autoClusters["spock:pg17"]
	if !ok {
		t.Fatalf("expected auto Spock cluster %q for three auto Spock nodes; got keys %v",
			"spock:pg17", keysOf(autoClusters))
	}
	if len(cluster.Servers) != 3 {
		t.Fatalf("expected 3 servers in auto Spock cluster, got %d", len(cluster.Servers))
	}
}

// TestTopologyExcludesManualChildrenFromAutoBinaryCluster is a
// regression test for the follow-up to issue #74: a child connection
// pinned to a manual cluster must not be pulled into the auto-detected
// tree rooted at a still-auto primary. Prior to the fix,
// buildServerWithChildren recursed over childrenMap without inspecting
// MembershipSource, so an auto binary primary with a manually-pinned
// streaming standby child produced an auto cluster that contained the
// manual standby in its Children tree.
func TestTopologyExcludesManualChildrenFromAutoBinaryCluster(t *testing.T) {
	ds := &Datastore{}

	conns := []connectionWithRole{
		{
			ID:               401,
			Name:             "auto-primary",
			Host:             "10.0.4.1",
			Port:             5432,
			DatabaseName:     "app",
			Username:         "postgres",
			PrimaryRole:      "binary_primary",
			MembershipSource: "auto",
			Status:           "online",
		},
		{
			ID:                 402,
			Name:               "manual-standby",
			Host:               "10.0.4.2",
			Port:               5432,
			DatabaseName:       "app",
			Username:           "postgres",
			PrimaryRole:        "binary_standby",
			MembershipSource:   "manual",
			Status:             "online",
			IsStreamingStandby: true,
			UpstreamHost:       sql.NullString{String: "10.0.4.1", Valid: true},
			UpstreamPort:       sql.NullInt32{Int32: 5432, Valid: true},
			ClusterID:          sql.NullInt64{Int64: 55, Valid: true},
		},
	}

	overrides := map[string]clusterOverride{}

	// buildAutoDetectedClusters must produce the auto binary cluster for
	// the primary (it still has the standby linked via childrenMap), but
	// the Children list of that primary must not contain the manually
	// pinned standby.
	autoClusters := ds.buildAutoDetectedClusters(conns, overrides)
	cluster, ok := autoClusters["binary:401"]
	if !ok {
		t.Fatalf("expected auto binary cluster %q for auto primary; got keys %v",
			"binary:401", keysOf(autoClusters))
	}
	if len(cluster.Servers) != 1 {
		t.Fatalf("expected 1 top-level server in auto binary cluster, got %d",
			len(cluster.Servers))
	}
	primary := cluster.Servers[0]
	if primary.ID != 401 {
		t.Fatalf("expected primary ID 401 at the top of the auto cluster, got %d", primary.ID)
	}
	for _, child := range primary.Children {
		if child.MembershipSource == "manual" {
			t.Fatalf("manual child %d (%s) leaked into auto binary cluster %q (issue #74 follow-up)",
				child.ID, child.Name, cluster.AutoClusterKey)
		}
		if child.ID == 402 {
			t.Fatalf("manual standby 402 leaked into auto binary cluster %q (issue #74 follow-up)",
				cluster.AutoClusterKey)
		}
	}

	// buildTopologyHierarchy must likewise keep the manual standby out of
	// the auto binary cluster tree in the default group.
	defaultGroup := &defaultGroupInfo{ID: 1, Name: "Servers/Clusters"}
	groups := ds.buildTopologyHierarchy(conns, overrides, map[string]bool{}, defaultGroup)
	if len(groups) != 1 {
		t.Fatalf("expected 1 topology group, got %d", len(groups))
	}
	foundAutoBinary := false
	for _, c := range groups[0].Clusters {
		if c.AutoClusterKey != "binary:401" {
			continue
		}
		foundAutoBinary = true
		if len(c.Servers) != 1 {
			t.Fatalf("expected 1 top-level server in auto binary cluster, got %d",
				len(c.Servers))
		}
		for _, child := range c.Servers[0].Children {
			if child.MembershipSource == "manual" {
				t.Fatalf("manual child %d (%s) leaked into auto binary cluster %q via buildTopologyHierarchy (issue #74 follow-up)",
					child.ID, child.Name, c.AutoClusterKey)
			}
			if child.ID == 402 {
				t.Fatalf("manual standby 402 leaked into auto binary cluster %q via buildTopologyHierarchy (issue #74 follow-up)",
					c.AutoClusterKey)
			}
		}
	}
	if !foundAutoBinary {
		t.Fatalf("expected buildTopologyHierarchy to return auto binary cluster %q; clusters: %+v",
			"binary:401", groups[0].Clusters)
	}
}

// TestTopologyExcludesManualSubscriberFromAutoLogicalCluster verifies
// that an auto-detected logical publisher does not pick up a manually
// pinned logical subscriber. The auto cluster is only created when the
// publisher has at least one non-manual subscriber; a manual subscriber
// must never appear in an auto logical cluster.
func TestTopologyExcludesManualSubscriberFromAutoLogicalCluster(t *testing.T) {
	ds := &Datastore{}

	conns := []connectionWithRole{
		{
			ID:               501,
			Name:             "auto-pub",
			Host:             "10.0.5.1",
			Port:             5432,
			DatabaseName:     "app",
			Username:         "postgres",
			PrimaryRole:      "logical_publisher",
			MembershipSource: "auto",
			Status:           "online",
		},
		{
			ID:               502,
			Name:             "auto-sub",
			Host:             "10.0.5.2",
			Port:             5432,
			DatabaseName:     "app",
			Username:         "postgres",
			PrimaryRole:      "logical_subscriber",
			MembershipSource: "auto",
			Status:           "online",
			PublisherHost:    sql.NullString{String: "10.0.5.1", Valid: true},
			PublisherPort:    sql.NullInt32{Int32: 5432, Valid: true},
		},
		{
			ID:               503,
			Name:             "manual-sub",
			Host:             "10.0.5.3",
			Port:             5432,
			DatabaseName:     "app",
			Username:         "postgres",
			PrimaryRole:      "logical_subscriber",
			MembershipSource: "manual",
			Status:           "online",
			PublisherHost:    sql.NullString{String: "10.0.5.1", Valid: true},
			PublisherPort:    sql.NullInt32{Int32: 5432, Valid: true},
			ClusterID:        sql.NullInt64{Int64: 77, Valid: true},
		},
	}

	overrides := map[string]clusterOverride{}

	autoClusters := ds.buildAutoDetectedClusters(conns, overrides)
	cluster, ok := autoClusters["logical:501"]
	if !ok {
		t.Fatalf("expected auto logical cluster %q for auto publisher; got keys %v",
			"logical:501", keysOf(autoClusters))
	}
	if len(cluster.Servers) != 1 {
		t.Fatalf("expected 1 top-level server in auto logical cluster, got %d",
			len(cluster.Servers))
	}
	publisher := cluster.Servers[0]
	if publisher.ID != 501 {
		t.Fatalf("expected publisher ID 501 at the top of the auto cluster, got %d", publisher.ID)
	}
	for _, child := range publisher.Children {
		if child.MembershipSource == "manual" {
			t.Fatalf("manual subscriber %d (%s) leaked into auto logical cluster %q (issue #74 follow-up)",
				child.ID, child.Name, cluster.AutoClusterKey)
		}
		if child.ID == 503 {
			t.Fatalf("manual subscriber 503 leaked into auto logical cluster %q (issue #74 follow-up)",
				cluster.AutoClusterKey)
		}
	}

	defaultGroup := &defaultGroupInfo{ID: 1, Name: "Servers/Clusters"}
	groups := ds.buildTopologyHierarchy(conns, overrides, map[string]bool{}, defaultGroup)
	if len(groups) != 1 {
		t.Fatalf("expected 1 topology group, got %d", len(groups))
	}
	foundAutoLogical := false
	for _, c := range groups[0].Clusters {
		if c.AutoClusterKey != "logical:501" {
			continue
		}
		foundAutoLogical = true
		if len(c.Servers) != 1 {
			t.Fatalf("expected 1 top-level server in auto logical cluster, got %d",
				len(c.Servers))
		}
		for _, child := range c.Servers[0].Children {
			if child.MembershipSource == "manual" {
				t.Fatalf("manual subscriber %d (%s) leaked into auto logical cluster %q via buildTopologyHierarchy (issue #74 follow-up)",
					child.ID, child.Name, c.AutoClusterKey)
			}
			if child.ID == 503 {
				t.Fatalf("manual subscriber 503 leaked into auto logical cluster %q via buildTopologyHierarchy (issue #74 follow-up)",
					c.AutoClusterKey)
			}
		}
	}
	if !foundAutoLogical {
		t.Fatalf("expected buildTopologyHierarchy to return auto logical cluster %q; clusters: %+v",
			"logical:501", groups[0].Clusters)
	}
}

// keysOf returns the keys of a map[string]TopologyCluster for readable
// test failure messages.
func keysOf(m map[string]TopologyCluster) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
