/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
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

// PgStatioAllIndexesProbe collects metrics from pg_statio_all_indexes view
type PgStatioAllIndexesProbe struct {
	BaseMetricsProbe
}

// NewPgStatioAllIndexesProbe creates a new pg_statio_all_indexes probe
func NewPgStatioAllIndexesProbe(config *ProbeConfig) *PgStatioAllIndexesProbe {
	return &PgStatioAllIndexesProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetName returns the probe name
func (p *PgStatioAllIndexesProbe) GetName() string {
	return ProbeNamePgStatioAllIndexes
}

// GetTableName returns the metrics table name
func (p *PgStatioAllIndexesProbe) GetTableName() string {
	return ProbeNamePgStatioAllIndexes
}

// IsDatabaseScoped returns true as pg_statio_all_indexes is database-scoped
func (p *PgStatioAllIndexesProbe) IsDatabaseScoped() bool {
	return true
}

// GetQuery returns the SQL query to execute
func (p *PgStatioAllIndexesProbe) GetQuery() string {
	return `
        SELECT
            relid,
            indexrelid,
            schemaname,
            relname,
            indexrelname,
            idx_blks_read,
            idx_blks_hit
        FROM pg_statio_all_indexes
        ORDER BY schemaname, relname, indexrelname
    `
}

// Execute runs the probe against a monitored connection
func (p *PgStatioAllIndexesProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn) ([]map[string]interface{}, error) {
	rows, err := monitoredConn.Query(ctx, p.GetQuery())
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgStatioAllIndexesProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
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
		"relid", "indexrelid", "schemaname", "relname", "indexrelname",
		"idx_blks_read", "idx_blks_hit",
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
			metric["indexrelid"],
			metric["schemaname"],
			metric["relname"],
			metric["indexrelname"],
			metric["idx_blks_read"],
			metric["idx_blks_hit"],
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
func (p *PgStatioAllIndexesProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
