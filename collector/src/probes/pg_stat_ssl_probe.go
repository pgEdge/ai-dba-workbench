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
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/collector/src/utils"

	"fmt"
	"time"
)

// PgStatSSLProbe collects metrics from pg_stat_ssl view
type PgStatSSLProbe struct {
	BaseMetricsProbe
}

// NewPgStatSSLProbe creates a new pg_stat_ssl probe
func NewPgStatSSLProbe(config *ProbeConfig) *PgStatSSLProbe {
	return &PgStatSSLProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetName returns the probe name
func (p *PgStatSSLProbe) GetName() string {
	return ProbeNamePgStatSSL
}

// GetTableName returns the metrics table name
func (p *PgStatSSLProbe) GetTableName() string {
	return ProbeNamePgStatSSL
}

// IsDatabaseScoped returns false as pg_stat_ssl is server-scoped
func (p *PgStatSSLProbe) IsDatabaseScoped() bool {
	return false
}

// GetQuery returns the SQL query to execute
func (p *PgStatSSLProbe) GetQuery() string {
	return `
        SELECT
            pid,
            ssl,
            version,
            cipher,
            bits,
            client_dn,
            client_serial::text,
            issuer_dn
        FROM pg_stat_ssl
    `
}

// Execute runs the probe against a monitored connection
func (p *PgStatSSLProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]interface{}, error) {
	rows, err := monitoredConn.Query(ctx, p.GetQuery())
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgStatSSLProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
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
		"pid", "ssl", "version", "cipher", "bits", "client_dn", "client_serial", "issuer_dn",
	}

	// Build values array
	var values [][]interface{}
	for _, metric := range metrics {
		row := []interface{}{
			connectionID,
			timestamp,
			metric["pid"],
			metric["ssl"],
			metric["version"],
			metric["cipher"],
			metric["bits"],
			metric["client_dn"],
			metric["client_serial"],
			metric["issuer_dn"],
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
func (p *PgStatSSLProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
