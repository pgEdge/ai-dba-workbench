/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package anomaly

import "sort"

// PeriodWarmup gates a single period type. Both checks must pass for
// detection to proceed; either threshold at 0 disables that half of the
// gate.
type PeriodWarmup struct {
	MinSamples   int `yaml:"min_samples" json:"min_samples"`
	MinSpanHours int `yaml:"min_span_hours" json:"min_span_hours"`
}

// WarmupConfig holds the per-period-type warmup thresholds.
type WarmupConfig struct {
	All    PeriodWarmup `yaml:"all" json:"all"`
	Hourly PeriodWarmup `yaml:"hourly" json:"hourly"`
	Daily  PeriodWarmup `yaml:"daily" json:"daily"`
}

// VarianceFloorConfig parameterises the hybrid variance floor:
//
//	effective_stddev = max(raw_stddev,
//	                       max(|mean| * RelativePct, AbsoluteFloor))
type VarianceFloorConfig struct {
	RelativePct   float64 `yaml:"relative_pct" json:"relative_pct"`
	AbsoluteFloor float64 `yaml:"absolute_floor" json:"absolute_floor"`
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
	MinValue  float64 `yaml:"min_value" json:"min_value"`
	MinStdDev float64 `yaml:"min_stddev" json:"min_stddev"`
}

// BelowValueFloor reports whether value is under a non-zero MinValue,
// which makes it normal without consulting any baseline.
func (mf MetricFloor) BelowValueFloor(value float64) bool {
	return mf.MinValue > 0 && value < mf.MinValue
}

// ScoringSettings is everything Tier 1 needs, beyond the baseline rows,
// to score a value.
type ScoringSettings struct {
	Warmup             WarmupConfig           `json:"warmup"`
	VarianceFloor      VarianceFloorConfig    `json:"variance_floor"`
	MaxZScore          float64                `json:"max_z_score"`
	DefaultSensitivity float64                `json:"default_sensitivity"`
	MetricFloors       map[string]MetricFloor `json:"metric_floors"`
}

// MetricFloor returns the floors for a metric, or zero floors when the
// settings have none for it.
func (s ScoringSettings) MetricFloor(metric string) MetricFloor {
	return s.MetricFloors[metric]
}

// DefaultScoringSettings returns the shipped Tier 1 settings, which are
// the alerter's configuration defaults; the alerter's config tests pin
// the two together. MetricFloors is a fresh copy of the built-in floors
// on every call.
func DefaultScoringSettings() ScoringSettings {
	floors := make(map[string]MetricFloor, len(defaultMetricFloors))
	for name, floor := range defaultMetricFloors {
		floors[name] = floor
	}
	return ScoringSettings{
		Warmup: WarmupConfig{
			All:    PeriodWarmup{MinSamples: 100, MinSpanHours: 24},
			Hourly: PeriodWarmup{MinSamples: 5, MinSpanHours: 120},
			Daily:  PeriodWarmup{MinSamples: 3, MinSpanHours: 336},
		},
		VarianceFloor: VarianceFloorConfig{
			RelativePct:   0.05,
			AbsoluteFloor: 0.001,
		},
		MaxZScore:          100.0,
		DefaultSensitivity: 3.0,
		MetricFloors:       floors,
	}
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
// temporary files in an hour is still an anomaly.
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
//   - Disk used percent (rules at 80 and 95): the same 50% floor, and a
//     2-point divisor. The relative floor scales with the baseline mean,
//     not the value, so the divisor floor matters for a baseline mean
//     below 40%: a disk steady at 30% that jumps to 50% would otherwise
//     score z 13.3, against 10 with the 2-point divisor.
//   - pg_stat_activity.count (rule at 200): fewer than 10 sessions is an
//     idle server whatever its baseline; a divisor of 5 sessions.
//   - pg_stat_database.temp_files_delta (rule at 100): fewer than 10
//     temporary files in an hour is routine for sorts and hashes that
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
//   - pg_stat_replication.lag_bytes (rule at 100 MiB): less than one
//     default 16 MiB WAL segment of lag is ordinary for a standby
//     replaying a busy primary; a divisor of 8 MiB.
//   - pg_stat_activity.max_lock_wait_seconds (rule at 30 seconds): a
//     lock wait under 5 seconds is ordinary row-lock contention; a
//     divisor of 2 seconds.
//   - pg_stat_checkpointer.checkpoints_req_delta (rule at 12 an hour)
//     and pg_stat_statements.slow_query_count (rule at 10): fewer than
//     3 is routine, and a divisor of 1 because a count cannot change by
//     less than 1.
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
	"pg_sys_disk_info.used_percent":                 {MinValue: 50, MinStdDev: 2},
	"pg_stat_activity.count":                        {MinValue: 10, MinStdDev: 5},
	"pg_stat_database.temp_files_delta":             {MinValue: 10, MinStdDev: 10},
	"pg_stat_activity.blocked_count":                {MinValue: 2, MinStdDev: 1},
	"pg_stat_activity.max_query_duration_seconds":   {MinValue: 60, MinStdDev: 10},
	"pg_stat_activity.max_xact_duration_seconds":    {MinValue: 60, MinStdDev: 10},
	"pg_stat_activity.idle_in_transaction_seconds":  {MinValue: 60, MinStdDev: 10},
	"pg_sys_load_avg_info.load_avg_fifteen_minutes": {MinValue: 1, MinStdDev: 0.5},
	"pg_replication_slots.max_retained_bytes":       {MinValue: 256 << 20, MinStdDev: 64 << 20},
	"pg_stat_replication.lag_bytes":                 {MinValue: 16 << 20, MinStdDev: 8 << 20},
	"pg_stat_activity.max_lock_wait_seconds":        {MinValue: 5, MinStdDev: 2},
	"pg_stat_checkpointer.checkpoints_req_delta":    {MinValue: 3, MinStdDev: 1},
	"pg_stat_statements.slow_query_count":           {MinValue: 3, MinStdDev: 1},
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
