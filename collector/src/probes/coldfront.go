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
	"fmt"
	"sync/atomic"

	"github.com/pgedge/ai-workbench/collector/src/coldfront"
)

// coldFrontMetrics records whether the metrics.* tables are stored in
// pgEdge ColdFront. It is process-wide because the storage mode is fixed
// for the life of the collector and is consulted by package-level
// helpers (EnsurePartition, DropExpiredPartitions, StoreMetrics) that
// every probe calls; main sets it once at startup, before the scheduler
// or the garbage collector starts.
var coldFrontMetrics atomic.Bool

// SetColdFrontMetrics selects whether the metrics.* tables are stored in
// pgEdge ColdFront. Under ColdFront the collector creates and drops no
// partitions, and StoreMetrics converts the values of columns whose type
// ColdFront stores differently.
func SetColdFrontMetrics(enabled bool) {
	coldFrontMetrics.Store(enabled)
}

// ColdFrontMetricsEnabled reports whether the metrics.* tables are
// stored in pgEdge ColdFront.
func ColdFrontMetricsEnabled() bool {
	return coldFrontMetrics.Load()
}

// convertForColdFront returns values with every value of a column whose
// ColdFront type differs from its PostgreSQL type converted to one pgx
// can encode into the ColdFront type. The conversions come from the
// table's ColdFront column specification. values is not modified; when
// no column needs a conversion it is returned as it is.
func convertForColdFront(tableName string, columns []string, values [][]any) ([][]any, error) {
	spec, ok := coldfront.Lookup(tableName)
	if !ok {
		return nil, fmt.Errorf("metrics.%s has no ColdFront column specification", tableName)
	}
	convs := spec.Converters(columns)
	if convs == nil {
		return values, nil
	}

	out := make([][]any, len(values))
	for i, row := range values {
		converted := make([]any, len(row))
		copy(converted, row)
		for col, conv := range convs {
			if conv != nil && col < len(converted) {
				converted[col] = conv(converted[col])
			}
		}
		out[i] = converted
	}
	return out, nil
}
