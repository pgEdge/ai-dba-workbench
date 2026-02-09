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
	"context"
	"fmt"
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/cron"
	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// checkScheduledBlackouts activates scheduled blackouts based on cron expressions
func (e *Engine) checkScheduledBlackouts(ctx context.Context) {
	e.debugLog("Checking scheduled blackouts...")

	// Get all enabled blackout schedules
	schedules, err := e.datastore.GetEnabledBlackoutSchedules(ctx)
	if err != nil {
		e.log("ERROR: Failed to get blackout schedules: %v", err)
		return
	}

	now := time.Now()

	for _, schedule := range schedules {
		if ctx.Err() != nil {
			return
		}

		// Check if current time matches the cron expression
		// This is a simplified check - a full implementation would use a cron parser
		if e.cronMatches(schedule.CronExpression, now, schedule.Timezone) {
			// Check if there's already an active blackout for this schedule
			connID := schedule.ConnectionID
			active, err := e.datastore.IsBlackoutActive(ctx, connID, schedule.DatabaseName)
			if err != nil {
				e.debugLog("Error checking blackout for schedule %s: %v", schedule.Name, err)
			}
			if active {
				continue
			}

			// Create a new blackout entry, inheriting scope from the schedule
			endTime := now.Add(time.Duration(schedule.DurationMinutes) * time.Minute)
			blackout := &database.Blackout{
				Scope:        schedule.Scope,
				ConnectionID: schedule.ConnectionID,
				GroupID:      schedule.GroupID,
				ClusterID:    schedule.ClusterID,
				DatabaseName: schedule.DatabaseName,
				Reason:       fmt.Sprintf("Scheduled: %s", schedule.Name),
				StartTime:    now,
				EndTime:      endTime,
				CreatedBy:    "scheduler",
				CreatedAt:    now,
			}

			if err := e.datastore.CreateBlackout(ctx, blackout); err != nil {
				e.log("ERROR: Failed to create blackout: %v", err)
			} else {
				e.log("Created scheduled blackout: %s (until %s)", schedule.Name, endTime.Format(time.RFC3339))
			}
		}
	}
}

// cronMatches checks if the current time matches a cron expression.
// It supports standard 5-field cron expressions with wildcards, ranges,
// lists, and steps (e.g., "*/15 9-17 * * 1-5" for every 15 minutes
// from 9am-5pm on weekdays).
func (e *Engine) cronMatches(cronExpr string, now time.Time, timezone string) bool {
	matches, err := cron.Matches(cronExpr, now, timezone)
	if err != nil {
		e.debugLog("Invalid cron expression %q: %v", cronExpr, err)
		return false
	}
	return matches
}
