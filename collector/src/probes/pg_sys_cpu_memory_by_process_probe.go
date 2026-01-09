/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
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

// PgSysCPUMemoryByProcessProbe collects metrics from pg_sys_cpu_memory_by_process() function
// Note: This function is provided by the system_stats extension
type PgSysCPUMemoryByProcessProbe struct {
	BaseMetricsProbe
}

// NewPgSysCPUMemoryByProcessProbe creates a new pg_sys_cpu_memory_by_process probe
func NewPgSysCPUMemoryByProcessProbe(config *ProbeConfig) *PgSysCPUMemoryByProcessProbe {
	return &PgSysCPUMemoryByProcessProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetName returns the probe name
func (p *PgSysCPUMemoryByProcessProbe) GetName() string {
	return ProbeNamePgSysCPUMemoryByProcess
}

// GetTableName returns the metrics table name
func (p *PgSysCPUMemoryByProcessProbe) GetTableName() string {
	return ProbeNamePgSysCPUMemoryByProcess
}

// IsDatabaseScoped returns false as pg_sys_cpu_memory_by_process is server-scoped
func (p *PgSysCPUMemoryByProcessProbe) IsDatabaseScoped() bool {
	return false
}

// GetQuery returns the SQL query to execute
func (p *PgSysCPUMemoryByProcessProbe) GetQuery() string {
	return `
        SELECT
            pid,
            name,
            running_since_seconds,
            cpu_usage,
            memory_usage,
            memory_bytes
        FROM pg_sys_cpu_memory_by_process()
    `
}

// Execute runs the probe against a monitored connection
func (p *PgSysCPUMemoryByProcessProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]interface{}, error) {
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
func (p *PgSysCPUMemoryByProcessProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
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
		"pid", "name", "running_since_seconds",
		"cpu_usage", "memory_usage", "memory_bytes",
	}

	// Build values array
	var values [][]interface{}
	for _, metric := range metrics {
		row := []interface{}{
			connectionID,
			timestamp,
			metric["pid"],
			metric["name"],
			metric["running_since_seconds"],
			metric["cpu_usage"],
			metric["memory_usage"],
			metric["memory_bytes"],
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
func (p *PgSysCPUMemoryByProcessProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
