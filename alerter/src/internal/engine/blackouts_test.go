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
)

// TestCronMatchesValidExpressions tests valid cron expression matching
func TestCronMatchesValidExpressions(t *testing.T) {
	engine := &Engine{}

	tests := []struct {
		name     string
		cronExpr string
		testTime time.Time
		timezone string
		expected bool
	}{
		// Exact time matches
		{
			name:     "exact time match",
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

		// Wildcard tests
		{
			name:     "all wildcards at midnight",
			cronExpr: "* * * * *",
			testTime: time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC),
			timezone: "UTC",
			expected: true,
		},
		{
			name:     "all wildcards any time",
			cronExpr: "* * * * *",
			testTime: time.Date(2025, 6, 20, 15, 45, 30, 0, time.UTC),
			timezone: "UTC",
			expected: true,
		},

		// Step expressions
		{
			name:     "every 15 minutes at 0",
			cronExpr: "*/15 * * * *",
			testTime: time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
			timezone: "UTC",
			expected: true,
		},
		{
			name:     "every 15 minutes at 15",
			cronExpr: "*/15 * * * *",
			testTime: time.Date(2025, 1, 15, 10, 15, 0, 0, time.UTC),
			timezone: "UTC",
			expected: true,
		},
		{
			name:     "every 15 minutes at 30",
			cronExpr: "*/15 * * * *",
			testTime: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			timezone: "UTC",
			expected: true,
		},
		{
			name:     "every 15 minutes at 45",
			cronExpr: "*/15 * * * *",
			testTime: time.Date(2025, 1, 15, 10, 45, 0, 0, time.UTC),
			timezone: "UTC",
			expected: true,
		},
		{
			name:     "every 15 minutes at 7 no match",
			cronExpr: "*/15 * * * *",
			testTime: time.Date(2025, 1, 15, 10, 7, 0, 0, time.UTC),
			timezone: "UTC",
			expected: false,
		},

		// Range expressions
		{
			name:     "weekday range Monday",
			cronExpr: "30 14 * * 1-5",
			testTime: time.Date(2025, 1, 13, 14, 30, 0, 0, time.UTC), // Monday
			timezone: "UTC",
			expected: true,
		},
		{
			name:     "weekday range Wednesday",
			cronExpr: "30 14 * * 1-5",
			testTime: time.Date(2025, 1, 15, 14, 30, 0, 0, time.UTC), // Wednesday
			timezone: "UTC",
			expected: true,
		},
		{
			name:     "weekday range Friday",
			cronExpr: "30 14 * * 1-5",
			testTime: time.Date(2025, 1, 17, 14, 30, 0, 0, time.UTC), // Friday
			timezone: "UTC",
			expected: true,
		},
		{
			name:     "weekday range Sunday no match",
			cronExpr: "30 14 * * 1-5",
			testTime: time.Date(2025, 1, 19, 14, 30, 0, 0, time.UTC), // Sunday
			timezone: "UTC",
			expected: false,
		},
		{
			name:     "weekday range Saturday no match",
			cronExpr: "30 14 * * 1-5",
			testTime: time.Date(2025, 1, 18, 14, 30, 0, 0, time.UTC), // Saturday
			timezone: "UTC",
			expected: false,
		},

		// Day of month
		{
			name:     "specific day of month match",
			cronExpr: "0 9 15 * *",
			testTime: time.Date(2025, 1, 15, 9, 0, 0, 0, time.UTC),
			timezone: "UTC",
			expected: true,
		},
		{
			name:     "specific day of month no match",
			cronExpr: "0 9 15 * *",
			testTime: time.Date(2025, 1, 16, 9, 0, 0, 0, time.UTC),
			timezone: "UTC",
			expected: false,
		},

		// Specific month
		{
			name:     "specific month match",
			cronExpr: "0 0 1 6 *",
			testTime: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
			timezone: "UTC",
			expected: true,
		},
		{
			name:     "specific month no match",
			cronExpr: "0 0 1 6 *",
			testTime: time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC),
			timezone: "UTC",
			expected: false,
		},

		// Edge times
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

// TestCronMatchesInvalidExpressions tests that invalid expressions return false
func TestCronMatchesInvalidExpressions(t *testing.T) {
	engine := &Engine{}

	invalidExprs := []string{
		"",
		"invalid",
		"30",
		"30 14",
		"30 14 *",
		"30 14 * *",
		"a b c d e",
		"60 14 * * *",  // minute out of range
		"30 25 * * *",  // hour out of range
		"30 14 32 * *", // day out of range
		"30 14 * 13 *", // month out of range
		"30 14 * * 8",  // weekday out of range
	}

	testTime := time.Date(2025, 1, 15, 14, 30, 0, 0, time.UTC)

	for _, expr := range invalidExprs {
		t.Run(expr, func(t *testing.T) {
			result := engine.cronMatches(expr, testTime, "UTC")
			if result {
				t.Errorf("cronMatches(%q) should return false for invalid expression", expr)
			}
		})
	}
}

// TestCronMatchesTimezoneAware tests timezone-aware cron matching
func TestCronMatchesTimezoneAware(t *testing.T) {
	engine := &Engine{}

	// Load EST timezone
	estLoc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("Skipping timezone test: America/New_York not available")
	}

	// Create 9:00 AM EST (which is 14:00 UTC in January due to EST being UTC-5)
	estTime := time.Date(2025, 1, 15, 9, 0, 0, 0, estLoc)

	tests := []struct {
		name     string
		cronExpr string
		testTime time.Time
		timezone string
		expected bool
	}{
		{
			name:     "9 AM EST matches EST cron",
			cronExpr: "0 9 * * *",
			testTime: estTime,
			timezone: "America/New_York",
			expected: true,
		},
		{
			name:     "9 AM EST is 14:00 UTC",
			cronExpr: "0 14 * * *",
			testTime: estTime,
			timezone: "UTC",
			expected: true,
		},
		{
			name:     "9 AM EST does not match 10 AM EST cron",
			cronExpr: "0 10 * * *",
			testTime: estTime,
			timezone: "America/New_York",
			expected: false,
		},
		{
			name:     "invalid timezone falls back to UTC",
			cronExpr: "0 14 * * *",
			testTime: estTime,
			timezone: "Invalid/Timezone",
			expected: true, // Falls back to UTC, where the time is 14:00
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

// TestCronMatchesComplexExpressions tests complex cron expressions
func TestCronMatchesComplexExpressions(t *testing.T) {
	engine := &Engine{}

	tests := []struct {
		name     string
		cronExpr string
		testTime time.Time
		timezone string
		expected bool
	}{
		// Business hours (9-17 on weekdays)
		{
			name:     "business hours match 9am Monday",
			cronExpr: "0 9-17 * * 1-5",
			testTime: time.Date(2025, 1, 13, 9, 0, 0, 0, time.UTC), // Monday 9am
			timezone: "UTC",
			expected: true,
		},
		{
			name:     "business hours match noon Wednesday",
			cronExpr: "0 9-17 * * 1-5",
			testTime: time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC), // Wednesday noon
			timezone: "UTC",
			expected: true,
		},
		{
			name:     "business hours no match 8am Friday",
			cronExpr: "0 9-17 * * 1-5",
			testTime: time.Date(2025, 1, 17, 8, 0, 0, 0, time.UTC), // Friday 8am
			timezone: "UTC",
			expected: false,
		},
		{
			name:     "business hours no match Saturday",
			cronExpr: "0 9-17 * * 1-5",
			testTime: time.Date(2025, 1, 18, 12, 0, 0, 0, time.UTC), // Saturday noon
			timezone: "UTC",
			expected: false,
		},

		// Maintenance window: 2am on first Sunday of month
		{
			name:     "maintenance window first Sunday",
			cronExpr: "0 2 1-7 * 0",
			testTime: time.Date(2025, 1, 5, 2, 0, 0, 0, time.UTC), // First Sunday of Jan 2025
			timezone: "UTC",
			expected: true,
		},

		// Every 5 minutes during business hours
		{
			name:     "5min interval at :05",
			cronExpr: "*/5 9-17 * * *",
			testTime: time.Date(2025, 1, 15, 10, 5, 0, 0, time.UTC),
			timezone: "UTC",
			expected: true,
		},
		{
			name:     "5min interval at :03 no match",
			cronExpr: "*/5 9-17 * * *",
			testTime: time.Date(2025, 1, 15, 10, 3, 0, 0, time.UTC),
			timezone: "UTC",
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
