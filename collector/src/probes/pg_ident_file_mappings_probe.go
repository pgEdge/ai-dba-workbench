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

// PgIdentFileMappingsProbe collects metrics from pg_ident_file_mappings view
// This probe only stores data when ident mapping configuration changes are detected
type PgIdentFileMappingsProbe struct {
	BaseMetricsProbe
}

// NewPgIdentFileMappingsProbe creates a new pg_ident_file_mappings probe
func NewPgIdentFileMappingsProbe(config *ProbeConfig) *PgIdentFileMappingsProbe {
	return &PgIdentFileMappingsProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetQuery returns the SQL query to execute (default for PG16+)
func (p *PgIdentFileMappingsProbe) GetQuery() string {
	return p.GetQueryForVersion(16)
}

// GetQueryForVersion returns the appropriate SQL query for the given PostgreSQL version
func (p *PgIdentFileMappingsProbe) GetQueryForVersion(pgVersion int) string {
	if pgVersion >= 16 {
		// PG16+ has map_number column
		return `
            SELECT
                map_number,
                file_name,
                line_number,
                map_name,
                sys_name,
                pg_username,
                error
            FROM pg_ident_file_mappings
            ORDER BY map_number
        `
	}
	// PG15: map_number and file_name don't exist
	return `
        SELECT
            line_number AS map_number,
            NULL::text AS file_name,
            line_number,
            map_name,
            sys_name,
            pg_username,
            error
        FROM pg_ident_file_mappings
        ORDER BY line_number
    `
}

// Execute runs the probe against a monitored connection
func (p *PgIdentFileMappingsProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]any, error) {
	// Check if view exists (PG15+) (cached)
	available, err := cachedCheck(connectionName, "pg_ident_file_mappings_exists", func() (bool, error) {
		return CheckViewExists(ctx, monitoredConn, "pg_ident_file_mappings")
	})
	if err != nil {
		return nil, err
	}

	if !available {
		// View not available (PG14 and earlier), return empty metrics
		return []map[string]any{}, nil
	}

	query := WrapQuery(ProbeNamePgIdentFileMappings, p.GetQueryForVersion(pgVersion))
	rows, err := monitoredConn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore only if data has changed
func (p *PgIdentFileMappingsProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]any) error {
	// Check if data has changed
	changed, err := HasDataChanged(ctx, datastoreConn, connectionID, "pg_ident_file_mappings", metrics,
		`SELECT map_number, file_name, line_number, map_name,
		        sys_name, pg_username, error
		 FROM metrics.pg_ident_file_mappings
		 WHERE connection_id = $1
		   AND collected_at = (
		       SELECT MAX(collected_at)
		       FROM metrics.pg_ident_file_mappings
		       WHERE connection_id = $1
		   )
		 ORDER BY map_number`,
		nil)
	if err != nil {
		return fmt.Errorf("failed to check if data changed: %w", err)
	}

	if !changed {
		// Data unchanged, skip storage
		return nil
	}

	logger.Infof("pg_ident_file_mappings data changed for connection %d, storing new snapshot", connectionID)

	// Ensure partition exists for this timestamp
	if err := p.EnsurePartition(ctx, datastoreConn, timestamp); err != nil {
		return fmt.Errorf("failed to ensure partition: %w", err)
	}

	// Define columns in order
	columns := []string{
		"connection_id", "collected_at",
		"map_number", "file_name", "line_number",
		"map_name", "sys_name", "pg_username", "error",
	}

	// Build values array
	var values [][]any
	for _, metric := range metrics {
		row := []any{
			connectionID,
			timestamp,
			metric["map_number"],
			metric["file_name"],
			metric["line_number"],
			metric["map_name"],
			metric["sys_name"],
			metric["pg_username"],
			metric["error"],
		}
		values = append(values, row)
	}

	// Use COPY protocol to store metrics
	if err := StoreMetricsWithCopy(ctx, datastoreConn, p.GetTableName(), columns, values); err != nil {
		return fmt.Errorf("failed to store metrics: %w", err)
	}

	return nil
}
