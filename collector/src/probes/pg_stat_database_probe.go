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

// PgStatDatabaseProbe collects metrics from pg_stat_database view
type PgStatDatabaseProbe struct {
	BaseMetricsProbe
}

// NewPgStatDatabaseProbe creates a new pg_stat_database probe
func NewPgStatDatabaseProbe(config *ProbeConfig) *PgStatDatabaseProbe {
	return &PgStatDatabaseProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetName returns the probe name
func (p *PgStatDatabaseProbe) GetName() string {
	return ProbeNamePgStatDatabase
}

// GetTableName returns the metrics table name
func (p *PgStatDatabaseProbe) GetTableName() string {
	return ProbeNamePgStatDatabase
}

// IsDatabaseScoped returns true as pg_stat_database is database-scoped
func (p *PgStatDatabaseProbe) IsDatabaseScoped() bool {
	return true
}

// GetQuery returns the SQL query to execute
func (p *PgStatDatabaseProbe) GetQuery() string {
	return `
        SELECT
            datid,
            datname,
            numbackends,
            xact_commit,
            xact_rollback,
            blks_read,
            blks_hit,
            tup_returned,
            tup_fetched,
            tup_inserted,
            tup_updated,
            tup_deleted,
            conflicts,
            temp_files,
            temp_bytes,
            deadlocks,
            checksum_failures,
            checksum_last_failure,
            blk_read_time,
            blk_write_time,
            session_time,
            active_time,
            idle_in_transaction_time,
            sessions,
            sessions_abandoned,
            sessions_fatal,
            sessions_killed,
            stats_reset
        FROM pg_stat_database
        WHERE datname = current_database()
    `
}

// Execute runs the probe against a monitored connection
func (p *PgStatDatabaseProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]interface{}, error) {
	query := WrapQuery(ProbeNamePgStatDatabase, p.GetQuery())
	rows, err := monitoredConn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgStatDatabaseProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
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
		"datid", "datname", "numbackends",
		"xact_commit", "xact_rollback",
		"blks_read", "blks_hit",
		"tup_returned", "tup_fetched", "tup_inserted", "tup_updated", "tup_deleted",
		"conflicts", "temp_files", "temp_bytes", "deadlocks",
		"checksum_failures", "checksum_last_failure",
		"blk_read_time", "blk_write_time",
		"session_time", "active_time", "idle_in_transaction_time",
		"sessions", "sessions_abandoned", "sessions_fatal", "sessions_killed",
		"stats_reset",
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
			metric["numbackends"],
			metric["xact_commit"],
			metric["xact_rollback"],
			metric["blks_read"],
			metric["blks_hit"],
			metric["tup_returned"],
			metric["tup_fetched"],
			metric["tup_inserted"],
			metric["tup_updated"],
			metric["tup_deleted"],
			metric["conflicts"],
			metric["temp_files"],
			metric["temp_bytes"],
			metric["deadlocks"],
			metric["checksum_failures"],
			metric["checksum_last_failure"],
			metric["blk_read_time"],
			metric["blk_write_time"],
			metric["session_time"],
			metric["active_time"],
			metric["idle_in_transaction_time"],
			metric["sessions"],
			metric["sessions_abandoned"],
			metric["sessions_fatal"],
			metric["sessions_killed"],
			metric["stats_reset"],
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
func (p *PgStatDatabaseProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
