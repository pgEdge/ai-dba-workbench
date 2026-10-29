/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package metrics

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/pgedge/ai-workbench/pkg/anomaly"
)

// cmpNow is the reference time of the pure tests: Saturday 24 October
// 2026, 23:00 UTC.
var cmpNow = time.Date(2026, 10, 24, 23, 0, 0, 0, time.UTC)

// warmSince is old enough for every period type's span gate.
var warmSince = cmpNow.AddDate(0, -1, 0)

func intPtr(i int) *int { return &i }

func allRow(mean, sd float64, samples int64, earliest time.Time) *anomaly.Baseline {
	return &anomaly.Baseline{PeriodType: "all", Mean: mean, StdDev: sd,
		SampleCount: samples, EarliestSampleAt: earliest}
}

func hourlyRow(hour int, mean, sd float64, samples int64, earliest time.Time) *anomaly.Baseline {
	return &anomaly.Baseline{PeriodType: "hourly", HourOfDay: intPtr(hour), Mean: mean,
		StdDev: sd, SampleCount: samples, EarliestSampleAt: earliest}
}

func dailyRow(day int, mean, sd float64, samples int64, earliest time.Time) *anomaly.Baseline {
	return &anomaly.Baseline{PeriodType: "daily", DayOfWeek: intPtr(day), Mean: mean,
		StdDev: sd, SampleCount: samples, EarliestSampleAt: earliest}
}

// noFloors are default settings without per-metric floors or a relative
// variance floor, so the expected divisors are the rows' own spreads.
func noFloors() anomaly.ScoringSettings {
	s := anomaly.DefaultScoringSettings()
	s.MetricFloors = nil
	s.VarianceFloor.RelativePct = 0
	return s
}

func oneHour() TimeWindow {
	return TimeWindow{Start: cmpNow.Add(-time.Hour), End: cmpNow}
}

func sample(offset time.Duration, value float64) ComparisonSample {
	return ComparisonSample{Time: cmpNow.Add(-time.Hour).Add(offset), Value: value}
}

func f64(p *float64) string {
	if p == nil {
		return "nil"
	}
	return jsonFloat(*p)
}

func jsonFloat(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}

func str(p *string) string {
	if p == nil {
		return "nil"
	}
	return *p
}

func TestBuildComparison_Models(t *testing.T) {
	const metric = "pg_stat_activity.count"
	// The hourly row for 22:00 is warm; the daily row for Saturday (6)
	// is cold by span; the all row is warm.
	rows := []*anomaly.Baseline{
		hourlyRow(22, 50, 5, 10, warmSince),
		dailyRow(6, 40, 4, 10, cmpNow.Add(-time.Hour)),
		allRow(30, 2, 1000, warmSince),
	}
	samples := []ComparisonSample{sample(30*time.Minute, 70)}

	tests := []struct {
		name        string
		model       string
		baselines   []*anomaly.Baseline
		wantPeriod  string
		wantMean    string
		wantStddev  string
		wantZ       string
		wantCold    bool
		wantOutside bool
	}{
		{"as_scored picks the warm hourly row", ComparisonModelAsScored, rows,
			"hourly", "50", "5", "4", false, true},
		{"hourly model", ComparisonModelHourly, rows, "hourly", "50", "5", "4", false, true},
		{"daily model shows a cold row, never outside", ComparisonModelDaily, rows,
			"daily", "40", "4", "7.5", true, false},
		{"all model", ComparisonModelAll, rows, "all", "30", "2", "20", false, true},
		{"as_scored falls through a cold hourly row", ComparisonModelAsScored,
			[]*anomaly.Baseline{hourlyRow(22, 50, 5, 1, warmSince), allRow(60, 5, 1000, warmSince)},
			"all", "60", "5", "2", false, false},
		{"as_scored with nothing warm shows the preferred cold row", ComparisonModelAsScored,
			[]*anomaly.Baseline{hourlyRow(22, 50, 5, 1, warmSince), allRow(30, 2, 1, warmSince)},
			"hourly", "50", "5", "4", true, false},
		{"as_scored with no rows has no band and is not cold", ComparisonModelAsScored, nil,
			"nil", "nil", "nil", "nil", false, false},
		{"as_scored with no row for the point has no band and is not cold", ComparisonModelAsScored,
			[]*anomaly.Baseline{hourlyRow(3, 50, 5, 1, warmSince)},
			"nil", "nil", "nil", "nil", false, false},
		{"explicit model without its row has no band and is not cold", ComparisonModelHourly,
			[]*anomaly.Baseline{allRow(30, 2, 1000, warmSince)},
			"nil", "nil", "nil", "nil", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildComparison(metric, samples, tt.baselines, noFloors(),
				tt.model, 3, oneHour(), 10)
			if len(got) != 1 {
				t.Fatalf("got %d points, want 1", len(got))
			}
			p := got[0]
			if str(p.PeriodType) != tt.wantPeriod || f64(p.Mean) != tt.wantMean ||
				f64(p.Stddev) != tt.wantStddev || f64(p.ZScore) != tt.wantZ {
				t.Errorf("period %s mean %s stddev %s z %s; want %s %s %s %s",
					str(p.PeriodType), f64(p.Mean), f64(p.Stddev), f64(p.ZScore),
					tt.wantPeriod, tt.wantMean, tt.wantStddev, tt.wantZ)
			}
			if p.Cold != tt.wantCold || p.Outside != tt.wantOutside {
				t.Errorf("cold %v outside %v; want %v %v", p.Cold, p.Outside,
					tt.wantCold, tt.wantOutside)
			}
			if p.Value != 70 || !p.Time.Equal(samples[0].Time) || p.Time.Location() != time.UTC {
				t.Errorf("point %v = %v, want the sample in UTC", p.Time, p.Value)
			}
		})
	}
}

func TestBuildComparison_ScoringInputs(t *testing.T) {
	window := oneHour()
	row := []*anomaly.Baseline{allRow(100, 1, 1000, warmSince)}

	t.Run("sensitivity widens the band", func(t *testing.T) {
		s := noFloors()
		samples := []ComparisonSample{sample(time.Minute, 104)}
		if p := BuildComparison("m", samples, row, s, ComparisonModelAll, 3, window, 1)[0]; !p.Outside {
			t.Error("z 4 at sensitivity 3 is not outside")
		}
		if p := BuildComparison("m", samples, row, s, ComparisonModelAll, 5, window, 1)[0]; p.Outside {
			t.Error("z 4 at sensitivity 5 is outside")
		}
	})

	t.Run("metric floors apply", func(t *testing.T) {
		s := noFloors()
		s.MetricFloors = map[string]anomaly.MetricFloor{"m": {MinValue: 500, MinStdDev: 10}}
		p := BuildComparison("m", []ComparisonSample{sample(time.Minute, 200)}, row, s,
			ComparisonModelAll, 3, window, 1)[0]
		if f64(p.Stddev) != "10" || f64(p.ZScore) != "10" || p.Outside {
			t.Errorf("stddev %s z %s outside %v; want 10, 10, false (under the value floor)",
				f64(p.Stddev), f64(p.ZScore), p.Outside)
		}
		other := BuildComparison("other", []ComparisonSample{sample(time.Minute, 200)}, row, s,
			ComparisonModelAll, 3, window, 1)[0]
		if f64(other.Stddev) != "1" || !other.Outside {
			t.Errorf("another metric took the floors: stddev %s outside %v", f64(other.Stddev), other.Outside)
		}
	})

	t.Run("z is clamped to the maximum", func(t *testing.T) {
		s := noFloors()
		s.MaxZScore = 7
		p := BuildComparison("m", []ComparisonSample{sample(time.Minute, 1000)}, row, s,
			ComparisonModelAll, 3, window, 1)[0]
		if f64(p.ZScore) != "7" {
			t.Errorf("z = %s, want 7", f64(p.ZScore))
		}
	})

	t.Run("a zero divisor leaves the point unscored", func(t *testing.T) {
		s := noFloors()
		s.VarianceFloor.AbsoluteFloor = 0
		flat := []*anomaly.Baseline{allRow(0, 0, 1000, warmSince)}
		p := BuildComparison("m", []ComparisonSample{sample(time.Minute, 5)}, flat, s,
			ComparisonModelAll, 3, window, 1)[0]
		if p.ZScore != nil || p.Outside || f64(p.Stddev) != "0" || str(p.PeriodType) != "all" {
			t.Errorf("z %s outside %v stddev %s; want an unscored point with its row",
				f64(p.ZScore), p.Outside, f64(p.Stddev))
		}
	})
}

func TestBuildComparison_Bucketing(t *testing.T) {
	s := noFloors()
	window := oneHour() // six buckets of ten minutes
	row := []*anomaly.Baseline{allRow(100, 10, 1000, warmSince)}

	samples := []ComparisonSample{
		{Time: window.Start.Add(-time.Second), Value: 999}, // before the window
		sample(1*time.Minute, 110),                         // bucket 0, z 1
		sample(2*time.Minute, 70),                          // bucket 0, z -3: kept
		sample(3*time.Minute, 120),                         // bucket 0, z 2
		sample(11*time.Minute, 130),                        // bucket 1, z 3: kept (tie)
		sample(12*time.Minute, 70),                         // bucket 1, z -3
		sample(60*time.Minute, 100),                        // the end: last bucket
		{Time: window.End.Add(time.Second), Value: 999},    // after the window
	}
	got := BuildComparison("m", samples, row, s, ComparisonModelAll, 3, window, 6)
	var summary []string
	for _, p := range got {
		summary = append(summary, p.Time.Format("15:04")+"="+jsonFloat(p.Value))
	}
	want := "22:02=70 22:11=130 23:00=100"
	if strings.Join(summary, " ") != want {
		t.Errorf("points = %v, want %s", summary, want)
	}

	t.Run("unscored buckets keep the largest value", func(t *testing.T) {
		got := BuildComparison("m", []ComparisonSample{
			sample(time.Minute, 5), sample(2*time.Minute, 9), sample(3*time.Minute, 7),
		}, nil, s, ComparisonModelAsScored, 3, window, 1)
		if len(got) != 1 || got[0].Value != 9 {
			t.Errorf("points = %+v, want the value 9", got)
		}
	})

	t.Run("a scored sample beats an unscored one", func(t *testing.T) {
		// The hourly row for 22:00 scores only the first sample; the
		// second, at 23:00, has no row under the hourly model.
		hourly := []*anomaly.Baseline{hourlyRow(22, 100, 10, 1000, warmSince)}
		samples := []ComparisonSample{sample(10*time.Minute, 101), sample(60*time.Minute, 500)}
		got := BuildComparison("m", samples, hourly, s, ComparisonModelHourly, 3, window, 1)
		if len(got) != 1 || got[0].Value != 101 {
			t.Errorf("points = %+v, want the scored value 101", got)
		}
		got = BuildComparison("m", []ComparisonSample{samples[1], samples[0]}, hourly, s,
			ComparisonModelHourly, 3, window, 1)
		if len(got) != 1 || got[0].Value != 101 {
			t.Errorf("reversed points = %+v, want the scored value 101", got)
		}
	})

	t.Run("an empty series is an empty slice", func(t *testing.T) {
		got := BuildComparison("m", nil, row, s, ComparisonModelAll, 3, window, 300)
		if got == nil || len(got) != 0 {
			t.Errorf("points = %#v, want an empty slice", got)
		}
		b, _ := json.Marshal(got)
		if string(b) != "[]" {
			t.Errorf("JSON = %s, want []", b)
		}
	})

	t.Run("a zero-width window or bucket count still yields points", func(t *testing.T) {
		instant := TimeWindow{Start: cmpNow, End: cmpNow}
		got := BuildComparison("m", []ComparisonSample{{Time: cmpNow, Value: 1}}, row, s,
			ComparisonModelAll, 3, instant, 0)
		if len(got) != 1 {
			t.Errorf("points = %+v, want one", got)
		}
	})

	t.Run("large windows are bounded by the bucket count", func(t *testing.T) {
		month := TimeWindow{Start: cmpNow.AddDate(0, 0, -30), End: cmpNow}
		var many []ComparisonSample
		for ts := month.Start; !ts.After(month.End); ts = ts.Add(time.Minute) {
			many = append(many, ComparisonSample{Time: ts, Value: 100})
		}
		// One spike, which must survive the reduction.
		many[len(many)/2].Value = 1000
		got := BuildComparison("m", many, row, s, ComparisonModelAll, 3, month, 300)
		if len(got) != 300 {
			t.Fatalf("got %d points, want 300", len(got))
		}
		found := false
		for _, p := range got {
			found = found || (p.Value == 1000 && p.Outside)
		}
		if !found {
			t.Error("the spike was averaged away")
		}
	})
}

// TestBuildComparison_UTCBoundary pins the hourly and daily keys to UTC,
// as the alerter's PeriodKeys does, across the Saturday to Sunday
// midnight; the samples are given in a non-UTC zone to prove the zone
// of the input does not matter.
// TestBuildComparison_StraddlingBucket proves a bucketed metric keeps
// the row for the bucket that starts before the window but covers its
// start, in the first interval at its own time, whilst a raw metric
// still drops a sample from before the window and a bucketed metric
// drops the bucket before that.
func TestBuildComparison_StraddlingBucket(t *testing.T) {
	const hourly = "pg_stat_database.deadlocks_delta"
	const raw = "pg_stat_activity.count"
	// 22:30 to 23:30, six ten-minute intervals.
	window := TimeWindow{Start: cmpNow.Add(-30 * time.Minute), End: cmpNow.Add(30 * time.Minute)}
	samples := []ComparisonSample{
		{Time: cmpNow.Add(-2 * time.Hour), Value: 1}, // 21:00 bucket: before the one covering the start
		{Time: cmpNow.Add(-time.Hour), Value: 2},     // 22:00 bucket: covers 22:30
		{Time: cmpNow, Value: 3},                     // 23:00 bucket: inside
	}
	describe := func(points []ComparisonPoint) string {
		var parts []string
		for _, p := range points {
			parts = append(parts, p.Time.Format("15:04")+"="+jsonFloat(p.Value))
		}
		return strings.Join(parts, " ")
	}

	got := BuildComparison(hourly, samples, nil, noFloors(), ComparisonModelAll, 3, window, 6)
	if want := "22:00=2 23:00=3"; describe(got) != want {
		t.Errorf("hourly points = %s, want %s", describe(got), want)
	}
	got = BuildComparison(raw, samples, nil, noFloors(), ComparisonModelAll, 3, window, 6)
	if want := "23:00=3"; describe(got) != want {
		t.Errorf("raw points = %s, want %s", describe(got), want)
	}
}

func TestBuildComparison_UTCBoundary(t *testing.T) {
	zone := time.FixedZone("UTC+5", 5*3600)
	before := time.Date(2026, 10, 24, 23, 59, 59, 0, time.UTC) // Saturday
	after := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)     // Sunday
	window := TimeWindow{Start: before.Add(-time.Minute), End: after.Add(time.Minute)}
	samples := []ComparisonSample{
		{Time: before.In(zone), Value: 10},
		{Time: after.In(zone), Value: 10},
	}
	rows := []*anomaly.Baseline{
		hourlyRow(23, 23, 1, 1000, warmSince),
		hourlyRow(0, 0, 1, 1000, warmSince),
		hourlyRow(4, 4, 1, 1000, warmSince), // the local hour of "before"
		dailyRow(6, 6, 1, 1000, warmSince),
		dailyRow(0, 0, 1, 1000, warmSince),
	}
	tests := []struct {
		model string
		want  string
	}{
		{ComparisonModelHourly, "hourly:23 hourly:0"},
		{ComparisonModelDaily, "daily:6 daily:0"},
		{ComparisonModelAsScored, "hourly:23 hourly:0"},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := BuildComparison("m", samples, rows, noFloors(), tt.model, 3, window, 2)
			var keys []string
			for _, p := range got {
				keys = append(keys, str(p.PeriodType)+":"+f64(p.Mean))
				if p.Time.Location() != time.UTC {
					t.Errorf("point time %v is not in UTC", p.Time)
				}
			}
			if strings.Join(keys, " ") != tt.want {
				t.Errorf("rows = %v, want %s", keys, tt.want)
			}
		})
	}
}

func TestParseScoringSettings(t *testing.T) {
	published := anomaly.DefaultScoringSettings()
	published.DefaultSensitivity = 4.5
	published.MaxZScore = 50
	good, _ := json.Marshal(published)

	tests := []struct {
		name        string
		raw         []byte
		wantSource  string
		wantDefault float64
	}{
		{"NULL", nil, ScoringSourceDefaults, 3},
		{"empty", []byte{}, ScoringSourceDefaults, 3},
		{"malformed", []byte(`{"default_sensitivity":`), ScoringSourceDefaults, 3},
		{"JSON null", []byte(`null`), ScoringSourceDefaults, 3},
		{"wrong shape", []byte(`[1,2,3]`), ScoringSourceDefaults, 3},
		{"no sensitivity", []byte(`{"max_z_score":10}`), ScoringSourceDefaults, 3},
		{"published", good, ScoringSourceAlerter, 4.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, source := ParseScoringSettings(tt.raw)
			if source != tt.wantSource || s.DefaultSensitivity != tt.wantDefault {
				t.Errorf("source %q sensitivity %v; want %q %v", source,
					s.DefaultSensitivity, tt.wantSource, tt.wantDefault)
			}
		})
	}
	s, _ := ParseScoringSettings(good)
	if s.MaxZScore != 50 || s.Warmup.Hourly.MinSamples != 5 {
		t.Errorf("published settings not decoded: %+v", s)
	}
}

// TestParseScoringSettings_MissingKeysKeepDefaults checks that settings
// published without some keys keep the shipped defaults for them, and
// that a published metric_floors map replaces the default floors.
func TestParseScoringSettings_MissingKeysKeepDefaults(t *testing.T) {
	defaults := anomaly.DefaultScoringSettings()

	s, source := ParseScoringSettings([]byte(`{"default_sensitivity":4}`))
	if source != ScoringSourceAlerter || s.DefaultSensitivity != 4 {
		t.Fatalf("source %q sensitivity %v; want alerter 4", source, s.DefaultSensitivity)
	}
	if s.Warmup != defaults.Warmup || s.VarianceFloor != defaults.VarianceFloor ||
		s.MaxZScore != defaults.MaxZScore {
		t.Errorf("settings = %+v, want the default warmup, variance floor and z cap", s)
	}
	if !reflect.DeepEqual(s.MetricFloors, defaults.MetricFloors) || len(s.MetricFloors) == 0 {
		t.Errorf("metric floors = %v, want the defaults %v", s.MetricFloors, defaults.MetricFloors)
	}

	s, _ = ParseScoringSettings([]byte(
		`{"default_sensitivity":4,"warmup":{"all":{"min_samples":7}},"metric_floors":{}}`))
	if s.Warmup.All.MinSamples != 7 || s.Warmup.All.MinSpanHours != defaults.Warmup.All.MinSpanHours ||
		s.Warmup.Hourly != defaults.Warmup.Hourly {
		t.Errorf("warmup = %+v, want min_samples 7 over the defaults", s.Warmup)
	}
	if s.MetricFloors == nil || len(s.MetricFloors) != 0 {
		t.Errorf("metric floors = %v, want the published empty map", s.MetricFloors)
	}

	for _, raw := range []string{`{"default_sensitivity":0}`, `{"default_sensitivity":null}`} {
		if _, source := ParseScoringSettings([]byte(raw)); source != ScoringSourceDefaults {
			t.Errorf("%s: source %q, want defaults", raw, source)
		}
	}
}

func TestResolveComparisonMetric(t *testing.T) {
	db := "postgres"
	tests := []struct {
		name    string
		metric  string
		db      *string
		wantErr string
	}{
		{"server-level", "pg_sys_cpu_usage_info.processor_time_percent", nil, ""},
		{"per-database", "pg_stat_database.temp_files_delta", &db, ""},
		{"unknown", "pg_stat_activity.nope", nil, "unknown metric"},
		{"per-database without a database", "pg_stat_database.temp_files_delta", nil,
			"database_name is required"},
		{"server-level with a database", "pg_sys_cpu_usage_info.processor_time_percent", &db,
			"database_name must not be given"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def, err := ResolveComparisonMetric(tt.metric, tt.db)
			if tt.wantErr == "" {
				if err != nil || def.Name != tt.metric {
					t.Errorf("def %q err %v; want %q", def.Name, err, tt.metric)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want %q", err, tt.wantErr)
			}
		})
	}
	if _, err := ResolveComparisonMetric("x", nil); !errors.Is(err, ErrUnknownComparisonMetric) {
		t.Errorf("err = %v, want ErrUnknownComparisonMetric", err)
	}
}

func TestComparisonHelpers(t *testing.T) {
	for _, m := range ComparisonModels {
		if !IsValidComparisonModel(m) {
			t.Errorf("model %q rejected", m)
		}
	}
	if IsValidComparisonModel("overall") {
		t.Error("model overall accepted")
	}

	tests := []struct {
		start time.Duration
		want  int
	}{
		{-time.Hour, 2},
		{-24 * time.Hour, 2},
		{-25 * time.Hour, 3},
		{-30 * 24 * time.Hour, 31},
		{time.Hour, 1}, // a window starting in the future
		{48 * time.Hour, 1},
	}
	for _, tt := range tests {
		w := TimeWindow{Start: cmpNow.Add(tt.start), End: cmpNow}
		if got := comparisonLookbackDays(w, cmpNow); got != tt.want {
			t.Errorf("lookback for a start %v from now = %d, want %d", tt.start, got, tt.want)
		}
	}
}

func TestComparisonUpperBound(t *testing.T) {
	raw, ok := anomaly.Metric(cpuMetric)
	if !ok || raw.BucketWidth != 0 {
		t.Fatalf("%s is not a raw metric", cpuMetric)
	}
	hourly, ok := anomaly.Metric(deadlocksMetric)
	if !ok || hourly.BucketWidth != time.Hour {
		t.Fatalf("%s is not an hourly metric", deadlocksMetric)
	}
	hour := time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		def  anomaly.MetricDef
		end  time.Time
		want time.Time
	}{
		{"raw metric ends at the window end", raw, hour.Add(10 * time.Minute), hour.Add(10 * time.Minute)},
		{"raw metric in another zone", raw, hour.In(time.FixedZone("x", 3600)), hour},
		{"hourly metric mid-bucket", hourly, hour.Add(10 * time.Minute), hour.Add(time.Hour)},
		{"hourly metric on a boundary", hourly, hour, hour.Add(time.Hour)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := comparisonUpperBound(tt.def, tt.end)
			if !got.Equal(tt.want) || got.Location() != time.UTC {
				t.Errorf("comparisonUpperBound = %v, want %v in UTC", got, tt.want)
			}
		})
	}
}

func TestIsMissingSchemaError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"undefined column", &pgconn.PgError{Code: "42703"}, true},
		{"undefined table", &pgconn.PgError{Code: "42P01"}, true},
		{"wrapped", fmt.Errorf("query: %w", &pgconn.PgError{Code: "42703"}), true},
		{"other SQLSTATE", &pgconn.PgError{Code: "42501"}, false},
		{"not a PostgreSQL error", errors.New("boom"), false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		if got := isMissingSchemaError(tt.err); got != tt.want {
			t.Errorf("%s: isMissingSchemaError = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestAllFinite(t *testing.T) {
	if !allFinite() || !allFinite(0, -1.5, 1e300) {
		t.Error("finite values rejected")
	}
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if allFinite(1, v) {
			t.Errorf("allFinite accepted %v", v)
		}
	}
}
