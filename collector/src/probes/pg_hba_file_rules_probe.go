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

// PgHbaFileRulesProbe collects metrics from pg_hba_file_rules view
// This probe only stores data when HBA configuration changes are detected
type PgHbaFileRulesProbe struct {
	BaseMetricsProbe
}

// NewPgHbaFileRulesProbe creates a new pg_hba_file_rules probe
func NewPgHbaFileRulesProbe(config *ProbeConfig) *PgHbaFileRulesProbe {
	return &PgHbaFileRulesProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetName returns the probe name
func (p *PgHbaFileRulesProbe) GetName() string {
	return ProbeNamePgHbaFileRules
}

// GetTableName returns the metrics table name
func (p *PgHbaFileRulesProbe) GetTableName() string {
	return ProbeNamePgHbaFileRules
}

// IsDatabaseScoped returns false as pg_hba_file_rules is server-scoped
func (p *PgHbaFileRulesProbe) IsDatabaseScoped() bool {
	return false
}

// GetQuery returns the SQL query to execute (default for PG16+)
func (p *PgHbaFileRulesProbe) GetQuery() string {
	return p.GetQueryForVersion(16)
}

// GetQueryForVersion returns the appropriate SQL query for the given PostgreSQL version
func (p *PgHbaFileRulesProbe) GetQueryForVersion(pgVersion int) string {
	if pgVersion >= 16 {
		// PG16+ has rule_number column
		return `
            SELECT
                rule_number,
                file_name,
                line_number,
                type,
                database,
                user_name,
                address,
                netmask,
                auth_method,
                options,
                error
            FROM pg_hba_file_rules
            ORDER BY rule_number
        `
	}
	// PG14-15: rule_number and file_name don't exist
	return `
        SELECT
            line_number AS rule_number,
            NULL::text AS file_name,
            line_number,
            type,
            database,
            user_name,
            address,
            netmask,
            auth_method,
            options,
            error
        FROM pg_hba_file_rules
        ORDER BY line_number
    `
}

// Execute runs the probe against a monitored connection
func (p *PgHbaFileRulesProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]interface{}, error) {
	query := p.GetQueryForVersion(pgVersion)
	rows, err := monitoredConn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore only if data has changed
func (p *PgHbaFileRulesProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
	// Check if data has changed
	changed, err := p.hasDataChanged(ctx, datastoreConn, connectionID, metrics)
	if err != nil {
		return fmt.Errorf("failed to check if data changed: %w", err)
	}

	if !changed {
		// Data unchanged, skip storage
		return nil
	}

	logger.Infof("pg_hba_file_rules data changed for connection %d, storing new snapshot", connectionID)

	// Ensure partition exists for this timestamp
	if err := p.EnsurePartition(ctx, datastoreConn, timestamp); err != nil {
		return fmt.Errorf("failed to ensure partition: %w", err)
	}

	// Define columns in order
	columns := []string{
		"connection_id", "collected_at",
		"rule_number", "file_name", "line_number", "type",
		"database", "user_name", "address", "netmask",
		"auth_method", "options", "error",
	}

	// Build values array
	var values [][]interface{}
	for _, metric := range metrics {
		row := []interface{}{
			connectionID,
			timestamp,
			metric["rule_number"],
			metric["file_name"],
			metric["line_number"],
			metric["type"],
			metric["database"],
			metric["user_name"],
			metric["address"],
			metric["netmask"],
			metric["auth_method"],
			metric["options"],
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

// hasDataChanged checks if the current HBA rules differ from the most recently stored data
func (p *PgHbaFileRulesProbe) hasDataChanged(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, currentMetrics []map[string]interface{}) (bool, error) {
	// Compute hash of current metrics
	currentHash, err := p.computeMetricsHash(currentMetrics)
	if err != nil {
		return false, fmt.Errorf("failed to compute current metrics hash: %w", err)
	}

	// Get the most recent stored data for this connection
	// Uses a subquery to get the latest collected_at timestamp, then retrieves
	// all rows from that snapshot ordered by rule_number (matching the collection query)
	query := `
        SELECT rule_number, file_name, line_number, type, database,
               user_name, address, netmask, auth_method, options, error
        FROM metrics.pg_hba_file_rules
        WHERE connection_id = $1
          AND collected_at = (
              SELECT MAX(collected_at)
              FROM metrics.pg_hba_file_rules
              WHERE connection_id = $1
          )
        ORDER BY rule_number
    `

	rows, err := datastoreConn.Query(ctx, query, connectionID)
	if err != nil {
		return false, fmt.Errorf("failed to query most recent data: %w", err)
	}
	defer rows.Close()

	previousMetrics, err := utils.ScanRowsToMaps(rows)
	if err != nil {
		return false, fmt.Errorf("failed to scan previous metrics: %w", err)
	}

	// If no previous data exists, this is a change
	if len(previousMetrics) == 0 {
		return true, nil
	}

	// Compute hash of previous metrics
	previousHash, err := p.computeMetricsHash(previousMetrics)
	if err != nil {
		return false, fmt.Errorf("failed to compute previous metrics hash: %w", err)
	}

	// Compare hashes
	return currentHash != previousHash, nil
}

// computeMetricsHash computes a SHA256 hash of metrics for comparison
func (p *PgHbaFileRulesProbe) computeMetricsHash(metrics []map[string]interface{}) (string, error) {
	return ComputeMetricsHash(metrics)
}

// EnsurePartition ensures a partition exists for the given timestamp
func (p *PgHbaFileRulesProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
