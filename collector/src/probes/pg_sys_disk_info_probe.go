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

// PgSysDiskInfoProbe collects metrics from pg_sys_disk_info() function
// Note: This function is provided by the system_stats extension
type PgSysDiskInfoProbe struct {
	BaseMetricsProbe
}

// NewPgSysDiskInfoProbe creates a new pg_sys_disk_info probe
func NewPgSysDiskInfoProbe(config *ProbeConfig) *PgSysDiskInfoProbe {
	return &PgSysDiskInfoProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetExtensionName returns the required extension name
func (p *PgSysDiskInfoProbe) GetExtensionName() string {
	return "system_stats"
}

// GetQuery returns the SQL query to execute
func (p *PgSysDiskInfoProbe) GetQuery() string {
	return `
        SELECT
            mount_point,
            file_system,
            drive_letter,
            drive_type,
            file_system_type,
            total_space,
            used_space,
            free_space,
            total_inodes,
            used_inodes,
            free_inodes
        FROM pg_sys_disk_info()
    `
}

// Execute runs the probe against a monitored connection
func (p *PgSysDiskInfoProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]any, error) {
	// Check if system_stats extension is installed
	exists, err := CheckExtensionExists(ctx, connectionName, monitoredConn, "system_stats")
	if err != nil {
		return nil, fmt.Errorf("failed to check for system_stats extension: %w", err)
	}
	if !exists {
		// Extension not installed, return empty result set without error
		return nil, nil
	}

	query := WrapQuery(ProbeNamePgSysDiskInfo, p.GetQuery())
	rows, err := monitoredConn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgSysDiskInfoProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]any) error {
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
		"mount_point", "file_system", "drive_letter", "drive_type",
		"file_system_type", "total_space", "used_space", "free_space",
		"total_inodes", "used_inodes", "free_inodes",
	}

	// Build values array
	var values [][]any
	for _, metric := range metrics {
		row := []any{
			connectionID,
			timestamp,
			metric["mount_point"],
			metric["file_system"],
			metric["drive_letter"],
			metric["drive_type"],
			metric["file_system_type"],
			metric["total_space"],
			metric["used_space"],
			metric["free_space"],
			metric["total_inodes"],
			metric["used_inodes"],
			metric["free_inodes"],
		}
		values = append(values, row)
	}

	// Store metrics
	if err := StoreMetrics(ctx, datastoreConn, p.GetTableName(), columns, values); err != nil {
		return fmt.Errorf("failed to store metrics: %w", err)
	}

	return nil
}
