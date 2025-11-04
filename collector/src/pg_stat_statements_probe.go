/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// PgStatStatementsProbe collects metrics from pg_stat_statements extension
type PgStatStatementsProbe struct {
	BaseMetricsProbe
}

// NewPgStatStatementsProbe creates a new pg_stat_statements probe
func NewPgStatStatementsProbe(config *ProbeConfig) *PgStatStatementsProbe {
	return &PgStatStatementsProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetName returns the probe name
func (p *PgStatStatementsProbe) GetName() string {
	return ProbeNamePgStatStatements
}

// GetTableName returns the metrics table name
func (p *PgStatStatementsProbe) GetTableName() string {
	return ProbeNamePgStatStatements
}

// IsDatabaseScoped returns true as pg_stat_statements is database-scoped
func (p *PgStatStatementsProbe) IsDatabaseScoped() bool {
	return true
}

// GetQuery returns the SQL query to execute
func (p *PgStatStatementsProbe) GetQuery() string {
	return fmt.Sprintf(`
        SELECT
            userid,
            dbid,
            queryid,
            query,
            calls,
            total_exec_time,
            mean_exec_time,
            min_exec_time,
            max_exec_time,
            stddev_exec_time,
            rows,
            shared_blks_hit,
            shared_blks_read,
            shared_blks_dirtied,
            shared_blks_written,
            local_blks_hit,
            local_blks_read,
            local_blks_dirtied,
            local_blks_written,
            temp_blks_read,
            temp_blks_written,
            blk_read_time,
            blk_write_time
        FROM pg_stat_statements
        ORDER BY total_exec_time DESC
        LIMIT %d
    `, PgStatStatementsQueryLimit)
}

// checkExtensionAvailable checks if pg_stat_statements extension is installed
func (p *PgStatStatementsProbe) checkExtensionAvailable(ctx context.Context, db *sql.DB) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx, `
        SELECT EXISTS(
            SELECT 1
            FROM pg_extension
            WHERE extname = 'pg_stat_statements'
        )
    `).Scan(&exists)

	if err != nil {
		return false, fmt.Errorf("failed to check for pg_stat_statements extension: %w", err)
	}

	return exists, nil
}

// Execute runs the probe against a monitored connection
func (p *PgStatStatementsProbe) Execute(ctx context.Context, monitoredDB *sql.DB) ([]map[string]interface{}, error) {
	// Check if extension is available
	available, err := p.checkExtensionAvailable(ctx, monitoredDB)
	if err != nil {
		return nil, err
	}

	if !available {
		// Extension not available, return empty metrics (not an error)
		return []map[string]interface{}{}, nil
	}

	rows, err := monitoredDB.QueryContext(ctx, p.GetQuery())
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer closeRows(rows)

	return scanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgStatStatementsProbe) Store(ctx context.Context, datastoreDB *sql.DB, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
	if len(metrics) == 0 {
		return nil // Nothing to store
	}

	// Ensure partition exists for this timestamp
	if err := p.EnsurePartition(ctx, datastoreDB, timestamp); err != nil {
		return fmt.Errorf("failed to ensure partition: %w", err)
	}

	// Define columns in order
	columns := []string{
		"connection_id", "collected_at", "database_name",
		"userid", "dbid", "queryid", "query",
		"calls", "total_exec_time", "mean_exec_time", "min_exec_time", "max_exec_time", "stddev_exec_time",
		"rows",
		"shared_blks_hit", "shared_blks_read", "shared_blks_dirtied", "shared_blks_written",
		"local_blks_hit", "local_blks_read", "local_blks_dirtied", "local_blks_written",
		"temp_blks_read", "temp_blks_written",
		"blk_read_time", "blk_write_time",
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
			metric["userid"],
			metric["dbid"],
			metric["queryid"],
			metric["query"],
			metric["calls"],
			metric["total_exec_time"],
			metric["mean_exec_time"],
			metric["min_exec_time"],
			metric["max_exec_time"],
			metric["stddev_exec_time"],
			metric["rows"],
			metric["shared_blks_hit"],
			metric["shared_blks_read"],
			metric["shared_blks_dirtied"],
			metric["shared_blks_written"],
			metric["local_blks_hit"],
			metric["local_blks_read"],
			metric["local_blks_dirtied"],
			metric["local_blks_written"],
			metric["temp_blks_read"],
			metric["temp_blks_written"],
			metric["blk_read_time"],
			metric["blk_write_time"],
		}
		values = append(values, row)
	}

	// Use COPY protocol to store metrics
	if err := StoreMetricsWithCopy(ctx, datastoreDB, p.GetTableName(), columns, values); err != nil {
		return fmt.Errorf("failed to store metrics: %w", err)
	}

	return nil
}

// EnsurePartition ensures a partition exists for the given timestamp
func (p *PgStatStatementsProbe) EnsurePartition(ctx context.Context, datastoreDB *sql.DB, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreDB, p.GetTableName(), timestamp)
}
