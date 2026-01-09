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

// PgStatWalProbe collects metrics from pg_stat_wal view
// Note: This view is available in PostgreSQL 14+
type PgStatWalProbe struct {
	BaseMetricsProbe
}

// NewPgStatWalProbe creates a new pg_stat_wal probe
func NewPgStatWalProbe(config *ProbeConfig) *PgStatWalProbe {
	return &PgStatWalProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetName returns the probe name
func (p *PgStatWalProbe) GetName() string {
	return ProbeNamePgStatWAL
}

// GetTableName returns the metrics table name
func (p *PgStatWalProbe) GetTableName() string {
	return ProbeNamePgStatWAL
}

// IsDatabaseScoped returns false as pg_stat_wal is server-scoped
func (p *PgStatWalProbe) IsDatabaseScoped() bool {
	return false
}

// GetQuery returns the SQL query to execute (default for PG14-17)
func (p *PgStatWalProbe) GetQuery() string {
	return p.GetQueryForVersion(14)
}

// GetQueryForVersion returns the appropriate SQL query for the given PostgreSQL version
func (p *PgStatWalProbe) GetQueryForVersion(pgVersion int) string {
	if pgVersion >= 18 {
		// PG18: wal_write column was removed
		return `
            SELECT
                wal_records,
                wal_fpi,
                wal_bytes,
                wal_buffers_full,
                NULL::bigint AS wal_write,
                wal_sync,
                wal_write_time,
                wal_sync_time,
                stats_reset
            FROM pg_stat_wal
        `
	}
	// PG14-17: wal_write column exists
	return `
        SELECT
            wal_records,
            wal_fpi,
            wal_bytes,
            wal_buffers_full,
            wal_write,
            wal_sync,
            wal_write_time,
            wal_sync_time,
            stats_reset
        FROM pg_stat_wal
    `
}

// Execute runs the probe against a monitored connection
func (p *PgStatWalProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]interface{}, error) {
	// First check if the view exists (PG 14+)
	var exists bool
	err := monitoredConn.QueryRow(ctx, `
        SELECT EXISTS (
            SELECT 1 FROM pg_views
            WHERE schemaname = 'pg_catalog'
            AND viewname = 'pg_stat_wal'
        )
    `).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("failed to check if pg_stat_wal exists: %w", err)
	}

	if !exists {
		return nil, nil // View doesn't exist, return empty result
	}

	query := p.GetQueryForVersion(pgVersion)
	rows, err := monitoredConn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgStatWalProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
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
		"wal_records", "wal_fpi", "wal_bytes", "wal_buffers_full",
		"wal_write", "wal_sync", "wal_write_time", "wal_sync_time",
		"stats_reset",
	}

	// Build values array
	var values [][]interface{}
	for _, metric := range metrics {
		row := []interface{}{
			connectionID,
			timestamp,
			metric["wal_records"],
			metric["wal_fpi"],
			metric["wal_bytes"],
			metric["wal_buffers_full"],
			metric["wal_write"],
			metric["wal_sync"],
			metric["wal_write_time"],
			metric["wal_sync_time"],
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
func (p *PgStatWalProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
