/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package config

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// MetricFloorConfig is one anomaly.tier1.metric_floors entry. The fields
// are pointers so that an entry can override one floor and keep the
// built-in default for the other; 0 disables a floor.
type MetricFloorConfig struct {
	MinValue  *float64 `yaml:"min_value"`
	MinStdDev *float64 `yaml:"min_stddev"`
}

// MetricFloor holds the resolved absolute floors Tier 1 applies to one
// metric (GitHub issue #617).
//
// MinValue is the value below which the metric is never anomalous: such
// a value raises no candidate, and counts as back within the band when
// deciding whether to clear an open anomaly alert. It suits metrics for
// which only a high value is a concern. 0 disables it.
//
// MinStdDev is a lower bound, in the metric's own units, on the standard
// deviation used as the z-score divisor. It joins the global
// variance_floor terms, so the divisor is the largest of the baseline's
// standard deviation, variance_floor.relative_pct times |mean|,
// variance_floor.absolute_floor and MinStdDev. 0 disables it.
type MetricFloor struct {
	MinValue  float64
	MinStdDev float64
}

// defaultMetricFloors are the built-in per-metric floors.
//
// The global variance_floor.absolute_floor of 0.001 guards against
// division by zero; it is not a meaningful unit of change. On a baseline
// with a standard deviation of 0, a change of one session or one
// temporary file therefore scored z = 1000 and was clamped to
// max_z_score. These floors give each metric a divisor sized to the
// smallest change that matters in its units, and a value below which
// nothing is worth reporting. A divisor floor alone still lets a large
// jump from a flat baseline score highly, which is intended: 0 to 200
// temporary files in one sample is still an anomaly.
//
// Every MinValue sits well below the default threshold rule on the same
// metric, so the threshold path still reports a value that matters
// whatever the anomaly path does:
//
//   - CPU, memory and connection-slot percentages (rules at 80, 85 and
//     80): below 50% the host or server has at least half its capacity
//     spare, so no movement there is actionable. A 5-point divisor means
//     a rise from a flat baseline must exceed 15 points to pass the
//     default sensitivity of 3.
//   - Disk used percent (rules at 80 and 95): the same 50% floor. It
//     needs no divisor floor, since above 50% the global relative floor
//     (5% of the mean) already gives a divisor of at least 2.5 points.
//   - pg_stat_activity.count (rule at 200): fewer than 10 sessions is an
//     idle server whatever its baseline; a divisor of 5 sessions.
//   - pg_stat_database.temp_files_delta (rule at 100): fewer than 10
//     temporary files in a sample is routine for sorts and hashes that
//     spill past work_mem; a divisor of 10 files.
//   - pg_stat_activity.blocked_count (rule at 5): one blocked backend is
//     an ordinary lock wait; a divisor of 1.
//   - Query, transaction and idle-in-transaction durations (rules at
//     600, 3600 and 300 seconds): under a minute is routine for reports
//     and batch steps; a divisor of 10 seconds.
//   - Fifteen-minute load average: below 1 the host is close to idle
//     whatever its core count; a divisor of 0.5.
//   - Replication slot retained WAL (rule at 1 GiB): below 256 MiB, 16
//     default WAL segments, is ordinary retention between checkpoints;
//     a divisor of 64 MiB, four segments.
//   - Integer counts whose threshold rules already fire on any
//     occurrence (deadlocks, Spock exceptions and Spock resolutions) get
//     no MinValue, so the anomaly path hides no occurrence, but a
//     divisor of 1: a count cannot change by less than 1, so a smaller
//     spread only means the baseline has never varied.
//
// A MinStdDev only takes effect while it exceeds the global relative
// floor, so it is pointless for a metric whose mean is always well above
// twenty times its value. That is why the cache hit ratio, in percent,
// has no entry: at its usual mean of 95 to 100 the relative floor is
// already near 5 points.
var defaultMetricFloors = map[string]MetricFloor{
	"pg_sys_cpu_usage_info.processor_time_percent":  {MinValue: 50, MinStdDev: 5},
	"pg_sys_memory_info.used_percent":               {MinValue: 50, MinStdDev: 5},
	"connection_utilization_percent":                {MinValue: 50, MinStdDev: 5},
	"pg_sys_disk_info.used_percent":                 {MinValue: 50},
	"pg_stat_activity.count":                        {MinValue: 10, MinStdDev: 5},
	"pg_stat_database.temp_files_delta":             {MinValue: 10, MinStdDev: 10},
	"pg_stat_activity.blocked_count":                {MinValue: 2, MinStdDev: 1},
	"pg_stat_activity.max_query_duration_seconds":   {MinValue: 60, MinStdDev: 10},
	"pg_stat_activity.max_xact_duration_seconds":    {MinValue: 60, MinStdDev: 10},
	"pg_stat_activity.idle_in_transaction_seconds":  {MinValue: 60, MinStdDev: 10},
	"pg_sys_load_avg_info.load_avg_fifteen_minutes": {MinValue: 1, MinStdDev: 0.5},
	"pg_replication_slots.max_retained_bytes":       {MinValue: 256 << 20, MinStdDev: 64 << 20},
	"pg_stat_database.deadlocks_delta":              {MinStdDev: 1},
	"spock_exception_log.recent_count":              {MinStdDev: 1},
	"spock_resolutions.recent_count":                {MinStdDev: 1},
}

// DefaultMetricFloor returns the built-in floors for a metric, or zero
// floors when it has none.
func DefaultMetricFloor(metric string) MetricFloor {
	return defaultMetricFloors[metric]
}

// DefaultMetricFloorNames returns, sorted, the metrics that have
// built-in floors.
func DefaultMetricFloorNames() []string {
	names := make([]string, 0, len(defaultMetricFloors))
	for name := range defaultMetricFloors {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// MetricFloor returns the floors Tier 1 applies to a metric: the
// built-in default with any anomaly.tier1.metric_floors override laid
// over it field by field.
func (t *Tier1Config) MetricFloor(metric string) MetricFloor {
	floor := DefaultMetricFloor(metric)
	if override, ok := t.MetricFloors[metric]; ok {
		if override.MinValue != nil {
			floor.MinValue = *override.MinValue
		}
		if override.MinStdDev != nil {
			floor.MinStdDev = *override.MinStdDev
		}
	}
	return floor
}

// validateMetricFloors checks every anomaly.tier1.metric_floors entry,
// in metric-name order so that the error reported is deterministic.
func validateMetricFloors(floors map[string]MetricFloorConfig) error {
	names := make([]string, 0, len(floors))
	for name := range floors {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, metric := range names {
		if strings.TrimSpace(metric) == "" {
			return fmt.Errorf("anomaly.tier1.metric_floors keys must be non-empty metric names")
		}
		floor := floors[metric]
		if err := checkFloorValue(metric, "min_value", floor.MinValue); err != nil {
			return err
		}
		if err := checkFloorValue(metric, "min_stddev", floor.MinStdDev); err != nil {
			return err
		}
	}
	return nil
}

// checkFloorValue rejects a set floor that is negative, NaN or infinite.
func checkFloorValue(metric, field string, v *float64) error {
	if v == nil {
		return nil
	}
	if *v < 0 || math.IsNaN(*v) || math.IsInf(*v, 0) {
		return fmt.Errorf(
			"anomaly.tier1.metric_floors.%s.%s must be a finite non-negative number",
			metric, field)
	}
	return nil
}
