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
	"testing"
	"time"
)

// TestPeriodKeys pins the bucket keys to UTC, including across the
// Saturday to Sunday boundary where a local-zone reading would land in
// a different weekday.
func TestPeriodKeys(t *testing.T) {
	tokyo := time.FixedZone("Asia/Tokyo", 9*60*60)
	newYork := time.FixedZone("America/New_York", -4*60*60)
	// 2026-09-16 is a Wednesday; 2026-10-31 is a Saturday.
	wed := time.Date(2026, time.September, 16, 23, 30, 0, 0, time.UTC)
	satEnd := time.Date(2026, time.October, 31, 23, 59, 59, 0, time.UTC)
	sunStart := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name     string
		t        time.Time
		wantHour int
		wantDay  int
	}{
		{"utc", wed, 23, int(time.Wednesday)},
		{"tokyo view of the same instant", wed.In(tokyo), 23, int(time.Wednesday)},
		{"new york view of the same instant", wed.In(newYork), 23, int(time.Wednesday)},
		{"saturday 23:59:59Z", satEnd, 23, 6},
		{"saturday 23:59:59Z seen from tokyo", satEnd.In(tokyo), 23, 6},
		{"sunday 00:00:00Z", sunStart, 0, 0},
		{"sunday 00:00:00Z seen from new york", sunStart.In(newYork), 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hour, day := PeriodKeys(tc.t)
			if hour != tc.wantHour || day != tc.wantDay {
				t.Errorf("PeriodKeys = (%d, %d), want (%d, %d)",
					hour, day, tc.wantHour, tc.wantDay)
			}
		})
	}
}

// shippedWarmup mirrors the alerter's default warmup thresholds.
var shippedWarmup = WarmupConfig{
	All:    PeriodWarmup{MinSamples: 100, MinSpanHours: 24},
	Hourly: PeriodWarmup{MinSamples: 5, MinSpanHours: 120},
	Daily:  PeriodWarmup{MinSamples: 3, MinSpanHours: 336},
}

// TestIsWarm covers the sample and span halves of the warmup gate, the
// zero EarliestSampleAt fail-closed rule, the zero-threshold escape
// hatch and the daily fallback for unknown period types.
func TestIsWarm(t *testing.T) {
	now := time.Date(2026, time.September, 16, 14, 0, 0, 0, time.UTC)
	zeroAll := WarmupConfig{All: PeriodWarmup{}}
	samplesOnly := WarmupConfig{All: PeriodWarmup{MinSamples: 10}}

	cases := []struct {
		name       string
		periodType string
		samples    int64
		earliest   time.Time
		cfg        WarmupConfig
		want       bool
	}{
		{"all: both pass", "all", 200, now.Add(-48 * time.Hour), shippedWarmup, true},
		{"all: under-samples", "all", 50, now.Add(-48 * time.Hour), shippedWarmup, false},
		{"all: under-span", "all", 200, now.Add(-12 * time.Hour), shippedWarmup, false},
		{"all: zero earliest", "all", 200, time.Time{}, shippedWarmup, false},
		{"all: exactly at both thresholds", "all", 100, now.Add(-24 * time.Hour), shippedWarmup, true},
		{"hourly: both pass", "hourly", 10, now.Add(-200 * time.Hour), shippedWarmup, true},
		{"hourly: under-span", "hourly", 10, now.Add(-100 * time.Hour), shippedWarmup, false},
		{"daily: both pass", "daily", 5, now.Add(-400 * time.Hour), shippedWarmup, true},
		{"daily: under-samples", "daily", 2, now.Add(-400 * time.Hour), shippedWarmup, false},
		{"gate disabled by zero config", "all", 0, time.Time{}, zeroAll, true},
		{"span check disabled ignores zero earliest", "all", 10, time.Time{}, samplesOnly, true},
		{"unknown period_type falls back to daily", "weekly", 5, now.Add(-400 * time.Hour), shippedWarmup, true},
		{"unknown period_type under daily span", "weekly", 5, now.Add(-100 * time.Hour), shippedWarmup, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := Baseline{
				PeriodType:       tc.periodType,
				SampleCount:      tc.samples,
				EarliestSampleAt: tc.earliest,
			}
			if got := IsWarm(b, tc.cfg, now); got != tc.want {
				t.Errorf("IsWarm = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestCandidates checks the [hourly, daily, all] ordering with nil gaps.
func TestCandidates(t *testing.T) {
	now := time.Date(2026, time.September, 16, 14, 0, 0, 0, time.UTC)
	hour, day := PeriodKeys(now)
	other := (hour + 1) % 24

	all := &Baseline{PeriodType: "all"}
	hourly := &Baseline{PeriodType: "hourly", HourOfDay: &hour}
	daily := &Baseline{PeriodType: "daily", DayOfWeek: &day}
	wrong := &Baseline{PeriodType: "hourly", HourOfDay: &other}

	got := Candidates([]*Baseline{nil, all, wrong, daily, hourly}, now)
	want := []*Baseline{hourly, daily, all}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("candidate[%d] = %s, want %s", i, describe(got[i]), describe(want[i]))
		}
	}

	got = Candidates([]*Baseline{all}, now)
	if got[0] != nil || got[1] != nil || got[2] != all {
		t.Errorf("gaps not preserved: %s, %s, %s",
			describe(got[0]), describe(got[1]), describe(got[2]))
	}
}

// TestSelect ports the alerter's selection cases: preference order,
// per-candidate warmth and the report of the most preferred cold row.
func TestSelect(t *testing.T) {
	now := time.Date(2026, time.September, 16, 14, 0, 0, 0, time.UTC)
	hour, weekday := PeriodKeys(now)
	otherHour := (hour + 1) % 24
	otherDay := (weekday + 1) % 7

	// The oldest sample a refreshed baseline can carry at the alerter's
	// default 15 day lookback.
	warmSince := now.Add(-15 * 24 * time.Hour).Add(time.Minute)
	coldSince := now.Add(-1 * time.Hour)

	mk := func(period string, h, d *int, samples int64, earliest time.Time) *Baseline {
		return &Baseline{
			PeriodType:       period,
			HourOfDay:        h,
			DayOfWeek:        d,
			SampleCount:      samples,
			EarliestSampleAt: earliest,
		}
	}
	warmAll := mk("all", nil, nil, 500, warmSince)
	coldAll := mk("all", nil, nil, 5, coldSince)
	warmHourly := mk("hourly", &hour, nil, 500, warmSince)
	coldHourly := mk("hourly", &hour, nil, 2, coldSince)
	wrongHourly := mk("hourly", &otherHour, nil, 500, warmSince)
	nilHourly := mk("hourly", nil, nil, 500, warmSince)
	warmDaily := mk("daily", nil, &weekday, 500, warmSince)
	coldDaily := mk("daily", nil, &weekday, 1, coldSince)
	wrongDaily := mk("daily", nil, &otherDay, 500, warmSince)
	nilDaily := mk("daily", nil, nil, 500, warmSince)
	weekly := mk("weekly", nil, nil, 500, warmSince)

	cases := []struct {
		name       string
		baselines  []*Baseline
		wantChosen *Baseline
		wantCold   *Baseline
	}{
		{"nil slice", nil, nil, nil},
		{"empty slice", []*Baseline{}, nil, nil},
		{"nil entries are skipped", []*Baseline{nil, warmAll}, warmAll, nil},
		{"all only", []*Baseline{warmAll}, warmAll, nil},
		{"hourly preferred over daily and all", []*Baseline{warmAll, warmDaily, warmHourly}, warmHourly, nil},
		{"daily preferred over all", []*Baseline{warmAll, warmDaily}, warmDaily, nil},
		{"cold hourly falls through to warm daily", []*Baseline{warmAll, warmDaily, coldHourly}, warmDaily, coldHourly},
		{"cold hourly and daily fall through to all", []*Baseline{coldDaily, coldHourly, warmAll}, warmAll, coldHourly},
		{"cold daily falls through to warm all", []*Baseline{coldDaily, warmAll}, warmAll, coldDaily},
		{"cold all does not block warm hourly", []*Baseline{coldAll, warmHourly}, warmHourly, nil},
		{"hourly for another hour is ignored", []*Baseline{wrongHourly, warmAll}, warmAll, nil},
		{"daily for another day is ignored", []*Baseline{wrongDaily, warmAll}, warmAll, nil},
		{"hourly with nil hour is ignored", []*Baseline{nilHourly, warmAll}, warmAll, nil},
		{"daily with nil day is ignored", []*Baseline{nilDaily, warmAll}, warmAll, nil},
		{"unknown period type is ignored", []*Baseline{weekly}, nil, nil},
		{"all cold reports most preferred", []*Baseline{coldAll, coldDaily, coldHourly}, nil, coldHourly},
		{"cold daily and all report daily", []*Baseline{coldAll, coldDaily}, nil, coldDaily},
		{"only cold all reports all", []*Baseline{coldAll}, nil, coldAll},
		{"only a non-matching warm hourly yields nothing", []*Baseline{wrongHourly}, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chosen, cold := Select(tc.baselines, now, shippedWarmup)
			if chosen != tc.wantChosen {
				t.Errorf("chosen = %s, want %s", describe(chosen), describe(tc.wantChosen))
			}
			if cold != tc.wantCold {
				t.Errorf("skippedCold = %s, want %s", describe(cold), describe(tc.wantCold))
			}
		})
	}
}

// TestSelectUsesUTCHour checks that a non-UTC now still matches the row
// keyed by the UTC hour.
func TestSelectUsesUTCHour(t *testing.T) {
	utcNow := time.Date(2026, time.September, 16, 23, 0, 0, 0, time.UTC)
	localNow := utcNow.In(time.FixedZone("Asia/Tokyo", 9*60*60))
	utcHour, localHour := 23, localNow.Hour()
	if localHour == utcHour {
		t.Fatal("fixture is not zone-sensitive")
	}
	utcRow := &Baseline{PeriodType: "hourly", HourOfDay: &utcHour}
	localRow := &Baseline{PeriodType: "hourly", HourOfDay: &localHour}

	chosen, _ := Select([]*Baseline{localRow, utcRow}, localNow, WarmupConfig{})
	if chosen != utcRow {
		t.Errorf("chosen = %s, want the UTC hour row", describe(chosen))
	}
}

// TestEffectiveStdDev covers the hybrid variance floor and the
// per-metric MinStdDev.
func TestEffectiveStdDev(t *testing.T) {
	cases := []struct {
		name      string
		mean      float64
		stddev    float64
		relPct    float64
		absFloor  float64
		minStdDev float64
		want      float64
	}{
		{"raw stddev dominates", 100, 10, 0.05, 0.001, 0, 10},
		{"relative floor kicks in", 100, 0.01, 0.05, 0.001, 0, 5},
		{"absolute floor kicks in", 0, 0.0001, 0.05, 0.001, 0, 0.001},
		{"mean zero stddev zero", 0, 0, 0.05, 0.001, 0, 0.001},
		{"negative mean uses abs", -200, 0.01, 0.05, 0.001, 0, 10},
		{"both floors zero", 100, 0.0001, 0, 0, 0, 0.0001},
		{"metric floor dominates", 2, 0, 0.05, 0.001, 5, 5},
		{"raw stddev beats metric floor", 2, 7, 0.05, 0.001, 5, 7},
		{"every floor zero", 0, 0, 0, 0, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EffectiveStdDev(
				Baseline{Mean: tc.mean, StdDev: tc.stddev},
				VarianceFloorConfig{RelativePct: tc.relPct, AbsoluteFloor: tc.absFloor},
				MetricFloor{MinStdDev: tc.minStdDev},
			)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("EffectiveStdDev = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestZScore covers the division, the symmetric cap, the disabled cap
// and the zero-divisor refusal.
func TestZScore(t *testing.T) {
	b := Baseline{Mean: 10}
	cases := []struct {
		name   string
		value  float64
		stddev float64
		maxZ   float64
		wantZ  float64
		wantOK bool
	}{
		{"plain", 16, 2, 100, 3, true},
		{"negative", 4, 2, 100, -3, true},
		{"capped high", 1010, 1, 100, 100, true},
		{"capped low", -990, 1, 100, -100, true},
		{"cap disabled", 1010, 1, 0, 1000, true},
		{"zero divisor", 16, 0, 100, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			z, ok := ZScore(tc.value, b, tc.stddev, tc.maxZ)
			if ok != tc.wantOK || math.Abs(z-tc.wantZ) > 1e-9 {
				t.Errorf("ZScore = (%v, %v), want (%v, %v)", z, ok, tc.wantZ, tc.wantOK)
			}
		})
	}
}

// TestInBand covers the sensitivity band, its inclusive edges and the
// MinValue floor.
func TestInBand(t *testing.T) {
	cases := []struct {
		name  string
		value float64
		z     float64
		floor MetricFloor
		want  bool
	}{
		{"inside", 10, 1, MetricFloor{}, true},
		{"upper edge", 10, 3, MetricFloor{}, true},
		{"lower edge", 10, -3, MetricFloor{}, true},
		{"above", 10, 3.01, MetricFloor{}, false},
		{"below", 10, -3.01, MetricFloor{}, false},
		{"under value floor", 10, 50, MetricFloor{MinValue: 20}, true},
		{"at value floor is scored", 20, 50, MetricFloor{MinValue: 20}, false},
		{"zero value floor disabled", -5, 50, MetricFloor{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := InBand(tc.value, tc.z, 3, tc.floor); got != tc.want {
				t.Errorf("InBand = %v, want %v", got, tc.want)
			}
		})
	}
}

// describe renders a baseline pointer for failure messages.
func describe(b *Baseline) string {
	if b == nil {
		return "<nil>"
	}
	return b.PeriodType
}
