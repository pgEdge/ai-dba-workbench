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

// PgSysCPUInfoProbe collects metrics from pg_sys_cpu_info() function
// Note: This function is provided by the system_stats extension
type PgSysCPUInfoProbe struct {
	BaseMetricsProbe
}

// NewPgSysCPUInfoProbe creates a new pg_sys_cpu_info probe
func NewPgSysCPUInfoProbe(config *ProbeConfig) *PgSysCPUInfoProbe {
	return &PgSysCPUInfoProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetExtensionName returns the required extension name
func (p *PgSysCPUInfoProbe) GetExtensionName() string {
	return "system_stats"
}

// GetQuery returns the SQL query to execute
func (p *PgSysCPUInfoProbe) GetQuery() string {
	return `
        SELECT
            vendor,
            description,
            model_name,
            processor_type,
            logical_processor,
            physical_processor,
            no_of_cores,
            architecture,
            clock_speed_hz,
            cpu_type,
            cpu_family,
            byte_order,
            l1dcache_size,
            l1icache_size,
            l2cache_size,
            l3cache_size
        FROM pg_sys_cpu_info()
    `
}

// Execute runs the probe against a monitored connection
func (p *PgSysCPUInfoProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]any, error) {
	// Check if system_stats extension is installed
	exists, err := CheckExtensionExists(ctx, connectionName, monitoredConn, "system_stats")
	if err != nil {
		return nil, fmt.Errorf("failed to check for system_stats extension: %w", err)
	}
	if !exists {
		// Extension not installed, return empty result set without error
		return nil, nil
	}

	query := WrapQuery(ProbeNamePgSysCPUInfo, p.GetQuery())
	rows, err := monitoredConn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgSysCPUInfoProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]any) error {
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
		"vendor", "description", "model_name", "processor_type",
		"logical_processor", "physical_processor", "no_of_cores",
		"architecture", "clock_speed_hz", "cpu_type", "cpu_family",
		"byte_order", "l1dcache_size", "l1icache_size", "l2cache_size",
		"l3cache_size",
	}

	// Build values array
	var values [][]any
	for _, metric := range metrics {
		row := []any{
			connectionID,
			timestamp,
			metric["vendor"],
			metric["description"],
			metric["model_name"],
			metric["processor_type"],
			metric["logical_processor"],
			metric["physical_processor"],
			metric["no_of_cores"],
			metric["architecture"],
			metric["clock_speed_hz"],
			metric["cpu_type"],
			metric["cpu_family"],
			metric["byte_order"],
			metric["l1dcache_size"],
			metric["l1icache_size"],
			metric["l2cache_size"],
			metric["l3cache_size"],
		}
		values = append(values, row)
	}

	// Use COPY protocol to store metrics
	if err := StoreMetricsWithCopy(ctx, datastoreConn, p.GetTableName(), columns, values); err != nil {
		return fmt.Errorf("failed to store metrics: %w", err)
	}

	return nil
}
