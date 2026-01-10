/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package database

import (
	"context"
	"fmt"
	"time"
)

// GetAlerterSettings retrieves the global alerter settings
func (d *Datastore) GetAlerterSettings(ctx context.Context) (*AlerterSettings, error) {
	var settings AlerterSettings
	err := d.pool.QueryRow(ctx, `
		SELECT id, retention_days, default_anomaly_enabled, default_anomaly_sensitivity,
		       baseline_refresh_interval_mins, correlation_window_seconds, updated_at
		FROM alerter_settings
		WHERE id = 1
	`).Scan(&settings.ID, &settings.RetentionDays, &settings.DefaultAnomalyEnabled,
		&settings.DefaultAnomalySensitivity, &settings.BaselineRefreshIntervalMins,
		&settings.CorrelationWindowSeconds, &settings.UpdatedAt)

	if err != nil {
		return nil, fmt.Errorf("failed to get alerter settings: %w", err)
	}
	return &settings, nil
}

// GetEnabledAlertRules retrieves all enabled alert rules
func (d *Datastore) GetEnabledAlertRules(ctx context.Context) ([]*AlertRule, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, name, description, category, metric_name, default_operator,
		       default_threshold, default_severity, default_enabled, required_extension,
		       is_built_in, created_at
		FROM alert_rules
		WHERE default_enabled = true
		ORDER BY category, name
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to get alert rules: %w", err)
	}
	defer rows.Close()

	var rules []*AlertRule
	for rows.Next() {
		var rule AlertRule
		err := rows.Scan(&rule.ID, &rule.Name, &rule.Description, &rule.Category,
			&rule.MetricName, &rule.DefaultOperator, &rule.DefaultThreshold,
			&rule.DefaultSeverity, &rule.DefaultEnabled, &rule.RequiredExtension,
			&rule.IsBuiltIn, &rule.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan alert rule: %w", err)
		}
		rules = append(rules, &rule)
	}

	return rules, nil
}

// GetEffectiveThreshold returns the threshold settings for a rule/connection
// Returns the per-connection override if it exists, otherwise global defaults
func (d *Datastore) GetEffectiveThreshold(ctx context.Context, ruleID int64, connectionID int, dbName *string) (threshold float64, operator string, severity string, enabled bool) {
	// First try per-connection override
	var found bool
	err := d.pool.QueryRow(ctx, `
		SELECT threshold, operator, severity, enabled
		FROM alert_thresholds
		WHERE rule_id = $1 AND connection_id = $2
		  AND (database_name = $3 OR ($3 IS NULL AND database_name IS NULL))
	`, ruleID, connectionID, dbName).Scan(&threshold, &operator, &severity, &enabled)

	if err == nil {
		found = true
	}

	if found {
		return threshold, operator, severity, enabled
	}

	// Fall back to rule defaults
	err = d.pool.QueryRow(ctx, `
		SELECT default_threshold, default_operator, default_severity, default_enabled
		FROM alert_rules
		WHERE id = $1
	`, ruleID).Scan(&threshold, &operator, &severity, &enabled)

	if err != nil {
		// Return disabled if rule not found
		return 0, "", "", false
	}

	return threshold, operator, severity, enabled
}

// GetLatestMetricValue retrieves the most recent value for a metric
// This queries the collected data tables to find current metric values
func (d *Datastore) GetLatestMetricValue(ctx context.Context, metricName string) (value float64, connectionID int, dbName *string, err error) {
	// The actual query depends on which table/probe the metric comes from
	// This is a simplified implementation - in practice, this would need to
	// query different tables based on the metric name

	// For now, return an error indicating this needs to be implemented
	// once we know the exact table structure for collected metrics
	err = fmt.Errorf("metric %s not found or collection not yet implemented", metricName)
	return
}

// GetActiveThresholdAlert checks if there's an existing active alert for a rule/connection
func (d *Datastore) GetActiveThresholdAlert(ctx context.Context, ruleID int64, connectionID int, dbName *string) (*Alert, error) {
	var alert Alert
	err := d.pool.QueryRow(ctx, `
		SELECT id, alert_type, rule_id, connection_id, database_name, probe_name,
		       metric_name, metric_value, threshold_value, operator, severity,
		       title, description, correlation_id, status, triggered_at, cleared_at,
		       anomaly_score, anomaly_details
		FROM alerts
		WHERE rule_id = $1 AND connection_id = $2 AND status = 'active'
		  AND (database_name = $3 OR ($3 IS NULL AND database_name IS NULL))
		LIMIT 1
	`, ruleID, connectionID, dbName).Scan(
		&alert.ID, &alert.AlertType, &alert.RuleID, &alert.ConnectionID,
		&alert.DatabaseName, &alert.ProbeName, &alert.MetricName, &alert.MetricValue,
		&alert.ThresholdValue, &alert.Operator, &alert.Severity, &alert.Title,
		&alert.Description, &alert.CorrelationID, &alert.Status, &alert.TriggeredAt,
		&alert.ClearedAt, &alert.AnomalyScore, &alert.AnomalyDetails)

	if err != nil {
		return nil, err
	}
	return &alert, nil
}

// CreateAlert creates a new alert
func (d *Datastore) CreateAlert(ctx context.Context, alert *Alert) error {
	return d.pool.QueryRow(ctx, `
		INSERT INTO alerts (
			alert_type, rule_id, connection_id, database_name, probe_name,
			metric_name, metric_value, threshold_value, operator, severity,
			title, description, correlation_id, status, triggered_at,
			anomaly_score, anomaly_details
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		RETURNING id
	`, alert.AlertType, alert.RuleID, alert.ConnectionID, alert.DatabaseName,
		alert.ProbeName, alert.MetricName, alert.MetricValue, alert.ThresholdValue,
		alert.Operator, alert.Severity, alert.Title, alert.Description,
		alert.CorrelationID, alert.Status, alert.TriggeredAt,
		alert.AnomalyScore, alert.AnomalyDetails).Scan(&alert.ID)
}

// GetActiveAlerts retrieves all active alerts
func (d *Datastore) GetActiveAlerts(ctx context.Context) ([]*Alert, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, alert_type, rule_id, connection_id, database_name, probe_name,
		       metric_name, metric_value, threshold_value, operator, severity,
		       title, description, correlation_id, status, triggered_at, cleared_at,
		       anomaly_score, anomaly_details
		FROM alerts
		WHERE status = 'active'
		ORDER BY triggered_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to get active alerts: %w", err)
	}
	defer rows.Close()

	var alerts []*Alert
	for rows.Next() {
		var alert Alert
		err := rows.Scan(
			&alert.ID, &alert.AlertType, &alert.RuleID, &alert.ConnectionID,
			&alert.DatabaseName, &alert.ProbeName, &alert.MetricName, &alert.MetricValue,
			&alert.ThresholdValue, &alert.Operator, &alert.Severity, &alert.Title,
			&alert.Description, &alert.CorrelationID, &alert.Status, &alert.TriggeredAt,
			&alert.ClearedAt, &alert.AnomalyScore, &alert.AnomalyDetails)
		if err != nil {
			return nil, fmt.Errorf("failed to scan alert: %w", err)
		}
		alerts = append(alerts, &alert)
	}

	return alerts, nil
}

// ClearAlert marks an alert as cleared
func (d *Datastore) ClearAlert(ctx context.Context, alertID int64) error {
	_, err := d.pool.Exec(ctx, `
		UPDATE alerts
		SET status = 'cleared', cleared_at = $1
		WHERE id = $2
	`, time.Now(), alertID)
	return err
}

// IsBlackoutActive checks if any blackout is currently active
func (d *Datastore) IsBlackoutActive(ctx context.Context, connectionID *int, dbName *string) (bool, error) {
	now := time.Now()
	var count int

	// Check manual blackouts
	err := d.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM blackouts
		WHERE start_time <= $1 AND end_time >= $1
		  AND (connection_id IS NULL OR connection_id = $2)
		  AND (database_name IS NULL OR database_name = $3)
	`, now, connectionID, dbName).Scan(&count)

	if err != nil {
		return false, fmt.Errorf("failed to check blackouts: %w", err)
	}

	return count > 0, nil
}

// DeleteOldAlerts deletes cleared alerts older than the cutoff date
func (d *Datastore) DeleteOldAlerts(ctx context.Context, cutoff time.Time) (int64, error) {
	result, err := d.pool.Exec(ctx, `
		DELETE FROM alerts
		WHERE status IN ('cleared', 'acknowledged')
		  AND cleared_at < $1
	`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// DeleteOldAnomalyCandidates deletes processed candidates older than the cutoff
func (d *Datastore) DeleteOldAnomalyCandidates(ctx context.Context, cutoff time.Time) (int64, error) {
	result, err := d.pool.Exec(ctx, `
		DELETE FROM anomaly_candidates
		WHERE processed_at IS NOT NULL AND processed_at < $1
	`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// GetProbeAvailability checks if a probe is available for a connection
func (d *Datastore) GetProbeAvailability(ctx context.Context, connectionID int, probeName string) (*ProbeAvailability, error) {
	var pa ProbeAvailability
	err := d.pool.QueryRow(ctx, `
		SELECT id, connection_id, database_name, probe_name, extension_name,
		       is_available, last_checked, last_collected, unavailable_reason
		FROM probe_availability
		WHERE connection_id = $1 AND probe_name = $2
		LIMIT 1
	`, connectionID, probeName).Scan(
		&pa.ID, &pa.ConnectionID, &pa.DatabaseName, &pa.ProbeName,
		&pa.ExtensionName, &pa.IsAvailable, &pa.LastChecked,
		&pa.LastCollected, &pa.UnavailableReason)

	if err != nil {
		return nil, err
	}
	return &pa, nil
}
