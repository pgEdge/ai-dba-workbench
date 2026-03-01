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
func (p *PgStatActivityProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]any, error) {
	query := WrapQuery(ProbeNamePgStatActivity, p.GetQuery())
	rows, err := monitoredConn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgStatActivityProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]any) error {
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
		"datid", "datname", "pid", "leader_pid", "usesysid", "usename",
		"application_name", "client_addr", "client_hostname", "client_port",
		"backend_start", "xact_start", "query_start", "state_change",
		"wait_event_type", "wait_event", "state",
		"backend_xid", "backend_xmin", "query", "backend_type",
	}

	// Build values array
	var values [][]any
	for _, metric := range metrics {
		row := []any{
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
	if err := StoreMetricsWithCopy(ctx, datastoreConn, p.GetTableName(), columns, values); err != nil {
		return fmt.Errorf("failed to store metrics: %w", err)
	}

	return nil
}

// EnsurePartition ensures a partition exists for the given timestamp
func (p *PgStatActivityProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
