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

// PgSettingsProbe collects PostgreSQL configuration settings
// This probe only stores data when changes are detected compared to the most recent stored data
type PgSettingsProbe struct {
	BaseMetricsProbe
}

// NewPgSettingsProbe creates a new pg_settings probe
func NewPgSettingsProbe(config *ProbeConfig) *PgSettingsProbe {
	return &PgSettingsProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetQuery returns the SQL query to execute
func (p *PgSettingsProbe) GetQuery() string {
	return `
        SELECT
            name,
            setting,
            unit,
            category,
            short_desc,
            extra_desc,
            context,
            vartype,
            source,
            min_val,
            max_val,
            enumvals,
            boot_val,
            reset_val,
            sourcefile,
            sourceline,
            pending_restart
        FROM pg_settings
        ORDER BY name
    `
}

// Execute runs the probe against a monitored connection
func (p *PgSettingsProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]any, error) {
	query := WrapQuery(ProbeNamePgSettings, p.GetQuery())
	rows, err := monitoredConn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore only if changes are detected
func (p *PgSettingsProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]any) error {
	if len(metrics) == 0 {
		return nil // Nothing to store
	}

	// Check if settings have changed compared to the most recent stored data
	hasChanged, err := HasDataChanged(ctx, datastoreConn, connectionID, "pg_settings", metrics,
		`SELECT name, setting, unit, category, short_desc, extra_desc,
		        context, vartype, source, min_val, max_val, enumvals,
		        boot_val, reset_val, sourcefile, sourceline, pending_restart
		 FROM metrics.pg_settings
		 WHERE connection_id = $1
		   AND collected_at = (
		       SELECT MAX(collected_at)
		       FROM metrics.pg_settings
		       WHERE connection_id = $1
		   )
		 ORDER BY name`,
		nil)
	if err != nil {
		return fmt.Errorf("failed to check for changes: %w", err)
	}

	if !hasChanged {
		logger.Infof("pg_settings data unchanged for connection %d, skipping storage", connectionID)
		return nil
	}

	logger.Infof("pg_settings data changed for connection %d, storing new snapshot", connectionID)

	// Ensure partition exists for this timestamp
	if err := p.EnsurePartition(ctx, datastoreConn, timestamp); err != nil {
		return fmt.Errorf("failed to ensure partition: %w", err)
	}

	// Define columns in order
	columns := []string{
		"connection_id", "collected_at",
		"name", "setting", "unit", "category", "short_desc", "extra_desc",
		"context", "vartype", "source", "min_val", "max_val", "enumvals",
		"boot_val", "reset_val", "sourcefile", "sourceline", "pending_restart",
	}

	// Build values array
	var values [][]any
	for _, metric := range metrics {
		row := []any{
			connectionID,
			timestamp,
			metric["name"],
			metric["setting"],
			metric["unit"],
			metric["category"],
			metric["short_desc"],
			metric["extra_desc"],
			metric["context"],
			metric["vartype"],
			metric["source"],
			metric["min_val"],
			metric["max_val"],
			metric["enumvals"],
			metric["boot_val"],
			metric["reset_val"],
			metric["sourcefile"],
			metric["sourceline"],
			metric["pending_restart"],
		}
		values = append(values, row)
	}

	// Use COPY protocol to store metrics
	if err := StoreMetricsWithCopy(ctx, datastoreConn, p.GetTableName(), columns, values); err != nil {
		return fmt.Errorf("failed to store metrics: %w", err)
	}

	return nil
}
