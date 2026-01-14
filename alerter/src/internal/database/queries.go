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

// MetricValue represents a metric value for a specific connection
type MetricValue struct {
	ConnectionID int
	DatabaseName *string
	Value        float64
	CollectedAt  time.Time
}

// GetLatestMetricValues retrieves the most recent values for a metric across all connections
// This queries the collected data tables to find current metric values
func (d *Datastore) GetLatestMetricValues(ctx context.Context, metricName string) ([]MetricValue, error) {
	var results []MetricValue

	// Parse metric name to determine table and column/aggregation
	// Format: table_name.column_name or computed_metric_name
	switch metricName {
	case "pg_stat_activity.count":
		// Count active connections per connection_id
		rows, err := d.pool.Query(ctx, `
			SELECT connection_id, COUNT(*) as value, MAX(collected_at) as collected_at
			FROM metrics.pg_stat_activity
			WHERE collected_at > NOW() - INTERVAL '5 minutes'
			GROUP BY connection_id
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to query %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var mv MetricValue
			if err := rows.Scan(&mv.ConnectionID, &mv.Value, &mv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan metric value: %w", err)
			}
			results = append(results, mv)
		}

	case "connection_utilization_percent":
		// Calculate connection utilization as percentage of max_connections
		rows, err := d.pool.Query(ctx, `
			WITH active_counts AS (
				SELECT connection_id, COUNT(*) as active
				FROM metrics.pg_stat_activity
				WHERE collected_at > NOW() - INTERVAL '5 minutes'
				GROUP BY connection_id
			),
			max_conns AS (
				SELECT connection_id, setting::float as max_connections
				FROM metrics.pg_settings
				WHERE name = 'max_connections'
				  AND collected_at > NOW() - INTERVAL '1 hour'
			)
			SELECT a.connection_id,
			       (a.active / NULLIF(m.max_connections, 0)) * 100 as value,
			       NOW() as collected_at
			FROM active_counts a
			JOIN max_conns m ON a.connection_id = m.connection_id
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to query %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var mv MetricValue
			if err := rows.Scan(&mv.ConnectionID, &mv.Value, &mv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan metric value: %w", err)
			}
			results = append(results, mv)
		}

	case "pg_stat_replication.replay_lag_seconds":
		// Get replication lag in seconds
		rows, err := d.pool.Query(ctx, `
			SELECT connection_id,
			       EXTRACT(EPOCH FROM (NOW() - replay_lsn_timestamp))::float as value,
			       collected_at
			FROM metrics.pg_stat_replication
			WHERE collected_at > NOW() - INTERVAL '5 minutes'
			  AND replay_lsn_timestamp IS NOT NULL
		`)
		if err != nil {
			return nil, fmt.Errorf("failed to query %s: %w", metricName, err)
		}
		defer rows.Close()

		for rows.Next() {
			var mv MetricValue
			if err := rows.Scan(&mv.ConnectionID, &mv.Value, &mv.CollectedAt); err != nil {
				return nil, fmt.Errorf("failed to scan metric value: %w", err)
			}
			results = append(results, mv)
		}

	default:
		return nil, fmt.Errorf("metric %s not implemented", metricName)
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("no data found for metric %s", metricName)
	}

	return results, nil
}

// GetLatestMetricValue retrieves the most recent value for a metric (single value)
// This is a convenience wrapper that returns the first value found
func (d *Datastore) GetLatestMetricValue(ctx context.Context, metricName string) (value float64, connectionID int, dbName *string, err error) {
	values, err := d.GetLatestMetricValues(ctx, metricName)
	if err != nil {
		return 0, 0, nil, err
	}
	if len(values) == 0 {
		return 0, 0, nil, fmt.Errorf("no data found for metric %s", metricName)
	}
	return values[0].Value, values[0].ConnectionID, values[0].DatabaseName, nil
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

// GetEnabledBlackoutSchedules retrieves all enabled blackout schedules
func (d *Datastore) GetEnabledBlackoutSchedules(ctx context.Context) ([]*BlackoutSchedule, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, connection_id, database_name, name, cron_expression,
		       duration_minutes, timezone, reason, enabled, created_by,
		       created_at, updated_at
		FROM blackout_schedules
		WHERE enabled = true
		ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to get blackout schedules: %w", err)
	}
	defer rows.Close()

	var schedules []*BlackoutSchedule
	for rows.Next() {
		var s BlackoutSchedule
		err := rows.Scan(&s.ID, &s.ConnectionID, &s.DatabaseName, &s.Name,
			&s.CronExpression, &s.DurationMinutes, &s.Timezone, &s.Reason,
			&s.Enabled, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan blackout schedule: %w", err)
		}
		schedules = append(schedules, &s)
	}

	return schedules, nil
}

// CreateBlackout creates a new blackout entry
func (d *Datastore) CreateBlackout(ctx context.Context, blackout *Blackout) error {
	return d.pool.QueryRow(ctx, `
		INSERT INTO blackouts (connection_id, database_name, reason, start_time,
		                       end_time, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`, blackout.ConnectionID, blackout.DatabaseName, blackout.Reason,
		blackout.StartTime, blackout.EndTime, blackout.CreatedBy,
		blackout.CreatedAt).Scan(&blackout.ID)
}

// GetMetricBaselines retrieves baselines for a metric on a connection
func (d *Datastore) GetMetricBaselines(ctx context.Context, connectionID int, metricName string) ([]*MetricBaseline, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, connection_id, database_name, metric_name, period_type,
		       day_of_week, hour_of_day, mean, stddev, min, max,
		       sample_count, last_calculated
		FROM metric_baselines
		WHERE connection_id = $1 AND metric_name = $2
		ORDER BY period_type, day_of_week, hour_of_day
	`, connectionID, metricName)
	if err != nil {
		return nil, fmt.Errorf("failed to get metric baselines: %w", err)
	}
	defer rows.Close()

	var baselines []*MetricBaseline
	for rows.Next() {
		var b MetricBaseline
		err := rows.Scan(&b.ID, &b.ConnectionID, &b.DatabaseName, &b.MetricName,
			&b.PeriodType, &b.DayOfWeek, &b.HourOfDay, &b.Mean, &b.StdDev,
			&b.Min, &b.Max, &b.SampleCount, &b.LastCalculated)
		if err != nil {
			return nil, fmt.Errorf("failed to scan metric baseline: %w", err)
		}
		baselines = append(baselines, &b)
	}

	return baselines, nil
}

// UpsertMetricBaseline inserts or updates a metric baseline
func (d *Datastore) UpsertMetricBaseline(ctx context.Context, b *MetricBaseline) error {
	_, err := d.pool.Exec(ctx, `
		INSERT INTO metric_baselines (connection_id, database_name, metric_name,
		                              period_type, day_of_week, hour_of_day,
		                              mean, stddev, min, max, sample_count,
		                              last_calculated)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (connection_id, COALESCE(database_name, ''), metric_name,
		             period_type, COALESCE(day_of_week, -1), COALESCE(hour_of_day, -1))
		DO UPDATE SET mean = $7, stddev = $8, min = $9, max = $10,
		              sample_count = $11, last_calculated = $12
	`, b.ConnectionID, b.DatabaseName, b.MetricName, b.PeriodType,
		b.DayOfWeek, b.HourOfDay, b.Mean, b.StdDev, b.Min, b.Max,
		b.SampleCount, b.LastCalculated)
	return err
}

// GetActiveConnections retrieves all active monitored connections
func (d *Datastore) GetActiveConnections(ctx context.Context) ([]int, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id FROM connections WHERE enabled = true ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to get active connections: %w", err)
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan connection id: %w", err)
		}
		ids = append(ids, id)
	}

	return ids, nil
}

// CreateAnomalyCandidate creates a new anomaly candidate for tier 2/3 processing
func (d *Datastore) CreateAnomalyCandidate(ctx context.Context, c *AnomalyCandidate) error {
	return d.pool.QueryRow(ctx, `
		INSERT INTO anomaly_candidates (connection_id, database_name, metric_name,
		                                metric_value, z_score, detected_at, context,
		                                tier1_pass)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id
	`, c.ConnectionID, c.DatabaseName, c.MetricName, c.MetricValue,
		c.ZScore, c.DetectedAt, c.Context, c.Tier1Pass).Scan(&c.ID)
}

// GetUnprocessedAnomalyCandidates retrieves candidates that need tier 2/3 processing
func (d *Datastore) GetUnprocessedAnomalyCandidates(ctx context.Context, limit int) ([]*AnomalyCandidate, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, connection_id, database_name, metric_name, metric_value,
		       z_score, detected_at, context, tier1_pass, tier2_score, tier2_pass,
		       tier3_result, tier3_pass, tier3_error, final_decision, alert_id,
		       processed_at
		FROM anomaly_candidates
		WHERE processed_at IS NULL AND tier1_pass = true
		ORDER BY detected_at ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get unprocessed candidates: %w", err)
	}
	defer rows.Close()

	var candidates []*AnomalyCandidate
	for rows.Next() {
		var c AnomalyCandidate
		err := rows.Scan(&c.ID, &c.ConnectionID, &c.DatabaseName, &c.MetricName,
			&c.MetricValue, &c.ZScore, &c.DetectedAt, &c.Context, &c.Tier1Pass,
			&c.Tier2Score, &c.Tier2Pass, &c.Tier3Result, &c.Tier3Pass,
			&c.Tier3Error, &c.FinalDecision, &c.AlertID, &c.ProcessedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan anomaly candidate: %w", err)
		}
		candidates = append(candidates, &c)
	}

	return candidates, nil
}

// UpdateAnomalyCandidate updates a candidate with tier 2/3 results
func (d *Datastore) UpdateAnomalyCandidate(ctx context.Context, c *AnomalyCandidate) error {
	_, err := d.pool.Exec(ctx, `
		UPDATE anomaly_candidates
		SET tier2_score = $2, tier2_pass = $3, tier3_result = $4, tier3_pass = $5,
		    tier3_error = $6, final_decision = $7, alert_id = $8, processed_at = $9
		WHERE id = $1
	`, c.ID, c.Tier2Score, c.Tier2Pass, c.Tier3Result, c.Tier3Pass,
		c.Tier3Error, c.FinalDecision, c.AlertID, c.ProcessedAt)
	return err
}
