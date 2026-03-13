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

// PgStatIOProbe collects metrics from pg_stat_io and pg_stat_slru views
// Note: pg_stat_io is available in PostgreSQL 16+
// SLRU data is collected with backend_type='slru' to distinguish from regular I/O stats
type PgStatIOProbe struct {
	BaseMetricsProbe
}

// NewPgStatIOProbe creates a new pg_stat_io probe
func NewPgStatIOProbe(config *ProbeConfig) *PgStatIOProbe {
	return &PgStatIOProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetQuery returns the SQL query to execute
func (p *PgStatIOProbe) GetQuery() string {
	return ""
}

// checkIOViewExists checks if pg_stat_io view exists (PG16+)
func (p *PgStatIOProbe) checkIOViewExists(ctx context.Context, conn *pgxpool.Conn) (bool, error) {
	var exists bool
	err := conn.QueryRow(ctx, `
        SELECT EXISTS (
            SELECT 1 FROM pg_views
            WHERE schemaname = 'pg_catalog'
            AND viewname = 'pg_stat_io'
        )
    `).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check if pg_stat_io exists: %w", err)
	}
	return exists, nil
}

// checkSLRUViewExists checks if pg_stat_slru view exists (PG13+)
func (p *PgStatIOProbe) checkSLRUViewExists(ctx context.Context, conn *pgxpool.Conn) (bool, error) {
	var exists bool
	err := conn.QueryRow(ctx, `
        SELECT EXISTS (
            SELECT 1 FROM pg_views
            WHERE schemaname = 'pg_catalog'
            AND viewname = 'pg_stat_slru'
        )
    `).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check if pg_stat_slru exists: %w", err)
	}
	return exists, nil
}

// Execute runs the probe against a monitored connection
func (p *PgStatIOProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]any, error) {
	var allMetrics []map[string]any

	// Check if pg_stat_io exists (PG 16+)
	ioExists, err := cachedCheck(connectionName, "pg_stat_io_exists", func() (bool, error) {
		return p.checkIOViewExists(ctx, monitoredConn)
	})
	if err != nil {
		return nil, err
	}

	if ioExists {
		var query string
		if pgVersion >= 18 {
			// PG18: op_bytes column was removed
			query = `
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
                    NULL::bigint AS op_bytes,
                    hits,
                    evictions,
                    reuses,
                    fsyncs,
                    fsync_time,
                    stats_reset,
                    NULL::bigint AS blks_zeroed,
                    NULL::bigint AS blks_exists,
                    NULL::bigint AS flushes,
                    NULL::bigint AS truncates
                FROM pg_stat_io
            `
		} else {
			// PG16-17: op_bytes column exists
			query = `
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
                    stats_reset,
                    NULL::bigint AS blks_zeroed,
                    NULL::bigint AS blks_exists,
                    NULL::bigint AS flushes,
                    NULL::bigint AS truncates
                FROM pg_stat_io
            `
		}

		wrappedQuery := WrapQuery(ProbeNamePgStatIO, query)
		rows, err := monitoredConn.Query(ctx, wrappedQuery)
		if err != nil {
			return nil, fmt.Errorf("failed to execute query: %w", err)
		}
		defer rows.Close()

		ioMetrics, err := utils.ScanRowsToMaps(rows)
		if err != nil {
			return nil, err
		}

		allMetrics = append(allMetrics, ioMetrics...)
	}

	// Check if pg_stat_slru exists (PG 13+)
	slruExists, err := cachedCheck(connectionName, "pg_stat_slru_exists", func() (bool, error) {
		return p.checkSLRUViewExists(ctx, monitoredConn)
	})
	if err != nil {
		return nil, err
	}

	if slruExists {
		// Query SLRU data and store with backend_type='slru'
		slruQuery := `
            SELECT
                'slru' AS backend_type,
                name AS object,
                'normal' AS context,
                blks_read AS reads,
                NULL::double precision AS read_time,
                blks_written AS writes,
                NULL::double precision AS write_time,
                NULL::bigint AS writebacks,
                NULL::double precision AS writeback_time,
                NULL::bigint AS extends,
                NULL::double precision AS extend_time,
                NULL::bigint AS op_bytes,
                blks_hit AS hits,
                NULL::bigint AS evictions,
                NULL::bigint AS reuses,
                NULL::bigint AS fsyncs,
                NULL::double precision AS fsync_time,
                stats_reset,
                blks_zeroed,
                blks_exists,
                flushes,
                truncates
            FROM pg_stat_slru
        `

		wrappedSlruQuery := WrapQuery(ProbeNamePgStatIO, slruQuery)
		slruRows, err := monitoredConn.Query(ctx, wrappedSlruQuery)
		if err != nil {
			return nil, fmt.Errorf("failed to execute SLRU query: %w", err)
		}
		defer slruRows.Close()

		slruMetrics, err := utils.ScanRowsToMaps(slruRows)
		if err != nil {
			return nil, err
		}

		allMetrics = append(allMetrics, slruMetrics...)
	}

	// If neither view exists, return empty result
	if !ioExists && !slruExists {
		return nil, nil
	}

	return allMetrics, nil
}

// Store stores the collected metrics in the datastore
func (p *PgStatIOProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]any) error {
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
		"blks_zeroed", "blks_exists", "flushes", "truncates",
	}

	// Build values array
	var values [][]any
	for _, metric := range metrics {
		row := []any{
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
			metric["blks_zeroed"],
			metric["blks_exists"],
			metric["flushes"],
			metric["truncates"],
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
