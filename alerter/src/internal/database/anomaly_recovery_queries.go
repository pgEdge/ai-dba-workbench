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
)

// The statements behind anomaly alert recovery (GitHub issue #611). Each
// write is gated on status = 'active' so that an alert a user
// acknowledges, or clears, between the Tier 1 pass reading it and writing
// to it is left exactly as the user left it.
const (
	listActiveAnomalyAlertsSQL = `
        SELECT id, alert_type, rule_id, connection_id, database_name, object_name,
               probe_name, metric_name, metric_value, threshold_value, operator,
               severity, title, description, correlation_id, status, triggered_at,
               cleared_at, last_updated, anomaly_score, anomaly_details
        FROM alerts
        WHERE alert_type = 'anomaly' AND status = 'active'
          AND metric_name IS NOT NULL AND connection_id IS NOT NULL
        ORDER BY id
    `

	// refreshAnomalyAlertSQL mirrors UpdateAlertValues, including
	// discarding a cached AI analysis that was written for a different
	// value, but leaves the threshold columns alone because an anomaly
	// alert has none.
	refreshAnomalyAlertSQL = `
        UPDATE alerts
        SET metric_value = $2, anomaly_score = $3, severity = $4, last_updated = $5,
            ai_analysis = CASE WHEN metric_value IS DISTINCT FROM $2 THEN NULL ELSE ai_analysis END,
            ai_analysis_metric_value = CASE WHEN metric_value IS DISTINCT FROM $2 THEN NULL ELSE ai_analysis_metric_value END
        WHERE id = $1 AND alert_type = 'anomaly' AND status = 'active'
    `

	clearActiveAnomalyAlertSQL = `
        UPDATE alerts
        SET status = 'cleared', cleared_at = $2
        WHERE id = $1 AND alert_type = 'anomaly' AND status = 'active'
    `

	recentlyClearedAnomalyAlertSQL = `
        SELECT EXISTS(
            SELECT 1 FROM alerts
            WHERE alert_type = 'anomaly'
              AND metric_name = $1
              AND connection_id = $2
              AND status = 'cleared'
              AND cleared_at > NOW() - $3::interval
              AND (database_name = $4 OR ($4 IS NULL AND database_name IS NULL))
        )
    `
)

// ListActiveAnomalyAlerts returns every anomaly alert whose status is
// 'active'. Acknowledged alerts are deliberately excluded: the Tier 1
// recovery check leaves them to the user and to re-evaluation, as the
// threshold cleaner does.
func (d *Datastore) ListActiveAnomalyAlerts(ctx context.Context) ([]*Alert, error) {
	rows, err := d.pool.Query(ctx, listActiveAnomalyAlertsSQL)
	if err != nil {
		return nil, fmt.Errorf("failed to list active anomaly alerts: %w", err)
	}
	defer rows.Close()

	var alerts []*Alert
	for rows.Next() {
		var alert Alert
		if err := scanAlert(rows, &alert); err != nil {
			return nil, fmt.Errorf("failed to scan anomaly alert: %w", err)
		}
		alerts = append(alerts, &alert)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}
	return alerts, nil
}

// RefreshAnomalyAlert records the latest out-of-band Tier 1 evaluation on
// an active anomaly alert: its metric value, z-score and severity, and
// last_updated. It reports whether a row was updated, which is false when
// the alert is no longer active.
func (d *Datastore) RefreshAnomalyAlert(ctx context.Context, alertID int64,
	metricValue, anomalyScore float64, severity string) (bool, error) {
	tag, err := d.pool.Exec(ctx, refreshAnomalyAlertSQL,
		alertID, metricValue, anomalyScore, severity, time.Now())
	if err != nil {
		return false, fmt.Errorf("failed to refresh anomaly alert %d: %w", alertID, err)
	}
	return tag.RowsAffected() > 0, nil
}

// ClearActiveAnomalyAlert clears an anomaly alert only if it is still
// active, and reports whether it did. Unlike ClearAlert it never touches
// an alert a user has acknowledged in the meantime, so the caller queues
// a clear notification only for an alert this call actually cleared.
func (d *Datastore) ClearActiveAnomalyAlert(ctx context.Context, alertID int64) (bool, error) {
	tag, err := d.pool.Exec(ctx, clearActiveAnomalyAlertSQL, alertID, time.Now())
	if err != nil {
		return false, fmt.Errorf("failed to clear anomaly alert %d: %w", alertID, err)
	}
	return tag.RowsAffected() > 0, nil
}

// GetRecentlyClearedAnomalyAlert reports whether an anomaly alert for the
// same metric, connection and database was cleared within the cooldown.
// It is the anomaly counterpart of GetRecentlyClearedAlert, which keys on
// a rule id that anomaly alerts do not carry.
func (d *Datastore) GetRecentlyClearedAnomalyAlert(ctx context.Context, metricName string,
	connectionID int, dbName *string, cooldown time.Duration) (bool, error) {
	var exists bool
	err := d.pool.QueryRow(ctx, recentlyClearedAnomalyAlertSQL, metricName, connectionID,
		fmt.Sprintf("%d seconds", int(cooldown.Seconds())), dbName).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check recently cleared anomaly alert: %w", err)
	}
	return exists, nil
}
