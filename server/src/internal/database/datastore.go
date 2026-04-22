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
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/pkg/logger"
)

// Sentinel errors for datastore operations
var (
	// ErrConnectionNotFound is returned when a connection is not found
	ErrConnectionNotFound = errors.New("connection not found")

	// ErrAlertNotFound is returned when an alert is not found
	ErrAlertNotFound = errors.New("alert not found")
)

// EstateServerSummary holds summary information for a single server
// in the estate snapshot
type EstateServerSummary struct {
	ID               int    `json:"id"`
	Name             string `json:"name"`
	Status           string `json:"status"`
	Role             string `json:"role"`
	ActiveAlertCount int    `json:"active_alert_count"`
}

// EstateAlertSummary holds a summary of a single active alert
type EstateAlertSummary struct {
	Title      string `json:"title"`
	ServerName string `json:"server_name"`
	Severity   string `json:"severity"`
}

// EstateBlackoutSummary holds a summary of a blackout period
type EstateBlackoutSummary struct {
	Scope     string    `json:"scope"`
	Reason    string    `json:"reason"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
}

// EstateEventSummary holds a summary of a recent timeline event
type EstateEventSummary struct {
	EventType  string    `json:"event_type"`
	ServerName string    `json:"server_name"`
	OccurredAt time.Time `json:"occurred_at"`
	Severity   string    `json:"severity"`
	Title      string    `json:"title"`
	Summary    string    `json:"summary"`
}

// EstateSnapshot contains a point-in-time summary of the entire
// monitored estate for the AI overview
type EstateSnapshot struct {
	Timestamp         time.Time               `json:"timestamp"`
	ServerTotal       int                     `json:"server_total"`
	ServerOnline      int                     `json:"server_online"`
	ServerOffline     int                     `json:"server_offline"`
	ServerWarning     int                     `json:"server_warning"`
	Servers           []EstateServerSummary   `json:"servers"`
	AlertTotal        int                     `json:"alert_total"`
	AlertCritical     int                     `json:"alert_critical"`
	AlertWarning      int                     `json:"alert_warning"`
	AlertInfo         int                     `json:"alert_info"`
	TopAlerts         []EstateAlertSummary    `json:"top_alerts"`
	ActiveBlackouts   []EstateBlackoutSummary `json:"active_blackouts"`
	UpcomingBlackouts []EstateBlackoutSummary `json:"upcoming_blackouts"`
	RecentEvents      []EstateEventSummary    `json:"recent_events"`
}

// Datastore manages the connection to the collector's datastore database
type Datastore struct {
	pool         *pgxpool.Pool
	serverSecret string
	mu           sync.RWMutex
}

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

// AlertCountsResult contains alert counts grouped by server
type AlertCountsResult struct {
	Total    int64         `json:"total"`
	ByServer map[int]int64 `json:"by_server"`
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

// AcknowledgeAlertRequest contains the data for acknowledging an alert
type AcknowledgeAlertRequest struct {
	AlertID        int64  `json:"alert_id"`
	AcknowledgedBy string `json:"acknowledged_by"`
	Message        string `json:"message"`
	FalsePositive  bool   `json:"false_positive"`
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

// GetEstateSnapshot gathers all data needed for an AI overview of the
// estate. It returns a point-in-time snapshot of server status, alerts,
// blackouts, and recent events. If individual sub-queries fail, partial
// data is returned with what succeeded.
func (d *Datastore) GetEstateSnapshot(ctx context.Context) (*EstateSnapshot, error) {
	snapshotCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	snapshot := &EstateSnapshot{
		Timestamp:         time.Now().UTC(),
		Servers:           []EstateServerSummary{},
		TopAlerts:         []EstateAlertSummary{},
		ActiveBlackouts:   []EstateBlackoutSummary{},
		UpcomingBlackouts: []EstateBlackoutSummary{},
		RecentEvents:      []EstateEventSummary{},
	}

	// Gather server topology and compute status counts
	d.gatherEstateServerData(snapshotCtx, snapshot)

	// Gather alert summary and top alerts
	d.gatherEstateAlertData(snapshotCtx, snapshot)

	// Gather active and upcoming blackout periods
	d.gatherEstateBlackoutData(snapshotCtx, snapshot)

	// Gather recent events from the last 24 hours
	d.gatherEstateRecentEvents(snapshotCtx, snapshot)

	return snapshot, nil
}

// gatherEstateServerData populates server status counts and per-server
// details by walking the cluster topology hierarchy. Servers are
// classified as offline (status is offline), warning (has active alerts
// but not offline), or online (no alerts, not offline).
func (d *Datastore) gatherEstateServerData(ctx context.Context, snapshot *EstateSnapshot) {
	// Sync cluster_id assignments before reading topology
	if err := d.RefreshClusterAssignments(ctx); err != nil {
		logger.Infof("gatherEstateServerData: failed to refresh cluster assignments: %v", err)
	}

	groups, err := d.GetClusterTopology(ctx, nil)
	if err != nil {
		logger.Errorf("GetEstateSnapshot: failed to get cluster topology: %v", err)
		return
	}

	var servers []EstateServerSummary
	for _, group := range groups {
		for _, cluster := range group.Clusters {
			flattenTopologyServers(cluster.Servers, &servers)
		}
	}

	// Map roles to display-friendly names and compute status counts
	for i := range servers {
		servers[i].Role = d.mapPrimaryRoleToDisplayRole(servers[i].Role)

		switch {
		case servers[i].Status == "offline":
			snapshot.ServerOffline++
		case servers[i].ActiveAlertCount > 0:
			snapshot.ServerWarning++
		default:
			snapshot.ServerOnline++
		}
	}

	snapshot.Servers = servers
	snapshot.ServerTotal = len(servers)
}

// flattenTopologyServers recursively extracts server summaries from the
// nested topology hierarchy into a flat slice. Children (such as hot
// standbys) are included alongside their parents.
func flattenTopologyServers(servers []TopologyServerInfo, result *[]EstateServerSummary) {
	for i := range servers {
		s := &servers[i]
		*result = append(*result, EstateServerSummary{
			ID:               s.ID,
			Name:             s.Name,
			Status:           s.Status,
			Role:             s.PrimaryRole,
			ActiveAlertCount: s.ActiveAlertCount,
		})
		if len(s.Children) > 0 {
			flattenTopologyServers(s.Children, result)
		}
	}
}

// gatherEstateAlertData populates alert severity counts and the top
// active alerts list. Active alerts are retrieved in order of most
// recent trigger time. Severity counts are computed from the returned
// set; if total active alerts exceed the query limit of 500, the
// breakdown is approximate while AlertTotal remains accurate.
func (d *Datastore) gatherEstateAlertData(ctx context.Context, snapshot *EstateSnapshot) {
	activeStatus := "active"
	result, err := d.GetAlerts(ctx, AlertListFilter{
		Status: &activeStatus,
		Limit:  500,
	})
	if err != nil {
		logger.Errorf("GetEstateSnapshot: failed to get alerts: %v", err)
		return
	}

	snapshot.AlertTotal = int(result.Total)

	for i := range result.Alerts {
		alert := &result.Alerts[i]
		switch alert.Severity {
		case "critical":
			snapshot.AlertCritical++
		case "warning":
			snapshot.AlertWarning++
		case "info":
			snapshot.AlertInfo++
		}

		if i < 10 {
			snapshot.TopAlerts = append(snapshot.TopAlerts, EstateAlertSummary{
				Title:      alert.Title,
				ServerName: alert.ServerName,
				Severity:   alert.Severity,
			})
		}
	}
}

// gatherEstateBlackoutData populates active blackouts and upcoming
// blackouts. Upcoming blackouts are one-time blackouts whose start
// time falls within the next 24 hours. Scheduled blackout occurrences
// are not evaluated because that would require a cron expression
// parser.
func (d *Datastore) gatherEstateBlackoutData(ctx context.Context, snapshot *EstateSnapshot) {
	// Retrieve recent blackouts ordered by start_time DESC; future
	// blackouts appear first, followed by currently active ones
	result, err := d.ListBlackouts(ctx, BlackoutFilter{
		Limit: 100,
	})
	if err != nil {
		logger.Errorf("GetEstateSnapshot: failed to get blackouts: %v", err)
		return
	}

	now := time.Now().UTC()
	cutoff := now.Add(24 * time.Hour)

	for i := range result.Blackouts {
		b := &result.Blackouts[i]
		summary := EstateBlackoutSummary{
			Scope:     b.Scope,
			Reason:    b.Reason,
			StartTime: b.StartTime,
			EndTime:   b.EndTime,
		}

		if b.IsActive {
			snapshot.ActiveBlackouts = append(snapshot.ActiveBlackouts, summary)
		} else if b.StartTime.After(now) && b.StartTime.Before(cutoff) {
			snapshot.UpcomingBlackouts = append(snapshot.UpcomingBlackouts, summary)
		}
	}
}

// gatherEstateRecentEvents populates recent events from the last 24
// hours, focusing on restarts, configuration changes, and blackout
// transitions. Events are returned in reverse chronological order
// with a maximum of 20 entries.
func (d *Datastore) gatherEstateRecentEvents(ctx context.Context, snapshot *EstateSnapshot) {
	now := time.Now().UTC()
	dayAgo := now.Add(-24 * time.Hour)

	result, err := d.GetTimelineEvents(ctx, TimelineFilter{
		StartTime: dayAgo,
		EndTime:   now,
		EventTypes: []string{
			EventTypeRestart,
			EventTypeConfigChange,
			EventTypeBlackoutStarted,
			EventTypeBlackoutEnded,
		},
		Limit: 20,
	})
	if err != nil {
		logger.Errorf("GetEstateSnapshot: failed to get timeline events: %v", err)
		return
	}

	for i := range result.Events {
		event := &result.Events[i]
		snapshot.RecentEvents = append(snapshot.RecentEvents, EstateEventSummary{
			EventType:  event.EventType,
			ServerName: event.ServerName,
			OccurredAt: event.OccurredAt,
			Severity:   event.Severity,
			Title:      event.Title,
			Summary:    event.Summary,
		})
	}
}

// GetServerSnapshot returns an estate snapshot filtered to a single
// server (connection). It returns the same EstateSnapshot structure but
// populated only with data for the given connection ID.
func (d *Datastore) GetServerSnapshot(ctx context.Context, serverID int) (*EstateSnapshot, string, error) {
	// Verify the connection exists and get its name.
	conn, err := d.GetConnection(ctx, serverID)
	if err != nil {
		return nil, "", fmt.Errorf("server not found: %w", err)
	}

	snapshot := d.buildScopedSnapshot(ctx, []int{serverID})
	return snapshot, conn.Name, nil
}

// GetClusterSnapshot returns an estate snapshot filtered to all servers
// in a given cluster. It returns the snapshot, the cluster name, and
// any error encountered.
func (d *Datastore) GetClusterSnapshot(ctx context.Context, clusterID int) (*EstateSnapshot, string, error) {
	cluster, err := d.GetCluster(ctx, clusterID)
	if err != nil {
		return nil, "", fmt.Errorf("cluster not found: %w", err)
	}

	connectionIDs, err := d.getConnectionIDsForCluster(ctx, clusterID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get connections for cluster: %w", err)
	}

	snapshot := d.buildScopedSnapshot(ctx, connectionIDs)
	return snapshot, cluster.Name, nil
}

// GetGroupSnapshot returns an estate snapshot filtered to all servers
// in all clusters belonging to a given group. It returns the snapshot,
// the group name, and any error encountered.
func (d *Datastore) GetGroupSnapshot(ctx context.Context, groupID int) (*EstateSnapshot, string, error) {
	group, err := d.GetClusterGroup(ctx, groupID)
	if err != nil {
		return nil, "", fmt.Errorf("group not found: %w", err)
	}

	connectionIDs, err := d.getConnectionIDsForGroup(ctx, groupID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get connections for group: %w", err)
	}

	snapshot := d.buildScopedSnapshot(ctx, connectionIDs)
	return snapshot, group.Name, nil
}

// GetConnectionsSnapshot returns an estate snapshot filtered to the
// specified connection IDs. It is a public wrapper around
// buildScopedSnapshot for callers that already have a list of
// connection IDs and do not need scope-name resolution.
func (d *Datastore) GetConnectionsSnapshot(ctx context.Context, connectionIDs []int) *EstateSnapshot {
	return d.buildScopedSnapshot(ctx, connectionIDs)
}

// buildScopedSnapshot creates an EstateSnapshot containing only data
// for the specified connection IDs. It reuses the same gathering
// helpers as the estate-wide snapshot but filters by connection.
func (d *Datastore) buildScopedSnapshot(ctx context.Context, connectionIDs []int) *EstateSnapshot {
	snapshotCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	snapshot := &EstateSnapshot{
		Timestamp:         time.Now().UTC(),
		Servers:           []EstateServerSummary{},
		TopAlerts:         []EstateAlertSummary{},
		ActiveBlackouts:   []EstateBlackoutSummary{},
		UpcomingBlackouts: []EstateBlackoutSummary{},
		RecentEvents:      []EstateEventSummary{},
	}

	if len(connectionIDs) == 0 {
		return snapshot
	}

	// Gather server data filtered to the given connections.
	d.gatherScopedServerData(snapshotCtx, snapshot, connectionIDs)

	// Gather alert data filtered to the given connections.
	d.gatherScopedAlertData(snapshotCtx, snapshot, connectionIDs)

	// Gather blackout data (estate-wide; filtered post-query is not
	// possible because blackouts reference scopes, not connections
	// directly). Include all blackouts so the LLM can note relevant
	// maintenance windows.
	d.gatherEstateBlackoutData(snapshotCtx, snapshot)

	// Gather recent events filtered to the given connections.
	d.gatherScopedRecentEvents(snapshotCtx, snapshot, connectionIDs)

	return snapshot
}

// getConnectionIDsForCluster returns all connection IDs that belong
// to the given cluster.
func (d *Datastore) getConnectionIDsForCluster(ctx context.Context, clusterID int) ([]int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `SELECT id FROM connections WHERE cluster_id = $1 ORDER BY id`
	rows, err := d.pool.Query(ctx, query, clusterID)
	if err != nil {
		return nil, fmt.Errorf("failed to query connections for cluster: %w", err)
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
	return ids, rows.Err()
}

// getConnectionIDsForGroup returns all connection IDs that belong to
// any cluster in the given group.
func (d *Datastore) getConnectionIDsForGroup(ctx context.Context, groupID int) ([]int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
        SELECT c.id
        FROM connections c
        JOIN clusters cl ON c.cluster_id = cl.id
        WHERE cl.group_id = $1
        ORDER BY c.id
    `
	rows, err := d.pool.Query(ctx, query, groupID)
	if err != nil {
		return nil, fmt.Errorf("failed to query connections for group: %w", err)
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
	return ids, rows.Err()
}

// GetConnectionIDsForCluster returns all connection IDs that belong to
// the given cluster. It is an exported wrapper around the internal
// helper so other packages (notably the API handlers that apply RBAC
// visibility filtering) can enumerate a cluster's members without
// duplicating the SQL.
func (d *Datastore) GetConnectionIDsForCluster(ctx context.Context, clusterID int) ([]int, error) {
	return d.getConnectionIDsForCluster(ctx, clusterID)
}

// GetConnectionIDsForGroup returns all connection IDs that belong to
// any cluster in the given group. Exported companion to
// GetConnectionIDsForCluster for group-scoped RBAC checks.
func (d *Datastore) GetConnectionIDsForGroup(ctx context.Context, groupID int) ([]int, error) {
	return d.getConnectionIDsForGroup(ctx, groupID)
}

// gatherScopedServerData populates server status counts for a specific
// set of connection IDs by walking the full topology and filtering.
func (d *Datastore) gatherScopedServerData(ctx context.Context, snapshot *EstateSnapshot, connectionIDs []int) {
	idSet := make(map[int]bool, len(connectionIDs))
	for _, id := range connectionIDs {
		idSet[id] = true
	}

	// Sync cluster_id assignments before reading topology
	if err := d.RefreshClusterAssignments(ctx); err != nil {
		logger.Infof("gatherScopedServerData: failed to refresh cluster assignments: %v", err)
	}

	groups, err := d.GetClusterTopology(ctx, connectionIDs)
	if err != nil {
		logger.Errorf("GetScopedSnapshot: failed to get cluster topology: %v", err)
		return
	}

	var allServers []EstateServerSummary
	for _, group := range groups {
		for _, cluster := range group.Clusters {
			flattenTopologyServers(cluster.Servers, &allServers)
		}
	}

	// Filter to only the requested connections.
	var servers []EstateServerSummary
	for i := range allServers {
		if idSet[allServers[i].ID] {
			allServers[i].Role = d.mapPrimaryRoleToDisplayRole(allServers[i].Role)
			servers = append(servers, allServers[i])

			switch {
			case allServers[i].Status == "offline":
				snapshot.ServerOffline++
			case allServers[i].ActiveAlertCount > 0:
				snapshot.ServerWarning++
			default:
				snapshot.ServerOnline++
			}
		}
	}

	snapshot.Servers = servers
	snapshot.ServerTotal = len(servers)
}

// gatherScopedAlertData populates alert severity counts and top alerts
// for a specific set of connection IDs.
func (d *Datastore) gatherScopedAlertData(ctx context.Context, snapshot *EstateSnapshot, connectionIDs []int) {
	activeStatus := "active"
	result, err := d.GetAlerts(ctx, AlertListFilter{
		Status:        &activeStatus,
		ConnectionIDs: connectionIDs,
		Limit:         500,
	})
	if err != nil {
		logger.Errorf("GetScopedSnapshot: failed to get alerts: %v", err)
		return
	}

	snapshot.AlertTotal = int(result.Total)

	for i := range result.Alerts {
		switch result.Alerts[i].Severity {
		case "critical":
			snapshot.AlertCritical++
		case "warning":
			snapshot.AlertWarning++
		case "info":
			snapshot.AlertInfo++
		}

		if i < 10 {
			snapshot.TopAlerts = append(snapshot.TopAlerts, EstateAlertSummary{
				Title:      result.Alerts[i].Title,
				ServerName: result.Alerts[i].ServerName,
				Severity:   result.Alerts[i].Severity,
			})
		}
	}
}

// gatherScopedRecentEvents populates recent events from the last 24
// hours for a specific set of connection IDs.
func (d *Datastore) gatherScopedRecentEvents(ctx context.Context, snapshot *EstateSnapshot, connectionIDs []int) {
	now := time.Now().UTC()
	dayAgo := now.Add(-24 * time.Hour)

	result, err := d.GetTimelineEvents(ctx, TimelineFilter{
		StartTime:     dayAgo,
		EndTime:       now,
		ConnectionIDs: connectionIDs,
		EventTypes: []string{
			EventTypeRestart,
			EventTypeConfigChange,
			EventTypeBlackoutStarted,
			EventTypeBlackoutEnded,
		},
		Limit: 20,
	})
	if err != nil {
		logger.Errorf("GetScopedSnapshot: failed to get timeline events: %v", err)
		return
	}

	for i := range result.Events {
		snapshot.RecentEvents = append(snapshot.RecentEvents, EstateEventSummary{
			EventType:  result.Events[i].EventType,
			ServerName: result.Events[i].ServerName,
			OccurredAt: result.Events[i].OccurredAt,
			Severity:   result.Events[i].Severity,
			Title:      result.Events[i].Title,
			Summary:    result.Events[i].Summary,
		})
	}
}

// ConnectionContext holds comprehensive system context for a monitored connection
type ConnectionContext struct {
	ConnectionID int                `json:"connection_id"`
	ServerName   string             `json:"server_name"`
	PostgreSQL   *PostgreSQLContext `json:"postgresql,omitempty"`
	System       *SystemContext     `json:"system,omitempty"`
}

// PostgreSQLContext holds PostgreSQL server information
type PostgreSQLContext struct {
	Version             string            `json:"version,omitempty"`
	VersionNum          int               `json:"version_num,omitempty"`
	MaxConnections      int               `json:"max_connections,omitempty"`
	DataDirectory       string            `json:"data_directory,omitempty"`
	InstalledExtensions []string          `json:"installed_extensions,omitempty"`
	Settings            map[string]string `json:"settings,omitempty"`
}

// SystemContext holds operating system and hardware information
type SystemContext struct {
	OSName       string         `json:"os_name,omitempty"`
	OSVersion    string         `json:"os_version,omitempty"`
	Architecture string         `json:"architecture,omitempty"`
	Hostname     string         `json:"hostname,omitempty"`
	CPU          *CPUContext    `json:"cpu,omitempty"`
	Memory       *MemoryContext `json:"memory,omitempty"`
	Disks        []DiskContext  `json:"disks,omitempty"`
}

// CPUContext holds CPU information
type CPUContext struct {
	Model             string `json:"model,omitempty"`
	Cores             int    `json:"cores,omitempty"`
	LogicalProcessors int    `json:"logical_processors,omitempty"`
}

// MemoryContext holds memory information
type MemoryContext struct {
	TotalBytes int64 `json:"total_bytes,omitempty"`
	FreeBytes  int64 `json:"free_bytes,omitempty"`
}

// DiskContext holds disk information for a single mount point
type DiskContext struct {
	MountPoint     string `json:"mount_point"`
	FilesystemType string `json:"filesystem_type,omitempty"`
	TotalBytes     int64  `json:"total_bytes,omitempty"`
	UsedBytes      int64  `json:"used_bytes,omitempty"`
	FreeBytes      int64  `json:"free_bytes,omitempty"`
}

// GetConnectionContext retrieves comprehensive system context for a connection
func (d *Datastore) GetConnectionContext(ctx context.Context, connectionID int) (*ConnectionContext, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Get connection name
	var serverName string
	err := d.pool.QueryRow(ctx,
		`SELECT name FROM connections WHERE id = $1`, connectionID).Scan(&serverName)
	if err != nil {
		return nil, fmt.Errorf("connection not found: %w", err)
	}

	result := &ConnectionContext{
		ConnectionID: connectionID,
		ServerName:   serverName,
	}

	// Query pg_server_info for PostgreSQL version, max_connections, extensions, etc.
	pgCtx := &PostgreSQLContext{}
	hasPGData := false

	var version sql.NullString
	var versionNum sql.NullInt32
	var maxConns sql.NullInt32
	var dataDir sql.NullString
	var extensions []string

	err = d.pool.QueryRow(ctx, `
		SELECT server_version, server_version_num, max_connections,
		       data_directory, installed_extensions
		FROM metrics.pg_server_info
		WHERE connection_id = $1
		ORDER BY collected_at DESC
		LIMIT 1
	`, connectionID).Scan(&version, &versionNum, &maxConns, &dataDir, &extensions)
	if err == nil {
		hasPGData = true
		if version.Valid {
			pgCtx.Version = version.String
		}
		if versionNum.Valid {
			pgCtx.VersionNum = int(versionNum.Int32)
		}
		if maxConns.Valid {
			pgCtx.MaxConnections = int(maxConns.Int32)
		}
		if dataDir.Valid {
			pgCtx.DataDirectory = dataDir.String
		}
		if len(extensions) > 0 {
			pgCtx.InstalledExtensions = extensions
		}
	}

	// Query pg_settings for key configuration parameters
	settingsRows, err := d.pool.Query(ctx, `
		SELECT name, setting
		FROM metrics.pg_settings
		WHERE connection_id = $1
		  AND name IN (
		      'shared_buffers', 'work_mem', 'effective_cache_size',
		      'maintenance_work_mem', 'max_worker_processes',
		      'max_parallel_workers', 'max_parallel_workers_per_gather',
		      'wal_level', 'wal_buffers', 'random_page_cost',
		      'effective_io_concurrency', 'checkpoint_completion_target',
		      'huge_pages', 'temp_buffers'
		  )
		  AND collected_at = (
		      SELECT MAX(collected_at)
		      FROM metrics.pg_settings
		      WHERE connection_id = $1
		  )
		ORDER BY name
	`, connectionID)
	if err == nil {
		defer settingsRows.Close()
		settings := make(map[string]string)
		for settingsRows.Next() {
			var name string
			var setting sql.NullString
			if err := settingsRows.Scan(&name, &setting); err == nil && setting.Valid {
				settings[name] = setting.String
			}
		}
		if len(settings) > 0 {
			hasPGData = true
			pgCtx.Settings = settings
		}
	}

	if hasPGData {
		result.PostgreSQL = pgCtx
	}

	// Query system information (requires system_stats extension)
	sysCtx := &SystemContext{}
	hasSysData := false

	// OS info
	var osName, osVersion, architecture, hostname sql.NullString
	err = d.pool.QueryRow(ctx, `
		SELECT name, version, architecture, host_name
		FROM metrics.pg_sys_os_info
		WHERE connection_id = $1
		ORDER BY collected_at DESC
		LIMIT 1
	`, connectionID).Scan(&osName, &osVersion, &architecture, &hostname)
	if err == nil {
		hasSysData = true
		if osName.Valid {
			sysCtx.OSName = osName.String
		}
		if osVersion.Valid {
			sysCtx.OSVersion = osVersion.String
		}
		if architecture.Valid {
			sysCtx.Architecture = architecture.String
		}
		if hostname.Valid {
			sysCtx.Hostname = hostname.String
		}
	}

	// CPU info
	var cpuModel sql.NullString
	var cores, logicalProcs sql.NullInt32
	err = d.pool.QueryRow(ctx, `
		SELECT model_name, no_of_cores, logical_processor
		FROM metrics.pg_sys_cpu_info
		WHERE connection_id = $1
		ORDER BY collected_at DESC
		LIMIT 1
	`, connectionID).Scan(&cpuModel, &cores, &logicalProcs)
	if err == nil {
		hasSysData = true
		cpu := &CPUContext{}
		hasCPU := false
		if cpuModel.Valid {
			cpu.Model = cpuModel.String
			hasCPU = true
		}
		if cores.Valid {
			cpu.Cores = int(cores.Int32)
			hasCPU = true
		}
		if logicalProcs.Valid {
			cpu.LogicalProcessors = int(logicalProcs.Int32)
			hasCPU = true
		}
		if hasCPU {
			sysCtx.CPU = cpu
		}
	}

	// Memory info
	var totalMem, freeMem sql.NullInt64
	err = d.pool.QueryRow(ctx, `
		SELECT total_memory, free_memory
		FROM metrics.pg_sys_memory_info
		WHERE connection_id = $1
		ORDER BY collected_at DESC
		LIMIT 1
	`, connectionID).Scan(&totalMem, &freeMem)
	if err == nil {
		hasSysData = true
		if totalMem.Valid || freeMem.Valid {
			mem := &MemoryContext{}
			if totalMem.Valid {
				mem.TotalBytes = totalMem.Int64
			}
			if freeMem.Valid {
				mem.FreeBytes = freeMem.Int64
			}
			sysCtx.Memory = mem
		}
	}

	// Disk info
	diskRows, err := d.pool.Query(ctx, `
		SELECT mount_point, file_system_type, total_space, used_space, free_space
		FROM metrics.pg_sys_disk_info
		WHERE connection_id = $1
		  AND collected_at = (
		      SELECT MAX(collected_at)
		      FROM metrics.pg_sys_disk_info
		      WHERE connection_id = $1
		  )
		ORDER BY mount_point
	`, connectionID)
	if err == nil {
		defer diskRows.Close()
		var disks []DiskContext
		for diskRows.Next() {
			var mountPoint string
			var fsType sql.NullString
			var totalSpace, usedSpace, freeSpace sql.NullInt64
			if err := diskRows.Scan(&mountPoint, &fsType, &totalSpace,
				&usedSpace, &freeSpace); err == nil {
				disk := DiskContext{MountPoint: mountPoint}
				if fsType.Valid {
					disk.FilesystemType = fsType.String
				}
				if totalSpace.Valid {
					disk.TotalBytes = totalSpace.Int64
				}
				if usedSpace.Valid {
					disk.UsedBytes = usedSpace.Int64
				}
				if freeSpace.Valid {
					disk.FreeBytes = freeSpace.Int64
				}
				disks = append(disks, disk)
			}
		}
		if len(disks) > 0 {
			hasSysData = true
			sysCtx.Disks = disks
		}
	}

	if hasSysData {
		result.System = sysCtx
	}

	return result, nil
}

// CreateManualCluster creates a cluster with no auto_cluster_key and an
// explicit replication_type. If groupID is nil the default group is used.
func (d *Datastore) CreateManualCluster(ctx context.Context, name, description, replicationType string, groupID *int) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	resolvedGroupID := 0
	if groupID != nil {
		resolvedGroupID = *groupID
	} else {
		info, err := d.getDefaultGroupInternal(ctx)
		if err != nil {
			return 0, fmt.Errorf("failed to get default group: %w", err)
		}
		resolvedGroupID = info.ID
	}

	var clusterID int
	query := `
        INSERT INTO clusters (name, description, replication_type, group_id)
        VALUES ($1, $2, $3, $4)
        RETURNING id
    `
	err := d.pool.QueryRow(ctx, query, name, description, replicationType, resolvedGroupID).Scan(&clusterID)
	if err != nil {
		return 0, fmt.Errorf("failed to create manual cluster: %w", err)
	}

	return clusterID, nil
}

// GetConnectionClusterInfo returns the cluster-related information for a
// connection including the joined cluster details. Fields are nil when the
// connection has no cluster assignment.
func (d *Datastore) GetConnectionClusterInfo(ctx context.Context, connectionID int) (*ConnectionClusterInfo, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
        SELECT c.cluster_id, c.role, c.membership_source,
               cl.name, cl.replication_type, cl.auto_cluster_key
        FROM connections c
        LEFT JOIN clusters cl ON c.cluster_id = cl.id
        WHERE c.id = $1
    `

	var info ConnectionClusterInfo
	err := d.pool.QueryRow(ctx, query, connectionID).Scan(
		&info.ClusterID, &info.Role, &info.MembershipSource,
		&info.ClusterName, &info.ReplicationType, &info.AutoClusterKey,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrConnectionNotFound
		}
		return nil, fmt.Errorf("failed to get connection cluster info: %w", err)
	}

	return &info, nil
}

// ListClustersForAutocomplete returns all clusters ordered by name for use
// in autocomplete and selection UIs.
func (d *Datastore) ListClustersForAutocomplete(ctx context.Context) ([]ClusterSummary, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
        SELECT id, name, replication_type, auto_cluster_key
        FROM clusters
        WHERE dismissed = FALSE
        ORDER BY name
    `

	rows, err := d.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list clusters: %w", err)
	}
	defer rows.Close()

	clusters := make([]ClusterSummary, 0)
	for rows.Next() {
		var cs ClusterSummary
		if err := rows.Scan(&cs.ID, &cs.Name, &cs.ReplicationType, &cs.AutoClusterKey); err != nil {
			return nil, fmt.Errorf("failed to scan cluster: %w", err)
		}
		clusters = append(clusters, cs)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating clusters: %w", err)
	}

	return clusters, nil
}

// ResetMembershipSource sets membership_source to 'auto' on a connection
// so that auto-detection resumes managing its cluster assignment.
func (d *Datastore) ResetMembershipSource(ctx context.Context, connectionID int) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := `
        UPDATE connections
        SET membership_source = 'auto', updated_at = CURRENT_TIMESTAMP
        WHERE id = $1
    `

	result, err := d.pool.Exec(ctx, query, connectionID)
	if err != nil {
		return fmt.Errorf("failed to reset membership source: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrConnectionNotFound
	}

	return nil
}

// GetClusterRelationships returns all node relationships for a cluster
// with source and target connection names joined from the connections table.
func (d *Datastore) GetClusterRelationships(ctx context.Context, clusterID int) ([]NodeRelationship, error) {
	query := `
        SELECT r.id, r.cluster_id,
               r.source_connection_id, r.target_connection_id,
               sc.name AS source_name, tc.name AS target_name,
               r.relationship_type, r.is_auto_detected
        FROM cluster_node_relationships r
        JOIN connections sc ON sc.id = r.source_connection_id
        JOIN connections tc ON tc.id = r.target_connection_id
        WHERE r.cluster_id = $1
        ORDER BY r.source_connection_id, r.target_connection_id
    `

	rows, err := d.pool.Query(ctx, query, clusterID)
	if err != nil {
		return nil, fmt.Errorf("failed to query cluster relationships: %w", err)
	}
	defer rows.Close()

	var relationships []NodeRelationship
	for rows.Next() {
		var rel NodeRelationship
		if err := rows.Scan(
			&rel.ID, &rel.ClusterID,
			&rel.SourceConnectionID, &rel.TargetConnectionID,
			&rel.SourceName, &rel.TargetName,
			&rel.RelationshipType, &rel.IsAutoDetected,
		); err != nil {
			return nil, fmt.Errorf("failed to scan relationship: %w", err)
		}
		relationships = append(relationships, rel)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating relationships: %w", err)
	}

	return relationships, nil
}

// SetNodeRelationships replaces manual (non-auto-detected) relationships
// for a given source node in a cluster. This runs in a transaction:
// first deleting existing manual rows for the source, then inserting
// the new ones with is_auto_detected = FALSE.
func (d *Datastore) SetNodeRelationships(ctx context.Context, clusterID int, sourceConnectionID int, relationships []RelationshipInput) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // Rollback is no-op if already committed

	// Delete existing manual relationships for this source in this cluster
	_, err = tx.Exec(ctx,
		`DELETE FROM cluster_node_relationships
         WHERE cluster_id = $1 AND source_connection_id = $2 AND is_auto_detected = FALSE`,
		clusterID, sourceConnectionID,
	)
	if err != nil {
		return fmt.Errorf("failed to delete existing manual relationships: %w", err)
	}

	// Insert new relationships
	for _, rel := range relationships {
		_, err = tx.Exec(ctx,
			`INSERT INTO cluster_node_relationships
             (cluster_id, source_connection_id, target_connection_id, relationship_type, is_auto_detected)
             VALUES ($1, $2, $3, $4, FALSE)
             ON CONFLICT (cluster_id, source_connection_id, target_connection_id, relationship_type) DO NOTHING`,
			clusterID, sourceConnectionID, rel.TargetConnectionID, rel.RelationshipType,
		)
		if err != nil {
			return fmt.Errorf("failed to insert relationship: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// SyncAutoDetectedRelationships inserts auto-detected relationships for a
// cluster. For each detected relationship it performs an INSERT ... ON
// CONFLICT DO NOTHING so that existing rows are preserved. This method
// NEVER removes existing rows; auto-detection only adds.
func (d *Datastore) SyncAutoDetectedRelationships(ctx context.Context, clusterID int, detected []AutoRelationshipInput) error {
	for _, rel := range detected {
		_, err := d.pool.Exec(ctx,
			`INSERT INTO cluster_node_relationships
             (cluster_id, source_connection_id, target_connection_id, relationship_type, is_auto_detected)
             VALUES ($1, $2, $3, $4, TRUE)
             ON CONFLICT (cluster_id, source_connection_id, target_connection_id, relationship_type)
             DO NOTHING`,
			clusterID, rel.SourceConnectionID, rel.TargetConnectionID, rel.RelationshipType,
		)
		if err != nil {
			return fmt.Errorf("failed to sync auto-detected relationship: %w", err)
		}
	}

	return nil
}

// RemoveNodeRelationship deletes a single relationship by its ID.
func (d *Datastore) RemoveNodeRelationship(ctx context.Context, relationshipID int) error {
	result, err := d.pool.Exec(ctx,
		`DELETE FROM cluster_node_relationships WHERE id = $1`,
		relationshipID,
	)
	if err != nil {
		return fmt.Errorf("failed to delete relationship: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("relationship not found")
	}

	return nil
}

// ClearNodeRelationships deletes all manual (is_auto_detected = FALSE)
// relationships for a source node in a cluster.
func (d *Datastore) ClearNodeRelationships(ctx context.Context, clusterID int, sourceConnectionID int) error {
	_, err := d.pool.Exec(ctx,
		`DELETE FROM cluster_node_relationships
         WHERE cluster_id = $1 AND source_connection_id = $2 AND is_auto_detected = FALSE`,
		clusterID, sourceConnectionID,
	)
	if err != nil {
		return fmt.Errorf("failed to clear manual relationships: %w", err)
	}

	return nil
}

// AddServerToCluster assigns a connection to a cluster with manual
// membership source. The caller provides the cluster ID, connection ID,
// and an optional role for the connection within the cluster.
func (d *Datastore) AddServerToCluster(ctx context.Context, clusterID int, connectionID int, role *string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Verify the cluster exists and is not dismissed
	var clusterExists bool
	err := d.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM clusters WHERE id = $1 AND dismissed = FALSE)`,
		clusterID,
	).Scan(&clusterExists)
	if err != nil {
		return fmt.Errorf("failed to check cluster existence: %w", err)
	}
	if !clusterExists {
		return ErrClusterNotFound
	}

	// Verify the connection exists
	var connExists bool
	err = d.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM connections WHERE id = $1)`,
		connectionID,
	).Scan(&connExists)
	if err != nil {
		return fmt.Errorf("failed to check connection existence: %w", err)
	}
	if !connExists {
		return ErrConnectionNotFound
	}

	query := `
        UPDATE connections
        SET cluster_id = $2, role = $3, membership_source = 'manual',
            updated_at = CURRENT_TIMESTAMP
        WHERE id = $1
    `

	_, err = d.pool.Exec(ctx, query, connectionID, clusterID, role)
	if err != nil {
		return fmt.Errorf("failed to add server to cluster: %w", err)
	}

	return nil
}

// RemoveServerFromCluster clears the cluster assignment for a connection
// and deletes all relationships in cluster_node_relationships where the
// connection is a source or target within the cluster.
func (d *Datastore) RemoveServerFromCluster(ctx context.Context, clusterID int, connectionID int) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Verify the connection belongs to this cluster
	var belongs bool
	err := d.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM connections WHERE id = $1 AND cluster_id = $2)`,
		connectionID, clusterID,
	).Scan(&belongs)
	if err != nil {
		return fmt.Errorf("failed to check connection cluster membership: %w", err)
	}
	if !belongs {
		return ErrConnectionNotFound
	}

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // Rollback is no-op if already committed

	// Delete all relationships where this connection is source or target
	_, err = tx.Exec(ctx,
		`DELETE FROM cluster_node_relationships
         WHERE cluster_id = $1
           AND (source_connection_id = $2 OR target_connection_id = $2)`,
		clusterID, connectionID,
	)
	if err != nil {
		return fmt.Errorf("failed to delete relationships: %w", err)
	}

	// Clear cluster assignment and reset membership source
	_, err = tx.Exec(ctx,
		`UPDATE connections
         SET cluster_id = NULL, role = NULL, membership_source = 'auto',
             updated_at = CURRENT_TIMESTAMP
         WHERE id = $1`,
		connectionID,
	)
	if err != nil {
		return fmt.Errorf("failed to clear cluster assignment: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// IsConnectionInCluster returns true if the given connection belongs to the
// specified cluster (i.e. connections.cluster_id matches).
func (d *Datastore) IsConnectionInCluster(ctx context.Context, clusterID int, connectionID int) (bool, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var exists bool
	err := d.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM connections WHERE id = $1 AND cluster_id = $2)`,
		connectionID, clusterID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check connection cluster membership: %w", err)
	}

	return exists, nil
}
