/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package database

import "sort"

// SupportsBaselines reports whether the metric can be baselined, which
// is true only when its registry entry carries a historical query.
// Metrics without one used to be routed through a fallback that built a
// baseline from the single latest sample; that row could never pass the
// anomaly warmup gate, so the engine now excludes such metrics from
// baseline calculation and detection outright. Names absent from the
// registry (for example metric_staleness, which is evaluated by its own
// code path) are unsupported too. See GitHub issue #408.
func SupportsBaselines(metricName string) bool {
	cfg, ok := metricRegistry[metricName]
	return ok && cfg.historicalSQL != ""
}

// BaselineSupportedMetrics returns the sorted names of every registry
// metric that SupportsBaselines accepts. The order is fixed so callers
// that pass the list as a query parameter produce a stable statement.
func BaselineSupportedMetrics() []string {
	names := make([]string, 0, len(metricRegistry))
	for name := range metricRegistry {
		if SupportsBaselines(name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
