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

import (
	"math"
	"time"
)

// Baseline is the part of a metric_baselines row that selection and
// scoring read.
type Baseline struct {
	// PeriodType is "all", "hourly" or "daily".
	PeriodType string

	// HourOfDay is the UTC hour an hourly row covers; nil otherwise.
	HourOfDay *int

	// DayOfWeek is the UTC weekday (Sunday = 0) a daily row covers; nil
	// otherwise.
	DayOfWeek *int

	Mean   float64
	StdDev float64

	// SampleCount is the number of raw samples behind the row.
	SampleCount int64

	// EarliestSampleAt is the oldest sample behind the row; the zero
	// time means it is unknown, which the warmup gate treats as cold.
	EarliestSampleAt time.Time
}

// PeriodKeys returns the hour of day and weekday (Sunday = 0) that key
// the hourly and daily baseline rows for t.
//
// The timestamp is normalised to UTC first. pgx scans timestamptz
// columns into the process's local zone unless a ScanLocation is
// configured, and time.Now() is local too, so without this a collector
// and an alerter in different zones, or one alerter restarted under a
// different TZ, would bucket the same instant under different keys. The
// keys are therefore UTC hours and UTC weekdays; a metric's diurnal
// cycle is still captured, just keyed in UTC.
func PeriodKeys(t time.Time) (hourOfDay, dayOfWeek int) {
	u := t.UTC()
	return u.Hour(), int(u.Weekday())
}

// Candidates returns the rows Select considers, in preference order: the
// hourly row for t's UTC hour, the daily row for t's UTC weekday, then
// the 'all' row. The result always has three entries; a missing row is
// a nil entry so the caller can keep the order without re-deriving it.
func Candidates(baselines []*Baseline, t time.Time) []*Baseline {
	hour, weekday := PeriodKeys(t)

	var hourly, daily, all *Baseline
	for _, b := range baselines {
		if b == nil {
			continue
		}
		switch b.PeriodType {
		case "hourly":
			if b.HourOfDay != nil && *b.HourOfDay == hour {
				hourly = b
			}
		case "daily":
			if b.DayOfWeek != nil && *b.DayOfWeek == weekday {
				daily = b
			}
		case "all":
			all = b
		}
	}
	return []*Baseline{hourly, daily, all}
}

// IsWarm reports whether the baseline has accumulated enough samples and
// enough wall-clock span to be trustworthy for anomaly detection. A zero
// EarliestSampleAt (a row written before the column was populated, or a
// baseline that lacks raw sample timestamps) is treated as not warm.
//
// The span check is skipped when the configured MinSpanHours is zero,
// which is the documented escape hatch to disable the time-span half of
// the gate.
//
// Unknown period types fall back to the daily thresholds, which are the
// strictest of the three configured pairs by default. This is
// defensive; period_type is enum-constrained at write time.
func IsWarm(b Baseline, w WarmupConfig, now time.Time) bool {
	thresh := warmupFor(b.PeriodType, w)
	if b.SampleCount < int64(thresh.MinSamples) {
		return false
	}
	if thresh.MinSpanHours > 0 {
		if b.EarliestSampleAt.IsZero() {
			return false
		}
		minSpan := time.Duration(thresh.MinSpanHours) * time.Hour
		if now.Sub(b.EarliestSampleAt) < minSpan {
			return false
		}
	}
	return true
}

// warmupFor selects the per-period-type warmup thresholds, falling back
// to the daily thresholds for any unrecognised period type.
func warmupFor(periodType string, w WarmupConfig) PeriodWarmup {
	switch periodType {
	case "all":
		return w.All
	case "hourly":
		return w.Hourly
	default:
		return w.Daily
	}
}

// Select picks the baseline row to score a value against. The preference
// order is the hourly row for the current UTC hour, then the daily row
// for the current UTC weekday, then the 'all' row; the first candidate in
// that order that passes IsWarm is returned as chosen. skippedCold is the
// most preferred candidate that exists but failed the gate, whether or
// not a later candidate was chosen, so the caller can log both a
// suppressed detection and a fall-through to a less specific baseline.
// It is nil when nothing was skipped.
//
// Hourly and daily rows are preferred over 'all' because a metric with a
// diurnal or weekly cycle has a much tighter spread within one period
// than across the whole window; scoring against the grand mean inflates
// the divisor and hides genuine within-cycle deviation. Warmth is
// checked per candidate so a cold hourly row does not block a mature
// 'all' row, and a cold 'all' row does not block a mature hourly one.
// See GitHub issue #408.
func Select(baselines []*Baseline, now time.Time, w WarmupConfig) (chosen, skippedCold *Baseline) {
	for _, candidate := range Candidates(baselines, now) {
		if candidate == nil {
			continue
		}
		if IsWarm(*candidate, w, now) {
			return candidate, skippedCold
		}
		if skippedCold == nil {
			skippedCold = candidate
		}
	}
	return nil, skippedCold
}

// EffectiveStdDev returns the z-score divisor for a baseline: the
// largest of the baseline's own standard deviation, |mean| times
// vf.RelativePct, vf.AbsoluteFloor and the metric's MinStdDev.
//
// The global absolute floor only guards against division by zero, so on
// a flat baseline it turns a change of one unit into a z-score in the
// thousands; MinStdDev sizes the divisor to the smallest change that
// matters in the metric's units (GitHub issue #617). The result is zero
// only when every term is zero; ZScore refuses that divisor.
func EffectiveStdDev(b Baseline, vf VarianceFloorConfig, mf MetricFloor) float64 {
	relFloor := math.Abs(b.Mean) * vf.RelativePct
	floor := math.Max(relFloor, vf.AbsoluteFloor)
	return math.Max(math.Max(b.StdDev, floor), mf.MinStdDev)
}

// ZScore returns value's distance from the baseline mean in units of
// stddev, with |z| clamped to maxZ when maxZ is positive; a zero maxZ
// disables the clamp. ok is false, and z zero, when stddev is zero, the
// degenerate case in which every floor is disabled and the baseline has
// no spread.
func ZScore(value float64, b Baseline, stddev, maxZ float64) (z float64, ok bool) {
	if stddev == 0 {
		return 0, false
	}
	z = (value - b.Mean) / stddev
	if maxZ > 0 {
		if z > maxZ {
			z = maxZ
		} else if z < -maxZ {
			z = -maxZ
		}
	}
	return z, true
}

// InBand reports whether a value is normal: under the metric's MinValue
// floor, or with |z| within the sensitivity. Detection raises no
// candidate for an in-band value, and recovery counts it towards
// clearing an open alert, so the two paths cannot disagree. A value
// under the floor is in band whatever z says, since the floor is
// checked before any baseline is consulted.
func InBand(value, z, sensitivity float64, mf MetricFloor) bool {
	return mf.BelowValueFloor(value) || (z <= sensitivity && z >= -sensitivity)
}
