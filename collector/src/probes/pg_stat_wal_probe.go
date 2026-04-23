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

// PgStatWalProbe collects metrics from pg_stat_wal and pg_stat_archiver views
// Note: pg_stat_wal is available in PostgreSQL 14+
type PgStatWalProbe struct {
	BaseMetricsProbe
}

// NewPgStatWalProbe creates a new pg_stat_wal probe
func NewPgStatWalProbe(config *ProbeConfig) *PgStatWalProbe {
	return &PgStatWalProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetQuery returns the SQL query to execute
func (p *PgStatWalProbe) GetQuery() string {
	return ""
}

// Execute runs the probe against a monitored connection
func (p *PgStatWalProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]any, error) {
	// First check if the pg_stat_wal view exists (PG 14+)
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
		// If pg_stat_wal doesn't exist (PG13 and earlier), just collect archiver stats
		query := `
            SELECT
                NULL::bigint AS wal_records,
                NULL::bigint AS wal_fpi,
                NULL::numeric AS wal_bytes,
                NULL::bigint AS wal_buffers_full,
                NULL::bigint AS wal_write,
                NULL::bigint AS wal_sync,
                NULL::double precision AS wal_write_time,
                NULL::double precision AS wal_sync_time,
                NULL::timestamptz AS stats_reset,
                a.archived_count,
                a.last_archived_wal,
                a.last_archived_time,
                a.failed_count,
                a.last_failed_wal,
                a.last_failed_time,
                a.stats_reset AS archiver_stats_reset
            FROM pg_stat_archiver a
        `
		wrappedQuery := WrapQuery(ProbeNamePgStatWAL, query)
		rows, err := monitoredConn.Query(ctx, wrappedQuery)
		if err != nil {
			return nil, fmt.Errorf("failed to execute query: %w", err)
		}
		defer rows.Close()
		return utils.ScanRowsToMaps(rows)
	}

	var query string
	if pgVersion >= 18 {
		// PG18: wal_write, wal_sync, wal_write_time, wal_sync_time columns were removed
		query = `
            SELECT
                w.wal_records,
                w.wal_fpi,
                w.wal_bytes,
                w.wal_buffers_full,
                NULL::bigint AS wal_write,
                NULL::bigint AS wal_sync,
                NULL::double precision AS wal_write_time,
                NULL::double precision AS wal_sync_time,
                w.stats_reset,
                a.archived_count,
                a.last_archived_wal,
                a.last_archived_time,
                a.failed_count,
                a.last_failed_wal,
                a.last_failed_time,
                a.stats_reset AS archiver_stats_reset
            FROM pg_stat_wal w
            CROSS JOIN pg_stat_archiver a
        `
	} else {
		// PG14-17: wal_write column exists
		query = `
            SELECT
                w.wal_records,
                w.wal_fpi,
                w.wal_bytes,
                w.wal_buffers_full,
                w.wal_write,
                w.wal_sync,
                w.wal_write_time,
                w.wal_sync_time,
                w.stats_reset,
                a.archived_count,
                a.last_archived_wal,
                a.last_archived_time,
                a.failed_count,
                a.last_failed_wal,
                a.last_failed_time,
                a.stats_reset AS archiver_stats_reset
            FROM pg_stat_wal w
            CROSS JOIN pg_stat_archiver a
        `
	}

	wrappedQuery := WrapQuery(ProbeNamePgStatWAL, query)
	rows, err := monitoredConn.Query(ctx, wrappedQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgStatWalProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]any) error {
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
		"archived_count", "last_archived_wal", "last_archived_time",
		"failed_count", "last_failed_wal", "last_failed_time",
		"archiver_stats_reset",
	}

	// Build values array
	var values [][]any
	for _, metric := range metrics {
		row := []any{
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
			metric["archived_count"],
			metric["last_archived_wal"],
			metric["last_archived_time"],
			metric["failed_count"],
			metric["last_failed_wal"],
			metric["last_failed_time"],
			metric["archiver_stats_reset"],
		}
		values = append(values, row)
	}

	// Use COPY protocol to store metrics
	if err := StoreMetricsWithCopy(ctx, datastoreConn, p.GetTableName(), columns, values); err != nil {
		return fmt.Errorf("failed to store metrics: %w", err)
	}

	return nil
}
