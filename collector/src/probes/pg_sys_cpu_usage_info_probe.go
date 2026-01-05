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

// PgSysCPUUsageInfoProbe collects metrics from pg_sys_cpu_usage_info() function
// Note: This function is provided by the system_stats extension
type PgSysCPUUsageInfoProbe struct {
	BaseMetricsProbe
}

// NewPgSysCPUUsageInfoProbe creates a new pg_sys_cpu_usage_info probe
func NewPgSysCPUUsageInfoProbe(config *ProbeConfig) *PgSysCPUUsageInfoProbe {
	return &PgSysCPUUsageInfoProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetName returns the probe name
func (p *PgSysCPUUsageInfoProbe) GetName() string {
	return ProbeNamePgSysCPUUsageInfo
}

// GetTableName returns the metrics table name
func (p *PgSysCPUUsageInfoProbe) GetTableName() string {
	return ProbeNamePgSysCPUUsageInfo
}

// IsDatabaseScoped returns false as pg_sys_cpu_usage_info is server-scoped
func (p *PgSysCPUUsageInfoProbe) IsDatabaseScoped() bool {
	return false
}

// GetQuery returns the SQL query to execute
func (p *PgSysCPUUsageInfoProbe) GetQuery() string {
	return `
        SELECT
            usermode_normal_process_percent,
            usermode_niced_process_percent,
            kernelmode_process_percent,
            idle_mode_percent,
            IO_completion_percent,
            servicing_irq_percent,
            servicing_softirq_percent,
            user_time_percent,
            processor_time_percent,
            privileged_time_percent,
            interrupt_time_percent
        FROM pg_sys_cpu_usage_info()
    `
}

// Execute runs the probe against a monitored connection
func (p *PgSysCPUUsageInfoProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]interface{}, error) {
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
func (p *PgSysCPUUsageInfoProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
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
		"usermode_normal_process_percent", "usermode_niced_process_percent",
		"kernelmode_process_percent", "idle_mode_percent",
		"IO_completion_percent", "servicing_irq_percent",
		"servicing_softirq_percent", "user_time_percent",
		"processor_time_percent", "privileged_time_percent",
		"interrupt_time_percent",
	}

	// Build values array
	var values [][]interface{}
	for _, metric := range metrics {
		row := []interface{}{
			connectionID,
			timestamp,
			metric["usermode_normal_process_percent"],
			metric["usermode_niced_process_percent"],
			metric["kernelmode_process_percent"],
			metric["idle_mode_percent"],
			metric["IO_completion_percent"],
			metric["servicing_irq_percent"],
			metric["servicing_softirq_percent"],
			metric["user_time_percent"],
			metric["processor_time_percent"],
			metric["privileged_time_percent"],
			metric["interrupt_time_percent"],
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
func (p *PgSysCPUUsageInfoProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
