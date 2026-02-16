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

// PgStatRecoveryPrefetchProbe collects metrics from pg_stat_recovery_prefetch view
type PgStatRecoveryPrefetchProbe struct {
	BaseMetricsProbe
}

// NewPgStatRecoveryPrefetchProbe creates a new pg_stat_recovery_prefetch probe
func NewPgStatRecoveryPrefetchProbe(config *ProbeConfig) *PgStatRecoveryPrefetchProbe {
	return &PgStatRecoveryPrefetchProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetName returns the probe name
func (p *PgStatRecoveryPrefetchProbe) GetName() string {
	return ProbeNamePgStatRecoveryPrefetch
}

// GetTableName returns the metrics table name
func (p *PgStatRecoveryPrefetchProbe) GetTableName() string {
	return ProbeNamePgStatRecoveryPrefetch
}

// IsDatabaseScoped returns false as pg_stat_recovery_prefetch is server-scoped
func (p *PgStatRecoveryPrefetchProbe) IsDatabaseScoped() bool {
	return false
}

// GetQuery returns the SQL query to execute
func (p *PgStatRecoveryPrefetchProbe) GetQuery() string {
	return `
        SELECT
            stats_reset,
            prefetch,
            hit,
            skip_init,
            skip_new,
            skip_fpw,
            skip_rep,
            wal_distance,
            block_distance,
            io_depth
        FROM pg_stat_recovery_prefetch
    `
}

// checkViewAvailable checks if pg_stat_recovery_prefetch view exists
// This view is only available in PostgreSQL 15+
func (p *PgStatRecoveryPrefetchProbe) checkViewAvailable(ctx context.Context, conn *pgxpool.Conn) (bool, error) {
	var exists bool
	err := conn.QueryRow(ctx, `
        SELECT EXISTS(
            SELECT 1
            FROM pg_catalog.pg_views
            WHERE schemaname = 'pg_catalog'
            AND viewname = 'pg_stat_recovery_prefetch'
        )
    `).Scan(&exists)

	if err != nil {
		return false, fmt.Errorf("failed to check for pg_stat_recovery_prefetch view: %w", err)
	}

	return exists, nil
}

// Execute runs the probe against a monitored connection
func (p *PgStatRecoveryPrefetchProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]interface{}, error) {
	// Check if view is available
	available, err := p.checkViewAvailable(ctx, monitoredConn)
	if err != nil {
		return nil, err
	}

	if !available {
		// View not available, return empty metrics (not an error)
		return []map[string]interface{}{}, nil
	}

	query := WrapQuery(ProbeNamePgStatRecoveryPrefetch, p.GetQuery())
	rows, err := monitoredConn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgStatRecoveryPrefetchProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
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
		"stats_reset", "prefetch", "hit",
		"skip_init", "skip_new", "skip_fpw", "skip_rep",
		"wal_distance", "block_distance", "io_depth",
	}

	// Build values array
	var values [][]interface{}
	for _, metric := range metrics {
		row := []interface{}{
			connectionID,
			timestamp,
			metric["stats_reset"],
			metric["prefetch"],
			metric["hit"],
			metric["skip_init"],
			metric["skip_new"],
			metric["skip_fpw"],
			metric["skip_rep"],
			metric["wal_distance"],
			metric["block_distance"],
			metric["io_depth"],
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
func (p *PgStatRecoveryPrefetchProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
