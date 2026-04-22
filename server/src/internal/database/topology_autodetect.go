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
	"strings"

	"github.com/pgedge/ai-workbench/pkg/logger"
)

// getAllConnectionsWithRoles queries all connections with their role data
func (d *Datastore) getAllConnectionsWithRoles(ctx context.Context) ([]connectionWithRole, error) {
	query := `
        WITH latest_connectivity AS (
            SELECT DISTINCT ON (connection_id)
                connection_id, collected_at
            FROM metrics.pg_connectivity
            WHERE collected_at > NOW() - INTERVAL '5 minutes'
            ORDER BY connection_id, collected_at DESC
        ),
        latest_roles AS (
            SELECT DISTINCT ON (connection_id)
                connection_id, primary_role, upstream_host, upstream_port,
                has_spock, spock_node_name, binary_standby_count, is_streaming_standby,
                publisher_host, publisher_port
            FROM metrics.pg_node_role
            WHERE collected_at > NOW() - INTERVAL '5 minutes'
            ORDER BY connection_id, collected_at DESC
        ),
        latest_server_info AS (
            SELECT DISTINCT ON (connection_id)
                connection_id, server_version, system_identifier
            FROM metrics.pg_server_info
            WHERE collected_at > NOW() - INTERVAL '5 minutes'
            ORDER BY connection_id, collected_at DESC
        ),
        latest_os_info AS (
            SELECT DISTINCT ON (connection_id)
                connection_id, name as os_name
            FROM metrics.pg_sys_os_info
            WHERE collected_at > NOW() - INTERVAL '5 minutes'
            ORDER BY connection_id, collected_at DESC
        ),
        latest_spock_version AS (
            SELECT DISTINCT ON (connection_id)
                connection_id, extversion as spock_version
            FROM metrics.pg_extension
            WHERE extname = 'spock'
              AND collected_at > NOW() - INTERVAL '1 hour'
            ORDER BY connection_id, collected_at DESC
        ),
        active_alerts AS (
            SELECT connection_id, COUNT(*) as alert_count
            FROM alerts
            WHERE status = 'active'
            GROUP BY connection_id
        )
        SELECT c.id, c.name, c.description, c.host, c.port, c.owner_username,
               c.database_name, c.username,
               lsi.server_version,
               lsi.system_identifier,
               loi.os_name,
               COALESCE(NULLIF(lr.primary_role, 'standalone'), c.role, lr.primary_role, 'unknown') as primary_role,
               lr.upstream_host, lr.upstream_port,
               COALESCE(lr.has_spock, false) as has_spock,
               lr.spock_node_name,
               lsv.spock_version,
               COALESCE(lr.binary_standby_count, 0) as binary_standby_count,
               COALESCE(lr.is_streaming_standby, false) as is_streaming_standby,
               lr.publisher_host, lr.publisher_port,
               c.membership_source,
               CASE
                   WHEN c.is_monitored AND c.connection_error IS NOT NULL
                   THEN 'offline'
                   WHEN c.is_monitored AND lc.connection_id IS NULL
                   THEN 'initialising'
                   WHEN lc.collected_at > NOW() - INTERVAL '60 seconds' THEN 'online'
                   WHEN lc.collected_at > NOW() - INTERVAL '150 seconds' THEN 'warning'
                   WHEN lc.collected_at IS NOT NULL THEN 'offline'
                   ELSE 'unknown'
               END as status,
               c.connection_error,
               COALESCE(aa.alert_count, 0) as active_alert_count,
               c.cluster_id
        FROM connections c
        LEFT JOIN latest_connectivity lc ON c.id = lc.connection_id
        LEFT JOIN latest_roles lr ON c.id = lr.connection_id
        LEFT JOIN latest_server_info lsi ON c.id = lsi.connection_id
        LEFT JOIN latest_os_info loi ON c.id = loi.connection_id
        LEFT JOIN latest_spock_version lsv ON c.id = lsv.connection_id
        LEFT JOIN active_alerts aa ON c.id = aa.connection_id
        ORDER BY c.name
    `

	rows, err := d.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query connections: %w", err)
	}
	defer rows.Close()

	var connections []connectionWithRole
	for rows.Next() {
		var conn connectionWithRole
		if err := rows.Scan(
			&conn.ID, &conn.Name, &conn.Description, &conn.Host, &conn.Port, &conn.OwnerUsername,
			&conn.DatabaseName, &conn.Username,
			&conn.Version,
			&conn.SystemIdentifier,
			&conn.OS,
			&conn.PrimaryRole, &conn.UpstreamHost, &conn.UpstreamPort,
			&conn.HasSpock, &conn.SpockNodeName,
			&conn.SpockVersion,
			&conn.BinaryStandbyCount, &conn.IsStreamingStandby,
			&conn.PublisherHost, &conn.PublisherPort,
			&conn.MembershipSource,
			&conn.Status,
			&conn.ConnectionError,
			&conn.ActiveAlertCount,
			&conn.ClusterID,
		); err != nil {
			return nil, fmt.Errorf("failed to scan connection: %w", err)
		}
		connections = append(connections, conn)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating connections: %w", err)
	}

	return connections, nil
}

// getClaimedAutoClusterKeys returns auto_cluster_keys that have been moved to non-default groups
// Clusters in the default group are NOT considered claimed (they should show in the default group)
func (d *Datastore) getClaimedAutoClusterKeys(ctx context.Context, defaultGroupID int) (map[string]bool, error) {
	query := `
        SELECT auto_cluster_key
        FROM clusters
        WHERE auto_cluster_key IS NOT NULL
          AND group_id IS NOT NULL
          AND group_id != $1
          AND dismissed = FALSE
    `

	rows, err := d.pool.Query(ctx, query, defaultGroupID)
	if err != nil {
		return nil, fmt.Errorf("failed to query claimed cluster keys: %w", err)
	}
	defer rows.Close()

	claimed := make(map[string]bool)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("failed to scan cluster key: %w", err)
		}
		claimed[key] = true
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating cluster keys: %w", err)
	}

	return claimed, nil
}

// buildAutoDetectedClusters builds a map of auto_cluster_key -> TopologyCluster
// This is used to get server information for auto-detected clusters that have been
// moved to manual groups
func (d *Datastore) buildAutoDetectedClusters(connections []connectionWithRole, clusterOverrides map[string]clusterOverride) map[string]TopologyCluster {
	result := make(map[string]TopologyCluster)

	// Create maps for lookups
	connByID := make(map[int]*connectionWithRole)
	connByHostPort := make(map[string]*connectionWithRole)
	connByNamePort := make(map[string]*connectionWithRole)

	for i := range connections {
		conn := &connections[i]
		connByID[conn.ID] = conn
		ipKey := fmt.Sprintf("%s:%d", conn.Host, conn.Port)
		connByHostPort[ipKey] = conn
		nameKey := fmt.Sprintf("%s:%d", conn.Name, conn.Port)
		connByNamePort[nameKey] = conn
	}

	// Track which connections are assigned to clusters
	assignedConnections := make(map[int]bool)

	// Build parent->children map for binary replication
	childrenMap := make(map[int][]int)
	for i := range connections {
		conn := &connections[i]
		if conn.IsStreamingStandby && conn.UpstreamHost.Valid && conn.UpstreamPort.Valid {
			upstreamKey := fmt.Sprintf("%s:%d", conn.UpstreamHost.String, conn.UpstreamPort.Int32)
			if parent, exists := connByHostPort[upstreamKey]; exists {
				childrenMap[parent.ID] = append(childrenMap[parent.ID], conn.ID)
			} else if parent, exists := connByNamePort[upstreamKey]; exists {
				childrenMap[parent.ID] = append(childrenMap[parent.ID], conn.ID)
			}
		}
	}

	// Second pass: use system_identifier to associate binary standbys
	// that weren't linked by upstream info. All physical replicas of a
	// PostgreSQL instance share the same system_identifier, making this
	// a reliable grouping mechanism regardless of network topology.
	alreadyChildren := make(map[int]bool)
	for _, children := range childrenMap {
		for _, childID := range children {
			alreadyChildren[childID] = true
		}
	}

	// Build a map of system_identifier -> primary connection ID
	sysIDToPrimary := make(map[int64]int)
	for i := range connections {
		conn := &connections[i]
		if conn.PrimaryRole == "binary_primary" && conn.SystemIdentifier.Valid {
			sysIDToPrimary[conn.SystemIdentifier.Int64] = conn.ID
		}
	}

	// Match unlinked binary standbys to their primary by system_identifier
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

	// Process Spock clusters
	// Exclude connections with membership_source = 'manual'; those are
	// pinned to a manually created cluster and must not be re-grouped
	// into an auto-detected cluster on the next topology refresh.
	spockNodes := make([]*connectionWithRole, 0)
	for i := range connections {
		conn := &connections[i]
		if conn.HasSpock && conn.PrimaryRole != "binary_standby" && conn.MembershipSource != "manual" {
			spockNodes = append(spockNodes, conn)
		}
	}

	spockClusters := d.groupSpockNodesByClusters(spockNodes, childrenMap, connByID, assignedConnections, clusterOverrides)
	for _, cluster := range spockClusters {
		if cluster.AutoClusterKey != "" {
			result[cluster.AutoClusterKey] = cluster
		}
	}

	// Process binary replication clusters
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
		if !conn.HasSpock && (conn.PrimaryRole == "binary_primary" && len(childrenMap[conn.ID]) > 0) {
			// Auto binary cluster: exclude manually pinned children.
			server := d.buildServerWithChildren(conn, childrenMap, connByID, assignedConnections, false)
			autoKey := fmt.Sprintf("binary:%d", conn.ID)
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
				ClusterType:    "binary",
				AutoClusterKey: autoKey,
				Servers:        []TopologyServerInfo{server},
			}
			result[autoKey] = cluster
		}
	}

	// Process logical replication clusters
	logicalClusters := d.groupLogicalReplicationByPublisher(connections, connByID, connByHostPort, connByNamePort, assignedConnections, clusterOverrides)
	for _, cluster := range logicalClusters {
		if cluster.AutoClusterKey != "" {
			result[cluster.AutoClusterKey] = cluster
		}
	}

	// Process standalone servers (not part of any cluster)
	for i := range connections {
		conn := &connections[i]
		if assignedConnections[conn.ID] {
			continue
		}
		// Build server info. Standalone is an auto-detected entry, so
		// manually pinned children must not be pulled into it.
		server := d.buildServerWithChildren(conn, childrenMap, connByID, assignedConnections, false)
		autoKey := fmt.Sprintf("standalone:%d", conn.ID)
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
			ClusterType:    "server",
			AutoClusterKey: autoKey,
			Servers:        []TopologyServerInfo{server},
		}
		result[autoKey] = cluster
	}

	return result
}

// serverIDAndRole pairs a connection ID with its detected role.
type serverIDAndRole struct {
	ID   int
	Role string
}

// collectServerIDsAndRoles walks servers and their children recursively,
// collecting each server's ID and Role.
func collectServerIDsAndRoles(servers []TopologyServerInfo) []serverIDAndRole {
	var result []serverIDAndRole
	for i := range servers {
		result = append(result, serverIDAndRole{
			ID:   servers[i].ID,
			Role: servers[i].PrimaryRole,
		})
		if len(servers[i].Children) > 0 {
			result = append(result, collectServerIDsAndRoles(servers[i].Children)...)
		}
	}
	return result
}

// syncAutoDetectedClusterAssignments ensures that auto-detected clusters
// have corresponding records in the clusters table and that each
// connection's cluster_id reflects the auto-detected topology.
// This must be called from within a write-locked context (d.mu held).
// It returns a map of auto_cluster_key -> database cluster ID for
// clusters that were successfully upserted.
func (d *Datastore) syncAutoDetectedClusterAssignments(ctx context.Context, autoDetectedClusters map[string]TopologyCluster, defaultGroupID int) map[string]int {
	clusterIDMap := make(map[string]int)

	for autoKey, cluster := range autoDetectedClusters {
		if cluster.ClusterType == "server" {
			// Standalone servers: never clear cluster_id. If a
			// connection already belongs to a cluster, the assignment
			// persists even when auto-detection no longer confirms the
			// relationship.
			continue
		}

		serversAndRoles := collectServerIDsAndRoles(cluster.Servers)

		// For real clusters (spock, binary, logical, spock_ha):
		// Upsert the cluster record. Only auto-update the name when the
		// cluster is still in the default group (the user has not moved it).
		// The UPSERT also returns the dismissed flag so we can skip
		// assigning connections to dismissed clusters.
		var clusterID int
		var dismissed bool
		err := d.pool.QueryRow(ctx, `
            INSERT INTO clusters (name, auto_cluster_key, group_id)
            VALUES ($1, $2, $3)
            ON CONFLICT (auto_cluster_key) DO UPDATE SET
                name = CASE WHEN clusters.group_id = $3 THEN EXCLUDED.name ELSE clusters.name END,
                updated_at = CURRENT_TIMESTAMP
            RETURNING id, dismissed
        `, cluster.Name, autoKey, defaultGroupID).Scan(&clusterID, &dismissed)
		if err != nil {
			logger.Errorf("syncAutoDetectedClusterAssignments: failed to upsert cluster %q: %v", autoKey, err)
			continue
		}

		// Skip connection assignment for dismissed clusters; the
		// dismissed row stays in the DB to prevent re-insertion.
		if dismissed {
			continue
		}

		clusterIDMap[autoKey] = clusterID

		// Update each connection's cluster_id and role based on
		// membership_source:
		//
		// - cluster_id IS NULL: new discovery; assign cluster_id and
		//   role regardless of membership_source.
		// - cluster_id = this cluster: re-confirm; update role to
		//   reflect current metrics (handles failover).
		// - cluster_id != this cluster AND membership_source = 'auto':
		//   reassign to the newly detected cluster.
		// - cluster_id != this cluster AND membership_source = 'manual':
		//   leave cluster_id unchanged but still update role when the
		//   connection is detected in this cluster.
		for _, sr := range serversAndRoles {
			// Assign when NULL (new discovery) or reassign when auto
			_, err := d.pool.Exec(ctx,
				`UPDATE connections
                 SET cluster_id = $1, role = $2, updated_at = CURRENT_TIMESTAMP
                 WHERE id = $3
                   AND (cluster_id IS NULL OR cluster_id = $1 OR membership_source = 'auto')`,
				clusterID, sr.Role, sr.ID,
			)
			if err != nil {
				logger.Errorf("syncAutoDetectedClusterAssignments: failed to update connection %d: %v", sr.ID, err)
			}

			// For manual connections pointing to a different cluster,
			// update only the role so failover is tracked.
			_, err = d.pool.Exec(ctx,
				`UPDATE connections
                 SET role = $1, updated_at = CURRENT_TIMESTAMP
                 WHERE id = $2
                   AND membership_source = 'manual'
                   AND cluster_id IS NOT NULL
                   AND cluster_id != $3`,
				sr.Role, sr.ID, clusterID,
			)
			if err != nil {
				logger.Errorf("syncAutoDetectedClusterAssignments: failed to update role for manual connection %d: %v", sr.ID, err)
			}
		}
	}

	return clusterIDMap
}

// getPersistedClusterMembers queries connections that have cluster_id set
// in the database but were not found by auto-detection in the current
// cycle. These persisted-but-undetected members are returned grouped by
// their cluster's auto_cluster_key so they can be merged into the
// topology response.
func (d *Datastore) getPersistedClusterMembers(ctx context.Context, autoDetectedIDs map[int]bool) (map[string][]TopologyServerInfo, error) {
	query := `
        WITH latest_connectivity AS (
            SELECT DISTINCT ON (connection_id)
                connection_id, collected_at
            FROM metrics.pg_connectivity
            WHERE collected_at > NOW() - INTERVAL '5 minutes'
            ORDER BY connection_id, collected_at DESC
        ),
        latest_roles AS (
            SELECT DISTINCT ON (connection_id)
                connection_id, primary_role, spock_node_name
            FROM metrics.pg_node_role
            WHERE collected_at > NOW() - INTERVAL '5 minutes'
            ORDER BY connection_id, collected_at DESC
        ),
        latest_server_info AS (
            SELECT DISTINCT ON (connection_id)
                connection_id, server_version
            FROM metrics.pg_server_info
            WHERE collected_at > NOW() - INTERVAL '5 minutes'
            ORDER BY connection_id, collected_at DESC
        ),
        latest_os_info AS (
            SELECT DISTINCT ON (connection_id)
                connection_id, name as os_name
            FROM metrics.pg_sys_os_info
            WHERE collected_at > NOW() - INTERVAL '5 minutes'
            ORDER BY connection_id, collected_at DESC
        ),
        active_alerts AS (
            SELECT connection_id, COUNT(*) as alert_count
            FROM alerts
            WHERE status = 'active'
            GROUP BY connection_id
        )
        SELECT c.id, c.name, c.description, c.host, c.port,
               c.owner_username, c.role,
               c.database_name, c.username,
               lsi.server_version,
               loi.os_name,
               lr.spock_node_name,
               COALESCE(NULLIF(lr.primary_role, 'standalone'), c.role, lr.primary_role, 'unknown') as primary_role,
               CASE
                   WHEN c.is_monitored AND c.connection_error IS NOT NULL
                   THEN 'offline'
                   WHEN c.is_monitored AND lc.connection_id IS NULL
                   THEN 'initialising'
                   WHEN lc.collected_at > NOW() - INTERVAL '60 seconds' THEN 'online'
                   WHEN lc.collected_at > NOW() - INTERVAL '150 seconds' THEN 'warning'
                   WHEN lc.collected_at IS NOT NULL THEN 'offline'
                   ELSE 'unknown'
               END as status,
               c.connection_error,
               COALESCE(aa.alert_count, 0) as active_alert_count,
               c.membership_source,
               cl.auto_cluster_key
        FROM connections c
        JOIN clusters cl ON cl.id = c.cluster_id
        LEFT JOIN latest_connectivity lc ON c.id = lc.connection_id
        LEFT JOIN latest_roles lr ON c.id = lr.connection_id
        LEFT JOIN latest_server_info lsi ON c.id = lsi.connection_id
        LEFT JOIN latest_os_info loi ON c.id = loi.connection_id
        LEFT JOIN active_alerts aa ON c.id = aa.connection_id
        WHERE c.cluster_id IS NOT NULL
          AND cl.auto_cluster_key IS NOT NULL
          AND cl.dismissed = FALSE
        ORDER BY c.name
    `

	rows, err := d.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query persisted cluster members: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]TopologyServerInfo)
	for rows.Next() {
		var s TopologyServerInfo
		var ownerUsername, description, role, version, osName, spockNodeName, connError sql.NullString
		var autoClusterKey string
		if err := rows.Scan(
			&s.ID, &s.Name, &description, &s.Host, &s.Port,
			&ownerUsername, &role,
			&s.DatabaseName, &s.Username,
			&version, &osName, &spockNodeName,
			&s.PrimaryRole, &s.Status,
			&connError,
			&s.ActiveAlertCount,
			&s.MembershipSource,
			&autoClusterKey,
		); err != nil {
			return nil, fmt.Errorf("failed to scan persisted member: %w", err)
		}

		// Skip connections that auto-detection already discovered
		if autoDetectedIDs[s.ID] {
			continue
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
		if connError.Valid {
			s.ConnectionError = connError.String
		}

		result[autoClusterKey] = append(result[autoClusterKey], s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating persisted members: %w", err)
	}

	return result, nil
}

// mergePersistedMembers adds persisted-but-undetected cluster members
// into the auto-detected topology. When a relationship exists that
// identifies a parent server (via streams_from or subscribes_to), the
// persisted member is nested as a child of that parent in the hierarchy.
// Members without a known parent are appended at the top level of the
// cluster.
// mergePersistedMembers adds persisted-but-undetected cluster members
// into the topology. It returns the set of auto_cluster_keys that were
// successfully merged so that the caller can identify any keys from the
// persisted map that still need shell clusters created for them.
func mergePersistedMembers(groups []TopologyGroup, persisted map[string][]TopologyServerInfo, parentMap map[int]int) map[string]bool {
	merged := make(map[string]bool)
	if len(persisted) == 0 {
		return merged
	}

	for i := range groups {
		for j := range groups[i].Clusters {
			cluster := &groups[i].Clusters[j]
			if cluster.AutoClusterKey == "" {
				continue
			}
			members, ok := persisted[cluster.AutoClusterKey]
			if !ok {
				continue
			}

			merged[cluster.AutoClusterKey] = true

			// Build a set of IDs already in this cluster
			existing := make(map[int]bool)
			collectServerIDsRecursive(cluster.Servers, existing)

			for k := range members {
				if existing[members[k].ID] {
					continue
				}

				parentID, hasParent := parentMap[members[k].ID]
				if hasParent {
					if addChildToServer(cluster.Servers, parentID, members[k]) {
						continue
					}
				}

				// No relationship or parent not found in tree;
				// fall back to top-level placement.
				cluster.Servers = append(cluster.Servers, members[k])
			}
		}
	}

	return merged
}

// createShellClustersForUnmerged creates placeholder cluster entries for
// persisted members that could not be merged because auto-detection did
// not produce a matching cluster in the topology. The function queries
// the clusters table for the unmerged auto_cluster_keys and adds shell
// clusters (with the persisted servers) to the appropriate group.
func (d *Datastore) createShellClustersForUnmerged(ctx context.Context, groups []TopologyGroup, persisted map[string][]TopologyServerInfo, mergedKeys map[string]bool, parentMap map[int]int) {
	// Collect unmerged keys
	var unmergedKeys []string
	for key := range persisted {
		if !mergedKeys[key] {
			unmergedKeys = append(unmergedKeys, key)
		}
	}
	if len(unmergedKeys) == 0 {
		return
	}

	// Query cluster metadata for unmerged keys (exclude dismissed)
	query := `
        SELECT id, name, COALESCE(description, ''), COALESCE(replication_type, ''), auto_cluster_key, group_id
        FROM clusters
        WHERE auto_cluster_key = ANY($1)
          AND dismissed = FALSE
    `
	rows, err := d.pool.Query(ctx, query, unmergedKeys)
	if err != nil {
		logger.Errorf("createShellClustersForUnmerged: failed to query clusters: %v", err)
		return
	}
	defer rows.Close()

	type shellInfo struct {
		dbID            int
		name            string
		description     string
		replicationType string
		autoClusterKey  string
		groupID         int
	}
	var shells []shellInfo
	for rows.Next() {
		var s shellInfo
		if err := rows.Scan(&s.dbID, &s.name, &s.description, &s.replicationType, &s.autoClusterKey, &s.groupID); err != nil {
			logger.Errorf("createShellClustersForUnmerged: failed to scan cluster: %v", err)
			continue
		}
		shells = append(shells, s)
	}
	if err := rows.Err(); err != nil {
		logger.Errorf("createShellClustersForUnmerged: error iterating clusters: %v", err)
	}

	// Derive cluster type from the auto_cluster_key prefix
	clusterTypeFromKey := func(key string) string {
		if idx := strings.Index(key, ":"); idx >= 0 {
			return key[:idx]
		}
		return "server"
	}

	// Build shell clusters and add to the matching group
	for _, s := range shells {
		members := persisted[s.autoClusterKey]
		if len(members) == 0 {
			continue
		}

		// Nest children using the parent map
		servers := nestPersistedMembers(members, parentMap)

		tc := TopologyCluster{
			ID:              fmt.Sprintf("cluster-%d", s.dbID),
			Name:            s.name,
			Description:     s.description,
			ReplicationType: s.replicationType,
			ClusterType:     clusterTypeFromKey(s.autoClusterKey),
			AutoClusterKey:  s.autoClusterKey,
			Servers:         servers,
		}

		// Find the matching group and append the shell cluster
		groupKey := fmt.Sprintf("group-%d", s.groupID)
		added := false
		for i := range groups {
			if groups[i].ID == groupKey {
				groups[i].Clusters = append(groups[i].Clusters, tc)
				added = true
				break
			}
		}
		if !added {
			logger.Errorf("createShellClustersForUnmerged: group %q not found for cluster %q", groupKey, s.autoClusterKey)
		}
	}
}

// nestPersistedMembers arranges persisted members into a parent-child
// hierarchy using the parentMap (child ID -> parent ID). Members without
// a parent or whose parent is not in the list stay at the top level.
func nestPersistedMembers(members []TopologyServerInfo, parentMap map[int]int) []TopologyServerInfo {
	if len(parentMap) == 0 {
		return members
	}

	// Build index by ID
	byID := make(map[int]*TopologyServerInfo)
	for i := range members {
		members[i].Children = make([]TopologyServerInfo, 0)
		byID[members[i].ID] = &members[i]
	}

	childIDs := make(map[int]bool)
	for i := range members {
		parentID, ok := parentMap[members[i].ID]
		if !ok {
			continue
		}
		parent, parentInSet := byID[parentID]
		if !parentInSet {
			continue
		}
		parent.IsExpandable = true
		parent.Children = append(parent.Children, *byID[members[i].ID])
		childIDs[members[i].ID] = true
	}

	var result []TopologyServerInfo
	for i := range members {
		if !childIDs[members[i].ID] {
			result = append(result, *byID[members[i].ID])
		}
	}
	return result
}

// addChildToServer searches the server tree for a server with the given
// parentID and appends the child to its Children slice. Returns true if
// the parent was found and the child was added.
func addChildToServer(servers []TopologyServerInfo, parentID int, child TopologyServerInfo) bool {
	for i := range servers {
		if servers[i].ID == parentID {
			servers[i].Children = append(servers[i].Children, child)
			servers[i].IsExpandable = true
			return true
		}
		if len(servers[i].Children) > 0 {
			if addChildToServer(servers[i].Children, parentID, child) {
				return true
			}
		}
	}
	return false
}

// getPersistedMemberParents queries cluster_node_relationships to build
// a map from persisted member connection ID to its parent connection ID.
// A member is a child when it has a streams_from or subscribes_to
// relationship (source = child, target = parent).
func (d *Datastore) getPersistedMemberParents(ctx context.Context, persisted map[string][]TopologyServerInfo) map[int]int {
	// Collect all persisted member IDs
	var ids []int
	for _, members := range persisted {
		for i := range members {
			ids = append(ids, members[i].ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}

	query := `
        SELECT source_connection_id, target_connection_id
        FROM cluster_node_relationships
        WHERE source_connection_id = ANY($1)
          AND relationship_type IN ('streams_from', 'subscribes_to')
    `

	rows, err := d.pool.Query(ctx, query, ids)
	if err != nil {
		logger.Errorf("getPersistedMemberParents: failed to query relationships: %v", err)
		return nil
	}
	defer rows.Close()

	parentMap := make(map[int]int)
	for rows.Next() {
		var sourceID, targetID int
		if err := rows.Scan(&sourceID, &targetID); err != nil {
			logger.Errorf("getPersistedMemberParents: failed to scan row: %v", err)
			continue
		}
		parentMap[sourceID] = targetID
	}

	if err := rows.Err(); err != nil {
		logger.Errorf("getPersistedMemberParents: error iterating rows: %v", err)
	}

	return parentMap
}

// collectAutoDetectedIDs gathers connection IDs that were placed into
// real clusters (spock, binary, logical, etc.) by auto-detection.
// Standalone entries (ClusterType "server") are excluded because they
// represent unassigned connections, not confirmed cluster membership.
// Without this exclusion a server that is down (and therefore detected
// as standalone) would be suppressed from the persisted-member merge
// step, causing it to vanish from its manually-assigned cluster.
func collectAutoDetectedIDs(autoDetectedClusters map[string]TopologyCluster) map[int]bool {
	ids := make(map[int]bool)
	for _, cluster := range autoDetectedClusters {
		if cluster.ClusterType == "server" {
			continue
		}
		collectServerIDsRecursive(cluster.Servers, ids)
	}
	return ids
}

// syncRelationshipsFromTopology extracts relationship edges from the
// auto-detected cluster topology and syncs them to the database.
func (d *Datastore) syncRelationshipsFromTopology(ctx context.Context, autoDetectedClusters map[string]TopologyCluster, clusterIDMap map[string]int) {
	for autoKey, cluster := range autoDetectedClusters {
		dbClusterID, ok := clusterIDMap[autoKey]
		if !ok {
			// Standalone or failed upsert; skip
			continue
		}

		var detected []AutoRelationshipInput

		switch cluster.ClusterType {
		case "binary":
			// Binary replication: children stream from their parent
			// Servers[0] is the primary; its Children are standbys
			detected = extractBinaryRelationships(cluster.Servers)

		case "spock", "spock_ha":
			// Spock bidirectional: every pair gets two replicates_with rows
			// Spock nodes with binary children also get streams_from edges
			detected = extractSpockRelationships(cluster.Servers)

		case "logical":
			// Logical replication: Servers[0] is the publisher with
			// subscribers as Children
			detected = extractLogicalRelationships(cluster.Servers)
		}

		if len(detected) > 0 {
			if err := d.SyncAutoDetectedRelationships(ctx, dbClusterID, detected); err != nil {
				logger.Errorf("syncRelationshipsFromTopology: failed to sync relationships for cluster %q (id=%d): %v", autoKey, dbClusterID, err)
			}
		}
	}
}

// extractBinaryRelationships extracts streams_from relationships from
// binary replication clusters. Each child (standby) streams_from its
// parent (primary).
func extractBinaryRelationships(servers []TopologyServerInfo) []AutoRelationshipInput {
	var result []AutoRelationshipInput
	for i := range servers {
		result = append(result, extractBinaryChildRelationships(servers[i])...)
	}
	return result
}

// extractBinaryChildRelationships recursively extracts streams_from
// relationships for a server and its children.
func extractBinaryChildRelationships(server TopologyServerInfo) []AutoRelationshipInput {
	var result []AutoRelationshipInput
	for i := range server.Children {
		child := &server.Children[i]
		// Child (standby) streams_from parent (primary)
		result = append(result, AutoRelationshipInput{
			SourceConnectionID: child.ID,
			TargetConnectionID: server.ID,
			RelationshipType:   "streams_from",
		})
		// Recurse for cascading standbys
		result = append(result, extractBinaryChildRelationships(*child)...)
	}
	return result
}

// extractSpockRelationships extracts replicates_with relationships for
// all Spock node pairs and streams_from relationships for any binary
// standbys attached to Spock nodes.
func extractSpockRelationships(servers []TopologyServerInfo) []AutoRelationshipInput {
	var result []AutoRelationshipInput

	// Collect all top-level Spock node IDs (not their children)
	for i := 0; i < len(servers); i++ {
		for j := i + 1; j < len(servers); j++ {
			// Bidirectional: two rows per pair
			result = append(result, AutoRelationshipInput{
				SourceConnectionID: servers[i].ID,
				TargetConnectionID: servers[j].ID,
				RelationshipType:   "replicates_with",
			})
			result = append(result, AutoRelationshipInput{
				SourceConnectionID: servers[j].ID,
				TargetConnectionID: servers[i].ID,
				RelationshipType:   "replicates_with",
			})
		}
		// Binary standbys of Spock nodes get streams_from edges
		result = append(result, extractBinaryChildRelationships(servers[i])...)
	}

	return result
}

// extractLogicalRelationships extracts subscribes_to relationships from
// logical replication clusters. Each subscriber (child) subscribes_to the
// publisher (parent).
func extractLogicalRelationships(servers []TopologyServerInfo) []AutoRelationshipInput {
	var result []AutoRelationshipInput
	for i := range servers {
		publisher := &servers[i]
		for j := range publisher.Children {
			subscriber := &publisher.Children[j]
			// Subscriber subscribes_to publisher
			result = append(result, AutoRelationshipInput{
				SourceConnectionID: subscriber.ID,
				TargetConnectionID: publisher.ID,
				RelationshipType:   "subscribes_to",
			})
		}
	}
	return result
}

// groupSpockNodesByClusters groups Spock nodes into clusters based on naming patterns
func (d *Datastore) groupSpockNodesByClusters(
	spockNodes []*connectionWithRole,
	childrenMap map[int][]int,
	connByID map[int]*connectionWithRole,
	assignedConnections map[int]bool,
	overrides map[string]clusterOverride,
) []TopologyCluster {
	if len(spockNodes) == 0 {
		return nil
	}

	// Group nodes by naming prefix (e.g., "pg17-" or "pg18-")
	// This is a heuristic - ideally we'd have explicit cluster metadata
	nodesByPrefix := make(map[string][]*connectionWithRole)
	for _, node := range spockNodes {
		prefix := d.extractClusterPrefix(node.Name)
		nodesByPrefix[prefix] = append(nodesByPrefix[prefix], node)
	}

	var clusters []TopologyCluster
	for prefix, nodes := range nodesByPrefix {
		// Determine cluster type based on whether nodes have hot standbys
		hasHotStandbys := false
		for _, node := range nodes {
			if len(childrenMap[node.ID]) > 0 {
				hasHotStandbys = true
				break
			}
		}

		clusterType := "spock"
		clusterName := fmt.Sprintf("%s Spock", prefix)
		if hasHotStandbys {
			clusterType = "spock_ha"
			clusterName = fmt.Sprintf("%s Spock HA", prefix)
		}

		// Compute auto_cluster_key and check for custom name/description
		autoKey := fmt.Sprintf("spock:%s", prefix)
		clusterDescription := ""
		if override, ok := overrides[autoKey]; ok {
			clusterName = override.Name
			clusterDescription = override.Description
		}

		cluster := TopologyCluster{
			ID:             fmt.Sprintf("cluster-spock-%s", prefix),
			Name:           clusterName,
			Description:    clusterDescription,
			ClusterType:    clusterType,
			AutoClusterKey: autoKey,
			Servers:        make([]TopologyServerInfo, 0, len(nodes)),
		}

		for _, node := range nodes {
			// Use buildServerWithChildren to include hot standbys as
			// children. This is an auto Spock (or Spock HA) cluster, so
			// manually pinned hot standbys must be excluded.
			server := d.buildServerWithChildren(node, childrenMap, connByID, assignedConnections, false)
			cluster.Servers = append(cluster.Servers, server)
		}

		clusters = append(clusters, cluster)
	}

	return clusters
}

// groupLogicalReplicationByPublisher groups logical subscribers with their publishers
// by matching subscriber's publisher_host:publisher_port to connection host:port
func (d *Datastore) groupLogicalReplicationByPublisher(
	connections []connectionWithRole,
	connByID map[int]*connectionWithRole,
	connByHostPort map[string]*connectionWithRole,
	connByNamePort map[string]*connectionWithRole,
	assignedConnections map[int]bool,
	overrides map[string]clusterOverride,
) []TopologyCluster {
	// Build publisher->subscribers map by matching publisher_host:port to connections.
	// Subscribers or publishers with membership_source = 'manual' are
	// pinned to a manually created cluster and must not be used to build
	// an auto-detected logical-replication cluster.
	subscribersByPublisher := make(map[int][]*connectionWithRole) // publisher connection ID -> subscribers

	for i := range connections {
		conn := &connections[i]
		if assignedConnections[conn.ID] {
			continue
		}
		if conn.MembershipSource == "manual" {
			continue
		}

		// Only process logical subscribers that have publisher connection info
		if conn.PrimaryRole != "logical_subscriber" {
			continue
		}
		if !conn.PublisherHost.Valid || !conn.PublisherPort.Valid {
			continue
		}

		// Try to match publisher_host:port to a known connection
		pubKey := fmt.Sprintf("%s:%d", conn.PublisherHost.String, conn.PublisherPort.Int32)

		var publisher *connectionWithRole
		// Try IP-based matching first
		if pub, exists := connByHostPort[pubKey]; exists {
			publisher = pub
		} else if pub, exists := connByNamePort[pubKey]; exists {
			// Fall back to name-based matching (publisher_host often contains hostname)
			publisher = pub
		}

		if publisher != nil && !assignedConnections[publisher.ID] && publisher.MembershipSource != "manual" {
			subscribersByPublisher[publisher.ID] = append(subscribersByPublisher[publisher.ID], conn)
		}
	}

	var clusters []TopologyCluster

	// Create clusters for publishers that have subscribers
	for pubID, subscribers := range subscribersByPublisher {
		publisher := connByID[pubID]
		if publisher == nil || len(subscribers) == 0 {
			continue
		}

		// Mark publisher as assigned
		assignedConnections[publisher.ID] = true

		// Build publisher server with subscribers as children
		pubOwner := ""
		if publisher.OwnerUsername.Valid {
			pubOwner = publisher.OwnerUsername.String
		}
		pubVersion := ""
		if publisher.Version.Valid {
			pubVersion = publisher.Version.String
		}
		pubOS := ""
		if publisher.OS.Valid {
			pubOS = publisher.OS.String
		}
		pubSpockNodeName := ""
		if publisher.SpockNodeName.Valid {
			pubSpockNodeName = publisher.SpockNodeName.String
		}
		pubSpockVersion := ""
		if publisher.SpockVersion.Valid {
			pubSpockVersion = publisher.SpockVersion.String
		}
		pubDescription := ""
		if publisher.Description.Valid {
			pubDescription = publisher.Description.String
		}
		pubServer := TopologyServerInfo{
			ID:               publisher.ID,
			Name:             publisher.Name,
			Description:      pubDescription,
			Host:             publisher.Host,
			Port:             publisher.Port,
			Status:           publisher.Status,
			Role:             d.mapPrimaryRoleToDisplayRole(publisher.PrimaryRole),
			PrimaryRole:      publisher.PrimaryRole,
			IsExpandable:     true,
			MembershipSource: publisher.MembershipSource,
			OwnerUsername:    pubOwner,
			Version:          pubVersion,
			OS:               pubOS,
			SpockNodeName:    pubSpockNodeName,
			SpockVersion:     pubSpockVersion,
			DatabaseName:     publisher.DatabaseName,
			Username:         publisher.Username,
			ActiveAlertCount: publisher.ActiveAlertCount,
			Children:         make([]TopologyServerInfo, 0, len(subscribers)),
		}

		// Add subscribers as children
		for _, sub := range subscribers {
			assignedConnections[sub.ID] = true
			subOwner := ""
			if sub.OwnerUsername.Valid {
				subOwner = sub.OwnerUsername.String
			}
			subVersion := ""
			if sub.Version.Valid {
				subVersion = sub.Version.String
			}
			subOS := ""
			if sub.OS.Valid {
				subOS = sub.OS.String
			}
			subSpockNodeName := ""
			if sub.SpockNodeName.Valid {
				subSpockNodeName = sub.SpockNodeName.String
			}
			subSpockVersion := ""
			if sub.SpockVersion.Valid {
				subSpockVersion = sub.SpockVersion.String
			}
			subDescription := ""
			if sub.Description.Valid {
				subDescription = sub.Description.String
			}
			subServer := TopologyServerInfo{
				ID:               sub.ID,
				Name:             sub.Name,
				Description:      subDescription,
				Host:             sub.Host,
				Port:             sub.Port,
				Status:           sub.Status,
				Role:             d.mapPrimaryRoleToDisplayRole(sub.PrimaryRole),
				PrimaryRole:      sub.PrimaryRole,
				IsExpandable:     false,
				MembershipSource: sub.MembershipSource,
				OwnerUsername:    subOwner,
				Version:          subVersion,
				OS:               subOS,
				SpockNodeName:    subSpockNodeName,
				SpockVersion:     subSpockVersion,
				DatabaseName:     sub.DatabaseName,
				Username:         sub.Username,
				ActiveAlertCount: sub.ActiveAlertCount,
				Children:         nil,
			}
			pubServer.Children = append(pubServer.Children, subServer)
		}

		// Compute auto_cluster_key and check for custom name/description
		autoKey := fmt.Sprintf("logical:%d", publisher.ID)
		clusterName := publisher.Name
		clusterDescription := ""
		if override, ok := overrides[autoKey]; ok {
			clusterName = override.Name
			clusterDescription = override.Description
		}

		// Use cluster_type "logical" so UI shows cluster header for logical replication
		cluster := TopologyCluster{
			ID:             fmt.Sprintf("server-%d", publisher.ID),
			Name:           clusterName,
			Description:    clusterDescription,
			ClusterType:    "logical",
			AutoClusterKey: autoKey,
			Servers:        []TopologyServerInfo{pubServer},
		}
		clusters = append(clusters, cluster)
	}

	return clusters
}

// extractClusterPrefix extracts a cluster prefix from a connection name
// e.g., "pg17-node1" -> "pg17", "pg18-spock1" -> "pg18"
func (d *Datastore) extractClusterPrefix(name string) string {
	// Look for common patterns: pgXX-*, name-XX, etc.
	for i, ch := range name {
		if ch == '-' && i > 0 {
			return name[:i]
		}
	}
	// No separator found, use the whole name
	return name
}
