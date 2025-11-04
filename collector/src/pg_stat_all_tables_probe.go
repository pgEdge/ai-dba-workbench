/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"
)

// PgStatAllTablesProbe collects metrics from pg_stat_all_tables view
type PgStatAllTablesProbe struct {
	BaseMetricsProbe
}

// NewPgStatAllTablesProbe creates a new pg_stat_all_tables probe
func NewPgStatAllTablesProbe(config *ProbeConfig) *PgStatAllTablesProbe {
	return &PgStatAllTablesProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetName returns the probe name
func (p *PgStatAllTablesProbe) GetName() string {
	return "pg_stat_all_tables"
}

// GetTableName returns the metrics table name
func (p *PgStatAllTablesProbe) GetTableName() string {
	return "pg_stat_all_tables"
}

// IsDatabaseScoped returns true as pg_stat_all_tables is database-scoped
func (p *PgStatAllTablesProbe) IsDatabaseScoped() bool {
	return true
}

// GetQuery returns the SQL query to execute
func (p *PgStatAllTablesProbe) GetQuery() string {
	return `
        SELECT
            schemaname,
            relname,
            seq_scan,
            seq_tup_read,
            idx_scan,
            idx_tup_fetch,
            n_tup_ins,
            n_tup_upd,
            n_tup_del,
            n_tup_hot_upd,
            n_live_tup,
            n_dead_tup,
            n_mod_since_analyze,
            last_vacuum,
            last_autovacuum,
            last_analyze,
            last_autoanalyze,
            vacuum_count,
            autovacuum_count,
            analyze_count,
            autoanalyze_count
        FROM pg_stat_all_tables
        ORDER BY schemaname, relname
    `
}

// Execute runs the probe against a monitored connection
func (p *PgStatAllTablesProbe) Execute(ctx context.Context, monitoredDB *sql.DB) ([]map[string]interface{}, error) {
	rows, err := monitoredDB.QueryContext(ctx, p.GetQuery())
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			log.Printf("Error closing rows: %v", cerr)
		}
	}()

	// Get column names
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	// Prepare result set
	var results []map[string]interface{}

	for rows.Next() {
		// Create a slice of interface{} to hold values
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		// Scan the row
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Create a map for this row
		rowMap := make(map[string]interface{})
		for i, colName := range columns {
			val := values[i]
			// Convert []byte to string for readability
			if b, ok := val.([]byte); ok {
				rowMap[colName] = string(b)
			} else {
				rowMap[colName] = val
			}
		}

		results = append(results, rowMap)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return results, nil
}

// Store stores the collected metrics in the datastore
func (p *PgStatAllTablesProbe) Store(ctx context.Context, datastoreDB *sql.DB, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
	if len(metrics) == 0 {
		return nil // Nothing to store
	}

	// Ensure partition exists for this timestamp
	if err := p.EnsurePartition(ctx, datastoreDB, timestamp); err != nil {
		return fmt.Errorf("failed to ensure partition: %w", err)
	}

	// Define columns in order
	columns := []string{
		"connection_id", "collected_at", "database_name",
		"schemaname", "relname",
		"seq_scan", "seq_tup_read", "idx_scan", "idx_tup_fetch",
		"n_tup_ins", "n_tup_upd", "n_tup_del", "n_tup_hot_upd",
		"n_live_tup", "n_dead_tup", "n_mod_since_analyze",
		"last_vacuum", "last_autovacuum", "last_analyze", "last_autoanalyze",
		"vacuum_count", "autovacuum_count", "analyze_count", "autoanalyze_count",
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
			metric["schemaname"],
			metric["relname"],
			metric["seq_scan"],
			metric["seq_tup_read"],
			metric["idx_scan"],
			metric["idx_tup_fetch"],
			metric["n_tup_ins"],
			metric["n_tup_upd"],
			metric["n_tup_del"],
			metric["n_tup_hot_upd"],
			metric["n_live_tup"],
			metric["n_dead_tup"],
			metric["n_mod_since_analyze"],
			metric["last_vacuum"],
			metric["last_autovacuum"],
			metric["last_analyze"],
			metric["last_autoanalyze"],
			metric["vacuum_count"],
			metric["autovacuum_count"],
			metric["analyze_count"],
			metric["autoanalyze_count"],
		}
		values = append(values, row)
	}

	// Use COPY protocol to store metrics
	if err := StoreMetricsWithCopy(ctx, datastoreDB, p.GetTableName(), columns, values); err != nil {
		return fmt.Errorf("failed to store metrics: %w", err)
	}

	return nil
}

// EnsurePartition ensures a partition exists for the given timestamp
func (p *PgStatAllTablesProbe) EnsurePartition(ctx context.Context, datastoreDB *sql.DB, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreDB, p.GetTableName(), timestamp)
}
