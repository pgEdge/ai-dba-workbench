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

// PgExtensionProbe collects installed PostgreSQL extensions and their versions
// This probe only stores data when changes are detected compared to the most
// recent stored data
type PgExtensionProbe struct {
	BaseMetricsProbe
}

// NewPgExtensionProbe creates a new pg_extension probe
func NewPgExtensionProbe(config *ProbeConfig) *PgExtensionProbe {
	return &PgExtensionProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetName returns the probe name
func (p *PgExtensionProbe) GetName() string {
	return ProbeNamePgExtension
}

// GetTableName returns the metrics table name
func (p *PgExtensionProbe) GetTableName() string {
	return ProbeNamePgExtension
}

// IsDatabaseScoped returns true as extensions can differ per database
func (p *PgExtensionProbe) IsDatabaseScoped() bool {
	return true
}

// GetQuery returns the SQL query to execute
func (p *PgExtensionProbe) GetQuery() string {
	return `
        SELECT
            extname,
            extversion,
            extrelocatable,
            nspname as schema_name
        FROM pg_extension e
        JOIN pg_namespace n ON e.extnamespace = n.oid
        ORDER BY extname
    `
}

// Execute runs the probe against a monitored connection
func (p *PgExtensionProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]interface{}, error) {
	query := WrapQuery(ProbeNamePgExtension, p.GetQuery())
	rows, err := monitoredConn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore only if changes are
// detected
func (p *PgExtensionProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
	if len(metrics) == 0 {
		return nil // Nothing to store
	}

	// Check if extensions have changed compared to the most recent stored data
	hasChanged, err := p.hasDataChanged(ctx, datastoreConn, connectionID, metrics)
	if err != nil {
		return fmt.Errorf("failed to check for changes: %w", err)
	}

	if !hasChanged {
		logger.Infof("pg_extension data unchanged for connection %d, skipping storage", connectionID)
		return nil
	}

	logger.Infof("pg_extension data changed for connection %d, storing new snapshot", connectionID)

	// Ensure partition exists for this timestamp
	if err := p.EnsurePartition(ctx, datastoreConn, timestamp); err != nil {
		return fmt.Errorf("failed to ensure partition: %w", err)
	}

	// Define columns in order
	columns := []string{
		"connection_id", "database_name", "collected_at",
		"extname", "extversion", "extrelocatable", "schema_name",
	}

	// Build values array
	var values [][]interface{}
	for _, metric := range metrics {
		row := []interface{}{
			connectionID,
			metric["_database_name"], // Added by scheduler for database-scoped probes
			timestamp,
			metric["extname"],
			metric["extversion"],
			metric["extrelocatable"],
			metric["schema_name"],
		}
		values = append(values, row)
	}

	// Use COPY protocol to store metrics
	if err := StoreMetricsWithCopy(ctx, datastoreConn, p.GetTableName(), columns, values); err != nil {
		return fmt.Errorf("failed to store metrics: %w", err)
	}

	return nil
}

// hasDataChanged checks if the current extensions differ from the most
// recently stored data
func (p *PgExtensionProbe) hasDataChanged(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, currentMetrics []map[string]interface{}) (bool, error) {
	// Normalize current metrics to match stored format
	// The scheduler adds _database_name but the DB stores it as database_name
	normalizedMetrics := make([]map[string]interface{}, len(currentMetrics))
	for i, m := range currentMetrics {
		normalized := make(map[string]interface{})
		for k, v := range m {
			if k == "_database_name" {
				normalized["database_name"] = v
			} else {
				normalized[k] = v
			}
		}
		normalizedMetrics[i] = normalized
	}

	// Compute hash of normalized current metrics
	currentHash, err := p.computeMetricsHash(normalizedMetrics)
	if err != nil {
		return false, fmt.Errorf("failed to compute current metrics hash: %w", err)
	}

	// Get the most recent stored data for this connection
	// Uses a subquery to get the latest collected_at timestamp, then retrieves
	// all rows from that snapshot ordered by database_name and extname
	query := `
		SELECT database_name, extname, extversion, extrelocatable, schema_name
		FROM metrics.pg_extension
		WHERE connection_id = $1
		  AND collected_at = (
		      SELECT MAX(collected_at)
		      FROM metrics.pg_extension
		      WHERE connection_id = $1
		  )
		ORDER BY database_name, extname
	`

	rows, err := datastoreConn.Query(ctx, query, connectionID)
	if err != nil {
		return false, fmt.Errorf("failed to query most recent data: %w", err)
	}
	defer rows.Close()

	// Scan the most recent data
	var storedMetrics []map[string]interface{}
	storedMetrics, err = utils.ScanRowsToMaps(rows)
	if err != nil {
		return false, fmt.Errorf("failed to scan stored data: %w", err)
	}

	// If there's no stored data, this is the first collection
	if len(storedMetrics) == 0 {
		logger.Infof("No previous pg_extension data found for connection %d", connectionID)
		return true, nil
	}

	// Compute hash of stored metrics
	storedHash, err := p.computeMetricsHash(storedMetrics)
	if err != nil {
		return false, fmt.Errorf("failed to compute stored metrics hash: %w", err)
	}

	// Compare hashes
	return currentHash != storedHash, nil
}

// computeMetricsHash computes a hash of the metrics for comparison
func (p *PgExtensionProbe) computeMetricsHash(metrics []map[string]interface{}) (string, error) {
	return ComputeMetricsHash(metrics)
}

// EnsurePartition ensures a partition exists for the given timestamp
func (p *PgExtensionProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
