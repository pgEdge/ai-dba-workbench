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

// PgStatioAllSequencesProbe collects metrics from pg_statio_all_sequences view
type PgStatioAllSequencesProbe struct {
	BaseMetricsProbe
}

// NewPgStatioAllSequencesProbe creates a new pg_statio_all_sequences probe
func NewPgStatioAllSequencesProbe(config *ProbeConfig) *PgStatioAllSequencesProbe {
	return &PgStatioAllSequencesProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config, databaseScoped: true},
	}
}

// GetQuery returns the SQL query to execute
func (p *PgStatioAllSequencesProbe) GetQuery() string {
	return `
        SELECT
            relid,
            schemaname,
            relname,
            blks_read,
            blks_hit
        FROM pg_statio_all_sequences
        ORDER BY schemaname, relname
    `
}

// Execute runs the probe against a monitored connection
func (p *PgStatioAllSequencesProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]interface{}, error) {
	query := WrapQuery(ProbeNamePgStatioAllSequences, p.GetQuery())
	rows, err := monitoredConn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgStatioAllSequencesProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
	if len(metrics) == 0 {
		return nil // Nothing to store
	}

	// Ensure partition exists for this timestamp
	if err := p.EnsurePartition(ctx, datastoreConn, timestamp); err != nil {
		return fmt.Errorf("failed to ensure partition: %w", err)
	}

	// Define columns in order
	columns := []string{
		"connection_id", "collected_at", "database_name",
		"relid", "schemaname", "relname",
		"blks_read", "blks_hit",
	}

	// Build values array
	var values [][]interface{}
	for _, metric := range metrics {
		// Extract database_name from the metric (set by scheduler)
		databaseName, ok := metric["_database_name"]
		if !ok {
			return fmt.Errorf("database_name not found in metrics")
		}

		row := []interface{}{
			connectionID,
			timestamp,
			databaseName,
			metric["relid"],
			metric["schemaname"],
			metric["relname"],
			metric["blks_read"],
			metric["blks_hit"],
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
func (p *PgStatioAllSequencesProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
