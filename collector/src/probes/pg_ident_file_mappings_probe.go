/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package probes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/collector/src/utils"
	"github.com/pgedge/ai-workbench/pkg/logger"
)

// PgIdentFileMappingsProbe collects metrics from pg_ident_file_mappings view
// This probe only stores data when ident mapping configuration changes are detected
type PgIdentFileMappingsProbe struct {
	BaseMetricsProbe
}

// NewPgIdentFileMappingsProbe creates a new pg_ident_file_mappings probe
func NewPgIdentFileMappingsProbe(config *ProbeConfig) *PgIdentFileMappingsProbe {
	return &PgIdentFileMappingsProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetName returns the probe name
func (p *PgIdentFileMappingsProbe) GetName() string {
	return ProbeNamePgIdentFileMappings
}

// GetTableName returns the metrics table name
func (p *PgIdentFileMappingsProbe) GetTableName() string {
	return ProbeNamePgIdentFileMappings
}

// IsDatabaseScoped returns false as pg_ident_file_mappings is server-scoped
func (p *PgIdentFileMappingsProbe) IsDatabaseScoped() bool {
	return false
}

// GetQuery returns the SQL query to execute (default for PG16+)
func (p *PgIdentFileMappingsProbe) GetQuery() string {
	return p.GetQueryForVersion(16)
}

// GetQueryForVersion returns the appropriate SQL query for the given PostgreSQL version
func (p *PgIdentFileMappingsProbe) GetQueryForVersion(pgVersion int) string {
	if pgVersion >= 16 {
		// PG16+ has map_number column
		return `
            SELECT
                map_number,
                file_name,
                line_number,
                map_name,
                sys_name,
                pg_username,
                error
            FROM pg_ident_file_mappings
            ORDER BY map_number
        `
	}
	// PG15: map_number and file_name don't exist
	return `
        SELECT
            line_number AS map_number,
            NULL::text AS file_name,
            line_number,
            map_name,
            sys_name,
            pg_username,
            error
        FROM pg_ident_file_mappings
        ORDER BY line_number
    `
}

// checkViewAvailable checks if pg_ident_file_mappings view exists (PG 15+)
func (p *PgIdentFileMappingsProbe) checkViewAvailable(ctx context.Context, conn *pgxpool.Conn) (bool, error) {
	var exists bool
	err := conn.QueryRow(ctx, `
        SELECT EXISTS(
            SELECT 1
            FROM pg_views
            WHERE schemaname = 'pg_catalog'
            AND viewname = 'pg_ident_file_mappings'
        )
    `).Scan(&exists)

	if err != nil {
		return false, fmt.Errorf("failed to check for pg_ident_file_mappings view: %w", err)
	}

	return exists, nil
}

// Execute runs the probe against a monitored connection
func (p *PgIdentFileMappingsProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]interface{}, error) {
	// Check if view exists (PG15+)
	available, err := p.checkViewAvailable(ctx, monitoredConn)
	if err != nil {
		return nil, err
	}

	if !available {
		// View not available (PG14 and earlier), return empty metrics
		return []map[string]interface{}{}, nil
	}

	query := p.GetQueryForVersion(pgVersion)
	rows, err := monitoredConn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore only if data has changed
func (p *PgIdentFileMappingsProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
	// Check if data has changed
	changed, err := p.hasDataChanged(ctx, datastoreConn, connectionID, metrics)
	if err != nil {
		return fmt.Errorf("failed to check if data changed: %w", err)
	}

	if !changed {
		// Data unchanged, skip storage
		return nil
	}

	logger.Infof("pg_ident_file_mappings data changed for connection %d, storing new snapshot", connectionID)

	// Ensure partition exists for this timestamp
	if err := p.EnsurePartition(ctx, datastoreConn, timestamp); err != nil {
		return fmt.Errorf("failed to ensure partition: %w", err)
	}

	// Define columns in order
	columns := []string{
		"connection_id", "collected_at",
		"map_number", "file_name", "line_number",
		"map_name", "sys_name", "pg_username", "error",
	}

	// Build values array
	var values [][]interface{}
	for _, metric := range metrics {
		row := []interface{}{
			connectionID,
			timestamp,
			metric["map_number"],
			metric["file_name"],
			metric["line_number"],
			metric["map_name"],
			metric["sys_name"],
			metric["pg_username"],
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

// hasDataChanged checks if the current ident mappings differ from the most recently stored data
func (p *PgIdentFileMappingsProbe) hasDataChanged(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, currentMetrics []map[string]interface{}) (bool, error) {
	// Compute hash of current metrics
	currentHash, err := p.computeMetricsHash(currentMetrics)
	if err != nil {
		return false, fmt.Errorf("failed to compute current metrics hash: %w", err)
	}

	// Get the most recent stored data for this connection
	// Uses a subquery to get the latest collected_at timestamp, then retrieves
	// all rows from that snapshot ordered by map_number (matching the collection query)
	query := `
        SELECT map_number, file_name, line_number, map_name,
               sys_name, pg_username, error
        FROM metrics.pg_ident_file_mappings
        WHERE connection_id = $1
          AND collected_at = (
              SELECT MAX(collected_at)
              FROM metrics.pg_ident_file_mappings
              WHERE connection_id = $1
          )
        ORDER BY map_number
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
func (p *PgIdentFileMappingsProbe) computeMetricsHash(metrics []map[string]interface{}) (string, error) {
	// Marshal metrics to JSON for consistent hashing
	jsonBytes, err := json.Marshal(metrics)
	if err != nil {
		return "", fmt.Errorf("failed to marshal metrics to JSON: %w", err)
	}

	// Compute SHA256 hash
	hash := sha256.Sum256(jsonBytes)
	return hex.EncodeToString(hash[:]), nil
}

// EnsurePartition ensures a partition exists for the given timestamp
func (p *PgIdentFileMappingsProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
