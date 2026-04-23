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
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/pkg/logger"
)

// PgServerInfoProbe collects server identification and configuration information
// This probe only stores data when changes are detected compared to the most recent stored data
type PgServerInfoProbe struct {
	BaseMetricsProbe
}

// NewPgServerInfoProbe creates a new pg_server_info probe
func NewPgServerInfoProbe(config *ProbeConfig) *PgServerInfoProbe {
	return &PgServerInfoProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetQuery returns the SQL query to execute
func (p *PgServerInfoProbe) GetQuery() string {
	return `
        SELECT
            current_setting('server_version') as server_version,
            current_setting('server_version_num')::integer as server_version_num,
            (SELECT system_identifier FROM pg_control_system()) as system_identifier,
            current_setting('cluster_name', true) as cluster_name,
            current_setting('data_directory') as data_directory,
            current_setting('max_connections')::integer as max_connections,
            current_setting('max_wal_senders')::integer as max_wal_senders,
            current_setting('max_replication_slots')::integer as max_replication_slots,
            (SELECT array_agg(extname ORDER BY extname) FROM pg_extension) as installed_extensions
    `
}

// Execute runs the probe against a monitored connection
func (p *PgServerInfoProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]any, error) {
	query := WrapQuery(ProbeNamePgServerInfo, p.GetQuery())
	row := monitoredConn.QueryRow(ctx, query)

	var probeMarker string
	var serverVersion string
	var serverVersionNum int
	var systemIdentifier int64
	var clusterName *string
	var dataDirectory string
	var maxConnections int
	var maxWalSenders int
	var maxReplicationSlots int
	var installedExtensions []string

	err := row.Scan(
		&probeMarker,
		&serverVersion,
		&serverVersionNum,
		&systemIdentifier,
		&clusterName,
		&dataDirectory,
		&maxConnections,
		&maxWalSenders,
		&maxReplicationSlots,
		&installedExtensions,
	)
	_ = probeMarker // marker column used for query identification only
	if err != nil {
		return nil, fmt.Errorf("failed to scan server info: %w", err)
	}

	metric := map[string]any{
		"server_version":        serverVersion,
		"server_version_num":    serverVersionNum,
		"system_identifier":     systemIdentifier,
		"cluster_name":          clusterName,
		"data_directory":        dataDirectory,
		"max_connections":       maxConnections,
		"max_wal_senders":       maxWalSenders,
		"max_replication_slots": maxReplicationSlots,
		"installed_extensions":  installedExtensions,
	}

	return []map[string]any{metric}, nil
}

// Store stores the collected metrics in the datastore only if changes are detected
func (p *PgServerInfoProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]any) error {
	if len(metrics) == 0 {
		return nil // Nothing to store
	}

	// Check if server info has changed compared to the most recent stored data
	hasChanged, err := p.hasDataChanged(ctx, datastoreConn, connectionID, metrics)
	if err != nil {
		return fmt.Errorf("failed to check for changes: %w", err)
	}

	if !hasChanged {
		logger.Infof("pg_server_info data unchanged for connection %d, skipping storage", connectionID)
		return nil
	}

	logger.Infof("pg_server_info data changed for connection %d, storing new snapshot", connectionID)

	// Ensure partition exists for this timestamp
	if err := p.EnsurePartition(ctx, datastoreConn, timestamp); err != nil {
		return fmt.Errorf("failed to ensure partition: %w", err)
	}

	// Define columns in order
	columns := []string{
		"connection_id", "collected_at",
		"server_version", "server_version_num", "system_identifier",
		"cluster_name", "data_directory",
		"max_connections", "max_wal_senders", "max_replication_slots",
		"installed_extensions",
	}

	// Build values array
	var values [][]any
	for _, metric := range metrics {
		row := []any{
			connectionID,
			timestamp,
			metric["server_version"],
			metric["server_version_num"],
			metric["system_identifier"],
			metric["cluster_name"],
			metric["data_directory"],
			metric["max_connections"],
			metric["max_wal_senders"],
			metric["max_replication_slots"],
			metric["installed_extensions"],
		}
		values = append(values, row)
	}

	// Use INSERT to store metrics
	if err := StoreMetricsWithCopy(ctx, datastoreConn, p.GetTableName(), columns, values); err != nil {
		return fmt.Errorf("failed to store metrics: %w", err)
	}

	return nil
}

// hasDataChanged checks if the current server info differs from the most recently stored data
func (p *PgServerInfoProbe) hasDataChanged(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, currentMetrics []map[string]any) (bool, error) {
	// Compute hash of current metrics
	currentHash, err := p.computeMetricsHash(currentMetrics)
	if err != nil {
		return false, fmt.Errorf("failed to compute current metrics hash: %w", err)
	}

	// Get the most recent stored data for this connection
	query := `
        SELECT server_version, server_version_num, system_identifier,
               cluster_name, data_directory,
               max_connections, max_wal_senders, max_replication_slots,
               installed_extensions
        FROM metrics.pg_server_info
        WHERE connection_id = $1
        ORDER BY collected_at DESC
        LIMIT 1
    `

	row := datastoreConn.QueryRow(ctx, query, connectionID)

	var serverVersion string
	var serverVersionNum int
	var systemIdentifier int64
	var clusterName *string
	var dataDirectory string
	var maxConnections int
	var maxWalSenders int
	var maxReplicationSlots int
	var installedExtensions []string

	err = row.Scan(
		&serverVersion,
		&serverVersionNum,
		&systemIdentifier,
		&clusterName,
		&dataDirectory,
		&maxConnections,
		&maxWalSenders,
		&maxReplicationSlots,
		&installedExtensions,
	)
	if err != nil {
		// If no rows found, this is the first collection
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Infof("No previous pg_server_info data found for connection %d", connectionID)
			return true, nil
		}
		return false, fmt.Errorf("failed to query most recent data: %w", err)
	}

	// Build stored metrics for comparison
	storedMetric := map[string]any{
		"server_version":        serverVersion,
		"server_version_num":    serverVersionNum,
		"system_identifier":     systemIdentifier,
		"cluster_name":          clusterName,
		"data_directory":        dataDirectory,
		"max_connections":       maxConnections,
		"max_wal_senders":       maxWalSenders,
		"max_replication_slots": maxReplicationSlots,
		"installed_extensions":  installedExtensions,
	}
	storedMetrics := []map[string]any{storedMetric}

	// Compute hash of stored metrics
	storedHash, err := p.computeMetricsHash(storedMetrics)
	if err != nil {
		return false, fmt.Errorf("failed to compute stored metrics hash: %w", err)
	}

	// Compare hashes
	return currentHash != storedHash, nil
}

// computeMetricsHash computes a hash of the metrics for comparison
func (p *PgServerInfoProbe) computeMetricsHash(metrics []map[string]any) (string, error) {
	return ComputeMetricsHash(metrics)
}
