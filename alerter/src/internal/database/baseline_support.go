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

// MetricIsPerDatabase reports whether the metric's latest query reports
// one value per database, so that every value it returns names a
// database, rather than one value per connection with no database. It
// is false for names outside the registry. Anomaly recovery uses it to
// spot an alert whose database scope can never match a latest value
// of its metric, such as one raised before detection was scoped by
// database. See GitHub issue #611.
func MetricIsPerDatabase(metricName string) bool {
	cfg, ok := metricRegistry[metricName]
	return ok && (cfg.scan == scanWithDB || cfg.scan == scanWithDBObject)
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
