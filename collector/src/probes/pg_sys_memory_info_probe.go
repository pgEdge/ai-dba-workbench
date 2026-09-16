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
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/collector/src/utils"
)

// PgSysMemoryInfoProbe collects metrics from pg_sys_memory_info() function
// Note: This function is provided by the system_stats extension
type PgSysMemoryInfoProbe struct {
	BaseMetricsProbe
}

// NewPgSysMemoryInfoProbe creates a new pg_sys_memory_info probe
func NewPgSysMemoryInfoProbe(config *ProbeConfig) *PgSysMemoryInfoProbe {
	return &PgSysMemoryInfoProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetExtensionName returns the required extension name
func (p *PgSysMemoryInfoProbe) GetExtensionName() string {
	return "system_stats"
}

// GetQuery returns the SQL query to execute
func (p *PgSysMemoryInfoProbe) GetQuery() string {
	return `
        SELECT
            total_memory,
            used_memory,
            free_memory,
            swap_total,
            swap_used,
            swap_free,
            cache_total,
            kernel_total,
            kernel_paged,
            kernel_non_paged,
            total_page_file,
            avail_page_file
        FROM pg_sys_memory_info()
    `
}

// Execute runs the probe against a monitored connection
func (p *PgSysMemoryInfoProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]any, error) {
	// Check if system_stats extension is installed
	exists, err := CheckExtensionExists(ctx, connectionName, monitoredConn, "system_stats")
	if err != nil {
		return nil, fmt.Errorf("failed to check for system_stats extension: %w", err)
	}
	if !exists {
		// Extension not installed, return empty result set without error
		return nil, nil
	}

	query := WrapQuery(ProbeNamePgSysMemoryInfo, p.GetQuery())
	rows, err := monitoredConn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgSysMemoryInfoProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]any) error {
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
		"total_memory", "used_memory", "free_memory",
		"swap_total", "swap_used", "swap_free",
		"cache_total", "kernel_total", "kernel_paged",
		"kernel_non_paged", "total_page_file", "avail_page_file",
		"available_memory",
	}

	// Build values array
	var values [][]any
	for _, metric := range metrics {
		row := []any{
			connectionID,
			timestamp,
			metric["total_memory"],
			metric["used_memory"],
			metric["free_memory"],
			metric["swap_total"],
			metric["swap_used"],
			metric["swap_free"],
			metric["cache_total"],
			metric["kernel_total"],
			metric["kernel_paged"],
			metric["kernel_non_paged"],
			metric["total_page_file"],
			metric["avail_page_file"],
			estimateAvailableMemory(metric),
		}
		values = append(values, row)
	}

	// Store metrics
	if err := StoreMetrics(ctx, datastoreConn, p.GetTableName(), columns, values); err != nil {
		return fmt.Errorf("failed to store metrics: %w", err)
	}

	return nil
}

// estimateAvailableMemory estimates the memory available for new
// workloads without swapping, as the sum of the free memory and the page
// cache reported by pg_sys_memory_info().
//
// The estimate exists because the system_stats extension does not expose
// the kernel's MemAvailable, and the collector reaches the monitored host
// only over SQL, so /proc/meminfo is unreachable. It overestimates
// availability where non-reclaimable slab is large or where much of the
// page cache is dirty. Should system_stats gain a real MemAvailable
// column, only this function and the probe's query need to change; the
// stored column keeps its meaning.
//
// It returns nil, and so stores SQL NULL, whenever either input is
// absent, is NULL on the monitored server, or is not an integer value.
// A missing input must never be read as zero, because zero is itself a
// meaningful reading.
func estimateAvailableMemory(metric map[string]any) any {
	free, ok := metricInt64(metric["free_memory"])
	if !ok {
		return nil
	}
	cache, ok := metricInt64(metric["cache_total"])
	if !ok {
		return nil
	}

	// Guard the addition: both operands come from the monitored server,
	// so a nonsensical pair must not wrap around into a negative figure.
	if (cache > 0 && free > math.MaxInt64-cache) ||
		(cache < 0 && free < math.MinInt64-cache) {
		return nil
	}

	return free + cache
}

// metricInt64 converts a value scanned out of a metrics row into an
// int64. The rows arrive as map[string]any from utils.ScanRowsToMaps, so
// the concrete type depends on what pgx hands back for the column: a
// BIGINT normally arrives as int64, but the same helper is used against
// test doubles and future column types, so every fixed-width integer
// kind is accepted. Floats, strings, nil and anything else report false
// rather than being coerced, so the caller can store NULL.
func metricInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int32:
		return int64(n), true
	case int16:
		return int64(n), true
	case int8:
		return int64(n), true
	case int:
		return int64(n), true
	case uint64:
		if n > math.MaxInt64 {
			return 0, false
		}
		return int64(n), true // #nosec G115 -- range checked above
	case uint32:
		return int64(n), true
	case uint16:
		return int64(n), true
	case uint8:
		return int64(n), true
	case uint:
		if uint64(n) > math.MaxInt64 {
			return 0, false
		}
		return int64(n), true // #nosec G115 -- range checked above
	default:
		return 0, false
	}
}
