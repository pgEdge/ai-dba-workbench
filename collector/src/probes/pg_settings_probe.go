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

// PgSettingsProbe collects PostgreSQL configuration settings
// This probe only stores data when changes are detected compared to the most recent stored data
type PgSettingsProbe struct {
	BaseMetricsProbe
}

// NewPgSettingsProbe creates a new pg_settings probe
func NewPgSettingsProbe(config *ProbeConfig) *PgSettingsProbe {
	return &PgSettingsProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetName returns the probe name
func (p *PgSettingsProbe) GetName() string {
	return ProbeNamePgSettings
}

// GetTableName returns the metrics table name
func (p *PgSettingsProbe) GetTableName() string {
	return ProbeNamePgSettings
}

// IsDatabaseScoped returns false as pg_settings is server-scoped
func (p *PgSettingsProbe) IsDatabaseScoped() bool {
	return false
}

// GetQuery returns the SQL query to execute
func (p *PgSettingsProbe) GetQuery() string {
	return `
        SELECT
            name,
            setting,
            unit,
            category,
            short_desc,
            extra_desc,
            context,
            vartype,
            source,
            min_val,
            max_val,
            enumvals,
            boot_val,
            reset_val,
            sourcefile,
            sourceline,
            pending_restart
        FROM pg_settings
        ORDER BY name
    `
}

// Execute runs the probe against a monitored connection
func (p *PgSettingsProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]interface{}, error) {
	rows, err := monitoredConn.Query(ctx, p.GetQuery())
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore only if changes are detected
func (p *PgSettingsProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
	if len(metrics) == 0 {
		return nil // Nothing to store
	}

	// Check if settings have changed compared to the most recent stored data
	hasChanged, err := p.hasDataChanged(ctx, datastoreConn, connectionID, metrics)
	if err != nil {
		return fmt.Errorf("failed to check for changes: %w", err)
	}

	if !hasChanged {
		logger.Infof("pg_settings data unchanged for connection %d, skipping storage", connectionID)
		return nil
	}

	logger.Infof("pg_settings data changed for connection %d, storing new snapshot", connectionID)

	// Ensure partition exists for this timestamp
	if err := p.EnsurePartition(ctx, datastoreConn, timestamp); err != nil {
		return fmt.Errorf("failed to ensure partition: %w", err)
	}

	// Define columns in order
	columns := []string{
		"connection_id", "collected_at",
		"name", "setting", "unit", "category", "short_desc", "extra_desc",
		"context", "vartype", "source", "min_val", "max_val", "enumvals",
		"boot_val", "reset_val", "sourcefile", "sourceline", "pending_restart",
	}

	// Build values array
	var values [][]interface{}
	for _, metric := range metrics {
		row := []interface{}{
			connectionID,
			timestamp,
			metric["name"],
			metric["setting"],
			metric["unit"],
			metric["category"],
			metric["short_desc"],
			metric["extra_desc"],
			metric["context"],
			metric["vartype"],
			metric["source"],
			metric["min_val"],
			metric["max_val"],
			metric["enumvals"],
			metric["boot_val"],
			metric["reset_val"],
			metric["sourcefile"],
			metric["sourceline"],
			metric["pending_restart"],
		}
		values = append(values, row)
	}

	// Use COPY protocol to store metrics
	if err := StoreMetricsWithCopy(ctx, datastoreConn, p.GetTableName(), columns, values); err != nil {
		return fmt.Errorf("failed to store metrics: %w", err)
	}

	return nil
}

// hasDataChanged checks if the current settings differ from the most recently stored data
func (p *PgSettingsProbe) hasDataChanged(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, currentMetrics []map[string]interface{}) (bool, error) {
	// Compute hash of current metrics
	currentHash, err := p.computeMetricsHash(currentMetrics)
	if err != nil {
		return false, fmt.Errorf("failed to compute current metrics hash: %w", err)
	}

	// Get the most recent stored data for this connection
	query := `
		SELECT name, setting, unit, category, short_desc, extra_desc,
		       context, vartype, source, min_val, max_val, enumvals,
		       boot_val, reset_val, sourcefile, sourceline, pending_restart
		FROM metrics.pg_settings
		WHERE connection_id = $1
		ORDER BY collected_at DESC
		LIMIT (SELECT COUNT(*) FROM pg_settings)
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
		logger.Infof("No previous pg_settings data found for connection %d", connectionID)
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
func (p *PgSettingsProbe) computeMetricsHash(metrics []map[string]interface{}) (string, error) {
	// Convert metrics to a canonical JSON representation
	jsonBytes, err := json.Marshal(metrics)
	if err != nil {
		return "", fmt.Errorf("failed to marshal metrics to JSON: %w", err)
	}

	// Compute SHA256 hash
	hash := sha256.Sum256(jsonBytes)
	return hex.EncodeToString(hash[:]), nil
}

// EnsurePartition ensures a partition exists for the given timestamp
func (p *PgSettingsProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
