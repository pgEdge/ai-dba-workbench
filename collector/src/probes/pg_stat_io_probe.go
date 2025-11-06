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

// PgStatIOProbe collects metrics from pg_stat_io view
// Note: This view is available in PostgreSQL 16+ and returns multiple rows
type PgStatIOProbe struct {
	BaseMetricsProbe
}

// NewPgStatIOProbe creates a new pg_stat_io probe
func NewPgStatIOProbe(config *ProbeConfig) *PgStatIOProbe {
	return &PgStatIOProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetName returns the probe name
func (p *PgStatIOProbe) GetName() string {
	return ProbeNamePgStatIO
}

// GetTableName returns the metrics table name
func (p *PgStatIOProbe) GetTableName() string {
	return ProbeNamePgStatIO
}

// IsDatabaseScoped returns false as pg_stat_io is server-scoped
func (p *PgStatIOProbe) IsDatabaseScoped() bool {
	return false
}

// GetQuery returns the SQL query to execute
func (p *PgStatIOProbe) GetQuery() string {
	return `
        SELECT
            backend_type,
            object,
            context,
            reads,
            read_time,
            writes,
            write_time,
            writebacks,
            writeback_time,
            extends,
            extend_time,
            op_bytes,
            hits,
            evictions,
            reuses,
            fsyncs,
            fsync_time,
            stats_reset
        FROM pg_stat_io
    `
}

// Execute runs the probe against a monitored connection
func (p *PgStatIOProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn) ([]map[string]interface{}, error) {
	// First check if the view exists (PG 16+)
	var exists bool
	err := monitoredConn.QueryRow(ctx, `
        SELECT EXISTS (
            SELECT 1 FROM pg_views
            WHERE schemaname = 'pg_catalog'
            AND viewname = 'pg_stat_io'
        )
    `).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("failed to check if pg_stat_io exists: %w", err)
	}

	if !exists {
		return nil, nil // View doesn't exist, return empty result
	}

	rows, err := monitoredConn.Query(ctx, p.GetQuery())
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgStatIOProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
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
		"backend_type", "object", "context",
		"reads", "read_time", "writes", "write_time",
		"writebacks", "writeback_time", "extends", "extend_time",
		"op_bytes", "hits", "evictions", "reuses",
		"fsyncs", "fsync_time", "stats_reset",
	}

	// Build values array
	var values [][]interface{}
	for _, metric := range metrics {
		row := []interface{}{
			connectionID,
			timestamp,
			metric["backend_type"],
			metric["object"],
			metric["context"],
			metric["reads"],
			metric["read_time"],
			metric["writes"],
			metric["write_time"],
			metric["writebacks"],
			metric["writeback_time"],
			metric["extends"],
			metric["extend_time"],
			metric["op_bytes"],
			metric["hits"],
			metric["evictions"],
			metric["reuses"],
			metric["fsyncs"],
			metric["fsync_time"],
			metric["stats_reset"],
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
func (p *PgStatIOProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
