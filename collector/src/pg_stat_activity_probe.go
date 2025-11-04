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

// PgStatActivityProbe collects metrics from pg_stat_activity view
type PgStatActivityProbe struct {
	BaseMetricsProbe
}

// NewPgStatActivityProbe creates a new pg_stat_activity probe
func NewPgStatActivityProbe(config *ProbeConfig) *PgStatActivityProbe {
	return &PgStatActivityProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetName returns the probe name
func (p *PgStatActivityProbe) GetName() string {
	return "pg_stat_activity"
}

// GetTableName returns the metrics table name
func (p *PgStatActivityProbe) GetTableName() string {
	return "pg_stat_activity"
}

// IsDatabaseScoped returns false as pg_stat_activity is server-scoped
func (p *PgStatActivityProbe) IsDatabaseScoped() bool {
	return false
}

// GetQuery returns the SQL query to execute
func (p *PgStatActivityProbe) GetQuery() string {
	return `
		SELECT
			datid,
			datname,
			pid,
			leader_pid,
			usesysid,
			usename,
			application_name,
			client_addr,
			client_hostname,
			client_port,
			backend_start,
			xact_start,
			query_start,
			state_change,
			wait_event_type,
			wait_event,
			state,
			backend_xid::text,
			backend_xmin::text,
			query,
			backend_type
		FROM pg_stat_activity
		WHERE pid <> pg_backend_pid()  -- Exclude the current backend
	`
}

// Execute runs the probe against a monitored connection
func (p *PgStatActivityProbe) Execute(ctx context.Context, monitoredDB *sql.DB) ([]map[string]interface{}, error) {
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
func (p *PgStatActivityProbe) Store(ctx context.Context, datastoreDB *sql.DB, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
	if len(metrics) == 0 {
		return nil // Nothing to store
	}

	// Ensure partition exists for this timestamp
	if err := p.EnsurePartition(ctx, datastoreDB, timestamp); err != nil {
		return fmt.Errorf("failed to ensure partition: %w", err)
	}

	// Define columns in order
	columns := []string{
		"connection_id", "collected_at",
		"datid", "datname", "pid", "leader_pid", "usesysid", "usename",
		"application_name", "client_addr", "client_hostname", "client_port",
		"backend_start", "xact_start", "query_start", "state_change",
		"wait_event_type", "wait_event", "state",
		"backend_xid", "backend_xmin", "query", "backend_type",
	}

	// Build values array
	var values [][]interface{}
	for _, metric := range metrics {
		row := []interface{}{
			connectionID,
			timestamp,
			metric["datid"],
			metric["datname"],
			metric["pid"],
			metric["leader_pid"],
			metric["usesysid"],
			metric["usename"],
			metric["application_name"],
			metric["client_addr"],
			metric["client_hostname"],
			metric["client_port"],
			metric["backend_start"],
			metric["xact_start"],
			metric["query_start"],
			metric["state_change"],
			metric["wait_event_type"],
			metric["wait_event"],
			metric["state"],
			metric["backend_xid"],
			metric["backend_xmin"],
			metric["query"],
			metric["backend_type"],
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
func (p *PgStatActivityProbe) EnsurePartition(ctx context.Context, datastoreDB *sql.DB, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreDB, p.GetTableName(), timestamp)
}
