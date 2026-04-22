/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// scanAlert scans all 21 fields from a query row into an Alert struct.
// This helper consolidates the duplicated scan pattern across alert queries.
func scanAlert(scanner interface{ Scan(dest ...any) error }, alert *Alert) error {
	return scanner.Scan(
		&alert.ID, &alert.AlertType, &alert.RuleID, &alert.ConnectionID,
		&alert.DatabaseName, &alert.ObjectName, &alert.ProbeName, &alert.MetricName,
		&alert.MetricValue, &alert.ThresholdValue, &alert.Operator, &alert.Severity,
		&alert.Title, &alert.Description, &alert.CorrelationID, &alert.Status,
		&alert.TriggeredAt, &alert.ClearedAt, &alert.LastUpdated, &alert.AnomalyScore,
		&alert.AnomalyDetails)
}

// GetActiveThresholdAlert checks if there's an existing active alert for a rule/connection
func (d *Datastore) GetActiveThresholdAlert(ctx context.Context, ruleID int64, connectionID int, dbName *string) (*Alert, error) {
	var alert Alert
	err := d.pool.QueryRow(ctx, `
		SELECT id, alert_type, rule_id, connection_id, database_name, object_name,
		       probe_name, metric_name, metric_value, threshold_value, operator,
		       severity, title, description, correlation_id, status, triggered_at,
		       cleared_at, last_updated, anomaly_score, anomaly_details
		FROM alerts
		WHERE rule_id = $1 AND connection_id = $2 AND status IN ('active', 'acknowledged')
		  AND (database_name = $3 OR ($3 IS NULL AND database_name IS NULL))
		LIMIT 1
	`, ruleID, connectionID, dbName).Scan(
		&alert.ID, &alert.AlertType, &alert.RuleID, &alert.ConnectionID,
		&alert.DatabaseName, &alert.ObjectName, &alert.ProbeName, &alert.MetricName,
		&alert.MetricValue, &alert.ThresholdValue, &alert.Operator, &alert.Severity,
		&alert.Title, &alert.Description, &alert.CorrelationID, &alert.Status,
		&alert.TriggeredAt, &alert.ClearedAt, &alert.LastUpdated, &alert.AnomalyScore,
		&alert.AnomalyDetails)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &alert, nil
}

// GetActiveAnomalyAlert checks for an existing active anomaly alert for the
// given metric name, connection, and optional database name.
func (d *Datastore) GetActiveAnomalyAlert(ctx context.Context, metricName string, connectionID int, dbName *string) (*Alert, error) {
	var alert Alert
	err := d.pool.QueryRow(ctx, `
        SELECT id, alert_type, rule_id, connection_id, database_name, object_name,
               probe_name, metric_name, metric_value, threshold_value, operator,
               severity, title, description, correlation_id, status, triggered_at,
               cleared_at, last_updated, anomaly_score, anomaly_details
        FROM alerts
        WHERE alert_type = 'anomaly' AND metric_name = $1 AND connection_id = $2
          AND status IN ('active', 'acknowledged')
          AND (database_name = $3 OR ($3 IS NULL AND database_name IS NULL))
        LIMIT 1
    `, metricName, connectionID, dbName).Scan(
		&alert.ID, &alert.AlertType, &alert.RuleID, &alert.ConnectionID,
		&alert.DatabaseName, &alert.ObjectName, &alert.ProbeName, &alert.MetricName,
		&alert.MetricValue, &alert.ThresholdValue, &alert.Operator, &alert.Severity,
		&alert.Title, &alert.Description, &alert.CorrelationID, &alert.Status,
		&alert.TriggeredAt, &alert.ClearedAt, &alert.LastUpdated, &alert.AnomalyScore,
		&alert.AnomalyDetails)

	if err != nil {
		return nil, err
	}
	return &alert, nil
}

// GetRecentlyClearedAlert checks if there's a recently cleared alert for the
// same rule, connection, and database within the cooldown period.
func (d *Datastore) GetRecentlyClearedAlert(ctx context.Context, ruleID int64, connectionID int, dbName *string, cooldown time.Duration) (bool, error) {
	var exists bool
	err := d.pool.QueryRow(ctx, `
        SELECT EXISTS(
            SELECT 1 FROM alerts
            WHERE rule_id = $1 AND connection_id = $2 AND status = 'cleared'
              AND cleared_at > NOW() - $3::interval
              AND (database_name = $4 OR ($4 IS NULL AND database_name IS NULL))
        )
    `, ruleID, connectionID, fmt.Sprintf("%d seconds", int(cooldown.Seconds())), dbName).Scan(&exists)

	if err != nil {
		return false, fmt.Errorf("failed to check recently cleared alert: %w", err)
	}
	return exists, nil
}

// GetReevaluationSuppressedAlert checks if a recently cleared anomaly alert
// for this metric+connection was cleared by the re-evaluation system (based on
// user feedback). These alerts are suppressed for longer to respect the user's
// assessment.
func (d *Datastore) GetReevaluationSuppressedAlert(ctx context.Context, metricName string, connectionID int, dbName *string, cooldown time.Duration) (bool, error) {
	var exists bool
	err := d.pool.QueryRow(ctx, `
        SELECT EXISTS(
            SELECT 1 FROM alerts
            WHERE alert_type = 'anomaly'
              AND metric_name = $1
              AND connection_id = $2
              AND status = 'cleared'
              AND reevaluation_count > 0
              AND cleared_at > NOW() - $3::interval
              AND (database_name = $4 OR ($4 IS NULL AND database_name IS NULL))
        )
    `, metricName, connectionID, fmt.Sprintf("%d seconds", int(cooldown.Seconds())), dbName).Scan(&exists)

	if err != nil {
		return false, fmt.Errorf("failed to check re-evaluation suppressed alert: %w", err)
	}
	return exists, nil
}

// GetFalsePositiveSuppressedAlert checks if there is an acknowledged anomaly
// alert for this metric+connection that was marked as a false positive within
// the suppression window. This prevents re-firing alerts that users have
// explicitly dismissed.
func (d *Datastore) GetFalsePositiveSuppressedAlert(ctx context.Context, metricName string, connectionID int, dbName *string, cooldown time.Duration) (bool, error) {
	var exists bool
	err := d.pool.QueryRow(ctx, `
        SELECT EXISTS(
            SELECT 1 FROM alerts a
            JOIN alert_acknowledgments ak ON ak.alert_id = a.id
            WHERE a.alert_type = 'anomaly'
              AND a.metric_name = $1
              AND a.connection_id = $2
              AND a.status = 'acknowledged'
              AND ak.false_positive = true
              AND ak.acknowledged_at > NOW() - $3::interval
              AND (a.database_name = $4 OR ($4 IS NULL AND a.database_name IS NULL))
        )
    `, metricName, connectionID, fmt.Sprintf("%d seconds", int(cooldown.Seconds())), dbName).Scan(&exists)

	if err != nil {
		return false, fmt.Errorf("failed to check false positive suppressed alert: %w", err)
	}
	return exists, nil
}

// UpdateAlertValues updates the metric_value, threshold, operator, severity,
// and last_updated timestamp for an active alert. AI analysis is cleared when
// the metric value changes.
func (d *Datastore) UpdateAlertValues(ctx context.Context, alertID int64, metricValue, thresholdValue float64, operator, severity string) error {
	_, err := d.pool.Exec(ctx, `
		UPDATE alerts
		SET metric_value = $2, last_updated = $3,
		    threshold_value = $4, operator = $5, severity = $6,
		    ai_analysis = CASE WHEN metric_value IS DISTINCT FROM $2 THEN NULL ELSE ai_analysis END,
		    ai_analysis_metric_value = CASE WHEN metric_value IS DISTINCT FROM $2 THEN NULL ELSE ai_analysis_metric_value END
		WHERE id = $1
	`, alertID, metricValue, time.Now(), thresholdValue, operator, severity)
	return err
}

// CreateAlert creates a new alert
func (d *Datastore) CreateAlert(ctx context.Context, alert *Alert) error {
	return d.pool.QueryRow(ctx, `
		INSERT INTO alerts (
			alert_type, rule_id, connection_id, database_name, object_name,
			probe_name, metric_name, metric_value, threshold_value, operator,
			severity, title, description, correlation_id, status, triggered_at,
			anomaly_score, anomaly_details
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		RETURNING id
	`, alert.AlertType, alert.RuleID, alert.ConnectionID, alert.DatabaseName,
		alert.ObjectName, alert.ProbeName, alert.MetricName, alert.MetricValue,
		alert.ThresholdValue, alert.Operator, alert.Severity, alert.Title,
		alert.Description, alert.CorrelationID, alert.Status, alert.TriggeredAt,
		alert.AnomalyScore, alert.AnomalyDetails).Scan(&alert.ID)
}

// GetActiveAlerts retrieves all active alerts
func (d *Datastore) GetActiveAlerts(ctx context.Context) ([]*Alert, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, alert_type, rule_id, connection_id, database_name, object_name,
		       probe_name, metric_name, metric_value, threshold_value, operator,
		       severity, title, description, correlation_id, status, triggered_at,
		       cleared_at, last_updated, anomaly_score, anomaly_details
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
		if err := scanAlert(rows, &alert); err != nil {
			return nil, fmt.Errorf("failed to scan alert: %w", err)
		}
		alerts = append(alerts, &alert)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return alerts, nil
}

// GetAlert retrieves a single alert by ID
func (d *Datastore) GetAlert(ctx context.Context, alertID int64) (*Alert, error) {
	var alert Alert
	row := d.pool.QueryRow(ctx, `
		SELECT id, alert_type, rule_id, connection_id, database_name, object_name,
		       probe_name, metric_name, metric_value, threshold_value, operator,
		       severity, title, description, correlation_id, status, triggered_at,
		       cleared_at, last_updated, anomaly_score, anomaly_details
		FROM alerts
		WHERE id = $1
	`, alertID)

	if err := scanAlert(row, &alert); err != nil {
		return nil, err
	}
	return &alert, nil
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

// ReactivateAlert sets an acknowledged alert back to active status and
// clears any acknowledgment rows so the UI no longer shows the alert as
// acknowledged. This is used when the severity of a threshold alert
// changes while the alert is acknowledged, so the user sees the severity
// change. The update is gated on status = 'acknowledged' to avoid
// clobbering ack rows for alerts that are not currently acknowledged;
// the delete only runs when the update actually reactivated the alert.
// Both statements execute in a single transaction so the alert cannot be
// left in a half-reactivated state (active status with a stale ack row,
// or vice versa).
func (d *Datastore) ReactivateAlert(ctx context.Context, alertID int64) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	//nolint:errcheck // Rollback is a no-op if the tx was already committed.
	defer tx.Rollback(ctx)

	result, err := tx.Exec(ctx, `
		UPDATE alerts
		SET status = 'active', last_updated = $1
		WHERE id = $2 AND status = 'acknowledged'
	`, time.Now(), alertID)
	if err != nil {
		return fmt.Errorf("failed to update alert status: %w", err)
	}

	// Only clear acknowledgment rows when the UPDATE actually flipped an
	// alert back to active. A no-op UPDATE (alert not found, already
	// active, or cleared) must not delete ack history for unrelated
	// state.
	if result.RowsAffected() == 0 {
		return tx.Commit(ctx)
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM alert_acknowledgments WHERE alert_id = $1
	`, alertID); err != nil {
		return fmt.Errorf("failed to clear alert acknowledgments: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

// GetAlertsByCluster retrieves all active or acknowledged alerts across all
// connections in the same cluster as the given connection, excluding alerts
// from the given connection itself. Returns an empty slice if the connection
// has no cluster.
func (d *Datastore) GetAlertsByCluster(ctx context.Context, connectionID int) ([]*Alert, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, alert_type, rule_id, connection_id, database_name, object_name,
		       probe_name, metric_name, metric_value, threshold_value, operator,
		       severity, title, description, correlation_id, status, triggered_at,
		       cleared_at, last_updated, anomaly_score, anomaly_details
		FROM alerts
		WHERE connection_id IN (
			SELECT id FROM connections
			WHERE cluster_id = (SELECT cluster_id FROM connections WHERE id = $1)
			  AND cluster_id IS NOT NULL
		)
		  AND connection_id != $1
		  AND status IN ('active', 'acknowledged')
		ORDER BY triggered_at DESC
	`, connectionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get alerts by cluster: %w", err)
	}
	defer rows.Close()

	var alerts []*Alert
	for rows.Next() {
		var alert Alert
		if err := scanAlert(rows, &alert); err != nil {
			return nil, fmt.Errorf("failed to scan alert: %w", err)
		}
		alerts = append(alerts, &alert)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return alerts, nil
}

// GetAlertsByConnection retrieves all active or acknowledged alerts for a
// given connection, ordered by triggered time descending.
func (d *Datastore) GetAlertsByConnection(ctx context.Context, connectionID int) ([]*Alert, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, alert_type, rule_id, connection_id, database_name, object_name,
		       probe_name, metric_name, metric_value, threshold_value, operator,
		       severity, title, description, correlation_id, status, triggered_at,
		       cleared_at, last_updated, anomaly_score, anomaly_details
		FROM alerts
		WHERE connection_id = $1 AND status IN ('active', 'acknowledged')
		ORDER BY triggered_at DESC
	`, connectionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get alerts by connection: %w", err)
	}
	defer rows.Close()

	var alerts []*Alert
	for rows.Next() {
		var alert Alert
		if err := scanAlert(rows, &alert); err != nil {
			return nil, fmt.Errorf("failed to scan alert: %w", err)
		}
		alerts = append(alerts, &alert)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return alerts, nil
}

// UpdateAlertReevaluation increments the re-evaluation count and updates the
// last re-evaluated timestamp for an alert.
func (d *Datastore) UpdateAlertReevaluation(ctx context.Context, alertID int64) error {
	_, err := d.pool.Exec(ctx, `
		UPDATE alerts
		SET reevaluation_count = reevaluation_count + 1,
		    last_reevaluated_at = NOW()
		WHERE id = $1
	`, alertID)
	return err
}
