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
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pgedge/ai-workbench/pkg/logger"
)

// ErrAlertNotFound is returned when an alert is not found
var ErrAlertNotFound = errors.New("alert not found")

// Alert represents an alert from the alerter
type Alert struct {
	ID             int64      `json:"id"`
	AlertType      string     `json:"alert_type"`
	RuleID         *int64     `json:"rule_id,omitempty"`
	ConnectionID   int        `json:"connection_id"`
	DatabaseName   *string    `json:"database_name,omitempty"`
	ObjectName     *string    `json:"object_name,omitempty"`
	ProbeName      *string    `json:"probe_name,omitempty"`
	MetricName     *string    `json:"metric_name,omitempty"`
	MetricValue    *float64   `json:"metric_value,omitempty"`
	MetricUnit     *string    `json:"metric_unit,omitempty"`
	ThresholdValue *float64   `json:"threshold_value,omitempty"`
	Operator       *string    `json:"operator,omitempty"`
	Severity       string     `json:"severity"`
	Title          string     `json:"title"`
	Description    string     `json:"description"`
	CorrelationID  *string    `json:"correlation_id,omitempty"`
	Status         string     `json:"status"`
	TriggeredAt    time.Time  `json:"triggered_at"`
	ClearedAt      *time.Time `json:"cleared_at,omitempty"`
	LastUpdated    *time.Time `json:"last_updated,omitempty"`
	AnomalyScore   *float64   `json:"anomaly_score,omitempty"`
	AnomalyDetails *string    `json:"anomaly_details,omitempty"`
	ServerName     string     `json:"server_name,omitempty"`
	// Acknowledgment fields (from alert_acknowledgments table)
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
	AcknowledgedBy *string    `json:"acknowledged_by,omitempty"`
	AckMessage     *string    `json:"ack_message,omitempty"`
	FalsePositive  *bool      `json:"false_positive,omitempty"`
	// AI analysis cache fields
	AIAnalysis            *string  `json:"ai_analysis,omitempty"`
	AIAnalysisMetricValue *float64 `json:"ai_analysis_metric_value,omitempty"`
}

// AlertListFilter holds filter options for listing alerts
type AlertListFilter struct {
	ConnectionID   *int
	ConnectionIDs  []int
	DatabaseName   *string
	Status         *string
	Severity       *string
	AlertType      *string
	StartTime      *time.Time
	EndTime        *time.Time
	ExcludeCleared bool // If true, only return alerts where cleared_at IS NULL
	Limit          int
	Offset         int
}

// AlertListResult holds the result of listing alerts
type AlertListResult struct {
	Alerts []Alert `json:"alerts"`
	Total  int64   `json:"total"`
}

// AlertCountsResult contains alert counts grouped by server
type AlertCountsResult struct {
	Total    int64         `json:"total"`
	ByServer map[int]int64 `json:"by_server"`
}

// AcknowledgeAlertRequest contains the data for acknowledging an alert
type AcknowledgeAlertRequest struct {
	AlertID        int64  `json:"alert_id"`
	AcknowledgedBy string `json:"acknowledged_by"`
	Message        string `json:"message"`
	FalsePositive  bool   `json:"false_positive"`
}

// GetAlerts retrieves alerts with optional filtering
func (d *Datastore) GetAlerts(ctx context.Context, filter AlertListFilter) (*AlertListResult, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Build the WHERE clause
	conditions := []string{}
	args := []any{}
	argNum := 1

	if filter.ConnectionID != nil {
		conditions = append(conditions, fmt.Sprintf("a.connection_id = $%d", argNum))
		args = append(args, *filter.ConnectionID)
		argNum++
	}

	if len(filter.ConnectionIDs) > 0 {
		placeholders := make([]string, len(filter.ConnectionIDs))
		for i, id := range filter.ConnectionIDs {
			placeholders[i] = fmt.Sprintf("$%d", argNum)
			args = append(args, id)
			argNum++
		}
		conditions = append(conditions, fmt.Sprintf("a.connection_id IN (%s)", strings.Join(placeholders, ", ")))
	}

	if filter.Status != nil {
		conditions = append(conditions, fmt.Sprintf("a.status = $%d", argNum))
		args = append(args, *filter.Status)
		argNum++
	}

	if filter.Severity != nil {
		conditions = append(conditions, fmt.Sprintf("a.severity = $%d", argNum))
		args = append(args, *filter.Severity)
		argNum++
	}

	if filter.AlertType != nil {
		conditions = append(conditions, fmt.Sprintf("a.alert_type = $%d", argNum))
		args = append(args, *filter.AlertType)
		argNum++
	}

	if filter.StartTime != nil {
		conditions = append(conditions, fmt.Sprintf("a.triggered_at >= $%d", argNum))
		args = append(args, *filter.StartTime)
		argNum++
	}

	if filter.EndTime != nil {
		conditions = append(conditions, fmt.Sprintf("a.triggered_at <= $%d", argNum))
		args = append(args, *filter.EndTime)
		argNum++
	}

	if filter.ExcludeCleared {
		conditions = append(conditions, "a.cleared_at IS NULL")
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Count total matching alerts
	countQuery := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM alerts a
		%s
	`, whereClause)

	var total int64
	err := d.pool.QueryRow(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, fmt.Errorf("failed to count alerts: %w", err)
	}

	// Apply limit and offset
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	// Query alerts with connection name, metric unit, and acknowledgment info
	// Uses DISTINCT ON to get only the most recent acknowledgment per alert
	query := fmt.Sprintf(`
		SELECT a.id, a.alert_type, a.rule_id, a.connection_id, a.database_name,
		       a.object_name, a.probe_name, a.metric_name, a.metric_value, r.metric_unit,
		       a.threshold_value, a.operator, a.severity, a.title, a.description,
		       a.correlation_id, a.status, a.triggered_at, a.cleared_at,
		       a.last_updated, a.anomaly_score, a.anomaly_details,
		       COALESCE(c.name, 'Unknown') as server_name,
		       ack.acknowledged_at, ack.acknowledged_by, ack.message, ack.false_positive,
		       a.ai_analysis, a.ai_analysis_metric_value
		FROM alerts a
		LEFT JOIN connections c ON a.connection_id = c.id
		LEFT JOIN alert_rules r ON a.rule_id = r.id
		LEFT JOIN LATERAL (
			SELECT acknowledged_at, acknowledged_by, message, false_positive
			FROM alert_acknowledgments
			WHERE alert_id = a.id
			ORDER BY acknowledged_at DESC
			LIMIT 1
		) ack ON true
		%s
		ORDER BY a.triggered_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argNum, argNum+1)

	args = append(args, limit, offset)

	rows, err := d.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query alerts: %w", err)
	}
	defer rows.Close()

	var alerts []Alert
	for rows.Next() {
		var alert Alert
		err := rows.Scan(
			&alert.ID, &alert.AlertType, &alert.RuleID, &alert.ConnectionID,
			&alert.DatabaseName, &alert.ObjectName, &alert.ProbeName, &alert.MetricName,
			&alert.MetricValue, &alert.MetricUnit, &alert.ThresholdValue, &alert.Operator,
			&alert.Severity, &alert.Title, &alert.Description,
			&alert.CorrelationID, &alert.Status, &alert.TriggeredAt,
			&alert.ClearedAt, &alert.LastUpdated,
			&alert.AnomalyScore, &alert.AnomalyDetails,
			&alert.ServerName,
			&alert.AcknowledgedAt, &alert.AcknowledgedBy, &alert.AckMessage,
			&alert.FalsePositive,
			&alert.AIAnalysis, &alert.AIAnalysisMetricValue,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan alert: %w", err)
		}
		alerts = append(alerts, alert)
	}

	if alerts == nil {
		alerts = []Alert{}
	}

	return &AlertListResult{
		Alerts: alerts,
		Total:  total,
	}, nil
}

// GetAlertCounts returns counts of active alerts grouped by connection_id.
// When connectionIDs is non-nil, the query is restricted to alerts whose
// connection_id appears in the slice. A nil slice means "no filter"
// (superuser or wildcard-scoped caller); an empty non-nil slice returns
// an empty result without touching the database.
func (d *Datastore) GetAlertCounts(ctx context.Context, connectionIDs []int) (*AlertCountsResult, error) {
	// An explicit empty allow-list means the caller can see no
	// connections; avoid the database round-trip entirely.
	if connectionIDs != nil && len(connectionIDs) == 0 {
		return &AlertCountsResult{
			Total:    0,
			ByServer: make(map[int]int64),
		}, nil
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	var (
		total    int64
		rows     pgx.Rows
		queryErr error
	)

	if connectionIDs == nil {
		queryErr = d.pool.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM alerts
			WHERE status = 'active'
		`).Scan(&total)
		if queryErr != nil {
			return nil, fmt.Errorf("failed to count total alerts: %w", queryErr)
		}

		rows, queryErr = d.pool.Query(ctx, `
			SELECT connection_id, COUNT(*) as count
			FROM alerts
			WHERE status = 'active'
			GROUP BY connection_id
		`)
	} else {
		queryErr = d.pool.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM alerts
			WHERE status = 'active'
			  AND connection_id = ANY($1)
		`, connectionIDs).Scan(&total)
		if queryErr != nil {
			return nil, fmt.Errorf("failed to count total alerts: %w", queryErr)
		}

		rows, queryErr = d.pool.Query(ctx, `
			SELECT connection_id, COUNT(*) as count
			FROM alerts
			WHERE status = 'active'
			  AND connection_id = ANY($1)
			GROUP BY connection_id
		`, connectionIDs)
	}
	if queryErr != nil {
		return nil, fmt.Errorf("failed to query alert counts: %w", queryErr)
	}
	defer rows.Close()

	byServer := make(map[int]int64)
	for rows.Next() {
		var connID int
		var count int64
		if err := rows.Scan(&connID, &count); err != nil {
			return nil, fmt.Errorf("failed to scan alert count: %w", err)
		}
		byServer[connID] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate alert counts: %w", err)
	}

	return &AlertCountsResult{
		Total:    total,
		ByServer: byServer,
	}, nil
}

// GetAlertConnectionID returns the connection_id for the given alert.
func (d *Datastore) GetAlertConnectionID(ctx context.Context, alertID int64) (int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var connectionID int
	err := d.pool.QueryRow(ctx,
		"SELECT connection_id FROM alerts WHERE id = $1", alertID).Scan(&connectionID)
	if err != nil {
		return 0, fmt.Errorf("failed to get alert connection ID: %w", err)
	}
	return connectionID, nil
}

// SaveAlertAnalysis saves an AI analysis result for an alert
func (d *Datastore) SaveAlertAnalysis(ctx context.Context, alertID int64, analysis string, metricValue float64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.pool.Exec(ctx, `
		UPDATE alerts
		SET ai_analysis = $2, ai_analysis_metric_value = $3
		WHERE id = $1
	`, alertID, analysis, metricValue)
	return err
}

// AcknowledgeAlert acknowledges an alert, updating its status and creating
// an acknowledgment record
func (d *Datastore) AcknowledgeAlert(ctx context.Context, req AcknowledgeAlertRequest) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	logger.Infof("AcknowledgeAlert: starting for alert_id=%d, by=%s, false_positive=%v",
		req.AlertID, req.AcknowledgedBy, req.FalsePositive)

	// Start a transaction
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		logger.Errorf("AcknowledgeAlert: failed to begin transaction: %v", err)
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	//nolint:errcheck // Rollback is no-op if already committed
	defer tx.Rollback(ctx)

	// Update alert status to acknowledged
	result, err := tx.Exec(ctx, `
		UPDATE alerts
		SET status = 'acknowledged'
		WHERE id = $1 AND status = 'active'
	`, req.AlertID)
	if err != nil {
		logger.Errorf("AcknowledgeAlert: failed to update alert status: %v", err)
		return fmt.Errorf("failed to update alert status: %w", err)
	}

	rowsAffected := result.RowsAffected()
	logger.Infof("AcknowledgeAlert: UPDATE affected %d rows", rowsAffected)

	if rowsAffected == 0 {
		logger.Infof("AcknowledgeAlert: alert %d not found or already acknowledged", req.AlertID)
		return fmt.Errorf("alert not found or already acknowledged")
	}

	// Create acknowledgment record
	_, err = tx.Exec(ctx, `
		INSERT INTO alert_acknowledgments (alert_id, acknowledged_by, message, acknowledge_type, false_positive)
		VALUES ($1, $2, $3, 'acknowledge', $4)
	`, req.AlertID, req.AcknowledgedBy, req.Message, req.FalsePositive)
	if err != nil {
		logger.Errorf("AcknowledgeAlert: failed to create acknowledgment record: %v", err)
		return fmt.Errorf("failed to create acknowledgment record: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		logger.Errorf("AcknowledgeAlert: failed to commit transaction: %v", err)
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	logger.Infof("AcknowledgeAlert: successfully acknowledged alert %d", req.AlertID)
	return nil
}

// UnacknowledgeAlert removes acknowledgment from an alert, returning it
// to active status and deleting any alert_acknowledgments rows so the
// server's alert listing query no longer surfaces the alert as
// acknowledged. Both statements run in a single transaction so the alert
// cannot end up with status = 'active' while a stale acknowledgment row
// still exists (or the reverse).
func (d *Datastore) UnacknowledgeAlert(ctx context.Context, alertID int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	//nolint:errcheck // Rollback is a no-op if the tx was already committed.
	defer tx.Rollback(ctx)

	// Update alert status back to active.
	result, err := tx.Exec(ctx, `
		UPDATE alerts
		SET status = 'active'
		WHERE id = $1 AND status = 'acknowledged'
	`, alertID)
	if err != nil {
		return fmt.Errorf("failed to update alert status: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("alert not found or not acknowledged")
	}

	// Clear acknowledgment rows so the LATERAL join in GetAlerts no
	// longer returns a stale acknowledged_at for this alert.
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
