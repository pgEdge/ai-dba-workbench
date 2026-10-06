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
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// AlertTypeSystem is the alert_type of an alert about the alerter itself,
// such as a failing LLM provider, rather than about a monitored server.
// A system alert has a NULL connection_id in the datastore, which Alert
// represents as a ConnectionID of 0; connection ids are serial and start
// at 1, so 0 never names a real connection. See GitHub issue #582.
const AlertTypeSystem = "system"

// IsSystem reports whether the alert concerns the alerter itself rather
// than a monitored connection.
func (a *Alert) IsSystem() bool {
	return a.AlertType == AlertTypeSystem
}

// nullableConnectionID maps Alert's 0-for-none convention onto the NULL
// the datastore stores for a system alert.
func nullableConnectionID(id int) *int {
	if id == 0 {
		return nil
	}
	return &id
}

// connectionIDFromNullable is the inverse of nullableConnectionID.
func connectionIDFromNullable(id *int) int {
	if id == nil {
		return 0
	}
	return *id
}

// openSystemAlertColumns is the column list scanAlert expects.
const openSystemAlertColumns = `
	id, alert_type, rule_id, connection_id, database_name, object_name,
	probe_name, metric_name, metric_value, threshold_value, operator,
	severity, title, description, correlation_id, status, triggered_at,
	cleared_at, last_updated, anomaly_score, anomaly_details`

// getOpenSystemAlertSQL selects the open system alert for one key.
const getOpenSystemAlertSQL = `SELECT ` + openSystemAlertColumns + `
	FROM alerts
	WHERE alert_type = 'system' AND metric_name = $1
	  AND status IN ('active', 'acknowledged')
	ORDER BY triggered_at DESC
	LIMIT 1`

// listOpenSystemAlertsSQL selects every open system alert whose key
// starts with a prefix. The prefix is matched with starts_with rather
// than LIKE so that no character in it acts as a wildcard.
const listOpenSystemAlertsSQL = `SELECT ` + openSystemAlertColumns + `
	FROM alerts
	WHERE alert_type = 'system' AND starts_with(metric_name, $1)
	  AND status IN ('active', 'acknowledged')
	ORDER BY triggered_at ASC`

// insertSystemAlertSQL inserts a system alert unless one is already open
// for the key. The conflict target names the partial unique index
// idx_alerts_system_open, so two alerter processes racing to raise the
// same alert leave exactly one row.
const insertSystemAlertSQL = `
	INSERT INTO alerts (
		alert_type, connection_id, object_name, metric_name, severity,
		title, description, status, triggered_at, anomaly_details
	) VALUES ('system', NULL, $1, $2, $3, $4, $5, 'active', $6, $7)
	ON CONFLICT (metric_name)
		WHERE alert_type = 'system' AND status <> 'cleared'
		DO NOTHING
	RETURNING id`

// updateSystemAlertSQL refreshes the text of an open system alert. A
// cleared alert is left alone, so a caller holding a stale ID learns
// that the alert has gone rather than rewriting history.
const updateSystemAlertSQL = `
	UPDATE alerts
	SET description = $1, anomaly_details = $2, last_updated = NOW()
	WHERE id = $3 AND alert_type = 'system' AND status <> 'cleared'`

// ErrSystemAlertNotOpen reports that UpdateSystemAlert found no open
// system alert with the given ID, typically because another alerter
// process cleared it.
var ErrSystemAlertNotOpen = errors.New("system alert is not open")

// GetOpenSystemAlert returns the active or acknowledged system alert for
// key, which is stored in metric_name, or nil when there is none.
func (d *Datastore) GetOpenSystemAlert(ctx context.Context, key string) (*Alert, error) {
	var alert Alert
	if err := scanAlert(d.pool.QueryRow(ctx, getOpenSystemAlertSQL, key), &alert); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get open system alert: %w", err)
	}
	return &alert, nil
}

// GetOpenSystemAlerts returns every active or acknowledged system alert
// whose key starts with keyPrefix, oldest first.
func (d *Datastore) GetOpenSystemAlerts(ctx context.Context, keyPrefix string) ([]*Alert, error) {
	rows, err := d.pool.Query(ctx, listOpenSystemAlertsSQL, keyPrefix)
	if err != nil {
		return nil, fmt.Errorf("failed to list open system alerts: %w", err)
	}
	defer rows.Close()

	var alerts []*Alert
	for rows.Next() {
		var alert Alert
		if err := scanAlert(rows, &alert); err != nil {
			return nil, fmt.Errorf("failed to scan system alert: %w", err)
		}
		alerts = append(alerts, &alert)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}
	return alerts, nil
}

// CreateSystemAlert raises a system alert keyed by alert.MetricName and
// reports whether it created a new row. When an alert is already open for
// the key it returns that alert instead, with created false, so a caller
// never raises a duplicate. The alert type, connection and status are
// forced to those of a new system alert whatever the caller set.
func (d *Datastore) CreateSystemAlert(ctx context.Context, alert *Alert) (*Alert, bool, error) {
	if alert.MetricName == nil || *alert.MetricName == "" {
		return nil, false, errors.New("a system alert needs a key in MetricName")
	}
	if alert.TriggeredAt.IsZero() {
		alert.TriggeredAt = time.Now()
	}
	alert.AlertType = AlertTypeSystem
	alert.ConnectionID = 0
	alert.Status = "active"

	err := d.pool.QueryRow(ctx, insertSystemAlertSQL,
		alert.ObjectName, alert.MetricName, alert.Severity, alert.Title,
		alert.Description, alert.TriggeredAt, alert.AnomalyDetails).Scan(&alert.ID)
	if err == nil {
		return alert, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, fmt.Errorf("failed to create system alert: %w", err)
	}

	// The insert did nothing because an alert is already open for the
	// key; hand that one back.
	existing, err := d.GetOpenSystemAlert(ctx, *alert.MetricName)
	if err != nil {
		return nil, false, err
	}
	if existing == nil {
		return nil, false, errors.New("system alert conflicted but no open alert was found")
	}
	return existing, false, nil
}

// UpdateSystemAlert rewrites the description and details of an open
// system alert and stamps last_updated. It never touches an alert of
// another type or a cleared one, and returns ErrSystemAlertNotOpen when
// no open system alert has the ID.
func (d *Datastore) UpdateSystemAlert(ctx context.Context, alertID int64, description string, details *string) error {
	tag, err := d.pool.Exec(ctx, updateSystemAlertSQL, description, details, alertID)
	if err != nil {
		return fmt.Errorf("failed to update system alert: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrSystemAlertNotOpen
	}
	return nil
}
