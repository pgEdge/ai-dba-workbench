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

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// evaluateThresholds checks all threshold rules against current metrics
func (e *Engine) evaluateThresholds(ctx context.Context) {
	e.debugLog("Evaluating threshold rules...")

	// Get all enabled rules
	rules, err := e.datastore.GetEnabledAlertRules(ctx)
	if err != nil {
		e.log("ERROR: Failed to get alert rules: %v", err)
		return
	}

	e.debugLog("Found %d enabled rules", len(rules))

	for _, rule := range rules {
		if ctx.Err() != nil {
			return
		}
		e.evaluateRuleForAllConnections(ctx, rule)
	}

	// Evaluate metric staleness after standard threshold rules
	e.evaluateMetricStaleness(ctx)
}

// evaluateRuleForAllConnections evaluates a rule across all connections with data
func (e *Engine) evaluateRuleForAllConnections(ctx context.Context, rule *database.AlertRule) {
	// Get all metric values for this rule's metric
	values, err := e.datastore.GetLatestMetricValues(ctx, rule.MetricName)
	if err != nil {
		e.debugLog("No data for metric %s: %v", rule.MetricName, err)
		return
	}

	for _, mv := range values {
		if ctx.Err() != nil {
			return
		}

		// Check if there's a blackout active for this connection
		connID := mv.ConnectionID
		active, err := e.datastore.IsBlackoutActive(ctx, &connID, mv.DatabaseName)
		if err != nil {
			e.debugLog("Error checking blackout for connection %d: %v", connID, err)
		}
		if active {
			e.debugLog("Skipping rule %s for connection %d: blackout active", rule.Name, connID)
			continue
		}

		// Get the effective threshold (global or per-connection override)
		threshold, operator, severity, enabled := e.datastore.GetEffectiveThreshold(
			ctx, rule.ID, mv.ConnectionID, mv.DatabaseName)
		if !enabled {
			e.debugLog("Rule %s disabled for connection %d", rule.Name, mv.ConnectionID)
			continue
		}

		// Check if threshold is violated
		violated := e.checkThreshold(mv.Value, operator, threshold)

		if violated {
			e.triggerThresholdAlert(ctx, rule, mv.Value, threshold, operator,
				severity, mv.ConnectionID, mv.DatabaseName, mv.ObjectName)
		}
	}
}

// checkThreshold checks if a value violates a threshold
func (e *Engine) checkThreshold(value float64, operator string, threshold float64) bool {
	switch operator {
	case ">":
		return value > threshold
	case ">=":
		return value >= threshold
	case "<":
		return value < threshold
	case "<=":
		return value <= threshold
	case "==":
		return value == threshold
	case "!=":
		return value != threshold
	default:
		return false
	}
}

// triggerThresholdAlert creates or updates an alert for a threshold violation
func (e *Engine) triggerThresholdAlert(ctx context.Context, rule *database.AlertRule, value, threshold float64, operator, severity string, connectionID int, dbName *string, objectName *string) {
	e.log("Threshold violated: %s (%.2f %s %.2f) on connection %d", rule.Name, value, operator, threshold, connectionID)

	// Check if there's already an active alert for this rule/connection
	existing, err := e.datastore.GetActiveThresholdAlert(ctx, rule.ID, connectionID, dbName)
	if err == nil && existing != nil {
		// Alert already exists - update metric_value, threshold, operator, severity, and last_updated timestamp
		if err := e.datastore.UpdateAlertValues(ctx, existing.ID, value, threshold, operator, severity); err != nil {
			e.log("ERROR: Failed to update alert values: %v", err)
		} else {
			e.debugLog("Updated metric value for active alert %s: %.2f -> %.2f", rule.Name, *existing.MetricValue, value)
		}
		return
	}

	// Create new alert
	alert := &database.Alert{
		AlertType:      "threshold",
		RuleID:         &rule.ID,
		ConnectionID:   connectionID,
		DatabaseName:   dbName,
		ObjectName:     objectName,
		MetricName:     &rule.MetricName,
		MetricValue:    &value,
		ThresholdValue: &threshold,
		Operator:       &operator,
		Severity:       severity,
		Title:          rule.Name,
		Description:    rule.Description,
		Status:         "active",
		TriggeredAt:    time.Now(),
	}

	if err := e.datastore.CreateAlert(ctx, alert); err != nil {
		e.log("ERROR: Failed to create alert: %v", err)
		return
	}

	// Queue alert notification for async processing by worker pool
	e.queueNotification(alert, database.NotificationTypeAlertFire)
}

// evaluateMetricStaleness checks for probes whose data exceeds the configured
// staleness threshold (expressed as a multiple of the collection interval).
func (e *Engine) evaluateMetricStaleness(ctx context.Context) {
	e.debugLog("Evaluating metric staleness...")

	entries, err := e.datastore.GetProbeStalenessByConnection(ctx)
	if err != nil {
		e.log("ERROR: Failed to get probe staleness: %v", err)
		return
	}

	rule, err := e.datastore.GetAlertRuleByName(ctx, "metric_staleness")
	if err != nil {
		e.debugLog("No metric_staleness rule found: %v", err)
		return
	}
	if !rule.DefaultEnabled {
		e.debugLog("metric_staleness rule is disabled")
		return
	}

	for _, entry := range entries {
		if ctx.Err() != nil {
			return
		}

		// Check if there's a blackout active for this connection
		connID := entry.ConnectionID
		active, err := e.datastore.IsBlackoutActive(ctx, &connID, nil)
		if err != nil {
			e.debugLog("Error checking blackout for connection %d: %v", connID, err)
		}
		if active {
			e.debugLog("Skipping staleness check for connection %d: blackout active", connID)
			continue
		}

		threshold, operator, severity, enabled := e.datastore.GetEffectiveThreshold(
			ctx, rule.ID, entry.ConnectionID, nil)
		if !enabled {
			e.debugLog("metric_staleness disabled for connection %d", entry.ConnectionID)
			continue
		}

		violated := e.checkThreshold(entry.StalenessRatio, operator, threshold)
		if violated {
			elapsedMinutes := float64(entry.CollectionInterval) * entry.StalenessRatio / 60.0
			thresholdMinutes := float64(entry.CollectionInterval) * threshold / 60.0

			title := fmt.Sprintf("Stale metrics: %s on %s", entry.ProbeName, entry.ConnectionName)
			description := fmt.Sprintf(
				"The %s probe on %s has not collected data for %.0f minutes (threshold: %.0f minutes). Dashboards may show outdated data.",
				entry.ProbeName, entry.ConnectionName, elapsedMinutes, thresholdMinutes)

			metricName := rule.MetricName
			probeName := entry.ProbeName
			alert := &database.Alert{
				AlertType:      "threshold",
				RuleID:         &rule.ID,
				ConnectionID:   entry.ConnectionID,
				ProbeName:      &probeName,
				MetricName:     &metricName,
				MetricValue:    &entry.StalenessRatio,
				ThresholdValue: &threshold,
				Operator:       &operator,
				Severity:       severity,
				Title:          title,
				Description:    description,
				Status:         "active",
				TriggeredAt:    time.Now(),
			}

			// Check if there's already an active alert for this rule/connection
			existing, err := e.datastore.GetActiveThresholdAlert(ctx, rule.ID, entry.ConnectionID, nil)
			if err == nil && existing != nil {
				if err := e.datastore.UpdateAlertValues(ctx, existing.ID, entry.StalenessRatio, threshold, operator, severity); err != nil {
					e.log("ERROR: Failed to update staleness alert values: %v", err)
				} else {
					e.debugLog("Updated staleness alert for %s on connection %d", entry.ProbeName, entry.ConnectionID)
				}
				continue
			}

			if err := e.datastore.CreateAlert(ctx, alert); err != nil {
				e.log("ERROR: Failed to create staleness alert: %v", err)
				continue
			}

			e.log("Staleness alert created: %s", title)
			e.queueNotification(alert, database.NotificationTypeAlertFire)
		}
	}
}

// evaluateConnectionErrors checks monitored connections for error states
func (e *Engine) evaluateConnectionErrors(ctx context.Context) {
	e.debugLog("Evaluating connection errors...")

	connections, err := e.datastore.GetMonitoredConnectionErrors(ctx)
	if err != nil {
		e.log("ERROR: Failed to get monitored connection errors: %v", err)
		return
	}

	for _, conn := range connections {
		if ctx.Err() != nil {
			return
		}

		if conn.ConnectionError != nil {
			// Connection has an error - create or update alert
			alertID, desc, found, err := e.datastore.GetActiveConnectionAlert(ctx, conn.ConnectionID)
			if err != nil {
				e.log("ERROR: Failed to check active connection alert for connection %d: %v", conn.ConnectionID, err)
				continue
			}

			if !found {
				alert, err := e.datastore.CreateConnectionAlert(ctx, conn.ConnectionID, conn.Name, *conn.ConnectionError)
				if err != nil {
					e.log("ERROR: Failed to create connection alert for connection %d: %v", conn.ConnectionID, err)
					continue
				}
				e.log("Connection error alert created for %s: %s", conn.Name, *conn.ConnectionError)

				// Queue notification for async processing
				e.queueNotification(alert, database.NotificationTypeAlertFire)
			} else if desc != *conn.ConnectionError {
				if err := e.datastore.UpdateConnectionAlertDescription(ctx, alertID, *conn.ConnectionError); err != nil {
					e.log("ERROR: Failed to update connection alert description for connection %d: %v", conn.ConnectionID, err)
				} else {
					e.debugLog("Updated connection error description for %s", conn.Name)
				}
			}
		} else {
			// No error - clear any active connection alert
			alertID, _, found, err := e.datastore.GetActiveConnectionAlert(ctx, conn.ConnectionID)
			if err != nil {
				e.log("ERROR: Failed to check active connection alert for connection %d: %v", conn.ConnectionID, err)
				continue
			}

			if found {
				if err := e.datastore.ClearAlert(ctx, alertID); err != nil {
					e.log("ERROR: Failed to clear connection alert for connection %d: %v", conn.ConnectionID, err)
					continue
				}
				e.log("Connection error alert cleared for %s", conn.Name)

				// Queue clear notification
				alert, err := e.datastore.GetAlert(ctx, alertID)
				if err == nil {
					e.queueNotification(alert, database.NotificationTypeAlertClear)
				}
			}
		}
	}
}
