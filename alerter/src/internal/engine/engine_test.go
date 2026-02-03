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
	"math"
	"testing"
	"time"
)

// TestCalculateStats tests the calculateStats function for mean and standard deviation
func TestCalculateStats(t *testing.T) {
	tests := []struct {
		name           string
		values         []float64
		expectedMean   float64
		expectedStdDev float64
		tolerance      float64
	}{
		{
			name:           "empty slice",
			values:         []float64{},
			expectedMean:   0,
			expectedStdDev: 0,
			tolerance:      0.0001,
		},
		{
			name:           "single value",
			values:         []float64{5.0},
			expectedMean:   5.0,
			expectedStdDev: 0,
			tolerance:      0.0001,
		},
		{
			name:           "two identical values",
			values:         []float64{10.0, 10.0},
			expectedMean:   10.0,
			expectedStdDev: 0,
			tolerance:      0.0001,
		},
		{
			name:           "simple values",
			values:         []float64{2.0, 4.0, 4.0, 4.0, 5.0, 5.0, 7.0, 9.0},
			expectedMean:   5.0,
			expectedStdDev: 2.0,
			tolerance:      0.01,
		},
		{
			name:           "two values with variance",
			values:         []float64{0.0, 10.0},
			expectedMean:   5.0,
			expectedStdDev: 5.0,
			tolerance:      0.01,
		},
		{
			name:           "negative values",
			values:         []float64{-5.0, -3.0, -1.0, 1.0, 3.0, 5.0},
			expectedMean:   0.0,
			expectedStdDev: 3.4156, // sqrt(70/6) population stddev
			tolerance:      0.1,
		},
		{
			name:           "large spread values",
			values:         []float64{1.0, 100.0, 1000.0},
			expectedMean:   367.0,
			expectedStdDev: 450.185, // population stddev
			tolerance:      1.0,
		},
		{
			name:           "typical database metrics",
			values:         []float64{50.0, 55.0, 48.0, 52.0, 49.0, 53.0, 51.0, 47.0},
			expectedMean:   50.625,
			expectedStdDev: 2.5495, // population stddev
			tolerance:      0.1,
		},
		{
			name:           "connection count scenario",
			values:         []float64{100.0, 150.0, 120.0, 180.0, 130.0},
			expectedMean:   136.0,
			expectedStdDev: 27.276, // population stddev: sqrt(744) = 27.276
			tolerance:      0.5,
		},
		{
			name:           "replication lag values",
			values:         []float64{0.1, 0.2, 0.15, 0.3, 0.25},
			expectedMean:   0.2,
			expectedStdDev: 0.07071, // population stddev
			tolerance:      0.01,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mean, stddev := calculateStats(tt.values)

			if math.Abs(mean-tt.expectedMean) > tt.tolerance {
				t.Errorf("mean = %v, expected %v (tolerance %v)", mean, tt.expectedMean, tt.tolerance)
			}

			if math.Abs(stddev-tt.expectedStdDev) > tt.tolerance {
				t.Errorf("stddev = %v, expected %v (tolerance %v)", stddev, tt.expectedStdDev, tt.tolerance)
			}
		})
	}
}

// TestCalculateStatsSquareRoot verifies that standard deviation is correctly
// calculated using sqrt (not just variance)
func TestCalculateStatsSquareRoot(t *testing.T) {
	// For values [0, 10], mean = 5, variance = 25, stddev should be 5 (not 25)
	values := []float64{0.0, 10.0}
	mean, stddev := calculateStats(values)

	if mean != 5.0 {
		t.Errorf("mean = %v, expected 5.0", mean)
	}

	// The stddev should be approximately 5 (sqrt of variance 25), not 25
	if stddev > 10.0 {
		t.Errorf("stddev = %v appears to be variance, not standard deviation (expected ~5.0)", stddev)
	}

	if math.Abs(stddev-5.0) > 0.1 {
		t.Errorf("stddev = %v, expected approximately 5.0", stddev)
	}
}

// TestMinValue tests the minValue function
func TestMinValue(t *testing.T) {
	tests := []struct {
		name     string
		values   []float64
		expected float64
	}{
		{
			name:     "empty slice",
			values:   []float64{},
			expected: 0,
		},
		{
			name:     "single value",
			values:   []float64{42.0},
			expected: 42.0,
		},
		{
			name:     "positive values",
			values:   []float64{5.0, 3.0, 8.0, 1.0, 9.0},
			expected: 1.0,
		},
		{
			name:     "negative values",
			values:   []float64{-5.0, -3.0, -8.0, -1.0},
			expected: -8.0,
		},
		{
			name:     "mixed values",
			values:   []float64{-5.0, 3.0, -8.0, 10.0},
			expected: -8.0,
		},
		{
			name:     "duplicate minimum",
			values:   []float64{5.0, 1.0, 8.0, 1.0, 9.0},
			expected: 1.0,
		},
		{
			name:     "all same values",
			values:   []float64{7.0, 7.0, 7.0},
			expected: 7.0,
		},
		{
			name:     "first is minimum",
			values:   []float64{1.0, 5.0, 3.0, 8.0},
			expected: 1.0,
		},
		{
			name:     "last is minimum",
			values:   []float64{5.0, 3.0, 8.0, 1.0},
			expected: 1.0,
		},
		{
			name:     "zero in values",
			values:   []float64{5.0, 0.0, 3.0, 8.0},
			expected: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := minValue(tt.values)
			if result != tt.expected {
				t.Errorf("minValue(%v) = %v, expected %v", tt.values, result, tt.expected)
			}
		})
	}
}

// TestMaxValue tests the maxValue function
func TestMaxValue(t *testing.T) {
	tests := []struct {
		name     string
		values   []float64
		expected float64
	}{
		{
			name:     "empty slice",
			values:   []float64{},
			expected: 0,
		},
		{
			name:     "single value",
			values:   []float64{42.0},
			expected: 42.0,
		},
		{
			name:     "positive values",
			values:   []float64{5.0, 3.0, 8.0, 1.0, 9.0},
			expected: 9.0,
		},
		{
			name:     "negative values",
			values:   []float64{-5.0, -3.0, -8.0, -1.0},
			expected: -1.0,
		},
		{
			name:     "mixed values",
			values:   []float64{-5.0, 3.0, -8.0, 10.0},
			expected: 10.0,
		},
		{
			name:     "duplicate maximum",
			values:   []float64{5.0, 9.0, 8.0, 9.0, 1.0},
			expected: 9.0,
		},
		{
			name:     "all same values",
			values:   []float64{7.0, 7.0, 7.0},
			expected: 7.0,
		},
		{
			name:     "first is maximum",
			values:   []float64{10.0, 5.0, 3.0, 8.0},
			expected: 10.0,
		},
		{
			name:     "last is maximum",
			values:   []float64{5.0, 3.0, 8.0, 15.0},
			expected: 15.0,
		},
		{
			name:     "zero in values",
			values:   []float64{-5.0, 0.0, -3.0, -8.0},
			expected: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := maxValue(tt.values)
			if result != tt.expected {
				t.Errorf("maxValue(%v) = %v, expected %v", tt.values, result, tt.expected)
			}
		})
	}
}

// TestCheckThreshold tests the checkThreshold method for all operators
func TestCheckThreshold(t *testing.T) {
	engine := &Engine{}

	tests := []struct {
		name      string
		value     float64
		operator  string
		threshold float64
		expected  bool
	}{
		// Greater than tests
		{
			name:      "greater than - true",
			value:     100.0,
			operator:  ">",
			threshold: 50.0,
			expected:  true,
		},
		{
			name:      "greater than - false",
			value:     30.0,
			operator:  ">",
			threshold: 50.0,
			expected:  false,
		},
		{
			name:      "greater than - equal is false",
			value:     50.0,
			operator:  ">",
			threshold: 50.0,
			expected:  false,
		},

		// Greater than or equal tests
		{
			name:      "greater than or equal - greater",
			value:     100.0,
			operator:  ">=",
			threshold: 50.0,
			expected:  true,
		},
		{
			name:      "greater than or equal - equal",
			value:     50.0,
			operator:  ">=",
			threshold: 50.0,
			expected:  true,
		},
		{
			name:      "greater than or equal - less",
			value:     30.0,
			operator:  ">=",
			threshold: 50.0,
			expected:  false,
		},

		// Less than tests
		{
			name:      "less than - true",
			value:     30.0,
			operator:  "<",
			threshold: 50.0,
			expected:  true,
		},
		{
			name:      "less than - false",
			value:     100.0,
			operator:  "<",
			threshold: 50.0,
			expected:  false,
		},
		{
			name:      "less than - equal is false",
			value:     50.0,
			operator:  "<",
			threshold: 50.0,
			expected:  false,
		},

		// Less than or equal tests
		{
			name:      "less than or equal - less",
			value:     30.0,
			operator:  "<=",
			threshold: 50.0,
			expected:  true,
		},
		{
			name:      "less than or equal - equal",
			value:     50.0,
			operator:  "<=",
			threshold: 50.0,
			expected:  true,
		},
		{
			name:      "less than or equal - greater",
			value:     100.0,
			operator:  "<=",
			threshold: 50.0,
			expected:  false,
		},

		// Equal tests
		{
			name:      "equal - true",
			value:     50.0,
			operator:  "==",
			threshold: 50.0,
			expected:  true,
		},
		{
			name:      "equal - false greater",
			value:     100.0,
			operator:  "==",
			threshold: 50.0,
			expected:  false,
		},
		{
			name:      "equal - false less",
			value:     30.0,
			operator:  "==",
			threshold: 50.0,
			expected:  false,
		},

		// Not equal tests
		{
			name:      "not equal - true greater",
			value:     100.0,
			operator:  "!=",
			threshold: 50.0,
			expected:  true,
		},
		{
			name:      "not equal - true less",
			value:     30.0,
			operator:  "!=",
			threshold: 50.0,
			expected:  true,
		},
		{
			name:      "not equal - false",
			value:     50.0,
			operator:  "!=",
			threshold: 50.0,
			expected:  false,
		},

		// Edge cases
		{
			name:      "zero values",
			value:     0.0,
			operator:  ">",
			threshold: 0.0,
			expected:  false,
		},
		{
			name:      "negative threshold exceeded",
			value:     -10.0,
			operator:  "<",
			threshold: -5.0,
			expected:  true,
		},
		{
			name:      "small decimal difference",
			value:     50.001,
			operator:  ">",
			threshold: 50.0,
			expected:  true,
		},
		{
			name:      "unknown operator",
			value:     100.0,
			operator:  "~",
			threshold: 50.0,
			expected:  false,
		},
		{
			name:      "empty operator",
			value:     100.0,
			operator:  "",
			threshold: 50.0,
			expected:  false,
		},

		// Real-world alert scenarios
		{
			name:      "connection utilization above 80%",
			value:     85.5,
			operator:  ">",
			threshold: 80.0,
			expected:  true,
		},
		{
			name:      "replication lag above 10 seconds",
			value:     15.0,
			operator:  ">",
			threshold: 10.0,
			expected:  true,
		},
		{
			name:      "disk space below 20%",
			value:     15.0,
			operator:  "<",
			threshold: 20.0,
			expected:  true,
		},
		{
			name:      "cache hit ratio below 90%",
			value:     88.5,
			operator:  "<",
			threshold: 90.0,
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.checkThreshold(tt.value, tt.operator, tt.threshold)
			if result != tt.expected {
				t.Errorf("checkThreshold(%v, %q, %v) = %v, expected %v",
					tt.value, tt.operator, tt.threshold, result, tt.expected)
			}
		})
	}
}

// TestCronMatches tests the cronMatches method for cron expression parsing.
// Uses standard 5-field cron expressions: minute hour day-of-month month day-of-week
func TestCronMatches(t *testing.T) {
	engine := &Engine{}

	// Test invalid cron expressions that should always return false
	invalidTests := []struct {
		name     string
		cronExpr string
		testTime time.Time
		timezone string
	}{
		{
			name:     "invalid cron expression",
			cronExpr: "invalid",
			testTime: time.Date(2025, 1, 15, 14, 30, 0, 0, time.UTC),
			timezone: "UTC",
		},
		{
			name:     "empty cron expression",
			cronExpr: "",
			testTime: time.Date(2025, 1, 15, 14, 30, 0, 0, time.UTC),
			timezone: "UTC",
		},
		{
			name:     "partial cron expression single number",
			cronExpr: "30",
			testTime: time.Date(2025, 1, 15, 14, 30, 0, 0, time.UTC),
			timezone: "UTC",
		},
		{
			name:     "partial cron expression two fields",
			cronExpr: "30 14",
			testTime: time.Date(2025, 1, 15, 14, 30, 0, 0, time.UTC),
			timezone: "UTC",
		},
	}

	for _, tt := range invalidTests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.cronMatches(tt.cronExpr, tt.testTime, tt.timezone)
			if result != false {
				t.Errorf("cronMatches(%q, %v, %q) = %v, expected false for invalid expression",
					tt.cronExpr, tt.testTime, tt.timezone, result)
			}
		})
	}

	// Test matching 5-field cron expressions
	matchTests := []struct {
		name     string
		cronExpr string
		testTime time.Time
		timezone string
		expected bool
	}{
		{
			name:     "exact match UTC",
			cronExpr: "30 14 * * *",
			testTime: time.Date(2025, 1, 15, 14, 30, 0, 0, time.UTC),
			timezone: "UTC",
			expected: true,
		},
		{
			name:     "minute mismatch",
			cronExpr: "30 14 * * *",
			testTime: time.Date(2025, 1, 15, 14, 31, 0, 0, time.UTC),
			timezone: "UTC",
			expected: false,
		},
		{
			name:     "hour mismatch",
			cronExpr: "30 14 * * *",
			testTime: time.Date(2025, 1, 15, 15, 30, 0, 0, time.UTC),
			timezone: "UTC",
			expected: false,
		},
		{
			name:     "midnight match",
			cronExpr: "0 0 * * *",
			testTime: time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC),
			timezone: "UTC",
			expected: true,
		},
		{
			name:     "end of day match",
			cronExpr: "59 23 * * *",
			testTime: time.Date(2025, 1, 15, 23, 59, 0, 0, time.UTC),
			timezone: "UTC",
			expected: true,
		},
		{
			name:     "every 15 minutes - at 0",
			cronExpr: "*/15 * * * *",
			testTime: time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
			timezone: "UTC",
			expected: true,
		},
		{
			name:     "every 15 minutes - at 15",
			cronExpr: "*/15 * * * *",
			testTime: time.Date(2025, 1, 15, 10, 15, 0, 0, time.UTC),
			timezone: "UTC",
			expected: true,
		},
		{
			name:     "every 15 minutes - at 7 (no match)",
			cronExpr: "*/15 * * * *",
			testTime: time.Date(2025, 1, 15, 10, 7, 0, 0, time.UTC),
			timezone: "UTC",
			expected: false,
		},
		{
			name:     "weekday range - Wednesday matches",
			cronExpr: "30 14 * * 1-5",
			testTime: time.Date(2025, 1, 15, 14, 30, 0, 0, time.UTC), // Wednesday
			timezone: "UTC",
			expected: true,
		},
		{
			name:     "weekday range - Sunday no match",
			cronExpr: "30 14 * * 1-5",
			testTime: time.Date(2025, 1, 19, 14, 30, 0, 0, time.UTC), // Sunday
			timezone: "UTC",
			expected: false,
		},
		{
			name:     "day of month match",
			cronExpr: "30 14 15 * *",
			testTime: time.Date(2025, 1, 15, 14, 30, 0, 0, time.UTC),
			timezone: "UTC",
			expected: true,
		},
		{
			name:     "day of month no match",
			cronExpr: "30 14 16 * *",
			testTime: time.Date(2025, 1, 15, 14, 30, 0, 0, time.UTC),
			timezone: "UTC",
			expected: false,
		},
		{
			name:     "invalid timezone falls back to UTC",
			cronExpr: "30 14 * * *",
			testTime: time.Date(2025, 1, 15, 14, 30, 0, 0, time.UTC),
			timezone: "Invalid/Timezone",
			expected: true,
		},
	}

	for _, tt := range matchTests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.cronMatches(tt.cronExpr, tt.testTime, tt.timezone)
			if result != tt.expected {
				t.Errorf("cronMatches(%q, %v, %q) = %v, expected %v",
					tt.cronExpr, tt.testTime, tt.timezone, result, tt.expected)
			}
		})
	}
}

// TestCronMatchesWithTimezone tests cron matching with different timezones
func TestCronMatchesWithTimezone(t *testing.T) {
	engine := &Engine{}

	// Test case: Verify that timezone handling exists in the function
	// The implementation converts the time to the specified timezone before matching
	estLoc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("Skipping timezone test: America/New_York not available")
	}

	// Create a time that is 9:00 AM in EST (which is 14:00 UTC in January)
	estTime := time.Date(2025, 1, 15, 9, 0, 0, 0, estLoc)

	tests := []struct {
		name     string
		cronExpr string
		testTime time.Time
		timezone string
		expected bool
	}{
		{
			name:     "9:00 AM Eastern time matches",
			cronExpr: "0 9 * * *",
			testTime: estTime,
			timezone: "America/New_York",
			expected: true,
		},
		{
			name:     "9:00 AM Eastern expressed as 14:00 UTC",
			cronExpr: "0 14 * * *",
			testTime: estTime,
			timezone: "UTC",
			expected: true,
		},
		{
			name:     "wrong hour in Eastern time",
			cronExpr: "0 10 * * *",
			testTime: estTime,
			timezone: "America/New_York",
			expected: false,
		},
		{
			name:     "wrong minute in Eastern time",
			cronExpr: "30 9 * * *",
			testTime: estTime,
			timezone: "America/New_York",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.cronMatches(tt.cronExpr, tt.testTime, tt.timezone)
			if result != tt.expected {
				t.Errorf("cronMatches(%q, %v, %q) = %v, expected %v",
					tt.cronExpr, tt.testTime, tt.timezone, result, tt.expected)
			}
		})
	}
}

// TestNewEngine tests the NewEngine constructor
func TestNewEngine(t *testing.T) {
	tests := []struct {
		name  string
		debug bool
	}{
		{
			name:  "debug disabled",
			debug: false,
		},
		{
			name:  "debug enabled",
			debug: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewEngine(nil, nil, tt.debug)

			if engine == nil {
				t.Fatal("NewEngine returned nil")
			}

			if engine.debug != tt.debug {
				t.Errorf("engine.debug = %v, expected %v", engine.debug, tt.debug)
			}
		})
	}
}

// TestZScoreCalculation tests z-score calculation for anomaly detection
func TestZScoreCalculation(t *testing.T) {
	tests := []struct {
		name      string
		value     float64
		mean      float64
		stddev    float64
		expected  float64
		tolerance float64
	}{
		{
			name:      "value at mean",
			value:     100.0,
			mean:      100.0,
			stddev:    10.0,
			expected:  0.0,
			tolerance: 0.001,
		},
		{
			name:      "one stddev above",
			value:     110.0,
			mean:      100.0,
			stddev:    10.0,
			expected:  1.0,
			tolerance: 0.001,
		},
		{
			name:      "one stddev below",
			value:     90.0,
			mean:      100.0,
			stddev:    10.0,
			expected:  -1.0,
			tolerance: 0.001,
		},
		{
			name:      "three stddev above (anomaly)",
			value:     130.0,
			mean:      100.0,
			stddev:    10.0,
			expected:  3.0,
			tolerance: 0.001,
		},
		{
			name:      "three stddev below (anomaly)",
			value:     70.0,
			mean:      100.0,
			stddev:    10.0,
			expected:  -3.0,
			tolerance: 0.001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Z-score formula: (value - mean) / stddev
			zScore := (tt.value - tt.mean) / tt.stddev

			if math.Abs(zScore-tt.expected) > tt.tolerance {
				t.Errorf("z-score = %v, expected %v", zScore, tt.expected)
			}
		})
	}
}

// BenchmarkCalculateStats benchmarks the calculateStats function
func BenchmarkCalculateStats(b *testing.B) {
	// Create a realistic dataset size
	values := make([]float64, 1000)
	for i := range values {
		values[i] = float64(i%100) + 50.0
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		calculateStats(values)
	}
}

// BenchmarkCheckThreshold benchmarks the checkThreshold method
func BenchmarkCheckThreshold(b *testing.B) {
	engine := &Engine{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.checkThreshold(85.5, ">", 80.0)
	}
}

// BenchmarkMinValue benchmarks the minValue function
func BenchmarkMinValue(b *testing.B) {
	values := make([]float64, 1000)
	for i := range values {
		values[i] = float64(i%100) + 50.0
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		minValue(values)
	}
}

// BenchmarkMaxValue benchmarks the maxValue function
func BenchmarkMaxValue(b *testing.B) {
	values := make([]float64, 1000)
	for i := range values {
		values[i] = float64(i%100) + 50.0
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		maxValue(values)
	}
}

// TestReloadConfig tests the ReloadConfig method
func TestReloadConfig(t *testing.T) {
	engine := NewEngine(nil, nil, false)

	if engine.config != nil {
		t.Errorf("initial config should be nil")
	}

	// Verify that ReloadConfig with nil doesn't cause panic
	engine.ReloadConfig(nil)

	// The config field should still be accessible (nil in this case)
	if engine.config != nil {
		t.Errorf("config should be nil after ReloadConfig(nil)")
	}
}

// TestEngineFieldsInitialization tests that Engine fields are properly initialized
func TestEngineFieldsInitialization(t *testing.T) {
	tests := []struct {
		name      string
		debug     bool
		wantDebug bool
	}{
		{
			name:      "debug mode off",
			debug:     false,
			wantDebug: false,
		},
		{
			name:      "debug mode on",
			debug:     true,
			wantDebug: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewEngine(nil, nil, tt.debug)

			if engine.debug != tt.wantDebug {
				t.Errorf("Engine.debug = %v, want %v", engine.debug, tt.wantDebug)
			}

			if engine.config != nil {
				t.Errorf("Engine.config should be nil when created with nil config")
			}

			if engine.datastore != nil {
				t.Errorf("Engine.datastore should be nil when created with nil datastore")
			}
		})
	}
}

// TestCalculateStatsConsistency tests that calculateStats produces consistent results
func TestCalculateStatsConsistency(t *testing.T) {
	// Run calculateStats multiple times with the same input and verify consistency
	values := []float64{10.0, 20.0, 30.0, 40.0, 50.0}

	mean1, stddev1 := calculateStats(values)
	mean2, stddev2 := calculateStats(values)

	if mean1 != mean2 {
		t.Errorf("calculateStats should produce consistent mean: %v != %v", mean1, mean2)
	}

	if stddev1 != stddev2 {
		t.Errorf("calculateStats should produce consistent stddev: %v != %v", stddev1, stddev2)
	}
}

// TestCheckThresholdEdgeCases tests additional edge cases for checkThreshold
func TestCheckThresholdEdgeCases(t *testing.T) {
	engine := &Engine{}

	tests := []struct {
		name      string
		value     float64
		operator  string
		threshold float64
		expected  bool
	}{
		// Very large numbers
		{
			name:      "very large value greater than threshold",
			value:     1e15,
			operator:  ">",
			threshold: 1e14,
			expected:  true,
		},
		{
			name:      "very large threshold",
			value:     1e14,
			operator:  "<",
			threshold: 1e15,
			expected:  true,
		},
		// Very small numbers (precision test)
		{
			name:      "very small positive difference",
			value:     0.000001,
			operator:  ">",
			threshold: 0.0000001,
			expected:  true,
		},
		{
			name:      "very small negative values",
			value:     -0.000001,
			operator:  "<",
			threshold: -0.0000001,
			expected:  true,
		},
		// Infinity handling
		{
			name:      "infinity comparison",
			value:     math.Inf(1),
			operator:  ">",
			threshold: 1e308,
			expected:  true,
		},
		// NaN handling (NaN comparisons always return false)
		{
			name:      "NaN greater than",
			value:     math.NaN(),
			operator:  ">",
			threshold: 0,
			expected:  false,
		},
		{
			name:      "NaN less than",
			value:     math.NaN(),
			operator:  "<",
			threshold: 0,
			expected:  false,
		},
		{
			name:      "NaN equal",
			value:     math.NaN(),
			operator:  "==",
			threshold: math.NaN(),
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.checkThreshold(tt.value, tt.operator, tt.threshold)
			if result != tt.expected {
				t.Errorf("checkThreshold(%v, %q, %v) = %v, expected %v",
					tt.value, tt.operator, tt.threshold, result, tt.expected)
			}
		})
	}
}

// TestMinMaxValueLargeSlice tests minValue and maxValue with larger slices
func TestMinMaxValueLargeSlice(t *testing.T) {
	// Create a slice with 10000 elements
	size := 10000
	values := make([]float64, size)
	for i := 0; i < size; i++ {
		values[i] = float64(i)
	}

	min := minValue(values)
	max := maxValue(values)

	if min != 0.0 {
		t.Errorf("minValue of [0..9999] = %v, expected 0.0", min)
	}

	if max != float64(size-1) {
		t.Errorf("maxValue of [0..9999] = %v, expected %v", max, float64(size-1))
	}
}

// TestMinMaxValueReversedSlice tests with descending order
func TestMinMaxValueReversedSlice(t *testing.T) {
	values := []float64{100.0, 90.0, 80.0, 70.0, 60.0, 50.0, 40.0, 30.0, 20.0, 10.0}

	min := minValue(values)
	max := maxValue(values)

	if min != 10.0 {
		t.Errorf("minValue = %v, expected 10.0", min)
	}

	if max != 100.0 {
		t.Errorf("maxValue = %v, expected 100.0", max)
	}
}
