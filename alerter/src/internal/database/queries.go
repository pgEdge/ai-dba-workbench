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

// MonitoredConnection represents a connection and its error status
type MonitoredConnection struct {
	ConnectionID    int
	Name            string
	ConnectionError *string
}

// GetMonitoredConnectionErrors retrieves all monitored connections and their error status
func (d *Datastore) GetMonitoredConnectionErrors(ctx context.Context) ([]MonitoredConnection, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, name, connection_error
		FROM connections
		WHERE is_monitored = true
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to get monitored connection errors: %w", err)
	}
	defer rows.Close()

	var connections []MonitoredConnection
	for rows.Next() {
		var conn MonitoredConnection
		if err := rows.Scan(&conn.ConnectionID, &conn.Name, &conn.ConnectionError); err != nil {
			return nil, fmt.Errorf("failed to scan monitored connection: %w", err)
		}
		connections = append(connections, conn)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return connections, nil
}

// GetActiveConnectionAlert checks if there's an existing active connection alert
func (d *Datastore) GetActiveConnectionAlert(ctx context.Context, connectionID int) (alertID int64, description string, found bool, err error) {
	err = d.pool.QueryRow(ctx, `
		SELECT id, description FROM alerts
		WHERE alert_type = 'connection' AND connection_id = $1 AND status = 'active'
		LIMIT 1
	`, connectionID).Scan(&alertID, &description)

	if err != nil {
		if err == pgx.ErrNoRows {
			return 0, "", false, nil
		}
		return 0, "", false, fmt.Errorf("failed to get active connection alert: %w", err)
	}
	return alertID, description, true, nil
}

// CreateConnectionAlert creates a new connection error alert
func (d *Datastore) CreateConnectionAlert(ctx context.Context, connectionID int, name string, errorMsg string) (*Alert, error) {
	alert := &Alert{
		AlertType:    "connection",
		ConnectionID: connectionID,
		Severity:     "critical",
		Title:        fmt.Sprintf("Connection error: %s", name),
		Description:  errorMsg,
		Status:       "active",
		TriggeredAt:  time.Now(),
	}

	err := d.pool.QueryRow(ctx, `
		INSERT INTO alerts (alert_type, connection_id, severity, title, description, status, triggered_at)
		VALUES ('connection', $1, 'critical', $2, $3, 'active', NOW())
		RETURNING id
	`, connectionID, alert.Title, errorMsg).Scan(&alert.ID)

	if err != nil {
		return nil, fmt.Errorf("failed to create connection alert: %w", err)
	}
	return alert, nil
}

// UpdateConnectionAlertDescription updates the description of a connection alert
func (d *Datastore) UpdateConnectionAlertDescription(ctx context.Context, alertID int64, description string) error {
	_, err := d.pool.Exec(ctx, `
		UPDATE alerts SET description = $1, last_updated = NOW() WHERE id = $2
	`, description, alertID)
	return err
}

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

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return rules, nil
}

// GetEffectiveThreshold returns the threshold settings for a rule/connection
// Resolution order: server override > cluster override > group override > estate defaults
func (d *Datastore) GetEffectiveThreshold(ctx context.Context, ruleID int64, connectionID int, dbName *string) (threshold float64, operator string, severity string, enabled bool) {
	// 1. Try server-level override
	err := d.pool.QueryRow(ctx, `
		SELECT threshold, operator, severity, enabled
		FROM alert_thresholds
		WHERE scope = 'server' AND rule_id = $1 AND connection_id = $2
		  AND (database_name = $3 OR ($3 IS NULL AND database_name IS NULL) OR database_name IS NULL)
		ORDER BY database_name IS NULL ASC
		LIMIT 1
	`, ruleID, connectionID, dbName).Scan(&threshold, &operator, &severity, &enabled)
	if err == nil {
		return threshold, operator, severity, enabled
	}

	// 2. Try cluster-level override
	err = d.pool.QueryRow(ctx, `
		SELECT at.threshold, at.operator, at.severity, at.enabled
		FROM alert_thresholds at
		JOIN connections c ON c.cluster_id = at.cluster_id
		WHERE at.scope = 'cluster' AND at.rule_id = $1 AND c.id = $2
	`, ruleID, connectionID).Scan(&threshold, &operator, &severity, &enabled)
	if err == nil {
		return threshold, operator, severity, enabled
	}

	// 3. Try group-level override
	err = d.pool.QueryRow(ctx, `
		SELECT at.threshold, at.operator, at.severity, at.enabled
		FROM alert_thresholds at
		JOIN clusters cl ON cl.group_id = at.group_id
		JOIN connections c ON c.cluster_id = cl.id
		WHERE at.scope = 'group' AND at.rule_id = $1 AND c.id = $2
	`, ruleID, connectionID).Scan(&threshold, &operator, &severity, &enabled)
	if err == nil {
		return threshold, operator, severity, enabled
	}

	// 4. Fall back to estate defaults from alert_rules
	err = d.pool.QueryRow(ctx, `
		SELECT default_threshold, default_operator, default_severity, default_enabled
		FROM alert_rules
		WHERE id = $1
	`, ruleID).Scan(&threshold, &operator, &severity, &enabled)
	if err != nil {
		return 0, "", "", false
	}

	return threshold, operator, severity, enabled
}

// NOTE: Metric query functions (queryMetricValues, queryMetricValuesWithDB,
// queryMetricValuesWithDBAndObject, GetLatestMetricValues, GetLatestMetricValue,
// GetHistoricalMetricValues) are now in metric_queries.go and use the registry
// pattern defined in metric_registry.go.

// NOTE: The large switch-statement metric functions that were here have been
// replaced with the registry pattern. See metric_registry.go for SQL definitions
// and metric_queries.go for the query execution functions.

// IsBlackoutActive checks if any blackout is currently active for a connection.
// It resolves the full hierarchy: estate, group, cluster, and server scopes.
func (d *Datastore) IsBlackoutActive(ctx context.Context, connectionID *int, dbName *string) (bool, error) {
	now := time.Now()
	var count int

	// Check blackouts across all scope levels using the hierarchy:
	// connections.cluster_id -> clusters.group_id
	err := d.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM blackouts b
		WHERE b.start_time <= $1 AND b.end_time >= $1
		AND (
			b.scope = 'estate'
			OR (b.scope = 'group' AND b.group_id = (
				SELECT cl.group_id FROM clusters cl
				JOIN connections c ON c.cluster_id = cl.id
				WHERE c.id = $2))
			OR (b.scope = 'cluster' AND b.cluster_id = (
				SELECT c.cluster_id FROM connections c WHERE c.id = $2))
			OR (b.scope = 'server'
				AND (b.connection_id IS NULL OR b.connection_id = $2)
				AND (b.database_name IS NULL OR b.database_name = $3))
		)
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
		SELECT id, scope, connection_id, group_id, cluster_id,
		       database_name, name, cron_expression,
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
		err := rows.Scan(&s.ID, &s.Scope, &s.ConnectionID, &s.GroupID,
			&s.ClusterID, &s.DatabaseName, &s.Name,
			&s.CronExpression, &s.DurationMinutes, &s.Timezone, &s.Reason,
			&s.Enabled, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan blackout schedule: %w", err)
		}
		schedules = append(schedules, &s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return schedules, nil
}

// CreateBlackout creates a new blackout entry
func (d *Datastore) CreateBlackout(ctx context.Context, blackout *Blackout) error {
	return d.pool.QueryRow(ctx, `
		INSERT INTO blackouts (scope, connection_id, group_id, cluster_id,
		                       database_name, reason, start_time,
		                       end_time, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id
	`, blackout.Scope, blackout.ConnectionID, blackout.GroupID,
		blackout.ClusterID, blackout.DatabaseName, blackout.Reason,
		blackout.StartTime, blackout.EndTime, blackout.CreatedBy,
		blackout.CreatedAt).Scan(&blackout.ID)
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

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return ids, nil
}

// ProbeStaleness is defined in types.go

// GetProbeStalenessByConnection retrieves staleness ratios for all enabled probes
// on monitored connections. The caller decides which entries exceed the threshold.
func (d *Datastore) GetProbeStalenessByConnection(ctx context.Context) ([]ProbeStaleness, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT c.id, c.name, pa.probe_name, pc.collection_interval_seconds,
		       EXTRACT(EPOCH FROM (NOW() - pa.last_collected)) / pc.collection_interval_seconds AS staleness_ratio
		FROM probe_availability pa
		JOIN probe_configs pc ON pc.name = pa.probe_name AND pc.connection_id IS NULL
		JOIN connections c ON c.id = pa.connection_id
		WHERE pa.is_available = TRUE
		  AND pc.is_enabled = TRUE
		  AND c.is_monitored = TRUE
		  AND pa.last_collected IS NOT NULL
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to get probe staleness: %w", err)
	}
	defer rows.Close()

	var results []ProbeStaleness
	for rows.Next() {
		var ps ProbeStaleness
		if err := rows.Scan(&ps.ConnectionID, &ps.ConnectionName, &ps.ProbeName,
			&ps.CollectionInterval, &ps.StalenessRatio); err != nil {
			return nil, fmt.Errorf("failed to scan probe staleness: %w", err)
		}
		results = append(results, ps)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return results, nil
}

// GetAlertRuleByName retrieves an alert rule by its unique name
func (d *Datastore) GetAlertRuleByName(ctx context.Context, name string) (*AlertRule, error) {
	var rule AlertRule
	err := d.pool.QueryRow(ctx, `
		SELECT id, name, description, category, metric_name, default_operator,
		       default_threshold, default_severity, default_enabled, required_extension,
		       is_built_in, created_at
		FROM alert_rules
		WHERE name = $1
	`, name).Scan(&rule.ID, &rule.Name, &rule.Description, &rule.Category,
		&rule.MetricName, &rule.DefaultOperator, &rule.DefaultThreshold,
		&rule.DefaultSeverity, &rule.DefaultEnabled, &rule.RequiredExtension,
		&rule.IsBuiltIn, &rule.CreatedAt)

	if err != nil {
		return nil, err
	}
	return &rule, nil
}

// GetClusterPeers returns information about other connections in the same
// cluster as the given connection, including their latest node role.
// Returns an empty slice if the connection has no cluster.
func (d *Datastore) GetClusterPeers(ctx context.Context, connectionID int) ([]*ClusterPeerInfo, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT c.id, c.name, COALESCE(lr.primary_role, 'unknown')
		FROM connections c
		LEFT JOIN LATERAL (
			SELECT primary_role
			FROM metrics.pg_node_role
			WHERE connection_id = c.id
			ORDER BY collected_at DESC
			LIMIT 1
		) lr ON true
		WHERE c.cluster_id = (SELECT cluster_id FROM connections WHERE id = $1)
		  AND c.cluster_id IS NOT NULL
		  AND c.id != $1
		ORDER BY c.name
	`, connectionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster peers: %w", err)
	}
	defer rows.Close()

	var peers []*ClusterPeerInfo
	for rows.Next() {
		var peer ClusterPeerInfo
		if err := rows.Scan(&peer.ConnectionID, &peer.ConnectionName, &peer.NodeRole); err != nil {
			return nil, fmt.Errorf("failed to scan cluster peer: %w", err)
		}
		peers = append(peers, &peer)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return peers, nil
}
