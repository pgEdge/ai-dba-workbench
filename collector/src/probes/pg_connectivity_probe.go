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
)

// PgConnectivityProbe checks database connectivity and measures response time
type PgConnectivityProbe struct {
	BaseMetricsProbe
}

// NewPgConnectivityProbe creates a new pg_connectivity probe
func NewPgConnectivityProbe(config *ProbeConfig) *PgConnectivityProbe {
	return &PgConnectivityProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetQuery returns the SQL query to execute (not used)
func (p *PgConnectivityProbe) GetQuery() string {
	return "" // Not used - Execute() runs SELECT 1 directly
}

// Execute runs the probe against a monitored connection
func (p *PgConnectivityProbe) Execute(ctx context.Context, _ string, monitoredConn *pgxpool.Conn, _ int) ([]map[string]any, error) {
	// Create a 5-second timeout context for the connectivity check
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	start := time.Now()
	var result int
	err := monitoredConn.QueryRow(queryCtx, "SELECT 1").Scan(&result)
	elapsed := time.Since(start)

	if err != nil {
		return nil, fmt.Errorf("connectivity check failed: %w", err)
	}

	responseTimeMs := float64(elapsed.Nanoseconds()) / 1e6

	metric := map[string]any{
		"response_time_ms": responseTimeMs,
	}

	return []map[string]any{metric}, nil
}

// Store stores the collected metrics in the datastore
func (p *PgConnectivityProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]any) error {
	if len(metrics) == 0 {
		return nil // Nothing to store
	}

	// Ensure partition exists for this timestamp
	if err := p.EnsurePartition(ctx, datastoreConn, timestamp); err != nil {
		return fmt.Errorf("failed to ensure partition: %w", err)
	}

	// Define columns in order
	columns := []string{
		"connection_id", "collected_at", "response_time_ms",
	}

	// Build values array
	var values [][]any
	for _, metric := range metrics {
		row := []any{
			connectionID,
			timestamp,
			metric["response_time_ms"],
		}
		values = append(values, row)
	}

	// Use INSERT to store metrics
	if err := StoreMetricsWithCopy(ctx, datastoreConn, p.GetTableName(), columns, values); err != nil {
		return fmt.Errorf("failed to store metrics: %w", err)
	}

	return nil
}
