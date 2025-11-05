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

// PgStatArchiverProbe collects metrics from pg_stat_archiver view
type PgStatArchiverProbe struct {
    BaseMetricsProbe
}

// NewPgStatArchiverProbe creates a new pg_stat_archiver probe
func NewPgStatArchiverProbe(config *ProbeConfig) *PgStatArchiverProbe {
    return &PgStatArchiverProbe{
        BaseMetricsProbe: BaseMetricsProbe{config: config},
    }
}

// GetName returns the probe name
func (p *PgStatArchiverProbe) GetName() string {
    return ProbeNamePgStatArchiver
}

// GetTableName returns the metrics table name
func (p *PgStatArchiverProbe) GetTableName() string {
    return ProbeNamePgStatArchiver
}

// IsDatabaseScoped returns false as pg_stat_archiver is server-scoped
func (p *PgStatArchiverProbe) IsDatabaseScoped() bool {
    return false
}

// GetQuery returns the SQL query to execute
func (p *PgStatArchiverProbe) GetQuery() string {
    return `
        SELECT
            archived_count,
            last_archived_wal,
            last_archived_time,
            failed_count,
            last_failed_wal,
            last_failed_time,
            stats_reset
        FROM pg_stat_archiver
    `
}

// Execute runs the probe against a monitored connection
func (p *PgStatArchiverProbe) Execute(ctx context.Context, monitoredConn *pgxpool.Conn) ([]map[string]interface{}, error) {
    rows, err := monitoredConn.Query(ctx, p.GetQuery())
    if err != nil {
        return nil, fmt.Errorf("failed to execute query: %w", err)
    }
    defer rows.Close()

    return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgStatArchiverProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
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
        "archived_count", "last_archived_wal", "last_archived_time",
        "failed_count", "last_failed_wal", "last_failed_time",
        "stats_reset",
    }

    // Build values array
    var values [][]interface{}
    for _, metric := range metrics {
        row := []interface{}{
            connectionID,
            timestamp,
            metric["archived_count"],
            metric["last_archived_wal"],
            metric["last_archived_time"],
            metric["failed_count"],
            metric["last_failed_wal"],
            metric["last_failed_time"],
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
func (p *PgStatArchiverProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
    return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
