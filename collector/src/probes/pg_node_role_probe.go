/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package probes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lib/pq"
	"github.com/pgedge/ai-workbench/pkg/logger"
)

// parseConnInfo parses a PostgreSQL connection info string and extracts host and port
// Format: "host=hostname port=5432 dbname=mydb user=myuser"
func parseConnInfo(conninfo string) (host string, port int) {
	// Parse key=value pairs from the conninfo string
	parts := strings.Fields(conninfo)
	for _, part := range parts {
		if strings.HasPrefix(part, "host=") {
			host = strings.TrimPrefix(part, "host=")
		} else if strings.HasPrefix(part, "port=") {
			portStr := strings.TrimPrefix(part, "port=")
			if p, err := strconv.Atoi(portStr); err == nil {
				port = p
			}
		}
	}
	// Default port if not specified
	if port == 0 && host != "" {
		port = 5432
	}
	return host, port
}

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
)

// Role flag constants
const (
	FlagBinaryPrimary     = "binary_primary"
	FlagBinaryStandby     = "binary_standby"
	FlagLogicalPublisher  = "logical_publisher"
	FlagLogicalSubscriber = "logical_subscriber"
	FlagSpockNode         = "spock_node"
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

// GetQuery returns the SQL query to execute (not used - we use multiple queries)
func (p *PgNodeRoleProbe) GetQuery() string {
	return "" // Not used - Execute() runs multiple queries
}

// NodeRoleInfo holds all the collected node role information
type NodeRoleInfo struct {
	// Fundamental status
	IsInRecovery        bool
	TimelineID          *int
	PostmasterStartTime *time.Time

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
	HasActiveLogicalSlots   bool
	ActiveLogicalSlotCount  int
	PublisherHost           *string // For subscribers: the publisher's host
	PublisherPort           *int    // For subscribers: the publisher's port

	// Spock
	HasSpock               bool
	SpockNodeID            *int64
	SpockNodeName          *string
	SpockSubscriptionCount int

	// Computed
	PrimaryRole string
	RoleFlags   []string
	RoleDetails map[string]any
}

// Execute runs the probe against a monitored connection
func (p *PgNodeRoleProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]any, error) {
	info := &NodeRoleInfo{
		RoleDetails: make(map[string]any),
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

	// 5. Determine primary role and flags
	info.PrimaryRole, info.RoleFlags = p.determineNodeRole(info)

	// Convert to map
	metric := p.infoToMap(info)
	return []map[string]any{metric}, nil
}

// getFundamentalStatus gets basic recovery status
func (p *PgNodeRoleProbe) getFundamentalStatus(ctx context.Context, conn *pgxpool.Conn, info *NodeRoleInfo) error {
	query := `
        SELECT
            pg_is_in_recovery() as is_in_recovery,
            (SELECT timeline_id FROM pg_control_checkpoint()) as timeline_id,
            pg_postmaster_start_time() as postmaster_start_time
    `
	row := conn.QueryRow(ctx, query)
	return row.Scan(&info.IsInRecovery, &info.TimelineID, &info.PostmasterStartTime)
}

// getBinaryReplicationStatus gets physical replication information
func (p *PgNodeRoleProbe) getBinaryReplicationStatus(ctx context.Context, conn *pgxpool.Conn, info *NodeRoleInfo) error {
	// Count active streaming standbys (physical/binary replication only)
	// pg_stat_replication shows BOTH physical and logical replication connections,
	// so we must join with pg_replication_slots to filter out logical replication.
	// Physical standbys either have no slot (streaming without slot) or a physical slot.
	countQuery := `
        SELECT COUNT(*)
        FROM pg_stat_replication r
        LEFT JOIN pg_replication_slots s ON r.pid = s.active_pid
        WHERE r.state = 'streaming'
          AND (s.slot_type IS NULL OR s.slot_type = 'physical')
    `
	row := conn.QueryRow(ctx, countQuery)
	if err := row.Scan(&info.BinaryStandbyCount); err != nil {
		return fmt.Errorf("failed to count standbys: %w", err)
	}
	info.HasBinaryStandbys = info.BinaryStandbyCount > 0

	// If in recovery, get standby info from wal receiver
	if info.IsInRecovery {
		// Note: pg_stat_wal_receiver has flushed_lsn (synced to disk) and written_lsn (received)
		// We use flushed_lsn as the "received" position since it's more reliable
		receiverQuery := `
            SELECT
                sender_host,
                sender_port,
                flushed_lsn::text,
                (SELECT pg_last_wal_replay_lsn()::text)
            FROM pg_stat_wal_receiver
            LIMIT 1
        `
		row := conn.QueryRow(ctx, receiverQuery)
		err := row.Scan(&info.UpstreamHost, &info.UpstreamPort, &info.ReceivedLSN, &info.ReplayedLSN)
		if err != nil {
			// Not necessarily an error - wal receiver might not be active
			if !errors.Is(err, pgx.ErrNoRows) {
				logger.Infof("WAL receiver query for %s: %v", "standby", err)
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
        WHERE relid IS NULL AND pid IS NOT NULL
    `
	row = conn.QueryRow(ctx, activeSubQuery)
	if err := row.Scan(&info.ActiveSubscriptionCount); err != nil {
		// May fail on older versions or if no subscriptions
		info.ActiveSubscriptionCount = 0
	}

	// Count active logical replication slots (indicates subscribers are connected)
	logicalSlotQuery := `
        SELECT COUNT(*)
        FROM pg_replication_slots
        WHERE slot_type = 'logical' AND active = true
    `
	row = conn.QueryRow(ctx, logicalSlotQuery)
	if err := row.Scan(&info.ActiveLogicalSlotCount); err != nil {
		info.ActiveLogicalSlotCount = 0
	}
	info.HasActiveLogicalSlots = info.ActiveLogicalSlotCount > 0

	// For subscribers, extract publisher host/port from subscription conninfo
	// We take the first subscription's connection info as the primary publisher
	if info.SubscriptionCount > 0 {
		pubConnQuery := `
            SELECT subconninfo FROM pg_subscription LIMIT 1
        `
		var conninfo string
		row = conn.QueryRow(ctx, pubConnQuery)
		if err := row.Scan(&conninfo); err == nil && conninfo != "" {
			host, port := parseConnInfo(conninfo)
			if host != "" {
				info.PublisherHost = &host
			}
			if port != 0 {
				info.PublisherPort = &port
			}
		}
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
	// Note: spock.local_node only has node_id, node_name is in spock.node
	// Cast node_id to bigint since OID type can't be scanned directly into int64
	nodeQuery := `
        SELECT ln.node_id::bigint, n.node_name
        FROM spock.local_node ln
        JOIN spock.node n ON ln.node_id = n.node_id
        LIMIT 1
    `
	row := conn.QueryRow(ctx, nodeQuery)
	err = row.Scan(&info.SpockNodeID, &info.SpockNodeName)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
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

// determineNodeRole computes the primary role and role flags
func (p *PgNodeRoleProbe) determineNodeRole(info *NodeRoleInfo) (string, []string) {
	flags := []string{}

	// Detect individual capabilities (flags are non-exclusive)
	if info.HasBinaryStandbys {
		flags = append(flags, FlagBinaryPrimary)
	}
	if info.IsStreamingStandby {
		flags = append(flags, FlagBinaryStandby)
	}
	if info.PublicationCount > 0 && info.HasActiveLogicalSlots {
		// Only flag as publisher if there are active logical replication slots
		flags = append(flags, FlagLogicalPublisher)
	}
	if info.SubscriptionCount > 0 {
		flags = append(flags, FlagLogicalSubscriber)
	}
	// Only flag as Spock node if NOT in recovery - a streaming standby of a Spock
	// node inherits the Spock tables but is not itself a Spock cluster member
	if info.HasSpock && info.SpockNodeName != nil && !info.IsInRecovery {
		flags = append(flags, FlagSpockNode)
	}

	// Determine primary role (most specific applicable role)
	// Priority: Spock > Binary replication > Logical replication > Standalone
	var primaryRole string
	switch {
	// Spock node: has Spock extension configured AND is not in recovery
	// A streaming standby of a Spock node has the Spock tables (replicated) but
	// is NOT a Spock cluster member - it's a binary standby for HA
	case info.HasSpock && info.SpockNodeName != nil && !info.IsInRecovery:
		primaryRole = RoleSpockNode

	// Any server in recovery mode is a binary standby (streaming replication)
	case info.IsInRecovery:
		if info.HasBinaryStandbys {
			primaryRole = RoleBinaryCascading
		} else {
			primaryRole = RoleBinaryStandby
		}

	// Primary with streaming standbys
	case info.HasBinaryStandbys:
		primaryRole = RoleBinaryPrimary

	// Logical replication: only if there are active subscribers (not just publications)
	// A server with publications but no active logical slots is effectively standalone
	case info.PublicationCount > 0 && info.SubscriptionCount > 0 && info.HasActiveLogicalSlots:
		primaryRole = RoleLogicalBidirectional
	case info.PublicationCount > 0 && info.HasActiveLogicalSlots:
		primaryRole = RoleLogicalPublisher
	case info.SubscriptionCount > 0:
		primaryRole = RoleLogicalSubscriber

	default:
		primaryRole = RoleStandalone
	}

	return primaryRole, flags
}

// infoToMap converts NodeRoleInfo to a map for storage
func (p *PgNodeRoleProbe) infoToMap(info *NodeRoleInfo) map[string]any {
	// Build role details JSON
	roleDetails := info.RoleDetails
	if info.PublicationCount > 0 || info.SubscriptionCount > 0 {
		roleDetails["logical"] = map[string]any{
			"publications":  info.PublicationCount,
			"subscriptions": info.SubscriptionCount,
		}
	}
	if info.HasSpock && info.SpockNodeName != nil {
		roleDetails["spock"] = map[string]any{
			"node_id":            info.SpockNodeID,
			"node_name":          info.SpockNodeName,
			"subscription_count": info.SpockSubscriptionCount,
		}
	}
	roleDetailsJSON, err := json.Marshal(roleDetails)
	if err != nil {
		roleDetailsJSON = []byte("{}")
	}

	return map[string]any{
		"is_in_recovery":            info.IsInRecovery,
		"timeline_id":               info.TimelineID,
		"postmaster_start_time":     info.PostmasterStartTime,
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
		"has_active_logical_slots":  info.HasActiveLogicalSlots,
		"active_logical_slot_count": info.ActiveLogicalSlotCount,
		"publisher_host":            info.PublisherHost,
		"publisher_port":            info.PublisherPort,
		"has_spock":                 info.HasSpock,
		"spock_node_id":             info.SpockNodeID,
		"spock_node_name":           info.SpockNodeName,
		"spock_subscription_count":  info.SpockSubscriptionCount,
		"primary_role":              info.PrimaryRole,
		"role_flags":                info.RoleFlags,
		"role_details":              string(roleDetailsJSON),
	}
}

// Store stores the collected metrics in the datastore
func (p *PgNodeRoleProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]any) error {
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
		"is_in_recovery", "timeline_id", "postmaster_start_time",
		"has_binary_standbys", "binary_standby_count",
		"is_streaming_standby", "upstream_host", "upstream_port",
		"received_lsn", "replayed_lsn",
		"publication_count", "subscription_count", "active_subscription_count",
		"has_active_logical_slots", "active_logical_slot_count",
		"publisher_host", "publisher_port",
		"has_spock", "spock_node_id", "spock_node_name", "spock_subscription_count",
		"primary_role", "role_flags", "role_details",
	}

	// Build values array
	var values [][]any
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

		row := []any{
			connectionID,
			timestamp,
			metric["is_in_recovery"],
			metric["timeline_id"],
			metric["postmaster_start_time"],
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
			metric["has_active_logical_slots"],
			metric["active_logical_slot_count"],
			metric["publisher_host"],
			metric["publisher_port"],
			metric["has_spock"],
			metric["spock_node_id"],
			metric["spock_node_name"],
			metric["spock_subscription_count"],
			metric["primary_role"],
			roleFlagsArr,
			metric["role_details"],
		}
		values = append(values, row)
	}

	// Store metrics
	if err := StoreMetrics(ctx, datastoreConn, p.GetTableName(), columns, values); err != nil {
		return fmt.Errorf("failed to store metrics: %w", err)
	}

	return nil
}
