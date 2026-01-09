/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package probes

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lib/pq"
	"github.com/pgedge/ai-workbench/pkg/logger"
)

// NodeRole constants
const (
	RoleStandalone           = "standalone"
	RoleBinaryPrimary        = "binary_primary"
	RoleBinaryStandby        = "binary_standby"
	RoleBinaryCascading      = "binary_cascading"
	RoleLogicalPublisher     = "logical_publisher"
	RoleLogicalSubscriber    = "logical_subscriber"
	RoleLogicalBidirectional = "logical_bidirectional"
	RoleSpockNode            = "spock_node"
	RoleSpockStandby         = "spock_standby"
	RoleBDRNode              = "bdr_node"
	RoleBDRStandby           = "bdr_standby"
)

// Role flag constants
const (
	FlagBinaryPrimary     = "binary_primary"
	FlagBinaryStandby     = "binary_standby"
	FlagLogicalPublisher  = "logical_publisher"
	FlagLogicalSubscriber = "logical_subscriber"
	FlagSpockNode         = "spock_node"
	FlagBDRNode           = "bdr_node"
)

// PgNodeRoleProbe detects and stores node role information for cluster topology analysis
type PgNodeRoleProbe struct {
	BaseMetricsProbe
}

// NewPgNodeRoleProbe creates a new pg_node_role probe
func NewPgNodeRoleProbe(config *ProbeConfig) *PgNodeRoleProbe {
	return &PgNodeRoleProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetName returns the probe name
func (p *PgNodeRoleProbe) GetName() string {
	return ProbeNamePgNodeRole
}

// GetTableName returns the metrics table name
func (p *PgNodeRoleProbe) GetTableName() string {
	return ProbeNamePgNodeRole
}

// IsDatabaseScoped returns false as pg_node_role is server-scoped
func (p *PgNodeRoleProbe) IsDatabaseScoped() bool {
	return false
}

// GetQuery returns the SQL query to execute (not used - we use multiple queries)
func (p *PgNodeRoleProbe) GetQuery() string {
	return "" // Not used - Execute() runs multiple queries
}

// NodeRoleInfo holds all the collected node role information
type NodeRoleInfo struct {
	// Fundamental status
	IsInRecovery bool
	TimelineID   *int

	// Binary replication
	HasBinaryStandbys  bool
	BinaryStandbyCount int
	IsStreamingStandby bool
	UpstreamHost       *string
	UpstreamPort       *int
	ReceivedLSN        *string
	ReplayedLSN        *string

	// Logical replication
	PublicationCount        int
	SubscriptionCount       int
	ActiveSubscriptionCount int

	// Spock
	HasSpock               bool
	SpockNodeID            *int64
	SpockNodeName          *string
	SpockSubscriptionCount int

	// BDR (future)
	HasBDR       bool
	BDRNodeID    *string
	BDRNodeName  *string
	BDRNodeGroup *string
	BDRNodeState *string

	// Computed
	PrimaryRole string
	RoleFlags   []string
	RoleDetails map[string]interface{}
}

// Execute runs the probe against a monitored connection
func (p *PgNodeRoleProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]interface{}, error) {
	info := &NodeRoleInfo{
		RoleDetails: make(map[string]interface{}),
	}

	// 1. Get fundamental status
	if err := p.getFundamentalStatus(ctx, monitoredConn, info); err != nil {
		return nil, fmt.Errorf("failed to get fundamental status: %w", err)
	}

	// 2. Get binary replication status
	if err := p.getBinaryReplicationStatus(ctx, monitoredConn, info); err != nil {
		return nil, fmt.Errorf("failed to get binary replication status: %w", err)
	}

	// 3. Get logical replication status
	if err := p.getLogicalReplicationStatus(ctx, monitoredConn, info); err != nil {
		return nil, fmt.Errorf("failed to get logical replication status: %w", err)
	}

	// 4. Check for Spock extension and get node info
	if err := p.getSpockStatus(ctx, connectionName, monitoredConn, info); err != nil {
		logger.Infof("Spock status check: %v", err)
		// Not a fatal error - Spock may not be installed
	}

	// 5. Check for BDR extension and get node info (future)
	if err := p.getBDRStatus(ctx, connectionName, monitoredConn, info); err != nil {
		logger.Infof("BDR status check: %v", err)
		// Not a fatal error - BDR may not be installed
	}

	// 6. Determine primary role and flags
	info.PrimaryRole, info.RoleFlags = p.determineNodeRole(info)

	// Convert to map
	metric := p.infoToMap(info)
	return []map[string]interface{}{metric}, nil
}

// getFundamentalStatus gets basic recovery status
func (p *PgNodeRoleProbe) getFundamentalStatus(ctx context.Context, conn *pgxpool.Conn, info *NodeRoleInfo) error {
	query := `
        SELECT
            pg_is_in_recovery() as is_in_recovery,
            (SELECT timeline_id FROM pg_control_checkpoint()) as timeline_id
    `
	row := conn.QueryRow(ctx, query)
	return row.Scan(&info.IsInRecovery, &info.TimelineID)
}

// getBinaryReplicationStatus gets physical replication information
func (p *PgNodeRoleProbe) getBinaryReplicationStatus(ctx context.Context, conn *pgxpool.Conn, info *NodeRoleInfo) error {
	// Count active streaming standbys
	countQuery := `
        SELECT COUNT(*)
        FROM pg_stat_replication
        WHERE state = 'streaming'
    `
	row := conn.QueryRow(ctx, countQuery)
	if err := row.Scan(&info.BinaryStandbyCount); err != nil {
		return fmt.Errorf("failed to count standbys: %w", err)
	}
	info.HasBinaryStandbys = info.BinaryStandbyCount > 0

	// If in recovery, get standby info from wal receiver
	if info.IsInRecovery {
		receiverQuery := `
            SELECT
                sender_host,
                sender_port,
                received_lsn::text,
                (SELECT pg_last_wal_replay_lsn()::text)
            FROM pg_stat_wal_receiver
            LIMIT 1
        `
		row := conn.QueryRow(ctx, receiverQuery)
		err := row.Scan(&info.UpstreamHost, &info.UpstreamPort, &info.ReceivedLSN, &info.ReplayedLSN)
		if err != nil {
			// Not necessarily an error - wal receiver might not be active
			if err.Error() != "no rows in result set" {
				logger.Infof("WAL receiver query: %v", err)
			}
		} else {
			info.IsStreamingStandby = true
		}
	}

	return nil
}

// getLogicalReplicationStatus gets logical replication information
func (p *PgNodeRoleProbe) getLogicalReplicationStatus(ctx context.Context, conn *pgxpool.Conn, info *NodeRoleInfo) error {
	// Count publications
	pubQuery := `SELECT COUNT(*) FROM pg_publication`
	row := conn.QueryRow(ctx, pubQuery)
	if err := row.Scan(&info.PublicationCount); err != nil {
		return fmt.Errorf("failed to count publications: %w", err)
	}

	// Count subscriptions
	subQuery := `SELECT COUNT(*) FROM pg_subscription`
	row = conn.QueryRow(ctx, subQuery)
	if err := row.Scan(&info.SubscriptionCount); err != nil {
		return fmt.Errorf("failed to count subscriptions: %w", err)
	}

	// Count active subscriptions (those with active workers)
	activeSubQuery := `
        SELECT COUNT(DISTINCT subid)
        FROM pg_stat_subscription
        WHERE subrelid IS NULL AND pid IS NOT NULL
    `
	row = conn.QueryRow(ctx, activeSubQuery)
	if err := row.Scan(&info.ActiveSubscriptionCount); err != nil {
		// May fail on older versions or if no subscriptions
		info.ActiveSubscriptionCount = 0
	}

	return nil
}

// getSpockStatus checks for Spock extension and gets node info
func (p *PgNodeRoleProbe) getSpockStatus(ctx context.Context, connectionName string, conn *pgxpool.Conn, info *NodeRoleInfo) error {
	// Check if Spock extension exists
	exists, err := CheckExtensionExists(ctx, connectionName, conn, "spock")
	if err != nil {
		return err
	}
	info.HasSpock = exists

	if !exists {
		return nil
	}

	// Get local node info from Spock
	nodeQuery := `
        SELECT node_id, node_name
        FROM spock.local_node
        LIMIT 1
    `
	row := conn.QueryRow(ctx, nodeQuery)
	err = row.Scan(&info.SpockNodeID, &info.SpockNodeName)
	if err != nil {
		if err.Error() != "no rows in result set" {
			return fmt.Errorf("failed to get Spock node info: %w", err)
		}
		// No local node registered - Spock installed but not configured
		return nil
	}

	// Count active Spock subscriptions
	subCountQuery := `
        SELECT COUNT(*)
        FROM spock.subscription
        WHERE sub_enabled = true
    `
	row = conn.QueryRow(ctx, subCountQuery)
	if err := row.Scan(&info.SpockSubscriptionCount); err != nil {
		// May fail if subscription table doesn't exist
		info.SpockSubscriptionCount = 0
	}

	return nil
}

// getBDRStatus checks for BDR extension and gets node info (future implementation)
func (p *PgNodeRoleProbe) getBDRStatus(ctx context.Context, connectionName string, conn *pgxpool.Conn, info *NodeRoleInfo) error {
	// Check if BDR extension exists
	exists, err := CheckExtensionExists(ctx, connectionName, conn, "bdr")
	if err != nil {
		return err
	}
	info.HasBDR = exists

	if !exists {
		return nil
	}

	// Get local node info from BDR
	// Note: BDR schema may vary by version, this is a basic implementation
	nodeQuery := `
        SELECT
            node_id::text,
            node_name,
            node_group_name,
            node_state
        FROM bdr.local_node_info
        LIMIT 1
    `
	row := conn.QueryRow(ctx, nodeQuery)
	err = row.Scan(&info.BDRNodeID, &info.BDRNodeName, &info.BDRNodeGroup, &info.BDRNodeState)
	if err != nil {
		if err.Error() != "no rows in result set" {
			// BDR installed but local_node_info view may not exist in all versions
			logger.Infof("BDR local_node_info query: %v", err)
		}
		return nil
	}

	return nil
}

// determineNodeRole computes the primary role and role flags
func (p *PgNodeRoleProbe) determineNodeRole(info *NodeRoleInfo) (string, []string) {
	var flags []string

	// Detect individual capabilities (flags are non-exclusive)
	if info.HasBinaryStandbys {
		flags = append(flags, FlagBinaryPrimary)
	}
	if info.IsStreamingStandby {
		flags = append(flags, FlagBinaryStandby)
	}
	if info.PublicationCount > 0 {
		flags = append(flags, FlagLogicalPublisher)
	}
	if info.SubscriptionCount > 0 {
		flags = append(flags, FlagLogicalSubscriber)
	}
	if info.HasSpock && info.SpockNodeName != nil {
		flags = append(flags, FlagSpockNode)
	}
	if info.HasBDR && info.BDRNodeName != nil {
		flags = append(flags, FlagBDRNode)
	}

	// Determine primary role (most specific applicable role)
	var primaryRole string
	switch {
	case info.HasSpock && info.SpockNodeName != nil:
		if info.IsInRecovery {
			primaryRole = RoleSpockStandby
		} else {
			primaryRole = RoleSpockNode
		}
	case info.HasBDR && info.BDRNodeName != nil:
		if info.IsInRecovery {
			primaryRole = RoleBDRStandby
		} else {
			primaryRole = RoleBDRNode
		}
	case info.IsInRecovery:
		if info.HasBinaryStandbys {
			primaryRole = RoleBinaryCascading
		} else {
			primaryRole = RoleBinaryStandby
		}
	case info.HasBinaryStandbys:
		primaryRole = RoleBinaryPrimary
	case info.PublicationCount > 0 && info.SubscriptionCount > 0:
		primaryRole = RoleLogicalBidirectional
	case info.PublicationCount > 0:
		primaryRole = RoleLogicalPublisher
	case info.SubscriptionCount > 0:
		primaryRole = RoleLogicalSubscriber
	default:
		primaryRole = RoleStandalone
	}

	return primaryRole, flags
}

// infoToMap converts NodeRoleInfo to a map for storage
func (p *PgNodeRoleProbe) infoToMap(info *NodeRoleInfo) map[string]interface{} {
	// Build role details JSON
	roleDetails := info.RoleDetails
	if info.PublicationCount > 0 || info.SubscriptionCount > 0 {
		roleDetails["logical"] = map[string]interface{}{
			"publications":  info.PublicationCount,
			"subscriptions": info.SubscriptionCount,
		}
	}
	if info.HasSpock && info.SpockNodeName != nil {
		roleDetails["spock"] = map[string]interface{}{
			"node_id":            info.SpockNodeID,
			"node_name":          info.SpockNodeName,
			"subscription_count": info.SpockSubscriptionCount,
		}
	}
	if info.HasBDR && info.BDRNodeName != nil {
		roleDetails["bdr"] = map[string]interface{}{
			"node_id":    info.BDRNodeID,
			"node_name":  info.BDRNodeName,
			"node_group": info.BDRNodeGroup,
			"node_state": info.BDRNodeState,
		}
	}

	roleDetailsJSON, err := json.Marshal(roleDetails)
	if err != nil {
		roleDetailsJSON = []byte("{}")
	}

	return map[string]interface{}{
		"is_in_recovery":            info.IsInRecovery,
		"timeline_id":               info.TimelineID,
		"has_binary_standbys":       info.HasBinaryStandbys,
		"binary_standby_count":      info.BinaryStandbyCount,
		"is_streaming_standby":      info.IsStreamingStandby,
		"upstream_host":             info.UpstreamHost,
		"upstream_port":             info.UpstreamPort,
		"received_lsn":              info.ReceivedLSN,
		"replayed_lsn":              info.ReplayedLSN,
		"publication_count":         info.PublicationCount,
		"subscription_count":        info.SubscriptionCount,
		"active_subscription_count": info.ActiveSubscriptionCount,
		"has_spock":                 info.HasSpock,
		"spock_node_id":             info.SpockNodeID,
		"spock_node_name":           info.SpockNodeName,
		"spock_subscription_count":  info.SpockSubscriptionCount,
		"has_bdr":                   info.HasBDR,
		"bdr_node_id":               info.BDRNodeID,
		"bdr_node_name":             info.BDRNodeName,
		"bdr_node_group":            info.BDRNodeGroup,
		"bdr_node_state":            info.BDRNodeState,
		"primary_role":              info.PrimaryRole,
		"role_flags":                info.RoleFlags,
		"role_details":              string(roleDetailsJSON),
	}
}

// Store stores the collected metrics in the datastore
func (p *PgNodeRoleProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
	if len(metrics) == 0 {
		return nil // Nothing to store
	}

	// Ensure partition exists for this timestamp
	if err := p.EnsurePartition(ctx, datastoreConn, timestamp); err != nil {
		return fmt.Errorf("failed to ensure partition: %w", err)
	}

	// Define columns in order
	columns := []string{
		"connection_id", "collected_at",
		"is_in_recovery", "timeline_id",
		"has_binary_standbys", "binary_standby_count",
		"is_streaming_standby", "upstream_host", "upstream_port",
		"received_lsn", "replayed_lsn",
		"publication_count", "subscription_count", "active_subscription_count",
		"has_spock", "spock_node_id", "spock_node_name", "spock_subscription_count",
		"has_bdr", "bdr_node_id", "bdr_node_name", "bdr_node_group", "bdr_node_state",
		"primary_role", "role_flags", "role_details",
	}

	// Build values array
	var values [][]interface{}
	for _, metric := range metrics {
		// Convert role_flags to PostgreSQL array
		roleFlags := metric["role_flags"]
		var roleFlagsSlice []string
		if roleFlags != nil {
			if flags, ok := roleFlags.([]string); ok {
				roleFlagsSlice = flags
			}
		}
		roleFlagsArr := pq.Array(roleFlagsSlice)

		row := []interface{}{
			connectionID,
			timestamp,
			metric["is_in_recovery"],
			metric["timeline_id"],
			metric["has_binary_standbys"],
			metric["binary_standby_count"],
			metric["is_streaming_standby"],
			metric["upstream_host"],
			metric["upstream_port"],
			metric["received_lsn"],
			metric["replayed_lsn"],
			metric["publication_count"],
			metric["subscription_count"],
			metric["active_subscription_count"],
			metric["has_spock"],
			metric["spock_node_id"],
			metric["spock_node_name"],
			metric["spock_subscription_count"],
			metric["has_bdr"],
			metric["bdr_node_id"],
			metric["bdr_node_name"],
			metric["bdr_node_group"],
			metric["bdr_node_state"],
			metric["primary_role"],
			roleFlagsArr,
			metric["role_details"],
		}
		values = append(values, row)
	}

	// Use INSERT to store metrics
	if err := StoreMetricsWithCopy(ctx, datastoreConn, p.GetTableName(), columns, values); err != nil {
		return fmt.Errorf("failed to store metrics: %w", err)
	}

	return nil
}

// EnsurePartition ensures a partition exists for the given timestamp
func (p *PgNodeRoleProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
