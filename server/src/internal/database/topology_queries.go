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

	"github.com/pgedge/ai-workbench/pkg/logger"
)

// TopologyServerInfo extends ServerInfo with topology and child servers
type TopologyServerInfo struct {
	ID               int                    `json:"id"`
	Name             string                 `json:"name"`
	Description      string                 `json:"description"`
	Host             string                 `json:"host"`
	Port             int                    `json:"port"`
	Status           string                 `json:"status"`
	ConnectionError  string                 `json:"connection_error,omitempty"`
	ActiveAlertCount int                    `json:"active_alert_count"`
	Role             string                 `json:"role,omitempty"`
	PrimaryRole      string                 `json:"primary_role"`
	IsExpandable     bool                   `json:"is_expandable"`
	MembershipSource string                 `json:"membership_source,omitempty"`
	OwnerUsername    string                 `json:"owner_username,omitempty"`
	Version          string                 `json:"version,omitempty"`
	OS               string                 `json:"os,omitempty"`
	SpockNodeName    string                 `json:"spock_node_name,omitempty"`
	SpockVersion     string                 `json:"spock_version,omitempty"`
	DatabaseName     string                 `json:"database_name,omitempty"`
	Username         string                 `json:"username,omitempty"`
	Children         []TopologyServerInfo   `json:"children,omitempty"`
	Relationships    []TopologyRelationship `json:"relationships,omitempty"`
}

// TopologyRelationship represents a directed edge from a source server to
// a target server within a cluster
type TopologyRelationship struct {
	TargetServerID   int    `json:"target_server_id"`
	TargetServerName string `json:"target_server_name"`
	RelationshipType string `json:"relationship_type"`
	IsAutoDetected   bool   `json:"is_auto_detected"`
}

// NodeRelationship represents a stored relationship between two nodes
type NodeRelationship struct {
	ID                 int    `json:"id"`
	ClusterID          int    `json:"cluster_id"`
	SourceConnectionID int    `json:"source_connection_id"`
	TargetConnectionID int    `json:"target_connection_id"`
	SourceName         string `json:"source_name"`
	TargetName         string `json:"target_name"`
	RelationshipType   string `json:"relationship_type"`
	IsAutoDetected     bool   `json:"is_auto_detected"`
}

// RelationshipInput is the request body for setting manual relationships
// from a source node
type RelationshipInput struct {
	TargetConnectionID int    `json:"target_connection_id"`
	RelationshipType   string `json:"relationship_type"`
}

// AutoRelationshipInput describes a single auto-detected relationship
// for use during topology refresh
type AutoRelationshipInput struct {
	SourceConnectionID int
	TargetConnectionID int
	RelationshipType   string
}

// TopologyCluster represents a replication-aware cluster
type TopologyCluster struct {
	ID              string               `json:"id"`
	Name            string               `json:"name"`
	Description     string               `json:"description,omitempty"`
	ClusterType     string               `json:"cluster_type"`               // spock, spock_ha, binary, logical, server
	ReplicationType string               `json:"replication_type,omitempty"` // user-set replication type from the database
	AutoClusterKey  string               `json:"auto_cluster_key,omitempty"`
	Servers         []TopologyServerInfo `json:"servers"`
}

// TopologyGroup represents a group with topology-aware clusters
type TopologyGroup struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	IsDefault    bool              `json:"is_default,omitempty"`
	AutoGroupKey string            `json:"auto_group_key,omitempty"`
	Clusters     []TopologyCluster `json:"clusters"`
}

// connectionWithRole holds connection data with role information from metrics
type connectionWithRole struct {
	ID                 int
	Name               string
	Description        sql.NullString
	Host               string
	Port               int
	OwnerUsername      sql.NullString
	DatabaseName       string
	Username           string
	Version            sql.NullString // PostgreSQL version from metrics
	OS                 sql.NullString // OS name from pg_sys_os_info
	PrimaryRole        string
	UpstreamHost       sql.NullString
	UpstreamPort       sql.NullInt32
	PublisherHost      sql.NullString // For logical subscribers: publisher's host
	PublisherPort      sql.NullInt32  // For logical subscribers: publisher's port
	HasSpock           bool
	SpockNodeName      sql.NullString
	SpockVersion       sql.NullString // Spock extension version from metrics.pg_extension
	BinaryStandbyCount int
	IsStreamingStandby bool
	MembershipSource   string
	Status             string
	ConnectionError    sql.NullString
	ActiveAlertCount   int
	SystemIdentifier   sql.NullInt64 // from metrics.pg_server_info
	ClusterID          sql.NullInt64 // persisted cluster_id from connections table
}

// ConnectionClusterInfo holds cluster-related information for a connection
type ConnectionClusterInfo struct {
	ClusterID        *int    `json:"cluster_id"`
	Role             *string `json:"role"`
	MembershipSource string  `json:"membership_source"`
	ClusterName      *string `json:"cluster_name"`
	ReplicationType  *string `json:"replication_type"`
	AutoClusterKey   *string `json:"auto_cluster_key"`
}

// ClusterSummary holds minimal cluster information for autocomplete
type ClusterSummary struct {
	ID              int     `json:"id"`
	Name            string  `json:"name"`
	ReplicationType *string `json:"replication_type"`
	AutoClusterKey  *string `json:"auto_cluster_key"`
}

// GetClusterTopology returns the combined topology including manually-created
// cluster groups and auto-detected replication topology. When
// visibleConnectionIDs is non-nil the topology is pruned to servers whose
// connection ID is in the slice; clusters with no visible servers and
// groups with no visible clusters are dropped. A nil slice means the
// caller has unrestricted visibility (superuser or wildcard scope) and
// the full topology is returned.
func (d *Datastore) GetClusterTopology(ctx context.Context, visibleConnectionIDs []int) ([]TopologyGroup, error) {
	// An explicit empty allow-list means no connections are visible;
	// short-circuit before any datastore work.
	if visibleConnectionIDs != nil && len(visibleConnectionIDs) == 0 {
		return []TopologyGroup{}, nil
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	var result []TopologyGroup

	// Step 1: Get the default group info (database-backed since migration 16)
	defaultGroup, err := d.getDefaultGroupInternal(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get default group: %w", err)
	}

	// Step 2: Query ALL connections for auto-detection (not filtered by cluster_id)
	// This builds the complete picture of auto-detected clusters
	allConnections, err := d.getAllConnectionsWithRoles(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query connections with roles: %w", err)
	}

	// Get cluster name overrides for auto-detected clusters
	clusterOverrides, err := d.getClusterOverridesInternal(ctx)
	if err != nil {
		clusterOverrides = make(map[string]clusterOverride)
	}

	// Step 3: Build auto-detected topology from ALL connections
	// This creates a map of auto_cluster_key -> TopologyCluster with servers
	autoDetectedClusters := d.buildAutoDetectedClusters(allConnections, clusterOverrides)

	// Step 4: Get clusters that have been moved to non-default groups
	// These have auto_cluster_key set AND group_id pointing to a non-default group
	claimedKeys, err := d.getClaimedAutoClusterKeys(ctx, defaultGroup.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get claimed cluster keys: %w", err)
	}

	// Step 5: Build manual groups topology (excluding the default group)
	manualGroups, err := d.buildManualGroupsTopology(ctx, autoDetectedClusters, defaultGroup.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to build manual groups topology: %w", err)
	}

	// Track connection IDs assigned to manual clusters (including children recursively)
	manuallyAssignedConnections := make(map[int]bool)
	for i := range manualGroups {
		for j := range manualGroups[i].Clusters {
			collectServerIDsRecursive(manualGroups[i].Clusters[j].Servers, manuallyAssignedConnections)
		}
	}

	// Add manual groups to result
	result = append(result, manualGroups...)

	// Step 6: Build default group with remaining (unclaimed) clusters
	// Filter out connections that are in manual groups
	var unclaimedConnections []connectionWithRole
	for i := range allConnections {
		if !manuallyAssignedConnections[allConnections[i].ID] {
			unclaimedConnections = append(unclaimedConnections, allConnections[i])
		}
	}

	// Build topology hierarchy for the default group, excluding claimed auto_cluster_keys
	defaultGroups := d.buildTopologyHierarchy(unclaimedConnections, clusterOverrides, claimedKeys, defaultGroup)

	// Step 6b: Add manual clusters (no auto_cluster_key) from the
	// default group. These clusters were created by users and are not
	// discovered by auto-detection, so buildTopologyHierarchy does
	// not include them.
	manualDefaultClusters, err := d.getManualClustersInDefaultGroup(ctx, defaultGroup.ID)
	if err != nil {
		logger.Errorf("GetClusterTopology: failed to get manual default-group clusters: %v", err)
	} else if len(manualDefaultClusters) > 0 && len(defaultGroups) > 0 {
		defaultGroups[0].Clusters = append(defaultGroups[0].Clusters, manualDefaultClusters...)
	}

	// Append the default group (contains auto-detected clusters)
	result = append(result, defaultGroups...)

	// Step 7: Merge persisted-but-undetected cluster members into the
	// topology. These are connections that have cluster_id set in the
	// database but were not found by auto-detection in this cycle.
	autoDetectedIDs := collectAutoDetectedIDs(autoDetectedClusters)
	persistedMembers, err := d.getPersistedClusterMembers(ctx, autoDetectedIDs)
	if err != nil {
		logger.Errorf("GetClusterTopology: failed to get persisted members: %v", err)
	} else {
		parentMap := d.getPersistedMemberParents(ctx, persistedMembers)
		mergedKeys := mergePersistedMembers(result, persistedMembers, parentMap)

		// Step 7b: Create shell clusters for any persisted members
		// whose auto_cluster_key did not match an existing cluster in
		// the topology. This happens when auto-detection temporarily
		// fails to recognize a cluster (e.g. stale metrics) but the
		// connections still have cluster_id set in the database.
		d.createShellClustersForUnmerged(ctx, result, persistedMembers, mergedKeys, parentMap)
	}

	// Step 8: Populate relationships on each server in the topology
	d.populateTopologyRelationships(ctx, result)

	// Step 9: Apply the caller's visibility filter, if any. Pruning
	// happens after the full topology is assembled so that relationships
	// are populated before servers are dropped; downstream code relies on
	// relationship metadata only for servers the caller can see.
	if visibleConnectionIDs != nil {
		result = pruneTopologyByVisibility(result, visibleConnectionIDs)
	}

	return result, nil
}

// pruneTopologyByVisibility removes servers, clusters, and groups that
// the caller is not allowed to see. A cluster with no visible servers is
// dropped; a group with no visible clusters is dropped. The visibleIDs
// slice is converted to a set once so the filter walks the topology in
// linear time. Child servers are filtered recursively so a hidden parent
// with visible children is dropped along with those children; this
// matches the "cluster visibility" contract where children are only
// meaningful under an accessible parent.
func pruneTopologyByVisibility(groups []TopologyGroup, visibleIDs []int) []TopologyGroup {
	visible := make(map[int]bool, len(visibleIDs))
	for _, id := range visibleIDs {
		visible[id] = true
	}

	filtered := make([]TopologyGroup, 0, len(groups))
	for i := range groups {
		group := groups[i]
		clusters := make([]TopologyCluster, 0, len(group.Clusters))
		for j := range group.Clusters {
			cluster := group.Clusters[j]
			servers := pruneTopologyServers(cluster.Servers, visible)
			if len(servers) == 0 {
				continue
			}
			cluster.Servers = servers
			clusters = append(clusters, cluster)
		}
		if len(clusters) == 0 {
			continue
		}
		group.Clusters = clusters
		filtered = append(filtered, group)
	}
	return filtered
}

// pruneTopologyServers returns the subset of servers whose connection IDs
// appear in the visible set, recursing into Children. Relationships
// pointing at hidden peers are also dropped so TargetServerID and
// TargetServerName never leak across the visibility boundary.
func pruneTopologyServers(servers []TopologyServerInfo, visible map[int]bool) []TopologyServerInfo {
	out := make([]TopologyServerInfo, 0, len(servers))
	for i := range servers {
		s := servers[i]
		if !visible[s.ID] {
			continue
		}
		if len(s.Children) > 0 {
			s.Children = pruneTopologyServers(s.Children, visible)
		}
		if len(s.Relationships) > 0 {
			rels := make([]TopologyRelationship, 0, len(s.Relationships))
			for _, rel := range s.Relationships {
				if visible[rel.TargetServerID] {
					rels = append(rels, rel)
				}
			}
			s.Relationships = rels
		}
		s.IsExpandable = len(s.Children) > 0
		out = append(out, s)
	}
	return out
}

// populateTopologyRelationships loads relationships from the database
// and attaches them to each server in the topology groups.
func (d *Datastore) populateTopologyRelationships(ctx context.Context, groups []TopologyGroup) {
	// Query all relationships at once, keyed by cluster ID
	rows, err := d.pool.Query(ctx, `
        SELECT r.cluster_id, r.source_connection_id,
               r.target_connection_id, tc.name AS target_name,
               r.relationship_type, r.is_auto_detected
        FROM cluster_node_relationships r
        JOIN connections tc ON tc.id = r.target_connection_id
        ORDER BY r.cluster_id, r.source_connection_id
    `)
	if err != nil {
		logger.Errorf("populateTopologyRelationships: failed to query relationships: %v", err)
		return
	}
	defer rows.Close()

	// Build map: source_connection_id -> []TopologyRelationship
	relsBySource := make(map[int][]TopologyRelationship)
	for rows.Next() {
		var clusterID, sourceID, targetID int
		var targetName, relType string
		var isAuto bool
		if err := rows.Scan(&clusterID, &sourceID, &targetID, &targetName, &relType, &isAuto); err != nil {
			logger.Errorf("populateTopologyRelationships: failed to scan relationship: %v", err)
			continue
		}
		relsBySource[sourceID] = append(relsBySource[sourceID], TopologyRelationship{
			TargetServerID:   targetID,
			TargetServerName: targetName,
			RelationshipType: relType,
			IsAutoDetected:   isAuto,
		})
	}
	if err := rows.Err(); err != nil {
		logger.Errorf("populateTopologyRelationships: error iterating relationships: %v", err)
	}

	if len(relsBySource) == 0 {
		return
	}

	// Walk the topology and attach relationships to matching servers
	for i := range groups {
		for j := range groups[i].Clusters {
			attachRelationshipsToServers(groups[i].Clusters[j].Servers, relsBySource)
		}
	}
}

// attachRelationshipsToServers recursively attaches relationships to
// servers and their children.
func attachRelationshipsToServers(servers []TopologyServerInfo, relsBySource map[int][]TopologyRelationship) {
	for i := range servers {
		if rels, ok := relsBySource[servers[i].ID]; ok {
			servers[i].Relationships = rels
		}
		if len(servers[i].Children) > 0 {
			attachRelationshipsToServers(servers[i].Children, relsBySource)
		}
	}
}

// collectServerIDsRecursive recursively collects all server IDs including children
// This is used to track which connections are assigned to clusters (including hot standbys)
func collectServerIDsRecursive(servers []TopologyServerInfo, ids map[int]bool) {
	for i := range servers {
		ids[servers[i].ID] = true
		if len(servers[i].Children) > 0 {
			collectServerIDsRecursive(servers[i].Children, ids)
		}
	}
}

// RefreshClusterAssignments rebuilds auto-detected cluster topology and
// syncs the cluster_id assignments on the connections table.
func (d *Datastore) RefreshClusterAssignments(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Get default group
	defaultGroup, err := d.getDefaultGroupInternal(ctx)
	if err != nil {
		return fmt.Errorf("failed to get default group: %w", err)
	}

	// Query all connections with roles
	allConnections, err := d.getAllConnectionsWithRoles(ctx)
	if err != nil {
		return fmt.Errorf("failed to query connections: %w", err)
	}

	// Get cluster overrides
	clusterOverrides, err := d.getClusterOverridesInternal(ctx)
	if err != nil {
		clusterOverrides = make(map[string]clusterOverride)
	}

	// Build auto-detected clusters
	autoDetectedClusters := d.buildAutoDetectedClusters(allConnections, clusterOverrides)

	// Sync assignments and get mapping of auto_cluster_key -> db cluster ID
	clusterIDMap := d.syncAutoDetectedClusterAssignments(ctx, autoDetectedClusters, defaultGroup.ID)

	// Sync auto-detected relationships for each cluster
	d.syncRelationshipsFromTopology(ctx, autoDetectedClusters, clusterIDMap)

	return nil
}

// buildTopologyHierarchy builds the topology hierarchy from connections.
// The claimedKeys parameter contains auto_cluster_keys that have been moved to
// manual groups - these clusters will be excluded from the default group.
// The defaultGroup parameter provides the database-backed default group info.
func (d *Datastore) buildTopologyHierarchy(connections []connectionWithRole, clusterOverrides map[string]clusterOverride, claimedKeys map[string]bool, defaultGroup *defaultGroupInfo) []TopologyGroup {
	// Create maps for lookups
	connByID := make(map[int]*connectionWithRole)
	connByHostPort := make(map[string]*connectionWithRole)
	connByNamePort := make(map[string]*connectionWithRole) // Name-based lookup for hostname matching

	for i := range connections {
		conn := &connections[i]
		connByID[conn.ID] = conn
		// IP-based key
		ipKey := fmt.Sprintf("%s:%d", conn.Host, conn.Port)
		connByHostPort[ipKey] = conn
		// Name-based key (connection names often match hostnames)
		nameKey := fmt.Sprintf("%s:%d", conn.Name, conn.Port)
		connByNamePort[nameKey] = conn
	}

	// Track which connections are assigned to clusters
	assignedConnections := make(map[int]bool)

	// Build parent->children map for binary replication (streaming standbys)
	childrenMap := make(map[int][]int) // parent connection ID -> child connection IDs
	for i := range connections {
		conn := &connections[i]
		if conn.IsStreamingStandby && conn.UpstreamHost.Valid && conn.UpstreamPort.Valid {
			upstreamKey := fmt.Sprintf("%s:%d", conn.UpstreamHost.String, conn.UpstreamPort.Int32)
			// Try IP-based matching first
			if parent, exists := connByHostPort[upstreamKey]; exists {
				childrenMap[parent.ID] = append(childrenMap[parent.ID], conn.ID)
			} else if parent, exists := connByNamePort[upstreamKey]; exists {
				// Fall back to name-based matching (upstream_host often contains hostname)
				childrenMap[parent.ID] = append(childrenMap[parent.ID], conn.ID)
			}
		}
	}

	// Second pass: use system_identifier to associate binary standbys
	// that weren't linked by upstream info.
	alreadyChildren := make(map[int]bool)
	for _, children := range childrenMap {
		for _, childID := range children {
			alreadyChildren[childID] = true
		}
	}

	sysIDToPrimary := make(map[int64]int)
	for i := range connections {
		conn := &connections[i]
		if conn.PrimaryRole == "binary_primary" && conn.SystemIdentifier.Valid {
			sysIDToPrimary[conn.SystemIdentifier.Int64] = conn.ID
		}
	}

	for i := range connections {
		conn := &connections[i]
		if conn.PrimaryRole != "binary_standby" || alreadyChildren[conn.ID] {
			continue
		}
		if !conn.SystemIdentifier.Valid {
			continue
		}
		if primaryID, ok := sysIDToPrimary[conn.SystemIdentifier.Int64]; ok {
			childrenMap[primaryID] = append(childrenMap[primaryID], conn.ID)
		}
	}

	// Identify Spock nodes (has_spock=true AND not a standby)
	// These are the actual Spock multi-master nodes.
	// Connections with membership_source = 'manual' are pinned to a
	// manually created cluster and must be excluded from auto-detected
	// Spock grouping; otherwise they would appear in both clusters.
	spockNodes := make([]*connectionWithRole, 0)
	for i := range connections {
		conn := &connections[i]
		// A Spock node has Spock installed and is not a binary standby
		// (binary standbys with Spock are hot standbys of Spock nodes)
		if conn.HasSpock && conn.PrimaryRole != "binary_standby" && conn.MembershipSource != "manual" {
			spockNodes = append(spockNodes, conn)
		}
	}

	// Group Spock nodes into clusters based on naming patterns
	// e.g., pg17-node1, pg17-node2, pg17-node3 -> one cluster
	// pg18-spock1, pg18-spock2 -> another cluster
	spockClusters := d.groupSpockNodesByClusters(spockNodes, childrenMap, connByID, assignedConnections, clusterOverrides)

	// Build clusters list, filtering out claimed clusters
	var clusters []TopologyCluster
	for _, cluster := range spockClusters {
		// Skip clusters that have been moved to manual groups
		if cluster.AutoClusterKey != "" && claimedKeys[cluster.AutoClusterKey] {
			continue
		}
		clusters = append(clusters, cluster)
	}

	// 2. Create entries for non-Spock primaries with standbys (binary replication)
	// These now get a cluster wrapper with type "binary" so UI shows cluster header.
	// Skip connections pinned to a manual cluster (membership_source =
	// 'manual'); they must not feed auto-detected cluster creation.
	for i := range connections {
		conn := &connections[i]
		if assignedConnections[conn.ID] {
			continue
		}
		if conn.MembershipSource == "manual" {
			continue
		}
		// Check if this is a primary with standbys (and not a Spock node)
		if !conn.HasSpock && (conn.PrimaryRole == "binary_primary" && len(childrenMap[conn.ID]) > 0) {
			// Compute auto_cluster_key first to check if claimed
			autoKey := fmt.Sprintf("binary:%d", conn.ID)

			// Skip if this cluster has been moved to a manual group
			if claimedKeys[autoKey] {
				continue
			}

			// Build server with children. This is an auto binary
			// cluster, so manually pinned standbys must be excluded.
			server := d.buildServerWithChildren(conn, childrenMap, connByID, assignedConnections, false)

			clusterName := conn.Name
			clusterDescription := ""
			if override, ok := clusterOverrides[autoKey]; ok {
				clusterName = override.Name
				clusterDescription = override.Description
			}

			cluster := TopologyCluster{
				ID:             fmt.Sprintf("server-%d", conn.ID),
				Name:           clusterName,
				Description:    clusterDescription,
				ClusterType:    "binary", // UI will show cluster header for binary replication
				AutoClusterKey: autoKey,
				Servers:        []TopologyServerInfo{server},
			}
			clusters = append(clusters, cluster)
		}
	}

	// 2.5. Group logical replication publishers with their subscribers
	// Match subscribers to publishers based on publisher_host:publisher_port
	logicalClusters := d.groupLogicalReplicationByPublisher(connections, connByID, connByHostPort, connByNamePort, assignedConnections, clusterOverrides)
	// Filter out claimed logical clusters
	for _, cluster := range logicalClusters {
		if cluster.AutoClusterKey != "" && claimedKeys[cluster.AutoClusterKey] {
			continue
		}
		clusters = append(clusters, cluster)
	}

	// 3. Handle standalone servers and servers without children
	for i := range connections {
		conn := &connections[i]
		if assignedConnections[conn.ID] {
			continue
		}

		// Skip connections that have a persisted cluster_id. These
		// belong to a cluster (manual or auto-detected) and will be
		// rendered inside that cluster, not as standalone.
		if conn.ClusterID.Valid {
			continue
		}

		// Compute auto_cluster_key for standalone servers
		autoKey := fmt.Sprintf("standalone:%d", conn.ID)

		// Skip if this standalone has been moved to a manual group
		if claimedKeys[autoKey] {
			continue
		}

		// Build server (with any children if applicable). Standalone is
		// an auto-detected entry, so manually pinned children must not
		// leak into its tree.
		server := d.buildServerWithChildren(conn, childrenMap, connByID, assignedConnections, false)
		standaloneDescription := ""
		if override, ok := clusterOverrides[autoKey]; ok {
			standaloneDescription = override.Description
		}
		standaloneCluster := TopologyCluster{
			ID:             fmt.Sprintf("server-%d", conn.ID),
			Name:           conn.Name,
			Description:    standaloneDescription,
			ClusterType:    "server", // UI will not show cluster header for this type
			AutoClusterKey: autoKey,
			Servers:        []TopologyServerInfo{server},
		}
		clusters = append(clusters, standaloneCluster)
	}

	// Create the topology group using the database-backed default group
	group := TopologyGroup{
		ID:        fmt.Sprintf("group-%d", defaultGroup.ID),
		Name:      defaultGroup.Name,
		IsDefault: true,
		Clusters:  clusters,
	}

	return []TopologyGroup{group}
}

// buildManualGroupsTopology builds TopologyGroup entries for manually-created
// cluster groups, including empty groups. For clusters that have auto_cluster_key
// (moved auto-detected clusters), it uses the autoDetectedClusters map to get
// their servers instead of querying by cluster_id.
// The default group is excluded (handled separately by buildTopologyHierarchy).
func (d *Datastore) buildManualGroupsTopology(ctx context.Context, autoDetectedClusters map[string]TopologyCluster, defaultGroupID int) ([]TopologyGroup, error) {
	// Get all cluster groups except the default group
	// (manual groups that users have created)
	query := `
        SELECT id, name
        FROM cluster_groups
        WHERE NOT is_default
        ORDER BY name
    `

	rows, err := d.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query manual cluster groups: %w", err)
	}
	defer rows.Close()

	type groupInfo struct {
		id   int
		name string
	}
	var groups []groupInfo
	for rows.Next() {
		var g groupInfo
		if err := rows.Scan(&g.id, &g.name); err != nil {
			return nil, fmt.Errorf("failed to scan cluster group: %w", err)
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating cluster groups: %w", err)
	}

	result := make([]TopologyGroup, 0, len(groups))
	for _, g := range groups {
		topologyGroup := TopologyGroup{
			ID:       fmt.Sprintf("group-%d", g.id),
			Name:     g.name,
			Clusters: []TopologyCluster{},
		}

		// Get clusters in this group
		clusters, err := d.getClustersInGroupInternal(ctx, g.id)
		if err != nil {
			return nil, fmt.Errorf("failed to get clusters for group %d: %w", g.id, err)
		}

		for _, c := range clusters {
			// Check if this is a moved auto-detected cluster
			if c.AutoClusterKey.Valid && c.AutoClusterKey.String != "" {
				// This cluster was auto-detected and moved to this manual group
				// Use the auto-detected cluster data (with servers) from our map
				if autoCluster, ok := autoDetectedClusters[c.AutoClusterKey.String]; ok {
					clusterDescription := ""
					if c.Description != nil {
						clusterDescription = *c.Description
					}
					replicationType := ""
					if c.ReplicationType != nil {
						replicationType = *c.ReplicationType
					}
					topologyCluster := TopologyCluster{
						ID:              fmt.Sprintf("cluster-%d", c.ID),
						Name:            c.Name, // Use the custom name from the cluster record
						Description:     clusterDescription,
						ReplicationType: replicationType,
						ClusterType:     autoCluster.ClusterType,
						AutoClusterKey:  c.AutoClusterKey.String,
						Servers:         autoCluster.Servers,
					}
					topologyGroup.Clusters = append(topologyGroup.Clusters, topologyCluster)
				}
				// If not found in autoDetectedClusters, the cluster may have been removed
				// or topology changed - skip it
				continue
			}

			// Regular manual cluster - get servers via cluster_id
			manualDescription := ""
			if c.Description != nil {
				manualDescription = *c.Description
			}
			manualReplicationType := ""
			if c.ReplicationType != nil {
				manualReplicationType = *c.ReplicationType
			}
			topologyCluster := TopologyCluster{
				ID:              fmt.Sprintf("cluster-%d", c.ID),
				Name:            c.Name,
				Description:     manualDescription,
				ReplicationType: manualReplicationType,
				ClusterType:     "manual",
				Servers:         []TopologyServerInfo{},
			}

			// Get servers in this cluster with their role data
			servers, err := d.getServersInClusterWithRolesInternal(ctx, c.ID)
			if err != nil {
				return nil, fmt.Errorf("failed to get servers for cluster %d: %w", c.ID, err)
			}
			topologyCluster.Servers = d.buildManualClusterHierarchy(ctx, c.ID, servers)

			topologyGroup.Clusters = append(topologyGroup.Clusters, topologyCluster)
		}

		result = append(result, topologyGroup)
	}

	return result, nil
}

// getServersInClusterWithRolesInternal returns servers with their topology role info
func (d *Datastore) getServersInClusterWithRolesInternal(ctx context.Context, clusterID int) ([]TopologyServerInfo, error) {
	query := `
        WITH latest_roles AS (
            SELECT DISTINCT ON (connection_id)
                connection_id, primary_role, spock_node_name,
                COALESCE(
                    CASE
                        WHEN collected_at > NOW() - INTERVAL '6 minutes' THEN 'online'
                        WHEN collected_at > NOW() - INTERVAL '12 minutes' THEN 'warning'
                        ELSE 'offline'
                    END, 'unknown'
                ) as status
            FROM metrics.pg_node_role
            ORDER BY connection_id, collected_at DESC
        ),
        latest_server_info AS (
            SELECT DISTINCT ON (connection_id)
                connection_id, server_version
            FROM metrics.pg_server_info
            ORDER BY connection_id, collected_at DESC
        ),
        latest_os_info AS (
            SELECT DISTINCT ON (connection_id)
                connection_id, name as os_name
            FROM metrics.pg_sys_os_info
            ORDER BY connection_id, collected_at DESC
        ),
        active_alerts AS (
            SELECT connection_id, COUNT(*) as alert_count
            FROM alerts
            WHERE status = 'active'
            GROUP BY connection_id
        )
        SELECT c.id, c.name, c.description, c.host, c.port, c.owner_username, c.role,
               c.database_name, c.username,
               lsi.server_version,
               loi.os_name,
               lr.spock_node_name,
               COALESCE(NULLIF(lr.primary_role, 'standalone'), c.role, lr.primary_role, 'unknown') as primary_role,
               COALESCE(lr.status, 'unknown') as status,
               COALESCE(aa.alert_count, 0) as active_alert_count,
               c.membership_source
        FROM connections c
        LEFT JOIN latest_roles lr ON c.id = lr.connection_id
        LEFT JOIN latest_server_info lsi ON c.id = lsi.connection_id
        LEFT JOIN latest_os_info loi ON c.id = loi.connection_id
        LEFT JOIN active_alerts aa ON c.id = aa.connection_id
        WHERE c.cluster_id = $1
        ORDER BY
            CASE WHEN c.role = 'primary' THEN 0 ELSE 1 END,
            c.name
    `

	rows, err := d.pool.Query(ctx, query, clusterID)
	if err != nil {
		return nil, fmt.Errorf("failed to query servers: %w", err)
	}
	defer rows.Close()

	var servers []TopologyServerInfo
	for rows.Next() {
		var s TopologyServerInfo
		var ownerUsername, description, role, version, osName, spockNodeName sql.NullString
		if err := rows.Scan(&s.ID, &s.Name, &description, &s.Host, &s.Port, &ownerUsername, &role,
			&s.DatabaseName, &s.Username, &version, &osName, &spockNodeName,
			&s.PrimaryRole, &s.Status, &s.ActiveAlertCount, &s.MembershipSource); err != nil {
			return nil, fmt.Errorf("failed to scan server: %w", err)
		}
		if ownerUsername.Valid {
			s.OwnerUsername = ownerUsername.String
		}
		if description.Valid {
			s.Description = description.String
		}
		if role.Valid {
			s.Role = role.String
		} else {
			s.Role = d.mapPrimaryRoleToDisplayRole(s.PrimaryRole)
		}
		if version.Valid {
			s.Version = version.String
		}
		if osName.Valid {
			s.OS = osName.String
		}
		if spockNodeName.Valid {
			s.SpockNodeName = spockNodeName.String
		}
		s.IsExpandable = false
		servers = append(servers, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating servers: %w", err)
	}

	return servers, nil
}

// buildManualClusterHierarchy takes a flat list of servers for a manual
// cluster and nests children under their parents based on relationships
// stored in cluster_node_relationships. A server with a streams_from or
// subscribes_to relationship is a child of the target server.
func (d *Datastore) buildManualClusterHierarchy(ctx context.Context, clusterID int, servers []TopologyServerInfo) []TopologyServerInfo {
	if len(servers) == 0 {
		return servers
	}

	// Query parent relationships for servers in this cluster
	query := `
        SELECT source_connection_id, target_connection_id
        FROM cluster_node_relationships
        WHERE cluster_id = $1
          AND relationship_type IN ('streams_from', 'subscribes_to')
    `
	rows, err := d.pool.Query(ctx, query, clusterID)
	if err != nil {
		logger.Errorf("buildManualClusterHierarchy: failed to query relationships: %v", err)
		return servers
	}
	defer rows.Close()

	// Build parent map: child ID -> parent ID
	parentMap := make(map[int]int)
	for rows.Next() {
		var sourceID, targetID int
		if err := rows.Scan(&sourceID, &targetID); err != nil {
			logger.Errorf("buildManualClusterHierarchy: failed to scan row: %v", err)
			continue
		}
		parentMap[sourceID] = targetID
	}
	if err := rows.Err(); err != nil {
		logger.Errorf("buildManualClusterHierarchy: error iterating rows: %v", err)
	}

	if len(parentMap) == 0 {
		return servers
	}

	// Build server index by ID
	serverByID := make(map[int]*TopologyServerInfo)
	for i := range servers {
		servers[i].Children = make([]TopologyServerInfo, 0)
		serverByID[servers[i].ID] = &servers[i]
	}

	// Track which servers are children
	childIDs := make(map[int]bool)
	for childID, parentID := range parentMap {
		parent, parentExists := serverByID[parentID]
		child, childExists := serverByID[childID]
		if parentExists && childExists {
			parent.IsExpandable = true
			parent.Children = append(parent.Children, *child)
			childIDs[childID] = true
		}
	}

	// Return only top-level servers (those that are not children)
	var result []TopologyServerInfo
	for i := range servers {
		if !childIDs[servers[i].ID] {
			result = append(result, *serverByID[servers[i].ID])
		}
	}

	return result
}

// getManualClustersInDefaultGroup returns topology clusters for
// manually-created clusters (no auto_cluster_key) that belong to the
// default group. These clusters are not discovered by auto-detection
// so they must be added to the topology explicitly.
func (d *Datastore) getManualClustersInDefaultGroup(ctx context.Context, defaultGroupID int) ([]TopologyCluster, error) {
	clusters, err := d.getClustersInGroupInternal(ctx, defaultGroupID)
	if err != nil {
		return nil, fmt.Errorf("failed to get clusters in default group: %w", err)
	}

	var result []TopologyCluster
	for _, c := range clusters {
		// Skip auto-detected clusters; they are already handled by
		// buildTopologyHierarchy.
		if c.AutoClusterKey.Valid && c.AutoClusterKey.String != "" {
			continue
		}

		clusterDescription := ""
		if c.Description != nil {
			clusterDescription = *c.Description
		}
		replicationType := ""
		if c.ReplicationType != nil {
			replicationType = *c.ReplicationType
		}
		tc := TopologyCluster{
			ID:              fmt.Sprintf("cluster-%d", c.ID),
			Name:            c.Name,
			Description:     clusterDescription,
			ReplicationType: replicationType,
			ClusterType:     "manual",
			Servers:         []TopologyServerInfo{},
		}

		servers, err := d.getServersInClusterWithRolesInternal(ctx, c.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get servers for cluster %d: %w", c.ID, err)
		}
		tc.Servers = d.buildManualClusterHierarchy(ctx, c.ID, servers)

		result = append(result, tc)
	}

	return result, nil
}

// buildServerWithChildren recursively builds server tree with standbys as children.
//
// The allowManual parameter controls whether children whose
// MembershipSource is "manual" are included in the returned tree. Auto
// cluster builders (binary, spock, standalone) must pass false so that a
// child pinned to a manual cluster does not leak into the auto-detected
// tree; manual cluster builders (which legitimately include every server
// they own) must pass true. See issue #74.
func (d *Datastore) buildServerWithChildren(
	conn *connectionWithRole,
	childrenMap map[int][]int,
	connByID map[int]*connectionWithRole,
	assignedConnections map[int]bool,
	allowManual bool,
) TopologyServerInfo {
	assignedConnections[conn.ID] = true

	ownerUsername := ""
	if conn.OwnerUsername.Valid {
		ownerUsername = conn.OwnerUsername.String
	}

	version := ""
	if conn.Version.Valid {
		version = conn.Version.String
	}

	os := ""
	if conn.OS.Valid {
		os = conn.OS.String
	}

	spockNodeName := ""
	if conn.SpockNodeName.Valid {
		spockNodeName = conn.SpockNodeName.String
	}

	spockVersion := ""
	if conn.SpockVersion.Valid {
		spockVersion = conn.SpockVersion.String
	}

	description := ""
	if conn.Description.Valid {
		description = conn.Description.String
	}

	server := TopologyServerInfo{
		ID:               conn.ID,
		Name:             conn.Name,
		Description:      description,
		Host:             conn.Host,
		Port:             conn.Port,
		Status:           conn.Status,
		Role:             d.mapPrimaryRoleToDisplayRole(conn.PrimaryRole),
		PrimaryRole:      conn.PrimaryRole,
		IsExpandable:     len(childrenMap[conn.ID]) > 0,
		MembershipSource: conn.MembershipSource,
		OwnerUsername:    ownerUsername,
		Version:          version,
		OS:               os,
		SpockNodeName:    spockNodeName,
		SpockVersion:     spockVersion,
		DatabaseName:     conn.DatabaseName,
		Username:         conn.Username,
		ActiveAlertCount: conn.ActiveAlertCount,
		Children:         make([]TopologyServerInfo, 0),
	}

	if conn.ConnectionError.Valid {
		server.ConnectionError = conn.ConnectionError.String
	}

	// Recursively add children. When allowManual is false, skip any
	// child whose MembershipSource is "manual": that connection has been
	// pinned to a manually created cluster and must not leak into the
	// auto-detected tree (issue #74).
	for _, childID := range childrenMap[conn.ID] {
		child, exists := connByID[childID]
		if !exists || assignedConnections[childID] {
			continue
		}
		if !allowManual && child.MembershipSource == "manual" {
			continue
		}
		childServer := d.buildServerWithChildren(child, childrenMap, connByID, assignedConnections, allowManual)
		server.Children = append(server.Children, childServer)
	}

	return server
}

// mapPrimaryRoleToDisplayRole maps the detailed primary_role to a simpler display role
func (d *Datastore) mapPrimaryRoleToDisplayRole(primaryRole string) string {
	switch primaryRole {
	case "binary_primary":
		return "primary"
	case "binary_standby", "binary_cascading":
		return "standby"
	case "spock_node":
		return "spock"
	case "spock_standby":
		return "spock_standby"
	case "logical_publisher":
		return "publisher"
	case "logical_subscriber":
		return "subscriber"
	case "logical_bidirectional":
		return "bidirectional"
	case "standalone":
		return "standalone"
	default:
		return primaryRole
	}
}
