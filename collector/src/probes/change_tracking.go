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

// probeMarkerColumn is the column name injected by WrapQuery into
// every collected probe result. Stored snapshots do not include this
// column, so it must be stripped before hashing or the live and
// stored hashes will never match and change-detection will misfire on
// every collection cycle. See WrapQuery in base.go.
const probeMarkerColumn = "ai_dba_wb_probe"

// HasDataChanged checks whether currentMetrics differ from the
// most recently stored data for the given connection. The
// fetchStoredQuery must be a SQL query that accepts a single $1
// parameter (connection_id) and returns the columns to compare.
// The optional normalizeMetrics function transforms collected
// metrics before hashing (e.g., renaming _database_name to
// database_name); pass nil to skip normalization.
//
// Live probe metrics produced via WrapQuery + ScanRowsToMaps include
// the synthetic probeMarkerColumn ("ai_dba_wb_probe") used by the
// monitoring panels to filter collector queries. Stored snapshots do
// not include that column, so HasDataChanged strips it from every
// row before hashing. This is essential for change-detection probes
// (pg_settings, pg_extension, pg_hba_file_rules,
// pg_ident_file_mappings); previously, the marker caused every
// hourly collection to look "changed" and write a fresh snapshot,
// inflating partition sizes by an order of magnitude.
func HasDataChanged(
	ctx context.Context,
	datastoreConn *pgxpool.Conn,
	connectionID int,
	probeName string,
	currentMetrics []map[string]any,
	fetchStoredQuery string,
	normalizeMetrics func([]map[string]any) []map[string]any,
) (bool, error) {
	// Strip the probe marker column injected by WrapQuery before any
	// further normalization or hashing. The stored query does not
	// project this column, so leaving it in causes a guaranteed hash
	// mismatch on every collection cycle.
	metricsToHash := stripProbeMarker(currentMetrics)

	// Apply caller-supplied normalization if provided.
	if normalizeMetrics != nil {
		metricsToHash = normalizeMetrics(metricsToHash)
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

// stripProbeMarker returns a copy of metrics with the probe marker
// column removed from every row. If no row contains the marker the
// input slice is returned unchanged to avoid an unnecessary
// allocation. The function never mutates its input.
func stripProbeMarker(metrics []map[string]any) []map[string]any {
	// Detect whether any row contains the marker; if none do, the
	// caller already has the right shape and we can skip the copy.
	hasMarker := false
	for _, m := range metrics {
		if _, ok := m[probeMarkerColumn]; ok {
			hasMarker = true
			break
		}
	}
	if !hasMarker {
		return metrics
	}

	result := make([]map[string]any, len(metrics))
	for i, m := range metrics {
		// Allocate at most len(m) entries; one slot is freed when the
		// marker key is dropped, but the over-allocation is bounded
		// and avoids a second map grow.
		cleaned := make(map[string]any, len(m))
		for k, v := range m {
			if k == probeMarkerColumn {
				continue
			}
			cleaned[k] = v
		}
		result[i] = cleaned
	}
	return result
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
