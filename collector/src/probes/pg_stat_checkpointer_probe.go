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

// PgStatCheckpointerProbe collects metrics from pg_stat_checkpointer view
// Note: This view is available in PostgreSQL 17+
type PgStatCheckpointerProbe struct {
    BaseMetricsProbe
}

// NewPgStatCheckpointerProbe creates a new pg_stat_checkpointer probe
func NewPgStatCheckpointerProbe(config *ProbeConfig) *PgStatCheckpointerProbe {
    return &PgStatCheckpointerProbe{
        BaseMetricsProbe: BaseMetricsProbe{config: config},
    }
}

// GetName returns the probe name
func (p *PgStatCheckpointerProbe) GetName() string {
    return ProbeNamePgStatCheckpointer
}

// GetTableName returns the metrics table name
func (p *PgStatCheckpointerProbe) GetTableName() string {
    return ProbeNamePgStatCheckpointer
}

// IsDatabaseScoped returns false as pg_stat_checkpointer is server-scoped
func (p *PgStatCheckpointerProbe) IsDatabaseScoped() bool {
    return false
}

// GetQuery returns the SQL query to execute
func (p *PgStatCheckpointerProbe) GetQuery() string {
    return `
        SELECT
            num_timed,
            num_requested,
            restartpoints_timed,
            restartpoints_req,
            restartpoints_done,
            write_time,
            sync_time,
            buffers_written,
            stats_reset
        FROM pg_stat_checkpointer
    `
}

// Execute runs the probe against a monitored connection
func (p *PgStatCheckpointerProbe) Execute(ctx context.Context, monitoredConn *pgxpool.Conn) ([]map[string]interface{}, error) {
    // First check if the view exists (PG 17+)
    var exists bool
    err := monitoredConn.QueryRow(ctx, `
        SELECT EXISTS (
            SELECT 1 FROM pg_views
            WHERE schemaname = 'pg_catalog'
            AND viewname = 'pg_stat_checkpointer'
        )
    `).Scan(&exists)
    if err != nil {
        return nil, fmt.Errorf("failed to check if pg_stat_checkpointer exists: %w", err)
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
