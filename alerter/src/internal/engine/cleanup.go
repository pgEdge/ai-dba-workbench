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
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// cleanResolvedAlerts clears alerts where the condition has resolved
func (e *Engine) cleanResolvedAlerts(ctx context.Context) {
	e.debugLog("Checking for resolved alerts...")

	// Get all active threshold alerts
	alerts, err := e.datastore.GetActiveAlerts(ctx)
	if err != nil {
		e.log("ERROR: Failed to get active alerts: %v", err)
		return
	}

	for _, alert := range alerts {
		if ctx.Err() != nil {
			return
		}

		if alert.AlertType == "threshold" && alert.RuleID != nil {
			e.checkAlertResolved(ctx, alert)
		}
	}
}

// checkAlertResolved checks if a threshold alert's condition has resolved
func (e *Engine) checkAlertResolved(ctx context.Context, alert *database.Alert) {
	if alert.MetricName == nil || alert.ThresholdValue == nil || alert.Operator == nil {
		return
	}

	// Get current metric values for all connections/databases
	values, err := e.datastore.GetLatestMetricValues(ctx, *alert.MetricName)
	if err != nil {
		// No data returned for this metric at all — the condition
		// no longer exists (e.g. all values filtered out), so clear
		e.clearResolvedAlert(ctx, alert, 0)
		return
	}

	// Find the metric value matching this alert's connection and database
	var found bool
	var value float64
	for _, mv := range values {
		if mv.ConnectionID != alert.ConnectionID {
			continue
		}
		// Match database name if the alert has one
		if alert.DatabaseName != nil {
			if mv.DatabaseName == nil || *mv.DatabaseName != *alert.DatabaseName {
				continue
			}
		}
		found = true
		value = mv.Value
		break
	}

	if !found {
		// Metric no longer reports for this connection/database — clear
		e.clearResolvedAlert(ctx, alert, 0)
		return
	}

	// Check if threshold is still violated
	stillViolated := e.checkThreshold(value, *alert.Operator, *alert.ThresholdValue)
	if !stillViolated {
		e.clearResolvedAlert(ctx, alert, value)
	}
}

// clearResolvedAlert clears an alert and queues a notification
func (e *Engine) clearResolvedAlert(ctx context.Context, alert *database.Alert, value float64) {
	e.log("Alert resolved: %s (%.2f no longer %s %.2f)", alert.Title, value, *alert.Operator, *alert.ThresholdValue)
	if err := e.datastore.ClearAlert(ctx, alert.ID); err != nil {
		e.log("ERROR: Failed to clear alert: %v", err)
		return
	}

	// Queue clear notification for async processing by worker pool
	e.queueNotification(alert, database.NotificationTypeAlertClear)
}

// cleanupOldData removes data older than retention period
func (e *Engine) cleanupOldData(ctx context.Context) {
	e.debugLog("Running retention cleanup...")

	// Get retention settings
	settings, err := e.datastore.GetAlerterSettings(ctx)
	if err != nil {
		e.log("ERROR: Failed to get settings: %v", err)
		return
	}

	retentionDays := settings.RetentionDays
	cutoff := time.Now().AddDate(0, 0, -retentionDays)

	// Delete old cleared/acknowledged alerts
	deleted, err := e.datastore.DeleteOldAlerts(ctx, cutoff)
	if err != nil {
		e.log("ERROR: Failed to delete old alerts: %v", err)
	} else if deleted > 0 {
		e.log("Deleted %d old alerts", deleted)
	}

	// Delete old anomaly candidates
	deleted, err = e.datastore.DeleteOldAnomalyCandidates(ctx, cutoff)
	if err != nil {
		e.log("ERROR: Failed to delete old anomaly candidates: %v", err)
	} else if deleted > 0 {
		e.log("Deleted %d old anomaly candidates", deleted)
	}
}
