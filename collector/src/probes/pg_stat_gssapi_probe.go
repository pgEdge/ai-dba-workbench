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

// PgStatGSSAPIProbe collects metrics from pg_stat_gssapi view
type PgStatGSSAPIProbe struct {
    BaseMetricsProbe
}

// NewPgStatGSSAPIProbe creates a new pg_stat_gssapi probe
func NewPgStatGSSAPIProbe(config *ProbeConfig) *PgStatGSSAPIProbe {
    return &PgStatGSSAPIProbe{
        BaseMetricsProbe: BaseMetricsProbe{config: config},
    }
}

// GetName returns the probe name
func (p *PgStatGSSAPIProbe) GetName() string {
    return ProbeNamePgStatGSSAPI
}

// GetTableName returns the metrics table name
func (p *PgStatGSSAPIProbe) GetTableName() string {
    return ProbeNamePgStatGSSAPI
}

// IsDatabaseScoped returns false as pg_stat_gssapi is server-scoped
func (p *PgStatGSSAPIProbe) IsDatabaseScoped() bool {
    return false
}

// GetQuery returns the SQL query to execute
func (p *PgStatGSSAPIProbe) GetQuery() string {
    return `
        SELECT
            pid,
            gss_authenticated,
            principal,
            encrypted,
            credentials_delegated
        FROM pg_stat_gssapi
    `
}

// checkViewAvailable checks if pg_stat_gssapi view exists (PG 12+)
func (p *PgStatGSSAPIProbe) checkViewAvailable(ctx context.Context, conn *pgxpool.Conn) (bool, error) {
    var exists bool
    err := conn.QueryRow(ctx, `
        SELECT EXISTS(
            SELECT 1
            FROM pg_views
            WHERE schemaname = 'pg_catalog'
            AND viewname = 'pg_stat_gssapi'
        )
    `).Scan(&exists)

    if err != nil {
        return false, fmt.Errorf("failed to check for pg_stat_gssapi view: %w", err)
    }

    return exists, nil
}

// Execute runs the probe against a monitored connection
func (p *PgStatGSSAPIProbe) Execute(ctx context.Context, monitoredConn *pgxpool.Conn) ([]map[string]interface{}, error) {
    // Check if view is available (PG 12+)
    available, err := p.checkViewAvailable(ctx, monitoredConn)
    if err != nil {
        return nil, err
    }

    if !available {
        // View not available, return empty metrics (not an error)
        return []map[string]interface{}{}, nil
    }

    rows, err := monitoredConn.Query(ctx, p.GetQuery())
    if err != nil {
        return nil, fmt.Errorf("failed to execute query: %w", err)
    }
    defer rows.Close()

    return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgStatGSSAPIProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
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
        "pid", "gss_authenticated", "principal", "encrypted", "credentials_delegated",
    }

    // Build values array
    var values [][]interface{}
    for _, metric := range metrics {
        row := []interface{}{
            connectionID,
            timestamp,
            metric["pid"],
            metric["gss_authenticated"],
            metric["principal"],
            metric["encrypted"],
            metric["credentials_delegated"],
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
func (p *PgStatGSSAPIProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
    return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
