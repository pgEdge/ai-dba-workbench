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

// GetQuery returns the SQL query to execute (default for PG16+)
func (p *PgStatGSSAPIProbe) GetQuery() string {
	return p.GetQueryForVersion(16)
}

// GetQueryForVersion returns the appropriate SQL query for the given PostgreSQL version
func (p *PgStatGSSAPIProbe) GetQueryForVersion(pgVersion int) string {
	if pgVersion >= 16 {
		// PG16+ has credentials_delegated column
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
	// PG12-15: credentials_delegated doesn't exist, return NULL
	return `
        SELECT
            pid,
            gss_authenticated,
            principal,
            encrypted,
            NULL::boolean AS credentials_delegated
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
func (p *PgStatGSSAPIProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]interface{}, error) {
	// Check if view is available (PG 12+)
	available, err := p.checkViewAvailable(ctx, monitoredConn)
	if err != nil {
		return nil, err
	}

	if !available {
		// View not available, return empty metrics (not an error)
		return []map[string]interface{}{}, nil
	}

	// Use version-specific query
	query := p.GetQueryForVersion(pgVersion)
	rows, err := monitoredConn.Query(ctx, query)
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
