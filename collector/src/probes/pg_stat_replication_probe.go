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

// PgStatReplicationProbe collects metrics from pg_stat_replication view
type PgStatReplicationProbe struct {
    BaseMetricsProbe
}

// NewPgStatReplicationProbe creates a new pg_stat_replication probe
func NewPgStatReplicationProbe(config *ProbeConfig) *PgStatReplicationProbe {
    return &PgStatReplicationProbe{
        BaseMetricsProbe: BaseMetricsProbe{config: config},
    }
}

// GetName returns the probe name
func (p *PgStatReplicationProbe) GetName() string {
    return ProbeNamePgStatReplication
}

// GetTableName returns the metrics table name
func (p *PgStatReplicationProbe) GetTableName() string {
    return ProbeNamePgStatReplication
}

// IsDatabaseScoped returns false as pg_stat_replication is server-scoped
func (p *PgStatReplicationProbe) IsDatabaseScoped() bool {
    return false
}

// GetQuery returns the SQL query to execute
func (p *PgStatReplicationProbe) GetQuery() string {
    return `
        SELECT
            pid,
            usesysid,
            usename,
            application_name,
            client_addr,
            client_hostname,
            client_port,
            backend_start,
            backend_xmin::text,
            state,
            sent_lsn::text,
            write_lsn::text,
            flush_lsn::text,
            replay_lsn::text,
            write_lag,
            flush_lag,
            replay_lag,
            sync_priority,
            sync_state,
            reply_time
        FROM pg_stat_replication
        ORDER BY pid
    `
}

// Execute runs the probe against a monitored connection
func (p *PgStatReplicationProbe) Execute(ctx context.Context, monitoredConn *pgxpool.Conn) ([]map[string]interface{}, error) {
    rows, err := monitoredConn.Query(ctx, p.GetQuery())
    if err != nil {
        return nil, fmt.Errorf("failed to execute query: %w", err)
    }
    defer rows.Close()

    return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgStatReplicationProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
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
        "pid", "usesysid", "usename", "application_name",
        "client_addr", "client_hostname", "client_port",
        "backend_start", "backend_xmin", "state",
        "sent_lsn", "write_lsn", "flush_lsn", "replay_lsn",
        "write_lag", "flush_lag", "replay_lag",
        "sync_priority", "sync_state", "reply_time",
    }

    // Build values array
    var values [][]interface{}
    for _, metric := range metrics {
        row := []interface{}{
            connectionID,
            timestamp,
            metric["pid"],
            metric["usesysid"],
            metric["usename"],
            metric["application_name"],
            metric["client_addr"],
            metric["client_hostname"],
            metric["client_port"],
            metric["backend_start"],
            metric["backend_xmin"],
            metric["state"],
            metric["sent_lsn"],
            metric["write_lsn"],
            metric["flush_lsn"],
            metric["replay_lsn"],
            metric["write_lag"],
            metric["flush_lag"],
            metric["replay_lag"],
            metric["sync_priority"],
            metric["sync_state"],
            metric["reply_time"],
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
func (p *PgStatReplicationProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
    return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
