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

// PgStatDatabaseConflictsProbe collects metrics from pg_stat_database_conflicts view
type PgStatDatabaseConflictsProbe struct {
	BaseMetricsProbe
}

// NewPgStatDatabaseConflictsProbe creates a new pg_stat_database_conflicts probe
func NewPgStatDatabaseConflictsProbe(config *ProbeConfig) *PgStatDatabaseConflictsProbe {
	return &PgStatDatabaseConflictsProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetName returns the probe name
func (p *PgStatDatabaseConflictsProbe) GetName() string {
	return ProbeNamePgStatDatabaseConflicts
}

// GetTableName returns the metrics table name
func (p *PgStatDatabaseConflictsProbe) GetTableName() string {
	return ProbeNamePgStatDatabaseConflicts
}

// IsDatabaseScoped returns true as pg_stat_database_conflicts is database-scoped
func (p *PgStatDatabaseConflictsProbe) IsDatabaseScoped() bool {
	return true
}

// GetQuery returns the SQL query to execute
func (p *PgStatDatabaseConflictsProbe) GetQuery() string {
	return `
        SELECT
            datid,
            datname,
            confl_tablespace,
            confl_lock,
            confl_snapshot,
            confl_bufferpin,
            confl_deadlock,
            confl_active_logicalslot
        FROM pg_stat_database_conflicts
        WHERE datname = current_database()
    `
}

// Execute runs the probe against a monitored connection
func (p *PgStatDatabaseConflictsProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn) ([]map[string]interface{}, error) {
	rows, err := monitoredConn.Query(ctx, p.GetQuery())
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgStatDatabaseConflictsProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
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
		"datid", "datname",
		"confl_tablespace", "confl_lock", "confl_snapshot",
		"confl_bufferpin", "confl_deadlock", "confl_active_logicalslot",
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
			metric["datid"],
			metric["datname"],
			metric["confl_tablespace"],
			metric["confl_lock"],
			metric["confl_snapshot"],
			metric["confl_bufferpin"],
			metric["confl_deadlock"],
			metric["confl_active_logicalslot"],
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
func (p *PgStatDatabaseConflictsProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
