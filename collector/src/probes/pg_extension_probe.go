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
	"github.com/pgedge/ai-workbench/pkg/logger"
)

// PgExtensionProbe collects installed PostgreSQL extensions and their versions
// This probe only stores data when changes are detected compared to the most
// recent stored data
type PgExtensionProbe struct {
	BaseMetricsProbe
}

// NewPgExtensionProbe creates a new pg_extension probe
func NewPgExtensionProbe(config *ProbeConfig) *PgExtensionProbe {
	return &PgExtensionProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config, databaseScoped: true},
	}
}

// GetQuery returns the SQL query to execute
func (p *PgExtensionProbe) GetQuery() string {
	return `
        SELECT
            extname,
            extversion,
            extrelocatable,
            nspname as schema_name
        FROM pg_extension e
        JOIN pg_namespace n ON e.extnamespace = n.oid
        ORDER BY extname
    `
}

// Execute runs the probe against a monitored connection
func (p *PgExtensionProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]any, error) {
	query := WrapQuery(ProbeNamePgExtension, p.GetQuery())
	rows, err := monitoredConn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore only if changes are
// detected
func (p *PgExtensionProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]any) error {
	if len(metrics) == 0 {
		return nil // Nothing to store
	}

	// Check if extensions have changed compared to the most recent stored data
	hasChanged, err := HasDataChanged(ctx, datastoreConn, connectionID, "pg_extension", metrics,
		`SELECT database_name, extname, extversion, extrelocatable, schema_name
		 FROM metrics.pg_extension
		 WHERE connection_id = $1
		   AND collected_at = (
		       SELECT MAX(collected_at)
		       FROM metrics.pg_extension
		       WHERE connection_id = $1
		   )
		 ORDER BY database_name, extname`,
		normalizeDatabaseName)
	if err != nil {
		return fmt.Errorf("failed to check for changes: %w", err)
	}

	if !hasChanged {
		logger.Infof("pg_extension data unchanged for connection %d, skipping storage", connectionID)
		return nil
	}

	logger.Infof("pg_extension data changed for connection %d, storing new snapshot", connectionID)

	// Ensure partition exists for this timestamp
	if err := p.EnsurePartition(ctx, datastoreConn, timestamp); err != nil {
		return fmt.Errorf("failed to ensure partition: %w", err)
	}

	// Define columns in order
	columns := []string{
		"connection_id", "database_name", "collected_at",
		"extname", "extversion", "extrelocatable", "schema_name",
	}

	// Build values array
	var values [][]any
	for _, metric := range metrics {
		row := []any{
			connectionID,
			metric["_database_name"], // Added by scheduler for database-scoped probes
			timestamp,
			metric["extname"],
			metric["extversion"],
			metric["extrelocatable"],
			metric["schema_name"],
		}
		values = append(values, row)
	}

	// Store metrics
	if err := StoreMetrics(ctx, datastoreConn, p.GetTableName(), columns, values); err != nil {
		return fmt.Errorf("failed to store metrics: %w", err)
	}

	return nil
}
