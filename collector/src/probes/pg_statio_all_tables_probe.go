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
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/collector/src/utils"

	"fmt"
	"time"
)

// PgStatioAllTablesProbe collects metrics from pg_statio_all_tables view
type PgStatioAllTablesProbe struct {
	BaseMetricsProbe
}

// NewPgStatioAllTablesProbe creates a new pg_statio_all_tables probe
func NewPgStatioAllTablesProbe(config *ProbeConfig) *PgStatioAllTablesProbe {
	return &PgStatioAllTablesProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetName returns the probe name
func (p *PgStatioAllTablesProbe) GetName() string {
	return ProbeNamePgStatioAllTables
}

// GetTableName returns the metrics table name
func (p *PgStatioAllTablesProbe) GetTableName() string {
	return ProbeNamePgStatioAllTables
}

// IsDatabaseScoped returns true as pg_statio_all_tables is database-scoped
func (p *PgStatioAllTablesProbe) IsDatabaseScoped() bool {
	return true
}

// GetQuery returns the SQL query to execute
func (p *PgStatioAllTablesProbe) GetQuery() string {
	return `
        SELECT
            relid,
            schemaname,
            relname,
            heap_blks_read,
            heap_blks_hit,
            idx_blks_read,
            idx_blks_hit,
            toast_blks_read,
            toast_blks_hit,
            tidx_blks_read,
            tidx_blks_hit
        FROM pg_statio_all_tables
        ORDER BY schemaname, relname
    `
}

// Execute runs the probe against a monitored connection
func (p *PgStatioAllTablesProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]interface{}, error) {
	rows, err := monitoredConn.Query(ctx, p.GetQuery())
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgStatioAllTablesProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
	if len(metrics) == 0 {
		return nil // Nothing to store
	}

	// Ensure partition exists for this timestamp
	if err := p.EnsurePartition(ctx, datastoreConn, timestamp); err != nil {
		return fmt.Errorf("failed to ensure partition: %w", err)
	}

	// Define columns in order
	columns := []string{
		"connection_id", "collected_at", "database_name",
		"relid", "schemaname", "relname",
		"heap_blks_read", "heap_blks_hit",
		"idx_blks_read", "idx_blks_hit",
		"toast_blks_read", "toast_blks_hit",
		"tidx_blks_read", "tidx_blks_hit",
	}

	// Build values array
	var values [][]interface{}
	for _, metric := range metrics {
		// Extract database_name from the metric (set by scheduler)
		databaseName, ok := metric["_database_name"]
		if !ok {
			return fmt.Errorf("database_name not found in metrics")
		}

		row := []interface{}{
			connectionID,
			timestamp,
			databaseName,
			metric["relid"],
			metric["schemaname"],
			metric["relname"],
			metric["heap_blks_read"],
			metric["heap_blks_hit"],
			metric["idx_blks_read"],
			metric["idx_blks_hit"],
			metric["toast_blks_read"],
			metric["toast_blks_hit"],
			metric["tidx_blks_read"],
			metric["tidx_blks_hit"],
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
func (p *PgStatioAllTablesProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
