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
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/collector/src/utils"
)

// PgSysNetworkInfoProbe collects metrics from pg_sys_network_info() function
// Note: This function is provided by the system_stats extension
type PgSysNetworkInfoProbe struct {
	BaseMetricsProbe
}

// NewPgSysNetworkInfoProbe creates a new pg_sys_network_info probe
func NewPgSysNetworkInfoProbe(config *ProbeConfig) *PgSysNetworkInfoProbe {
	return &PgSysNetworkInfoProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetExtensionName returns the required extension name
func (p *PgSysNetworkInfoProbe) GetExtensionName() string {
	return "system_stats"
}

// GetQuery returns the SQL query to execute
func (p *PgSysNetworkInfoProbe) GetQuery() string {
	return `
        SELECT
            interface_name,
            ip_address,
            tx_bytes,
            tx_packets,
            tx_errors,
            tx_dropped,
            rx_bytes,
            rx_packets,
            rx_errors,
            rx_dropped,
            link_speed_mbps
        FROM pg_sys_network_info()
    `
}

// Execute runs the probe against a monitored connection
func (p *PgSysNetworkInfoProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]interface{}, error) {
	// Check if system_stats extension is installed
	exists, err := CheckExtensionExists(ctx, connectionName, monitoredConn, "system_stats")
	if err != nil {
		return nil, fmt.Errorf("failed to check for system_stats extension: %w", err)
	}
	if !exists {
		// Extension not installed, return empty result set without error
		return nil, nil
	}

	query := WrapQuery(ProbeNamePgSysNetworkInfo, p.GetQuery())
	rows, err := monitoredConn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgSysNetworkInfoProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
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
		"interface_name", "ip_address", "tx_bytes", "tx_packets",
		"tx_errors", "tx_dropped", "rx_bytes", "rx_packets",
		"rx_errors", "rx_dropped", "link_speed_mbps",
	}

	// Build values array
	var values [][]interface{}
	for _, metric := range metrics {
		row := []interface{}{
			connectionID,
			timestamp,
			metric["interface_name"],
			metric["ip_address"],
			metric["tx_bytes"],
			metric["tx_packets"],
			metric["tx_errors"],
			metric["tx_dropped"],
			metric["rx_bytes"],
			metric["rx_packets"],
			metric["rx_errors"],
			metric["rx_dropped"],
			metric["link_speed_mbps"],
		}
		values = append(values, row)
	}

	// Use COPY protocol to store metrics
	if err := StoreMetricsWithCopy(ctx, datastoreConn, p.GetTableName(), columns, values); err != nil {
		return fmt.Errorf("failed to store metrics: %w", err)
	}

	return nil
}

// EnsurePartition ensures a partition exists for the given timestamp
func (p *PgSysNetworkInfoProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
