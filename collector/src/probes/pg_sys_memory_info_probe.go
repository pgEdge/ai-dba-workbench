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

// PgSysMemoryInfoProbe collects metrics from pg_sys_memory_info() function
// Note: This function is provided by the system_stats extension
type PgSysMemoryInfoProbe struct {
	BaseMetricsProbe
}

// NewPgSysMemoryInfoProbe creates a new pg_sys_memory_info probe
func NewPgSysMemoryInfoProbe(config *ProbeConfig) *PgSysMemoryInfoProbe {
	return &PgSysMemoryInfoProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetExtensionName returns the required extension name
func (p *PgSysMemoryInfoProbe) GetExtensionName() string {
	return "system_stats"
}

// GetQuery returns the SQL query to execute
func (p *PgSysMemoryInfoProbe) GetQuery() string {
	return `
        SELECT
            total_memory,
            used_memory,
            free_memory,
            swap_total,
            swap_used,
            swap_free,
            cache_total,
            kernel_total,
            kernel_paged,
            kernel_non_paged,
            total_page_file,
            avail_page_file
        FROM pg_sys_memory_info()
    `
}

// Execute runs the probe against a monitored connection
func (p *PgSysMemoryInfoProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]any, error) {
	// Check if system_stats extension is installed
	exists, err := CheckExtensionExists(ctx, connectionName, monitoredConn, "system_stats")
	if err != nil {
		return nil, fmt.Errorf("failed to check for system_stats extension: %w", err)
	}
	if !exists {
		// Extension not installed, return empty result set without error
		return nil, nil
	}

	query := WrapQuery(ProbeNamePgSysMemoryInfo, p.GetQuery())
	rows, err := monitoredConn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgSysMemoryInfoProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]any) error {
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
		"total_memory", "used_memory", "free_memory",
		"swap_total", "swap_used", "swap_free",
		"cache_total", "kernel_total", "kernel_paged",
		"kernel_non_paged", "total_page_file", "avail_page_file",
	}

	// Build values array
	var values [][]any
	for _, metric := range metrics {
		row := []any{
			connectionID,
			timestamp,
			metric["total_memory"],
			metric["used_memory"],
			metric["free_memory"],
			metric["swap_total"],
			metric["swap_used"],
			metric["swap_free"],
			metric["cache_total"],
			metric["kernel_total"],
			metric["kernel_paged"],
			metric["kernel_non_paged"],
			metric["total_page_file"],
			metric["avail_page_file"],
		}
		values = append(values, row)
	}

	// Store metrics
	if err := StoreMetrics(ctx, datastoreConn, p.GetTableName(), columns, values); err != nil {
		return fmt.Errorf("failed to store metrics: %w", err)
	}

	return nil
}
