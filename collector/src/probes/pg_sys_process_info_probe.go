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

// PgSysProcessInfoProbe collects metrics from pg_sys_process_info() function
// Note: This function is provided by the system_stats extension
type PgSysProcessInfoProbe struct {
	BaseMetricsProbe
}

// NewPgSysProcessInfoProbe creates a new pg_sys_process_info probe
func NewPgSysProcessInfoProbe(config *ProbeConfig) *PgSysProcessInfoProbe {
	return &PgSysProcessInfoProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetExtensionName returns the required extension name
func (p *PgSysProcessInfoProbe) GetExtensionName() string {
	return "system_stats"
}

// GetQuery returns the SQL query to execute
func (p *PgSysProcessInfoProbe) GetQuery() string {
	return `
        SELECT
            total_processes,
            running_processes,
            sleeping_processes,
            stopped_processes,
            zombie_processes
        FROM pg_sys_process_info()
    `
}

// Execute runs the probe against a monitored connection
func (p *PgSysProcessInfoProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]any, error) {
	// Check if system_stats extension is installed
	exists, err := CheckExtensionExists(ctx, connectionName, monitoredConn, "system_stats")
	if err != nil {
		return nil, fmt.Errorf("failed to check for system_stats extension: %w", err)
	}
	if !exists {
		// Extension not installed, return empty result set without error
		return nil, nil
	}

	query := WrapQuery(ProbeNamePgSysProcessInfo, p.GetQuery())
	rows, err := monitoredConn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgSysProcessInfoProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]any) error {
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
		"total_processes", "running_processes", "sleeping_processes",
		"stopped_processes", "zombie_processes",
	}

	// Build values array
	var values [][]any
	for _, metric := range metrics {
		row := []any{
			connectionID,
			timestamp,
			metric["total_processes"],
			metric["running_processes"],
			metric["sleeping_processes"],
			metric["stopped_processes"],
			metric["zombie_processes"],
		}
		values = append(values, row)
	}

	// Use COPY protocol to store metrics
	if err := StoreMetricsWithCopy(ctx, datastoreConn, p.GetTableName(), columns, values); err != nil {
		return fmt.Errorf("failed to store metrics: %w", err)
	}

	return nil
}
