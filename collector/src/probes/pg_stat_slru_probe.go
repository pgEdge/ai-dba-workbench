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
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/collector/src/utils"

	"fmt"
	"time"
)

// PgStatSLRUProbe collects metrics from pg_stat_slru view
// Note: This view is available in PostgreSQL 13+ and returns multiple rows
type PgStatSLRUProbe struct {
	BaseMetricsProbe
}

// NewPgStatSLRUProbe creates a new pg_stat_slru probe
func NewPgStatSLRUProbe(config *ProbeConfig) *PgStatSLRUProbe {
	return &PgStatSLRUProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetName returns the probe name
func (p *PgStatSLRUProbe) GetName() string {
	return ProbeNamePgStatSLRU
}

// GetTableName returns the metrics table name
func (p *PgStatSLRUProbe) GetTableName() string {
	return ProbeNamePgStatSLRU
}

// IsDatabaseScoped returns false as pg_stat_slru is server-scoped
func (p *PgStatSLRUProbe) IsDatabaseScoped() bool {
	return false
}

// GetQuery returns the SQL query to execute
func (p *PgStatSLRUProbe) GetQuery() string {
	return `
        SELECT
            name,
            blks_zeroed,
            blks_hit,
            blks_read,
            blks_written,
            blks_exists,
            flushes,
            truncates,
            stats_reset
        FROM pg_stat_slru
    `
}

// Execute runs the probe against a monitored connection
func (p *PgStatSLRUProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]interface{}, error) {
	// First check if the view exists (PG 13+)
	var exists bool
	err := monitoredConn.QueryRow(ctx, `
        SELECT EXISTS (
            SELECT 1 FROM pg_views
            WHERE schemaname = 'pg_catalog'
            AND viewname = 'pg_stat_slru'
        )
    `).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("failed to check if pg_stat_slru exists: %w", err)
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
func (p *PgStatSLRUProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
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
		"name", "blks_zeroed", "blks_hit", "blks_read",
		"blks_written", "blks_exists", "flushes", "truncates",
		"stats_reset",
	}

	// Build values array
	var values [][]interface{}
	for _, metric := range metrics {
		row := []interface{}{
			connectionID,
			timestamp,
			metric["name"],
			metric["blks_zeroed"],
			metric["blks_hit"],
			metric["blks_read"],
			metric["blks_written"],
			metric["blks_exists"],
			metric["flushes"],
			metric["truncates"],
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
func (p *PgStatSLRUProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
