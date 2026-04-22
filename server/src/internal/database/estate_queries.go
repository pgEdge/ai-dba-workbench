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
	"time"

	"github.com/pgedge/ai-workbench/pkg/logger"
)

// EstateServerSummary holds summary information for a single server
// in the estate snapshot
type EstateServerSummary struct {
	ID               int    `json:"id"`
	Name             string `json:"name"`
	Status           string `json:"status"`
	Role             string `json:"role"`
	ActiveAlertCount int    `json:"active_alert_count"`
}

// EstateAlertSummary holds a summary of a single active alert
type EstateAlertSummary struct {
	Title      string `json:"title"`
	ServerName string `json:"server_name"`
	Severity   string `json:"severity"`
}

// EstateBlackoutSummary holds a summary of a blackout period
type EstateBlackoutSummary struct {
	Scope     string    `json:"scope"`
	Reason    string    `json:"reason"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
}

// EstateEventSummary holds a summary of a recent timeline event
type EstateEventSummary struct {
	EventType  string    `json:"event_type"`
	ServerName string    `json:"server_name"`
	OccurredAt time.Time `json:"occurred_at"`
	Severity   string    `json:"severity"`
	Title      string    `json:"title"`
	Summary    string    `json:"summary"`
}

// EstateSnapshot contains a point-in-time summary of the entire
// monitored estate for the AI overview
type EstateSnapshot struct {
	Timestamp         time.Time               `json:"timestamp"`
	ServerTotal       int                     `json:"server_total"`
	ServerOnline      int                     `json:"server_online"`
	ServerOffline     int                     `json:"server_offline"`
	ServerWarning     int                     `json:"server_warning"`
	Servers           []EstateServerSummary   `json:"servers"`
	AlertTotal        int                     `json:"alert_total"`
	AlertCritical     int                     `json:"alert_critical"`
	AlertWarning      int                     `json:"alert_warning"`
	AlertInfo         int                     `json:"alert_info"`
	TopAlerts         []EstateAlertSummary    `json:"top_alerts"`
	ActiveBlackouts   []EstateBlackoutSummary `json:"active_blackouts"`
	UpcomingBlackouts []EstateBlackoutSummary `json:"upcoming_blackouts"`
	RecentEvents      []EstateEventSummary    `json:"recent_events"`
}

// ConnectionContext holds comprehensive system context for a monitored connection
type ConnectionContext struct {
	ConnectionID int                `json:"connection_id"`
	ServerName   string             `json:"server_name"`
	PostgreSQL   *PostgreSQLContext `json:"postgresql,omitempty"`
	System       *SystemContext     `json:"system,omitempty"`
}

// PostgreSQLContext holds PostgreSQL server information
type PostgreSQLContext struct {
	Version             string            `json:"version,omitempty"`
	VersionNum          int               `json:"version_num,omitempty"`
	MaxConnections      int               `json:"max_connections,omitempty"`
	DataDirectory       string            `json:"data_directory,omitempty"`
	InstalledExtensions []string          `json:"installed_extensions,omitempty"`
	Settings            map[string]string `json:"settings,omitempty"`
}

// SystemContext holds operating system and hardware information
type SystemContext struct {
	OSName       string         `json:"os_name,omitempty"`
	OSVersion    string         `json:"os_version,omitempty"`
	Architecture string         `json:"architecture,omitempty"`
	Hostname     string         `json:"hostname,omitempty"`
	CPU          *CPUContext    `json:"cpu,omitempty"`
	Memory       *MemoryContext `json:"memory,omitempty"`
	Disks        []DiskContext  `json:"disks,omitempty"`
}

// CPUContext holds CPU information
type CPUContext struct {
	Model             string `json:"model,omitempty"`
	Cores             int    `json:"cores,omitempty"`
	LogicalProcessors int    `json:"logical_processors,omitempty"`
}

// MemoryContext holds memory information
type MemoryContext struct {
	TotalBytes int64 `json:"total_bytes,omitempty"`
	FreeBytes  int64 `json:"free_bytes,omitempty"`
}

// DiskContext holds disk information for a single mount point
type DiskContext struct {
	MountPoint     string `json:"mount_point"`
	FilesystemType string `json:"filesystem_type,omitempty"`
	TotalBytes     int64  `json:"total_bytes,omitempty"`
	UsedBytes      int64  `json:"used_bytes,omitempty"`
	FreeBytes      int64  `json:"free_bytes,omitempty"`
}

// GetEstateSnapshot gathers all data needed for an AI overview of the
// estate. It returns a point-in-time snapshot of server status, alerts,
// blackouts, and recent events. If individual sub-queries fail, partial
// data is returned with what succeeded.
func (d *Datastore) GetEstateSnapshot(ctx context.Context) (*EstateSnapshot, error) {
	snapshotCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	snapshot := &EstateSnapshot{
		Timestamp:         time.Now().UTC(),
		Servers:           []EstateServerSummary{},
		TopAlerts:         []EstateAlertSummary{},
		ActiveBlackouts:   []EstateBlackoutSummary{},
		UpcomingBlackouts: []EstateBlackoutSummary{},
		RecentEvents:      []EstateEventSummary{},
	}

	// Gather server topology and compute status counts
	d.gatherEstateServerData(snapshotCtx, snapshot)

	// Gather alert summary and top alerts
	d.gatherEstateAlertData(snapshotCtx, snapshot)

	// Gather active and upcoming blackout periods
	d.gatherEstateBlackoutData(snapshotCtx, snapshot)

	// Gather recent events from the last 24 hours
	d.gatherEstateRecentEvents(snapshotCtx, snapshot)

	return snapshot, nil
}

// gatherEstateServerData populates server status counts and per-server
// details by walking the cluster topology hierarchy. Servers are
// classified as offline (status is offline), warning (has active alerts
// but not offline), or online (no alerts, not offline).
func (d *Datastore) gatherEstateServerData(ctx context.Context, snapshot *EstateSnapshot) {
	// Sync cluster_id assignments before reading topology
	if err := d.RefreshClusterAssignments(ctx); err != nil {
		logger.Infof("gatherEstateServerData: failed to refresh cluster assignments: %v", err)
	}

	groups, err := d.GetClusterTopology(ctx, nil)
	if err != nil {
		logger.Errorf("GetEstateSnapshot: failed to get cluster topology: %v", err)
		return
	}

	var servers []EstateServerSummary
	for _, group := range groups {
		for _, cluster := range group.Clusters {
			flattenTopologyServers(cluster.Servers, &servers)
		}
	}

	// Map roles to display-friendly names and compute status counts
	for i := range servers {
		servers[i].Role = d.mapPrimaryRoleToDisplayRole(servers[i].Role)

		switch {
		case servers[i].Status == "offline":
			snapshot.ServerOffline++
		case servers[i].ActiveAlertCount > 0:
			snapshot.ServerWarning++
		default:
			snapshot.ServerOnline++
		}
	}

	snapshot.Servers = servers
	snapshot.ServerTotal = len(servers)
}

// flattenTopologyServers recursively extracts server summaries from the
// nested topology hierarchy into a flat slice. Children (such as hot
// standbys) are included alongside their parents.
func flattenTopologyServers(servers []TopologyServerInfo, result *[]EstateServerSummary) {
	for i := range servers {
		s := &servers[i]
		*result = append(*result, EstateServerSummary{
			ID:               s.ID,
			Name:             s.Name,
			Status:           s.Status,
			Role:             s.PrimaryRole,
			ActiveAlertCount: s.ActiveAlertCount,
		})
		if len(s.Children) > 0 {
			flattenTopologyServers(s.Children, result)
		}
	}
}

// gatherEstateAlertData populates alert severity counts and the top
// active alerts list. Active alerts are retrieved in order of most
// recent trigger time. Severity counts are computed from the returned
// set; if total active alerts exceed the query limit of 500, the
// breakdown is approximate while AlertTotal remains accurate.
func (d *Datastore) gatherEstateAlertData(ctx context.Context, snapshot *EstateSnapshot) {
	activeStatus := "active"
	result, err := d.GetAlerts(ctx, AlertListFilter{
		Status: &activeStatus,
		Limit:  500,
	})
	if err != nil {
		logger.Errorf("GetEstateSnapshot: failed to get alerts: %v", err)
		return
	}

	snapshot.AlertTotal = int(result.Total)

	for i := range result.Alerts {
		alert := &result.Alerts[i]
		switch alert.Severity {
		case "critical":
			snapshot.AlertCritical++
		case "warning":
			snapshot.AlertWarning++
		case "info":
			snapshot.AlertInfo++
		}

		if i < 10 {
			snapshot.TopAlerts = append(snapshot.TopAlerts, EstateAlertSummary{
				Title:      alert.Title,
				ServerName: alert.ServerName,
				Severity:   alert.Severity,
			})
		}
	}
}

// gatherEstateBlackoutData populates active blackouts and upcoming
// blackouts. Upcoming blackouts are one-time blackouts whose start
// time falls within the next 24 hours. Scheduled blackout occurrences
// are not evaluated because that would require a cron expression
// parser.
func (d *Datastore) gatherEstateBlackoutData(ctx context.Context, snapshot *EstateSnapshot) {
	// Retrieve recent blackouts ordered by start_time DESC; future
	// blackouts appear first, followed by currently active ones
	result, err := d.ListBlackouts(ctx, BlackoutFilter{
		Limit: 100,
	})
	if err != nil {
		logger.Errorf("GetEstateSnapshot: failed to get blackouts: %v", err)
		return
	}

	now := time.Now().UTC()
	cutoff := now.Add(24 * time.Hour)

	for i := range result.Blackouts {
		b := &result.Blackouts[i]
		summary := EstateBlackoutSummary{
			Scope:     b.Scope,
			Reason:    b.Reason,
			StartTime: b.StartTime,
			EndTime:   b.EndTime,
		}

		if b.IsActive {
			snapshot.ActiveBlackouts = append(snapshot.ActiveBlackouts, summary)
		} else if b.StartTime.After(now) && b.StartTime.Before(cutoff) {
			snapshot.UpcomingBlackouts = append(snapshot.UpcomingBlackouts, summary)
		}
	}
}

// gatherEstateRecentEvents populates recent events from the last 24
// hours, focusing on restarts, configuration changes, and blackout
// transitions. Events are returned in reverse chronological order
// with a maximum of 20 entries.
func (d *Datastore) gatherEstateRecentEvents(ctx context.Context, snapshot *EstateSnapshot) {
	now := time.Now().UTC()
	dayAgo := now.Add(-24 * time.Hour)

	result, err := d.GetTimelineEvents(ctx, TimelineFilter{
		StartTime: dayAgo,
		EndTime:   now,
		EventTypes: []string{
			EventTypeRestart,
			EventTypeConfigChange,
			EventTypeBlackoutStarted,
			EventTypeBlackoutEnded,
		},
		Limit: 20,
	})
	if err != nil {
		logger.Errorf("GetEstateSnapshot: failed to get timeline events: %v", err)
		return
	}

	for i := range result.Events {
		event := &result.Events[i]
		snapshot.RecentEvents = append(snapshot.RecentEvents, EstateEventSummary{
			EventType:  event.EventType,
			ServerName: event.ServerName,
			OccurredAt: event.OccurredAt,
			Severity:   event.Severity,
			Title:      event.Title,
			Summary:    event.Summary,
		})
	}
}

// GetServerSnapshot returns an estate snapshot filtered to a single
// server (connection). It returns the same EstateSnapshot structure but
// populated only with data for the given connection ID.
func (d *Datastore) GetServerSnapshot(ctx context.Context, serverID int) (*EstateSnapshot, string, error) {
	// Verify the connection exists and get its name.
	conn, err := d.GetConnection(ctx, serverID)
	if err != nil {
		return nil, "", fmt.Errorf("server not found: %w", err)
	}

	snapshot := d.buildScopedSnapshot(ctx, []int{serverID})
	return snapshot, conn.Name, nil
}

// GetClusterSnapshot returns an estate snapshot filtered to all servers
// in a given cluster. It returns the snapshot, the cluster name, and
// any error encountered.
func (d *Datastore) GetClusterSnapshot(ctx context.Context, clusterID int) (*EstateSnapshot, string, error) {
	cluster, err := d.GetCluster(ctx, clusterID)
	if err != nil {
		return nil, "", fmt.Errorf("cluster not found: %w", err)
	}

	connectionIDs, err := d.getConnectionIDsForCluster(ctx, clusterID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get connections for cluster: %w", err)
	}

	snapshot := d.buildScopedSnapshot(ctx, connectionIDs)
	return snapshot, cluster.Name, nil
}

// GetGroupSnapshot returns an estate snapshot filtered to all servers
// in all clusters belonging to a given group. It returns the snapshot,
// the group name, and any error encountered.
func (d *Datastore) GetGroupSnapshot(ctx context.Context, groupID int) (*EstateSnapshot, string, error) {
	group, err := d.GetClusterGroup(ctx, groupID)
	if err != nil {
		return nil, "", fmt.Errorf("group not found: %w", err)
	}

	connectionIDs, err := d.getConnectionIDsForGroup(ctx, groupID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get connections for group: %w", err)
	}

	snapshot := d.buildScopedSnapshot(ctx, connectionIDs)
	return snapshot, group.Name, nil
}

// GetConnectionsSnapshot returns an estate snapshot filtered to the
// specified connection IDs. It is a public wrapper around
// buildScopedSnapshot for callers that already have a list of
// connection IDs and do not need scope-name resolution.
func (d *Datastore) GetConnectionsSnapshot(ctx context.Context, connectionIDs []int) *EstateSnapshot {
	return d.buildScopedSnapshot(ctx, connectionIDs)
}

// buildScopedSnapshot creates an EstateSnapshot containing only data
// for the specified connection IDs. It reuses the same gathering
// helpers as the estate-wide snapshot but filters by connection.
func (d *Datastore) buildScopedSnapshot(ctx context.Context, connectionIDs []int) *EstateSnapshot {
	snapshotCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	snapshot := &EstateSnapshot{
		Timestamp:         time.Now().UTC(),
		Servers:           []EstateServerSummary{},
		TopAlerts:         []EstateAlertSummary{},
		ActiveBlackouts:   []EstateBlackoutSummary{},
		UpcomingBlackouts: []EstateBlackoutSummary{},
		RecentEvents:      []EstateEventSummary{},
	}

	if len(connectionIDs) == 0 {
		return snapshot
	}

	// Gather server data filtered to the given connections.
	d.gatherScopedServerData(snapshotCtx, snapshot, connectionIDs)

	// Gather alert data filtered to the given connections.
	d.gatherScopedAlertData(snapshotCtx, snapshot, connectionIDs)

	// Gather blackout data (estate-wide; filtered post-query is not
	// possible because blackouts reference scopes, not connections
	// directly). Include all blackouts so the LLM can note relevant
	// maintenance windows.
	d.gatherEstateBlackoutData(snapshotCtx, snapshot)

	// Gather recent events filtered to the given connections.
	d.gatherScopedRecentEvents(snapshotCtx, snapshot, connectionIDs)

	return snapshot
}

// getConnectionIDsForCluster returns all connection IDs that belong
// to the given cluster.
func (d *Datastore) getConnectionIDsForCluster(ctx context.Context, clusterID int) ([]int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `SELECT id FROM connections WHERE cluster_id = $1 ORDER BY id`
	rows, err := d.pool.Query(ctx, query, clusterID)
	if err != nil {
		return nil, fmt.Errorf("failed to query connections for cluster: %w", err)
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan connection id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// getConnectionIDsForGroup returns all connection IDs that belong to
// any cluster in the given group.
func (d *Datastore) getConnectionIDsForGroup(ctx context.Context, groupID int) ([]int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
        SELECT c.id
        FROM connections c
        JOIN clusters cl ON c.cluster_id = cl.id
        WHERE cl.group_id = $1
        ORDER BY c.id
    `
	rows, err := d.pool.Query(ctx, query, groupID)
	if err != nil {
		return nil, fmt.Errorf("failed to query connections for group: %w", err)
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan connection id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetConnectionIDsForCluster returns all connection IDs that belong to
// the given cluster. It is an exported wrapper around the internal
// helper so other packages (notably the API handlers that apply RBAC
// visibility filtering) can enumerate a cluster's members without
// duplicating the SQL.
func (d *Datastore) GetConnectionIDsForCluster(ctx context.Context, clusterID int) ([]int, error) {
	return d.getConnectionIDsForCluster(ctx, clusterID)
}

// GetConnectionIDsForGroup returns all connection IDs that belong to
// any cluster in the given group. Exported companion to
// GetConnectionIDsForCluster for group-scoped RBAC checks.
func (d *Datastore) GetConnectionIDsForGroup(ctx context.Context, groupID int) ([]int, error) {
	return d.getConnectionIDsForGroup(ctx, groupID)
}

// gatherScopedServerData populates server status counts for a specific
// set of connection IDs by walking the full topology and filtering.
func (d *Datastore) gatherScopedServerData(ctx context.Context, snapshot *EstateSnapshot, connectionIDs []int) {
	idSet := make(map[int]bool, len(connectionIDs))
	for _, id := range connectionIDs {
		idSet[id] = true
	}

	// Sync cluster_id assignments before reading topology
	if err := d.RefreshClusterAssignments(ctx); err != nil {
		logger.Infof("gatherScopedServerData: failed to refresh cluster assignments: %v", err)
	}

	groups, err := d.GetClusterTopology(ctx, connectionIDs)
	if err != nil {
		logger.Errorf("GetScopedSnapshot: failed to get cluster topology: %v", err)
		return
	}

	var allServers []EstateServerSummary
	for _, group := range groups {
		for _, cluster := range group.Clusters {
			flattenTopologyServers(cluster.Servers, &allServers)
		}
	}

	// Filter to only the requested connections.
	var servers []EstateServerSummary
	for i := range allServers {
		if idSet[allServers[i].ID] {
			allServers[i].Role = d.mapPrimaryRoleToDisplayRole(allServers[i].Role)
			servers = append(servers, allServers[i])

			switch {
			case allServers[i].Status == "offline":
				snapshot.ServerOffline++
			case allServers[i].ActiveAlertCount > 0:
				snapshot.ServerWarning++
			default:
				snapshot.ServerOnline++
			}
		}
	}

	snapshot.Servers = servers
	snapshot.ServerTotal = len(servers)
}

// gatherScopedAlertData populates alert severity counts and top alerts
// for a specific set of connection IDs.
func (d *Datastore) gatherScopedAlertData(ctx context.Context, snapshot *EstateSnapshot, connectionIDs []int) {
	activeStatus := "active"
	result, err := d.GetAlerts(ctx, AlertListFilter{
		Status:        &activeStatus,
		ConnectionIDs: connectionIDs,
		Limit:         500,
	})
	if err != nil {
		logger.Errorf("GetScopedSnapshot: failed to get alerts: %v", err)
		return
	}

	snapshot.AlertTotal = int(result.Total)

	for i := range result.Alerts {
		switch result.Alerts[i].Severity {
		case "critical":
			snapshot.AlertCritical++
		case "warning":
			snapshot.AlertWarning++
		case "info":
			snapshot.AlertInfo++
		}

		if i < 10 {
			snapshot.TopAlerts = append(snapshot.TopAlerts, EstateAlertSummary{
				Title:      result.Alerts[i].Title,
				ServerName: result.Alerts[i].ServerName,
				Severity:   result.Alerts[i].Severity,
			})
		}
	}
}

// gatherScopedRecentEvents populates recent events from the last 24
// hours for a specific set of connection IDs.
func (d *Datastore) gatherScopedRecentEvents(ctx context.Context, snapshot *EstateSnapshot, connectionIDs []int) {
	now := time.Now().UTC()
	dayAgo := now.Add(-24 * time.Hour)

	result, err := d.GetTimelineEvents(ctx, TimelineFilter{
		StartTime:     dayAgo,
		EndTime:       now,
		ConnectionIDs: connectionIDs,
		EventTypes: []string{
			EventTypeRestart,
			EventTypeConfigChange,
			EventTypeBlackoutStarted,
			EventTypeBlackoutEnded,
		},
		Limit: 20,
	})
	if err != nil {
		logger.Errorf("GetScopedSnapshot: failed to get timeline events: %v", err)
		return
	}

	for i := range result.Events {
		snapshot.RecentEvents = append(snapshot.RecentEvents, EstateEventSummary{
			EventType:  result.Events[i].EventType,
			ServerName: result.Events[i].ServerName,
			OccurredAt: result.Events[i].OccurredAt,
			Severity:   result.Events[i].Severity,
			Title:      result.Events[i].Title,
			Summary:    result.Events[i].Summary,
		})
	}
}

// GetConnectionContext retrieves comprehensive system context for a connection
func (d *Datastore) GetConnectionContext(ctx context.Context, connectionID int) (*ConnectionContext, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Get connection name
	var serverName string
	err := d.pool.QueryRow(ctx,
		`SELECT name FROM connections WHERE id = $1`, connectionID).Scan(&serverName)
	if err != nil {
		return nil, fmt.Errorf("connection not found: %w", err)
	}

	result := &ConnectionContext{
		ConnectionID: connectionID,
		ServerName:   serverName,
	}

	// Query pg_server_info for PostgreSQL version, max_connections, extensions, etc.
	pgCtx := &PostgreSQLContext{}
	hasPGData := false

	var version sql.NullString
	var versionNum sql.NullInt32
	var maxConns sql.NullInt32
	var dataDir sql.NullString
	var extensions []string

	err = d.pool.QueryRow(ctx, `
        SELECT server_version, server_version_num, max_connections,
               data_directory, installed_extensions
        FROM metrics.pg_server_info
        WHERE connection_id = $1
        ORDER BY collected_at DESC
        LIMIT 1
    `, connectionID).Scan(&version, &versionNum, &maxConns, &dataDir, &extensions)
	if err == nil {
		hasPGData = true
		if version.Valid {
			pgCtx.Version = version.String
		}
		if versionNum.Valid {
			pgCtx.VersionNum = int(versionNum.Int32)
		}
		if maxConns.Valid {
			pgCtx.MaxConnections = int(maxConns.Int32)
		}
		if dataDir.Valid {
			pgCtx.DataDirectory = dataDir.String
		}
		if len(extensions) > 0 {
			pgCtx.InstalledExtensions = extensions
		}
	}

	// Query pg_settings for key configuration parameters
	settingsRows, err := d.pool.Query(ctx, `
        SELECT name, setting
        FROM metrics.pg_settings
        WHERE connection_id = $1
          AND name IN (
              'shared_buffers', 'work_mem', 'effective_cache_size',
              'maintenance_work_mem', 'max_worker_processes',
              'max_parallel_workers', 'max_parallel_workers_per_gather',
              'wal_level', 'wal_buffers', 'random_page_cost',
              'effective_io_concurrency', 'checkpoint_completion_target',
              'huge_pages', 'temp_buffers'
          )
          AND collected_at = (
              SELECT MAX(collected_at)
              FROM metrics.pg_settings
              WHERE connection_id = $1
          )
        ORDER BY name
    `, connectionID)
	if err == nil {
		defer settingsRows.Close()
		settings := make(map[string]string)
		for settingsRows.Next() {
			var name string
			var setting sql.NullString
			if err := settingsRows.Scan(&name, &setting); err == nil && setting.Valid {
				settings[name] = setting.String
			}
		}
		if len(settings) > 0 {
			hasPGData = true
			pgCtx.Settings = settings
		}
	}

	if hasPGData {
		result.PostgreSQL = pgCtx
	}

	// Query system information (requires system_stats extension)
	sysCtx := &SystemContext{}
	hasSysData := false

	// OS info
	var osName, osVersion, architecture, hostname sql.NullString
	err = d.pool.QueryRow(ctx, `
        SELECT name, version, architecture, host_name
        FROM metrics.pg_sys_os_info
        WHERE connection_id = $1
        ORDER BY collected_at DESC
        LIMIT 1
    `, connectionID).Scan(&osName, &osVersion, &architecture, &hostname)
	if err == nil {
		hasSysData = true
		if osName.Valid {
			sysCtx.OSName = osName.String
		}
		if osVersion.Valid {
			sysCtx.OSVersion = osVersion.String
		}
		if architecture.Valid {
			sysCtx.Architecture = architecture.String
		}
		if hostname.Valid {
			sysCtx.Hostname = hostname.String
		}
	}

	// CPU info
	var cpuModel sql.NullString
	var cores, logicalProcs sql.NullInt32
	err = d.pool.QueryRow(ctx, `
        SELECT model_name, no_of_cores, logical_processor
        FROM metrics.pg_sys_cpu_info
        WHERE connection_id = $1
        ORDER BY collected_at DESC
        LIMIT 1
    `, connectionID).Scan(&cpuModel, &cores, &logicalProcs)
	if err == nil {
		hasSysData = true
		cpu := &CPUContext{}
		hasCPU := false
		if cpuModel.Valid {
			cpu.Model = cpuModel.String
			hasCPU = true
		}
		if cores.Valid {
			cpu.Cores = int(cores.Int32)
			hasCPU = true
		}
		if logicalProcs.Valid {
			cpu.LogicalProcessors = int(logicalProcs.Int32)
			hasCPU = true
		}
		if hasCPU {
			sysCtx.CPU = cpu
		}
	}

	// Memory info
	var totalMem, freeMem sql.NullInt64
	err = d.pool.QueryRow(ctx, `
        SELECT total_memory, free_memory
        FROM metrics.pg_sys_memory_info
        WHERE connection_id = $1
        ORDER BY collected_at DESC
        LIMIT 1
    `, connectionID).Scan(&totalMem, &freeMem)
	if err == nil {
		hasSysData = true
		if totalMem.Valid || freeMem.Valid {
			mem := &MemoryContext{}
			if totalMem.Valid {
				mem.TotalBytes = totalMem.Int64
			}
			if freeMem.Valid {
				mem.FreeBytes = freeMem.Int64
			}
			sysCtx.Memory = mem
		}
	}

	// Disk info
	diskRows, err := d.pool.Query(ctx, `
        SELECT mount_point, file_system_type, total_space, used_space, free_space
        FROM metrics.pg_sys_disk_info
        WHERE connection_id = $1
          AND collected_at = (
              SELECT MAX(collected_at)
              FROM metrics.pg_sys_disk_info
              WHERE connection_id = $1
          )
        ORDER BY mount_point
    `, connectionID)
	if err == nil {
		defer diskRows.Close()
		var disks []DiskContext
		for diskRows.Next() {
			var mountPoint string
			var fsType sql.NullString
			var totalSpace, usedSpace, freeSpace sql.NullInt64
			if err := diskRows.Scan(&mountPoint, &fsType, &totalSpace,
				&usedSpace, &freeSpace); err == nil {
				disk := DiskContext{MountPoint: mountPoint}
				if fsType.Valid {
					disk.FilesystemType = fsType.String
				}
				if totalSpace.Valid {
					disk.TotalBytes = totalSpace.Int64
				}
				if usedSpace.Valid {
					disk.UsedBytes = usedSpace.Int64
				}
				if freeSpace.Valid {
					disk.FreeBytes = freeSpace.Int64
				}
				disks = append(disks, disk)
			}
		}
		if len(disks) > 0 {
			hasSysData = true
			sysCtx.Disks = disks
		}
	}

	if hasSysData {
		result.System = sysCtx
	}

	return result, nil
}
