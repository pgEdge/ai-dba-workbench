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
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/collector/src/utils"
	"github.com/pgedge/ai-workbench/pkg/logger"
	"github.com/pgedge/ai-workbench/pkg/sqlmarker"
)

// probeMarkerColumn is the column name injected by WrapQuery into
// every collected probe result. Stored snapshots do not include this
// column, so it must be stripped before hashing or the live and
// stored hashes will never match and change-detection will misfire on
// every collection cycle. See WrapQuery in base.go.
const probeMarkerColumn = sqlmarker.ProbeAlias

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

	// Rows ColdFront refused when this snapshot was last stored are not
	// in the stored snapshot, so they are left out of the comparison.
	metricsToHash = refusedSnapshotRows.exclude(probeName, connectionID, metricsToHash)

	currentHash, err := ComputeMetricsHash(metricsToHash)
	if err != nil {
		return false, fmt.Errorf(
			"failed to compute current metrics hash: %w", err)
	}

	// The stored-snapshot read runs against the Workbench's own
	// datastore, so tag it as internal to keep it out of the Top
	// Queries panel when monitoring queries are hidden.
	rows, err := datastoreConn.Query(
		ctx, sqlmarker.Tag(fetchStoredQuery), connectionID)
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

// snapshotRows holds the snapshot a change-tracked probe stores: the
// table columns, the rows to insert, and the metrics each row was built
// from, so that values[i] is the row built from metrics[i].
type snapshotRows struct {
	columns []string
	values  [][]any
	metrics []map[string]any
}

// storeSnapshot stores the snapshot of a change-tracked probe and
// records the rows that were dropped because ColdFront refused them, so
// that HasDataChanged leaves them out of the next comparison. Without
// that, the stored
// snapshot lacks the refused row, every later comparison sees a change,
// and the probe stores its whole snapshot again on every run.
//
// normalizeMetrics is the function the probe passes to HasDataChanged.
// After a store that drops nothing, as always in the default mode,
// nothing is left out.
func storeSnapshot(
	ctx context.Context,
	datastoreConn *pgxpool.Conn,
	probeName string,
	connectionID int,
	tableName string,
	snapshot snapshotRows,
	normalizeMetrics func([]map[string]any) []map[string]any,
) error {
	dropped, err := storeMetrics(ctx, datastoreConn, tableName, snapshot.columns, snapshot.values)
	if err != nil {
		return err
	}
	refused := make([]map[string]any, 0, len(dropped))
	for _, i := range dropped {
		refused = append(refused, snapshot.metrics[i])
	}
	refused = stripProbeMarker(refused)
	if normalizeMetrics != nil {
		refused = normalizeMetrics(refused)
	}
	refusedSnapshotRows.set(probeName, connectionID, refused)
	return nil
}

// refusedSnapshotRows holds, for each change-tracked probe and
// connection, the rows ColdFront refused when the probe last stored its
// snapshot. It lives in memory only: after a restart a probe with a
// refused row stores its snapshot once more and then settles again.
var refusedSnapshotRows = &refusedRowSet{rows: map[refusedRowKey]map[string]struct{}{}}

// refusedRowKey identifies a change-tracked probe on one connection.
type refusedRowKey struct {
	probe        string
	connectionID int
}

// refusedRowSet records refused rows by their hash, as
// ComputeMetricsHash gives it for the row on its own. It is safe for
// concurrent use. A probe drops at most maxColdFrontDroppedRows rows per
// store, so each entry stays small.
type refusedRowSet struct {
	mu   sync.Mutex
	rows map[refusedRowKey]map[string]struct{}
}

// rowHash returns the hash of one row, or "" if it cannot be hashed, in
// which case it never matches a recorded row.
func rowHash(row map[string]any) string {
	h, err := ComputeMetricsHash([]map[string]any{row})
	if err != nil {
		return ""
	}
	return h
}

// set replaces the refused rows recorded for probe on connectionID.
func (s *refusedRowSet) set(probe string, connectionID int, rows []map[string]any) {
	key := refusedRowKey{probe, connectionID}
	hashes := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if h := rowHash(row); h != "" {
			hashes[h] = struct{}{}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(hashes) == 0 {
		delete(s.rows, key)
		return
	}
	s.rows[key] = hashes
}

// exclude returns metrics without the rows recorded as refused for probe
// on connectionID, or metrics itself when none are recorded.
func (s *refusedRowSet) exclude(probe string, connectionID int, metrics []map[string]any) []map[string]any {
	s.mu.Lock()
	hashes := s.rows[refusedRowKey{probe, connectionID}]
	s.mu.Unlock()
	if len(hashes) == 0 {
		return metrics
	}
	kept := make([]map[string]any, 0, len(metrics))
	for _, row := range metrics {
		if _, refused := hashes[rowHash(row)]; !refused {
			kept = append(kept, row)
		}
	}
	return kept
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
