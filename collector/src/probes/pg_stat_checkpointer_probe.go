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

// PgStatCheckpointerProbe collects metrics from pg_stat_checkpointer and pg_stat_bgwriter
// Note: pg_stat_checkpointer is available in PostgreSQL 17+
// For older versions, we collect from the combined pg_stat_bgwriter view
type PgStatCheckpointerProbe struct {
	BaseMetricsProbe
}

// NewPgStatCheckpointerProbe creates a new pg_stat_checkpointer probe
func NewPgStatCheckpointerProbe(config *ProbeConfig) *PgStatCheckpointerProbe {
	return &PgStatCheckpointerProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetQuery returns the SQL query to execute
func (p *PgStatCheckpointerProbe) GetQuery() string {
	return ""
}

// checkCheckpointerViewExists checks if pg_stat_checkpointer view exists (PG17+)
func (p *PgStatCheckpointerProbe) checkCheckpointerViewExists(ctx context.Context, conn *pgxpool.Conn) (bool, error) {
	var exists bool
	err := conn.QueryRow(ctx, `
        SELECT EXISTS (
            SELECT 1 FROM pg_views
            WHERE schemaname = 'pg_catalog'
            AND viewname = 'pg_stat_checkpointer'
        )
    `).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check if pg_stat_checkpointer exists: %w", err)
	}
	return exists, nil
}

// Execute runs the probe against a monitored connection
func (p *PgStatCheckpointerProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]interface{}, error) {
	// Check if the pg_stat_checkpointer view exists (PG 17+)
	checkpointerExists, err := p.checkCheckpointerViewExists(ctx, monitoredConn)
	if err != nil {
		return nil, err
	}

	var query string
	if checkpointerExists {
		// PG17+: Separate pg_stat_checkpointer and pg_stat_bgwriter views
		// Query both and combine results
		query = `
            SELECT
                c.num_timed,
                c.num_requested,
                c.restartpoints_timed,
                c.restartpoints_req,
                c.restartpoints_done,
                c.write_time,
                c.sync_time,
                c.buffers_written,
                c.stats_reset,
                b.buffers_clean,
                b.maxwritten_clean,
                b.buffers_alloc,
                b.stats_reset AS bgwriter_stats_reset
            FROM pg_stat_checkpointer c
            CROSS JOIN pg_stat_bgwriter b
        `
	} else {
		// PG16 and earlier: Combined pg_stat_bgwriter view has both checkpoint and bgwriter stats
		query = `
            SELECT
                checkpoints_timed AS num_timed,
                checkpoints_req AS num_requested,
                NULL::bigint AS restartpoints_timed,
                NULL::bigint AS restartpoints_req,
                NULL::bigint AS restartpoints_done,
                checkpoint_write_time AS write_time,
                checkpoint_sync_time AS sync_time,
                buffers_checkpoint AS buffers_written,
                stats_reset,
                buffers_clean,
                maxwritten_clean,
                buffers_alloc,
                stats_reset AS bgwriter_stats_reset
            FROM pg_stat_bgwriter
        `
	}

	wrappedQuery := WrapQuery(ProbeNamePgStatCheckpointer, query)
	rows, err := monitoredConn.Query(ctx, wrappedQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgStatCheckpointerProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
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
		"num_timed", "num_requested", "restartpoints_timed",
		"restartpoints_req", "restartpoints_done", "write_time",
		"sync_time", "buffers_written", "stats_reset",
		"buffers_clean", "maxwritten_clean", "buffers_alloc",
		"bgwriter_stats_reset",
	}

	// Build values array
	var values [][]interface{}
	for _, metric := range metrics {
		row := []interface{}{
			connectionID,
			timestamp,
			metric["num_timed"],
			metric["num_requested"],
			metric["restartpoints_timed"],
			metric["restartpoints_req"],
			metric["restartpoints_done"],
			metric["write_time"],
			metric["sync_time"],
			metric["buffers_written"],
			metric["stats_reset"],
			metric["buffers_clean"],
			metric["maxwritten_clean"],
			metric["buffers_alloc"],
			metric["bgwriter_stats_reset"],
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
func (p *PgStatCheckpointerProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
