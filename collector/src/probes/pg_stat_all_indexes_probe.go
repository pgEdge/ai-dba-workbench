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
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/collector/src/utils"
	"context"
	
	"fmt"
	"time"
)

// PgStatAllIndexesProbe collects metrics from pg_stat_all_indexes view
type PgStatAllIndexesProbe struct {
	BaseMetricsProbe
}

// NewPgStatAllIndexesProbe creates a new pg_stat_all_indexes probe
func NewPgStatAllIndexesProbe(config *ProbeConfig) *PgStatAllIndexesProbe {
	return &PgStatAllIndexesProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetName returns the probe name
func (p *PgStatAllIndexesProbe) GetName() string {
	return ProbeNamePgStatAllIndexes
}

// GetTableName returns the metrics table name
func (p *PgStatAllIndexesProbe) GetTableName() string {
	return ProbeNamePgStatAllIndexes
}

// IsDatabaseScoped returns true as pg_stat_all_indexes is database-scoped
func (p *PgStatAllIndexesProbe) IsDatabaseScoped() bool {
	return true
}

// GetQuery returns the SQL query to execute
func (p *PgStatAllIndexesProbe) GetQuery() string {
	return `
        SELECT
            relid,
            indexrelid,
            schemaname,
            relname,
            indexrelname,
            idx_scan,
            last_idx_scan,
            idx_tup_read,
            idx_tup_fetch
        FROM pg_stat_all_indexes
        ORDER BY schemaname, relname, indexrelname
    `
}

// Execute runs the probe against a monitored connection
func (p *PgStatAllIndexesProbe) Execute(ctx context.Context, monitoredConn *pgxpool.Conn) ([]map[string]interface{}, error) {
	rows, err := monitoredConn.Query(ctx, p.GetQuery())
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgStatAllIndexesProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
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
		"idx_scan", "last_idx_scan", "idx_tup_read", "idx_tup_fetch",
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
			metric["idx_scan"],
			metric["last_idx_scan"],
			metric["idx_tup_read"],
			metric["idx_tup_fetch"],
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
func (p *PgStatAllIndexesProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
