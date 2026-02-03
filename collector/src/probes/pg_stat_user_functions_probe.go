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

// PgStatUserFunctionsProbe collects metrics from pg_stat_user_functions view
type PgStatUserFunctionsProbe struct {
	BaseMetricsProbe
}

// NewPgStatUserFunctionsProbe creates a new pg_stat_user_functions probe
func NewPgStatUserFunctionsProbe(config *ProbeConfig) *PgStatUserFunctionsProbe {
	return &PgStatUserFunctionsProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetName returns the probe name
func (p *PgStatUserFunctionsProbe) GetName() string {
	return ProbeNamePgStatUserFunctions
}

// GetTableName returns the metrics table name
func (p *PgStatUserFunctionsProbe) GetTableName() string {
	return ProbeNamePgStatUserFunctions
}

// IsDatabaseScoped returns true as pg_stat_user_functions is database-scoped
func (p *PgStatUserFunctionsProbe) IsDatabaseScoped() bool {
	return true
}

// GetQuery returns the SQL query to execute
func (p *PgStatUserFunctionsProbe) GetQuery() string {
	return `
        SELECT
            funcid,
            schemaname,
            funcname,
            calls,
            total_time,
            self_time
        FROM pg_stat_user_functions
        ORDER BY schemaname, funcname
    `
}

// Execute runs the probe against a monitored connection
func (p *PgStatUserFunctionsProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]interface{}, error) {
	rows, err := monitoredConn.Query(ctx, p.GetQuery())
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgStatUserFunctionsProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
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
		"funcid", "schemaname", "funcname",
		"calls", "total_time", "self_time",
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
			metric["funcid"],
			metric["schemaname"],
			metric["funcname"],
			metric["calls"],
			metric["total_time"],
			metric["self_time"],
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
func (p *PgStatUserFunctionsProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
