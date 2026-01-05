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

// PgSysOsInfoProbe collects metrics from pg_sys_os_info() function
// Note: This function is provided by the system_stats extension
type PgSysOsInfoProbe struct {
	BaseMetricsProbe
}

// NewPgSysOsInfoProbe creates a new pg_sys_os_info probe
func NewPgSysOsInfoProbe(config *ProbeConfig) *PgSysOsInfoProbe {
	return &PgSysOsInfoProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetName returns the probe name
func (p *PgSysOsInfoProbe) GetName() string {
	return ProbeNamePgSysOsInfo
}

// GetTableName returns the metrics table name
func (p *PgSysOsInfoProbe) GetTableName() string {
	return ProbeNamePgSysOsInfo
}

// IsDatabaseScoped returns false as pg_sys_os_info is server-scoped
func (p *PgSysOsInfoProbe) IsDatabaseScoped() bool {
	return false
}

// GetQuery returns the SQL query to execute
func (p *PgSysOsInfoProbe) GetQuery() string {
	return `
        SELECT
            name,
            version,
            host_name,
            domain_name,
            handle_count,
            process_count,
            thread_count,
            architecture,
            last_bootup_time,
            os_up_since_seconds
        FROM pg_sys_os_info()
    `
}

// Execute runs the probe against a monitored connection
func (p *PgSysOsInfoProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]interface{}, error) {
	// Check if system_stats extension is installed
	exists, err := CheckExtensionExists(ctx, connectionName, monitoredConn, "system_stats")
	if err != nil {
		return nil, fmt.Errorf("failed to check for system_stats extension: %w", err)
	}
	if !exists {
		// Extension not installed, return empty result set without error
		return nil, nil
	}

	rows, err := monitoredConn.Query(ctx, p.GetQuery())
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgSysOsInfoProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
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
		"name", "version", "host_name", "domain_name",
		"handle_count", "process_count", "thread_count",
		"architecture", "last_bootup_time", "os_up_since_seconds",
	}

	// Build values array
	var values [][]interface{}
	for _, metric := range metrics {
		row := []interface{}{
			connectionID,
			timestamp,
			metric["name"],
			metric["version"],
			metric["host_name"],
			metric["domain_name"],
			metric["handle_count"],
			metric["process_count"],
			metric["thread_count"],
			metric["architecture"],
			metric["last_bootup_time"],
			metric["os_up_since_seconds"],
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
func (p *PgSysOsInfoProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
