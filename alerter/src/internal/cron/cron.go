/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package cron provides utilities for parsing and evaluating cron expressions.
// It uses the robfig/cron/v3 library for reliable cron parsing that supports
// all standard cron features including wildcards, ranges, lists, and steps.
package cron

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// Parser wraps the robfig/cron parser configured for standard 5-field cron
// expressions (minute, hour, day-of-month, month, day-of-week).
type Parser struct {
	parser cron.Parser
}

// NewParser creates a new cron Parser configured for standard 5-field
// cron expressions.
func NewParser() *Parser {
	return &Parser{
		parser: cron.NewParser(
			cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow,
		),
	}
}

// Schedule represents a parsed cron schedule that can check if a time matches.
type Schedule interface {
	// Next returns the next activation time after the given time.
	Next(t time.Time) time.Time
}

// Parse parses a standard 5-field cron expression and returns a Schedule.
// The expression format is: minute hour day-of-month month day-of-week
//
// Supported syntax:
//   - Wildcards: * (matches any value)
//   - Ranges: 1-5 (matches 1, 2, 3, 4, 5)
//   - Lists: 1,3,5 (matches 1, 3, or 5)
//   - Steps: */5 (matches every 5th value)
//   - Combined: 1-5/2 (matches 1, 3, 5)
//
// Examples:
//   - "0 0 * * *" - Daily at midnight
//   - "*/15 * * * *" - Every 15 minutes
//   - "0 9-17 * * 1-5" - Every hour from 9am-5pm on weekdays
//   - "30 4 1,15 * *" - At 4:30am on the 1st and 15th of each month
func (p *Parser) Parse(expr string) (Schedule, error) {
	schedule, err := p.parser.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression %q: %w", expr, err)
	}
	return schedule, nil
}

// Matches checks if the given time matches a cron expression in the
// specified timezone. Returns true if the current minute matches the
// schedule (i.e., the next scheduled time from one minute ago is now).
func (p *Parser) Matches(expr string, t time.Time, timezone string) (bool, error) {
	// Parse timezone
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	localTime := t.In(loc)

	// Parse the cron expression
	schedule, err := p.Parse(expr)
	if err != nil {
		return false, err
	}

	// Truncate to the current minute for comparison
	currentMinute := localTime.Truncate(time.Minute)

	// Check if the current minute is a scheduled time by calculating
	// the next scheduled time from just before the current minute
	oneMinuteAgo := currentMinute.Add(-1 * time.Minute)
	nextScheduled := schedule.Next(oneMinuteAgo)

	// The current minute matches if it equals the next scheduled time
	return nextScheduled.Equal(currentMinute), nil
}

// Validate checks if a cron expression is valid without returning a schedule.
func (p *Parser) Validate(expr string) error {
	_, err := p.Parse(expr)
	return err
}

// DefaultParser is a package-level parser for convenience.
var DefaultParser = NewParser()

// Parse parses a cron expression using the default parser.
func Parse(expr string) (Schedule, error) {
	return DefaultParser.Parse(expr)
}

// Matches checks if a time matches a cron expression using the default parser.
func Matches(expr string, t time.Time, timezone string) (bool, error) {
	return DefaultParser.Matches(expr, t, timezone)
}

// Validate validates a cron expression using the default parser.
func Validate(expr string) error {
	return DefaultParser.Validate(expr)
}
