/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package engine

import (
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// TestBaselinePeriodKeys pins the bucket keys to UTC so an alerter whose
// process time zone differs from the collector's, or that is restarted
// under another TZ, still writes and reads the same hourly and daily
// rows. See GitHub issue #408.
func TestBaselinePeriodKeys(t *testing.T) {
	// 2026-09-16 is a Wednesday (weekday 3). 23:30 UTC is 08:30 the
	// following morning in Tokyo, which is a Thursday there.
	instant := time.Date(2026, time.September, 16, 23, 30, 0, 0, time.UTC)
	tokyo := time.FixedZone("Asia/Tokyo", 9*60*60)
	newYork := time.FixedZone("America/New_York", -4*60*60)

	cases := []struct {
		name string
		t    time.Time
	}{
		{"utc", instant},
		{"tokyo", instant.In(tokyo)},
		{"new york", instant.In(newYork)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hour, weekday := baselinePeriodKeys(tc.t)
			if hour != 23 {
				t.Errorf("hour = %d, want 23", hour)
			}
			if weekday != int(time.Wednesday) {
				t.Errorf("weekday = %d, want %d", weekday, time.Wednesday)
			}
		})
	}

	// The local-zone view of the Tokyo time really is a different
	// bucket, which is exactly what the helper must ignore.
	if h := instant.In(tokyo).Hour(); h == 23 {
		t.Fatalf("test fixture is not zone-sensitive: Tokyo hour = %d", h)
	}
}

// TestSelectBaseline exercises the preference order and the per-candidate
// warmth check of selectBaseline.
func TestSelectBaseline(t *testing.T) {
	// Fixed instant: Wednesday 14:00 UTC.
	now := time.Date(2026, time.September, 16, 14, 0, 0, 0, time.UTC)
	hour, weekday := baselinePeriodKeys(now)
	otherHour := (hour + 1) % 24
	otherDay := (weekday + 1) % 7

	cfg := config.WarmupConfig{
		All:    config.PerPeriodWarmupConfig{MinSamples: 100, MinSpanHours: 24},
		Hourly: config.PerPeriodWarmupConfig{MinSamples: 5, MinSpanHours: 120},
		Daily:  config.PerPeriodWarmupConfig{MinSamples: 3, MinSpanHours: 336},
	}
	warmSince := now.Add(-30 * 24 * time.Hour)
	coldSince := now.Add(-1 * time.Hour)

	mk := func(period string, h, d *int, samples int64, earliest time.Time) *database.MetricBaseline {
		return &database.MetricBaseline{
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
		baselines  []*database.MetricBaseline
		wantChosen *database.MetricBaseline
		wantCold   *database.MetricBaseline
	}{
		{"nil slice", nil, nil, nil},
		{"empty slice", []*database.MetricBaseline{}, nil, nil},
		{"nil entries are skipped", []*database.MetricBaseline{nil, warmAll}, warmAll, nil},
		{"all only", []*database.MetricBaseline{warmAll}, warmAll, nil},
		{"hourly preferred over daily and all",
			[]*database.MetricBaseline{warmAll, warmDaily, warmHourly}, warmHourly, nil},
		{"daily preferred over all",
			[]*database.MetricBaseline{warmAll, warmDaily}, warmDaily, nil},
		{"cold hourly falls through to warm daily",
			[]*database.MetricBaseline{warmAll, warmDaily, coldHourly}, warmDaily, nil},
		{"cold hourly and daily fall through to warm all",
			[]*database.MetricBaseline{coldDaily, coldHourly, warmAll}, warmAll, nil},
		{"cold all does not block warm hourly",
			[]*database.MetricBaseline{coldAll, warmHourly}, warmHourly, nil},
		{"hourly for another hour is ignored",
			[]*database.MetricBaseline{wrongHourly, warmAll}, warmAll, nil},
		{"daily for another day is ignored",
			[]*database.MetricBaseline{wrongDaily, warmAll}, warmAll, nil},
		{"hourly with nil hour is ignored",
			[]*database.MetricBaseline{nilHourly, warmAll}, warmAll, nil},
		{"daily with nil day is ignored",
			[]*database.MetricBaseline{nilDaily, warmAll}, warmAll, nil},
		{"unknown period type is ignored",
			[]*database.MetricBaseline{weekly}, nil, nil},
		{"all cold reports most preferred cold candidate",
			[]*database.MetricBaseline{coldAll, coldDaily, coldHourly}, nil, coldHourly},
		{"cold daily and all report daily",
			[]*database.MetricBaseline{coldAll, coldDaily}, nil, coldDaily},
		{"only cold all reports all",
			[]*database.MetricBaseline{coldAll}, nil, coldAll},
		{"only a non-matching warm hourly yields nothing",
			[]*database.MetricBaseline{wrongHourly}, nil, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chosen, cold := selectBaseline(tc.baselines, now, cfg)
			if chosen != tc.wantChosen {
				t.Errorf("chosen = %s, want %s", describe(chosen), describe(tc.wantChosen))
			}
			if cold != tc.wantCold {
				t.Errorf("fallbackCold = %s, want %s", describe(cold), describe(tc.wantCold))
			}
			if chosen != nil && cold != nil {
				t.Error("a warm choice must not also report a cold fallback")
			}
		})
	}
}

// TestSelectBaselineUsesUTCHour checks that the current hour is matched
// in UTC even when now carries a different zone, mirroring how
// calculateHourlyBaselines keyed the rows.
func TestSelectBaselineUsesUTCHour(t *testing.T) {
	utcNow := time.Date(2026, time.September, 16, 23, 0, 0, 0, time.UTC)
	localNow := utcNow.In(time.FixedZone("Asia/Tokyo", 9*60*60))

	utcHour := 23
	localHour := localNow.Hour()
	if localHour == utcHour {
		t.Fatal("fixture is not zone-sensitive")
	}

	cfg := config.WarmupConfig{} // every threshold zero: everything is warm
	utcRow := &database.MetricBaseline{PeriodType: "hourly", HourOfDay: &utcHour}
	localRow := &database.MetricBaseline{PeriodType: "hourly", HourOfDay: &localHour}

	chosen, _ := selectBaseline([]*database.MetricBaseline{localRow, utcRow}, localNow, cfg)
	if chosen != utcRow {
		t.Errorf("chosen = %s, want the UTC hour %d row", describe(chosen), utcHour)
	}
}

// describe renders a baseline pointer for test failure messages.
func describe(b *database.MetricBaseline) string {
	if b == nil {
		return "<nil>"
	}
	s := b.PeriodType
	if b.HourOfDay != nil {
		s += "/hour=" + time.Duration(*b.HourOfDay).String()
	}
	if b.DayOfWeek != nil {
		s += "/day=" + time.Weekday(*b.DayOfWeek).String()
	}
	if b.SampleCount > 0 {
		s += "/samples=" + time.Duration(b.SampleCount).String()
	}
	return s
}

// TestBaselineableValues checks the per-connection filter that feeds
// detectAnomalies: values for other connections and object-scoped values
// (which have no baseline row to pair with) are dropped, and every
// remaining value is returned so per-database metrics are scored once
// per database.
func TestBaselineableValues(t *testing.T) {
	alpha, beta, tbl := "alpha", "beta", "public.orders"
	values := []database.MetricValue{
		{ConnectionID: 1, DatabaseName: &alpha, Value: 1},
		{ConnectionID: 2, DatabaseName: &alpha, Value: 2},
		{ConnectionID: 1, DatabaseName: &beta, Value: 3},
		{ConnectionID: 1, DatabaseName: &beta, ObjectName: &tbl, Value: 4},
		{ConnectionID: 1, Value: 5},
	}

	got := baselineableValues(values, 1)
	want := []float64{1, 3, 5}
	if len(got) != len(want) {
		t.Fatalf("got %d values, want %d", len(got), len(want))
	}
	for i, v := range got {
		if v.Value != want[i] {
			t.Errorf("value[%d] = %v, want %v", i, v.Value, want[i])
		}
		if v != &values[indexOfValue(values, want[i])] {
			t.Errorf("value[%d] is a copy, want a pointer into the input slice", i)
		}
	}

	if got := baselineableValues(values, 3); len(got) != 0 {
		t.Errorf("unknown connection returned %d values, want 0", len(got))
	}
	if got := baselineableValues(nil, 1); len(got) != 0 {
		t.Errorf("nil input returned %d values, want 0", len(got))
	}
}

// indexOfValue returns the position of the first value equal to v.
func indexOfValue(values []database.MetricValue, v float64) int {
	for i := range values {
		if values[i].Value == v {
			return i
		}
	}
	return -1
}
