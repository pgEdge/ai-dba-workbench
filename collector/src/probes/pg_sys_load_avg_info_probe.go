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

// PgSysLoadAvgInfoProbe collects metrics from pg_sys_load_avg_info() function
// Note: This function is provided by the system_stats extension
type PgSysLoadAvgInfoProbe struct {
	BaseMetricsProbe
}

// NewPgSysLoadAvgInfoProbe creates a new pg_sys_load_avg_info probe
func NewPgSysLoadAvgInfoProbe(config *ProbeConfig) *PgSysLoadAvgInfoProbe {
	return &PgSysLoadAvgInfoProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetName returns the probe name
func (p *PgSysLoadAvgInfoProbe) GetName() string {
	return ProbeNamePgSysLoadAvgInfo
}

func (p *PgSysLoadAvgInfoProbe) GetExtensionName() string {
	return "system_stats"
}

// GetTableName returns the metrics table name
func (p *PgSysLoadAvgInfoProbe) GetTableName() string {
	return ProbeNamePgSysLoadAvgInfo
}

// IsDatabaseScoped returns false as pg_sys_load_avg_info is server-scoped
func (p *PgSysLoadAvgInfoProbe) IsDatabaseScoped() bool {
	return false
}

// GetQuery returns the SQL query to execute
func (p *PgSysLoadAvgInfoProbe) GetQuery() string {
	return `
        SELECT
            load_avg_one_minute,
            load_avg_five_minutes,
            load_avg_ten_minutes,
            load_avg_fifteen_minutes
        FROM pg_sys_load_avg_info()
    `
}

// Execute runs the probe against a monitored connection
func (p *PgSysLoadAvgInfoProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]interface{}, error) {
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
func (p *PgSysLoadAvgInfoProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
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
		"load_avg_one_minute", "load_avg_five_minutes",
		"load_avg_ten_minutes", "load_avg_fifteen_minutes",
	}

	// Build values array
	var values [][]interface{}
	for _, metric := range metrics {
		row := []interface{}{
			connectionID,
			timestamp,
			metric["load_avg_one_minute"],
			metric["load_avg_five_minutes"],
			metric["load_avg_ten_minutes"],
			metric["load_avg_fifteen_minutes"],
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
func (p *PgSysLoadAvgInfoProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
