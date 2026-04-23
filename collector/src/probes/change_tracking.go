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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/collector/src/utils"
	"github.com/pgedge/ai-workbench/pkg/logger"
)

// HasDataChanged checks whether currentMetrics differ from the
// most recently stored data for the given connection. The
// fetchStoredQuery must be a SQL query that accepts a single $1
// parameter (connection_id) and returns the columns to compare.
// The optional normalizeMetrics function transforms collected
// metrics before hashing (e.g., renaming _database_name to
// database_name); pass nil to skip normalization.
func HasDataChanged(
	ctx context.Context,
	datastoreConn *pgxpool.Conn,
	connectionID int,
	probeName string,
	currentMetrics []map[string]any,
	fetchStoredQuery string,
	normalizeMetrics func([]map[string]any) []map[string]any,
) (bool, error) {
	// Apply normalization if provided
	metricsToHash := currentMetrics
	if normalizeMetrics != nil {
		metricsToHash = normalizeMetrics(currentMetrics)
	}

	currentHash, err := ComputeMetricsHash(metricsToHash)
	if err != nil {
		return false, fmt.Errorf(
			"failed to compute current metrics hash: %w", err)
	}

	rows, err := datastoreConn.Query(
		ctx, fetchStoredQuery, connectionID)
	if err != nil {
		return false, fmt.Errorf(
			"failed to query most recent data: %w", err)
	}
	defer rows.Close()

	storedMetrics, err := utils.ScanRowsToMaps(rows)
	if err != nil {
		return false, fmt.Errorf(
			"failed to scan stored data: %w", err)
	}

	storedHash, err := ComputeMetricsHash(storedMetrics)
	if err != nil {
		return false, fmt.Errorf(
			"failed to compute stored metrics hash: %w", err)
	}

	changed := currentHash != storedHash
	if changed && len(storedMetrics) == 0 {
		logger.Infof(
			"No previous %s data found for connection %d",
			probeName, connectionID)
	}

	return changed, nil
}

// normalizeDatabaseName renames _database_name keys to
// database_name to match the stored column name.
func normalizeDatabaseName(metrics []map[string]any) []map[string]any {
	result := make([]map[string]any, len(metrics))
	for i, m := range metrics {
		normalized := make(map[string]any, len(m))
		for k, v := range m {
			if k == "_database_name" {
				normalized["database_name"] = v
			} else {
				normalized[k] = v
			}
		}
		result[i] = normalized
	}
	return result
}
