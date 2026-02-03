/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package cron

import (
	"testing"
	"time"
)

func TestParse_ValidExpressions(t *testing.T) {
	tests := []struct {
		name string
		expr string
	}{
		{"every minute", "* * * * *"},
		{"every hour at minute 0", "0 * * * *"},
		{"daily at midnight", "0 0 * * *"},
		{"weekly on Sunday", "0 0 * * 0"},
		{"monthly on first day", "0 0 1 * *"},
		{"yearly on January 1st", "0 0 1 1 *"},
		{"every 5 minutes", "*/5 * * * *"},
		{"every 15 minutes", "*/15 * * * *"},
		{"range of hours", "0 9-17 * * *"},
		{"list of days", "0 0 * * 1,3,5"},
		{"complex expression", "30 4 1,15 * *"},
		{"weekday at 9am", "0 9 * * 1-5"},
		{"step with range", "0 0-12/2 * * *"},
		{"all wildcards", "* * * * *"},
		{"specific time", "30 14 * * *"},
	}

	parser := NewParser()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schedule, err := parser.Parse(tt.expr)
			if err != nil {
				t.Errorf("Parse(%q) unexpected error: %v", tt.expr, err)
			}
			if schedule == nil {
				t.Errorf("Parse(%q) returned nil schedule", tt.expr)
			}
		})
	}
}

func TestParse_InvalidExpressions(t *testing.T) {
	tests := []struct {
		name string
		expr string
	}{
		{"empty string", ""},
		{"single field", "0"},
		{"two fields", "0 0"},
		{"three fields", "0 0 *"},
		{"four fields", "0 0 * *"},
		{"invalid minute", "60 * * * *"},
		{"invalid hour", "0 24 * * *"},
		{"invalid day of month", "0 0 32 * *"},
		{"invalid month", "0 0 * 13 *"},
		{"invalid day of week", "0 0 * * 8"},
		{"invalid characters", "a b c d e"},
		{"negative values", "-1 * * * *"},
	}

	parser := NewParser()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parser.Parse(tt.expr)
			if err == nil {
				t.Errorf("Parse(%q) expected error but got none", tt.expr)
			}
		})
	}
}

func TestMatches_BasicExpressions(t *testing.T) {
	parser := NewParser()

	// Test time: 2024-06-15 14:30:00 UTC (Saturday)
	testTime := time.Date(2024, 6, 15, 14, 30, 0, 0, time.UTC)

	tests := []struct {
		name     string
		expr     string
		expected bool
	}{
		{"every minute matches", "* * * * *", true},
		{"exact minute and hour", "30 14 * * *", true},
		{"wrong minute", "31 14 * * *", false},
		{"wrong hour", "30 15 * * *", false},
		{"every 30 minutes at 30", "*/30 * * * *", true},
		{"every 30 minutes at 0", "*/30 * * * *", true}, // Will match since */30 matches 0 and 30
		{"every 15 minutes", "*/15 14 * * *", true},     // 30 is divisible by 15
		{"Saturday (day 6)", "30 14 * * 6", true},
		{"Sunday (day 0)", "30 14 * * 0", false},
		{"weekday only", "30 14 * * 1-5", false}, // Saturday is not 1-5
		{"15th of month", "30 14 15 * *", true},
		{"wrong day of month", "30 14 16 * *", false},
		{"June match", "30 14 15 6 *", true},
		{"July no match", "30 14 15 7 *", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches, err := parser.Matches(tt.expr, testTime, "UTC")
			if err != nil {
				t.Errorf("Matches(%q) unexpected error: %v", tt.expr, err)
				return
			}
			if matches != tt.expected {
				t.Errorf("Matches(%q, %v) = %v, want %v",
					tt.expr, testTime, matches, tt.expected)
			}
		})
	}
}

func TestMatches_Timezone(t *testing.T) {
	parser := NewParser()

	// 14:30 UTC = 10:30 EDT (America/New_York in summer)
	testTime := time.Date(2024, 6, 15, 14, 30, 0, 0, time.UTC)

	tests := []struct {
		name     string
		expr     string
		timezone string
		expected bool
	}{
		{"UTC exact match", "30 14 * * *", "UTC", true},
		{"NYC time match", "30 10 * * *", "America/New_York", true},
		{"NYC wrong hour", "30 14 * * *", "America/New_York", false},
		{"LA time match", "30 7 * * *", "America/Los_Angeles", true},
		{"invalid timezone defaults to UTC", "30 14 * * *", "Invalid/Zone", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches, err := parser.Matches(tt.expr, testTime, tt.timezone)
			if err != nil {
				t.Errorf("Matches(%q) unexpected error: %v", tt.expr, err)
				return
			}
			if matches != tt.expected {
				t.Errorf("Matches(%q, %v, %q) = %v, want %v",
					tt.expr, testTime, tt.timezone, matches, tt.expected)
			}
		})
	}
}

func TestMatches_Ranges(t *testing.T) {
	parser := NewParser()

	// Test at 10:30 on a Tuesday (day 2)
	testTime := time.Date(2024, 6, 18, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name     string
		expr     string
		expected bool
	}{
		{"hour in range", "30 9-17 * * *", true},
		{"hour below range", "30 11-17 * * *", false},
		{"hour above range", "30 8-9 * * *", false},
		{"minute in range", "25-35 10 * * *", true},
		{"weekday in range", "30 10 * * 1-5", true},
		{"day not in range", "30 10 * * 4-5", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches, err := parser.Matches(tt.expr, testTime, "UTC")
			if err != nil {
				t.Errorf("Matches(%q) unexpected error: %v", tt.expr, err)
				return
			}
			if matches != tt.expected {
				t.Errorf("Matches(%q, %v) = %v, want %v",
					tt.expr, testTime, matches, tt.expected)
			}
		})
	}
}

func TestMatches_Lists(t *testing.T) {
	parser := NewParser()

	// Test at 09:30 on Tuesday June 18, 2024
	testTime := time.Date(2024, 6, 18, 9, 30, 0, 0, time.UTC)

	tests := []struct {
		name     string
		expr     string
		expected bool
	}{
		{"minute in list", "15,30,45 9 * * *", true},
		{"minute not in list", "15,45 9 * * *", false},
		{"hour in list", "30 8,9,10 * * *", true},
		{"hour not in list", "30 8,10,11 * * *", false},
		{"day in list", "30 9 * * 1,2,3", true},
		{"day not in list", "30 9 * * 0,3,4", false},
		{"month in list", "30 9 * 5,6,7 *", true},
		{"month not in list", "30 9 * 1,2,3 *", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches, err := parser.Matches(tt.expr, testTime, "UTC")
			if err != nil {
				t.Errorf("Matches(%q) unexpected error: %v", tt.expr, err)
				return
			}
			if matches != tt.expected {
				t.Errorf("Matches(%q, %v) = %v, want %v",
					tt.expr, testTime, matches, tt.expected)
			}
		})
	}
}

func TestMatches_Steps(t *testing.T) {
	parser := NewParser()

	tests := []struct {
		name     string
		expr     string
		testTime time.Time
		expected bool
	}{
		{
			"every 5 min at 0",
			"*/5 * * * *",
			time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC),
			true,
		},
		{
			"every 5 min at 5",
			"*/5 * * * *",
			time.Date(2024, 6, 15, 10, 5, 0, 0, time.UTC),
			true,
		},
		{
			"every 5 min at 7",
			"*/5 * * * *",
			time.Date(2024, 6, 15, 10, 7, 0, 0, time.UTC),
			false,
		},
		{
			"every 15 min at 15",
			"*/15 * * * *",
			time.Date(2024, 6, 15, 10, 15, 0, 0, time.UTC),
			true,
		},
		{
			"every 15 min at 30",
			"*/15 * * * *",
			time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC),
			true,
		},
		{
			"every 2 hours at 0",
			"0 */2 * * *",
			time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC),
			true,
		},
		{
			"every 2 hours at 2",
			"0 */2 * * *",
			time.Date(2024, 6, 15, 2, 0, 0, 0, time.UTC),
			true,
		},
		{
			"every 2 hours at 3",
			"0 */2 * * *",
			time.Date(2024, 6, 15, 3, 0, 0, 0, time.UTC),
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches, err := parser.Matches(tt.expr, tt.testTime, "UTC")
			if err != nil {
				t.Errorf("Matches(%q) unexpected error: %v", tt.expr, err)
				return
			}
			if matches != tt.expected {
				t.Errorf("Matches(%q, %v) = %v, want %v",
					tt.expr, tt.testTime, matches, tt.expected)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		wantErr bool
	}{
		{"valid every minute", "* * * * *", false},
		{"valid daily", "0 0 * * *", false},
		{"valid complex", "*/15 9-17 * * 1-5", false},
		{"invalid empty", "", true},
		{"invalid format", "not a cron", true},
		{"invalid minute", "99 * * * *", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.expr)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate(%q) error = %v, wantErr %v",
					tt.expr, err, tt.wantErr)
			}
		})
	}
}

func TestSchedule_Next(t *testing.T) {
	parser := NewParser()

	// Test getting next scheduled time
	schedule, err := parser.Parse("0 9 * * *") // Daily at 9am
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Start from 8am on June 15
	startTime := time.Date(2024, 6, 15, 8, 0, 0, 0, time.UTC)
	nextTime := schedule.Next(startTime)

	// Should be 9am same day
	expected := time.Date(2024, 6, 15, 9, 0, 0, 0, time.UTC)
	if !nextTime.Equal(expected) {
		t.Errorf("Next(%v) = %v, want %v", startTime, nextTime, expected)
	}

	// Start from 10am on June 15
	startTime = time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)
	nextTime = schedule.Next(startTime)

	// Should be 9am next day
	expected = time.Date(2024, 6, 16, 9, 0, 0, 0, time.UTC)
	if !nextTime.Equal(expected) {
		t.Errorf("Next(%v) = %v, want %v", startTime, nextTime, expected)
	}
}

func TestDefaultParser(t *testing.T) {
	// Test package-level functions use the default parser
	_, err := Parse("* * * * *")
	if err != nil {
		t.Errorf("Parse with default parser failed: %v", err)
	}

	_, err = Matches("* * * * *", time.Now(), "UTC")
	if err != nil {
		t.Errorf("Matches with default parser failed: %v", err)
	}

	err = Validate("* * * * *")
	if err != nil {
		t.Errorf("Validate with default parser failed: %v", err)
	}
}
