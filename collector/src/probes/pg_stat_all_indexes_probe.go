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

// PgStatAllIndexesProbe collects metrics from pg_stat_all_indexes view
// and joins with pg_statio_all_indexes for I/O statistics
type PgStatAllIndexesProbe struct {
	BaseMetricsProbe
}

// NewPgStatAllIndexesProbe creates a new pg_stat_all_indexes probe
func NewPgStatAllIndexesProbe(config *ProbeConfig) *PgStatAllIndexesProbe {
	return &PgStatAllIndexesProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config, databaseScoped: true},
	}
}

// GetQuery returns the SQL query to execute (default for PG16+)
func (p *PgStatAllIndexesProbe) GetQuery() string {
	return p.GetQueryForVersion(16)
}

// GetQueryForVersion returns the appropriate SQL query for the given PostgreSQL version
func (p *PgStatAllIndexesProbe) GetQueryForVersion(pgVersion int) string {
	if pgVersion >= 16 {
		// PG16+ has last_idx_scan column
		return `
            SELECT
                s.relid,
                s.indexrelid,
                s.schemaname,
                s.relname,
                s.indexrelname,
                s.idx_scan,
                s.last_idx_scan,
                s.idx_tup_read,
                s.idx_tup_fetch,
                io.idx_blks_read,
                io.idx_blks_hit
            FROM pg_stat_all_indexes s
            LEFT JOIN pg_statio_all_indexes io ON s.indexrelid = io.indexrelid
            ORDER BY s.schemaname, s.relname, s.indexrelname
        `
	}
	// PG14-15: last_idx_scan doesn't exist, return NULL
	return `
        SELECT
            s.relid,
            s.indexrelid,
            s.schemaname,
            s.relname,
            s.indexrelname,
            s.idx_scan,
            NULL::timestamptz AS last_idx_scan,
            s.idx_tup_read,
            s.idx_tup_fetch,
            io.idx_blks_read,
            io.idx_blks_hit
        FROM pg_stat_all_indexes s
        LEFT JOIN pg_statio_all_indexes io ON s.indexrelid = io.indexrelid
        ORDER BY s.schemaname, s.relname, s.indexrelname
    `
}

// Execute runs the probe against a monitored connection
func (p *PgStatAllIndexesProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]any, error) {
	query := WrapQuery(ProbeNamePgStatAllIndexes, p.GetQueryForVersion(pgVersion))
	rows, err := monitoredConn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgStatAllIndexesProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]any) error {
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
		"idx_blks_read", "idx_blks_hit",
	}

	// Build values array
	var values [][]any
	for _, metric := range metrics {
		// Extract database_name from the metric (set by scheduler)
		databaseName, ok := metric["_database_name"]
		if !ok {
			return fmt.Errorf("database_name not found in metrics")
		}

		row := []any{
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
func (p *PgStatAllIndexesProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
